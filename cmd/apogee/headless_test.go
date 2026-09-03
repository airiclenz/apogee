package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/sanitize"
)

// stubRunner stands in for internal/run.Once: it records the Spec the command composed and
// returns a canned outcome, so every assertion below is about the CLI — parsing, composition,
// output routing, exit codes — and none of them needs a model, a server, or a real run.
type stubRunner struct {
	called bool
	spec   run.Spec
	res    run.Result
	err    error
	// emit, when set, is handed the Config's Event sink before the stub returns — the way a test
	// drives an Event the COMMAND's own sink is supposed to render (the prune notice) without a
	// model or a real loop. nil emits nothing, which is what every other test here wants.
	emit func(domain.EventSink)
}

func (s *stubRunner) once(_ context.Context, spec run.Spec) (run.Result, error) {
	s.called = true
	s.spec = spec
	if s.emit != nil && spec.Config.Events != nil {
		s.emit(spec.Config.Events)
	}
	return s.res, s.err
}

// fakeConfiner is a Confiner whose capability matrix the test dictates — the seam that makes the
// Auto gate assertable off the machine the suite happens to run on, where the real backend may or
// may not be able to fence. Its type name is also what the backend label derives from
// ("fakeConfiner" → "fake"), which is what the refusal is asserted to name.
type fakeConfiner struct{ caps apogee.ConfinementCaps }

func (f fakeConfiner) Capabilities() apogee.ConfinementCaps { return f.caps }

func (fakeConfiner) Confine(context.Context, apogee.ConfinementBox, *exec.Cmd) error { return nil }

// fenceableHost is the backend most of this file assumes: one that can enforce a filesystem box,
// so the Auto gate has nothing to say and every other assertion is about what it was written for.
// It is a POINTER because apogee.ConfinementCaps carries a Residuals slice and is therefore not
// comparable: a test that asserts "the Config got THIS backend" compares the interface value, and
// pointer identity is both comparable and the stronger claim.
var fenceableHost = &fakeConfiner{caps: apogee.ConfinementCaps{FSWrite: true}}

// headlessRun executes one `apogee headless` invocation against the stub and hermetic roots, and
// returns what landed on each stream plus the error the command returned. The apogee home and the
// workspace are temporary so no real ~/.apogee config can reach the resolution, and stdin is
// empty unless a test replaces it.
func headlessRun(t *testing.T, stub *stubRunner, args ...string) (out, errOut string, err error) {
	t.Helper()
	return headlessRunOn(t, stub, fenceableHost, testConfigHome(t, ""), args...)
}

// headlessRunOn is headlessRun with the two facts the Auto gate reads under the caller's control:
// the host's confinement backend, and the apogee home whose config.yaml carries the confinement
// posture (`confine-to-workspace:` is file-only by design — ADR 0012 — so an unconfined run is
// expressed by writing that file, never by a flag).
//
// It swaps the package-level runner and Confiner seams for the duration of the test. That is
// shared mutable state, so nothing here runs in parallel.
func headlessRunOn(t *testing.T, stub *stubRunner, confiner apogee.Confiner, configDir string, args ...string) (out, errOut string, err error) {
	t.Helper()
	prevRunner, prevConfiner := runOnce, newConfiner
	runOnce = stub.once
	newConfiner = func() apogee.Confiner { return confiner }
	t.Cleanup(func() { runOnce, newConfiner = prevRunner, prevConfiner })
	// The environment must not decide what the mode assertions measure.
	t.Setenv(config.EnvMode, "")

	cmd := newHeadlessCommand()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(append([]string{"--config", configDir, "--workspace", t.TempDir()}, args...))

	err = cmd.ExecuteContext(context.Background())
	return outBuf.String(), errBuf.String(), err
}

// unconfinedHome writes an apogee home whose config switches confinement off — the user's own
// explicit "I am the sandbox", which is the only way that posture is ever reached.
func unconfinedHome(t *testing.T) string {
	t.Helper()
	return testConfigHome(t, "confine-to-workspace: false\n")
}

// subAgentStderrLines picks the per-delegation lines out of a headless run's stderr, in the order
// they were printed, so an assertion can be about the WHOLE line rather than a substring of the
// stream — which is what "byte-identical to what this line has always printed" needs.
func subAgentStderrLines(errOut string) []string {
	var lines []string
	for _, l := range strings.Split(errOut, "\n") {
		if strings.HasPrefix(l, "sub-agent:") {
			lines = append(lines, l)
		}
	}
	return lines
}

// The prompt is the argument, or stdin, or a usage error — never an empty request to the model.
func TestHeadlessPromptResolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		stdin   string
		want    string
		wantErr bool
	}{
		{name: "the positional argument is the prompt", args: []string{"list the files"}, want: "list the files"},
		{name: "no argument reads all of stdin", stdin: "summarise the repo\nin one line\n", want: "summarise the repo\nin one line"},
		{name: "the argument wins over stdin", args: []string{"from the argument"}, stdin: "from stdin", want: "from the argument"},
		{name: "the prompt is trimmed", args: []string{"  spaced out \n"}, want: "spaced out"},
		{name: "nothing in either is a usage error", stdin: "", wantErr: true},
		{name: "whitespace in either is a usage error", args: []string{" \n\t "}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveHeadlessPrompt(tc.args, strings.NewReader(tc.stdin))
			if tc.wantErr {
				if !errors.Is(err, errHeadlessNoPrompt) {
					t.Fatalf("err = %v; want errHeadlessNoPrompt", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveHeadlessPrompt: %v", err)
			}
			if got != tc.want {
				t.Errorf("prompt = %q; want %q", got, tc.want)
			}
		})
	}
}

// An empty prompt stops the command before anything is composed, and says so with exit 2: the run
// never started, which is a different thing for a script to read than a run that failed.
func TestHeadlessEmptyPromptNeverStartsARun(t *testing.T) {
	stub := &stubRunner{}
	_, _, err := headlessRun(t, stub)

	if err == nil {
		t.Fatal("an empty prompt was accepted")
	}
	if code := exitCodeFor(err); code != exitNotStarted {
		t.Errorf("exit code = %d; want %d", code, exitNotStarted)
	}
	if stub.called {
		t.Error("the runner ran for an empty prompt")
	}
}

// Only plan and auto reach the runner. The other two rungs of the ladder exist to consult a
// human, and the refusal happens at the flag — before a Confiner is built or a model is bound.
func TestHeadlessRefusesModesWithNobodyToAsk(t *testing.T) {
	for _, mode := range []string{"ask-before", "allow-edits"} {
		t.Run(mode, func(t *testing.T) {
			stub := &stubRunner{}
			_, errOut, err := headlessRun(t, stub, "--mode", mode, "do a thing")

			if err == nil {
				t.Fatalf("--mode %s was accepted", mode)
			}
			if code := exitCodeFor(err); code != exitNotStarted {
				t.Errorf("exit code = %d; want %d", code, exitNotStarted)
			}
			if stub.called {
				t.Error("the runner ran in a mode a headless run may not use")
			}
			for _, want := range []string{"plan", "auto"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not name %q: %q", want, err.Error())
				}
			}
			if errOut != "" && strings.Contains(errOut, "turns:") {
				t.Errorf("a refused run printed a summary line: %q", errOut)
			}
		})
	}

	t.Run("a mode that is no mode at all", func(t *testing.T) {
		stub := &stubRunner{}
		_, _, err := headlessRun(t, stub, "--mode", "yolo", "do a thing")
		if err == nil {
			t.Fatal("--mode yolo was accepted")
		}
		if code := exitCodeFor(err); code != exitNotStarted {
			t.Errorf("exit code = %d; want %d", code, exitNotStarted)
		}
	})
}

// The bottom layer is this command's own: an unset --mode is plan, not the interactive ladder's
// ask-before, so a bare invocation on a host that has never spelled a mode out runs read-only
// rather than refusing.
func TestHeadlessModeResolution(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want apogee.Mode
	}{
		{name: "unset is plan", args: []string{"a prompt"}, want: apogee.ModePlan},
		{name: "explicit plan", args: []string{"--mode", "plan", "a prompt"}, want: apogee.ModePlan},
		{name: "explicit auto", args: []string{"--mode", "auto", "a prompt"}, want: apogee.ModeAuto},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubRunner{}
			_, _, err := headlessRun(t, stub, tc.args...)
			if err != nil {
				t.Fatalf("headless: %v", err)
			}
			if !stub.called {
				t.Fatal("the runner did not run")
			}
			if stub.spec.Config.Mode != tc.want {
				t.Errorf("Config.Mode = %q; want %q", stub.spec.Config.Mode, tc.want)
			}
		})
	}
}

