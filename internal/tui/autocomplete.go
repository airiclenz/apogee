package tui

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// Chat input mini-language — the autocomplete overlay
// ----------------------------------------------------------------------------
//
// A suggestion popup that opens while the human types: a "/" token lists the commands AND the
// skills in ONE merged menu, and an "@" token lists workspace files. Every region opens wherever
// the box is editable — the namespace is most wanted exactly where it used to vanish, on the
// message being composed while the model works, so an interjection reaches a file, a skill and a
// reporting command as easily as a submitted message does (see computeAutocomplete; a command that
// needs a quiescent engine is offered TAGGED rather than hidden, and refused with a note if it is
// accepted anyway). It is painted by the shared selector-popup
// module (popup.go) — a titled, bordered pane rendered above the input box, in a slot that
// shrinks the transcript viewport to make room. The overlay completes the TOKEN AT THE CARET
// (caretToken): forward typing at the end of the draft is only its commonest case, and going back
// to fix a misspelled skill id mid-message offers exactly the same menu the end does.
//
// Accepting a row is not always a completion. A skill or a file row splices its token and leaves
// the human typing; a COMMAND row runs the command there and then (acceptAutocomplete), cutting its
// "/verb" out of the draft and leaving everything else in the box — which is what lets a command be
// invoked from the middle of a half-written message without destroying it. The verbs that need what
// follows them — the takesArgs rows of commandSpecs (command.go), today /color-scheme, /confine,
// /model, /rename, /schedule and /server — complete instead.

// maxAutocompleteItems caps how many suggestions the overlay OFFERS (and how far the file walk
// runs) — enough to be useful, small enough that a large workspace walk stays cheap. Type more to
// narrow further. It is a cap on the menu's length, not a promise about the frame: how many of the
// offered rows a given window can actually seat is popupBudget's call, which this cap is spent
// inside of (renderAutocomplete). Reading it as the frame's guard is what let the "/" menu — whose
// rows come from commandSpecs and so outnumber it — push the input box off a short terminal.
const maxAutocompleteItems = 8

// acKind tags what an open overlay is completing.
type acKind int

const (
	acCommand acKind = iota // a "/command" word
	acFile                  // an "@file" reference
)

// acItem is one suggestion: value is the text spliced in (the command name, the skill id or the
// file path, without the "/"/"@" sigil), cells are the row's COLUMNS in its menu's fixed schema
// (popupRow — the popup module pads them into vertically aligned columns, so no producer here
// concatenates its own spacing), and skill marks a row of the merged "/" menu that names a SKILL
// rather than a command. The mark is not decoration: the two kinds of row do different things at
// accept (a skill writes its token, a command RUNS), so the row has to carry which it is.
//
// Only the cells are display: prefix matching and accept-on-enter read value (and the parsed verb)
// exclusively, so re-columning a menu can never change what a row DOES.
//
// rank is the row's MATCH QUALITY against the partial being typed (slashMatchRank): the "/" menu
// sorts by it, so a row the partial prefixes outranks one it merely appears inside. It is a
// property of the pair (partial, row), not of the row alone, which is why it is computed where the
// row is built and not stored on the skill or the commandSpec behind it.
//
// source is the skill half's other row fact: WHICH source dir the skill was loaded from
// (skillSource — "workspace", "library"), empty on every command and file row. It rides here rather
// than being re-derived at render time because the skill it describes is in hand where the row is
// built (skillSuggestions) and gone by the time the row is composed (slashSuggestions).
type acItem struct {
	value  string
	cells  popupRow
	skill  bool
	rank   int
	source string
}

// slashMatchRank scores how well name answers partial, lowest-is-best: 0 exact, 1 prefix,
// 2 substring elsewhere, 3 no match at all. It is the whole of the "/" menu's ordering rule —
// the rows are sorted by it STABLY, so quality decides between tiers and the registries' own scan
// order (commandSpecs' table order, then the catalog's DisplayName order) decides inside one.
//
// Both sides are lowercased because the skill half of the menu filters case-insensitively
// (skillSuggestions), and a rank that disagreed with the filter that admitted a row would sort a
// genuine match as if it were none. 3 is defensive: every caller ranks a name its own filter has
// already accepted, so no rendered row ever carries it.
//
// An empty partial — the bare "/" that opens the whole menu — prefixes every name, so the entire
// list lands in one tier and reads exactly as it did before ranking existed.
func slashMatchRank(partial, name string) int {
	needle, hay := strings.ToLower(partial), strings.ToLower(name)
	switch {
	case hay == needle:
		return 0
	case strings.HasPrefix(hay, needle):
		return 1
	case strings.Contains(hay, needle):
		return 2
	default:
		return 3
	}
}

// autocompleteState is the overlay's data. active gates rendering and key capture (it is a
// value field on the Model, so an inactive zero value simply means "hidden"). tokenStart and
// tokenEnd bound the byte range of the token being completed — the completion REGION: accept
// splices over exactly that range and re-seats the caret after it, so whatever the human had
// already written on either side survives untouched.
type autocompleteState struct {
	active     bool
	kind       acKind
	items      []acItem
	selected   int
	tokenStart int
	tokenEnd   int
}

