package tui

import (
	"time"

	"charm.land/bubbles/v2/spinner"
	lipgloss "charm.land/lipgloss/v2"
)

// ----------------------------------------------------------------------------
// The theme (P2.7 — TUI presentation pass)
// ----------------------------------------------------------------------------
//
// theme is the single place the look-and-feel lives: the palette, the marker glyphs, the
// spinner frames, and the reusable lipgloss styles every renderer draws with. It is built
// once in newModel and stored as a Model value field. A lipgloss.Style holds no
// self-referential no-copy type (it is value-copy by design — its whole API returns new
// Styles), so a theme of Styles is safe inside the value-copied Model (ADR 0011;
// TestModelNoBuilderByValue guards the strings.Builder case structurally).

// The palette. Colours are hex so lipgloss maps them to the terminal's profile; the two
// "dark gray" roles (the user block's background and the chrome's borders) share one tone,
// matching the layout sketch (layout.md).
var (
	colWhite    = lipgloss.Color("#ffffff") // user-prompt text
	colDarkGray = lipgloss.Color("#4a4a4a") // user-block background + input/footer borders
	colBlack    = lipgloss.Color("#000000") // input-box interior
	colFaint    = lipgloss.Color("#8a8a8a") // status/footer/tool-detail dim
	colDiffAdd  = lipgloss.Color("#3fb950") // diff "+" lines (reserved — no producer yet)
	colDiffDel  = lipgloss.Color("#f85149") // diff "-" lines (reserved — no producer yet)
	colError    = lipgloss.Color("#f85149") // recovered-fault notices
	colCode     = lipgloss.Color("#f0883e") // inline `code` + fenced code blocks (orange)

	// The autonomy-mode footer markers, warming up the privilege ladder (least → most
	// autonomous): plan turquoise-green, ask-before green, allow-edits blue, auto orange.
	colModePlan       = lipgloss.Color("#2ee6c5") // plan — turquoise green
	colModeAskBefore  = lipgloss.Color("#3fb950") // ask-before — green
	colModeAllowEdits = lipgloss.Color("#58a6ff") // allow-edits — blue
	colModeAuto       = lipgloss.Color("#f0883e") // auto — orange

	colSkill = lipgloss.Color("#8957e5") // attached-skill chips — violet

	colGauge = lipgloss.Color("#7c7cf0") // context-fill gauge bar — periwinkle (llama-launcher look)

	colSelection = lipgloss.Color("#3a5fcd") // mouse drag-selection highlight background — blue
)