// The eligibility ladder for --mode auto, ruled on by the surface that offers the mode (ADR 0033,
// decision 3). Three cells, and each one is a different answer: a host that can fence runs auto and
// says nothing, a host that cannot refuses it outright rather than running a plan run under auto's
// name, and a host the user has declared disposable runs it and warns.
func TestHeadlessAutoEligibilityGate(t *testing.T) {
	const warning = "running UNCONFINED"

	tests := []struct {
		name        string
		caps        apogee.ConfinementCaps
		unconfined  bool
		wantRun     bool
		wantWarning bool
	}{
		{
			name:    "a backend that can fence runs auto, silently",
			caps:    apogee.ConfinementCaps{FSWrite: true},
			wantRun: true,
		},
		{
			name: "a backend that cannot fence refuses auto",
			caps: apogee.ConfinementCaps{},
		},
		{
			name:        "an acknowledged disposable host runs auto unfenced, and says so",
			caps:        apogee.ConfinementCaps{},
			unconfined:  true,
			wantRun:     true,
			wantWarning: true,
		},
		{
			name:        "the warning is about the posture, not the backend",
			caps:        apogee.ConfinementCaps{FSWrite: true},
			unconfined:  true,
			wantRun:     true,
			wantWarning: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := testConfigHome(t, "")
			if tc.unconfined {
				home = unconfinedHome(t)
			}
			stub := &stubRunner{res: run.Result{FinalText: "the answer", Turns: 1}}
			out, errOut, err := headlessRunOn(t, stub, fakeConfiner{caps: tc.caps}, home, "--mode", "auto", "a prompt")

			if tc.wantRun {
				if err != nil {
					t.Fatalf("headless: %v (stderr: %q)", err, errOut)
				}
				if !stub.called {
					t.Fatal("the runner did not run on a host that may run auto")
				}
				if stub.spec.Config.Mode != apogee.ModeAuto {
					t.Errorf("Config.Mode = %q; want auto", stub.spec.Config.Mode)
				}
			} else {
				if err == nil {
					t.Fatal("auto was accepted on a host that cannot fence")
				}
				if code := exitCodeFor(err); code != exitNotStarted {
					t.Errorf("exit code = %d; want %d (err: %v)", code, exitNotStarted, err)
				}
				if stub.called {
					t.Error("the runner ran in a mode this host may not run unattended")
				}
				if !strings.Contains(err.Error(), "nobody to ask") || !strings.Contains(err.Error(), "fake") {
					t.Errorf("the refusal does not state the reason or name the backend: %q", err.Error())
				}
				if !strings.Contains(err.Error(), "--mode plan") {
					t.Errorf("the refusal names no way forward: %q", err.Error())
				}
			}

			if got := strings.Contains(errOut, warning); got != tc.wantWarning {
				t.Errorf("unconfined-auto warning present = %v; want %v (stderr = %q)", got, tc.wantWarning, errOut)
			}
			if strings.Contains(out, warning) {
				t.Errorf("the warning reached stdout, where a pipeline would read it as the answer: %q", out)
			}
		})
	}
}

// The refusal is the schedule surface's sentence with its noun adapted, from one source: the two
// unattended surfaces must never tell a user two different stories about the same host.
func TestHeadlessAutoRefusalSharesTheScheduleSentence(t *testing.T) {
	t.Parallel()
	unfenceable := apogee.ConfinementCaps{}

	const want = "the landlock backend on this host reports no filesystem confinement, " +
		"so auto falls back to approval — and a headless run has nobody to ask"
	got := probe.AutoUnattendedBlocked("a headless run", "landlock", unfenceable, true)
	if got != want {
		t.Errorf("the refusal reads\n  %q\nwant\n  %q", got, want)
	}
	if firing := scheduleAutoBlocked("landlock", unfenceable, true); strings.Replace(firing, "a firing", "a headless run", 1) != got {
		t.Errorf("the two unattended surfaces word the same verdict differently:\n  %q\n  %q", firing, got)
	}

	// The cells that are not a refusal: a backend that can fence, and a posture that asked for no
	// confinement at all (the user's own explicit loosen, never blocked).
	if blocked := probe.AutoUnattendedBlocked("a headless run", "landlock", apogee.ConfinementCaps{FSWrite: true}, true); blocked != "" {
		t.Errorf("a fenceable host was refused auto: %q", blocked)
	}
	if blocked := probe.AutoUnattendedBlocked("a headless run", "deny", unfenceable, false); blocked != "" {
		t.Errorf("an unconfined run was refused auto: %q", blocked)
	}
}

// The degraded cell — auto, confinement asked for, a backend that cannot fence — is where the TUI
// prints probe.DegradedNotice and the session runs on, because every gated command falls back to
// approval. A headless run has no approval to fall back to, so the SAME cell is a refusal here.
//
// This is the routing claim, and the reason no degradation notice is printed by this command:
// wherever DegradedNotice would speak, the run never starts, and the notice's remedies (`/confine
// off`) are slash commands nobody is present to type. If the two predicates ever drift apart, this
// test fails rather than leaving a headless run silently gating every write.
func TestHeadlessAutoDegradedCellIsARefusalNotANotice(t *testing.T) {
	for _, caps := range []apogee.ConfinementCaps{
		{},
		{FSWrite: true},
		{NetworkEgress: true},
		{FSWrite: true, NetworkEgress: true},
	} {
		confiner := fakeConfiner{caps: caps}
		degraded := probe.DegradedNotice(probe.BackendName(confiner), caps, apogee.ModeAuto, true)

		t.Run(probe.CapabilityLine(probe.BackendName(confiner), caps), func(t *testing.T) {
			stub := &stubRunner{res: run.Result{FinalText: "the answer", Turns: 1}}
			_, errOut, err := headlessRunOn(t, stub, confiner, testConfigHome(t, ""), "--mode", "auto", "a prompt")

			if degraded == "" {
				if err != nil {
					t.Fatalf("a host with nothing to degrade about refused the run: %v", err)
				}
				return
			}
			if err == nil || exitCodeFor(err) != exitNotStarted {
				t.Fatalf("the degraded cell did not refuse the run: err = %v (exit %d)", err, exitCodeFor(err))
			}
			if stub.called {
				t.Error("the runner ran in the cell the TUI only survives by asking a human")
			}
			if strings.Contains(errOut, "auto mode is gating terminal commands") || strings.Contains(errOut, "/confine off") {
				t.Errorf("the TUI's degradation notice was printed to a run that cannot act on it: %q", errOut)
			}
		})
	}
}

// The residual cell is the degraded cell's mirror in FSWrite, and the opposite decision: a backend
// that DOES fence — so the run is never refused — on a kernel where it knowingly leaves one
// write-class access open (landlock ABI 1–2 cannot fence truncate(2)). Nothing is gated and nothing
// falls back to approval, so there is nothing to refuse; the one thing the fence does not stop is
// disclosed instead, on stderr, where it cannot contaminate the answer a pipeline reads off stdout.
//
// Both directions are driven through the confiner seam rather than read off this machine's kernel,
// because the silent half is the half a real host almost always lands in: a suite that only ever
// saw a residual-free backend would pass with the print deleted.
func TestHeadlessAutoDisclosesTheFenceResidual(t *testing.T) {
	const disclosure = "cannot fence"

	tests := []struct {
		name string
		caps apogee.ConfinementCaps
		want bool
	}{
		{
			name: "a fence with a known hole in it discloses the hole",
			caps: apogee.ConfinementCaps{FSWrite: true, Residuals: []string{"truncate(2)"}},
			want: true,
		},
		{
			name: "a fence with no known hole says nothing",
			caps: apogee.ConfinementCaps{FSWrite: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubRunner{res: run.Result{FinalText: "the answer", Turns: 1}}
			out, errOut, err := headlessRunOn(
				t, stub, fakeConfiner{caps: tc.caps}, testConfigHome(t, ""), "--mode", "auto", "a prompt")
			if err != nil {
				t.Fatalf("headless: %v (stderr: %q)", err, errOut)
			}
			if !stub.called {
				t.Fatal("the runner did not run on a backend that can fence")
			}
			if got := strings.Contains(errOut, disclosure); got != tc.want {
				t.Errorf("residual disclosure on stderr = %v; want %v (stderr = %q)", got, tc.want, errOut)
			}
			if tc.want && !strings.Contains(errOut, "truncate(2)") {
				t.Errorf("the disclosure does not name the access it is about: %q", errOut)
			}
			if strings.Contains(out, disclosure) {
				t.Errorf("the disclosure reached stdout, where a pipeline would read it as the answer: %q", out)
			}
			if !strings.Contains(out, "the answer") {
				t.Errorf("stdout = %q; want the run's answer, untouched", out)
			}
		})
	}
}

