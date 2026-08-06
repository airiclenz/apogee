package tui

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// ----------------------------------------------------------------------------
// The shared selector-popup painter (selector-popup plan §1)
// ----------------------------------------------------------------------------
//
// renderPopup is the single place overlay-pane chrome is painted: a titled, bordered pane holding
// an optional wrapping body block, a scrolled row list with the selected row highlighted, and a
// key-hint footer. The /sessions history browser (sessions.go), the command, file and skill autocomplete
// dropdown (autocomplete.go), and the ask and approval prompts (model.go) all compose their pane
// through it, so every overlay shares one look and one right edge. The dependency points inward
// only — callers compose a popupSpec and hand it here; renderPopup reaches back into neither
// overlay's state.
//
// Contract:
//   - width is the TOTAL box width: the rounded border and the padding fold INTO width, so every
//     rendered line is exactly width display cells in the painter's own measure, and the pane is
//     exactly as many painted rows as it composed (drawBox, like renderStartupBox). Callers pass
//     m.width — the full window width, the same value the input box spans — so the pane's right
//     border lands on the terminal's last column, flush with the prompt box below it.
//   - The pane is filled solid black: the border style paints its border and padding cells black,
//     and every content line is padded out to the full inner width on the same black field, so no
//     interior cell (including the gap after a short row) is left on the terminal background.
//   - The module owns the marker (glyphUser + a space on the selected row, two spaces
//     otherwise — or glyphMenuUnselected in menu style, popupSpec.menuRows), the selected-row
//     highlight (th.userBlock's full bar, or the menu's accent run instead), the per-row treatment
//     a caller asks for by KIND (popupSpec.rowKinds: a section header's white label, an edited
//     row's own field), the scroll windowing
//     (popupRowWindow), and — since the column contract below — the COLUMN ALIGNMENT of the rows
//     themselves: callers hand over the FULL row list plus the global selected index, and
//     renderPopup lays the rows out, windows around the selection, and truncates every content
//     line to the inner budget, so no line can ever wrap the box.
//   - A row is ONE painted line unless the spec asks for more: popupSpec.wrapRows word-wraps a row
//     too wide for the pane onto continuation lines indented under its own first cell
//     (popupRowIndent) — plus, where the row is columned, under its LAST column
//     (popupRowHangingIndent) — instead of eliding its tail, popupSpec.rowGap sets consecutive rows one
//     blank line apart, and popupSpec.rowPadAbove/rowPadBelow set one blank line above the block
//     and one below it — each end asked for on its own, because the two decision surfaces do not
//     ask for the same shape. All of them are opt-in, and all are paid for out of the SAME row
//     budget: the window is measured in painted LINES (popupRowWindow), a row is seated only when
//     every line of it fits, and the separators between the seated rows are counted too — so a
//     wrapped option is never half on the screen and a pane never outgrows the rows the frame
//     budgeted it. The pad is the one that gives way, because it is the only one that is not
//     content: it is drawn out of what the seated rows LEFT OVER, so a blank line never costs a
//     decision surface an option it had the room to show.
//   - A row is a popupRow — a slice of CELLS, not a pre-concatenated string — and the module lays
//     the cells of every row out as vertically aligned columns (layoutPopupRows): each column is
//     as wide as its widest cell measured in DISPLAY cells over ALL rows (not just the visible
//     window, so alignment never shifts while scrolling), adjacent columns are separated by a
//     two-space gutter, a column empty in every row collapses to nothing, and the composed line is
//     right-trimmed before the marker/highlight/truncate steps run on it. Each popup kind picks a
//     fixed column schema and leaves an absent tier as an empty cell, which pads like any other so
//     the later columns stay aligned; grammar separators ("— ", "· ") lead the cell they belong to,
//     so the separator glyphs line up too. Truncation is whole-row, never per-column. A
//     single-cell row (singleCellRows) is the degenerate case and renders exactly as it did before
//     columns existed. Cells arrive escape-stripped, as they always did.
//   - The optional body block (spec.body) sits between the title and the rows and carries prose the
//     rows cannot: it is the ONE part the module word-wraps rather than truncates, because a
//     question or an approval reason/args body must break across lines instead of losing its tail
//     to an ellipsis. Embedded newlines are honoured as layout (the labelled arguments' one-per-line
//     shape — a `name:` line with its value's own lines indented beneath it — and the blank
//     separators survive), each segment is word-wrapped to the inner budget,
//     and the flattened block is capped at spec.maxBodyRows — negative = uncapped, ZERO = no body
//     rows at all, the same sense maxRows carries. Its first line may open with a LABEL
//     (popupSpec.bodyLead) the module paints as a heading, which is the one styled run inside a
//     block that is otherwise prose. Past a positive cap the last row becomes an explicit faint
//     "… (+N more lines)" marker counting the hidden lines, so the body never
//     exceeds its cap and truncation is never silent. wrapText is ANSI-unaware, so body arrives
//     PLAIN and escape-stripped — the module wraps first and styles after.
//   - HIDING CONTENT IS NEVER SILENT, at any budget OR any width — and that holds for the ROWS as
//     much as for the body. A cap of zero leaves no row for the marker either, so the pane says it
//     on the row it always has: the marker moves onto the TITLE row (popupTitleLine) rather than
//     the content vanishing without a word. That is what lets a pane shrink to its irreducible four
//     rows on a 12-row terminal (popupBudget) and still be honest — the approval prompt is a
//     security surface, and a decision must never be taken against text the pane dropped quietly.
//     The marker rides the title only when the block that owes it has no row of its own to put it
//     on: with one body row it is the body's own last line, exactly as before, and a row window
//     granted at least one row SCROLLS around the selection (popupRowWindow), so the rows outside
//     it are reachable rather than hidden. A row budget of ZERO is the case scrolling cannot
//     answer — every choice or entry gone while the hint still offers ↑↓ — so those rows are
//     counted onto the title as well, in the SAME marker and the same wording: what the pane holds
//     and is not showing is one fact, and a title row too narrow to seat one count has no room for
//     two. A pane too narrow to seat the phrase beside its name sheds the phrase's NOUN before the
//     count (popupElisionMarkerFitting) and its own name before the number, so the count survives
//     every width a pane can be drawn at — a short window is usually a narrow one too.
//
// Every overlay pane — the /sessions browser, the command, file and skill dropdowns, and the ask and
// approval prompts — now paints through this module; no boxed overlay renders its own chrome
// (plan D3).

// popupChrome is a pane's irreducible height: its two borders, its title row and its hint row. It
// is what renderPopup draws for a spec whose every budget is zero — the smallest a pane can be and
// still name itself and say how to act on it — so it is also the floor the frame's row allocation
// hands out before any surface gets a comfortable row ([Model.frameRowPlan]). A pane the frame
// cannot give this many rows is not drawn at all.
const popupChrome = 4

// popupTitleBorderChrome is that same irreducible height for a pane that draws no title ROW
// (popupSpec.titleInBorder): whatever name such a pane has rides a line the box was drawing anyway,
// so its floor is its two borders and its hint row — one row less than popupChrome. Its only
// production caller draws no title at ALL: the ask prompt's name went into the question itself
// (layout.md), so an empty title leaves the top border plain, and the border carries the question's
// own lead only on the windows that seat no line of it (popupSpec.titleFromBody). Either way the
// name costs no content row, which is the whole of what this constant states. It is the chrome such
// a pane hands [Model.popupBudget], which is what spends the freed row on the pane's own content
// instead of leaving it unclaimed. The frame's per-pane floor stays popupChrome
// ([Model.frameRowPlan]): the allocation cannot know a pane's title placement, and a floor one row
// generous seats a pane the budget then fills, where a floor one row short would seat one it cannot
// draw.
const popupTitleBorderChrome = popupChrome - 1

