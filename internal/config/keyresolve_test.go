package config

// The key resolver's suite. Its `api-key-cmd:` fixtures are THIS test binary re-invoked behind a
// sentinel argument (the fork-and-exec idiom internal/mcp and internal/platform already use): a real
// process, a real argv, real exit codes and real streams, on every OS `go test` runs on and with no
// shell script to keep executable in testdata.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/security"
)

// keyFixtureSentinel is the first argument that turns a run of this test binary into a key-source
// command instead of a test run.
const keyFixtureSentinel = "__apogee_key_fixture"

// TestMain dispatches the sentinel to the fixture command, and otherwise runs the suite.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == keyFixtureSentinel {
		os.Exit(runKeyFixture(os.Args[2:]))
	}
	os.Exit(m.Run())
}

// runKeyFixture is the in-child half of the fixture: one mode per behaviour a real key command can
// have. `count` is the one with state — it appends a byte to a tally file per run, which is how the
// caching tests count how many times the command actually ran, and sleeps first when given a delay
// so concurrent callers are still inside one run when the next arrives.
func runKeyFixture(args []string) int {
	mode := ""
	if len(args) > 0 {
		mode = args[0]
	}
	switch mode {
	case "print": // print <key>
		fmt.Println(argAt(args, 1))
	case "fail": // fail <stderr text>
		fmt.Fprintln(os.Stderr, argAt(args, 1))
		return 3
	case "blank": // print only whitespace
		fmt.Print(" \t\n\n")
	case "hang": // never answer
		time.Sleep(10 * time.Second)
	case "flood": // print far more than a key
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("k"), maxKeyCommandOutput+1024))
	case "count": // count <tally file> <key> [sleep ms]
		if delay, err := strconv.Atoi(argAt(args, 3)); err == nil && delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
		if err := tallyRun(argAt(args, 1)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Println(argAt(args, 2))
	case "countfail": // countfail <tally file> <stderr text>
		if err := tallyRun(argAt(args, 1)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Fprintln(os.Stderr, argAt(args, 2))
		return 3
	default:
		fmt.Fprintf(os.Stderr, "key fixture: no such mode %q\n", mode)
		return 2
	}
	return 0
}

// argAt is the fixture's out-of-range-tolerant argv read: a mode called with too few arguments is a
// defect in the test, and reporting it as an empty argument beats panicking inside a subprocess
// whose output nobody prints.
func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

// tallyRun records one run of the fixture. O_APPEND makes the one-byte write atomic across the
// concurrent processes the single-flight test would otherwise be counting unreliably.
func tallyRun(path string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte("x")); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// fixtureCommand renders an `api-key-cmd:` line that runs this test binary in the named fixture
// mode. Every word is single-quoted, so a Windows path's backslashes and a temp directory's spaces
// survive the POSIX split the resolver puts the line through.
func fixtureCommand(t *testing.T, args ...string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return fixtureCommandAt(exe, args...)
}

// fixtureCommandAt is fixtureCommand for a program somewhere other than this binary's own path —
// the planted copies the exec fence's tests resolve, whose whole point is WHERE they sit.
func fixtureCommandAt(program string, args ...string) string {
	words := make([]string, 0, len(args)+2)
	words = append(words, quoteFixtureArg(program), keyFixtureSentinel)
	for _, arg := range args {
		words = append(words, quoteFixtureArg(arg))
	}
	return strings.Join(words, " ")
}

// plantKeyCommand copies THIS test binary to dir/name and returns its absolute path — a real,
// runnable `api-key-cmd:` program sitting wherever the caller puts it. A copy rather than a symlink
// because the fence resolves symlinks before judging (security.EvalRealPath), so a link inside the
// workspace pointing at a binary outside it is not the collision the refusal exists for. Being this
// binary, the copy answers the fixture sentinel like every other command here, which is what lets a
// refusal test prove the program never ran.
func plantKeyCommand(t *testing.T, dir, name string) string {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("reading this test binary: %v", err)
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the planted command's directory: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatalf("planting the key command: %v", err)
	}
	return path
}

