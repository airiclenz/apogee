package keystore

// The keystore suite runs against FAKE store tools that are THIS test binary re-invoked under the
// tool's own name — a temp directory holding `security` and `secret-tool`, put on PATH, each a link
// to the running test executable, which TestMain dispatches by the name it was called as. It is the
// fork-and-exec fixture idiom internal/config and internal/mcp already use, and it buys what a
// stubbed exec seam cannot: a real process with a real argv and a real standard input, so the one
// claim this package exists to make — the secret travels on stdin and NEVER in an argument vector —
// is asserted against what an operating system actually received.
//
// The fakes emulate only what apogee asks of the real tools, and they record every invocation. Their
// parsers mirror apogee's assumptions about the real ones (in particular `security -i`'s quoting),
// so they cannot catch a disagreement with the real tool — that is what the live-gated round trip in
// live_test.go and, at runtime, migration's read-back verification are for.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/google/shlex"
)

// The environment the fakes are steered by: where to record what they were asked, where to keep the
// secrets they were given, and — when a test wants a broken machine — what to complain about and
// with which exit status.
const (
	fakeLogEnv    = "APOGEE_KEYSTORE_FAKE_LOG"
	fakeStoreEnv  = "APOGEE_KEYSTORE_FAKE_STORE"
	fakeStderrEnv = "APOGEE_KEYSTORE_FAKE_STDERR"
	fakeExitEnv   = "APOGEE_KEYSTORE_FAKE_EXIT"
	fakeEchoEnv   = "APOGEE_KEYSTORE_FAKE_ECHO"
)

// fakeToolDir is the directory holding the two fake tools, built once for the whole package run.
var fakeToolDir string

// invocation is one run of a fake tool as the fake recorded it.
type invocation struct {
	Argv  []string
	Stdin string
}

// TestMain is both halves of the fixture: called as a store tool it IS that tool, and called as
// itself it installs the two tool names and runs the suite.
func TestMain(m *testing.M) {
	switch programName(os.Args[0]) {
	case keychainProgram:
		os.Exit(runFakeKeychain(os.Args[1:]))
	case secretServiceProgram:
		os.Exit(runFakeSecretService(os.Args[1:]))
	}

	dir, err := installFakeTools()
	if err != nil {
		fmt.Fprintf(os.Stderr, "keystore suite: %v\n", err)
		os.Exit(1)
	}
	fakeToolDir = dir

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// programName is the tool name a path was invoked as, with Windows' extension and case folded away.
func programName(path string) string {
	return strings.ToLower(strings.TrimSuffix(filepath.Base(path), ".exe"))
}

// installFakeTools puts a copy of the running test binary in a temp directory under each store
// tool's name. A hard link is tried first and a symlink second — they cost nothing, where copying a
// test binary costs tens of megabytes — and the copy is the fallback for the platforms and
// filesystems that refuse both.
func installFakeTools() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locating the test binary: %w", err)
	}
	dir, err := os.MkdirTemp("", "apogee-keystore-tools")
	if err != nil {
		return "", fmt.Errorf("making the fake tool directory: %w", err)
	}
	for _, name := range []string{keychainProgram, secretServiceProgram} {
		path := filepath.Join(dir, name)
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
		if err := linkOrCopy(self, path); err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("installing the fake %s: %w", name, err)
		}
	}
	return dir, nil
}

// linkOrCopy puts the file at source at destination by whichever means the platform allows.
func linkOrCopy(source, destination string) error {
	if err := os.Link(source, destination); err == nil {
		return nil
	}
	if err := os.Symlink(source, destination); err == nil {
		return nil
	}

	from, err := os.Open(source)
	if err != nil {
		return err
	}
	defer from.Close()

	to, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(to, from); err != nil {
		to.Close()
		return err
	}
	return to.Close()
}

// ---------------------------------------------------------------------------------------------
// The fake tools, running in the child process.

