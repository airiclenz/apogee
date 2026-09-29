package floor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
)

// readFileTool mirrors apogee's real read_file schema — it DECLARES max_lines, so the cap has a
// field to attach to (the tool menu the guard reads through LoopView.Tools()).
var readFileTool = domain.ToolDef{
	Name:   "read_file",
	Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"start_line":{"type":"integer"},"end_line":{"type":"integer"},"max_lines":{"type":"integer"}}}`),
}

// editCall is an edit_existing_file call over path — apogee's own edit spelling, a write-since on
// the isFileMutatingTool superset though it carries no file body.
func editCall(id, path string) domain.ToolCall {
	return domaintest.Call(id, "edit_existing_file", map[string]string{"path": path, "old": "a", "new": "b"})
}

// toolResult is a committed tool result for callID.
func toolResult(callID, content string) domain.Message {
	return domaintest.ToolResultMessage(callID, content)
}

// cacheReadWithTools runs the guard once against the pending call over history, with tools as the
// menu it sees, and returns the (possibly capped) call and whether the guard reported a cap.
func cacheReadWithTools(history []domain.Message, call domain.ToolCall, tools []domain.ToolDef) (domain.ToolCall, bool) {
	c := call
	view := domain.NewRequest("m", history, tools, domain.Budget{}, 0).View()
	ok := CacheRead(view, domain.NewToolCallEdit(&c))
	return c, ok
}

// cacheRead runs the guard over the default menu (apogee's read_file, whose schema declares
// max_lines).
func cacheRead(history []domain.Message, call domain.ToolCall) (domain.ToolCall, bool) {
	return cacheReadWithTools(history, call, []domain.ToolDef{readFileTool})
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

// A read of a file already read successfully (and not written since) is capped to a header-only
// slice, so the full content already in context is not re-dumped (apogee-sim detectCachedReread
// @pin, expressed as an argument cap because the pre-tool-exec seam shapes the pending call).
func TestCacheReadCapsRedundantReRead(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a\nfunc F() {}"),
		assistantCall(readCall("r2", "a.go")),
	}
	got, ok := cacheRead(history, readCall("r2", "a.go"))
	if !ok {
		t.Error("CacheRead reported no cap on a redundant re-read")
	}
	if !hasMaxLines(got.Arguments) {
		t.Errorf("redundant re-read not capped; args = %s", got.Arguments)
	}
}

// A read of a file not read before is untouched — a novel read is legitimate work.
func TestCacheReadLeavesNovelReadUntouched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit b.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(readCall("r2", "b.go")),
	}
	call := readCall("r2", "b.go")
	got, ok := cacheRead(history, call)
	if ok {
		t.Error("CacheRead reported a cap on a novel read")
	}
	if string(got.Arguments) != string(call.Arguments) {
		t.Errorf("novel read arguments mutated: %s vs %s", got.Arguments, call.Arguments)
	}
}

// A file written after its last successful read may have changed, so re-reading it is not
// redundant — the guard leaves it alone (the "unchanged path" strengthening over the sim).
func TestCacheReadLeavesWrittenSinceUntouched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(writeCall("w1", "a.go")),
		toolResult("w1", "ok"),
		assistantCall(readCall("r2", "a.go")),
	}
	got, ok := cacheRead(history, readCall("r2", "a.go"))
	if ok || hasMaxLines(got.Arguments) {
		t.Errorf("a re-read after a write was capped; the file may have changed. args = %s", got.Arguments)
	}
}

// The guard must NOT cap a re-read of a file EDITED after its last read — the edit may have changed
// the file, so its cached copy is stale. This holds only because isFileMutatingTool counts
// edit_existing_file as a write-since (the 2026-08-10 write-detection pin, moved here with the
// subject it pins).
func TestCacheReadLeavesEditedSinceUntouched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(editCall("e1", "a.go")),
		toolResult("e1", "edited a.go"),
		assistantCall(readCall("r2", "a.go")),
	}
	got, ok := cacheRead(history, readCall("r2", "a.go"))
	if ok || hasMaxLines(got.Arguments) {
		t.Errorf("a re-read after an edit was capped; the cached copy is stale. args = %s", got.Arguments)
	}
}

// A targeted read (an explicit line range/limit) is not a redundant full re-dump — it is left
// intact, the model's own bound untouched.
func TestCacheReadLeavesRangedReadUntouched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(readCall("r2", "a.go")),
	}
	ranged := domain.ToolCall{ID: "r2", Tool: "read_file", Arguments: json.RawMessage(`{"path":"a.go","max_lines":50}`)}
	got, ok := cacheRead(history, ranged)
	if ok {
		t.Error("CacheRead reported a cap on a ranged read")
	}
	if string(got.Arguments) != string(ranged.Arguments) {
		t.Errorf("a ranged read was mutated: %s vs %s", got.Arguments, ranged.Arguments)
	}
	if !strings.Contains(string(got.Arguments), "50") {
		t.Error("the model's explicit max_lines was overwritten")
	}
}

// An MCP-style read tool whose argument schema does NOT declare max_lines (a strict server with
// additionalProperties:false) is inspected but never mutated — appending max_lines would hand it an
// argument it rejects, so the redundant re-read proceeds uncapped.
func TestCacheReadSkipsToolWithoutMaxLinesSchema(t *testing.T) {
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
	got, ok := cacheReadWithTools(history, call, []domain.ToolDef{mcpReadTool})
	if ok || hasMaxLines(got.Arguments) {
		t.Errorf("a read tool without a max_lines schema was capped; args = %s", got.Arguments)
	}
	if string(got.Arguments) != string(call.Arguments) {
		t.Errorf("arguments mutated: %s vs %s", got.Arguments, call.Arguments)
	}
}

// toolDeclaresMaxLines has three conservative fallbacks that all withhold the cap: the pending read
// tool is (a) absent from the tool menu (the realistic case — toolfilter narrowing removed it from
// Tools()), (b) present with an empty schema, or (c) present with a schema that does not parse. In
// each, max_lines cannot be confirmed as a declared property, and appending it might hand a strict
// tool an argument it rejects — so a genuine redundant re-read is left byte-identical.
func TestCacheReadSchemaGateConservativeFallbacks(t *testing.T) {
	t.Parallel()
	// a.go was read successfully earlier and not written since, so the read below is genuinely
	// redundant: only the schema gate stands between it and a cap.
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a\nfunc F() {}"),
		assistantCall(readCall("r2", "a.go")),
	}
	// otherTool stands in for a narrowed menu that no longer carries the pending read tool.
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
			got, ok := cacheReadWithTools(history, call, tc.tools)
			if ok || hasMaxLines(got.Arguments) {
				t.Errorf("redundant re-read capped despite an unconfirmed schema; args = %s", got.Arguments)
			}
			if string(got.Arguments) != string(call.Arguments) {
				t.Errorf("arguments mutated: %s vs %s", got.Arguments, call.Arguments)
			}
		})
	}
}

// shellCall is a terminal call whose command rewrites a file it never names as a path argument —
// the audit's stale-cache trigger (`sed -i` through the shell).
func shellCall(id string) domain.ToolCall {
	return domaintest.Call(id, "terminal", map[string]string{"command": "sed -i s/F/G/ a.go"})
}

// A call to any tool that is not read-only — or that the menu does not list at all — after the last
// successful read may have rewritten the file without naming it, so the next read of that path gets
// the whole file. A read-only tool (grep, list_dir, git_status) keeps the cache: it cannot have
// changed anything.
func TestCacheReadYieldsAfterANonReadOnlyCall(t *testing.T) {
	t.Parallel()
	menu := []domain.ToolDef{
		readFileTool,
		{Name: "terminal"},
		{Name: "mcp__fs__rewrite"},
		{Name: "grep", ReadOnly: true},
		{Name: "list_dir", ReadOnly: true},
		{Name: "git_status", ReadOnly: true},
	}
	cases := []struct {
		name    string
		between domain.ToolCall
		wantCap bool
	}{
		{name: "terminal", between: shellCall("x1")},
		{name: "MCP tool", between: domaintest.Call("x1", "mcp__fs__rewrite", map[string]string{"target": "a.go"})},
		{name: "tool absent from the menu", between: domaintest.Call("x1", "python_exec", map[string]string{"code": "open('a.go','w')"})},
		{name: "grep", between: domaintest.Call("x1", "grep", map[string]string{"pattern": "F"}), wantCap: true},
		{name: "list_dir", between: domaintest.Call("x1", "list_dir", map[string]string{"path": "."}), wantCap: true},
		{name: "git_status", between: domaintest.Call("x1", "git_status", map[string]string{}), wantCap: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			history := []domain.Message{
				userMsg("edit a.go"),
				assistantCall(readCall("r1", "a.go")),
				toolResult("r1", "package a\nfunc F() {}"),
				assistantCall(tc.between),
				toolResult("x1", "ok"),
				assistantCall(readCall("r2", "a.go")),
			}
			got, ok := cacheReadWithTools(history, readCall("r2", "a.go"), menu)
			if ok != tc.wantCap || hasMaxLines(got.Arguments) != tc.wantCap {
				t.Errorf("after a %s call: capped = %v (args %s), want %v", tc.name, ok, got.Arguments, tc.wantCap)
			}
		})
	}
}

// A non-read-only call in the SAME assistant message as the read may have run after it, so it voids
// the cached copy too; one issued BEFORE the read (an earlier message) does not.
func TestCacheReadOrdersANonReadOnlyCallAgainstTheRead(t *testing.T) {
	t.Parallel()
	menu := []domain.ToolDef{readFileTool, {Name: "terminal"}}
	sameMessage := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go"), shellCall("x1")),
		toolResult("r1", "package a"),
		toolResult("x1", "ok"),
		assistantCall(readCall("r2", "a.go")),
	}
	if got, ok := cacheReadWithTools(sameMessage, readCall("r2", "a.go"), menu); ok || hasMaxLines(got.Arguments) {
		t.Errorf("a shell call beside the read left the cache in force; args = %s", got.Arguments)
	}
	before := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(shellCall("x1")),
		toolResult("x1", "ok"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(readCall("r2", "a.go")),
	}
	if got, ok := cacheReadWithTools(before, readCall("r2", "a.go"), menu); !ok || !hasMaxLines(got.Arguments) {
		t.Errorf("a shell call before the read voided the cache; args = %s", got.Arguments)
	}
}

// readCallWith is a read_file call over a.go carrying extra, raw JSON argument members.
func readCallWith(id, extra string) domain.ToolCall {
	return domain.ToolCall{ID: id, Tool: "read_file", Arguments: json.RawMessage(`{"path":"a.go"` + extra + `}`)}
}

// A prior read that returned only a slice or windows of the file — a line range, a line limit, a
// locate — is not a copy of the file, so a bare re-read after it goes through whole.
func TestCacheReadLeavesReReadAfterSliceReadUntouched(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		extra string
	}{
		{"line range", `,"start_line":1,"end_line":40`},
		{"start line only", `,"start_line":200`},
		{"line limit", `,"max_lines":20`},
		{"locate", `,"locate":"func F"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			history := []domain.Message{
				userMsg("edit a.go"),
				assistantCall(readCallWith("r1", tc.extra)),
				toolResult("r1", "package a"),
				assistantCall(readCall("r2", "a.go")),
			}
			got, ok := cacheRead(history, readCall("r2", "a.go"))
			if ok || hasMaxLines(got.Arguments) {
				t.Errorf("a bare re-read after a %s read was capped; args = %s", tc.name, got.Arguments)
			}
		})
	}
}