// A `mechanisms:` key naming a Mechanism this release retired is tolerated, not refused — and the
// run says so rather than arming nothing in silence. The line lands on stderr, where the other
// resolution notices go, so a pipeline still reads only the answer.
func TestHeadlessReportsARetiredMechanism(t *testing.T) {
	stub := &stubRunner{res: run.Result{FinalText: "the answer", Turns: 1}}
	home := testConfigHome(t, "mechanisms:\n  grammar: true\n")

	out, errOut, err := headlessRunOn(t, stub, fenceableHost, home, "a prompt")

	if err != nil {
		t.Fatalf("headless: %v (stderr: %q)", err, errOut)
	}
	if !stub.called {
		t.Fatal("the run never started; a retired mechanism id is tolerated, never a refusal")
	}
	want := retiredMechanismNotice("grammar")
	if got := strings.Count(errOut, want); got != 1 {
		t.Errorf("the retired-mechanism notice appeared %d times on stderr; want exactly 1 line\n"+
			"want: %q\nstderr: %q", got, want, errOut)
	}
	if strings.Contains(out, "mechanism") {
		t.Errorf("the notice reached stdout, where a pipeline reads the answer: %q", out)
	}
	// The retired id is dropped and nothing stands in its place: no catalogued row is on by default,
	// a headless run getting its recovery guarantees from the Floor guards instead (ADR 0071).
	if got := stub.spec.Config.EnableMechanisms; len(got) != 0 {
		t.Errorf("EnableMechanisms = %v, want nothing armed", got)
	}
}

// What the command hands the runner: the resolved prompt, the roots this invocation was pointed
// at, and none of the delegates that assume a human — run.Once pins its own, and a Firing reaches
// no MCP server (ADR 0033/0034).
func TestHeadlessComposesTheRunnerSpec(t *testing.T) {
	stub := &stubRunner{}
	_, _, err := headlessRun(t, stub, "explain this repo")
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	if !stub.called {
		t.Fatal("the runner did not run")
	}
	if stub.spec.Prompt != "explain this repo" {
		t.Errorf("Spec.Prompt = %q; want %q", stub.spec.Prompt, "explain this repo")
	}
	if stub.spec.ScheduleID != "" || stub.spec.ScheduleName != "" || stub.spec.Title != "" {
		t.Errorf("a bare headless run carries a Schedule identity or a title: %+v", stub.spec)
	}
	cfg := stub.spec.Config
	if cfg.Approver != nil || cfg.Asker != nil || cfg.Presenter != nil {
		t.Error("the command wired a delegate run.Once pins for itself")
	}
	// The Events sink is the ONE delegate the command does wire, and wiring it is not the same as
	// claiming run.Once's: Spec says the Config's sink is WRAPPED by the runner's tap, not replaced
	// (internal/run/run.go), so the command's own prune-notice sink sits inside the tap rather than
	// displacing it. What must hold is that it is the headless one and nothing else.
	if _, ok := cfg.Events.(pruneNoticeSink); !ok {
		t.Errorf("Config.Events = %T; want the headless prune-notice sink", cfg.Events)
	}
	if cfg.Tools != nil {
		t.Error("the command wired a tool registry; with `sub-agents-choice:` unset a headless run " +
			"takes the engine's own")
	}
	if cfg.Confiner == nil {
		t.Error("no Confiner was wired; the run would not be fenced")
	}
	if cfg.WorkspaceDir == "" || cfg.ConfigDir == "" {
		t.Errorf("the state roots did not reach the Config: %+v", cfg)
	}
}

// stubSlots stands in for the one-shot discovery probe: it reports the slot count the test dictates
// and records whether it was consulted at all — the second half of the pin assertion, since a pinned
// entry has to be answered without spending a round trip on a question already settled.
type stubSlots struct {
	called bool
	slots  int
}

func (s *stubSlots) discover(context.Context, string, string, string) int {
	s.called = true
	return s.slots
}

// stubDialect is the effort half of stubSlots: the discovery seam a test dictates the server's
// answer through (ADR 0060), plus the record of whether it was consulted at all — a bound entry
// that FORCES an `effort-dialect:` must skip the round trip, exactly as a `parallel-agents:` pin
// skips the width probe.
type stubDialect struct {
	called  bool
	dialect provider.EffortDialect
}

func (s *stubDialect) discover(context.Context, string, string, string) provider.EffortDialect {
	s.called = true
	return s.dialect
}

// The Parallel agents cap reaches this Driver too, resolved exactly as a session resolves it (ADR
// 0039 decision 2, ADR 0031's benchable-all-the-way-up): the bound entry's pin, else what the server
// advertises, else one delegation at a time.
func TestHeadlessInstallsTheParallelAgentsCap(t *testing.T) {
	const pinnedServer = "servers:\n  - name: testbox\n    endpoint: " + testServerEndpoint +
		"\n    parallel-agents: 3\nserver: testbox\n"

	tests := []struct {
		name       string
		configYAML string
		slots      int
		want       int
		wantProbe  bool
	}{
		{name: "a pin decides, and nothing is probed", configYAML: pinnedServer, slots: 9, want: 3},
		{name: "no pin takes what the server advertises", slots: 4, want: 4, wantProbe: true},
		{name: "no pin and a silent server is serial", slots: 0, want: 1, wantProbe: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			slots := &stubSlots{slots: tc.slots}
			prev := discoverSlots
			discoverSlots = slots.discover
			t.Cleanup(func() { discoverSlots = prev })

			stub := &stubRunner{}
			home := testConfigHome(t, tc.configYAML)
			if _, _, err := headlessRunOn(t, stub, fenceableHost, home, "a prompt"); err != nil {
				t.Fatalf("headless: %v", err)
			}
			if got := stub.spec.Config.ParallelAgents; got != tc.want {
				t.Errorf("Config.ParallelAgents = %d; want %d", got, tc.want)
			}
			if slots.called != tc.wantProbe {
				t.Errorf("the discovery probe ran = %v; want %v", slots.called, tc.wantProbe)
			}
		})
	}
}

// The effort wire dialect reaches this Driver too, on the same terms (ADR 0060, ADR 0031's
// benchable-all-the-way-up): the bound entry's forced `effort-dialect:`, else what discovery saw,
// else the zero — the historical `chat_template_kwargs` shape a request has always carried.
//
// It is asserted on the Config the CLI hands the runner because that IS this Driver's seam: an
// unattended run never rebinds, so the construction surface is the only place the dialect can be
// stated, and internal/agent's TestNewSeedsTheEffortDialectFromTheConfig carries the same value the
// rest of the way onto the wire. Before the seed existed every Firing sent the zero whatever the
// server read (2026-08-25 audit C-03).
func TestHeadlessSendsTheServersEffortDialect(t *testing.T) {
	const forcedServer = "servers:\n  - name: testbox\n    endpoint: " + testServerEndpoint +
		"\n    effort-dialect: off\nserver: testbox\n"

	tests := []struct {
		name       string
		configYAML string
		observed   provider.EffortDialect
		want       domain.EffortDialect
		wantProbe  bool
	}{
		{
			name:       "a forced effort-dialect: decides, and nothing is probed",
			configYAML: forcedServer,
			observed:   provider.EffortDialectReasoning,
			want:       domain.EffortDialectOff,
		},
		{
			name:      "nothing forced takes the shape discovery saw",
			observed:  provider.EffortDialectReasoning,
			want:      domain.EffortDialectReasoning,
			wantProbe: true,
		},
		{
			name:      "nothing forced and a server with no tell keeps the historical shape",
			want:      domain.EffortDialectNone,
			wantProbe: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dialects := &stubDialect{dialect: tc.observed}
			prev := discoverDialect
			discoverDialect = dialects.discover
			t.Cleanup(func() { discoverDialect = prev })

			stub := &stubRunner{}
			home := testConfigHome(t, tc.configYAML)
			if _, _, err := headlessRunOn(t, stub, fenceableHost, home, "a prompt"); err != nil {
				t.Fatalf("headless: %v", err)
			}
			if got := stub.spec.Config.EffortDialect; got != tc.want {
				t.Errorf("Config.EffortDialect = %q; want %q", got, tc.want)
			}
			if dialects.called != tc.wantProbe {
				t.Errorf("the dialect probe ran = %v; want %v", dialects.called, tc.wantProbe)
			}
		})
	}
}

