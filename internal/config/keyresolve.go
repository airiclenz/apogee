package config

// The key resolver: where a server entry's API key actually comes from, and when.
//
// ServerEntry names ONE key source (config.go's ValidateServers enforces the "one of three" rule):
// a literal `api-key:`, a command whose standard output IS the key (`api-key-cmd:`), or the NAME of
// an environment variable holding it (`api-key-env:`). Naming none is the keyless state. This file
// is the one place that turns any of those into the token the Authorization header carries.
//
// Three properties are the whole design, and each is a refusal of an easier shape.
//
// It resolves at FIRST USE, not at load. A config listing six servers must not run six commands at
// startup — five of them for servers this session never talks to, each possibly popping a keychain
// dialog — so the source runs at the seam that needs the key (the startup bind, a `/server` switch,
// a delegation spawn, a probe or heartbeat build) and nowhere else. It is why ValidateServers stays
// offline: a defect in a command is reported by the run that needed it, in the words of the thing
// the user was trying to do.
//
// It CACHES per session, keyed by the entry's name AND the source triple it resolved from. The
// keychain is asked once, not once per delegation. Carrying the triple in the key is what makes the
// cache honest across a config reload (ADR 0041): an entry whose key fields the user edited no
// longer matches its cached answer, so the next use re-resolves, while an entry whose OTHER fields
// changed keeps the answer it already paid for. A renamed entry is a natural miss.
//
// An empty answer is a HARD ERROR, never "no auth". A command that exits non-zero, times out or
// prints nothing, and a variable that is unset or empty, all refuse with the entry's name and what
// the command said — because keyless is spelled by leaving all three keys OUT, so a source that
// answers with nothing is a broken source. Degrading to an unauthenticated request there would send
// the user's prompts to a remote endpoint the file said to authenticate against, and they would
// learn about it from a 401 at best.
//
// The command runs with no shell (shlex-split argv, the `editor:` / `present.command` idiom), no
// stdin and no terminal, bounded by a timeout: a pipeline needs a wrapper script of the user's own,
// and a backend that must ask the human to unlock has to prompt through a GUI agent (pinentry-mac,
// the Keychain dialog) rather than through the terminal apogee is drawing on.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/airiclenz/apogee/internal/userexec"
	"github.com/google/shlex"
)

// keyCommandTimeout bounds one `api-key-cmd:` run. It is generous on purpose: the command in front
// of a locked keychain is often a GUI unlock prompt, and a human reaching for a hardware key or a
// password manager takes tens of seconds. What it stops is the run that never answers at all — an
// agent waiting on a socket nobody is serving — which would otherwise hang the startup bind with no
// message at all.
const keyCommandTimeout = 60 * time.Second

// maxKeyCommandOutput bounds what one command may make apogee hold in memory. An API key is a short
// line; a command printing more than 64 KiB of it is a misconfigured command (`cat /dev/urandom`, a
// binary on the wrong path), and reading it to the end is how a typo in a config file becomes an
// out-of-memory kill. It is the stdout cap handed to internal/userexec, which bounds stderr and the
// quoted tail on its own.
const maxKeyCommandOutput = 64 << 10

// keySource is the triple of key-source fields an entry carries, captured as a comparable value so
// the cache can ask "is this still the same source?" with one equality test. It holds the raw
// strings rather than a resolved kind because that question is about what the FILE says: an edit
// that swaps one command for another must miss the cache, and comparing kinds alone would call the
// two the same source.
type keySource struct {
	literal string
	command string
	env     string
}

// keySourceKind is which of the three sources an entry actually names — or none of them.
type keySourceKind int

const (
	keySourceNone keySourceKind = iota
	keySourceLiteral
	keySourceCommand
	keySourceEnv
)

// keySourceOf reads an entry's key source off it.
func keySourceOf(e ServerEntry) keySource {
	return keySource{literal: e.APIKey, command: e.APIKeyCmd, env: e.APIKeyEnv}
}

