package tools

import (
	"testing"
)

// TestClassifyEveryDefaultTool pins the blast-radius class of EVERY tool DefaultTools offers, by
// name — the ladder's whole default input in one table (contract §4). It fails in both
// directions: a default tool with no row here, and a row naming a tool the default menu no
// longer carries — so a new built-in has to state its class in the same change that adds it,
// and a marker a tool loses or gains (a writer that stops being workspace-scoped, a git read
// that stops being hardened) shows up as a moved row rather than as a silently moved ladder
// verdict. The fake-driven order test is its sibling on the ladder's side
// (internal/agent's TestClassifyTool).
func TestClassifyEveryDefaultTool(t *testing.T) {
	t.Parallel()

	want := map[string]ToolClass{
		// The self-declared read-only floor: no marker claims these, so ClassReadOnly is what
		// the declaration decides.
		"read_file":  ClassReadOnly,
		"list_dir":   ClassReadOnly,
		"grep":       ClassReadOnly,
		"find_files": ClassReadOnly,
		"view_diff":  ClassReadOnly,
		"task_list":  ClassReadOnly,
		// Apogee's own workspace-scoped writers — the four content verbs and the three
		// multi-path verbs all mint the unexported marker.
		"write_file":              ClassWorkspaceWrite,
		"single_find_and_replace": ClassWorkspaceWrite,
		"multi_find_and_replace":  ClassWorkspaceWrite,
		"edit_existing_file":      ClassWorkspaceWrite,
		"copy_file":               ClassWorkspaceWrite,
		"move_file":               ClassWorkspaceWrite,
		"delete_file":             ClassWorkspaceWrite,
		// Subprocess launchers Apogee cannot vouch for, diagnostics' read-only declaration
		// included: the bare subprocess declaration outranks it.
		"terminal":    ClassSubprocess,
		"python_exec": ClassSubprocess,
		"git_branch":  ClassSubprocess,
		"git_commit":  ClassSubprocess,
		"diagnostics": ClassSubprocess,
		"run_tests":   ClassSubprocess,
		// The hardened git read set carries the unexported readOnlySubprocess marker.
		"git_status":     ClassReadOnlySubprocess,
		"git_log":        ClassReadOnlySubprocess,
		"git_diff_range": ClassReadOnlySubprocess,
		"git_show":       ClassReadOnlySubprocess,
		// The network family routes through this package's funnel, so it is vouched for.
		"web_fetch":    ClassNetwork,
		"http_request": ClassNetwork,
		"web_search":   ClassNetwork,
		// sub_agent deliberately carries no marker and no declaration: the ladder never reads
		// its class (resolve() delegates it first, ADR 0013), and the terminal floor for an
		// unmarked, undeclared tool is the third-party writer.
		"sub_agent": ClassThirdPartyWrite,
	}

	seen := make(map[string]bool, len(want))
	for _, tool := range DefaultTools(t.TempDir()) {
		name := tool.Name()
		seen[name] = true
		wantClass, ok := want[name]
		if !ok {
			t.Errorf("default tool %q has no row in this table — state its class", name)
			continue
		}
		if got := Classify(tool); got != wantClass {
			t.Errorf("Classify(%s) = %s, want %s", name, got, wantClass)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("row %q names a tool DefaultTools no longer offers — drop the row", name)
		}
	}
}

// TestToolClass_String spells every class by its contract row name and never panics on a value
// outside the list.
func TestToolClass_String(t *testing.T) {
	t.Parallel()

	for class, name := range toolClassNames {
		if got := ToolClass(class).String(); got != name {
			t.Errorf("ToolClass(%d).String() = %q, want %q", class, got, name)
		}
	}
	if got := ToolClass(len(toolClassNames)).String(); got != "ToolClass(?)" {
		t.Errorf("out-of-range String() = %q, want %q", got, "ToolClass(?)")
	}
}