// The three bounds the bound entry carries reach this Driver as well, on the same terms every other
// per-entry fact does (ADR 0031's benchable-all-the-way-up): the window is the entry's own
// `context-window:` resolved over the top-level key — config.ResolveContextWindow, the same
// precedence a session's bind resolves — the share that window is split by is its
// `response-reserve:` resolved the same way (config.ResolveResponseReserve), and the reply ceiling is
// the entry's `max-output-tokens:` (ADR 0046). One configuration cannot mean two windows, or two
// splits of one window, depending on which Driver reads it.
//
// The unpinned row keeps the precedence honest in the other direction: an entry pinning nothing
// leaves the top-level keys answering for the window and the share, and leaves the cap at 0, where
// the engine derives it from the room its Budget reserves.
func TestHeadlessBudgetsAgainstTheBoundEntrysPins(t *testing.T) {
	tests := []struct {
		name        string
		entryKeys   string
		wantWindow  int
		wantOutput  int
		wantReserve float64
	}{
		{
			name: "the bound entry's own pins outrank the top-level key",
			entryKeys: "    context-window: 65536\n    max-output-tokens: 4096\n" +
				"    response-reserve: 0.35\n",
			wantWindow:  65536,
			wantOutput:  4096,
			wantReserve: 0.35,
		},
		{
			name:        "an entry pinning nothing leaves the top-level key answering",
			wantWindow:  16384,
			wantReserve: 0.25,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := testConfigHome(t, "context-window: 16384\nresponse-reserve: 0.25\nservers:\n"+
				"  - name: testbox\n    endpoint: "+testServerEndpoint+"\n"+tc.entryKeys+"server: testbox\n")

			stub := &stubRunner{}
			if _, _, err := headlessRunOn(t, stub, fenceableHost, home, "a prompt"); err != nil {
				t.Fatalf("headless: %v", err)
			}
			if got := stub.spec.Config.Context.MaxContextTokens; got != tc.wantWindow {
				t.Errorf("Config.Context.MaxContextTokens = %d; want %d — the window this run budgets "+
					"against", got, tc.wantWindow)
			}
			if got := stub.spec.Config.Context.MaxOutputTokens; got != tc.wantOutput {
				t.Errorf("Config.Context.MaxOutputTokens = %d; want %d — the ceiling one reply of an "+
					"unattended run may reach", got, tc.wantOutput)
			}
			if got := stub.spec.Config.Context.ResponseReserveFraction; got != tc.wantReserve {
				t.Errorf("Config.Context.ResponseReserveFraction = %v; want %v — the share of that window "+
					"this run holds back for the reply", got, tc.wantReserve)
			}
		})
	}
}

// One run cannot hold two opinions about how it splits its window. The headless path resolves the
// share once, writes it onto the copy rebindSpecFor reads (the overlay a session gets from
// liveSettings.rebindInputs, a seam this Driver never passes through), and the Config then reads the
// share back OFF the resulting RebindSpec — so what the spec states and what the run divides by are
// the same number by construction.
//
// The entry override is what makes the assertion bite: feed the spec the top-level share instead and
// the Config states 0.2 here, because it is the spec's own field it reads.
func TestHeadlessSpecStatesTheShareTheConfigDividesBy(t *testing.T) {
	const (
		topLevelShare = 0.2
		entryShare    = 0.35
	)
	home := testConfigHome(t, "response-reserve: 0.2\nservers:\n"+
		"  - name: testbox\n    endpoint: "+testServerEndpoint+"\n    response-reserve: 0.35\n"+
		"server: testbox\n")

	stub := &stubRunner{}
	if _, _, err := headlessRunOn(t, stub, fenceableHost, home, "a prompt"); err != nil {
		t.Fatalf("headless: %v", err)
	}
	got := stub.spec.Config.Context.ResponseReserveFraction
	if got == topLevelShare {
		t.Fatalf("Config.Context.ResponseReserveFraction = %v — the TOP-LEVEL share; the spec this "+
			"Config reads its share off must state the bound entry's %v", got, entryShare)
	}
	if got != entryShare {
		t.Errorf("Config.Context.ResponseReserveFraction = %v; want the entry-resolved %v that the "+
			"RebindSpec states — spec and Config must not drift apart", got, entryShare)
	}
}

// A headless run gets a scratch dir of its own, named after the record it will be saved under
// (residuals sweep item 6, 2026-08-24). It had none at all before: nothing on this path mints a
// session id, so the model was offered no writable scratch inside the box and put its working files
// wherever else it could reach — the workspace itself, under an Auto fence. The same start also
// sweeps the stale dirs the TUI's boot sweeps, because a host only ever driven headlessly never
// reaches that boot and would otherwise accumulate one dir per run forever.
func TestHeadlessRunGetsItsOwnScratchDirAndSweepsStaleOnes(t *testing.T) {
	home := testConfigHome(t, "")
	scratchRoot := filepath.Join(home, "scratch")
	stale := filepath.Join(scratchRoot, "2026-01-01T00-00-00-stale")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	aged := time.Now().Add(-scratchMaxAge - time.Hour)
	if err := os.Chtimes(stale, aged, aged); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	stub := &stubRunner{}
	if _, _, err := headlessRunOn(t, stub, fenceableHost, home, "a prompt"); err != nil {
		t.Fatalf("headless: %v", err)
	}

	assertFiringScratchDir(t, stub.spec.RecordID, stub.spec.Config.ScratchDir, scratchRoot)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a stale scratch dir survived a headless start (stat err = %v); a host driven only "+
			"headlessly never passes the TUI's boot sweep", err)
	}
}

// Persistence is on by default and --no-save is the opt-out, expressed the only way internal/run
// reads it: a nil Store.
func TestHeadlessNoSaveDropsTheStore(t *testing.T) {
	t.Run("saved by default", func(t *testing.T) {
		stub := &stubRunner{}
		if _, _, err := headlessRun(t, stub, "a prompt"); err != nil {
			t.Fatalf("headless: %v", err)
		}
		if stub.spec.Store == nil {
			t.Error("Spec.Store is nil; a headless run is saved unless --no-save says otherwise")
		}
	})

	t.Run("--no-save", func(t *testing.T) {
		stub := &stubRunner{}
		if _, _, err := headlessRun(t, stub, "--no-save", "a prompt"); err != nil {
			t.Fatalf("headless: %v", err)
		}
		if stub.spec.Store != nil {
			t.Error("Spec.Store is set under --no-save; the run would be recorded")
		}
	})
}

