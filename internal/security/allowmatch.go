package security

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// AllowSite is where a `terminal` command line would run, as the Allow-rule matcher needs to
// know it: the workspace a `cd` must stay inside, the call's own workdir and whether the line
// goes to a POSIX shell at all.
type AllowSite struct {
	// Root is the workspace root. A `cd` matches only when it lands, symlinks resolved, in an
	// existing directory inside it; an empty Root lets no `cd` match.
	Root string
	// Workdir is the call's workdir argument as the model wrote it — relative to Root or
	// absolute, "" for Root itself. The line's first `cd` is resolved from it, exactly where the
	// terminal tool would run the line (resolveWorkdirInRoot).
	Workdir string
	// IsPOSIXShell reports whether the line is handed to a POSIX shell. A shell apogee has no
	// reader for (Windows cmd.exe) never matches a rule: the matcher cannot split its line.
	IsPOSIXShell bool
}

// MatchAllowRules reports whether every simple command of a `terminal` command line is covered
// by one of the word-prefix Allow rules (ADR 0096 §2). A rule is a run of whole words — `go test`
// covers `go test ./...` and never `gotest` — compared literally: no wildcards, no leader
// basename. The line must first pass an allowlist (scanMatchableShellLine): simple commands joined
// by `;`, `&&`, `||` or `|`, built only of plain words, single-quoted literals, double-quoted
// strings with no `$`, backtick or backslash, and bare `$NAME` / `${NAME}`, no word empty once
// its quotes are removed, and read by the tokenizer as the very same words. Anything else — a
// substitution, any redirect or heredoc, a lone `&`, a subshell, a brace, a glob, `~`, `#`, a
// newline, a backslash, a non-ASCII byte — asks, since the matcher cannot vouch for reading it as
// the shell does. The words are then split by the shell write view's tokenizer
// (splitSimpleCommands), and the line asks on a wrapper leader (allowWrapperLeaders), a
// reserved-word leader (allowReservedWords), a shell-builtin leader other than the short safe
// list (allowAskBuiltins; the safe ones are `cd`, `.`, `source`, `echo`, `pwd`, `true`,
// `false`) or a leading assignment (`NAME=value`, `NAME+=value`), since each runs or writes
// something no rule word names — `trap 'cmd' EXIT`, `test -v 'a[$(cmd)]'`. A `cd` is matched by
// no rule but read strictly (allowedCdDestination): it counts as covered only when it sits in
// the leading run of `cd`s at the very start of the line, takes one plain relative operand with
// no `..`, is joined to the next command by `&&` and nothing else (so a failed `cd` stops the
// line, and no earlier command's failure can route the shell past it), and lands — every
// symlink resolved, from the call's workdir or the previous `cd` — in an existing directory
// inside Site.Root. Any other `cd` (`cd`, `cd -`, `cd -P x`, `cd $DIR`, `cd ..`, `cd x; …`,
// `cd x || …`, a trailing `cd`, a `cd` after any other command) makes the line ask. A
// line of nothing but `cd`s matches no rule and asks too, so an answer always names the rule
// that gave it.
//
// used lists the indices into rules of the rules that covered the line, in first-use order and
// once each; it is nil whenever isAllowed is false.
func MatchAllowRules(command string, rules []string, site AllowSite) (used []int, isAllowed bool) {
	commands, ok := allowCandidates(command, site)
	if !ok {
		return nil, false
	}
	ruleWords := make([][]string, len(rules))
	for i, rule := range rules {
		ruleWords[i] = strings.Fields(rule)
	}
	seen := make(map[int]bool, len(rules))
	for _, words := range commands {
		index := firstCoveringRule(words, ruleWords)
		if index < 0 {
			return nil, false
		}
		if !seen[index] {
			seen[index] = true
			used = append(used, index)
		}
	}
	return used, true
}

// SuggestAllowRules proposes the rules that would let a `terminal` command line through, one
// per distinct simple command in first-use order, for the approval pane's editable line: the
// leader plus the next word when that word is plain (allowSuggestionOperand) — `go test ./...`
// suggests `go test`, `ls -la` suggests `ls`. An interpreter (allowInterpreters) takes its script
// path instead — `python3 scripts/x.py` suggests itself — and one running no script file
// (`python3 -c …`, `bash -s`) suggests nothing, since its bare name would allow any program.
// An in-workspace `cd` needs no rule, and a line led by an unsafe builtin (`test`, `trap`,
// `read`) suggests nothing, since no rule can let it through. The answer is nil when the line could never match
// (MatchAllowRules' reasons) or holds a command no literal rule could name; every rule it does
// return covers its line.
func SuggestAllowRules(command string, site AllowSite) []string {
	commands, ok := allowCandidates(command, site)
	if !ok {
		return nil
	}
	var suggestions []string
	seen := make(map[string]bool, len(commands))
	for _, words := range commands {
		rule, ok := suggestRule(words)
		if !ok {
			return nil
		}
		if !seen[rule] {
			seen[rule] = true
			suggestions = append(suggestions, rule)
		}
	}
	return suggestions
}

