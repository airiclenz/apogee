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
//     file, a `git` read subcommand, `find` without `-delete` / `-exec`, `sed` and `perl`
//     without an in-place flag, and the like (readLeaders): its operands are what it reads,
//   - only the `of=` values for `dd`,
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
// redirect carries, not a command.
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
	heredocs []string // delimiters whose bodies follow the next newline, in order
	// flushAfter holds the commands a substitution nested in the current command; they
	// are emitted right after it so a reader sees the host before its guests.
	flushAfter []simpleCommand
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
// can be skipped at the next newline, and `<>` opens read-write and counts as a write.
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
// (a newline) until each delimiter appears on a line of its own, in order. Returns the
// index of the last rune consumed.
func (tk *shellTokenizer) skipHeredocBodies(runes []rune, i int) int {
	for _, delimiter := range tk.heredocs {
		for i+1 < len(runes) {
			end := i + 1
			for end < len(runes) && runes[end] != '\n' {
				end++
			}
			line := strings.TrimSpace(string(runes[i+1 : end]))
			i = end
			if line == delimiter {
				break
			}
		}
	}
	tk.heredocs = nil
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
		tk.heredocs = append(tk.heredocs, w)
	case tk.pending == pendingSkip:
	default:
		tk.current.words = append(tk.current.words, w)
	}
	tk.pending = pendingOperand
}

// endCommand closes the simple command in progress, keeping it only when it has words or
// redirect targets, followed by any substitutions it hosted.
func (tk *shellTokenizer) endCommand() {
	tk.endWord()
	tk.pending = pendingOperand
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
// their simple command contributes nothing beyond its redirect targets. Membership is a
// claim that the program writes to no file its operands name, on any option — so `sort`
// (`-o`) and `awk` (a program may redirect) are deliberately absent and fail closed. So are
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
	words = stripAssignments(words)
	for len(words) > 0 && wrapperLeaders[path.Base(words[0])] {
		words = stripAssignments(stripBareOptions(words[1:]))
	}
	if len(words) == 0 {
		return assignmentValues(all)
	}
	leader, operands := path.Base(words[0]), words[1:]
	switch {
	case leader == "git":
		return gitTargets(operands)
	case leader == "dd":
		return ddTargets(operands)
	case leader == "find":
		if !hasAny(operands, findWritingPredicates) {
			return nil
		}
	case leader == "sed":
		if !hasInPlaceFlag(operands) {
			return nil
		}
	case leader == "perl":
		if !hasInPlaceFlag(operands) {
			return valueOperands(operands) // arbitrary code: fail closed like an unknown leader
		}
	case readLeaders[leader]:
		return nil
	}
	return valueOperands(operands)
}

// gitTargets judges a git command by its subcommand: a read verb contributes nothing, any
// other verb's operands (the global options before it included) are write targets. A `config`
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
			return nil
		}
		rest = rest[1:]
	}
	if len(rest) == 0 || gitReadSubcommands[rest[0]] {
		return nil
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