// computeAutocomplete derives the overlay from the current input value and the caret's byte offset
// into it. It returns an inactive state when nothing should be suggested. Called wherever the box
// is editable and the overlay claims keys — stateIdle and stateRunning (handleKey).
//
// The caret arrives as an ARGUMENT rather than being read off the widget, which keeps this a pure
// function of (value, caret): recomputeAutocomplete is the one place that asks the textarea where
// its cursor is, and every test constructs the pair directly.
//
// Both regions now share ONE lifetime: each is offered wherever the box is editable, a
// running worker included — the first ISSUES #12 symptom was that the "/" namespace vanished
// exactly when the human was composing the message to send next. An "@file" ref is as useful in an
// interjection as in a submitted message (it resolves at delivery, fresh) and a skill "/token" is
// message content that rides the interjection the same way. A COMMAND is the one that cannot
// simply ride, so the menu tells the truth about it instead of hiding it: the verbs that only
// report run mid-run, the ones that need a quiescent engine are TAGGED "— idle only"
// (commandSuggestions) and earn commandsAtIdleNote if accepted. An offered row that says what it
// will do is worth more than a namespace that disappears.
//
// Each region is scoped to the TOKEN AT THE CARET, never to the whole line and no longer to the end
// of the buffer: the "/" menu opens on the "/word" being edited in a draft that already holds text
// — before it, after it, or both — which is what lets a command be summoned, a skill invoked, or a
// mistyped id repaired without first emptying the box or walking the caret back to the end.
func (m Model) computeAutocomplete(caret int) autocompleteState {
	value := m.input.Value()

	// Command + skill: the caret's word is a "/" token — one namespace, one menu.
	if start, end, partial, ok := caretSlashToken(value, caret); ok {
		items := m.slashSuggestions(partial, outsideRegion(value, start, end))
		if len(items) == 0 {
			return autocompleteState{}
		}
		return autocompleteState{active: true, kind: acCommand, items: items, tokenStart: start, tokenEnd: end}
	}

	// File: the caret stands in an "@" token being typed — bare, or quoted across its spaces.
	if start, end, partial, ok := caretFileToken(value, caret); ok {
		items := m.fileSuggestions(partial)
		if len(items) == 0 {
			return autocompleteState{}
		}
		return autocompleteState{active: true, kind: acFile, items: items, tokenStart: start, tokenEnd: end}
	}

	return autocompleteState{}
}

// outsideRegion is the draft MINUS the completion region: the text the region does not cover. It is
// what "already invoked" is read against (skillSuggestions), since the token being completed is
// about to be replaced and so cannot count as its own duplicate. The two halves join directly
// because a region always begins at a word boundary — the head is empty or ends in whitespace — so
// cutting the middle out never fuses two words into one.
func outsideRegion(value string, start, end int) string {
	return value[:start] + value[end:]
}

// recomputeAutocomplete re-derives the overlay from the current input and stores it, and hands back
// the skill-catalog reload the moment the catalog-listing region OPENS — the input entering the
// merged "/" menu that it was not in before. The reload swaps the shared
// skills.Provider that both those rows and the agent loop read, so a skill added since launch — or
// since the menu last closed — both shows in the dropdown and resolves when invoked. It is
// edge-triggered on skillRegion so a burst of keystrokes inside one open region re-scans disk once,
// not per byte (mirroring the filecache TTL's "reuse one walk" intent, but keyed to opens). This is
// also the ONE place the textarea is
// asked where its cursor is: callers use it instead of assigning m.computeAutocomplete(…) directly,
// and computeAutocomplete stays a pure function of the (value, caret) pair a test can construct.
//
// The reload comes back as a Cmd rather than being CALLED here, which is the whole of this
// function's second return value. A re-scan is a full walk of the skill source dirs, and running it
// on this goroutine blocked the render loop for the length of that walk — on the keystroke that
// opened the menu, which is the one moment the human is watching the box. ADR 0011's division puts
// work that touches the disk on a worker and lands its result as a message, exactly as the recall
// read and the session list already do (loadRecallCmd). The menu therefore opens over the catalog as
// it stood, and skillsReloadedMsg repaints it over the fresh one a moment later
// (foldSkillsReloaded). Callers must return the Cmd to the Update loop — the signature is what makes
// the compiler ask them to, rather than a reload silently going nowhere.
//
// The reload is state-blind, because the region is: a skill invoked from an interjection is
// resolved by the same shared provider a submitted one is, so a "/" token typed while the model
// works must see the catalog as it stands now, exactly as one typed at idle does.
func (m Model) recomputeAutocomplete() (Model, tea.Cmd) {
	value := m.input.Value()
	caret := m.caretByteOffset() // the one place the widget is asked where its cursor is
	_, _, _, inMenu := caretSlashToken(value, caret)
	var reload tea.Cmd
	if inMenu && !m.skillRegion {
		reload = m.reloadSkillsCmd() // region opening: re-scan off the loop, repaint when it lands
	}
	m.skillRegion = inMenu
	m.autocomplete = m.computeAutocomplete(caret)
	return m, reload
}

// skillsReloadedMsg reports that the catalog re-scan reloadSkillsCmd dispatched has finished and the
// shared skills.Provider now holds the fresh snapshot. It carries no payload: the catalog is read
// through [Options.Skills], so this is the SIGNAL that the read is worth redoing rather than the
// result of it (recallLoadedMsg's posture, one field shorter).
type skillsReloadedMsg struct{}

// Compile-time assertion that the reload Msg is a valid tea.Msg (mirroring messages.go).
var _ tea.Msg = skillsReloadedMsg{}