// runFakeKeychain is `security` as much of it as apogee uses: the interactive mode migration writes
// through, and the lookup its persisted `api-key-cmd:` reads back through.
func runFakeKeychain(args []string) int {
	stdin := readAllStdin()
	recordInvocation(os.Args, stdin)
	if code, failed := fakeFailure(stdin); failed {
		return code
	}

	switch {
	case len(args) == 1 && args[0] == "-i":
		for _, line := range strings.Split(stdin, "\n") {
			words := splitSecurityLine(line)
			if len(words) == 0 {
				continue
			}
			if words[0] != "add-generic-password" {
				fmt.Fprintf(os.Stderr, "security: unknown command %q\n", words[0])
				return 2
			}
			flags := securityFlags(words[1:])
			putFakeSecret(flags["-s"], flags["-a"], flags["-w"])
		}
		return 0

	case len(args) > 0 && args[0] == "find-generic-password":
		flags := securityFlags(args[1:])
		secret, found := fakeSecret(flags["-s"], flags["-a"])
		if !found {
			fmt.Fprintln(os.Stderr, "security: SecKeychainSearchCopyNext: The specified item could not be "+
				"found in the keychain.")
			return 44
		}
		fmt.Println(secret)
		return 0
	}

	fmt.Fprintf(os.Stderr, "security: unsupported invocation %q\n", args)
	return 2
}

// runFakeSecretService is `secret-tool` as much of it as apogee uses: the store that reads its
// secret from stdin, and the lookup that answers with nothing and a non-zero status when the item is
// not there — the healthy-keyring answer the Linux probe depends on.
func runFakeSecretService(args []string) int {
	stdin := readAllStdin()
	recordInvocation(os.Args, stdin)
	if code, failed := fakeFailure(stdin); failed {
		return code
	}

	switch {
	case len(args) > 0 && args[0] == "store":
		attributes := secretToolAttributes(args[1:])
		putFakeSecret(attributes["service"], attributes["entry"], strings.TrimSuffix(stdin, "\n"))
		return 0

	case len(args) > 0 && args[0] == "lookup":
		attributes := secretToolAttributes(args[1:])
		secret, found := fakeSecret(attributes["service"], attributes["entry"])
		if !found {
			return 1
		}
		fmt.Print(secret)
		return 0
	}

	fmt.Fprintf(os.Stderr, "secret-tool: unsupported invocation %q\n", args)
	return 2
}

// fakeFailure is the broken-machine mode: when a test asked for one, the tool says what it was told
// to say and exits with the status it was told to use (a dead D-Bus exits non-zero; `security -i`
// can report a refusal and still exit 0 for having read its input). When the test also asked for a
// leaky tool, the standard input the tool was fed is echoed after the complaint — the shape a real
// tool takes when it reports on input it could not use, and the one that puts the secret on stderr.
func fakeFailure(stdin string) (int, bool) {
	text := os.Getenv(fakeStderrEnv)
	if text == "" {
		return 0, false
	}
	if os.Getenv(fakeEchoEnv) != "" {
		text += " " + strings.TrimSpace(stdin)
	}
	fmt.Fprintln(os.Stderr, text)
	code, err := strconv.Atoi(os.Getenv(fakeExitEnv))
	if err != nil {
		code = 1
	}
	return code, true
}

// readAllStdin drains the child's standard input. A tool apogee gave no input to is handed the null
// device, so this returns "" rather than blocking.
func readAllStdin() string {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	return string(data)
}

// recordInvocation appends one run to the log the test reads.
func recordInvocation(argv []string, stdin string) {
	path := os.Getenv(fakeLogEnv)
	if path == "" {
		return
	}
	line, err := json.Marshal(invocation{Argv: argv, Stdin: stdin})
	if err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintln(file, string(line))
}

// securityFlags reads the `-s`/`-a`/`-w` values off a `security` command line; the boolean flags it
// carries (`-U`) need no value and are skipped.
func securityFlags(words []string) map[string]string {
	flags := make(map[string]string)
	for i := 0; i < len(words); i++ {
		switch words[i] {
		case "-s", "-a", "-w":
			if i+1 < len(words) {
				flags[words[i]] = words[i+1]
				i++
			}
		}
	}
	return flags
}

