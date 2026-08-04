# Plan — the scheduler library and `/schedule`, the engine's first timed Driver surface

**Date:** 2026-08-03
**Status:** not started
**Revised:** 2026-08-03 — five alignment fixes from the owner-requested review against ADR
0031/0022: the bench cannot import `internal/` (invariant-4 posture restated), the save-cadence
claim re-grounded (ADR 0022 misquote removed; divergence goes into ADR 0033), the firing's
Asker/Presenter pinned nil, the Gate's release pinned to engine idle, decision 6's guard framing
corrected. Renumbered the same day: the scheduler ADR is **0033** — 0032 went to the wave-3
skills-precedence ADR (`docs/adr/0032-the-user-skill-library-outranks-the-workspace.md`,
committed 2026-08-03).
**Goal:** build the `/schedule` feature from `TODO.md` ("apogee-code feature parity" → the
`/schedule` bullet) as ADR 0031 demands it be built: a **scheduler library**
(`internal/schedule`) that owns every when-and-how decision, a **one-firing headless runner**
(`internal/run`) that is the shared core of the deferred `apogee headless` subcommand, and a TUI
surface (`/schedule`, `/schedule-stop`, pickers, notices) that owns nothing but display and
input. Each firing is a fresh headless run saved as an ordinary session record, marked with its
schedule's identity and browsable in `/sessions`.

**Owner decisions taken 2026-08-03 (grill session — do not re-open; implement as written):**

1. **Overlap policy: skip the tick.** A tick landing while that schedule's previous firing is
   still in flight is skipped with a notice; the next tick fires normally. Never queue, never
   run a schedule's firings concurrently.
2. **Modes: Plan and Auto only.** A schedule is created in read-only Plan or confined Auto. The
   firing's Approver is a **fail-safe denier** — a gated action fails visibly, nothing ever
   parks (the headless-Asker pattern, CONTEXT.md ~`:256`). Ask-Before / Allow-Edits schedules
   are excluded in v1.
3. **The popup choice authorizes the mode.** A schedule's mode is chosen explicitly at creation
   and is independent of the TUI's launch mode. Auto stays gated by the same global
   Auto-eligibility ladder that gates launching in Auto (confinement capability +
   `confine-to-workspace`); it is never silently escalated.
4. **Schedule identity lands in browsable `Meta`** (ADR 0022): optional `ScheduleID` +
   `ScheduleName` fields, empty on every plain session, no `RecordVersion` bump.
5. **Fresh context per firing.** Each firing constructs a new Agent and a new session record.
   This part-answers the fresh-vs-resumed open question ADR 0031 records for workflow nodes —
   recorded in ADR 0033 (item 1). No carry-over summary in v1 (that injection is model-visible
   content → benchable-Mechanism territory, later and benched).
6. **The runner is its own library component** (`internal/run`), called by the scheduler today
   and reused as-is by the future `apogee headless` subcommand — the core of ADR 0031's "first
   mechanical consequence"; the standing-guard value (TUI-only capabilities visibly breaking a
   headless path) lands when the subcommand ships in its own plan.
7. **UX scope v1 (all in):** multiple concurrent schedules; the arg form
   `/schedule <cycle> [auto] <prompt>`; ticks defer while the interactive session is
   mid-Exchange (fire at the quiescent boundary); a status surface (bare `/schedule` lists the
   live schedules).
8. **A new ADR (0033) records the decisions** — written as this plan's first item, before any
   code.

**Plan-level prescriptions (not grilled — flag deviations as NOTES, they are open to owner
review, unlike the eight above):**

- A firing's record saves **once at completion**, and on error saves whatever completed with the
  error noted. This is a **deliberate divergence from ADR 0022's per-Turn cadence**: that cadence
  is the interactive TUI's crash-safety policy, implemented in the TUI worker — `session.Store`
  itself prescribes no cadence — and a firing is bounded and unattended, so at-completion is
  honest and cheap; a crash loses only that firing. ADR 0033 records the divergence explicitly
  (item 1) as a scoping of ADR 0022, never resting on it silently.
