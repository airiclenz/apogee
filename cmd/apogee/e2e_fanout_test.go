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

	// fanOutCeilingPrompt is a prompt testdata/stubllm/fanout-ceiling.yaml answers with TEN
	// `sub_agent` calls (its `^Fan out ten` turn), and fanOutCeilingWrapUp is what the parent says
	// once the results are back (its `tool_result: sub_agent` turn).
	fanOutCeilingPrompt = "Fan out ten delegations."
	fanOutCeilingWrapUp = "All ten reported."

	// fanOutCeilingPin is the `parallel-agents:` line of the ceiling run's server entry: width 4,
	// which under the default two rounds is a ceiling of eight — two short of the ten the fixture
	// asks for.
	fanOutCeilingPin = "    parallel-agents: 4\n"

	// fanOutCeilingRefusal is the exact result the ninth and tenth delegations carry — internal/
	// agent/dispatch.go's fanOutCeilingResultFormat rendered for ten calls at width 4 under two
	// rounds, restated here for fanOutWidthLine's reason: it is what the parent MODEL reads.
	fanOutCeilingRefusal = "sub-agent not started: this reply fanned out 10 delegations and the ceiling is 8 " +
		"(2 rounds × width 4) — the first 8 ran; delegate the rest again once their results are in"
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

	before, after := fanOutParentRequests(t, stub, fanOutWidthPrompt)

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

// TestE2EFanOutCeilingRefusesTheOverflow drives a ten-way fan-out under a width of four — a
// ceiling of eight at the default two rounds — and reads the parent's next request off the
// server's log: the first eight results are the children's own, the ninth and tenth are exactly the
// ceiling refusal, no result carries a width line, and the parent still wraps up. The children
// that were refused were never asked: no request in the log carries their tasks.
func TestE2EFanOutCeilingRefusesTheOverflow(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "fanout-ceiling"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, stub, fanOutCeilingHome(t, stub), "")
	waitIdle(drv)
	drv.WaitQuiet(settled)

	submit(drv, fanOutCeilingPrompt)
	drv.WaitText(fanOutCeilingWrapUp)
	drv.WaitQuiet(settled)

	_, after := fanOutParentRequests(t, stub, fanOutCeilingPrompt)

	results := toolResultsOf(after)
	if len(results) != 10 {
		t.Fatalf("the request after the fan-out carries %d tool results; want all ten delegations':\n%+v",
			len(results), after.Messages)
	}
	for i, result := range results[:8] {
		if strings.Contains(result, "sub-agent not started") {
			t.Errorf("delegation %d's result is a refusal; the first eight run: %q", i+1, result)
		}
	}
	for i, result := range results[8:] {
		if got := resultBody(result); got != fanOutCeilingRefusal {
			t.Errorf("delegation %d's result = %q; want exactly the ceiling refusal %q", i+9, got, fanOutCeilingRefusal)
		}
	}
	for i, result := range results {
		if strings.Contains(result, "of this group's") {
			t.Errorf("delegation %d's result carries a width line; a group with a refused slot states none: %q", i+1, result)
		}
	}
	// A child's task is its own user message; the parent's next request quotes the refused tasks
	// too, in the delegations note on its tail, which is why the match is on a user message alone.
	for _, req := range stub.Requests() {
		for _, name := range []string{"iota", "kappa"} {
			if hasUserMessage(req, "Count the "+name+" files") {
				t.Errorf("request %d is the refused delegation %q's conversation; a refused child was run", req.N, name)
			}
		}
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
	return pinnedFanOutHome(t, stub, parallelPin)
}

// fanOutCeilingHome is fanOutWidthHome pinned to four parallel agents instead: the width the
// ceiling run states, and the one number in its refusal that comes from the home.
func fanOutCeilingHome(t *testing.T, stub *stubllm.Server) string {
	t.Helper()
	return pinnedFanOutHome(t, stub, fanOutCeilingPin)
}

// pinnedFanOutHome writes the one-server home the two fan-out runs share, with pin as the entry's
// `parallel-agents:` line.
func pinnedFanOutHome(t *testing.T, stub *stubllm.Server, pin string) string {
	t.Helper()

	body := "servers:\n" +
		"  - name: " + parallelFanOutServer + "\n" +
		"    endpoint: " + stub.URL + "\n" +
		"    model: " + stub.Model + "\n" +
		pin +
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
func fanOutParentRequests(t *testing.T, stub *stubllm.Server, prompt string) (before, after stubllm.Request) {
	t.Helper()

	var haveBefore, haveAfter bool
	for _, req := range stub.Requests() {
		if len(req.Messages) == 0 {
			continue
		}
		last := req.Messages[len(req.Messages)-1]
		switch {
		case !haveBefore && last.Role == "user" && last.Content == prompt && len(req.Tools) > 0:
			before, haveBefore = req, true
		case !haveAfter && last.Role == "tool":
			after, haveAfter = req, true
		}
	}
	if !haveBefore {
		t.Fatalf("no request closed on the fan-out prompt %q with a tool menu", prompt)
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