// quoteFixtureArg wraps one argument in single quotes, POSIX-style.
func quoteFixtureArg(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// fixtureRuns is how many times the fixture has run against this tally file.
func fixtureRuns(t *testing.T, tally string) int {
	t.Helper()
	data, err := os.ReadFile(tally)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatalf("read the fixture tally: %v", err)
	}
	return len(data)
}

// tallyPath is a fresh tally file path in the test's own temp directory.
func tallyPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

// A literal key and a keyless entry are answered without running anything — and an entry carrying a
// literal answers with it whatever else it names, which is what makes the ADR 0036 decision 6
// overlay (APOGEE_API_KEY onto the startup entry) beat that entry's own command source.
func TestKeyResolverAnswersLiteralAndKeylessEntriesDirectly(t *testing.T) {
	t.Parallel()

	tally := tallyPath(t, "runs")
	resolver := NewKeyResolver("")

	if got, err := resolver.Resolve(ServerEntry{Name: "rented", APIKey: "sk-literal"}); err != nil || got != "sk-literal" {
		t.Errorf("literal source resolved to (%q, %v), want the key as written", got, err)
	}
	if got, err := resolver.Resolve(ServerEntry{Name: "localbox"}); err != nil || got != "" {
		t.Errorf("keyless entry resolved to (%q, %v), want the empty key and no error", got, err)
	}
	overlaid := ServerEntry{
		Name:      "rented",
		APIKey:    "sk-overlay",
		APIKeyCmd: fixtureCommand(t, "count", tally, "sk-from-command"),
	}
	if got, err := resolver.Resolve(overlaid); err != nil || got != "sk-overlay" {
		t.Errorf("overlaid entry resolved to (%q, %v), want the literal that overlays the command", got, err)
	}
	if runs := fixtureRuns(t, tally); runs != 0 {
		t.Errorf("the command ran %d times; an entry holding a literal key must never run one", runs)
	}
}

// The variable an `api-key-env:` names answers for the entry — and unset or empty is a refusal
// naming both the entry and the variable, never a silent keyless server.
func TestKeyResolverEnvSource(t *testing.T) {
	// Not parallel: t.Setenv owns the process environment for the duration.
	const (
		hit     = "APOGEE_TEST_KEY_HIT"
		blank   = "APOGEE_TEST_KEY_BLANK"
		missing = "APOGEE_TEST_KEY_MISSING"
	)
	t.Setenv(hit, "sk-from-env")
	t.Setenv(blank, "   ")
	// t.Setenv restores the original (absent) value at cleanup; unsetting after it is how the test
	// gets a reliably unset variable without leaking one.
	t.Setenv(missing, "")
	if err := os.Unsetenv(missing); err != nil {
		t.Fatalf("unset %s: %v", missing, err)
	}

	tests := []struct {
		name     string
		variable string
		wantKey  string
		wantErr  []string
	}{
		{"a set variable is the key", hit, "sk-from-env", nil},
		{"an unset variable refuses", missing, "", []string{"openrouter", missing, "not set"}},
		{"an empty variable refuses", blank, "", []string{"openrouter", blank, "empty"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewKeyResolver("").Resolve(ServerEntry{Name: "openrouter", APIKeyEnv: tt.variable})
			assertResolved(t, got, err, tt.wantKey, tt.wantErr)
		})
	}
}

