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
// skills in ONE merged menu (at idle, where a command can actually run), and an "@" token lists
// workspace files (wherever the box is editable, so an interjection references a file as easily as
// a submitted message does — see computeAutocomplete). It is painted by the shared selector-popup
// module (popup.go) — a titled, bordered pane rendered above the input box, in a slot that
// shrinks the transcript viewport to make room. The overlay completes the WORD AT THE END of the
// input (the common forward-typing case), which keeps it cursor-position-free and robust.
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
// file path, without the "/"/"@" sigil), label is what the row displays, and skill marks a row of
// the merged "/" menu that names a SKILL rather than a command. The mark is not decoration: the two
// kinds of row do different things at accept (a skill writes its token, a command RUNS), so the row
// has to carry which it is.
type acItem struct {
	value string
	label string
	skill bool
}

// autocompleteState is the overlay's data. active gates rendering and key capture (it is a
// value field on the Model, so an inactive zero value simply means "hidden"). tokenStart is
// the byte offset in the input value where the token being completed begins; accept splices
// from there to the end.
type autocompleteState struct {
	active     bool
	kind       acKind
	items      []acItem
	selected   int
	tokenStart int
}

// computeAutocomplete derives the overlay from the current input value, treating the cursor
// as at the end (the common case while typing). It returns an inactive state when nothing
// should be suggested. Called at stateIdle and — for the file region only — at stateRunning.
//
// The three regions do NOT share a lifetime. An "@file" reference is exactly as useful in a
// message interjected mid-run as in one submitted at idle (the ref resolves at delivery, fresh),
// so the file region is offered wherever the box is editable. The "/" region is idle-only
// because a command typed mid-run is REFUSED rather than queued (it earns a note instead), and
// offering it while running would be the overlay lying about what ⏎ does. The "/skill" picker
// stays idle-only alongside it for now — a hold on the menu, not a limit of what it splices: the
// inline token it writes is message content, and rides an interjection exactly as an @ref does.
//
// Each region is scoped to a TOKEN, never to the whole line: the "/" menu opens on the trailing
// "/word" of a draft that already holds text, which is what lets a command be summoned — or a skill
// invoked — without first emptying the box.
func (m Model) computeAutocomplete() autocompleteState {
	value := m.input.Value()
	idle := m.state == stateIdle

	// Skill argument: a "/skill <partial>" region (the trailing word after a "/skill" token).
	// Checked FIRST so it wins over the merged "/" branch — which would otherwise see the partial as
	// a "/" token of its own. tokenStart marks the "/skill" itself, so accepting replaces the
	// whole "/skill <partial>" run with the skill's own "/id " token.
	if start, partial, ok := skillArgToken(value); ok && idle {
		items := m.skillSuggestions(partial, value[:start])
		if len(items) == 0 {
			return autocompleteState{}
		}
		return autocompleteState{active: true, kind: acSkill, items: items, tokenStart: start}
	}

	// Command + skill: the trailing word is a "/" token — one namespace, one menu.
	if start, partial, ok := trailingSlashToken(value); ok && idle {
		items := m.slashSuggestions(partial, value[:start])
		if len(items) == 0 {
			return autocompleteState{}
		}
		return autocompleteState{active: true, kind: acCommand, items: items, tokenStart: start}
	}

	// File: the input ends in an "@" token being typed — bare, or quoted across its spaces.
	if start, partial, ok := trailingFileToken(value); ok {
		items := m.fileSuggestions(partial)
		if len(items) == 0 {
			return autocompleteState{}
		}
		return autocompleteState{active: true, kind: acFile, items: items, tokenStart: start}
	}

	return autocompleteState{}
}

// recomputeAutocomplete re-derives the overlay from the current input and stores it, reloading
// the skill catalog the moment a catalog-listing region OPENS — the input entering a "/skill
// <partial>" picker, or the merged "/" menu, that it was not in before. The reload swaps the shared
// skills.Provider that both those rows and the agent loop read, so a skill added since launch — or
// since the menu last closed — both shows in the dropdown and resolves when invoked. It is
// edge-triggered on skillRegion so a burst of keystrokes inside one open region re-scans disk once,
// not per byte (mirroring the filecache TTL's "reuse one walk" intent, but keyed to opens), and the
// two regions share the flag because typing "/skill" and then a space walks straight from one into
// the other — a single visit to the catalog, not two. Callers use this instead of assigning
// m.computeAutocomplete() directly; computeAutocomplete itself stays a pure function of the input,
// so unit tests that call it keep working.
func (m Model) recomputeAutocomplete() Model {
	value := m.input.Value()
	_, _, inPicker := skillArgToken(value)
	_, _, inMenu := trailingSlashToken(value)
	// Both regions are idle-only (computeAutocomplete), so a "/" token typed into an interjection is
	// not one: it must neither re-scan disk nor arm the edge trigger, or the first keystroke back at
	// idle would find skillRegion already true and skip the reload.
	inSkill := (inPicker || inMenu) && m.state == stateIdle
	if inSkill && !m.skillRegion && m.opts.ReloadSkills != nil {
		m.opts.ReloadSkills() // region opening: re-scan before computeAutocomplete lists suggestions
	}
	m.skillRegion = inSkill
	m.autocomplete = m.computeAutocomplete()
	return m
}