// A full read whose result was since pruned to a stub no longer holds the file, so a bare re-read
// of it goes through whole.
func TestCacheReadLeavesReReadOfPrunedReadUntouched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "[pruned: 40 lines from read_file a.go — re-run the call if you need it]"),
		assistantCall(readCall("r2", "a.go")),
	}
	got, ok := cacheRead(history, readCall("r2", "a.go"))
	if ok || hasMaxLines(got.Arguments) {
		t.Errorf("a re-read of a pruned read was capped; args = %s", got.Arguments)
	}
}

// A bare read this guard capped keeps its bare arguments in history (prepareCall caps a local copy)
// while its result holds only a header, so it must never stand as the copy: after a pruned r1 and a
// capped-looking r2, a third read still goes through whole.
func TestCacheReadIgnoresALaterBareReadAfterAPrunedOne(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "[pruned: 40 lines from read_file a.go — re-run the call if you need it]"),
		assistantCall(readCall("r2", "a.go")),
		toolResult("r2", "a.go (40 lines, showing 1-1)"),
		assistantCall(readCall("r3", "a.go")),
	}
	got, ok := cacheRead(history, readCall("r3", "a.go"))
	if ok || hasMaxLines(got.Arguments) {
		t.Errorf("a re-read after a pruned read and a header-only read was capped; args = %s", got.Arguments)
	}
}

// A slice read before a full one does not stop the full one standing as the copy.
func TestCacheReadCapsReReadAfterSliceThenFullRead(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCallWith("r1", `,"start_line":1,"end_line":10`)),
		toolResult("r1", "package a"),
		assistantCall(readCall("r2", "a.go")),
		toolResult("r2", "package a\nfunc F() {}"),
		assistantCall(readCall("r3", "a.go")),
	}
	got, ok := cacheRead(history, readCall("r3", "a.go"))
	if !ok || !hasMaxLines(got.Arguments) {
		t.Errorf("a re-read after a full read was not capped; args = %s", got.Arguments)
	}
}
