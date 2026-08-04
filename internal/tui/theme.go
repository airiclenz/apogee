package tui

import (
	lipgloss "charm.land/lipgloss/v2"
)

// ----------------------------------------------------------------------------
// The theme (P2.7 — TUI presentation pass)
// ----------------------------------------------------------------------------
//
// theme is the single place the look-and-feel lives: the palette, the marker glyphs, and the
// reusable lipgloss styles every renderer draws with — including spinnerBase, the field the
// status-line spinner paints on (its frames and their timing live with the animation, in
// spinner.go). It is built once in newModel and stored as a Model value field. A lipgloss.Style
// holds no self-referential no-copy type (it is value-copy by design — its whole API returns new
// Styles), so a theme of Styles is safe inside the value-copied Model (ADR 0011;
// TestModelNoBuilderByValue guards the strings.Builder case structurally).

// The palette. Colours are hex so lipgloss maps them to the terminal's profile; the two
// "dark gray" roles (the user block's background and the chrome's borders) share one tone,
// matching the layout sketch (layout.md).
var (
	colWhite    = lipgloss.Color("#ffffff") // user-prompt text
	colDarkGray = lipgloss.Color("#4a4a4a") // user-block background + input/footer borders
	colDimGray  = lipgloss.Color("#333333") // top-edge divider hairline — dimmer than the chrome border so it recedes
	colBlack    = lipgloss.Color("#000000") // input-box interior
	colFaint    = lipgloss.Color("#8a8a8a") // status/footer/tool-detail dim
	colDiffAdd  = lipgloss.Color("#3fb950") // diff "+" lines (reserved — no producer yet)
	colDiffDel  = lipgloss.Color("#f85149") // diff "-" lines (reserved — no producer yet)
	colError    = lipgloss.Color("#f85149") // recovered-fault notices
	colCode     = lipgloss.Color("#f0883e") // inline `code` + fenced code blocks (orange)

	// The autonomy-mode footer markers, warming up the privilege ladder (least → most
	// autonomous): plan turquoise-green, ask-before green, allow-edits blue, auto orange.
	colModePlan       = lipgloss.Color("#2afefa") // plan — turquoise green
	colModeAskBefore  = lipgloss.Color("#3fb950") // ask-before — green
	colModeAllowEdits = lipgloss.Color("#58a6ff") // allow-edits — blue
	colModeAuto       = lipgloss.Color("#f0883e") // auto — orange

	colSkill   = lipgloss.Color("#b1baff") // skills — violet: the prompt's inline /token accent (skillToken) and its twin inside a sent block (skillAccent)
	colFileRef = lipgloss.Color("#cdffa4") // the prompt's inline @file token accent — blue, the tone a reference reads as

	colPromptToggle = lipgloss.Color("#b0d2ff") // the collapsed prompt's see-more / see-less marker — light gray-blue: an apogee affordance that reads as chrome inside the block, not as more of what the human wrote

	colGauge = lipgloss.Color("#c396ff") // context-fill gauge bar — periwinkle (llama-launcher look)

	colSelection = lipgloss.Color("#3a5fcd") // mouse drag-selection highlight background — blue

	colSpinner1 = lipgloss.Color("#8668ff")
	colSpinner2 = lipgloss.Color("#19a946")
	colSpinner3 = lipgloss.Color("#ffbf00")
	colSpinner4 = lipgloss.Color("#ff4a81")
)