// popupBorderChrome is that height for a pane that draws NEITHER a title row nor a hint row: its
// name rides the top border (popupSpec.titleInBorder) and how to act on it is written into its own
// rows rather than into a legend beneath them (the approval menu's shortcut column). Its two border
// lines are the whole of its frame, so every row the frame grants past them is the pane's to spend.
// It is what such a pane hands [Model.popupBudget] — the parameter is the CALLER's precisely because
// only the caller knows which rows its spec will draw, and a pane that budgeted for a hint row it
// does not paint leaves the row unclaimed at exactly the window where it has fewest to spare.
const popupBorderChrome = popupTitleBorderChrome - 1

// popupFloor is what a pane's two content blocks each keep before the other claims the rest of the
// window ([Model.popupBudget]). The ZERO VALUE is the standing rule and every pane but one passes
// it: the ROWS take what they ask for and the body keeps the one line the budget has always left it,
// because on a picker, a browser or a dropdown the rows ARE the pane and the prose above them is a
// caption.
//
// body states a different pane: one whose prose is the thing being decided about, where a row list
// long enough to eat the whole grant leaves the question itself as a count (the ask prompt at eighty
// by twenty-four — four answers, the blanks between them and the pad around the block cost nine
// lines of ten, and the human is asked to pick between them without the question on the screen). It
// is the lines the body keeps FIRST, and the caller states its own prose's real wrapped height in
// it (popupBodyLineCount) rather than a taste, so a one-line question claims one line and the rows
// keep the rest.
//
// rows is what that claim may never take: the lines the row block needs to seat the one row the
// window is anchored on, which is what keeps a floor on the prose from emptying a decision surface at
// the heights where it has least to give (popupRowWindow seats a row whole or not at all, so a
// two-line answer needs both its lines or it is not on the screen). Stated by the caller for the same
// reason chrome is — only the pane knows how tall its own rows land. A body claim with no row claim
// beside it still leaves the rows one line.
type popupFloor struct {
	body int // lines the prose keeps before the rows claim the remainder
	rows int // lines the rows keep whatever the prose claims
}

// popupRow is one row of a popup as its columns: the escape-stripped cells the module lays out
// into vertically aligned columns. Every row of one spec follows that popup kind's fixed column
// schema — an absent optional tier is an empty cell, which still pads, so the columns after it
// stay aligned. A one-cell row (singleCellRows) is the degenerate case: one column, laid out as
// the plain label it holds.
type popupRow []string

// popupRowKind is what a row IS to the painter, where being it changes how the row is drawn. The
// zero value is the row every pane has always had — content, faint until the selection lands on it
// — so a spec that states no kinds at all renders exactly as it did before kinds existed.
//
// The two named kinds are the /settings pane's, and they are stated by the CALLER for the reason
// every other row fact is: the module lays rows out and knows nothing about what they mean. A
// SECTION HEADER is a label the row list is divided by rather than a row the selection can land on,
// and the eye has to find it without reading it (docs/layout/settings-screen-layout.md); a row being
// EDITED is the one the next keypress goes into, and saying so in colour is what tells a human
// typing into a list which of its rows is the field.
type popupRowKind int

const (
	popupRowPlain   popupRowKind = iota // content: faint, or the selection's own bar/accent
	popupRowHeading                     // a section label dividing the list (th.popupHeading)
	popupRowEditing                     // the row the pane is being typed into (th.popupEdit)
)

// popupSpec describes one boxed selector popup. title and hint each drop their row when empty;
// body is plain, escape-stripped prose the module word-wraps to the inner budget and caps at
// maxBodyRows (an empty body adds no rows); rows are the escape-stripped cell rows the module
// aligns into columns; selected indexes rows (−1 = no highlight); maxRows caps the scroll window
// around the selection, in the LINES that window paints — the same number as rows until wrapRows or
// rowGap makes a row cost more than one. The two caps read the same way: negative shows everything, and ZERO shows
// nothing — a window with no rows left to spare saying so (popupBudget) rather than the pane
// quietly showing all of them. Zero is the reading that keeps a pane inside the shortest window it
// can be drawn in at all, where its border, title and hint are the whole budget — and where a
// dropped body AND a dropped row list are both reported on the title row instead
// (popupTitleLine), so the cap costs the prose and the choices but never the knowledge that there
// are some.
//
// titleInBorder moves the title OFF its own content row and into the top border, centred between
// the border's own runes with one space each side — ╭────── Approve terminal? ──────╮ — so a
// decision surface spends the row on what the decision turns on instead of on a heading
// (docs/design/user-questions-layout.md). It costs the pane a row less than a title row does
// (popupTitleBorderChrome), and with an EMPTY title it opens the pane on a plain unbroken border,
// which is how a surface whose body is its own heading (the ask prompt's question) drops the
// heading without dropping the box. It changes where the title is drawn and nothing about what it
// says: the elision marker rides the border title exactly as it rides the title row
// (popupTitleLine), so a pane cannot go quiet about content it hid by moving its name into the frame.
//
// titleFromBody is the identity a pane keeps when its body IS its heading and the window seats no
// line of that body. The ask prompt is that pane: it draws no title, because the question says what
// a heading over it would (docs/design/user-questions-layout.md) — but a question is BODY, and on the
// shortest windows the single body row a pane is granted goes to the elision marker, which left the
// box carrying a count and a key hint and nothing at all about what the live ⏎ would answer. Its
// identity had become a number. With this set, the lead of the body falls back INTO the top border
// there — the same place a titled pane's name already rides (titleInBorder) — and only there: at
// every height where one line of the body is on the screen the pane composes exactly as it did, so
// the untitled top border stays the normal appearance of a surface whose first body line is its
// heading.
//
// It requires titleInBorder, and that is the whole of why it costs nothing: the top border is drawn
// at every height, so a name spliced into it claims no row. A fallback title on a pane that draws
// title ROWS would claim the row the budget had just refused it, which is the one thing shrinking may
// never do.
//
// menuRows paints the row list as a MENU rather than as a list with a cursor in it: the selected row
// is glyphUser plus its label lit in the accent (th.popupAccent), every other row is
// glyphMenuUnselected plus its label faint, and NO row carries th.userBlock's full-width highlight
// bar (docs/design/user-questions-layout.md). The difference is what the row list is FOR. A picker
// offers a long scrolled list of things to look through, and the bar is the position marker that
// finds the cursor in it; a decision surface offers four options to choose between, all of them on
// screen at once, and there the bar is a banner across a quarter of the pane rather than a pointer —
// the ❯ and the accent say the same thing in the width of one glyph, and every row that is not the
// answer stays out of the way. It is opt-in for exactly that reason: the flag off is the picker's
// rendering, unchanged down to the two-space marker (contract 3).
//
// wrapRows lets a row BREAK instead of eliding: a row wider than the pane word-wraps onto
// continuation lines hanging under its own first cell (popupRowIndent) — and, on a COLUMNED row,
// under its LAST column as well (popupRowHangingIndent), so a wrapped label lands under the label
// rather than under the checkbox beside it — the way the body block
// already breaks, so a long option arrives whole rather than ending in an ellipsis
// (docs/design/user-questions-layout.md). It is the difference between a list of NAMES and a list of
// SENTENCES. A picker offers file paths and session titles — things a human recognises from the
// start of them, where the ellipsis costs a tail already identified — while a decision surface
// offers prose written for this one question, where the tail is often the whole of the point ("…
// the config change is the riskier part — get it stable first"). A decision must not be taken
// against half a sentence, so a surface that asks in sentences wraps them; every other pane keeps
// the one-line row and the elision that has always fitted it.
//
// rowGap sets consecutive rows one blank line apart. It is what keeps a wrapped list READABLE: once
// a row can be three lines long, the only thing telling the eye where one option ends and the next
// begins is the marker column, and a blank line says it without being read. A list whose every row
// is one line needs none of that and pays no line for it.
//
// rowPadAbove sets the row BLOCK one blank line off what stands above it. It is what tells the eye
// that the rows are a MENU rather than more of the prose above them: on a decision surface the body
// asks and the rows answer, and unbroken they read as one paragraph whose last lines happen to start
// with a glyph. It is a separate flag from rowGap because the two say different things — the gap
// divides the options from EACH OTHER, the pad divides the offering from the pane — and the approval
// menu asks for the second without the first, its four one-line options staying adjacent as the
// mockup draws them.
//
// rowPadBelow closes the block with a blank line above the bottom border, and it is its OWN flag
// because the mockup does not spend it on both surfaces (docs/design/user-questions-layout.md): the
// ask box closes its offering, the approval box ends on its last decision. That is the mockup being
// consistent rather than arbitrary — the ask box's answers are already a blank line apart, so a
// block ending flush against the border would read as one option crowded where its siblings are not,
// while the approval's four adjacent rows end where the last of them does. Two flags rather than one
// derived from rowGap, because that is a coincidence of these two panes and not a rule: a caller
// says what its surface looks like instead of inferring it.
//
// Either pad is drawn only out of the lines the seated rows LEFT OVER (popupRowLines), so it never
// pushes an option out of the window: a short pane loses its breathing room rather than a decision.
//
// rowKinds says what each row IS where that changes how it is painted (popupRowKind) — a section
// header, a row being edited — parallel to rows, and short or nil for a pane whose rows are all
// content, which is every pane but /settings. It is a list rather than a flag pair because the fact
// belongs to a ROW: a header is one whether or not the selection is near it, and the selection
// itself already travels as an index.
//
// bodyLead is a run at the FRONT of the body's first line that is painted as a heading
// (th.popupBodyLead) instead of as prose — the "Description:" label of the /settings pane's own
// header. It is a lead rather than a block of its own because it is part of the sentence: the label
// and the text after it wrap as one paragraph and are budgeted as one block, so a caller that has to
// state its prose's height before the block is composed (popupBodyLineCount) still states the height
// the painter will pay. The module bolds it where the first line still opens with it and paints the
// line plain where truncation took it, so the styling can never claim a label the pane is not
// showing.
type popupSpec struct {
	title         string
	titleInBorder bool
	titleFromBody bool
	body          string
	bodyLead      string
	maxBodyRows   int
	rows          []popupRow
	rowKinds      []popupRowKind
	menuRows      bool
	wrapRows      bool
	rowGap        bool
	rowPadAbove   bool
	rowPadBelow   bool
	selected      int
	hint          string
	maxRows       int
}

