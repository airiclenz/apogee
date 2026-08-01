package tui

import (
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
// invoked from the middle of a half-written message without destroying it. The two verbs that need
// what follows them — /confine (arguments) and the menu-only /skill (a picker) — complete instead.

// maxAutocompleteItems caps how many suggestions the overlay shows (and how far the file
// walk runs) — enough to be useful, small enough that the popup never crowds the transcript
// off a short terminal and a large workspace walk stays cheap. Type more to narrow further.
const maxAutocompleteItems = 8

// acKind tags what an open overlay is completing.
type acKind int

const (
	acCommand acKind = iota // a "/command" word
	acFile                  // an "@file" reference
	acSkill                 // a "/skill <id>" argument (splices the skill's own inline "/id" token)
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
type acItem struct {
	value string
	cells popupRow
	skill bool
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
// All three regions now share ONE lifetime: each is offered wherever the box is editable, a
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

	// Skill argument: a "/skill <partial>" region (the caret's word, introduced by a "/skill" token).
	// Checked FIRST so it wins over the merged "/" branch — which would otherwise see the partial as
	// a "/" token of its own. The region starts at the "/skill" itself, so accepting replaces the
	// whole "/skill <partial>" run with the skill's own "/id " token.
	if start, end, partial, ok := skillArgToken(value, caret); ok {
		items := m.skillSuggestions(partial, outsideRegion(value, start, end))
		if len(items) == 0 {
			return autocompleteState{}
		}
		return autocompleteState{active: true, kind: acSkill, items: items, tokenStart: start, tokenEnd: end}
	}

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

// recomputeAutocomplete re-derives the overlay from the current input and stores it, reloading
// the skill catalog the moment a catalog-listing region OPENS — the input entering a "/skill
// <partial>" picker, or the merged "/" menu, that it was not in before. The reload swaps the shared
// skills.Provider that both those rows and the agent loop read, so a skill added since launch — or
// since the menu last closed — both shows in the dropdown and resolves when invoked. It is
// edge-triggered on skillRegion so a burst of keystrokes inside one open region re-scans disk once,
// not per byte (mirroring the filecache TTL's "reuse one walk" intent, but keyed to opens), and the
// two regions share the flag because typing "/skill" and then a space walks straight from one into
// the other — a single visit to the catalog, not two. This is also the ONE place the textarea is
// asked where its cursor is: callers use it instead of assigning m.computeAutocomplete(…) directly,
// and computeAutocomplete stays a pure function of the (value, caret) pair a test can construct.
//
// The reload is state-blind, because both regions are: a skill invoked from an interjection is
// resolved by the same shared provider a submitted one is, so a "/" token typed while the model
// works must see the catalog as it stands now, exactly as one typed at idle does.
func (m Model) recomputeAutocomplete() Model {
	value := m.input.Value()
	caret := m.caretByteOffset() // the one place the widget is asked where its cursor is
	_, _, _, inPicker := skillArgToken(value, caret)
	_, _, _, inMenu := caretSlashToken(value, caret)
	inSkill := inPicker || inMenu
	if inSkill && !m.skillRegion && m.opts.ReloadSkills != nil {
		m.opts.ReloadSkills() // region opening: re-scan before computeAutocomplete lists suggestions
	}
	m.skillRegion = inSkill
	m.autocomplete = m.computeAutocomplete(caret)
	return m
}

// caretToken reports the whitespace-delimited token the caret stands in: the byte range
// [start,end) reaching from the boundary left of the caret to the boundary right of it. It is the
// whole of the caret-awareness rule, and every completion region is derived from it — which is what
// scopes a menu to the word being EDITED and never to one further along the line.
//
// The caret counts as being in the token when it sits anywhere inside it or immediately after its
// last byte, because that is where an editing caret sits while a word is being typed or repaired. A
// caret in a run of whitespace yields an empty range (start == end): there is no word under it, so
// no region matches — except the "/skill " picker, whose partial is legitimately empty (skillArgToken).
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
// There is no quoted shape to read: command verbs and skill ids are whitespace-free by construction
// (skill ids are directory names — extractSkillRefs), so the bare word is the whole grammar.
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
// verb). It is the command half of the merged "/" menu (slashSuggestions). Every row is offered,
// menuOnly ones included: accepting /skill completes to "/skill " and chains into the skill picker
// (acceptAutocomplete recomputes the overlay), never sending "/skill" as a literal message — like
// the apogee-code oracle's selectSkill.
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
		items = append(items, acItem{value: c.name, cells: popupRow{"/" + c.name, c.summary, tag}})
	}
	return items
}

// skillArgToken reports the "/skill <partial>" region at the caret: the byte range running from the
// "/skill" token to the end of the partial (the whole run the picked skill's own token is spliced
// over), the partial id/name being typed, and whether the caret stands in such a region. The partial
// is the caret's own word (caretToken — empty when the caret sits just after the "/skill "), and the
// word immediately before it must be exactly "/skill". It accepts "/skill ", "/skill cl", and
// mid-line "fix /skill cl"; it rejects a bare "/skill" (no arg yet) and a completed "/skill foo "
// (the word before the caret is "foo", not "/skill").
func skillArgToken(value string, caret int) (start, end int, partial string, ok bool) {
	argStart, argEnd := caretToken(value, caret)
	if argStart == 0 {
		return 0, 0, "", false // nothing precedes the partial ⇒ no "/skill" can introduce it
	}
	// caretToken guarantees the byte before argStart is whitespace; the verb is the word ending there.
	verbEnd := argStart - 1
	verbStart := verbEnd
	for verbStart > 0 && !isInputSpace(value[verbStart-1]) {
		verbStart--
	}
	if value[verbStart:verbEnd] != "/skill" {
		return 0, 0, "", false
	}
	return verbStart, argEnd, value[argStart:argEnd], true
}

// slashSuggestions builds the merged "/" menu: first the commands whose name partial prefixes, in
// table order and labelled with their summaries (commandSuggestions), then the catalog skills
// partial matches, each marked with glyphSkill — the transcript's own skill glyph — and shown as
// the "/id" token accepting it writes. One namespace, two kinds of row, commands first because a
// verb ACTS on the session while a skill is content the human is composing.
//
// Commands SHADOW skills: a skill whose id equals any verb in commandSpecs is dropped from the
// merged rows, because the whole-input parse would read "/id" as that command anyway. The collision
// is settled here, menu-side, so the parse layer never has to know skills exist — and the shadowed
// skill stays reachable through the /skill picker, which splices its token where no command rule
// claims it.
//
// outside is the draft text OUTSIDE the region being completed, so the half-typed token can never
// suppress its own row while the already-invoked ones stay out (skillSuggestions).
//
// The skill rows are never tagged, whatever the model is doing: a skill token is message content
// that rides an interjection to the running Exchange, so it is as invocable mid-run as at idle. Only
// the command half answers to the while-running policy (commandSuggestions takes m.busy()).
//
// A skill row follows the merged menu's schema, not the picker's: ["✦ /id", what the skill is]. The
// two kinds of row therefore share one second column, so a skill's description starts exactly where
// the command summaries above it do rather than wherever its own token happened to end.
func (m Model) slashSuggestions(partial, outside string) []acItem {
	items := commandSuggestions(partial, m.busy())
	for _, sk := range m.skillSuggestions(partial, outside) {
		if _, shadowed := commandByName(sk.value); shadowed {
			continue
		}
		items = append(items, acItem{
			value: sk.value,
			// The token cell is escape-stripped like every other cell this module builds
			// (sessionRowCells, launchProfileRows): a skill id is a directory name a repo chose.
			// The description cell arrives already stripped from skillSuggestions.
			cells: popupRow{glyphSkill + " /" + stripEscapes(sk.value), skillMenuCell(sk.cells)},
			skill: true,
		})
	}
	return items
}

// skillMenuCell flattens a skill picker row's cells — display name, summary — into the ONE cell the
// merged "/" menu gives a skill, joined by the module's own gutter so the flattened text reads with
// the same rhythm the columns elsewhere do. The picker aligns those two tiers against each other;
// the merged menu instead aligns the whole description against the command summaries, and a row
// cannot be in two column schemas at once. Empty tiers drop out rather than leaving a hanging gutter.
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
// displayName), excluding those the message already invokes, as the picker's two cells —
// ["DisplayName", "Summary"] — which the popup module lays out as columns, so what each skill DOES
// starts at one shared offset however long the names beside it run. The value is the skill ID (what
// the accepted row splices in as a "/id" token). A nil catalog yields nothing (the picker is dark).
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
		// unstripped cell would both reach the terminal live and lie to the column math.
		items = append(items, acItem{
			value: sk.ID,
			cells: popupRow{stripEscapes(sk.DisplayName), stripEscapes(sk.Summary)},
		})
		if len(items) >= maxAutocompleteItems {
			break
		}
	}
	return items
}

