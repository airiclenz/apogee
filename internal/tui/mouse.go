package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// Mouse support: click-to-position + drag-to-select in the prompt AND transcript (layout.md)
// ----------------------------------------------------------------------------
//
// The terminal does its own click-drag text selection only while no application captures the
// mouse. apogee captures it (MouseModeCellMotion, set in View — MouseModeAllMotion while the
// pointer-shape gate is open, pointerprobe.go) so the wheel can scroll the
// transcript and a click can position the caret — which is exactly why the terminal's own
// selection is off. So selection is implemented here, on top of the widgets' public API: no
// copy of the widgets' internal wrapping is needed, which keeps the geometry correct across
// releases.
//
// Two rectangles, two selection models. The prompt (textarea) selection carries rune offsets
// into the source Value, so it copies the exact typed text. The transcript (viewport) selection
// is screen-space — "copy what you see": it anchors in content coordinates (rendered-line index
// + display cell) and slices the cached rendered lines on release, so markers, rail gutters, and
// the painter's own wrap breaks are copied verbatim (the accepted terminal-native semantics, D4) —
// and verbatim is exact here, because the viewport no longer wraps: a rendered line is a row
// (newModel, model.go). The one row not copied verbatim is a prompt block's ▄/▀ half row: it copies
// as the blank separator it stands in for, or as nothing where the paint added it, and is never
// shaded, so the clipboard holds what the unpadded transcript would (transcriptSelectionText). The mouse handlers arbitrate by region — a point in the input rect drives
// the editor, a point in the viewport drives the transcript — so the two selections never coexist.
//
// A transcript selection SURVIVES a repaint by the keep-if-unchanged rule (transcriptSel.
// spanUnchanged, applied in refreshViewport): it lives on exactly while every rendered line it
// spans is identical before and after. The transcript is append-only at a fixed width, so a drag
// over settled text keeps extending while the model streams new lines beneath it — and the moment
// the text under the span does move (the streaming tail growing, a rewrap on resize, a tool call
// joining its group) the selection drops rather than pointing at something else. That is what
// makes copy equal sight: the release slices the very lines the rule protected. A COLLAPSED span is
// exempt and survives every repaint — it shades nothing, so it is a click in progress rather than a
// selection, and the release still needs the line the press named to toggle a block (toggleBlockAt);
// without the exemption a live block's blinking star rewrote its own header line out from under the
// press and the toggle was lost.
//
// Screen rows become content lines through ONE mapping, Model.contentLineAt (model.go), which the
// mouse and the highlight both read. It is overlay-aware at BOTH ends of the transcript, and that
// is what keeps copy equal to sight rather than merely close to it:
//
//   - At the top, the sticky header draws the owning prompt over the viewport's first rows, so
//     those rows name the header's own lines, not the reply lines the scroll offset hides beneath
//     them. A drag that starts on the header and runs down into the reply therefore spans the
//     content between the two — the lines the header covers included, since a screen-space span
//     stays a contiguous run of content lines. The header's ▄/▀ half rows are the exception to
//     "copy what you see": a drag starting on the ▄ copies from the ❯ row, and one crossing a
//     half row copies the separator it stands in for (transcriptSelectionText).
//   - At the bottom, the approval and ask popups, the /sessions browser, the picker, the
//     autocomplete dropdown and the staged-interjection strip take their rows OFF the transcript
//     (Model.transcriptRows measures what is left; View composes the frame from it). Those rows map
//     to no content line at all, so a click on a popup border or a session row arms no transcript
//     selection — the alternative, bounding by the height layout() stored, addressed reply lines
//     that were not on screen. The
//     WHEEL reads the same rows the same way and gives the same answer: a notch on them belongs to
//     the pane that painted them, so it walks that pane's own list instead of scrolling a transcript
//     that is not there (foldMouseWheel below routes all seven panes; the /settings pane takes the
//     whole budget rather than a slot at the bottom, and is in that chain too). The CLICK follows
//     the notch through the same panes in the same order (handleMouseClick below), so a row a notch
//     would walk is a row a click can highlight, and what a click OUTSIDE a pane means is that
//     chain's one rule, stated on handleMouseClick.

// cell is an absolute visual position inside the textarea content: row counts wrapped (visual)
// lines from the top of the value; col is the display column within that row.
type cell struct{ row, col int }

// fieldSel is an editable field's drag-selection — the prompt's ([Model.sel]) and the /settings
// pane's edit field's ([settingsPane.sel]) — and the one protocol both surfaces keep it by: a press
// seats it (seat), a drag moves its moving end (extend), and the release that copies it, the
// highlight that paints it and the delete keys that cut it all read it through span, nonEmpty and
// taken, so the two fields cannot disagree about what a selection is.
//
// It carries the same span two ways: rune offsets into the field's value (anchorOff/headOff — what
// gets copied, so real newlines survive and soft-wraps do not) and absolute visual cells
// (anchorVis/headVis — what gets highlighted, derived straight from the mouse so no wrap math is
// needed to draw it). Only the prompt draws from the cells; the /settings field carries the offsets
// alone and derives its columns at paint time (settingsPane says why), so its seat and extend pass
// the zero cell. anchor is the drag's fixed end (set on press); head is the moving end (updated on
// drag). The zero value is none.
type fieldSel struct {
	active             bool
	anchorOff, headOff int
	anchorVis, headVis cell
}

// seat arms a collapsed selection at off — a press: a caret placed by the pointer, not yet a span.
// vis is the visual cell the press landed on (the zero cell on a surface that does not draw from it).
func (s *fieldSel) seat(off int, vis cell) {
	*s = fieldSel{active: true, anchorOff: off, headOff: off, anchorVis: vis, headVis: vis}
}

// extend moves the selection's head to off (and vis) as a drag goes on; the anchor stays where the
// press seated it.
func (s *fieldSel) extend(off int, vis cell) {
	s.headOff, s.headVis = off, vis
}