// splitSecurityLine splits one line of `security -i` input into words the way the tool's own parser
// does: whitespace separates, double quotes group, and a backslash escapes the next character.
func splitSecurityLine(line string) []string {
	var (
		words   []string
		word    strings.Builder
		started bool
		quoted  bool
		escaped bool
	)
	for _, r := range line {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped, started = true, true
		case r == '"':
			quoted, started = !quoted, true
		case !quoted && unicode.IsSpace(r):
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if started {
		words = append(words, word.String())
	}
	return words
}

// secretToolAttributes reads the attribute/value pairs off a `secret-tool` command line, ignoring
// the options (`--label=…`) that precede them.
func secretToolAttributes(words []string) map[string]string {
	pairs := make([]string, 0, len(words))
	for _, word := range words {
		if strings.HasPrefix(word, "--") {
			continue
		}
		pairs = append(pairs, word)
	}
	attributes := make(map[string]string)
	for i := 0; i+1 < len(pairs); i += 2 {
		attributes[pairs[i]] = pairs[i+1]
	}
	return attributes
}

// putFakeSecret and fakeSecret are the fake store itself: one JSON file, addressed by the same
// (service, account) pair the real stores are addressed by, so writing the same pair twice
// overwrites exactly as an upsert must.
func putFakeSecret(service, account, secret string) {
	path := os.Getenv(fakeStoreEnv)
	items := readFakeStore(path)
	items[service+"\x00"+account] = secret
	data, err := json.Marshal(items)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func fakeSecret(service, account string) (string, bool) {
	secret, found := readFakeStore(os.Getenv(fakeStoreEnv))[service+"\x00"+account]
	return secret, found
}

func readFakeStore(path string) map[string]string {
	items := make(map[string]string)
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &items)
	}
	return items
}

// ---------------------------------------------------------------------------------------------
// The test side.

// fakeTools is the handle a test steers its fake machine with.
type fakeTools struct {
	log   string
	store string
}

// useFakeTools puts the two fake tools on PATH and gives them a log and a store of this test's own.
// PATH is REPLACED rather than prepended: a developer's machine may carry a real `secret-tool`, and
// a suite that found it would be testing that machine rather than this package.
func useFakeTools(t *testing.T) fakeTools {
	t.Helper()

	dir := t.TempDir()
	tools := fakeTools{
		log:   filepath.Join(dir, "invocations.jsonl"),
		store: filepath.Join(dir, "store.json"),
	}
	t.Setenv("PATH", fakeToolDir)
	t.Setenv(fakeLogEnv, tools.log)
	t.Setenv(fakeStoreEnv, tools.store)
	t.Setenv(fakeStderrEnv, "")
	t.Setenv(fakeExitEnv, "")
	t.Setenv(fakeEchoEnv, "")
	return tools
}

// useNoTools points PATH at an empty directory: a machine with no store tool at all.
func useNoTools(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// breakWith makes every later run of a fake tool complain with text and exit with code.
func (f fakeTools) breakWith(t *testing.T, text string, code int) {
	t.Helper()
	t.Setenv(fakeStderrEnv, text)
	t.Setenv(fakeExitEnv, strconv.Itoa(code))
}

// leakStdin makes every later run of a fake tool complain with text AND echo back the standard input
// it was handed — the leaky tool: `security -i` reporting on the `add-generic-password … -w <key>`
// line it could not run, `secret-tool` quoting the secret it was fed.
func (f fakeTools) leakStdin(t *testing.T, text string, code int) {
	t.Helper()
	f.breakWith(t, text, code)
	t.Setenv(fakeEchoEnv, "1")
}

// invocations is every run of a fake tool since this test began, in order.
func (f fakeTools) invocations(t *testing.T) []invocation {
	t.Helper()

	data, err := os.ReadFile(f.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("reading the fake tool log: %v", err)
	}

	var runs []invocation
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var run invocation
		if err := json.Unmarshal([]byte(line), &run); err != nil {
			t.Fatalf("reading the fake tool log: %v", err)
		}
		runs = append(runs, run)
	}
	return runs
}

// last is the most recent run of a fake tool.
func (f fakeTools) last(t *testing.T) invocation {
	t.Helper()

	runs := f.invocations(t)
	if len(runs) == 0 {
		t.Fatal("no store tool ran at all")
	}
	return runs[len(runs)-1]
}

// secret is what the fake store holds for one apogee entry.
func (f fakeTools) secret(t *testing.T, entry string) (string, bool) {
	t.Helper()
	value, found := readFakeStore(f.store)[service+"\x00"+entry]
	return value, found
}

// probedStore is the Store the given platform's probe finds, with the fakes in place.
func probedStore(t *testing.T, goos string) Store {
	t.Helper()

	store, found := probe(goos, exec.LookPath, runTool)
	if !found {
		t.Fatalf("probe(%q) found no store, want the fake tool on PATH", goos)
	}
	return store
}

