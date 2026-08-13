// Package keystore reaches the machine's own secret store through the command-line tool the
// operating system already ships, so a plaintext `api-key:` in the config file can be moved into
// that store and the entry left pointing back at it with an ordinary `api-key-cmd:` line.
//
// It is deliberately NOT a keychain library. apogee links no D-Bus client and no Keychain
// framework: the store is spoken to the way every other external program is spoken to (ADR 0042 —
// external programs are optional enhancements, never linked dependencies), which is what keeps a
// container, a headless box and a Windows machine from carrying code they can never run, and what
// makes migration's product an ORDINARY key source. What it writes into the config file is a
// command line the user could have typed themselves, read by the same resolver that reads every
// other `api-key-cmd:`; nothing here is a private channel between apogee and the store.
//
// A platform is covered only when apogee can do BOTH halves — write the secret in AND hand back the
// exact command that reads it out again. macOS ships `security` on every install. Linux has
// `secret-tool` only when libsecret is installed, and a keyring behind it only when a secret service
// is actually answering on the bus — the two are independent, which is why the Linux probe ASKS the
// service a question instead of trusting the binary's presence: the headless machines small models
// are hosted on frequently carry the tool and nothing behind it. Every other platform reports no
// store, and the caller tells the user what they can do by hand instead of offering half a move.
//
// The secret itself only ever travels on the tool's STDIN. An argv is world-readable on both
// platforms (`ps`, /proc/<pid>/cmdline), so passing the key as an argument would publish it to every
// process on the machine for as long as the write takes — a strange price to pay for a migration
// whose entire purpose is getting that key out of a readable place.
package keystore

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// service is the store item's service field. Every item apogee writes is filed under this one name
// with the ENTRY's name as the account, so the pair addresses exactly one server's key: re-migrating
// an entry updates the item it wrote before instead of leaving a second one behind, and a human
// browsing their keychain sees apogee's items grouped under a name they recognise.
const service = "apogee"

// probeAccount is the account the Linux probe asks about. Nothing ever writes it — the question is
// whether the secret service ANSWERS AT ALL, and "there is no such item" is the answer that proves
// it did.
const probeAccount = "__apogee_probe__"

// The programs the two covered platforms are reached through. They are named, not paths: `security`
// lives in /usr/bin on macOS and `secret-tool` wherever the distribution or Homebrew put it, so PATH
// is the only lookup that is right on both.
const (
	keychainProgram      = "security"
	secretServiceProgram = "secret-tool"
)

// kind is which store a Store speaks to — and, since the zero value means none, whether it speaks to
// one at all.
type kind int

const (
	kindNone kind = iota
	kindKeychain
	kindSecretService
)

// Store is this machine's secret store, addressed through its own CLI: what to call it in a prompt,
// how to put one server's key into it, and the command line that reads that key back out.
//
// It is obtained from Probe and used by value. The zero Store is the "no store here" answer — its
// methods stay safe to call and refuse, so a caller that ignored Probe's bool cannot silently write
// a secret nowhere.
type Store struct {
	kind    kind
	program string
	run     runner
}

// Probe reports the secret store this machine can actually be migrated into, and whether it found
// one at all. It runs at startup, before any offer is made, because an offer apogee cannot complete
// is worse than no offer: the user would consent to moving their key and be told afterwards that the
// move was never possible.
func Probe() (Store, bool) {
	return probe(runtime.GOOS, exec.LookPath, runTool)
}

// probe is Probe with its three machine-dependent inputs — the platform, the PATH lookup and the
// exec seam — passed in, so the suite can exercise every platform's answer from whichever machine it
// runs on (the internal/present opener idiom).
func probe(goos string, lookPath func(string) (string, error), run runner) (Store, bool) {
	switch goos {
	case "darwin":
		program, err := lookPath(keychainProgram)
		if err != nil {
			return Store{}, false
		}
		return Store{kind: kindKeychain, program: program, run: run}, true

	case "linux":
		program, err := lookPath(secretServiceProgram)
		if err != nil {
			return Store{}, false
		}
		store := Store{kind: kindSecretService, program: program, run: run}
		if !store.answers() {
			return Store{}, false
		}
		return store, true
	}
	return Store{}, false
}

// answers asks the secret service one harmless question — look up an account nothing ever writes —
// and reports whether the service was there to answer it.
//
// The EXIT STATUS is not the signal: "no such secret" is a non-zero exit on a perfectly healthy
// keyring, so reading the status would call every working machine broken. What separates a live
// service from a headless box is that a live one complains about nothing, while a missing bus makes
// the tool say so on stderr ("Cannot autolaunch D-Bus without X11 $DISPLAY", "org.freedesktop.
// secrets was not provided by any .service files"). A lookup that never returns — a bus autolaunch
// waiting on something that will never start — is bounded by probeTimeout and counts as no store,
// because this runs on the startup path and a stalled probe would be indistinguishable from a hung
// apogee.
func (s Store) answers() bool {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	outcome, err := s.run(ctx, []string{s.program, "lookup", "service", service, "entry", probeAccount}, "")
	return err == nil && strings.TrimSpace(outcome.stderr) == ""
}

// Name is what the migration prompt calls this store — the name the user's own operating system uses
// for it, so the question names something they can go and look at afterwards. The zero Store has
// none.
func (s Store) Name() string {
	switch s.kind {
	case kindKeychain:
		return "macOS Keychain"
	case kindSecretService:
		return "Secret Service"
	}
	return ""
}

