package tui

import (
	"sort"
	"strings"
)

// ----------------------------------------------------------------------------
// The skill-suggestion band (ADR 0061)
// ----------------------------------------------------------------------------

// The band is a Driver-side hint and nothing else: while the human types, the draft is ranked
// against the skill catalog by the ENGINE's matcher (skills.Catalog.Suggest) and the closest skills
// are named in one row above the input box. Nothing about the catalog reaches the model — a skill is
// prompt text only when the human invokes it with a "/token" (CONTEXT.md "Skill", ADR 0027) — so the
// band changes what the SCREEN says and never what is sent. It is the reason the matcher lives in
// internal/skills rather than here: ranking is engine work, presentation is the Driver's.

// maxSkillHints is how many skills the band names at once. It is the ROW's taste, the way
// maxQueuedRows is the staged strip's: the band is one line, and three "/id" tokens plus the legend
// still fit an eighty-column window with the ids readable. Past three the row would be a list, and a
// list the human has to read is the "/" menu's job (Tab opens it on exactly these rows).
const maxSkillHints = 3

// The band row's fixed parts. They are constants because the row is composed from pre-styled
// segments — the ids carry the prompt box's violet, everything else the band's faint text — so a
// literal spelled inline would be a literal spelled inside a style call, where a typo is invisible.
const (
	skillHintLead      = "skills: " // follows the ✦ glyph and names what the row is about
	skillHintSeparator = " · "      // between two suggested ids, the chrome's own list separator
	skillHintGap       = "   "      // sets the legend off from the ids without a second separator
	skillHintLegend    = "tab to pick"
)

// recomputeSkillHints re-derives what the band shows from the draft as it now stands. It runs on the
// EDIT path beside the autocomplete overlay ([Model.recomputeAutocomplete] folds it in), so the row
// tracks the box keystroke by keystroke: the matcher's index is built once per catalog
// (skills.Catalog.Suggest), so a rank costs a walk of the corpus and no disk, and the recompute
// needs neither a debounce nor a Cmd.
//
// It says nothing in four cases, and each is a different silence: the knob is off (the human asked
// for no band), no catalog is wired (there is nothing to suggest), a "/" or "@" overlay is open (the
// menu below the band is already answering the same question, and two answers to one question is
// noise), or the matcher itself found too little evidence in the draft to name a skill honestly —
// which is its own gate, not this function's.
//
// value is the draft as the box holds it, and what the matcher is given is that draft MINUS every
// resolving "/token" and "@ref": a "/code-audit" already in the message would otherwise match the
// code-audit skill on its own name and pin it to the top of the band it has already been invoked
// from. The ids those tokens name are excluded outright, together with the session's spent set
// ([Model.spentSkills]) — a skill shown when a message went out is not offered again.
func (m Model) recomputeSkillHints(value string) Model {
	m.skillHints = nil
	if !m.opts.SkillSuggestions || m.opts.Skills == nil || m.autocomplete.active {
		return m
	}
	invoked := map[string]bool{}
	for _, id := range extractSkillRefs(value, m.knownSkillID) {
		invoked[id] = true
	}
	spent := m.spentSkills
	m.skillHints = m.opts.Skills.Suggest(
		hintDraft(value, m.knownSkillID),
		func(id string) bool { return invoked[id] || spent[id] },
		maxSkillHints,
	)
	return m
}

// hintDraft is the draft the matcher ranks: value with the byte ranges of its resolving "/token"s
// and "@file" references cut out, each replaced by a single space so the words on either side of a
// removed token stay two words. The grammars are the ones the sent message is parsed with
// (skillRefSpans, fileRefSpans), so the band and the parser agree on what counts as a reference:
// anything the parser would NOT resolve is ordinary prose and stays in, which is what keeps a plain
// "/" or an email address from silently editing the text being matched.
//
// known is the catalog probe skillRefSpans needs; a nil probe locates no skill tokens, exactly as it
// does everywhere else in the package.
func hintDraft(value string, known func(id string) bool) string {
	spans := append(skillRefSpans(value, known), fileRefSpans(value)...)
	if len(spans) == 0 {
		return value
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })

	var b strings.Builder
	b.Grow(len(value))
	cut := 0
	for _, sp := range spans {
		if sp.start < cut { // two grammars overlapping on one token: the first cut already took it
			continue
		}
		b.WriteString(value[cut:sp.start])
		b.WriteByte(' ')
		cut = sp.end
	}
	b.WriteString(value[cut:])
	return b.String()
}