// allowWrapperLeaders extend wrapperLeaders — the leaders that run a command their operands
// name — with the ones only the Allow-rule matcher needs to know: a wrapper puts a program
// no rule word names in front of the one a rule would vouch for, so its line always asks.
var allowWrapperLeaders = map[string]bool{
	"timeout": true, "stdbuf": true, "doas": true, "su": true, "runuser": true,
	"pkexec": true, "ionice": true, "chrt": true, "taskset": true, "setsid": true,
	"unbuffer": true, "flock": true, "chroot": true, "nsenter": true, "unshare": true,
	"watch": true, "strace": true, "ltrace": true, "systemd-run": true, "xvfb-run": true,
	"script": true, "caffeinate": true, "firejail": true, "busybox": true,
	"parallel": true, "eval": true,
}

// allowInterpreters are the leaders whose suggested rule names the script they run rather
// than a subcommand: the interpreter alone would allow any program at all. `.` and `source`
// run a script in the shell itself, so `. ./x.sh` suggests itself, never a bare `.`.
var allowInterpreters = map[string]bool{
	"python": true, "python3": true, "node": true, "bash": true, "sh": true, "zsh": true,
	"ruby": true, "perl": true, "deno": true, "bun": true, ".": true, "source": true,
}

// allowReservedWords are the shell's reserved words: as a leader the view reads one as the
// command, where the shell reads it as grammar and runs the word after it (`then rm -rf ~`,
// `! rm -rf ~`), so a line led by one always asks. `time` is a wrapper already.
var allowReservedWords = map[string]bool{
	"!": true, "[[": true, "]]": true, "case": true, "coproc": true, "do": true, "done": true,
	"elif": true, "else": true, "esac": true, "fi": true, "for": true, "function": true,
	"if": true, "in": true, "select": true, "then": true, "until": true, "while": true,
}

// allowAskBuiltins are the shell builtins — bash's, POSIX sh's, dash's, busybox ash's and the
// common ksh and zsh ones — that a line may not be led by, whatever rule names them: a builtin
// runs inside the shell itself, and many run code their operands only hint at — `trap` and `fc`
// run a string, `eval` and `command` a command, and `test -v`, `printf -v`, `read`, `declare`,
// `let`, `mapfile -C` and the rest evaluate an array subscript (`a[$(cmd)]`) or a callback as
// code — while `pushd`, `popd`, `exec` and `set` change where or how the rest of the line runs.
// Only a short, explicit safe list of builtins is left off: `cd`, read strictly
// (allowedCdDestination) and never matched by a rule; `.` and `source`, which run the script
// their rule names (allowInterpreters); and `echo`, `pwd`, `true` and `false`, which run no
// code, assign nothing and touch nothing beyond their own output. A builtin missing from this
// set would be matched like any program, so a doubtful one belongs in it. A builtin is matched
// by its bare name, as the shell looks it up: `/usr/bin/test` is a program.
var allowAskBuiltins = map[string]bool{
	":": true, "[": true, "alias": true, "autoload": true, "bg": true, "bind": true,
	"break": true, "builtin": true, "caller": true, "chdir": true, "command": true,
	"compgen": true, "complete": true, "compopt": true, "continue": true, "declare": true,
	"dirs": true, "disown": true, "emulate": true, "enable": true, "eval": true, "exec": true,
	"exit": true, "export": true, "fc": true, "fg": true, "float": true, "functions": true,
	"getopts": true, "hash": true, "help": true, "history": true, "integer": true, "jobs": true,
	"kill": true, "let": true, "local": true, "logout": true, "mapfile": true, "nameref": true,
	"noglob": true, "popd": true, "print": true, "printf": true, "pushd": true, "read": true,
	"readarray": true, "readonly": true, "return": true, "set": true, "setopt": true,
	"shift": true, "shopt": true, "suspend": true, "test": true, "times": true, "trap": true,
	"type": true, "typeset": true, "ulimit": true, "umask": true, "unalias": true,
	"unset": true, "unsetopt": true, "wait": true, "whence": true, "zmodload": true,
}

