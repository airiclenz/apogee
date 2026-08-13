---
Status: accepted
---

# API keys resolve through a per-entry key source

## Context

A `servers:` entry that talks to anything but a bare local llama.cpp needs a credential, and until
now the only place to put one was `api-key:` — the key itself, in `~/.apogee/config.yaml`. That
file is the worst home a secret can have in this project specifically. It is **hand-edited** (the
settings surface splices into it, [ADR 0035](0035-the-settings-surface-persists-one-key-per-deliberate-edit.md),
and the user edits the rest by hand), it is **watched and re-read** every second
([ADR 0041](0041-the-config-file-is-watched.md)), it is the **single definition of what servers
exist** ([ADR 0036](0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md))
so it is the file a user copies to a second machine, and it is exactly the kind of dotfile that ends
up in a backup, a screen-share or a pasted bug report. Nothing about apogee makes that safe, and a
key that lives in it stays there for as long as the entry does.

The obvious answer — "apogee should use the keychain" — does not survive contact with the machines
this agent runs on. Small models are hosted on **headless boxes**: a devbox over SSH, a container, a
GPU machine with no session bus. Linking a keychain library would put a D-Bus client and a Keychain
framework into a binary that must stay CGO-free and cross-compile six ways
([ADR 0042](0042-external-programs-are-optional-enhancements-never-prerequisites.md)), to reach a
store that half those hosts do not have — and it would make apogee the *keeper* of somebody's
secret, with its own storage format, its own recovery story, and its own bugs.

The users who have already solved this solved it with a **command**: `pass show`, `op read`,
`security find-generic-password`, `gcloud secrets versions access`, a wrapper script. Every one of
those is a program on PATH that prints a secret to stdout — the shape apogee already deals with
everywhere else. What was missing was a place in the config file to say so, a moment to run it, and
a way to get an existing plaintext key out of the file it is already sitting in.

## Decision

**A `servers:` entry names ONE key source — a literal key, a command whose output is the key, or an
environment variable — resolved at first use of that entry and cached for the session; and where the
machine has a secret store apogee can both write to and read back from, a plaintext key earns a
consented start-up offer to move into it, after which the entry is an ordinary command source.**
Concretely:

**1 — Three sources, exactly one per entry.** `api-key:` (the literal), `api-key-cmd:` (a command
line whose standard output IS the key), `api-key-env:` (the NAME of an environment variable holding
it). Naming none is the **keyless** state — the local-server default. An entry setting more than one
is a start-up refusal from `ValidateServers`, naming the entry and every source it set: two sources
for one value is the duplicate-name defect wearing another key, and inventing a precedence would
leave a configured key silently ignored. A whitespace-only `api-key-cmd:`/`api-key-env:` is refused
on the `llama-launcher:` reasoning — configured while naming nothing.

**2 — Resolution happens at FIRST USE of the entry, not at load.** A config listing six servers must
not run six commands at start-up for five servers this session never talks to, each possibly popping
a keychain dialog. The source runs at the seam that needs the key — the start-up bind, a `/server`
switch, a Sub-agent server's beat, a probe, a headless run — and nowhere else. `ValidateServers`
therefore stays **offline**: it never runs the command or reads the variable, the way it never
touches an endpoint. A defect in a source is reported by the run that needed it, in the words of
what the user was trying to do.

**3 — One resolver per run, caching per entry name AND source triple.** The keychain is asked once
per session, not once per delegation, and concurrent first uses (parallel sub-agents,
[ADR 0039](0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md))
single-flight into one run of the command. Carrying the triple in the cache key is what keeps the
cache honest under the watched file: an entry whose key fields were edited no longer matches its
cached answer and re-resolves, an entry whose other fields changed keeps the answer it already paid
for, and a rename is a natural miss. A FAILED resolution is not cached — a locked keychain or a
dismissed unlock prompt is fixable without editing the config, so the next use retries.

