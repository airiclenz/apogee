# Per-server key sources — api-key-cmd, api-key-env, and keychain migration

- **Goal:** let a `servers:` entry name where its API key comes from instead of carrying it
  as plaintext — `api-key-cmd` (a command whose stdout is the key) and `api-key-env` (a
  named environment variable) beside the existing literal `api-key` — and, where the OS has
  a native secret store, OFFER at startup to move a plaintext key into it and rewrite the
  entry to the matching `api-key-cmd`.
- **Date:** 2026-08-13 · **Status:** ready · **sized for:** ~200k-context host
- **Authoritative sources:**
  - `internal/config/config.go` at `30ee9df` — `ServerEntry` (:1080, key-field doc :998),
    `ValidateServers` (:1094, refusal style), `EnvAPIKey` (:1699), detached-env fallback
    entry (:2078).
  - Seams where an entry's key becomes a spec today (all in the composition root):
    `cmd/apogee/upstream.go` (:166, :241), `cmd/apogee/wire_settings.go` (RebindSpec :944),
    `cmd/apogee/delegation.go` (DelegationTarget :367–369), `cmd/apogee/launcher.go` (:651),
    `cmd/apogee/wire_boot.go` (:133), `cmd/apogee/headless.go` (:332),
    `cmd/apogee/probe.go` (:111), `cmd/apogee/wire_server.go` (:28).
  - Exec idiom precedent: `internal/present/opener.go` (:405, shlex split; google/shlex is
    already a dependency); `internal/tools/exec_common.go` (:54, why no shell on Windows);
    `internal/tui/settings.go` (:731, editor argv launch).
  - Surgical config writer: `internal/config/configwrite.go` — the `unconfined-hosts:`
    append (:61–170) is the paranoia style to copy (refuse when the edit would change more
    than intended); it cannot yet address a field INSIDE a `servers:` list entry.
  - Startup interaction precedent: `internal/tui/prebound.go` (:42, first-boot server
    picker — an unasked-for startup prompt already exists).
  - ADR 0035 (one key per deliberate edit), ADR 0036 (servers list is the single
    definition; decision 6 = `APOGEE_API_KEY` overlays the startup entry only), ADR 0041
    (config file is watched), ADR 0042 (external programs are optional enhancements),
    ADR 0031 (engine door-keeping — engine stays config-ignorant).