// shellExpansionChars are the characters that make a word mean something other than its own
// spelling once the shell expands it; a suggested rule never carries one.
const shellExpansionChars = "$*?[]{}~"

// allowCandidates splits command into the word lists of its simple commands that a rule must
// cover — every command but a vouched-for `cd` — and reports false when the line can never
// match a rule: a non-POSIX shell, a construct outside the allowlist (scanMatchableShellLine), a
// line the tokenizer splits into other commands or words than the allowlist read, no
// command at all, a wrapper, reserved-word, unsafe-builtin (allowAskBuiltins) or assignment
// leader, a `cd` outside the line's leading run of `cd`s or one the strict reading
// (allowedCdDestination) cannot vouch for, or nothing left once the `cd`s are set aside. Since each `cd` of that leading run must be joined
// to the next command by `&&`, every separator from the start of the line up to and including
// the one after the last `cd` is `&&`: the `cd`s either all succeed in turn or the line stops.
func allowCandidates(command string, site AllowSite) ([][]string, bool) {
	if !site.IsPOSIXShell {
		return nil, false
	}
	scanned, separators, isMatchable := scanMatchableShellLine(command)
	if !isMatchable {
		return nil, false
	}
	commands := splitSimpleCommands(command, workDir{})
	if !isSameWordList(commands, scanned) {
		return nil, false // the tokenizer read the line differently from the allowlist
	}
	var candidates [][]string
	var cdDir string          // the physical directory the line's `cd`s have reached; "" before the first
	isPastLeadingCds := false // a command other than `cd` has been seen: no `cd` may follow it
	for i, cmd := range commands {
		if len(cmd.words) == 0 {
			return nil, false
		}
		leader := cmd.words[0]
		if isPrefixAssignment(leader) || allowReservedWords[leader] || allowAskBuiltins[leader] ||
			wrapperLeaders[path.Base(leader)] || allowWrapperLeaders[path.Base(leader)] {
			return nil, false
		}
		if leader == "cd" {
			if isPastLeadingCds {
				return nil, false // `go vet || cd x && …`: which directory the rest runs in depends on go vet
			}
			isJoinedByAnd := i < len(separators) && separators[i] == "&&" && i+1 < len(commands)
			if !isJoinedByAnd {
				return nil, false
			}
			dest, ok := allowedCdDestination(cmd.words, cdDir, site)
			if !ok {
				return nil, false
			}
			cdDir = dest
			continue
		}
		isPastLeadingCds = true
		candidates = append(candidates, cmd.words)
	}
	return candidates, len(candidates) > 0
}