// span is the selection's rune range in reading order: lo <= hi whichever way the drag ran, since a
// right-to-left drag stores its head before its anchor.
func (s fieldSel) span() (lo, hi int) {
	lo, hi = s.anchorOff, s.headOff
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

// nonEmpty reports whether the selection is one the human can SEE: active and not collapsed. A
// collapsed one is a click in progress — a caret, not a selection — so it shades nothing, copies
// nothing and cuts nothing; this is the one predicate the highlight, the release and the delete keys
// all ask.
func (s fieldSel) nonEmpty() bool {
	return s.active && s.anchorOff != s.headOff
}

// taken is the text the selection takes out of value: the exact runes of its span, clamped to the
// value (selectionText), and "" for a selection that is not nonEmpty.
func (s fieldSel) taken(value string) string {
	if !s.nonEmpty() {
		return ""
	}
	lo, hi := s.span()
	return selectionText(value, lo, hi)
}

// contentCell is an absolute position in the rendered transcript: line indexes into the cached
// m.lines and col is the display column within that line. The transcript selection anchors in
// these content coordinates (not screen coordinates) so it survives a wheel-scroll mid-drag —
// the scroll moves what is on screen, not the line the anchor names.
type contentCell struct{ line, col int }

// transcriptSel is the transcript's drag-selection in content coordinates. anchor is the drag's
// fixed end (set on press); head is the moving end (updated on drag). The zero value is none.
// Unlike fieldSel it stores no rune offsets: the copied text is sliced from the rendered lines
// (screen-space), not from a source string.
type transcriptSel struct {
	active       bool
	anchor, head contentCell
}

// spanUnchanged reports whether the selection's ground held still across a re-render: every line
// it spans is identical in the outgoing lines and the incoming ones. It is the keep-if-unchanged
// rule refreshViewport decides a selection's fate by — true keeps it, false drops it.
//
// An inactive selection has nothing to keep, so it reports false. An active COLLAPSED one
// (anchor == head) is kept whatever moved beneath it, and that exemption is this rule's own purpose
// read literally: the drop exists so a highlight never stands over text that has since been
// rewritten, and a collapsed span paints no highlight at all — shadeCells returns on exactly that
// test. Collapsed, it is not a selection but a CLICK IN PROGRESS: the press's record of the line
// the human pressed on, which the release consumes to toggle a block (handleMouseRelease →
// toggleBlockAt). Dropping it threw that answer away — a live block's header alternates ✦ with a
// bare cell on the spinner phase (blockState.star), rewriting its own line twice a second, so a
// press on a running tool's header was zeroed before the button came up and the toggle never
// fired. A kept
// anchor may outlive the paint it was taken in — a collapse elsewhere can leave it past the last
// marked line — and that costs nothing: toggleBlockAt's bounds check makes a stale line a no-op.
//
// A DRAGGED span is judged exactly as before. One reaching past either slice reports false: the
// lines it named are no longer all there, which is a change by any reading. Only the LINE range is
// normalised (anchor above head or below it names the same rows), and the columns need no re-check
// of their own — identical lines have identical widths, so a column that was valid still is.
func (s transcriptSel) spanUnchanged(oldLines, newLines []string) bool {
	if !s.active {
		return false
	}
	if s.anchor == s.head {
		return true // a click in progress: it shades nothing, so nothing under it can go stale
	}
	top, bot := s.anchor.line, s.head.line
	if bot < top {
		top, bot = bot, top
	}
	if top < 0 || bot >= len(oldLines) || bot >= len(newLines) {
		return false
	}
	for i := top; i <= bot; i++ {
		if oldLines[i] != newLines[i] {
			return false
		}
	}
	return true
}

// flashClearMsg clears the transient status-line note (m.flash) once flashDuration elapses. gen is
// the flash that scheduled it (Model.flashGen); a clear for a flash since replaced is a no-op.
type flashClearMsg struct{ gen int }

// showFlash puts note in the status line's flash slot under a fresh generation and returns the
// tick that clears it after flashDuration — cleared only if no later flash has replaced it.
func (m *Model) showFlash(note string) tea.Cmd {
	m.flash = note
	m.flashGen++
	gen := m.flashGen
	return tea.Tick(flashDuration, func(time.Time) tea.Msg { return flashClearMsg{gen: gen} })
}

// flashDuration is how long a flash — a mouse-copy confirmation, a refusal raised inside a run
// view — lingers in the status line.
const flashDuration = 2 * time.Second

// inputEditable reports whether the prompt is live for the human to edit — the states in which a
// keypress reaches the textarea and a mouse click positions the caret. Editability IS the rule,
// and which states are editable is [uiState.editable]'s to say (model.go); what this adds is the
// focus half. Where it does not hold the box is inert — a/d/s and Enter-dismiss own the keyboard
// at an approval, ⏎ dismisses at errored — so clicks fall through to the transcript arbitration.
func (m Model) inputEditable() bool {
	return m.state.editable() && m.input.Focused()
}

// inputContentRect returns the textarea text area's on-screen rectangle: the top-left cell
// (x0,y0) and its width and height in cells. The input box is bottom-anchored above the footer's
// single line and the ▁ hairline under it (View stacks the flexible viewport above them), so the
// rectangle follows from the window height and the box's own height without tracking the overlays
// that float above it.
//
// boxTop is stated in the LAYOUT's own constants rather than in literals, so the mapping cannot
// drift from the arithmetic transcriptBudget spends: the box occupies its content rows plus
// inputBorderRows (it closes its own rounded frame), and what stands below it is the footer line
// and the bottom hairline. Every one of the three is named here — an omitted term is exactly the
// off-by-one that puts the caret on the wrong row.
func (m Model) inputContentRect() (x0, y0, w, h int) {
	h = m.input.Height()
	boxTop := m.height - bottomRuleHeight - footerHeight - (h + inputBorderRows) // the box's top border row
	y0 = boxTop + 1                                                              // the first text row sits below that border
	x0 = borderFrame/2 + inputPadding/2                                          // one border column + one padding column = 2 in from the left
	w = m.inputInnerWidth()
	return x0, y0, w, h
}

// pointInputRow maps a screen point to a visual position inside the textarea content. ok is
// false when the point is above or below the text rows (so a drag that strays out of the box
// vertically is ignored). A point left or right of the text clamps to the row's ends, so a drag
// past the edge still selects to the line boundary. ScrollYOffset folds in the textarea's own
// vertical scroll, so the mapping holds even for a prompt taller than the box.
func (m Model) pointInputRow(x, y int) (visRow, visCol int, ok bool) {
	x0, y0, w, h := m.inputContentRect()
	if h < 1 || y < y0 || y >= y0+h {
		return 0, 0, false
	}
	visRow = m.input.ScrollYOffset() + (y - y0)
	visCol = clampInt(x-x0, 0, w)
	return visRow, visCol, true
}

// pointTranscriptRow maps a screen point to a content coordinate in the rendered transcript: the
// line index into m.lines and the display cell within it. The viewport is top-anchored at the
// screen origin (View stacks it first) and its content spans whatever width layout() gave it: the
// window less the scroll-bar gutter while the bar is shown (ui.show-scrollbar), the whole window
// width once it is hidden and the column goes back to the body. The column bound below is therefore
// ASKED of the viewport rather than restating either arithmetic, so the mapping holds in both
// states. ok is false when the point falls outside the transcript's rows or past the last rendered
// line — a session shorter than the viewport leaves the rows beneath its tail empty, and a click
// there names no content at all, so it selects nothing. contentLineAt (model.go) resolves the row, folding in the scroll offset, the
// sticky-header overlay and the rows this frame's overlays took off the bottom
// ([Model.transcriptRows]) — so a click on a header row names the prompt line drawn there rather
// than the reply line hidden beneath it, a click on a popup row names nothing at all, and the
// mapping holds at any scroll position.
//
// The vertical bound is that same derivation and NOT the stored viewport height, which is the whole
// of what makes the arbitration honest: the height layout() stored is the transcript's only while
// no overlay is open, and the rows between the drawn transcript and the input box are painted by an
// approval popup or the /sessions browser. Bounding by the stored height let a click there arm a
// selection over the reply lines the popup covers — invisible text, copied to the clipboard on
// release.
//
// The column needs no conversion, and that is a CONSEQUENCE, not a coincidence. The terminal
// reports x as a PAINTED cell index — the column the glyph under the pointer occupies on screen —
// while the returned col indexes the rendered line by display cell for the cuts downstream
// (transcriptSelectionText, shadeCells). Those two spaces coincide exactly while the display-width
// authority measures the way the painter paints (width.go), which is the whole of what makes it an
// authority; the transcript body starts at screen column 0, so the identity holds outright. It is
// the CUTS that have to speak the authority — measure the line in one method and slice it in
// another and the pointer names one glyph while the clipboard takes its neighbour.
func (m Model) pointTranscriptRow(x, y int) (line, col int, ok bool) {
	line = m.contentLineAt(y) // −1 off the transcript's own rows, overlay rows included
	if line < 0 || line >= len(m.lines) {
		return 0, 0, false
	}
	col = clampInt(x, 0, m.viewport.Width())
	return line, col, true
}

// clickArm is the row one boxed pane's click handler highlighted with the POINTER: which pane it was,
// which of that pane's rows, the first row of the window that pane was showing when the click landed,
// and whether anything is armed at all. It is the Model field of the same name (model.go), whose doc
// states the rule; the type is here because the click chain is its only author and its only reader.
//
// top is what keeps the rows still under the pointer (pin): the pane goes on opening its window at the
// row it opened at before the click, rather than re-centring on the row just highlighted, so a second
// click at the same screen position lands on that same row.
type clickArm struct {
	pane framePane
	row  int
	top  int
	ok   bool
}

// holds reports whether row on pane is the row the POINTER armed — the one thing an activating click
// is allowed to act on. Every pane's click handler asks it rather than comparing the three fields
// itself, so "the pointer highlighted this" has one spelling in the package.
func (a clickArm) holds(pane framePane, row int) bool {
	return a.ok && a.pane == pane && a.row == row
}

// pin is the row window pane's spec opens at while selected is the row the POINTER armed on it
// (popupSpec.rowTop, popupSpec.pinTop): the pre-click window's first row, and pinned true. Anywhere
// else — nothing armed, another pane, a highlight a key has since moved — it pins nothing and the pane
// windows around its cursor. Every spec that paints a clickable list asks it, so a click keeps the rows
// still on every pane alike.
func (a clickArm) pin(pane framePane, selected int) (top int, pinned bool) {
	if !a.holds(pane, selected) {
		return 0, false
	}
	return a.top, true
}

// pointerPanes is the click chain and the wheel chain in ONE order, the order the two gestures have
// always been asked in — the CLICK-CHAIN order, which is NOT the slot's stacking order (the
// framePane order, model.go) — so a row a notch would walk is a row a click can highlight, and a
// pane added later is asked by both gestures the day its row is given a pointer rank. What each pane
// DOES with a click or a notch is its row of the pane table (paneSpecs, panes.go: the click and wheel
// funcs, in the currencies [paneSpec] states), and WHEN it is asked is that row's pointer rank
// ([paneRank]); this list is derived from those ranks in panes.go's init() ([panesRankedBy]) — never by
// a declaration-time initializer, which would run before the table is filled and read zero rows — and
// it is a list of panes, not of funcs.
//
// The order: the /settings pane is asked FIRST because it is the frame's one full-height pane, drawn
// over the transcript for exactly its own rows. The four reports — /usage, /inspect, /thinking,
// /advice — are asked next, in the order the slot draws them, because the reports are the only panes of that slot
// that can be up TOGETHER (View): a click on the /inspect pane dismisses the /usage report before it
// reaches it, and what makes that safe is the pre-click frame, not the geometry (reportpane.go). The
// /sessions browser opens the MODAL half of the chain, asked first of the modals because it is the
// top rung of the overlay precedence (keyClaimOrder, model.go): where it is up it is what the human
// is looking at. The /model | /server picker is the rung below it, and the /workflows view below that
// — the two are never up together, since each owns ⏎ while it is. The prompt slot — the ask pane
// and the approval pane beside it, which share panePrompt's rectangle — is asked next: while a
// question is up it claims what lands INSIDE its box, and a click outside leaves the question standing
// and travels on. The "/" | "@" dropdown closes the chain: it is the input slot's OWN tenant, drawn
// flush over the box rather than over the transcript, and the one list that is not modal at all.
// Wherever two of these panes can never share a frame the order between them is arbitrary, because
// only one rectangle can hold the pointer at a time; what is never arbitrary is that every pane is
// asked before the footer, the prompt and the transcript below.
var pointerPanes []framePane

// settingsPointerClick is the /settings pane's click, the one its row of the pane table names
// (panes.go): [Model.handleSettingsClick], plus the one thing
// the chain does on its behalf when it does NOT claim the click — its highlight goes, as the other two
// selections would. The converse is the pane's own (a claimed click drops the prompt's and the
// transcript's spans); this half is the one a live selection makes silent, because a settings span
// left armed would keep answering every motion and every release taken elsewhere on the frame.
func settingsPointerClick(m, pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
	next, cmd, claimed := m.handleSettingsClick(pre, msg)
	if !claimed {
		next.settings.sel = fieldSel{}
	}
	return next, cmd, claimed
}

// promptPointerClick is the prompt slot's click, the one panePrompt's row of the pane table names
// (panes.go): the ask pane ([Model.handleAskClick]) and the approval
// pane ([Model.handleApprovalClick]) share panePrompt's rectangle ("the approval or the ask prompt",
// model.go) and answer a click the same way, for the same reasons. Which of the two is up is a question
// of STATE rather than of geometry, so the two are asked one after the other rather than arbitrated
// between here — and a pane that is not up returns the model untouched, so the order is a formality.
func promptPointerClick(m, pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
	if ask, cmd, claimed := m.handleAskClick(pre, msg); claimed {
		return ask, cmd, true
	}
	return m.handleApprovalClick(pre, msg)
}

// handleMouseClick starts a fresh, collapsed selection under a left-click. It arbitrates by
// region: a click on the open /settings pane's row list belongs to the pane (selecting a row, or
// seating the caret in the row being typed into); a click on the footer's mode marker opens the mode
// picker ([Model.handleFooterModeClick]) and selects nothing; a click in the prompt's editable text area
// positions the caret and arms a prompt selection there; otherwise a click in the transcript viewport
// arms a transcript selection at that rendered cell; a click in none of them clears all three. Starting
// one selection clears the others, so no two coexist. Non-left buttons are ignored.
//
// The pane is asked FIRST because it is drawn over the transcript, and only for its own rows: it takes
// no more of the frame than its list needs, so the transcript above it keeps its pointer.
//
// A click FOLLOWS THE NOTCH. Region arbitration is the wheel's doctrine spelled for the other gesture
// — the pane under the POINTER owns it, not the pane with focus and not the topmost modal — and the
// chain below asks the same panes in the same order foldMouseWheel does, off the same rectangles and
// through the same shared hit-test (popupPaneHit, over the placement the painter itself returned). So
// the row a notch would walk onto is the row a click highlights, and on every pane that ASKS something
// a second click on that same row is the ⏎ the highlight was already offering: never one click, because
// the row an activating click may take is the row the POINTER armed (clickArm above) and a pane's own
// default highlight was never put there by the human. What differs from the notch is only the answer to
// a point outside every box — the wheel has no dismissal to spend there — and that is the one rule
// stated below, for the whole chain at once.
//
// Every GEOMETRY question the chain asks is put to the PRE-CLICK frame — pre below, the Model value as
// it stood when the button went down (the Model is a value, ADR 0011, so the copy is the snapshot),
// carrying that frame's published spans so the chain composes it once and reads it thereafter — while
// every state predicate and every mutation runs on the live model. The distinction is load-bearing
// because a dismissal re-lays the frame under the very click that caused it: the transcript-side slot is
// bottom-anchored (frameOverlays.transcriptRows), so dropping the /usage report grows the /inspect pane
// UPWARD, and the transcript grows back down into whatever neither pane takes. Asked of the mutated
// model, the next rect in the chain is a frame the human never clicked on. So a click landing where a
// dismissed pane WAS drawn only dismisses: it is never claimed by the box that grew there, and it never
// names a transcript row the pre-click frame did not show at that Y.
//
// Every handler in the chain answers in the same three-part currency — the Model, a tea.Cmd, and
// whether it CLAIMED the click — and the Cmd of the claiming handler is the one this returns. The
// pointer acts where the keyboard acts, and the keys on these panes answer questions: taking an
// approval decision, submitting an answer, loading a session (approval.go, ask.go, sessions.go) all
// hand back work for the runtime to do, so a chain that could only answer with a Model would have to
// drop it.
//
// What a boxed pane does with a click OUTSIDE its own box is ONE rule for the whole chain, stated here
// rather than re-decided in each handler (call C, owner 2026-09-06, narrowed by the owner the same
// day). Whatever the pane is, no outside click may CANCEL its question, dismiss the pane where esc
// would stop a run, or stop the run itself: those meanings are esc's, and a stray of the pointer's
// never carries them. Beyond that a pane picks between the two answers the currency already has. A
// pane with nothing to decide DISMISSES, and what it does with the click AFTER that is the pane's
// kind: the reports do not claim, so the click goes on to whatever it named, while the two
// MODAL lists — the picker and the /sessions browser — dismiss AND CLAIM, because a modal is what the
// human was looking at and the click that closed it is spent on closing it. The "/" | "@" DROPDOWN
// dismisses and CONTINUES, the reports' answer, because it is the one list that is not modal at
// all: it hangs over a chat box the human is still typing in, so the click that closed it is very
// often the click that seats the caret in that box — and it decides nothing, the next keystroke in
// the box deriving it back. A DECISION pane — the
// ask prompt, and the approval prompt beside it — keeps
// standing and simply does not claim, so the click FALLS THROUGH to the footer, the prompt and the
// transcript below. The fall-through is not a detail: [Model.inputEditable] promises the prompt to the
// ask state, and a pane that swallowed every outside click would take the caret seat and the
// transcript drag away from the human for as long as its box was up.
func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	m.mousePressed = true // the press latch is armed here and cleared by the release, whatever the click goes on to claim
	// The frame the click was aimed at, carrying its geometry with it: the composition happens once
	// here, and every rect below is a READ of it rather than a fresh render of every overlay
	// ([Model.withFrameSpans], model.go).
	pre := m.withFrameSpans()
	// The panes, in the one order both gestures ask them in (pointerPanes), each answering with its
	// row's click (paneSpecs). A pane that does not claim the click hands the model on — dismissed
	// where its currency says so — and the next pane's rect is still read off pre, never off the model
	// a dismissal has moved.
	for _, p := range pointerPanes {
		next, cmd, claimed := paneSpecs[p].click(m, pre, msg)
		if claimed {
			return next, cmd
		}
		m = next
	}
	// The footer's mode marker is the frame's one CHROME control ([Model.handleFooterModeClick]): a
	// click on it opens the mode picker. It is asked after the panes that draw OVER the transcript —
	// they can cover any row, the footer's included, and a click on a pane belongs to the pane — and
	// before the prompt and transcript rects, which own the rows above it.
	if footer, cmd, claimed := m.handleFooterModeClick(pre, msg); claimed {
		return footer, cmd
	}
	if m.inputEditable() {
		if visRow, visCol, ok := pre.pointInputRow(msg.X, msg.Y); ok {
			m.transcriptSel = transcriptSel{} // the prompt claims it: drop any transcript selection
			m.dropRecall()                    // a click IN the box is acting in it: the arrows go back to the caret
			m.sel.seat(m.caretTo(visRow, visCol), cell{visRow, visCol})
			return m, nil
		}
	}
	if line, col, ok := pre.pointTranscriptRow(msg.X, msg.Y); ok {
		m.sel = fieldSel{} // the transcript claims it: drop any prompt selection
		m.transcriptSel = transcriptSel{
			active: true,
			anchor: contentCell{line, col}, head: contentCell{line, col},
		}
		return m, nil
	}
	m.sel = fieldSel{} // a click off both fields deselects
	m.transcriptSel = transcriptSel{}
	return m, nil
}

// footerRowY is the screen row the footer line is painted on. View stacks the input box, the
// footer's single frameless line and the ▁ bottom-edge hairline at the bottom of the window
// (model.go), so the footer stands exactly bottomRuleHeight rows above the terminal's last line.
// Both terms are named rather than folded into a literal, for inputContentRect's reason: an omitted
// one is exactly the off-by-one that puts the rect on the hairline.
func (m Model) footerRowY() int {
	return m.height - bottomRuleHeight - footerHeight
}

