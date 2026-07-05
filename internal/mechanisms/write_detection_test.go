package mechanisms

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// mutatingCall builds a file-mutating tool call for one of apogee's own edit tools (tool) over path.
// isFileMutatingTool — semantic (b), "did this call mutate a file / was it a write action" — counts
// these even though the sim-only isWriteTool (semantic (a), content repair) does not; only the path
// is load-bearing for the history family's write-since / progress detection. Canonical names come
// from internal/tools Name() methods (edit_existing_file, single_find_and_replace,
// multi_find_and_replace), per the S1 precedent.
func mutatingCall(id, tool, path string) domain.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path})
	return domain.ToolCall{ID: id, Tool: tool, Arguments: args}
}

// editCall is an edit_existing_file tool call over path — one of apogee's own edit tools, which
// semantic (b) (isFileMutatingTool) counts as a file write even though the sim-only isWriteTool
// does not. Only the path is load-bearing for the history family's write-since detection.
func editCall(id, path string) domain.ToolCall { return mutatingCall(id, "edit_existing_file", path) }

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

// NOTE — offramps.go:98 (wroteRecently, the tool_use_enforcer stand-down) carries NO edit-tool test
// here because the site cannot carry regression-detecting coverage. shouldEnforceToolUse ends with
// `return !hasEverUsedTools(conv)`, and hasEverUsedTools reads the same signal wroteRecently does — an
// assistant message with tool calls. The only history in which wroteRecently's edit branch could
// matter is one that contains an edit call, but that same edit makes hasEverUsedTools true, which
// forces the enforcer to stand down regardless of whether wroteRecently counts the edit. So mutating
// the isFileMutatingTool branch at :98 (e.g. to isWriteTool, dropping the edit tools) cannot flip any
// enforcer decision — a test claiming to pin it would pass under that mutation and be vacuous. See the
// plan's item-7 dated NOTES for the full rationale. The three sites below (offramps.go:149,
// toolloop.go:170, historyhints.go:106) DO discriminate the edit tools and are pinned genuinely.

// hasRecentProgress (offramps.go, the empty_response_recovery gate at offramps.go:149) counts an
// apogee edit tool as a file write, so an empty reply AFTER an edit is progress worth recovering even
// past the early-turn grace and with fewer than two distinct reads — the same branch a write_file
// would take. Without the edit the identical spinning-reads history has no progress and the off-ramp
// is inert; the edit is the only difference, so it is what drives the isFileMutatingTool write branch.
func TestEmptyResponseRecoveryTreatsRecentEditAsProgress(t *testing.T) {
	t.Parallel()
	// >3 assistant turns (past the grace) re-reading one file (fewer than two distinct paths): no
	// progress on its own, so the off-ramp is inert — the control the edit is measured against.
	spinning := []domain.Message{
		userMsg("do it"),
		assistantCall(readCall("c1", "a.go")),
		assistantCall(readCall("c2", "a.go")),
		assistantCall(readCall("c3", "a.go")),
		assistantCall(readCall("c4", "a.go")),
	}
	if d := postResponse(t, emptyResponseRecoveryID, offrampResponse(spinning, toolMenu(), "")); d.Action != "" {
		t.Fatalf("control decision = %+v, want inert: spinning reads of one file are not progress", d)
	}

	for _, tool := range []string{"edit_existing_file", "single_find_and_replace"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			withEdit := append(spinning[:len(spinning):len(spinning)],
				assistantCall(mutatingCall("e1", tool, "a.go")),
			)
			d := postResponse(t, emptyResponseRecoveryID, offrampResponse(withEdit, toolMenu(), ""))
			if d.Action != domain.ActionRetry || d.Inject != completionCheckNudge {
				t.Errorf("decision = %+v, want ActionRetry with the nudge: a recent %s is progress worth recovering", d, tool)
			}
		})
	}
}

// extractConversationContext (toolloop.go:170's write branch) counts an apogee edit tool into
// filesWritten, so the loop-breaking directive credits an edit_existing_file / single_find_and_replace
// as work already done ("You have already written: …") and steers toward the remaining work rather
// than restarting from write_file. Holds only because isFileMutatingTool counts apogee's own edit
// tools; the identical read-repeat of b.go is what trips the interceptor.
func TestToolLoopDirectiveCreditsEditToolWrite(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"edit_existing_file", "single_find_and_replace"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			history := []domain.Message{
				userMsg("update a.go"),
				assistantCall(mutatingCall("e1", tool, "a.go")),
				toolResult("e1", "wrote a.go"),
				assistantCall(readCall("r1", "b.go")),
				toolResult("r1", "package b"),
			}
			resp := offrampResponse(history, nil, "", readCall("r2", "b.go")) // repeats the previous turn's exact call
			d := postResponse(t, toolLoopInterceptorID, resp)
			if d.Action != domain.ActionRetry {
				t.Fatalf("Action = %q, want ActionRetry on the identical repeat", d.Action)
			}
			if !strings.Contains(d.Inject, "already written: a.go") {
				t.Errorf("directive = %q, want it to credit the %s write of a.go", d.Inject, tool)
			}
		})
	}
}

// writtenPaths (historyhints.go:106) counts an apogee edit tool as a successful write, so
// deriveWriteTarget excludes an edit_existing_file / single_find_and_replace-written path from the
// read-loop hint's "create X" suggestion — the suggestion always points at REMAINING work. Holds only
// because isFileMutatingTool counts apogee's own edit tools.
func TestReadLoopHintExcludesEditWrittenTarget(t *testing.T) {
	t.Parallel()
	// spec.go re-read three times without acting → the successful-read-loop hint fires and derives the
	// prompt's backtick-named target.go as the next write target.
	base := []domain.Message{
		userMsg("implement `target.go`"),
		assistantCall(readCall("r1", "spec.go")), toolResult("r1", "package spec"),
		assistantCall(readCall("r2", "spec.go")), toolResult("r2", "package spec"),
		assistantCall(readCall("r3", "spec.go")), toolResult("r3", "package spec"),
	}
	// Control: with target.go unwritten, the hint names it as the derived write target.
	if fired, hint := fireReadLoop(t, base); !fired || !strings.Contains(hint, "target.go") {
		t.Fatalf("control: fired=%v hint=%q, want the hint to derive target.go as the write target", fired, hint)
	}

	for _, tool := range []string{"edit_existing_file", "single_find_and_replace"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			edited := append(base[:len(base):len(base)],
				assistantCall(mutatingCall("e1", tool, "target.go")), toolResult("e1", "wrote target.go"),
			)
			fired, hint := fireReadLoop(t, edited)
			if !fired {
				t.Fatalf("the read loop on spec.go still fires alongside an unrelated %s", tool)
			}
			if strings.Contains(hint, "target.go") {
				t.Errorf("hint = %q, want target.go excluded: writtenPaths counts the %s, so it is not remaining work", hint, tool)
			}
		})
	}
}