// rowKind is what row i of the spec is: what rowKinds says, or plain where it says nothing. Every
// read goes through here so a caller may state kinds for the rows that have one and leave the slice
// short — or leave it out entirely, which is what keeps a spec that never heard of kinds rendering
// as it always did.
func (s popupSpec) rowKind(i int) popupRowKind {
	if i < 0 || i >= len(s.rowKinds) {
		return popupRowPlain
	}
	return s.rowKinds[i]
}

// popupPlacement is where a rendered pane's ROW LIST landed inside it: the index among the box's
// PAINTED rows — the top border counted as row 0 — of the row block's first line, and the
// [start, end) window of spec.rows that block holds. It is meaningful only for a pane that was
// actually drawn (renderPopupPlaced returning a non-empty view).
//
// It exists for the surface that has to map a SCREEN row back to one of its own rows — the mouse in
// the /settings pane (mouse.go) — and it is the PAINTER's own answer rather than a second derivation
// of it: the title row, the body block and the row window are all things the module decides, and a
// caller re-deriving them would name the row above or below the one under the pointer the first time
// a description wrapped differently than it guessed.
//
// Mapping a line back to a row is a subtraction only where every row is ONE line — a spec with
// neither popupSpec.wrapRows nor popupSpec.rowGap, which is what the /settings key list is. A spec
// whose rows cost more than a line each maps through blocks instead: the lines each row was composed
// into, in order, so the caller walks the window counting the heights the painter actually drew. The
// /settings multi-line field is that caller (settingsTextPaint) — its rows are the prompt's lines and
// they wrap.
type popupPlacement struct {
	rowsAt     int        // painted row index of the row block's first line
	start, end int        // the window of spec.rows that block holds
	blocks     [][]string // the composed lines of EVERY row, the window's and the rows outside it alike
}

// popupBoxBorderRow is the one row drawTitledBox draws above a pane's content lines: its top border.
// It is what turns an index into the composed CONTENT into an index into the pane's painted rows.
const popupBoxBorderRow = 1

// renderPopup paints the bordered selector pane described by spec at the given TOTAL width
// (lipgloss v2 folds the border and padding into width, so every returned line is exactly width
// cells). The inner content budget follows the border style — like renderStartupBox — rather
// than a hard-coded frame, and every content line (title, rows, hint) is truncated to it so none
// can wrap the box. The selected row within the scrolled window carries the glyphUser marker and
// the full-bar highlight; the others render faint.
func renderPopup(th theme, spec popupSpec, width int) string {
	view, _ := renderPopupPlaced(th, spec, width)
	return view
}

// renderPopupPlaced is renderPopup with the pane's row geometry reported alongside the paint
// (popupPlacement) — one composition, two answers, so a surface that maps a click back to a row
// reads the very numbers the painter spent. Every other pane asks for the paint alone.
func renderPopupPlaced(th theme, spec popupSpec, width int) (string, popupPlacement) {
	frame := th.popupBorder.GetHorizontalFrameSize()
	if width <= frame {
		// No room for even one content cell inside the border + padding: lipgloss cannot render a
		// bordered box narrower than frame+1, so a box here would overflow the View. Degrade to
		// nothing instead — the same way footerView blanks below 3 columns (plan D3).
		return "", popupPlacement{}
	}
	inner := popupInnerWidth(th, width)

	// Every content line sits on a solid-black field: the outer border style only paints its own
	// border and padding cells, so a line whose own styles carry no background would otherwise show
	// the terminal's default background through the pane (a black-hole strip). blackFill closes that
	// — title, plain rows, and hint all read as sitting on the same black pane — and drawBox pads
	// each line out to the full inner width on that same field. The selected row keeps th.userBlock's
	// dark-gray highlight bar, squared to the inner width itself, as its deliberate selection cue.
	//
	// It is deliberately NOT a Width style any more. lipgloss pads — and past its width WRAPS — in
	// GraphemeWidth whatever the painter is doing, so a line the authority calls exactly inner cells
	// wide can measure wider to lipgloss (any VARIATION SELECTOR-16 cluster does) and a Width style
	// would fold that one pane row into two: the pane outgrows the row budget popupBudget granted it
	// and neither half of the folded row reaches the pane's right border (ADR 0030 §5).
	blackFill := lipgloss.NewStyle().Background(colBlack)

	// BOTH content blocks are composed BEFORE the title row is written, because what they could not
	// fit changes what that row says: a pane granted no body rows and no row window reports those
	// elisions on its title, the one row it still has (popupTitleLine). Composing in the other order
	// would mean either a silent drop or a fifth row the frame has not got.
	body, hiddenBody := popupBodyLines(th, spec.body, spec.bodyLead, spec.maxBodyRows, inner, blackFill)
	block := popupRowLines(th, spec, inner, blackFill)

	// The two counts merge into the ONE marker the title row can seat: a hidden body block and a
	// hidden row list are the same fact about the pane — content it holds and is not showing — and a
	// row narrow enough to make the pane trade wording for width has no room to state it twice. The
	// body's count only lands here when the body block had no row to put its own marker on.
	hidden := block.hidden
	if len(body) == 0 {
		hidden += hiddenBody
	}

	title := popupTitleLine(th, popupHeading(spec, body, hiddenBody), hidden, inner)

	lines := make([]string, 0, len(body)+len(block.lines)+2) //nolint:mnd // +2: the optional title and hint rows
	if title != "" && !spec.titleInBorder {
		lines = append(lines, blackFill.Render(th.presentTitle.Render(truncateToWidth(th, title, inner))))
	}
	// The row block's first LINE is where the rows begin among the painted ones: the box's top border,
	// the title row where the spec spends one on itself, the body block, and whatever blank the row
	// block leads with (popupSpec.rowPadAbove) all stand above it.
	place := popupPlacement{
		rowsAt: popupBoxBorderRow + len(lines) + len(body) + block.lead,
		start:  block.start,
		end:    block.end,
		blocks: block.blocks,
	}
	lines = append(lines, body...)
	lines = append(lines, block.lines...)

	if spec.hint != "" {
		lines = append(lines, blackFill.Render(th.statusFaint.Render(truncateToWidth(th, spec.hint, inner))))
	}

	// A title-in-border spec spends no content row on its name: the composed title row — the name and
	// whatever elision it owes (popupTitleLine), the same string the title ROW would have carried —
	// goes to the box instead, and drawTitledBox splices it into the top border. It is fitted to the
	// SAME budget a title row is fitted to: the two spaces that set the name off the border cost
	// exactly the two padding cells a content row would have spent on it, so inner is the honest
	// width either way and the box's every-line-is-exactly-width contract holds at every width the
	// pane can be drawn at. An empty title leaves the border plain.
	borderTitle := ""
	if spec.titleInBorder {
		borderTitle = truncateToWidth(th, title, inner)
	}

	// drawBox rather than th.popupBorder.Width(width).Render: the pane's own rows are DRAWN, squared
	// to the inner width in the painter's measure, so one composed line is always one painted row and
	// the pane is exactly as tall as the frame budgeted for it. lipgloss.JoinVertical went with it —
	// it left-aligned by padding every row out to the widest row IT measured, which is the same
	// GraphemeWidth pad one level in.
	return strings.Join(drawTitledBox(th.measure, th.popupBorder, lines, width, borderTitle, th.presentTitle), "\n"), place
}

