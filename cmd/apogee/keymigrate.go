package main

// The start-up key migration (ADR 0047), from the composition root's side: which `servers:` entries
// still carry their API key as plain text in the config file, whether this machine has a secret
// store to offer them a better home in, and — when the human takes the offer — the move itself.
//
// The move is deliberately assembled HERE rather than in either package it is made of. The store
// knows how to hold a secret (internal/keystore) and the writer knows how to edit one entry of the
// file (internal/config), but "write it, read it back through the command you are about to persist,
// and only then rewrite" is a policy about a user's credential rather than a fact about either — and
// this is the layer that owns the config path, the resolved entries and the seams the renderer is
// handed. The engine is not involved at any point (ADR 0031): what migration produces is an ordinary
// key source that every seam already reads.
//
// Nothing here migrates anything on its own. A plaintext key with nowhere to go earns a NOTICE
// naming the manual alternatives, an offer is raised only where the whole move can be completed,
// and the move runs only on the human's own answer (ADR 0035's deliberate-edit grain).

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/keystore"
	"github.com/airiclenz/apogee/internal/tui"
)

// secretStore is the half of [keystore.Store] this file uses, declared here so the sequence below
// can be exercised against a fake: the consumer names the interface it needs, the provider returns
// its concrete type (the Go standards' "accept interfaces, return concrete types").
type secretStore interface {
	Name() string
	Write(entry, key string) error
	ReadCmd(entry string) string
}

// probeKeyStore is the production probe, adapting [keystore.Probe] to the interface above. It is
// passed to prepareKeyMigration rather than called by it so the decision — offer, or notice — can be
// tested from a machine of any kind.
func probeKeyStore() (secretStore, bool) {
	store, ok := keystore.Probe()
	if !ok {
		return nil, false
	}
	return store, true
}

// prepareKeyMigration decides what this run does about plaintext keys, before the alternate screen
// opens and while a raw write to notices is still safe (announceConfinement's window).
//
// The store is probed only when there is something to offer. The Linux probe asks the secret service
// a live question, which is a subprocess on the start-up path, and a machine whose config names no
// plaintext key at all has nothing to gain from the answer.
//
// Where the whole move can be completed, the offer is recorded for the renderer to raise. Where it
// cannot — no store on this platform, no keyring behind the tool — the notice is the whole answer:
// the entries are named, so is what can be done about them by hand, and nothing is asked, because an
// offer apogee cannot complete is worse than no offer at all.
func (w *rootWiring) prepareKeyMigration(probe func() (secretStore, bool), notices io.Writer) {
	names := plaintextKeyEntries(w.opts.Servers)
	if len(names) == 0 {
		return
	}
	store, ok := probe()
	if !ok {
		fmt.Fprintln(notices, plaintextKeyNotice(w.configPath(), names))
		return
	}
	w.secrets = store
	w.keyOffer = tui.KeyMigrationOffer{StoreName: store.Name(), Entries: names}
}

// plaintextKeyEntries names the `servers:` entries whose key is sitting in the config file: a
// literal `api-key:` and no `plaintext-key-ok: true` beside it.
//
// The marker is the human's own "never for this entry", so an entry carrying it is not a candidate
// and is not counted in the notice either — an acknowledged decision that keeps being reported is a
// decision the human is being asked to make again. An entry with a command or a variable source has
// no plaintext key to move, and a nameless one cannot be addressed: neither is a candidate, and
// ValidateServers has already refused the file if it mixes sources.
//
// The list keeps the file's order, because that is the order the human reads their own config in.
func plaintextKeyEntries(servers []config.ServerEntry) []string {
	names := make([]string, 0, len(servers))
	for _, entry := range servers {
		if entry.APIKey == "" || entry.PlaintextKeyOK || strings.TrimSpace(entry.Name) == "" {
			continue
		}
		names = append(names, entry.Name)
	}
	return names
}

// plaintextKeyFor finds the literal key of one candidate entry at the moment the answer comes in.
// It is looked up rather than carried alongside the offer for the reason the renderer is handed
// names only: a secret that lives in exactly one place — the resolved config this run read — is a
// secret with exactly one place to leak from.
func plaintextKeyFor(servers []config.ServerEntry, name string) (string, bool) {
	for _, entry := range servers {
		if entry.Name == name && entry.APIKey != "" {
			return entry.APIKey, true
		}
	}
	return "", false
}

