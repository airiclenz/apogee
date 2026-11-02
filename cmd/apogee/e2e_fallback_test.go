package main

// Server fallback end to end (ADR 0105): a real `apogee headless` bound to a `servers:` entry whose
// `fallback:` names a second stubllm server. The primary answers the run's start-up beat and then
// dies under the run's first request — the stub is closed while that request is held at an `await:`
// gate, and the held reply is a cut stream — so the grace window's beats are refused, the window
// runs out, and the seat moves to the fallback, which answers the resent request. `re-stream-budget:
// 0` sends the cut straight to the resolver rather than through the re-stream hold-off ladder, and
// the grace window is a fixed second.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/testtime"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// fallbackGraceForTests is the `fallback-after:` the e2e runs wait out: what they claim is that the
// window ends and the seat moves, not how long the window is.
var fallbackGraceForTests = testtime.Fixed(time.Second)

// fallbackRunBudget bounds one whole headless run, the grace window and the walk included.
var fallbackRunBudget = testtime.Budget(2 * time.Minute)

// fallbackDieGate is the `await:` label the primary's fatal reply is held at.
const fallbackDieGate = "primary-dies"

// fallbackAnswer is what the fallback server answers the resent request with.
const fallbackAnswer = "Finished on the backup."

// headlessResult is what one headless run left behind.
type headlessResult struct {
	stdout, stderr string
	err            error
}

// dyingPrimary is a primary whose first request is held, so the test can close the server under
// it, and whose reply — once released — is a cut stream.
func dyingPrimary(t *testing.T) *stubllm.Server {
	t.Helper()
	return stubllm.New(t, stubllm.Script{Model: "primary-model", Turns: []stubllm.Turn{{
		Await: fallbackDieGate,
		Text:  "The primary never finishes this.",
		Cut:   &stubllm.Cut{AfterRunes: 3},
	}}})
}

// backupServer is the fallback: it answers whatever reaches it.
func backupServer(t *testing.T) *stubllm.Server {
	t.Helper()
	return stubllm.New(t, stubllm.Script{Model: "backup-model", Turns: []stubllm.Turn{{Text: fallbackAnswer}}})
}

// fallbackHome writes an apogee home whose startup entry is primary, falling back to backup.
func fallbackHome(t *testing.T, primary, backup *stubllm.Server) string {
	t.Helper()
	home := t.TempDir()
	writeFallbackConfig(t, home, primary, backup)
	return home
}

// writeFallbackConfig writes home's config.yaml: the startup entry `primary` at primary's endpoint,
// falling back to `backup` at backup's. Rewriting it over a home that already holds records is how
// a test points the same entry names at fresh servers.
func writeFallbackConfig(t *testing.T, home string, primary, backup *stubllm.Server) {
	t.Helper()
	writeConfigHome(t, home,
		"context-window: 32768\n"+
			"re-stream-budget: 0\n"+
			"servers:\n"+
			"  - name: primary\n"+
			"    endpoint: "+primary.URL+"\n"+
			"    model: "+primary.Model+"\n"+
			"    fallback: [backup]\n"+
			"    fallback-after: "+fallbackGraceForTests.String()+"\n"+
			"  - name: backup\n"+
			"    endpoint: "+backup.URL+"\n"+
			"    model: "+backup.Model+"\n"+
			"server: primary\n")
}

// startHeadless runs `apogee headless` against home on its own goroutine, with the real runner.
func startHeadless(t *testing.T, home string, args ...string) <-chan headlessResult {
	t.Helper()
	assertNoAmbientApogeeConfig(t)
	t.Setenv(config.EnvMode, "")
	workspace := e2eWorkspace(t)

	done := make(chan headlessResult, 1)
	go func() {
		cmd := newHeadlessCommandWith(headlessDeps{runner: run.Once})
		var outBuf, errBuf bytes.Buffer
		cmd.SetOut(&outBuf)
		cmd.SetErr(&errBuf)
		cmd.SetIn(strings.NewReader(""))
		cmd.SetArgs(append([]string{"--config", home, "--workspace", workspace}, append(args, upstreamPrompt)...))
		err := cmd.ExecuteContext(context.Background())
		done <- headlessResult{stdout: outBuf.String(), stderr: errBuf.String(), err: err}
	}()
	return done
}