// popupGutter separates two adjacent popup columns: two spaces, the minimum gap between the widest
// cell of one column and the first cell of the next. A pop-up's columns are told apart by that gap
// alone — no rule between them, unlike a markdown table's (mdtable.go) — because a pane already
// has a border of its own and a second stroke inside it would read as a grid.
const popupGutter = "  "

// layoutPopupRows composes the spec's cell rows into the plain lines the pane paints, one line per
// row, every column padded to a single width so the columns line up vertically down the pane. The
// widths are measured over ALL rows — not just the window popupRowWindow will show — so scrolling
// a long list can never shift a column sideways. Each composed line is right-trimmed: the trailing
// pad of the last column carries no information, and the pane's black fill covers that gap
// already, which is what keeps a single-cell spec byte-identical to the plain labels it was
// composed from.
func layoutPopupRows(th theme, rows []popupRow) []string {
	widths := popupColumnWidths(th, rows)
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = layoutPopupRow(th, row, widths)
	}
	return out
}

// popupColumnWidths measures each column of the spec: the widest cell in it across every row, in
// DISPLAY cells (th.measure, the package's width authority — a CJK glyph is two cells wide and an
// escape sequence none), never a rune or byte count, so a wide-rune cell cannot under-measure its
// column and let the row spill past the pane's right border. The authority rather than
// ansi.StringWidth because the pad computed from this width is painted, and a column measured in a
// measure the painter is not on lands a cell off on any row carrying VARIATION SELECTOR-16
// (ADR 0030). A column no row filled measures 0, which is the signal layoutPopupRow collapses it on.
//
// A cell's TABs are expanded before it is measured, for the reason expandTabs (render.go) gives: a
// tab weighs nothing in this measure and four cells in every lipgloss style the row meets on its way
// to the screen (popupRowLines styles every row it emits). layoutPopupRow expands the same cell as
// it writes it, so the width measured here and the text padded out to it are the one string.
func popupColumnWidths(th theme, rows []popupRow) []int {
	columns := 0
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	widths := make([]int, columns)
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], th.measure.Width(expandTabs(cell)))
		}
	}
	return widths
}

// layoutPopupRow lays one row's cells into the measured column widths: each cell padded out to its
// column, the columns joined by popupGutter, and the whole line right-trimmed. A column that is
// empty in every row (width 0) collapses entirely — no width and no gutter — so an optional tier
// no row filled costs the pane nothing; an empty cell in a column another row DID fill still pads,
// which is what keeps the columns after an absent tier aligned. A row shorter than the schema is
// treated as ending in empty cells, so a producer may leave trailing tiers off.
//
// The cell is written with its TABs already expanded — the same expansion popupColumnWidths measured
// the column with — so the pad computed here lands on the text the pane actually paints. Left raw,
// the tab reached the pane's own style (popupRowLines) and became four spaces AFTER this row had
// been padded and the column settled, so every column to the right of it painted four cells right of
// the column the rows above and below it opened, and the pane's truncation ate that much off the
// far end of the row (expandTabs, render.go).
func layoutPopupRow(th theme, row popupRow, widths []int) string {
	var b strings.Builder
	written := 0
	for i, w := range widths {
		if w == 0 {
			continue // a column empty in every row collapses: no width, no gutter
		}
		if written > 0 {
			b.WriteString(popupGutter)
		}
		cell := ""
		if i < len(row) {
			cell = expandTabs(row[i])
		}
		b.WriteString(cell)
		if pad := w - th.measure.Width(cell); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		written++
	}
	return strings.TrimRight(b.String(), " ")
}

// popupCellColumn is the display column cell i of a laid-out row STARTS at, given the column widths
// the module measured over the whole list (popupColumnWidths): layoutPopupRow's own walk read
// forwards, gutters and collapsed columns included, so the answer is where the painter put that cell
// rather than where a second sum thinks it should be.
//
// It exists for the one caller that has to go the other way — the mouse seating a caret in the
// /settings edit row (mouse.go), which knows a screen column and needs the cell under it. A column no
// row filled collapses (no width, no gutter), so asking for one answers with the start of the next
// cell that is actually drawn; an index past the schema answers with the end of the row.
func popupCellColumn(th theme, widths []int, i int) int {
	gutter := th.measure.Width(popupGutter)
	col, written := 0, 0
	for c, w := range widths {
		if w == 0 {
			continue // a column empty in every row collapses: no width, no gutter (layoutPopupRow)
		}
		if written > 0 {
			col += gutter // the gutter is written BEFORE the cell, so it belongs to this column's start
		}
		if c >= i {
			return col
		}
		col += w
		written++
	}
	return col
}

// singleCellRows lifts a plain label list into one-cell popup rows — the shape a producer with
// nothing to align (a file suggestion, an ask_user choice) hands the module. One cell is one
// column, so the composed line is the label itself: the contract's promise that a single-cell row
// renders exactly as it did before the popup grew columns.
func singleCellRows(labels []string) []popupRow {
	rows := make([]popupRow, len(labels))
	for i, label := range labels {
		rows[i] = popupRow{label}
	}
	return rows
}

// popupRowBlock is a spec's composed row list: the painted lines, how many rows the window could not
// seat at all (hidden), the [start, end) window of spec.rows the lines hold, how many lines the
// block leads with before its first ROW (lead — the blank popupSpec.rowPadAbove asks for, and nothing
// else), and the per-row composition all of that was derived from (blocks). The counts travel with the
// lines because renderPopup owes two different answers about the same composition: what to paint, and
// where among the painted rows each row landed (popupPlacement).
type popupRowBlock struct {
	lines      []string
	hidden     int
	start, end int
	lead       int
	blocks     [][]string // what each row was composed into, before the window and the styling
}