// skillRescanCmd builds the Cmd that re-scans the skill source dirs OFF the Update loop and reports
// the finished swap as done — the Msg the caller passes, which is what decides which repaint the
// scan is owed. It captures the seam by value so the closure holds no pointer into the value-copied
// Model (loadRecallCmd's posture, ADR 0011). An unwired [Options.ReloadSkills] yields a nil Cmd, so
// a build with no refresh schedules nothing at all — the pre-Cmd nil guard, moved one layer out.
//
// Every trigger of the walk is built here — the menu opening (reloadSkillsCmd) and the /skills
// listing (runSkills, skills.go) — so a re-scan is dispatched in exactly ONE way and no future
// trigger can quietly acquire its own inline walk or its own capture rule.
//
// The host's reload now runs on a Cmd goroutine while the loop goroutine may be resolving skills
// against the same provider, which is precisely the concurrency that provider is built for: it swaps
// a whole immutable catalog under an atomic pointer (internal/skills/provider.go), so a reader sees
// either the old snapshot or the new one and never a torn one.
func (m Model) skillRescanCmd(done tea.Msg) tea.Cmd {
	reload := m.opts.ReloadSkills
	if reload == nil {
		return nil
	}
	return func() tea.Msg {
		reload()
		return done
	}
}

// reloadSkillsCmd is the menu's half of the re-scan: the walk dispatched off the Update loop,
// reported as the skillsReloadedMsg that repaints the open dropdown over the fresh catalog.
func (m Model) reloadSkillsCmd() tea.Cmd {
	return m.skillRescanCmd(skillsReloadedMsg{})
}

// foldSkillsReloaded re-derives the dropdown over the catalog the finished scan installed, so the
// skill that scan discovered shows in the menu that asked for it. It is the second half of the
// off-loop reload: the keystroke opened the menu over the catalog as it stood, this repaints it over
// the one on disk.
//
// It repaints only where a repaint is OWED. A menu the human has since closed (skillRegion false), or
// a modal that has taken the frame since — an approval, an ask, the states the overlay is never
// derived at (dismissAutocomplete) — leave the fold inert, so a scan landing late can never re-open a
// dropdown over a decision surface.
//
// The highlighted ROW survives the repaint (reselectRow). A bare re-derivation hands the selection
// back to the first item, and the very reason the walk is now off the loop is that it may finish long
// after the human started arrowing down the list — a menu that jumped its highlight out from under
// them would be a worse trade than the block it replaced.
func (m *Model) foldSkillsReloaded() {
	if !m.skillRegion || (m.state != stateIdle && m.state != stateRunning) {
		return
	}
	prev := m.autocomplete
	next := m.computeAutocomplete(m.caretByteOffset())
	next.selected = reselectRow(prev, next)
	m.autocomplete = next
	m.layout() // the dropdown's rows come out of the viewport, and a fresh catalog may change how many
}

// reselectRow maps the highlighted row of prev onto next by the VALUE it stood on, falling back to
// the first row when that value is gone from the list. Matching on the value rather than the index is
// what survives a reload that inserts a row ABOVE the selection: the id sorts where it sorts, and an
// index kept blindly would then point at the row above the one the human was looking at.
func reselectRow(prev, next autocompleteState) int {
	if !prev.active || prev.selected < 0 || prev.selected >= len(prev.items) {
		return 0
	}
	want := prev.items[prev.selected]
	for i, it := range next.items {
		if it.value == want.value && it.skill == want.skill {
			return i
		}
	}
	return 0
}

// caretToken reports the whitespace-delimited token the caret stands in: the byte range
// [start,end) reaching from the boundary left of the caret to the boundary right of it. It is the
// whole of the caret-awareness rule, and every completion region is derived from it — which is what
// scopes a menu to the word being EDITED and never to one further along the line.
//
// The caret counts as being in the token when it sits anywhere inside it or immediately after its
// last byte, because that is where an editing caret sits while a word is being typed or repaired. A
// caret in a run of whitespace yields an empty range (start == end): there is no word under it, so
// no region matches.
//
// Two invariants the callers rely on: start <= caret <= end, and start is either 0 or preceded by
// whitespace, so a token is at a word boundary by construction (the extractSkillRefs/extractFileRefs
// rule, arrived at from the other direction).
func caretToken(value string, caret int) (start, end int) {
	caret = clampInt(caret, 0, len(value))
	start, end = caret, caret
	for start > 0 && !isInputSpace(value[start-1]) {
		start--
	}
	for end < len(value) && !isInputSpace(value[end]) {
		end++
	}
	return start, end
}

// caretSlashToken reports the "/" token at the caret: its byte range, the partial verb-or-id after
// the slash, and whether the caret's token is one at all. It is caretFileToken's bare rule under a
// different sigil, and it is what turns the "/" menu from a whole-LINE rule into a caret-scoped one:
// the menu opens on the "/word" being edited anywhere in a draft that already holds text, instead of
// only on an otherwise-empty box. A caret on whitespace has no token (the word is finished).
//
// There is no quoted shape to read, because the token is a NAME: command verbs are whitespace-free
// by their registry, and a skill id is whitespace-free because the loader refuses one that is not
// (skills.validate — an id is one token, no whitespace and no control characters). That is an
// enforced rule, not the property of directory names it was once mistaken for: a repo names its own
// skill folders, so "by construction" was never true here, and an id like "confine off --save" read
// as one bare word right up to the moment the parser cut it into a verb and arguments. The bare
// word is the whole grammar because nothing may enter the catalog that would need more of one.
func caretSlashToken(value string, caret int) (start, end int, partial string, ok bool) {
	start, end = caretToken(value, caret)
	if start == end || value[start] != '/' {
		return 0, 0, "", false
	}
	return start, end, value[start+1 : end], true
}