// kind reports which source answers for this entry. ValidateServers has already refused an entry
// setting more than one, so the ordering below is only ever reached by an entry that never passed
// validation — and there it is the safe order rather than an arbitrary one: a literal key already in
// hand is returned without running anything, which is also what makes the ADR 0036 decision 6
// overlay work at the seam that applies it (APOGEE_API_KEY writes the literal onto the startup
// entry, and that literal must beat whatever source the file named for it).
func (s keySource) kind() keySourceKind {
	switch {
	case s.literal != "":
		return keySourceLiteral
	case s.command != "":
		return keySourceCommand
	case s.env != "":
		return keySourceEnv
	}
	return keySourceNone
}

// keyResolution is one entry's cache slot: the source it was resolved from, and the answer. done is
// closed when the answer is in, which is what makes concurrent first uses SHARE one resolution
// instead of racing to run the same command twice — the parallel delegations of ADR 0039 hit exactly
// that case, and a keychain prompted four times over would be the visible symptom.
type keyResolution struct {
	source keySource
	done   chan struct{}
	key    string
	err    error
}

// KeyResolver turns a ServerEntry into the API key its seam should send, running the entry's key
// source at most once per session (see this file's opening comment for the why of all three).
//
// The zero value is ready to use; NewKeyResolver is the spelling the composition root uses. ONE
// resolver is built for the session and shared by every seam that reads a key, because the cache is
// the point: two resolvers would ask the keychain twice.
//
// It is safe for concurrent use.
type KeyResolver struct {
	mu    sync.Mutex
	cache map[string]*keyResolution

	// workspaceRoot is the fence an `api-key-cmd:` program is measured against before it runs (see
	// runKeyCommand). It is the resolved workspace root of the Driver that built this resolver, and
	// EMPTY on the two commands that have no workspace to name — `probe model` and `daemon` — where
	// internal/userexec's empty-fence rule then refuses nothing.
	workspaceRoot string

	// commandTimeout overrides keyCommandTimeout for one resolver. It exists for tests, which
	// cannot wait a minute to see a hanging command refused; set it before the first Resolve, since
	// it is read without the mutex.
	commandTimeout time.Duration
}

// NewKeyResolver returns an empty resolver fenced to workspaceRoot: an `api-key-cmd:` whose program
// resolves inside that root is refused before it runs. An empty root fences nothing, which is what
// a command holding no workspace passes.
func NewKeyResolver(workspaceRoot string) *KeyResolver {
	return &KeyResolver{workspaceRoot: workspaceRoot}
}

// Resolve returns the API key entry e's seam should send: its literal `api-key:` as written, the
// output of its `api-key-cmd:`, or the value of the variable its `api-key-env:` names — and the
// empty string, with no error, for an entry naming no source at all (the keyless local-server
// default).
//
// The error is the refusal a failing source earns, phrased for the user and naming the entry: an
// unset or empty variable, a command that could not be parsed, ran non-zero, timed out or printed
// nothing. Callers surface it where the thing the user was doing fails — startup exits with it, a
// `/server` switch is refused by it, a delegation fails with it — and never fall back to sending no
// key.
//
// A source runs once per session: the answer is cached against the entry's name and its key fields,
// so later uses are free, an edit to those fields re-resolves after a reload, and concurrent first
// uses share the single run. A FAILURE is deliberately not cached — a keychain that was locked, a
// GUI prompt the user dismissed, an agent that had not started yet are all fixable without editing
// the config, so the next use asks again rather than holding the session to the first bad minute.
func (r *KeyResolver) Resolve(e ServerEntry) (string, error) {
	source := keySourceOf(e)
	switch source.kind() {
	case keySourceNone:
		return "", nil
	case keySourceLiteral:
		return source.literal, nil
	}

	resolution, mine := r.claim(e.Name, source)
	if !mine {
		<-resolution.done
		return resolution.key, resolution.err
	}
	resolution.key, resolution.err = resolveKeySource(e.Name, source, r.workspaceRoot, r.timeout())
	if resolution.err != nil {
		r.forget(e.Name, resolution)
	}
	close(resolution.done)
	return resolution.key, resolution.err
}

