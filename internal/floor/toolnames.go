package floor

// The tool-name families every Floor guard shares. A guard decides by asking what a tool call
// DID — was it a read, did it mutate a file — and a model spells the same concept several ways,
// so the spellings live in one place and each set composes from a family rather than copying it
// (the F8 consolidation this package inherits from internal/mechanisms).

// wave4WriteTools is the apogee-complete file-mutation superset: apogee-sim's toolsets.WriteTools
// @pin plus apogee's own write-tool spellings (edit_existing_file / single_ & multi_find_and_replace
// and the file-operation trio copy_file / move_file / delete_file), so a guard's write detection
// sees apogee's real menu rather than the sim's. Its apogee half is exactly internal/tools'
// workspaceScopedWriter set — the built-ins whose execution mutates a NAMED workspace file; the sim
// spellings above it are additive and name no registered apogee tool.
var wave4WriteTools = map[string]bool{
	"write_file": true, "writeFile": true, "write_to_file": true, "create_file": true,
	"edit_file": true, "editFile": true, "replace_in_file": true,
	"edit_existing_file": true, "single_find_and_replace": true, "multi_find_and_replace": true,
	"copy_file": true, "move_file": true, "delete_file": true,
}

// readSpellings is the read-tool SPELLING family: one tool concept written every way apogee's real
// menu can present it (apogee-sim toolsets.ReadTools @pin — read_file / readFile — plus apogee's own
// open_file, a RETIRED tool name merged into read_file on 2026-08-11 and kept because a model may
// still emit it). Every read set in this package composes from it through toolSet, so a newly
// supported spelling is added in ONE place.
var readSpellings = []string{"read_file", "readFile", "open_file"}

// toolSet unions one or more spelling groups into a name→true membership set — the composition seam
// that lets a set name the families it draws from instead of copying their spellings.
func toolSet(groups ...[]string) map[string]bool {
	set := make(map[string]bool)
	for _, g := range groups {
		for _, name := range g {
			set[name] = true
		}
	}
	return set
}

// readToolNames are the tools whose calls count as a file read wherever a guard measures reading —
// recent progress, a repeated read, a cache hit. It composes from readSpellings so the guards'
// read sets stay identical by construction rather than by hand-maintained copies.
var readToolNames = toolSet(readSpellings)

// isReadTool reports whether name is one of the file-reading tools progress detection counts.
func isReadTool(name string) bool { return readToolNames[name] }

// isFileMutatingTool reports whether a call to name mutated a file — "was this a write action",
// over the apogee-complete superset above. It is deliberately NOT "does this call carry a full file
// payload to syntax-check": a fragment edit and a move mutate the workspace while carrying no file
// body, and only the content-repair rows (which stay lab Mechanisms) need the narrower question.
func isFileMutatingTool(name string) bool { return wave4WriteTools[name] }