// caretFileToken reports the "@" token at the caret: its byte range, the partial path after the "@"
// (and after any opening quote), and whether the caret stands in such a token. The token must sit at
// a word boundary (start of value or after whitespace); a caret on whitespace stands in no token.
//
// It reads both shapes of the ref grammar (scanRefToken owns it, command.go):
//
//   - bare — the caret's own whitespace-delimited word, "@internal/loop.go";
//   - quoted — a word-boundary "@" followed by a quote whose token spans the caret. An open quote
//     keeps the overlay alive across the spaces the bare rule would tokenize on (@"my pl → partial
//     "my pl"), and a closing quote yields the inner path (@"my plan.md" → partial "my plan.md"), so
//     a fully-typed quoted token can still match its suggestion exactly and let ⏎ submit.
//
// The quoted shape is tried first: its own closing quote and interior spaces are precisely what
// the bare rule would mis-read.
func caretFileToken(value string, caret int) (start, end int, partial string, ok bool) {
	for i := 0; i < len(value); i++ {
		if value[i] != '@' {
			continue
		}
		if i > 0 && !isInputSpace(value[i-1]) { // not at a word boundary ⇒ not a ref (e.g. an email)
			continue
		}
		if i+1 >= len(value) || (value[i+1] != '"' && value[i+1] != '\'') {
			continue
		}
		p, e := scanRefToken(value, i+1)
		if i <= caret && caret <= e { // the caret stands in (or just after) this quoted token
			return i, e, p, true
		}
		i = e - 1 // some other quoted token: resume scanning past it
	}
	start, end = caretToken(value, caret)
	if start == end || value[start] != '@' {
		return 0, 0, "", false
	}
	return start, end, value[start+1 : end], true
}

// fileRefToken renders path as the "@" reference the overlay shows and splices for it: the
// canonical double-quoted form when the path contains a space or a tab (a bare token would
// split there and never resolve), the bare form otherwise. Labels and accept share this one
// function, so a row always shows exactly what accepting it will insert.
func fileRefToken(path string) string {
	if strings.ContainsAny(path, " \t") {
		return `@"` + path + `"`
	}
	return "@" + path
}

// idleOnlyTag fills the third column of a command row that cannot run in the state the menu is open
// in. The dropdown offers every verb while a worker works — hiding half the namespace is what made
// the "/" menu useless mid-run — so the row that would be refused says so instead of pretending. It
// needs no style of its own: renderPopup paints every unselected row faint already, and the
// selected one on its highlight bar, so the tag inherits whichever the row is wearing (layout.md's
// "in the pane's faint unselected style").
const idleOnlyTag = "— idle only"

// commandSuggestions returns the verbs of commandSpecs (command.go — the one registry the parser
// reads too) whose name has partial as a prefix, in table order, each as the command schema's three
// cells — ["/verb", summary, idle-only tag] — which the popup module lays out as columns, so the
// summaries line up down the pane however long the verbs beside them are (the value stays the bare
// verb). It is the command half of the merged "/" menu (slashSuggestions). Every row of the
// registry is offered.
//
// Each row also carries its match rank (slashMatchRank), which the merged menu sorts on. The
// command half's own order is untouched by that sort: a verb only ever matches exactly or by
// prefix, and the exactly-matched one is the shortest of the names sharing the prefix, so it
// already stood first in the alphabetical table.
//
// busy says a worker owns the engine, which is what fills the tag cell: the verbs
// commandSpec.whileRunning marks as reporting-only leave it empty (they run right here), every other
// row carries the tag and earns commandsAtIdleNote if accepted. The tag is a property of the
// MOMENT, not of the verb, so it is a parameter rather than a second table column. At idle no row
// fills that cell at all and the whole column collapses (layoutPopupRow), costing the pane nothing.
func commandSuggestions(partial string, busy bool) []acItem {
	var items []acItem
	for _, c := range commandSpecs {
		if !strings.HasPrefix(c.name, partial) {
			continue
		}
		tag := ""
		if busy && !c.whileRunning {
			tag = idleOnlyTag
		}
		items = append(items, acItem{
			value: c.name,
			cells: popupRow{"/" + c.name, c.summary, tag},
			rank:  slashMatchRank(partial, c.name),
		})
	}
	return items
}

