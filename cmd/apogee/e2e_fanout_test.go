package main

// A fan-out wider than the width it ran under tells the parent model so, once — the driven half of
// plan 2026-09-14 - 04 item 4. internal/agent's tests pin the line's placement on the committed
// result; this run pins that the line reaches the WIRE the way the model meets it, as the last line
// of the last delegation's tool result on the parent's next request, and that nothing ELSE reaches
// the wire: the standing system prompt of that request is byte for byte the one the session sent
// before it ever fanned out. The width is a per-reply fact and the orientation block is
// session-constant (ADR 0069 decisions 4 and 6), so a request after a fan-out must carry the same
// block a request before one did.
//
// The width comes from a `parallel-agents: 2` pin on the session's own server entry, as
// e2e_parallel_test.go's runs take it: stubllm answers no `/props`, so a pin is the only way a
// driven session runs wider than one.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

const (
	// fanOutWidthPrompt is a prompt testdata/stubllm/fanout.yaml answers with three `sub_agent`
	// calls (its `^Fan out` turn), and fanOutWidthWrapUp is what the parent says once the three
	// results are back (its `tool_result: sub_agent` turn).
	fanOutWidthPrompt = "Fan out three delegations."
	fanOutWidthWrapUp = "All three reported."

	// fanOutWidthLine is the exact line the LAST of three delegations run two at a time carries:
	// one waited for a worker, and the width was two. It is restated here rather than imported
	// because it is what the parent MODEL reads, so a rewording over in internal/agent has to fail
	// here.
	fanOutWidthLine = "[1 of this group's 3 delegations ran after the others finished — the width is 2]"
)

// TestE2EFanOutTrailerStatesTheWidth drives a three-way fan-out under a width of two and reads the
// parent's requests off the server's log: the third delegation's result ends with the width line,
// the first two carry none, and the system prompt is unchanged by the fan-out.
func TestE2EFanOutTrailerStatesTheWidth(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "fanout"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, stub, fanOutWidthHome(t, stub), "")
	waitIdle(drv)
	drv.WaitQuiet(settled)

	// The wrap-up on screen is a fan-out that came back: its turn is keyed on the sub_agent tool
	// result, so the request carrying the three results has been made by the time it shows.
	submit(drv, fanOutWidthPrompt)
	drv.WaitText(fanOutWidthWrapUp)
	drv.WaitQuiet(settled)

	before, after := fanOutParentRequests(t, stub)

	results := toolResultsOf(after)
	if len(results) != 3 {
		t.Fatalf("the request after the fan-out carries %d tool results; want the three delegations':\n%+v",
			len(results), after.Messages)
	}
	for i, result := range results[:2] {
		if strings.Contains(result, "of this group's") {
			t.Errorf("delegation %d's result carries the width line; only the last one states it: %q", i+1, result)
		}
	}
	if last := resultBody(results[2]); !strings.HasSuffix(strings.TrimRight(last, "\n"), fanOutWidthLine) {
		t.Errorf("the last delegation's result does not end with the width line %q:\n%q", fanOutWidthLine, last)
	}

	if got, want := seatSystemText(after), seatSystemText(before); got != want {
		t.Errorf("the system prompt changed across the fan-out; the width is a per-result fact, "+
			"never an orientation one\nbefore:\n%s\nafter:\n%s", want, got)
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// fanOutWidthHome writes an apogee home whose one server is stub, pinned to two parallel agents.
// It is spelled out rather than taken from [e2eHome] for the reason [parallelHome] is: the pin sits
// INSIDE the `servers:` entry, where no line appended to the file afterwards can reach.
func fanOutWidthHome(t *testing.T, stub *stubllm.Server) string {
	t.Helper()

	body := "servers:\n" +
		"  - name: " + parallelFanOutServer + "\n" +
		"    endpoint: " + stub.URL + "\n" +
		"    model: " + stub.Model + "\n" +
		parallelPin +
		"server: " + parallelFanOutServer + "\n"
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write the pinned home's config: %v", err)
	}
	return home
}

// fanOutParentRequests picks the parent's two Turns out of the server's log: the request the
// fan-out prompt closed — the one whose last message is that prompt and which offered a tool menu,
// so the title call's is never mistaken for it — and the request that carried the delegations'
// results back, whose last message is a tool result. Both are fatal when absent: a comparison over
// a missing request would be one over nothing.
func fanOutParentRequests(t *testing.T, stub *stubllm.Server) (before, after stubllm.Request) {
	t.Helper()

	var haveBefore, haveAfter bool
	for _, req := range stub.Requests() {
		if len(req.Messages) == 0 {
			continue
		}
		last := req.Messages[len(req.Messages)-1]
		switch {
		case !haveBefore && last.Role == "user" && last.Content == fanOutWidthPrompt && len(req.Tools) > 0:
			before, haveBefore = req, true
		case !haveAfter && last.Role == "tool":
			after, haveAfter = req, true
		}
	}
	if !haveBefore {
		t.Fatalf("no request closed on the fan-out prompt %q with a tool menu", fanOutWidthPrompt)
	}
	if !haveAfter {
		t.Fatal("no request carried a tool result back; the fan-out never returned to the parent")
	}
	return before, after
}

// toolResultsOf returns the contents of req's tool-result messages in wire order — for the request
// after a fan-out, the delegations' results in emitted-call order.
func toolResultsOf(req stubllm.Request) []string {
	var out []string
	for _, msg := range req.Messages {
		if msg.Role == "tool" {
			out = append(out, msg.Content)
		}
	}
	return out
}