// handleFooterModeClick gives the footer's mode marker its pointer: a click anywhere on it opens
// the four-rung mode picker (pickerMode, picker.go), whose accept takes the rung through the same
// SetMode path shift+tab does. It is a second ROUTE to the ladder and never a cycle — the human
// sees the four rungs and names one. Whether a click lands on the marker, and whether the picker may
// open there, is [Model.footerModeHit]'s to say — the one predicate the pointer-shape classifier asks
// as well (pointershape.go), so the marker never shows a pointer a click there would not honour.
func (m Model) handleFooterModeClick(pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
	if !m.footerModeHit(pre, msg.X, msg.Y) {
		return m, nil, false
	}
	m.picker = picker{open: true, kind: pickerMode}
	return m, nil, true
}

// footerModeHit reports whether a click at (x, y) on the frame pre was aimed at is a click on the
// footer's mode marker that may open the mode picker: the footer-click handler's whole decision,
// geometry and state gate together, so the click and the pointer shape over the marker are one
// answer ([Model.handleFooterModeClick], [Model.pointerShapeAt]). The gate is read off the live m
// and the geometry off pre, handleMouseClick's rule.
//
// The rect is the footer's own row and the columns [Model.footerModeSpan] reports for pre — the same
// value the painter drew from, so the pointer can never address a column the painter did not paint,
// and the narrow branch that drops the marker whole (ok false) leaves the footer naming nothing. The
// WHOLE marker is the hit area, Auto's blast-radius word included: that word is a FACT about the mode
// rather than a second control, so every cell of the marker acts on the mode. The confinement word
// stays display-only in the sense that matters — clicking it opens the mode picker, not a confinement
// one.
//
// The gate is the PICKER's OWN entitlement rather than !m.picker.open alone. renderPicker paints on
// m.picker.open by itself, while the picker rung claims keys only under m.state.live() &&
// m.picker.open and the two higher modal rungs — the /sessions browser and the /settings pane — claim
// before it (keyClaimOrder, model.go). Opening the overlay from underneath one of those, or while a
// Turn is awaiting approval, would put up a modal the human can neither answer nor close, which is the
// very thing that rung's comment forbids. shift+tab stays the every-state route to the ladder.
func (m Model) footerModeHit(pre Model, x, y int) bool {
	if !m.state.live() || m.picker.open || m.sessionBrowser.open || m.settingsOwnsInput() {
		return false
	}
	text, col, ok := pre.footerModeSpan(pre.width)
	if !ok || y != pre.footerRowY() {
		return false
	}
	return x >= col && x < col+pre.th.measure.Width(text)
}

// handleMouseMotion extends whichever selection is live as the mouse drags with the left button
// held: head advances while the click-set anchor stays put. A drag never STARTS a selection
// (only a click does), so at most one of the two is active and they cannot both extend. Motion
// outside the owning rectangle never moves the head directly, so a stray past the edge does not
// collapse or hijack the selection. For the TRANSCRIPT that motion has one meaning of its own:
// held on the top row, or anywhere below the last, it scrolls the transcript toward the pointer
// and the head follows the revealed edge line (trackDragEdge). CellMotion reports motion only
// while a button is down; AllMotion — set only while the pointer-shape gate is open
// (pointerprobe.go) — also reports button-less hover, which moves no selection either: with no
// press armed it goes to hoverPointerShape (pointershape.go), which may write the pointer shape a
// click there implies and touches nothing else, so a selection moves only mid-drag. A right or
// middle drag under AllMotion carries its own button, so it neither hovers nor drags.
//
// One decoder quirk lands here rather than in handleMouseRelease: some terminals encode the
// release with the motion bit set (SGR "ESC[<35;x;ym"), which decodes to a BUTTON-LESS motion
// (MouseNone) instead of a release, so while the press latch is armed such a motion IS the
// release and is routed as one — otherwise the press stays armed for the rest of the session and
// the drag never finishes. With nothing armed it is the hover it reads as.
func (m Model) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if msg.Button == tea.MouseNone && m.mousePressed {
		return m.handleMouseRelease(tea.MouseReleaseMsg{X: msg.X, Y: msg.Y, Button: tea.MouseLeft, Mod: msg.Mod})
	}
	if msg.Button == tea.MouseNone {
		return m.hoverPointerShape(msg.X, msg.Y)
	}
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	if next, claimed := m.handleSettingsMotion(msg); claimed {
		return next, nil
	}
	if m.sel.active && m.inputEditable() {
		if visRow, visCol, ok := m.pointInputRow(msg.X, msg.Y); ok {
			m.sel.extend(m.caretTo(visRow, visCol), cell{visRow, visCol})
		}
		return m, nil
	}
	if m.transcriptSel.active {
		if line, col, ok := m.pointTranscriptRow(msg.X, msg.Y); ok {
			m.transcriptSel.head = contentCell{line, col}
		}
		return m.trackDragEdge(msg.X, msg.Y)
	}
	return m, nil
}

// dragScroll is the edge auto-scroll of a held transcript drag (layout.md, the drag-select rule):
// while the pointer sits on the transcript's top row with the view scrolled up, or below its last
// row with more below, a tick loop scrolls toward it and moves the selection head onto each newly
// revealed edge line. A motion inside the zone only restates where the pointer is; ONE loop runs at a
// time, and gen numbers it — the release and the pointer leaving the zone bump gen, so a tick still in
// flight for the old loop is dropped, exactly as a flashClearMsg for a replaced flash is.
type dragScroll struct {
	gen     int  // the live loop's generation; a dragScrollTickMsg carrying any other is dropped
	running bool // a tick for gen is in flight
	up      bool // the pointer is on the top row (scroll up) rather than below the last (scroll down)
	past    int  // rows below the transcript's last row, 0 for the row right under it; unused going up
	x       int  // the pointer's column, where the head lands on each revealed line
}

// dragScrollTickMsg is one step of the edge auto-scroll loop; gen is the loop that scheduled it
// (dragScroll.gen).
type dragScrollTickMsg struct{ gen int }

const (
	// dragScrollInterval is the edge auto-scroll's step period.
	dragScrollInterval = 50 * time.Millisecond
	// dragScrollMaxStep caps the lines one downward step scrolls, however far below the edge the
	// pointer is held.
	dragScrollMaxStep = 5
)

// trackDragEdge reads a transcript drag's motion for the edge auto-scroll. Inside the zone it
// stores where the pointer is and starts the loop if none is running; anywhere else it retires a
// running loop. The top row counts only while the view is scrolled up and the rows below the
// transcript only while there is more below, so a drag at an edge with nothing past it starts nothing.
func (m Model) trackDragEdge(x, y int) (Model, tea.Cmd) {
	rows := m.transcriptRows()
	up := rows > 0 && y <= 0 && m.viewport.YOffset() > 0
	down := rows > 0 && y >= rows && !m.viewport.AtBottom()
	if !up && !down {
		if m.dragScroll.running {
			m.stopDragScroll()
		}
		return m, nil
	}
	m.dragScroll.up, m.dragScroll.x = up, x
	m.dragScroll.past = max(0, y-rows)
	if m.dragScroll.running {
		return m, nil // the live loop reads the new distance on its next tick
	}
	m.dragScroll.running = true
	return m, m.dragScrollTick()
}

// stopDragScroll retires the edge auto-scroll loop: a tick already scheduled for it is dropped.
func (m *Model) stopDragScroll() {
	m.dragScroll.gen++
	m.dragScroll.running = false
}

// dragScrollTick schedules the live loop's next step.
func (m Model) dragScrollTick() tea.Cmd {
	gen := m.dragScroll.gen
	return tea.Tick(dragScrollInterval, func(time.Time) tea.Msg { return dragScrollTickMsg{gen: gen} })
}

// foldDragScrollTick takes one edge auto-scroll step: 1 line up, or min(dragScrollMaxStep,
// 1 + past) lines down, through the tail every user scroll ends on (settleUserScroll), so
// reaching the bottom resumes following. The head moves onto the newly revealed edge line — going
// up, the first row the sticky header does not cover, never row 0, which draws the owning prompt.
// The loop ends at the top or the bottom, and when the button is up or the selection has dropped.
func (m Model) foldDragScrollTick(msg dragScrollTickMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.dragScroll.gen || !m.dragScroll.running {
		return m, nil
	}
	if !m.mousePressed || !m.transcriptSel.active {
		m.stopDragScroll()
		return m, nil
	}
	before := m.viewport.YOffset()
	if m.dragScroll.up {
		m.viewport.ScrollUp(1)
	} else {
		m.viewport.ScrollDown(min(dragScrollMaxStep, 1+m.dragScroll.past))
	}
	m.settleUserScroll()
	if m.viewport.YOffset() == before {
		m.stopDragScroll() // nothing left to reveal: the floor, the top or the bottom
		return m, nil
	}
	if line, ok := m.dragEdgeLine(); ok {
		m.transcriptSel.head = contentCell{line, clampInt(m.dragScroll.x, 0, m.viewport.Width())}
	}
	if (m.dragScroll.up && m.viewport.YOffset() == 0) || (!m.dragScroll.up && m.viewport.AtBottom()) {
		m.stopDragScroll()
		return m, nil
	}
	return m, m.dragScrollTick()
}

// dragEdgeLine is the content line the edge auto-scroll just revealed: going up, the first line
// drawn under the sticky header ([Model.stickyHeaderSpan]); going down, the transcript's last row.
func (m Model) dragEdgeLine() (int, bool) {
	line := m.viewport.YOffset() + m.transcriptRows() - 1
	if m.dragScroll.up {
		_, count := m.stickyHeaderSpan()
		line = m.viewport.YOffset() + count
	}
	if line < 0 || line >= len(m.lines) {
		return 0, false
	}
	return line, true
}

// handleMouseRelease finalises a drag on whichever selection is live. A non-empty span is copied
// to the clipboard (OSC52, the system clipboard, and tmux inside tmux — copyFlash) and a transient
// note confirms it; the highlight stays until the next click or edit — or, for a transcript span,
// until the lines under it change (spanUnchanged) — so the human sees what was taken, even while
// the model keeps streaming beneath it. A bare click (anchor == head) is not a selection and just leaves the caret/anchor where it
// landed. The prompt copies the exact typed runes; the transcript copies the rendered text under
// the span.
//
// A bare click in the TRANSCRIPT has one further meaning: on a block's click surface it toggles
// that block (toggleBlockAt), at the line the PRESS anchored on rather than at whatever the release
// point names by then. MOTION is what arbitrates, exactly as it already separates
// click-to-position from drag in the prompt — a drag that starts on a header line is a
// drag-select like any other, because it never reaches this branch.
func (m Model) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	m.mousePressed = false // the press is over on every path out of here, selection or not
	m.stopDragScroll()     // and so is any edge auto-scroll it was holding
	switch {
	case m.settings.sel.active:
		// The /settings edit field copies what a release took, exactly as the prompt does: the value is
		// a string the human is typing, so the exact runes are what belongs on the clipboard.
		return m.releaseField(&m.settings.sel, m.settings.editor.value())
	case m.sel.active:
		return m.releaseField(&m.sel, m.input.Value())
	case m.transcriptSel.active:
		if m.transcriptSel.anchor == m.transcriptSel.head {
			pressed := m.transcriptSel.anchor.line
			m.transcriptSel.active = false
			toggled, cmd := m.toggleBlockAt(pressed, msg.Y)
			return toggled, cmd
		}
		text := transcriptSelectionText(
			m.th.measure, m.lines, m.padRows(), m.transcriptSel.anchor, m.transcriptSel.head,
		)
		if strings.TrimSpace(text) == "" {
			m.transcriptSel.active = false // a drag that took only blank rows copies nothing
			return m, nil
		}
		return m.copyFlash(text)
	}
	return m, nil
}