// claim hands back the cache slot for this entry and source, and says whether THIS caller is the one
// that has to fill it. A slot already in the map whose source still matches is the hit — filled or
// still in flight, the caller waits on it — and anything else (no slot, or one resolved from key
// fields the user has since edited) is replaced by a fresh slot this caller owns.
func (r *KeyResolver) claim(name string, source keySource) (*keyResolution, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if resolution, ok := r.cache[name]; ok && resolution.source == source {
		return resolution, false
	}
	if r.cache == nil {
		r.cache = make(map[string]*keyResolution)
	}
	resolution := &keyResolution{source: source, done: make(chan struct{})}
	r.cache[name] = resolution
	return resolution, true
}

// forget drops a failed resolution so the next use retries it. It removes the slot only while it is
// still the entry's current one: a reload that already replaced it with a newer source must keep the
// newer slot, or the retry would be racing the config rather than the keychain.
func (r *KeyResolver) forget(name string, resolution *keyResolution) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cache[name] == resolution {
		delete(r.cache, name)
	}
}

// timeout is the bound one command run gets: the resolver's own override when a test set one, the
// production constant otherwise.
func (r *KeyResolver) timeout() time.Duration {
	if r.commandTimeout > 0 {
		return r.commandTimeout
	}
	return keyCommandTimeout
}

// resolveKeySource runs the one source that answers for this entry. Only the command and variable
// kinds reach it — a literal needs no work and a keyless entry has none to do, and Resolve answers
// both before any cache slot is claimed.
func resolveKeySource(entry string, source keySource, workspaceRoot string, timeout time.Duration) (string, error) {
	if source.kind() == keySourceEnv {
		return resolveEnvKey(entry, source.env)
	}
	return runKeyCommand(entry, source.command, workspaceRoot, timeout)
}

// resolveEnvKey reads the variable an entry's `api-key-env:` names. The value is returned exactly as
// the environment holds it, the way a literal `api-key:` is: what the user exported is the key.
//
// Unset and empty are two messages rather than one because they have two fixes — a variable nobody
// exported, and one exported from a command that produced nothing — and neither is read as "this
// server takes no key": an entry that wants no Authorization header names no key source at all.
func resolveEnvKey(entry, variable string) (string, error) {
	name := strings.TrimSpace(variable)
	value, set := os.LookupEnv(name)
	switch {
	case !set:
		//nolint:staticcheck // ST1005: ends with the api-key-cmd: and api-key-env: key spellings by design.
		return "", fmt.Errorf("apogee: server %q: api-key-env: names %s, and that variable is not set in "+
			"apogee's environment — export it in the shell apogee starts from, or point the entry at another "+
			"key source; an entry that should send no key at all names none of api-key:, api-key-cmd: and "+
			"api-key-env:", entry, name)
	case strings.TrimSpace(value) == "":
		return "", fmt.Errorf("apogee: server %q: api-key-env: %s is set but empty — give it the key, or "+
			"remove the entry's key source altogether to send no Authorization header", entry, name)
	}
	return value, nil
}

// APIKeyEnvNames returns the name of every environment variable this configuration reads an API key
// out of: each `servers:` entry's `api-key-env:` in file order, then the startup entry's own
// (Options.APIKeyEnv) when it names one none of them already did. Nil when nothing names a variable
// at all, which is the ordinary case — the other two key sources read no environment.
//
// It answers for the execution tools' credential scrub (domain.Config.SecretEnvVars): a variable the
// operator exported so apogee could authenticate is otherwise inherited by every `terminal` /
// `python_exec` / `run_tests` subprocess, whose contents the MODEL chose, where reading it and
// sending it somewhere is one command away. The union is deliberately wider than the entry this
// session is bound to — `/server` switches mid-run, and a scrub that tracked the binding would leave
// the other entries' keys readable in every subprocess until the switch happened.
//
// Names are trimmed the way resolveEnvKey trims one before the lookup, so the scrub drops exactly
// the variable the resolver reads, and deduplicated case-insensitively because that is how the scrub
// compares them (one Windows variable in either spelling).
func APIKeyEnvNames(opts Options) []string {
	var names []string
	seen := make(map[string]bool)
	add := func(raw string) {
		name := strings.TrimSpace(raw)
		if name == "" {
			return
		}
		// Upper-cased as the canonical spelling: environment variable names are upper-case by
		// convention, and the map only ever holds the fold, never the name that is returned.
		folded := strings.ToUpper(name)
		if seen[folded] {
			return
		}
		seen[folded] = true
		names = append(names, name)
	}

	for _, entry := range opts.Servers {
		add(entry.APIKeyEnv)
	}
	add(opts.APIKeyEnv)
	return names
}

