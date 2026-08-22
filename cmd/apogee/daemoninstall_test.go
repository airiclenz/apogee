package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/airiclenz/apogee/internal/daemon"
)

// ----------------------------------------------------------------------------
// The fixed host every golden is rendered against
// ----------------------------------------------------------------------------

const (
	// installFixtureBinary and installFixtureHome are the two paths the goldens were rendered with.
	// They are literals rather than filepath.Join results so the rendered text is identical on every
	// host: what a plan resolves is a separate test (TestDaemonInstallResolvesEachUnitPath).
	installFixtureBinary = "/opt/apogee/bin/apogee"
	installFixtureHome   = "/home/tester/.apogee"
	installFixtureLog    = "/home/tester/.apogee/daemon/daemon.log"
)

// installFixture is the plan the goldens pin: a pinned home, the default grace, one fake binary.
func installFixture(goos string, grace time.Duration) installPlan {
	return installPlan{
		GOOS:       goos,
		Executable: installFixtureBinary,
		ConfigDir:  installFixtureHome,
		UnitPath:   "/unit/path/is/not/rendered",
		LogPath:    installFixtureLog,
		Grace:      grace,
	}
}

// goldenUnit reads one recorded unit out of testdata.
func goldenUnit(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name+".golden"))
	if err != nil {
		t.Fatalf("read the recorded unit: %v", err)
	}
	return string(data)
}

// ----------------------------------------------------------------------------
// Generation
// ----------------------------------------------------------------------------

// TestDaemonInstallRendersTheGoldenUnitForEveryOS pins the exact bytes of all three supervisor
// units. Generation is pure and OS-parameterized precisely so this can happen on any host: two of
// the three units are for machines the suite will never run on, and a template edit that broke one
// of them would otherwise be found by the owner rather than by CI.
func TestDaemonInstallRendersTheGoldenUnitForEveryOS(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ goos, golden string }{
		{goos: "linux", golden: systemdUnitFileName},
		{goos: "darwin", golden: launchdLabel + ".plist"},
		{goos: "windows", golden: taskFileName},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			t.Parallel()

			rendered, err := renderDaemonUnit(installFixture(tc.goos, daemon.DefaultShutdownGrace))

			if err != nil {
				t.Fatalf("render the %s unit: %v", tc.goos, err)
			}
			if want := goldenUnit(t, tc.golden); rendered != want {
				t.Errorf("the %s unit no longer matches testdata/%s.golden:\n%s",
					tc.goos, tc.golden, firstDifference(rendered, want))
			}
		})
	}
}

// TestDaemonInstallDerivesTheKillEscalationFromShutdownGrace is the one value in a unit that is not
// a constant: every supervisor has to wait out the whole grace the daemon promises an in-flight
// Firing, plus the margin it needs to save and exit, before it stops asking nicely.
func TestDaemonInstallDerivesTheKillEscalationFromShutdownGrace(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		goos  string
		grace time.Duration
		want  string
	}{
		{name: "systemd takes seconds", goos: "linux", grace: 45 * time.Second, want: "TimeoutStopSec=75"},
		{name: "systemd follows a long grace", goos: "linux", grace: time.Hour, want: "TimeoutStopSec=3630"},
		{name: "launchd takes seconds", goos: "darwin", grace: 45 * time.Second, want: "<integer>75</integer>"},
		{name: "the task xml says it in prose", goos: "windows", grace: 45 * time.Second,
			want: "(45s) to finish, and the daemon is gone within 1m15s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rendered, err := renderDaemonUnit(installFixture(tc.goos, tc.grace))

			if err != nil {
				t.Fatalf("render the %s unit: %v", tc.goos, err)
			}
			if !strings.Contains(rendered, tc.want) {
				t.Errorf("the %s unit does not carry %q:\n%s", tc.goos, tc.want, rendered)
			}
		})
	}
}