// Write files key in this machine's store as entry's API key, replacing whatever was stored for that
// entry before: a second migration of the same server updates its item rather than adding a rival.
//
// The secret is handed to the tool on STDIN and never as an argument (see the package comment). The
// caller is expected to read the key back — through the ReadCmd line it is about to persist — before
// it rewrites anything, because "the tool exited 0" is a weaker claim than "the command that will
// live in the config file produces this key".
func (s Store) Write(entry, key string) error {
	switch {
	case s.kind == kindNone:
		return errors.New("apogee: this machine has no secret store apogee can write to")
	case strings.TrimSpace(entry) == "":
		return errors.New("apogee: a store item is addressed by the server entry's name, and this entry has none")
	case key == "":
		return fmt.Errorf("apogee: server %q: refusing to store an empty key — a key source that answers with "+
			"nothing is a broken source, so storing one would only move the defect into %s", entry, s.Name())
	case strings.ContainsAny(entry, "\r\n"), strings.ContainsAny(key, "\r\n"):
		// A line break cannot survive the trip: the keychain is written one command LINE at a time,
		// and both stores hand a secret back as the output of a command whose trailing whitespace the
		// resolver trims. Refusing here says so, where a read-back mismatch later could only say that
		// something went wrong.
		return fmt.Errorf("apogee: server %q: a key or entry name spanning lines cannot be stored in %s "+
			"and read back unchanged", entry, s.Name())
	}

	argv, stdin := s.writeCommand(entry, key)

	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	outcome, err := s.run(ctx, argv, stdin)
	if err != nil {
		return fmt.Errorf("apogee: server %q: could not be stored in %s: %w%s",
			entry, s.Name(), err, said(outcome.stderr))
	}
	// A non-zero exit is the ordinary failure. The stderr test is there for `security -i`, which runs
	// each line it reads and can report a refusal on stderr while still exiting 0 for having read the
	// input successfully — treating that as a write would hand the caller a key the store never took.
	if outcome.code != 0 || strings.TrimSpace(outcome.stderr) != "" {
		return fmt.Errorf("apogee: server %q: %s refused the key%s",
			entry, s.Name(), said(outcome.stderr))
	}
	return nil
}

// writeCommand is the argv and the stdin one write is made of.
//
// macOS goes through `security -i`, the tool's interactive mode: it reads command LINES from its
// standard input, so the whole add — the secret included — travels on a stream instead of in an
// argument vector. `-U` is what makes it an upsert rather than a duplicate.
//
// Linux hands `secret-tool store` the attributes as arguments and the secret on stdin, which is how
// that tool is built to be used; storing the same attributes again replaces the item.
func (s Store) writeCommand(entry, key string) ([]string, string) {
	if s.kind == kindKeychain {
		line := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n",
			service, securityWord(entry), securityWord(key))
		return []string{s.program, "-i"}, line
	}
	return []string{
		s.program, "store",
		"--label=" + service + ": " + entry,
		"service", service,
		"entry", entry,
	}, key
}

// ReadCmd is the `api-key-cmd:` line that reads this entry's key back out of the store — the exact
// text migration verifies against and then persists into the config file. The zero Store has none.
//
// It names the program rather than the absolute path Probe resolved: this line is written into a
// file that outlives the run, and is executed later by the resolver against the user's own PATH, so
// a store tool that moves (a distribution upgrade, a Homebrew prefix change) keeps working while a
// baked-in path would rot into a key source that fails for a reason the file does not explain.
func (s Store) ReadCmd(entry string) string {
	switch s.kind {
	case kindKeychain:
		return fmt.Sprintf("%s find-generic-password -s %s -a %s -w",
			keychainProgram, service, shellWord(entry))
	case kindSecretService:
		return fmt.Sprintf("%s lookup service %s entry %s",
			secretServiceProgram, service, shellWord(entry))
	}
	return ""
}

// shellWord renders one word for a command line the RESOLVER will split POSIX-style. A word made
// only of ordinary characters is left bare — the documented spellings are what a user comparing
// their config against the docs expects to see — and anything else is single-quoted, because a
// server entry may be named "work laptop" and a line that split into two arguments would look up an
// account nobody ever wrote.
func shellWord(word string) string {
	if isPlainWord(word) {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// securityWord renders one word for the command line `security -i` reads on its standard input. The
// tool parses that line itself, honouring double quotes and backslash escapes, so a secret or an
// entry name carrying a space survives the trip; an ordinary word is left bare to keep the line the
// same one a person would type. Migration's read-back verification is the backstop: if this quoting
// and the tool's parser ever disagree, the key that comes back out is not the key that went in, and
// the migration aborts with the config untouched rather than persisting a line that reads garbage.
func securityWord(word string) string {
	if isPlainWord(word) {
		return word
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(word)
	return `"` + escaped + `"`
}

// isPlainWord reports whether a word can stand unquoted on any of these command lines: a non-empty
// run of characters that no shell, and no tool's own parser, treats as anything but text. The set is
// deliberately a whitelist — a blacklist of metacharacters is one forgotten character away from
// emitting a line that means something else.
func isPlainWord(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("._@%+:/-", r):
		default:
			return false
		}
	}
	return true
}
