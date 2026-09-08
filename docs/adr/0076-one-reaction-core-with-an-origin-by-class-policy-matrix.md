---
Status: accepted
---

# One Reaction core: an origin × class policy matrix replaces the four-rung Reaction surface

## Context

apogee reacts to the loop through three subsystems that are one abstraction in three coats: seven
**Floor guards** (engine Go, in-loop, edit the working value; seven flat booleans), the **Mechanism**
lab layer (Go, in-loop, retry / inject / edit; a `mechanisms:` map governing zero shipped rows since
[ADR 0071](0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md)), and five
observe-only **Hooks** (user argv / webhook, post-hoc, return nothing;
[ADR 0073](0073-hooks-are-observe-only-driver-side-reactions-to-engine-events.md)). They mutate the
same working values at the same five seams through three vocabularies, two firing events, a
guards-then-hooks double ladder at every seam, three config idioms and three live-swap idioms —
roughly 5.5k production lines of which the ~1.5k of policy in `internal/floor` is the value.

Bead `apogee-575` asked whether a Hook may advise the model. The research
([hook-talkback-findings.md](../design/hook-talkback-findings.md)) placed that at tier C of a six-tier
space, found the machinery already exists, and named the obstacle: ADR 0031 invariant 4 ("benchable
all the way up"), whose worked examples include trigger-injected prompts. The greenfield design
([reaction-core-greenfield.md](../design/reaction-core-greenfield.md)) reframed the question from
"supersede five ADRs" to "one core, one matrix, which user cells on day one". This ADR records the
grill of 2026-09-07 over that design's §3 and §8. It replaces ADR 0071 decisions 4 and 6, ADR 0073
decisions 2, 4, 5 and 7 and its second and third rejections, and rewrites the `Reaction surface`
glossary entry. ADR 0031 is **not** superseded: invariant 4 is satisfied by construction (decision 5).

## Decision

**1. One vocabulary, one type, one dispatcher.** Every point the loop passes is a **Moment**, of two
kinds: a **seam** (in-loop, synchronous, the payload is an editable working value — `pre-request`,
`post-response`, `pre-tool-exec`, `post-tool-result`, `history-rewrite`) and a **notice** (post-hoc,
the payload is sealed — `exchange-finished`, `turn-finished`, `file-changed`, `approval-requested`,
`approval-decided`, `error`, additive). Every seam publishes a notice when it closes. A **Reaction**
is `{id, origin, class, on: [moments], handler}`; `origin` is `engine` (a builtin, or a Go reaction
the bench registers in-process through the facade) or `user` (configured); `handler` is a Go func,
an argv list or a webhook. The **lab layer goes**: the hook API, the registry, `Deps`, `register`,
`SwapCatalogue`, `Config.EnableMechanisms`, the `mechanisms:` key and the `/settings` mechanisms row
are deleted, and runtime self-regulation (strikes, Turn Budget) leaves the core — no shipped row
ever needed it, and the bench arms an engine-origin reaction without a catalogue.
`MechanismFiredEvent` and `FloorGuardEvent` fold into one **`ReactionFiredEvent`** keyed by reaction
id. The `internal/floor` policy stays byte-for-byte; the guards' ratified order (salvage first, no
short-circuit — ADR 0071 amendment) stays. This supersedes ADR 0071 decision 4 and its rejected
alternative B.

**2. The policy matrix.** The four exclusive rungs become an executable matrix; exclusivity survives
as *one reaction, one cell*.

| origin ↓ / class → | observe | advise | gate | shape (view) | shape (work) |
|---|---|---|---|---|---|
| engine (builtin or bench-armed) | ✓ | ✓ | ✓ | ✓ | ✓ |
| user | ✓ | ✓ | ✓ | ✓ (reserved) | ✗ |

- **observe** — no return. Today's Hook.
- **advise** — returns text that becomes model-visible context: fenced, capped, fail-open.
- **gate** — returns allow / deny / ask at `pre-tool-exec`, implemented as a stage of the existing
  Approver, never as a seam edit. Deny text stays engine-authored. A user script saying No is the
  same act as a human saying No, so it does not touch the floor.
- **shape (view)** — edits what the model *sees* (`post-tool-result`, `pre-request`,
  `history-rewrite`). The seven Floor guards are the engine-origin builtins of this class.
- **shape (work)** — edits what the model *does*: tool-call arguments at `pre-tool-exec`. **Engine
  only.** Denied to users on evidence: once a later reaction can mutate arguments an earlier gate
  approved, no gate in the system is sound.