// TestDaemonInstallLeavesTheHomeUnpinnedWhenNoFlagWasPassed keeps the unpinned form honest: with no
// --config, the unit starts a bare `apogee daemon` and the daemon resolves its own home, exactly as
// a session does.
func TestDaemonInstallLeavesTheHomeUnpinnedWhenNoFlagWasPassed(t *testing.T) {
	t.Parallel()

	plan := installFixture("linux", daemon.DefaultShutdownGrace)
	plan.ConfigDir = ""

	rendered, err := renderDaemonUnit(plan)

	if err != nil {
		t.Fatalf("render the unit: %v", err)
	}
	if want := "ExecStart=" + installFixtureBinary + " daemon\n"; !strings.Contains(rendered, want) {
		t.Errorf("the unit does not start a bare daemon: want %q in\n%s", want, rendered)
	}
}

// TestDaemonInstallQuotesAPathWithSpaces covers the one host detail generation cannot assume away:
// a binary under "Program Files" must survive both the systemd parser and the XML.
func TestDaemonInstallQuotesAPathWithSpaces(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ goos, want string }{
		{goos: "linux", want: `ExecStart="/opt/Program Files/apogee"`},
		{goos: "windows", want: `<Arguments>daemon --config &#34;/home/a b/.apogee&#34;</Arguments>`},
		{goos: "darwin", want: `<string>/opt/Program Files/apogee</string>`},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			t.Parallel()

			plan := installFixture(tc.goos, daemon.DefaultShutdownGrace)
			plan.Executable = "/opt/Program Files/apogee"
			plan.ConfigDir = "/home/a b/.apogee"

			rendered, err := renderDaemonUnit(plan)

			if err != nil {
				t.Fatalf("render the %s unit: %v", tc.goos, err)
			}
			if !strings.Contains(rendered, tc.want) {
				t.Errorf("the %s unit does not carry %q:\n%s", tc.goos, tc.want, rendered)
			}
		})
	}
}

// TestDaemonInstallEncodesTheTaskDefinitionAsUTF16 pins the encoding `schtasks /create /xml` reads:
// the Task Scheduler exports UTF-16LE with a byte-order mark, and the template's own declaration
// says UTF-16, so bytes that said otherwise would be a file Windows refuses.
func TestDaemonInstallEncodesTheTaskDefinitionAsUTF16(t *testing.T) {
	t.Parallel()

	plan := installFixture("windows", daemon.DefaultShutdownGrace)
	rendered, err := renderDaemonUnit(plan)
	if err != nil {
		t.Fatalf("render the task definition: %v", err)
	}

	encoded := daemonUnitBytes(plan, rendered)

	if len(encoded) < 2 || encoded[0] != 0xFF || encoded[1] != 0xFE {
		t.Fatalf("the task definition does not start with a UTF-16LE byte-order mark: % x",
			encoded[:min(4, len(encoded))])
	}
	if decoded := decodeUTF16LE(t, encoded[2:]); decoded != rendered {
		t.Errorf("the encoded task definition does not decode back to what was rendered:\n%s",
			firstDifference(decoded, rendered))
	}
	if other := installFixture("linux", daemon.DefaultShutdownGrace); string(daemonUnitBytes(other, "x")) != "x" {
		t.Errorf("a non-Windows unit was re-encoded; want the rendered bytes unchanged")
	}
}

