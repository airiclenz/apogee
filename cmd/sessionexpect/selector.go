package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/session"
)

// EntrySelector picks one transcript entry out of a saved session: Kind is an entry kind as the
// session JSON spells it, Text, Tool and Target are prefix matches on the entry's text, tool
// label and tool target, and Nth picks among the matches. A storyboard's `run:` expect passes it
// on the command line to name the entry it judges.
type EntrySelector struct {
	Kind   string
	Text   string
	Tool   string
	Target string
	Nth    Nth
}

// Nth picks which of an entry selector's matches counts: the zero value is the first match, a
// positive value the Nth, and NthLast the last one.
type Nth int

// NthLast is the `last` spelling of Nth.
const NthLast Nth = -1

// sessionKinds are the entry kinds a selector may name, as the session JSON spells them.
var sessionKinds = []string{
	session.EntryKindUser,
	session.EntryKindAssistant,
	session.EntryKindToolCall,
	session.EntryKindToolResult,
	session.EntryKindError,
	session.EntryKindNote,
	session.EntryKindPresented,
	session.EntryKindInterjected,
	session.EntryKindSchedule,
	session.EntryKindCompacted,
	session.EntryKindWorkflow,
	session.EntryKindWorkflowItem,
}

// The selector's keys, as a command line spells them.
const (
	keyKind   = "kind"
	keyText   = "text"
	keyTool   = "tool"
	keyTarget = "target"
	keyNth    = "nth"
)

// errNoKind is the refusal of a selector that names no entry kind.
var errNoKind = errors.New("want kind=<entry kind>")

// parseSelector reads a selector spelled as comma-separated key=value pairs —
// `kind=toolCall,tool=Replace,target=task.go,nth=last`. Kind is required and must be a session
// entry kind; text, tool and target are prefix matches; nth is a positive integer or `last`.
// Whitespace around a pair is dropped. A value cannot carry a comma — a prefix stops short of
// one. An unknown key, a repeated key, a pair without `=` and an empty value are refused.
func parseSelector(spelled string) (EntrySelector, error) {
	var selector EntrySelector
	seen := make(map[string]bool)
	for _, pair := range strings.Split(spelled, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(pair), "=")
		if !found {
			return EntrySelector{}, fmt.Errorf("%q: want key=value", pair)
		}
		if seen[key] {
			return EntrySelector{}, fmt.Errorf("%s: given twice", key)
		}
		seen[key] = true
		if value == "" {
			return EntrySelector{}, fmt.Errorf("%s: want a value", key)
		}
		if err := selector.set(key, value); err != nil {
			return EntrySelector{}, err
		}
	}
	if selector.Kind == "" {
		return EntrySelector{}, errNoKind
	}
	return selector, nil
}

// set assigns one parsed key=value pair to the selector.
func (s *EntrySelector) set(key, value string) error {
	switch key {
	case keyKind:
		if !slices.Contains(sessionKinds, value) {
			return fmt.Errorf("kind: unknown entry kind %q", value)
		}
		s.Kind = value
	case keyText:
		s.Text = value
	case keyTool:
		s.Tool = value
	case keyTarget:
		s.Target = value
	case keyNth:
		nth, err := parseNth(value)
		if err != nil {
			return err
		}
		s.Nth = nth
	default:
		return fmt.Errorf("unknown key %q (want kind, text, tool, target or nth)", key)
	}
	return nil
}

// parseNth accepts a positive integer or the string `last`.
func parseNth(value string) (Nth, error) {
	if value == "last" {
		return NthLast, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("nth: want a positive integer or last, got %q", value)
	}
	return Nth(parsed), nil
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

// String spells the selector as a YAML flow mapping, for a detail line.
func (s EntrySelector) String() string {
	var fields []string
	add := func(name, value string) {
		if value != "" {
			fields = append(fields, name+": "+value)
		}
	}
	add(keyKind, s.Kind)
	add(keyText, s.Text)
	add(keyTool, s.Tool)
	add(keyTarget, s.Target)
	switch {
	case s.Nth == NthLast:
		add(keyNth, "last")
	case s.Nth > 0:
		add(keyNth, fmt.Sprint(int(s.Nth)))
	}
	return "{" + strings.Join(fields, ", ") + "}"
}
