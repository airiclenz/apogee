package stubllm

import (
	"fmt"
	"regexp"
	"strings"
)

// matcher decides which Turn answers a request and remembers which Turns are spent.
//
// The rule, in one sentence: a request takes the first unconsumed Turn whose `when:` matches
// it, and failing that the next unconsumed Turn without a `when:` at all. Ordered turns are
// therefore the script's spine — request 1 takes turn 0, request 2 takes turn 1 — while a
// `when:` turn is an interrupt that jumps the queue for the requests it recognises, which is
// what lets a fixture answer a sub-agent's question without knowing where in the run it lands.
// A Repeat turn is never consumed, so it stays available for every later request.
type matcher struct {
	turns    []Turn
	patterns []*regexp.Regexp // per turn: the compiled When.LastMessage, nil when it has none
	consumed []bool
}

// newMatcher compiles a Script's matchers. It fails on a regexp that does not compile, so a
// broken fixture is reported at construction rather than as an unmatched request later.
func newMatcher(s Script) (*matcher, error) {
	m := &matcher{
		turns:    s.Turns,
		patterns: make([]*regexp.Regexp, len(s.Turns)),
		consumed: make([]bool, len(s.Turns)),
	}
	for i := range s.Turns {
		when := s.Turns[i].When
		if when == nil || when.LastMessage == "" {
			continue
		}
		pattern, err := regexp.Compile(when.LastMessage)
		if err != nil {
			return nil, fmt.Errorf("stubllm: turn %d: when.last_message: %w", i, err)
		}
		m.patterns[i] = pattern
	}
	return m, nil
}

// next returns the index of the Turn answering r and marks it consumed, or -1 when the script
// has nothing left for it. It is not safe for concurrent use; the Server holds its lock.
func (m *matcher) next(r Request) int {
	for i := range m.turns {
		if m.turns[i].When == nil || m.consumed[i] || !m.matches(i, r) {
			continue
		}
		m.take(i)
		return i
	}
	for i := range m.turns {
		if m.turns[i].When != nil || m.consumed[i] {
			continue
		}
		m.take(i)
		return i
	}
	return -1
}

// take spends a Turn, unless it repeats.
func (m *matcher) take(i int) {
	if !m.turns[i].Repeat {
		m.consumed[i] = true
	}
}

// release makes a Turn available again after it was taken but could not be played — the case
// of a turn whose captures found nothing in the request. A request the script cannot answer
// must not spend a turn, or the failure would shift every later turn by one and bury its own
// cause under a cascade of mismatches.
func (m *matcher) release(i int) {
	m.consumed[i] = false
}

// matches reports whether turn i's When selects r. Both members, when set, must match.
func (m *matcher) matches(i int, r Request) bool {
	when := m.turns[i].When
	if m.patterns[i] != nil && !m.patterns[i].MatchString(lastText(r.Messages)) {
		return false
	}
	if when.ToolResult != "" && lastToolResultName(r.Messages) != when.ToolResult {
		return false
	}
	return true
}

// unserved returns the indexes of the non-repeat Turns no request ever took, in script order.
func (m *matcher) unserved() []int {
	var out []int
	for i := range m.turns {
		if !m.turns[i].Repeat && !m.consumed[i] {
			out = append(out, i)
		}
	}
	return out
}

// expand renders the Turn that answers r: every `{{name}}` in the text and in the tool calls'
// arguments replaced by what this Turn's captures lift out of the request. A Turn without
// captures is its own answer. Nothing is written back — the Script is immutable, because a
// repeating turn answers many requests and each must expand against its own.
//
// A capture that finds nothing is an error rather than an empty substitution: a fixture that
// quietly rendered `mkdir -p /tmp` from `mkdir -p {{scratch}}/tmp` would fail somewhere far
// from the missing announcement that actually caused it.
func (t Turn) expand(r Request) (Turn, error) {
	if len(t.Captures) == 0 {
		return t, nil
	}

	pairs := make([]string, 0, len(t.Captures)*2)
	for i := range t.Captures {
		value, err := t.Captures[i].value(r)
		if err != nil {
			return Turn{}, err
		}
		pairs = append(pairs, placeholder(t.Captures[i].Name), value)
	}
	replacer := strings.NewReplacer(pairs...)

	expanded := t
	expanded.Text = replacer.Replace(t.Text)
	if len(t.ToolCalls) > 0 {
		expanded.ToolCalls = make([]ToolCall, len(t.ToolCalls))
		copy(expanded.ToolCalls, t.ToolCalls)
		for i := range expanded.ToolCalls {
			expanded.ToolCalls[i].Arguments = replacer.Replace(expanded.ToolCalls[i].Arguments)
		}
	}
	return expanded, nil
}

// value is what this Capture lifts out of request r — group 1 of its pattern's first match over
// the text From names.
func (c Capture) value(r Request) (string, error) {
	pattern, err := regexp.Compile(c.Pattern)
	if err != nil {
		return "", fmt.Errorf("capture %s: pattern is not a regexp: %w", c.Name, err)
	}
	match := pattern.FindStringSubmatch(c.source(r))
	if match == nil {
		return "", fmt.Errorf("capture %s unmatched", c.Name)
	}
	return match[1], nil
}

// source is the request text this Capture reads.
func (c Capture) source(r Request) string {
	if c.From == captureFromSystem {
		return systemText(r.Messages)
	}
	return lastText(r.Messages)
}
