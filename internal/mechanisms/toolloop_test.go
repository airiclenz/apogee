package mechanisms

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// A response repeating the previous turn's exact tool calls retries in place with the loop-breaking
// directive (apogee-sim detectToolCallLoop + retryWithToolLoopDirective @pin).
func TestToolLoopRetriesOnIdenticalRepeat(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("build the thing"),
		assistantCall(writeCall("w1", "a.go", "package main")),
		toolResult("w1", "ok"),
	}
	resp := offrampResponse(history, nil, "", writeCall("w2", "a.go", "package main"))
	d := postResponse(t, toolLoopInterceptorID, resp)
	if d.Action != domain.ActionRetry {
		t.Fatalf("Action = %q, want ActionRetry", d.Action)
	}
	if !strings.Contains(d.Inject, "in a loop") {
		t.Errorf("directive = %q, want the loop-breaking wording", d.Inject)
	}
}

// A response with different tool calls than the previous turn is not a loop — no retry.
func TestToolLoopInertOnDifferentCalls(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("build the thing"),
		assistantCall(writeCall("w1", "a.go", "package main")),
		toolResult("w1", "ok"),
	}
	resp := offrampResponse(history, nil, "", writeCall("w2", "b.go", "package b"))
	if d := postResponse(t, toolLoopInterceptorID, resp); d.Action != "" {
		t.Errorf("Action = %q, want no action on differing calls", d.Action)
	}
}

// With no previous tool-call turn there is nothing to loop against — the first tool-call response
// never fires the interceptor.
func TestToolLoopInertWithoutPreviousTurn(t *testing.T) {
	t.Parallel()
	history := []domain.Message{userMsg("build the thing")}
	resp := offrampResponse(history, nil, "", writeCall("w1", "a.go", "package main"))
	if d := postResponse(t, toolLoopInterceptorID, resp); d.Action != "" {
		t.Errorf("Action = %q, want no action on the first tool-call turn", d.Action)
	}
}

// TestEmbeddedDirectivePromptsLoad pins the loader contract behind the directive's prompt assets:
// every file under prompts/ carries text, mustPrompt returns it with the single trailing newline
// the file ends in already stripped, and each asset holds exactly the number of %s verbs the
// builder feeds it — a verb added to or dropped from an asset would otherwise surface only as a
// %!s(MISSING) or an %!(EXTRA ...) inside a live directive. The table doubles as the roster: an
// asset embedded but not named here fails, so a new fragment cannot slip in unpinned.
func TestEmbeddedDirectivePromptsLoad(t *testing.T) {
	t.Parallel()

	verbs := map[string]int{
		"loop-header.txt":               1,
		"results-above.txt":             0,
		"task-reminder.txt":             1,
		"files-written.txt":             1,
		"files-read.txt":                1,
		"tail-continue-work.txt":        0,
		"tail-write-implementation.txt": 0,
		"tail-different-action.txt":     0,
	}

	entries, err := promptFS.ReadDir("prompts")
	if err != nil {
		t.Fatalf("read the embedded prompts directory: %v", err)
	}
	if len(entries) != len(verbs) {
		t.Errorf("%d prompt assets are embedded, want the %d this test pins", len(entries), len(verbs))
	}
	for _, e := range entries {
		want, pinned := verbs[e.Name()]
		if !pinned {
			t.Errorf("prompt asset %s is embedded but not pinned by this test", e.Name())
			continue
		}
		got := mustPrompt(e.Name())
		if strings.TrimSpace(got) == "" {
			t.Errorf("prompt asset %s loads as empty text", e.Name())
		}
		if strings.HasSuffix(got, "\n") {
			t.Errorf("prompt asset %s still ends in a newline after load: %q", e.Name(), got)
		}
		if n := strings.Count(got, "%s"); n != want {
			t.Errorf("prompt asset %s holds %d %%s verbs, want %d", e.Name(), n, want)
		}
	}
}
