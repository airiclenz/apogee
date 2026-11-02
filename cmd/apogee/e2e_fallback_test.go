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
	"net"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
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