// runKeyCommand runs an entry's `api-key-cmd:` and returns what it printed as the key.
//
// The command line is split by the POSIX splitter and executed DIRECTLY through internal/userexec
// — argv[0] resolved the way the user's own shell would resolve it, their config and their PATH
// (opener.go's rung 3 reasoning) — with no shell between apogee and it. A key-fetching command is a
// fixed invocation of a credential tool, and handing the string to a shell would buy `|` and `$(…)`
// at the price of making every metacharacter in a config file executable.
//
// It still resolves on the USER's PATH — but a program that resolves INSIDE the workspace is refused
// before it runs, through the same fence as every other exec apogee performs. The config file is the
// operator's and the workspace is the model's, so an `api-key-cmd:` landing in the latter would hand
// the model the credential this key source exists to protect. The workspace root each Driver holds
// is the whole fence: this command runs on apogee's own behalf, before any confinement box exists.
//
// The child gets no stdin and no terminal (a tool that must prompt does so through a GUI agent), and
// inherits the environment whole — `pass`, `op`, `security` and their agents need HOME, DISPLAY, the
// D-Bus and GPG agent addresses; internal/userexec's package comment carries the why of each.
func runKeyCommand(entry, command, workspaceRoot string, timeout time.Duration) (string, error) {
	argv, err := shlex.Split(command)
	if err != nil {
		return "", fmt.Errorf("apogee: server %q: api-key-cmd: %q cannot be read as a command line: %w — "+
			"check the quoting; the line is split the way a POSIX shell splits one, but no shell runs it, so a "+
			"pipeline belongs in a script of your own", entry, command, err)
	}
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return "", fmt.Errorf("apogee: server %q: api-key-cmd: %q names no program — give the command whose "+
			"output IS the key, for example security find-generic-password -s apogee -a %s -w",
			entry, command, entry)
	}

	result, err := userexec.Run(context.Background(), argv, userexec.Options{
		WorkspaceRoot: workspaceRoot,
		WantStdout:    true,
		StdoutCap:     maxKeyCommandOutput,
		Timeout:       timeout,
	})
	said := saidOnStderr(result.StderrTail)
	switch {
	case err != nil:
		return "", fmt.Errorf("apogee: server %q: api-key-cmd: %w%s", entry, err, said)
	case result.TimedOut:
		return "", fmt.Errorf("apogee: server %q: api-key-cmd: %q did not answer within %s — a backend that "+
			"has to ask you to unlock must prompt through a GUI agent (pinentry-mac, the Keychain dialog), "+
			"since this command runs with no terminal of its own", entry, command, timeout)
	case result.ExitCode != 0:
		return "", fmt.Errorf("apogee: server %q: api-key-cmd: %q failed: exit status %d%s",
			entry, command, result.ExitCode, said)
	case result.StdoutTruncated:
		return "", fmt.Errorf("apogee: server %q: api-key-cmd: %q printed more than %d bytes — that is not an "+
			"API key; check that the command is the one that PRINTS the key and nothing else",
			entry, command, maxKeyCommandOutput)
	}

	key := strings.TrimRightFunc(result.Stdout, unicode.IsSpace)
	if key == "" {
		return "", fmt.Errorf("apogee: server %q: api-key-cmd: %q printed nothing — a key source that answers "+
			"with nothing is a broken source, not a keyless server; remove the entry's key source altogether to "+
			"send no Authorization header%s", entry, command, said)
	}
	return key, nil
}

// saidOnStderr renders the tail internal/userexec kept of what the command complained about as the
// refusal's own clause, or nothing at all when it stayed quiet — the tool's first sentence is almost
// always the part that names the fix.
func saidOnStderr(tail string) string {
	if tail == "" {
		return ""
	}
	return " — it said: " + tail
}
