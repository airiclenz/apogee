package security

import (
	"path"
	"regexp"
	"strings"
	"unicode"
)

// ----------------------------------------------------------------------------
// The shell write view — what a command line can WRITE, for the write-shaped rules
// ----------------------------------------------------------------------------

// writeTargetsOf returns the text a write-shaped rule that opted into the shell write view
// (Rule.ShellWriteView) inspects for a shell command line: the paths the line can write to
// or delete, space-joined, and nothing it merely reads. It is a verb-aware reading of the
// line, not a shell: the command is split into simple commands at pipes, `&&`, `||`, `;`,
// `&` and newlines, with quotes honoured and command substitutions (`$(…)`, backticks)
// split out and read as commands of their own, and each simple command contributes
//
//   - every OUTPUT redirect target (`>`, `>>`, `>|`, `&>`, `<>`; a heredoc delimiter, a
//     here-string and an fd dup such as `2>&1` name no file and contribute nothing),
//   - nothing for a READ leader — ls, cat, head, tail, less, cmp, diff, stat, wc, grep,
//     file, a `git` read subcommand, `find` without `-delete` / `-exec`, `sed` without an
//     in-place flag, and the like (readLeaders): its operands are what it reads — except
//     the file an output option names (`--output=file`, `--output file`; outputOptionValues),
//     the second operand of `uniq` and `xxd` (`uniq in out`, `xxd -r in out`), `tree`'s `-o`
//     value and the file a `sed` script's `w` / `W` command or `s///w` flag writes,
//   - only the `of=` values (and an output option's file) for `dd`,
//   - and every operand of any OTHER leader — a mutating one (rm, mv, cp, install, ln,
//     chmod, chown, touch, mkdir, rmdir, truncate, tee, `sed -i`, `perl -i`, a `git`
//     subcommand that writes) or an unknown one, which fails closed: a leader this file
//     cannot vouch for is judged as writing to everything it names.
//
// A bare option (`-rf`, `--force`) names no path and is dropped; an option carrying a value
// (`--output=x`) contributes the value. Wrapper leaders (sudo, env, nohup, nice, time, xargs,
// command, exec) and leading `NAME=value` assignments are stripped before the leader is read;
// a command that is nothing but assignments (`d=.git/hooks; rm -rf $d`) contributes their
// values, since a later command's operands resolve through them.
// The heredoc body a `<<` opens is skipped up to its delimiter line: it is the payload the
// redirect carries, not a command — save when its host command is a shell interpreter (sh,
// bash, dash, zsh, ksh) reading its script on stdin (no `-c`, no script-file operand), whose
// body is shell: it is split into simple commands of its own and judged as a substitution
// body is, appended after its host (owner decision, 2026-10-06).
//
// It is a footgun-guard reading (ADR 0012), not an obfuscation-resistant one: `cat .git/config`
// is a read and `echo x > .git/config` a write, and a line built to look like the former while
// doing the latter is the adversary game this guard does not play.
func writeTargetsOf(command string) string {
	var targets []string
	for _, cmd := range splitSimpleCommands(command) {
		targets = append(targets, cmd.redirectTargets...)
		targets = append(targets, operandTargets(cmd.words)...)
	}
	return strings.Join(targets, " ")
}

// simpleCommand is one pipeline stage or chain member of a command line: its words in order
// (leader first, quotes already removed) and the output redirect targets found beside them.
type simpleCommand struct {
	words           []string
	redirectTargets []string
}

// pendingWord says where the tokenizer routes the next word it completes: to the command's
// words, to its redirect targets, to a heredoc delimiter, or nowhere (an input file, a
// here-string, an fd number).
type pendingWord int

const (
	pendingOperand pendingWord = iota
	pendingRedirectTarget
	pendingHeredocDelimiter
	pendingSkip
)

// shellTokenizer walks one command line once, splitting it into simple commands. It is a
// small state machine over runes rather than a shell grammar: enough to tell a redirect
// target from an operand, a quoted word from a separator, and a substitution from its host.
type shellTokenizer struct {
	commands []simpleCommand
	current  simpleCommand
	word     strings.Builder
	inWord   bool
	pending  pendingWord
	heredocs []heredoc // the heredocs whose bodies follow the next newline, in order
	// boundHeredocs counts the heredocs already bound to the command that opened them; the
	// rest belong to the command in progress, which binds them when it closes.
	boundHeredocs int
	// flushAfter holds the commands a substitution nested in the current command; they
	// are emitted right after it so a reader sees the host before its guests.
	flushAfter []simpleCommand
}

// heredoc is one pending `<<` body: the delimiter line that ends it, and whether its host
// command reads it as a shell script (readsStdinScript), which makes the body commands of
// its own rather than payload.
type heredoc struct {
	delimiter string
	script    bool
}

