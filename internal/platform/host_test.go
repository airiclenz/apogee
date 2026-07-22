package platform

import (
	"reflect"
	"strings"
	"testing"
)

// Every test in this file drives BOTH rule sets on whatever OS the suite runs on: the
// rules are pure data behind one implementation, so Windows semantics are asserted from
// Linux and macOS runs too (the injected-seam pattern the Phase-5 plan mandates). The
// native Windows behaviour — the OS long-path resolver and a real cmd.exe — is asserted
// separately in platform_windows_test.go.

func TestCommandWrapsLineInThePlatformShell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		rules hostRules
		want  []string
	}{
		{"posix", posixRules(), []string{"sh", "-c", "echo hi"}},
		{"windows", windowsRules(), []string{"cmd", "/c", "echo hi"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.rules.Command("echo hi"); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Command() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCommandDoesNotAliasTheRuleSetsShellSlice(t *testing.T) {
	t.Parallel()

	// Command appends to the shared shell prefix; if it appended in place, two calls
	// would fight over one backing array and the second line would overwrite the first.
	rules := windowsRules()
	first := rules.Command("one")
	second := rules.Command("two")
	if first[2] != "one" || second[2] != "two" {
		t.Fatalf("Command() aliased its rule set: got %q and %q", first, second)
	}
}

func TestCommandLineIsWindowsOnlyAndVerbatim(t *testing.T) {
	t.Parallel()

	const line = `echo "hello world" > "C:\pro be\out.txt"`

	if got := posixRules().CommandLine(line); got != "" {
		t.Errorf("posix CommandLine() = %q, want \"\" (execve takes a real argv)", got)
	}

	// The Windows command line must carry the shell prefix AND the line unaltered: the
	// whole point is that os/exec's argv escaping (\" for an embedded quote) never sees it.
	got := windowsRules().CommandLine(line)
	if want := `cmd /c ` + line; got != want {
		t.Errorf("windows CommandLine() = %q, want %q", got, want)
	}
	if strings.Contains(got, `\"`) {
		t.Errorf("windows CommandLine() escaped a quote (%q); cmd.exe does not understand \\\"", got)
	}
}

func TestQuoteIsLiteralForThePlatformShell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		rules hostRules
		arg   string
		want  string
	}{
		{"posix plain", posixRules(), "/tmp/work", `'/tmp/work'`},
		{"posix space", posixRules(), "/tmp/pro be/x", `'/tmp/pro be/x'`},
		{"posix quote", posixRules(), "it's", `'it'\''s'`},
		{"posix empty", posixRules(), "", `''`},
		{"windows plain", windowsRules(), `C:\Work`, `"C:\Work"`},
		{"windows space", windowsRules(), `C:\pro be\x.txt`, `"C:\pro be\x.txt"`},
		{"windows trailing backslash", windowsRules(), `C:\Work\`, `"C:\Work\\"`},
		{"windows empty", windowsRules(), "", `""`},
		// Only a backslash run that actually touches a quote is an escape run, and then
		// EVERY such run is doubled, however long — the closing quote is otherwise
		// swallowed as an escape and the argument runs on into the next one.
		{"windows multiple trailing backslashes", windowsRules(), `C:\Work\\`, `"C:\Work\\\\"`},
		// cmd metacharacters need no escape of their own while the token stays inside one
		// cmd-quoted region: &, |, ^ and > are not syntax there, so they survive literally.
		{"windows cmd metacharacters", windowsRules(), `a & b | c ^ d > e`, `"a & b | c ^ d > e"`},
		// PINNED NON-GUARANTEE (host.go, windowsQuote's doc comment): %VAR% is expanded by
		// cmd.exe before either parser sees the line and has no in-line escape, so Quote
		// passes it through untouched. Apogee's callers quote filesystem paths; a caller
		// quoting untrusted text is quoting a value cmd may still expand. If this row ever
		// needs to change, the doc comment and every caller's threat model change with it.
		{"windows env var is not neutralised", windowsRules(), `%PATH%`, `"%PATH%"`},

		// ADVERSARIAL: a value that contains a quote of its own. The \" that
		// CommandLineToArgvW needs for a literal quote is still a quote to cmd, which would
		// toggle out of its quoted region and read the remainder of the token as syntax, so
		// the whole token is caret-escaped instead and cmd never enters quote mode. Each
		// `want` below is the string that was verified to round-trip byte-for-byte through
		// a real `cmd /c` launch — see TestWindowsQuoteRoundTripsThroughCmd, which re-runs
		// exactly these values natively.
		{"windows quote", windowsRules(), `say "hi"`, `^"say \^"hi\^"^"`},
		// A backslash run that ends AT an embedded quote is an escape run like any other
		// and is doubled: one backslash becomes two, so the child reads one back. Leaving
		// it single would let it escape the quote and eat the backslash outright.
		{"windows backslash before an embedded quote", windowsRules(), `a\"b`, `^"a\\\^"b^"`},
		{"windows backslash run before an embedded quote", windowsRules(), `a\\"b`, `^"a\\\\\^"b^"`},
		// Backslashes AFTER an embedded quote are the trailing run, doubled as usual — the
		// two rules are about position, not about which one fired first.
		{"windows trailing backslashes after a quote", windowsRules(), `say "hi"\\`, `^"say \^"hi\^"\\\\^"`},
		// The reason the caret branch exists: a value carrying both a quote and an & would
		// otherwise hand cmd a live command separator halfway through the argument.
		{"windows quote and metacharacter", windowsRules(), `a"b & c"d`, `^"a\^"b ^& c\^"d^"`},
		{"windows quote and redirect", windowsRules(), `x">"y`, `^"x\^"^>\^"y^"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.rules.Quote(tt.arg); got != tt.want {
				t.Errorf("Quote(%q) = %s, want %s", tt.arg, got, tt.want)
			}
		})
	}
}

