package tui

import (
	"io/fs"
	"os"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
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

// commandSuggestions returns the known commands whose verb has partial as a prefix.
func commandSuggestions(partial string) []acItem {
	var items []acItem
	for _, c := range knownCommands {
		if strings.HasPrefix(c, partial) {
			items = append(items, acItem{value: c, label: "/" + c})
		}
	}
	return items
}

// fileSuggestions lists workspace files matching the typed partial as "@path" rows.
func (m Model) fileSuggestions(partial string) []acItem {
	paths := workspaceFiles(m.opts.Workspace, partial, maxAutocompleteItems)
	items := make([]acItem, 0, len(paths))
	for _, p := range paths {
		items = append(items, acItem{value: p, label: "@" + p})
	}
	return items
}

// workspaceFiles returns up to limit workspace-relative file paths matching partial (a
// case-insensitive substring), via a bounded walk rooted at root through os.Root — so the
// walk cannot escape the workspace or follow a symlink out of it. It skips .git and hidden
// directories/files, lists files only, and stops once limit matches are collected. An empty
// or unreadable root yields nothing.
func workspaceFiles(root, partial string, limit int) []string {
	if root == "" {
		return nil
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil
	}
	defer r.Close()

	needle := strings.ToLower(partial)
	var out []string
	_ = fs.WalkDir(r.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return nil // skip unreadable entries / the root itself, keep walking
		}
		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") { // skip .git and other dotted dirs
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil // skip hidden files
		}
		if needle == "" || strings.Contains(strings.ToLower(p), needle) {
			out = append(out, p)
			if len(out) >= limit {
				return fs.SkipAll
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
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
	sigil := "/"
	if ac.kind == acFile {
		sigil = "@"
	}
	return m.input.Value()[ac.tokenStart:] == sigil+ac.items[ac.selected].value
}

// acceptAutocomplete splices the highlighted suggestion into the input, replacing the token
// from tokenStart to the end with the sigil + value + a trailing space, then closes the
// overlay. It does not submit. The cursor lands at the end of the spliced text.
func (m Model) acceptAutocomplete() Model {
	ac := m.autocomplete
	if !ac.active || len(ac.items) == 0 {
		return m
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
	m.autocomplete = autocompleteState{}
	m.layout()
	return m
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
