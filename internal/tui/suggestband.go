package tui

import (
	"slices"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/refs"
	"github.com/airiclenz/apogee/internal/skills"
)

// ----------------------------------------------------------------------------
// The skill-suggestion band (ADR 0061)
// ----------------------------------------------------------------------------

// The band is a Driver-side hint and nothing else: while the human types, the draft is ranked
// against the skill catalog by the ENGINE's matcher (skills.Catalog.Suggest) and the skills that
// clearly fit, up to three, are named in one row above the input box. Nothing about the catalog
// reaches the model — a skill is prompt text only when the human invokes it with a "/token"
// (CONTEXT.md "Skill", ADR 0027) — so the band changes what the SCREEN says and never what is sent.
// It is the reason the matcher lives in internal/skills rather than here: ranking is engine work,
// presentation is the Driver's.
//
// The advice is made ONCE. Every skill the row is naming at the moment a message goes out — a plain
// send or a staged interjection — is SPENT for the session and is never suggested again
// ([Model.spendSkillHints]), and only a new conversation (/clear, /new) starts that over. Until a
// message goes out the row is free to change with the draft: what the human never sent on has cost
// them nothing.

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

// skillHintDelay is how long the draft must stand still before the band re-ranks it. The rank is
// the matcher's walk of the whole catalog corpus — cheap once, but not per keystroke on a long
// draft being typed or a paste landing — so a burst of edits ranks ONCE, this long after the last
// of them (ADR 0061, 2026-09-23 amendment). It is short enough that a human who pauses sees the row
// settle before they have finished reading their own sentence.
const skillHintDelay = 150 * time.Millisecond

// skillHintTickMsg is the debounce tick an edit arms ([Model.scheduleSkillHints]). gen is the edit
// that armed it (Model.skillHintGen); a tick for an edit since superseded — or for a draft since
// spent by a send — is a no-op, exactly as ctrlCResetMsg and flashClearMsg are.
type skillHintTickMsg struct{ gen int }

// scheduleSkillHints is the band's half of the EDIT path: [Model.recomputeAutocomplete] folds it
// in, so every edit — a typed key, a paste, a splice, an undo, a withdrawn interjection put back in
// the box — reaches it. It does NOT rank. It opens a new generation, which retires any tick still
// in flight, and arms one fresh tick carrying it; the rank runs when that tick lands with its
// generation still current ([Model.foldSkillHintTick]), so a burst of edits costs one Suggest, not
// one per edit.
//
// What must not wait for the tick happens here, at once. Where the band would say nothing anyway —
// the knob is off, no catalog is wired, a "/" or "@" overlay is open, or the draft is empty — the
// row is cleared and no tick is armed, so typing with the band switched off schedules nothing at
// all. And every id the draft already invokes as a "/token" is dropped from the row as it stands
// (ADR 0061 §3: a skill already invoked in the draft is never suggested at all), which is what
// takes a Tab-accepted skill off the band the moment it is written into the box rather than one
// pause later.
func (m Model) scheduleSkillHints(value string) (Model, tea.Cmd) {
	m.skillHintGen++
	if !m.skillHintsWanted() || strings.TrimSpace(value) == "" {
		m.skillHints = nil
		return m, nil
	}
	m.skillHints = withoutInvoked(m.skillHints, refs.SkillRefs(value, m.knownSkillID))
	gen := m.skillHintGen
	return m, tea.Tick(skillHintDelay, func(time.Time) tea.Msg { return skillHintTickMsg{gen: gen} })
}

// skillHintsWanted is the recompute's own guard, named once so the edit path arms a tick exactly
// where a tick would rank: the knob is on, a catalog is wired, and no "/" or "@" overlay is open.
func (m Model) skillHintsWanted() bool {
	return m.opts.UI.SkillSuggestions && m.opts.Skills != nil && !m.autocomplete.active
}

// withoutInvoked returns hints minus every suggestion whose id is in invoked. It never edits hints
// in place — the slice header is shared by every value copy of the Model (ADR 0011) — and hands
// hints itself back when nothing is dropped, so the common case allocates nothing.
func withoutInvoked(hints []skills.Suggestion, invoked []string) []skills.Suggestion {
	if len(hints) == 0 || len(invoked) == 0 {
		return hints
	}
	var kept []skills.Suggestion
	for _, h := range hints {
		if !slices.Contains(invoked, h.ID) {
			kept = append(kept, h)
		}
	}
	if len(kept) == len(hints) {
		return hints
	}
	return kept
}

// foldSkillHintTick lands the debounce tick: a tick whose generation is still current means the
// draft has stood still for [skillHintDelay], and the band is re-ranked over it. A stale tick
// changes nothing — a later edit armed its own, or a send spent the row and retired the chain — so
// it neither ranks nor re-lays the frame.
//
// A current tick that lands while an overlay is open changes nothing either. The edit path already
// cleared the row for a "/" or "@" menu it opened, so an overlay here is the one tab opened over
// the band's own rows (openSuggestMenu), and those rows are what the band should come back with
// when the menu closes. The frame is re-laid only when the row appears or leaves, since that is all
// the band changes about the frame's row allocation ([Model.frameRowPlan]).
func (m Model) foldSkillHintTick(msg skillHintTickMsg) Model {
	if msg.gen != m.skillHintGen || m.autocomplete.active {
		return m
	}
	shown := m.hasSkillHints()
	m = m.recomputeSkillHints(m.input.Value())
	if m.hasSkillHints() != shown {
		m.layout()
	}
	return m
}

