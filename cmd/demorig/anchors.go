package main

import (
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/session"
)

// located is the transcript entry a selector located: its index in the entry list and the
// entry itself. The index is what an ordering expect compares.
type located struct {
	Index int
	Entry session.Entry
}

// noEntry is the index of a beat whose first entry expect locates no transcript entry — or
// that has no entry expect at all.
const noEntry = -1

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

// matches reports whether one entry satisfies the selector's kind and prefix matches. A tool
// selector on an entry that carries no tool view never matches.
func (s EntrySelector) matches(entry session.Entry) bool {
	if entry.Kind != s.Kind || !strings.HasPrefix(entry.Text, s.Text) {
		return false
	}
	if s.Tool == "" && s.Target == "" {
		return true
	}
	return entry.Tool != nil &&
		strings.HasPrefix(entry.Tool.Label, s.Tool) &&
		strings.HasPrefix(entry.Tool.Target, s.Target)
}

// String spells the selector the way the storyboard writes it, for error messages.
func (s EntrySelector) String() string {
	var fields []string
	add := func(name, value string) {
		if value != "" {
			fields = append(fields, name+": "+value)
		}
	}
	add("kind", s.Kind)
	add("text", s.Text)
	add("tool", s.Tool)
	add("target", s.Target)
	switch {
	case s.Nth == NthLast:
		add("nth", "last")
	case s.Nth > 0:
		add("nth", fmt.Sprint(int(s.Nth)))
	}
	return "{" + strings.Join(fields, ", ") + "}"
}