// fileSuggestions lists workspace files matching the typed partial as "@path" rows — quoted
// rows for paths with spaces (fileRefToken), so the dropdown teaches the syntax before the user
// ever types a quote — served through the Model's file cache so a typing burst reuses one
// workspace walk (filecache.go). newModel always installs the cache, so m.files is never nil
// here. The item's value stays the raw path; only the cell carries the sigil and quotes.
//
// A path is one thing, not a name and a description, so a file row stays SINGLE-CELL: one column,
// which the popup module lays out as the plain "@path" it always was.
//
// The cell is escape-stripped: a filename is the WORKSPACE's, not this program's, and a clone can
// carry one holding an ESC byte. The item's value keeps the raw path, because that is what the
// splice must reproduce for the reference to resolve on disk.
func (m Model) fileSuggestions(partial string) []acItem {
	paths := m.files.suggest(m.opts.Workspace, partial, maxAutocompleteItems, time.Now())
	items := make([]acItem, 0, len(paths))
	for _, p := range paths {
		items = append(items, acItem{value: p, cells: popupRow{stripEscapes(fileRefToken(p))}})
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
		m.autocomplete = autocompleteState{}
		return true, m, nil
	}
	return false, m, nil
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
// alive rather than sending it. /skill is excluded outright: it is no parser verb, so submitting it
// would earn the typo guard's usage note instead of the picker it is an entry point to.
func (m Model) autocompleteExactMatch() bool {
	ac := m.autocomplete
	value := m.input.Value()
	if !ac.active || len(ac.items) == 0 || ac.tokenEnd > len(value) || ac.tokenStart > ac.tokenEnd {
		return false
	}
	// Inside the "/skill <partial>" picker the typed text is the PICKER's syntax, never the message
	// — a "/skill cl" that happens to equal nothing sendable — so Enter always completes there,
	// swapping the run for the skill's own token. (A directly typed "/id" token is ordinary text and
	// takes the exact-match path below.)
	if ac.kind == acSkill {
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
	if spec, ok := commandByName(it.value); ok && spec.menuOnly {
		return false // /skill chains into the picker; the parser would only refuse it
	}
	return strings.TrimSpace(value) == typed
}

// acceptAutocomplete applies the highlighted row over the region that opened the overlay — and for
// most command rows that means ACTING, not completing:
//
//   - a skill (picked from the /skill picker, or chosen directly in the merged menu) becomes its own
//     inline "/id " token — insertSkillToken, which for the picker REPLACES the whole "/skill
//     <partial>" run that summoned it;
//   - a file becomes its reference token (fileRefToken — quoted when the path has spaces, whatever
//     form the partial was typed in). The quoting is decided by the PATH, never by how the user
//     started typing: a bare "@my" partial completing to a spaced path still splices the quoted
//     token, because only that one resolves;
//   - /confine and the menu-only /skill complete to "/verb " and wait — one reads arguments, the
//     other chains into the picker, and firing a verb that is not finished would be wrong for both;
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
	case ac.kind == acSkill || it.skill:
		return m.insertSkillToken(it.value), nil
	case ac.kind == acFile:
		return m.spliceCompletion(fileRefToken(it.value)), nil
	}
	if spec, ok := commandByName(it.value); ok && (spec.takesArgs || spec.menuOnly) {
		return m.spliceCompletion("/" + it.value), nil
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
	m.autocomplete = autocompleteState{}
	m.layout()
	return m
}

// insertSkillToken writes the skill's inline invocation — "/id " — over the completion region,
// which is the whole "/skill <partial>" run the picker opened on (tokenStart marks the "/skill"
// itself). The token IS the attachment: it stays in the text the human sends, submitParse reads it
// back out as a skill reference, and deleting it un-invokes the skill. Shared by the picker's
// accept and (from the merged menu) a directly chosen skill row.
func (m Model) insertSkillToken(id string) Model {
	return m.spliceCompletion("/" + id)
}

// spliceCompletion writes token, plus the separator that ends it, over the completion region and
// re-derives the overlay, leaving the caret just after what it wrote. It RECOMPUTES rather than
// blindly closing: that closes the overlay for a completed command/file/skill token (the separator
// ends the token) but reopens it as the skill picker after "/skill " — the chain the oracle's
// selectSkill mirrors.
//
// The separator is written only when the draft does not already carry one: completing a token in the
// middle of a sentence must not double the space before the next word. The caret then lands after
// the token — before the space the draft already had, or after the one just written — which is where
// the human goes on typing either way.
func (m Model) spliceCompletion(token string) Model {
	value := m.input.Value()
	start, end := m.completionRegion()
	head, tail := value[:start], value[end:]
	sep := " "
	if tail != "" && isInputSpace(tail[0]) {
		sep = "" // the draft already separates this token from what follows it
	}
	m.input.SetValue(head + token + sep + tail)
	m.caretToOffset(len(head) + len(token) + len(sep))
	m = m.recomputeAutocomplete() // chains "/skill " → picker (reloading the catalog); else closes
	m.layout()
	return m
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
	case acSkill:
		return "skills"
	default:
		return ""
	}
}

// renderAutocomplete draws the suggestion dropdown shown above the input box, through the shared
// popup module (renderPopup): a titled, bordered pane spanning the full window width (m.width,
// flush with the input box below) holding the suggestion rows and a key legend, the selected row
// highlighted. The kind picks the title ("commands and skills"/"files"/"skills"); each row hands
// over its CELLS and the module owns the column alignment along with the marker, highlight,
// truncation, and scroll windowing — so a menu mixing "/clear" with "✦ /clean-code" still starts
// every summary at one column. It returns "" when the overlay is inactive, so View treats it like
// the approval-prompt slot.
func (m Model) renderAutocomplete() string {
	ac := m.autocomplete
	if !ac.active || len(ac.items) == 0 {
		return ""
	}
	rows := make([]popupRow, len(ac.items))
	for i, it := range ac.items {
		rows[i] = it.cells
	}
	spec := popupSpec{
		title:    autocompleteTitle(ac.kind),
		rows:     rows,
		selected: ac.selected,
		hint:     autocompleteHint,
		maxRows:  maxAutocompleteItems,
	}
	return renderPopup(m.th, spec, m.width)
}