- **Ratified design calls** (owner, via AskUserQuestion, 2026-08-13):
  1. Scope = per-entry `api-key-cmd` + `api-key-env`, plus startup MIGRATION of plaintext
     keys into the OS-native store. No keychain LIBRARY, no encrypted config — a command
     already reaches any keychain via its CLI (ADR 0042 reasoning); apogee may WRITE a
     secret into the user's store once, with consent, but never stores one itself.
  2. Resolution runs at FIRST USE of the entry's key (startup bind, `/server` switch,
     delegation spawn, probe/heartbeat construction) and is cached for the session; a
     cache entry self-invalidates when the entry's key-source fields change (config
     reload). Unused servers never run their command.
  3. Exactly ONE of `api-key` / `api-key-cmd` / `api-key-env` per entry; `ValidateServers`
     refuses combos (duplicate-name reasoning: two sources for one value is a file defect).
     `APOGEE_API_KEY` still overlays the startup entry over whichever source it carries.
  4. Failure is a HARD ERROR at the use point, carrying the entry name and the command's
     stderr (or the variable name): non-zero exit, empty/whitespace stdout, unset or empty
     env var all refuse — keyless is spelled by omitting all three fields, so an empty
     result is a defect, never "no auth". Startup on that entry fails with the message; a
     switch is refused and the session stays put; a delegation to it fails that delegation.
     ValidateServers itself never runs the command (endpoint-probe reasoning: validation
     stays offline).
  5. `api-key-cmd` is a scalar string, shlex-split into argv, executed with NO shell — the
     `editor:` / `present.command` idiom. Pipes need a wrapper script.
  6. Exec contract: no stdin, no terminal (stdout/stderr captured), 60s timeout, trailing
     whitespace trimmed from stdout. Interactive backends must prompt via a GUI agent
     (pinentry-mac, Keychain dialog); documented in the config template.
  7. Placement = a small cached resolver in `internal/config`, called at the composition-
     root seams where `entry.APIKey` is read today. Engine, agent, heartbeat, probe
     packages keep receiving a plain resolved string — zero engine change (ADR 0031).
  8. Docs = CONTEXT.md gains the term "Key source"; ADR 0047 records the decision and the
     rejected alternatives (keychain library, encrypted config, fallback chains).
  9. Migration trigger = STARTUP OFFER with consent: a plaintext `api-key` on any entry
     raises a TUI prompt with three choices — migrate now / not now (silent for this
     session, re-offered next startup) / never for this entry (persists a per-entry
     acknowledgement marker). Headless runs never prompt — notice only. Consent per
     ADR 0035's deliberate-edit grain.
  10. Store coverage = a full WRITE+READ pair or no offer: macOS `security` (always
      present), Linux `secret-tool` (probed live — binary present AND secret-service
      answering over D-Bus). Windows and storeless machines degrade to a startup notice
      naming the manual options (api-key-env, wrapper script, file permissions) — pure
      ADR 0042; the go-keyring library and a fourth `api-key-keychain:` source were
      rejected.
  11. Migration WRITES A STANDARD KEY-SOURCE ENTRY — after it, the config is an ordinary
      `api-key-cmd` line and the read path is design calls 2–7 unchanged. Store item
      convention: service `apogee`, account = entry name, upsert on re-migration. The
      secret travels to the store tool via STDIN, never argv (`security -i` interactive
      mode; `secret-tool store` reads stdin natively).
  12. Read-back verification BEFORE the rewrite: after writing the store item, migration
      runs the exact `api-key-cmd` it is about to persist and compares the result to the
      original key; any mismatch aborts with a message and leaves the config untouched.
  13. The rewrite goes through the surgical writer in configwrite.go's paranoia style:
      inside the matched entry block only, replace the `api-key:` line with the
      `api-key-cmd:` line (or add the acknowledgement marker on "never"); refuse if
      anything else would change. The "never" marker is `plaintext-key-ok: true` on the
      entry, legal ONLY beside a literal `api-key` (meaningless beside cmd/env — refused).
- **Standing requirements:** `skills: coding-standards`. Any authorized deviation from item
  text lands as a dated NOTES line under the item. VERSION-SUGGESTION lines only — never
  bump VERSION unasked.
- **Out of scope:** the detached `APOGEE_ENDPOINT`/`APOGEE_API_KEY`/`APOGEE_MODEL` fallback
  entry (config.go:2078) — it stays a literal overlay; any settings-registry surface for
  the new keys (the servers list has none today); MCP server auth; Windows migration (no
  built-in generic-secret CLI — revisit only with a new decision superseding call 10);
  secret storage of any kind inside `~/.apogee`; deleting/renaming store items when an
  entry is removed or renamed (the store is the user's).

## 1. Schema: key-source fields and the exactly-one rule — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the item names the embedded config template as `cmd/apogee/defaults/`; the
template's actual home is `internal/config/defaults/config.yaml` (embedded by
internal/config/defaults.go) — edited there.
NOTES (2026-08-13): no ADR 0047 citation in the new doc comments, since that ADR is written by
item 7 and a reference to a file that does not exist yet would dangle until then.