// plaintextKeyNotice is what a machine that cannot be offered a migration is told instead: which
// entries hold a key in the file, and the three things its owner can do about it without apogee's
// help (ADR 0042 — an external program is an enhancement, and this is the case where there is none).
//
// It names the file rather than assuming ~/.apogee, because --config and APOGEE_CONFIG both move it,
// and a notice pointing at a file the run never read would send the human to edit the wrong one.
func plaintextKeyNotice(path string, names []string) string {
	return fmt.Sprintf("apogee: the api-key for %s is stored in plain text in %s, and this machine "+
		"has no secret store apogee can move it into. You can point the entry at a variable "+
		"(api-key-env: APOGEE_WORK_KEY), at any command that prints the key "+
		"(api-key-cmd: pass show apogee/work — a wrapper script for anything needing a pipe), or "+
		"at least keep the file to your own account (chmod 600 %s). Adding "+
		"`plaintext-key-ok: true` to an entry answers this for good.",
		strings.Join(names, ", "), path, path)
}

// keyMigrator is the [tui.Options.MigrateKey] seam: the whole move for one entry, reported by the
// file it rewrote. nil when this run found no store, which is also when no offer was raised.
func (w *rootWiring) keyMigrator() func(string) (string, error) {
	if w.secrets == nil {
		return nil
	}
	return func(entry string) (string, error) {
		key, ok := plaintextKeyFor(w.opts.Servers, entry)
		if !ok {
			return "", fmt.Errorf("apogee: server %q carries no plaintext api-key to move", entry)
		}
		path := w.configPath()
		if err := migrateKey(w.secrets, path, entry, key); err != nil {
			return "", err
		}
		return path, nil
	}
}

// plaintextKeyKeeper is the [tui.Options.KeepPlaintextKey] seam: the "never for this entry" answer,
// recorded as the marker on that entry alone. It is wired with the migrator because it is an answer
// to the same question — a run that raised no offer has nothing to record.
func (w *rootWiring) plaintextKeyKeeper() func(string) (string, error) {
	if w.secrets == nil {
		return nil
	}
	return func(entry string) (string, error) {
		path := w.configPath()
		if err := config.SaveServerPlaintextKeyOK(path, entry); err != nil {
			return "", err
		}
		return path, nil
	}
}

// migrateKey moves one entry's key out of the file and into the store, in the one order that can
// never lose a key or leave a config file pointing at nothing:
//
//  1. WRITE the secret into the store. Until this succeeds nothing has changed anywhere.
//  2. READ IT BACK — by running the exact `api-key-cmd:` line that is about to be persisted, through
//     the resolver's own exec path, and comparing what comes out to what went in. "The store tool
//     exited 0" is a much weaker claim than "the command that will live in this file produces this
//     key": quoting, an account name the store folded, a tool that stored something else under the
//     same attributes — every one of those passes step 1 and fails here.
//  3. REWRITE the entry, and only then. A mismatch or a failed read leaves the config exactly as it
//     was, so the session keeps working off the literal it already has and the human is told why.
//
// The verification runs on a resolver of its own rather than on the session's. It is a CHECK, not a
// use: a resolution cached under this entry's name from a command that has not been persisted yet
// would be a session-long answer derived from a file state that may never come to exist — and on
// the mismatch branch it would be an answer known to be wrong.
func migrateKey(store secretStore, path, entry, key string) error {
	if err := store.Write(entry, key); err != nil {
		return err
	}

	command := store.ReadCmd(entry)
	got, err := config.NewKeyResolver().Resolve(config.ServerEntry{Name: entry, APIKeyCmd: command})
	if err != nil {
		return fmt.Errorf("apogee: server %q: the key is in %s, but reading it back with %q failed, "+
			"so the config file was left alone: %w", entry, store.Name(), command, err)
	}
	if got != key {
		return fmt.Errorf("apogee: server %q: %s handed back a different key than the one just "+
			"stored, so the config file was left alone — check %q by hand before trying again",
			entry, store.Name(), command)
	}
	return config.SaveServerKeyCommand(path, entry, command)
}

// configPath is the config file this run reads and these edits write — the one spelling of it, so a
// notice, a migration and a marker can never name three different files.
func (w *rootWiring) configPath() string {
	return filepath.Join(w.roots.config, "config.yaml")
}
