package agent

// The SYNC lane's out-of-process executor (syncexec.go). These tests drive runSyncArgv directly
// rather than through a seam: the seams that call it (the advise slot, the gate stage) have tests
// of their own, and what is pinned here is the executor's own contract — the permit row, the class
// deadline, the payload document and what a failure is reported as. The webhook pair at the end is
// the exception: it drives runSyncWebhook through the gate stage (gate_test.go's helpers), because
// what it pins is what the gate makes of a post the url-safety guard refused or did not follow.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// syncAgent builds an Agent whose confinement posture is the one under test. The MODE is
// deliberately Plan on every row: a user's sync reaction fires in every mode, so a test that only
// ever ran in Auto would pass against a mode-gated permit row too.
func syncAgent(t *testing.T, sink domain.EventSink, conf domain.Confiner, confine bool) *Agent {
	t.Helper()

	cfg := baseConfig(sink)
	cfg.Mode = domain.ModePlan
	cfg.WorkspaceDir = t.TempDir()
	cfg.Confiner = conf
	cfg.ConfineToWorkspace = confine

	a, err := newAgent(cfg, echoResponder(t, "reply"))
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

// redirectingGateEndpoint is a gate webhook endpoint that answers every POST with a 307 to a second
// server, which would answer "deny" were it ever reached; it returns the endpoint and the target's
// hit counter.
func redirectingGateEndpoint(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	targetHits := new(atomic.Int64)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		_, _ = w.Write([]byte("deny\nfollowed the redirect\n"))
	}))
	t.Cleanup(target.Close)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/elsewhere", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirector.Close)
	return redirector, targetHits
}

// TestSyncWebhookRedirectIsAFailedPost pins the sync lane onto the guarded client: a gate webhook
// whose endpoint redirects gets the 3xx back as its reply, never followed, so the gate treats it as
// any other failed post — it escalates to ask naming the status — and the host the redirect named
// never hears from apogee (its "deny" would otherwise have refused the call outright).
func TestSyncWebhookRedirectIsAFailedPost(t *testing.T) {
	t.Parallel()

	redirector, targetHits := redirectingGateEndpoint(t)
	sink := &recordingSink{}
	approver := &gateApprover{decision: domain.ApprovalDeny}
	a := gateAgent(t, sink, approver, nil, userGateWebhook("warden", domain.WebhookHandler{URL: redirector.URL}))

	prepareAndRun(a, readCallOnly())

	if len(approver.requests) != 1 {
		t.Fatalf("the Approver was consulted %d times, want 1 — a redirect is a failed post, which asks",
			len(approver.requests))
	}
	if reason := approver.requests[0].Reason; !strings.Contains(reason, "HTTP 307") {
		t.Errorf("Approval reason = %q, want it to name the redirect as %q", reason, "HTTP 307")
	}
	if targetHits.Load() != 0 {
		t.Errorf("the redirect target was called %d times; it must never be reached", targetHits.Load())
	}
}

// TestSyncWebhookDenyListedEndpointIsNeverPosted pins that the sync lane's guard is built from the
// engine config's url-safety lists: a gate webhook whose host `deny-hosts` names is refused before
// anything is dialled — the gate escalates to ask naming the url-safety refusal, and the endpoint,
// which would have answered "allow", sees no request.
func TestSyncWebhookDenyListedEndpointIsNeverPosted(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	server := gateEndpoint(t, &hits, nil, http.StatusOK, "allow")
	sink := &recordingSink{}
	approver := &gateApprover{decision: domain.ApprovalDeny}
	a := gateAgent(t, sink, approver, nil, userGateWebhook("warden", domain.WebhookHandler{URL: server.URL}))
	a.cfg.URLDenyHosts = []string{"127.0.0.1"}

	prepareAndRun(a, readCallOnly())

	if len(approver.requests) != 1 {
		t.Fatalf("the Approver was consulted %d times, want 1 — a refused post is a failed post, which asks",
			len(approver.requests))
	}
	if reason := approver.requests[0].Reason; !strings.Contains(reason, "refused by url-safety") {
		t.Errorf("Approval reason = %q, want it to name the url-safety refusal", reason)
	}
	if hits.Load() != 0 {
		t.Errorf("the deny-listed endpoint was called %d times, want 0", hits.Load())
	}
}