// awaitHeadless waits for the run to end.
func awaitHeadless(t *testing.T, done <-chan headlessResult) headlessResult {
	t.Helper()
	select {
	case res := <-done:
		return res
	case <-time.After(fallbackRunBudget):
		t.Fatalf("headless did not finish within %s", fallbackRunBudget)
		return headlessResult{}
	}
}

// killHeldPrimary closes primary while its first request is held at the gate, waits until its
// port refuses a connection, and only then releases the held reply — so every beat the grace
// window takes finds the server gone.
func killHeldPrimary(t *testing.T, primary *stubllm.Server) {
	t.Helper()
	budget := testtime.Budget(30 * time.Second)
	testtime.Eventually(t, budget, func() bool { return len(primary.Requests()) >= 1 },
		"the primary to receive the run's first request")
	closed := make(chan struct{})
	go func() {
		primary.Close()
		close(closed)
	}()
	address := hostOf(t, primary.URL)
	testtime.Eventually(t, budget, func() bool {
		conn, err := net.Dial("tcp", address)
		if err != nil {
			return true
		}
		_ = conn.Close()
		return false
	}, "the primary's port to refuse connections")
	primary.Release(fallbackDieGate)
	select {
	case <-closed:
	case <-time.After(budget):
		t.Fatalf("the primary did not finish closing within %s", budget)
	}
}

func hostOf(t *testing.T, endpoint string) string {
	t.Helper()
	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("parse %q: %v", endpoint, err)
	}
	return parsed.Host
}

// switchedLines counts the stderr lines that announce a switch to the backup.
func switchedLines(stderr string) int {
	count := 0
	for _, line := range strings.Split(stderr, "\n") {
		if strings.Contains(line, "continuing on backup (backup-model)") {
			count++
		}
	}
	return count
}

// TestHeadlessFallbackFinishesOnTheFallback: the primary dies mid-run, the grace window runs out
// with the primary still gone, and the run finishes on the fallback with exit 0 — its answer on
// stdout and the switch said once on stderr.
func TestHeadlessFallbackFinishesOnTheFallback(t *testing.T) {
	primary, backup := dyingPrimary(t), backupServer(t)
	done := startHeadless(t, fallbackHome(t, primary, backup))

	killHeldPrimary(t, primary)
	res := awaitHeadless(t, done)

	if res.err != nil {
		t.Fatalf("headless: %v (exit %d)\n%s", res.err, exitCodeFor(res.err), res.stderr)
	}
	if got := strings.TrimRight(res.stdout, "\n"); got != fallbackAnswer {
		t.Errorf("stdout = %q, want the fallback's answer", got)
	}
	if got := switchedLines(res.stderr); got != 1 {
		t.Errorf("stderr carries %d switch lines, want 1:\n%s", got, res.stderr)
	}
	if got := len(backup.Requests()); got != 1 {
		t.Errorf("the fallback saw %d requests, want the one resend", got)
	}
}