// Every way an `api-key-cmd:` can answer, and the refusal each broken one earns.
func TestKeyResolverCommandSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		timeout time.Duration
		wantKey string
		wantErr []string
	}{
		{
			name:    "the output is the key, without its trailing newline",
			command: fixtureCommand(t, "print", "sk-cmd"),
			wantKey: "sk-cmd",
		},
		{
			name:    "a non-zero exit refuses and quotes what the command said",
			command: fixtureCommand(t, "fail", "the specified item could not be found in the keychain"),
			wantErr: []string{"openrouter", "could not be found in the keychain"},
		},
		{
			name:    "printing only whitespace is a broken source, not a keyless server",
			command: fixtureCommand(t, "blank"),
			wantErr: []string{"openrouter", "printed nothing"},
		},
		{
			name:    "a command that never answers is cut off",
			command: fixtureCommand(t, "hang"),
			timeout: 100 * time.Millisecond,
			wantErr: []string{"openrouter", "did not answer"},
		},
		{
			name:    "output far past a key's length refuses instead of being held",
			command: fixtureCommand(t, "flood"),
			wantErr: []string{"openrouter", "printed more than"},
		},
		{
			name:    "an unbalanced quote refuses before anything runs",
			command: `security find-generic-password -s 'apogee`,
			wantErr: []string{"openrouter", "cannot be read as a command line"},
		},
		{
			name:    "a line naming no program refuses",
			command: "   ",
			wantErr: []string{"openrouter", "names no program"},
		},
		{
			name:    "a program that is not on PATH refuses",
			command: "apogee-no-such-key-command",
			wantErr: []string{"openrouter", "is not on this machine's PATH"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resolver := &KeyResolver{commandTimeout: tt.timeout}
			got, err := resolver.Resolve(ServerEntry{Name: "openrouter", APIKeyCmd: tt.command})
			assertResolved(t, got, err, tt.wantKey, tt.wantErr)
		})
	}
}

// The `api-key-cmd:` program is resolved and FENCED before it runs, like every other exec apogee
// performs: a command whose argv[0] lands inside the workspace is a program the model could have
// written, and running it would hand the model the credential this key source exists to protect.
func TestKeyResolverRefusesACommandProgramInsideTheWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	planted := plantKeyCommand(t, filepath.Join(root, "node_modules", ".bin"), "getkey")
	tally := tallyPath(t, "runs")

	resolver := NewKeyResolver(root)
	got, err := resolver.Resolve(ServerEntry{
		Name:      "openrouter",
		APIKeyCmd: fixtureCommandAt(planted, "count", tally, "sk-planted"),
	})
	if err == nil {
		t.Fatalf("the planted command answered with %q, want a refusal", got)
	}
	if !errors.Is(err, security.ErrExecFromWritablePath) {
		t.Errorf("the refusal does not carry security.ErrExecFromWritablePath: %v", err)
	}
	for _, want := range []string{"openrouter", "api-key-cmd", "refusing to run", security.EvalRealPath(planted)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal never says %q:\n%s", want, err)
		}
	}
	if runs := fixtureRuns(t, tally); runs != 0 {
		t.Errorf("the planted command ran %d times, want 0 — the fence refuses BEFORE anything runs", runs)
	}
}

// The fence judges where a program IS, not that one was named: a command outside the workspace
// resolves and runs exactly as it did before the fence existed.
func TestKeyResolverRunsACommandProgramOutsideTheWorkspace(t *testing.T) {
	t.Parallel()

	resolver := NewKeyResolver(t.TempDir())
	got, err := resolver.Resolve(ServerEntry{
		Name: "openrouter", APIKeyCmd: fixtureCommand(t, "print", "sk-outside"),
	})
	if err != nil || got != "sk-outside" {
		t.Fatalf("resolved to (%q, %v), want the command's printed key", got, err)
	}
}

// The manual's wrapper-script shape — `api-key-cmd: bin/getkey.sh` — is a relative argv[0], which
// exec.Command resolves against apogee's own working directory rather than PATH. It is made
// absolute against that same directory before the fence judges it, so a wrapper living OUTSIDE the
// workspace keeps working; only one that actually resolves inside is refused.
func TestKeyResolverRunsARelativeCommandProgramOutsideTheWorkspace(t *testing.T) {
	home := t.TempDir()
	planted := plantKeyCommand(t, filepath.Join(home, "bin"), "getkey")
	t.Chdir(home)

	resolver := NewKeyResolver(t.TempDir())
	got, err := resolver.Resolve(ServerEntry{
		Name:      "openrouter",
		APIKeyCmd: fixtureCommandAt(filepath.Join("bin", filepath.Base(planted)), "print", "sk-wrapper"),
	})
	if err != nil || got != "sk-wrapper" {
		t.Fatalf("the wrapper script resolved to (%q, %v), want its printed key", got, err)
	}
}