// splitSimpleCommands tokenizes command into its simple commands; nested command
// substitutions are appended after the command that hosts them.
func splitSimpleCommands(command string) []simpleCommand {
	tk := &shellTokenizer{}
	tk.scan(command)
	tk.endCommand()
	return tk.commands
}

func (tk *shellTokenizer) scan(s string) {
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\\':
			if i+1 < len(runes) {
				i++
				tk.writeRune(runes[i])
			}
		case r == '\'':
			i = tk.scanSingleQuoted(runes, i)
		case r == '"':
			i = tk.scanDoubleQuoted(runes, i)
		case r == '`':
			i = tk.scanBacktick(runes, i)
		case r == '$':
			i = tk.scanDollar(runes, i)
		case r == '#' && !tk.inWord:
			for i+1 < len(runes) && runes[i+1] != '\n' {
				i++
			}
		case r == '\n':
			tk.endCommand()
			i = tk.skipHeredocBodies(runes, i)
		case r == ';' || r == '|' || r == '(' || r == ')':
			tk.endCommand()
		case r == '{' || r == '}':
			// A brace is a group delimiter only as a word of its own (`{ cmd; }`); inside a
			// word it is text — find's `{}` above all.
			if !tk.inWord && (i+1 == len(runes) || strings.ContainsRune(" \t\n;&|)", runes[i+1])) {
				tk.endCommand()
				continue
			}
			tk.writeRune(r)
		case r == '&':
			if i+1 < len(runes) && runes[i+1] == '>' {
				i = tk.scanOutputRedirect(runes, i+1)
				continue
			}
			tk.endCommand()
		case r == '>':
			i = tk.scanOutputRedirect(runes, i)
		case r == '<':
			i = tk.scanInputRedirect(runes, i)
		case unicode.IsSpace(r):
			tk.endWord()
		default:
			tk.writeRune(r)
		}
	}
}

// scanSingleQuoted copies a '…' body verbatim; returns the index of the closing quote.
func (tk *shellTokenizer) scanSingleQuoted(runes []rune, i int) int {
	tk.inWord = true
	for i++; i < len(runes) && runes[i] != '\''; i++ {
		tk.word.WriteRune(runes[i])
	}
	return i
}

// scanDoubleQuoted copies a "…" body, honouring backslash escapes and the substitutions a
// double-quoted string still performs; returns the index of the closing quote.
func (tk *shellTokenizer) scanDoubleQuoted(runes []rune, i int) int {
	tk.inWord = true
	for i++; i < len(runes) && runes[i] != '"'; i++ {
		switch runes[i] {
		case '\\':
			if i+1 < len(runes) {
				i++
				tk.word.WriteRune(runes[i])
			}
		case '`':
			i = tk.scanBacktick(runes, i)
		case '$':
			i = tk.scanDollar(runes, i)
		default:
			tk.word.WriteRune(runes[i])
		}
	}
	return i
}

// scanBacktick lifts a `…` command substitution out as a nested command; returns the index
// of the closing backtick.
func (tk *shellTokenizer) scanBacktick(runes []rune, i int) int {
	tk.inWord = true
	start := i + 1
	end := start
	for end < len(runes) && runes[end] != '`' {
		end++
	}
	tk.nest(string(runes[start:end]))
	return end
}