// TestHeadlessFallbackJSONCarriesOneSwitchedLine: the same run under `--format json` puts exactly
// one upstream_switched Event line on stdout and no switch sentence on stderr.
func TestHeadlessFallbackJSONCarriesOneSwitchedLine(t *testing.T) {
	primary, backup := dyingPrimary(t), backupServer(t)
	done := startHeadless(t, fallbackHome(t, primary, backup), "--format", "json")

	killHeldPrimary(t, primary)
	res := awaitHeadless(t, done)

	if res.err != nil {
		t.Fatalf("headless: %v (exit %d)\n%s", res.err, exitCodeFor(res.err), res.stderr)
	}
	switched := 0
	for _, line := range strings.Split(strings.TrimSpace(res.stdout), "\n") {
		var envelope struct {
			Event string `json:"event"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Fatalf("stdout line %q is not JSON: %v", line, err)
		}
		if envelope.Event == "upstream_switched" {
			switched++
		}
	}
	if switched != 1 {
		t.Errorf("stdout carries %d upstream_switched lines, want 1:\n%s", switched, res.stdout)
	}
	if got := switchedLines(res.stderr); got != 0 {
		t.Errorf("stderr carries %d switch sentences under json, want 0", got)
	}
}

// TestHeadlessFallbackAllDownExitsFaulted: with the fallback gone too, the walk lands nowhere and
// the run is abandoned exactly as it would be without a fallback — exit 3, no switch announced.
func TestHeadlessFallbackAllDownExitsFaulted(t *testing.T) {
	primary, backup := dyingPrimary(t), backupServer(t)
	backup.Close()
	done := startHeadless(t, fallbackHome(t, primary, backup))

	killHeldPrimary(t, primary)
	res := awaitHeadless(t, done)

	if code := exitCodeFor(res.err); res.err == nil || code != exitRunFaulted {
		t.Fatalf("headless err = %v (exit %d), want the abandoned run's exit %d\n%s",
			res.err, code, exitRunFaulted, res.stderr)
	}
	if got := switchedLines(res.stderr); got != 0 {
		t.Errorf("stderr carries %d switch lines, want 0", got)
	}
}

// TestHeadlessFallbackPrimaryBackGetsTheResend: a primary whose reply is cut but whose discovery
// still answers is recovered on the grace window's first beat — the request is resent on it, the
// fallback is never asked, and nothing is announced.
func TestHeadlessFallbackPrimaryBackGetsTheResend(t *testing.T) {
	primary := stubllm.New(t, stubllm.Script{Model: "primary-model", Turns: []stubllm.Turn{
		{Text: "Cut short.", Cut: &stubllm.Cut{AfterRunes: 3}},
		{Text: "Answered on the primary."},
	}})
	backup := backupServer(t)
	done := startHeadless(t, fallbackHome(t, primary, backup))

	res := awaitHeadless(t, done)

	if res.err != nil {
		t.Fatalf("headless: %v (exit %d)\n%s", res.err, exitCodeFor(res.err), res.stderr)
	}
	primary.AssertConsumed(t)
	if got := strings.TrimRight(res.stdout, "\n"); got != "Answered on the primary." {
		t.Errorf("stdout = %q, want the primary's resent answer", got)
	}
	if got := len(backup.Requests()); got != 0 {
		t.Errorf("the fallback saw %d requests, want none", got)
	}
	if got := switchedLines(res.stderr); got != 0 {
		t.Errorf("stderr carries %d switch lines, want 0", got)
	}
}

// TestHeadlessFallbackUnknownNameIsRefused: a `fallback:` naming no entry is a configuration
// error, refused before the run starts — exit 2, nothing sent.
func TestHeadlessFallbackUnknownNameIsRefused(t *testing.T) {
	primary := stubllm.New(t, stubllm.Script{Model: "primary-model", Turns: []stubllm.Turn{{Text: "unused"}}})
	home := t.TempDir()
	writeConfigHome(t, home,
		"servers:\n"+
			"  - name: primary\n"+
			"    endpoint: "+primary.URL+"\n"+
			"    model: "+primary.Model+"\n"+
			"    fallback: [nowhere]\n"+
			"server: primary\n")

	res := awaitHeadless(t, startHeadless(t, home))

	if code := exitCodeFor(res.err); res.err == nil || code != exitNotStarted {
		t.Fatalf("headless err = %v (exit %d), want the refusal's exit %d", res.err, code, exitNotStarted)
	}
	if !strings.Contains(res.err.Error(), "nowhere") {
		t.Errorf("refusal %q does not name the unknown fallback", res.err)
	}
	if got := len(primary.Requests()); got != 0 {
		t.Errorf("the primary saw %d requests, want none", got)
	}
}

// TestHeadlessFallbackFansOutAtTheFallbackWidth: the primary pins a fan-out width of 1 and the
// fallback a width of 3, under one fan-out round. The primary dies under the run's first request,
// the seat falls over, and the fallback answers the resend with three delegations in one reply. The
// run fans out at the fallback's width, so its ceiling is 3 and all three children reach the
// fallback; at the dead primary's width the ceiling would be 1 and two of them would be refused
// before they started.
func TestHeadlessFallbackFansOutAtTheFallbackWidth(t *testing.T) {
	const wrapUp = "The delegates reported."
	tasks := []string{"Survey the docs", "Survey the tests", "Survey the tools"}
	calls := make([]stubllm.ToolCall, 0, len(tasks))
	for i, task := range tasks {
		calls = append(calls, stubllm.ToolCall{
			Name: "sub_agent", Arguments: `{"task":"` + task + `","name":"survey ` + strconv.Itoa(i) + `"}`,
		})
	}
	primary := dyingPrimary(t)
	backup := stubllm.New(t, stubllm.Script{Model: "backup-model", Turns: []stubllm.Turn{
		{When: &stubllm.Match{ToolResult: "sub_agent"}, Repeat: true, Text: wrapUp},
		{When: &stubllm.Match{LastMessage: "^" + upstreamPrompt}, Repeat: true, ToolCalls: calls},
		{Repeat: true, Text: "Surveyed."},
	}})
	home := t.TempDir()
	writeConfigHome(t, home,
		"context-window: 32768\n"+
			"re-stream-budget: 0\n"+
			"delegate-fanout-rounds: 1\n"+
			"servers:\n"+
			"  - name: primary\n"+
			"    endpoint: "+primary.URL+"\n"+
			"    model: "+primary.Model+"\n"+
			"    parallel-agents: 1\n"+
			"    fallback: [backup]\n"+
			"    fallback-after: "+fallbackGraceForTests.String()+"\n"+
			"  - name: backup\n"+
			"    endpoint: "+backup.URL+"\n"+
			"    model: "+backup.Model+"\n"+
			"    parallel-agents: 3\n"+
			"server: primary\n")

	done := startHeadless(t, home)
	killHeldPrimary(t, primary)
	res := awaitHeadless(t, done)

	if res.err != nil {
		t.Fatalf("headless: %v (exit %d)\n%s", res.err, exitCodeFor(res.err), res.stderr)
	}
	if got := strings.TrimRight(res.stdout, "\n"); got != wrapUp {
		t.Errorf("stdout = %q, want the wrap-up after the delegations reported", got)
	}
	for _, task := range tasks {
		if got := childRequestsFor(backup, task); got != 1 {
			t.Errorf("the fallback saw %d requests for %q, want 1 — the fan-out ceiling refused it at the "+
				"dead primary's width (log %v)", got, task, backup.Requests())
		}
	}
}

// childRequestsFor counts the requests up answered whose last message is task itself: a child's
// request on its brief. The parent's own requests never qualify, since a refusal or a report that
// names the task carries more than the task.
func childRequestsFor(up *stubllm.Server, task string) int {
	n := 0
	for _, r := range up.Requests() {
		if len(r.Messages) > 0 && strings.TrimSpace(r.Messages[len(r.Messages)-1].Content) == task {
			n++
		}
	}
	return n
}

// fallbackResumePrompt and fallbackResumeAnswer are the one Exchange a resumed fallen-over session runs, on the
// primary the config names.
const (
	fallbackResumePrompt = "Carry on from where the backup left off."
	fallbackResumeAnswer = "Picked up on the primary again."
)

// TestResumeAfterFallbackDialsThePrimary: a run that fell over to the backup is saved naming the
// backup's model — the model it finished on — yet resuming it starts on the configured primary
// (ADR 0028 D5): the fallover is the run's, never the record's server. The primary that died is
// replaced by a fresh one under the same entry name, and the resumed session's prompt goes to it
// while the backup sees nothing past the run's one resend.
func TestResumeAfterFallbackDialsThePrimary(t *testing.T) {
	primary, backup := dyingPrimary(t), backupServer(t)
	home := fallbackHome(t, primary, backup)
	done := startHeadless(t, home)

	killHeldPrimary(t, primary)
	if res := awaitHeadless(t, done); res.err != nil {
		t.Fatalf("headless: %v (exit %d)\n%s", res.err, exitCodeFor(res.err), res.stderr)
	}
	metas, err := session.NewStore(filepath.Join(home, sessionsDirName)).List()
	if err != nil || len(metas) != 1 {
		t.Fatalf("the sessions dir lists %d records (%v), want the fallen-over run's one", len(metas), err)
	}
	if got := metas[0].Model; got != backup.Model {
		t.Errorf("the record names model %q, want the fallback's %q", got, backup.Model)
	}

	revived := stubllm.New(t, stubllm.Script{Model: primary.Model, Turns: []stubllm.Turn{
		{When: &stubllm.Match{LastMessage: "^" + fallbackResumePrompt}, Repeat: true, Text: fallbackResumeAnswer},
		// Anything else — a title or a Mechanism's own call — so the script never runs dry.
		{Repeat: true, Text: "Nothing else to add."},
	}})
	writeFallbackConfig(t, home, revived, backup)

	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, revived, home, "", "--resume", metas[0].ID)
	waitIdle(drv)
	submit(drv, fallbackResumePrompt)
	drv.WaitText(fallbackResumeAnswer)
	waitIdle(drv)

	if !slices.ContainsFunc(revived.Requests(), func(r stubllm.Request) bool {
		return strings.Contains(string(r.Body), fallbackResumePrompt)
	}) {
		t.Errorf("the revived primary never saw the resumed prompt; it saw %d requests", len(revived.Requests()))
	}
	if got := len(backup.Requests()); got != 1 {
		t.Errorf("the backup saw %d requests, want only the run's one resend", got)
	}
	if err := sess.Quit(); err != nil {
		t.Fatalf("the resumed session returned %v; want a clean quit", err)
	}
}

// tuiFallbackBeat is the heartbeat the TUI fallback runs beat on: short, so the claim about the
// beats that follow a switch costs seconds rather than minutes.
var tuiFallbackBeat = testtime.Fixed(200 * time.Millisecond)

// launchFallbackTUI starts a driven TUI session on home, bound to its `server:` entry, and returns
// once the session is idle with model bound.
func launchFallbackTUI(t *testing.T, stub *stubllm.Server, home, model string) (*e2eSession, *tuitest.Driver) {
	t.Helper()
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOnBeating(t, drv, stub, home, "", tuiFallbackBeat)
	waitIdle(drv)
	drv.WaitText(model)
	return sess, drv
}

// TestTUIFallbackContinuesTheTurnOnTheFallback: the primary dies under the session's first request,
// the grace window runs out, and the Turn continues on the fallback — one note naming both servers
// and both models, the fallback's reply on screen, beats off the fallback that note nothing more,
// and `/server primary` afterwards a move back rather than "already on".
func TestTUIFallbackContinuesTheTurnOnTheFallback(t *testing.T) {
	primary, backup := dyingPrimary(t), backupServer(t)
	sess, drv := launchFallbackTUI(t, primary, fallbackHome(t, primary, backup), primary.Model)

	submit(drv, upstreamPrompt)
	killHeldPrimary(t, primary)

	awaitNotice(t, drv, "server fallback: primary (primary-model) failed", tuitest.Within(fallbackRunBudget))
	awaitNotice(t, drv, "continuing on backup (backup-model)")
	awaitNotice(t, drv, fallbackAnswer)
	waitIdle(drv)
	if got := len(backup.Requests()); got != 1 {
		t.Errorf("the fallback saw %d requests, want the one resend", got)
	}
	probes := len(backup.Probes())
	testtime.Eventually(t, fallbackRunBudget, func() bool { return len(backup.Probes()) >= probes+2 },
		"two heartbeats off the fallback")
	frame := flatten(drv.Frame().String())
	for _, unwanted := range []string{"model changed", "connected:"} {
		if strings.Contains(frame, unwanted) {
			t.Errorf("the frame carries %q after the switch — the fallback's beats are no change:\n%s",
				unwanted, drv.Frame())
		}
	}

	submit(drv, "/server primary")

	awaitNotice(t, drv, "switching server: backup → primary")
	if strings.Contains(flatten(drv.Frame().String()), "already on primary") {
		t.Errorf("`/server primary` answered already-on:\n%s", drv.Frame())
	}
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestTUIFallbackOnAnEntryWithoutFallbackIsAbandoned: after `/server plain`, an entry with no
// `fallback:`, a 503 from plain abandons the Turn as it always has — no switch, nothing sent to the
// primary's fallback.
func TestTUIFallbackOnAnEntryWithoutFallbackIsAbandoned(t *testing.T) {
	primary := stubllm.New(t, stubllm.Script{Model: "primary-model", Turns: []stubllm.Turn{{Text: "unused"}}})
	backup := backupServer(t)
	// Every request plain is sent is answered 503, a fault a fallback would take.
	plain := stubllm.New(t, stubllm.Script{Model: "plain-model", Turns: []stubllm.Turn{{
		Repeat: true, HTTP: &stubllm.HTTPReply{Status: 503, Body: "plain is overloaded"},
	}}})
	home := t.TempDir()
	writeConfigHome(t, home,
		"context-window: 32768\n"+
			"re-stream-budget: 0\n"+
			"servers:\n"+
			"  - name: primary\n"+
			"    endpoint: "+primary.URL+"\n"+
			"    model: "+primary.Model+"\n"+
			"    fallback: [backup]\n"+
			"    fallback-after: "+fallbackGraceForTests.String()+"\n"+
			"  - name: backup\n"+
			"    endpoint: "+backup.URL+"\n"+
			"    model: "+backup.Model+"\n"+
			"  - name: plain\n"+
			"    endpoint: "+plain.URL+"\n"+
			"    model: "+plain.Model+"\n"+
			"server: primary\n")
	sess, drv := launchFallbackTUI(t, primary, home, primary.Model)
	submit(drv, "/server plain")
	awaitNotice(t, drv, "connected: "+plain.Model)

	submit(drv, upstreamPrompt)

	awaitNotice(t, drv, "upstream HTTP 503", tuitest.Within(fallbackRunBudget))
	waitIdle(drv)
	if got := len(backup.Requests()); got != 0 {
		t.Errorf("the primary's fallback saw %d requests, want none — plain names no fallback", got)
	}
	if strings.Contains(flatten(drv.Frame().String()), "server fallback") {
		t.Errorf("the frame announces a fallback for an entry that names none:\n%s", drv.Frame())
	}
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestHeadlessRoutedFallback: a headless run whose `sub-agents-server:` entry names a `fallback:`
// routes its delegation to that entry; the Sub-agent server dies under the child's request, the
// grace window runs out, and the routed child continues on the entry's own fallback — the session
// seat, which names none, never moves — so the delegation reports and the run finishes with exit 0,
// the switch said once on stderr.
func TestHeadlessRoutedFallback(t *testing.T) {
	session := stubllm.New(t, stubllm.Script{Model: "session-model", Turns: []stubllm.Turn{
		{When: &stubllm.Match{ToolResult: "sub_agent"}, Repeat: true, Text: "The delegate reported."},
		{When: &stubllm.Match{LastMessage: "^" + upstreamPrompt}, Repeat: true, ToolCalls: []stubllm.ToolCall{{
			Name: "sub_agent", Arguments: `{"task":"Survey the workspace","name":"grunt work"}`,
		}}},
	}})
	grunt := dyingPrimary(t)
	spare := backupServer(t)
	home := t.TempDir()
	writeConfigHome(t, home,
		"context-window: 32768\n"+
			"re-stream-budget: 0\n"+
			"sub-agents-server: grunt\n"+
			"servers:\n"+
			"  - name: session\n"+
			"    endpoint: "+session.URL+"\n"+
			"    model: "+session.Model+"\n"+
			"  - name: grunt\n"+
			"    endpoint: "+grunt.URL+"\n"+
			"    model: "+grunt.Model+"\n"+
			"    fallback: [backup]\n"+
			"    fallback-after: "+fallbackGraceForTests.String()+"\n"+
			"  - name: backup\n"+
			"    endpoint: "+spare.URL+"\n"+
			"    model: "+spare.Model+"\n"+
			"server: session\n")

	done := startHeadless(t, home)
	killHeldPrimary(t, grunt)
	res := awaitHeadless(t, done)

	if res.err != nil {
		t.Fatalf("headless: %v (exit %d)\n%s", res.err, exitCodeFor(res.err), res.stderr)
	}
	if got := strings.TrimRight(res.stdout, "\n"); got != "The delegate reported." {
		t.Errorf("stdout = %q, want the session's wrap-up after the delegation reported", got)
	}
	if got := len(spare.Requests()); got != 1 {
		t.Errorf("the grunt's fallback saw %d requests, want the routed child's one", got)
	}
	if got := switchedLines(res.stderr); got != 1 {
		t.Errorf("stderr carries %d switch lines, want 1:\n%s", got, res.stderr)
	}
	if !strings.Contains(res.stderr, "grunt failed") {
		t.Errorf("the switch line does not name the Sub-agent server:\n%s", res.stderr)
	}
}

// fallingGrunt is a Sub-agent server that stays listening for the whole run: it answers discovery
// until its first chat request, cuts that request's stream, and from then on answers 503 to
// everything — so the grace window's beats find it down while a request that still reaches it is
// counted rather than refused at the port.
type fallingGrunt struct {
	server *httptest.Server
	dead   atomic.Bool
	chats  atomic.Int32
}

// newFallingGrunt starts a fallingGrunt advertising model.
func newFallingGrunt(t *testing.T, model string) *fallingGrunt {
	t.Helper()
	g := &fallingGrunt{}
	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions" {
			g.chats.Add(1)
			if g.dead.Swap(true) {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			g.cut(w, model)
			return
		}
		if g.dead.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"`+model+`","object":"model"}]}`)
	}))
	t.Cleanup(g.server.Close)
	return g
}

