package mechanisms

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// fireCached fires cached_content_intercept once against the pending call over history and returns
// the (possibly mutated) call.
func fireCached(t *testing.T, history []domain.Message, call domain.ToolCall) domain.ToolCall {
	t.Helper()
	hook, ok := mustBuild(t, cachedContentInterceptID).(domain.PreToolExecHook)
	if !ok {
		t.Fatal("cached_content_intercept does not implement PreToolExecHook")
	}
	c := call
	if err := hook.PreToolExec(context.Background(), &c, historyView(history)); err != nil {
		t.Fatalf("PreToolExec: %v", err)
	}
	return c
}

// hasMaxLines reports whether the read arguments carry a max_lines cap.
func hasMaxLines(args json.RawMessage) bool {
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return false
	}
	_, ok := m["max_lines"]
	return ok
}

// A read of a file already read successfully (and not written since) is intercepted: the redundant
// re-read is capped to a header-only slice, so the full content already in context is not re-dumped
// (apogee-sim detectCachedReread @pin, relocated to pre-tool-exec — the token-saving intent, capped
// via the arguments because pre-tool-exec has no result-substitution primitive).
func TestCachedContentInterceptsRedundantReRead(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a\nfunc F() {}"),
		assistantCall(readCall("r2", "a.go")),
	}
	got := fireCached(t, history, readCall("r2", "a.go"))
	if !hasMaxLines(got.Arguments) {
		t.Errorf("redundant re-read not capped; args = %s", got.Arguments)
	}
}

// A read of a file not read before is untouched — a novel read is legitimate work.
func TestCachedContentLeavesNovelReadUntouched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit b.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(readCall("r2", "b.go")),
	}
	call := readCall("r2", "b.go")
	got := fireCached(t, history, call)
	if hasMaxLines(got.Arguments) {
		t.Errorf("novel read was capped; args = %s", got.Arguments)
	}
	if string(got.Arguments) != string(call.Arguments) {
		t.Errorf("novel read arguments mutated: %s vs %s", got.Arguments, call.Arguments)
	}
}

// A file written after its last successful read may have changed, so re-reading it is not redundant —
// cached_content_intercept leaves it alone (the item's "unchanged path", strengthening the sim).
func TestCachedContentLeavesWrittenSinceUntouched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(writeCall("w1", "a.go", "package a\nfunc G() {}")),
		toolResult("w1", "ok"),
		assistantCall(readCall("r2", "a.go")),
	}
	got := fireCached(t, history, readCall("r2", "a.go"))
	if hasMaxLines(got.Arguments) {
		t.Errorf("a re-read after a write was capped; the file may have changed. args = %s", got.Arguments)
	}
}

// A targeted read (an explicit line range/limit) is not a redundant full re-dump — it is left intact.
func TestCachedContentLeavesRangedReadUntouched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(readCall("r2", "a.go")),
	}
	ranged := domain.ToolCall{ID: "r2", Tool: "read_file", Arguments: json.RawMessage(`{"path":"a.go","max_lines":50}`)}
	got := fireCached(t, history, ranged)
	if string(got.Arguments) != string(ranged.Arguments) {
		t.Errorf("a ranged read was mutated: %s vs %s", got.Arguments, ranged.Arguments)
	}
	if !strings.Contains(string(got.Arguments), "50") {
		t.Error("the model's explicit max_lines was overwritten")
	}
}