// popupRowLines composes the spec's rows into the styled, black-filled content lines the pane
// paints — the selected row within the scroll window carrying the glyphUser marker and the
// full-bar highlight, the others faint — and reports how many rows it is NOT showing. In MENU style
// (popupSpec.menuRows) the marker and the styling change and nothing else does: the unselected rows
// lead with glyphMenuUnselected instead of two blank cells, the selected row is lit in the accent
// instead of barred, and the columns, the window and the count are the ones every other pane gets.
//
// The columns are measured and padded over the WHOLE row list before any windowing, so a row
// scrolled out of view still holds its column open and the alignment never shifts as the selection
// moves through a long list. Every composed line is truncated to inner, so a wide row can never
// wrap the box.
//
// A row is one line unless the spec asks for more (popupSpec.wrapRows, popupSpec.rowGap), and then
// the window is spent in LINES: a row is seated only when every line of it fits, and each separator
// between two seated rows costs a line of the same budget. Whole rows or none of them, because half
// an option on the screen is worse than the scroll that reaches the whole of it.
//
// The pad around the block (popupSpec.rowPadAbove, rowPadBelow) is spent LAST, out of the lines the
// seated window left over, and BOTH ends are dropped together where they do not cover it — a pane
// that keeps one blank and drops the other would move the block rather than tighten it. That order
// is the priority the pane is budgeted on (popupBudget): the rows are what the human acts on and the
// pad is what makes them pleasant to read, so a window that can seat every option unpadded shows
// every option — the approval menu keeps its four decisions on a short terminal and gives up its
// breathing room, which is the trade the other way round from the one a decision surface must never
// make.
//
// The hidden count is ALL-OR-NOTHING on purpose, and it is not the body's rule with the words
// changed. A window granted at least one row scrolls around the selection (popupRowWindow): the
// rows outside it are one keypress away, so they are off-screen rather than hidden, and a marker
// counting them would cost a row of the very list it is describing. A window that seats NO row is
// the case scrolling cannot answer — every choice or entry gone, no way to reach one, and a hint
// still offering ↑↓ to select among them — so THAT is what the pane owes an accounting for, and
// renderPopup carries the count to the title row (popupTitleLine), the one row the pane always has.
// With wrapped rows that case is reachable one step earlier than a zero budget: a budget of two
// lines and a three-line answer seats nothing either, and it is counted the same way rather than
// shown two thirds of.
func popupRowLines(th theme, spec popupSpec, inner int, blackFill lipgloss.Style) popupRowBlock {
	blocks := popupRowBlocks(th, spec.rows, spec.wrapRows, inner)
	heights := popupRowHeights(blocks)

	gap := 0
	if spec.rowGap {
		gap = 1
	}
	padAbove, padBelow := spec.rowPadAbove, spec.rowPadBelow
	capLines := spec.maxRows
	if capLines < 0 {
		// Negative spends whatever the whole list needs.
		capLines = popupRowBlockLines(heights, gap, popupRowPadLines(padAbove, padBelow))
	}
	start, end := popupRowWindow(spec.selected, heights, gap, capLines)
	if start == end {
		// No row on the screen: with rows on offer this is the budget's call, and the pane owes the
		// human the count (an empty offering owes nothing — there is no list to hide).
		return popupRowBlock{hidden: len(blocks), start: start, end: end, blocks: blocks}
	}
	if popupRowBlockLines(heights[start:end], gap, popupRowPadLines(padAbove, padBelow)) > capLines {
		// The block fits, the blank lines around it do not: the rows come first.
		padAbove, padBelow = false, false
	}

	out := make([]string, 0, capLines)
	blockLead := 0
	if padAbove {
		out = append(out, "")
		blockLead = 1 // the pad stands between the block's first LINE and its first ROW (popupPlacement)
	}
	for i := start; i < end; i++ {
		if gap > 0 && i > start {
			// A bare line, not a styled one: drawTitledBox pads every content line out on the box's own
			// field, so the empty string paints as a full-width black row — the separator is the pane
			// showing through, which is exactly what a gap between two options should be.
			out = append(out, "")
		}
		selected := i == spec.selected
		marker := "  "
		switch {
		case selected:
			marker = glyphUser + " "
		case spec.menuRows:
			// The dot is a marker like the chevron, in the same two cells: the labels of a menu line
			// up in one column whichever row is selected, so moving the selection moves the light and
			// not the text.
			marker = glyphMenuUnselected + " "
		}
		for j, text := range blocks[i] {
			// The marker leads the row's FIRST line and a blank of the same width leads every
			// continuation, so a wrapped option's tail lands under its own head rather than under the
			// pointer — one option reads as one block of text, and the marker column stays a column.
			lead := marker
			if j > 0 {
				lead = strings.Repeat(" ", popupRowIndent)
			}
			out = append(out, popupRowLine(th, spec, blackFill, truncateToWidth(th, lead+text, inner), i, selected, inner))
		}
	}
	if padBelow {
		out = append(out, "")
	}
	return popupRowBlock{lines: out, start: start, end: end, lead: blockLead, blocks: blocks}
}

// popupWrapOffsets is where each line of a wrapped SINGLE-CELL row began in the row's own text: the
// rune offset of blocks[i]'s j-th line into the text it was composed from. It is the wrap read
// backwards, and it exists for the one caller that has to answer in the row's own coordinates rather
// than in the pane's — the pointer in the /settings multi-line field, which turns a clicked cell into
// a caret position in the prompt being written (mouse.go).
//
// The walk is a match rather than an arithmetic because a word wrap DROPS the blank it broke on
// (popupRowBlocks → wrapText): each line is looked for at the cursor, blanks are stepped over until it
// is found there, and the cursor then advances by the line's own length. Stepping over blanks ONLY
// where the line does not already start at the cursor is what keeps a line the wrap kept its
// indentation on (a hard break inside a run of spaces) landing on its own first space rather than
// past it. A single-cell row is the whole of the contract: a columned row's continuation lines carry
// a hanging indent that is not in the text at all (popupWrappedRowLines), and no caller needs that.
func popupWrapOffsets(text string, lines []string) []int {
	runes := []rune(text)
	offsets := make([]int, len(lines))
	at := 0
	for i, line := range lines {
		for at < len(runes) && runes[at] == ' ' && !strings.HasPrefix(string(runes[at:]), line) {
			at++
		}
		offsets[i] = at
		at = min(at+len([]rune(line)), len(runes))
	}
	return offsets
}

// popupRowLine styles ONE painted line of a row — the whole row when it did not wrap, and every line
// of it when it did, so a selected answer is lit or barred over its full height rather than on its
// first line with its tail reading as somebody else's row.
//
// The row's KIND is read before its selection (popupSpec.rowKinds), because the two named kinds both
// outrank it: a row being EDITED is selected by construction and has to say which of the two states
// it is in, and a section HEADER is a label the selection cannot land on at all, so a kind that
// disagreed with the selection would be describing a row the pane cannot produce.
func popupRowLine(th theme, spec popupSpec, blackFill lipgloss.Style, row string, i int, selected bool, inner int) string {
	switch kind := spec.rowKind(i); {
	case kind == popupRowEditing:
		// Squared like the selection bar below and for the same reason — the field is what says the
		// whole row is the field — in the edit tone rather than in the selection's own.
		return th.popupEdit.Render(squareLine(th.measure, row, inner))
	case kind == popupRowHeading:
		return blackFill.Render(th.popupHeading.Render(row))
	case selected && spec.menuRows:
		// No squareLine here, deliberately: the accent is on the CHOSEN WORDS, so it stops where
		// they do and the rest of the row stays the pane's own black (drawTitledBox pads it out on
		// that field like every other content line). Padding the row first would light the empty
		// tail as well and hand the menu back the bar it exists to avoid.
		return blackFill.Render(th.popupAccent.Render(row))
	case selected:
		// Squared in the authority's measure before the style is applied, the way renderUserBlock
		// pads its own rows: the highlight bar spans the full inner width on the block's OWN
		// dark-gray field rather than the pane's black, and a lipgloss Width would fold the row
		// instead of padding it whenever the two measures disagree about it (ADR 0030 §5).
		return th.userBlock.Render(squareLine(th.measure, row, inner))
	default:
		return blackFill.Render(th.statusFaint.Render(row))
	}
}

