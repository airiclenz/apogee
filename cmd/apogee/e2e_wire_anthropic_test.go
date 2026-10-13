package main

// The end-to-end proof of the second outbound wire (ADR 0078): one `wire: anthropic` key on a
// server entry, and the whole tool-use loop — the prompt, the tool call, the result, the answer
// and the accounting — travels the Messages wire instead of chat completions. Everything above the
// provider is the same code either way, which is the point: the assertion is about the ROUTE the
// run took and the shape its usage came back in, judged from the stub's request log and the
// headless `usage` line, and about nothing the wires already share.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/testtime"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// wireLoopPrompt is what the driven and the headless runs both ask; the fixture's first loop turn
// matches its opening words.
const wireLoopPrompt = "Read the workspace file and say what is in it"

// wireLoopAnswer is the fixture's final reply, the line the frame shows once the loop has closed.
const wireLoopAnswer = "The file says hello."

// wireHome writes an apogee home whose one server entry names the given wire. It is spelled out
// here rather than taken from [e2eHome] for the same reason launcherHome is: `wire:` sits INSIDE
// the `servers:` entry and no line appended afterwards can reach in there. An empty wire writes
// no key at all, which is the default entry every server was before the key existed. The context
// window is pinned because the headless `usage` line carries it and a stub advertises none.
func wireHome(t *testing.T, stub *stubllm.Server, wire string) string {
	t.Helper()

	entry := "servers:\n  - name: stub\n    endpoint: " + stub.URL + "\n    model: " + stub.Model + "\n"
	if wire != "" {
		entry += "    wire: " + wire + "\n"
	}
	home := t.TempDir()
	body := "context-window: 32768\n" + entry + "server: stub\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write the wire home's config: %v", err)
	}
	return home
}