**What:** Add `APIKeyCmd string` (`yaml:"api-key-cmd,omitempty"`), `APIKeyEnv string`
(`yaml:"api-key-env,omitempty"`), and `PlaintextKeyOK bool`
(`yaml:"plaintext-key-ok,omitempty"`) to `ServerEntry` (internal/config/config.go:1080),
with doc comments in the file's house style stating design calls 3–6 and 9/13 (one source
per entry, first-use resolution, hard-error semantics, no-shell exec, the marker's
migration-silencing job). Extend `ValidateServers` to refuse: an entry setting more than
one of the three key fields (name all the set fields in the message), a whitespace-only
`api-key-cmd` or `api-key-env` (configured-but-names-nothing, the `llama-launcher:`
reasoning), and `plaintext-key-ok: true` on an entry without a literal `api-key`. Update
the embedded config template (`cmd/apogee/defaults/`) `servers:` comment block: the three
sources as commented examples (`pass show …`, `op read …`,
`security find-generic-password -s apogee -a <name> -w`), the exactly-one rule, and the
GUI-prompt note for interactive backends. Reconcile
`TestEmbeddedDefaultConfigDocumentsTheAPIKey` (internal/config/defaults_test.go:122).

**Tests:** `ValidateServers` table: cmd+literal refused, env+literal refused, cmd+env
refused, all three refused, each alone accepted, whitespace-only cmd/env refused, marker
without literal refused, marker beside literal accepted, message names the entry and the
offending fields. Template test asserts the new commented examples and the exactly-one
sentence.

**Acceptance:** `go test ./internal/config/`.

**Commit:** `feat(config): server entries declare one key source — literal, command, or env`

## 2. Resolver: cached first-use key resolution in internal/config — ✅ DONE (2026-08-13)

NOTES (2026-08-13): a FAILED resolution is deliberately not cached (the slot is dropped and the next
use retries). The item says the cache stores the resolved key; holding an error would lock a session
out of a server for a locked keychain or a dismissed unlock prompt — both fixable without editing the
config. Concurrent callers of one in-flight attempt still share that attempt's error.
NOTES (2026-08-13): the command's stdout/stderr are read into capped buffers (64 KiB / 4 KiB) and
output past the cap is refused as "not a key" — an unbounded read would turn a misconfigured
`api-key-cmd:` into an out-of-memory kill. Not in the item text; tested.
NOTES (2026-08-13): an entry setting more than one key source (which ValidateServers already refuses,
so only an unvalidated entry can reach it) resolves literal → command → variable. Chosen so item 3's
ADR 0036 decision 6 overlay works by writing APOGEE_API_KEY onto the startup entry's `APIKey` alone,
without having to clear the source the file named.
NOTES (2026-08-13): the suite gained internal/config's first `TestMain` — the sentinel re-exec of the
test binary that stands in as the `api-key-cmd` fixture (the item's "os.Executable re-invocation"
option, as internal/mcp and internal/platform already do it).
NOTES (2026-08-13): doc.go's package file map gained keyresolve.go's line — the package's own
docmap guard fails on a file the map does not name.

**What:** New file `internal/config/keyresolve.go`: a `KeyResolver` type with
`Resolve(e ServerEntry) (string, error)`. Behaviour: literal `api-key` returns as-is;
`api-key-env` reads the named variable, unset/empty ⇒ error naming the entry and variable;
`api-key-cmd` shlex-splits (google/shlex), runs argv-form `exec.CommandContext` with no
shell, `Stdin: nil`, captured stdout/stderr, 60-second timeout, then trims trailing
whitespace — non-zero exit / timeout / empty result ⇒ error carrying the entry name and
stderr (or the timeout). No source fields set ⇒ "" (keyless), no error. Cache: map keyed by
entry name, storing the resolved key plus the source triple it was resolved from; a hit
requires the triple to match, so a reload that edits any key field re-resolves and a rename
is a natural miss. Concurrency-safe (mutex) — parallel delegations (ADR 0039) share one
resolution; single-flight per entry so concurrent first uses run the command once.

