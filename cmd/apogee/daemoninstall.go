package main

// `apogee daemon install` — the turnkey half of ADR 0034 decision 7: it writes the unit this host's
// supervisor wants and prints the one command that activates it.
//
// It generates and instructs; it never runs the supervisor's CLI. Registering a unit is a change to
// what a machine does when nobody is looking, and the person who owns the machine should be the one
// who types the sentence that makes it happen — which also means a generated unit can be read before
// it is trusted, and the command works the same over SSH, in a container build, and inside a
// configuration-management run that only wants the file.
//
// Generation is pure and OS-parameterized: everything the three templates interpolate arrives as an
// [installPlan] the caller resolved, so all three units are rendered — and golden-tested — on any
// host, and the only OS-specific thing left at runtime is which plan gets built.

import (
	_ "embed"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"
	"unicode/utf16"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/daemon"
)

const (
	// systemdUnitFileName is the unit's name under ~/.config/systemd/user, and systemdUnitName the
	// name systemctl calls it by — the same word without the suffix, which is why one is derived
	// from the other rather than written twice.
	systemdUnitFileName = "apogee-daemon.service"
	systemdUnitName     = "apogee-daemon"
	// launchdLabel is the launchd job label. launchd requires the plist's basename to match the
	// label, so the file name is the label plus `.plist` and nothing else.
	launchdLabel = "com.airiclenz.apogee.daemon"
	// taskFileName is the Task Scheduler definition, and taskName the name it is registered under.
	// The XML lands in the daemon's own directory rather than a supervisor directory because the
	// Task Scheduler has none: it copies the definition into its own store at registration, so what
	// `install` writes is the source a human re-registers from.
	taskFileName = "apogee-daemon-task.xml"
	taskName     = "apogee-daemon"
	// daemonLogFileName is where launchd is told to append the daemon's narration. systemd and the
	// Task Scheduler both capture stdout themselves; launchd captures nothing unless it is given a
	// path, so this is the one supervisor that needs apogee to name a file.
	daemonLogFileName = "daemon.log"
	// stopMargin is how much longer than shutdown-grace the supervisor waits before it stops asking
	// nicely. The daemon's own deadline is the grace; the margin is the room a process needs to save
	// what the cancelled Firing produced and exit, and it exists so the supervisor never becomes the
	// reason a record is lost.
	stopMargin = 30 * time.Second
)

// The three unit templates, compiled into the binary on the schedules-file precedent
// (defaultSchedulesYAML): //go:embed re-reads them on every build, so what `install` writes can
// never drift from the binary that wrote it.
var (
	//go:embed defaults/apogee-daemon.service
	systemdUnitTemplate string
	//go:embed defaults/com.airiclenz.apogee.daemon.plist
	launchdPlistTemplate string
	//go:embed defaults/apogee-daemon-task.xml
	taskXMLTemplate string
)

// unitTemplates holds the parsed form of each, keyed by the GOOS it serves. Parsing at package
// scope is deliberate: a broken template is a broken build's mistake, not a user's, and it should
// fail every test in the package rather than one unlucky install.
var unitTemplates = map[string]*template.Template{
	"linux":   template.Must(template.New(systemdUnitFileName).Parse(systemdUnitTemplate)),
	"darwin":  template.Must(template.New(launchdLabel).Parse(launchdPlistTemplate)),
	"windows": template.Must(template.New(taskFileName).Parse(taskXMLTemplate)),
}

// daemonExecutable is the seam onto "where is the binary that is running". A generated unit must
// name an absolute path — a supervisor starts with neither the shell's PATH nor its working
// directory — and a test must be able to pin that path to make the golden output stable.
// Production never reassigns it.
var daemonExecutable = os.Executable

// daemonUserHome is the seam onto the user's home directory — the anchor of both supervisor
// directories `install` writes into. It is a seam for the reason daemonExecutable is one: a test
// that let the real home through would write a real unit onto the machine running the suite.
// Production never reassigns it.
var daemonUserHome = os.UserHomeDir

