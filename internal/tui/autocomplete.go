package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// Chat input mini-language — the autocomplete overlay
// ----------------------------------------------------------------------------
//
// A suggestion popup that opens while the human types at idle: "/" lists the known
// commands, and an "@" token lists workspace files. It mirrors the approval-prompt overlay
// (model.go View): a slot rendered above the input box that shrinks the transcript viewport
// to make room. The overlay completes the WORD AT THE END of the input (the common
// forward-typing case), which keeps it cursor-position-free and robust.

// maxAutocompleteItems caps how many suggestions the overlay shows (and how far the file
// walk runs) — enough to be useful, small enough that the popup never crowds the transcript
// off a short terminal and a large workspace walk stays cheap. Type more to narrow further.
const maxAutocompleteItems = 8

// acKind tags what an open overlay is completing.
type acKind int

const (
	acCommand acKind = iota // a "/command" word
	acFile                  // an "@file" reference
	acSkill                 // a "/skill <id>" argument (attaches a skill chip, not spliced as text)
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
// should be suggested. Only ever called in stateIdle.
func (m Model) computeAutocomplete() autocompleteState {
	value := m.input.Value()

	// Skill argument: a "/skill <partial>" region (the trailing word after a "/skill" token).
	// Checked FIRST so it wins over the bare-command branch — which would otherwise see "/skill"
	// the moment a space is typed. tokenStart marks the "/skill" itself, so accepting strips the
	// whole "/skill <partial>" run when the chip is popped.
	if start, partial, ok := skillArgToken(value); ok {
		items := m.skillSuggestions(partial)
		if len(items) == 0 {
			return autocompleteState{}
		}
		return autocompleteState{active: true, kind: acSkill, items: items, tokenStart: start}
	}

	// Command: the whole line is "/<partial>" with no whitespace yet.
	if strings.HasPrefix(value, "/") && !strings.ContainsAny(value, " \t\n") {
		items := commandSuggestions(strings.TrimPrefix(value, "/"))
		if len(items) == 0 {
			return autocompleteState{}
		}
		return autocompleteState{active: true, kind: acCommand, items: items, tokenStart: 0}
	}

	// File: the final whitespace-delimited word is an "@" token being typed.
	if start, partial, ok := trailingFileToken(value); ok {
		items := m.fileSuggestions(partial)
		if len(items) == 0 {
			return autocompleteState{}
		}
		return autocompleteState{active: true, kind: acFile, items: items, tokenStart: start}
	}

	return autocompleteState{}
}

// trailingFileToken reports the "@" token at the very end of value (the word being typed):
// its start offset, the partial path after "@", and whether value ends in such a token. The
// token must sit at a word boundary (start of value or after whitespace); a value ending in
// whitespace has no trailing token (the ref is complete).
func trailingFileToken(value string) (int, string, bool) {
	start := strings.LastIndexAny(value, " \t\n") + 1
	word := value[start:]
	if !strings.HasPrefix(word, "@") {
		return 0, "", false
	}
	return start, word[1:], true
}

// commandMenuItem is one entry the "/" dropdown offers: the verb and a one-line summary.
type commandMenuItem struct {
	name    string
	summary string
}

// commandMenu is the set the "/" dropdown offers, with summaries. It is a SUPERSET of the
// parser's knownCommands: it also offers /skill, which the parser deliberately does NOT treat
// as a command (matchCommand leaves it alone). Accepting /skill completes to "/skill " and
// chains into the skill picker (acceptAutocomplete recomputes the overlay); it is never sent as
// a literal message — attachment happens via the picker, like the apogee-code oracle's
// selectSkill. Keeping it out of knownCommands is what keeps an unknown "/skill foo" a message.
var commandMenu = []commandMenuItem{
	{name: "clear", summary: "reset the model's memory of this session"},
	{name: "compact", summary: "summarise the conversation to reclaim context"},
	{name: "continue", summary: "ask the model to keep going"},
	{name: "skill", summary: "attach a skill to your next message"},
}

// commandSuggestions returns the menu commands whose verb has partial as a prefix, labeling
// each "/verb  summary" (the value stays the bare verb, so accept splices "/verb ").
func commandSuggestions(partial string) []acItem {
	var items []acItem
	for _, c := range commandMenu {
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
// the "/skill" token (the strip point when a chip is popped), the partial id/name being typed,
// and whether value ends in such a region. The partial is the trailing whitespace-delimited
// word, and the word immediately before it must be exactly "/skill". It accepts "/skill ",
// "/skill cl", and mid-line "fix /skill cl"; it rejects a bare "/skill" (no arg yet) and a
// completed "/skill foo " (the word before the trailing position is "foo", not "/skill").
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
// displayName), excluding those already attached, as rows showing "displayName  summary". The
// value is the skill ID (what gets attached). A nil catalog yields nothing (the picker is dark).
func (m Model) skillSuggestions(partial string) []acItem {
	if m.opts.Skills == nil {
		return nil
	}
	attached := make(map[string]bool, len(m.pendingSkills))
	for _, id := range m.pendingSkills {
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

// fileSuggestions lists workspace files matching the typed partial as "@path" rows, served
// through the Model's file cache so a typing burst reuses one workspace walk (filecache.go).
// newModel always installs the cache, so m.files is never nil here.
func (m Model) fileSuggestions(partial string) []acItem {
	paths := m.files.suggest(m.opts.Workspace, partial, maxAutocompleteItems, time.Now())
	items := make([]acItem, 0, len(paths))
	for _, p := range paths {
		items = append(items, acItem{value: p, label: "@" + p})
	}
	return items
}

// autocompleteKey handles a keypress while the overlay is open (idle only). It reports
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
// than re-complete.
func (m Model) autocompleteExactMatch() bool {
	ac := m.autocomplete
	if !ac.active || len(ac.items) == 0 || ac.tokenStart > len(m.input.Value()) {
		return false
	}
	// A skill is attached via accept (it pops a chip), never submitted literally — so Enter
	// always completes it, regardless of how exactly the typed text matches.
	if ac.kind == acSkill {
		return false
	}
	selected := ac.items[ac.selected].value
	// /skill is not a real command: accepting it chains into the skill picker, so Enter must
	// complete (open the picker), never submit "/skill" as a message.
	if ac.kind == acCommand && selected == "skill" {
		return false
	}
	sigil := "/"
	if ac.kind == acFile {
		sigil = "@"
	}
	return m.input.Value()[ac.tokenStart:] == sigil+selected
}

// acceptAutocomplete applies the highlighted suggestion. A skill is attached (a chip is popped
// and its "/skill <partial>" text stripped — attachSkill); a command/file is spliced in as
// sigil + value + a trailing space. After a splice it RECOMPUTES the overlay rather than
// blindly closing it: that closes the overlay for a completed command/file (the trailing space
// ends the token) but reopens it as the skill picker after "/skill " — the chain the oracle's
// selectSkill mirrors. It never submits; the cursor lands at the end of the spliced text.
func (m Model) acceptAutocomplete() Model {
	ac := m.autocomplete
	if !ac.active || len(ac.items) == 0 {
		return m
	}
	if ac.kind == acSkill {
		return m.attachSkill(ac.items[ac.selected].value)
	}
	value := m.input.Value()
	start := ac.tokenStart
	if start > len(value) {
		start = len(value) // defensive: the value cannot have shrunk, but never slice out of range
	}
	sigil := "/"
	if ac.kind == acFile {
		sigil = "@"
	}
	m.input.SetValue(value[:start] + sigil + ac.items[ac.selected].value + " ")
	m.input.MoveToEnd()
	m.autocomplete = m.computeAutocomplete() // chains "/skill " → picker; else closes (trailing space)
	m.layout()
	return m
}

// attachSkill pops the skill onto the pending chip row (deduped) and strips the "/skill
// <partial>" text that triggered it (from tokenStart to the end), then recomputes the overlay
// (which closes, the stripped text no longer being a skill region). The chip is what carries
// the attachment to submit; the input is freed for the message itself.
func (m Model) attachSkill(id string) Model {
	if !containsString(m.pendingSkills, id) {
		m.pendingSkills = append(m.pendingSkills, id)
	}
	value := m.input.Value()
	start := m.autocomplete.tokenStart
	if start > len(value) {
		start = len(value)
	}
	m.input.SetValue(value[:start])
	m.input.MoveToEnd()
	m.autocomplete = m.computeAutocomplete()
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

// renderAutocomplete draws the suggestion popup shown above the input box. The selected row
// gets the user-block highlight (white on dark gray); the rest are faint. It returns "" when
// the overlay is inactive, so View can treat it like the approval-prompt slot.
func (m Model) renderAutocomplete() string {
	ac := m.autocomplete
	if !ac.active || len(ac.items) == 0 {
		return ""
	}
	rows := make([]string, len(ac.items))
	for i, it := range ac.items {
		marker := "  "
		style := m.th.statusFaint
		if i == ac.selected {
			marker = "❯ "
			style = m.th.userBlock
		}
		rows[i] = style.Render(truncateLabel(marker+it.label, m.width))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// renderSkillChips draws the attached-skill badges shown just above the input box, one chip per
// pending skill (its display name, resolved through the catalog; the raw ID if unresolved). It
// returns "" when nothing is attached, so View can treat it like the autocomplete slot. A row
// of chips that would overrun the width is clipped to one line, not wrapped — it is a status
// strip, not content.
func (m Model) renderSkillChips() string {
	if len(m.pendingSkills) == 0 {
		return ""
	}
	// One resolver (skillDisplayNames) and one chip renderer (renderSkillChip), shared with the
	// sent-block chip row — so the pending strip and the transcript chips never drift.
	names := m.skillDisplayNames(m.pendingSkills)
	chips := make([]string, 0, len(names))
	for _, name := range names {
		chips = append(chips, renderSkillChip(m.th, name))
	}
	// ANSI-aware clip: the chips carry styling, so a rune-count truncation could cut mid-escape.
	return ansi.Truncate(strings.Join(chips, " "), max(0, m.width), "…")
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