**4 — The command runs with no shell.** The line is split POSIX-style (`google/shlex`) and executed
in argv form — the `editor:` / `present.command` idiom — with no stdin, none of apogee's terminal,
captured stdout/stderr (capped, so a misconfigured command cannot OOM the process), a 60-second
bound and trailing whitespace trimmed. A pipeline needs a wrapper script of the user's own; a
backend that must ask the human to unlock has to prompt through a GUI agent (pinentry-mac, the
Keychain dialog) rather than through the frame apogee is drawing on.

**5 — An empty answer is a HARD ERROR, never "no auth".** Non-zero exit, timeout, empty stdout,
unset or empty variable: all refuse, carrying the entry name and what the command said on stderr.
Keyless is spelled by naming no source at all, so a source that answers with nothing is a broken
source — degrading to an unauthenticated request would send the user's prompts to a remote endpoint
the file said to authenticate against, and they would learn about it from a 401 at best. The failure
lands on the thing the user was doing: start-up exits with the message, a `/server` switch is
refused and the session stays put, a headless run stops before spending a token, and a Sub-agent
server whose key cannot be produced takes no delegations — they fall back to the session's own
server with the reason said once per routing state change, which is
[ADR 0045](0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md) §4's floor rather
than a new failure mode inside a child.

**6 — The resolver lives in `internal/config`; the seams are in the composition root; the engine
never learns any of this.** Every layer below still receives a plain resolved string, exactly as
before ([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md):
the engine stays wire-silent and config-ignorant, and a bench Driver builds the same specs). The
`APOGEE_API_KEY` overlay keeps ADR 0036 decision 6's meaning: it applies to the start-up entry
*before* resolution, so an overlaid entry is a literal-source entry for that run whatever the file
named.

**7 — A plaintext key earns a start-up OFFER, with consent, per entry.** Entries carrying a literal
`api-key:` are collected at start-up; in a TUI session on a machine with a usable store, each raises
one pane (the first-boot picker's posture — an unasked-for question opens under a notice saying
why): **move it** / **not now** / **never for this entry**. "Not now" persists nothing and is asked
again next start-up. "Never" writes `plaintext-key-ok: true` on that entry alone — a marker legal
ONLY beside a literal `api-key:`, meaningless and refused beside the other two. Nothing is ever
migrated without that answer: this is ADR 0035's deliberate-edit grain applied to a credential.

**8 — Store coverage is a full WRITE+READ pair or no offer.** macOS ⇒ `security` (present on every
install). Linux ⇒ `secret-tool` on PATH **and** a secret service actually answering, asked live,
because the tool and a running keyring are independent on exactly the headless hosts this project
targets. Everything else ⇒ no store. A machine with no usable store — and every `apogee headless`
run, which has nobody to consent — gets a **notice** naming the plaintext entries and the manual
options (`api-key-env:`, an `api-key-cmd:` wrapper script, `chmod 600`), never half a move. The
store is reached through its CLI like every other external program (ADR 0042); apogee links no
keychain library and stores nothing itself.

**9 — Migration writes a STANDARD key source.** The item is filed under service `apogee` with the
entry's name as the account (upsert, so re-migrating updates rather than duplicates), and the secret
travels to the tool on **STDIN, never in argv** — an argv is world-readable on both platforms, a
strange price for a move whose whole purpose is getting the key out of a readable place. What lands
in the config file afterwards is an ordinary `api-key-cmd:` line the user could have typed
themselves, read by the same resolver as any other; there is no private channel between apogee and
the store.

**10 — Read-back verification BEFORE the rewrite.** After writing the item, migration runs the exact
`api-key-cmd:` line it is about to persist and compares the result to the key that went in. Any
mismatch or failed read aborts with a message and the config file untouched, so a migration can
never leave an entry pointing at a key nobody stored.

**11 — The rewrite is surgical or it is refused.** Inside the matched entry block only, the
`api-key:` line becomes the `api-key-cmd:` line (or the marker is added); comments, indentation,
ordering and every neighbouring line stay byte-identical, the result is re-parsed and re-validated
before it is persisted, and a file shape the writer cannot edit exactly is refused with the
writer's "add it by hand" idiom rather than guessed at. This is the `unconfined-hosts:` writer's
paranoia, addressed one level deeper — into a list entry.