// The marker glyphs. The assistant and tool headers lead with ✦; tool detail hangs off a
// tree branch (┝ for an interior line, ┕ for the last); the user prompt leads with ❯, and a
// menu-style popup row that is NOT the selected one leads with ·, the ❯'s quiet counterpart. A
// sub-agent (Depth > 0) block is framed by a vertical rail (│ per nesting level) and opened
// by a ⤷ sub-agent label (P3.14).
const (
	glyphAssistant       = "✦"
	glyphAssistantHollow = "✧" // the other half of the live star: a tool block still holding an open call alternates ✦/✧ on the spinner's tick (layout.md, "The live star")
	glyphBranch          = "┝"
	glyphBranchLast      = "┕"
	glyphUser            = "❯"
	glyphMenuUnselected  = "·" // U+00B7 MIDDLE DOT — an unselected row of a menu-style popup (popupSpec.menuRows): glyphUser's counterpart, deliberately NOT glyphBullet's "•", because a menu row is an option waiting to be pointed at rather than an item of a list
	glyphSubRail         = "│"
	glyphSubLabel        = "⤷"
	glyphBullet          = "•" // a markdown bullet-list item (- / * / +)
	glyphSkill           = "✦" // marks a skill: the "/" menu's skill rows (the sent block marks its own by colouring the token, not by badging it)
	glyphPresented       = "▤" // leads a presented document — deliberately NOT ✦: a deliverable is not a tool call
	glyphInterject       = "⧖" // leads an interjection — waiting as a staged row, then delivered as a transcript block (ADR 0025)
	glyphTableRule       = "─" // one cell of a markdown table's horizontal rule — under the header row and between adjacent body rows alike (mdtable.go)
	glyphTableColumn     = "│" // U+2502 LIGHT VERTICAL — the rule between two markdown table columns (mdtable.go); one cell wide in either width method, which is what lets tableDividerWidth be a constant (TestTableDividerHoldsOneColumn). Its shape is glyphSubRail's and glyphScrollTrack's but deliberately NOT shared with either: a column boundary, a sub-agent rail and a scroll-bar track are three elements that move independently.
	glyphTableCross      = "┼" // U+253C LIGHT VERTICAL AND HORIZONTAL — where a horizontal rule crosses a column divider (mdtable.go); one cell wide in either method, like the divider it crosses
)

// The transcript scroll bar's two glyphs (renderScrollbar). They are one axis drawn in two
// weights — the thumb is the heavy stroke, the track the light one — so the bar reads as a single
// centered line that only thickens where the view sits, rather than as a block sliding over a
// hairline. Both are one terminal cell wide in either width method, which is what lets the gutter
// stay one column (scrollbarWidth); TestPaintedScrollbarHoldsOneColumn pins that against a really-
// painted frame. The track's shape is glyphSubRail's but deliberately not shared with it: the
// sub-agent rail is a different element and the two move independently.
const (
	glyphScrollThumb = "┃" // U+2503 HEAVY VERTICAL — the thumb, the position marker
	glyphScrollTrack = "│" // U+2502 LIGHT VERTICAL — the groove behind it
)

// The autonomy-mode glyphs, one per rung of the ladder (modeSymbol). They lead the footer's mode
// marker in the mode's OWN colour — the glyph and the word are one styled run, not a coloured
// badge beside a label — so the rung reads at a glance from the shape before the word is read.
// Each is one terminal cell wide except ▸▸, which is deliberately two: auto is the one rung that
// acts without asking, and the doubled chevron is what "running ahead" looks like.
const (
	glyphModePlan       = "⊞"  // plan — an outline, nothing filled in yet
	glyphModeAskBefore  = "◐"  // ask before — the barred circle of a held action
	glyphModeAllowEdits = "✔"  // allow edits — edits pass
	glyphModeAuto       = "▸▸" // auto — fast-forward, no gate
)

// subAgentLabel is the one-line header that opens each contiguous run of sub-agent
// (Depth > 0) blocks, announcing the nested section (P3.14).
const subAgentLabel = "sub-agent"

// bodyIndent is the column every transcript block's body text starts in, as a blank prefix: a
// marker ("✦ " / "❯ " — the glyph plus its trailing space) is exactly this wide, and a wrapped
// line hangs under it (hangingPrefixes). The status line indents by it so the spinner and the
// activity phrase sit in the same column as the text above them (layout.md), rather than flush
// left against the marker column. TestStatusLineAlignsWithTranscriptText pins the two together
// against a really-rendered block, so a change to the marker or the hanging indent fails there.
const bodyIndent = "  "

