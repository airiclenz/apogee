package main

// The transient-stream seam end to end: a real `apogee headless` against a stubllm server whose
// reply dies mid-stream (testdata/stubllm/upstream-eof.yaml, a `cut` turn that kills the TCP
// connection). The engine's one re-stream per Turn was granted to an in-band 502 alone; a body cut
// by an EOF used to fail the Turn on the spot. Both cases below pay the loop's unexported 1 s
// restreamHoldoff on every re-stream — it is production code and this package cannot shorten it —
// so each costs at least a second of wall clock.

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

// TestE2EUpstreamEOFIsRetriedOnce: turn 1 is cut three runes in, turn 2 is the whole answer. The
// answer on stdout is turn 2's text — never the three runes streamed before the cut — and the stub
// saw exactly two requests: the cut stream and its one re-stream.
func TestE2EUpstreamEOFIsRetriedOnce(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "upstream-eof"))

	stdout, stderr, err := headlessUpstream(t, stub)

	if err != nil {
		t.Fatalf("headless: %v\n%s", err, stderr)
	}
	stub.AssertConsumed(t)
	if got := strings.TrimRight(stdout, "\n"); got != "Hello after the retry." {
		t.Errorf("stdout = %q, want the re-streamed reply alone", got)
	}
	if got := len(stub.Requests()); got != 2 {
		t.Errorf("stub saw %d requests, want 2 — the cut stream and its one re-stream", got)
	}
}

// TestE2EUpstreamEOFTwiceFaults: the same cut turn twice. The second cut lands on a Turn whose one
// re-stream is spent, so the run's final Turn is abandoned with the read fault as its reason, and
// a third request never happens.
func TestE2EUpstreamEOFTwiceFaults(t *testing.T) {
	script := loadScript(t, "upstream-eof")
	script.Turns = []stubllm.Turn{script.Turns[0], script.Turns[0]}
	stub := stubllm.New(t, script)

	stdout, stderr, err := headlessUpstream(t, stub)

	if err == nil {
		t.Fatalf("headless returned no error; want the abandoned-Turn fault\nstdout: %q\nstderr: %s", stdout, stderr)
	}
	stub.AssertConsumed(t)
	for _, want := range []string{"final turn was abandoned", "read stream", "unexpected EOF"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("headless error %q lacks %q", err, want)
		}
	}
	if got := len(stub.Requests()); got != 2 {
		t.Errorf("stub saw %d requests, want 2 — one re-stream, never a third attempt", got)
	}
}

// headlessUpstream runs one real `apogee headless` against stub in a home of its own and returns
// what reached stdout and stderr with the command's error. The runner is the real one, restored
// after: a canned runner would prove nothing about the stream the engine consumes.
func headlessUpstream(t *testing.T, stub *stubllm.Server) (stdout, stderr string, err error) {
	t.Helper()

	prev := runOnce
	runOnce = run.Once
	t.Cleanup(func() { runOnce = prev })
	// The environment must not move the home or the mode out from under the run.
	assertNoAmbientApogeeConfig(t)
	t.Setenv(config.EnvMode, "")

	home := eventLinesHome(t, stub.URL, stub.Model)
	cmd := newHeadlessCommand()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--config", home, "--workspace", e2eWorkspace(t), upstreamPrompt})
	err = cmd.ExecuteContext(context.Background())
	return outBuf.String(), errBuf.String(), err
}
