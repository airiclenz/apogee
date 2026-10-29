---
title: ADR 0036 decision
---

**1. `servers:` is the single definition of what upstream servers exist.** One entry is one server:
a required `name`, a required `endpoint`, an optional `api-key`, an optional `model` discovery hint.
The top-level `endpoint:`, `api-key:`, `host-alias:` and `model:` keys are **retired from the
schema** — they described one server in a shape the list already describes better, and the field
that survives them is the one ADR 0028 D5 already gave three jobs: `name` labels the picker row, is
the `/server` argument, and **is** the footer's host alias. The standalone `host-alias:` key
therefore has nothing left to do; the alias of the server you are on is the name you call it.
(Amended 2026-09-30: the entry has grown well past these four fields — among them `wire:`,
ADR 0078; `ServerEntry`
in `internal/config/config.go` is the current shape.)

What does not change: models are still **discovered**, never declared — the heartbeat (and, on a
host that has one, the launcher linkage) reports what a server actually serves, `model` in an entry
stays a per-server hint, and exactly one Monitor observes the bound server (ADR 0028 D4 stands).
Launch profiles do not become entries (ADR 0029's never stands).

**2. A session starts on the LAST-CHOSEN server, recorded in a `server:` scalar key.** `server:`
names one entry of the list. Every `/server` switch **to a configured entry** splice-writes
`server: <name>` through ADR 0035's verified writer, and the switch's fold notes the recording, so
the next launch comes up where the last session ended. This **supersedes ADR 0028 D5's
session-scoped half** ("nothing is written back to `config.yaml` and the next launch starts at
`endpoint:` again") and converts that ADR's rejected `server:` startup key from deferred into
ratified.

It is a recording, not a save prompt: 0028's declined `--save` form would ask a human to re-state,
with a flag, an intent they already expressed by choosing the server. The write is **best-effort
persistence of an act that already happened** — a failed write surfaces as a status-line warning
and never blocks or unwinds the switch, because the session has moved either way. Moves that land
on an endpoint no entry names — a launcher profile load, the ephemeral override entry of decision 6
— have no name to write and write nothing, silently: `server:` points into the list, and a pointer
to something unlisted would be a lie the next launch would trip over.

*(Amended 2026-08-06 by the implementation: a failed recording surfaces as a **transcript note
under the move it belongs to** — "could not record the server choice: …" — not as a status-line
warning. That slot carries STANDING session facts, what a surface that has gone leaves behind
(`layout.md`, "the two facts an *idle* frame may still carry"), while a one-off write failure is
news about an act, which is what a note is. The rest of this decision stands: the write is
best-effort and never blocks or unwinds the switch.)*

**3. First boot ASKS, and engine construction is DEFERRED to the answer.** With a non-empty list and
no `server:` (a fresh install, a hand-written config), the TUI starts **pre-bound**: no Agent is
constructed, the engine-facing seams route through a holder that is nil-safe until bound, and the
existing `/server` picker opens by itself over the configured entries. The choice constructs the
engine and records `server:` per decision 2.

This **extends ADR 0024 rather than re-opening it**. Construction still requires an endpoint —
`errMissingEndpoint` stands, nothing is ever built against a placeholder — what moves is *when* the
build happens, from wire time to the moment a server is known. The alternative to deferral is
guessing (bind the first entry) or refusing (a hard error on a machine that has servers configured
and a picker one keystroke away), and asking is the honest thing a first run can do.

**4. A stale `server:` is state, not intent.** A name that matches no entry — the user renamed an
entry, or deleted the one they were on — earns a startup notice naming the missing entry and the
same pre-bound picker, not a hard error. The picker fixes in one keystroke what a hard error would
send to file surgery, and the file is not wrong so much as out of date.

**5. `server:` is an ordinary string key with `APOGEE_SERVER` / `--server` sources.** It is
`kindString`, deliberately not `kindEnum`: the registry's enum vocabularies are static lists pinned
to their parse sites by `TestRegistryEnumValuesMatchParseSites`, and server names are whatever the
user's file says. Validity is therefore checked **at selection time**, where decision 4 already
defines what an unmatched name does. (Amended 2026-10-29: the row is `KindServer` today, not
`kindString` — a kind of its own since 2026-08-06, so the `/settings` surface offers the configured
entries as a picker (ADR 0037 decision 5); the writer still treats it as a plain string, and it is
still not an enum: `EnumValues` stays empty and validity is checked at selection,
`internal/config/registry.go`.) The key is editable and restart-required, and it rides the
ordinary flag > env > file precedence like `mode` and `bypass`. (Amended 2026-09-30: no key is
restart-gated any more — ADR 0037
decision 8 retired `RestartRequired`.)

**6. Raw overrides build an EPHEMERAL unnamed startup entry; `--server` selects a named one.**
`--endpoint` / `APOGEE_ENDPOINT` do not edit a config key any more — they construct an unnamed
`serverEntry` for this run (`api-key` from `APOGEE_API_KEY`, hint from `--model` / `APOGEE_MODEL`,
alias from `hostFromEndpoint`), select it as the startup entry, override any `server:` or
`--server` name, and are **never persisted**. With no endpoint override, `APOGEE_API_KEY` and
`--model` / `APOGEE_MODEL` overlay those two fields of the selected entry, which is what a one-off
key or a hint for today's run has always meant. An override also rescues a startup that the list
alone could not answer, so `APOGEE_ENDPOINT=… apogee` works with an empty file.

Two consequences are deliberate. The override **env and flag names detach from the key registry**:
they no longer describe config-file keys, so they leave `multiSourceKeys` — whose rows the bijection
guard pins to `fileConfig` tags — and live in the startup-override resolver with their own binding
test, so nothing advertises a source that nothing reads. And `upstreamChoices` synthesizes a row
**only** when the startup entry is ephemeral: ADR 0028's "the startup server is always offered"
invariant holds unchanged, while its duplicate-name edge dissolves, because a configured startup
server is already in the list it would have been prepended to.

*(Amended 2026-08-22: the table those names leave is called `keyAccessors` today. `multiSourceKeys`
held only the three keys with sources above the file; it gained an entry per registry row — a
`fromFile` projection for every key — when resolution became a loop over the registry, so its pin to
`fileConfig` now runs through the registry: `TestKeyAccessorsBindDescribedKeys` binds one entry per
row, and the bijection guard `TestRegistryIsBijectionWithFileConfig` pins those rows to the tags. The
overrides' own resolver and binding test are `startupOverrideSources` and
`TestStartupOverrideSourcesBindTheDetachedNames`. Nothing this decision decides changes.)*

*(Amended 2026-09-24: the `keyAccessors` table is retired. Each registry row now carries its own
file, env and flag projections — derived from the row's typed field, or, for a row without one,
its `fromFile` written on the row — and the resolution passes range over `KeyRegistry` itself.
`TestKeyAccessorsBindDescribedKeys` folded into `TestRegistryRowInvariants`, which holds every row
to a file projection and to exactly the env and flag plumbing its row names; the bijection guard
is unchanged. The detached overrides still resolve off the registry, as this decision says.)*

**7. An empty list guides into `/settings`, whose scope does not change.** No entries and no
override → the TUI starts pre-bound and opens the `/settings` pane, with a status-line fact naming
`~/.apogee/config.yaml` and a restart. The pane behaves exactly as ADR 0035 shipped it: the
`servers` row stays **read-only with its "edit in `config.yaml`" pointer** (D3), because a
structured add-form is the list-editor problem that ADR was right to decline, and shipping a
half-built one over the user's hand-edited file is worse than pointing at the file. A live rescan of
`servers:` mid-session is out of scope for the same reason the pointer says *restart*: the resolved
list is frozen at wire time, and pretending otherwise is a second source of truth. (Amended
2026-09-30: ADR 0037 decision 6 deleted
the read-only pointer — the `servers` row opens the editor — and
ADR 0041 watches the file, so an edited list is re-read and
installed mid-session (`reloadServers` in `cmd/apogee/wire_settings.go`).)

**8. Only the TUI gets the pre-bound flows; every other Driver gets a determinate error.**
`apogee headless`, `probe`, `probe model` and bench paths cannot open a picker and must not block on
one, so when no startup server is determinable they fail with the friendly hard error that names the
config file and shows an example block. This is ADR 0031's benchable-all-the-way-up invariant doing
its job: the interactive flow is an affordance layered on a startup rule that is complete without
it, not the only way to answer the question.

**9. A legacy config is auto-migrated ONCE — verified, backed up, announced.** A file carrying any
of the four retired keys is detected by an explicit **legacy sniff** (a plain unmarshal ignores
unknown keys, so without it an old config's endpoint would vanish in silence and the session would
start somewhere else or nowhere). On detection, apogee copies the file to a timestamped
`config.yaml.bak-YYYYMMDD-HHMMSS` sibling, then rewrites it through the same splice-and-verify
machinery ADR 0035 uses: the four legacy lines are removed, a `servers:` entry and a `server:`
pointer are inserted, comments, key order and every unrelated line are preserved, and the re-parse
must equal the original **apart from exactly that fold**. The entry's name is the old `host-alias:`
if set, else `hostFromEndpoint(endpoint)`. One startup line names the backup and states what moved.
Any verify failure, or a migrated name colliding with an existing entry, means **no write at all**:
apogee falls back to the hard error carrying a ready-to-paste `servers:` + `server:` block built
from the old values. A file already in the new schema is never touched — the sniff is the only
trigger.

This is the one write in apogee that is **not** a user act in the moment, and this record is the
only authority for it. It is a **narrow, announced exception to `seedConfig`'s never-overwrite
invariant** (ADR 0023 §8) and an **extension of ADR 0035's authorized write set**, bounded on every
axis the fence cares about: it happens at most once per file, only when the file already cannot be
read correctly by this version, in exactly one shape, with the previous bytes preserved beside it,
with the change stated on screen, and with refusal — not a best guess — as the answer to any doubt.
ADR 0035's rejected boot-time auto-sync is *not* re-opened by it: that proposal was apogee deciding
to enrich a working file with keys the user never asked for; this is the only alternative to
discarding an intent the user did express, on a file this version can no longer honour as written.