// A store is offered only where apogee can do BOTH halves of the move. macOS is the binary's
// presence; Linux is the binary AND a secret service that answers, since the machines a small model
// is hosted on routinely carry libsecret's tool with no keyring behind it — offering a migration
// there would take the user's consent and then fail with their key half-moved.
func TestProbeReportsWhatThePlatformCanDo(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		tools    bool   // the fake store tools are on PATH
		broken   string // what those tools complain about; "" means a healthy machine
		wantName string // the store's human name; "" means no store at all
	}{
		{
			name:     "macOS reaches the keychain through security",
			goos:     "darwin",
			tools:    true,
			wantName: "macOS Keychain",
		},
		{
			name:     "linux reaches a live secret service through secret-tool",
			goos:     "linux",
			tools:    true,
			wantName: "Secret Service",
		},
		{
			name:   "linux with the tool but nothing answering on the bus",
			goos:   "linux",
			tools:  true,
			broken: "secret-tool: Cannot autolaunch D-Bus without X11 $DISPLAY",
		},
		{
			name:  "linux without secret-tool installed",
			goos:  "linux",
			tools: false,
		},
		{
			name:  "macOS without security on PATH",
			goos:  "darwin",
			tools: false,
		},
		{
			name:  "windows has no store apogee can write and read back",
			goos:  "windows",
			tools: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.tools {
				tools := useFakeTools(t)
				if tc.broken != "" {
					tools.breakWith(t, tc.broken, 1)
				}
			} else {
				useNoTools(t)
			}

			store, found := probe(tc.goos, exec.LookPath, runTool)

			if found != (tc.wantName != "") {
				t.Fatalf("probe(%q) found = %v, want %v", tc.goos, found, tc.wantName != "")
			}
			if store.Name() != tc.wantName {
				t.Errorf("probe(%q) named the store %q, want %q", tc.goos, store.Name(), tc.wantName)
			}
		})
	}
}

// The one claim this package exists to make: the secret reaches the store tool on its standard
// input and never in an argument vector, which every process on the machine can read.
func TestWriteSendsTheSecretOnStdinAndNeverInArgv(t *testing.T) {
	const secret = "sk-live-3f9c2b7a"

	tests := []struct {
		name        string
		goos        string
		wantProgram string
	}{
		{name: "macOS keychain", goos: "darwin", wantProgram: keychainProgram},
		{name: "secret service", goos: "linux", wantProgram: secretServiceProgram},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tools := useFakeTools(t)
			store := probedStore(t, tc.goos)

			if err := store.Write("prod", secret); err != nil {
				t.Fatalf("Write() = %v, want the key stored", err)
			}

			written := tools.last(t)
			for _, word := range written.Argv {
				if strings.Contains(word, secret) {
					t.Fatalf("Write() passed the secret in argv (%q) — every process on the machine can read that",
						word)
				}
			}
			if !strings.Contains(written.Stdin, secret) {
				t.Errorf("Write() sent stdin %q, want it to carry the secret", written.Stdin)
			}
			if got := programName(written.Argv[0]); got != tc.wantProgram {
				t.Errorf("Write() ran %q, want %q", got, tc.wantProgram)
			}
			if got, found := tools.secret(t, "prod"); !found || got != secret {
				t.Errorf("the store holds %q (found = %v), want %q", got, found, secret)
			}
		})
	}
}

// Migrating the same entry twice must UPDATE its item rather than leave two rival ones behind, so a
// user who re-runs a migration after rotating a key does not have to go and clean up by hand.
func TestWriteUpdatesTheEntrysExistingItem(t *testing.T) {
	tests := []struct {
		name           string
		goos           string
		wantUpsertFlag string // the flag that makes the tool replace rather than duplicate
	}{
		{name: "macOS keychain", goos: "darwin", wantUpsertFlag: "-U"},
		{name: "secret service", goos: "linux"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tools := useFakeTools(t)
			store := probedStore(t, tc.goos)

			if err := store.Write("prod", "sk-first"); err != nil {
				t.Fatalf("first Write() = %v, want the key stored", err)
			}
			if err := store.Write("prod", "sk-second"); err != nil {
				t.Fatalf("second Write() = %v, want the key replaced", err)
			}

			if got, _ := tools.secret(t, "prod"); got != "sk-second" {
				t.Errorf("the store holds %q, want the second key", got)
			}
			written := tools.last(t)
			asked := strings.Join(written.Argv, " ") + " " + written.Stdin
			if tc.wantUpsertFlag != "" && !strings.Contains(asked, tc.wantUpsertFlag) {
				t.Errorf("Write() asked %q, want the %s flag that replaces an existing item",
					asked, tc.wantUpsertFlag)
			}
		})
	}
}

