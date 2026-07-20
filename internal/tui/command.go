package tui

import "strings"

// ----------------------------------------------------------------------------
// Chat input mini-language — the parse layer (TODO: apogee-code feature parity)
// ----------------------------------------------------------------------------
//
// This file is the pure, Model-free parser between the input box and the agent: it
// classifies a raw input line into a local /command or an agent message, and extracts
// @file references from a message. apogee-code's webview (media/chat.js, array Ws) is the
// behavioral oracle. Keeping it a pure function of the input string makes it unit-testable
// without standing up a Model.

// inputKind classifies a parsed input line.
type inputKind int

const (
	kindMessage inputKind = iota // free text for the agent (with @file refs extracted)
	kindCommand                  // a recognised /command handled locally or as a canned turn
)

// parsedInput is the result of classifying one raw input line. For kindCommand, command
// names the recognised verb (without the leading slash). For kindMessage, text is the line
// (trimmed, with @tokens left in place so the model sees what was referenced) and fileRefs
// holds the extracted workspace-relative paths.
type parsedInput struct {
	kind     inputKind
	command  string
	text     string
	fileRefs []string
}

// knownCommands is the recognised /command set for this slice, in display order. The parser
// intercepts a line only when its first whitespace token is exactly "/<verb>" for a verb in
// this set; any other slash-prefixed line is treated as an ordinary message (never silently
// swallowed). /new is an alias of /clear — both verbs are recognised here and route to the same
// context-reset logic in runCommand. The autocomplete overlay offers a superset: it also offers
// /skill, which attaches via the picker and is deliberately not parsed as a command (see
// commandMenu in autocomplete.go). /server is deferred (it needs a swappable provider seam) and
// so is absent here.
var knownCommands = []string{"clear", "new", "compact", "continue"}

// parseInput classifies a raw input line. A blank line yields a kindMessage with empty text
// (the caller ignores it).
func parseInput(raw string) parsedInput {
	trimmed := strings.TrimSpace(raw)
	if cmd, ok := matchCommand(trimmed); ok {
		return parsedInput{kind: kindCommand, command: cmd}
	}
	text, refs := extractFileRefs(trimmed)
	return parsedInput{kind: kindMessage, text: text, fileRefs: refs}
}

// matchCommand reports the recognised command verb when trimmed's first whitespace token is
// "/<verb>" for a known verb. Trailing arguments are ignored (the recognised commands take none).
func matchCommand(trimmed string) (string, bool) {
	if !strings.HasPrefix(trimmed, "/") {
		return "", false
	}
	first := trimmed
	if i := strings.IndexAny(trimmed, " \t"); i >= 0 {
		first = trimmed[:i]
	}
	verb := strings.TrimPrefix(first, "/")
	for _, c := range knownCommands {
		if verb == c {
			return c, true
		}
	}
	return "", false
}

// extractFileRefs scans s for @file references and returns s unchanged plus the referenced
// workspace-relative paths. An @-ref is an "@" at the start of s or immediately after
// whitespace, followed by one or more non-whitespace characters — so an email like
// foo@bar.com (where "@" follows a non-space) is not a reference. The literal @token is left
// in the text so the model sees what the human pointed at; the path (without the leading
// "@") is collected, de-duplicated in first-seen order.
func extractFileRefs(s string) (string, []string) {
	var refs []string
	seen := map[string]bool{}
	for i := 0; i < len(s); i++ {
		if s[i] != '@' {
			continue
		}
		if i > 0 && !isInputSpace(s[i-1]) { // not at a word boundary ⇒ not a ref (e.g. an email)
			continue
		}
		j := i + 1
		for j < len(s) && !isInputSpace(s[j]) {
			j++
		}
		if path := s[i+1 : j]; path != "" && !seen[path] {
			seen[path] = true
			refs = append(refs, path)
		}
		i = j // resume scanning past this token
	}
	return s, refs
}

// isInputSpace reports whether b is an ASCII whitespace byte used as a token boundary.
func isInputSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