// The answer goes to stdout alone; the summary — and the escape bytes the model's raw text may
// carry — never reach it.
func TestHeadlessOutputRouting(t *testing.T) {
	t.Run("the answer on stdout, the summary on stderr", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{SessionID: "s-42", FinalText: "the answer", Turns: 3, Denied: 2}}
		out, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		if strings.TrimRight(out, "\n") != "the answer" {
			t.Errorf("stdout = %q; want the answer alone", out)
		}
		for _, want := range []string{"session: s-42", "turns: 3", "denied: 2"} {
			if !strings.Contains(errOut, want) {
				t.Errorf("the summary does not state %q: %q", want, errOut)
			}
		}
		if strings.Contains(out, "turns:") {
			t.Errorf("the summary leaked onto stdout: %q", out)
		}
	})

	t.Run("an unsaved run omits the session segment", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{FinalText: "the answer", Turns: 1}}
		_, errOut, err := headlessRun(t, stub, "--no-save", "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		if strings.Contains(errOut, "session:") {
			t.Errorf("an unsaved run claimed a record: %q", errOut)
		}
		if !strings.Contains(errOut, "turns: 1") {
			t.Errorf("the summary is missing: %q", errOut)
		}
	})

	t.Run("each sub-agent's fill is a stderr line, in order, before the summary", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			SessionID: "s-9", FinalText: "the answer", Turns: 4, Denied: 0,
			SubAgents: []run.SubAgentUsage{
				{Used: 12000, Limit: 32768, Task: "audit the issues"},
				{Used: 800, Limit: 32768, Task: "summarise the findings"},
			},
		}}
		out, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		first := strings.Index(errOut, "sub-agent: 12k/32k · audit the issues")
		second := strings.Index(errOut, "sub-agent: 800/32k · summarise the findings")
		summary := strings.Index(errOut, "turns: 4")
		if first < 0 || second < 0 {
			t.Fatalf("the per-run lines are missing or misspelled: %q", errOut)
		}
		if first > second {
			t.Errorf("the runs are not reported in finish order: %q", errOut)
		}
		if second > summary {
			t.Errorf("a per-run line printed after the summary: %q", errOut)
		}
		if strings.TrimRight(out, "\n") != "the answer" {
			t.Errorf("stdout = %q; want the answer alone", out)
		}
	})

	t.Run("a run that delegated nothing says nothing about sub-agents", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{FinalText: "the answer", Turns: 1}}
		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		if strings.Contains(errOut, "sub-agent:") {
			t.Errorf("a run with no delegation printed a sub-agent line: %q", errOut)
		}
	})

	// The self-hiding rule the TUI's cell keeps: a fill only means something beside its limit, so a
	// run whose window the Config never named is dropped rather than spelled against nothing. Its
	// neighbour still reports, so the drop is a skip and not an abort.
	t.Run("a run with no window is omitted rather than spelled against nothing", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			FinalText: "the answer", Turns: 2,
			SubAgents: []run.SubAgentUsage{
				{Used: 9000, Limit: 0, Task: "no window here"},
				{Used: 4000, Limit: 32768, Task: "this one has one"},
			},
		}}
		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		if strings.Contains(errOut, "no window here") {
			t.Errorf("a limitless reading was printed: %q", errOut)
		}
		if !strings.Contains(errOut, "sub-agent: 4k/32k · this one has one") {
			t.Errorf("the reportable run was dropped with it: %q", errOut)
		}
	})

	// The collapsed run header's rule, kept on the Driver that has no header: a named delegation is
	// reported by its name, an unnamed one by its task exactly as before names existed. The unnamed
	// expectation is spelled as a whole line rather than a substring, because "byte-identical to what
	// this line has always printed" is the actual claim.
	t.Run("a named delegation is reported by its name, an unnamed one by its task", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			FinalText: "the answer", Turns: 3,
			SubAgents: []run.SubAgentUsage{
				{Used: 12000, Limit: 32768, Task: "audit the config loader", Name: "repo-scout"},
				{Used: 4000, Limit: 32768, Task: "summarise the findings"},
			},
		}}
		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		lines := subAgentStderrLines(errOut)
		want := []string{
			"sub-agent: 12k/32k · repo-scout",
			"sub-agent: 4k/32k · summarise the findings",
		}
		if !slices.Equal(lines, want) {
			t.Errorf("sub-agent lines = %q; want %q", lines, want)
		}
		if strings.Contains(errOut, "audit the config loader") {
			t.Errorf("a named delegation printed its task beside the name: %q", errOut)
		}
	})

	// Routing a delegation to the Sub-agent server (ADR 0045) is shown on the one line this Driver
	// gives a run: the model it went to closes that line, and only when it is not the session's own —
	// which run.SubAgentUsage.Model has already decided, so an unrouted run prints the line headless
	// runs have always printed. The id is server-reported, so it is escape-stripped and clipped on
	// the terms every wire-sourced cell beside it gets.
	t.Run("a routed delegation closes its line with the model it ran on", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			FinalText: "the answer", Turns: 3,
			SubAgents: []run.SubAgentUsage{
				{Used: 12000, Limit: 32768, Task: "audit the issues", Model: "qwen3-4b"},
				{Used: 4000, Limit: 32768, Task: "summarise the findings"},
				{Used: 4000, Limit: 32768, Name: "scout", Model: "sneaky\x1b[2Kmodel"},
			},
		}}
		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		if strings.ContainsRune(errOut, 0x1b) {
			t.Errorf("an ESC byte reached stderr from a server-reported model: %q", errOut)
		}
		lines := subAgentStderrLines(errOut)
		want := []string{
			"sub-agent: 12k/32k · audit the issues · qwen3-4b",
			"sub-agent: 4k/32k · summarise the findings",
			"sub-agent: 4k/32k · scout · sneaky[2Kmodel",
		}
		if !slices.Equal(lines, want) {
			t.Errorf("sub-agent lines = %q; want %q", lines, want)
		}
	})

	// The fill on that line is measured against the window the CHILD filled, which for a routed
	// delegation is the Sub-agent server's and not the session's (ADR 0045): run.SubAgentUsage.Limit
	// has already resolved which, so a small window shows as a small window instead of a routed run
	// reading as almost-empty against a session window it never had.
	t.Run("a routed delegation's fill reads against the window it filled", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			FinalText: "the answer", Turns: 3,
			SubAgents: []run.SubAgentUsage{
				{Used: 7000, Limit: 8192, Task: "audit the issues", Model: "qwen3-4b"},
				{Used: 7000, Limit: 131072, Task: "summarise the findings"},
			},
		}}
		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		lines := subAgentStderrLines(errOut)
		want := []string{
			"sub-agent: 7k/8k · audit the issues · qwen3-4b",
			"sub-agent: 7k/128k · summarise the findings",
		}
		if !slices.Equal(lines, want) {
			t.Errorf("sub-agent lines = %q; want %q", lines, want)
		}
	})

	// A name is raw model output on the same terms as the task, and it stands in the same slot: it is
	// folded to one line, stripped and clipped identically, and a "name" that is nothing but control
	// characters survives none of that — the task still shows rather than the slot going blank.
	t.Run("a delegation name is escape-stripped, clipped, and never blanks the slot", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			FinalText: "the answer", Turns: 3,
			SubAgents: []run.SubAgentUsage{
				{Used: 4000, Limit: 32768, Task: "the task", Name: "safe\rname\tcol\nline"},
				{Used: 4000, Limit: 32768, Task: "the task", Name: strings.Repeat("n", 200)},
				{Used: 4000, Limit: 32768, Task: "the fallback task", Name: "\x1b\x07\x00"},
			},
		}}
		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		if strings.ContainsRune(errOut, 0x1b) {
			t.Errorf("an ESC byte reached stderr: %q", errOut)
		}
		lines := subAgentStderrLines(errOut)
		if len(lines) != 3 {
			t.Fatalf("want three sub-agent lines; got %q", lines)
		}
		if lines[0] != "sub-agent: 4k/32k · safename col line" {
			t.Errorf("the name did not fold onto its own single line: %q", lines[0])
		}
		clipped := strings.TrimPrefix(lines[1], "sub-agent: 4k/32k · ")
		if n := len([]rune(clipped)); n != headlessTaskMax || !strings.HasSuffix(clipped, "…") {
			t.Errorf("the name printed %d runes (%q); want it clipped to %d with an ellipsis",
				n, clipped, headlessTaskMax)
		}
		if lines[2] != "sub-agent: 4k/32k · the fallback task" {
			t.Errorf("an all-escapes name blanked the slot instead of falling back: %q", lines[2])
		}
	})

	// The task is raw model output (internal/run says so in as many words), and this is its render
	// seam — the same seam the answer is stripped at, one stream over.
	t.Run("a sub-agent task is escape-stripped and clipped", func(t *testing.T) {
		long := strings.Repeat("t", 200)
		stub := &stubRunner{res: run.Result{
			FinalText: "the answer", Turns: 2,
			SubAgents: []run.SubAgentUsage{
				{Used: 4000, Limit: 32768, Task: "safe \x1b]52;c;cGF5bG9hZA==\x07 " + long},
			},
		}}
		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		if strings.ContainsRune(errOut, 0x1b) {
			t.Errorf("an ESC byte reached stderr: %q", errOut)
		}
		var line string
		for _, l := range strings.Split(errOut, "\n") {
			if strings.HasPrefix(l, "sub-agent:") {
				line = l
			}
		}
		task := strings.TrimPrefix(line, "sub-agent: 4k/32k · ")
		if n := len([]rune(task)); n != headlessTaskMax {
			t.Errorf("the task printed %d runes; want it clipped to %d: %q", n, headlessTaskMax, task)
		}
		if !strings.HasSuffix(task, "…") {
			t.Errorf("a clipped task does not say it was clipped: %q", task)
		}
	})

	// A task label shares its line with the reading it follows, so the two controls the answer keeps
	// fold to a space here instead of surviving: a newline would forge a second line, a tab would
	// re-column the one it is on, and a CR would rewind over the reading itself.
	t.Run("a sub-agent task cannot break the line it is printed on", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			FinalText: "the answer", Turns: 2,
			SubAgents: []run.SubAgentUsage{
				{Used: 4000, Limit: 32768, Task: "shown\rhidden\tcolumn\nline"},
			},
		}}
		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		if !strings.Contains(errOut, "sub-agent: 4k/32k · shownhidden column line") {
			t.Errorf("the task did not fold onto its own single line: %q", errOut)
		}
	})

	// What the run SPENT, beside what it filled: one line for the Firing's own totals and one per
	// delegated run that accounted for anything, all of them ahead of the summary. A sub-agent's
	// spend line is named exactly as its fill line is, so the two read as one report.
	t.Run("cumulative usage prints per agent, before the summary", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			SessionID: "s-7", FinalText: "the answer", Turns: 4,
			Usage: run.Usage{Calls: 3, PromptTokens: 18000, CompletionTokens: 1200, TotalTokens: 19200},
			SubAgents: []run.SubAgentUsage{
				{
					Used: 12000, Limit: 32768, Task: "audit the issues", Name: "repo-scout",
					Calls: 2, PromptTokens: 11800, CompletionTokens: 200, TotalTokens: 12000,
				},
			},
		}}
		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		// Spelled in the gauge's own coarse units (format.Tokens, the spelling the fill lines
		// above use), so a spend and a fill never read in two dialects on one report.
		main := strings.Index(errOut, "usage: calls 3 · prompt 18k · completion 1k · total 19k\n")
		child := strings.Index(errOut, "usage: calls 2 · prompt 12k · completion 200 · total 12k · repo-scout\n")
		summary := strings.Index(errOut, "turns: 4")
		if main < 0 || child < 0 {
			t.Fatalf("the usage lines are missing or misspelled: %q", errOut)
		}
		if main > child {
			t.Errorf("a delegated run's spend printed before the firing's own: %q", errOut)
		}
		if child > summary {
			t.Errorf("a usage line printed after the summary: %q", errOut)
		}
	})

	// The fill lines' self-hiding rule, applied to the reading this line carries: an agent that
	// accounted for no call has nothing to say about spend, and four zeros would say it wrongly.
	t.Run("an agent that counted no call prints no usage line", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			FinalText: "the answer", Turns: 2,
			SubAgents: []run.SubAgentUsage{
				{Used: 4000, Limit: 32768, Task: "silent about its spend"},
			},
		}}
		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		if strings.Contains(errOut, "usage:") {
			t.Errorf("a run that accounted for nothing printed a usage line: %q", errOut)
		}
		if !strings.Contains(errOut, "sub-agent: 4k/32k · silent about its spend") {
			t.Errorf("the fill line went with it: %q", errOut)
		}
	})

	// The cached share is a SUBSET of the prompt count that most Upstreams never report, so its
	// zero means "no breakdown was sent" rather than a spend of zero — it hides itself, unlike the
	// counters the line always carries. It rides both grains: the Firing's own line and a
	// delegate's, since a child's cache hits are its own spend to account for.
	t.Run("the cached share appears only when the server reported one", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			SessionID: "s-8", FinalText: "the answer", Turns: 2,
			Usage: run.Usage{
				Calls: 2, PromptTokens: 18000, CompletionTokens: 1200, TotalTokens: 19200,
				CachedPromptTokens: 12000,
			},
			SubAgents: []run.SubAgentUsage{
				{
					Used: 12000, Limit: 32768, Task: "audit the issues", Name: "repo-scout",
					Calls: 2, PromptTokens: 11800, CompletionTokens: 200, TotalTokens: 12000,
				},
			},
		}}
		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		if !strings.Contains(errOut, "usage: calls 2 · prompt 18k · completion 1k · total 19k · cached 12k\n") {
			t.Errorf("the cached column is missing or misspelled: %q", errOut)
		}
		if !strings.Contains(errOut, "usage: calls 2 · prompt 12k · completion 200 · total 12k · repo-scout\n") {
			t.Errorf("a delegate that reported no cached share grew a cached column anyway: %q", errOut)
		}
	})

	// A server that reports the two parts and omits the sum leaves the total at zero, and the
	// shared token formatter spells a zero as nothing at all — which would leave the column
	// hanging. The line has already earned its place by counting a call, so the zero is spelled.
	t.Run("a zero counter is spelled rather than left blank", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			FinalText: "the answer", Turns: 1,
			Usage: run.Usage{Calls: 1, PromptTokens: 900, CompletionTokens: 100},
		}}
		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		if !strings.Contains(errOut, "usage: calls 1 · prompt 900 · completion 100 · total 0\n") {
			t.Errorf("the zero total is not spelled: %q", errOut)
		}
	})

	t.Run("terminal escapes are stripped from the answer", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{FinalText: "safe \x1b]52;c;cGF5bG9hZA==\x07 text", Turns: 1}}
		out, _, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}
		if strings.ContainsRune(out, 0x1b) {
			t.Errorf("an ESC byte reached stdout: %q", out)
		}
		if !strings.Contains(out, "safe ") || !strings.Contains(out, " text") {
			t.Errorf("the strip ate ordinary text: %q", out)
		}
	})
}

