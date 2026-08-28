package main

// The documentation-drift half of checklist T-23: the manual's own claims about the environment,
// the hidden trace flags and the `url-safety:` prose, asserted against the binary rather than
// re-read by a human. A doc claim nothing checks is a doc claim that goes stale on the commit
// after the one that wrote it.
//
// The reading tests here are cheap greps over files the repo layout fixes in place, so a missing
// file is a failure rather than a reason to skip. The two driven tests are e2e runs and follow the
// kit's rules (docs/design/test-drivers.md): a temp home, a temp workspace, bounded waits, no
// t.Parallel — they swap process-wide state.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tui"
	"github.com/airiclenz/apogee/internal/tuitest"
)

const (
	manualConfigPath   = "../../docs/manual/configuration.md"
	configTemplatePath = "../../internal/config/defaults/config.yaml"
	envOverridesHeader = "## Environment overrides"
)

// apogeeEnvName matches an APOGEE_* variable wherever it is written — in Go source, in prose, in
// back-ticks. The trailing class requires at least one character, so the `APOGEE_*` wildcard the
// help text and the manual both use is not mistaken for a variable of its own.
var apogeeEnvName = regexp.MustCompile(`APOGEE_[A-Z0-9_]+`)

// envConstDecl matches the declarations in internal/config that ARE the reads: every variable
// apogee honours reaches the resolver through one of these constants.
var envConstDecl = regexp.MustCompile(`Env[A-Za-z]*\s*=\s*"(APOGEE_[A-Z0-9_]+)"`)

// envNamesNotRead are the APOGEE_* spellings that appear in cmd/apogee's source without being
// variables apogee reads, so the manual is right not to list them. Each one needs a reason.
var envNamesNotRead = map[string]string{
	// An EXAMPLE of a name the USER picks for `api-key-env:`, printed by the key-migration
	// guidance. apogee reads whatever that key names; this spelling has no meaning of its own.
	"APOGEE_WORK_KEY": "an example `api-key-env:` value in the key-migration guidance, not a variable apogee reads",
}

// numberWords is how the manual spells the count of variables it documents. A ninth variable must
// move the word as well as the list, or the section's first sentence starts lying.
var numberWords = map[int]string{
	6: "Six", 7: "Seven", 8: "Eight", 9: "Nine", 10: "Ten", 11: "Eleven", 12: "Twelve",
}

// TestManualListsEveryEnvironmentOverride is the drift gate on the manual's "Environment
// overrides" section, in both directions: every APOGEE_* variable the binary actually reads is
// named there, and every APOGEE_* variable named there is one the binary actually reads. It is the
// twin of internal/tools' TestManualListsEveryKnownToolName, for the other hand-maintained list.
//
// The read set comes from internal/config's own `Env… = "APOGEE_…"` declarations — those constants
// ARE the reads — widened by anything cmd/apogee's source spells out, so a variable read straight
// from the environment in the command layer cannot slip past the constants. Whatever that union
// turns up must be documented or explicitly exempted (envNamesNotRead).
func TestManualListsEveryEnvironmentOverride(t *testing.T) {
	t.Parallel()

	read := environmentVariablesRead(t)
	section := environmentOverridesSection(t)

	for _, name := range read {
		if !strings.Contains(section, name) {
			t.Errorf("%s's %q section does not name %s, which apogee reads",
				manualConfigPath, envOverridesHeader, name)
		}
	}

	documented := map[string]bool{}
	for _, name := range apogeeEnvName.FindAllString(section, -1) {
		documented[name] = true
	}
	inRead := map[string]bool{}
	for _, name := range read {
		inRead[name] = true
	}
	for name := range documented {
		if !inRead[name] {
			t.Errorf("%s's %q section documents %s, which apogee does not read",
				manualConfigPath, envOverridesHeader, name)
		}
	}

	// The section opens by counting itself. A variable added to the list and not to the sentence
	// leaves the reader with a number that is one short of the truth.
	if word, ok := numberWords[len(read)]; ok {
		if !strings.Contains(section, word+" `APOGEE_*` variables are read") {
			t.Errorf("%s reads %d variables but the section does not say %q; it says: %s",
				manualConfigPath, len(read), word+" `APOGEE_*` variables are read",
				firstSentence(section))
		}
	}
}