// recomputeSkillHints re-derives what the band shows from the draft as it now stands. It runs when
// the debounce tick lands ([Model.foldSkillHintTick]), not on each edit: the matcher's index is
// built once per catalog (skills.Catalog.Suggest), so a rank costs a walk of the corpus and no disk
// — once per pause in the typing rather than once per keystroke.
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
	if !m.skillHintsWanted() {
		return m
	}
	invoked := map[string]bool{}
	for _, id := range refs.SkillRefs(value, m.knownSkillID) {
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
// (refs.SkillSpans, refs.FileSpans), so the band and the parser agree on what counts as a reference:
// anything the parser would NOT resolve is ordinary prose and stays in, which is what keeps a plain
// "/" or an email address from silently editing the text being matched.
//
// known is the catalog probe refs.SkillSpans needs; a nil probe locates no skill tokens, exactly as it
// does everywhere else in the package.
func hintDraft(value string, known func(id string) bool) string {
	spans := append(refs.SkillSpans(value, known), refs.FileSpans(value)...)
	if len(spans) == 0 {
		return value
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].Start < spans[j].Start })

	var b strings.Builder
	b.Grow(len(value))
	cut := 0
	for _, sp := range spans {
		if sp.Start < cut { // two grammars overlapping on one token: the first cut already took it
			continue
		}
		b.WriteString(value[cut:sp.Start])
		b.WriteByte(' ')
		cut = sp.End
	}
	b.WriteString(value[cut:])
	return b.String()
}

// spendSkillHints retires what the band is showing right now: every id on the row is marked spent
// for the session ([Model.spentSkills]) and the row is emptied. Its callers are the two places a
// message leaves the human's hands — the send at idle ([Model.submit]) and the ⏎ that stages a row
// while a worker runs ([Model.stageInterjection]) — and nowhere else: a refusal, a mistyped "/word"
// and a "/command" line are not sends, and advice the human was given no chance to act on must still
// be given the next time it fits.
//
// Spending also retires any pending debounce tick (Model.skillHintGen): a tick armed by the last
// keystroke before the send would otherwise land after it and rank a draft that is no longer there.
//
// Spending at SEND rather than at first sight is what makes the rule honest in both directions. The
// draft is a moving thing and so is the row above it, so a suggestion that came and went while the
// sentence was being written was never really made; but the moment the message goes out, whatever
// the row was advising has had its chance, and repeating it on the next draft is no longer advice —
// it is nagging, and a row that nags is a row the human stops reading.
//
// The map is allocated on first use, so the zero-value Model needs no construction step and a
// session that never sees a suggestion never allocates one. It is emptied at the conversation
// boundary /clear and /new draw ([Model.startNewSession]) — the same boundary the transcript resets
// on, and the same reading: a fresh conversation has heard none of the old one's advice.
func (m *Model) spendSkillHints() {
	for _, h := range m.skillHints {
		if m.spentSkills == nil {
			m.spentSkills = map[string]bool{}
		}
		m.spentSkills[h.ID] = true
	}
	m.skillHints = nil
	m.skillHintGen++
}

// hasSkillHints is the ONE answer to "is there a suggestion row on this frame", and both readers of
// that question go through it: the frame's row allocation, which must reserve the row
// ([Model.frameRowPlan]), and the render that paints it. They must never disagree — a granted row
// nobody paints takes the staged band's closing framing row away and leaves the group open at the
// bottom, and a painted row nobody granted overflows the frame.
//
// The knob is re-read HERE rather than trusted from the recompute because a `/settings` edit applies
// live (ADR 0037): switching the band off must take the row off the very next frame, not off the
// next keystroke. An open dropdown is re-read for a nearer reason: tab opens the "/" menu over these
// very hints WITHOUT re-deriving them (openSuggestMenu, autocomplete.go), so the recompute's own
// overlay rule cannot answer for that menu — and a band still advising "tab to pick" underneath the
// pane tab just opened would repeat the popup's rows and name a key that no longer means what it
// says.
//
// The live states are the third re-read, and the one the recompute cannot make at all: hints are
// derived from the draft's edits, so a run that goes from idle to an approval, an ask or an error
// never passes through that path and leaves m.skillHints holding whatever the last pause ranked.
// Advice about a draft the human is no longer composing is stale by then, and it would be advising
// "tab to pick" against a key the decision surface has taken (the same set keyClaimOrder gives the
// overlays, and the same gate the tab case in handleKey answers with) — so the row stands down for
// the whole time the prompt is not the human's own, and comes back on the next frame once it is.
func (m Model) hasSkillHints() bool {
	return m.opts.UI.SkillSuggestions && m.state.live() && !m.autocomplete.active && len(m.skillHints) > 0
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