// toggleBlockAt changes the state of the block whose click surface a MOTIONLESS click landed on
// (layout.md, "Collapsed and expanded blocks"): a toggle line flips what it names. A line that is
// not one is left exactly as it was: everywhere else in the transcript
// a click keeps its selection meaning, which is the overwhelmingly common case and the one this
// returns on first.
//
// A super-group's two extra kinds are the same rule at its two extra levels (renderSuperGroup): a
// TYPE ROW flips the second state of the run it heads — its member rows, not their bodies — and the
// UMBRELLA HEADER — marked on every umbrella, wearing the fold's ▶/▼ — answers by the umbrella's
// size: a LARGE one folds to its header line and back under the one preference every large
// umbrella shares, written back as `ui.tools-open` (toggleToolsFold), while a small one folds on
// its own session-only head flag (transcript.umbrellaIsLarge, setUmbrellaFolded). A run view's TASK
// ROW is the same rule again at the level ADR 0063 added: it flips the fold of the task the viewed
// run was handed, which is the view's own state and not the head's block state (render.go's rooted
// paint marks the row, transcript.setTaskExpanded holds it). A workflow's STAGE ROW opens rather
// than flips: its item's run view, or the stage view listing its items ([Model.openStageAt]).
//
// WHAT a toggle line names is the paint's business and not this function's, which is why one case
// covers a single block, its body and a group member alike. A single tool block marks every row it
// paints for its HEAD entry — header, leader row and body — so a click on a Terminal call's output
// closes the block that produced it, exactly as a click on a long prompt's text closes the prompt
// (renderToolBlock, renderUserBlock). Inside a folded run every row of a member — its one collapsed
// row, and, open, its leader row, its body and the see-less row closing it — is marked for that
// member's OWN entry instead, so this same flip opens the third of ten reads and leaves the other
// nine exactly where they were (blocktarget.go, lineMark). Adding a case per shape here would be a
// second accounting of what the painter already stated, and the two would drift.
//
// line is the PRESS's own content line, not the release point's, and THAT is what makes the toggle
// land while a reply streams. The press already stored the scroll-immune answer: transcriptSel.
// anchor is in content coordinates, which are append-stable by design, whereas re-resolving the
// release point through pointTranscriptRow reads the LIVE scroll offset — and refreshViewport ends
// every streamed event at GotoBottom, so in the 50–150 ms a human takes to let a button up the
// content under a motionless pointer has already moved, and the release names some other line,
// almost always no target at all. One press, one intent, the same answer at any stream rate.
//
// releaseRow stays a SCREEN row because the anchoring half of the rule is a screen fact: the
// toggled line goes back on the row the pointer is resting on (refreshViewportAnchored), so what
// the human is looking at holds still while the body appears or goes.
//
// The lookup reads the PAINT'S OWN accounting — m.lineTargets, which refreshViewport stashes from
// the marks the painter made as it emitted each line (render.go). One accounting for what is drawn
// where, so the block that flips is the block under the cursor and never its neighbour; the bounds
// check is the same defence the stash's builder states, and it earns its keep twice over here,
// since an anchor OUTLIVES the paint it was taken in — a collapse elsewhere can leave it past the
// last marked line, and a stale index would be a click toggling some other block.
//
// Nothing here re-derives the transcript: an index the entry list has grown past, or one naming a
// kind with no block state, answers false from the transcript's own guard and this changes
// nothing at all.
// The concrete Model comes back rather than a tea.Model because the KEYBOARD reaches the same
// toggle (toggleAtBlockCursor, blockcursor.go) and carries the result on into its own state rather
// than returning it to Update — an interface there would put a type assertion of this package's own
// type in the middle of that path.
func (m Model) toggleBlockAt(line, releaseRow int) (Model, tea.Cmd) {
	target, ok := m.blockTargetAt(line)
	if !ok {
		return m, nil
	}
	switch target.kind {
	case targetBreadcrumb:
		// The run view's header band is not a block: it is the way back up, and a click on any of
		// its three rows leaves the view (runview.go). It returns here rather than falling to the anchoring below, which is
		// about a body that grew or shrank under a header that STAYED — upRun repaints a different
		// transcript and parks it by its own rule.
		return m.upRun(), nil
	case targetHeader:
		// A framed delegation opens as a run VIEW rather than as a rail in place (ADR 0063): one
		// redirect, asked before the flip, so the mouse and the block cursor's ⏎ — which arrives
		// through this same call — cannot come to mean different things by the same gesture.
		if next, opened := m.openRunAt(target.entry); opened {
			return next, nil
		}
		// A task-list card's fold is the one every such card shares, and flipping it is also a
		// write of `ui.task-list-open` (toggleTaskListFold): asked after the run redirect and before
		// the ordinary flip for the same reason the redirect is — one rule for the mouse and for ⏎.
		if m.transcript.foldsWithTaskLists(target.entry) {
			m = m.toggleTaskListFold()
			break
		}
		if !m.transcript.toggleExpanded(target.entry) {
			return m, nil
		}
	case targetStage:
		// A workflow's stage row opens what the stage leads to — its item's run view, or a stage
		// view listing its items — and a stage no item of which has started opens nothing
		// ([Model.openStageAt]). Like the breadcrumb it repaints a different transcript, so it
		// returns rather than anchoring.
		next, _ := m.openStageAt(target.entry, target.stage)
		return next, nil
	case targetTask:
		// Inside a run view the task row folds by its own state, never by the head's block state:
		// the redirect above would refuse the view's own head and setExpanded would refuse the flag,
		// leaving the see-more marker the fold painted advertising rows nothing could open
		// (transcript.setTaskExpanded, ADR 0063).
		if !m.transcript.toggleTaskExpanded(target.entry) {
			return m, nil
		}
	case targetType:
		if !m.transcript.toggleTypeExpanded(target.entry) {
			return m, nil
		}
	case targetUmbrella:
		// Every umbrella's header is a fold. A LARGE one's fold is the shared preference, and
		// flipping it is also a write of `ui.tools-open` (toggleToolsFold) — asked first for the
		// task-list card's reason, one rule for the mouse and for ⏎. A SMALL one folds on its own
		// head flag alone (transcript.setUmbrellaFolded): in-session, written nowhere, leaving every
		// child's state beneath it exactly as it stood for the reopen.
		if m.transcript.umbrellaIsLarge(target.entry) {
			m = m.toggleToolsFold()
		} else if !m.transcript.setUmbrellaFolded(target.entry, !m.transcript.umbrellaFolded(target.entry)) {
			return m, nil
		}
	default:
		return m, nil
	}
	m.refreshViewportAnchored(line, releaseRow)
	return m, nil
}

// blockTargetAt is the click surface the paint stashed for content line line — the target a motionless
// click there hands [Model.toggleBlockAt] — and whether there is one at all: false off the stashed map
// (an anchor can outlive the paint it was taken in) and on a line no block marked. It is the one
// lookup the toggle and the pointer-shape classifier (pointershape.go) both read, so a line shows the
// pointer exactly where a click there would act on a block.
func (m Model) blockTargetAt(line int) (lineTarget, bool) {
	if line < 0 || line >= len(m.lineTargets) {
		return lineTarget{}, false
	}
	target := m.lineTargets[line]
	return target, target.kind != targetNone
}

// releaseField finalises a drag on an editable field's selection — the prompt's or the /settings
// field's, the one release both surfaces share (fieldSel). What the span took is copied exactly as
// typed (copyFlash); a bare click, or a span over nothing, copies nothing, leaves the caret where it
// landed and retires the selection. sel points into m — the release's own copy of the Model, which
// is why this step takes the Model by pointer — so the retirement lands on the Model it returns.
func (m *Model) releaseField(sel *fieldSel, value string) (tea.Model, tea.Cmd) {
	text := sel.taken(value)
	if text == "" {
		sel.active = false
		return *m, nil
	}
	return m.copyFlash(text)
}

// copyFlash copies text to the clipboard over EVERY channel — OSC52, the system clipboard and,
// inside tmux, tmux's own buffer — and shows a transient confirmation counting the runes taken
// (flashClearMsg clears it after flashDuration). OSC52 (tea.SetClipboard) stays the primary: it is
// cross-terminal and SSH-safe, needing no local program, and it is unchanged. The system write
// beside it (systemClipboardCmd, clipboard.go) is the fallback for terminals that ignore the
// escape — where the flash used to promise a copy that went nowhere (the ISSUES defect) — and the
// tmux write (tmuxClipboardCmd) is the route tmux forwards on its default `set-clipboard external`,
// where it drops an application's OSC52 (apogee-tmux-copy-dropped); it starts nothing outside tmux.
// Both are best-effort, so the confirmation stays unconditional, and both resolve their program
// against the session workspace (m.opts.Workspace) inside the Cmd body, so a workspace directory on
// PATH cannot supply it. Shared by the prompt, transcript
// and /settings drag-release paths so all three confirm a copy identically.
func (m Model) copyFlash(text string) (tea.Model, tea.Cmd) {
	n := len([]rune(text))
	noun := "chars"
	if n == 1 {
		noun = "char"
	}
	flashCmd := m.showFlash(fmt.Sprintf("copied %d %s", n, noun))
	return m, tea.Batch(
		tea.SetClipboard(text),
		systemClipboardCmd(m.opts.Workspace, text),
		tmuxClipboardCmd(m.opts.Workspace, text),
		flashCmd,
	)
}

// highlightInput overlays the drag-selection's background on the textarea's rendered block. It
// works purely in visual-cell space — shading the cells between the selection's two ends on the
// already-wrapped lines — so it needs no copy of the textarea's wrap. ScrollYOffset maps the
// stored absolute rows onto the visible block. With no active (non-empty) selection the view is
// returned unchanged.
func (m Model) highlightInput(view string) string {
	if !m.sel.nonEmpty() {
		return view
	}
	top, bot := m.sel.anchorVis, m.sel.headVis
	if bot.row < top.row || (bot.row == top.row && bot.col < top.col) {
		top, bot = bot, top // normalise to reading order
	}
	scroll := m.input.ScrollYOffset()
	lines := strings.Split(view, "\n")
	for r := range lines {
		absRow := scroll + r
		if absRow < top.row || absRow > bot.row {
			continue
		}
		c0, c1 := 0, m.th.measure.Width(lines[r])
		if absRow == top.row {
			c0 = top.col
		}
		if absRow == bot.row {
			c1 = bot.col
		}
		if c1 <= c0 {
			continue
		}
		lines[r] = shadeCells(m.th.measure, lines[r], c0, c1, m.th.selection)
	}
	return strings.Join(lines, "\n")
}

// shadeCells re-renders the display columns [c0,c1) of an ANSI line under style. The flanking
// parts keep their original styling (the cut slices by display cell without breaking escape
// codes); the selected span is stripped and re-rendered so the selection colours win. The
// prompt text is single-styled, so a stripped span loses nothing there — the caret is not part of
// the rendered content at all: the terminal draws the real cursor over the frame (steadyCursor).
//
// It slices through the width authority (width.go) rather than the package-level ansi.Cut, which
// is hard-wired to GraphemeWidth. c0/c1 are painted columns — a mouse report's own space, or a
// token's cells on a row the widget already drew — so a cut in any measure but the painter's
// shades a different run of cells than the one that is on screen there.
func shadeCells(measure widthAuthority, line string, c0, c1 int, style lipgloss.Style) string {
	w := measure.Width(line)
	left := measure.Cut(line, 0, c0)
	mid := measure.Cut(line, c0, c1)
	right := measure.Cut(line, c1, w)
	return left + style.Render(ansi.Strip(mid)) + right
}

// transcriptSelectionText extracts the plain text under a content-coordinate span from the
// cached rendered lines — the "copy what you see" slice. It normalises the span to reading order,
// then for each spanned line cuts the display-cell range [c0,c1) with the width authority
// (cell-accurate and escape-safe), strips the styling, and trims the block's trailing pad; the
// lines join with '\n'. The first and last lines cut to the span's own columns; the lines between
// take the whole width. Markers, rail gutters, and the painter's own wrap breaks are copied
// verbatim — the accepted terminal-native semantics of a screen-space selection (D4). The breaks a
// selection can meet are the painter's alone: the viewport does not wrap, so one cached line is one
// row (newModel, model.go).
//
// The one exception to verbatim is a prompt block's ▄/▀ half row (pads): it is paint, not text,
// so the clipboard holds exactly what the unpadded transcript would have given. A span that starts
// or ends ON half rows copies from or to the nearest content row instead, whole; a half row inside
// the span copies as the blank separator it stands in for, or as nothing where the paint added it
// ([padRows.copyText]). A zero padRows — hand-built lines, or a colourless paint — copies as before.
//
// The measure is the authority's (width.go) rather than ansi.Cut's hard-wired GraphemeWidth
// because the columns came from a mouse report, which counts PAINTED cells: cutting in the measure
// the terminal paints in is what makes the clipboard hold the glyphs the pointer ran over.
func transcriptSelectionText(
	measure widthAuthority,
	lines []string,
	pads padRows,
	a, b contentCell,
) string {
	top, bot := a, b
	if bot.line < top.line || (bot.line == top.line && bot.col < top.col) {
		top, bot = bot, top // normalise to reading order
	}
	// A half row at either end of the span is breathing room the pointer happened to land on, not a
	// separator the human meant to take: the span shrinks to the content rows, taken whole.
	isBotWhole := false
	for top.line <= bot.line && pads.isPad(top.line) {
		top = contentCell{line: top.line + 1}
	}
	for bot.line >= top.line && pads.isPad(bot.line) {
		bot, isBotWhole = contentCell{line: bot.line - 1}, true
	}
	out := make([]string, 0, max(0, bot.line-top.line+1))
	for row := top.line; row <= bot.line; row++ {
		if row < 0 || row >= len(lines) {
			continue
		}
		if pads.isPad(row) {
			if text, ok := pads.copyText(row); ok {
				out = append(out, text)
			}
			continue
		}
		line := lines[row]
		c0, c1 := 0, measure.Width(line)
		if row == top.line {
			c0 = top.col
		}
		if row == bot.line && !isBotWhole {
			c1 = bot.col
		}
		if c1 <= c0 {
			out = append(out, "") // an empty or fully-clipped row is a blank line in the copy
			continue
		}
		out = append(out, strings.TrimRight(ansi.Strip(measure.Cut(line, c0, c1)), " "))
	}
	return strings.Join(out, "\n")
}

