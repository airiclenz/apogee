package tui

import "strings"

// The /help key legend, cell by cell, in the vocabulary the prompt box's own legends use
// (prompteditor.go) so a reader meets one spelling of each gesture across the program. The cells
// that the box already advertises are spelled EXACTLY as the box spells them — the newline chord as
// idlePlaceholder / idleShiftPlaceholder do, the stop as runningPlaceholder does — and
// TestHelpNoteListsEveryVerb pins each to the legend constant it copies, so a rewording of the box
// cannot leave /help teaching a spelling the box no longer shows. The rest name gestures the box
// has no room for: the down half of recall, the autonomy-mode cycle and transcript scrolling
// (docs/manual/commands.md's keys paragraph).
const (
	helpKeySend         = "⏎ send"
	helpKeyNewline      = "⌥⏎ newline"    // the chord every terminal delivers (idlePlaceholder)
	helpKeyNewlineShift = "⇧⏎/⌥⏎ newline" // once key disambiguation is negotiated (idleShiftPlaceholder)
	helpKeyRecall       = "↑/↓ recall"
	helpKeyStop         = "esc×2 stop" // as the running legend spells it (runningPlaceholder)
	helpKeyQuit         = "⌃c quit"
	helpKeyMode         = "⇧⇥ mode"
	helpKeyScroll       = "PgUp/PgDn scroll"
)

// helpLegendPrefix opens the key line so it reads as a legend under the verb list, not as one more
// verb; helpCellSeparator joins its cells the way the prompt box joins its own.
const (
	helpLegendPrefix  = "keys: "
	helpCellSeparator = " · "
)

// helpNote is the /help transcript note: one "/name — summary" line per commandSpecs row, in table
// order, a blank line, then the key legend. It reads the registry rather than a hand-written list
// so a verb added to the table is in /help the same moment it is in the parser and the menu, and
// it reports every row — the effort-gated one included — because /help describes the program, not
// this session's menu.
//
// keyDisambiguation is the editor's flag of that name: the newline cell may claim ⇧⏎ only on a
// terminal that delivers it as anything other than a plain ⏎ (promptEditor.idleLegend has the
// reasoning), so the legend takes the same answer the box's idle legend reads and never names a
// key the box does not deliver.
func helpNote(keyDisambiguation bool) string {
	lines := make([]string, 0, len(commandSpecs)+2)
	for _, spec := range commandSpecs {
		lines = append(lines, "/"+spec.name+" — "+spec.summary)
	}
	lines = append(lines, "", helpKeyLegend(keyDisambiguation))
	return strings.Join(lines, "\n")
}

// helpKeyLegend is the note's closing line: the fixed key cells joined the way the box joins its
// legend, with the newline cell chosen by the terminal's key-disambiguation answer.
func helpKeyLegend(keyDisambiguation bool) string {
	newline := helpKeyNewline
	if keyDisambiguation {
		newline = helpKeyNewlineShift
	}
	cells := []string{
		helpKeySend,
		newline,
		helpKeyRecall,
		helpKeyStop,
		helpKeyQuit,
		helpKeyMode,
		helpKeyScroll,
	}
	return helpLegendPrefix + strings.Join(cells, helpCellSeparator)
}