// allowedCdDestination resolves the `cd` whose words are words from the physical directory
// from ("" for the call's own workdir) and reports where it lands — or false when the matcher
// cannot vouch for it. It reads `cd` strictly rather than lexically, since bash follows
// symlinks and CDPATH where a lexical join does not: exactly one operand and no option (`-P`,
// `-L`, `-`, `--`); no `..` component, no absolute path, no `~`, no `$`, `:` or `\`, nothing
// empty; and the operand, joined onto from and resolved through every symlink, must be an
// existing directory inside the resolved Site.Root.
func allowedCdDestination(words []string, from string, site AllowSite) (string, bool) {
	if len(words) != 2 || site.Root == "" {
		return "", false
	}
	operand := words[1]
	if operand == "" || strings.HasPrefix(operand, "-") || strings.HasPrefix(operand, "~") ||
		strings.ContainsAny(operand, `$:\`) || path.IsAbs(operand) || filepath.IsAbs(operand) {
		return "", false
	}
	for _, part := range strings.Split(operand, "/") {
		if part == ".." {
			return "", false
		}
	}
	realRoot, err := filepath.EvalSymlinks(site.Root)
	if err != nil {
		return "", false
	}
	if from == "" {
		start, err := ResolveInRoot(site.Workdir, site.Root)
		if err != nil || !isDirectoryInRoot(start, realRoot) {
			return "", false
		}
		from = start
	}
	dest, err := filepath.EvalSymlinks(filepath.Join(from, filepath.FromSlash(operand)))
	if err != nil || !isDirectoryInRoot(dest, realRoot) {
		return "", false
	}
	return dest, true
}

// isSameWordList reports whether the tokenizer's simple commands hold exactly the words the
// allowlist scan read, command for command and word for word. The allowlist decides which
// constructs are matchable while the tokenizer supplies the words a rule is held against, so a
// line on which the two disagree — in the count of commands or words, or in a word's value —
// is one the matcher cannot vouch for and asks.
func isSameWordList(commands []simpleCommand, scanned [][]string) bool {
	if len(commands) != len(scanned) {
		return false
	}
	for i, cmd := range commands {
		if !slices.Equal(cmd.words, scanned[i]) {
			return false
		}
	}
	return true
}

// isDirectoryInRoot reports whether the symlink-resolved path dir is an existing directory at
// or under the symlink-resolved realRoot.
func isDirectoryInRoot(dir, realRoot string) bool {
	if dir != realRoot && !strings.HasPrefix(dir, realRoot+string(filepath.Separator)) {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// isPrefixAssignment reports whether w would be read by the shell as an assignment in front of
// a command: `NAME=value` (isAssignment) and bash's `NAME+=value` and `NAME[i]=value` forms,
// which isAssignment does not know. It errs towards yes — any word whose leading name is cut by
// `=`, `+` or `[` and that holds an `=` — since a yes only makes the line ask.
func isPrefixAssignment(w string) bool {
	end := strings.IndexAny(w, "=+[")
	return end > 0 && strings.Contains(w[end:], "=") && isAssignment(w[:end]+"=")
}

// firstCoveringRule returns the index of the first rule whose words are a whole-word prefix of
// words, or -1. A rule with no words covers nothing.
func firstCoveringRule(words []string, ruleWords [][]string) int {
	for i, rule := range ruleWords {
		if len(rule) == 0 || len(rule) > len(words) {
			continue
		}
		isPrefix := true
		for j, w := range rule {
			if words[j] != w {
				isPrefix = false
				break
			}
		}
		if isPrefix {
			return i
		}
	}
	return -1
}

// suggestRule proposes the rule for one simple command (SuggestAllowRules), reporting false
// when no literal rule could name it.
func suggestRule(words []string) (string, bool) {
	leader := words[0]
	if !isLiteralRuleWord(leader) {
		return "", false
	}
	if allowInterpreters[path.Base(leader)] {
		if len(words) < 2 || strings.HasPrefix(words[1], "-") || !isLiteralRuleWord(words[1]) {
			return "", false
		}
		return leader + " " + words[1], true
	}
	if len(words) > 1 && allowSuggestionOperand(words[1]) {
		return leader + " " + words[1], true
	}
	return leader, true
}

// allowSuggestionOperand reports whether the word after a leader is plain enough to join its
// suggested rule — a subcommand such as `test` in `go test`, not an option (`-la`) or a path
// (`./...`).
func allowSuggestionOperand(w string) bool {
	return isLiteralRuleWord(w) && !strings.HasPrefix(w, "-") && !strings.Contains(w, "/")
}

// isLiteralRuleWord reports whether w can stand in a rule as written: non-empty, free of
// whitespace and control characters (a rule is split on whitespace) and of every character the
// shell would expand (shellExpansionChars).
func isLiteralRuleWord(w string) bool {
	if w == "" || strings.ContainsAny(w, shellExpansionChars) {
		return false
	}
	return strings.IndexFunc(w, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

// allowWordPunctuation is the punctuation an unquoted word may hold besides ASCII letters and
// digits (scanMatchableShellLine). None of it means anything to sh or bash outside the constructs
// the matcher already refuses: `~` (tilde expansion), the glob characters `*?[]`, `#`, `!`,
// braces and `^` are left out on purpose, so a word holding one asks.
const allowWordPunctuation = "_-./,:=+@%"

// scanMatchableShellLine reports whether every character of command sits in a construct the
// shell write view's tokenizer reads exactly as POSIX sh and bash (`bash --posix` included)
// would — an allowlist, so a construct it does not name makes the line ask. A matchable line is
// one or more simple commands joined by `;`, `&&`, `||` or `|` (a trailing `;` allowed), each a
// run of words separated by spaces and tabs, and each word a concatenation of:
//   - unquoted ASCII letters, digits and allowWordPunctuation;
//   - a '…' literal of printable ASCII (no newline);
//   - a "…" string of printable ASCII holding no `$`, backtick or backslash;
//   - a bare `$NAME` or `${NAME}`, NAME being `[A-Za-z_][A-Za-z0-9_]*`;
//
// and no word may be empty once its quotes are removed — a pair of single or double quotes with
// nothing between them, alone or run together: the shell passes an empty word on as an argument
// (a `cd` to it stays put; as a leader it is a command not found) where the tokenizer drops it,
// so the two would read different word lists.
//
// Everything else asks: every other `$` form (`$(`, `$((`, `$'`, `$"`, `$[`, `$1`, `$@`, and a
// `${…}` holding anything beyond a plain NAME — `${a[_]}`, `${PWD:_}`, `${!_}`, `${_@P}` are
// evaluated as code), backslashes, backticks, every redirect, a lone `&`, parentheses, braces,
// globs, `~`, `#`, newlines, control and non-ASCII bytes, and a separator with no command on
// either side of it. The tokenizer's own reading of anything outside this set is never trusted.
//
// words lists the words of each simple command as the scan read them, quotes removed and
// `$NAME` / `${NAME}` kept as written — the list the tokenizer must reproduce (isSameWordList);
// separators lists the line's separators in order (`;`, `&&`, `||`, `|`), the one after the
// i-th simple command at index i. Both are nil whenever isMatchable is false.
func scanMatchableShellLine(command string) (words [][]string, separators []string, isMatchable bool) {
	expectCommand := true // a simple command must follow: at the start and after `&&`, `||`, `|`
	var current []string  // the words of the simple command in progress
	for i := 0; i < len(command); {
		switch c := command[i]; {
		case c == ' ' || c == '\t':
			i++
		case c == ';':
			if len(current) == 0 {
				return nil, nil, false
			}
			words, current = append(words, current), nil
			separators = append(separators, ";")
			expectCommand = false
			i++
		case c == '|' || c == '&':
			width := 1
			if i+1 < len(command) && command[i+1] == c {
				width = 2
			}
			if len(current) == 0 || (c == '&' && width == 1) {
				return nil, nil, false
			}
			words, current = append(words, current), nil
			separators = append(separators, command[i:i+width])
			expectCommand = true
			i += width
		default:
			end, word, ok := scanMatchableWord(command, i)
			if !ok {
				return nil, nil, false
			}
			current = append(current, word)
			expectCommand = false
			i = end
		}
	}
	if expectCommand {
		return nil, nil, false
	}
	if len(current) > 0 {
		words = append(words, current)
	}
	return words, separators, true
}

// scanMatchableWord walks the word starting at command[start] up to the space, tab or
// separator that ends it, returning that index, the word with its quotes removed, and whether
// every part of the word is one scanMatchableShellLine allows and the word is not empty.
func scanMatchableWord(command string, start int) (end int, word string, ok bool) {
	var value strings.Builder
	i := start
	for i < len(command) && !strings.ContainsRune(" \t;|&", rune(command[i])) {
		c := command[i]
		switch {
		case isASCIIAlphanumeric(c) || strings.IndexByte(allowWordPunctuation, c) >= 0:
			value.WriteByte(c)
			i++
		case c == '\'':
			closing := strings.IndexByte(command[i+1:], '\'')
			if closing < 0 || !isQuotableText(command[i+1:i+1+closing], "") {
				return i, "", false
			}
			value.WriteString(command[i+1 : i+1+closing])
			i += closing + 2
		case c == '"':
			closing := strings.IndexByte(command[i+1:], '"')
			if closing < 0 || !isQuotableText(command[i+1:i+1+closing], "$`\\") {
				return i, "", false
			}
			value.WriteString(command[i+1 : i+1+closing])
			i += closing + 2
		case c == '$':
			next, isPlain := scanPlainParameter(command, i)
			if !isPlain {
				return i, "", false
			}
			value.WriteString(command[i:next])
			i = next
		default:
			return i, "", false
		}
	}
	if value.Len() == 0 {
		return i, "", false // `''`, `""`: the shell keeps an empty word the tokenizer drops
	}
	return i, value.String(), true
}

// scanPlainParameter reads the `$NAME` or `${NAME}` whose `$` stands at command[start],
// returning the index just past it and false for any other `$` form.
func scanPlainParameter(command string, start int) (next int, isPlain bool) {
	i := start + 1
	isBraced := i < len(command) && command[i] == '{'
	if isBraced {
		i++
	}
	nameStart := i
	for i < len(command) && (command[i] == '_' || isASCIIAlphanumeric(command[i])) {
		i++
	}
	if i == nameStart || (command[nameStart] >= '0' && command[nameStart] <= '9') {
		return start, false
	}
	if !isBraced {
		return i, true
	}
	if i == len(command) || command[i] != '}' {
		return start, false
	}
	return i + 1, true
}

// isQuotableText reports whether a quoted body is printable ASCII — no newline, control or
// non-ASCII byte — free of every byte in forbidden.
func isQuotableText(body, forbidden string) bool {
	for i := 0; i < len(body); i++ {
		if body[i] < ' ' || body[i] > '~' || strings.IndexByte(forbidden, body[i]) >= 0 {
			return false
		}
	}
	return true
}

// isASCIIAlphanumeric reports whether c is an ASCII letter or digit.
func isASCIIAlphanumeric(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