// trailingSlashToken reports the "/" token at the very end of value (the token being typed): its
// start offset, the partial verb-or-id after the slash, and whether value ends in such a token. It
// is trailingFileToken's bare rule under a different sigil, and it is what turns the "/" menu from
// a whole-LINE rule into a TOKEN rule: the menu now opens at the end of a draft that already holds
// text, instead of only on an otherwise-empty box. A value ending in whitespace has no trailing
// token (the word is finished).
//
// There is no quoted shape to read: command verbs and skill ids are whitespace-free by construction
// (skill ids are directory names — extractSkillRefs), so the bare word is the whole grammar.
func trailingSlashToken(value string) (int, string, bool) {
	start := strings.LastIndexAny(value, " \t\n") + 1
	word := value[start:]
	if !strings.HasPrefix(word, "/") {
		return 0, "", false
	}
	return start, word[1:], true
}

// trailingFileToken reports the "@" token at the very end of value (the token being typed):
// its start offset, the partial path after the "@" (and after any opening quote), and whether
// value ends in such a token. The token must sit at a word boundary (start of value or after
// whitespace); a value ending in whitespace has no trailing token (the ref is complete).
//
// It reads both shapes of the ref grammar (scanRefToken owns it, command.go):
//
//   - bare — the trailing whitespace-delimited word, "@internal/loop.go";
//   - quoted — a word-boundary "@" followed by a quote whose token reaches the very end of
//     value. An open quote keeps the overlay alive across the spaces the bare rule would
//     tokenize on (@"my pl → partial "my pl"), and a closing quote flush at the end yields the
//     inner path (@"my plan.md" → partial "my plan.md"), so a fully-typed quoted token can
//     still match its suggestion exactly and let ⏎ submit.
//
// The quoted shape is tried first: its own closing quote and interior spaces are precisely what
// the bare rule would mis-read. Bare tokens keep their previous behaviour byte for byte.
func trailingFileToken(value string) (int, string, bool) {
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
		partial, end := scanRefToken(value, i+1)
		if end == len(value) {
			return i, partial, true
		}
		i = end - 1 // a closed quote mid-line: resume scanning past this token
	}
	start := strings.LastIndexAny(value, " \t\n") + 1
	word := value[start:]
	if !strings.HasPrefix(word, "@") {
		return 0, "", false
	}
	return start, word[1:], true
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

// commandSuggestions returns the verbs of commandSpecs (command.go — the one registry the parser
// reads too) whose name has partial as a prefix, in table order, labeling each "/verb  summary"
// (the value stays the bare verb). It is the command half of the merged "/" menu
// (slashSuggestions). Every row is offered, menuOnly ones included: accepting /skill completes to
// "/skill " and chains into the skill picker (acceptAutocomplete recomputes the overlay), never
// sending "/skill" as a literal message — like the apogee-code oracle's selectSkill.
func commandSuggestions(partial string) []acItem {
	var items []acItem
	for _, c := range commandSpecs {
		if strings.HasPrefix(c.name, partial) {
			label := "/" + c.name
			if c.summary != "" {
				label += "  " + c.summary
			}
			items = append(items, acItem{value: c.name, label: label})
		}
	}
	return items
}