// TestHeadlessSubAgentLineUsesTheGeneratedName pins this Driver's half of the naming journey (ADR
// 0068): a delegation the model left unnamed, named out of band while it ran, reaches headless as a
// run.SubAgentUsage whose Name is the generated one — and the line prints it exactly as it prints a
// name the call gave. That indistinguishability IS the claim: nothing here asks where the name came
// from, so the sibling rows are the byte-identical pin that a call-given name and an unnamed
// delegation still print the line they always printed.
func TestHeadlessSubAgentLineUsesTheGeneratedName(t *testing.T) {
	stub := &stubRunner{res: run.Result{
		FinalText: "the answer", Turns: 4,
		SubAgents: []run.SubAgentUsage{
			// Unnamed by its call, named by the naming call: run.go folds the generated
			// name onto this reading, so it arrives here in the same field as any other.
			{Used: 12000, Limit: 32768, Task: "audit the config loader", Name: "audit config keys"},
			{Used: 8000, Limit: 32768, Task: "read the manual", Name: "repo-scout"},
			{Used: 4000, Limit: 32768, Task: "summarise the findings"},
		},
	}}
	_, errOut, err := headlessRun(t, stub, "a prompt")
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	lines := subAgentStderrLines(errOut)
	want := []string{
		"sub-agent: 12k/32k · audit config keys",
		"sub-agent: 8k/32k · repo-scout",
		"sub-agent: 4k/32k · summarise the findings",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("sub-agent lines = %q; want %q", lines, want)
	}
	if strings.Contains(errOut, "audit the config loader") {
		t.Errorf("a renamed delegation printed its task beside the generated name: %q", errOut)
	}
}

// The sanitizer's whole job as THIS Driver spends it, pinned character by character. The set and
// its reasons belong to internal/sanitize, which tests them exhaustively; what this table guards is
// that the two CLI-visible forms keep coming from that one helper — a C0 control character is an
// instruction to the terminal rather than a character in the text, and a bidi one reorders the
// glyphs of a line printed beside a reading without touching a byte. Both forms drop the class;
// they differ only over the two controls prose is written with, which the answer keeps and a
// one-line label folds to a space.
func TestHeadlessStripEscapesDropsControlCharacters(t *testing.T) {
	for _, tc := range []struct {
		name     string
		in       string
		want     string // what the answer path prints
		wantLine string // what a sub-agent's task label prints
	}{
		{"plain text passes through untouched", "just an answer", "just an answer", "just an answer"},
		{"ESC opens an ANSI sequence", "safe\x1b[31mred", "safe[31mred", "safe[31mred"},
		{"BEL rings the bell", "safe\x07text", "safetext", "safetext"},
		{"CR rewinds the line", "shown\rhidden", "shownhidden", "shownhidden"},
		{"CRLF leaves the newline behind", "first\r\nsecond", "first\nsecond", "first second"},
		{
			"an OSC 52 clipboard write is left inert",
			"safe \x1b]52;c;cGF5bG9hZA==\x07 text",
			"safe ]52;c;cGF5bG9hZA== text",
			"safe ]52;c;cGF5bG9hZA== text",
		},
		{"NUL, backspace and the rest of C0 go too", "a\x00b\x08c\x1fd", "abcd", "abcd"},
		{"DEL goes with them", "a\x7fb", "ab", "ab"},
		{"newline and tab are the answer's own", "para\n\nnext\tcolumn", "para\n\nnext\tcolumn", "para  next column"},
		{"non-ASCII text is not control text", "héllo — 世界 ✓", "héllo — 世界 ✓", "héllo — 世界 ✓"},
		{"the bidi set goes too: it reorders a line the CLI prints", "a\u202eb\u2066c\u200e", "abc", "abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitize.StripEscapes(tc.in)
			if got != tc.want {
				t.Errorf("StripEscapes(%q) = %q; want %q", tc.in, got, tc.want)
			}
			for _, r := range got {
				if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || sanitize.BidiControl(r) {
					t.Errorf("StripEscapes(%q) left %#U behind: %q", tc.in, r, got)
				}
			}
			line := sanitize.StripEscapesToLine(tc.in)
			if line != tc.wantLine {
				t.Errorf("StripEscapesToLine(%q) = %q; want %q", tc.in, line, tc.wantLine)
			}
			for _, r := range line {
				if r < 0x20 || r == 0x7f || sanitize.BidiControl(r) {
					t.Errorf("StripEscapesToLine(%q) left %#U behind: %q", tc.in, r, line)
				}
			}
		})
	}
}

// The spelling this Driver's sub-agent lines are built from, pinned value by value at the CLI seam.
// internal/format owns the rule and tests it exhaustively; what this table guards is that the
// user-visible CLI strings keep coming from that one helper — a headless line is read by scripts,
// so a silent change of unit here is a change of interface. A count below one renders empty, which
// is why a zero-limit run is dropped upstream rather than printed as "9k/".
func TestHeadlessTokenSpellingMatchesTheGauge(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{-1, ""},
		{0, ""},
		{1, "1"},
		{999, "999"},
		{1000, "1k"},
		{1999, "2k"},
		{18432, "18k"},
		{32768, "32k"},
	} {
		if got := format.Tokens(tc.n); got != tc.want {
			t.Errorf("format.Tokens(%d) = %q; want %q", tc.n, got, tc.want)
		}
	}
}

