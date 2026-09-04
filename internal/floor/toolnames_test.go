package floor

import "testing"

// The two predicates answer different questions and must not drift into one: isReadTool asks "did
// this call read a file", isFileMutatingTool "did this call mutate one". The 2026-08-10 trio
// (copy_file / move_file / delete_file) is the sharpest case — moving or deleting a file mutates
// the workspace, and a guard that missed it would read a real write as a non-write. The table names
// every member of wave4WriteTools, sim spellings included, so dropping one from the map fails here
// and not only in the cross-package coverage test (internal/agent/writedetection_test.go), which
// walks the registered roster and so cannot see a lost sim spelling at all.
func TestToolNamePredicates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tool     string
		read     bool
		mutating bool
	}{
		{tool: "read_file", read: true},
		{tool: "readFile", read: true},            // the sim spelling
		{tool: "open_file", read: true},           // retired name a model may still emit
		{tool: "write_file", mutating: true},      // the sim spelling both families know
		{tool: "writeFile", mutating: true},       // the sim camelCase spelling
		{tool: "write_to_file", mutating: true},   // sim @pin, no registered apogee tool
		{tool: "create_file", mutating: true},     // sim @pin, no registered apogee tool
		{tool: "edit_file", mutating: true},       // sim @pin, no registered apogee tool
		{tool: "editFile", mutating: true},        // sim @pin, no registered apogee tool
		{tool: "replace_in_file", mutating: true}, // sim @pin, no registered apogee tool
		{tool: "edit_existing_file", mutating: true},
		{tool: "single_find_and_replace", mutating: true},
		{tool: "multi_find_and_replace", mutating: true},
		{tool: "copy_file", mutating: true},
		{tool: "move_file", mutating: true},
		{tool: "delete_file", mutating: true},
		{tool: "list_dir"}, // neither
		{tool: "terminal"}, // effects are the model's command, not a named read or write
	}

	var named int
	for _, c := range cases {
		if c.mutating {
			named++
		}
	}
	if want := len(FileMutatingToolNames()); named != want {
		t.Errorf("the table names %d mutating tools, wave4WriteTools has %d — name them all", named, want)
	}

	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			t.Parallel()

			if got := isReadTool(c.tool); got != c.read {
				t.Errorf("isReadTool(%q) = %v, want %v", c.tool, got, c.read)
			}
			if got := isFileMutatingTool(c.tool); got != c.mutating {
				t.Errorf("isFileMutatingTool(%q) = %v, want %v", c.tool, got, c.mutating)
			}
			if got := IsFileMutatingTool(c.tool); got != c.mutating {
				t.Errorf("IsFileMutatingTool(%q) = %v, want %v — the exported face must not diverge", c.tool, got, c.mutating)
			}
		})
	}
}

// toolSet unions the groups it is handed, which is what lets a set name a spelling family instead
// of copying its members.
func TestToolSetUnionsItsGroups(t *testing.T) {
	t.Parallel()

	set := toolSet([]string{"a", "b"}, []string{"b", "c"})

	for _, name := range []string{"a", "b", "c"} {
		if !set[name] {
			t.Errorf("toolSet has no %q", name)
		}
	}
	if len(set) != 3 {
		t.Errorf("toolSet has %d names, want 3 — duplicates must collapse", len(set))
	}
}
