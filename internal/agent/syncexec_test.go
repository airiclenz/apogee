package agent

// The SYNC lane's out-of-process executor (syncexec.go). These tests drive runSyncArgv directly
// rather than through a seam: the seams that call it (the advise slot, the gate stage) arrive in
// later items, and what is pinned here is the executor's own contract — the permit row, the class
// deadline, the payload document and what a failure is reported as.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// syncAgent builds an Agent whose confinement posture is the one under test. The MODE is
// deliberately Plan on every row: a user's sync reaction fires in every mode, so a test that only
// ever ran in Auto would pass against a copy of hookExecutionCtx's ladder.
func syncAgent(t *testing.T, sink domain.EventSink, conf domain.Confiner, confine bool) *Agent {
	t.Helper()

	cfg := baseConfig(sink)
	cfg.Mode = domain.ModePlan
	cfg.WorkspaceDir = t.TempDir()
	cfg.Confiner = conf
	cfg.ConfineToWorkspace = confine

	a, err := newAgent(cfg, echoResponder{reply: "reply"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a
}

// syncReaction is one user-origin sync reaction over argv, with the class and deadline the row
// under test needs.
func syncReaction(class domain.Class, timeout time.Duration, argv ...string) domain.Reaction {
	on := domain.MomentPostToolResult
	if class == domain.ClassGate {
		on = domain.MomentPreToolExec
	}
	return domain.Reaction{
		ID:      "watcher",
		Origin:  domain.OriginUser,
		Class:   class,
		On:      []domain.Moment{on},
		Handler: domain.ArgvHandler{Argv: argv},
		Timeout: timeout,
	}
}

// TestSyncArgvRunsUnfencedAndReturnsStdout pins the permit row's unfenced arm: with
// confine-to-workspace off the reaction spawns under a permit carrying no box, and the caller gets
// the command's standard output alone. It also pins the two halves of the document — the JSON on
// stdin, whose identity block the executor stamps, and the headline environment variables a
// one-line script reads instead of parsing it.
func TestSyncArgvRunsUnfencedAndReturnsStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the executor it pins is platform-independent")
	}
	t.Parallel()

	sink := &recordingSink{}
	a := syncAgent(t, sink, &fakeConfiner{caps: capsBoth()}, false)

	out, err := a.runSyncArgv(
		context.Background(),
		7,
		syncReaction(domain.ClassAdvise, 0, "/bin/sh", "-c", `printf '%s' "$APOGEE_REACTION_EVENT"; cat`),
		domain.SeamPayload{Event: domain.MomentPostToolResult, Tool: "write_file"},
	)
	if err != nil {
		t.Fatalf("runSyncArgv: %v", err)
	}

	event, document, ok := strings.Cut(out, "{")
	if !ok {
		t.Fatalf("stdout = %q, want the event name followed by the JSON document", out)
	}
	if event != string(domain.MomentPostToolResult) {
		t.Errorf("APOGEE_REACTION_EVENT = %q, want %q", event, domain.MomentPostToolResult)
	}

	var got domain.SeamPayload
	if err := json.Unmarshal([]byte("{"+document), &got); err != nil {
		t.Fatalf("the stdin document is not the payload JSON: %v (%q)", err, document)
	}
	if got.Reaction != "watcher" || got.Tool != "write_file" {
		t.Errorf("document reaction/tool = %q/%q, want %q/%q", got.Reaction, got.Tool, "watcher", "write_file")
	}
	if got.Turn != 7 {
		t.Errorf("document turn = %d, want 7 — the executor stamps the identity block", got.Turn)
	}
	if got.Workspace != a.cfg.WorkspaceDir {
		t.Errorf("document workspace = %q, want %q", got.Workspace, a.cfg.WorkspaceDir)
	}
	if got.Time == "" {
		t.Error("document time is empty, want the firing's RFC 3339 stamp")
	}
	if len(sink.events) != 0 {
		t.Errorf("a successful run emitted %d events, want none — only a failure books a firing", len(sink.events))
	}
}

// TestSyncArgvRunsConfinedWhenTheBoxCanBeBuilt pins the permit row's fenced arm: confine-to-workspace
// on with a capable Confiner hands the funnel the workspace box, so the command runs inside the
// same fence a subprocess tool would have.
func TestSyncArgvRunsConfinedWhenTheBoxCanBeBuilt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the executor it pins is platform-independent")
	}
	t.Parallel()

	conf := &fakeConfiner{caps: capsBoth()}
	a := syncAgent(t, &recordingSink{}, conf, true)

	out, err := a.runSyncArgv(
		context.Background(),
		1,
		syncReaction(domain.ClassGate, 0, "/bin/sh", "-c", "echo allow"),
		domain.SeamPayload{Event: domain.MomentPreToolExec},
	)
	if err != nil {
		t.Fatalf("runSyncArgv: %v", err)
	}
	if out != "allow\n" {
		t.Errorf("stdout = %q, want %q", out, "allow\n")
	}
	if conf.confineCount() != 1 {
		t.Errorf("Confine called %d times, want 1 — the permit's box must reach the funnel", conf.confineCount())
	}
	if conf.lastBox.WorkspaceRoot != a.cfg.WorkspaceDir {
		t.Errorf("box.WorkspaceRoot = %q, want %q", conf.lastBox.WorkspaceRoot, a.cfg.WorkspaceDir)
	}
}