// slashSuggestions builds the merged "/" menu: the commands whose name partial prefixes, labelled
// with their summaries (commandSuggestions), and the catalog skills partial matches, each marked
// with glyphSkill — the transcript's own skill glyph — and shown as the "/id" token accepting it
// writes. One namespace, two kinds of row.
//
// The rows are ordered by MATCH QUALITY rather than by which half produced them: a stable sort on
// the rank both halves already carry (slashMatchRank) puts an exact match first, then the prefix
// matches, then the substring matches only a skill can be. Ties keep the scan order the merge laid
// down — the commands in table order, then the skills in catalog order — so a bare "/" (one tier:
// everything is a prefix match) still reads alphabetically with the verbs above the skills, and
// commands still lead every tier they share with a skill, because a verb ACTS on the session while
// a skill is content the human is composing. What changes is only that a skill matched somewhere
// in the MIDDLE of its name no longer outranks one the partial actually starts — typing "imple"
// now offers /implement-plan above /feature-implementation, which is what ⏎ and tab accept.
//
// Commands SHADOW skills: a skill whose id's FIRST TOKEN is a verb of commandSpecs is dropped from
// the merged rows, because the whole-input parse would read "/id" as that command anyway. The
// first-token rule is the parser's own (firstCommandToken, command.go) and is read from there
// rather than restated: a guard that tested the whole id would leave exactly the ids that carry
// arguments — "/confine off --save" — showing as innocent skill rows. The collision is settled
// here, menu-side, so the parse layer never has to know skills exist — and the shadowed skill stays
// invocable by typing its "/id" token anywhere but at the head of the line, where no command rule
// claims it.
//
// outside is the draft text OUTSIDE the region being completed, so the half-typed token can never
// suppress its own row while the already-invoked ones stay out (skillSuggestions).
//
// The skill rows are never tagged, whatever the model is doing: a skill token is message content
// that rides an interjection to the running Exchange, so it is as invocable mid-run as at idle. Only
// the command half answers to the while-running policy (commandSuggestions takes m.busy()).
//
// A skill row follows the merged menu's schema: ["✦ /id · source", what the skill is]. The
// two kinds of row therefore share one second column, so a skill's description starts exactly where
// the command summaries above it do rather than wherever its own token happened to end.
//
// The source rides in the FIRST cell, beside the id, rather than in a column of its own after the
// description (skills.go says why: the description is the repo's own text and is long, so a source
// placed after it is the first thing a padded summary pushes off the pane). The id it sits beside
// is bounded and folded by skillTokenLabel, so nothing a SKILL.md chooses can move it, hide it, or
// paint a second row beneath it.
func (m Model) slashSuggestions(partial, outside string) []acItem {
	items := commandSuggestions(partial, m.busy())
	for _, sk := range m.skillSuggestions(partial, outside) {
		// Keyed on the id's FIRST TOKEN, the same cut matchCommand makes (firstCommandToken,
		// command.go) — never on the whole id, or the guard and the parser read different things
		// and a skill named "confine off --save" is offered as a skill row and run as a command.
		if _, shadowed := commandByName(firstCommandToken(sk.value)); shadowed {
			continue
		}
		items = append(items, acItem{
			value: sk.value,
			// The token cell is escape-stripped like every other cell this module builds
			// (sessionRowCells, launchProfileRows) — a skill id is a directory name a repo chose —
			// and is folded and clipped besides (skillTokenLabel → skillIDCell), so the row shows
			// the whole id or says it did not. The description cell arrives already stripped and
			// flattened from skillSuggestions.
			cells: popupRow{glyphSkill + " " + skillTokenLabel(sk.value, sk.source), skillMenuCell(sk.cells)},
			skill: true,
			rank:  sk.rank,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].rank < items[j].rank })
	return items
}

// skillMenuCell flattens a skill row's cells — display name, summary — into the ONE cell the
// merged "/" menu gives a skill, joined by the module's own gutter so the flattened text reads with
// the same rhythm the columns elsewhere do. Aligning those two tiers against each other would need
// a column schema of their own; the merged menu instead aligns the whole description against the
// command summaries, and a row cannot be in two column schemas at once. Empty tiers drop out rather
// than leaving a hanging gutter.
func skillMenuCell(cells popupRow) string {
	parts := make([]string, 0, len(cells))
	for _, cell := range cells {
		if cell != "" {
			parts = append(parts, cell)
		}
	}
	return strings.Join(parts, popupGutter)
}

// skillSuggestions lists skills matching partial (a case-insensitive substring of id or
// displayName), excluding those the message already invokes, as two cells —
// ["DisplayName", "Summary"] — which the merged "/" menu flattens into the single description cell
// a skill row gets (skillMenuCell), so what each skill DOES aligns against the command summaries
// above it. The value is the skill ID (what the accepted row splices in as a "/id" token), and the
// source is which dir the skill was loaded from, for the merged menu to render beside that id. A
// nil catalog yields nothing (the menu shows no skill rows).
//
// The list is ranked by match quality (slashMatchRank) and only THEN cut to maxAutocompleteItems,
// which is the whole reason the cap moved out of the scan loop. Capping first meant the cut was
// made in catalog order, so a skill the partial prefixes could be dropped for weaker substring
// matches that merely sorted earlier alphabetically — the menu discarded its best answer to keep
// its worst ones. The sort is stable, so equally-matched skills keep the catalog's order.
//
// "Already invoked" is read off the BUFFER — the /tokens standing in the text right now — because
// the text is where an invocation lives; there is no attachment state beside it to consult. Delete
// the token and the skill is offered again, which is the same self-healing rule the inline accents
// and the submit parse read. outside is the part of the buffer the completion region does NOT
// cover, on BOTH sides of it (outsideRegion): the region itself is about to be replaced, so a skill
// named inside it is not yet invoked (a fully typed "/clean-code" must keep offering its own row, or
// ⏎ could never confirm it), while one named further along the draft is.
func (m Model) skillSuggestions(partial, outside string) []acItem {
	if m.opts.Skills == nil {
		return nil
	}
	attached := map[string]bool{}
	for _, id := range extractSkillRefs(outside, m.knownSkillID) {
		attached[id] = true
	}
	needle := strings.ToLower(partial)
	var items []acItem
	for _, sk := range m.opts.Skills.List() {
		if attached[sk.ID] {
			continue
		}
		if needle != "" &&
			!strings.Contains(strings.ToLower(sk.ID), needle) &&
			!strings.Contains(strings.ToLower(sk.DisplayName), needle) {
			continue
		}
		// Both cells come from a repo-supplied SKILL.md front matter, so they are escape-stripped
		// where the row is built — the popup module strips nothing and truncates
		// ANSI-preservingly, and an ESC byte takes string length but no display cell, so an
		// unstripped cell would both reach the terminal live and lie to the column math. They are
		// FLATTENED for the neighbouring reason (flattenField): stripEscapes keeps "\n" because its
		// biggest callers are wrapped prose bodies, while a menu cell is one row — a kept newline
		// there is a row the pane did not author, in a pane whose rows are chosen with ⏎.
		items = append(items, acItem{
			value: sk.ID,
			cells: popupRow{
				flattenField(stripEscapes(sk.DisplayName)),
				flattenField(stripEscapes(sk.Summary)),
			},
			// The better of the two names the filter above accepted the skill on: matching an id
			// by prefix is worth as much as matching a display name by one, and a skill must not
			// be ranked down for the name it did NOT match through.
			rank: min(slashMatchRank(partial, sk.ID), slashMatchRank(partial, sk.DisplayName)),
			// The one row fact the SKILL.md does not author, carried from the only place that holds
			// the Dir it is read off (Options.ConfigHome/Workspace are the loader's own roots).
			source: skillSource(sk.Dir, m.opts.ConfigHome, m.opts.Workspace),
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].rank < items[j].rank })
	if len(items) > maxAutocompleteItems {
		items = items[:maxAutocompleteItems]
	}
	return items
}