// cut starts a stream, sends one content delta and drops the connection without an ending.
func (g *fallingGrunt) cut(w http.ResponseWriter, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `data: {"id":"cut","object":"chat.completion.chunk","model":"`+model+
		`","choices":[{"index":0,"delta":{"content":"The"}}]}`+"\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		panic(http.ErrAbortHandler)
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	_ = conn.Close()
}

// requestsCarrying counts the requests up answered whose last message carries text.
func requestsCarrying(up *stubllm.Server, text string) int {
	n := 0
	for _, r := range up.Requests() {
		if len(r.Messages) > 0 && strings.Contains(r.Messages[len(r.Messages)-1].Content, text) {
			n++
		}
	}
	return n
}

// TestHeadlessRoutedFallbackRelatchesLaterSpawns: a headless run delegates twice, one after the
// other, to a `sub-agents-server:` entry naming a `fallback:`. The Sub-agent server fails under the
// first child, the seat falls over, and the run re-latches its Delegation target on the fallback —
// so the second child dials the fallback directly and the dead primary sees no request after the
// first one.
func TestHeadlessRoutedFallbackRelatchesLaterSpawns(t *testing.T) {
	const secondTask = "Survey the tests"
	session := stubllm.New(t, stubllm.Script{Model: "session-model", Turns: []stubllm.Turn{
		{When: &stubllm.Match{ToolResult: "sub_agent"}, ToolCalls: []stubllm.ToolCall{{
			ID: "call_second", Name: "sub_agent", Arguments: `{"task":"` + secondTask + `","name":"second survey"}`,
		}}},
		{When: &stubllm.Match{ToolResult: "sub_agent"}, Repeat: true, Text: "Both delegates reported."},
		{When: &stubllm.Match{LastMessage: "^" + upstreamPrompt}, Repeat: true, ToolCalls: []stubllm.ToolCall{{
			ID: "call_first", Name: "sub_agent", Arguments: `{"task":"Survey the workspace","name":"first survey"}`,
		}}},
	}})
	grunt := newFallingGrunt(t, "grunt-model")
	spare := stubllm.New(t, stubllm.Script{Model: "backup-model", Turns: []stubllm.Turn{
		{Repeat: true, Text: fallbackAnswer},
	}})
	home := t.TempDir()
	writeConfigHome(t, home,
		"context-window: 32768\n"+
			"re-stream-budget: 0\n"+
			"sub-agents-server: grunt\n"+
			"servers:\n"+
			"  - name: session\n"+
			"    endpoint: "+session.URL+"\n"+
			"    model: "+session.Model+"\n"+
			"  - name: grunt\n"+
			"    endpoint: "+grunt.server.URL+"\n"+
			"    model: grunt-model\n"+
			"    fallback: [backup]\n"+
			"    fallback-after: "+fallbackGraceForTests.String()+"\n"+
			"  - name: backup\n"+
			"    endpoint: "+spare.URL+"\n"+
			"    model: "+spare.Model+"\n"+
			"server: session\n")

	res := awaitHeadless(t, startHeadless(t, home))

	if res.err != nil {
		t.Fatalf("headless: %v (exit %d)\n%s", res.err, exitCodeFor(res.err), res.stderr)
	}
	if got := strings.TrimRight(res.stdout, "\n"); got != "Both delegates reported." {
		t.Errorf("stdout = %q, want the session's wrap-up after both delegations reported", got)
	}
	if got := grunt.chats.Load(); got != 1 {
		t.Errorf("the dead Sub-agent server saw %d chat requests, want 1 — a child spawned after the "+
			"fallover dialled it instead of the re-latched fallback", got)
	}
	if got := requestsCarrying(spare, secondTask); got != 1 {
		t.Errorf("the fallback saw %d of the second child's requests, want 1 (log %v)", got, spare.Requests())
	}
	if got := len(spare.Requests()); got != 2 {
		t.Errorf("the fallback saw %d requests, want 2 — the first child's resend and the second child's one",
			got)
	}
}