// driveWireLoop runs the fixture's tool loop through the TUI against a home whose entry names
// wire, waits for the answer to reach the frame, and returns the stub whose log holds what was
// asked. The run is quit before the return so the log is complete and the session's cleanups own
// nothing that is still talking.
func driveWireLoop(t *testing.T, wire string) *stubllm.Server {
	t.Helper()

	stub := stubllm.New(t, loadScript(t, "wire-anthropic"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, stub, wireHome(t, stub, wire), "")
	// Registered after the launch so it runs before the session's own cleanup (LIFO): the stub
	// and the driver are still live when a failed run is dumped.
	t.Cleanup(func() { dumpWireLoopOnFailure(t, stub, drv, sess) })

	submit(drv, wireLoopPrompt)
	drv.WaitText(wireLoopAnswer)
	waitForCommittedReply(t, sess, wireLoopAnswer)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
	stub.AssertConsumed(t)
	return stub
}

// dumpWireLoopOnFailure logs everything a failed wire loop leaves behind, so a failure seen only on
// a host nobody can reproduce on (default-wire-arm64-flake) explains itself from its own log: what
// the stub was asked and on which wire, what it answered, what it was asked that no route serves,
// the frame the run ended on, what the command tree printed, what the session store holds, and the
// host and time scale the budgets ran under. A passing run logs nothing.
func dumpWireLoopOnFailure(t *testing.T, stub *stubllm.Server, drv *tuitest.Driver, sess *e2eSession) {
	t.Helper()

	if !t.Failed() {
		return
	}
	// The frame is read while the run may still be up — a stopped run has left the alternate
	// screen — and the output only after it stops, since the app writes it until then; stop is a
	// no-op for a run already quit.
	frame := strings.TrimRight(drv.Frame().String(), "\n")
	sess.stop()

	var b strings.Builder
	fmt.Fprintf(&b, "wire loop failed on %s/%s, test time scale %g\n", runtime.GOOS, runtime.GOARCH, testtime.Scale())
	b.WriteString("requests:\n")
	for _, r := range stub.Requests() {
		fmt.Fprintf(&b, "  #%d wire=%s turn=%d unmatched=%t at=%s last=%q\n",
			r.N, r.Wire, r.TurnIndex, r.Unmatched, r.At.Format(time.RFC3339Nano), lastMessageText(r))
	}
	b.WriteString("probes:\n")
	for _, p := range stub.Probes() {
		fmt.Fprintf(&b, "  %s at=%s\n", p.Path, p.At.Format(time.RFC3339Nano))
	}
	b.WriteString("unmatched:\n")
	for _, r := range stub.Unmatched() {
		fmt.Fprintf(&b, "  %s\n", r)
	}
	b.WriteString("strays:\n")
	for _, s := range stub.Strays() {
		fmt.Fprintf(&b, "  %s %s at=%s\n", s.Method, s.Path, s.At.Format(time.RFC3339Nano))
	}
	fmt.Fprintf(&b, "frame:\n%s\n", frame)
	fmt.Fprintf(&b, "output:\n%s\n", sess.Output())
	b.WriteString("session store:\n")
	entries, err := os.ReadDir(filepath.Join(sess.Home(), "sessions"))
	if err != nil {
		fmt.Fprintf(&b, "  unreadable: %v\n", err)
	}
	for _, entry := range entries {
		fmt.Fprintf(&b, "  %s\n", entry.Name())
	}
	t.Log(b.String())
}

// lastMessageText is the text of a request's last message, or "" when it carried none.
func lastMessageText(r stubllm.Request) string {
	if len(r.Messages) == 0 {
		return ""
	}
	return r.Messages[len(r.Messages)-1].Content
}

// assertRequestsOnWire fails unless every request the stub logged arrived on wire — the loop's
// own calls and whatever apogee asked on its own account alike, since the wire is the entry's,
// not the prompt's.
func assertRequestsOnWire(t *testing.T, requests []stubllm.Request, wire string) {
	t.Helper()

	if len(requests) == 0 {
		t.Fatal("the stub logged no requests; the run never reached it")
	}
	for _, r := range requests {
		if r.Wire != wire {
			t.Errorf("%s arrived on the %q wire; the entry names %q", r, r.Wire, wire)
		}
	}
}

// assertToolResultAnswered finds the request that carried the read_file result back and checks it
// answers the call the model issued: its last message is the tool role, and its id is the id of a
// read_file call on an earlier assistant message of the same request. On the anthropic wire that
// message is what a `tool_result` block decoded to (internal/stubllm/wire_anthropic.go) — the
// block is the only shape the Messages route folds to the tool role, so a tool-role message on an
// anthropic-wire request IS a tool_result block that arrived.
func assertToolResultAnswered(t *testing.T, requests []stubllm.Request) {
	t.Helper()

	for _, r := range requests {
		if len(r.Messages) == 0 {
			continue
		}
		last := r.Messages[len(r.Messages)-1]
		if last.Role != "tool" {
			continue
		}
		for _, m := range r.Messages[:len(r.Messages)-1] {
			for _, call := range m.ToolCalls {
				if call.ID == last.ToolCallID && call.Name == "read_file" {
					return
				}
			}
		}
		t.Fatalf("%s carries a tool result for call %q, which no read_file call on the request issued",
			r, last.ToolCallID)
	}
	t.Fatal("no request carried the read_file result back; the loop never closed")
}

// TestE2EAnthropicWireCompletesAToolLoop is the issue's acceptance (apogee-6fp): with `wire:
// anthropic` on the entry, a driven prompt that needs a read completes — the call goes out, the
// result comes back as a tool_result block, the answer lands in the frame — and every request of
// the run posted to /v1/messages. The same script is then run headless, because the `usage` line
// is where the wire's accounting shows: the Messages wire reports its cached share beside the
// input count rather than inside it, and the line has to carry the fixture's numbers under the
// names ADR 0075 D10 fixed, whichever wire answered.
//
// Parallel like every driven launch (test-drivers.md, rule 9): the headless half goes through
// [headlessEventLines], whose runner is injected (headlessDeps) and which reaches no t.Setenv,
// so neither half swaps anything a sibling could read mid-swap.
func TestE2EAnthropicWireCompletesAToolLoop(t *testing.T) {
	t.Parallel()

	stub := driveWireLoop(t, "anthropic")

	requests := stub.Requests()
	assertRequestsOnWire(t, requests, stubllm.WireAnthropic)
	assertToolResultAnswered(t, requests)

	// The accounting, read where a consumer reads it.
	headless := stubllm.New(t, loadScript(t, "wire-anthropic"))
	workspace := e2eWorkspace(t)
	out, errOut, err := headlessEventLines(t, run.Once, wireHome(t, headless, "anthropic"), workspace, wireLoopPrompt)
	if err != nil {
		t.Fatalf("the headless run returned %v\nstderr:\n%s", err, errOut)
	}
	headless.AssertConsumed(t)
	assertRequestsOnWire(t, headless.Requests(), stubllm.WireAnthropic)

	var usages []map[string]any
	for _, line := range jsonEventLines(t, out) {
		if line["event"] == "usage" {
			data, ok := line["data"].(map[string]any)
			if !ok {
				t.Fatalf("a usage line carried no data object: %v", line)
			}
			usages = append(usages, data)
		}
	}
	// The fixture's two calls, in order: prompt = input + cached reads, completion = output.
	want := []map[string]float64{
		{"prompt_tokens": 420, "completion_tokens": 18, "cached_prompt_tokens": 100},
		{"prompt_tokens": 470, "completion_tokens": 9, "cached_prompt_tokens": 300},
	}
	if len(usages) != len(want) {
		t.Fatalf("the stream carried %d usage lines; want %d, one per model call:\n%s", len(usages), len(want), out)
	}
	for i, data := range usages {
		for name, value := range want[i] {
			if data[name] != value {
				t.Errorf("usage line %d: %s = %v; want %v as the Messages usage maps to it", i+1, name, data[name], value)
			}
		}
	}
}

// TestE2EDefaultWireStaysChatCompletions is the other half of the key's zero value: an entry that
// names no wire is the chat-completions wire it always was, so the same loop over the same script
// posts every request to /v1/chat/completions and nothing to /v1/messages.
func TestE2EDefaultWireStaysChatCompletions(t *testing.T) {
	t.Parallel()

	stub := driveWireLoop(t, "")

	requests := stub.Requests()
	assertRequestsOnWire(t, requests, stubllm.WireOpenAI)
	assertToolResultAnswered(t, requests)
}