// bodyRightGutter is bodyIndent's mirror on the right: the columns the transcript body leaves
// free at its right edge, so wrapped text breaks short of whatever sits beside it instead of
// running up against it. While the scroll bar is enabled (`ui.show-scrollbar`, default on) its
// column is reserved unconditionally (scrollbarWidth) and the bar paints inside it only while
// there is something to scroll, so the gutter is a constant rather than a function of whether the
// bar is currently drawn: a wrap width that changed when the bar appeared would re-wrap the whole
// visible transcript mid-run. That leaves one free column between the text and a painted bar, and
// two to the window edge while the gutter is blank. Turning the setting off removes the column and
// the bar together — the body takes the column and this gutter still holds it off the window edge
// — which cannot re-wrap mid-run either, because the setting is fixed for the process lifetime.
// TestTranscriptBodyLeavesRightGutter pins the shown state against a really-composed View and
// TestHiddenScrollbarYieldsTheColumn the hidden one.
const bodyRightGutter = 1

// theme bundles the reusable styles. They are intentionally spare — a few colour and weight
// cues — so the transcript stays legible under any terminal profile.
type theme struct {
	// measure is the TUI's display-width authority (width.go): the one answer to "how many
	// terminal columns does this occupy", following whatever measure the painter itself uses. It
	// rides on the theme because the theme is already handed to every free renderer function, so
	// the layout code reaches its measure exactly where it reaches its styles — and because a
	// widthAuthority is a plain value, the theme stays as copy-safe as it was (ADR 0011).
	measure widthAuthority

	userBlock    lipgloss.Style // white on dark-gray, full-width block (the last user prompt)
	promptToggle lipgloss.Style // the see-more / see-less marker a long prompt block carries near its right edge (renderUserBlock): bold light gray-blue on the block's OWN dark-gray field, held a promptMarkerMargin off the edge, so the toggle reads as an affordance sitting inside the block rather than as another row of what the human wrote
	toolHeader   lipgloss.Style // the ✦ Label target header
	toolLabel    lipgloss.Style // the tool label inside that header (bold, orange — the colCode tone inline code and the auto-mode marker already carry)
	toolDetail   lipgloss.Style // the ┝/┕ branch detail lines (dim)
	subRail      lipgloss.Style // the │ rail and ⤷ label framing a sub-agent (Depth > 0) block (the toolLabel orange — one tone for the whole sub-agent frame)
	skillAccent  lipgloss.Style // an invoked "/id" token INSIDE a sent user block (violet on the block's own dark-gray field): skillToken's transcript twin, and the whole of what now says a message invoked a skill
	skillToken   lipgloss.Style // a RESOLVING inline "/id" token in the prompt box (violet on the box's black)
	fileToken    lipgloss.Style // a RESOLVING inline "@path" token in the prompt box (blue on the box's black)
	selection    lipgloss.Style // the prompt's mouse drag-selection highlight (white on blue)
	diffAdded    lipgloss.Style // a "+" diff detail line (reserved)
	diffRemoved  lipgloss.Style // a "-" diff detail line (reserved)
	errorText    lipgloss.Style // a recovered-fault notice
	noteText     lipgloss.Style // a neutral note (cancelled, approval record) + a presentation's status line
	queuedText   lipgloss.Style // a staged-interjection strip row: faint on black, painted edge to edge so the strip reads as one band (its own role, deliberately not statusBar's)
	presentTitle lipgloss.Style // the ▤ marker and title of a presented document (bold white — a deliverable reads as a heading, not as plumbing; its path and URL stay unstyled so the terminal linkifies plain text)

	// Markdown styles for assistant chat text (markdown.go): **bold** weight, ## headings
	// as bold white, `inline code` and ``` fenced blocks ``` in orange, and the dim frame a
	// table draws around its columns.
	mdBold        lipgloss.Style // **bold** span
	mdHeading     lipgloss.Style // # … ###### heading line (bold white)
	mdCode        lipgloss.Style // `inline code` span (orange)
	mdCodeBlock   lipgloss.Style // a ``` fenced ``` code-block line (orange)
	mdRule        lipgloss.Style // a markdown table's whole frame: the ─ run under its header, the ─ runs between its body rows, and the │ divider between its columns (with the ┼ where the two meet) — faint, because the frame frames the columns and is not content
	inputBorder   lipgloss.Style // the rounded, dark-gray, black-bg input box — a closed frame, bottom edge included
	startupBorder lipgloss.Style // the one-time start-up card: the prompt box's rounded glyphs, no black fill (transparent, self-closing) — shares its shape with popupBorder
	popupBorder   lipgloss.Style // selector-popup chrome (renderPopup): startupBorder's rounded shape, filled solid black so the pane reads as a distinct overlay
	popupBody     lipgloss.Style // a popup's wrapped body block (renderPopup): normal white on black — between presentTitle (bold) and statusFaint (chrome) in the hierarchy
	popupAccent   lipgloss.Style // the SELECTED row of a MENU-style popup (popupSpec.menuRows): its ❯ and its label lit as one bold accent-orange run on the pane's black, with no highlight bar behind them — the cue th.userBlock's full-width bar is replaced by wherever a pane is a menu rather than a list
	statusFaint   lipgloss.Style // dim status text, bg-free (approval/ask prompts)
	statusBar     lipgloss.Style // status-line segments: faint on black
	spinnerBase   lipgloss.Style // the status-line spinner's field: the status bar's black, with no foreground of its own so an uncoloured glyph keeps the terminal's text colour — the colour loop layers a per-frame foreground onto it (spinner.go)
	statusError   lipgloss.Style // status-line "error" token: red bold on black
	chromeRule    lipgloss.Style // the prompt box's own border hairline (dark gray on black): the rule runes and corners inputElisionEdge composes the box's top border row from
	hairline      lipgloss.Style // both chrome hairlines — the ▔ above the status line and the ▁ under the footer — a dimmer rule (colDimGray) so they recede
	footerText    lipgloss.Style // the footer's content (faint on black)
	scrollThumb   lipgloss.Style // the transcript scroll-bar thumb (the position marker)
	scrollTrack   lipgloss.Style // the transcript scroll-bar track (the dim groove behind it)

	// Context-fill gauge (statusLine). The bar is a solid two-tone strip in the
	// llama-launcher style: gaugeFill paints the filled portion (full blocks + an eighth-block
	// partial cell), gaugeTrack the dark-gray groove behind the empty remainder.
	gaugeFill  lipgloss.Style // the gauge's filled portion (periwinkle)
	gaugeTrack lipgloss.Style // the gauge's empty track (dark-gray background)
}

