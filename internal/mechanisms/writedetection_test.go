package mechanisms

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
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

// openCall is an open_file tool call over path — the retired open_file spelling, a separate read tool
// until it merged into read_file on 2026-08-11, kept in readSpellings because models may still emit the
// name, so the family's read set counts it.
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
	resp := historyResponse(history, nil, "", readCall("r2", "a.go"))
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
	resp := historyResponse(history, nil, "", readCall("r2", "a.go"))
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
	resp := historyResponse(history, nil, "", openCall("o2", "a.go"))
	if d := postResponse(t, readRepeatID, resp); d.Action != domain.ActionRetry {
		t.Errorf("Action = %q, want ActionRetry: open_file counts as a read on both sides", d.Action)
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

// NOTE — wroteRecently (the tool-use enforcer's stand-down, internal/floor/conversation.go and
// library.go's copy of the same scan) carries NO edit-tool test because the site cannot carry
// regression-detecting coverage. shouldEnforceToolUse ends with `return !hasEverUsedTools(conv)`,
// and hasEverUsedTools reads the same signal wroteRecently does — an assistant message with tool
// calls. The only history in which wroteRecently's edit branch could matter is one that contains an
// edit call, but that same edit makes hasEverUsedTools true, which forces the check to stand down
// regardless of whether wroteRecently counts the edit. So mutating the isFileMutatingTool branch
// there (e.g. to isWriteTool, dropping the edit tools) cannot flip any enforcement decision — a test
// claiming to pin it would pass under that mutation and be vacuous. See the plan's item-7 dated
// NOTES for the full rationale. The empty-reply guard's own progress branch is pinned in
// internal/floor (emptyreply_test.go). The read_loop hint's writtenPaths site, which DID
// discriminate the edit tools and was pinned genuinely here, retired with its row in v0.20.0.

// workspaceWritingBuiltins names the registered built-ins whose EXECUTION mutates a named workspace
// file — internal/tools' workspaceScopedWriter set, mirrored here because that marker is unexported.
// Every one of them must appear in wave4WriteTools, or the whole history family (read_repeat,
// error_enrichment) treats a real write as a non-write, which is exactly how copy_file, move_file
// and delete_file went unnoticed from 2026-08-10 until this pin.
var workspaceWritingBuiltins = map[string]bool{
	"write_file":              true,
	"edit_existing_file":      true,
	"single_find_and_replace": true,
	"multi_find_and_replace":  true,
	"copy_file":               true,
	"move_file":               true,
	"delete_file":             true,
}

// writeCapableNonFileBuiltins names the registered built-ins that are write-CAPABLE (they gate
// through Approval) yet mutate no workspace file a NAME can classify: the subprocess tools, whose
// effect is whatever command the model composed; the two git tools that write .git rather than the
// worktree; the network tools; and the sub_agent recursion point. They belong OUT of
// wave4WriteTools — counting them would make "the model has written a file" true for a web_search.
var writeCapableNonFileBuiltins = map[string]bool{
	"terminal":     true,
	"python_exec":  true,
	"run_tests":    true,
	"git_branch":   true,
	"git_commit":   true,
	"web_fetch":    true,
	"http_request": true,
	"web_search":   true,
	"sub_agent":    true,
	// The Console family's write-capable half (ADR 0059): what a Console writes is whatever the
	// model typed into a live shell, so no NAME in the call classifies it — the same reason
	// terminal sits here. Its read-only half (console_read, console_close) never reaches this
	// table, which only classifies write-capable tools.
	"console_open": true,
	"console_send": true,
}

// stubAsker and stubPresenter are non-nil host delegates: the registry omits ask_user and
// present_document when either is nil, and the pin below wants the whole roster, not one Driver's.
type stubAsker struct{}

func (stubAsker) Ask(context.Context, domain.AskRequest) (domain.AskAnswer, error) {
	return domain.AskAnswer{}, nil
}

// stubSkillLookup stands in for the host's skill catalog, so load_skill (ADR 0065) is on the menu
// the pin walks — it is host-supplied like the two delegates below, and a menu missing it would be
// a tool whose write classification nobody answered.
type stubSkillLookup struct{}

func (stubSkillLookup) LookupSkill(string) domain.SkillLookupResult {
	return domain.SkillLookupResult{}
}

type stubPresenter struct{}

func (stubPresenter) Present(context.Context, domain.PresentRequest) (domain.PresentOutcome, error) {
	return domain.PresentOutcome{}, nil
}

// The drift pin defect (a) closes: wave4WriteTools is the history family's single source for "did
// this call mutate a file", but nothing tied it to the tool menu it describes, so the file-operation
// trio was registered in internal/tools without ever reaching it. Walk the REGISTERED built-ins, and
// require every write-capable one to be classified — a workspace file writer that wave4WriteTools
// must contain, or a documented non-writer it must not. A new write tool in internal/tools now fails
// this test until someone answers the question for it.
//
// The sim spellings in wave4WriteTools (write_to_file, editFile, …) name no registered tool; the pin
// walks the menu, never the map, so those stay additive.
func TestWave4WriteToolsCoversEveryWorkspaceWritingBuiltin(t *testing.T) {
	t.Parallel()
	// Every built-in, default-off ones included: a tool registered default-off (the Console family,
	// ADR 0059) is still a tool a configured roster can lift, so the classification question has to
	// be answered for it here rather than the day someone turns it on. Lifting the whole build rung
	// by name is what makes the menu the pin walks the same set KnownToolNames spells.
	menu := tools.DefaultToolsWithHost("", tools.HostTools{
		Asker:       stubAsker{},
		Presenter:   stubPresenter{},
		SkillLookup: stubSkillLookup{},
		Enabled:     tools.KnownToolNames(),
	})
	if len(menu) != len(tools.KnownToolNames()) {
		t.Fatalf("menu has %d tools, KnownToolNames %d: the pin is not walking the whole roster", len(menu), len(tools.KnownToolNames()))
	}

	for _, tool := range menu {
		name := tool.Name()
		if domain.IsReadOnly(tool) {
			if workspaceWritingBuiltins[name] {
				t.Errorf("%q is declared ReadOnly but classified as a workspace file writer", name)
			}
			continue
		}
		switch {
		case workspaceWritingBuiltins[name]:
			if !wave4WriteTools[name] {
				t.Errorf("%q mutates workspace files but is missing from wave4WriteTools; the history family would read its writes as non-writes", name)
			}
		case writeCapableNonFileBuiltins[name]:
			if wave4WriteTools[name] {
				t.Errorf("%q mutates no named workspace file but sits in wave4WriteTools", name)
			}
		default:
			t.Errorf("built-in %q is write-capable but classified by neither table; decide whether it mutates workspace files and add it to workspaceWritingBuiltins (and wave4WriteTools) or to writeCapableNonFileBuiltins", name)
		}
	}
}
