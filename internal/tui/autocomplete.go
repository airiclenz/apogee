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
// A suggestion popup that opens while the human types: "/" lists the known commands (at idle,
// where a command can actually run), and an "@" token lists workspace files (wherever the box is
// editable, so an interjection references a file as easily as a submitted message does — see
// computeAutocomplete). It is painted by the shared selector-popup
// module (popup.go) — a titled, bordered pane rendered above the input box, in a slot that
// shrinks the transcript viewport to make room. The overlay completes the WORD AT THE END of the
// input (the common forward-typing case), which keeps it cursor-position-free and robust.

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

// acItem is one suggestion: value is the text spliced in (the command name or file path,
// without the "/"/"@" sigil), label is what the row displays.
type acItem struct {
	value string
	label string
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
// so the file region is offered wherever the box is editable. The "/command" region is idle-only
// because a command typed mid-run is REFUSED rather than queued (it earns a note instead), and
// offering it while running would be the overlay lying about what ⏎ does. The "/skill" picker
// stays idle-only alongside it for now — a hold on the menu, not a limit of what it splices: the
// inline token it writes is message content, and rides an interjection exactly as an @ref does.
func (m Model) computeAutocomplete() autocompleteState {
	value := m.input.Value()
	idle := m.state == stateIdle

	// Skill argument: a "/skill <partial>" region (the trailing word after a "/skill" token).
	// Checked FIRST so it wins over the bare-command branch — which would otherwise see "/skill"
	// the moment a space is typed. tokenStart marks the "/skill" itself, so accepting replaces the
	// whole "/skill <partial>" run with the skill's own "/id " token.
	if start, partial, ok := skillArgToken(value); ok && idle {
		items := m.skillSuggestions(partial)
		if len(items) == 0 {
			return autocompleteState{}
		}
		return autocompleteState{active: true, kind: acSkill, items: items, tokenStart: start}
	}

	// Command: the whole line is "/<partial>" with no whitespace yet.
	if idle && strings.HasPrefix(value, "/") && !strings.ContainsAny(value, " \t\n") {
		items := commandSuggestions(strings.TrimPrefix(value, "/"))
		if len(items) == 0 {
			return autocompleteState{}
		}
		return autocompleteState{active: true, kind: acCommand, items: items, tokenStart: 0}
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
// the skill catalog the moment the /skill picker OPENS (the input entering a "/skill <partial>"
// region it was not in before). The reload swaps the shared skills.Provider that both this picker
// and the agent loop read, so a skill added since launch — or since the picker last closed —
// both shows in the dropdown and resolves when attached. It is edge-triggered on skillRegion so a
// burst of keystrokes inside one open picker re-scans disk once, not per byte (mirroring the
// filecache TTL's "reuse one walk" intent, but keyed to opens). Callers use this instead of
// assigning m.computeAutocomplete() directly; computeAutocomplete itself stays a pure function of
// the input, so unit tests that call it keep working.
func (m Model) recomputeAutocomplete() Model {
	_, _, inSkill := skillArgToken(m.input.Value())
	// The picker itself is idle-only (computeAutocomplete), so a "/skill " region typed into an
	// interjection is not one: it must neither re-scan disk nor arm the edge trigger, or the first
	// keystroke back at idle would find skillRegion already true and skip the reload.
	inSkill = inSkill && m.state == stateIdle
	if inSkill && !m.skillRegion && m.opts.ReloadSkills != nil {
		m.opts.ReloadSkills() // picker opening: re-scan before computeAutocomplete lists suggestions
	}
	m.skillRegion = inSkill
	m.autocomplete = m.computeAutocomplete()
	return m
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
// (the value stays the bare verb, so accept splices "/verb "). The dropdown offers every row,
// menuOnly ones included: accepting /skill completes to "/skill " and chains into the skill
// picker (acceptAutocomplete recomputes the overlay), never sending "/skill" as a literal message
// — attachment happens via the picker, like the apogee-code oracle's selectSkill.
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

// skillSuggestions lists skills matching partial (a case-insensitive substring of id or
// displayName), excluding those the message already invokes, as rows showing "displayName
// summary". The value is the skill ID (what the accepted row splices in as a "/id" token). A nil
// catalog yields nothing (the picker is dark).
//
// "Already invoked" is read off the BUFFER — the /tokens standing in the text right now — because
// the text is where an invocation lives; there is no attachment state beside it to consult. Delete
// the token and the skill is offered again, which is the same self-healing rule the inline accents
// and the submit parse read.
func (m Model) skillSuggestions(partial string) []acItem {
	if m.opts.Skills == nil {
		return nil
	}
	attached := map[string]bool{}
	for _, id := range extractSkillRefs(m.input.Value(), m.knownSkillID) {
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
		return true, m.acceptAutocomplete(), nil
	case "enter":
		// Enter submits when the token is already fully typed (an exact match); otherwise it
		// completes the highlighted suggestion (a second Enter then submits).
		if m.autocompleteExactMatch() {
			return false, m, nil
		}
		return true, m.acceptAutocomplete(), nil
	case "esc":
		m.autocomplete = autocompleteState{}
		return true, m, nil
	}
	return false, m, nil
}

// autocompleteExactMatch reports whether the token under completion already equals the
// highlighted suggestion verbatim (sigil included) — in which case Enter should submit rather
// than re-complete. A file token counts as typed out in any dialect the parser accepts: bare,
// double-quoted or single-quoted (command.go), so ⏎ submits whichever the user typed.
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
	selected := ac.items[ac.selected].value
	// /skill is not a real command: accepting it chains into the skill picker, so Enter must
	// complete (open the picker), never submit "/skill" as a message.
	if ac.kind == acCommand && selected == "skill" {
		return false
	}
	typed := m.input.Value()[ac.tokenStart:]
	if ac.kind == acFile {
		return typed == "@"+selected ||
			typed == `@"`+selected+`"` ||
			typed == "@'"+selected+"'"
	}
	return typed == "/"+selected
}

// acceptAutocomplete applies the highlighted suggestion, splicing it over the region that opened
// the overlay: a skill as its own inline "/id" token (insertSkillToken — the picked skill REPLACES
// the "/skill <partial>" run that summoned the picker), a command as "/" + value, and a file as its
// reference token (fileRefToken — quoted when the path has spaces, whatever form the partial was
// typed in). The quoting is decided by the PATH, never by how the user started typing: a bare "@my"
// partial completing to a spaced path still splices the quoted token, because only that one
// resolves. It never submits; the cursor lands at the end of the spliced text.
func (m Model) acceptAutocomplete() Model {
	ac := m.autocomplete
	if !ac.active || len(ac.items) == 0 {
		return m
	}
	selected := ac.items[ac.selected].value
	switch ac.kind {
	case acSkill:
		return m.insertSkillToken(selected)
	case acFile:
		return m.spliceCompletion(fileRefToken(selected))
	default:
		return m.spliceCompletion("/" + selected)
	}
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

// autocompleteTitle names the dropdown by what it completes: the popup module's title row.
func autocompleteTitle(kind acKind) string {
	switch kind {
	case acCommand:
		return "commands"
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