// scanDollar handles `$(…)` (a nested command), `${…}` (a parameter, copied through) and a
// bare `$`; returns the index of the last rune consumed.
func (tk *shellTokenizer) scanDollar(runes []rune, i int) int {
	tk.inWord = true
	if i+1 < len(runes) && runes[i+1] == '(' {
		depth := 0
		end := i + 1
		for ; end < len(runes); end++ {
			if runes[end] == '(' {
				depth++
			} else if runes[end] == ')' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		tk.nest(string(runes[i+2 : end]))
		return end
	}
	if i+1 < len(runes) && runes[i+1] == '{' {
		for ; i < len(runes) && runes[i] != '}'; i++ {
			tk.word.WriteRune(runes[i])
		}
		if i < len(runes) {
			tk.word.WriteRune(runes[i])
		}
		return i
	}
	tk.word.WriteRune('$')
	return i
}

// nest tokenizes a command substitution's body as commands of its own, appended after the
// hosting command's own entry once that closes.
func (tk *shellTokenizer) nest(body string) {
	tk.flushAfter = append(tk.flushAfter, splitSimpleCommands(body)...)
}

// scanOutputRedirect consumes a `>`-family operator starting at runes[i] (`>`, `>>`, `>|`,
// `>&`, and `&>` / `&>>` with i on the `>`) and routes the following word to the redirect
// targets — or nowhere, for an fd dup (`>&1`, `>&-`). A digit word just before the operator
// is its fd and is discarded. Returns the index of the last rune consumed.
func (tk *shellTokenizer) scanOutputRedirect(runes []rune, i int) int {
	tk.dropFdWord()
	for i+1 < len(runes) && (runes[i+1] == '>' || runes[i+1] == '|') {
		i++
	}
	if i+1 < len(runes) && runes[i+1] == '&' {
		i++
		if i+1 < len(runes) && (unicode.IsDigit(runes[i+1]) || runes[i+1] == '-') {
			tk.pending = pendingSkip
			return i
		}
	}
	tk.pending = pendingRedirectTarget
	return i
}

// scanInputRedirect consumes a `<`-family operator: `<` and `<<<` name what is READ (their
// word is skipped), `<<` / `<<-` open a heredoc whose delimiter word is recorded so the body
// can be skipped — or, for an interpreter's stdin script, read — at the next newline, and
// `<>` opens read-write and counts as a write.
// Returns the index of the last rune consumed.
func (tk *shellTokenizer) scanInputRedirect(runes []rune, i int) int {
	tk.dropFdWord()
	switch {
	case i+2 < len(runes) && runes[i+1] == '<' && runes[i+2] == '<':
		tk.pending = pendingSkip
		return i + 2
	case i+1 < len(runes) && runes[i+1] == '<':
		i++
		if i+1 < len(runes) && runes[i+1] == '-' {
			i++
		}
		tk.pending = pendingHeredocDelimiter
		return i
	case i+1 < len(runes) && runes[i+1] == '>':
		tk.pending = pendingRedirectTarget
		return i + 1
	case i+1 < len(runes) && runes[i+1] == '&':
		tk.pending = pendingSkip
		return i + 1
	}
	tk.pending = pendingSkip
	return i
}

// dropFdWord discards the word in progress when it is a file-descriptor number (`2>`), and
// ends it as an ordinary word otherwise (`foo>bar` is `foo > bar`).
func (tk *shellTokenizer) dropFdWord() {
	if !tk.inWord {
		return
	}
	w := tk.word.String()
	isFd := w != "" && strings.IndexFunc(w, func(r rune) bool { return !unicode.IsDigit(r) }) < 0
	if isFd {
		tk.word.Reset()
		tk.inWord = false
		return
	}
	tk.endWord()
}

// skipHeredocBodies advances past every pending heredoc body: line by line from runes[i]
// (a newline) until each delimiter appears on a line of its own, in order. A body its host
// reads as a shell script is tokenized as commands of its own, appended after the commands
// already closed — its host among them. Returns the index of the last rune consumed.
func (tk *shellTokenizer) skipHeredocBodies(runes []rune, i int) int {
	for _, doc := range tk.heredocs {
		var body []string
		for i+1 < len(runes) {
			end := i + 1
			for end < len(runes) && runes[end] != '\n' {
				end++
			}
			line := string(runes[i+1 : end])
			i = end
			if strings.TrimSpace(line) == doc.delimiter {
				break
			}
			body = append(body, line)
		}
		if doc.script {
			tk.commands = append(tk.commands, splitSimpleCommands(strings.Join(body, "\n"))...)
		}
	}
	tk.heredocs = nil
	tk.boundHeredocs = 0
	return i
}

func (tk *shellTokenizer) writeRune(r rune) {
	tk.inWord = true
	tk.word.WriteRune(r)
}

// endWord closes the word in progress and routes it by what the last operator asked for.
func (tk *shellTokenizer) endWord() {
	if !tk.inWord {
		return
	}
	w := tk.word.String()
	tk.word.Reset()
	tk.inWord = false
	switch {
	case w == "": // a word that was only a substitution: its commands were nested, nothing is left
	case tk.pending == pendingRedirectTarget:
		tk.current.redirectTargets = append(tk.current.redirectTargets, w)
	case tk.pending == pendingHeredocDelimiter:
		tk.heredocs = append(tk.heredocs, heredoc{delimiter: w})
	case tk.pending == pendingSkip:
	default:
		tk.current.words = append(tk.current.words, w)
	}
	tk.pending = pendingOperand
}

// endCommand closes the simple command in progress, keeping it only when it has words or
// redirect targets, followed by any substitutions it hosted. The heredocs it opened are
// bound to it here, once its words are complete: each body is a script exactly when this
// command reads one on stdin.
func (tk *shellTokenizer) endCommand() {
	tk.endWord()
	tk.pending = pendingOperand
	if tk.boundHeredocs < len(tk.heredocs) {
		script := readsStdinScript(tk.current.words)
		for i := tk.boundHeredocs; i < len(tk.heredocs); i++ {
			tk.heredocs[i].script = script
		}
		tk.boundHeredocs = len(tk.heredocs)
	}
	if len(tk.current.words) > 0 || len(tk.current.redirectTargets) > 0 {
		tk.commands = append(tk.commands, tk.current)
	}
	tk.commands = append(tk.commands, tk.flushAfter...)
	tk.flushAfter = nil
	tk.current = simpleCommand{}
}

// ----------------------------------------------------------------------------
// Leaders: which words of a simple command are write targets
// ----------------------------------------------------------------------------

// readLeaders are the commands whose operands name what they READ, never what they write:
// their simple command contributes nothing beyond its redirect targets and the file an
// output option names (`--output=file`, outputOptionValues). Membership is a claim that the
// program writes to no other file its operands name, on any option — save three members
// operandTargets judges ahead of this map: `uniq` and `xxd` write their second operand
// (secondPositional) and `tree` its `-o` value (treeOutputTargets). `sort` (`-o`) and `awk`
// (a program may redirect) are deliberately absent and fail closed. So are
// the builtins that change what a LATER command's operands resolve to — `cd` (the working
// directory), `export` / `set` / `unset` (the environment): `cd .git/hooks && rm -rf
// pre-commit` writes under the control plane while naming it only in the `cd`, so those
// operands feed the view too (owner decision, 2026-09-15).
var readLeaders = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "less": true, "more": true,
	"cmp": true, "diff": true, "stat": true, "wc": true, "grep": true, "egrep": true,
	"fgrep": true, "rg": true, "file": true, "tree": true, "du": true, "df": true,
	"readlink": true, "realpath": true, "basename": true, "dirname": true,
	"md5sum": true, "sha1sum": true, "sha256sum": true, "strings": true, "od": true,
	"xxd": true, "hexdump": true, "nl": true, "tac": true, "cut": true, "tr": true,
	"uniq": true, "echo": true, "printf": true, "pwd": true, "test": true,
	"[": true, "true": true, "false": true, "type": true, "which": true,
	"sleep": true, "exit": true, ":": true,
}

