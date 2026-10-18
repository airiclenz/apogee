package tui

import (
	"slices"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The adoption pane's body (docs/layout/user-questions-layout.md §Adoption pane)
// ----------------------------------------------------------------------------
//
// Between the title and the rows, a repo server's or reaction's pane shows what Adoption pins (ADR
// 0104 §2–§4) VERBATIM: the values exactly as the file spells them, so the human rules on the bytes
// that will run rather than on a summary of them. The body is read in parts one blank line apart —
// the re-ask reason, what the entry runs or where it connects, what it carries (a server), each
// action (a reaction), and the pinned files — each part a run of labelled fields.
//
// The pane's own words start at column zero and no line that starts with the file's text ever
// does: a scalar follows its label on the label's line, and every list element, map line and
// continuation hangs at least two cells in, so a value crafted to look like a label reads as the
// value it is. A line too long for the pane wraps under its own hang rather than being cut, so no
// value ever ends at the pane's edge; what a value can give up is height alone — its own lines past
// argumentValueMaxLines, counted on a marker (elisionSplit) — and the popup module elides the body
// as a whole around one more marker when the window cannot seat it, the approval pane's rule.

// adoptionRunLabels are a server's fields that say what it runs or where it connects — its first
// part. Every other field it sets is what it carries, its second part.
var adoptionRunLabels = []string{"transport", "command", "args", "endpoint"}

// The labels the server parts are arranged around: the stdio transport whose child inherits only
// what env-allowlist lists, and the env field the allowlist's absence is stated after.
const (
	adoptionTransportLabel    = "transport"
	adoptionStdioTransport    = "stdio"
	adoptionEnvLabel          = "env"
	adoptionEnvAllowlistLabel = "env-allowlist"
)

// adoptionFloorOnly is what a repo stdio server with no `env-allowlist:` is shown to get (ADR 0104
// §6): the absence is itself a fact the human is ruling on.
const adoptionFloorOnly = "not set — the platform floor only"

// adoptionChangedTail ends the re-ask reason: what moved since the human last said yes.
const adoptionChangedTail = " changed since you adopted it"

// adoptionPinnedFilesLabel heads the part naming every project file whose content the pin holds.
const adoptionPinnedFilesLabel = "pinned files"

// adoptionHang is how far a continuation, an element or a map line hangs in from its label.
const adoptionHang = 2

// bodyLine is one line of the body before it is wrapped: its lead — a label, or the indent and `- `
// of an element — the text after it, and the column every continuation of it hangs at.
type bodyLine struct {
	lead string
	text string
	hang int
}

// adoptionBody is the open pane's body at its width: the head entry's parts (proposalBodyLines),
// wrapped to the pane's inner width. An Allow rule's pane, and a pane with nothing queued, has none.
func (m Model) adoptionBody() string {
	if len(m.picker.adoption) == 0 || !isStructured(m.picker.adoption[0]) {
		return ""
	}
	inner := popupInnerWidth(m.th, m.width)
	var wrapped []string
	for _, line := range proposalBodyLines(m.picker.adoption[0]) {
		wrapped = append(wrapped, wrapBodyLine(m.th, line, inner)...)
	}
	return strings.Join(wrapped, "\n")
}

// proposalBodyLines is p's body as its parts, one blank line apart: the re-ask reason, the fields
// in the parts their kind groups them into, and the pinned files.
func proposalBodyLines(p domain.Proposal) []bodyLine {
	var parts [][]bodyLine
	if len(p.Changed) > 0 {
		parts = append(parts, []bodyLine{{text: changedReason(p.Changed), hang: adoptionHang}})
	}
	parts = append(parts, fieldParts(p)...)
	if len(p.Files) > 0 {
		parts = append(parts, listField(adoptionPinnedFilesLabel, p.Files, 0))
	}
	var lines []bodyLine
	for i, part := range parts {
		if i > 0 {
			lines = append(lines, bodyLine{})
		}
		lines = append(lines, part...)
	}
	return lines
}

// changedReason is the re-ask reason: each field and then each pinned file that moved, named.
func changedReason(changed []string) string {
	names := make([]string, 0, len(changed))
	for _, name := range changed {
		names = append(names, "`"+flatText(name)+"`")
	}
	return entryNameList(names) + adoptionChangedTail
}

// fieldParts groups p's fields into the body's parts: a server's into what it runs or connects to
// and what it carries — the latter stating an unset allowlist on a stdio server — and a reaction's
// one part per action.
func fieldParts(p domain.Proposal) [][]bodyLine {
	if p.Kind == domain.ProposalReaction {
		parts := make([][]bodyLine, 0, len(p.Fields))
		for _, f := range p.Fields {
			parts = append(parts, fieldLines(f, 0))
		}
		return parts
	}
	var runs, carries []bodyLine
	for _, f := range p.Fields {
		if slices.Contains(adoptionRunLabels, f.Label) {
			runs = append(runs, fieldLines(f, 0)...)
			continue
		}
		carries = append(carries, fieldLines(f, 0)...)
		if f.Label == adoptionEnvLabel && needsFloorFact(p) {
			carries = append(carries, floorFact())
		}
	}
	if needsFloorFact(p) && !hasField(p, adoptionEnvLabel) {
		carries = append([]bodyLine{floorFact()}, carries...)
	}
	return nonEmptyParts(runs, carries)
}

// needsFloorFact reports whether p is a stdio server that lists no `env-allowlist:` — the one
// field the pane draws although the entry does not set it.
func needsFloorFact(p domain.Proposal) bool {
	if p.Kind != domain.ProposalMCPServer || hasField(p, adoptionEnvAllowlistLabel) {
		return false
	}
	i := slices.IndexFunc(p.Fields, func(f domain.ProposalField) bool { return f.Label == adoptionTransportLabel })
	return i >= 0 && slices.Equal(p.Fields[i].Values, []string{adoptionStdioTransport})
}

// hasField reports whether p sets the field label.
func hasField(p domain.Proposal, label string) bool {
	return slices.ContainsFunc(p.Fields, func(f domain.ProposalField) bool { return f.Label == label })
}

// floorFact is the line an unset allowlist on a stdio server reads.
func floorFact() bodyLine {
	return bodyLine{lead: adoptionEnvAllowlistLabel + ": ", text: adoptionFloorOnly, hang: adoptionHang}
}

// nonEmptyParts is the parts that hold a line, in order.
func nonEmptyParts(parts ...[]bodyLine) [][]bodyLine {
	kept := make([][]bodyLine, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			kept = append(kept, part)
		}
	}
	return kept
}

