---
title: ADR 0047 decision
---

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
ADR 0039)
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
ADR 0045 §4's floor rather
than a new failure mode inside a child.

**6 — The resolver lives in `internal/config`; the seams are in the composition root; the engine
never learns any of this.** Every layer below still receives a plain resolved string, exactly as
before (ADR 0031:
the engine stays wire-silent and config-ignorant, and a bench Driver builds the same specs). The
`APOGEE_API_KEY` overlay keeps ADR 0036 decision 6's meaning: it applies to the start-up entry
*before* resolution, so an overlaid entry is a literal-source entry for that run whatever the file
named.

**Note (2026-08-14) — the overlay is dropped on the switch BACK, and that is the intent.** "Before
resolution" is scoped to the start-up bind and to nothing after it. A `/server` switch away from a
configured start-up entry and back onto it resolves that entry's OWN configured source, so the
overlaid key does not survive the round trip: the picker's rows are `opts.Servers` verbatim
(`upstreamChoices`, `cmd/apogee/upstream.go:291`) and the move hands the picked entry straight to
the run's resolver (`sessionMover.move` → `m.keys.Resolve(entry)`, `cmd/apogee/upstream.go:248`),
which sees the file's entry, not the overlay. This is the literal reading of the point above and is
owner-ratified (2026-08-14) rather than a defect: the overlay names the key for the run's *start-up
entry*, not a key for the session to carry onto whatever it later switches to and back from. A user
who wants the overlaid key after a round trip either restarts or names a source on the entry. The
one row that does carry it is the synthesized EPHEMERAL start-up entry, which is built from the
resolved options (`opts.APIKey`) precisely because it exists nowhere in the file to be re-resolved
from.

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

> **Amended 2026-10-08 (ADR 0095).** "Stores
> nothing itself" no longer holds for one class of secret: the OAuth tokens and client
> registrations apogee itself obtains for `auth: oauth` MCP servers live in 0600 files under
> `~/.apogee/mcp-auth/`. Every secret the user brings (this ADR's key sources included) is still
> read from the user's own source and never stored by apogee.

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