// wrapperLeaders run another command named by their operands; the wrapper and its bare
// options are stripped so the wrapped leader is the one judged.
var wrapperLeaders = map[string]bool{
	"sudo": true, "env": true, "nohup": true, "nice": true, "time": true, "xargs": true,
	"command": true, "exec": true, "builtin": true,
}

// gitReadSubcommands are the `git` verbs that inspect a repository without writing to any
// path their operands name. Every other verb — including `config`, `branch`, `tag`, `remote`
// and `stash`, which read or write by flag — is judged as writing.
var gitReadSubcommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true, "blame": true,
	"ls-files": true, "ls-tree": true, "ls-remote": true, "cat-file": true,
	"rev-parse": true, "rev-list": true, "describe": true, "grep": true, "shortlog": true,
	"reflog": true, "show-ref": true, "name-rev": true, "merge-base": true, "var": true,
	"count-objects": true, "fsck": true, "verify-pack": true, "diff-tree": true,
	"diff-files": true, "diff-index": true, "for-each-ref": true, "check-ignore": true,
	"check-attr": true, "help": true, "version": true, "--version": true, "--help": true,
}

// GitCommandConfigNameSource is the one source string behind every "config name whose VALUE is
// a program git executes" pattern: an sshCommand, an editor, a pager, an askpass, a filter or
// merge or diff driver, a credential helper, a gpg.program, a proxy command. internal/gitexec
// anchors it as its CommandConfigName — the repo-local probe that costs a repository its git
// tools — and hands that string to git's --get-regexp verbatim, so it must stay POSIX-ERE
// compatible: plain ( ) groups, no (?: and no (?i). It lives here because gitexec imports
// security, never the reverse; a name it matches is spelled lowercase, so a caller lowers what
// it matches first.
const GitCommandConfigNameSource = `core\.(sshcommand|editor|pager|askpass|gitproxy|alternaterefscommand)|sequence\.editor|diff\.external|diff\..*\.(command|textconv)|merge\..*\.driver|mergetool\..*\.cmd|difftool\..*\.cmd|filter\..*\.(clean|smudge|process)|credential\.helper|credential\..*\.helper|gpg\.program|gpg\..*\.program|uploadpack\.packobjectshook|remote\..*\.proxy|pager\..*`

// GitCommandConfigName is the shell write view's superset of gitexec's CommandConfigName: the
// same names plus core.hookspath. The probe leaves core.hooksPath out on purpose — gitexec's
// hardening options empty it on every call, so refusing it there would cost a repository its
// git tools for a key that reaches no program — but a `git config` line that SETS it is a write
// into the repository's control plane all the same, and that is what this pattern judges.
var GitCommandConfigName = regexp.MustCompile(`^(` + GitCommandConfigNameSource + `|core\.hookspath)$`)

// gitConfigReadForms are the `git config` options under which the invocation only reads a
// value and writes no config file.
var gitConfigReadForms = map[string]bool{
	"--get": true, "--get-all": true, "--get-regexp": true, "--get-urlmatch": true,
	"--get-color": true, "--get-colorbool": true, "-l": true, "--list": true,
}