// padRows is what a copy needs to know about the prompt blocks' ▄/▀ half rows: which rows they are
// (rows, the paint's own map — Model.pads) and, on each one standing in for a separator, that
// separator's rail depth (seps — Model.padSeps, -1 where the half row stands in for none). A zero
// padRows names no half row at all, so a copy over it is verbatim.
type padRows struct {
	rows []bool
	seps []int
}

// padRows is the model's half-row map with the separator depths its paint recorded.
func (m Model) padRows() padRows {
	return padRows{rows: m.pads, seps: m.padSeps}
}

// isPad reports whether row is a half row. It is bounds-checked, so a map shorter than the lines —
// or none at all — reads as "no half row".
func (p padRows) isPad(row int) bool {
	return row >= 0 && row < len(p.rows) && p.rows[row]
}

// copyText is what the half row at row copies as, and ok is false where it copies as nothing.
// renderView decided, as it placed the half row, whether it STANDS IN for the blank separator on
// the side it pads or was ADDED where none would have been drawn, and recorded the stood-in
// separator's depth (renderedTranscript.padSeps). A stand-in copies as that separator's own text —
// railSpacer's gutter at that depth, bare at depth 0 — and an added half row as nothing. The depth
// is the paint's, never inferred from the neighbouring rows' text, which can open with a "│" of its
// own.
func (p padRows) copyText(row int) (string, bool) {
	if row < 0 || row >= len(p.seps) || p.seps[row] < 0 {
		return "", false
	}
	return strings.TrimRight(strings.Repeat(glyphSubRail+" ", p.seps[row]), " "), true
}

// ----------------------------------------------------------------------------
// Mouse in the /settings pane (settings.go, docs/layout/settings-screen-layout.md requirement 7)
// ----------------------------------------------------------------------------
//
// A third rectangle joins the prompt's and the transcript's, and it claims the pointer only where the
// pane is actually DRAWN. /settings is the frame's one full-height pane but it takes only the rows its
// list needs (frameRowPlan), so on a tall window the transcript is still on screen above it and a drag
// up there copies what it always did. Inside the pane the pointer does what the keys do: a click on a
// key row selects it, the wheel walks the list, and on the row being typed into a click seats the
// caret and a drag selects the value's text.
//
// The mapping is the PAINTER's own arithmetic rather than a second copy of it. The pane composes its
// spec once for both readers (settingsKeyListSpec) and renderPopupPlaced reports where the row block
// landed among the pane's painted rows and which window of rows it holds (popupPlacement) — so the row
// under the pointer is the row the frame drew there at every description length, window height and
// scroll position. The one fact stated on this side is where the pane BEGINS on the screen, and that is
// read off the frame's own stacking (View): the transcript's rows, the single gap row above the overlay
// slot, and whatever else that slot is holding.
//
// The MULTI-LINE field is the same three gestures over a surface that is all field: it replaces the key
// list rather than one row of it (renderSettingsText), so a click seats the caret at the glyph under the
// pointer wherever in the prompt that is, a drag selects across its lines, and the wheel walks it a line
// at a time. Its geometry is read the same way and needs one thing more — the lines each of the prompt's
// lines WRAPPED to, which the placement now carries (popupPlacement.blocks) and popupWrapOffsets reads
// backwards into the prompt's own rune offsets.
//
// Two states take no pointer. The value sub-list replaces the key list with a menu of a different shape
// (renderSettingsEnum), and an armed reset is a question waiting on ⏎ or esc; a click in either names
// nothing rather than guessing at the list it covers. And while a value is being typed the selection
// never MOVES: the buffer belongs to the selected row (settingsBufferTarget), so a click that walked
// the highlight would leave the field editing a key it is no longer drawn on.

// settingsPaneRect is where the open /settings pane is drawn: the screen row its top border lands on
// and how many rows the pane takes. ok is false when it is not on the frame at all — closed, or given
// way to a window too short to seat it (settingsGiveWayNote).
//
// The row is READ off the frame's own stacking rather than counted up from the bottom the way the
// input box is (inputContentRect): the frame publishes where every pane of the transcript-side slot
// landed while it composes them (stackTranscriptSlot, model.go), and this is a lookup into that. No
// term of the sum is restated here, because an omitted term is exactly the off-by-one that puts the
// click on the wrong row — and the slot has gained tenants since this pane joined it.
func (m Model) settingsPaneRect() (y0, h int, ok bool) {
	if !m.settings.open {
		// Asked before the frame is composed, because every click and every wheel notch asks: with no
		// pane up there is nothing to place, and composing the frame's overlays to learn that would put
		// a render on the path of a click the pane has no part in.
		return 0, 0, false
	}
	return m.frameSpans().pane(paneSettings)
}

// popupPaneHit maps a screen row to the row the named pane drew on it. It is the whole of what a
// pointer gesture over a boxed pane has to ask, in the order the answers matter:
//
//   - ok is false and inRect false where the pane is not on the frame at all (openPanes) — the gate
//     dropdownWheel makes for the same reason (autocomplete.go). A handler that skipped it would claim,
//     or DISMISS, on a frame carrying no such pane, using a rectangle belonging to whatever else the
//     slot is holding.
//   - inRect says the point is inside the pane's box (frameSpans.pane). It is the answer the panes that
//     dismiss on an outside click act on, and it is stated separately because "not on a row" and "not
//     on the pane" are different clicks: one is the pane's chrome, the other is somebody else's frame.
//   - row is which of the spec's rows is drawn on that line, through the painter's own placement
//     (popupPlacement.rowAt) — ok is false on the title, the body, a pad line, a gap, the hint and the
//     borders, so a click there claims the pane and does nothing.
//   - top is the first row of the window that placement seated (popupPlacement.start): what a handler
//     arming the row records (clickArm.top), so the pane keeps that window while the arm stands and
//     the row stays under the pointer for the second click.
//
// pre is the frame the gesture was aimed at (handleMouseClick's snapshot), and render composes that
// frame's pane exactly as View does — one composition, the painter's own numbers, rather than a second
// arithmetic that can disagree with it. It is called only once the point is inside the rectangle, so a
// gesture the pane has no part in costs no render.
func popupPaneHit(pre Model, pane framePane, render func() (string, popupPlacement), y int) (row, top int, inRect, ok bool) {
	if !pre.openPanes().has(pane) {
		return 0, 0, false, false
	}
	paneTop, h, drawn := pre.frameSpans().pane(pane)
	if !drawn || y < paneTop || y >= paneTop+h {
		return 0, 0, false, false
	}
	_, place := render()
	row, _, ok = place.rowAt(y - paneTop)
	return row, place.start, true, ok
}

// paneHit is what a left-click at one point of the frame would do to one pane, read WITHOUT doing it:
// the decision the pane's click handler acts on, taken by a pure predicate of the pane's (geometry
// through popupPaneHit, and the handler's state gates beside it). The handler acts on it, and the
// pane's hover answers from it ([paneHit.shape]) — one predicate, two callers — so the pointer can
// never promise a click the handler would refuse, nor hide one it would take.
type paneHit struct {
	at    hitKind
	row   int // the row a hitRow names: the pane's own row index (the /settings pane: the key)
	top   int // the first row of the window the placement seated, for the arm (popupPaneHit)
	count int // how many choices the row is one of, for the seat
	off   int // the rune offset a hitField seats the caret at
}

// hitKind is which of the answers a pane's click currency has a point gets.
type hitKind int

const (
	hitAbsent  hitKind = iota // not the pane's click at all — the pane is not up, or the point is chrome it leaves to the chain
	hitOutside                // outside the box of a pane that does NOT claim there: the chain goes on (a pane that dismisses on its way out has done so)
	hitDismiss                // outside the box of a modal list: dismissed, and the click is spent on that
	hitInert                  // claimed, and nothing happens: the chrome, prose, or a state that owns the pane
	hitRow                    // on a row the click acts on: it seats the highlight, or takes the armed row
	hitField                  // on the editable field: the click seats the caret
)

// shape is the pointer the hit implies, and whether the pane answers for the point: a row is the hand,
// the field the I-beam, and a point the pane claims for nothing — its chrome, or the outside of a
// modal list, where the click only dismisses — the arrow. A point the pane does not claim is not its
// to answer, so the classifier asks the next region, exactly as the click chain carries on.
func (h paneHit) shape() (pointerShape, bool) {
	switch h.at {
	case hitRow:
		return shapePointer, true
	case hitField:
		return shapeText, true
	case hitInert, hitDismiss:
		return shapeDefault, true
	default:
		return shapeDefault, false
	}
}

// settingsPaint is the open key list as the frame composes it: the display rows the pane built, and the
// composition a pointer is mapped through (popupPaneHit). It is everything a gesture needs in order to
// name what is under it — the row through the placement, the value column through the display.
//
// The pane is not RENDERED here. What a gesture needs is the row under one point, and popupPaneHit
// renders only once that point is inside the box, so a motion that strays off the pane costs the spec
// and nothing more.
type settingsPaint struct {
	display settingsDisplay
	render  func() (string, popupPlacement) // the pane as View draws it, for popupPaneHit to place
}

// settingsPaint composes the open pane exactly as the frame does. ok is false wherever there is nothing
// to address: the pane closed, or a second step up that takes no pointer. Whether the frame could seat
// the pane at all is popupPaneHit's question, asked of the rectangle the frame published for it.
func (m Model) settingsPaint() (settingsPaint, bool) {
	if !m.settings.open {
		// Asked before the frame is composed, because every gesture asks: with no pane up there is
		// nothing to place, and building the pane's rows to learn that would put the whole config
		// listing on the path of a click the pane has no part in.
		return settingsPaint{}, false
	}
	rows := m.settingRows()
	// A step that paints for itself has replaced the key list — the value sub-list is a menu of its
	// own, and the multi-line field is answered by settingsFieldPaint — so no pointer names a key row
	// there. Which steps those are is the step table's to say ([settingsStep.paint]), asked through
	// the same target the painter re-derives: a step whose row is gone has fallen back to the list, and
	// the list is what the pointer is over.
	if step, ok := settingsSteps[m.settings.kind]; ok && step.paint != nil {
		if _, own := step.target(m, rows); own {
			return settingsPaint{}, false
		}
	}
	spec, display, seated := m.settingsKeyListSpec(rows)
	if !seated {
		return settingsPaint{}, false
	}
	return settingsPaint{
		display: display,
		render:  func() (string, popupPlacement) { return renderPopupPlaced(m.th, spec, m.width) },
	}, true
}

// rowAt maps a screen row to the DISPLAY row drawn on it — the pane's own list index, its section
// labels and spacers included. It is popupPaneHit with this pane's name bound to it, so the name is
// written once. ok is false above or below the drawn window, so a click on the pane's title, its
// description header or its legend names no row — and false too where the pane is not on the frame at
// all. The two panes that DISMISS on an outside click need those told apart and ask popupPaneHit
// themselves; /settings claims its rows and nothing else, so one answer serves it.
func (p settingsPaint) rowAt(pre Model, y int) (int, bool) {
	row, _, _, ok := popupPaneHit(pre, paneSettings, p.render, y)
	return row, ok
}

// settingsContentX is the screen column a pane's content lines begin at: the box's left border glyph
// and the padding cell drawn inside it, both asked of the border style the box is drawn with
// (drawTitledBox reads the same two).
func (m Model) settingsContentX() int {
	return m.th.popupBorder.GetBorderLeftSize() + m.th.popupBorder.GetPaddingLeft()
}

// settingsValueX is the screen column a row's VALUE cell starts at: the pane's content origin, the
// marker column every row leads with (popupRowIndent), and the columns laid out before the value
// (popupCellColumn, over the widths the module measured across the whole list). It is the value row's
// text column in the field geometry both its readers spend ([Model.settingsFieldGeometry]) — the click
// that seats a caret and the highlight that shades a span — so the two can never land on different
// columns of the same row.
func (m Model) settingsValueX(display settingsDisplay) int {
	return m.settingsContentX() + popupRowIndent +
		popupCellColumn(m.th, popupColumnWidths(m.th, display.rows), settingsValueColumn)
}

// settingsFieldPaint is the open /settings FIELD as it was drawn this frame — the value row standing in
// the key list, or the multi-line field that replaced the list — reduced to the one shape both are:
// painted sub-rows, each a run of the field's PAINTED text (the caret glyph among it,
// [lineEditor.textWithCaret]) drawn on one line, plus the column that text begins at. The value row is
// one sub-row; the multi-line field is one per wrapped line in the window. Everything a pointer or a
// highlight asks of either field is asked of this, so the caret-at ([Model.settingsFieldCaretAt]), the
// shading ([Model.settingsFieldShades]) and the claim a click makes cannot answer the two fields
// differently.
//
// origin is the pane's TOP BORDER, in whichever coordinates the caller asked for: the pane's own
// painted rows for the highlight, the SCREEN's for the pointer. One builder answers both
// ([Model.settingsFieldGeometry]), so the row a click names and the row a span is shaded on cannot come
// apart. It is the box's own origin rather than the block's because that is the coordinate the
// placement maps in (popupPlacement.rowAt).
type settingsFieldPaint struct {
	place  popupPlacement     // where the painter put the pane's rows, and what each of them wrapped to
	origin int                // the pane's top border, in the caller's own coordinates
	x      int                // the column the field's text begins at on every sub-row
	rows   []settingsFieldRow // the field's sub-rows inside the drawn window, top to bottom
	glyph  glyphShift         // where the caret glyph stands in the painted text ([lineEditor.caretGlyph])
}