// fileSuggestions lists workspace files matching the typed partial as "@path" rows — quoted
// rows for paths with spaces (fileRefToken), so the dropdown teaches the syntax before the user
// ever types a quote — served through the Model's file cache so a typing burst reuses one
// workspace walk (filecache.go). newModel always installs the cache, so m.files is never nil
// here. The item's value is the path itself; only the cell carries the sigil and quotes.
//
// A path is one thing, not a name and a description, so a file row stays SINGLE-CELL: one column,
// which the popup module lays out as the plain "@path" it always was.
//
// The PATH is escape-stripped ONCE, before either half of the row is derived from it — a filename is
// the WORKSPACE's, not this program's, and a clone can carry one holding an ESC byte. Stripping only
// the cell left the row's other half unsanitized, and a row is not merely shown: accepting it
// SPLICES its value into the composer (acceptAutocomplete → fileRefToken → spliceCompletion), a box
// this package then paints. That the escape does not in fact reach the screen today is the bubbles
// textarea's doing, not this package's — every insertion path runs an internal runeutil.Sanitizer
// that drops control runes — which is a third-party internal with no compatibility promise standing
// where doc.go's seam invariant says this package's own strip belongs.
//
// Keeping the value raw "so the reference still resolves on disk" was never the trade it looked
// like, because display and resolution are the SAME string: an @ref resolves from the token read
// back out of the composed text (extractFileRefs → UserInput.FileRefs → the loop's resolveFileRefs),
// never from the acItem, so there is no second channel a raw path could travel down. What the
// mismatch did buy was a bug: the box held the widget's sanitized text while the row held the raw
// path, so autocompleteExactMatch could never match such a row and ⏎ on a fully-typed token
// re-accepted instead of submitting. Stripping here restores fileRefToken's contract — a row shows
// exactly what accepting it will insert — for the one case that broke it.
//
// The cost is that a workspace file whose NAME carries an ESC byte cannot be referenced through the
// dropdown: the stripped path names nothing on disk, so the token never lights up (inputaccent's
// knownWorkspaceFile asks the same listing) and a submitted ref is reported unresolvable and skipped
// rather than silently read. An ESC byte in a filename is hostile far more often than load-bearing,
// and the seam invariant settles which way that trade goes.
func (m Model) fileSuggestions(partial string) []acItem {
	paths := m.files.suggest(m.opts.Workspace, partial, maxAutocompleteItems, time.Now())
	items := make([]acItem, 0, len(paths))
	for _, p := range paths {
		p = stripEscapes(p)
		items = append(items, acItem{value: p, cells: popupRow{fileRefToken(p)}})
	}
	return items
}

// autocompleteKey handles a keypress while the overlay is open (idle, or the file region while a
// worker runs — handleKey gates which states consult it). It reports
// whether it consumed the key: up/down (and ctrl+p/ctrl+n) move the selection; tab/enter
// accept the highlighted item (splicing it in, NOT submitting); esc dismisses the overlay.
// Any other key returns handled=false so the input-editing path takes it and re-derives the
// overlay.
func (m Model) autocompleteKey(msg tea.KeyPressMsg) (bool, tea.Model, tea.Cmd) {
	ac := m.autocomplete
	n := len(ac.items)
	if n == 0 {
		return false, m, nil
	}
	switch msg.String() {
	case "up", "ctrl+p":
		ac.selected = (ac.selected - 1 + n) % n
		m.autocomplete = ac
		return true, m, nil
	case "down", "ctrl+n":
		ac.selected = (ac.selected + 1) % n
		m.autocomplete = ac
		return true, m, nil
	case "tab":
		nm, cmd := m.acceptAutocomplete()
		return true, nm, cmd
	case "enter":
		// Enter falls through to submit when the token is already fully typed AND submitting is the
		// more useful answer (autocompleteExactMatch); otherwise it accepts the highlighted row —
		// which completes a skill or a file, and RUNS a command.
		if m.autocompleteExactMatch() {
			return false, m, nil
		}
		nm, cmd := m.acceptAutocomplete()
		return true, nm, cmd
	case "esc":
		m.dismissAutocomplete()
		return true, m, nil
	}
	return false, m, nil
}

