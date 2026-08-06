---
Status: accepted
---

# The servers list is the single definition, and the last switch is the startup choice

## Context

[ADR 0028](0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md) gave apogee
a `/server` picker and, with it, a **second** description of what an upstream server is. Its
decision 5 added the file-only `servers:` list — one entry carrying `name`, `endpoint`, optional
`api-key`, optional `model` — *beside* the top-level `endpoint:` / `api-key:` / `host-alias:` /
`model:` quadruple that had described the startup server since the first release. The two say the
same four things in two shapes: one block per server in the list, four sibling scalars for the one
the session starts on. The seam is visible in the code that has to bridge it — `serverEntry`'s doc
comment explains that `APOGEE_API_KEY` "is a single value and it belongs to the STARTUP server (the
top-level `endpoint:`)", and `upstreamChoices` synthesizes a row for that startup server on every
launch, carrying a duplicate-name edge (the synthesized alias colliding with a configured `name`)
that exists only because the quadruple is not already a list entry.

ADR 0028 also declined, explicitly *for now and not on principle*, "a `--save` form for `/server`,
and a `server:` startup key naming an entry": session-scoped was the smaller, reversible claim and
matched `/confine off`-without-`--save`. What that leaves is a picker whose most ordinary use —
move to the box you actually work on — is forgotten at quit, and whose only durable form is hand
surgery on `endpoint:`, the very fact the list beside it already states.

[ADR 0035](0035-the-settings-surface-persists-one-key-per-deliberate-edit.md) then built the
machinery that makes the schema change mechanical rather than delicate: a declarative key registry
with a bijection guard against `fileConfig`'s yaml tags (a schema key without a registry row is a
test failure), and a comment-preserving splice writer that persists one named key per deliberate
edit, verified by re-parse and written atomically. `ISSUES.md` carried the request to retire the
quadruple and named its open design calls — which entry a session starts on, the override story,
the empty-list first run, migration — and said it needed grilling and its own record. A 2026-08-05
grill (two rounds of owner questions) settled all four. This record is that ratification; it is
implemented by `docs/plans/2026-08-05 - 05 - servers-single-definition-plan.md`.