// captureProcessStreams swaps the process's REAL os.Stdout and os.Stderr for pipes while fn runs
// and returns what each one received. Wiring Cobra's own SetOut/SetErr cannot answer the question
// this file has to answer — which of the two streams a shell would find the answer on — because
// setting an out writer changes where Cobra's OutOrStderr fallback lands. Only the process streams
// tell the truth.
//
// Both pipes are drained concurrently so a write can never block on a full pipe buffer, and the
// originals are restored before the reads are joined.
func captureProcessStreams(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	var wg sync.WaitGroup
	var outBuf, errBuf bytes.Buffer
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&outBuf, outR) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&errBuf, errR) }()

	prevOut, prevErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	func() {
		defer func() { os.Stdout, os.Stderr = prevOut, prevErr }()
		fn()
	}()

	_ = outW.Close()
	_ = errW.Close()
	wg.Wait()
	_ = outR.Close()
	_ = errR.Close()
	return outBuf.String(), errBuf.String()
}

// The stream split as a SHELL sees it, over the process's real stdout and stderr with no out
// writer wired — the invocation `apogee headless "..." > answer.txt` actually performs.
//
// This is the guard on a mistake the earlier attempt made and the earlier test enshrined: Cobra's
// cmd.Println writes to OutOrStderr, so printing the product with it sends the answer to stderr
// everywhere except in a test that has called SetOut. Asserting on Cobra's buffers alone cannot
// catch that — this test would fail immediately if the print regressed to the Print family.
func TestHeadlessAnswerLandsOnTheProcessStdout(t *testing.T) {
	stub := &stubRunner{res: run.Result{SessionID: "s-7", FinalText: "the answer", Turns: 3, Denied: 1}}
	prev := runOnce
	runOnce = stub.once
	t.Cleanup(func() { runOnce = prev })
	t.Setenv(config.EnvMode, "")

	configDir, workspace := testConfigHome(t, ""), t.TempDir()
	var runErr error
	stdout, stderr := captureProcessStreams(t, func() {
		cmd := newHeadlessCommand()
		// Deliberately no SetOut: the fallback under test is the one every real run takes.
		cmd.SetIn(strings.NewReader(""))
		cmd.SetArgs([]string{"--config", configDir, "--workspace", workspace, "a prompt"})
		runErr = cmd.ExecuteContext(context.Background())
	})
	if runErr != nil {
		t.Fatalf("headless: %v (stderr: %q)", runErr, stderr)
	}

	if strings.TrimRight(stdout, "\n") != "the answer" {
		t.Errorf("process stdout = %q; want the answer and nothing else", stdout)
	}
	if strings.Contains(stderr, "the answer") {
		t.Errorf("the answer reached process stderr; a redirect of stdout would lose it: %q", stderr)
	}
	if !strings.Contains(stderr, "turns: 3") || !strings.Contains(stderr, "denied: 1") {
		t.Errorf("the summary is not on process stderr: %q", stderr)
	}
	if strings.Contains(stdout, "turns:") {
		t.Errorf("the summary reached process stdout: %q", stdout)
	}
}

// The exit-code convention this command introduces, end to end through the error type: 0 for a
// completed run, 1 for a run that started and failed, 2 for one that never started, 3 for one that
// reached its boundary with its final Turn abandoned.
func TestHeadlessExitCodes(t *testing.T) {
	t.Run("a completed run exits 0", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{SessionID: "s-1", FinalText: "done", Turns: 2, Denied: 4}}
		_, _, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("a completed run returned an error: %v", err)
		}
	})

	t.Run("a failed run exits 1 and names the partial record", func(t *testing.T) {
		stub := &stubRunner{
			res: run.Result{SessionID: "s-2", FinalText: "half an answer", Turns: 1},
			err: errors.New("apogee: the firing was cancelled"),
		}
		out, _, err := headlessRun(t, stub, "a prompt")
		if err == nil {
			t.Fatal("a failed run returned no error")
		}
		if code := exitCodeFor(err); code != exitRunFailed {
			t.Errorf("exit code = %d; want %d", code, exitRunFailed)
		}
		if !strings.Contains(err.Error(), "partial run saved as s-2") {
			t.Errorf("the failure does not name the partial record: %q", err.Error())
		}
		if !strings.Contains(out, "half an answer") {
			t.Errorf("a failed run withheld what it salvaged: %q", out)
		}
	})

	t.Run("a faulted run exits 3 and says so on stdout, stderr and the error", func(t *testing.T) {
		// run.Once's shape for an abandoned final Turn: no error at all, a saved record, and
		// whatever the run last said on FinalText — which is exactly why the exit code and the
		// summary have to carry the fault, since nothing else about the Result looks unusual.
		stub := &stubRunner{res: run.Result{
			SessionID: "s-3",
			FinalText: "the run's last words",
			Turns:     2,
			Faulted:   true,
			Fault:     "the model returned an empty reply",
		}}
		out, errOut, err := headlessRun(t, stub, "a prompt")
		if err == nil {
			t.Fatal("a faulted run was reported as a success")
		}
		if code := exitCodeFor(err); code != exitRunFaulted {
			t.Errorf("exit code = %d; want %d", code, exitRunFaulted)
		}
		if !strings.Contains(err.Error(), "the model returned an empty reply") {
			t.Errorf("the fault does not name the reason: %q", err.Error())
		}
		if !strings.Contains(err.Error(), "partial run saved as s-3") {
			t.Errorf("the fault does not name the partial record: %q", err.Error())
		}
		if strings.TrimRight(out, "\n") != "the run's last words" {
			t.Errorf("stdout = %q; a faulted run still hands over what it said", out)
		}
		if !strings.Contains(errOut, "faulted") {
			t.Errorf("the summary does not say the run faulted: %q", errOut)
		}
	})

	t.Run("a run that both errored and faulted exits 1", func(t *testing.T) {
		// The failure is the more actionable of the two, so it keeps the exit code it has always
		// had: the fault branch sits behind the failure branch and never overrides it.
		stub := &stubRunner{
			res: run.Result{SessionID: "s-4", FinalText: "half an answer", Turns: 1, Faulted: true, Fault: "an empty reply"},
			err: errors.New("apogee: the firing was cancelled"),
		}
		_, _, err := headlessRun(t, stub, "a prompt")
		if err == nil {
			t.Fatal("a failed run returned no error")
		}
		if code := exitCodeFor(err); code != exitRunFailed {
			t.Errorf("exit code = %d; want %d", code, exitRunFailed)
		}
	})

	t.Run("a save failure after a good run still prints the answer", func(t *testing.T) {
		// run.Once's own shape for this case: the Result carries the answer and no record id,
		// and only the returned error reports that the record did not land.
		stub := &stubRunner{
			res: run.Result{FinalText: "the answer", Turns: 2},
			err: errors.New("apogee: save the firing's record: disk full"),
		}
		out, _, err := headlessRun(t, stub, "a prompt")
		if err == nil {
			t.Fatal("a save failure was swallowed")
		}
		if code := exitCodeFor(err); code != exitRunFailed {
			t.Errorf("exit code = %d; want %d", code, exitRunFailed)
		}
		if strings.TrimRight(out, "\n") != "the answer" {
			t.Errorf("stdout = %q; the answer must survive a failed save", out)
		}
		if strings.Contains(err.Error(), "partial run saved") {
			t.Errorf("a run with no record claimed one: %q", err.Error())
		}
	})

	t.Run("an unavailable auto never started, so it exits 2", func(t *testing.T) {
		stub := &stubRunner{err: apogee.ErrAutoUnavailable}
		_, errOut, err := headlessRun(t, stub, "--mode", "auto", "a prompt")
		if err == nil {
			t.Fatal("an unavailable auto was reported as a success")
		}
		if code := exitCodeFor(err); code != exitNotStarted {
			t.Errorf("exit code = %d; want %d", code, exitNotStarted)
		}
		if !strings.Contains(err.Error(), "filesystem-write confinement") {
			t.Errorf("the refusal is not the friendly one: %q", err.Error())
		}
		if strings.Contains(errOut, "turns:") {
			t.Errorf("a run that never started printed a summary: %q", errOut)
		}
	})

	t.Run("a construction refusal never started, so it exits 2", func(t *testing.T) {
		// run.Once's own shape for every pre-run exit: the ZERO Result — no Turn was ever driven —
		// beside the error that says why. The commonest one in the field is a host whose config
		// never named an endpoint, and it must not be reported as a run that failed.
		stub := &stubRunner{err: errors.New("apogee: construct the firing's agent: apogee: Config.Endpoint is required")}
		out, errOut, err := headlessRun(t, stub, "a prompt")
		if err == nil {
			t.Fatal("a construction refusal was reported as a success")
		}
		if code := exitCodeFor(err); code != exitNotStarted {
			t.Errorf("exit code = %d; want %d", code, exitNotStarted)
		}
		if strings.Contains(errOut, "turns:") || strings.Contains(errOut, "sub-agent:") {
			t.Errorf("a run that never started printed a summary: %q", errOut)
		}
		if out != "" {
			t.Errorf("a run that never started wrote to stdout: %q", out)
		}
	})

	t.Run("an error carrying no code exits 1", func(t *testing.T) {
		if code := exitCodeFor(errors.New("something else went wrong")); code != 1 {
			t.Errorf("exit code = %d; want 1 for an ordinary error", code)
		}
		if code := exitCodeFor(nil); code != 1 {
			t.Errorf("exit code = %d; want the 1 default", code)
		}
	})
}

