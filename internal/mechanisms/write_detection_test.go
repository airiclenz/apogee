package mechanisms

import (
	"encoding/json"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// editCall is an edit_existing_file tool call over path — one of apogee's own edit tools, which
// semantic (b) (isFileMutatingTool) counts as a file write even though the sim-only isWriteTool
// does not. Only the path is load-bearing for the history family's write-since detection.
func editCall(id, path string) domain.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path})
	return domain.ToolCall{ID: id, Tool: "edit_existing_file", Arguments: args}
}

// openCall is an open_file tool call over path — apogee's own read tool, whose result places file
// content into the conversation exactly like read_file, so the family's read set counts it.
func openCall(id, path string) domain.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path})
	return domain.ToolCall{ID: id, Tool: "open_file", Arguments: args}
}

// A same-turn [read a.go, write a.go] supersedes the read: the next turn's read of a.go is NOT a
// redundant re-read, because the write may have changed the file (the reproduced C-02 case, with the
// sim's write_file spelling). The two-pass scan collects the write before evaluating the read, so
// order within the turn does not matter.
func TestReadRepeatInertAfterSameTurnReadThenWrite(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go"), writeCall("w1", "a.go", "package a")),
		toolResult("r1", "package a"),
		toolResult("w1", "wrote a.go"),
	}
	resp := offrampResponse(history, nil, "", readCall("r2", "a.go"))
	if d := postResponse(t, readRepeatID, resp); d.Action != "" {
		t.Errorf("Action = %q, want no action: a same-turn read-then-write supersedes the read (C-02)", d.Action)
	}
}

// A read of a.go followed by an apogee edit tool (edit_existing_file) on a.go supersedes the read the
// same way a write does: the verify-read of a.go is not a redundant re-read. This only holds because
// isFileMutatingTool counts apogee's own edit tools (the sim-only isWriteTool did not — the falsified
// NOTES claim this item fixes).
func TestReadRepeatInertAfterReadThenEdit(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(editCall("e1", "a.go")),
		toolResult("e1", "edited a.go"),
	}
	resp := offrampResponse(history, nil, "", readCall("r2", "a.go"))
	if d := postResponse(t, readRepeatID, resp); d.Action != "" {
		t.Errorf("Action = %q, want no action: an edit_existing_file supersedes the earlier read", d.Action)
	}
}

// open_file is in the family read set, so re-opening a file already opened successfully is a
// redundant re-read that read_repeat catches — pinning the read-set addition of open_file.
func TestReadRepeatFiresOnOpenFileReRead(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(openCall("o1", "a.go")),
		toolResult("o1", "File: a.go\n\npackage a"),
	}
	resp := offrampResponse(history, nil, "", openCall("o2", "a.go"))
	if d := postResponse(t, readRepeatID, resp); d.Action != domain.ActionRetry {
		t.Errorf("Action = %q, want ActionRetry: open_file counts as a read on both sides", d.Action)
	}
}

// cached_content_intercept must NOT cap a re-read of a file EDITED after its last read — the edit may
// have changed the file, so its cached copy is stale. This holds only because isFileMutatingTool
// counts edit_existing_file as a write-since.
func TestCachedContentLeavesEditedSinceUntouched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(editCall("e1", "a.go")),
		toolResult("e1", "edited a.go"),
		assistantCall(readCall("r2", "a.go")),
	}
	got := fireCached(t, history, readCall("r2", "a.go"))
	if hasMaxLines(got.Arguments) {
		t.Errorf("a re-read after an edit was capped; the file may have changed. args = %s", got.Arguments)
	}
}

// A second edit_existing_file to the same file failing the same way earns the enrichment hint — the
// edit tool is a write action, so error_enrichment acts on it (the sim-only isWriteTool would have
// skipped it entirely).
func TestErrorEnrichmentEnrichesRepeatedEditError(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("fix a.go"),
		assistantCall(editCall("e1", "a.go")),
		toolResult("e1", "syntax error: unexpected token near }"),
		assistantCall(editCall("e2", "a.go")),
	}
	result := &domain.ToolResult{CallID: "e2", Content: "syntax error: unexpected }", IsError: true}
	if !enrich(t, history, editCall("e2", "a.go"), result) {
		t.Error("a repeated same-file same-category edit_existing_file error should be enriched")
	}
}

// The successful-read-loop count is DECREMENTED by an interleaved edit_existing_file: three reads of
// a.go with an edit between them is not a re-read loop (the model acted). This pins that read_loop's
// write-decrement counts an apogee edit tool.
func TestReadLoopEditToolDecrementsSuccessfulReadCount(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		userMsg("work on a.go"),
		assistantCall(readCall("r1", "a.go")), toolResult("r1", "package a"),
		assistantCall(readCall("r2", "a.go")), toolResult("r2", "package a"),
		assistantCall(editCall("e1", "a.go")), toolResult("e1", "edited a.go"),
		assistantCall(readCall("r3", "a.go")), toolResult("r3", "package a"),
	}
	if fired, _ := fireReadLoop(t, msgs); fired {
		t.Error("an interleaved edit decrements the successful-read count; 3 reads with an edit between them is not a loop")
	}
}

// Regression pin for S1's non-extension: syntax must ignore apogee's edit tools even when the call
// carries a content field with broken code — edit payloads are fragments/patches the sim never
// syntax-checked, so semantic (a) (isWriteTool) deliberately excludes them.
func TestSyntaxIgnoresEditToolCall(t *testing.T) {
	t.Parallel()
	call := domain.ToolCall{
		ID:        "e1",
		Tool:      "edit_existing_file",
		Arguments: json.RawMessage(`{"path":"broken.go","content":"package main\nfunc main() {"}`),
	}
	if d := postResponse(t, syntaxID, responseWith(nil, call)); d.Action != "" || d.Inject != "" {
		t.Errorf("decision = %+v, want the no-op zero decision: syntax must ignore edit-tool calls", d)
	}
}

// Regression pin for S1's non-extension: autofix must likewise ignore apogee's edit tools, never
// rewriting their fragment payloads.
func TestAutofixIgnoresEditToolCall(t *testing.T) {
	t.Parallel()
	call := domain.ToolCall{
		ID:        "e1",
		Tool:      "edit_existing_file",
		Arguments: json.RawMessage(`{"path":"messy.go","content":"x = (1\n"}`),
	}
	hook := buildAutofix(t, notFound)
	if d := fireAutofix(t, hook, responseWith(nil, call)); d.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision: autofix must ignore edit-tool calls", d.Action)
	}
}
