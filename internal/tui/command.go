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
	kindMessage      inputKind = iota // free text for the agent (with @file refs extracted)
	kindCommand                       // a recognised /command handled locally or as a canned turn
	kindUnknownSlash                  // the whole input is one /word naming nothing — refused, never sent
)

// parsedInput is the result of classifying one raw input line. For kindCommand, command
// names the recognised verb (without the leading slash); args carries the verb's argument tokens
// for every takesArgs verb (nil for the rest, which ignore what follows them, as they always
// have); rest carries the SAME arguments unsplit — the line's raw tail, for the one verb whose
// argument is prose rather than tokens (/schedule's prompt, which must reach the model spaced and
// lined as it was typed); confine carries the dedicated argument parse of a /confine line (zero value — a status
// report — for every other verb); colorScheme carries the same for a /color-scheme line (zero value — a
// listing — for every other verb); and err is set when a
// recognised verb was given arguments it does not understand. An arguments error stays a
// kindCommand: the router reports the usage line rather than sending the line to the agent or
// silently doing nothing. For kindMessage, text is the line (trimmed, with @tokens and "/id"
// skill tokens left in place so the model sees what was referenced), fileRefs holds the extracted
// workspace-relative paths, skillIDs the extracted skill references, and skillSpans the byte
// ranges those references occupy IN text — one per occurrence, which is what a sent block paints
// its inline accent from ([skillSpan]). For kindUnknownSlash,
// text is the lone token as typed (leading slash included) — the refusal note names it back
// (unknownSlashNote) and nothing else on the value is set.
type parsedInput struct {
	kind        inputKind
	command     string
	args        []string
	rest        string
	confine     confineArgs
	colorScheme colorSchemeArgs
	err         error
	text        string
	fileRefs    []string
	skillIDs    []string
	skillSpans  []skillSpan
}

// commandSpec is one verb of the "/" namespace: what the parser does with it and what the
// dropdown shows for it. name is the verb without its leading slash; summary is the one-line
// description the dropdown displays beside it. The three flags say how the verb behaves:
//
//   - takesArgs — the verb reads what follows it, and parseInput hands it the tokens in
//     parsedInput.args. /confine, whose grammar is richer than a token list, keeps its dedicated
//     parse (parseConfine) on top of them; every non-takesArgs verb ignores surplus tokens, as it
//     always has. It is also what the dropdown reads to COMPLETE such a verb rather than run it
//     (acceptAutocomplete): firing a verb that is not finished would be wrong.
//   - whileRunning — the verb is safe to run while a worker is working, because it only REPORTS:
//     no engine mutation, no worker of its own, no quiescent boundary needed. Every other verb is
//     idle-only and earns commandsAtIdleNote mid-run instead of running (parsedInput.safeWhileRunning
//     is where the flag is read, and /confine's reporting FORM is the one nuance it adds).
//   - noRecall — a sent invocation of this verb is NEVER recorded as a recallable prompt, in memory
//     or on disk. The session-reset pair /clear and /new carry it because recall exists so a line
//     can be handed back and re-sent with one ⏎, and a walk that hands back a session wipe arms that
//     gesture with the one action nothing undoes. /settings carries it for the other reason a line is
//     not worth handing back: it opens a pane and changes nothing, so a recalled invocation would
//     spend a walk step on a keystroke the human can retype. Every other sent line — messages,
//     Interjections, every other whole-line /command — stays recallable (parsedInput.recallable is
//     where the flag is read).
type commandSpec struct {
	name         string
	summary      string
	takesArgs    bool
	whileRunning bool
	noRecall     bool
}