- **continue** is **not a class**. Every rival that shipped continuation control wedged and shipped
  a loop breaker late; not building it is the cheapest safety decision available.

The line the old rungs shared survives as the user row: a user reaction may change the model's
*view* and may say No; it may not change the model's *work* or make its *choices*. This supersedes
ADR 0073 decision 2 and its rejections of "tool-call policy or veto hooks" and "post-edit lint fed
back to the model".

**3. Day-one user cells: observe, advise, gate.** The user `shape (view)` cell is **reserved**: it is
in the matrix, no case has yet asked for it, and an edit leaves no span for the provenance ledger
(decision 6) to record — it needs its own design and its own grill before it ships. `mcp:` handlers
are **reserved** likewise: a reaction calling an MCP tool at a seam collides with the Approver (a
tool needing approval would prompt inside a prompt), so the key is held and the posture decided when
a case arrives. Day one is argv and webhook.

**4. Two lanes; the class picks the lane.** The **sync lane** runs on the loop goroutine at a seam,
engine before user, each reaction under a recover boundary and a deadline; outcomes fold to
`(retry, inject, edit)`. The **async lane** is today's Runner: one bounded, ordered queue per
reaction, drop-newest under overload, never waited on. `observe` runs async; `advise`, `gate` and
`shape` run sync. `EventSink.Emit` stays return-less and the tree-wide serialising mutex is
untouched: a reaction that must answer runs *before* the Moment is published, never on the stream.
This supersedes ADR 0073 decision 7 ("the engine never waits on a Hook") for the sync classes only,
and decision 4's "no `pre-*` event, ever": seams are Moments, and a user gate at `pre-tool-exec` is
an Approver stage rather than user-authored policy at a lab seam.

**5. Invariant 4 is satisfied by construction, and one arm admits the advise cell.** Every handler
registers on one core, so any reaction is bench-drivable in-process and the ledger attributes its
effect to a reaction id — that is what ADR 0031 invariant 4 asks for. What a per-user script
*returns* is not a behaviour the bench can own; it carries the standing obligation ADR 0064 §6
already places on a user's configured prompt: content that makes a model worse has moved the floor
down and is a defect. The **admission arm** for the advise cell is therefore the machinery, once: an
argv advise reaction returning a **fixed neutral sentence** at `post-tool-result`, measured against
Bypass under [ADR 0009](0009-the-ab-decision-rule.md) on the target class. Passing proves the
injection path — slot, fence, cap — is not itself a regression. The user advise cell ships only
after this arm passes; no content arm is required or owned by the bench.

**6. The advise slot, and the provenance ledger.** Advise text never enters the system prompt. For a
tool-shaped Moment (`post-tool-result`, `file-changed`) it lands as a fenced **trailer on the
closing tool result**; for any other Moment it is a fenced user-role message appended at the tail.
Both positions are role-safe under strict chat templates and leave the request prefix untouched, so
a local server's prefix cache survives the Turn; neither sits beside host-authored orientation, which
closes the forgery adjacency `Request.InjectContext`'s system-prompt fold created. Every injected
span records `{reaction, origin, moment, turn}` in a **provenance ledger**: the fence header is
derived from provenance, so nothing out-of-process can forge an engine header; spans are
**ephemeral**, dropped on resume, so a replay never re-reads a stale SHA or timestamp; the bench
attributes effect by reaction id; `/settings` can show what the model saw this Turn.

**7. Deadlines, cap and failure.** `advise` defaults to **10s** and is **fail-open** — no advice is
just no advice. `gate` defaults to **5s** (it runs on every tool call) and on timeout or crash
**escalates to ask**: a broken guard becomes a human question, never a silent allow and never a hard
deny. Advise output is capped at **8 KiB** and truncated with a marker, never spilled to a file. Every
entry may set its own `timeout:`. Failures report through the Driver-supplied reporter as Hook
failures do today (ADR 0073 decision 8 stands), never as an `ErrorEvent`.