// environmentVariablesRead is the union described above, sorted, with the exemptions removed.
func environmentVariablesRead(t *testing.T) []string {
	t.Helper()

	found := map[string]bool{}

	const configSource = "../../internal/config/config.go"
	source, err := os.ReadFile(configSource)
	if err != nil {
		t.Fatalf("read %s: %v", configSource, err)
	}
	for _, m := range envConstDecl.FindAllStringSubmatch(string(source), -1) {
		found[m[1]] = true
	}
	if len(found) == 0 {
		t.Fatalf("%s declares no Env… = \"APOGEE_…\" constants; the scan has stopped working", configSource)
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, spelled := range apogeeEnvName.FindAllString(string(body), -1) {
			if _, exempt := envNamesNotRead[spelled]; exempt {
				continue
			}
			found[spelled] = true
		}
	}

	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	// Sorted so a failure message reads the same way twice.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

// environmentOverridesSection returns the manual's "Environment overrides" section — everything
// from its heading to the next one.
func environmentOverridesSection(t *testing.T) string {
	t.Helper()

	manual, err := os.ReadFile(manualConfigPath)
	if err != nil {
		t.Fatalf("read %s: %v", manualConfigPath, err)
	}
	text := string(manual)
	start := strings.Index(text, envOverridesHeader)
	if start < 0 {
		t.Fatalf("%s has no %q heading", manualConfigPath, envOverridesHeader)
	}
	rest := text[start+len(envOverridesHeader):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// firstSentence is enough of a section to quote in a failure message.
func firstSentence(section string) string {
	trimmed := strings.TrimSpace(section)
	if cut := strings.Index(trimmed, ". "); cut >= 0 {
		return trimmed[:cut+1]
	}
	if len(trimmed) > 200 {
		return trimmed[:200] + "…"
	}
	return trimmed
}

// TestDocsEnvBadValuesNameTheVariableAndTheValue is the manual's own promise about a value it
// cannot parse (checklist T-23 step 7): `APOGEE_MODE=fast` and `APOGEE_BYPASS=maybe` are startup
// ERRORS naming the setting and the value, never a silent fall back to the default and never a run
// that starts anyway. Headless is the shape that proves it without a terminal, and it is the shape
// where a silent default would be least visible.
//
// It runs the headless command itself rather than through headlessRun: that helper deliberately
// CLEARS APOGEE_MODE so its own mode assertions measure flags, which is the opposite of the claim
// here.
//
// The two refusals do not word themselves alike. APOGEE_BYPASS names the variable; APOGEE_MODE's
// comes from domain.ParseMode, which is shared by the flag, the variable and the config file's
// `mode:` key and says `--mode` for all three. Both name the setting and the offending value, which
// is what is asserted; naming the SOURCE in the mode refusal too is recorded as a follow-up rather
// than fixed here, since the message belongs to internal/domain.
func TestDocsEnvBadValuesNameTheVariableAndTheValue(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    string
		wantSaid []string
	}{
		{config.EnvMode, "fast", []string{"mode", "fast"}},
		{config.EnvBypass, "maybe", []string{config.EnvBypass, "maybe"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubRunner{}
			said, err := headlessRunUnderEnv(t, stub, tc.name, tc.value)
			if err == nil {
				t.Fatalf("%s=%s started without an error; the manual promises a startup error:\n%s",
					tc.name, tc.value, said)
			}
			for _, want := range tc.wantSaid {
				if !strings.Contains(said, want) {
					t.Errorf("the refusal of %s=%s does not name %q:\n%s", tc.name, tc.value, want, said)
				}
			}
			if stub.called {
				t.Errorf("%s=%s reached the runner; an unparseable value must stop before any work", tc.name, tc.value)
			}
		})
	}
}

// headlessRunUnderEnv runs one headless invocation with exactly one APOGEE_* variable set — every
// other one is cleared, so the developer's own shell cannot decide the outcome — and returns
// everything the command said on either stream beside the error it returned.
func headlessRunUnderEnv(t *testing.T, stub *stubRunner, name, value string) (string, error) {
	t.Helper()

	prevRunner, prevConfiner := runOnce, newConfiner
	runOnce = stub.once
	newConfiner = func() apogee.Confiner { return fenceableHost }
	t.Cleanup(func() { runOnce, newConfiner = prevRunner, prevConfiner })

	for _, other := range []string{
		config.EnvServer, config.EnvEndpoint, config.EnvModel, config.EnvMode, config.EnvBypass,
		config.EnvAPIKey, config.EnvConfig, config.EnvWorkspace,
	} {
		t.Setenv(other, "")
	}
	t.Setenv(name, value)

	cmd := newHeadlessCommand()
	var out, errOut strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--config", testConfigHome(t, ""), "--workspace", t.TempDir(), "hello"})

	err := cmd.ExecuteContext(context.Background())
	said := out.String() + errOut.String()
	if err != nil {
		said = err.Error() + "\n" + said
	}
	return said, err
}

// TestDocsEnvTraceFlagsWriteFilesAndStayOutOfHelp is checklist T-23 step 10: `--tui-trace` and
// `--tui-diag` both write the file they are given, and neither appears in `apogee --help`. They are
// instruments for whoever is debugging the renderer (docs/manual/probe.md documents them there),
// and a hidden flag that stopped working is a flag nobody notices is broken.
//
// The PTY driver is the one that can make this claim: `--tui-trace` wraps the program's real
// stdout, and an in-process launch supplies its own output writer, which tui.Build refuses a trace
// beside. launchPTY already passes --tui-trace; --tui-diag is this test's own argument.
func TestDocsEnvTraceFlagsWriteFilesAndStayOutOfHelp(t *testing.T) {
	help := rootHelpText(t)
	for _, flag := range []string{"--tui-trace", "--tui-diag"} {
		if strings.Contains(help, flag) {
			t.Errorf("apogee --help lists %s; the manual says both trace flags are hidden", flag)
		}
	}

	diag := filepath.Join(t.TempDir(), "tui-diag.txt")
	stub := stubllm.New(t, loadScript(t, "docs-env"))
	sess := launchPTY(t, stub, "--tui-diag", diag)
	drv := sess.drv

	submit(drv, "Say hello.")
	drv.WaitText("Ready.")
	drv.WaitQuiet(settled)

	if code := sess.drv.Quit(); code != 0 {
		t.Fatalf("the run exited %d; want a clean quit", code)
	}

	for _, f := range []struct{ what, path string }{
		{"--tui-trace", sess.trace},
		{"--tui-diag", diag},
	} {
		info, err := os.Stat(f.path)
		if err != nil {
			t.Errorf("%s wrote no file at %s: %v", f.what, f.path, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s wrote an EMPTY file at %s", f.what, f.path)
		}
	}
}

// rootHelpText is `apogee --help` as a user sees it, off the real root command.
func rootHelpText(t *testing.T) string {
	t.Helper()

	cmd := newRootCommand(func(_ context.Context, _ tui.Engine, _ *tui.Bridge, _ tui.Options) error {
		t.Error("--help must not launch a session")
		return nil
	})
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--help returned an error: %v", err)
	}
	return out.String()
}

// TestDocsEnvRootsMoveTheHomeAndTheFence is checklist T-23 step 9, and it is the reason the two
// root variables are documented apart from the other six: they say WHERE resolution runs. With
// APOGEE_CONFIG and APOGEE_WORKSPACE set and no flag in sight, the session record lands under the
// named home, and a read of a path outside the named workspace is refused — the manual's claim is
// that the workspace decides what the model may read at all, not merely which directory a session
// opens in.
//
// It launches through the composition seam by hand rather than through launchTUI: every helper in
// e2e_support_test.go passes --config and --workspace as FLAGS and refuses to run with
// APOGEE_CONFIG set, which is exactly the resolution path this test is not about.
func TestDocsEnvRootsMoveTheHomeAndTheFence(t *testing.T) {
	tuitest.CheckLeaks(t)
	driveConfigWatch(t)

	stub := stubllm.New(t, loadScript(t, "docs-env"))
	home := upstreamHome(t, stub.URL, stub.Model)

	// The workspace sits INSIDE a temp root that also holds the file the fence has to refuse, so
	// the fixture's `../outside.txt` resolves to a real file the tool could otherwise have read.
	outer := t.TempDir()
	ws := filepath.Join(outer, "ws")
	if err := os.Mkdir(ws, 0o700); err != nil {
		t.Fatalf("create the workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outer, "outside.txt"), []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("seed the file outside the workspace: %v", err)
	}

	t.Setenv(config.EnvConfig, home)
	t.Setenv(config.EnvWorkspace, ws)
	for _, name := range []string{
		config.EnvServer, config.EnvEndpoint, config.EnvModel, config.EnvMode, config.EnvBypass,
	} {
		t.Setenv(name, "")
	}

	drv := tuitest.NewDriver(t, e2eSize)
	sess := &e2eSession{t: t, home: home, ws: ws, stub: stub}
	sess.startRooted(drv)

	submit(drv, "Read the file just outside the workspace.")
	drv.WaitText("outside the workspace, so the read was refused")
	drv.WaitQuiet(settled)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}

	// The home the VARIABLE named is where the session was persisted — no flag said so.
	if records := sess.sessionRecords(); len(records) == 0 {
		t.Errorf("no session record under %s/sessions; %s did not move the home",
			home, config.EnvWorkspace)
	}

	// And the refusal the model was handed is the workspace fence's own, not a missing file.
	var sawRefusal bool
	for _, req := range stub.Requests() {
		for _, msg := range req.Messages {
			if msg.ToolCallID != "" && strings.Contains(msg.Content, "outside the workspace root") {
				sawRefusal = true
			}
		}
	}
	if !sawRefusal {
		t.Errorf("no tool result carried the workspace-fence refusal; %s did not fence the read.\n%s",
			config.EnvWorkspace, stub.Requests())
	}
}

// startRooted starts the session with NO --config and NO --workspace: both roots come from the
// environment, which is the resolution path this file's root test is about. Everything else is
// e2eSession.start.
func (s *e2eSession) startRooted(drv *tuitest.Driver) {
	s.t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	drv.Attach(nil, cancel)

	launch := func(ctx context.Context, eng tui.Engine, br *tui.Bridge, opts tui.Options) error {
		program, cleanup, err := tui.Build(ctx, eng, br, opts, drv.Output(), drv.ProgramOptions()...)
		if err != nil {
			return err
		}
		defer cleanup()
		drv.Attach(program, cancel)
		_, err = program.Run()
		return err
	}

	cmd := newRootCommand(launch)
	cmd.SetArgs(nil)
	cmd.SetOut(&s.out)
	cmd.SetErr(&s.out)
	go func() { drv.Finished(cmd.ExecuteContext(ctx)) }()

	s.drv = drv
	s.done = false
	s.t.Cleanup(func() {
		s.stop()
		drv.Close()
	})
	drv.WaitFor(func() bool { return drv.Screen().BytesWritten() > 0 },
		tuitest.Awaiting("apogee's first frame"))
}

// TestDocsEnvURLSafetyProseIsLiveAndCoversMCP pins the two corrections checklist T-23 step 11 asks
// a reader to verify, in BOTH places a reader meets them: the manual and the config template a
// first run seeds. The old prose said the `url-safety:` lists were startup-only and exempted
// `mcp-servers:` endpoints; both claims were wrong, and a template comment that drifts back to
// them is a claim shipped to every new install.
func TestDocsEnvURLSafetyProseIsLiveAndCoversMCP(t *testing.T) {
	t.Parallel()

	for _, doc := range []struct {
		path  string
		wants []string
	}{
		{manualConfigPath, []string{
			"Both lists are live.",
			"rebuilds the session's",
			"a denied endpoint is refused at startup with the same url-safety",
		}},
		{configTemplatePath, []string{
			"Both lists are LIVE",
			"rebuilds the session's tool set around the new guard",
			"An mcp-servers: endpoint is checked against these two lists as well",
		}},
	} {
		body, err := os.ReadFile(doc.path)
		if err != nil {
			t.Fatalf("read %s: %v", doc.path, err)
		}
		text := string(body)
		for _, want := range doc.wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s no longer says %q — the url-safety prose has drifted back", doc.path, want)
			}
		}
	}
}