// TestHeadlessRoutedFallbackNamesLaterSpawnsOnTheFallback: the sibling of
// TestHeadlessRoutedFallbackRelatchesLaterSpawns whose second `sub_agent` call names nothing, so
// the run asks its delegation namer for a name. The namer follows the re-latched target as the
// spawn does: the naming call goes to the fallback, and the dead primary still sees no request
// after the first child's.
func TestHeadlessRoutedFallbackNamesLaterSpawnsOnTheFallback(t *testing.T) {
	const secondTask = "Survey the tests"
	session := stubllm.New(t, stubllm.Script{Model: "session-model", Turns: []stubllm.Turn{
		{When: &stubllm.Match{ToolResult: "sub_agent"}, ToolCalls: []stubllm.ToolCall{{
			ID: "call_second", Name: "sub_agent", Arguments: `{"task":"` + secondTask + `"}`,
		}}},
		{When: &stubllm.Match{ToolResult: "sub_agent"}, Repeat: true, Text: "Both delegates reported."},
		{When: &stubllm.Match{LastMessage: "^" + upstreamPrompt}, Repeat: true, ToolCalls: []stubllm.ToolCall{{
			ID: "call_first", Name: "sub_agent", Arguments: `{"task":"Survey the workspace","name":"first survey"}`,
		}}},
	}})
	grunt := newFallingGrunt(t, "grunt-model")
	spare := stubllm.New(t, stubllm.Script{Model: "backup-model", Turns: []stubllm.Turn{
		{Repeat: true, Text: fallbackAnswer},
	}})
	home := t.TempDir()
	writeConfigHome(t, home,
		"context-window: 32768\n"+
			"re-stream-budget: 0\n"+
			"sub-agents-server: grunt\n"+
			"servers:\n"+
			"  - name: session\n"+
			"    endpoint: "+session.URL+"\n"+
			"    model: "+session.Model+"\n"+
			"  - name: grunt\n"+
			"    endpoint: "+grunt.server.URL+"\n"+
			"    model: grunt-model\n"+
			"    fallback: [backup]\n"+
			"    fallback-after: "+fallbackGraceForTests.String()+"\n"+
			"  - name: backup\n"+
			"    endpoint: "+spare.URL+"\n"+
			"    model: "+spare.Model+"\n"+
			"server: session\n")

	res := awaitHeadless(t, startHeadless(t, home))

	if res.err != nil {
		t.Fatalf("headless: %v (exit %d)\n%s", res.err, exitCodeFor(res.err), res.stderr)
	}
	if got := strings.TrimRight(res.stdout, "\n"); got != "Both delegates reported." {
		t.Errorf("stdout = %q, want the session's wrap-up after both delegations reported", got)
	}
	if got := grunt.chats.Load(); got != 1 {
		t.Errorf("the dead Sub-agent server saw %d chat requests, want 1 — the unnamed second child's "+
			"naming call or its turn dialled it instead of the re-latched fallback", got)
	}
	if got := requestsCarrying(spare, secondTask); got != 2 {
		t.Errorf("the fallback saw %d requests carrying the second child's task, want 2 — its naming call "+
			"and its one turn (log %v)", got, spare.Requests())
	}
}