- A firing runs **without MCP tools** in v1. MCP connections are live host state, re-established
  per session (ADR 0022 §8, ADR 0008 — external effects are non-forkable); sharing the TUI's
  live connections into a concurrent second agent is exactly the fork those ADRs forbid.
- The automatic **naming call does not fire for firings** — it is a `tui.Options` seam by design
  (ADR 0022 addendum 2026-07-31). A firing's record title is
  `<schedule name> — <HH:MM>` (local time), so runs read chronologically under their schedule.
- Preset cycle choices in the picker: `1m, 5m, 15m, 30m, 1h, 4h`. The arg form accepts any Go
  duration ≥ 30s (floor guards against a typo like `5s` hammering the single-slot server).
- While a deferred (gated) firing waits for quiescence, further ticks of the same schedule are
  skipped under decision 1 — at most one firing is ever pending per schedule.

**Authoritative sources (precedence in this order):**

1. The **owner decisions above** — the ratified record of the 2026-08-03 grill. ADR 0033
   (item 1) transcribes them; until it exists this header is the source.
2. [ADR 0031](../adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
   — the four door-keeping invariants. The engine stays wire-silent (1); nothing assumes an
   interactive human (2); no connectors (3). Invariant 4 (benchable all the way up) imposes
   nothing on v1 **because the scheduler injects no model-visible content** — the prompt is the
   prompt (decision 5). Note the bench **cannot** import `internal/` packages (it is a separate
   module, ADR 0001): if model-visible content ever arrives it lands as a catalogued Mechanism
   (engine-side, already benchable) or forces the facade-export decision — recorded in ADR 0033
   (item 1).
3. `TODO.md` → "apogee-code feature parity" → the `/schedule` bullet (the enriched 2026-08-03
   body) — the feature brief this plan closes.
4. [ADR 0022](../adr/0022-sessions-persist-per-turn-as-dual-representation-records.md) + its
   addenda — the record shape, `Meta`, the store contract, what is deliberately NOT session
   state.
5. [ADR 0024](../adr/0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md)
   and the autotitle precedent (`internal/tui/autotitle.go`, `cmd/apogee/title.go`) — the
   single-slot upstream posture an out-of-band call lives under: generous timeout, retries off,
   fire-and-wait.
6. [ADR 0027](../adr/0027-one-slash-namespace-with-inline-skill-tokens.md) — the one command
   namespace; `internal/tui/command.go` `commandSpecs` is the registration point.
7. Repo conventions: ADR 0010 (layering — internal packages import `internal/agent` /
   `internal/domain` downward, never the root facade), ADR 0011 + `internal/tui/doc.go` (the
   value-copied Bubble Tea `Model`; never a `strings.Builder` by value), `AGENTS.md`.

Where any document above disagrees with an item's text, **the document wins** — implement what
it says and record the divergence as a dated NOTES line under the item.

**Standing requirements:**

- Invoke with forwarded skills: `coding-standards` (Go).
- `make check` green before every commit; it runs `-race`. One commit per item.
- **Never** bump `VERSION`, a CHANGELOG release heading, or any other version identifier. The
  closing note carries the suggestion; the owner decides.
- No live LLM endpoint is needed except the one live-gated test in item 3 (skips without
  `APOGEE_LIVE_ENDPOINT`, like every live test).
- Any authorized deviation from an item's text lands as a dated NOTES line under that item.

**Out of scope (deliberate):**

- The daemon, durable schedules that survive TUI quit, and any wire surface. A TUI-hosted
  schedule **dies with the TUI** — that is the honest v1 promise (TODO brief), and durability is
  the future daemon's value-add over the same library.
- Ask-Before / Allow-Edits schedules and any cross-session approval UI (decision 2).
- Carry-over context between firings (decision 5) and any scheduled-run preamble — no
  model-visible content is injected by the scheduler in v1; the prompt is the prompt.
- MCP tools inside firings (prescription above).
- The `apogee headless` subcommand itself — it becomes a thin CLI over `internal/run` in its own
  plan; this plan only builds the core it will reuse.
- Exporting `run`/`schedule` on the public facade (root aliases, ADR 0010). They stay
  `internal/` until an embedder asks — which also means the **external bench cannot reach them**
  (separate module, ADR 0001). Acceptable **only because** v1 injects no model-visible content;
  the export question re-opens the moment that changes (recorded in ADR 0033, item 1).
- Schedule persistence in config (`schedules:` keys), webhook/event triggers, retention of
  firing records (the browser's `d` and the existing no-pruning stance apply unchanged).
- Re-sorting or restructuring the `/sessions` browser around schedules — v1 labels rows
  (item 7); a grouped browsing mode is future work if labelling proves insufficient.

---

## 1. ADR 0033 + CONTEXT.md terms — the paper trail before the code — ✅ DONE (2026-08-04)

NOTES (2026-08-04): owner decision 8 ("a new ADR records the decisions") is recorded in ADR 0033
as a closing sentence of the Decision section rather than as an eighth numbered decision — inside
the ADR it would be purely self-referential; decisions 1–7 are numbered in ADR voice as written.

**What:**

- Write `docs/adr/0033-the-scheduler-is-a-library-and-the-tui-is-its-first-driver-surface.md`
  (Status: accepted): Context (the TODO brief; ADR 0031's invariants and its named headless-run
  consequence), Decision (the eight owner decisions from this plan's header, in ADR voice, plus
  two recorded scopings: **the at-completion save cadence** — a Firing saves once at completion,
  a deliberate scoping of ADR 0022's per-Turn cadence to the interactive TUI, not a silent
  contradiction of it — and **the invariant-4 posture** — v1 injects no model-visible content so
  nothing needs benching, and since the bench cannot import `internal/` (separate module,
  ADR 0001), future model-visible content lands as a catalogued Mechanism or forces the
  facade-export decision), Considered options (queue/concurrent overlap; Ask-Before schedules;
  carry-over context; runner private to the scheduler; capping schedule mode at the host's
  mode — each with the rejection reason from the grill), Consequences (the `internal/run` /
  `internal/schedule` / TUI-surface split; `apogee headless` shrinks to a thin CLI over
  `internal/run`; the fresh-per-firing answer feeds the ADR 0031 workflow-node question).
- Amend ADR 0031's "Open questions recorded, not decided" with a one-line pointer: the
  fresh-vs-resumed question is answered for the **scheduled-trigger** case by ADR 0033 (fresh);
  the workflow-node case stays open.
- `CONTEXT.md`: add **Schedule** (a standing instruction — prompt + cycle + mode — held by a
  Driver for its lifetime; TUI-hosted schedules die with the TUI) and **Firing** (one headless
  run of a Schedule's prompt: fresh Agent, fresh session record) to the concept map, cross-linked
  to ADR 0033 and the existing **Driver** / **Embeddable agent** entries. Keep both entries in
  the house voice with an _Avoid_ line each (e.g. avoid "cron job", "background task" — a
  Firing is a session, not a detached process).

**Tests:** none (docs only).

**Acceptance:** the three files exist and cross-link correctly
(`grep -l "0033" docs/adr/0031*.md CONTEXT.md` finds both); `make check` green.

**Commit:** `docs(adr): ADR 0033 — the scheduler is a library; the TUI is its first Driver surface`

---

## 2. Schedule identity on the session record's `Meta` — ✅ DONE (2026-08-04)

NOTES (2026-08-04): the tags are `json:"scheduleID,omitempty"` / `json:"scheduleName,omitempty"`
rather than the item's literal bare `json:",omitempty"` — the item's own parenthetical ("match the
file's existing tag style") wins, since every other `Meta` field names its key explicitly in
lowerCamel, and `internal/tui/transcriptcodec.go`'s `callID` sets the repo's ID-suffix casing.

**What:**

- `internal/session/store.go` — `Meta` gains two optional fields beside `Model`:
  `ScheduleID string` and `ScheduleName string`, both `json:",omitempty"` (match the file's
  existing tag style). Doc comment: empty means an ordinary session; the pair marks a record as
  one Firing of a Schedule (ADR 0033). **No `RecordVersion` bump** — the addition is
  backward-compatible both ways (older builds ignore unknown fields on load and never write
  them; decision 4).
- No caller changes: the TUI's `sessionHost.Save` (`cmd/apogee/wire.go` ~`:742`) leaves them
  zero, which is correct for every interactive session.

**Tests** (`internal/session`): a `Record` saved with both fields round-trips through
`Save`/`Load` and surfaces them in `List()`; a record written *without* the fields (legacy
bytes on disk) loads with both empty; a record with fields set is readable as valid JSON by the
current `RecordVersion` (no sentinel trips).

**Acceptance:** `go test ./internal/session/...`; `make check`.

**Commit:** `feat(session): schedule identity on browsable Meta`

---

## 3. `internal/run` — the one-firing headless runner — ✅ DONE (2026-08-04)

Depends on item 2.

NOTES (2026-08-04): three additions beyond the item's literal text, all inside its semantics.
(a) `Spec.Now` — an injectable clock (nil ⇒ `time.Now`) so the derived `— <HH:MM>` title and the
record's timestamps are pinnable in a test, the same seam `session.Store` and the TUI's session
host already carry. (b) `Config.Events` is pinned too, as a fourth forced delegate: `Once` wraps
the caller's sink (nil ⇒ discard) so a Firing satisfies construction with no observer — and the
record's `CtxUsed` comes from that tap's latest top-level `UsageEvent` rather than "from the
snapshot", since the fill is genuinely not in the snapshot and one type assertion in flight is
the cheap honest source. (c) A run ending in `StatusCancelled` is reported as `Result.Err`: a
cancel is not a loop error, but it is why the Firing has no answer, and an unattended caller has
no other way to tell. ADR 0033 needed no touch-up — its Consequences already record the
no-scrollback limitation this item's package doc restates.

**What:**

- New package `internal/run` — the shared core of a headless run, importing `internal/agent`,
  `internal/domain`, `internal/session` (downward only, ADR 0010; never the root facade, never
  `internal/tui`). Public shape (names are the implementer's within these semantics):

  - `run.Spec`: the agent `Config` (caller-composed — endpoint, model, workspace, mode,
    confinement posture), the prompt, optional schedule identity (`ScheduleID`,
    `ScheduleName`), an optional `*session.Store` (nil ⇒ run without persisting, the bench
    case), and a title (empty ⇒ derive: schedule identity present →
    `<schedule name> — <HH:MM>`; absent → the first-prompt heuristic, ≤50 chars,
    word-boundary truncate).
  - `run.Once(ctx, Spec) (run.Result, error)`: validate mode ∈ {Plan, Auto} (anything else is
    an error — decision 2); construct a **fresh** agent via `agent.New`; `Submit` the prompt;
    drive `Run(ctx)` to the quiescent boundary; `Snapshot`; build a `session.Record` with
    `Meta` filled (id via `session.NewID`, title, timestamps, workspace, model, schedule
    identity, user-message count 1, context fill from the snapshot if cheaply available);
    `Store.Save`; `Close` the agent. Save once at completion; on a run error, still save what
    completed and return both (the record notes the interruption via its ordinary state — no
    new record fields).
  - The Approver wired into the Config is a **fail-safe denier**: every request is denied with
    a recorded reason, counted on `run.Result`. Nothing can park (decision 2; ADR 0031
    invariant 2 — parking is a Driver composition, and this Driver's composition is "don't").
  - The other interactive host delegates are pinned the same way: **Asker `nil`** and
    **Presenter `nil`** — per CONTEXT.md a `nil` delegate simply unregisters `ask_user` /
    `present_document`, so nothing in a firing can rendezvous with a human; a deliverable is
    simply a file in the workspace, its path in the conversation. `run.Once` forces all three
    regardless of what the Spec's Config carries; a Spec cannot override them in v1.
  - `run.Result`: session record id (empty when unpersisted), title, turn count, denied-action
    count, and the run error if any.
- Auto-eligibility is **the caller's gate** (decision 3): `run.Once` trusts a Spec that says
  Auto, exactly as `agent.New` trusts a Config that says Auto — the ladder lives where the mode
  is chosen (item 5/6 for the TUI; the CLI plan later for headless). State this in the package
  doc.
- No event streaming in v1: the transcript blob is absent from the record (a firing's record
  resumes engine-only, the ADR 0022 legacy-shape degrade path — "resumed, no scrollback
  recorded" — which is correct and already handled). Record this limitation in the package doc
  and in the ADR 0033 Consequences (item 1 writes the ADR before this lands; if wording needs a
  touch-up, this item may append one NOTES-flagged sentence there).

**Tests** (`internal/run`): against a `httptest` fake OpenAI-compatible upstream (reuse the
existing fake-upstream pattern from `internal/agent`'s tests): a firing persists a record whose
`Meta` carries the schedule identity and derived title; two sequential `Once` calls construct
independent agents (fresh context — no state bleeds); nil `Store` persists nothing and still
returns a Result; a gated action under the denier fails the step visibly and increments the
denied count without hanging; a Spec whose Config carries an Asker or a Presenter still runs
with both unregistered (the pin is `run.Once`'s, not the caller's); mode `Ask-Before` in the
Spec errors before any request.
**Live-gated:** `TestRunOnceLive` under `APOGEE_LIVE_ENDPOINT` — one real Plan-mode firing
end-to-end, record on disk, skipped without the env var.

**Acceptance:** `go test ./internal/run/...`; `make check`.

**Commit:** `feat(run): one-firing headless runner over the engine API`

---

## 4. `internal/schedule` — the scheduler library — ✅ DONE (2026-08-04)

NOTES (2026-08-04): three additions beyond the item's literal text, all inside its semantics.
(a) `Spec` validation also refuses an empty prompt (`ErrPrompt`) — a Schedule with nothing to
submit is not a Schedule, and the library owns creation policy. (b) An empty `Spec.Name` is
DERIVED from the prompt's first line rather than refused, so item 5's `/schedule <cycle> [auto]
<prompt>` form — which collects no name — needs no naming policy of its own; the derivation is a
dozen lines of pure string work rather than a shared helper, since the existing title heuristics
live in `internal/run` and `internal/tui`, both closed to this package by ADR 0010. (c) The
"injected `now` + timer/ticker factory" seam is one `Clock` interface (`Now` + `NewTicker`)
rather than two independent fields: a fake `Now` paired with a real ticker is a bug, not a
configuration. Also: the in-flight flag clears in a deferred call just AFTER the completed event
goes out, so a surface can observe `completed` marginally before `List` reports the Schedule
idle — the defer covers a panicking runner, which is worth more than the microsecond.

**What:**

- New package `internal/schedule` — owns every when-and-how decision (ADR 0031 invariant 4 /
  ADR 0033). **Runner-agnostic**: it defines its own small seam types and does not import
  `internal/run` (a daemon composes its own runner; the TUI composes `run.Once` in item 6).
  Importing `internal/domain` for the mode type is fine; nothing TUI-side.
- Shape (names are the implementer's within these semantics):

  - `schedule.Spec`: display name, cycle (`time.Duration`, floor 30s enforced here — the
    library owns policy), prompt, mode (Plan|Auto — validated here too).
  - `schedule.Scheduler`: `Add(Spec) (id string, err error)` (multiple concurrent schedules —
    decision 7), `Stop(id) error`, `List() []Status` (id, name, cycle, mode, next-fire time,
    fired/skipped counts, in-flight flag), `Close()` (stops everything; TUI-lifetime semantics
    — nothing persists).
  - Injected seams at construction: `Fire func(ctx, Firing) (Outcome, error)` (the runner;
    `Firing` carries schedule id/name/prompt/mode, `Outcome` carries the saved record id/title),
    `Gate func(ctx) error` (blocks until the host is quiescent; nil ⇒ no deferral — decision
    7's defer-ticks), `Notify func(Event)` (event stream to the host: created, fired,
    completed, skipped, stopped, failed — each carrying the schedule id/name and, for
    completed, the Outcome), and a clock seam (injected `now` + timer/ticker factory) so every
    policy test runs on a fake clock.
  - Policy (the library's, tested here): per-schedule firings are strictly serial; a tick
    landing while that schedule's firing is in flight → skip + `Notify(skipped)` (decision 1);
    a due firing first waits on `Gate`, and further ticks arriving during that wait are skipped
    the same way (header prescription — at most one pending firing per schedule); `Stop` on an
    in-flight schedule cancels its pending wait and lets the current firing finish (it is a
    fresh agent mid-run; killing it is the ctx's job at `Close`); `Close` cancels all firing
    contexts and returns after the goroutines exit.

**Tests** (`internal/schedule`, all on the fake clock, run under `-race` via `make check`):
skip-on-overlap emits exactly one skipped event and the next tick fires; gate deferral holds a
due firing and collapses ticks that pile up behind it; two schedules fire independently (one
blocking gate does not starve the other — per-schedule gating); `Stop` mid-wait emits stopped
and never fires again; `List` reports next-fire and counts; `Close` is idempotent and joins all
goroutines; events arrive in per-schedule order.

**Acceptance:** `go test ./internal/schedule/...`; `make check`.

**Commit:** `feat(schedule): the scheduler library — cycles, skip policy, gate, lifecycle`

---

## 5. TUI surface — `/schedule`, `/schedule-stop`, pickers, notices — ✅ DONE (2026-08-04)

Depends on item 4.

NOTES (2026-08-04): five choices beyond the item's literal text, all inside its semantics.
(a) `matchCommand` now returns the line's RAW tail instead of pre-split tokens and `parsedInput`
gained `rest` beside `args` — a prompt is text a human wrote and a Firing submits verbatim, so
re-joining `strings.Fields` would silently re-space it; splitting moved to the one caller that wants
tokens, leaving a single splitter. (b) The picker's key routing widened from idle-only to
idle-or-running: the item makes both verbs `whileRunning`, and `/schedule <prompt>` mid-Exchange
would otherwise render a modal that claims no keys. The older kinds are unaffected — their verbs are
idle-only, and a picker cannot be open when a worker starts. (c) The seam is typed against
`internal/schedule` (Spec/Status/Event) rather than projected TUI values — the `SessionHost`
/`internal/session` posture, which keeps the library the single owner of every policy the surface
would otherwise restate. (d) The verbs stay SILENT on success: the item lists `created` and
`stopped` among the rendered event notices, so a confirmation from the command path too would
double every one of them; the created notice reads the cycle/mode/next-fire off `List()`.
(e) `pickerHintFor` replaces the shared `⏎ switch` legend for the three new kinds — none of them
switches anything. Docs: `README.md`'s command table, `layout.md` (the arg-taking verbs, the
while-running verbs, a paragraph on the three new panes) and `internal/tui/doc.go`. CHANGELOG
untouched — item 8 owns it.

**What:**

- `internal/tui/command.go` — two `commandSpecs` rows, alphabetical position preserved
  (`TestCommandSpecsReadAlphabetically` pins it): `schedule` (takes args, summary in the house
  voice, allowed while running — creating a schedule mid-Exchange is fine) and `schedule-stop`
  (no args, allowed while running).
- Parsing (`internal/tui`, beside the existing command parse):
  - `/schedule` bare → the status surface: with live schedules, note rows listing each (name ·
    cycle · mode · next fire · fired/skipped); with none, a usage note.
  - `/schedule <cycle> [auto] <prompt>` — first token parses as a Go duration ⇒ direct create
    (< 30s floor → error note); optional literal `auto` token selects Auto (gated — below);
    default mode Plan.
  - `/schedule <prompt>` (first token not a duration) → the popup path: cycle picker (presets
    `1m, 5m, 15m, 30m, 1h, 4h`) then mode picker (Plan / Auto), then create.
- Pickers (`internal/tui/picker.go`): new `pickerKind`s — `pickerCycle`, `pickerScheduleMode`,
  and `pickerScheduleStop` (rows = live schedules; `/schedule-stop` with exactly one live
  schedule stops it directly, with none emits a note, with several opens this picker). Three
  switch arms each (`pickerRows`, `pickerTitle`, `acceptPicker`), following the existing kinds.
- The Auto picker row (and the `auto` arg token) is gated by an **Auto-eligibility** value the
  host provides (decision 3 — same ladder as launching in Auto); ineligible ⇒ the row renders
  annotated/disabled and the arg form errors with the reason.
- The scheduler reaches the Model through a nil-able seam on `tui.Options` (the ADR 0011
  pattern — the renderer holds plain values and closures): a small interface with
  `Add`/`Stop`/`List`, plus the Auto-eligibility value. Nil seam ⇒ both commands report
  unavailability as a note, never an error (the autotitle nil-seam precedent).
- Scheduler events arrive as a tea message (`scheduleEventMsg`, carried by the same
  program-send route the heartbeat Monitor uses — wired in item 6) and render as **persisted**
  notes (they record something that happened — ADR 0022 addendum's ephemeral test): created,
  fired, completed (with the saved record's title), skipped, stopped, failed.
- Mind `internal/tui/doc.go`: no `strings.Builder` (or any no-copy type) by value anywhere the
  Model reaches.

**Tests** (`internal/tui`): the alphabetical-specs test updated; parse table — bare form,
duration token (valid, sub-floor, malformed), `auto` token, prompt-only; picker flows for all
three new kinds (rows/title/accept) against a fake seam; nil-seam note; eligibility-gated Auto
row; event-message → persisted note rendering (and that the notes encode into the transcript
blob).

**Acceptance:** `go test ./internal/tui/...`; `make check`.

**Commit:** `feat(tui): /schedule and /schedule-stop — pickers, status, notices`

---

## 6. Composition root — wire the scheduler live

Depends on items 3, 4, 5.

**What:**

- `cmd/apogee/wire.go` — build the scheduler beside `newTitleWiring` (~`:493`) and
  `newSessionHost` (~`:304`):
  - `Fire` adapts `run.Once`: at fire time (not create time) compose the agent `Config` from
    the **current** binding — server endpoint + model via the same holder the title wiring
    reads (`holder.Binding`), workspace and config home from the roots, the Firing's mode, and
    the same confinement posture an Auto *launch* gets (`confine-to-workspace` et al.). The
    session store is the existing one. No MCP tools (header prescription): the firing's
    registry is the library-side default (`internal/agent/construct.go` `hostTools`), not
    `registryWithMCP` — note the two-site `HostTools` trap in a comment only if a field is
    actually touched (none should be).
  - `Gate` defers a due firing until the interactive session is quiescent (decision 7). The
    release condition is **engine idle** — no in-flight Turn AND no queued continuation or
    interjection — never `StatusTurnComplete` alone: Exchanges span Turns (ADR 0025), so a
    per-Turn boundary can land mid-Exchange, and a firing released there contends with the live
    task the Gate exists to protect. The implementer chooses the seam — e.g. the Model publishes
    busy/idle transitions to the scheduler wiring at the transitions it already knows — keeping
    **policy in the library, state in the TUI**. Upstream contention while a firing runs is
    accepted and bounded (the autotitle posture: generous per-request patience, no retries
    hammering the slot).
  - `Notify` sends the tea message item 5 defined (the heartbeat Monitor's program-send route).
  - `tui.Options` gains the scheduler seam + Auto-eligibility value (from the same predicate
    the launch ladder uses, `resolveLadderAuto`'s eligibility half). TUI quit path calls
    `Scheduler.Close()` — schedules die with the TUI, firings' contexts cancelled.
- Firing records then appear in `/sessions` with schedule identity (items 2/3) — resuming one
  takes the ADR 0022 no-transcript degrade path by design (item 3).

**Tests:** wiring-level — a fake-upstream end-to-end in the highest existing harness that can
host it (the pattern of the existing wire/launch tests in `cmd/apogee`): create a schedule
through the seam, advance/trigger a firing, assert a record lands in the store with schedule
identity and a completed note reaches the transcript; TUI quit closes the scheduler (goroutine
leak check under `-race`). If `cmd/apogee` has no harness that can carry this cheaply, the
end-to-end lands as an `internal/tui`-level test over fakes plus a minimal wire smoke — record
the choice as a NOTES line.

**Acceptance:** `go test ./cmd/... ./internal/tui/...`; `make check`.

**Commit:** `feat(cmd): compose the scheduler — live firings from the TUI`

---

## 7. `/sessions` labels a Firing under its Schedule

Depends on item 2 (fields) — independent of items 3–6.

**What:**

- `internal/tui/sessions.go` — a row whose `Meta.ScheduleName` is set renders a schedule tag
  (e.g. `⟳ <name>`) in the existing row layout, truncation and width discipline included
  (ADR 0030: measure with the painter's measure — follow the surrounding code's `th.measure`
  usage, never `ansi.StringWidth`). Ordering, resume, rename and delete behaviour are
  unchanged; legacy and plain records render exactly as before. Re-sorting/grouping the browser
  is out of scope (header).

**Tests** (`internal/tui`): row rendering with and without schedule identity; a tagged row
truncates within the width authority under both width methods (follow the existing
`sessionRows` tests' pattern); plain records unchanged.

**Acceptance:** `go test ./internal/tui/...`; `make check`.

**Commit:** `feat(tui): label scheduled firings in the /sessions browser`

---

## 8. Close-out — the TODO trail and the CHANGELOG

Depends on items 1–7.

**What:**

- `TODO.md` — the `/schedule` bullet's body leaves the "apogee-code feature parity" entry (the
  entry itself stays — its other bullets are open); add the shipped-ledger line under that
  entry's "Shipped since parking" list pointing at ADR 0033 + this plan, in the ledger's house
  voice. No line under "Closed entries" — that section is for whole entries; the parity entry's
  own ledger is this bullet's trail (mirror how `/model`/`/server` bullets closed).
- `CHANGELOG.md` — feature lines under the current unreleased/top section in the file's
  existing voice: the `/schedule` + `/schedule-stop` surface, the scheduler library, the
  headless one-firing runner, schedule identity in `/sessions`. **Touch no release heading and
  no `VERSION`.**
- Verify every cross-reference this plan introduced resolves (ADR 0033 ↔ ADR 0031 ↔ CONTEXT.md
  ↔ TODO ledger line).

**Tests:** none (docs only).

**Acceptance:** `grep -n "schedule" TODO.md` shows the ledger line and no orphaned body;
`grep -n "0033" CHANGELOG.md TODO.md` resolves; `make check` green.

**Commit:** `docs: close the /schedule TODO bullet into ADR 0033 and the plan trail`

---

**Suggested version bump (owner decides, not an item):** minor — `v0.11.0`. A new user-facing
feature family (`/schedule`, `/schedule-stop`, `/sessions` labels) plus two new library
packages; no breaking change to the public surface.