func TestScopeEnvKeepsTheCallersAllowlistAndAddsThePlatformFloor(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"PATH":       "/bin",
		"Path":       `C:\Windows`,
		"HOME":       "/home/u",
		"SystemRoot": `C:\WINDOWS`,
		"ComSpec":    `C:\WINDOWS\system32\cmd.exe`,
		"PATHEXT":    ".COM;.EXE",
	}
	lookup := func(key string) (string, bool) { value, ok := env[key]; return value, ok }

	t.Run("posix adds nothing and drops absent keys", func(t *testing.T) {
		t.Parallel()
		got := posixRules().ScopeEnv([]string{"PATH", "HOME", "ABSENT"}, lookup)
		want := []string{"PATH=/bin", "HOME=/home/u"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ScopeEnv() = %q, want %q", got, want)
		}
	})

	t.Run("windows appends its essentials after the allowlist", func(t *testing.T) {
		t.Parallel()
		got := windowsRules().ScopeEnv([]string{"PATH", "HOME"}, lookup)
		if len(got) < 3 || got[0] != "PATH=/bin" || got[1] != "HOME=/home/u" {
			t.Fatalf("ScopeEnv() = %q, want the allowlist first, in order", got)
		}
		for _, want := range []string{`SystemRoot=C:\WINDOWS`, `ComSpec=C:\WINDOWS\system32\cmd.exe`, "PATHEXT=.COM;.EXE"} {
			if !contains(got, want) {
				t.Errorf("ScopeEnv() = %q, missing the Windows essential %q", got, want)
			}
		}
	})

	t.Run("windows folds duplicate names", func(t *testing.T) {
		t.Parallel()
		// PATH and Path are one variable on Windows; emitting both would let the second
		// silently win in the child.
		got := windowsRules().ScopeEnv([]string{"PATH", "Path"}, lookup)
		if n := countPrefix(got, "PATH="); n != 1 {
			t.Errorf("ScopeEnv() = %q, want exactly one PATH entry, got %d", got, n)
		}
		if countPrefix(got, "Path=") != 0 {
			t.Errorf("ScopeEnv() = %q, want the caller's first spelling to win", got)
		}
	})

	t.Run("posix keeps distinct names distinct", func(t *testing.T) {
		t.Parallel()
		// The same two names are two variables on POSIX — folding them there would be
		// the bug, not the fix.
		got := posixRules().ScopeEnv([]string{"PATH", "Path"}, lookup)
		want := []string{"PATH=/bin", `Path=C:\Windows`}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ScopeEnv() = %q, want %q", got, want)
		}
	})
}

func TestScopeEnvDefaultsToTheProcessEnvironment(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process state.
	t.Setenv("APOGEE_SCOPEENV_PROBE", "set")
	got := posixRules().ScopeEnv([]string{"APOGEE_SCOPEENV_PROBE"}, nil)
	if len(got) != 1 || got[0] != "APOGEE_SCOPEENV_PROBE=set" {
		t.Errorf("ScopeEnv(nil lookup) = %q, want the process environment to be read", got)
	}
}

func TestContainsPOSIX(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		root, target string
		want         bool
	}{
		{"identical", "/work", "/work", true},
		{"child", "/work", "/work/src/main.go", true},
		{"trailing separator", "/work/", "/work/src", true},
		{"sibling with shared prefix", "/work", "/work2/src", false},
		{"parent", "/work/src", "/work", false},
		{"case differs and POSIX is exact", "/Work", "/work/src", false},
		{"dot segments normalise", "/work/./src/..", "/work/src", true},
		{"escape via dotdot", "/work", "/work/../etc/passwd", false},
		{"relative target, absolute root", "/work", "work/src", false},
		{"relative pair", "work", "work/src", true},
		{"empty root contains nothing", "", "/work", false},
		{"root of the filesystem", "/", "/work", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := posixRules().Contains(tt.root, tt.target); got != tt.want {
				t.Errorf("Contains(%q, %q) = %v, want %v", tt.root, tt.target, got, tt.want)
			}
		})
	}
}