// fakePin is a domain.ProjectPin the repo-run tests state outright: the root, the pinned programs,
// what its check reports as changed since adoption, and whether that check could be made at all.
type fakePin struct {
	root     string
	programs []string
	changed  []string
	err      error
}

func (p fakePin) Root() string               { return p.root }
func (p fakePin) Changed() ([]string, error) { return p.changed, p.err }
func (p fakePin) Programs() []string         { return p.programs }

// repoReaction is an adopted repo reaction over the script at root/scripts/fmt.sh, written with
// body, whose pin reports changed. The argv is the relative spelling a repo writes.
func repoReaction(t *testing.T, root string, class domain.Class, body string, changed ...string) domain.Reaction {
	t.Helper()

	script := filepath.Join(root, "scripts", "fmt.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write the script: %v", err)
	}
	r := syncReaction(class, 0, "./scripts/fmt.sh")
	r.Project = true
	r.Pin = fakePin{root: root, programs: []string{script}, changed: changed}
	return r
}

// TestRepoRunRunsInsideTheExecFence pins ADR 0104 §8 where runner and permit meet: with
// confine-to-workspace on, an adopted repo reaction's command — its pinned script inside the
// workspace, named relatively — runs on either lane under the permit's box, in the Project root,
// with the tools' secret scrub. The observe lane reaches it through the runner the Agent binds.
func TestRepoRunRunsInsideTheExecFence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the executor it pins is platform-independent")
	}
	t.Setenv("REPO_RUN_SECRET", "leaked")

	conf := &fakeConfiner{caps: capsBoth()}
	a := syncAgent(t, &recordingSink{}, conf, true)
	a.cfg.SecretEnvVars = []string{"REPO_RUN_SECRET"}
	root := a.cfg.WorkspaceDir
	r := repoReaction(t, root, domain.ClassGate, `printf '%s|%s' "$(pwd -P)" "${REPO_RUN_SECRET:-scrubbed}"`)
	var bound domain.ProjectRun
	a.cfg.BindProjectRun = func(run domain.ProjectRun) { bound = run }

	out, err := a.runSyncArgv(context.Background(), 1, r, domain.SeamPayload{Event: domain.MomentPreToolExec})
	a.bindProjectRun()
	observeErr := bound(context.Background(), r, "{}", nil)

	if err != nil {
		t.Fatalf("runSyncArgv: %v", err)
	}
	realRoot, _ := filepath.EvalSymlinks(root)
	if want := realRoot + "|scrubbed"; out != want {
		t.Errorf("stdout = %q, want %q — the Project root as working directory, the secret scrubbed", out, want)
	}
	if observeErr != nil {
		t.Errorf("bound observe runner: %v", observeErr)
	}
	if conf.confineCount() != 2 {
		t.Errorf("Confine called %d times, want 2 — both lanes run inside the permit's box", conf.confineCount())
	}
	if box := conf.lastConfinedBox(); box.WorkspaceRoot != root {
		t.Errorf("box.WorkspaceRoot = %q, want %q", box.WorkspaceRoot, root)
	}
}