// popupRowIndent is the width of the marker column every row leads with — glyphUser,
// glyphMenuUnselected or two blank cells, one cell plus the space after it either way — and so also
// the HANGING INDENT a wrapped row's continuation lines carry (popupSpec.wrapRows). It is what a
// wrapped row is wrapped TO: the text budget is the pane's inner width less this, on the first line
// and every line after it alike, so the block of text is a rectangle under the marker rather than a
// paragraph that steps left where the pointer stops. A COLUMNED row hangs its continuations this far
// plus its own leading columns (popupRowHangingIndent), for the same reason one column further in.
const popupRowIndent = 2

// popupRowBlocks composes each row into the plain text lines it will paint: one laid-out line per
// row as it always was, or — with wrap set (popupSpec.wrapRows) — the word-wrapped lines of a row
// too wide for the marker column and the pane's inner width together. The wrap is wrapText, the same
// break the body block gets, so a row and a body paragraph divide their words the same way; the
// non-wrapping path returns the laid-out row untouched and leaves the elision to the caller's
// truncateToWidth, which is what keeps a picker's rendering byte-identical to the one it had before
// rows could wrap.
//
// It takes the CELLS rather than the composed lines because a columned row wraps under its own last
// column (popupRowHangingIndent) and the composed line no longer says where that column starts: the
// row is split back into the leading tiers and the prose after them, the prose is what breaks, and
// the continuations are indented to where it began. A single-cell row has no leading tier, so its
// head is empty and the whole path collapses to the wrapText call it always was.
func popupRowBlocks(th theme, rows []popupRow, wrap bool, inner int) [][]string {
	if !wrap {
		lines := layoutPopupRows(th, rows)
		blocks := make([][]string, len(lines))
		for i, line := range lines {
			blocks[i] = []string{line}
		}
		return blocks
	}

	widths := popupColumnWidths(th, rows)
	last := popupLastColumn(widths)
	hang := popupRowHangingIndent(th, widths, last)
	budget := max(1, inner-popupRowIndent)
	blocks := make([][]string, len(rows))
	for i, row := range rows {
		head, tail := popupRowSplit(th, row, widths, last)
		if hang == 0 || hang >= budget {
			// One column, or a pane so narrow the leading tiers have nothing to hang prose under:
			// the composed line breaks whole, exactly as it did before rows could be columned.
			blocks[i] = wrapText(th, strings.TrimRight(head+tail, " "), budget)
			continue
		}
		blocks[i] = popupWrappedRowLines(th, head, tail, budget, hang)
	}
	return blocks
}

// popupLastColumn is the index of the last column any row filled — the column a wrapping spec
// BREAKS, because the columns before it are fixed tiers (a marker, a shortcut, a size) and the last
// is where the prose written for the human lives. It is −1 when no row filled a column at all.
func popupLastColumn(widths []int) int {
	for i := len(widths) - 1; i >= 0; i-- {
		if widths[i] > 0 {
			return i
		}
	}
	return -1
}

// popupRowHangingIndent is how far past the marker column a wrapped COLUMNED row's continuation
// lines start: the width of every column before the last, each with the gutter that follows it. It
// is what makes a checkbox a column rather than a prefix — the tail of a wrapped label hangs under
// the label, so the markers stay a column of their own down the pane and one option still reads as
// one block of text. A single-cell row measures zero here and keeps the rendering it always had.
func popupRowHangingIndent(th theme, widths []int, last int) int {
	hang := 0
	for i, w := range widths {
		if i >= last || w == 0 {
			continue // at or past the wrapped column, or a column no row filled (it collapses)
		}
		hang += w + th.measure.Width(popupGutter)
	}
	return hang
}

// popupRowSplit lays a row out like layoutPopupRow but stops short of the column that wraps: the
// head is every column before it, padded to its measured width and gutter-joined (exactly
// popupRowHangingIndent cells, whatever the row put in them), and the tail is the wrapping column's
// own text. strings.TrimRight(head+tail, " ") is layoutPopupRow's line for the same row, so the
// split changes what a row is COMPOSED of and nothing about what it paints.
func popupRowSplit(th theme, row popupRow, widths []int, last int) (head, tail string) {
	var b strings.Builder
	for i := 0; i < last; i++ {
		if widths[i] == 0 {
			continue // a column empty in every row collapses: no width, no gutter
		}
		cell := ""
		if i < len(row) {
			cell = expandTabs(row[i])
		}
		b.WriteString(cell)
		if pad := widths[i] - th.measure.Width(cell); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		b.WriteString(popupGutter)
	}
	if last >= 0 && last < len(row) {
		tail = expandTabs(row[last])
	}
	return b.String(), tail
}

// popupWrappedRowLines breaks a columned row's prose across lines and hangs it under its own column:
// the head leads the first line, every line after it starts hang blank cells in, and the text breaks
// to what is left of the budget on all of them alike — so the block is a rectangle under the column
// it belongs to rather than a paragraph that steps left where the tiers beside it end. Each composed
// line is right-trimmed, like layoutPopupRow's, so a row that wrapped and a row that did not are
// trimmed the same way before the painter's marker, styling and truncation run on them.
func popupWrappedRowLines(th theme, head, tail string, budget, hang int) []string {
	wrapped := wrapText(th, tail, budget-hang)
	indent := strings.Repeat(" ", hang)
	out := make([]string, len(wrapped))
	for i, line := range wrapped {
		lead := indent
		if i == 0 {
			lead = head
		}
		out[i] = strings.TrimRight(lead+line, " ")
	}
	return out
}

// popupRowHeights is how many painted lines each composed row costs — the number the window spends
// its budget in (popupRowWindow).
func popupRowHeights(blocks [][]string) []int {
	heights := make([]int, len(blocks))
	for i, block := range blocks {
		heights[i] = len(block)
	}
	return heights
}

// popupRowPadLines is what a spec's pad flags cost the row budget: one line per padded END of the
// block, whichever way the rows inside it are laid out. It is a function rather than two constants
// because both halves of the budget spend the same figure — the painter draws the pad and the CALLER
// has to ask the frame for the lines it takes ([Model.popupBudget]) — and a pane budgeting for one
// blank line and painting two is the overflow the line accounting exists to prevent. Callers state
// the same two flags here that they set on their popupSpec.
func popupRowPadLines(above, below bool) int {
	lines := 0
	if above {
		lines++
	}
	if below {
		lines++
	}
	return lines
}

// popupRowBlockLines is what showing the WHOLE list would cost: every row's lines, a separator
// between each adjacent pair (gap = 0 when the spec asks for none), and the pad around the block
// (popupRowPadLines, 0 when it asks for neither end). It is the budget an uncapped row
// list (maxRows < 0) is windowed against and the figure a caller states its own demand in, so "show
// everything", "show what fits" and "ask the frame for the room" are all the one arithmetic rather
// than three that have to agree. An EMPTY list costs nothing at all, pad included: there is no block
// for a blank line to sit around.
func popupRowBlockLines(heights []int, gap, pad int) int {
	if len(heights) == 0 {
		return 0
	}
	total := pad + gap*(len(heights)-1)
	for _, h := range heights {
		total += h
	}
	return total
}