// skillArgToken reports the "/skill <partial>" region at the end of value: the byte offset of
// the "/skill" token (the point the picked skill's own token is spliced over), the partial
// id/name being typed, and whether value ends in such a region. The partial is the trailing
// whitespace-delimited word, and the word immediately before it must be exactly "/skill". It
// accepts "/skill ", "/skill cl", and mid-line "fix /skill cl"; it rejects a bare "/skill" (no arg
// yet) and a completed "/skill foo " (the word before the trailing position is "foo", not "/skill").
func skillArgToken(value string) (int, string, bool) {
	lastSpace := strings.LastIndexAny(value, " \t\n")
	if lastSpace < 0 {
		return 0, "", false // no whitespace ⇒ a bare "/skill" or a single word, no arg region
	}
	partial := value[lastSpace+1:]
	before := value[:lastSpace]
	prevSpace := strings.LastIndexAny(before, " \t\n")
	if before[prevSpace+1:] != "/skill" {
		return 0, "", false
	}
	return prevSpace + 1, partial, true
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
func (m Model) slashSuggestions(partial, outside string) []acItem {
	items := commandSuggestions(partial)
	for _, sk := range m.skillSuggestions(partial, outside) {
		if _, shadowed := commandByName(sk.value); shadowed {
			continue
		}
		label := glyphSkill + " /" + sk.value
		if sk.label != "" {
			label += "  " + sk.label
		}
		items = append(items, acItem{value: sk.value, label: label, skill: true})
	}
	return items
}

// skillSuggestions lists skills matching partial (a case-insensitive substring of id or
// displayName), excluding those the message already invokes, as rows showing "displayName
// summary". The value is the skill ID (what the accepted row splices in as a "/id" token). A nil
// catalog yields nothing (the picker is dark).
//
// "Already invoked" is read off the BUFFER — the /tokens standing in the text right now — because
// the text is where an invocation lives; there is no attachment state beside it to consult. Delete
// the token and the skill is offered again, which is the same self-healing rule the inline accents
// and the submit parse read. outside is the part of the buffer the completion region does NOT
// cover: the region itself is about to be replaced, so a skill named inside it is not yet invoked
// (a fully typed "/clean-code" must keep offering its own row, or ⏎ could never confirm it).
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
		label := sk.DisplayName
		if sk.Summary != "" {
			label += "  " + sk.Summary
		}
		items = append(items, acItem{value: sk.ID, label: label})
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
// here. The item's value stays the raw path; only the label carries the sigil and quotes.
func (m Model) fileSuggestions(partial string) []acItem {
	paths := m.files.suggest(m.opts.Workspace, partial, maxAutocompleteItems, time.Now())
	items := make([]acItem, 0, len(paths))
	for _, p := range paths {
		items = append(items, acItem{value: p, label: fileRefToken(p)})
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
	if !ac.active || len(ac.items) == 0 || ac.tokenStart > len(m.input.Value()) {
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
	typed := m.input.Value()[ac.tokenStart:]
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
	return strings.TrimSpace(m.input.Value()) == typed
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
//   - every other command RUNS: its token is cut out of the draft (removeCompletionToken) and
//     runCommand drives it, so invoking a command from the middle of a half-written message costs
//     the message nothing.
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
	return m.removeCompletionToken().runCommand(parsedInput{kind: kindCommand, command: it.value})
}

// removeCompletionToken cuts the token the overlay was completing out of the draft and closes the
// overlay. It is the editor half of "a command row is an action": the verb is consumed by RUNNING
// it, so what stays in the box is the message the human was writing, minus the word that invoked
// the command — never an emptied box (which would lose the draft) and never a dead "/clear" left
// standing in the text (which would be sent along with it).
//
// The region runs from tokenStart to the end of the value — the trailing-token rule the menu opens
// under — so the cut leaves a prefix and the caret lands at its end. The separator the human typed
// before the token stays: it is where they were writing, and it is what the next word needs anyway.
func (m Model) removeCompletionToken() Model {
	value := m.input.Value()
	start := min(m.autocomplete.tokenStart, len(value)) // defensive: the value cannot have shrunk
	m.input.SetValue(value[:start])
	m.input.MoveToEnd()
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

// spliceCompletion writes token, plus the trailing space that ends it, over the completion region
// and re-derives the overlay. It RECOMPUTES rather than blindly closing: that closes the overlay
// for a completed command/file/skill token (the trailing space ends the token) but reopens it as
// the skill picker after "/skill " — the chain the oracle's selectSkill mirrors.
func (m Model) spliceCompletion(token string) Model {
	value := m.input.Value()
	start := m.autocomplete.tokenStart
	if start > len(value) {
		start = len(value) // defensive: the value cannot have shrunk, but never slice out of range
	}
	m.input.SetValue(value[:start] + token + " ")
	m.input.MoveToEnd()
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
// flush with the input box below) holding the suggestion rows and a key legend,
// the selected row highlighted. The kind picks the title ("commands"/"files"/"skills"); row
// composition (the acItem labels, verbatim) stays caller-side while the module owns the marker,
// highlight, truncation, and scroll windowing. It returns "" when the overlay is inactive, so
// View treats it like the approval-prompt slot.
func (m Model) renderAutocomplete() string {
	ac := m.autocomplete
	if !ac.active || len(ac.items) == 0 {
		return ""
	}
	rows := make([]string, len(ac.items))
	for i, it := range ac.items {
		rows[i] = it.label
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

// truncateLabel clips s to at most width display runes, ending in an ellipsis when it had to
// cut — so a long file path never overflows the terminal and breaks the overlay's layout.
func truncateLabel(s string, width int) string {
	if width <= 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "…"
}