// The marker glyphs. The assistant and tool headers lead with ✦; tool detail hangs off a
// tree branch (┝ for an interior line, ┕ for the last); the user prompt leads with ❯. A
// sub-agent (Depth > 0) block is framed by a vertical rail (│ per nesting level) and opened
// by a ⤷ sub-agent label (P3.14).
const (
	glyphAssistant  = "✦"
	glyphBranch     = "┝"
	glyphBranchLast = "┕"
	glyphUser       = "❯"
	glyphSubRail    = "│"
	glyphSubLabel   = "⤷"
	glyphBullet     = "•" // a markdown bullet-list item (- / * / +)
	glyphSkill      = "✦" // leads an attached-skill chip (matches the assistant marker)
	glyphPresented  = "▤" // leads a presented document — deliberately NOT ✦: a deliverable is not a tool call
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
// running up against it. The scroll-bar column is reserved unconditionally (scrollbarWidth) and
// the bar paints inside it only while there is something to scroll, so the gutter is a constant
// rather than a function of whether the bar is currently drawn: a wrap width that changed when
// the bar appeared would re-wrap the whole visible transcript mid-run. That leaves two free
// columns between the text and a painted bar, and three to the window edge while the gutter is
// blank. TestTranscriptBodyLeavesRightGutter pins it against a really-composed View.
const bodyRightGutter = 2

// brailleFrames are the status-line spinner frames (a single braille cell that appears to
// rotate), shown while a worker drives the Exchange.
var brailleFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// newBrailleSpinner builds the status-line spinner from brailleFrames.
func newBrailleSpinner() spinner.Model {
	return spinner.New(spinner.WithSpinner(spinner.Spinner{
		Frames: brailleFrames,
		FPS:    time.Second / 10, //nolint:mnd // 10 fps, matching the bundled spinners
	}))
}

// theme bundles the reusable styles. They are intentionally spare — a few colour and weight
// cues — so the transcript stays legible under any terminal profile.
type theme struct {
	userBlock    lipgloss.Style // white on dark-gray, full-width block (the last user prompt)
	toolHeader   lipgloss.Style // the ✦ Label target header
	toolLabel    lipgloss.Style // the tool label inside that header (bold, orange — the colCode tone inline code and the auto-mode marker already carry)
	toolDetail   lipgloss.Style // the ┝/┕ branch detail lines (dim)
	subRail      lipgloss.Style // the │ rail framing a sub-agent (Depth > 0) block (dim)
	skillChip    lipgloss.Style // an attached-skill chip above the input (white on violet)
	selection    lipgloss.Style // the prompt's mouse drag-selection highlight (white on blue)
	diffAdded    lipgloss.Style // a "+" diff detail line (reserved)
	diffRemoved  lipgloss.Style // a "-" diff detail line (reserved)
	errorText    lipgloss.Style // a recovered-fault notice
	noteText     lipgloss.Style // a neutral note (cancelled, approval record) + a presentation's status line
	presentTitle lipgloss.Style // the ▤ marker and title of a presented document (bold white — a deliverable reads as a heading, not as plumbing; its path and URL stay unstyled so the terminal linkifies plain text)

	// Markdown styles for assistant chat text (markdown.go): **bold** weight, ## headings
	// as bold white, `inline code` and ``` fenced blocks ``` in orange.
	mdBold      lipgloss.Style // **bold** span
	mdHeading   lipgloss.Style // # … ###### heading line (bold white)
	mdCode      lipgloss.Style // `inline code` span (orange)
	mdCodeBlock lipgloss.Style // a ``` fenced ``` code-block line (orange)
	inputBorder lipgloss.Style // the rounded, dark-gray, black-bg input box (no bottom edge)
	statusFaint lipgloss.Style // dim status text, bg-free (approval/ask prompts)
	statusBar   lipgloss.Style // status-line segments: faint on black
	statusError lipgloss.Style // status-line "error" token: red bold on black
	chromeRule  lipgloss.Style // the bottom-chrome hairlines (dark gray on black): the input box's top-edge row and the footer's border runes, corners, and │ bars
	footerText  lipgloss.Style // the footer's content (faint on black)
	scrollThumb lipgloss.Style // the transcript scroll-bar thumb (the position marker)
	scrollTrack lipgloss.Style // the transcript scroll-bar track (the dim groove behind it)

	// Context-fill gauge (statusLine). The bar is a solid two-tone strip in the
	// llama-launcher style: gaugeFill paints the filled portion (full blocks + an eighth-block
	// partial cell), gaugeTrack the dark-gray groove behind the empty remainder.
	gaugeFill  lipgloss.Style // the gauge's filled portion (periwinkle)
	gaugeTrack lipgloss.Style // the gauge's empty track (dark-gray background)
}

// newTheme builds the styles from the palette. The input border drops its bottom edge: the
// footer's top rule is the shared divider, so the input box and footer read as one connected
// unit (layout.md), and a single lipgloss.Border cannot produce the ├/┤ junction corners where
// that divider meets the box — those rules are composed by hand in footerView.
func newTheme() theme {
	return theme{
		userBlock:    lipgloss.NewStyle().Foreground(colWhite).Background(colDarkGray),
		toolHeader:   lipgloss.NewStyle(),
		toolLabel:    lipgloss.NewStyle().Bold(true).Foreground(colCode),
		toolDetail:   lipgloss.NewStyle().Foreground(colFaint),
		subRail:      lipgloss.NewStyle().Foreground(colFaint),
		skillChip:    lipgloss.NewStyle().Foreground(colWhite).Background(colSkill),
		selection:    lipgloss.NewStyle().Foreground(colWhite).Background(colSelection),
		diffAdded:    lipgloss.NewStyle().Foreground(colDiffAdd),
		diffRemoved:  lipgloss.NewStyle().Foreground(colDiffDel),
		errorText:    lipgloss.NewStyle().Foreground(colError).Bold(true),
		noteText:     lipgloss.NewStyle().Foreground(colFaint),
		presentTitle: lipgloss.NewStyle().Bold(true).Foreground(colWhite),
		mdBold:       lipgloss.NewStyle().Bold(true),
		mdHeading:    lipgloss.NewStyle().Bold(true).Foreground(colWhite),
		mdCode:       lipgloss.NewStyle().Foreground(colCode),
		mdCodeBlock:  lipgloss.NewStyle().Foreground(colCode),
		inputBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderBottom(false).
			BorderForeground(colDarkGray).
			BorderBackground(colBlack).
			Background(colBlack).
			Padding(0, 1),
		statusFaint: lipgloss.NewStyle().Foreground(colFaint),
		statusBar:   lipgloss.NewStyle().Foreground(colFaint).Background(colBlack),
		statusError: lipgloss.NewStyle().Foreground(colError).Bold(true).Background(colBlack),
		chromeRule:  lipgloss.NewStyle().Foreground(colDarkGray).Background(colBlack),
		footerText:  lipgloss.NewStyle().Foreground(colFaint).Background(colBlack),
		scrollThumb: lipgloss.NewStyle().Foreground(colFaint),
		scrollTrack: lipgloss.NewStyle().Foreground(colDarkGray),
		gaugeFill:   lipgloss.NewStyle().Foreground(colGauge),
		gaugeTrack:  lipgloss.NewStyle().Background(colDarkGray),
	}
}