// popupFlatRowHeights is what a NON-wrapping spec's rows cost in painted lines: one apiece, whatever
// they measure, because such a row elides its tail rather than breaking it onto a second line
// (popupRowBlocks). It is popupWrappedRowHeights' counterpart for a caller that has to state its row
// budget in LINES and knows its own rows never wrap — the approval menu, whose four labels are the
// pane's own and not the model's.
func popupFlatRowHeights(rows int) []int {
	heights := make([]int, rows)
	for i := range heights {
		heights[i] = 1
	}
	return heights
}

// popupInnerWidth is the content budget inside a pane drawn at the given TOTAL width: the width less
// whatever the border style spends on itself, floored at one cell (renderPopup, which returns nothing
// at all below that floor). It is a function rather than an inline subtraction because a CALLER needs
// it now too — a spec whose rows wrap cannot know what its rows will cost in lines without knowing
// the width they will be broken to, and a caller measuring against a frame of its own would budget
// for a pane the painter is not drawing.
func popupInnerWidth(th theme, width int) int {
	return max(1, width-th.popupBorder.GetHorizontalFrameSize())
}

// popupWrappedRowHeights is what each of rows will cost in painted LINES on a wrapping spec
// (popupSpec.wrapRows) drawn at the given TOTAL width — the per-row costs a caller needs to state its
// row budget in the LINES popupSpec.maxRows is denominated in ([Model.popupBudget]).
//
// It exists because the two halves of that budget are computed in different places: the pane knows
// how many options it is offering, the painter knows how tall each of them lands, and a pane that
// handed the frame its row COUNT would ask for three rows and paint eight. It composes exactly the
// calls renderPopup makes to reach the same numbers, in the same order, so the demand a caller
// budgets for is the cost the painter then pays.
func popupWrappedRowHeights(th theme, rows []popupRow, width int) []int {
	return popupRowHeights(popupRowBlocks(th, rows, true, popupInnerWidth(th, width)))
}

// popupBodyLines word-wraps spec.body into styled, black-filled content lines for the pane, sitting
// between the title and the rows, and reports how many wrapped lines it is NOT showing. Each
// embedded newline is layout the caller composed (the approval args' JSON indentation and its blank
// separator lines), so the block is split on "\n" and each segment is word-wrapped to inner
// independently — an empty segment yields one blank row. When the flattened line count exceeds
// maxBodyRows (> 0), the block keeps the first maxBodyRows−1 lines and appends a faint
// "… (+N more lines)" marker counting the hidden lines — the short "… +N" form on a pane too narrow
// to seat that phrase (popupElisionMarkerFitting), so the count is stated rather than clipped — so
// it never exceeds maxBodyRows rows and the truncation is never silent; a NEGATIVE maxBodyRows
// shows every wrapped line and ZERO shows none at all. Body lines render normal (th.popupBody) —
// the marker faint (th.statusFaint) — each padded on the same black field as every other content
// line and clipped to inner so, like every popup line, none can wrap the box.
//
// The hidden count is returned rather than kept private because a budget of ZERO leaves no row to
// put the marker on: the block is empty, the count is the whole body, and renderPopup carries the
// marker up to the title row (popupTitleLine). So "hidden > 0 with no lines" is the pane's signal
// that it owes the human a word about prose it cannot show, not a licence to drop it quietly.
//
// lead is the spec's bodyLead: the run at the front of the FIRST line the module paints as a heading
// rather than as prose. It is styled after the wrap and after the truncation, never before, so the
// label is bolded only where the line the pane is drawing still opens with it.
func popupBodyLines(th theme, body, lead string, maxBodyRows, inner int, blackFill lipgloss.Style) ([]string, int) {
	if body == "" {
		return nil, 0
	}

	wrapped := popupBodyWrapped(th, body, inner)

	if maxBodyRows == 0 {
		// A window with nothing left to spend on prose (popupBudget), not an invitation to show all
		// of it: honouring the budget is what keeps the pane inside the frame it is drawn in. The
		// lines are still COUNTED — dropping them is the budget's call, hiding the fact is nobody's.
		return nil, len(wrapped)
	}

	marker := ""
	hidden := 0
	if maxBodyRows > 0 && len(wrapped) > maxBodyRows {
		hidden = len(wrapped) - (maxBodyRows - 1)
		wrapped = wrapped[:maxBodyRows-1]
		marker = popupElisionMarkerFitting(th, hidden, inner)
	}

	out := make([]string, 0, len(wrapped)+1)
	for i, ln := range wrapped {
		line := truncateToWidth(th, ln, inner)
		if i == 0 {
			out = append(out, blackFill.Render(popupBodyLeadLine(th, line, lead)))
			continue
		}
		out = append(out, blackFill.Render(th.popupBody.Render(line)))
	}
	if marker != "" {
		out = append(out, blackFill.Render(th.statusFaint.Render(truncateToWidth(th, marker, inner))))
	}
	return out, hidden
}

// popupBodyLeadLine styles a body line whose front is a LABEL (popupSpec.bodyLead): the label as a
// heading, the rest of the line as the prose it leads. Both halves carry the pane's own black field,
// like every other styled run this module composes (skillAccent's argument, theme.go): a style with
// no background of its own would cut a notch of the terminal's through the pane, and each run's
// own reset ends the fill the enclosing black would otherwise have carried past it.
//
// A line that does NOT open with the label is painted plain — the width the pane was drawn at cut
// the label off (truncateToWidth), and a heading style over what survived would be bolding a word
// that is no longer the label.
func popupBodyLeadLine(th theme, line, lead string) string {
	if lead == "" || !strings.HasPrefix(line, lead) {
		return th.popupBody.Render(line)
	}
	return th.popupBodyLead.Render(lead) + th.popupBody.Render(strings.TrimPrefix(line, lead))
}

// popupBodyWrapped breaks a body block into the lines it wants at the given inner width, before any
// budget is applied to it: each embedded newline is layout the caller composed, so the block is split
// on "\n" and each segment word-wrapped independently. It is the one wrap both halves of the body's
// accounting go through — the painter's (popupBodyLines) and the CALLER's (popupBodyLineCount) — for
// the reason popupWrappedRowHeights exists on the row side: a pane that has to state its prose's cost
// before the block is composed must state the cost the painter will actually pay.
func popupBodyWrapped(th theme, body string, inner int) []string {
	if body == "" {
		return nil
	}
	var wrapped []string
	for _, seg := range strings.Split(body, "\n") {
		wrapped = append(wrapped, wrapText(th, seg, inner)...)
	}
	return wrapped
}

// popupBodyLineCount is what body will cost in painted LINES on a pane drawn at the given TOTAL
// width, uncapped — the figure a caller needs to claim room for prose it will actually use
// (popupFloor.body). Claiming a fixed taste instead would reserve three lines for a one-line
// question and hand the rows a budget two lines short of the block they would otherwise have seated.
func popupBodyLineCount(th theme, body string, width int) int {
	return len(popupBodyWrapped(th, body, popupInnerWidth(th, width)))
}

// popupElisionMarker is the ONE phrase a pane uses to say prose it holds is not on the screen,
// wherever that phrase has to be shown — the body block's last row, or the title row when the body
// block has no rows at all. One wording, so the same fact never reads as two different ones.
func popupElisionMarker(hidden int) string {
	return fmt.Sprintf("… (+%d more lines)", hidden)
}

// popupElisionMarkerShort is that same phrase at the width a NARROW pane can pay: the count with
// the noun dropped. It keeps the "… +" lead-in a clipped tool result already carries in the
// transcript (outputDetail), so it reads as the same statement made shorter rather than a second
// wording — and it costs some thirteen cells less, which is the difference between a title row that
// seats the count and one that clips it off the end. A pane short enough to have no body row is
// usually a split pane, and a split pane is usually narrow too, so the two squeezes arrive
// together; the noun is what the row can afford to lose, because the row already says which pane it
// belongs to.
func popupElisionMarkerShort(hidden int) string {
	return fmt.Sprintf("… +%d", hidden)
}