**Tests:** literal passthrough; env hit/unset/empty; cmd success with trailing-newline trim;
cmd non-zero exit surfaces stderr in the error; empty stdout errors; timeout errors (short
timeout override via an unexported field or option); cache hit runs the command once
(counter script); changed source triple re-resolves; concurrent Resolve calls on one entry
run the command once; keyless entry resolves to "" without error. Command fixtures via
`os.Executable` re-invocation or `go run` helper in testdata, per existing repo test idiom.

**Acceptance:** `go test ./internal/config/` (race detector: `go test -race
./internal/config/ -run KeyResolv`).

**Commit:** `feat(config): cached first-use key resolver — command and env sources`

## 3. Wire the seams: every entry-key read resolves through the resolver

**What:** Construct ONE `KeyResolver` in the composition root and route every seam that
reads `entry.APIKey` today through `Resolve`: `cmd/apogee/upstream.go` (:241 and the startup
binding path), `wire_settings.go` RebindSpec build (:944), `delegation.go` DelegationTarget
build (:367), `launcher.go` (:651), `wire_boot.go` (:133), `headless.go` (:332), `probe.go`
(:111), `wire_server.go` (:28). Preserve ADR 0036 decision 6: the `APOGEE_API_KEY` overlay
applies to the startup entry BEFORE resolution (an overlaid entry is a literal-source entry
for this run). Failure semantics per design call 4: startup exits with the resolver's
message; a refused switch surfaces the error where switch failures surface today and stays
on the current server; a failed delegation-target build fails that delegation with the
message. `probe.APIKeyConfigured` now means "a key source is configured" — reconcile its doc
comment (internal/probe/host.go:53).

**Tests:** seam-level: a switch spec built from a cmd-source entry carries the resolved key;
a failing cmd-source entry refuses the switch with the entry name in the error; delegation
target resolution failure fails the delegation; startup overlay beats a cmd source on the
startup entry. Live-gated paths untouched (`APOGEE_LIVE_ENDPOINT` suite still passes
unmodified).

**Acceptance:** `go test ./cmd/... ./internal/config/ ./internal/probe/`, plus `make check`
at closeout.

**Commit:** `feat(config): entry keys resolve through the key resolver at every seam`

## 4. Keystore: probe the OS store and write a secret into it

**What:** New package `internal/keystore` (ADR 0043 grain: one concern, one package).
`Probe() (Store, bool)` detects the platform's usable store per design call 10:
darwin ⇒ `security` (present on every macOS); linux ⇒ `secret-tool` on PATH AND a live
secret-service answer (a cheap `secret-tool lookup` probe distinguishes a running keyring
from a headless box); anything else ⇒ not available. `Store` carries: a human name for the
prompt ("macOS Keychain", "Secret Service"), `Write(entry, key) error` (upsert; service
`apogee`, account = entry name; the secret via STDIN — `security -i` interactive mode on
darwin, `secret-tool store` stdin on linux; never argv), and `ReadCmd(entry) string` — the
exact `api-key-cmd` line migration will persist
(`security find-generic-password -s apogee -a <entry> -w` /
`secret-tool lookup service apogee entry <entry>`). Timeouts and captured stderr per the
resolver's exec contract.

**Tests:** unit tests with a fake tool on PATH (testdata scripts): write passes the secret
on stdin and never in argv (script records both), upsert overwrites, probe negative on
missing binary, linux probe negative when the lookup probe fails, `ReadCmd` strings match
the documented spellings. Real-store integration is NOT tested in CI (needs a live
keychain) — a live-gated smoke test behind an env guard, skipped by default, is enough.

**Acceptance:** `go test ./internal/keystore/`.

**Commit:** `feat(keystore): probe the OS secret store and write apogee entries via its CLI`

## 5. Configwrite: entry-scoped key-source rewrite