// installPlan is one host's answer to "what unit, where, filled with what". Every field is already
// resolved: [renderDaemonUnit] and [daemonActivation] read nothing else and touch no disk, which is
// what makes all three targets testable from a host that is only ever one of them.
type installPlan struct {
	// GOOS names which supervisor this plan is for, spelled as Go spells it.
	GOOS string
	// Executable is the absolute path of the apogee binary the unit starts.
	Executable string
	// ConfigDir is the apogee home the unit pins with --config, or empty when the daemon should
	// resolve its home for itself the way a session does.
	ConfigDir string
	// UnitPath is the absolute path the unit is written to.
	UnitPath string
	// LogPath is where launchd is told to append stdout and stderr; unused by the other two.
	LogPath string
	// Grace is the shutdown-grace the schedules file declares, from which the supervisor's kill
	// escalation is derived.
	Grace time.Duration
}

// unitFields is everything the three templates interpolate. One struct serves all three because the
// values are the same few facts in three syntaxes, and each template names only what it needs.
type unitFields struct {
	// ExecStart is systemd's whole command line, quoted the way systemd parses it.
	ExecStart string
	// ProgramArguments is launchd's argv, one XML-escaped element per word.
	ProgramArguments []string
	// Label is the launchd job label.
	Label string
	// Command and Arguments are the Task Scheduler's split of the same argv, XML-escaped.
	Command   string
	Arguments string
	// LogPath is launchd's stdout/stderr sink, XML-escaped.
	LogPath string
	// StopSeconds is the kill escalation in whole seconds, which is the only unit systemd's
	// TimeoutStopSec and launchd's ExitTimeOut both take.
	StopSeconds int
	// StopWindow and Grace are the same two durations spelled for a human reading the file.
	StopWindow string
	Grace      string
}

// newDaemonInstallCommand builds `apogee daemon install`. It carries the same single flag its parent
// does, for the same reason and with one addition: the home passed here is also the home the
// generated unit pins, so a daemon started by the supervisor reads the schedules file the person who
// ran `install` meant it to read.
func newDaemonInstallCommand() *cobra.Command {
	var opts config.Options

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write this host's supervisor unit for `apogee daemon` and print how to activate it",
		Long: "apogee daemon install writes the unit that makes this host run `apogee daemon` for\n" +
			"you: a systemd user unit on Linux, a launchd agent on macOS, a Task Scheduler\n" +
			"definition on Windows. It then prints the one command that activates it.\n\n" +
			"It never runs that command itself. Putting a standing process on a machine is the\n" +
			"machine owner's sentence to type, and a generated unit is worth reading before it is\n" +
			"trusted.\n\n" +
			"The unit names the absolute path of the apogee binary that generated it and derives\n" +
			"the supervisor's kill escalation from shutdown-grace in schedules.yaml. Re-run this\n" +
			"after changing shutdown-grace or moving the binary: it regenerates the file and says\n" +
			"whether anything changed.\n\n" +
			"--config is the only apogee home a unit records. A home set through APOGEE_CONFIG is\n" +
			"the shell's, not the supervisor's, so pass it as a flag here if the daemon should use\n" +
			"it.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonInstall(runtime.GOOS, opts.ConfigDir, cmd.Flags().Changed("config"),
				cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&opts.ConfigDir, "config", "",
		"apogee home directory the generated unit pins (default: resolved at run time)")

	return cmd
}

// runDaemonInstall resolves this host's plan, writes the unit and narrates what happened. It returns
// an error for every refusal — an unsupported OS, an unlocatable binary, a directory it may not
// write — which main turns into exit 1.
func runDaemonInstall(goos, configDir string, configPassed bool, out io.Writer) error {
	home, err := config.ApogeeHome(configDir)
	if err != nil {
		return err
	}
	userHome, err := daemonUserHome()
	if err != nil {
		return fmt.Errorf("apogee daemon install: resolve home directory: %w", err)
	}
	executable, err := daemonExecutable()
	if err != nil {
		return fmt.Errorf("apogee daemon install: locate the apogee binary: %w", err)
	}

	// The unit pins the RESOLVED home rather than the flag as typed: a supervisor starts the daemon
	// from a working directory nobody chose, where a relative --config would mean somewhere else.
	pinned := ""
	if configPassed {
		pinned = home
	}
	grace, notice := installGrace(filepath.Join(home, daemonDirName, schedulesFileName))
	if notice != "" {
		fmt.Fprintln(out, notice)
	}

	plan, err := daemonInstallPlan(goos, userHome, home, executable, pinned, grace)
	if err != nil {
		return err
	}
	content, err := renderDaemonUnit(plan)
	if err != nil {
		return err
	}
	existed, changed, err := writeDaemonUnit(plan, content)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, installSummary(plan.UnitPath, existed, changed))
	fmt.Fprintf(out, "stop escalation: %s (shutdown-grace %s + %s)\n",
		stopWindow(grace), grace, stopMargin)
	fmt.Fprintf(out, "\nactivate it with:\n\n    %s\n\n", daemonActivation(plan))
	fmt.Fprintln(out, "re-run `apogee daemon install` after changing shutdown-grace or moving the binary.")

	return nil
}