// commandSpecs is THE registry of "/" verbs, in display order (alphabetical — see below): one table
// feeding both the parser (matchCommand recognises every name) and the dropdown
// (commandSuggestions renders every row, summaries included), so the two can no longer
// drift apart. The parser intercepts a line only when its first whitespace token is exactly
// "/<verb>" for a verb in this table; any other slash-prefixed line is treated as an ordinary
// message (never silently swallowed).
//
// /new is an alias of /clear — both verbs are recognised here and route to the same context-reset
// logic in runCommand. /sessions opens the history-browser overlay (idle-only, handled
// synchronously in runCommand like /clear); /rename names THIS session instead of a browsed one —
// with an argument it takes what was typed, and BARE it asks the model for a title (autotitle.go),
// which is the reason it is idle-only: that bare form issues a completion, and a completion fired
// into a live Exchange would contend with the answer being streamed. /model and /server open the
// shared picker (picker.go)
// the same way — /server over what config.yaml names, /model over the Launch profiles the
// llama-launcher config defines when the launcher is configured and over what the upstream
// advertises when it is not. That first offering is the one whose accept does not finish on the
// Update loop: it runs a blocking launcher verb behind the actuation latch instead (actuation.go,
// ADR 0029).
//
// /unload-model and /stop-server are that same latch without an overlay: they take no argument because
// there is nothing to choose — both act on the server this session is talking to and on nothing else,
// which is what keeps the one mistake available here (stopping somebody else's server) off the table.
// Their NAMES carry that object, which is why they are offered like every other row: "/stop" alone
// reads as "stop the running turn" (which is Esc) and "/unload" names nothing at all, so the two were
// briefly kept off the menu — but the hazard lived in the silence of those names, not in the verbs,
// and naming what they act on removes it at the source. A verb the human cannot discover is a verb
// they will not find.
//
// /schedule and /schedule-stop are the standing-instruction pair (ADR 0033): the first puts a prompt
// on a cycle, the second takes one off it. They are the only verbs here that are BOTH argument-taking
// and safe while a worker works — a Schedule is created in the scheduler library and fires as a
// separate headless run, so nothing they do touches this session's engine, and the tick that would
// contend with a live Exchange is deferred by the host's Gate rather than by this table. /schedule's
// prompt form reads the raw tail of the line rather than its tokens (parsedInput.rest), because a
// prompt is text the human wrote, not a token list to be re-spaced.
//
// /color-scheme is the palette verb (colorscheme.go, ADR 0040): bare it lists what this session can
// switch to, with a name it switches — persisting the key and repainting on the same keypress, the
// settings pane's own validate → persist → apply — and `export <name>` writes an editable copy of a
// built-in into the human's schemes folder. Idle-only like every other verb that writes config, and
// argument-taking like /confine, whose grammar it follows down to the usage line.
//
// /settings opens the configuration pane (settings.go): every config key with the value this run
// resolved for it, over the binary's declarative key registry (ADR 0035). Idle-only and modal like
// /sessions, and noRecall like the reset pair — it opens a surface rather than saying anything to the
// model, so a recalled invocation would only spend a walk step.
//
// Order is display order, and it is ALPHABETICAL — declared here in the literal rather than sorted
// at render time, because this table is the registry and the order the dropdown reads is one of the
// things it declares. A menu the human can scan without knowing the table is worth more than any
// hand-curated grouping, and it settles where a future verb goes without a judgement call.
// TestCommandSpecsReadAlphabetically pins it, so a row added out of place fails loudly instead of
// quietly un-sorting the menu.
var commandSpecs = []commandSpec{
	{name: "clear", summary: "reset the model's memory of this session", noRecall: true},
	{name: "color-scheme", summary: "list, switch or export the screen's colour schemes", takesArgs: true},
	{name: "compact", summary: "summarise the conversation to reclaim context"},
	{name: "confine", summary: "report or change auto mode's blast radius", takesArgs: true, whileRunning: true},
	{name: "continue", summary: "ask the model to keep going"},
	{name: "model", summary: "switch model — the launcher's profiles, or what the server serves", takesArgs: true},
	{name: "new", summary: "start a fresh conversation (same as /clear)", noRecall: true},
	{name: "rename", summary: "rename this session (bare = ask the model)", takesArgs: true},
	{name: "schedule", summary: "run a prompt on a cycle (bare = list what is live)", takesArgs: true, whileRunning: true},
	{name: "schedule-stop", summary: "take a schedule off the clock", whileRunning: true},
	{name: "server", summary: "switch to another configured server", takesArgs: true},
	{name: "sessions", summary: "browse, resume, rename or delete saved sessions"},
	{name: "settings", summary: "view the configuration this session resolved", noRecall: true},
	{name: "skills", summary: "list the available skills", whileRunning: true},
	{name: "stop-server", summary: "stop the server this session is on"},
	{name: "unload-model", summary: "free the model of the server this session is on"},
	{name: "version", summary: "show the apogee version", whileRunning: true},
}

