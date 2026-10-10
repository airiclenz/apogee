package security

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// allowTestWorkspace builds a workspace for the `cd` rows: real directories `internal/tui`,
// `sub/deeper` and `foo`, a file `notes.txt`, and `link`, a symlink to a directory outside the workspace that has a
// `foo` beside it — so `link/../foo` is ws/foo to bash's logical `cd` and outside/foo to the
// kernel.
func allowTestWorkspace(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "ws")
	for _, dir := range []string{
		filepath.Join(root, "internal", "tui"),
		filepath.Join(root, "sub", "deeper"),
		filepath.Join(root, "foo"),
		filepath.Join(base, "outside", "in"),
		filepath.Join(base, "outside", "foo"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "outside", "in"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestMatchAllowRules pins the ratified word-prefix matching call (ADR 0096 §2): every simple
// command must be covered, whole words only, and every shell feature that runs or writes
// something no rule word names makes the line ask. The first block is the grill's table.
func TestMatchAllowRules(t *testing.T) {
	t.Parallel()

	root := allowTestWorkspace(t)
	goTest := []string{"go test"}
	rmBuild := []string{"rm -rf build"}
	cases := []struct {
		name      string
		command   string
		rules     []string
		workdir   string
		wantUsed  []int
		isAllowed bool
	}{
		// The grill's table.
		{"a covered command", "go test ./...", goTest, "", []int{0}, true},
		{"a chain needs both rules: one is not enough", "go test ./... && go vet ./...", goTest, "", nil, false},
		{"a chain needs both rules: both cover it", "go test ./... && go vet ./...", []string{"go test", "go vet"}, "", []int{0, 1}, true},
		{"a pipe into an uncovered command", "go test ./... | tee out.txt", goTest, "", nil, false},
		{"a command substitution", "go test $(cat pkgs)", goTest, "", nil, false},
		{"an output redirect", "go test ./... > out", goTest, "", nil, false},
		{"whole words only", "gotest ./...", []string{"go"}, "", nil, false},
		{"a sudo wrapper, even when a rule names it", "sudo go test ./...", []string{"go test", "sudo"}, "", nil, false},
		{"an env wrapper, even when a rule names it", "env go test ./...", []string{"go test", "env"}, "", nil, false},
		{"an assignment prefix", "GOFLAGS=-v go test ./...", goTest, "", nil, false},
		{"a cd inside the workspace", "cd internal/tui && go test ./...", goTest, "", []int{0}, true},
		{"a cd out of the workspace", "cd /tmp && go test ./...", goTest, "", nil, false},

		// The other shell features that ask.
		{"a backtick substitution", "go test `cat pkgs`", goTest, "", nil, false},
		{"a substitution inside double quotes", `go test "$(cat pkgs)"`, goTest, "", nil, false},
		{"a substitution hidden in a parameter default", "go test ${P:-$(cat pkgs)}", goTest, "", nil, false},
		{"a double-quoted brace in a parameter default", `go test ${x:-"}"}; rm -rf ~`, goTest, "", nil, false},
		{"a single-quoted brace in a parameter default", "go test ${x:-'}'}; rm -rf ~", goTest, "", nil, false},
		{"a quoted brace in a parameter default inside double quotes", `go test "${x:-"}"}; rm -rf ~"`, goTest, "", nil, false},
		{"an escaped brace in a parameter default", `go test ${x:-\}}; rm -rf ~`, goTest, "", nil, false},
		{"a nested parameter expansion", "go test ${x:-${y}}", goTest, "", nil, false},
		{"an unclosed parameter expansion", "go test ${x", goTest, "", nil, false},
		{"an ANSI-C quoted string", "go test $'\\''\nrm -rf ~\necho '", goTest, "", nil, false},
		{"an ANSI-C quoted string inside double quotes", `go test "$'x'"`, goTest, "", nil, false},
		{"an arithmetic expansion", "go test -count=$((1+1)) ./...", goTest, "", nil, false},
		{"a heredoc", "go test ./... <<EOF\nx\nEOF", goTest, "", nil, false},
		{"a here-string", "go test ./... <<< x", goTest, "", nil, false},
		{"an fd dup", "go test ./... 2>&1", goTest, "", nil, false},
		{"an input redirect", "go test ./... < in", goTest, "", nil, false},
		{"a process substitution", "go test <(cat pkgs)", goTest, "", nil, false},
		{"an unclosed single quote", "go test 'x", goTest, "", nil, false},
		{"an unclosed double quote", `go test "x`, goTest, "", nil, false},
		{"a trailing escape", `go test \`, goTest, "", nil, false},
		{"an unclosed subshell", "(go test ./...", goTest, "", nil, false},
		{"a stray close paren", "go test ./...)", goTest, "", nil, false},
		{"a timeout wrapper", "timeout 60 go test ./...", []string{"go test", "timeout"}, "", nil, false},
		{"a wrapper spelled as a path", "/usr/bin/sudo go test", []string{"/usr/bin/sudo"}, "", nil, false},
		{"an append assignment prefix", "GOFLAGS+=-v go test ./...", []string{"go test", "GOFLAGS+=-v go test"}, "", nil, false},
		{"a subscripted assignment prefix", "a[0]=x go test ./...", []string{"go test", "a[0]=x go test"}, "", nil, false},
		{"a reserved word leads the command", "! go test ./...", []string{"! go test"}, "", nil, false},
		{"reserved words hide the command they run", "if go test ./...; then rm -rf x; fi", []string{"if go test", "then rm", "fi"}, "", nil, false},

		// Builtins that run code their rule does not name: every probe line asks.
		{"a trap runs its string on exit", "trap 'touch p' EXIT; go test ./...", []string{"trap", "go test"}, "", nil, false},
		{"test -v evaluates a subscript as code", "test -v 'a[$(touch p)]'", []string{"test"}, "", nil, false},
		{"printf -v evaluates a subscript as code", "printf -v 'a[$(touch p)]' x", []string{"printf"}, "", nil, false},
		{"read evaluates a subscript as code", "echo x | read 'a[$(touch p)]'", []string{"echo", "read"}, "", nil, false},
		{"declare evaluates a subscript as code", "declare 'a[$(touch p)]=x'", []string{"declare"}, "", nil, false},
		{"mapfile runs its callback", "echo x | mapfile -C 'touch p' -c 1 a", []string{"echo", "mapfile"}, "", nil, false},
		{"a subscript built at run time", `printf -v b 'c[\x24(touch p)]'; test -v $b`, []string{"printf", "test"}, "", nil, false},
		{"a quoted builtin is still the builtin", `"test" -v x`, []string{"test"}, "", nil, false},

		// The safe builtins still match.
		{"echo", "echo hello", []string{"echo"}, "", []int{0}, true},
		{"pwd", "pwd -P", []string{"pwd"}, "", []int{0}, true},
		{"true and false", "true && false || go test ./...", []string{"true", "false", "go test"}, "", []int{0, 1, 2}, true},
		{"a dot-source the rule names", ". ./x.sh", []string{". ./x.sh"}, "", []int{0}, true},
		{"a builtin's name spelled as a path is a program", "/usr/bin/test -e x", []string{"/usr/bin/test"}, "", []int{0}, true},

		// Where sh would cut, join or quote the line differently from the view.
		{"a vertical tab is no word break", "go test ./...\v#$(touch pwned)", goTest, "", nil, false},
		{"a form feed is no word break", "go test ./...\f#$(touch pwned)", goTest, "", nil, false},
		{"a carriage return is no word break", "go test ./...\r#$(touch pwned)", goTest, "", nil, false},
		{"a no-break space is no word break", "go test ./...\u00a0#$(touch pwned)", goTest, "", nil, false},
		{"a next-line rune is no word break", "go test ./...\u0085#$(touch pwned)", goTest, "", nil, false},
		{"a control rune that is not whitespace", "go test ./...\x1b", goTest, "", nil, false},
		{"a line continuation joins what the view splits", "go test \\\n  ./...", goTest, "", nil, false},
		{"an empty single-quoted word the view drops", "go '' test", goTest, "", nil, false},
		{"a lone empty double-quoted word the view drops", `go test "" ./...`, goTest, "", nil, false},
		{"an empty double-quoted command the view drops", `"" && go test ./...`, goTest, "", nil, false},
		{"an empty word of two empty quotes the view drops", `go test ''"" ./...`, goTest, "", nil, false},
		{"a trailing empty word the view drops", "go test ./... ''", goTest, "", nil, false},
		{"a backslash the shell keeps inside double quotes", `"g\o" test ./...`, goTest, "", nil, false},
		{"a line continuation inside double quotes", "go test \"a\\\nb\"", goTest, "", nil, false},
		{"bash's locale quoting", `$"go" test ./...`, []string{"go test", "$go test"}, "", nil, false},
		{"bash's old arithmetic", "go test -count=$[1+1] ./...", goTest, "", nil, false},
		{"bash's old arithmetic inside double quotes", `go test "$[1+1]"`, goTest, "", nil, false},
		{"an arithmetic command", "((x)) && go test ./...", []string{"go test", "x"}, "", nil, false},

		// The allowlist: whatever it does not name asks, however harmless.
		{"a subscripted parameter is evaluated as code", "go test ${a[_]}", goTest, "", nil, false},
		{"a substring offset is evaluated as code", "go test ${PWD:_}", goTest, "", nil, false},
		{"an indirect parameter", "go test ${!_}", goTest, "", nil, false},
		{"a prompt-expanded parameter", "go test ${_@P}", goTest, "", nil, false},
		{"a subscripted length is evaluated as code", "go test ${#a[_]}", goTest, "", nil, false},
		{"a parameter with a default", "go test ${TAGS:-unit}", goTest, "", nil, false},
		{"a parameter inside double quotes", `go test "$TAGS"`, goTest, "", nil, false},
		{"a positional parameter", "go test $1", goTest, "", nil, false},
		{"a special parameter", "go test $@", goTest, "", nil, false},
		{"a braced positional parameter", "go test ${1}", goTest, "", nil, false},
		{"a bare dollar", "go test $", goTest, "", nil, false},
		{"a comment", "go test ./... # it's $(fine) > here", goTest, "", nil, false},
		{"a hash inside a word", "go test ./a#b", goTest, "", nil, false},
		{"a newline", "go test ./a\ngo test ./b", goTest, "", nil, false},
		{"a newline inside single quotes", "go test -run 'a\nb'", goTest, "", nil, false},
		{"an escape the shell honours inside double quotes", "go test -run \"a\\\"b\"", goTest, "", nil, false},
		{"an escape outside quotes", `go test a\ b`, goTest, "", nil, false},
		{"nested subshells spaced apart", "( (go test ./...) )", goTest, "", nil, false},
		{"a brace group", "{ go test ./...; }", goTest, "", nil, false},
		{"a brace expansion", "go test ./{a,b}", goTest, "", nil, false},
		{"a glob", "go test ./*", goTest, "", nil, false},
		{"a question-mark glob", "go test ./?", goTest, "", nil, false},
		{"a bracket glob", "go test ./[ab]", goTest, "", nil, false},
		{"a tilde", "go test ~/x", goTest, "", nil, false},
		{"a non-ASCII letter", "go test ./caf\u00e9", goTest, "", nil, false},
		{"a non-ASCII letter inside quotes", "go test 'caf\u00e9'", goTest, "", nil, false},
		{"a tab inside quotes", "go test 'a\tb'", goTest, "", nil, false},
		{"a pipe of stderr", "go test ./... |& cat", []string{"go test", "cat"}, "", nil, false},
		{"a leading separator", "; go test ./...", goTest, "", nil, false},
		{"a doubled semicolon", "go test ./a;; go test ./b", goTest, "", nil, false},
		{"a trailing and", "go test ./... &&", goTest, "", nil, false},
		{"a separator with nothing between", "go test ./a && || go test ./b", goTest, "", nil, false},
		{"a triple pipe", "go test ./a ||| go test ./b", goTest, "", nil, false},

		// Ordinary commands still match.
		{"go test", "go test ./...", goTest, "", []int{0}, true},
		{"git status", "git status", []string{"git status"}, "", []int{0}, true},
		{"npm run build", "npm run build", []string{"npm run"}, "", []int{0}, true},
		{"make check", "make check", []string{"make"}, "", []int{0}, true},
		{"a single-quoted run pattern", "go test -run 'TestX' ./pkg/", goTest, "", []int{0}, true},
		{"a double-quoted message", `git commit -m "fix: the bug, again!"`, []string{"git commit"}, "", []int{0}, true},
		{"punctuation an unquoted word may hold", "go test -count=1 -tags=a,b ./x_y-z.go:1 +v @r %p", goTest, "", []int{0}, true},
		{"a braced plain parameter", "go test ${PKG}/... ./...", goTest, "", []int{0}, true},
		{"a pipe and an or", "go test ./... | cat || go vet ./...", []string{"go test", "cat", "go vet"}, "", []int{0, 1, 2}, true},
		{"a trailing semicolon", "go test ./...;", goTest, "", []int{0}, true},

		// What is inert, and how words compare.
		{"single-quoted text is literal", "echo '$(x) > y'", []string{"echo"}, "", []int{0}, true},
		{"redirect characters inside double quotes are literal", `echo "a > b"`, []string{"echo"}, "", []int{0}, true},
		{"a plain parameter expansion", "go test ${PKG} $TAGS ./...", goTest, "", []int{0}, true},
		{"a tab is a word break", "go test\t./...", goTest, "", []int{0}, true},
		{"quotes are removed before comparing", `go "test" ./...`, goTest, "", []int{0}, true},
		{"a semicolon chain, rules used once each", "go test ./a; go vet ./b; go test ./c", []string{"go vet", "go test"}, "", []int{1, 0}, true},
		{"a background job is a command of its own", "go test ./... & rm -rf x", goTest, "", nil, false},
		{"a rule longer than the command", "go", goTest, "", nil, false},
		{"an empty rule covers nothing", "go test ./...", []string{"", "   "}, "", nil, false},
		{"no rules", "go test ./...", nil, "", nil, false},
		{"an empty line", "", goTest, "", nil, false},
		{"a leader spelled as a path is another word", "/usr/local/go/bin/go test ./...", goTest, "", nil, false},

		// cd and the call's workdir.
		{"only cds: no rule answers", "cd internal", goTest, "", nil, false},
		{"a bare cd goes home", "cd && go test ./...", goTest, "", nil, false},
		{"cd - is the previous directory", "cd - && go test ./...", goTest, "", nil, false},
		{"cd ~ is home", "cd ~ && go test ./...", goTest, "", nil, false},
		{"a cd through a variable", "cd $D && go test ./...", goTest, "", nil, false},
		{"a cd that climbs out of the root", "cd .. && go test ./...", goTest, "", nil, false},
		{"a cd inside a subshell that leaves", "(cd /tmp && go test ./...)", goTest, "", nil, false},
		{"an absolute cd, even back into the root", "cd " + filepath.ToSlash(root) + "/internal && go test ./...", goTest, "", nil, false},
		{"a cd from the workdir that stays in", "cd tui && go test ./...", goTest, "internal", []int{0}, true},
		{"a cd up from the workdir climbs with ..", "cd .. && go test ./...", goTest, "internal", nil, false},
		{"a cd that leaves the root via the workdir", "cd ../../p/ws && go test ./...", goTest, "a", nil, false},
		{"chained cds resolve from each other", "cd internal && cd ../.. && go test ./...", goTest, "", nil, false},
		{"a workdir outside the root", "cd x && go test ./...", goTest, "../elsewhere", nil, false},
		{"a workdir that does not exist", "cd x && go test ./...", goTest, "nope", nil, false},

		// cd, read strictly: one plain relative operand, joined by && to the next command,
		// landing in an existing workspace directory once every symlink is resolved.
		{"a cd into a real directory", "cd sub && rm -rf build", rmBuild, "", []int{0}, true},
		{"a dotted cd into a real directory", "cd ./sub && rm -rf build", rmBuild, "", []int{0}, true},
		{"chained cds from each resolved directory", "cd sub && cd deeper && rm -rf build", rmBuild, "", []int{0}, true},
		{"a cd into a missing directory", "cd missing && rm -rf build", rmBuild, "", nil, false},
		{"a cd joined by a semicolon", "cd sub; rm -rf build", rmBuild, "", nil, false},
		{"a cd joined by an or", "cd sub || rm -rf build", rmBuild, "", nil, false},
		{"a cd piped on", "cd sub | rm -rf build", rmBuild, "", nil, false},
		{"a cd that ends the line", "rm -rf build && cd sub", rmBuild, "", nil, false},
		{"a cd followed by a trailing semicolon", "rm -rf build && cd sub;", rmBuild, "", nil, false},
		{"a cd through a symlink that points outside", "cd link && rm -rf build", rmBuild, "", nil, false},
		{"a failed cd then a climb, semicolons", "cd nope/x; cd ../..; rm -rf build", rmBuild, "", nil, false},
		{"a failed cd or a climb", "cd nope/x || cd ../.. && rm -rf build", rmBuild, "", nil, false},
		{"a physical cd up out of a symlink", "cd -P link/.. && rm -rf build", rmBuild, "", nil, false},
		{"a logical climb through a symlink", "cd link/../foo && rm -rf build", rmBuild, "", nil, false},
		{"a cd with an option", "cd -L sub && rm -rf build", rmBuild, "", nil, false},
		{"a cd with an end of options", "cd -- sub && rm -rf build", rmBuild, "", nil, false},
		{"a cd with two operands", "cd sub deeper && rm -rf build", rmBuild, "", nil, false},
		{"a cd to an empty operand", "cd '' && rm -rf build", rmBuild, "", nil, false},
		{"a cd to a quoted tilde", "cd '~' && rm -rf build", rmBuild, "", nil, false},
		{"a cd onto a file", "cd notes.txt && rm -rf build", rmBuild, "", nil, false},
		{"a program named cd", "./cd sub && rm -rf build", rmBuild, "", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			site := AllowSite{Root: root, Workdir: tc.workdir, IsPOSIXShell: true}
			used, isAllowed := MatchAllowRules(tc.command, tc.rules, site)
			if isAllowed != tc.isAllowed || !reflect.DeepEqual(used, tc.wantUsed) {
				t.Errorf("MatchAllowRules(%q, %q, workdir %q) = %v, %v; want %v, %v",
					tc.command, tc.rules, tc.workdir, used, isAllowed, tc.wantUsed, tc.isAllowed)
			}
		})
	}
}

// TestMatchAllowRulesCdsOnlyLeadTheLine pins that a `cd` counts as covered only in the leading
// run of `cd`s at the very start of the line, every separator up to and including the one after
// the last `cd` being `&&`: a `cd` after any other command lands where that command's exit status
// sends the shell. The workspace has real directories ws/x and ws/x/y and a symlink ws/y that
// points outside, so a `cd y` resolved from the wrong directory would leave the root.
func TestMatchAllowRulesCdsOnlyLeadTheLine(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	root := filepath.Join(base, "ws")
	for _, dir := range []string{filepath.Join(root, "x", "y"), filepath.Join(base, "outside")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(base, "outside"), filepath.Join(root, "y")); err != nil {
		t.Fatal(err)
	}
	rules := []string{"go"}
	cases := []struct {
		command   string
		wantUsed  []int
		isAllowed bool
	}{
		{"cd x && go test", []int{0}, true},
		{"cd x && cd y && go test", []int{0}, true},
		{"cd '' x && cd y && go test", nil, false}, // sh runs `cd ''`, stays put, and follows the symlink y
		{"cd x '' && cd y && go test", nil, false},
		{"go vet || cd x && cd y && go test", nil, false},
		{"go version | cd x && cd y && go test", nil, false},
		{"cd x && go vet || cd y && go test", nil, false},
		{"go vet && cd x && go test", nil, false},
		{"go vet; cd x && go test", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			t.Parallel()
			site := AllowSite{Root: root, IsPOSIXShell: true}
			used, isAllowed := MatchAllowRules(tc.command, rules, site)
			if isAllowed != tc.isAllowed || !reflect.DeepEqual(used, tc.wantUsed) {
				t.Errorf("MatchAllowRules(%q, %q) = %v, %v; want %v, %v",
					tc.command, rules, used, isAllowed, tc.wantUsed, tc.isAllowed)
			}
		})
	}
}

// allowTestAskLeaders are the builtins and reserved words that must never lead a line a rule
// answers — the verifier's list, kept apart from allowAskBuiltins so the set cannot shrink
// silently.
var allowTestAskLeaders = []string{
	"trap", "let", "mapfile", "readarray", "declare", "typeset", "local", "readonly", "export",
	"read", "unset", "printf", "test", "[", "[[", "eval", "exec", "command", "builtin", "enable",
	"alias", "unalias", "set", "shopt", "hash", "getopts", "bind", "complete", "compgen", "fc",
	"history", "ulimit", "umask", "wait", "kill", "jobs", "fg", "bg", "disown", "suspend",
	"times", "type", "caller", "coproc", "select", "function", "pushd", "popd", ":",
}

// TestMatchAllowRulesAskOnUnsafeBuiltins pins that a line led by a shell builtin or reserved
// word outside the safe list (`cd`, `.`, `source`, `echo`, `pwd`, `true`, `false`) asks, even
// when a rule names it exactly, plain or quoted, alone or behind a covered command — and that
// no such line gets a suggested rule.
func TestMatchAllowRulesAskOnUnsafeBuiltins(t *testing.T) {
	t.Parallel()

	leaders := slices.Clone(allowTestAskLeaders)
	for name := range allowAskBuiltins {
		leaders = append(leaders, name)
	}
	site := AllowSite{Root: t.TempDir(), IsPOSIXShell: true}
	for _, leader := range leaders {
		for _, command := range []string{
			leader + " x",
			"'" + leader + "' x",
			"go test ./... && '" + leader + "' x",
		} {
			rules := []string{leader, leader + " x", "go test"}
			if used, isAllowed := MatchAllowRules(command, rules, site); isAllowed || used != nil {
				t.Errorf("MatchAllowRules(%q, %q) = %v, %v; want nil, false", command, rules, used, isAllowed)
			}
			if got := SuggestAllowRules(command, site); got != nil {
				t.Errorf("SuggestAllowRules(%q) = %q; want nil", command, got)
			}
		}
	}
}

// TestMatchAllowRulesNeverOnANonPOSIXShell pins that a shell apogee has no reader for (cmd.exe)
// is never answered by a rule, however plainly the line matches.
func TestMatchAllowRulesNeverOnANonPOSIXShell(t *testing.T) {
	t.Parallel()

	site := AllowSite{Root: t.TempDir(), IsPOSIXShell: false}
	if used, isAllowed := MatchAllowRules("go test ./...", []string{"go test"}, site); isAllowed || used != nil {
		t.Errorf("MatchAllowRules on a non-POSIX shell = %v, %v; want nil, false", used, isAllowed)
	}
	if got := SuggestAllowRules("go test ./...", site); got != nil {
		t.Errorf("SuggestAllowRules on a non-POSIX shell = %q; want nil", got)
	}
}

// TestSuggestAllowRules pins the approval pane's suggested rules: the leader plus a plain next
// word, an interpreter's script path, and nothing for a line no rule could ever let through.
// Every suggestion it makes must cover its own line.
func TestSuggestAllowRules(t *testing.T) {
	t.Parallel()

	root := allowTestWorkspace(t)
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		// The grill's rows.
		{"a subcommand joins the leader", "go test ./...", []string{"go test"}},
		{"an interpreter takes its script path", "python3 scripts/x.py", []string{"python3 scripts/x.py"}},
		{"an option is not a subcommand", "ls -la", []string{"ls"}},
		{"a substitution line suggests nothing", "go test $(cat pkgs)", nil},
		{"a redirect line suggests nothing", "go test ./... > out", nil},

		// The rest of the shape.
		{"a path is not a subcommand", "cat ./notes.txt", []string{"cat"}},
		{"a bare leader", "make", []string{"make"}},
		{"one rule per distinct command", "go build ./... && go vet ./...", []string{"go build", "go vet"}},
		{"a repeated command suggests once", "go test ./a && go test ./b", []string{"go test"}},
		{"an in-workspace cd needs no rule", "cd internal/tui && go test ./...", []string{"go test"}},
		{"a cd out of the workspace suggests nothing", "cd /tmp && go test ./...", nil},
		{"a wrapper suggests nothing", "sudo ls", nil},
		{"an assignment prefix suggests nothing", "GOFLAGS=-v go test ./...", nil},
		{"an interpreter with no script suggests nothing", "python3 -c 'print(1)'", nil},
		{"a bare interpreter suggests nothing", "bash", nil},
		{"an expanded leader suggests nothing", "$GO test", nil},
		{"an expanded next word stays off the rule", "echo $HOME", []string{"echo"}},
		{"a quoted word with a space stays off the rule", `git commit "a b"`, []string{"git commit"}},
		{"a quoted leader with a space suggests nothing", `"my tool" run`, nil},
		{"only cds suggest nothing", "cd internal", nil},
		{"a carriage return line suggests nothing", "go test ./...\r#$(touch pwned)", nil},
		{"a reserved-word leader suggests nothing", "! go test ./...", nil},
		{"an append assignment suggests nothing", "GOFLAGS+=-v go test ./...", nil},
		{"a dot-source takes its script path", ". ./x.sh", []string{". ./x.sh"}},
		{"source takes its script path", "source scripts/env.sh", []string{"source scripts/env.sh"}},
		{"a bare dot suggests nothing", ".", nil},
		{"test suggests nothing", "test -v x", nil},
		{"trap suggests nothing", "trap 'touch p' EXIT; go test ./...", nil},
		{"read suggests nothing", "echo x | read y", nil},
		{"printf suggests nothing", "printf '%s' x", nil},
		{"echo is safe", "echo hello", []string{"echo hello"}},
		{"pwd is safe", "pwd", []string{"pwd"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			site := AllowSite{Root: root, IsPOSIXShell: true}
			got := SuggestAllowRules(tc.command, site)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SuggestAllowRules(%q) = %q; want %q", tc.command, got, tc.want)
			}
			if got == nil {
				return
			}
			if _, isAllowed := MatchAllowRules(tc.command, got, site); !isAllowed {
				t.Errorf("MatchAllowRules(%q, %q) = false; a suggestion must cover its own line", tc.command, got)
			}
		})
	}
}