// TestDaemonInstallRendersWellFormedXML guards the two units a parser on another OS has to accept.
// It caught a real defect: an XML comment may not contain a double hyphen, and the prose in both
// templates named the `--config` flag inside one — well-formed to read, rejected by every parser.
func TestDaemonInstallRendersWellFormedXML(t *testing.T) {
	t.Parallel()

	for _, goos := range []string{"darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			t.Parallel()

			rendered, err := renderDaemonUnit(installFixture(goos, daemon.DefaultShutdownGrace))
			if err != nil {
				t.Fatalf("render the %s unit: %v", goos, err)
			}

			decoder := xml.NewDecoder(strings.NewReader(rendered))
			// The task definition declares UTF-16 because that is what schtasks reads; the bytes are
			// encoded at write time (daemonUnitBytes), so what is parsed here is the rendered text.
			decoder.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
			for {
				switch _, err := decoder.Token(); {
				case errors.Is(err, io.EOF):
					return
				case err != nil:
					t.Fatalf("the %s unit is not well-formed XML: %v", goos, err)
				}
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Where the unit goes, and what activates it
// ----------------------------------------------------------------------------

// TestDaemonInstallResolvesEachUnitPath pins the per-user location every supervisor reads from. The
// expectations are joined rather than written out so the test says the same thing on every host.
func TestDaemonInstallResolvesEachUnitPath(t *testing.T) {
	t.Parallel()

	userHome, apogeeHome := filepath.Join("/", "home", "tester"), filepath.Join("/", "home", "tester", ".apogee")
	for _, tc := range []struct{ goos, want string }{
		{goos: "linux", want: filepath.Join(userHome, ".config", "systemd", "user", systemdUnitFileName)},
		{goos: "darwin", want: filepath.Join(userHome, "Library", "LaunchAgents", launchdLabel+".plist")},
		{goos: "windows", want: filepath.Join(apogeeHome, daemonDirName, taskFileName)},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			t.Parallel()

			plan, err := daemonInstallPlan(tc.goos, userHome, apogeeHome, installFixtureBinary, "",
				daemon.DefaultShutdownGrace)

			if err != nil {
				t.Fatalf("plan the %s install: %v", tc.goos, err)
			}
			if plan.UnitPath != tc.want {
				t.Errorf("unit path = %q, want %q", plan.UnitPath, tc.want)
			}
			if want := filepath.Join(apogeeHome, daemonDirName, daemonLogFileName); plan.LogPath != want {
				t.Errorf("log path = %q, want %q", plan.LogPath, want)
			}
		})
	}
}

// TestDaemonInstallPinsTheActivationCommand keeps the printed sentence exact. It is the whole of
// what `install` asks a person to do, and a command that is nearly right is worse than none.
func TestDaemonInstallPinsTheActivationCommand(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ goos, unit, want string }{
		{goos: "linux", unit: "/home/tester/.config/systemd/user/apogee-daemon.service",
			want: "systemctl --user enable --now apogee-daemon"},
		{goos: "darwin", unit: "/home/tester/Library/LaunchAgents/com.airiclenz.apogee.daemon.plist",
			want: `launchctl load -w "/home/tester/Library/LaunchAgents/com.airiclenz.apogee.daemon.plist"`},
		{goos: "windows", unit: `C:\Users\tester\.apogee\daemon\apogee-daemon-task.xml`,
			want: `schtasks /create /tn "apogee-daemon" /xml "C:\\Users\\tester\\.apogee\\daemon\\apogee-daemon-task.xml"`},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			t.Parallel()

			got := daemonActivation(installPlan{GOOS: tc.goos, UnitPath: tc.unit})

			if got != tc.want {
				t.Errorf("activation = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDaemonInstallRefusesAnUnknownOS: a host with no supervisor apogee knows gets a sentence, not a
// file nothing will ever read. The daemon still runs there in the foreground.
func TestDaemonInstallRefusesAnUnknownOS(t *testing.T) {
	t.Parallel()

	_, err := daemonInstallPlan("plan9", "/home/tester", "/home/tester/.apogee", installFixtureBinary, "",
		daemon.DefaultShutdownGrace)

	if err == nil {
		t.Fatalf("planning an install for plan9 succeeded; want a refusal")
	}
	if !strings.Contains(err.Error(), "plan9") {
		t.Errorf("the refusal does not name the OS: %v", err)
	}
}

// ----------------------------------------------------------------------------
// The grace the escalation is derived from
// ----------------------------------------------------------------------------

// TestDaemonInstallReadsShutdownGraceFromTheSchedulesFile covers the one thing `install` reads off
// disk — and every way that read can come up empty. A unit has to be generable before the schedules
// file is finished, so nothing here is a refusal.
func TestDaemonInstallReadsShutdownGraceFromTheSchedulesFile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		content    string
		absent     bool
		want       time.Duration
		wantNotice bool
	}{
		{name: "the declared grace", content: "shutdown-grace: 45s\n", want: 45 * time.Second},
		{name: "no file yet", absent: true, want: daemon.DefaultShutdownGrace},
		{name: "the key absent", content: "schedules: []\n", want: daemon.DefaultShutdownGrace},
		{name: "a grace of nothing", content: "shutdown-grace: 0s\n", want: daemon.DefaultShutdownGrace},
		{name: "a file that does not parse", content: "shutdown-grace: [\n",
			want: daemon.DefaultShutdownGrace, wantNotice: true},
		{name: "an entry that does not validate", content: "shutdown-grace: 2m\nschedules:\n  - name: \"\"\n",
			want: 2 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), schedulesFileName)
			if !tc.absent {
				if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
					t.Fatalf("write the schedules file: %v", err)
				}
			}

			grace, notice := installGrace(path)

			if grace != tc.want {
				t.Errorf("grace = %s, want %s", grace, tc.want)
			}
			if (notice != "") != tc.wantNotice {
				t.Errorf("notice = %q, want a notice: %v", notice, tc.wantNotice)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The command
// ----------------------------------------------------------------------------

// TestDaemonInstallWritesThenReportsUnchangedThenUpdated drives the whole command over a temporary
// home. The three states are the whole point of re-running it: a second run has to say whether
// anything changed, because "did my shutdown-grace edit reach the supervisor?" is the question that
// brings anyone back to this command.
func TestDaemonInstallWritesThenReportsUnchangedThenUpdated(t *testing.T) {
	root := t.TempDir()
	userHome := filepath.Join(root, "user")
	apogeeHome := filepath.Join(root, "apogee")
	restoreExecutable, restoreHome := daemonExecutable, daemonUserHome
	daemonExecutable = func() (string, error) { return installFixtureBinary, nil }
	daemonUserHome = func() (string, error) { return userHome, nil }
	t.Cleanup(func() { daemonExecutable, daemonUserHome = restoreExecutable, restoreHome })

	unit := filepath.Join(userHome, ".config", "systemd", "user", systemdUnitFileName)
	first := installRun(t, apogeeHome)

	if want := "wrote " + unit; !strings.Contains(first, want) {
		t.Fatalf("the first run does not report %q:\n%s", want, first)
	}
	if !strings.Contains(first, "systemctl --user enable --now apogee-daemon") {
		t.Errorf("the first run does not print the activation command:\n%s", first)
	}
	if !strings.Contains(first, "stop escalation: 10m30s (shutdown-grace 10m0s + 30s)") {
		t.Errorf("the first run does not report the escalation:\n%s", first)
	}
	written, err := os.ReadFile(unit)
	if err != nil {
		t.Fatalf("read the written unit: %v", err)
	}
	if !strings.Contains(string(written), "ExecStart="+installFixtureBinary+" daemon --config "+apogeeHome) {
		t.Errorf("the written unit does not pin the home passed to install:\n%s", written)
	}

	if second := installRun(t, apogeeHome); !strings.Contains(second, unit+" is already up to date") {
		t.Errorf("the second run does not report an unchanged unit:\n%s", second)
	}

	writeSchedulesGrace(t, apogeeHome, "shutdown-grace: 2m\n")

	third := installRun(t, apogeeHome)

	if !strings.Contains(third, "updated "+unit) {
		t.Errorf("the run after a grace edit does not report an update:\n%s", third)
	}
	if !strings.Contains(third, "stop escalation: 2m30s") {
		t.Errorf("the run after a grace edit does not report the new escalation:\n%s", third)
	}
	regenerated, err := os.ReadFile(unit)
	if err != nil {
		t.Fatalf("read the regenerated unit: %v", err)
	}
	if !strings.Contains(string(regenerated), "TimeoutStopSec=150") {
		t.Errorf("the regenerated unit does not carry the new escalation:\n%s", regenerated)
	}
}

// TestDaemonInstallWritesTheUnitViaTempFileAndRename pins the write's contract from the outside:
// the bytes that land are exactly the ones daemonUnitBytes produced for a golden rendering —
// UTF-16LE with a BOM on windows — and the unit's directory holds no leftover temp file afterwards,
// which is the only observable difference between the temp-file-plus-rename write and a plain one
// on the success path.
func TestDaemonInstallWritesTheUnitViaTempFileAndRename(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ goos, golden string }{
		{goos: "linux", golden: systemdUnitFileName},
		{goos: "windows", golden: taskFileName},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			t.Parallel()

			plan := installFixture(tc.goos, daemon.DefaultShutdownGrace)
			plan.UnitPath = filepath.Join(t.TempDir(), "units", tc.golden)
			content, err := renderDaemonUnit(plan)
			if err != nil {
				t.Fatalf("render the %s unit: %v", tc.goos, err)
			}
			if want := goldenUnit(t, tc.golden); content != want {
				t.Fatalf("the %s unit no longer matches testdata/%s.golden:\n%s",
					tc.goos, tc.golden, firstDifference(content, want))
			}

			existed, changed, err := writeDaemonUnit(plan, content)

			if err != nil {
				t.Fatalf("write the %s unit: %v", tc.goos, err)
			}
			if existed || !changed {
				t.Errorf("a first write reports existed=%t changed=%t, want false and true", existed, changed)
			}
			written, err := os.ReadFile(plan.UnitPath)
			if err != nil {
				t.Fatalf("read the written unit: %v", err)
			}
			if !bytes.Equal(written, daemonUnitBytes(plan, content)) {
				t.Errorf("the written bytes are not the ones daemonUnitBytes produced for the golden unit")
			}
			entries, err := os.ReadDir(filepath.Dir(plan.UnitPath))
			if err != nil {
				t.Fatalf("list the unit directory: %v", err)
			}
			for _, entry := range entries {
				if entry.Name() != tc.golden {
					t.Errorf("the unit directory holds %s next to %s — a temp file survived the write",
						entry.Name(), tc.golden)
				}
			}
		})
	}
}