// dismissAutocomplete closes the "/" | "@" dropdown.
//
// Besides the keystrokes that dismiss it, it is what a MODAL PROMPT arriving does to a menu the
// human left open while the agent worked. The dropdown is only ever DERIVED at the two states where
// the box is the human's own — recomputeAutocomplete runs at idle and running and nowhere else — so
// a menu that survives into the approval or ask fold is frozen: no keystroke there filters it,
// dismisses it or accepts from it (handleKey gives those keys to the decision), yet it went on
// sharing the frame with the decision surface and competing with it for the same rows. Neither fold
// cleared it, so a "/" typed a moment before the gate opened was still on the screen, still
// highlighting a row that could no longer be chosen.
//
// It clears the skillRegion edge-trigger with it, so "a region is open" and "a menu is on the
// screen" cannot disagree. That matters now the catalog re-scan is asynchronous: without it, a walk
// dispatched by the open would land on a dismissed menu and paint it back (foldSkillsReloaded),
// which is the one thing esc is for. The cost is one extra walk when the human goes on typing in a
// token they dismissed the menu on — a re-opened menu is an opening, and an opening owes a re-scan.
func (m *Model) dismissAutocomplete() {
	m.autocomplete = autocompleteState{}
	m.skillRegion = false
}

// autocompleteExactMatch reports whether ⏎ should fall THROUGH to submit instead of accepting the
// highlighted row. Two things decide it: the token under completion must already equal that row
// verbatim (sigil included), and accepting must not be the more useful answer.
//
// A file token counts as typed out in any dialect the parser accepts — bare, double-quoted or
// single-quoted (command.go) — and a directly typed skill token is ordinary message text, so both
// let ⏎ send the moment they are complete, wherever in the draft they stand.
//
// A COMMAND is the asymmetric one, because accepting it now RUNS it (acceptAutocomplete). ⏎ falls
// through only when the token is the WHOLE trimmed input — the form the whole-input parse owns, and
// the only form that can carry arguments ("/confine off --save"). Mid-draft, an exactly typed
// "/clear" executes through the accept path instead, which is what keeps the rest of the draft
// alive rather than sending it.
func (m Model) autocompleteExactMatch() bool {
	ac := m.autocomplete
	value := m.input.Value()
	if !ac.active || len(ac.items) == 0 || ac.tokenEnd > len(value) || ac.tokenStart > ac.tokenEnd {
		return false
	}
	it := ac.items[ac.selected]
	typed := value[ac.tokenStart:ac.tokenEnd]
	if ac.kind == acFile {
		return typed == "@"+it.value ||
			typed == `@"`+it.value+`"` ||
			typed == "@'"+it.value+"'"
	}
	if typed != "/"+it.value {
		return false
	}
	if it.skill {
		return true // a finished skill token is text: ⏎ sends the message it stands in
	}
	return strings.TrimSpace(value) == typed
}

// acceptAutocomplete applies the highlighted row over the region that opened the overlay — and for
// most command rows that means ACTING, not completing:
//
//   - a skill row chosen in the merged menu becomes its own inline "/id " token (insertSkillToken);
//   - a file becomes its reference token (fileRefToken — quoted when the path has spaces, whatever
//     form the partial was typed in). The quoting is decided by the PATH, never by how the user
//     started typing: a bare "@my" partial completing to a spaced path still splices the quoted
//     token, because only that one resolves;
//   - /confine completes to "/verb " and waits — it reads arguments, and firing a verb that is not
//     finished would be wrong;
//   - every other command RUNS — if it may run NOW. Its token is cut out of the draft
//     (removeCompletionToken) and runCommand drives it, so invoking a command from the middle of a
//     half-written message costs the message nothing. An idle-only verb accepted while a worker
//     works is refused instead (refuseIdleOnlyCommand): the row was tagged "— idle only", and the
//     note repeats that answer without touching a character the human typed.
//
// The cursor lands at the end of the spliced text, or where the cut token stood.
func (m Model) acceptAutocomplete() (tea.Model, tea.Cmd) {
	ac := m.autocomplete
	if !ac.active || len(ac.items) == 0 {
		return m, nil
	}
	it := ac.items[ac.selected]
	switch {
	case it.skill:
		return m.insertSkillToken(it.value)
	case ac.kind == acFile:
		return m.spliceCompletion(fileRefToken(it.value))
	}
	if spec, ok := commandByName(it.value); ok && spec.takesArgs {
		return m.spliceCompletion("/" + it.value)
	}
	parsed := parsedInput{kind: kindCommand, command: it.value}
	if !m.commandRunnable(parsed) {
		return m.refuseIdleOnlyCommand()
	}
	return m.removeCompletionToken().runCommand(parsed)
}

// completionRegion is the byte range the overlay is completing, clamped to the value as it stands
// now. The clamp is defensive — the value cannot have shrunk between the compute and the accept,
// which happen inside one Update — but a splice must never slice out of range.
func (m Model) completionRegion() (start, end int) {
	n := len(m.input.Value())
	start = clampInt(m.autocomplete.tokenStart, 0, n)
	end = clampInt(m.autocomplete.tokenEnd, start, n)
	return start, end
}