// daemonInstallPlan resolves where this OS's unit belongs and what it says. userHome anchors the two
// supervisor directories; apogeeHome anchors the files apogee owns.
//
// An OS with no supervisor apogee knows is a refusal naming it, not a silently skipped write: the
// daemon still runs there in the foreground, and saying so is more use than a file nothing reads.
func daemonInstallPlan(goos, userHome, apogeeHome, executable, configDir string,
	grace time.Duration) (installPlan, error) {
	plan := installPlan{
		GOOS:       goos,
		Executable: executable,
		ConfigDir:  configDir,
		LogPath:    filepath.Join(apogeeHome, daemonDirName, daemonLogFileName),
		Grace:      grace,
	}
	switch goos {
	case "linux":
		plan.UnitPath = filepath.Join(userHome, ".config", "systemd", "user", systemdUnitFileName)
	case "darwin":
		plan.UnitPath = filepath.Join(userHome, "Library", "LaunchAgents", launchdLabel+".plist")
	case "windows":
		plan.UnitPath = filepath.Join(apogeeHome, daemonDirName, taskFileName)
	default:
		return installPlan{}, fmt.Errorf("apogee daemon install: no supervisor unit for %s — run "+
			"`apogee daemon` under whatever supervises processes on this host", goos)
	}
	return plan, nil
}

// renderDaemonUnit fills this plan's template. It is the whole of the generation: no file is read,
// no host is asked anything, so the same plan renders the same bytes everywhere.
func renderDaemonUnit(plan installPlan) (string, error) {
	unit, ok := unitTemplates[plan.GOOS]
	if !ok {
		return "", fmt.Errorf("apogee daemon install: no supervisor unit for %s", plan.GOOS)
	}
	var rendered strings.Builder
	if err := unit.Execute(&rendered, unitFieldsFor(plan)); err != nil {
		return "", fmt.Errorf("apogee daemon install: render %s: %w", filepath.Base(plan.UnitPath), err)
	}
	return rendered.String(), nil
}

// unitFieldsFor spells one plan in all three syntaxes at once. Escaping happens HERE rather than in
// the templates so that each value is escaped for the syntax it actually lands in — text/template's
// own escaping knows nothing about systemd quoting or XML.
func unitFieldsFor(plan installPlan) unitFields {
	argv := []string{plan.Executable, "daemon"}
	if plan.ConfigDir != "" {
		argv = append(argv, "--config", plan.ConfigDir)
	}

	escaped := make([]string, len(argv))
	systemd := make([]string, len(argv))
	for i, arg := range argv {
		escaped[i] = xmlEscape(arg)
		systemd[i] = systemdQuote(arg)
	}

	return unitFields{
		ExecStart:        strings.Join(systemd, " "),
		ProgramArguments: escaped,
		Label:            launchdLabel,
		Command:          escaped[0],
		Arguments:        strings.Join(windowsQuoteAll(argv[1:]), " "),
		LogPath:          xmlEscape(plan.LogPath),
		StopSeconds:      int(stopWindow(plan.Grace) / time.Second),
		StopWindow:       stopWindow(plan.Grace).String(),
		Grace:            plan.Grace.String(),
	}
}

// stopWindow is the supervisor's kill escalation: the grace the daemon promises an in-flight Firing
// plus the margin it needs to save and exit. Rounded to whole seconds because both supervisors that
// take a number take seconds.
func stopWindow(grace time.Duration) time.Duration {
	return (grace + stopMargin).Round(time.Second)
}