// settingsFieldRow is one painted line of the field: the row it is drawn on, the runes drawn there,
// and the offset those runes begin at in the field's whole painted text.
type settingsFieldRow struct {
	y     int
	start int
	text  []rune
}

// onRow reports whether a row is one of the PANE's rows — a key row, a section label or spacer, a line
// of the field — rather than its title, description header, legend or border. It is the claim a click
// makes on an open field: a click on a row that is not the field is still the pane's and is swallowed,
// and a click on the chrome is left to whoever else wants it. Over the multi-line field the pane's rows
// ARE the field's lines, so there the two answers are one.
func (p settingsFieldPaint) onRow(y int) bool {
	_, _, ok := p.place.rowAt(y - p.origin)
	return ok
}

// settingsFieldGeometry reads the placement the painter reported into the coordinates a pointer or a
// highlight works in. It re-derives none of the pane's layout: the window, the wrap and the gaps are
// the painter's own (popupPlacement), and the walk over them is the one rowAt takes.
//
// display is the key list the pane was composed from when the field is the VALUE ROW standing in it,
// and nil when the field is the multi-line prompt that replaced the list — which of the two the caller
// painted is the caller's to say, since a text step whose row has gone paints the key list. A value row
// scrolled out of the window emits no sub-row, and a pane in any other state emits none at all, so a
// highlight shades nothing and a click seats no caret there.
func (m Model) settingsFieldGeometry(place popupPlacement, display *settingsDisplay, origin int) settingsFieldPaint {
	p := settingsFieldPaint{place: place, origin: origin, glyph: m.settings.editor.caretGlyph()}
	var at func(row, sub int) ([]rune, int, bool)
	switch {
	case display != nil && m.settings.kind == settingsValueBuffer:
		p.x = m.settingsValueX(*display)
		text := []rune(m.settingsEditText())
		at = func(row, sub int) ([]rune, int, bool) {
			return text, 0, row == display.selected && sub == 0
		}
	case display == nil && m.settings.kind == settingsTextEditor:
		p.x = m.settingsContentX() + popupRowIndent
		at = m.settingsTextSubRows(place)
	default:
		return p
	}
	y := origin + place.rowsAt
	for i := place.start; i < place.end && i < len(place.blocks); i++ {
		if i > place.start {
			y += place.gap
		}
		for sub := range place.blocks[i] {
			if text, start, ok := at(i, sub); ok {
				p.rows = append(p.rows, settingsFieldRow{y: y, start: start, text: text})
			}
			y++
		}
	}
	return p
}

// settingsTextSubRows answers, for the multi-line field, which painted text one wrapped sub-line of a
// prose line holds and where it begins in the whole painted text. The runes are the painter's own
// (place.blocks); the only arithmetic is the running rune offset of each line into the painted text —
// the one fact the module never had, because it is about the VALUE and not about the pane.
func (m Model) settingsTextSubRows(place popupPlacement) func(row, sub int) ([]rune, int, bool) {
	lines := m.settingsTextLines()
	starts := make([]int, len(lines))
	subs := make([][]int, len(lines))
	off := 0
	for i, line := range lines {
		starts[i] = off
		off += len([]rune(line)) + 1 // the +1 is the '\n' the split removed
		if i < len(place.blocks) {
			subs[i] = popupWrapOffsets(line, place.blocks[i])
		}
	}
	return func(row, sub int) ([]rune, int, bool) {
		if row < 0 || row >= len(lines) || sub < 0 || sub >= len(subs[row]) {
			return nil, 0, false
		}
		return []rune(place.blocks[row][sub]), starts[row] + subs[row][sub], true
	}
}

// settingsFieldPaint composes the open field exactly as the frame does and reports where it landed, in
// SCREEN rows. ok is false wherever there is nothing to address: the pane closed or given way, no field
// open, or a frame that cannot seat the pane.
func (m Model) settingsFieldPaint() (settingsFieldPaint, bool) {
	paneTop, _, ok := m.settingsPaneRect()
	if !ok {
		return settingsFieldPaint{}, false
	}
	rows := m.settingRows()
	switch m.settings.kind {
	case settingsValueBuffer:
		spec, display, seated := m.settingsKeyListSpec(rows)
		if !seated {
			return settingsFieldPaint{}, false
		}
		_, place := renderPopupPlaced(m.th, spec, m.width)
		return m.settingsFieldGeometry(place, &display, paneTop), true
	case settingsTextEditor:
		if _, open := m.settingsTextTarget(rows); !open {
			return settingsFieldPaint{}, false
		}
		spec, seated := m.settingsTextSpec(rows)
		if !seated {
			return settingsFieldPaint{}, false
		}
		_, place := renderPopupPlaced(m.th, spec, m.width)
		return m.settingsFieldGeometry(place, nil, paneTop), true
	}
	return settingsFieldPaint{}, false
}

// settingsFieldCaretAt is the rune offset in the field's VALUE that a point names: the sub-row under
// the pointer, the column read across its painted text in the painter's own measure
// (cellToRuneOffsetIn), and the caret glyph then taken back out ([glyphShift.toValue]). ok is false off
// the field's sub-rows.
func (m Model) settingsFieldCaretAt(p settingsFieldPaint, x, y int) (int, bool) {
	for _, row := range p.rows {
		if row.y != y {
			continue
		}
		painted := row.start + cellToRuneOffsetIn(m.th.measure, row.text, max(0, x-p.x))
		return p.glyph.toValue(painted), true
	}
	return 0, false
}

// fieldShade is one run of a field's selection as it is shaded: the row, and the columns [x0, x1) —
// in the coordinates the paint was built in.
type fieldShade struct {
	y, x0, x1 int
}

// settingsFieldShades is where the field's drag-selection is shaded: each painted sub-row lit for
// exactly the part of the span it holds, so a span that begins mid-line and ends mid-line three lines
// down lights those three lines and nothing either side of it. The span is converted from the VALUE's
// offsets to the PAINTED text's first ([glyphShift.toPainted]) and measured in the painter's authority,
// so the shaded run covers exactly the glyphs a release would copy — and the cells a click on them
// seats the caret in ([Model.settingsFieldCaretAt] undoes the same shift). Nothing is shaded without
// an active, non-empty span (fieldSel.nonEmpty).
func (m Model) settingsFieldShades(p settingsFieldPaint) []fieldShade {
	if !m.settings.sel.nonEmpty() {
		return nil
	}
	lo, hi := p.glyph.toPainted(m.settings.sel.span())
	var shades []fieldShade
	for _, row := range p.rows {
		a, b := max(lo, row.start), min(hi, row.start+len(row.text))
		if b <= a {
			continue
		}
		shades = append(shades, fieldShade{
			y:  row.y,
			x0: p.x + m.th.measure.Width(string(row.text[:a-row.start])),
			x1: p.x + m.th.measure.Width(string(row.text[:b-row.start])),
		})
	}
	return shades
}

// highlightSettingsField overlays the open field's drag-selection on the composed pane —
// highlightInput's idiom one surface along, for the value row and the multi-line field alike. It is
// done on the pane's PAINTED lines rather than in the cells the rows are composed from because the
// popup module takes plain, escape-free cells and styles its rows whole (doc.go): a pre-shaded cell
// would hand it the very escapes its contract forbids. p is built in the pane's own coordinates
// (origin 0), so its rows index the view's lines directly.
func (m Model) highlightSettingsField(view string, p settingsFieldPaint) string {
	shades := m.settingsFieldShades(p)
	if len(shades) == 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	for _, s := range shades {
		if s.y >= 0 && s.y < len(lines) {
			lines[s.y] = shadeCells(m.th.measure, lines[s.y], s.x0, s.x1, m.th.selection)
		}
	}
	return strings.Join(lines, "\n")
}

// handleSettingsClick answers a left-click inside the open /settings pane: in an open field — the row
// being typed into, or the multi-line field — it seats the caret at the clicked glyph and arms a
// selection there, on any other KEY row it moves the selection to that row, and on a section label, a
// spacer or the pane's chrome it does nothing at all. ok is false when the point falls outside the
// pane's row block, which leaves the click to the prompt and the transcript exactly as it was.
//
// A click the pane DOES claim drops both of the other selections: starting one selection clears the
// others, the arbitration this file has always made, and here it also says the pointer has moved to
// the surface the keyboard is already on. The converse is the caller's, and it is the half a live
// selection makes silent: a click the pane does NOT claim drops the pane's own span, or the field's
// highlight would keep answering every motion and every release taken elsewhere on the frame.
//
// pre is the pre-click frame the pane's rows are placed from (handleMouseClick). It is the same value
// as the receiver while this is the FIRST handler in the chain, and it is written down anyway: the one
// invariant the chain holds is that no rect in it is ever read off a model a dismissal has moved.
func (m Model) handleSettingsClick(pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
	hit := m.settingsHit(pre, msg.X, msg.Y)
	if hit.at == hitAbsent {
		return m, nil, false
	}
	m.sel, m.transcriptSel = fieldSel{}, transcriptSel{}
	switch hit.at {
	case hitField:
		m.settings.editor.edit(func(e *lineEditor) tea.Cmd {
			e.caretToRune(hit.off)
			return nil
		})
		m.settings.sel.seat(hit.off, cell{})
	case hitRow:
		m.settings.selected = hit.row
	}
	return m, nil, true
}

// settingsHit is handleSettingsClick's decision for a point, read without acting on it ([paneHit]), in
// the handler's own branch order. Geometry is put to pre and every state gate to the receiver
// (handleMouseClick's rule).
func (m Model) settingsHit(pre Model, x, y int) paneHit {
	if m.settings.kind == settingsValueBuffer || m.settings.kind == settingsTextEditor {
		// One branch for both fields, over their one geometry: a click on one of the pane's rows is
		// claimed — on the field it seats the caret, on another key row of the value buffer's list it is
		// swallowed and the buffer stays where it is — and a click on the chrome is left to the chain.
		paint, ok := pre.settingsFieldPaint()
		if !ok || !paint.onRow(y) {
			return paneHit{}
		}
		if off, on := pre.settingsFieldCaretAt(paint, x, y); on {
			return paneHit{at: hitField, off: off}
		}
		return paneHit{at: hitInert}
	}
	paint, ok := pre.settingsPaint()
	if !ok {
		return paneHit{}
	}
	display, ok := paint.rowAt(pre, y)
	if !ok {
		return paneHit{}
	}
	// Only the key list moves the selection: a step that still paints the list under it — an armed
	// reset, the save-target question — owns the pane, so a click on a key row there is swallowed.
	if m.settings.kind == settingsKeyList {
		if key, isKey := paint.display.settingKeyAt(display); isKey {
			return paneHit{at: hitRow, row: key}
		}
	}
	return paneHit{at: hitInert}
}

// handleSettingsMotion extends the open field's selection as the mouse drags with the left button held
// — within the value row, or across the multi-line field's lines: the caret follows the moving end,
// exactly as it does in the prompt, while the click-set anchor stays put. A drag never STARTS one, and
// motion that strays off the field is still the pane's drag, ignored so a stray past its ends neither
// collapses the span nor hijacks another row. Motion is read off the LIVE model, whose frame is the one
// the pointer is moving over.
func (m Model) handleSettingsMotion(msg tea.MouseMotionMsg) (Model, bool) {
	if !m.settings.sel.active {
		return m, false
	}
	paint, ok := m.settingsFieldPaint()
	if !ok {
		return m, false // no field open, or the pane gave way to a short window: nothing to drag in
	}
	off, on := m.settingsFieldCaretAt(paint, msg.X, msg.Y)
	if !on {
		return m, true // off the field, but still the pane's drag: the span keeps what it had
	}
	m.settings.editor.edit(func(e *lineEditor) tea.Cmd {
		e.caretToRune(off)
		return nil
	})
	m.settings.sel.extend(off, cell{})
	return m, true
}

