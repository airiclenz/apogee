package mechanisms

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// readFileTool mirrors apogee's real read_file schema — its argument schema DECLARES max_lines, so
// the cap has a field to attach to (the tool menu the hook reads via view.Tools()).
var readFileTool = domain.ToolDef{
	Name:   "read_file",
	Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"start_line":{"type":"integer"},"end_line":{"type":"integer"},"max_lines":{"type":"integer"}}}`),
}

// fireCachedWithTools fires cached_content_intercept once against the pending call over history, with
// tools as the menu the hook sees, and returns the (possibly mutated) call.
func fireCachedWithTools(t *testing.T, history []domain.Message, call domain.ToolCall, tools []domain.ToolDef) domain.ToolCall {
	t.Helper()
	hook, ok := mustBuild(t, cachedContentInterceptID).(domain.PreToolExecHook)
	if !ok {
		t.Fatal("cached_content_intercept does not implement PreToolExecHook")
	}
	c := call
	view := domain.NewRequest("m", history, tools, domain.Budget{}, 0, nil).View()
	if err := hook.PreToolExec(context.Background(), &c, view); err != nil {
		t.Fatalf("PreToolExec: %v", err)
	}
	return c
}

// fireCached fires cached_content_intercept over the default menu (apogee's read_file, whose schema
// declares max_lines).
func fireCached(t *testing.T, history []domain.Message, call domain.ToolCall) domain.ToolCall {
	t.Helper()
	return fireCachedWithTools(t, history, call, []domain.ToolDef{readFileTool})
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

// An MCP-style read tool whose argument schema does NOT declare max_lines (a strict server with
// additionalProperties:false) is inspected but never mutated — appending max_lines would hand it an
// argument it rejects, so the redundant re-read proceeds uncapped (no mutation ⇒ no fire, R4).
func TestCachedContentSkipsToolWithoutMaxLinesSchema(t *testing.T) {
	t.Parallel()
	mcpRead := func(id, path string) domain.ToolCall {
		args, _ := json.Marshal(map[string]string{"path": path})
		return domain.ToolCall{ID: id, Tool: "readFile", Arguments: args}
	}
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(mcpRead("r1", "a.go")),
		toolResult("r1", "package a\nfunc F() {}"),
		assistantCall(mcpRead("r2", "a.go")),
	}
	mcpReadTool := domain.ToolDef{
		Name:   "readFile",
		Schema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"path":{"type":"string"}}}`),
	}
	call := mcpRead("r2", "a.go")
	got := fireCachedWithTools(t, history, call, []domain.ToolDef{mcpReadTool})
	if hasMaxLines(got.Arguments) {
		t.Errorf("a read tool without a max_lines schema was capped; args = %s", got.Arguments)
	}
	if string(got.Arguments) != string(call.Arguments) {
		t.Errorf("arguments mutated: %s vs %s", got.Arguments, call.Arguments)
	}
}

// toolDeclaresMaxLines has three conservative fallbacks that all withhold the cap: the pending read
// tool is (a) absent from the tool menu (the realistic case — toolfilter narrowing removed it from
// view.Tools()), (b) present with an empty schema, or (c) present with a schema that does not parse.
// In each, max_lines cannot be confirmed as a declared property, and appending it might hand a strict
// tool an argument it rejects — so a genuine redundant re-read is left byte-identical (no mutation ⇒
// no fire, R4). This pins the fallbacks the review found mutation-proven silent.
func TestCachedContentSchemaGateConservativeFallbacks(t *testing.T) {
	t.Parallel()
	// a.go was read successfully earlier and not written since, so the read below is genuinely
	// redundant: only the schema gate stands between it and a cap.
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a\nfunc F() {}"),
		assistantCall(readCall("r2", "a.go")),
	}
	// otherTool stands in for a toolfilter-narrowed menu that no longer carries the pending read tool.
	otherTool := domain.ToolDef{
		Name:   "list_dir",
		Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	}
	cases := []struct {
		name  string
		tools []domain.ToolDef
	}{
		{"absent from the menu", []domain.ToolDef{otherTool}},
		{"present with an empty schema", []domain.ToolDef{{Name: "read_file"}}},
		{"present with malformed schema JSON", []domain.ToolDef{{Name: "read_file", Schema: json.RawMessage(`{"type":"object","properties":`)}}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			call := readCall("r2", "a.go")
			got := fireCachedWithTools(t, history, call, tc.tools)
			if hasMaxLines(got.Arguments) {
				t.Errorf("redundant re-read capped despite an unconfirmed schema; args = %s", got.Arguments)
			}
			if string(got.Arguments) != string(call.Arguments) {
				t.Errorf("arguments mutated (a fire was booked): %s vs %s", got.Arguments, call.Arguments)
			}
		})
	}
}