// newTheme builds the styles from the palette. The input border is a CLOSED rounded frame: the
// box owns its own ╰─╯ bottom edge, and the footer below it is a frameless line rather than the
// shared bottom half of the box's chrome (layout.md). That is why nothing here has to compose
// junction corners by hand any more — one lipgloss.Border draws the whole box.
func newTheme() theme {
	return theme{
		// The painter's own starting measure. It moves only when the terminal tells the program
		// the painter moved (Update's tea.ModeReportMsg case).
		measure:   newWidthAuthority(),
		userBlock: lipgloss.NewStyle().Foreground(colWhite).Background(colDarkGray),
		// The collapse marker keeps the block's background and changes everything else: a light
		// gray-blue, bolded, against the dark-gray field the prompt text is white on. It is the one
		// run inside the block that is apogee talking rather than the human, and the hue says so by
		// cooling away from the prompt's white rather than by competing with it — a marker that
		// shouts is one you stop reading past.
		promptToggle: lipgloss.NewStyle().Bold(true).Foreground(colPromptToggle).Background(colDarkGray),
		toolHeader:   lipgloss.NewStyle(),
		toolLabel:    lipgloss.NewStyle().Bold(true).Foreground(colCode),
		toolDetail:   lipgloss.NewStyle().Foreground(colFaint),
		subRail:      lipgloss.NewStyle().Foreground(colCode),
		// The inline token accents are one act on two fields: the skill's violet moves to the
		// FOREGROUND and the background stays whatever the token is standing on — the prompt box's
		// black while the message is being typed, the user block's dark gray once it is sent — so an
		// accented token reads as one word of the sentence it stands in rather than as a badge pasted
		// over the field. Carrying the field is not cosmetic: a style with the wrong background cuts a
		// notch of the other colour through the block wherever a token lands.
		skillAccent:  lipgloss.NewStyle().Foreground(colSkill).Background(colDarkGray),
		skillToken:   lipgloss.NewStyle().Foreground(colSkill).Background(colBlack),
		fileToken:    lipgloss.NewStyle().Foreground(colFileRef).Background(colBlack),
		selection:    lipgloss.NewStyle().Foreground(colWhite).Background(colSelection),
		diffAdded:    lipgloss.NewStyle().Foreground(colDiffAdd),
		diffRemoved:  lipgloss.NewStyle().Foreground(colDiffDel),
		errorText:    lipgloss.NewStyle().Foreground(colError).Bold(true),
		noteText:     lipgloss.NewStyle().Foreground(colFaint),
		queuedText:   lipgloss.NewStyle().Foreground(colFaint).Background(colBlack),
		presentTitle: lipgloss.NewStyle().Bold(true).Foreground(colWhite),
		mdBold:       lipgloss.NewStyle().Bold(true),
		mdHeading:    lipgloss.NewStyle().Bold(true).Foreground(colWhite),
		mdCode:       lipgloss.NewStyle().Foreground(colCode),
		mdCodeBlock:  lipgloss.NewStyle().Foreground(colCode),
		mdRule:       lipgloss.NewStyle().Foreground(colFaint),
		inputBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()). // all four edges: ╭ ╮ ╰ ╯ — the box closes its own frame
			BorderForeground(colDarkGray).
			BorderBackground(colBlack).
			Background(colBlack).
			Padding(0, 1),
		startupBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()). // same glyphs as the prompt box: ╭ ╮ ╰ ╯ ─ │
			BorderForeground(colDarkGray).    // same border tone
			Padding(0, 1),                    // no Background / no BorderBackground → transparent, self-closing card
		popupBorder: lipgloss.NewStyle(). // selector-popup chrome — startupBorder's shape, but filled solid black (renderPopup)
							Border(lipgloss.RoundedBorder()).
							BorderForeground(colDarkGray).
							BorderBackground(colBlack).
							Background(colBlack).
							Padding(0, 1),
		popupBody: lipgloss.NewStyle().Foreground(colWhite).Background(colBlack), // wrapped body prose: normal white, not bold (title) nor faint (chrome)
		// A menu's selected row is marked by LIGHT rather than by a block of colour: bold in the
		// accent tone the theme already spends on "this is apogee's own" — colCode, the orange the
		// tool label, the sub-agent rail and the auto-mode marker all carry — against the faint gray
		// the other rows keep. The contrast between the two IS the cue, which is why the row needs no
		// bar behind it: a full-width highlight on a four-row decision menu paints a quarter of the
		// pane a second colour and reads as a banner, not as a pointer. The black is the pane's own
		// field, carried the way skillAccent carries the field it stands on, so the lit run sits IN
		// the pane instead of cutting a notch of another background through it.
		popupAccent: lipgloss.NewStyle().Bold(true).Foreground(colCode).Background(colBlack),
		statusFaint: lipgloss.NewStyle().Foreground(colFaint),
		statusBar:   lipgloss.NewStyle().Foreground(colFaint).Background(colBlack),
		spinnerBase: lipgloss.NewStyle().Background(colBlack), // match the status bar's black field
		statusError: lipgloss.NewStyle().Foreground(colError).Bold(true).Background(colBlack),
		chromeRule:  lipgloss.NewStyle().Foreground(colDarkGray).Background(colBlack),
		hairline:    lipgloss.NewStyle().Foreground(colDimGray).Background(colBlack),
		footerText:  lipgloss.NewStyle().Foreground(colFaint).Background(colBlack),
		scrollThumb: lipgloss.NewStyle().Foreground(colFaint),
		scrollTrack: lipgloss.NewStyle().Foreground(colDarkGray),
		gaugeFill:   lipgloss.NewStyle().Foreground(colGauge),
		gaugeTrack:  lipgloss.NewStyle().Background(colDarkGray),
	}
}