// parseInput classifies a raw input line. A blank line yields a kindMessage with empty text
// (the caller ignores it).
//
// known reports whether a bare token names a skill in the catalog — the seam that keeps this
// layer pure, since the catalog is Model state. It is consulted for a kindMessage only, and only
// after the whole-input command rule has had its say: a command verb SHADOWS a skill of the same
// id, so "/clear" alone stays the command even in a workspace that ships a skill called clear. A
// nil predicate means no catalog is wired (or the caller does not care), and no skill reference
// is extracted.
func parseInput(raw string, known func(string) bool) parsedInput {
	trimmed := strings.TrimSpace(raw)
	if cmd, rest, ok := matchCommand(trimmed); ok {
		parsed := parsedInput{kind: kindCommand, command: cmd}
		args := strings.Fields(rest)
		if spec, found := commandByName(cmd); found && spec.takesArgs {
			// A verb that does not read its arguments carries neither form (commandSpec).
			parsed.args, parsed.rest = args, rest
		}
		switch cmd {
		case "confine":
			parsed.confine, parsed.err = parseConfine(args)
		case "color-scheme":
			parsed.colorScheme, parsed.err = parseColorScheme(args)
		}
		return parsed
	}
	if token, ok := soleUnknownSlash(trimmed, known); ok {
		return parsedInput{kind: kindUnknownSlash, text: token}
	}
	// The skill grammar is scanned ONCE and read twice — the ids that travel with the message, the
	// spans that paint it — and both offsets and text come from the same trimmed line, since
	// extractFileRefs hands the text back unchanged. That is what makes skillSpans offsets into
	// parsedInput.text, and so into the entry the submit path stores.
	text, refs := extractFileRefs(trimmed)
	skills := skillRefSpans(trimmed, known)
	return parsedInput{
		kind:       kindMessage,
		text:       text,
		fileRefs:   refs,
		skillIDs:   spanNames(skills),
		skillSpans: skillTokenSpans(skills),
	}
}

// soleUnknownSlash reports the lone "/word" of an input that is nothing but that word and names
// nothing this build can act on: no parser verb, no catalog skill. It is the typo
// guard's whole rule, and it is deliberately narrow — the ONLY input it claims is one the human
// can have meant as an invocation and nothing else, so every real message keeps travelling
// untouched:
//
//   - "/code-adit"          → guarded: a mistyped skill (or command) that would otherwise be sent
//     to the model verbatim, which is exactly the confusion this guard exists to end;
//   - "/code-adit the parser" → a message: more than the one token means prose, whatever it opens
//     with (the mid-message "/word is prose" rule of extractSkillRefs, unchanged);
//   - "/clear", "/grill-me" → not unknown: a verb matchCommand recognises never reaches here, and a
//     token `known` confirms is an ordinary message that happens to invoke a skill (edge default:
//     an input that is ONLY a skill token sends);
//
// A bare "/" carries no word at all and stays a message: there is no token to name back, and the
// human is mid-thought rather than mistaken.
func soleUnknownSlash(trimmed string, known func(string) bool) (string, bool) {
	if len(trimmed) < 2 || trimmed[0] != '/' || strings.ContainsAny(trimmed, " \t\n\r") {
		return "", false
	}
	if _, _, ok := matchCommand(trimmed); ok {
		return "", false
	}
	if known != nil && known(trimmed[1:]) {
		return "", false
	}
	return trimmed, true
}