// Cobra validates flags and the argument count BEFORE it calls RunE, so the exit convention has to
// reach those refusals from outside the command body or they leave as bare errors and exit 1 —
// telling a script the model ran and failed when nothing was ever sent.
func TestHeadlessUsageErrorsNeverStartARun(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "an unknown flag", args: []string{"--bogus", "a prompt"}},
		{name: "a flag value that will not parse", args: []string{"--no-save=maybe", "a prompt"}},
		{name: "an unquoted prompt arrives as several arguments", args: []string{"summarise", "the", "repo"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubRunner{}
			out, errOut, err := headlessRun(t, stub, tc.args...)

			if err == nil {
				t.Fatal("the invocation was accepted")
			}
			if code := exitCodeFor(err); code != exitNotStarted {
				t.Errorf("exit code = %d; want %d (err: %v)", code, exitNotStarted, err)
			}
			if stub.called {
				t.Error("the runner ran for an invocation Cobra rejected")
			}
			if out != "" {
				t.Errorf("a run that never started wrote to stdout: %q", out)
			}
			if strings.Contains(errOut, "turns:") {
				t.Errorf("a run that never started printed a summary: %q", errOut)
			}
		})
	}
}

// The same convention through the REAL runner, on the invocation a fresh host actually makes:
// `apogee headless --config <fresh> "hi"` with no server named anywhere. Selection has nothing to
// start on, so no prompt is ever sent — exit 2, no answer, and no summary claiming a run that never
// happened. Nothing here touches the network: the refusal lands before a request exists, which is
// exactly why the run counts as never started. A headless run gets the hard error rather than a
// picker for the reason ADR 0036 gives: there is nobody to ask.
func TestHeadlessWithNoServerNeverStartsARun(t *testing.T) {
	// The host's own environment must not hand the run a server the test is asserting is absent.
	t.Setenv(config.EnvServer, "")
	t.Setenv(config.EnvMode, "")

	cmd := newHeadlessCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--config", t.TempDir(), "--workspace", t.TempDir(), "--no-save", "hi"})

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("a run with no server configured was reported as a success")
	}
	if !strings.Contains(err.Error(), "no servers are configured") {
		t.Errorf("the refusal does not say what is missing: %v", err)
	}
	if code := exitCodeFor(err); code != exitNotStarted {
		t.Errorf("exit code = %d; want %d (err: %v)", code, exitNotStarted, err)
	}
	if out.String() != "" {
		t.Errorf("a run that never started wrote to stdout: %q", out.String())
	}
	if strings.Contains(errOut.String(), "turns:") {
		t.Errorf("a run that never started printed a summary: %q", errOut.String())
	}
}

// The pre-bound start is the TUI's alone. Every way a startup server can be undetermined — nothing
// configured, nothing chosen, a choice that names an entry the list no longer carries — is still a
// refusal here, because the reason the TUI may ask instead (there is a human in front of it) is
// exactly the reason a headless run may not.
func TestHeadlessRefusesEveryUndeterminedStartup(t *testing.T) {
	t.Setenv(config.EnvServer, "")
	t.Setenv(config.EnvMode, "")
	t.Setenv(config.EnvEndpoint, "")

	const list = "servers:\n  - name: laptop\n    endpoint: http://127.0.0.1:1111\n"
	tests := []struct {
		name       string
		configYAML string
		want       string
	}{
		{name: "nothing configured", configYAML: "mode: plan\n", want: "no servers are configured"},
		{name: "nothing chosen", configYAML: list, want: "no startup server is chosen"},
		{name: "a stale choice", configYAML: list + "server: the-old-name\n", want: `names "the-old-name"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(tt.configYAML), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			cmd := newHeadlessCommand()
			var out, errOut bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			cmd.SetIn(strings.NewReader(""))
			cmd.SetArgs([]string{"--config", home, "--workspace", t.TempDir(), "--no-save", "hi"})

			err := cmd.ExecuteContext(context.Background())
			if err == nil {
				t.Fatal("an undetermined startup was accepted by a driver with nobody to ask")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("the refusal does not say what is missing (%q): %v", tt.want, err)
			}
			if code := exitCodeFor(err); code != exitNotStarted {
				t.Errorf("exit code = %d; want %d (err: %v)", code, exitNotStarted, err)
			}
			if out.String() != "" {
				t.Errorf("a run that never started wrote to stdout: %q", out.String())
			}
		})
	}
}

// The prompt reaches the runner off stdin too — the pipeline form, with no argument at all.
func TestHeadlessReadsThePromptFromStdin(t *testing.T) {
	stub := &stubRunner{res: run.Result{FinalText: "ok", Turns: 1}}
	prev := runOnce
	runOnce = stub.once
	t.Cleanup(func() { runOnce = prev })
	t.Setenv(config.EnvMode, "")

	cmd := newHeadlessCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader("what does this repo do?\n"))
	cmd.SetArgs([]string{"--config", testConfigHome(t, ""), "--workspace", t.TempDir(), "--no-save"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("headless: %v\n%s", err, errOut.String())
	}
	if stub.spec.Prompt != "what does this repo do?" {
		t.Errorf("Spec.Prompt = %q; want the piped prompt", stub.spec.Prompt)
	}
}

// The command is reachable by name off the root, and registering it leaves the bare invocation
// alone — the seam's standing guarantee (root_test.go).
func TestHeadlessIsRegisteredOnTheRoot(t *testing.T) {
	t.Parallel()
	var found *cobra.Command
	for _, sub := range subcommands() {
		if sub.Name() == "headless" {
			found = sub
		}
	}
	if found == nil {
		t.Fatal("`headless` is not registered in subcommands()")
	}
	if !found.SilenceUsage || !found.SilenceErrors {
		t.Error("the command dumps usage or prints its own error; main owns both")
	}
}

// TestHeadlessPrintsThePruneNotice pins the one Event the headless Driver renders live. A prune
// happens MID-run and leaves no trace on Result, so without this line a human watching an
// unattended run would see the window quietly shrink with nothing said.
//
// It goes to STDERR, where it cannot contaminate the answer a pipeline reads off stdout, and it is
// worded exactly as every other Driver words it (internal/tui's transcript.addPrune, internal/run's
// transcriptFold.fold) from the event's own two counts.
func TestHeadlessPrintsThePruneNotice(t *testing.T) {
	stub := &stubRunner{emit: func(sink domain.EventSink) {
		sink.Emit(domain.PruneEvent{Results: 3, Tokens: 1200})
	}}

	out, errOut, err := headlessRun(t, stub, "explain this repo")
	if err != nil {
		t.Fatalf("headless: %v", err)
	}

	const want = "pruned 3 tool results (~1200 tokens)"
	if !slices.Contains(strings.Split(errOut, "\n"), want) {
		t.Errorf("stderr carries no prune notice reading %q:\n%s", want, errOut)
	}
	if strings.Contains(out, "pruned") {
		t.Errorf("the notice reached stdout, where a pipeline reads the answer: %q", out)
	}
}

// TestPruneNoticeSinkForwardsEveryEvent pins the wrap half of the sink: it prints for a prune and
// hands EVERY Event on to the sink it wraps, its own included. run.Once puts its tap around this
// one (run.Spec), so a sink that swallowed what it rendered would cost the Firing's record the very
// entry the notice is about.
//
// The FloorGuardEvent is here to pin the silence: a Floor guard firing is forwarded like anything
// else and prints NO stderr line, since a guard repairing the model's own failure is engine
// behaviour rather than news for an unattended run (ADR 0071).
func TestPruneNoticeSinkForwardsEveryEvent(t *testing.T) {
	t.Parallel()

	inner := &recordingSink{}
	var errOut bytes.Buffer
	sink := pruneNoticeSink{inner: inner, out: &errOut}

	sink.Emit(domain.PruneEvent{Results: 2, Tokens: 800})
	sink.Emit(domain.FloorGuardEvent{Guard: "tool-call-repair", Action: "retry"})
	sink.Emit(domain.MessageEvent{Text: "done"})

	if len(inner.events) != 3 {
		t.Errorf("the wrapped sink saw %d events, want all three: %+v", len(inner.events), inner.events)
	}
	if got, want := errOut.String(), "pruned 2 tool results (~800 tokens)\n"; got != want {
		t.Errorf("printed %q, want %q — the prune alone, nothing for the guard or the message", got, want)
	}
}

// recordingSink keeps every Event handed to it, in order. [domain.EventSink] promises serialized
// emission, so it needs no lock.
type recordingSink struct{ events []domain.Event }

func (s *recordingSink) Emit(e domain.Event) { s.events = append(s.events, e) }