// popupElisionMarkerFitting picks the widest form of the marker that fits budget display cells: the
// full phrase where there is room for it, the short form where there is not. It is the ONE place
// the pane trades wording for width, so the body block's last row and the title row shed the same
// words in the same order. When even the short form is wider than the budget the pane is narrower
// than any statement of the fact can be — the caller's truncateToWidth still holds the width
// contract, and the count leads the short form, so what survives the clip is the number rather than
// the noun.
func popupElisionMarkerFitting(th theme, hidden, budget int) string {
	if marker := popupElisionMarker(hidden); th.measure.Width(marker) <= budget {
		return marker
	}
	return popupElisionMarkerShort(hidden)
}

// popupHeading is the name popupTitleLine composes its row — or, for a title-in-border spec, its top
// border — around: the spec's own title, or, on a pane whose body is its heading
// (popupSpec.titleFromBody), the lead of that body on the windows that seated no line of it.
//
// WHICH windows those are is read off the composed block rather than re-derived from the budget the
// block was composed against, because the block is where the two things that decide it meet: the
// budget the frame granted, and how many lines the prose wrapped onto at this width. A block that
// hid lines (hiddenBody > 0) and came back one line or none is a block whose every line went to the
// elision marker — the pane is showing a count where its identity should be — and that is exactly
// the case the fallback is for. A block still holding one of its own lines is a pane already saying
// what it is, and gets its plain border back unchanged.
//
// The lead is the body flattened onto a single line, its newlines and runs of spaces closed up: the
// title row spends the width it has on the FRONT of the name (popupTitleLine), which is where a
// question identifies itself, and a heading that kept the body's own line breaks would spend that
// width on the layout of prose that is not being shown.
func popupHeading(spec popupSpec, body []string, hiddenBody int) string {
	if spec.title != "" || !spec.titleFromBody || !spec.titleInBorder {
		return spec.title
	}
	if hiddenBody == 0 || len(body) > 1 {
		return "" // a line of the body is on the screen: the pane is naming itself already
	}
	return strings.Join(strings.Fields(spec.body), " ")
}

// popupTitleLine composes the pane's title row: the spec's title, plus the elision marker for the
// hidden lines that have no row of their own to be counted on — the body block when it got NO rows,
// the row list when the window granted it NONE, or both together (renderPopup sums them; one fact,
// one marker). This is the fallback that keeps hidden content from ever being silent. On a
// 12–15-row terminal popupBudget grants a prose-bearing pane a body budget of zero and an
// eight-entry offering a row window of zero — a fifth row would push the input box off the frame
// (D2) — so the marker has to ride a row the pane already owns, and the title is the row every pane
// draws. It is deliberately not the hint: the hint is how the human acts on the pane, and on the
// approval prompt that legend is the decision itself.
//
// A title-less spec with hidden content gets the marker AS its title row rather than losing it. The
// ask prompt composes one at every height where its question is on the screen — the question is its
// name, so its border carries the count alone — and popupHeading is what keeps that from becoming a
// pane with NO identity on the heights that seat the question nowhere.
//
// NARROWNESS IS NOT AN EXCUSE EITHER. The row is composed TO the pane's inner width rather than
// composed long and clipped to it: clipping put the count at the end of a row it did not fit, so at
// 42 columns and below the name won and the elision went silent again — on a terminal that is both
// short and narrow, which is one split pane, not two unlikely coincidences. So the width is spent
// in the order the row is read for: the pane's name, then the count, then the phrasing around the
// count. Full phrase beside the whole name where both fit; the short form ("… +3") where they do
// not; and only past that is the NAME clipped to an ellipsis, never the number — a decision is
// taken against a name the human can still read, and taken knowing there is text behind it. When
// not even a clipped name survives, the count is the whole row: on a pane that narrow the name is
// no longer identifying anything anyway.
func popupTitleLine(th theme, title string, hidden, inner int) string {
	if hidden == 0 {
		return title
	}
	if title == "" {
		return popupElisionMarkerFitting(th, hidden, inner)
	}
	gutter := th.measure.Width(popupGutter)
	marker := popupElisionMarkerFitting(th, hidden, inner-gutter-th.measure.Width(title))
	if room := inner - gutter - th.measure.Width(marker); th.measure.Width(title) > room {
		if title = truncateToWidth(th, title, room); title == "" {
			return marker
		}
	}
	return title + popupGutter + marker
}

// popupRowWindow returns the [start, end) slice of a row list to show at once: the rows whose
// painted LINES fit inside budget lines together, gap lines apart, scrolled to keep the selection
// roughly centred so a long list never overflows the pane. heights holds every row's line count in
// order — one apiece until popupSpec.wrapRows makes some of them taller — and gap is the separator
// popupSpec.rowGap asks for, 0 when it asks for none.
//
// It counts LINES rather than rows because that is what the pane is short of: the budget it is
// handed (popupSpec.maxRows) comes from a frame allocation measured in terminal rows (popupBudget),
// and a list whose rows can wrap would otherwise promise four of them and paint nine. On the list
// every other pane has — one line a row, no separators — counting lines is counting rows, and this
// returns the very window it returned before it knew the difference.
//
// The window grows OUT OF the selected row, alternately upwards and downwards, and takes a row only
// when its whole height and the separator before it still fit: a row is seated entirely or not at
// all, so the pane never paints the first two lines of a three-line answer and leaves the human to
// decide against the half of it that is on the screen. When one side runs out — of rows or of budget
// — the other keeps growing, which is what clamps the window at the ends of the list.
//
// An empty window is the honest answer to a budget that cannot seat even the selected row: the pane
// shows no rows at all and renderPopup carries their count onto the title row (popupTitleLine),
// rather than showing a fraction of an option and saying nothing about the rest.
func popupRowWindow(selected int, heights []int, gap, budget int) (int, int) {
	total := len(heights)
	if total == 0 {
		return 0, 0
	}
	// A selection outside the list (−1: no highlight, the ask prompt once free text is typed) anchors
	// the window at the top, which is where a list with no cursor in it is read from.
	anchor := clampInt(selected, 0, total-1)
	if heights[anchor] > budget {
		return 0, 0
	}

	start, end, spent := anchor, anchor+1, heights[anchor]
	for {
		grew := false
		if start > 0 && spent+gap+heights[start-1] <= budget {
			spent += gap + heights[start-1]
			start--
			grew = true
		}
		if end < total && spent+gap+heights[end] <= budget {
			spent += gap + heights[end]
			end++
			grew = true
		}
		if !grew {
			return start, end
		}
	}
}

// truncateToWidth clips s to at most width DISPLAY cells, ending in an ellipsis when it had to cut
// — so a long file path, a wide-rune title, or an aligned row too wide for the pane never
// overflows the terminal and breaks the overlay's layout. It is the package's elision, not the
// overlay's alone: the start-up card's stacked info rows fit themselves through it too
// (renderStartupStacked), so a cut value looks the same wherever one is made.
//
// The measure is th.measure, the package's
// width authority (width.go), not a rune count: a CJK glyph occupies two terminal cells and an
// escape sequence none, so counting runes would let a wide row spill past the right border while
// measuring as if it fit — the exact bug the pane's every-line-is-exactly-width contract cannot
// survive. It measures AND cuts through the authority rather than through ansi.StringWidth and
// ansi.Truncate, because a width measured in one method and cut in another is the same defect one
// step later (ADR 0030 §3) and the cut is what the painter then draws. A width of 1 or less leaves
// no room for text beside the ellipsis, so the line degrades to nothing rather than to a lone "…".
func truncateToWidth(th theme, s string, width int) string {
	if width <= 1 {
		return ""
	}
	if th.measure.Width(s) <= width {
		return s
	}
	return th.measure.Truncate(s, width, "…")
}