// unknownSlashNote words the refusal a kindUnknownSlash earns instead of a send. token is the line
// as typed, leading slash included, and the note names it back so the human sees WHICH word failed
// to resolve — a typo'd skill id differs from a mistyped verb only in the spelling, and the fix is
// the same either way.
func unknownSlashNote(token string) string {
	return "unknown command or skill: " + token + " — nothing sent"
}

// commandByName looks a verb up in commandSpecs by its bare name (no leading slash). It is the one
// membership test over the table: the parser asks it what a line opens with, the dropdown's accept
// path asks it what an accepted row DOES (a takesArgs verb completes, every other verb
// runs on the spot), and the merged menu asks it which skill ids a command verb shadows.
func commandByName(name string) (commandSpec, bool) {
	for _, c := range commandSpecs {
		if c.name == name {
			return c, true
		}
	}
	return commandSpec{}, false
}

// safeWhileRunning reports whether this parsed command LINE may be driven while a worker works.
// It is the whole of the per-command policy, in one pure place both ⏎ (stageInterjection) and the
// dropdown's accept (acceptAutocomplete) read, so the menu's "— idle only" tag and what the key
// actually does can never disagree.
//
// Two tests, because the policy is about the line and not only about the verb. The verb's own
// commandSpec.whileRunning says it does nothing this session's engine must be quiescent for —
// /version, /skills, /confine report, and the Schedule pair /schedule, /schedule-stop touches only
// the scheduler library (ADR 0033, see commandSpecs). /confine then adds the one nuance: its
// STATUS form reports (Engine.ConfineToWorkspace is goroutine-safe, read under
// the engine's own confineMu), while "/confine off|on" swaps Auto's blast radius under a Step that
// is already dispatching tool calls and is idle-only for the same reason /clear is. confineArgs'
// zero value IS confineStatus, so that second test reads true for every other verb without naming
// one — and an argument error, which parseConfine reports as the zero value, stays runnable so a
// mistyped /confine earns its usage line mid-run exactly as it does at idle.
func (p parsedInput) safeWhileRunning() bool {
	spec, ok := commandByName(p.command)
	return ok && spec.whileRunning && p.confine.action == confineStatus
}

// recallable reports whether this parsed command LINE is recorded as a recallable prompt. It is
// the one place the noRecall flag is read, so every send path that records agrees on the answer:
// an unrecognised verb never reaches a record site as a kindCommand, and reads recallable for the
// same reason a missing spec reads not-safe-while-running — the table is the authority, and a name
// it does not carry carries no policy either.
func (p parsedInput) recallable() bool {
	spec, ok := commandByName(p.command)
	return !ok || !spec.noRecall
}