// TestDaemonInstallIsRegisteredUnderDaemon guards the wiring: generation is worth nothing if the
// child command is not reachable from `apogee daemon`.
func TestDaemonInstallIsRegisteredUnderDaemon(t *testing.T) {
	t.Parallel()

	for _, child := range newDaemonCommand().Commands() {
		if child.Name() == "install" {
			return
		}
	}
	t.Errorf("`apogee daemon` has no install child")
}

// installRun runs the command against apogeeHome with --config passed, and returns everything it
// printed.
func installRun(t *testing.T, apogeeHome string) string {
	t.Helper()

	var out bytes.Buffer
	if err := runDaemonInstall("linux", apogeeHome, true, &out); err != nil {
		t.Fatalf("apogee daemon install: %v", err)
	}
	return out.String()
}

// writeSchedulesGrace puts a schedules file carrying one shutdown-grace into an apogee home.
func writeSchedulesGrace(t *testing.T, apogeeHome, content string) {
	t.Helper()

	dir := filepath.Join(apogeeHome, daemonDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create the daemon directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, schedulesFileName), []byte(content), 0o600); err != nil {
		t.Fatalf("write the schedules file: %v", err)
	}
}

// decodeUTF16LE turns UTF-16LE bytes back into a string, so the encoding test can assert a round
// trip rather than a byte pattern.
func decodeUTF16LE(t *testing.T, encoded []byte) string {
	t.Helper()

	if len(encoded)%2 != 0 {
		t.Fatalf("UTF-16LE bytes have an odd length: %d", len(encoded))
	}
	units := make([]uint16, 0, len(encoded)/2)
	for i := 0; i < len(encoded); i += 2 {
		units = append(units, uint16(encoded[i])|uint16(encoded[i+1])<<8)
	}
	return string(utf16.Decode(units))
}

// firstDifference reports where two renderings part company, because a whole-file dump of two
// sixty-line units says nothing about which line moved.
func firstDifference(got, want string) string {
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
		g, w := lineAt(gotLines, i), lineAt(wantLines, i)
		if g != w {
			return fmt.Sprintf("line %d:\n  got  %s\n  want %s", i+1, g, w)
		}
	}
	return "the two renderings are identical"
}

// lineAt is the line at index i, or a marker when the rendering ended before it.
func lineAt(lines []string, i int) string {
	if i >= len(lines) {
		return "(end of file)"
	}
	return lines[i]
}
