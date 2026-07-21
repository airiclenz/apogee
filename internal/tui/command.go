package tui

import (
	"fmt"
	"strings"
)

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
// names the recognised verb (without the leading slash); confine carries the argument parse of
// a /confine line (zero value — a status report — for every other verb) and err is set when a
// recognised verb was given arguments it does not understand. An arguments error stays a
// kindCommand: the router reports the usage line rather than sending the line to the agent or
// silently doing nothing. For kindMessage, text is the line (trimmed, with @tokens left in
// place so the model sees what was referenced) and fileRefs holds the extracted
// workspace-relative paths.
type parsedInput struct {
	kind     inputKind
	command  string
	confine  confineArgs
	err      error
	text     string
	fileRefs []string
}

// knownCommands is the recognised /command set for this slice, in display order. The parser
// intercepts a line only when its first whitespace token is exactly "/<verb>" for a verb in
// this set; any other slash-prefixed line is treated as an ordinary message (never silently
// swallowed). /new is an alias of /clear — both verbs are recognised here and route to the same
// context-reset logic in runCommand. /confine is the only verb that takes arguments (parseConfine
// owns its grammar). The autocomplete overlay offers a superset: it also offers
// /skill, which attaches via the picker and is deliberately not parsed as a command (see
// commandMenu in autocomplete.go). /server is deferred (it needs a swappable provider seam) and
// so is absent here.
var knownCommands = []string{"clear", "new", "compact", "continue", "confine"}

// parseInput classifies a raw input line. A blank line yields a kindMessage with empty text
// (the caller ignores it).
func parseInput(raw string) parsedInput {
	trimmed := strings.TrimSpace(raw)
	if cmd, args, ok := matchCommand(trimmed); ok {
		parsed := parsedInput{kind: kindCommand, command: cmd}
		if cmd == "confine" {
			parsed.confine, parsed.err = parseConfine(args)
		}
		return parsed
	}
	text, refs := extractFileRefs(trimmed)
	return parsedInput{kind: kindMessage, text: text, fileRefs: refs}
}

// matchCommand reports the recognised command verb when trimmed's first whitespace token is
// "/<verb>" for a known verb, together with the remaining whitespace-separated argument tokens.
// Only /confine reads the arguments; for every other verb they are surplus and ignored (as they
// always were). The verb itself is delimited by a space or a tab, never a newline — so a
// multi-line message whose first line is "/clear" stays a message, as it did before arguments
// existed.
func matchCommand(trimmed string) (string, []string, bool) {
	if !strings.HasPrefix(trimmed, "/") {
		return "", nil, false
	}
	first, rest := trimmed, ""
	if i := strings.IndexAny(trimmed, " \t"); i >= 0 {
		first, rest = trimmed[:i], trimmed[i+1:]
	}
	verb := strings.TrimPrefix(first, "/")
	for _, c := range knownCommands {
		if verb == c {
			return c, strings.Fields(rest), true
		}
	}
	return "", nil, false
}

// ----------------------------------------------------------------------------
// /confine — the blast-radius command's argument grammar
// ----------------------------------------------------------------------------

// confineAction is the subcommand of a parsed /confine line: what the user asked the command to
// do. The zero value is confineStatus, so a bare "/confine" reports rather than changes anything.
type confineAction int

const (
	confineStatus confineAction = iota // report the backend, its capabilities, and the effective setting
	confineOff                         // run Auto unconfined — the user's explicit "I am the sandbox"
	confineOn                          // re-enable confinement
)

// String names the action as the user typed it, for error text and test output.
func (a confineAction) String() string {
	switch a {
	case confineOff:
		return "off"
	case confineOn:
		return "on"
	default:
		return "status"
	}
}

// confineArgs is the parsed argument list of a /confine line: the action asked for, and whether
// the user also asked to persist this host's acknowledgement (--save, meaningful only with off —
// "off" alone changes the running Session and writes nothing).
type confineArgs struct {
	action confineAction
	save   bool
}

// confineUsage is the one-line grammar every /confine argument error carries, so a mistyped
// subcommand teaches the syntax instead of vanishing.
const confineUsage = "usage: /confine [status] | /confine off [--save] | /confine on"

// parseConfine parses the argument tokens that followed a "/confine" verb. No arguments means
// status (report, change nothing). An unrecognised subcommand, an unrecognised argument, or a
// --save that is not persisting an "off" is an error carrying confineUsage — never a silent
// no-op, because a user who mistyped the one command that widens Auto's blast radius must not
// be left believing it took effect.
func parseConfine(args []string) (confineArgs, error) {
	parsed := confineArgs{action: confineStatus}
	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		switch rest[0] {
		case "status":
			parsed.action = confineStatus
		case "off":
			parsed.action = confineOff
		case "on":
			parsed.action = confineOn
		default:
			return confineArgs{}, fmt.Errorf("unknown /confine subcommand %q. %s", rest[0], confineUsage)
		}
		rest = rest[1:]
	}
	for _, arg := range rest {
		if arg != "--save" {
			return confineArgs{}, fmt.Errorf("unrecognised /confine argument %q. %s", arg, confineUsage)
		}
		parsed.save = true
	}
	if parsed.save && parsed.action != confineOff {
		return confineArgs{}, fmt.Errorf(
			"--save persists this host's acknowledgement and applies only to /confine off, not /confine %s. %s",
			parsed.action, confineUsage)
	}
	return parsed, nil
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