**What:** Extend `internal/config/configwrite.go` with a surgical edit addressed INTO a
`servers:` list entry, in the file's existing paranoia style: locate the entry block by its
`name:`, then either (a) replace its `api-key:` line with a given `api-key-cmd:` line, or
(b) append `plaintext-key-ok: true` to the block (the "never" branch). Comments, ordering
and every other line stay byte-identical; if the edit would change anything beyond the
target lines, refuse with the writer's "add it by hand" idiom. The rewrite must parse back
cleanly (re-load and re-validate before persisting, matching saveHostAcknowledgement's
verify-before-write shape).

**Tests:** golden-file pairs: replace on a commented, oddly-indented entry; replace on the
last entry; marker append; entry not found refuses; flow-style `servers: [...]` refuses;
an edit that would disturb neighbouring lines refuses; rewritten file re-validates with
exactly-one satisfied.

**Acceptance:** `go test ./internal/config/`.

**Commit:** `feat(config): surgical key-source rewrite inside a servers entry`

## 6. Startup migration offer: TUI prompt, headless notice, wiring

**What:** At startup, after config load and store probe: collect entries carrying a literal
`api-key` without `plaintext-key-ok: true`. With a store available and a TUI session, raise
the offer per entry (prebound.go's first-boot picker is the interaction precedent):
"move the key for <entry> into <store name> and point the entry at it?" — choices
migrate / not now / never. Migrate ⇒ `Store.Write`, then READ-BACK VERIFY (run the exact
`ReadCmd` through the resolver's exec path, compare to the original key; mismatch aborts,
config untouched, error shown), then the item-5 rewrite, then a one-line confirmation
notice. The running session keeps its already-resolved key; the watcher picks up the
rewritten file (ADR 0041) and the resolver's triple-keyed cache makes the switch seamless.
Not-now ⇒ nothing persisted, re-offered next startup. Never ⇒ item-5 marker append.
No store, or headless ⇒ no prompt: a startup notice names the plaintext entries and the
manual options (api-key-env, wrapper script, file permissions).

**Tests:** offer appears only for literal-key entries without the marker; decline persists
nothing; never appends the marker; migrate path with a fake store + temp config performs
write → verify → rewrite in that order and aborts before rewrite on verify mismatch;
headless run emits the notice and never prompts; storeless machine emits the notice.

**Acceptance:** `go test ./internal/... ./cmd/...`, plus `make check` at closeout.

**Commit:** `feat(tui): startup offer migrates plaintext keys into the OS store`

## 7. Docs: CONTEXT.md "Key source", ADR 0047, changelog

**What:** CONTEXT.md gains the glossary term **Key source**: "The one place a server entry's
API key comes from: a literal `api-key`, a command (`api-key-cmd`) whose output is the key,
or a named environment variable (`api-key-env`). An entry has at most one; having none is
the keyless state. Key migration is the startup offer that moves a plaintext key into the
OS secret store and turns the entry into a command source." (glossary only — no
implementation detail). New
`docs/adr/0047-api-keys-resolve-through-a-per-entry-key-source.md` in the house ADR format:
context (plaintext keys in a watched, user-edited file; small-model host machines are often
headless), decision (design calls 1–13 above, condensed: three sources exactly-one,
first-use cached resolution at the seam, no-shell exec, hard-error semantics, consented
startup migration writing standard entries, full write+read pair or no offer),
consequences, and rejected alternatives — keychain library and a fourth `api-key-keychain:`
source (D-Bus/headless breakage, a dependency for what a command already reaches;
ADR 0042), encrypted config (passphrase management for one field), fixed precedence and
fallback chains (shadowed or silently-stale keys), silent auto-migration (against
ADR 0035's deliberate-edit grain). CHANGELOG `[Unreleased]` entry under Added.
VERSION-SUGGESTION: minor feature bump (e.g. v0.13.x → v0.14.0) — owner decides.

**Tests:** none (docs).

**Acceptance:** ADR file exists and cross-references 0035/0036/0041/0042; CONTEXT.md term
present; `make check` still green.

**Commit:** `docs: key sources and key migration — CONTEXT term, ADR 0047, changelog`