**8. Trust posture is class-bound; one entry may carry more than one class.** `observe` runs as ADR
0073 decision 6 has it: the user's config, outside confinement, payload unscrubbed — an outbound
channel at the same trust as the screen. `advise` and `gate` are inbound and run **inside the
workspace exec fence** (the contract's §10 posture), and advise stdout passes the terminal tool's
secret redaction before the fence and the cap. An entry carrying `run:` and `advise:` runs two
handlers, each under its own class's posture; adding a blocking key never silently confines the
notifier beside it.

**9. Bypass switches off the model-shaping classes.** `--bypass` turns off every `advise` and
`shape` reaction of user or bench-armed origin; `observe`, `gate` and the seven Floor guards stay.
That keeps ADR 0006's meaning exactly — what can move the floor is off — without stripping a bench
run of the user's notifications or guards, which change nothing the floor measures. The control arm
and the shipped default remain the same agent.

**10. One config shape, three layers; execution keys need adoption.** `reactions:` is a list;
`run:` is observe, `advise:` returns text, `gate:` returns a decision; builtins appear by id with
`enabled:`. Resolution is `embedded defaults → ~/.apogee/config.yaml → <workspace>/.apogee/config.yaml`,
later layers overriding by key, `/settings` showing each value's layer. The rule that replaces
"global only" is *a clone cannot run a command before the user has seen it*: a repo layer may set
**parameters** (`enabled`, `timeout`, `on:`, `workspace:`, model profile, floor toggles) freely, and
its **execution keys** (`run:` / `advise:` / `gate:`) are **proposed, not live** until the user
adopts them. Adoption pins the entry's hash; an edit re-proposes; the repo config path is on the
tool write deny list, and the pin holds even if that deny is switched off, because `write_file`
could otherwise author a reaction that runs on the next reload. This is doctrine now; the repo
layer **ships in stage 2** (decision 13), and day one stays the global file plus the per-entry
`workspace:` filter. This supersedes ADR 0073 decision 5.

**11. Migration.** The seven Floor-guard booleans **stay canonical** — they are parameters, and a
Floor guard is by definition one top-level file-only boolean — so nothing migrates. A `hooks:` list
keeps loading through one migration table as an alias for `reactions:`, with a **one-time load
notice** naming the new form. A `mechanisms:` key gets the retired-roll message.

**12. Terms.** **Reaction** and **Moment** enter the glossary. **Mechanism**, **Hook point** and
**Experimental hook** retire (a bench-armed engine-origin reaction is what the last one named).
**Hook** survives only as the colloquial alias the manual's introduction offers ("what other tools
call a hook"). **Floor guard** stays as the name of the seven engine-origin `shape (view)`
builtins.

**13. Staged route.** (1) **Core** — Moment, Reaction, one ladder, `ReactionFiredEvent`; a pure,
behaviour-identical refactor proved by a bench identity arm. (2) **Config** — `reactions:` with the
migration table, three layers, adoption pin. (3) **User cells** — observe (already shipped as
Hooks), gate, and advise once decision 5's arm passes.

## Rejected

- **A config-only merge over the three existing runtimes** — cheaper now, but a fourth idiom on top
  of three that keeps every duplication.
- **Keeping the lab layer beside the core** (ADR 0071 D4) — zero risk to today's bench arms, but it
  is the config-only merge's cost in code form; the bench arms through the facade instead.
- **All four user cells on day one** — user `shape (view)` has no case and no ledger design for
  edits.
- **A dedicated tail message for every advise** — a user-role message after a tool result breaks
  strict chat templates, the very case `InjectContext` folds into the system prompt to avoid.
- **Gate fail-open** — symmetric with advise, but a guard that fails open under load is a guard an
  attacker can exhaust.
- **Bypass turns every user reaction off** — the greenfield wording; it strips a bench run of
  notifications and guards for no floor reason.
- **`mcp:` handlers now, inheriting the tool's posture** — a third handler path in the day-one arm
  and the fence story, for a case that has not arrived.
- **Global config only, D5 stands** — leaves the per-project linter, the case advise exists for,
  with no home.
- **A content arm for advise** — benches one user's content, which the obligation model says the
  bench cannot own.
- **Hook as the user-facing term** — two names for one row of the matrix, the collision the old
  entry's `_Avoid_` line already warned about.

## Consequences

- ADR 0071 keeps decisions 1, 2, 3 and 5 and its amendment; decision 4 and rejected alternative B
  are superseded and gain a pointer note. ADR 0073 keeps decisions 1 (term, as amended by decision 12
  above), 3, 6, 8 and 9 and its plugin and Driver-side-feed rejections; decisions 2, 4, 5 and 7 and
  the veto and lint rejections are superseded and gain a pointer note.
- ADR 0031 stands unchanged. ADR 0033 decision 5 and ADR 0034 §8's payload-discarded / payload-
  injected split, and ADR 0061 §4's B1 deferral, are not reopened here: a daemon Firing's or a
  skill's *own* payload still does not enter the model; a user advise reaction is a different
  route, opted into per entry.
- The advise cell carries the costs the findings document lists — an injection channel, token cost,
  staleness, prefix cache, forgery adjacency; decisions 6, 7 and 8 are the tools that address them,
  not a proof they are gone. Advise scripts should return facts, not imperatives; the manual says so.
- `docs/manual/hooks.md` and `docs/manual/configuration.md` are rewritten around `reactions:`; the
  glossary's `Reaction surface` entry becomes the matrix; the `Mechanism`, `Hook point` and
  `Experimental hook` entries become pointers.
- Most of the ~6k test lines across the three packages rewrite; one harness (Moment payload in,
  outcome out) replaces three.
- Bead `apogee-575` closes on this record. Implementation follows a plan in the house format; the
  bench identity arm for stage 1 and the fixed-text arm for stage 3 are that plan's acceptance,
  not this ADR's.

## Amendment — 2026-09-08: the stage-2 config surface

Grilled 2026-09-08 while stage 1 was in flight, before the stage-2 plan was written. This amendment
refines decisions 10, 11 and 13; it supersedes nothing in decisions 1–9 and 12.

**A1. `reactions:` is the user-origin surface only.** Decision 10's "builtins appear by id with
`enabled:`" is **struck**. The seven Floor booleans stay the single canonical spelling (decision 11),
so a builtin id inside `reactions:` would be a second spelling for one value — the fourth idiom this
ADR rejected, and a break of the mechanical anti-drift in `TestRegistryIsBijectionWithFileConfig`. A
builtin or Floor-guard id in `reactions:` is a **load-time validation error naming the top-level
boolean instead** — never silently ignored. Engine origin stays code: the bench arms a Go reaction
in-process through the facade (decision 1), not through this key. `enabled:` keeps a real job —
parking a user entry in the file. `docs/design/reaction-core-greenfield.md` §2.4 and §9.1 rows 12–13
are amended to match.

**A2. Stage 2 ships the global file only; the repo layer becomes stage 2b.** Decision 13's stage 2 is
split. **Stage 2** = the `reactions:` key in `~/.apogee/config.yaml`, the migration, the async lane
over `domain.Reaction`, the generation swap, the `/settings` row. **Stage 2b** = the repo layer, the
adoption pin, the write deny and the key-class rule — grilled on its own, discharging bead
`apogee-089` ("Project-local config with a trust gate", which asks for exactly that grill). Three
reasons the layer is not refactor work: decision 10's key-class table calls every non-execution key a
freely-overridable *parameter*, which would hand a repo layer `confine-to-workspace`,
`unconfined-hosts`, `tools.disabled` and `url-safety.deny-hosts` — all `GlobalOnly` precisely so a
project cannot set them (ADR 0012) — and names neither `GlobalOnly` nor the tighten-only shape that
already exists unfed in `security.MergeDangerousRules`' `projectAdd`; decision 10's backstop "the
repo config path is on the tool write deny list" describes machinery that does not exist (there is no
path deny list, and `~/.apogee` is only `TierForceApproval` — "a forced look, never a boundary",
ADR 0049 §4); and nothing in the tree hashes a config entry or models workspace trust. The file
watcher is also single-file by construction (ADR 0041). Per-project reactions keep working meanwhile
through the per-entry `workspace:` filter, which already ships. `AGENTS.md`'s "single `~/.apogee`
dotdir — settled decision" stands until stage 2b reopens it.