// fieldLines spells one field at indent by its shape: a scalar on its label's line, a list
// element by element behind `- `, a map as `key: value` lines and a group as its own fields, each
// hung one step in. Every value is bounded at argumentValueMaxLines of its own lines.
func fieldLines(f domain.ProposalField, indent int) []bodyLine {
	pad := strings.Repeat(" ", indent)
	label := flatText(f.Label)
	switch f.Shape {
	case domain.ProposalGroup:
		lines := []bodyLine{{lead: pad + label + ":"}}
		for _, sub := range f.Fields {
			lines = append(lines, fieldLines(sub, indent+adoptionHang)...)
		}
		return lines
	case domain.ProposalList:
		return listField(label, f.Values, indent)
	case domain.ProposalMap:
		var value []bodyLine
		for _, v := range f.Values {
			value = append(value, valueLines(pad+"  ", v, indent+2*adoptionHang)...)
		}
		return append([]bodyLine{{lead: pad + label + ":"}}, boundValue(value, indent+adoptionHang)...)
	}
	text := ""
	if len(f.Values) > 0 {
		text = f.Values[0]
	}
	value := valueLines(pad+label+": ", text, indent+adoptionHang)
	return boundValue(value, indent+adoptionHang)
}

// listField spells a list under its label, one element per line behind `- `; an explicitly empty
// list reads as the file spells one.
func listField(label string, values []string, indent int) []bodyLine {
	pad := strings.Repeat(" ", indent)
	if len(values) == 0 {
		return []bodyLine{{lead: pad + label + ": ", text: "[]", hang: indent + adoptionHang}}
	}
	var value []bodyLine
	for _, v := range values {
		value = append(value, valueLines(pad+"  - ", v, indent+2*adoptionHang)...)
	}
	return append([]bodyLine{{lead: pad + label + ":"}}, boundValue(value, indent+adoptionHang)...)
}

// valueLines is one value as lines: its first behind lead, and each of its own line breaks as a
// line of its own under hang — escape-stripped, because the value is a repository file's text.
func valueLines(lead, value string, hang int) []bodyLine {
	segments := splitLines(stripEscapes(value))
	lines := make([]bodyLine, 0, len(segments))
	indent := strings.Repeat(" ", hang)
	for i, segment := range segments {
		if i == 0 {
			lines = append(lines, bodyLine{lead: lead, text: segment, hang: hang})
			continue
		}
		lines = append(lines, bodyLine{lead: indent, text: segment, hang: hang})
	}
	return lines
}

// boundValue seats a value's lines in argumentValueMaxLines, head, marker and tail (elisionSplit),
// the marker hung at indent; a short-enough value comes back untouched.
func boundValue(lines []bodyLine, indent int) []bodyLine {
	head, tail, hidden := elisionSplit(len(lines), argumentValueMaxLines)
	if hidden == 0 {
		return lines
	}
	out := make([]bodyLine, 0, head+1+tail)
	out = append(out, lines[:head]...)
	out = append(out, bodyLine{lead: strings.Repeat(" ", indent), text: popupElisionMarker(hidden), hang: indent})
	return append(out, lines[len(lines)-tail:]...)
}

// wrapBodyLine wraps one body line to inner, never cutting it: the first line carries the lead,
// and every continuation hangs at the line's hang. A pane too narrow to hold the hang and a column
// of text beside it sheds the hang whole and wraps flat (hangCollapses' order, as the popup's own
// body does). Every line comes back no wider than inner, so the popup's own wrap leaves it be.
func wrapBodyLine(th theme, line bodyLine, inner int) []string {
	full := expandTabs(line.lead + line.text)
	if full == "" {
		return []string{""}
	}
	indent := len(full) - len(strings.TrimLeft(full, " "))
	if line.hang >= inner-1 || indent >= inner-1 {
		return wrapText(th, full, inner)
	}
	first := wrapText(th, full[indent:], inner-indent)
	if len(first) == 1 {
		return []string{full[:indent] + first[0]}
	}
	rest := strings.TrimPrefix(full[indent:], first[0])
	if len(rest) == len(full)-indent {
		// The wrap did not keep its first line as written, so there is no remainder to hang: the
		// flat wrap still shows every cell, which is the promise that matters.
		return wrapText(th, full, inner)
	}
	out := []string{full[:indent] + first[0]}
	hang := strings.Repeat(" ", line.hang)
	for _, ln := range wrapText(th, strings.TrimLeft(rest, " "), inner-line.hang) {
		out = append(out, hang+ln)
	}
	return out
}