// ReadCmd's output is persisted into the user's config file, so its exact spelling is the contract —
// with the documentation, and with the person who later reads the line and has to recognise it.
func TestReadCmdIsTheDocumentedSpelling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		store Store
		entry string
		want  string
	}{
		{
			name:  "keychain",
			store: Store{kind: kindKeychain},
			entry: "prod",
			want:  "security find-generic-password -s apogee -a prod -w",
		},
		{
			name:  "secret service",
			store: Store{kind: kindSecretService},
			entry: "prod",
			want:  "secret-tool lookup service apogee entry prod",
		},
		{
			name:  "an entry name with a space is quoted for the resolver's splitter",
			store: Store{kind: kindSecretService},
			entry: "work laptop",
			want:  "secret-tool lookup service apogee entry 'work laptop'",
		},
		{
			name:  "no store, no command",
			store: Store{},
			entry: "prod",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.store.ReadCmd(tc.entry); got != tc.want {
				t.Errorf("ReadCmd(%q) = %q, want %q", tc.entry, got, tc.want)
			}
		})
	}
}

// The write and the read are one promise, not two: the line ReadCmd hands back must produce the key
// Write was given, split the way the resolver splits it and run with no shell. This is the shape of
// the read-back verification migration performs before it rewrites a single byte of config.
func TestReadCmdReadsBackWhatWriteStored(t *testing.T) {
	const secret = "sk-round-trip-7f3c"

	tests := []struct {
		name  string
		goos  string
		entry string
	}{
		{name: "macOS keychain", goos: "darwin", entry: "prod"},
		{name: "macOS keychain, entry name with a space", goos: "darwin", entry: "work laptop"},
		{name: "secret service", goos: "linux", entry: "prod"},
		{name: "secret service, entry name with a space", goos: "linux", entry: "work laptop"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			useFakeTools(t)
			store := probedStore(t, tc.goos)
			if err := store.Write(tc.entry, secret); err != nil {
				t.Fatalf("Write() = %v, want the key stored", err)
			}

			argv, err := shlex.Split(store.ReadCmd(tc.entry))
			if err != nil {
				t.Fatalf("ReadCmd() produced %q, which does not split: %v", store.ReadCmd(tc.entry), err)
			}
			output, err := exec.Command(argv[0], argv[1:]...).Output()
			if err != nil {
				t.Fatalf("running %q: %v", store.ReadCmd(tc.entry), err)
			}

			if got := strings.TrimRight(string(output), " \t\r\n"); got != secret {
				t.Errorf("%q printed %q, want the stored key", store.ReadCmd(tc.entry), got)
			}
		})
	}
}

// A store that refuses must fail the write loudly, naming the entry and quoting the tool: the user
// consented to a move, and a silent failure would leave them believing their key had moved.
func TestWriteReportsWhatTheStoreSaid(t *testing.T) {
	const complaint = "security: SecKeychainAddGenericPassword <NULL>: User interaction is not allowed."

	tests := []struct {
		name string
		code int
	}{
		{name: "the tool exits non-zero", code: 1},
		{name: "the tool complains but exits zero, as security -i does", code: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tools := useFakeTools(t)
			store := probedStore(t, "darwin")
			tools.breakWith(t, complaint, tc.code)

			err := store.Write("prod", "sk-live-1")

			if err == nil {
				t.Fatal("Write() = nil, want the refusal surfaced")
			}
			if !strings.Contains(err.Error(), "prod") || !strings.Contains(err.Error(), "User interaction") {
				t.Errorf("Write() = %v, want the entry name and what the tool said", err)
			}
		})
	}
}