// hasSkillHints is the ONE answer to "is there a suggestion row on this frame", and both readers of
// that question go through it: the frame's row allocation, which must reserve the row
// ([Model.frameRowPlan]), and the render that paints it. They must never disagree — a granted row
// nobody paints takes the staged band's closing framing row away and leaves the group open at the
// bottom, and a painted row nobody granted overflows the frame.
//
// The knob is re-read HERE rather than trusted from the recompute because a `/settings` edit applies
// live (ADR 0037): switching the band off must take the row off the very next frame, not off the
// next keystroke.
func (m Model) hasSkillHints() bool {
	return m.opts.SkillSuggestions && len(m.skillHints) > 0
}

// renderSkillHints draws the band's one row, or "" when there is nothing to draw — no hints, or a
// frame whose row allocation could not pay for the row (bandShape). View treats the empty answer
// exactly as it treats a closed dropdown.
//
// What it returns is one line beside a seated staged queue and TWO on its own: the group above the
// input box is framed by one blank band row above it, and the staged strip draws that row when it is
// there (renderPendingInterjections). Either way the hint is the group's last row, directly above
// the box — the hint is about the draft, and the draft is in the box.
func (m Model) renderSkillHints() string {
	if !m.hasSkillHints() {
		return ""
	}
	band := m.frameRowPlan(m.openPanes()).band
	if !band.hint {
		return ""
	}
	row := m.skillHintRow()
	if band.shown == 0 && band.hidden == 0 {
		return m.queuedRow("") + "\n" + row // no staged strip above: the band draws its own frame row
	}
	return row
}

// skillHintRow composes the row itself: "  ✦ skills: /grill-me · /code-audit · /handoff   tab to
// pick" — the body indent every band row shares, the transcript's own skill glyph, the suggested ids
// as the "/id" tokens that invoke them, and the legend naming the key that opens the menu on them.
//
// The ids are painted in the PROMPT BOX's skill violet (th.skillToken) and everything else in the
// band's faint text, both on the band's black field: the row is naming tokens that will look exactly
// like this once Tab writes one into the box below it, so the colour is the continuity between the
// advice and the result rather than decoration.
//
// That is also why the row is composed from pre-styled segments and padded with a styled pad rather
// than rendered as one string at the end (the status line's posture, statusLine): wrapping a string
// that already carries styles in a second one clobbers the backgrounds the segments set, which cuts
// a notch of the terminal's own colour through the band.
//
// Every id is escape-stripped and flattened here, at the row: an id is a repo-supplied SKILL.md's
// word, an ESC byte in it would reach the terminal live AND lie to the column arithmetic that pads
// the field, and this is the one place the id becomes screen (doc.go's seam invariant).
func (m Model) skillHintRow() string {
	var b strings.Builder
	b.WriteString(m.th.queuedText.Render(bodyIndent + glyphSkill + " " + skillHintLead))
	for i, h := range m.skillHints {
		if i > 0 {
			b.WriteString(m.th.queuedText.Render(skillHintSeparator))
		}
		b.WriteString(m.th.skillToken.Render("/" + flattenField(stripEscapes(h.ID))))
	}
	b.WriteString(m.th.queuedText.Render(skillHintGap + skillHintLegend))

	// Clipped ANSI-aware to the window and then padded back out to it, both through the package's
	// width authority for the reason queuedRow states: a row composed TO the window width and then
	// painted has to be measured and cut in the method that paints it (ADR 0030 §3). The ellipsis
	// carries the band's own style, since nothing wraps the finished line to give it one.
	w := max(1, m.width)
	line := m.th.measure.Truncate(b.String(), w, m.th.queuedText.Render("…"))
	if pad := w - m.th.measure.Width(line); pad > 0 {
		line += m.th.queuedText.Render(strings.Repeat(" ", pad))
	}
	return line
}
