package stubllm

import (
	"fmt"
	"regexp"
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