## Considered and rejected

- **A keychain library (`go-keyring` or equivalent) and a fourth `api-key-keychain:` source** —
  rejected: it links a D-Bus client and a Keychain framework into a CGO-free six-target binary for a
  store that a command already reaches (ADR 0042), it breaks or hangs on the headless hosts small
  models actually run on, and it would make apogee the keeper of the secret rather than a reader of
  the user's own store. The `api-key-cmd:` a migration writes is strictly more portable than a
  library call, because the user can run it themselves.
- **Encrypting the config file (or the key field) with a passphrase** — rejected: it buys a
  passphrase-management problem, a prompt on a file that is read every second by the watcher, and a
  recovery story, all for one field that the OS already has a place for.
- **A fixed precedence or a fallback chain across sources** (`api-key-cmd:` first, fall back to
  `api-key:`) — rejected: it makes a configured key silently ignored, or a stale one silently used,
  and either way the file no longer says what happens. Exactly-one means the answer to "where does
  this key come from" is always the entry itself.
- **Silent auto-migration of every plaintext key** — rejected: it writes into the user's file
  without an answer, against ADR 0035's deliberate-edit grain, and it takes a key out of a file the
  user may be copying to another machine on purpose. "Never for this entry" exists for the same
  reason: an answer that is not "yes" must be able to be final, or the question becomes a nag the
  human learns to dismiss unread.
- **Resolving at load, or running the command in `ValidateServers`** — rejected: validation would
  become a keychain storm on every start-up and every watched re-read of the file, and a config check
  would depend on the state of an unlocked keyring.
- **Running `api-key-cmd:` through a shell** — rejected for the reason the exec tools already give:
  it is a quoting-and-injection surface on every platform and there is no shell to assume on Windows.
  A wrapper script is the user's own, and is visible in the file.
- **Windows migration** — deferred, not denied: there is no built-in generic-secret CLI to make the
  write+read pair from, so Windows takes the notice. Revisiting it needs a decision superseding
  point 8.

## Consequences

- **A key source is a documented config surface, and the seeded template teaches it.** The
  `servers:` comment block carries all three sources as commented examples, the exactly-one rule, and
  the GUI-prompt note an interactive backend needs — a template test pins that.
- **A new seam that needs a server's key calls the run's resolver.** Reading `entry.APIKey` directly
  is now a bug that presents as "my command-source server has no key": the composition root holds one
  `KeyResolver` and every seam — bind, switch, delegation target, heartbeat, `probe`, `probe model`,
  `headless` — goes through it.
- **`probe`'s `APIKeyConfigured` means "a key source is configured"**, not "a literal key is
  present"; its doc comment says so.
- **A first use can now block on a human.** A `/server` switch onto a keychain-backed entry may put a
  GUI unlock in front of the user for up to a minute. That cost is paid once per entry per session
  (point 3), and it is why the offer's product is a *command* the user recognises rather than a
  hidden lookup.
- **The store is the user's.** Apogee upserts one item per migrated entry and never deletes one:
  removing or renaming a `servers:` entry leaves its store item alone, because a secret the user may
  have started depending on elsewhere is not apogee's to garbage-collect.
- **Realisations.** `ServerEntry`'s three key fields and `ValidateServers`' refusals
  (`internal/config/config.go`); the resolver (`internal/config/keyresolve.go`); the entry-scoped
  rewrite and the marker (`internal/config/configwrite.go`); the store probe, write and `ReadCmd`
  (`internal/keystore/`, one concern one package per
  [ADR 0043](0043-files-split-by-concern-and-config-gets-a-package.md)); the move itself — write,
  verify, rewrite — assembled in the composition root (`cmd/apogee/keymigrate.go`), with the offer
  pane in `internal/tui/keymigration.go` and the storeless/headless notice beside the confinement
  warnings on stderr.