**A3. `/settings` gets one read-only structured row.** With the Floor rows staying (decision 11), the
reactions row replaces exactly one thing: today's read-only `hooks` row. It is `KindStructured`,
`Editable: false`, ⏎ opening `$EDITOR` on the key — no per-entry toggle table. The only in-tree
precedent for such a table is the mechanisms sub-list that stage 1 deletes, and the columns that
would justify rebuilding it (*layer*, *adopted?*) have no values until stage 2b. Bead `apogee-tbs`
(ADR 0071 D5 vs the seven editable Floor rows) therefore no longer blocks this work and stays open.

**A4. The async lane keeps its sink seat.** `hooks.Runner` remains the `EventSink` decorator in the
wire chain and keeps its matcher and executors; what changes is that it takes `[]domain.Reaction` of
class `observe` and `hooks.Hook` is deleted in favour of the domain type. Notice derivation does not
move into the agent — that would lift event derivation out of the sink chain that `eventjson.Writer`
and the Driver bridge both sit on, for no gain the config work needs. This is greenfield §9.1 row 8
read literally. The package is renamed **`internal/hooks` → `internal/reactions`**: every
user-facing name in it changes anyway, so the package name is the last piece carrying the retired
term.

**A5. Decision 1's "every seam publishes a notice when it closes" is implemented.** Five notice
Moments are added — `pre-request-finished`, `post-response-finished`, `pre-tool-exec-finished`,
`post-tool-result-finished`, `history-rewrite-finished` — named for consistency with
`exchange-finished` / `turn-finished` and because `Moment` is one namespace, so a notice cannot reuse
a seam's name. Each fires **unconditionally**, armed or not, and carries the sealed working value
plus the reaction ids that fired at that seam. They are **sink-only**: `eventjson.Encode` returns
`ok=false` for them, so the headless Event lines are unchanged. `pre-request` and `post-response`
fire per streamed Turn and the two tool seams per tool call; five new line kinds would roughly double
a typical stream's volume for consumers who never asked, and ADR 0075 D10 makes adding the lines
later purely additive.