The constraints it lands inside:
[ADR 0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md) — the engine
is never constructed without an endpoint (`errMissingEndpoint`), and the heartbeat, not config, is
what discovers a model;
[ADR 0023](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md) §8 and
`seedConfig`'s pinned never-overwrite invariant — the config file is a document the user owns;
ADR 0035's authorized write set and its explicit rejection of boot-time auto-sync;
[ADR 0029](0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md) — Launch
profiles are the launcher's, and never ride in `servers:`;
[ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
— every Driver stays viable, so nothing safety- or startup-critical may exist only as an
interactive TUI flow;
[ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md) — the renderer is thin
and its Model is copied by value.

## Decision

**1. `servers:` is the single definition of what upstream servers exist.** One entry is one server:
a required `name`, a required `endpoint`, an optional `api-key`, an optional `model` discovery hint.
The top-level `endpoint:`, `api-key:`, `host-alias:` and `model:` keys are **retired from the
schema** — they described one server in a shape the list already describes better, and the field
that survives them is the one ADR 0028 D5 already gave three jobs: `name` labels the picker row, is
the `/server` argument, and **is** the footer's host alias. The standalone `host-alias:` key
therefore has nothing left to do; the alias of the server you are on is the name you call it.

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
defines what an unmatched name does. The key is editable and restart-required, and it rides the
ordinary flag > env > file precedence like `mode` and `bypass`.

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

**7. An empty list guides into `/settings`, whose scope does not change.** No entries and no
override → the TUI starts pre-bound and opens the `/settings` pane, with a status-line fact naming
`~/.apogee/config.yaml` and a restart. The pane behaves exactly as ADR 0035 shipped it: the
`servers` row stays **read-only with its "edit in `config.yaml`" pointer** (D3), because a
structured add-form is the list-editor problem that ADR was right to decline, and shipping a
half-built one over the user's hand-edited file is worse than pointing at the file. A live rescan of
`servers:` mid-session is out of scope for the same reason the pointer says *restart*: the resolved
list is frozen at wire time, and pretending otherwise is a second source of truth.

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

## Considered options

- **Keep the quadruple as the startup server and the list as "the alternatives"** (the status quo)
  — rejected: it is two definitions of one thing, and every consumer pays for the seam. The
  synthesized picker row, its duplicate-name edge, `APOGEE_API_KEY`'s "belongs to the startup
  server" carve-out and `host-alias:`' existence are all bridge code between two shapes of the same
  four facts.
- **Keep the switch session-scoped and add `--save`** (ADR 0028's deferred option, taken literally)
  — rejected: a human who opened a picker and chose a machine has already expressed the intent the
  flag would ask them to repeat, and the failure mode of forgetting is silent and recurring, while
  the failure mode of recording is one visible line and a key they can delete. `/confine off`, the
  precedent that motivated session scope, is a **loosening of a safety fence** — the asymmetry is
  the whole point of that flag, and it does not transfer to "which box do I talk to".
- **Persist the switched endpoint in the session record so `--resume` returns to it** — rejected
  again, unchanged from ADR 0028: a session record describes a conversation, and which machine
  served it is not part of what is resumed. `server:` is machine state, and it lives in the config.
- **Auto-select the first entry when `server:` is unset** — rejected: it is a guess presented as a
  decision, and on a multi-server file it silently binds a machine the user did not pick. First run
  is exactly when asking is cheapest.
- **Hard-error on a stale `server:`** — rejected per decision 4: the config is stale, not corrupt,
  and the picker is a keystroke.
- **Make `server:` an enum over the configured names** — rejected: registry `EnumValues` are static
  and guard-pinned to parse sites, and a config-dependent vocabulary would either break that guard
  or invent a second kind of enum. Selection-time validation already exists and already has a
  defined failure behaviour.
- **Keep `--endpoint` / `APOGEE_ENDPOINT` as registry rows over a hidden key** — rejected: registry
  rows describe config-file keys and the bijection guard pins them to `fileConfig` tags; a row whose
  file half no longer exists would either weaken the guard or lie about the schema. The override
  resolver owns them, with its own binding test.
- **Persist the ephemeral override entry into `servers:`** (learn the endpoints you were pointed at)
  — rejected: an override is a statement about this run, its key may come from an environment the
  user does not want written down, and writing it would make `--endpoint` a config editor.
- **Refuse legacy configs with the paste-able error only, no auto-rewrite** — rejected as the
  default while kept as the fallback: the schema change is apogee's, not the user's, and handing
  every existing installation a manual edit to keep working is a cost this project imposed. The
  refusal path survives for exactly the cases where the rewrite cannot prove itself (decision 9).
- **Migrate by unmarshal → mutate → re-marshal** — rejected for the reason `configwrite.go` records
  and ADR 0035 restated: it hands the user back a file with a couple of settings in it, having
  silently deleted every word of documentation they wrote.
- **Tolerate the legacy keys silently for a release** (ignore unknown keys, let the list win) —
  rejected: a plain unmarshal already ignores them, which is precisely the trap — the old
  `endpoint:` would vanish without a word and the session would start somewhere the file never
  said. Silence is the one behaviour this change cannot afford.
- **A structured server add-form in the settings pane for the empty-list case** — rejected for
  scope, as ADR 0035 D3 rejected it: a list editor is its own design problem (ordering, deletion,
  nested validation) and belongs to its own plan.
- **A live `servers:` rescan mid-session** so an added entry appears without restart — rejected for
  scope: the resolved list is wire-time state, and a second load path would need its own answer for
  the entry you are currently bound to.

## Consequences

- **`config.yaml`'s schema breaks, with a migration that repairs it in place.** Four top-level keys
  go, `server:` arrives. A user who never opens the file sees one startup line and keeps working; a
  user who reads it finds a backup beside it. The change warrants a **minor** bump when the next
  version is cut — that call is the owner's, and nothing in the implementing plan touches a version
  identifier.
- **ADR 0035's authorized write set gains two members**, both named by this record: the `server:`
  recording that rides a `/server` switch, and the one-time legacy migration. Every other write
  still needs an ADR of its own; the migration in particular is bounded by decision 9 and does not
  generalize into "apogee may update your config".
- **ADR 0028 is amended, not replaced.** Its decisions 1–4, 6 and 7 stand entirely — the picker is
  still UI over the prepared layers, the hint still follows the binding, a switch still unbinds the
  model and the first beat still completes it, the Monitor is still swapped whole, the post-switch
  seed is still announced. Only D5's second half moves.
- **The seeded template becomes the teaching surface for the new schema.** `servers:` grows the
  per-field documentation the quadruple used to carry, and `server:` gets a commented example so the
  splice writer's insert-below-example rule lands a recorded choice under its own prose — the
  template-and-registry step-keeping obligation ADR 0035 recorded, applied to this change.
- **CONTEXT.md's Upstream and Heartbeat entries carry the change and gain no noun.** "Startup
  server" is a role played by an ordinary list entry, not a new term; the `/server` paragraph loses
  its session-scoped sentence and gains the recording and the first-boot ask.
- **`ISSUES.md`'s config-schema entry closes** — the grilling it asked for is this record. The
  pre-existing launch-endpoint drift it exposes (`rebindSpecFor` and `scheduleWiring.fire` keep
  reading launch `opts` after a switch) is not created here, but it becomes more visible once a
  switch persists, so it moves to `ISSUES.md` as its own entry rather than riding along.
- **The TUI grows a pre-bound phase**, a state in which no engine exists. It is contained by
  decision 8 (no other Driver has it) and by ADR 0024 (nothing is constructed endpoint-less), and it
  is the price of asking instead of guessing on first run.