// TestRepoRunFailsWithoutConfinement pins the permit row's refusal for a repo command: the operator
// asked for the fence and the host cannot build one, so the observe lane's runner spawns nothing,
// exactly as an `advise:` or `gate:` command would not. The canary is the file it would write.
func TestRepoRunFailsWithoutConfinement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the executor it pins is platform-independent")
	}
	t.Parallel()

	a := syncAgent(t, &recordingSink{}, &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: false}}, true)
	canary := filepath.Join(t.TempDir(), "spawned")
	r := repoReaction(t, a.cfg.WorkspaceDir, domain.ClassObserve, "touch "+canary)

	err := a.RunProjectReaction(context.Background(), r, "{}", nil)

	if !errors.Is(err, errConfinementUnavailable) {
		t.Errorf("err = %v, want errConfinementUnavailable", err)
	}
	if _, statErr := os.Stat(canary); statErr == nil {
		t.Error("the command ran; a refused permit must spawn nothing at all")
	}
}

// TestRepoRunRefusesAChangedScript pins the spawn-time pin check (ADR 0104 §4): a repo reaction
// whose pinned script moved since adoption, or that carries no pin at all, is refused before
// anything is confined or spawned, and the refusal names the file.
func TestRepoRunRefusesAChangedScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the executor it pins is platform-independent")
	}
	t.Parallel()

	tests := []struct {
		name string
		edit func(*domain.Reaction)
		want string
	}{
		{name: "changed script", edit: func(*domain.Reaction) {}, want: "scripts/fmt.sh changed since you adopted it"},
		{name: "no pin", edit: func(r *domain.Reaction) { r.Pin = nil }, want: "carries no adoption pin"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conf := &fakeConfiner{caps: capsBoth()}
			a := syncAgent(t, &recordingSink{}, conf, true)
			canary := filepath.Join(t.TempDir(), "spawned")
			r := repoReaction(t, a.cfg.WorkspaceDir, domain.ClassGate, "touch "+canary, "scripts/fmt.sh")
			tc.edit(&r)

			out, err := a.runSyncArgv(context.Background(), 1, r, domain.SeamPayload{Event: domain.MomentPreToolExec})

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v (out = %q), want it to say %q", err, out, tc.want)
			}
			if _, statErr := os.Stat(canary); statErr == nil {
				t.Error("the command ran; a refused pin must spawn nothing")
			}
			if conf.confineCount() != 0 {
				t.Errorf("Confine called %d times, want 0", conf.confineCount())
			}
		})
	}
}

// TestRepoRunRefusalPokesPinMoved pins the re-ask cue (Config.PinMoved): every spawn the pin check
// refuses because the pinned files moved, or could not be checked, tells the Driver once — on
// either lane — so a pane can ask again; a reaction carrying no pin moved no file and cues nothing.
func TestRepoRunRefusalPokesPinMoved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the executor it pins is platform-independent")
	}
	t.Parallel()

	tests := []struct {
		name string
		edit func(*domain.Reaction)
		want int
	}{
		{name: "changed script", edit: func(*domain.Reaction) {}, want: 2},
		{name: "unreadable pin", edit: func(r *domain.Reaction) {
			r.Pin = fakePin{root: r.Pin.Root(), err: errors.New("scripts/fmt.sh: permission denied")}
		}, want: 2},
		{name: "no pin", edit: func(r *domain.Reaction) { r.Pin = nil }, want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := syncAgent(t, &recordingSink{}, &fakeConfiner{caps: capsBoth()}, true)
			pokes := 0
			a.cfg.PinMoved = func() { pokes++ }
			canary := filepath.Join(t.TempDir(), "spawned")
			r := repoReaction(t, a.cfg.WorkspaceDir, domain.ClassGate, "touch "+canary, "scripts/fmt.sh")
			tc.edit(&r)

			_, syncErr := a.runSyncArgv(context.Background(), 1, r, domain.SeamPayload{Event: domain.MomentPreToolExec})
			observeErr := a.RunProjectReaction(context.Background(), r, "{}", nil)

			if syncErr == nil || observeErr == nil {
				t.Fatalf("sync err = %v, observe err = %v; want both lanes refused", syncErr, observeErr)
			}
			if pokes != tc.want {
				t.Errorf("PinMoved called %d times, want %d — once per refused spawn of a moved pin", pokes, tc.want)
			}
			if _, statErr := os.Stat(canary); statErr == nil {
				t.Error("the command ran; a refused pin must spawn nothing")
			}
		})
	}
}