// gitConfigOtherFileOptions are the `git config` options that direct the write at a file OTHER
// than the repository's own: the operator's scopes, or a file the line names outright (which
// then feeds the view as an operand of its own).
var gitConfigOtherFileOptions = map[string]bool{
	"--global": true, "--system": true, "-f": true, "--file": true,
}

// findWritingPredicates are the `find` operands that make it delete, run a program or write
// a listing; with any of them present the whole command is judged as writing.
var findWritingPredicates = map[string]bool{
	"-delete": true, "-exec": true, "-execdir": true, "-ok": true, "-okdir": true,
	"-fprint": true, "-fprint0": true, "-fprintf": true, "-fls": true,
}

// operandTargets returns the words of one simple command that its leader may write to. A
// command that is only assignments (`d=.git/hooks`) has no leader to judge; it sets a name a
// LATER command's operands resolve through, so its values feed the view as `export`'s would.
func operandTargets(words []string) []string {
	all := words
	words = stripWrappers(words)
	if len(words) == 0 {
		return assignmentValues(all)
	}
	leader, operands := path.Base(words[0]), words[1:]
	// A branch that returns valueOperands already keeps an output option's file; every other
	// branch adds it from outputs, so the file is emitted once either way.
	outputs := outputOptionValues(leader, operands)
	switch {
	case leader == "git":
		return gitTargets(operands)
	case leader == "dd":
		return append(ddTargets(operands), outputs...)
	case leader == "uniq":
		return append(secondPositional(operands, uniqOptionTakesValue), outputs...)
	case leader == "xxd":
		return append(secondPositional(operands, xxdOptionTakesValue), outputs...)
	case leader == "tree":
		return append(treeOutputTargets(operands), outputs...)
	case leader == "find":
		if !hasAny(operands, findWritingPredicates) {
			return outputs
		}
	case leader == "sed":
		if !hasInPlaceFlag(operands) {
			return append(sedWriteTargets(operands), outputs...)
		}
	case leader == "perl":
		if !hasInPlaceFlag(operands) {
			return valueOperands(operands) // arbitrary code: fail closed like an unknown leader
		}
	case readLeaders[leader]:
		return outputs
	}
	return valueOperands(operands)
}

// outputOptionValues returns the files a leader's output options name: the value of every
// `--output=file` and the word after every bare `--output`, the spelling any leader (`git diff
// --output=…` included) uses to write its result to a file. Option parsing stops at `--`;
// `echo` / `printf` print their operands rather than parse them, and a grep / rg `-e` value is
// a pattern, so neither is read as an option.
func outputOptionValues(leader string, operands []string) []string {
	if leader == "echo" || leader == "printf" {
		return nil
	}
	takesPattern := leader == "grep" || leader == "egrep" || leader == "fgrep" || leader == "rg"
	var targets []string
	for i := 0; i < len(operands); i++ {
		w := operands[i]
		switch {
		case w == "--":
			return targets
		case takesPattern && w == "-e":
			i++
		case w == "--output":
			if i+1 < len(operands) {
				i++
				targets = append(targets, operands[i])
			}
		default:
			if v, ok := strings.CutPrefix(w, "--output="); ok && v != "" {
				targets = append(targets, v)
			}
		}
	}
	return targets
}

// secondPositional returns the second positional operand of an option list — the output file
// of `uniq in out` and `xxd -r in out` — as a one-element slice, or nil when there is none. An
// option word for which takesValue reports true consumes the word after it; after `--` every
// word is positional.
func secondPositional(operands []string, takesValue func(string) bool) []string {
	const outputPosition = 2
	position, optionsDone := 0, false
	for i := 0; i < len(operands); i++ {
		w := operands[i]
		if !optionsDone && w == "--" {
			optionsDone = true
			continue
		}
		if !optionsDone && strings.HasPrefix(w, "-") && w != "-" {
			if takesValue(w) {
				i++
			}
			continue
		}
		position++
		if position == outputPosition {
			return []string{w}
		}
	}
	return nil
}

// uniqValueOptions are uniq's long options that take the next word as their value.
var uniqValueOptions = map[string]bool{
	"--skip-fields": true, "--skip-chars": true, "--check-chars": true,
}

// uniqOptionTakesValue reports whether a uniq option word consumes the word after it: one of
// uniqValueOptions, or a short bundle whose first value letter (`f`, `s`, `w`) ends it — a
// value letter earlier in the bundle takes the bundle's rest as its value instead.
func uniqOptionTakesValue(w string) bool {
	if strings.HasPrefix(w, "--") {
		return uniqValueOptions[w]
	}
	letters := w[1:]
	if at := strings.IndexAny(letters, "fsw"); at >= 0 {
		return at == len(letters)-1
	}
	return false
}