// A resolver built with NO workspace root — the shape `probe model` and `daemon` pass, neither
// having a workspace to measure a program against — inherits security.ResolveProgram's empty-fence
// rule and refuses nothing, so a command those two commands could run before still runs.
func TestKeyResolverWithNoWorkspaceRootFencesNothing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	planted := plantKeyCommand(t, filepath.Join(root, "node_modules", ".bin"), "getkey")

	resolver := NewKeyResolver("")
	got, err := resolver.Resolve(ServerEntry{
		Name: "openrouter", APIKeyCmd: fixtureCommandAt(planted, "print", "sk-unfenced"),
	})
	if err != nil || got != "sk-unfenced" {
		t.Fatalf("the empty-root resolver answered (%q, %v), want the command's key — an empty fence refuses nothing", got, err)
	}
}

// The answer is cached against the entry's NAME and its key fields: a second use is free, an edited
// command re-resolves the way a config reload needs it to, and another entry is its own resolution.
func TestKeyResolverCachesPerEntryAndSourceTriple(t *testing.T) {
	t.Parallel()

	first := tallyPath(t, "first")
	second := tallyPath(t, "second")
	resolver := NewKeyResolver("")
	entry := ServerEntry{Name: "openrouter", APIKeyCmd: fixtureCommand(t, "count", first, "sk-one")}

	for use := 1; use <= 3; use++ {
		if got, err := resolver.Resolve(entry); err != nil || got != "sk-one" {
			t.Fatalf("use %d resolved to (%q, %v), want the cached key", use, got, err)
		}
	}
	if runs := fixtureRuns(t, first); runs != 1 {
		t.Errorf("the command ran %d times over three uses, want 1 — the session asks a key source once", runs)
	}

	// The user edits that entry's command and the watcher reloads the file: the cached answer no
	// longer matches the entry's source, so the next use resolves the NEW command.
	edited := ServerEntry{Name: "openrouter", APIKeyCmd: fixtureCommand(t, "count", second, "sk-two")}
	if got, err := resolver.Resolve(edited); err != nil || got != "sk-two" {
		t.Fatalf("the edited entry resolved to (%q, %v), want the new command's key", got, err)
	}
	if runs := fixtureRuns(t, second); runs != 1 {
		t.Errorf("the edited command ran %d times, want 1", runs)
	}

	// And the old source is no longer held anywhere: asked again, it runs again.
	if got, err := resolver.Resolve(entry); err != nil || got != "sk-one" {
		t.Fatalf("the original entry resolved to (%q, %v), want its own key", got, err)
	}
	if runs := fixtureRuns(t, first); runs != 2 {
		t.Errorf("the original command ran %d times, want 2 — its cached answer was replaced, not kept", runs)
	}

	// A different entry naming the same source is its own cache slot.
	sibling := ServerEntry{Name: "openrouter-eu", APIKeyCmd: entry.APIKeyCmd}
	if got, err := resolver.Resolve(sibling); err != nil || got != "sk-one" {
		t.Fatalf("the sibling entry resolved to (%q, %v), want its own resolution", got, err)
	}
	if runs := fixtureRuns(t, first); runs != 3 {
		t.Errorf("the command ran %d times, want 3 — the cache is keyed by entry name", runs)
	}
}

// A failure is not cached: a locked keychain or a dismissed prompt is fixable without editing the
// config, so the next use asks again.
func TestKeyResolverRetriesAfterAFailedSource(t *testing.T) {
	t.Parallel()

	tally := tallyPath(t, "runs")
	resolver := NewKeyResolver("")
	entry := ServerEntry{Name: "openrouter", APIKeyCmd: fixtureCommand(t, "countfail", tally, "keychain is locked")}

	for use := 1; use <= 2; use++ {
		if _, err := resolver.Resolve(entry); err == nil {
			t.Fatalf("use %d: a failing command resolved without an error", use)
		}
	}
	if runs := fixtureRuns(t, tally); runs != 2 {
		t.Errorf("the failing command ran %d times over two uses, want 2 — a failure must not be cached", runs)
	}
}