// TestRepoRunRunnerIsBoundByNew pins the late bind a Driver's Runner reaches the Agent's fenced
// runner through (Config.BindProjectRun): New hands it over once the Agent is built, and the runner
// it hands over is that Agent's — a repo command fired through it is refused by THIS Agent's permit
// row when the host cannot confine.
func TestRepoRunRunnerIsBoundByNew(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.Endpoint = "http://127.0.0.1:1/v1"
	cfg.WorkspaceDir = t.TempDir()
	cfg.Confiner = &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: false}}
	cfg.ConfineToWorkspace = true
	var bound domain.ProjectRun
	cfg.BindProjectRun = func(run domain.ProjectRun) { bound = run }

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	if bound == nil {
		t.Fatal("New bound no runner")
	}
	r := syncReaction(domain.ClassObserve, 0, "true")
	r.Project = true
	r.Pin = fakePin{root: cfg.WorkspaceDir}
	if err := bound(context.Background(), r, "{}", nil); !errors.Is(err, errConfinementUnavailable) {
		t.Errorf("bound runner err = %v, want this Agent's errConfinementUnavailable", err)
	}
}

// TestSyncLaneReadsTheReloadedScrub pins the sync lane on the live credential scrub (ADR 0104
// §12): a Config carrying a SecretEnv holder is read at each spawn, so a name the host widens the
// holder by mid-session — a `headers-env:` token an adopted Project config entry introduced — is
// scrubbed from the very next command's environment with no rebuild of the Agent, and the advise
// lane's redaction reads that same holder.
func TestSyncLaneReadsTheReloadedScrub(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the executor it pins is platform-independent")
	}
	// No t.Parallel: t.Setenv.
	t.Setenv("APOGEE_TEST_RELOADED_SECRET", "hunter2-adopted")

	t.Run("spawn", func(t *testing.T) {
		a := syncAgent(t, &recordingSink{}, nil, false)
		holder := domain.NewSecretEnv(nil)
		a.cfg.SecretEnv = holder
		r := syncReaction(domain.ClassGate, 0, "/bin/sh", "-c",
			`printf '%s' "${APOGEE_TEST_RELOADED_SECRET:-scrubbed}"`)
		payload := domain.SeamPayload{Event: domain.MomentPreToolExec}

		before, errBefore := a.runSyncArgv(context.Background(), 1, r, payload)
		holder.Widen([]string{"APOGEE_TEST_RELOADED_SECRET"})
		after, errAfter := a.runSyncArgv(context.Background(), 1, r, payload)

		if errBefore != nil || errAfter != nil {
			t.Fatalf("runSyncArgv: before %v, after %v", errBefore, errAfter)
		}
		if before != "hunter2-adopted" {
			t.Errorf("stdout before the widening = %q; nothing named the variable yet", before)
		}
		if after != "scrubbed" {
			t.Errorf("stdout after the widening = %q, want %q — the spawn reads the holder", after, "scrubbed")
		}
	})

	t.Run("advise redaction", func(t *testing.T) {
		var reported []string
		a := adviseArgvAgent(t, &recordingSink{}, t.TempDir(), false, &reported,
			userAdvise("coach", []domain.Moment{domain.MomentPostToolResult},
				"/bin/sh", "-c", `printf '%s\n' 'token=hunter2-adopted'`))
		holder := domain.NewSecretEnv(nil)
		a.cfg.SecretEnv = holder
		holder.Widen([]string{"APOGEE_TEST_RELOADED_SECRET"})

		call, result := readCall()
		msg := adviseArgvCall(t, a, call, result)

		if !strings.Contains(msg.Content, "token=[redacted]") {
			t.Errorf("advised content = %q, want the widened name's value redacted", msg.Content)
		}
	})
}