// xxdValueOptions are xxd's options that take the next word as their value; xxd parses no
// bundles, and a value glued to the option (`-l64`) consumes nothing.
var xxdValueOptions = map[string]bool{
	"-c": true, "-cols": true, "-g": true, "-groupsize": true, "-l": true, "-len": true,
	"-o": true, "-offset": true, "-s": true, "-seek": true, "-n": true, "-name": true,
}

// xxdOptionTakesValue reports whether an xxd option word consumes the word after it.
func xxdOptionTakesValue(w string) bool {
	return xxdValueOptions[w]
}

// treeOutputTargets returns the files tree's `-o` option writes its listing to. A bare `-o`
// writes only for tree: `ls -o` and `grep -o` are formatting flags. Option parsing stops at `--`.
func treeOutputTargets(operands []string) []string {
	var targets []string
	for i := 0; i < len(operands); i++ {
		switch {
		case operands[i] == "--":
			return targets
		case operands[i] == "-o" && i+1 < len(operands):
			i++
			targets = append(targets, operands[i])
		}
	}
	return targets
}

// sedWriteTargets returns the files a sed invocation without an in-place flag writes: the
// filenames its scripts' `w` / `W` commands and `s///w` flags name (sedScriptWriteTargets),
// never its file operands, which it reads. A script sed reads from a file (`-f`) is not seen.
func sedWriteTargets(operands []string) []string {
	var targets []string
	for _, script := range sedScripts(operands) {
		targets = append(targets, sedScriptWriteTargets(script)...)
	}
	return targets
}

// sedScripts returns the scripts a sed option list runs: every `-e` / `--expression` value,
// or — when it has none and no `-f` / `--file` — its first non-option operand.
func sedScripts(operands []string) []string {
	var scripts []string
	firstOperand, hasOperand, hasScriptFile := "", false, false
	for i := 0; i < len(operands); i++ {
		w := operands[i]
		if w == "--" {
			if !hasOperand && i+1 < len(operands) {
				firstOperand, hasOperand = operands[i+1], true
			}
			break
		}
		option, value, needsNext := sedOption(w)
		if needsNext && i+1 < len(operands) {
			i++
			value = operands[i]
		}
		switch option {
		case "":
			if !hasOperand {
				firstOperand, hasOperand = w, true
			}
		case "e":
			scripts = append(scripts, value)
		case "f":
			hasScriptFile = true
		}
	}
	if len(scripts) > 0 || hasScriptFile || !hasOperand {
		return scripts
	}
	return []string{firstOperand}
}

// sedLongValueOptions maps sed's value-taking long options to their short letters.
var sedLongValueOptions = map[string]string{
	"--expression": "e", "--file": "f", "--line-length": "l",
}

// sedOption classifies one sed word: option "" for an operand, "e" / "f" / "l" for the
// value-taking `-e` (`--expression`), `-f` (`--file`) and `-l` (`--line-length`), and "-" for
// any other option. value is a value glued to the option (`-es/a/b/`, `--file=x`); needsNext
// says the value is the next word instead. In a short bundle (`-ne`) the first value letter
// takes the bundle's rest.
func sedOption(w string) (option, value string, needsNext bool) {
	if !strings.HasPrefix(w, "-") || w == "-" {
		return "", "", false
	}
	if strings.HasPrefix(w, "--") {
		name, rest, hasValue := strings.Cut(w, "=")
		short, takesValue := sedLongValueOptions[name]
		if !takesValue {
			return "-", "", false
		}
		return short, rest, !hasValue
	}
	letters := w[1:]
	at := strings.IndexAny(letters, "efl")
	if at < 0 {
		return "-", "", false
	}
	return letters[at : at+1], letters[at+1:], at == len(letters)-1
}

// sedScriptWriteTargets returns the filenames a sed script writes: the rest of the line after
// a `w` / `W` command or after an `s` command's `w` flag. Addresses, regexes, the `s` and `y`
// operands, labels and the text of `a` / `i` / `c` / `r` / `e` / `#` are skipped, so a `w`
// inside them (`/worktree/p`) is never read as a command.
func sedScriptWriteTargets(script string) []string {
	var targets []string
	runes := []rune(script)
	for i := skipSedAddresses(runes, 0); i < len(runes); i = skipSedAddresses(runes, i) {
		command := runes[i]
		i++
		switch command {
		case 'w', 'W':
			var file string
			file, i = sedRestOfLine(runes, i)
			targets = appendNonEmpty(targets, file)
		case 's':
			i = skipSedDelimited(runes, i, 2)
			var file string
			file, i = sedSubstituteFlags(runes, i)
			targets = appendNonEmpty(targets, file)
		case 'y':
			i = skipSedDelimited(runes, i, 2)
		case 'a', 'i', 'c', 'r', 'R', 'e', '#':
			_, i = sedRestOfLine(runes, i)
		case ':', 'b', 't', 'T':
			for i < len(runes) && runes[i] != ';' && runes[i] != '\n' {
				i++
			}
		}
	}
	return targets
}

