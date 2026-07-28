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

// commandSpec is one verb of the "/" namespace: what the parser does with it and what the
// dropdown shows for it. name is the verb without its leading slash; summary is the one-line
// description the dropdown displays beside it. The three flags say how the verb behaves:
//
//   - takesArgs — the verb reads what follows it. Only /confine does (parseConfine owns its
//     grammar); every other verb ignores surplus tokens, as it always has.
//   - whileRunning — the verb is safe to run while a worker is working, because it only reports.
//     Recorded here for the dispatch side to honour; nothing reads it yet, so today every
//     command still runs at idle only.
//   - menuOnly — the dropdown offers the verb but the parser must never recognise it. /skill is
//     the one: accepting it chains into the skill picker, and keeping it unparsed is exactly
//     what keeps an unknown "/skill foo" line an ordinary message.
type commandSpec struct {
	name         string
	summary      string
	takesArgs    bool
	whileRunning bool
	menuOnly     bool
}

// commandSpecs is THE registry of "/" verbs, in display order: one table feeding both the parser
// (matchCommand recognises every non-menuOnly name) and the dropdown (commandSuggestions renders
// every row, summaries included), so the two can no longer drift apart. The parser intercepts a
// line only when its first whitespace token is exactly "/<verb>" for a verb in this table; any
// other slash-prefixed line is treated as an ordinary message (never silently swallowed).
//
// /new is an alias of /clear — both verbs are recognised here and route to the same context-reset
// logic in runCommand. /sessions opens the history-browser overlay (idle-only, handled
// synchronously in runCommand like /clear). /server is deferred (it needs a swappable provider
// seam) and so is absent.
//
// Order is display order, and one pair depends on it: /skill must precede /skills, because the
// dropdown prefix-matches in table order and highlights its first row — so a typed "/skill"
// completes to the picker rather than to the listing that merely shares its prefix.
var commandSpecs = []commandSpec{
	{name: "clear", summary: "reset the model's memory of this session"},
	{name: "new", summary: "start a fresh conversation (same as /clear)"},
	{name: "sessions", summary: "browse, resume, rename or delete saved sessions"},
	{name: "compact", summary: "summarise the conversation to reclaim context"},
	{name: "continue", summary: "ask the model to keep going"},
	{name: "confine", summary: "report or change auto mode's blast radius", takesArgs: true, whileRunning: true},
	{name: "version", summary: "show the apogee version", whileRunning: true},
	{name: "skill", summary: "attach a skill to your next message", menuOnly: true},
	{name: "skills", summary: "list the available skills", whileRunning: true},
}

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
// "/<verb>" for a non-menuOnly verb of commandSpecs, together with the remaining
// whitespace-separated argument tokens. Only /confine reads the arguments; for every other verb
// they are surplus and ignored (as they always were). The verb itself is delimited by a space or
// a tab, never a newline — so a multi-line message whose first line is "/clear" stays a message,
// as it did before arguments existed.
func matchCommand(trimmed string) (string, []string, bool) {
	if !strings.HasPrefix(trimmed, "/") {
		return "", nil, false
	}
	first, rest := trimmed, ""
	if i := strings.IndexAny(trimmed, " \t"); i >= 0 {
		first, rest = trimmed[:i], trimmed[i+1:]
	}
	verb := strings.TrimPrefix(first, "/")
	for _, c := range commandSpecs {
		if c.menuOnly {
			continue // offered by the dropdown, never parsed (see commandSpec)
		}
		if verb == c.name {
			return c.name, strings.Fields(rest), true
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
// whitespace — so an email like foo@bar.com (where "@" follows a non-space) is not a
// reference — followed by a token in one of two forms (scanRefToken owns the grammar):
//
//   - bare: a run of non-whitespace characters, @internal/agent/loop.go;
//   - quoted: @"path with spaces" or @'path with spaces' — both quote characters are
//     accepted, the closing quote ends the token so ordinary text may follow it, and an
//     unterminated quote runs to the end of that line. There are no escape sequences.
//
// The literal @token — quotes included — is left in the text so the model sees what the human
// pointed at; the path (without the "@" and without any quotes) is collected, de-duplicated in
// first-seen order, so @x and @"x" collapse to one reference.
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
		path, end := scanRefToken(s, i+1)
		if path != "" && !seen[path] {
			seen[path] = true
			refs = append(refs, path)
		}
		i = end - 1 // resume scanning past this token (the loop's own i++ lands on end)
	}
	return s, refs
}

// scanRefToken scans the token of an @file reference and reports the referenced path together
// with the offset just past the token. start is the byte immediately after the "@"; the caller
// owns the word-boundary rule.
//
// A token opening with a quote character (" or ') runs to the next occurrence of that same
// character on the same line: the path is the text between the quotes, and the closing quote
// ends the token — anything after it (a comma, more prose) is ordinary text again. An
// unterminated quote runs to the end of the line ("\n") or of s, with the path right-trimmed of
// spaces and tabs: a word-boundary @" is unambiguous intent, and a token never crosses a
// newline, so a stray quote cannot swallow the rest of a multi-line message. There are no
// escape sequences — a path containing " is quoted with ', and vice versa. Any other token is
// the bare form: a run of non-whitespace bytes.
func scanRefToken(s string, start int) (string, int) {
	if start >= len(s) {
		return "", start
	}
	if quote := s[start]; quote == '"' || quote == '\'' {
		for j := start + 1; j < len(s); j++ {
			switch s[j] {
			case quote:
				return s[start+1 : j], j + 1
			case '\n':
				return strings.TrimRight(s[start+1:j], " \t"), j
			}
		}
		return strings.TrimRight(s[start+1:], " \t"), len(s)
	}
	j := start
	for j < len(s) && !isInputSpace(s[j]) {
		j++
	}
	return s[start:j], j
}

// isInputSpace reports whether b is an ASCII whitespace byte used as a token boundary.
func isInputSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