func TestContainsWindows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		root, target string
		want         bool
	}{
		{"identical", `C:\Work`, `C:\Work`, true},
		{"child", `C:\Work`, `C:\Work\src\main.go`, true},
		{"case-folded root", `C:\Work`, `c:\work\src`, true},
		{"case-folded drive only", `c:\work`, `C:\WORK`, true},
		{"short vs long case collision", `C:\Work`, `C:\WORK2\src`, false},
		{"sibling with shared prefix", `C:\Work`, `C:\Work2`, false},
		{"forward slashes normalise", `C:/Work`, `C:\Work\src`, true},
		{"trailing separator", `C:\Work\`, `C:\Work\src`, true},
		{"long-path prefix on the target", `C:\Work`, `\\?\C:\Work\src`, true},
		{"long-path prefix on the root", `\\?\C:\Work`, `C:\Work\src`, true},
		{"dot segments normalise", `C:\Work\.\src\..`, `C:\Work\src`, true},
		{"escape via dotdot", `C:\Work`, `C:\Work\..\Windows`, false},
		{"different drive", `C:\Work`, `D:\Work\src`, false},
		{"UNC share", `\\server\share\work`, `\\server\share\work\src`, true},
		{"UNC long-path spelling", `\\?\UNC\server\share\work`, `\\server\share\work\src`, true},
		{"different UNC share", `\\server\share\work`, `\\server\other\work\src`, false},
		{"drive-relative path is not a location", `C:Work`, `C:Work\src`, false},
		{"device path is refused", `\\.\PhysicalDrive0`, `\\.\PhysicalDrive0\x`, false},
		{"empty root contains nothing", "", `C:\Work`, false},
		{"drive root", `C:\`, `C:\Work`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := windowsRules().Contains(tt.root, tt.target); got != tt.want {
				t.Errorf("Contains(%q, %q) = %v, want %v", tt.root, tt.target, got, tt.want)
			}
		})
	}
}

func TestContainsRefusesUnresolvableShortNames(t *testing.T) {
	t.Parallel()

	// An 8.3 short name cannot be compared lexically: PROGRA~1 is "Program Files" on one
	// machine and "Program Files (x86)" on the next. With no resolver wired (the pure
	// rule set, and every non-Windows host) Contains refuses rather than guessing — it
	// must never SILENTLY mismatch, because one of its two callers is the guardrail that
	// refuses to relabel %ProgramFiles% (ADR 0020 §2/§6).
	pure := windowsRules()
	if pure.Contains(`C:\Program Files`, `C:\PROGRA~1\Go`) {
		t.Error("Contains matched an unresolved 8.3 short name, want refusal")
	}
	if pure.Contains(`C:\PROGRA~1`, `C:\PROGRA~1\Go`) {
		t.Error("Contains compared two unresolved 8.3 short names, want refusal")
	}

	// A tilde is not an alias: an ordinary long name that happens to contain one stays
	// comparable, or every path with a "~" in it would be refused.
	if !pure.Contains(`C:\Work`, `C:\Work\my~file.txt`) {
		t.Error(`Contains("C:\Work", "C:\Work\my~file.txt") = false, want true (not an 8.3 name)`)
	}
	if !pure.Contains(`C:\Work~ing`, `C:\work~ing\src`) {
		t.Error(`Contains("C:\Work~ing", "C:\work~ing\src") = false, want true (not an 8.3 name)`)
	}

	// With a resolver wired (what Current does on Windows) the short name normalises and
	// the containment is answered honestly.
	resolved := windowsRules()
	resolved.longPath = func(p string) string {
		return strings.ReplaceAll(p, `PROGRA~1`, `Program Files`)
	}
	if !resolved.Contains(`C:\Program Files`, `C:\PROGRA~1\Go`) {
		t.Error(`Contains("C:\Program Files", "C:\PROGRA~1\Go") with a resolver = false, want true`)
	}
	if resolved.Contains(`C:\Program Files`, `C:\PROGRA~2\Go`) {
		t.Error("Contains matched a short name the resolver could not expand, want refusal")
	}
}

// contains reports whether entries holds want.
func contains(entries []string, want string) bool {
	for _, entry := range entries {
		if entry == want {
			return true
		}
	}
	return false
}

// countPrefix counts the entries beginning with prefix.
func countPrefix(entries []string, prefix string) int {
	n := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry, prefix) {
			n++
		}
	}
	return n
}