// settingsWheel walks the /settings key list one row per notch while the pointer is over the pane — the
// keyboard's ↑/↓ under the wheel, with the scroll window following it exactly as it does for them
// (popupRowWindow re-derives around the selection on every frame). handled is false anywhere else,
// which leaves the notch to the transcript scrolling above and behind the pane.
//
// It CLAMPS at the ends where the keys WRAP, and the difference is the gesture rather than an
// inconsistency: ↑/↓ walk a list as a cycle, while a wheel is a scroll — rolling past the last key and
// landing back on the first would move the human somewhere they did not aim. Under a filter the ends
// are the first and last MATCHING keys, the rows ↑/↓ walk too. A wheel over a pane that
// is asking something (the value sub-list, an armed reset, an open buffer) moves nothing: the step owns
// the surface until it is answered, as it owns every key.
func (m Model) settingsWheel(msg tea.MouseWheelMsg) (Model, bool) {
	y0, h, ok := m.settingsPaneRect()
	if !ok || msg.Y < y0 || msg.Y >= y0+h {
		return m, false
	}
	if m.settings.kind == settingsTextEditor {
		// The multi-line field scrolls by its CARET, because that is what its window follows
		// (renderSettingsText points selected at the caret's line): one prompt line per notch, the same
		// step ↑/↓ take in it, and the widget's own clamp at the two ends the wheel must not roll past.
		step := 0
		switch msg.Button {
		case tea.MouseWheelUp:
			step = -1
		case tea.MouseWheelDown:
			step = 1
		}
		if step != 0 {
			m.settings.editor.edit(func(e *lineEditor) tea.Cmd {
				e.stepLine(step)
				return nil
			})
		}
		return m, true
	}
	if m.settings.kind != settingsKeyList {
		return m, true
	}
	// The notch walks the rows the filter leaves, by the cursor's own clamp ([listCursor.wheel]), and
	// lands back on the full slice the pane indexes — so a notch and an ↑ agree on the row, and with
	// no row showing (an empty list, a filter matching nothing) there is nothing to walk.
	view, cursor := m.settingsFilteredCursor(m.settingRows())
	cursor.wheel(msg, len(view.offering))
	m.settingsSeatFiltered(view, cursor)
	return m, true
}

// ----------------------------------------------------------------------------
// Mouse in the /sessions browser and the picker (sessions.go, picker.go)
// ----------------------------------------------------------------------------

// handleBrowserClick answers a left-click while the /sessions overlay is up. The pointer does what
// the keyboard does on this pane and no more: a click on a session row moves the highlight onto it,
// the way ↑/↓ do, and a SECOND click on that same row resumes it — the ⏎ the highlight was already
// offering, through the pane's one accept (acceptBrowser, sessions.go), so the record the pointer
// loads and the record ⏎ loads can never be two different sessions. Two clicks ALWAYS: the row an
// activating click may take is the row the POINTER put the highlight on (clickArm, model.go), so the
// pane's own default never becomes a session somebody chose to reopen.
//
// The row it names is the row the pane PAINTS — the filtered one — because that is what
// popupPaneHit reads off the painter's placement, and acceptBrowser resolves it back to the record
// it describes through the very filter mapping ⏎ uses (ADR 0053 D5). With a filter typed, "the third
// painted row" and "the third record saved" are different sessions, and only the mapping tells them
// apart.
//
// A live rename edit or a delete confirm swallows the click whole. They are modal surfaces WITHIN
// the modal — the browser's wheel treats them the same way (browserWheel, sessions.go) — and each is
// a question: dismissing the pane out from under one would cancel it with the pointer, which is
// exactly what the outside-click rule handleMouseClick states forbids.
//
// Outside the box this pane DISMISSES and CLAIMS (call C as the owner amended it): the browser is
// MODAL, so a click that closes it is the click that closed it and reaches nothing underneath. The
// open gate above is what keeps that honest — with no browser on the frame there is no rectangle to
// be outside of, and an ungated handler would dismiss and swallow every click in the program.
//
// pre is the pre-click frame the pane is placed from and the live model is what mutates
// (handleMouseClick's rule); the rows are composed once here, unfiltered, and the count, the seat
// and the accept all read that one composition, exactly as sessionBrowserKey composes them once per
// keypress.
func (m Model) handleBrowserClick(pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
	hit, rows := m.browserHit(pre, msg.Y)
	switch hit.at {
	case hitAbsent:
		return m, nil, false
	case hitDismiss:
		m.sessionBrowser = sessionBrowser{} // outside the modal: dismissed, and the click is spent on that
		return m, nil, true
	case hitRow:
	default:
		return m, nil, true // a modal surface within the modal, the pane's chrome, or prose: claimed, with no row to take
	}
	if m.clickArmed.holds(paneBrowser, hit.row) {
		// The row the POINTER highlighted, clicked again: the arm is spent here rather than left for
		// the next gesture to drop, because the pane it belonged to is closing.
		m.clickArmed = clickArm{}
		next, cmd := acceptedModel(m.acceptBrowser(rows))
		return next, cmd, true
	}
	m.sessionBrowser.seat(hit.row, hit.count)
	m.clickArmed = clickArm{pane: paneBrowser, row: hit.row, top: hit.top, ok: true}
	return m, nil, true
}

// browserHit is handleBrowserClick's decision for a point, read without acting on it ([paneHit]), with
// the unfiltered rows a row hit was counted against — the one composition the handler's seat and
// accept then read. Geometry is put to pre and every state gate to the receiver (handleMouseClick's
// rule).
func (m Model) browserHit(pre Model, y int) (paneHit, []popupRow) {
	if !m.sessionBrowser.open || !pre.sessionBrowser.open {
		return paneHit{}, nil
	}
	if m.sessionBrowser.renaming || m.sessionBrowser.confirming {
		return paneHit{at: hitInert}, nil // a modal surface within the modal: it owns the pane until it is answered
	}
	row, top, inRect, onRow := popupPaneHit(pre, paneBrowser, pre.renderSessionBrowserPlaced, y)
	if !inRect {
		return paneHit{at: hitDismiss}, nil
	}
	if !onRow {
		return paneHit{at: hitInert}, nil // the pane's chrome
	}
	rows, _ := m.sessionBrowser.unfilteredRows(m.th, m.opts.Workspace, time.Now())
	count := len(m.sessionBrowser.view(rows).rows)
	if count == 0 {
		return paneHit{at: hitInert}, nil // the empty-workspace note, or the zero-match one, is prose, not a choice: no row to name
	}
	return paneHit{at: hitRow, row: row, top: top, count: count}, rows
}

// handlePickerClick answers a left-click while the picker is up — every kind of it, the two
// /schedule questions and the start-up migrations included. It is handleBrowserClick's rule one pane
// along and for the same reasons: a click on a row seats the highlight, a SECOND click on that row
// is the ⏎ (acceptPicker, picker.go), and the arm is what makes it always two — a picker that acted
// on its own default highlight would switch the model, or create a Schedule, on one press.
//
// The row is the FILTERED one the pane painted, and acceptPicker maps it back to the offering it
// names (pickerView.offeringIndex) exactly as it does for ⏎, so a filtered list can never move the
// session somewhere the human never saw.
//
// A latched pane (the adoption and start-up migration panes — picker.latched) gates that second click on its arming latch
// exactly as the approval pane does (handleApprovalClick): an unarmed accepting click is swallowed
// and leaves the arm STANDING, so once the pane arms the next click on that row takes it.
//
// Outside the box: dismissed and CLAIMED, the browser's answer above, because the picker is modal
// too. The dismissal zeroes the whole overlay — the half-built Schedule and the migration round with
// it — which is what esc already does to it (pickerKey), and the open gate keeps a frame carrying no
// picker out of it. The workflow boundary's stop-or-keep confirm is the one kind whose esc is more
// than that zeroing: it cancels through answerBoundary, which drains the commands the confirm held
// and runs a held flush, so an outside click there cancels the same way rather than stranding them.
func (m Model) handlePickerClick(pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
	hit := m.pickerHit(pre, msg.Y)
	switch hit.at {
	case hitAbsent:
		return m, nil, false
	case hitDismiss:
		if m.boundaryConfirmOpen() {
			next, cmd := acceptedModel(m.answerBoundary(boundaryCancel))
			return next, cmd, true
		}
		m.picker = picker{} // outside the modal: dismissed, and the click is spent on that
		return m, nil, true
	case hitRow:
	default:
		return m, nil, true // the pane's chrome, or the zero-match note: claimed, with no row to take
	}
	if m.clickArmed.holds(panePicker, hit.row) {
		if !m.picker.isArmed() {
			return m, nil, true // the latch, not the arm: the row stays armed for the click after the tick
		}
		m.clickArmed = clickArm{}
		next, cmd := acceptedModel(m.acceptPicker())
		return next, cmd, true
	}
	m.picker.seat(hit.row, hit.count)
	m.clickArmed = clickArm{pane: panePicker, row: hit.row, top: hit.top, ok: true}
	return m, nil, true
}

// pickerHit is handlePickerClick's decision for a point, read without acting on it ([paneHit]).
// Geometry is put to pre and every state gate to the receiver (handleMouseClick's rule). The arming
// latch is not part of it: it holds back only the second click on the row the pointer armed, for the
// round trip it waits on, and the row is still the one the click is aimed at.
func (m Model) pickerHit(pre Model, y int) paneHit {
	if !m.picker.open || !pre.picker.open {
		return paneHit{}
	}
	row, top, inRect, onRow := popupPaneHit(pre, panePicker, pre.renderPickerPlaced, y)
	if !inRect {
		return paneHit{at: hitDismiss}
	}
	if !onRow {
		return paneHit{at: hitInert} // the pane's chrome
	}
	count := m.pickerCount()
	if count == 0 {
		return paneHit{at: hitInert} // a filter matching nothing: its one row is the zero-match note, prose and not a choice
	}
	return paneHit{at: hitRow, row: row, top: top, count: count}
}

// ----------------------------------------------------------------------------
// Mouse in the ask prompt (ask.go)
// ----------------------------------------------------------------------------

// handleAskClick answers a left-click while an ask_user question is up. The pointer does what the
// keyboard does on this pane and no more: a click on an offered answer moves the highlight onto it —
// ticking its box where the question is multi-select, exactly as ␣ does — and a SECOND click on that
// same row sends it, the ⏎ the highlight was already offering (submitAnswer). The two-click rule is the
// arm's (clickArm, model.go): the pane's own default highlight was put there by the pane, not by the
// human, so a single click can never turn it into an answer somebody gave.
//
// The pane's ARMING latch gates that second click on top of the arm, exactly as it gates ⏎ and as it
// gates the approval pane's deciding click (handleApprovalClick): an unarmed sending click is
// swallowed and leaves the arm STANDING, so once the pane arms the next click on that row sends. The
// first click — the highlight, and the tick on a multi-select row — sends nothing and stays live.
//
// Everything else INSIDE the box it swallows. A click that names no row — the question, a pad line,
// the hint, a border — is the pane's and does nothing; so is every click while the input box holds
// text, which is the empty-box guard askChoiceKey makes for the arrows (D5, call G), and so is a
// question offering no choices at all, which has no row to name. Nothing is ever selected by those
// clicks, so handleMouseRelease falls through unchanged.
//
// A click OUTSIDE the box is not the pane's at all: it goes back unclaimed and the chain carries on
// with it, by the outside-click rule handleMouseClick states. The question is untouched by it —
// cancelling a decision stays esc's alone — and the prompt's caret seat and the transcript's drag
// stay reachable for as long as it stands.
//
// pre is the pre-click frame the pane is placed from (handleMouseClick) and the live model is what
// mutates — the chain's rule, and here it is also the reason the question the geometry is composed from
// is read off pre: submitAnswer clears m.pendingAsk, and a second click composing the pane from the
// answered model would have nothing to place.
func (m Model) handleAskClick(pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
	hit := m.askHit(pre, msg.Y)
	switch hit.at {
	case hitAbsent, hitOutside:
		return m, nil, false // not up, or outside the box: unclaimed, and the chain goes on with it
	case hitRow:
	default:
		return m, nil, true // off its rows, the box holds a typed answer, or no choices are offered
	}
	if m.clickArmed.holds(panePrompt, hit.row) {
		if !m.approvalArmed {
			return m, nil, true // the latch, not the arm: the row stays armed for the click after the tick
		}
		// The row the POINTER highlighted, clicked again: the arm is spent here rather than left for
		// the next keypress to drop, because the question it belonged to is over.
		m.clickArmed = clickArm{}
		next, cmd := acceptedModel(m.submitAnswer())
		return next, cmd, true
	}
	m.askSel.seat(hit.row, hit.count)
	if pre.pendingAsk.Request.MultiSelect {
		// A checked set shorter than the offering ticks nothing rather than panicking — the −1
		// listCursor.highlight answers for an empty one, the same guard ␣ takes (askChoiceKey).
		if sel := m.askSel.highlight(len(m.askChecked)); sel >= 0 {
			m.askChecked[sel] = !m.askChecked[sel]
		}
	}
	m.clickArmed = clickArm{pane: panePrompt, row: hit.row, top: hit.top, ok: true}
	return m, nil, true
}

// askHit is handleAskClick's decision for a point, read without acting on it ([paneHit]). Geometry is
// put to pre — the question it is composed from included — and every state gate to the receiver
// (handleMouseClick's rule).
func (m Model) askHit(pre Model, y int) paneHit {
	if m.state != stateAwaitingAsk || m.pendingAsk == nil || pre.pendingAsk == nil {
		return paneHit{}
	}
	req := pre.pendingAsk.Request
	row, top, inRect, onRow := popupPaneHit(pre, panePrompt, func() (string, popupPlacement) {
		return pre.askPromptPlaced(req)
	}, y)
	if !inRect {
		return paneHit{at: hitOutside}
	}
	if !onRow || m.input.Value() != "" {
		return paneHit{at: hitInert} // off its rows, or the box holds a typed answer
	}
	choices := m.pendingAsk.Request.Choices
	if len(choices) == 0 {
		return paneHit{at: hitInert}
	}
	return paneHit{at: hitRow, row: row, top: top, count: len(choices)}
}

// ----------------------------------------------------------------------------
// Mouse in the approval prompt (approval.go)
// ----------------------------------------------------------------------------