// skipSedAddresses returns the index of the next sed command character at or after i,
// stepping over separators (blanks, `;`, `{`, `}`), negation and the addresses before a
// command: line numbers, `$`, `first~step`, `addr,+N`, and `/regex/` or `\cregexc`.
func skipSedAddresses(runes []rune, i int) int {
	for i < len(runes) {
		r := runes[i]
		switch {
		case unicode.IsSpace(r) || unicode.IsDigit(r) || strings.ContainsRune(";{}!,~+$", r):
			i++
		case r == '/':
			i = skipSedRegex(runes, i+1, '/')
		case r == '\\' && i+1 < len(runes):
			i = skipSedRegex(runes, i+2, runes[i+1])
		default:
			return i
		}
	}
	return i
}

// skipSedRegex returns the index just past the delimiter that closes a sed regex or
// replacement starting at i, honouring backslash escapes.
func skipSedRegex(runes []rune, i int, delimiter rune) int {
	for i < len(runes) && runes[i] != delimiter {
		if runes[i] == '\\' {
			i++
		}
		i++
	}
	return min(i+1, len(runes))
}

// skipSedDelimited steps over the delimiter at i and the parts of an `s` or `y` command it
// opens (`/re/repl/` is two parts), returning the index just past the last closing delimiter.
func skipSedDelimited(runes []rune, i, parts int) int {
	if i >= len(runes) {
		return i
	}
	delimiter := runes[i]
	i++
	for range parts {
		i = skipSedRegex(runes, i, delimiter)
	}
	return i
}

// sedSubstituteFlags reads the flags after an `s` command (`g`, `p`, a number, `i`, `m`, `e`,
// `w file`), returning the file a `w` flag names ("" for none) and the index past the flags.
func sedSubstituteFlags(runes []rune, i int) (string, int) {
	for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i])) {
		if runes[i] == 'w' {
			return sedRestOfLine(runes, i+1)
		}
		i++
	}
	return "", i
}

// sedRestOfLine returns the text from i to the end of the line, leading and trailing blanks
// trimmed, and the index of the newline that ends it; a backslash-escaped newline continues
// the text (`a\` then the appended line).
func sedRestOfLine(runes []rune, i int) (string, int) {
	start := i
	for i < len(runes) && runes[i] != '\n' {
		if runes[i] == '\\' {
			i++
		}
		i++
	}
	i = min(i, len(runes))
	return strings.TrimSpace(string(runes[start:i])), i
}

// appendNonEmpty appends w to words unless it is empty.
func appendNonEmpty(words []string, w string) []string {
	if w == "" {
		return words
	}
	return append(words, w)
}

// gitTargets judges a git command by its subcommand: a read verb contributes only the file an
// output option names (`git diff --output=file`; outputOptionValues), any other verb's operands (the global options before it included) are write targets. A `config`
// write of a command-valued key into the repository's own config additionally names
// `.git/config`, the file it lands in — its operands alone (`filter.x.clean cmd`) never spell
// the control-plane path the write-git-control-plane rule matches, so the approval would not
// show what the line really touches (the git tools' probe re-reads a changed config on its own).
func gitTargets(operands []string) []string {
	rest := operands
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
		// `-C <dir>` and `-c <k=v>` take a value; every other global option is one word.
		if (rest[0] == "-C" || rest[0] == "-c") && len(rest) > 1 {
			rest = rest[2:]
			continue
		}
		if strings.HasPrefix(rest[0], "--") && gitReadSubcommands[rest[0]] {
			return outputOptionValues("git", operands)
		}
		rest = rest[1:]
	}
	if len(rest) == 0 || gitReadSubcommands[rest[0]] {
		return outputOptionValues("git", operands)
	}
	targets := make([]string, 0, len(operands))
	for _, w := range operands {
		if w != rest[0] {
			targets = append(targets, w)
		}
	}
	targets = valueOperands(targets)
	if rest[0] == "config" && gitConfigWritesCommandKey(rest[1:]) {
		targets = append(targets, ".git/config")
	}
	return targets
}

// gitConfigWritesCommandKey reports whether the operands of a `git config` invocation write a
// command-valued key (GitCommandConfigName, matched on the lowered operand — git canonicalises
// the name's case) into the repository's own config: no operand directs the write at another
// file (gitConfigOtherFileOptions), none makes it a read (gitConfigReadForms).
func gitConfigWritesCommandKey(operands []string) bool {
	matched := false
	for _, w := range operands {
		option, _, _ := strings.Cut(w, "=")
		if gitConfigOtherFileOptions[option] || gitConfigReadForms[option] {
			return false
		}
		if GitCommandConfigName.MatchString(strings.ToLower(w)) {
			matched = true
		}
	}
	return matched
}

