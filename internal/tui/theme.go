package tui

import (
	"image/color"

	lipgloss "charm.land/lipgloss/v2"
	colorful "github.com/lucasb-eyer/go-colorful"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// ----------------------------------------------------------------------------
// The theme (P2.7 — TUI presentation pass)
// ----------------------------------------------------------------------------
//
// theme is the single place the look-and-feel lives: the palette, the marker glyphs, and the
// reusable lipgloss styles every renderer draws with — including spinnerBase, the field the
// status-line spinner paints on (its frames and their timing live with the animation, in
// spinner.go). It is built in newModel, stored as a Model value field, and rebuilt whole whenever the
// colour scheme changes under it (ADR 0040 — settingsApplyLocal). A lipgloss.Style
// holds no self-referential no-copy type (it is value-copy by design — its whole API returns new
// Styles), so a theme of Styles is safe inside the value-copied Model (ADR 0011;
// TestModelNoBuilderByValue guards the strings.Builder case structurally).

// The palette is no longer written here: every colour a style takes comes from the active
// [scheme.Scheme] handed to [newTheme], one hex value per semantic role (ADR 0040). Colours stay hex
// so lipgloss maps them to the terminal's profile, and the two "dark gray" roles of the shipped dark
// scheme (the user block's background and the chrome's borders) still share one tone — `chrome` —
// matching the layout sketch (layout.md).

// The marker glyphs. The assistant and tool headers lead with ✦; tool detail hangs off a
// tree branch (┝ for an interior line, ┕ for the last); the user prompt leads with ❯, and a
// menu-style popup row that is NOT the selected one leads with ·, the ❯'s quiet counterpart. A
// sub-agent (Depth > 0) block is framed by a vertical rail (│ per nesting level), opened by the
// delegation's own ┌─┶ header row and parted from the next row of its group by a lone ┊
// (docs/layout/tool-layout.md, "Grouped Sub-agents") — that frame is the whole of what announces a
// descent, at every depth and on the live path as well as the committed one.
const (
	glyphAssistant      = "✦" // the assistant and tool-header star. A tool block still holding an open call BLINKS it — half a second showing, half a second a bare cell that holds the column (layout.md, "The live star"; blockState.star)
	glyphBranch         = "┝"
	glyphBranchLast     = "┕"
	glyphLeaderDot      = "⋯" // U+22EF MIDLINE HORIZONTAL ELLIPSIS — one cell of the dotted leader a tool row runs from its target to its outcome slot (toolleader.go, leaderRowIn), painted in the `tool-leader` role. Deliberately one glyph per cell, so the run's length IS its cell count
	glyphUser           = "❯"
	glyphMenuUnselected = "·" // U+00B7 MIDDLE DOT — an unselected row of a menu-style popup (popupSpec.menuRows): glyphUser's counterpart, deliberately NOT glyphBullet's "•", because a menu row is an option waiting to be pointed at rather than an item of a list
	glyphSubRail        = "│"
	glyphMemberGutter   = "│" // U+2502 LIGHT VERTICAL — the gutter continuing an EXPANDED group member's rows under its ┝ (memberGutter, render.go). Its shape is glyphSubRail's and deliberately NOT shared with it: the member gutter is painted in the detail tone and the sub-agent rail in the label gold (design call 8), so an open member nested inside a run cannot be read as a frame of the run.
	glyphRailCorner     = "┌" // U+250C LIGHT DOWN AND RIGHT — the corner an EXPANDED delegation's header row opens its frame with, at the very left of the row (subAgentOpenMarker, docs/layout/tool-layout.md "Grouped Sub-agents"). It is the one cell of that marker painted in the rail's gold, because it IS the rail's top end (design call 2 of docs/plans/"2026-08-11 - 01")
	glyphRailTee        = "┶" // U+2536 LIGHT UP AND HORIZONTAL AND HEAVY LEFT — where the frame's arm meets the delegation's own branch on that header row. It stands in the row's detail tone with the ─ beside it, not in the rail's gold: it is this row's branch marker, the ┝/┕ of a member that happens to be open (design call 2)
	glyphRailClose      = "┊" // U+250A BOX DRAWINGS LIGHT QUADRUPLE DASH VERTICAL — the lone line closing an expanded delegation's span where the next row of its group follows it, in the rail's gold (railJoin; a group's last member and a lone run show none). Its dashes are the point: the frame stops rather than continuing, which is what a solid │ under the last row of a span would say
	glyphDone           = "✓" // U+2713 CHECK MARK — a FINISHED delegation's mark, after its name on the row and before the leaders, in the scheme's `success` green (theme.successMark, design call 6). A failed run wears no glyph at all: its red outcome slot is the whole of the failure marking (summaryStyle)
	glyphBullet         = "•" // a markdown bullet-list item (- / * / +)
	glyphSkill          = "✦" // marks a skill: the "/" menu's skill rows (the sent block marks its own by colouring the token, not by badging it)
	glyphPresented      = "▤" // leads a presented document — deliberately NOT ✦: a deliverable is not a tool call
	glyphInterject      = "⧖" // leads an interjection — waiting as a staged row, then delivered as a transcript block (ADR 0025)
	glyphTableRule      = "─" // one cell of a markdown table's horizontal rule — under the header row and between adjacent body rows alike (mdtable.go)
	glyphTableColumn    = "│" // U+2502 LIGHT VERTICAL — the rule between two markdown table columns (mdtable.go); one cell wide in either width method, which is what lets tableDividerWidth be a constant (TestTableDividerHoldsOneColumn). Its shape is glyphSubRail's and glyphScrollTrack's but deliberately NOT shared with either: a column boundary, a sub-agent rail and a scroll-bar track are three elements that move independently.
	glyphTableCross     = "┼" // U+253C LIGHT VERTICAL AND HORIZONTAL — where a horizontal rule crosses a column divider (mdtable.go); one cell wide in either method, like the divider it crosses
)

// The state indicator a tool block wears — at the right edge of a leader row, past the outcome
// slot, and after the label on the header of the targetless shape, which paints no leader row —
// and its one purpose: saying which of the two states the block is in, and, by being
// there at all, that the block is a click target (layout.md, "Collapsed and expanded blocks"). A
// block with nothing to reveal wears neither. The shape is the ▶/▼ pair of the owner's sketch
// (docs/layout/tool-layout.md), full-size rather than the small ▸/▾ these used to be: the indicator
// is the transcript's one affordance and it reads at a glance. glyphModeAuto's doubled "⏵⏵" is a
// different element in a different place and the two are deliberately not shared.
const (
	glyphCollapsed = "▶" // U+25B6 BLACK RIGHT-POINTING TRIANGLE — this block is collapsed: a click opens it
	glyphExpanded  = "▼" // U+25BC BLACK DOWN-POINTING TRIANGLE — this block is expanded: a click closes it
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
// Each is one terminal cell wide except ⏵⏵, which is deliberately two: auto is the one rung that
// acts without asking, and the doubled chevron is what "running ahead" looks like.
const (
	glyphModePlan       = "⊞"  // plan — an outline, nothing filled in yet
	glyphModeAskBefore  = "◐"  // ask before — the barred circle of a held action
	glyphModeAllowEdits = "✔"  // allow edits — edits pass
	glyphModeAuto       = "⏵⏵" // auto — fast-forward, no gate
)

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
// — which does re-wrap mid-run, but only where the human asked for it: /settings applies the key to
// the running session (ADR 0037) and lays the frame out again from there (settingsApplyLocal), while
// the config file is read at start-up. One deliberate re-wrap, never an incidental one.
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

	userBlock     lipgloss.Style // white on dark-gray, full-width block (the last user prompt)
	promptToggle  lipgloss.Style // the see-more / see-less marker a long prompt block carries near its right edge (renderUserBlock): bold light gray-blue on the block's OWN dark-gray field, held a promptMarkerMargin off the edge, so the toggle reads as an affordance sitting inside the block rather than as another row of what the human wrote
	toolHeader    lipgloss.Style // the ✦ Label target header
	toolLabel     lipgloss.Style // the tool label inside that header (bold, in the scheme's `tool-header` role — a tone of the header's own, no longer borrowed from the `code` role inline code and fenced blocks carry)
	toolIndicator lipgloss.Style // the ▶/▼ state indicator at the right edge of a toggleable block's leader row (or trailing the label where the targetless shape puts it on the header): the detail tone, deliberately NOT toolLabel's gold, so the affordance reads as chrome beside the text rather than as part of it
	toolDetail    lipgloss.Style // the ┝/┕ branch detail lines of a COLLAPSED block (dim)
	// toolDetailBright is toolDetail's open twin: the same lines once the block they belong to is
	// expanded, a step out of the collapsed dim (the scheme's `muted-bright` role against
	// toolDetail's `muted`), so what a reader opened stands out from the collapsed blocks around it.
	// A block is opened to be read, and the dim a scrollback of closed blocks wants — where the eye
	// is skimming past the blocks rather than into one — is a step too quiet for the output someone
	// just asked to see. ONE step, deliberately: the two tones have to read as the same voice at two
	// volumes, so a block does not change what it IS by being opened. That is why the pair is two
	// roles of one ramp rather than two free colours, and why the shipped schemes are guarded to keep
	// them apart in each direction their terminal calls "brighter" (design call 9 of
	// docs/plans/"2026-08-06 - 03"; TestBuiltinSchemesKeepBothMutedStepsDistinct).
	//
	// It is the PLAIN detail tone alone — a diff line keeps diffAdded / diffRemoved in both states,
	// since its colour carries meaning rather than emphasis — and the chrome a block wears
	// (toolIndicator, the remainder marker, the open member's gutter) stays dim in both, because the
	// affordances are not what the reader opened the block for (detailStyle). The outcome slot is
	// not on this ramp at all: it takes the marker role, which has a step of its own for the open
	// state (toolMarkerBright, summaryStyle).
	toolDetailBright lipgloss.Style
	toolLeader       lipgloss.Style // the ⋯ run carrying a tool row's eye from its target to the outcome slot at the row's edge (its own `tool-leader` role, seeded from `muted`): chrome like the ▶ it runs up to, so the dots never read as content — and its own role so a scheme can damp them without moving the indicator with them
	toolMarker       lipgloss.Style // the synthesized "+N more lines" remainder marker beneath a hidden body, and the outcome slot at a tool row's right edge (summaryStyle): its own `tool-marker` role (a warm orange under `dark`, a cooler blue under `light`), no background and no bold weight, so a marker is never mistakable for a body line that happens to open with "+" — the prompt block's see-more keeps the heavier promptToggle treatment
	// toolMarkerBright is toolMarker's open step, the same one-ramp pair toolDetail /
	// toolDetailBright models one level down (the scheme's `tool-marker-bright` role): the outcome
	// slot of an EXPANDED block, a step out of the collapsed slots around it. It is the marker role
	// and not the detail gray that paints the slot because the slot is apogee's reading of what the
	// call came to — "12 lines", "exit 0 · 1.2s" — rather than a line of the output itself, so it
	// speaks in the same voice as the marker that counts the body away (design call 2 of
	// docs/plans/"2026-08-11 - 00"). A stated role rather than a tone computed at render time, since
	// every colour on screen is a role a scheme can name (ADR 0040).
	toolMarkerBright lipgloss.Style
	subRail          lipgloss.Style // the sub-agent frame: the │ rail down a Depth > 0 block, the ┌ opening an expanded delegation's header row, and the ┊ closing its span (toolLabel's `tool-header` role — one tone for the whole frame)
	// successMark is the ✓ a FINISHED delegation wears after its name (the scheme's `success` role,
	// green): errorText's counterpart, and read as one — the two are what the transcript says about
	// how something came out. It is a foreground alone, with no bold weight, because the mark is a
	// small punctuation of a row the reader is skimming rather than a verdict shouted at them; the
	// red that DOES shout belongs to a failure, which wears no glyph of its own (design call 6).
	successMark  lipgloss.Style
	skillAccent  lipgloss.Style // an invoked "/id" token INSIDE a sent user block (violet on the block's own dark-gray field): skillToken's transcript twin, and the whole of what now says a message invoked a skill
	skillToken   lipgloss.Style // a RESOLVING inline "/id" token in the prompt box (violet on the box's black)
	fileToken    lipgloss.Style // a RESOLVING inline "@path" token in the prompt box (the scheme's `file-ref` role on the box's black — a pale green under `dark`, an olive one under `light`)
	selection    lipgloss.Style // the prompt's mouse drag-selection highlight (white on blue)
	diffAdded    lipgloss.Style // a "+" diff detail line (reserved)
	diffRemoved  lipgloss.Style // a "-" diff detail line (reserved)
	errorText    lipgloss.Style // a recovered-fault notice
	noteText     lipgloss.Style // a neutral note (cancelled, approval record) + a presentation's status line
	queuedText   lipgloss.Style // a staged-interjection strip row: faint on black, painted edge to edge so the strip reads as one band (its own role, deliberately not statusBar's)
	presentTitle lipgloss.Style // the ▤ marker and title of a presented document (bold white — a deliverable reads as a heading, not as plumbing; its path and URL stay unstyled so the terminal linkifies plain text)

	// Markdown styles for assistant chat text (markdown.go): **bold** weight, <u>underline</u>,
	// ## headings as bold white, `inline code` and ``` fenced blocks ``` in the scheme's `code` role,
	// and the dim frame a table draws around its columns.
	mdBold        lipgloss.Style // **bold** span
	mdUnderline   lipgloss.Style // <u>underline</u> span — SGR 4 and nothing else, so the underlined run keeps whatever colour it stands in
	mdHeading     lipgloss.Style // # … ###### heading line (bold white)
	mdCode        lipgloss.Style // `inline code` span (the `code` role)
	mdCodeBlock   lipgloss.Style // a ``` fenced ``` code-block line (the `code` role)
	mdRule        lipgloss.Style // a markdown table's whole frame: the ─ run under its header, the ─ runs between its body rows, and the │ divider between its columns (with the ┼ where the two meet) — faint, because the frame frames the columns and is not content
	inputBorder   lipgloss.Style // the rounded, dark-gray, black-bg input box — a closed frame, bottom edge included
	startupBorder lipgloss.Style // the one-time start-up card: the prompt box's rounded glyphs, no black fill (transparent, self-closing) — shares its shape with popupBorder
	popupBorder   lipgloss.Style // selector-popup chrome (renderPopup): startupBorder's rounded shape, filled solid black so the pane reads as a distinct overlay
	popupBody     lipgloss.Style // a popup's wrapped body block (renderPopup): normal white on black — between presentTitle (bold) and statusFaint (chrome) in the hierarchy
	popupBodyLead lipgloss.Style // the LABEL a popup's body block may open with (popupSpec.bodyLead — the /settings header's "Description:"): popupBody bolded, so the label reads as the heading of the sentence it leads rather than as a second colour inside it
	popupHeading  lipgloss.Style // a SECTION header row inside a popup's row list (popupRowHeading — /settings' UPSTREAM, AUTONOMY, …): white on the pane's black, one weight above the faint rows it opens, so the divisions of a long list are found without being read (docs/layout/settings-screen-layout.md)
	popupEdit     lipgloss.Style // the row a pane is EDITING (popupRowEditing): the selection's own full-width bar with its text lit in the accent tone instead of white — the row is selected either way, and what the colour says is that the next keypress goes INTO it
	popupAccent   lipgloss.Style // the SELECTED row of a MENU-style popup (popupSpec.menuRows): its ❯ and its label lit as one bold run of the `code` role on the pane's black, with no highlight bar behind them — the cue th.userBlock's full-width bar is replaced by wherever a pane is a menu rather than a list
	statusFaint   lipgloss.Style // dim status text, bg-free (approval/ask prompts)
	statusBar     lipgloss.Style // status-line segments: faint on black
	spinnerBase   lipgloss.Style // the status-line spinner's field: the status bar's black, with no foreground of its own so an uncoloured glyph keeps the terminal's text colour — the colour loop layers a per-frame foreground onto it (spinner.go)
	statusError   lipgloss.Style // status-line "error" token: red bold on black
	statusWarning lipgloss.Style // status-line qualifier (the `warning` role on the bar's black): amber, and deliberately NOT bold — it tints a fact onto a running phrase rather than announcing a fault the way statusError does
	chromeRule    lipgloss.Style // the prompt box's own border hairline (dark gray on black): the rule runes and corners inputElisionEdge composes the box's top border row from
	hairline      lipgloss.Style // both chrome hairlines — the ▔ above the status line and the ▁ under the footer — a dimmer rule (the `divider` role) so they recede
	footerText    lipgloss.Style // the footer's content (faint on black)
	scrollThumb   lipgloss.Style // the transcript scroll-bar thumb (the position marker)
	scrollTrack   lipgloss.Style // the transcript scroll-bar track (the dim groove behind it)

	// Context-fill gauge (statusLine). The bar is a solid two-tone strip in the
	// llama-launcher style: gaugeFill paints the filled portion (full blocks + an eighth-block
	// partial cell), gaugeTrack the dark-gray groove behind the empty remainder.
	gaugeFill  lipgloss.Style // the gauge's filled portion (periwinkle)
	gaugeTrack lipgloss.Style // the gauge's empty track (dark-gray background)

	// spinnerStops are the status-line colour loop's waypoints — the scheme's four `spinner-*`
	// roles, converted into the space the blend works in (buildSpinnerStops, spinner.go). They ride
	// on the theme rather than in a package var because a package var is built once at init and a
	// scheme switch happens at runtime: the loop has to follow the switch like every other colour.
	// The slice is built inside newTheme and never mutated afterwards, so the copies the value-copied
	// Model makes of the theme all share one backing array and none of them can disagree about it —
	// the same sharing discipline theme.measure carries (ADR 0030), and no no-copy type in sight
	// (ADR 0011).
	spinnerStops []colorful.Color

	// The raw colours, for the handful of places that paint with a COLOUR rather than with a whole
	// style: a widget the theme does not own (the textarea's four background slots, fillInput), a
	// field laid under someone else's style (popup.go's blackFill, the gauge's partial cell), and a
	// tone chosen per value rather than per role (the footer's mode marker, [theme.modeColor]).
	// Before ADR 0040 those sites read the package-level palette vars directly, which is exactly what
	// a runtime scheme switch cannot reach — so the theme carries them and the palette is gone. They
	// are plain lipgloss.Color values (a string type), so the theme stays copy-safe (ADR 0011).
	surface color.Color // the `surface` role: the input box's interior, the status field, a popup pane's fill
	chrome  color.Color // the `chrome` role: the user block's field, the borders, the gauge's track
	muted   color.Color // the `muted` role: the dim tone, and modeColor's off-ladder fallback
	errorFg color.Color // the `error` role: the footer's offline segment, painted onto footerText

	// The four autonomy-mode marker colours, warming up the privilege ladder (least → most
	// autonomous): plan turquoise-green, ask-before green, allow-edits blue, auto orange.
	// [theme.modeColor] is the only reader.
	modePlan       color.Color
	modeAskBefore  color.Color
	modeAllowEdits color.Color
	modeAuto       color.Color
}

// modeColor maps an autonomy mode to its footer-marker colour. An unknown mode falls back to the
// muted tone, so an off-ladder value is never invisible.
func (th theme) modeColor(m domain.Mode) color.Color {
	switch m {
	case domain.ModePlan:
		return th.modePlan
	case domain.ModeAskBefore:
		return th.modeAskBefore
	case domain.ModeAllowEdits:
		return th.modeAllowEdits
	case domain.ModeAuto:
		return th.modeAuto
	default:
		return th.muted
	}
}

// newTheme builds the styles from a colour scheme — the 31 semantic roles of ADR 0040, resolved
// before it is called (the renderer never reads a scheme file itself). It is the ONE seam between a
// scheme and the look: calling it again with another scheme rebuilds every style, which is what a
// live scheme switch does (settingsApplyLocal).
//
// The input border is a CLOSED rounded frame: the box owns its own ╰─╯ bottom edge, and the footer
// below it is a frameless line rather than the shared bottom half of the box's chrome (layout.md).
// That is why nothing here has to compose junction corners by hand any more — one lipgloss.Border
// draws the whole box.
func newTheme(s scheme.Scheme) theme {
	// The scheme's roles, bound once and read by everything below. A role is named here by what it
	// MEANS, not by the tone the dark scheme happens to give it, so the styles keep reading as
	// arguments about hierarchy under a scheme that inverts every value.
	var (
		userText       = lipgloss.Color(s.UserText)
		chrome         = lipgloss.Color(s.Chrome)
		divider        = lipgloss.Color(s.Divider)
		surface        = lipgloss.Color(s.Surface)
		muted          = lipgloss.Color(s.Muted)
		openDetail     = lipgloss.Color(s.MutedBright)
		diffAdd        = lipgloss.Color(s.DiffAdd)
		diffDel        = lipgloss.Color(s.DiffDel)
		errFg          = lipgloss.Color(s.Error)
		successFg      = lipgloss.Color(s.Success)
		warningFg      = lipgloss.Color(s.Warning)
		code           = lipgloss.Color(s.Code)
		toolHeaderFg   = lipgloss.Color(s.ToolHeader)
		modePlan       = lipgloss.Color(s.ModePlan)
		modeAskBefore  = lipgloss.Color(s.ModeAskBefore)
		modeAllowEdits = lipgloss.Color(s.ModeAllowEdits)
		modeAuto       = lipgloss.Color(s.ModeAuto)
		skill          = lipgloss.Color(s.Skill)
		fileRef        = lipgloss.Color(s.FileRef)
		promptToggleFg = lipgloss.Color(s.PromptToggle)
		toolMarkerFg   = lipgloss.Color(s.ToolMarker)
		openMarker     = lipgloss.Color(s.ToolMarkerBright)
		toolLeaderFg   = lipgloss.Color(s.ToolLeader)
		gauge          = lipgloss.Color(s.Gauge)
		selectionFill  = lipgloss.Color(s.Selection)
	)

	return theme{
		// The painter's own starting measure. It moves only when the terminal tells the program
		// the painter moved (Update's tea.ModeReportMsg case).
		measure:   newWidthAuthority(),
		userBlock: lipgloss.NewStyle().Foreground(userText).Background(chrome),
		// The collapse marker keeps the block's background and changes everything else: a light
		// gray-blue, bolded, against the dark-gray field the prompt text is white on. It is the one
		// run inside the block that is apogee talking rather than the human, and the hue says so by
		// cooling away from the prompt's white rather than by competing with it — a marker that
		// shouts is one you stop reading past.
		promptToggle:     lipgloss.NewStyle().Bold(true).Foreground(promptToggleFg).Background(chrome),
		toolHeader:       lipgloss.NewStyle(),
		toolLabel:        lipgloss.NewStyle().Bold(true).Foreground(toolHeaderFg),
		toolIndicator:    lipgloss.NewStyle().Foreground(muted),
		toolDetail:       lipgloss.NewStyle().Foreground(muted),
		toolDetailBright: lipgloss.NewStyle().Foreground(openDetail),
		toolLeader:       lipgloss.NewStyle().Foreground(toolLeaderFg),
		toolMarker:       lipgloss.NewStyle().Foreground(toolMarkerFg),
		toolMarkerBright: lipgloss.NewStyle().Foreground(openMarker),
		subRail:          lipgloss.NewStyle().Foreground(toolHeaderFg),
		successMark:      lipgloss.NewStyle().Foreground(successFg),
		// The inline token accents are one act on two fields: the skill's violet moves to the
		// FOREGROUND and the background stays whatever the token is standing on — the prompt box's
		// black while the message is being typed, the user block's dark gray once it is sent — so an
		// accented token reads as one word of the sentence it stands in rather than as a badge pasted
		// over the field. Carrying the field is not cosmetic: a style with the wrong background cuts a
		// notch of the other colour through the block wherever a token lands.
		skillAccent:  lipgloss.NewStyle().Foreground(skill).Background(chrome),
		skillToken:   lipgloss.NewStyle().Foreground(skill).Background(surface),
		fileToken:    lipgloss.NewStyle().Foreground(fileRef).Background(surface),
		selection:    lipgloss.NewStyle().Foreground(userText).Background(selectionFill),
		diffAdded:    lipgloss.NewStyle().Foreground(diffAdd),
		diffRemoved:  lipgloss.NewStyle().Foreground(diffDel),
		errorText:    lipgloss.NewStyle().Foreground(errFg).Bold(true),
		noteText:     lipgloss.NewStyle().Foreground(muted),
		queuedText:   lipgloss.NewStyle().Foreground(muted).Background(surface),
		presentTitle: lipgloss.NewStyle().Bold(true).Foreground(userText),
		mdBold:       lipgloss.NewStyle().Bold(true),
		mdUnderline:  lipgloss.NewStyle().Underline(true),
		mdHeading:    lipgloss.NewStyle().Bold(true).Foreground(userText),
		mdCode:       lipgloss.NewStyle().Foreground(code),
		mdCodeBlock:  lipgloss.NewStyle().Foreground(code),
		mdRule:       lipgloss.NewStyle().Foreground(muted),
		inputBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()). // all four edges: ╭ ╮ ╰ ╯ — the box closes its own frame
			BorderForeground(chrome).
			BorderBackground(surface).
			Background(surface).
			Padding(0, 1),
		startupBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()). // same glyphs as the prompt box: ╭ ╮ ╰ ╯ ─ │
			BorderForeground(chrome).         // same border tone
			Padding(0, 1),                    // no Background / no BorderBackground → transparent, self-closing card
		popupBorder: lipgloss.NewStyle(). // selector-popup chrome — startupBorder's shape, but filled solid black (renderPopup)
							Border(lipgloss.RoundedBorder()).
							BorderForeground(chrome).
							BorderBackground(surface).
							Background(surface).
							Padding(0, 1),
		popupBody: lipgloss.NewStyle().Foreground(userText).Background(surface), // wrapped body prose: normal white, not bold (title) nor faint (chrome)
		// The body's label and the row list's section headers are the same argument in two places:
		// white where the prose around them is white and the chrome below them is faint, so a heading
		// is told from what it heads by WEIGHT and by tone rather than by a rule or a badge. The label
		// is bolded because it stands inside a line it shares with the text it introduces; a section
		// header has a line of its own and needs no more than the tone to be found.
		popupBodyLead: lipgloss.NewStyle().Bold(true).Foreground(userText).Background(surface),
		popupHeading:  lipgloss.NewStyle().Foreground(userText).Background(surface),
		// An edited row keeps the selection's dark-gray field — it IS the selected row — and changes
		// what is written on it: the accent tone the theme spends on "this is apogee's own", bolded
		// against the white the same bar carries when the row is merely highlighted. Light rather than
		// a second block of colour, popupAccent's argument on a field that is already there.
		popupEdit: lipgloss.NewStyle().Bold(true).Foreground(code).Background(chrome),
		// A menu's selected row is marked by LIGHT rather than by a block of colour: bold in the
		// accent tone the theme already spends on "this is apogee's own" — the `code` role, the tone
		// inline code and fenced code blocks carry (the tool label and the sub-agent rail have their own
		// `tool-header` role now, and the auto-mode marker its `mode-auto` one) — against the faint gray
		// the other rows keep. The contrast between the two IS the cue, which is why the row needs no
		// bar behind it: a full-width highlight on a four-row decision menu paints a quarter of the
		// pane a second colour and reads as a banner, not as a pointer. The black is the pane's own
		// field, carried the way skillAccent carries the field it stands on, so the lit run sits IN
		// the pane instead of cutting a notch of another background through it.
		popupAccent: lipgloss.NewStyle().Bold(true).Foreground(code).Background(surface),
		statusFaint: lipgloss.NewStyle().Foreground(muted),
		statusBar:   lipgloss.NewStyle().Foreground(muted).Background(surface),
		spinnerBase: lipgloss.NewStyle().Background(surface), // match the status bar's black field
		statusError: lipgloss.NewStyle().Foreground(errFg).Bold(true).Background(surface),
		// The bar's second voice, one rung below the error token: the `warning` role on the same
		// black field, and no bold. Weight is what statusError spends to make a fault unmissable;
		// a qualifier hung off a phrase that is still running has to be readable as a tint on that
		// phrase, so it takes the colour and leaves the weight alone.
		statusWarning: lipgloss.NewStyle().Foreground(warningFg).Background(surface),
		chromeRule:    lipgloss.NewStyle().Foreground(chrome).Background(surface),
		hairline:      lipgloss.NewStyle().Foreground(divider).Background(surface),
		footerText:    lipgloss.NewStyle().Foreground(muted).Background(surface),
		scrollThumb:   lipgloss.NewStyle().Foreground(muted),
		scrollTrack:   lipgloss.NewStyle().Foreground(chrome),
		gaugeFill:     lipgloss.NewStyle().Foreground(gauge),
		gaugeTrack:    lipgloss.NewStyle().Background(chrome),

		// The colour loop's waypoints, in the order it visits them: violet → green → amber → pink
		// and back to violet under the dark scheme. Converting them here is what makes the loop a
		// property of the ACTIVE scheme instead of of the binary.
		spinnerStops: buildSpinnerStops(
			lipgloss.Color(s.Spinner1),
			lipgloss.Color(s.Spinner2),
			lipgloss.Color(s.Spinner3),
			lipgloss.Color(s.Spinner4),
		),

		// The raw colours the call sites that paint without a style reach for.
		surface:        surface,
		chrome:         chrome,
		muted:          muted,
		errorFg:        errFg,
		modePlan:       modePlan,
		modeAskBefore:  modeAskBefore,
		modeAllowEdits: modeAllowEdits,
		modeAuto:       modeAuto,
	}
}