**A6. A hard rename, delivered by an automatic file migration.** `approval-waiting` becomes
`approval-requested`, `approval-decided` is added and carries the verdict (the engine already emits
both phases at `dispatch.go:1021` and `:1032`; `matchApproval` today discards the second), the
payload's `"hook"` field becomes `"reaction"`, and `APOGEE_HOOK_*` becomes `APOGEE_REACTION_*`. **No
aliases survive in the code.** The break is absorbed instead by the migration idiom already in the
tree (`migrateLegacyConfig`, ADR 0036): `hooks:` folds into `reactions:` and the event names are
rewritten **in the user's own file** — verified fold, timestamped backup, atomic rewrite, one-time
note. The note also names the `APOGEE_HOOK_*` → `APOGEE_REACTION_*` and `"hook"` → `"reaction"`
changes a user's *scripts* need, which no migration can make for them; that is the one half of the
break that stays theirs to fix, and naming it in the note is what keeps it from being silent. If
both `hooks:` and `reactions:` are present the fold **refuses** in `legacyRefusal`'s shape — one
paragraph, no write at all — rather than guessing a merge. The dead `mechanisms:` key (top-level and
per-seat) is stripped by the same migration, printing stage 1's retired-roll message once in the
same note. This supersedes decision 11's "keeps loading through one migration table as an alias" and
its one-time load notice: the file is fixed, not aliased.

**A7. The entry schema.** `name:` → `id:`, `events:` → `on:`; `workspace:` and `timeout:` unchanged;
`enabled:` added. `run:` carries the class and is **polymorphic** — a sequence is argv
(`run: ["notify-send", "done"]`, decision 10's shown form), a mapping is a webhook
(`run: {url:, headers:, headers-env:}`) — so class lives on one key and the common one-line notifier
stays one line. `advise:` and `gate:` are **rejected at load** in stage 2 with "not yet shipped";
they arrive in stage 3.

**A8. One generation swap covers bypass too.** A generation is *the reactions that will fire*: the
builtin enable set resolved from the seven Floor booleans, the user entries from `reactions:`, and
the bypass filter applied. One `SetReactions(gen)` retires `SetBypass`, `SetFloor` and
`Runner.Replace` together, so a reload is one atomic swap and the row-driven read-modify-write on the
floor holder disappears. Greenfield §9.1 row 9, delivered whole. Consolidating the five independent
`LoadFileConfig(a.configPath, …)` re-reads in `wire_settings.go` is **out of scope** — four are MCP,
model profiles and validated sets, unrelated to reactions; a bead carries it.

**A9. `validated-sets:` is deleted.** Stage 1 leaves it loading, validating and arming nothing, and
an inert key is exactly the failure `internal/config/config.go:2756` names ("a config apogee has
quietly stopped understanding is indistinguishable from one that never said anything"). The key,
`internal/validated`, `shipped.json`, both `/settings` rows and the manual section go; the migration
strips the key from the file. ADR 0016's per-model enable sets no longer have a referent — the
Mechanism roster they gated is gone (ADR 0071) and `shipped.json` has been empty since v0.20.0 — and
per-model reaction rosters, if a case ever arrives, are a model-profile concern. ADR 0016 gains an
amendment note. This closes bead `apogee-jwf`.

**Consequences of this amendment.** `docs/manual/hooks.md` becomes `docs/manual/reactions.md`;
`docs/manual/configuration.md` §357-387 and §795-833 and `docs/manual/commands.md` §344-436 are
rewritten. Bead `apogee-pjx` narrows to the stage-2 scope above; stage 2b and stage 3 get their own
beads. `CONTEXT.md`'s glossary work belongs to the stage-2 plan, not here, so it does not collide
with stage 1 item 20, which is in flight.