// matchCommand reports the recognised command verb when trimmed's first whitespace token is
// "/<verb>" for a verb of commandSpecs, together with everything that followed it —
// the line's RAW tail, split into argument tokens by the caller. Only a takesArgs verb reads it
// (parseInput hands it on as parsedInput.args and .rest); for every other verb it is surplus and
// ignored (as it always was). Splitting is left to the caller because one verb wants the tail
// unsplit: /schedule's argument is a prompt, and re-joining tokens would re-space the human's text.
// The verb itself is delimited by a space or
// a tab, never a newline — so a multi-line message whose first line is "/clear" stays a message,
// as it did before arguments existed.
func matchCommand(trimmed string) (string, string, bool) {
	if !strings.HasPrefix(trimmed, "/") {
		return "", "", false
	}
	first, rest := trimmed, ""
	if i := strings.IndexAny(trimmed, " \t"); i >= 0 {
		first, rest = trimmed[:i], trimmed[i+1:]
	}
	c, ok := commandByName(strings.TrimPrefix(first, "/"))
	if !ok {
		return "", "", false
	}
	return c.name, rest, true
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

// ----------------------------------------------------------------------------
// /color-scheme — the palette command's argument grammar
// ----------------------------------------------------------------------------

// colorSchemeAction is the subcommand of a parsed /color-scheme line. The zero value is
// colorSchemeList, so a bare "/color-scheme" reports what there is to choose from rather than
// changing the screen — the /confine posture, and for the same reason: the verb that changes
// something must be the one the human spelled out.
type colorSchemeAction int

const (
	colorSchemeList   colorSchemeAction = iota // name every scheme this session can switch to
	colorSchemeSwitch                          // load a scheme by name, persisting the choice
	colorSchemeExport                          // write an editable copy of a built-in to the schemes folder
)

// colorSchemeArgs is the parsed argument list of a /color-scheme line: what was asked for, and the
// scheme name it was asked of (empty for the bare listing, which names nothing).
type colorSchemeArgs struct {
	action colorSchemeAction
	name   string
}

// colorSchemeUsage is the one-line grammar every /color-scheme argument error carries, so a
// mistyped line teaches the syntax instead of switching to a scheme nobody asked for.
const colorSchemeUsage = "usage: /color-scheme | /color-scheme <name> | /color-scheme export <name>"

// parseColorScheme parses the argument tokens that followed a "/color-scheme" verb. No arguments
// means the listing. One token is a scheme NAME to switch to — any name, because a scheme this
// build cannot find is a forgiving load with a warning rather than a parse error (ADR 0040 design
// call 8), and refusing it here would put the parser in the business of knowing what is on disk.
// "export" is the one reserved first token, and it takes exactly one name.
//
// Everything else — a bare "export", a name with tokens after it — is an error carrying
// colorSchemeUsage rather than a guess: two tokens are as likely a mistyped subcommand as a scheme
// whose name has a space in it (schemes are file basenames, so it is neither).
func parseColorScheme(args []string) (colorSchemeArgs, error) {
	switch {
	case len(args) == 0:
		return colorSchemeArgs{action: colorSchemeList}, nil
	case args[0] == "export":
		if len(args) != 2 {
			return colorSchemeArgs{}, fmt.Errorf("/color-scheme export takes exactly one scheme name. %s", colorSchemeUsage)
		}
		return colorSchemeArgs{action: colorSchemeExport, name: args[1]}, nil
	case len(args) == 1:
		return colorSchemeArgs{action: colorSchemeSwitch, name: args[0]}, nil
	default:
		return colorSchemeArgs{}, fmt.Errorf("unknown /color-scheme subcommand %q. %s", args[0], colorSchemeUsage)
	}
}

// refSpan is one resolving token of the mini-language, LOCATED in the text: the byte range
// [start,end) it occupies and the name it resolves to (a workspace-relative path for an @ref, a
// skill id for a "/" one). Two readers want different halves of that pair — the extractors below
// take the names to send with the message, the inline accents take the ranges to paint
// (inputaccent.go) — so both grammars are scanned in exactly ONE place and the two readers can
// never disagree about what is a token.
type refSpan struct {
	start, end int
	name       string
}

// fileRefSpans locates the @file references in s. An @-ref is an "@" at the start of s or
// immediately after whitespace — so an email like foo@bar.com (where "@" follows a non-space) is
// not a reference — followed by a token in one of two forms (scanRefToken owns the grammar):
//
//   - bare: a run of non-whitespace characters, @internal/agent/loop.go;
//   - quoted: @"path with spaces" or @'path with spaces' — both quote characters are
//     accepted, the closing quote ends the token so ordinary text may follow it, and an
//     unterminated quote runs to the end of that line. There are no escape sequences.
//
// The span covers the literal token, quotes included; the name is the path without the "@" and
// without any quotes. A token naming nothing (a bare "@", an empty quoted pair) is skipped, but
// the scan still resumes past it.
func fileRefSpans(s string) []refSpan {
	var spans []refSpan
	for i := 0; i < len(s); i++ {
		if s[i] != '@' {
			continue
		}
		if i > 0 && !isInputSpace(s[i-1]) { // not at a word boundary ⇒ not a ref (e.g. an email)
			continue
		}
		path, end := scanRefToken(s, i+1)
		if path != "" {
			spans = append(spans, refSpan{start: i, end: end, name: path})
		}
		i = end - 1 // resume scanning past this token (the loop's own i++ lands on end)
	}
	return spans
}

// skillRefSpans locates the inline skill references in s. A skill reference is the exact mirror of
// an @file one — the same word-boundary, whitespace-delimited grammar, and the same "the token
// stays in the text" rule — so the two halves of the prompt mini-language read alike:
//
//	/code-audit please check @internal/tui/command.go
//
// The token must start at the beginning of s or immediately after whitespace, and it runs to the
// next whitespace byte. Only a token whose bare name `known` confirms as a catalog ID counts:
// every other slash-prefixed word is ordinary prose, which is what lets a path (/usr/bin), a
// fraction (and/or) or a typo (/code-adit) travel to the model untouched. Skill IDs are directory
// names and so never contain whitespace, which is why this grammar needs no quoted form.
func skillRefSpans(s string, known func(string) bool) []refSpan {
	if known == nil {
		return nil
	}
	var spans []refSpan
	for i := 0; i < len(s); i++ {
		if s[i] != '/' {
			continue
		}
		if i > 0 && !isInputSpace(s[i-1]) { // not at a word boundary ⇒ prose (a path, a fraction)
			continue
		}
		end := i + 1
		for end < len(s) && !isInputSpace(s[end]) {
			end++
		}
		if id := s[i+1 : end]; id != "" && known(id) {
			spans = append(spans, refSpan{start: i, end: end, name: id})
		}
		i = end - 1 // resume past this token (the loop's own i++ lands on end)
	}
	return spans
}

// spanNames reduces located tokens to the names they resolve to, de-duplicated in first-seen
// order — so @x and @"x" collapse to one reference, and a skill named twice is invoked once.
func spanNames(spans []refSpan) []string {
	var names []string
	seen := map[string]bool{}
	for _, sp := range spans {
		if seen[sp.name] {
			continue
		}
		seen[sp.name] = true
		names = append(names, sp.name)
	}
	return names
}

// skillTokenSpans reduces located tokens to the bare byte ranges a sent block paints — the other
// half of the refSpan pair, and the half spanNames throws away. It keeps one span per OCCURRENCE
// where spanNames de-dupes by name, because painting is about where the words are: a skill named
// twice is invoked once and lights up twice.
func skillTokenSpans(spans []refSpan) []skillSpan {
	if len(spans) == 0 {
		return nil
	}
	out := make([]skillSpan, 0, len(spans))
	for _, sp := range spans {
		out = append(out, skillSpan{start: sp.start, end: sp.end})
	}
	return out
}

// extractFileRefs returns s unchanged plus the workspace-relative paths its @file references name
// (fileRefSpans owns the grammar). The literal @token — quotes included — is left in the text so
// the model sees what the human pointed at.
func extractFileRefs(s string) (string, []string) {
	return s, spanNames(fileRefSpans(s))
}

// extractSkillRefs returns the catalog IDs the inline "/" tokens of s name (skillRefSpans owns the
// grammar). The literal token is left in the text — the owner's explicit choice over stripping it:
// the model sees the invocation the human typed AND the skill body the agent prepends for it.
func extractSkillRefs(s string, known func(string) bool) []string {
	return spanNames(skillRefSpans(s, known))
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
