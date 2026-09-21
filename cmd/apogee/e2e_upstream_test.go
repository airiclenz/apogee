package main

// The transient-stream seam end to end: a real `apogee headless` against a stubllm server whose
// reply dies mid-stream (testdata/stubllm/upstream-eof.yaml, a `cut` turn that kills the TCP
// connection). A Turn re-streams a transient fault up to its `re-stream-budget` (3 by default, ADR
// 0082) with a doubling hold-off — 1 s, 2 s, 4 s — and the fault after the budget abandons the Turn;
// a body cut by an EOF used to fail the Turn on the spot, when only an in-band 502 was re-streamed.
// Every re-stream below pays the loop's unexported restreamHoldoff ladder — it is production code
// and this package cannot shorten it — so the tests state a small budget in the run's config where
// the default's 7 s of hold-off would buy nothing the ladder's unit tests do not already pin.

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// upstreamPrompt is what the headless run asks; the stub answers by position, so the text is free.
const upstreamPrompt = "Say hello."

// TestE2EUpstreamEOFIsReStreamed: turn 1 is cut three runes in, turn 2 is the whole answer. The
// answer on stdout is turn 2's text — never the three runes streamed before the cut — and the stub
// saw exactly two requests: the cut stream and the re-stream that answered it, under the default
// budget.
func TestE2EUpstreamEOFIsReStreamed(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "upstream-eof"))

	stdout, stderr, err := headlessUpstream(t, stub, "")

	if err != nil {
		t.Fatalf("headless: %v\n%s", err, stderr)
	}
	stub.AssertConsumed(t)
	if got := strings.TrimRight(stdout, "\n"); got != "Hello after the retry." {
		t.Errorf("stdout = %q, want the re-streamed reply alone", got)
	}
	if got := len(stub.Requests()); got != 2 {
		t.Errorf("stub saw %d requests, want 2 — the cut stream and its re-stream", got)
	}
}

// TestE2EUpstreamEOFPastTheBudgetFaults: the cut turn three times under `re-stream-budget: 2`. The
// first two cuts are re-streamed (the stub sees requests 2 and 3); the third lands on a Turn whose
// budget is spent, so the run's final Turn is abandoned with the read fault as its reason and a
// fourth request never happens — the budget is a count of re-streams, and the fault past it ends
// the Turn.
func TestE2EUpstreamEOFPastTheBudgetFaults(t *testing.T) {
	script := loadScript(t, "upstream-eof")
	cut := script.Turns[0]
	script.Turns = []stubllm.Turn{cut, cut, cut}
	stub := stubllm.New(t, script)

	stdout, stderr, err := headlessUpstream(t, stub, "re-stream-budget: 2\n")

	if err == nil {
		t.Fatalf("headless returned no error; want the abandoned-Turn fault\nstdout: %q\nstderr: %s", stdout, stderr)
	}
	stub.AssertConsumed(t)
	for _, want := range []string{"final turn was abandoned", "read stream", "unexpected EOF"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("headless error %q lacks %q", err, want)
		}
	}
	if got := len(stub.Requests()); got != 3 {
		t.Errorf("stub saw %d requests, want 3 — the cut stream and the budget's two re-streams, never a fourth attempt", got)
	}
}

// TestE2EUpstreamEOFBudgetZeroNeverReStreams: `re-stream-budget: 0` is the documented spelling of
// "never re-stream". The one cut is the fault that abandons the Turn, and the stub sees that single
// request — no hold-off is paid, because no re-stream is ever set up.
func TestE2EUpstreamEOFBudgetZeroNeverReStreams(t *testing.T) {
	script := loadScript(t, "upstream-eof")
	script.Turns = []stubllm.Turn{script.Turns[0]}
	stub := stubllm.New(t, script)

	stdout, stderr, err := headlessUpstream(t, stub, "re-stream-budget: 0\n")

	if err == nil {
		t.Fatalf("headless returned no error; want the abandoned-Turn fault\nstdout: %q\nstderr: %s", stdout, stderr)
	}
	stub.AssertConsumed(t)
	for _, want := range []string{"final turn was abandoned", "read stream", "unexpected EOF"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("headless error %q lacks %q", err, want)
		}
	}
	if got := len(stub.Requests()); got != 1 {
		t.Errorf("stub saw %d requests, want 1 — a budget of 0 never re-streams", got)
	}
}

// headlessUpstream runs one real `apogee headless` against stub in a home of its own and returns
// what reached stdout and stderr with the command's error. extraConfig is appended to the home's
// config.yaml verbatim — a `re-stream-budget:` line, or "" for the defaults. The runner injected
// is the real one, stated rather than defaulted: a canned runner would prove nothing about the
// stream the engine consumes.
func headlessUpstream(t *testing.T, stub *stubllm.Server, extraConfig string) (stdout, stderr string, err error) {
	t.Helper()

	// The environment must not move the home or the mode out from under the run.
	assertNoAmbientApogeeConfig(t)
	t.Setenv(config.EnvMode, "")

	home := t.TempDir()
	writeConfigHome(t, home,
		"context-window: 32768\n"+
			extraConfig+
			"servers:\n"+
			"  - name: stub\n"+
			"    endpoint: "+stub.URL+"\n"+
			"    model: "+stub.Model+"\n"+
			"server: stub\n")
	cmd := newHeadlessCommandWith(headlessDeps{runner: run.Once})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--config", home, "--workspace", e2eWorkspace(t), upstreamPrompt})
	err = cmd.ExecuteContext(context.Background())
	return outBuf.String(), errBuf.String(), err
}