// removeCompletionToken cuts the token the overlay was completing out of the draft and closes the
// overlay. It is the editor half of "a command row is an action": the verb is consumed by RUNNING
// it, so what stays in the box is the message the human was writing, minus the word that invoked
// the command — never an emptied box (which would lose the draft) and never a dead "/clear" left
// standing in the text (which would be sent along with it).
//
// The caret lands where the token stood, between what flanked it. Cutting a token from the middle of
// a draft would leave the separators from BOTH sides ("fix /clear it" → "fix  it"), so one of them
// is collapsed — the word spacing the human typed is restored, not doubled. A token at the end
// leaves the separator before it standing: it is where they were writing, and it is what the next
// word needs anyway.
func (m Model) removeCompletionToken() Model {
	value := m.input.Value()
	start, end := m.completionRegion()
	head, tail := value[:start], value[end:]
	if head != "" && tail != "" && isInputSpace(head[len(head)-1]) && isInputSpace(tail[0]) {
		tail = tail[1:] // collapse the doubled separator the cut would otherwise leave
	}
	m.input.SetValue(head + tail)
	m.caretToOffset(len(head))
	m.dismissAutocomplete()
	m.layout()
	return m
}

// insertSkillToken writes the skill's inline invocation — "/id " — over the completion region,
// which is the "/" token the merged menu opened on. The token IS the attachment: it stays in the
// text the human sends, submitParse reads it back out as a skill reference, and deleting it
// un-invokes the skill.
func (m Model) insertSkillToken(id string) (Model, tea.Cmd) {
	return m.spliceCompletion("/" + id)
}

// spliceCompletion writes token, plus the separator that ends it, over the completion region and
// re-derives the overlay, leaving the caret just after what it wrote. It RECOMPUTES rather than
// blindly closing, so the overlay tracks the draft the splice left behind — which for a completed
// command, file or skill token means closing, because the separator ends the token.
//
// The separator is written only when the draft does not already carry one: completing a token in the
// middle of a sentence must not double the space before the next word. The caret then lands after
// the token — before the space the draft already had, or after the one just written — which is where
// the human goes on typing either way.
//
// The Cmd it returns is whatever that recompute owes (recomputeAutocomplete): a splice that leaves
// the caret inside a "/" token the box was not in before opens the menu, and an opening menu owes a
// catalog re-scan. It is passed out rather than dropped, because a dropped one is a refresh that
// silently never happens.
func (m Model) spliceCompletion(token string) (Model, tea.Cmd) {
	value := m.input.Value()
	start, end := m.completionRegion()
	head, tail := value[:start], value[end:]
	sep := " "
	if tail != "" && isInputSpace(tail[0]) {
		sep = "" // the draft already separates this token from what follows it
	}
	m.input.SetValue(head + token + sep + tail)
	m.caretToOffset(len(head) + len(token) + len(sep))
	var reload tea.Cmd
	m, reload = m.recomputeAutocomplete() // the separator ends the token, so this closes the overlay
	m.layout()
	return m, reload
}

// containsString reports whether s is in xs.
func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// autocompleteHint is the one-line key legend shown at the foot of the dropdown. It is coarse
// like sessionBrowserHint — the exact-match-Enter-submits nuance (autocompleteExactMatch) stays
// undocumented in the legend, as the session hint also elides its modes.
const autocompleteHint = "↑/↓ select · ⏎/tab accept · esc dismiss"

// autocompleteTitle names the dropdown by what it completes: the popup module's title row. The "/"
// region names both halves of its merged list, so the title never implies the skills below the
// command rows are commands too.
func autocompleteTitle(kind acKind) string {
	switch kind {
	case acCommand:
		return "commands and skills"
	case acFile:
		return "files"
	default:
		return ""
	}
}

// renderAutocomplete draws the suggestion dropdown shown above the input box, through the shared
// popup module (renderPopup): a titled, bordered pane spanning the full window width (m.width,
// flush with the input box below) holding the suggestion rows and a key legend, the selected row
// highlighted. The kind picks the title ("commands and skills"/"files"); each row hands
// over its CELLS and the module owns the column alignment along with the marker, highlight,
// truncation, and scroll windowing — so a menu mixing "/clear" with "✦ /clean-code" still starts
// every summary at one column. It returns "" when the overlay is inactive, so View treats it like
// the approval-prompt slot.
//
// The row window is the SCREEN's to grant, exactly as it is for the /sessions browser and the
// picker: maxAutocompleteItems is this overlay's own taste — how long a menu is worth reading —
// and popupBudget is the window's answer to it (D2). Asking for the taste outright is what the
// audit measured: the fourteen-verb "/" menu composed a 20-row frame on a 12-row terminal, +8 rows
// of input box and footer pushed off the alternate screen. A dropdown is the pane MOST likely to
// be open on a short window, because it opens while the human is typing, which is the one thing
// they are always doing.
//
// Shrinking costs rows, never the selection and never silence. A window granted at least one row
// scrolls around the selected item (popupRowWindow), so arrowing through a live filter keeps the
// highlight on the screen however few rows the frame can spare; a window granted NONE is counted
// out on the title row in the module's one marker (popupTitleLine), the same wording the browser
// and the ask prompt use — a menu that quietly showed nothing while its hint still read "↑/↓
// select" would be the browser's silent-drop defect wearing this pane's title.
func (m Model) renderAutocomplete() string {
	ac := m.autocomplete
	if !ac.active || len(ac.items) == 0 {
		return ""
	}
	rows := make([]popupRow, len(ac.items))
	for i, it := range ac.items {
		rows[i] = it.cells
	}
	_, shown, seated := m.popupBudget(paneDropdown, len(rows), maxAutocompleteItems, popupChrome, popupFloor{})
	if !seated {
		return "" // the frame cannot seat this pane beside its siblings (frameRowPlan)
	}
	spec := popupSpec{
		title:    autocompleteTitle(ac.kind),
		rows:     rows,
		selected: ac.selected,
		hint:     autocompleteHint,
		maxRows:  shown,
	}
	return renderPopup(m.th, spec, m.width)
}
