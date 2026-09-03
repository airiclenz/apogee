package mechanisms

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// NOTE — wroteRecently (the tool-use enforcer's stand-down, internal/floor/conversation.go and
// library.go's copy of the same scan) carries NO edit-tool test because the site cannot carry
// regression-detecting coverage. shouldEnforceToolUse ends with `return !hasEverUsedTools(conv)`,
// and hasEverUsedTools reads the same signal wroteRecently does — an assistant message with tool
// calls. The only history in which wroteRecently's edit branch could matter is one that contains an
// edit call, but that same edit makes hasEverUsedTools true, which forces the check to stand down
// regardless of whether wroteRecently counts the edit. So mutating the isFileMutatingTool branch
// there (e.g. narrowing it to the sim-only write spellings) cannot flip any enforcement decision — a test
// claiming to pin it would pass under that mutation and be vacuous. See the plan's item-7 dated
// NOTES for the full rationale. The empty-reply guard's own progress branch is pinned in
// internal/floor (emptyreply_test.go). The read_loop hint's writtenPaths site, which DID
// discriminate the edit tools and was pinned genuinely here, retired with its row in v0.20.0.

// workspaceWritingBuiltins names the registered built-ins whose EXECUTION mutates a named workspace
// file — internal/tools' workspaceScopedWriter set, mirrored here because that marker is unexported.
// Every one of them must appear in wave4WriteTools, or every caller that asks "did the model write
// a file" — library.go's narration check, the internal/floor guards' own copy of the set — treats a
// real write as a non-write, which is exactly how copy_file, move_file and delete_file went
// unnoticed from 2026-08-10 until this pin.
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