// Concurrent first uses of one entry share a single run of its command — the parallel delegations of
// ADR 0039 must not prompt a keychain once per agent.
func TestKeyResolverSingleFlightsConcurrentFirstUses(t *testing.T) {
	t.Parallel()

	tally := tallyPath(t, "runs")
	resolver := NewKeyResolver("")
	// The fixture sleeps before answering, so every caller below is inside the same in-flight run.
	entry := ServerEntry{Name: "openrouter", APIKeyCmd: fixtureCommand(t, "count", tally, "sk-shared", "150")}

	const callers = 8
	var wg sync.WaitGroup
	keys := make([]string, callers)
	errs := make([]error, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			keys[i], errs[i] = resolver.Resolve(entry)
		}()
	}
	wg.Wait()

	for i := range callers {
		if errs[i] != nil || keys[i] != "sk-shared" {
			t.Errorf("caller %d resolved to (%q, %v), want the one shared answer", i, keys[i], errs[i])
		}
	}
	if runs := fixtureRuns(t, tally); runs != 1 {
		t.Errorf("the command ran %d times for %d concurrent first uses, want 1", runs, callers)
	}
}

// assertResolved fails t unless Resolve answered with the wanted key, or with an error naming every
// wanted substring.
func assertResolved(t *testing.T, got string, err error, wantKey string, wantErr []string) {
	t.Helper()

	if len(wantErr) == 0 {
		if err != nil {
			t.Fatalf("Resolve: %v, want the key %q", err, wantKey)
		}
		if got != wantKey {
			t.Errorf("Resolve = %q, want %q", got, wantKey)
		}
		return
	}
	if err == nil {
		t.Fatalf("Resolve = %q with no error, want a refusal naming %v", got, wantErr)
	}
	if got != "" {
		t.Errorf("Resolve returned the key %q beside its refusal; a failed source answers with nothing", got)
	}
	for _, want := range wantErr {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err, want)
		}
	}
}

// TestAPIKeyEnvNames covers the scrub list the execution tools are handed: the union across every
// configured entry, deduplicated case-insensitively, trimmed the way the resolver trims, and nil
// when no entry names a variable at all.
func TestAPIKeyEnvNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{
			name: "no entry names a variable",
			opts: Options{Servers: []ServerEntry{
				{Name: "local"},
				{Name: "literal", APIKey: "sk-literal"},
				{Name: "command", APIKeyCmd: "security find-generic-password -w"},
			}},
			want: nil,
		},
		{
			name: "unions across entries in file order, startup entry last",
			opts: Options{
				APIKeyEnv: "STARTUP_KEY",
				Servers: []ServerEntry{
					{Name: "first", APIKeyEnv: "FIRST_KEY"},
					{Name: "second", APIKeyEnv: "SECOND_KEY"},
				},
			},
			want: []string{"FIRST_KEY", "SECOND_KEY", "STARTUP_KEY"},
		},
		{
			name: "the startup entry's own name is not repeated",
			opts: Options{
				APIKeyEnv: "SHARED_KEY",
				Servers: []ServerEntry{
					{Name: "first", APIKeyEnv: "SHARED_KEY"},
					{Name: "second", APIKeyEnv: "OTHER_KEY"},
				},
			},
			want: []string{"SHARED_KEY", "OTHER_KEY"},
		},
		{
			name: "two entries naming one variable dedupe, whatever the spelling",
			opts: Options{Servers: []ServerEntry{
				{Name: "first", APIKeyEnv: "Shared_Key"},
				{Name: "second", APIKeyEnv: "SHARED_KEY"},
			}},
			want: []string{"Shared_Key"},
		},
		{
			name: "surrounding space is trimmed as the resolver trims it",
			opts: Options{Servers: []ServerEntry{
				{Name: "padded", APIKeyEnv: "  PADDED_KEY \t"},
				{Name: "blank", APIKeyEnv: "   "},
			}},
			want: []string{"PADDED_KEY"},
		},
		{
			name: "an empty Options names nothing",
			opts: Options{},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := APIKeyEnvNames(tc.opts); !slices.Equal(got, tc.want) {
				t.Errorf("APIKeyEnvNames = %q, want %q", got, tc.want)
			}
		})
	}
}