// writeDaemonUnit writes the rendered unit, creating the supervisor's directory if it is not there,
// and reports whether a file was already present and whether these bytes differ from it. Identical
// bytes are not rewritten: a re-run that changes nothing should leave the file's mtime alone, so a
// supervisor watching it has nothing to react to.
func writeDaemonUnit(plan installPlan, content string) (existed, changed bool, err error) {
	want := daemonUnitBytes(plan, content)
	switch have, readErr := os.ReadFile(plan.UnitPath); {
	case readErr == nil:
		existed = true
		if string(have) == string(want) {
			return true, false, nil
		}
	case !errors.Is(readErr, os.ErrNotExist):
		return false, false, fmt.Errorf("apogee daemon install: read %s: %w", plan.UnitPath, readErr)
	}

	dir := filepath.Dir(plan.UnitPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return existed, false, fmt.Errorf("apogee daemon install: create %s: %w", dir, err)
	}
	// 0o600, matching the rest of the apogee home: a per-user unit is read by the user's own
	// supervisor and by nobody else, and it names the paths this account's schedules run in.
	if err := os.WriteFile(plan.UnitPath, want, 0o600); err != nil {
		return existed, false, fmt.Errorf("apogee daemon install: write %s: %w", plan.UnitPath, err)
	}
	return existed, true, nil
}

// daemonUnitBytes encodes the rendered unit the way its supervisor reads it. Two of them read UTF-8;
// `schtasks /create /xml` reads what the Task Scheduler itself exports, which is UTF-16LE with a
// byte-order mark — and the template's own declaration says so, so the bytes and the declaration
// have to agree.
func daemonUnitBytes(plan installPlan, content string) []byte {
	if plan.GOOS != "windows" {
		return []byte(content)
	}
	units := utf16.Encode([]rune("\ufeff" + content))
	encoded := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		encoded = append(encoded, byte(unit), byte(unit>>8))
	}
	return encoded
}

// daemonActivation is the one command a person types to make the written unit live. It is printed,
// never run (see the file's opening note).
func daemonActivation(plan installPlan) string {
	switch plan.GOOS {
	case "linux":
		return "systemctl --user enable --now " + systemdUnitName
	case "darwin":
		return fmt.Sprintf("launchctl load -w %q", plan.UnitPath)
	case "windows":
		return fmt.Sprintf("schtasks /create /tn %q /xml %q", taskName, plan.UnitPath)
	}
	return ""
}

// installSummary is the one line saying what the write did, in the three states a re-run can land
// in — because "did that change anything?" is the whole question a second run is asking.
func installSummary(path string, existed, changed bool) string {
	switch {
	case !existed:
		return "wrote " + path
	case changed:
		return "updated " + path
	default:
		return path + " is already up to date"
	}
}

// installGrace reads shutdown-grace out of the schedules file, falling back to the default with a
// notice when it cannot. It decodes that ONE key and validates nothing else on purpose: a unit has
// to be generable before the file is finished — the common case is installing on a host whose
// schedules are still commented out — and refusing to write a supervisor unit over an entry's typo
// would put the daemon's own validation in the way of setting the daemon up.
func installGrace(path string) (time.Duration, string) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return daemon.DefaultShutdownGrace, ""
	}
	fallback := fmt.Sprintf("using the default shutdown-grace of %s", daemon.DefaultShutdownGrace)
	if err != nil {
		return daemon.DefaultShutdownGrace, fmt.Sprintf("could not read %s (%v) — %s", path, err, fallback)
	}
	var document struct {
		ShutdownGrace *time.Duration `yaml:"shutdown-grace"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return daemon.DefaultShutdownGrace, fmt.Sprintf("could not read shutdown-grace from %s (%v) — %s",
			path, oneLine(err.Error()), fallback)
	}
	if document.ShutdownGrace == nil || *document.ShutdownGrace <= 0 {
		return daemon.DefaultShutdownGrace, ""
	}
	return *document.ShutdownGrace, ""
}

// systemdQuote spells one argument the way systemd's ExecStart parser reads it: bare when it is one
// plain word, double-quoted with backslash escapes when it is not.
func systemdQuote(arg string) string {
	if !strings.ContainsAny(arg, " \t\"'\\$%") {
		return arg
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(arg)
	return `"` + escaped + `"`
}

// windowsQuoteAll spells an argument list the way a Windows command line reads it: quoted only where
// a space would otherwise split the word in two.
func windowsQuoteAll(args []string) []string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsAny(arg, " \t") {
			quoted[i] = xmlEscape(`"` + arg + `"`)
			continue
		}
		quoted[i] = xmlEscape(arg)
	}
	return quoted
}

// xmlEscape makes one value safe to interpolate into the plist and the task definition. Both are
// XML, and a path may hold an ampersand.
func xmlEscape(value string) string {
	var escaped strings.Builder
	if err := xml.EscapeText(&escaped, []byte(value)); err != nil {
		// xml.EscapeText only fails when the writer does, and strings.Builder never does.
		return value
	}
	return escaped.String()
}