// TestSyncArgvRefusesWhenConfinementIsUnavailable pins the permit row's refusal: the operator asked
// for the fence and the host cannot build one, so there is no permit and NOTHING is spawned. The
// canary is the file the command would have written.
func TestSyncArgvRefusesWhenConfinementIsUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the executor it pins is platform-independent")
	}
	t.Parallel()

	a := syncAgent(t, &recordingSink{}, &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: false}}, true)
	canary := filepath.Join(t.TempDir(), "spawned")

	out, err := a.runSyncArgv(
		context.Background(),
		1,
		syncReaction(domain.ClassGate, 0, "/bin/sh", "-c", "touch "+canary),
		domain.SeamPayload{Event: domain.MomentPreToolExec},
	)
	if err == nil {
		t.Fatalf("runSyncArgv err = nil, want the confinement refusal (out = %q)", out)
	}
	if want := "workspace confinement is unavailable on this host"; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, want it to say %q", err, want)
	}
	if _, statErr := os.Stat(canary); statErr == nil {
		t.Error("the command ran; a refused permit must spawn nothing at all")
	}
}

// TestSyncArgvTimesOutAndTheFailureIsReported pins what a command that never answers costs: the
// deadline kills it, the error says so, and reportReaction turns that into exactly one line for the
// user and one `failed` firing for the Event stream — never an ErrorEvent, since a user's command
// failing is that reaction doing nothing rather than a fault of the engine's.
func TestSyncArgvTimesOutAndTheFailureIsReported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the executor it pins is platform-independent")
	}
	t.Parallel()

	sink := &recordingSink{}
	a := syncAgent(t, sink, &fakeConfiner{caps: capsBoth()}, false)

	var reported []string
	a.cfg.Report = func(msg string) { reported = append(reported, msg) }

	r := syncReaction(domain.ClassGate, 150*time.Millisecond, "/bin/sh", "-c", "sleep 30")
	_, err := a.runSyncArgv(context.Background(), 4, r, domain.SeamPayload{Event: domain.MomentPreToolExec})
	if err == nil {
		t.Fatal("runSyncArgv err = nil, want the deadline to have killed the command")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v, want it to name the timeout", err)
	}

	a.reportReaction(4, r.ID, domain.MomentPreToolExec, err)

	if len(reported) != 1 {
		t.Fatalf("Report called %d times, want exactly 1: %q", len(reported), reported)
	}
	if want := "reaction watcher (pre-tool-exec): "; !strings.HasPrefix(reported[0], want) {
		t.Errorf("report line = %q, want it to start %q — the observe lane's sentence", reported[0], want)
	}

	if len(sink.events) != 1 {
		t.Fatalf("the failure emitted %d events, want exactly 1: %+v", len(sink.events), sink.events)
	}
	fired, ok := sink.events[0].(domain.ReactionFiredEvent)
	if !ok {
		t.Fatalf("event = %T, want a domain.ReactionFiredEvent — a failing command is never an ErrorEvent", sink.events[0])
	}
	if fired.Reaction != "watcher" || fired.Origin != domain.OriginUser {
		t.Errorf("firing = %q/%q, want %q/%q", fired.Reaction, fired.Origin, "watcher", domain.OriginUser)
	}
	if fired.Moment != domain.MomentPreToolExec || fired.Action != "failed" {
		t.Errorf("firing moment/action = %q/%q, want %q/%q", fired.Moment, fired.Action, domain.MomentPreToolExec, "failed")
	}
	if fired.Detail != err.Error() {
		t.Errorf("firing detail = %q, want the error verbatim", fired.Detail)
	}
	if fired.Turn != 4 {
		t.Errorf("firing turn = %d, want 4", fired.Turn)
	}
}

// TestSyncArgvClassDeadlines pins the deadline the executor applies when an entry sets no
// `timeout:` of its own: 10s for advise, 5s for gate (ADR 0076 D7), and the entry's own value
// whenever it set one. It is a table over syncTimeout rather than four sleeping commands, so
// pinning a ten-second default costs no ten seconds.
func TestSyncArgvClassDeadlines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		class domain.Class
		set   time.Duration
		want  time.Duration
	}{
		{name: "advise takes the class default", class: domain.ClassAdvise, want: domain.DefaultAdviseTimeout},
		{name: "gate takes the shorter class default", class: domain.ClassGate, want: domain.DefaultGateTimeout},
		{name: "an entry's own timeout wins", class: domain.ClassGate, set: 90 * time.Second, want: 90 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := syncTimeout(syncReaction(tc.class, tc.set, "/bin/true")); got != tc.want {
				t.Errorf("syncTimeout = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestReportReactionWithoutAReportSinkIsSilent pins the nil-Report default: a Driver with nowhere
// to put the line drops it, and the firing is still booked — the Event stream is the half every
// Driver has.
func TestReportReactionWithoutAReportSinkIsSilent(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a := syncAgent(t, sink, &fakeConfiner{caps: capsBoth()}, false)

	a.reportReaction(2, "watcher", domain.MomentPostToolResult, context.DeadlineExceeded)

	if len(sink.events) != 1 {
		t.Fatalf("emitted %d events, want exactly 1", len(sink.events))
	}
}
