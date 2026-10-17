package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/session"
)

// Expect is one judgement on a saved session: Entry selects the entry judged, Contains is a
// substring of its text, tool label, tool stat or tool summary, and Before and After order it
// against the entry another selector locates. Every clause it sets must hold.
type Expect struct {
	Entry    EntrySelector
	Contains string
	Before   *EntrySelector
	After    *EntrySelector
}

// located is the transcript entry a selector located: its index in the entry list and the
// entry itself. The index is what an ordering clause compares.
type located struct {
	Index int
	Entry session.Entry
}

// loadEntries reads a saved session record by path and decodes its transcript into the
// committed entries, in list order. The shapes are internal/session's own — an expect is matched
// against exactly what apogee wrote, never a redeclared mirror. A record with no transcript is
// an error: a session that recorded no scrollback has nothing to judge.
func loadEntries(path string) ([]session.Entry, error) {
	record, err := session.NewStore(filepath.Dir(path)).LoadPath(path)
	if err != nil {
		return nil, err
	}
	entries, err := session.DecodeTranscript(record.Transcript)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s: the session recorded no transcript entries", path)
	}
	return entries, nil
}

// findEntry resolves an entry selector over the entries in list order: the kind must match and
// each of text, tool and target that the selector sets is a prefix match on the entry's text,
// tool label and tool target. Nth picks among the matches — the first by default, the Nth, or
// the last. A selector that locates nothing is an error spelling the selector.
func findEntry(entries []session.Entry, selector EntrySelector) (located, error) {
	var matches []located
	for index, entry := range entries {
		if selector.matches(entry) {
			matches = append(matches, located{Index: index, Entry: entry})
		}
	}
	switch {
	case len(matches) == 0:
		return located{}, fmt.Errorf("no entry matches %s", selector)
	case selector.Nth == NthLast:
		return matches[len(matches)-1], nil
	case int(selector.Nth) > len(matches):
		return located{}, fmt.Errorf("%s: only %d match(es), nth %d wanted", selector, len(matches), selector.Nth)
	case selector.Nth > 0:
		return matches[selector.Nth-1], nil
	default:
		return matches[0], nil
	}
}

// judge evaluates the expect's clauses — contains, before, after — on the entry its selector
// locates; every clause must hold. The detail spells the entry and the first clause that
// failed, or every clause that passed; a selector that locates nothing fails with findEntry's
// error.
func (e Expect) judge(entries []session.Entry) (bool, string) {
	judged, err := findEntry(entries, e.Entry)
	if err != nil {
		return false, err.Error()
	}
	described := describeEntry(judged)
	var passed []string
	for _, clause := range e.clauses(entries) {
		detail, ok := clause(judged)
		if !ok {
			return false, described + ": " + detail
		}
		passed = append(passed, detail)
	}
	if len(passed) == 0 {
		return true, described
	}
	return true, described + ": " + strings.Join(passed, ", ")
}

// clause judges one assertion of an expect on the judged entry, returning the detail to print
// and whether it held.
type clause func(judged located) (string, bool)

// clauses lists the assertions the expect sets, in the order demorig's schema named them.
func (e Expect) clauses(entries []session.Entry) []clause {
	var clauses []clause
	if e.Contains != "" {
		clauses = append(clauses, containsClause(e.Contains))
	}
	if e.Before != nil {
		clauses = append(clauses, orderClause("before", *e.Before, entries, func(judged, other int) bool { return judged < other }))
	}
	if e.After != nil {
		clauses = append(clauses, orderClause("after", *e.After, entries, func(judged, other int) bool { return judged > other }))
	}
	return clauses
}

// containsClause holds when the entry's text, tool label, tool stat or tool summary contains the
// substring.
func containsClause(want string) clause {
	return func(judged located) (string, bool) {
		if entryContains(judged.Entry, want) {
			return fmt.Sprintf("contains %q", want), true
		}
		return fmt.Sprintf("does not contain %q", want), false
	}
}

// orderClause holds when the judged entry's list position relates to the entry the other
// selector locates as holds says. An other selector that locates nothing cannot be ordered
// against, and the detail carries findEntry's reason.
func orderClause(name string, other EntrySelector, entries []session.Entry, holds func(judged, other int) bool) clause {
	return func(judged located) (string, bool) {
		found, err := findEntry(entries, other)
		if err != nil {
			return fmt.Sprintf("%s: %v", name, err), false
		}
		if holds(judged.Index, found.Index) {
			return fmt.Sprintf("%s %s (entry %d)", name, other, found.Index), true
		}
		return fmt.Sprintf("is not %s %s (entry %d)", name, other, found.Index), false
	}
}

// entryContains reports whether the entry's text, tool label, tool stat or tool summary carries
// the substring. The summary is read because apogee writes most tool outcomes there rather than
// in the stat — an edit's diffstat ("+1 −1") and a test run's verdict ("PASS") ride in
// tool.summary.text, and the stat stays empty for both.
func entryContains(entry session.Entry, want string) bool {
	if strings.Contains(entry.Text, want) {
		return true
	}
	if entry.Tool == nil {
		return false
	}
	for _, field := range []string{entry.Tool.Label, entry.Tool.Stat, entry.Tool.Summary.Text} {
		if strings.Contains(field, want) {
			return true
		}
	}
	return false
}

// describeEntry spells a located entry for a detail line: its index and kind, then the tool
// card's label, target and stat, or the head of its text.
func describeEntry(judged located) string {
	entry := judged.Entry
	parts := []string{fmt.Sprintf("entry %d", judged.Index), entry.Kind}
	if entry.Tool != nil {
		for _, field := range []string{entry.Tool.Label, entry.Tool.Target, entry.Tool.Stat} {
			if field != "" {
				parts = append(parts, field)
			}
		}
	} else if entry.Text != "" {
		parts = append(parts, fmt.Sprintf("%q", headOf(entry.Text)))
	}
	return strings.Join(parts, " ")
}

// textHead is how much of an entry's text a detail line shows.
const textHead = 40

// headOf returns the text's first line, cut to textHead runes with an ellipsis.
func headOf(text string) string {
	if line, _, found := strings.Cut(text, "\n"); found {
		text = line + "…"
	}
	if runes := []rune(text); len(runes) > textHead {
		return string(runes[:textHead]) + "…"
	}
	return text
}