// Quoting the tool must not quote the secret back. A store tool is fed the key on its standard input,
// and a tool complaining about input it could not use echoes that input — so the error apogee builds
// out of that complaint is the migration publishing the very key it was moving out of a readable
// place, into the terminal, the session log and whatever the user pastes into a bug report.
func TestWriteRedactsTheSecretFromWhatTheStoreSaid(t *testing.T) {
	const complaint = "security: User interaction is not allowed."

	tests := []struct {
		name     string
		goos     string
		key      string
		fragment string // a distinctive run of the key that must survive in no spelling
		code     int
	}{
		{
			name:     "the keychain tool echoes the line it could not run",
			goos:     "darwin",
			key:      "sk-live-3f9c2b7a",
			fragment: "3f9c2b7a",
			code:     1,
		},
		{
			name:     "the keychain tool complains but exits zero, as security -i does",
			goos:     "darwin",
			key:      "sk-live-3f9c2b7a",
			fragment: "3f9c2b7a",
			code:     0,
		},
		{
			name:     "a key that had to be quoted is redacted in the spelling that went over the wire",
			goos:     "darwin",
			key:      `sk live "3f9c2b7a"`,
			fragment: "3f9c2b7a",
			code:     1,
		},
		{
			name:     "the secret service tool echoes the stdin it was handed",
			goos:     "linux",
			key:      "sk-live-3f9c2b7a",
			fragment: "3f9c2b7a",
			code:     1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tools := useFakeTools(t)
			store := probedStore(t, tc.goos)
			tools.leakStdin(t, complaint, tc.code)

			err := store.Write("prod", tc.key)

			if err == nil {
				t.Fatal("Write() = nil, want the refusal surfaced")
			}
			assertRedacted(t, err, tc.key, tc.fragment)
		})
	}
}

// The other error path quotes the same captured stderr, so it carries the same leak. A tool that
// never finished cannot be staged with a tool that runs, so this one is driven through the exec seam.
func TestWriteRedactsTheSecretWhenTheToolNeverFinished(t *testing.T) {
	t.Parallel()

	const key = "sk-live-3f9c2b7a"

	store := Store{
		kind:    kindKeychain,
		program: keychainProgram,
		run: func(context.Context, []string, string) (toolResult, error) {
			return toolResult{stderr: "security: User interaction is not allowed. add-generic-password -w " + key},
				errors.New("security did not answer in time")
		},
	}

	err := store.Write("prod", key)

	if err == nil {
		t.Fatal("Write() = nil, want the failure surfaced")
	}
	assertRedacted(t, err, key, "3f9c2b7a")
}

// assertRedacted is the whole claim about a quoted complaint: no spelling of the secret survives in
// it, the secret's place is marked, and the tool's own words — the part that names the fix — are kept.
func assertRedacted(t *testing.T, err error, key, fragment string) {
	t.Helper()

	message := err.Error()
	if strings.Contains(message, key) || strings.Contains(message, fragment) {
		t.Errorf("Write() = %v, want no spelling of the secret in what the tool said", message)
	}
	if !strings.Contains(message, "[redacted]") {
		t.Errorf("Write() = %v, want the secret's place marked [redacted]", message)
	}
	if !strings.Contains(message, "User interaction") {
		t.Errorf("Write() = %v, want what the tool said kept — it is the part that names the fix", message)
	}
}

// The arguments are checked before anything is run: a store apogee does not have, an entry with no
// name to file the item under, and an empty key — which is a broken key source, not a secret worth
// storing — are all refused without spawning a tool.
func TestWriteRefusesWhatItCannotStore(t *testing.T) {
	tools := useFakeTools(t)
	keychain := probedStore(t, "darwin")

	tests := []struct {
		name  string
		store Store
		entry string
		key   string
	}{
		{name: "no store on this machine", store: Store{}, entry: "prod", key: "sk-live-1"},
		{name: "an entry with no name", store: keychain, entry: "   ", key: "sk-live-1"},
		{name: "an empty key", store: keychain, entry: "prod", key: ""},
		{name: "a key spanning lines", store: keychain, entry: "prod", key: "sk-live\n-1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.store.Write(tc.entry, tc.key); err == nil {
				t.Errorf("Write(%q, %q) = nil, want a refusal", tc.entry, tc.key)
			}
		})
	}

	if runs := tools.invocations(t); len(runs) != 0 {
		t.Errorf("a refused write ran %d store tool(s), want none", len(runs))
	}
}