// ddTargets keeps only what dd writes: the values of its `of=` operands.
func ddTargets(operands []string) []string {
	var targets []string
	for _, w := range operands {
		if v, ok := strings.CutPrefix(w, "of="); ok {
			targets = append(targets, v)
		}
	}
	return targets
}

// hasInPlaceFlag reports whether a sed / perl option list edits its files in place: `-i`,
// `-i.bak`, a short bundle carrying `i` (`-pi`, `-ni`) or `--in-place`.
func hasInPlaceFlag(operands []string) bool {
	for _, w := range operands {
		switch {
		case w == "--":
			return false
		case strings.HasPrefix(w, "--in-place"):
			return true
		case strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--"):
			if strings.Contains(strings.TrimLeft(w, "-"), "i") {
				return true
			}
		}
	}
	return false
}

func hasAny(words []string, set map[string]bool) bool {
	for _, w := range words {
		if set[w] {
			return true
		}
	}
	return false
}

// stripWrappers drops a simple command's leading `NAME=value` assignments and wrapper
// leaders (with their bare options), so the wrapped leader comes first; it returns nothing
// for a command that is only assignments.
func stripWrappers(words []string) []string {
	words = stripAssignments(words)
	for len(words) > 0 && wrapperLeaders[path.Base(words[0])] {
		words = stripAssignments(stripBareOptions(words[1:]))
	}
	return words
}

// shellInterpreters are the leaders whose heredoc body, when it is the script they read on
// stdin, is shell the view reads for write targets (readsStdinScript).
var shellInterpreters = map[string]bool{
	"sh": true, "bash": true, "dash": true, "zsh": true, "ksh": true,
}

// interpreterLongOptionsTakingValue are the long options of the shell interpreters whose
// value is the next word, never a script-file operand.
var interpreterLongOptionsTakingValue = map[string]bool{
	"--rcfile": true, "--init-file": true, "--emulate": true,
}

// readsStdinScript reports whether a simple command, after wrapper and assignment stripping,
// is a shell interpreter (shellInterpreters) that reads its script from stdin: its operands
// are options only, or `-s` (whose following words are the script's arguments). A `-c`
// string or a script-file operand is the script instead, and the stdin a heredoc feeds is
// then the script's input, payload like any other. An `-o` / `+o` / `-O` / `+O` option and
// interpreterLongOptionsTakingValue consume the next word as their value.
func readsStdinScript(words []string) bool {
	words = stripWrappers(words)
	if len(words) == 0 || !shellInterpreters[path.Base(words[0])] {
		return false
	}
	operands := words[1:]
	for i := 0; i < len(operands); i++ {
		w := operands[i]
		switch {
		case w == "--" || w == "-":
			return i == len(operands)-1
		case strings.HasPrefix(w, "--"):
			if interpreterLongOptionsTakingValue[w] {
				i++
			}
		case len(w) > 1 && (w[0] == '-' || w[0] == '+'):
			flags := w[1:]
			if w[0] == '-' && strings.ContainsRune(flags, 'c') {
				return false
			}
			if w[0] == '-' && strings.ContainsRune(flags, 's') {
				return true
			}
			if last := flags[len(flags)-1]; last == 'o' || last == 'O' {
				i++
			}
		default:
			return false
		}
	}
	return true
}

// stripAssignments drops the leading `NAME=value` words of a simple command — environment
// for the leader, not operands of it.
func stripAssignments(words []string) []string {
	for len(words) > 0 && isAssignment(words[0]) {
		words = words[1:]
	}
	return words
}

// assignmentValues returns the `value` half of every `NAME=value` word in words.
func assignmentValues(words []string) []string {
	var values []string
	for _, w := range words {
		if isAssignment(w) {
			_, v, _ := strings.Cut(w, "=")
			values = append(values, v)
		}
	}
	return values
}

func isAssignment(w string) bool {
	name, _, ok := strings.Cut(w, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if !(r == '_' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r))) {
			return false
		}
	}
	return true
}

// stripBareOptions drops the leading option words (`-u`, `--login`) of a wrapper's operands.
func stripBareOptions(words []string) []string {
	for len(words) > 0 && strings.HasPrefix(words[0], "-") && words[0] != "-" {
		words = words[1:]
	}
	return words
}

// valueOperands returns the operands that can name a path: every word that is not a bare
// option, with an option's `=value` reduced to the value.
func valueOperands(operands []string) []string {
	targets := make([]string, 0, len(operands))
	for _, w := range operands {
		if strings.HasPrefix(w, "-") && w != "-" {
			if _, v, ok := strings.Cut(w, "="); ok && v != "" {
				targets = append(targets, v)
			}
			continue
		}
		targets = append(targets, w)
	}
	return targets
}
