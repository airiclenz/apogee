package agent

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/floor"
	"github.com/airiclenz/apogee/internal/tools"
)

// This file re-homes the write-detection coverage pin that died with
// internal/mechanisms/writedetection_test.go (deleted in 6ba1322c). It lives in package agent
// because internal/agent is the ONE package that imports both internal/floor and internal/tools:
// floor's "never imports internal/tools" rule (internal/floor/doc.go) is untouched by this pin —
// it reaches floor's superset through the exported faces floor.IsFileMutatingTool and
// floor.FileMutatingToolNames instead of the other way round.

// workspaceWritingBuiltins names the registered built-ins whose EXECUTION mutates a named workspace
// file — internal/tools' workspaceScopedWriter set, mirrored here because that marker is unexported.
// Every one of them must be file-mutating to floor, or every caller that asks "did the model write
// a file" — the Floor guards' read cache and empty-reply recovery — treats a real write as a
// non-write, which is exactly how copy_file, move_file and delete_file went unnoticed from
// 2026-08-10 until this pin.
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
// worktree; the network tools; and the sub_agent recursion point. They belong OUT of floor's
// superset — counting them would make "the model has written a file" true for a web_search.
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

// The drift this pin closes: floor's wave4WriteTools is the single source for "did this call mutate
// a file", but nothing ties it to the tool menu it describes, so the file-operation trio was
// registered in internal/tools without ever reaching it. Walk the REGISTERED built-ins — read live
// from the registry, never a list copied into this file, so a tool added after this test was
// written (task_list, at 805fda78) cannot go unclassified — and require every write-capable one to
// be answered for: a workspace file writer floor must call mutating, or a documented non-writer it
// must not.
//
// The sim spellings in floor's superset (write_to_file, editFile, …) name no registered tool; the
// pin walks the menu, never the name set, so those stay additive. internal/floor's own table test
// is what names them.
func TestFloorWriteSupersetCoversEveryWorkspaceWritingBuiltin(t *testing.T) {
	t.Parallel()
	// Every built-in, default-off ones included: a tool registered default-off (the Console family,
	// ADR 0059) is still a tool a configured roster can lift, so the classification question has to
	// be answered for it here rather than the day someone turns it on. Lifting the whole build rung
	// by name is what makes the menu the pin walks the same set KnownToolNames spells. The three
	// host-supplied tools need non-nil delegates or the registry omits them; package agent already
	// declares the stubs (planmenu_test.go, construct_test.go).
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
			if !floor.IsFileMutatingTool(name) {
				t.Errorf("%q mutates workspace files but floor does not call it mutating; every guard that asks whether the model wrote a file would read its writes as non-writes", name)
			}
		case writeCapableNonFileBuiltins[name]:
			if floor.IsFileMutatingTool(name) {
				t.Errorf("%q mutates no named workspace file but floor calls it mutating", name)
			}
		default:
			t.Errorf("built-in %q is write-capable but classified by neither table; decide whether it mutates workspace files and add it to workspaceWritingBuiltins (and floor's superset) or to writeCapableNonFileBuiltins", name)
		}
	}
}

// FileMutatingToolNames is the superset's members and nothing else: sorted, complete, and a copy —
// a caller that appends to what it returns must not widen the guards' write detection.
func TestFileMutatingToolNamesIsASortedCopy(t *testing.T) {
	t.Parallel()

	names := floor.FileMutatingToolNames()
	if len(names) == 0 {
		t.Fatal("FileMutatingToolNames returned nothing")
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("FileMutatingToolNames is not sorted: %q before %q", names[i-1], names[i])
		}
	}
	for _, name := range names {
		if !floor.IsFileMutatingTool(name) {
			t.Errorf("FileMutatingToolNames lists %q, which IsFileMutatingTool denies", name)
		}
	}

	names[0] = "not_a_tool"
	if floor.IsFileMutatingTool("not_a_tool") {
		t.Error("writing to the returned slice reached the superset — it must be a copy")
	}
	if again := floor.FileMutatingToolNames(); again[0] == "not_a_tool" {
		t.Error("a second call sees the first call's mutation — it must be a fresh slice")
	}
}