// handleApprovalClick answers a left-click while a tool call waits on the human. The pointer does
// what the keyboard does on this pane and no more: a click on a menu row moves the highlight onto it,
// the way ↑/↓ do, and a SECOND click on that same row takes it — the ⏎ the highlight was already
// offering (resolveApproval), so a click on Cancel stops the worker exactly as ⏎ on it does.
//
// Two clicks ALWAYS, and on this pane that rule is what keeps the surface a security surface. The
// pane opens with Allow highlighted (foldApprovalRequest) and settles long before a hand reaches the
// mouse, so a handler that activated whatever the highlight already sat on would grant a tool call on
// ONE click. The row an activating click may take is therefore the row the POINTER put the highlight
// on — the arm (clickArm, model.go) — and the pane's own default is never one of those.
//
// The pane's ARMING latch gates that second click on top of the arm, exactly as it gates ⏎
// (approvalDrainMarker, approval.go; model.go's ⏎ case): the pane's promise is that the human saw the
// call before they ruled on it, and a click is as capable as a keystroke of having been aimed at the
// frame before this one. An unarmed activating click is swallowed and leaves the arm STANDING, so
// once the pane arms the next click on that row rules — the latch costs the gesture nothing but the
// round trip it waits for.
//
// Everything else INSIDE the box it swallows: a click naming no row — the reason, the argument block,
// a pad line, the border — is the pane's and does nothing. Nothing is selected by it, so
// handleMouseRelease falls through unchanged.
//
// A click OUTSIDE the box is not the pane's at all: it goes back unclaimed and the chain carries on
// with it, by the outside-click rule handleMouseClick states. The call is untouched by it —
// cancelling a decision stays esc's alone — and the transcript the human reads the call's context in
// stays draggable for as long as the pane stands.
//
// pre is the pre-click frame the pane is placed from (handleMouseClick) and the live model is what
// mutates — the chain's rule, and here it is also the reason the request the geometry is composed from
// is read off pre: sendApproval clears m.pending, and a second click composing the pane from the
// answered model would have nothing to place.
func (m Model) handleApprovalClick(pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
	hit := m.approvalHit(pre, msg.Y)
	switch hit.at {
	case hitAbsent, hitOutside:
		return m, nil, false // not up, or outside the box: unclaimed, and the chain goes on with it
	case hitRow:
	default:
		return m, nil, true // the pane's chrome, or the open rule field: claimed, with no row to take
	}
	if m.clickArmed.holds(panePrompt, hit.row) {
		if !m.approvalArmed {
			return m, nil, true // the latch, not the arm: the row stays armed for the click after the tick
		}
		// The row the POINTER highlighted, clicked again: the arm is spent here rather than left for
		// the next gesture to drop, because the decision it belonged to is taken.
		m.clickArmed = clickArm{}
		next, cmd := acceptedModel(m.resolveApproval())
		return next, cmd, true
	}
	m.approvalSel.seat(hit.row, hit.count)
	m.clickArmed = clickArm{pane: panePrompt, row: hit.row, top: hit.top, ok: true}
	return m, nil, true
}

// approvalHit is handleApprovalClick's decision for a point, read without acting on it ([paneHit]).
// Geometry is put to pre — the request it is composed from included — and every state gate to the
// receiver (handleMouseClick's rule).
func (m Model) approvalHit(pre Model, y int) paneHit {
	if m.state != stateAwaitingApproval || m.pending == nil || pre.pending == nil {
		return paneHit{}
	}
	req := pre.pending.Request
	row, top, inRect, onRow := popupPaneHit(pre, panePrompt, func() (string, popupPlacement) {
		return pre.approvalPromptPlaced(req)
	}, y)
	if !inRect {
		return paneHit{at: hitOutside}
	}
	if !onRow || m.editingProjectRule() {
		// The pane's chrome — or the open rule field, whose save is ⏎'s alone.
		return paneHit{at: hitInert}
	}
	return paneHit{at: hitRow, row: row, top: top, count: len(m.approvalMenuFor(req))}
}

// ----------------------------------------------------------------------------
// Mouse in the "/" | "@" dropdown (autocomplete.go)
// ----------------------------------------------------------------------------

// handleDropdownClick answers a left-click while the "/" | "@" menu is up. The pointer does what the
// keyboard does on this pane and no more: a click on a row moves the highlight onto it, the way ↑/↓
// do, and a SECOND click on that same row accepts it — the tab spelling (acceptAutocomplete),
// unconditionally, never ⏎'s fall-through to submit on an already-typed token
// (autocompleteExactMatch): tab is the key a dropdown over a text box is completed with, and it is
// the one whose answer never depends on what the box happens to hold.
//
// Two clicks ALWAYS, and on this pane the arm (clickArm, model.go) is what keeps that honest. The
// menu opens with row 0 already highlighted (computeAutocomplete), row 0 of the "/" menu is /clear
// (the alphabetical table, command.go) and accepting /clear RUNS it — so a handler that activated
// whatever the highlight already sat on would throw away the session on the pointer's FIRST press.
// The row an activating click may take is the row the POINTER put the highlight on, and the menu's
// own default is never one of those.
//
// A click OUTSIDE the box DISMISSES the menu and then goes on down the chain unclaimed — the report
// trio's currency (handleReportClick, reportpane.go) rather than the modals' dismiss-and-claim (call C as
// the owner amended it). This is the one list of the package that is NOT modal: it hangs over a chat
// box the human is still typing in, which is what its keys and its wheel already say
// (autocompleteKey, dropdownWheel), so the click that closes it is very often the click that seats
// the caret in that box or starts a drag over the transcript, and swallowing it would cost the human
// the gesture they actually made. Nothing is decided by the dismissal: the menu is DERIVED from the
// token in the box (recomputeAutocomplete) and the next keystroke there puts it back.
//
// Inside the box but on no row — the title, a pad line, the hint, a border — the click is the pane's
// and does nothing, exactly as it is on the four panes above: "not on a row" is the pane's chrome and
// "not on the pane" is somebody else's frame, and only the second one dismisses.
//
// The open gate comes BEFORE the rectangle, as dropdownWheel's does (autocomplete.go): ungated,
// inRect == false cannot tell "no menu on the frame" from "a click outside the menu", so every click
// in the program would clear the skillRegion edge-trigger the menu's dismissal carries with it.
//
// pre is the pre-click frame the pane is placed from and the live model is what mutates
// (handleMouseClick's rule); the geometry is composed from pre's own menu, so a second click reads
// the frame the human aimed at rather than one an accept has already re-derived.
func (m Model) handleDropdownClick(pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
	hit := m.dropdownHit(pre, msg.Y)
	switch hit.at {
	case hitAbsent:
		return m, nil, false
	case hitOutside:
		m.dismissAutocomplete() // outside a menu that decides nothing: it closes, and the click travels on
		return m, nil, false
	case hitRow:
	default:
		return m, nil, true // the pane's chrome: claimed, with no row to take
	}
	if m.clickArmed.holds(paneDropdown, hit.row) {
		// The row the POINTER highlighted, clicked again: the arm is spent here rather than left for
		// the next gesture to drop, because the menu it belonged to is being accepted from.
		m.clickArmed = clickArm{}
		next, cmd := acceptedModel(m.acceptAutocomplete())
		return next, cmd, true
	}
	m.autocomplete.seat(hit.row, hit.count)
	m.clickArmed = clickArm{pane: paneDropdown, row: hit.row, top: hit.top, ok: true}
	return m, nil, true
}

// dropdownHit is handleDropdownClick's decision for a point, read without acting on it ([paneHit]).
// Geometry is put to pre and every state gate to the receiver (handleMouseClick's rule).
func (m Model) dropdownHit(pre Model, y int) paneHit {
	if !m.openPanes().has(paneDropdown) || !pre.openPanes().has(paneDropdown) {
		return paneHit{}
	}
	row, top, inRect, onRow := popupPaneHit(pre, paneDropdown, pre.renderAutocompletePlaced, y)
	if !inRect {
		return paneHit{at: hitOutside}
	}
	if !onRow {
		return paneHit{at: hitInert} // the pane's chrome
	}
	return paneHit{at: hitRow, row: row, top: top, count: len(m.autocomplete.items)}
}

// highlightTranscript overlays the transcript drag-selection's background on the viewport's
// visible block, mirroring highlightInput: it shades the display cells between the selection's
// two content-anchored ends on each visible line. It is handed the transcript block View composed,
// so its rows are the transcript's by construction — it spends the frame's row budget
// ([Model.transcriptRows]) once as a loop bound and maps each row through drawnLineAt, the same
// mapping contentLineAt gives the mouse. So the highlight tracks the selection through a mid-drag
// wheel-scroll, and a header row highlights exactly when the header line under the span is the one
// drawn there. A prompt block's ▄/▀ half row is never shaded: it is paint the copy leaves out
// (transcriptSelectionText), so shading it would show a selection the clipboard does not hold.
// With no active (non-empty) selection the view is returned unchanged.
func (m Model) highlightTranscript(view string) string {
	if !m.transcriptSel.active || m.transcriptSel.anchor == m.transcriptSel.head {
		return view
	}
	top, bot := m.transcriptSel.anchor, m.transcriptSel.head
	if bot.line < top.line || (bot.line == top.line && bot.col < top.col) {
		top, bot = bot, top // normalise to reading order
	}
	drawn := m.transcriptRows()
	lines := strings.Split(view, "\n")
	for r := range lines {
		if r >= drawn {
			break // past the transcript: an overlay owns these rows, nothing of ours is drawn on them
		}
		absRow := m.drawnLineAt(r)
		if absRow < top.line || absRow > bot.line || m.padRows().isPad(absRow) {
			continue // a prompt block's half row copies as no text of its own, so it shows none selected
		}
		c0, c1 := 0, m.th.measure.Width(lines[r])
		if absRow == top.line {
			c0 = top.col
		}
		if absRow == bot.line {
			c1 = bot.col
		}
		if c1 <= c0 {
			continue
		}
		lines[r] = shadeCells(m.th.measure, lines[r], c0, c1, m.th.selection)
	}
	return strings.Join(lines, "\n")
}

// foldMouseWheel routes one wheel notch: to whichever open pane holds the pointer, and to the
// transcript everywhere else. The chain asks every pane the frame can have open, in the one order
// pointerPanes holds — the /settings pane (settingsWheel), the /usage, /inspect, /thinking and
// /advice reports (reportWheel), the /sessions browser (browserWheel), the /model | /server picker
// (pickerWheel), the approval menu and the ask offering (promptWheel), the "/" | "@" autocomplete
// dropdown (dropdownWheel) — and each one takes the notch only when the pointer is inside its own
// rectangle.
// Wherever two of those panes can never share a frame the order between them is arbitrary, because
// only one rectangle can hold the pointer at a time.
//
// The transcript is the FLOOR of that chain rather than its default: scrollViewport is what a notch
// reaches once no open pane has claimed it, so a pane never has to argue its way past a transcript
// that took the notch first. That is the whole doctrine — the pane under the POINTER owns the notch —
// and it is why the wheel answer does not follow focus or modality.
//
// A CLICK follows the notch. handleMouseClick asks the same panes in the same order, off the same
// rectangles and through the same shared hit-test (popupPaneHit over the painter's own placement),
// so the row a notch would walk onto is the row a click highlights and a second click on that row
// acts as ⏎ does. Only the answer to a point OUTSIDE every box differs between the two gestures,
// because only the click has a dismissal to spend: a notch outside a pane simply scrolls the
// transcript, while a click outside one may close the pane it missed — never cancel its question —
// and the per-pane spelling of that is stated once, on handleMouseClick, rather than here.
//
// The approval menu and the ask offering are the frame's SOFT-modal panes: the transcript stays
// scrollable underneath them, so what they claim is the notch INSIDE their box and only inside it,
// while a notch anywhere outside carries on down the chain and scrolls the transcript exactly as it
// did before they opened (ratified 2026-08-22). Their soft-modality stays a fact about KEYS.
//
// The wheel scrolls the transcript in every state — unlike the ordinary keyboard path, which is
// state-gated (idle/ask/running feed the input; only PgUp/PgDn scroll from the keyboard everywhere).
// Mouse reporting is enabled in View (MouseModeCellMotion, or AllMotion behind the pointer-shape
// gate — pointerprobe.go); the viewport's own Update turns the
// wheel into a scroll.
func (m Model) foldMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	// A notch drops any click-armed row, whichever pane below takes it: the wheel MOVES the highlight
	// on every list in this chain, and the latch means "the pointer highlighted this row". Cleared
	// here rather than in each pane's wheel handler, the pair of the drop handleKey makes (model.go)
	// — the two ways a highlight moves without a click, cleared in the two places they arrive.
	m.clickArmed = clickArm{}
	// The panes, in the one order both gestures ask them in (pointerPanes), each answering with its
	// row's wheel (paneSpecs): each takes the notch only when the pointer is inside its own rectangle,
	// and the first to take it answers.
	for _, p := range pointerPanes {
		if next, handled := paneSpecs[p].wheel(m, msg); handled {
			return next, nil
		}
	}
	return m.scrollViewport(msg)
}
