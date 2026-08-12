# Sub-agent server — delegations route to the flagged server with its own posture

- **Goal:** a smart model orchestrates the session while a cheaper model — possibly on
  another box — does the delegated grunt work: one `servers:` entry flagged
  `sub-agents: true` receives every delegation, carrying its own bypass/mechanisms
  posture, observed by a second heartbeat, degrading loudly to today's behaviour when
  unusable.
- **Date:** 2026-08-11
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **Authoritative sources:**
  - `docs/adr/0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md` —
    THE decision record (grill 2026-08-11); on any conflict with an item, ADR 0045 wins.
  - `CONTEXT.md` "Sub-agent server" and "Delegation target" — the ratified terms; both
    carry a "wiring lands via the sub-agent-server plan" marker item 9 removes.
  - ADR 0036 (servers list = single definition), ADR 0039 (per-server cap, one width
    everywhere), ADR 0024 (heartbeat observes, boundary applies), ADR 0031 (wire-silent
    engine, benchable-all-the-way-up), ADR 0042 (degrade, never block), ADR 0044 +
    plan `2026-08-11 - 05` (per-model profiles — executes FIRST, see item 1).
  - Key seams: `internal/agent/subagent.go:163-211` (`newChildAgent`; line 188
    `newAgent(childCfg, a.upstream)` is the routing seam), `internal/agent/rebind.go`
    `SwitchUpstream` (client-from-spec idiom), `internal/config/config.go:1027`
    (`ServerEntry`) + `ValidateServers`, `cmd/apogee/wire_server.go:65-91`
    (`serverBinder.bind`, `caps.follow`, `heartbeat.NewMonitor`).
- **Ratified design calls** (owner, 2026-08-11, via AskUserQuestion — recorded in ADR 0045):
  1. **Scope IN:** core routing + discovery; per-child mechanisms split. **DEFERRED**
     (dated 2026-08-11): model-chosen routing (a `sub_agent` tool param); llama-launcher
     actuation for the Sub-agent server.
  2. **Config:** `sub-agents: true` flag on ONE `servers:` entry; a second flagged entry
     is a `ValidateServers` startup error (duplicate-name reasoning).
  3. **Posture home:** `bypass:` + `mechanisms:` live ON the flagged entry, refused on
     an unflagged entry; present key replaces the inherited value WHOLE, absent key
     inherits the parent's LIVE value at spawn; posture rides the ROUTING — the
     parent's own location is irrelevant.
  4. **Discovery:** a second heartbeat monitor on the flagged entry latches an
     engine-side Delegation target (endpoint/key/model/per-slot window/cap/profile);
     entry `model:` stays a pin and `ServerEntry` GROWS a `context-window:` pin; the
     latch is mutex-read at spawn, never idle-gated.
  5. **Fallback:** unusable target ⇒ child runs on the parent's upstream WITH the
     parent's posture; ONE notice per routing state change, never per spawn.
  6. **Cap:** the RECEIVING server's `parallel-agents` bounds the fan-out and the
     guided-decomposition batch width; fallback engaged ⇒ the parent server's cap.
  7. **Invariant:** no defaults move (absent keys = today's behaviour), so no
     ADR 0006/0009 bench gate is owed.
  8. **Surfacing minimal:** the delegation's line shows the child's model when it
     differs from the parent's.
  9. **Terms:** "Sub-agent server" (the flagged entry), "Delegation target" (the latch).
- **Plan-writer calls** (recorded, not owner-ratified; veto by NOTES line):
  - `context-window:` is allowed on ANY `servers:` entry (it describes the server, like
    `model:`); only the Delegation-target resolution consumes it today. The posture keys
    alone are flagged-entry-only.
  - The Delegation-target latch is SHARED down the agent tree (a child holds the same
    handle), so a depth≥1 spawn snapshots the current target too — routing at every
    depth with one latch.
  - The child's mechanisms posture travels as a BUILT-registry factory on the spec
    (composition root translates the config map; the engine never reads config), nil =
    inherit via `ForSubAgent()` as today. Each call of the factory yields a fresh
    registry per child (the live-state-isolation rule `ForSubAgent` already encodes).
  - Notice spellings: `sub-agents: routing to <name> (<model>)` /
    `sub-agents: <name> unavailable — delegations run on the session server`.
- **Standing requirements:**
  - skills: coding-standards
  - Run `make check` before every commit.
  - Each user-visible item adds one bullet to `CHANGELOG.md` under `[Unreleased]`.
  - Never change `VERSION` or add a CHANGELOG release heading (VERSION-SUGGESTION at
    the end instead).
  - Any authorized deviation from item text lands as a dated NOTES line under the item.
- **Out of scope:**
  - Model-chosen routing and launcher actuation (deferred — ADR 0045 "Deferred").
  - Per-server mechanisms/bypass for the SESSION itself (posture keys are sub-agent-only).
  - Any change to the profile machinery beyond CONSUMING `profiles.Resolve` (ADR 0044).
  - Bench runs — no default moves, no gate owed (ADR 0045 §6).

## 1. Gate: the per-model-profiles plan is archived — ✅ DONE (2026-08-12)

**What:** Verify plan `2026-08-11 - 05` has been executed and archived:
`docs/plans/archived/2026-08-11 - 05 - per-model-profiles-plan.md` exists, ADR 0044
exists in `docs/adr/`, and package `internal/profiles` exists with `Resolve`. This plan
consumes `profiles.Resolve` and the profile-through-one-swap machinery that plan builds
(items 3, 5, 6 there). If ANY check fails, report BLOCKED — do not start item 2.

**Tests:** none (verification-only).

**Acceptance:** `ls "docs/plans/archived/2026-08-11 - 05 - per-model-profiles-plan.md"
docs/adr/0044-*.md internal/profiles/` all succeed.

Commit: none — verification-only item; nothing to commit.

**NOTES (2026-08-12):** the gate passed — archived plan `2026-08-11 - 05` (all 6 items
✅ DONE), ADR 0044 (`accepted`), and `internal/profiles` with `Resolve` (`match.go:53`) all
present; the one-swap `applyProfile` seam items 4/5 consume exists at
`internal/agent/setprofile.go:78` and is used by `rebind.go:147`. Deviation from "nothing
to commit": the ✅ marker above is itself committed, so the plan file keeps working as the
resume state. No code changed.

## 2. Config: `sub-agents:` flag, posture keys, `context-window:` pin on servers entries — ✅ DONE (2026-08-12)

**What:** In `internal/config` (`config.go:1027` `ServerEntry`): add
`SubAgents bool yaml:"sub-agents,omitempty"`,
`Bypass *bool yaml:"bypass,omitempty"`,
`Mechanisms map[string]bool yaml:"mechanisms,omitempty"`,
`ContextWindow int yaml:"context-window,omitempty"`.
`ValidateServers` grows three refusals, each in the file-defect voice the function
already speaks: (a) two entries flagged `sub-agents: true` — name BOTH entries; (b)
`bypass:`/`mechanisms:` on an UNflagged entry — name the entry and say the keys ride the
flag; (c) negative `context-window:` — the negative-`parallel-agents` wording.
`context-window:` is legal on any entry (plan-writer call). Add helper
`SubAgentServer(entries []ServerEntry) (ServerEntry, bool)` returning the flagged entry.
Update the `servers:` registry row's doc text (`registry.go`) to mention the new entry
keys if it enumerates them today.

**Tests:** parse an entry carrying all four keys; the three refusal messages (two flags;
posture on unflagged; negative window); `SubAgentServer` found/absent; existing
`ValidateServers` tests unchanged.

**Acceptance:** `go test ./internal/config/` passes; `make check` passes; CHANGELOG
bullet added (user-visible config surface).

**NOTES (2026-08-12):** two deviations from the item's literal text. (a) `registry.go` was NOT
touched: the `servers:` row's Desc ("name, endpoint, and what each one needs") does not enumerate
the entry keys today, so the item's "if it enumerates them" condition did not fire. (b)
`configmigrate.go`'s `serversAppended` had to change: `Mechanisms map[string]bool` makes
`ServerEntry` non-comparable, so its `==`/`slices.Equal` comparison no longer compiled and now goes
through `reflect.DeepEqual` (via `slices.EqualFunc`, which keeps nil and empty `before` reading
alike). The seeded template (`internal/config/defaults/config.yaml`) was deliberately left alone —
it would have to describe routing that does not exist until items 6-7.

Commit: `feat(config): sub-agents flag, posture keys, and context-window pin on servers entries`

## 3. Engine: the Delegation-target latch, shared down the agent tree — ✅ DONE (2026-08-12)

**What:** Depends on item 1. In `internal/agent`, new file `delegationtarget.go`: type
`DelegationTarget{Endpoint, APIKey, Model string; ContextWindow, ParallelAgents int;
Profile domain.ModelProfile; Bypass *bool; Mechanisms func() <the type
Config.Mechanisms/ForSubAgent already carries>}` plus an unexported mutex-guarded latch
holder. `(*Agent).SetDelegationTarget(*DelegationTarget)` stores it (nil clears);
`(*Agent).delegationTarget()` snapshots. Deliberately NEVER idle-gated — beats land
mid-Exchange and each spawn snapshots; document that contrast with Rebind/SwitchUpstream
on the setter. The latch HANDLE is shared down the tree: `newChildAgent`
(`subagent.go:163`) hands the child the parent's holder, so depth≥1 spawns read the same
current target (plan-writer call). No consumption yet — this item is the latch alone.

**Tests:** set/clear/snapshot under concurrent readers (`-race`); a child constructed
via `newChildAgent` observes a target set on the PARENT after the child's construction;
nil latch snapshot is nil.

**Acceptance:** `go test ./internal/agent/ -race` passes; `make check` passes; the
setter's doc comment states the never-idle-gated call and cites ADR 0045.

**NOTES (2026-08-12):** three files beyond the item's named ones changed, all of them the
repo's own conventions for a new exported engine surface. (a) `apogee.go` gained the
`DelegationTarget = agent.DelegationTarget` alias and `example_test.go` its completeness-guard
line: `SetDelegationTarget` is exported, so without the alias no external Driver — nor item 6's
`apogee.DelegationTarget` — could name its argument type. (b) `internal/agent/doc.go`'s file map
gained the `delegationtarget.go` line (and "Nineteen files" became "Twenty"), which
`TestDocMapNamesEveryFile` enforces. No CHANGELOG bullet: this item's Acceptance omits one, the
latch being engine-internal until item 6 wires it.

Commit: `feat(engine): delegation-target latch shared down the agent tree`

## 4. Engine: a routed spawn builds upstream, window, profile, and posture from the target — ✅ DONE (2026-08-12)

**What:** Depends on item 3. In `newChildAgent` (`subagent.go:163-211`): snapshot the
latch; when non-nil, the child is ROUTED — `childCfg.Endpoint/Model/APIKey/ContextWindow`
come from the target, the child's upstream is `provider.NewClient(target.Endpoint,
target.Model, provider.WithAPIKey(target.APIKey))` instead of `a.upstream` (the
`SwitchUpstream` idiom, `rebind.go`), the child's profile is `target.Profile` applied
through the same one-swap path the profiles plan built (its `applyProfile`), and posture
applies per ADR 0045 §2: `target.Bypass != nil` ⇒ that value, else the parent's live
`a.bypassEnabled()` as today; `target.Mechanisms != nil` ⇒ `childCfg.Mechanisms =
target.Mechanisms()` (a fresh registry per child), else `a.registry.ForSubAgent()` as
today. Nil latch ⇒ today's path VERBATIM (the fallback — no behavioural change). Reset
the child's token estimator when routed (different model — the `SwitchUpstream`
rationale). Update `newChildAgent`'s doc comment ("the SAME Upstream responder" sentence
becomes conditional, quote-reverse citing ADR 0045). If direct
`provider.NewClient` construction resists unit testing, an unexported client-factory
seam (package-level var, swapped in tests) is acceptable — mechanical choice,
implementer's latitude.

**Tests:** routed child carries the target's model/window; routed child's upstream is
NOT the parent's responder; profile applied (a delimited-profile target strips thinking
tags in the child's parse path); bypass override both ways + absent-inherits-live;
mechanisms factory used when present, `ForSubAgent` when nil; nil latch ⇒ existing
inheritance tests still green; a mid-Exchange latch swap affects only spawns AFTER it.

**Acceptance:** `go test ./internal/agent/ -race` passes; `make check` passes; no
comment in `internal/agent` still claims the child always shares the parent's upstream.

**NOTES (2026-08-12):** three deviations from the item's literal text, all of them the item's intent
met by construction rather than by a call. (a) The profile lands as `childCfg.Profile =
target.Profile` BEFORE `newAgent` instead of a post-construction `child.applyProfile(...)`:
construction runs the identical `processing.ParserFor` translation applyProfile runs (setprofile.go
calls it "exactly the construction path"), so routing through the swap door would translate twice
and leave the child holding the PARENT's parsers in between; setting it on the config keeps
`cfg.Profile` and the parse seam coherent from the child's first instant, and a bad profile still
fails loudly as "could not construct sub-agent". (b) No token-estimator reset was added: `newAgent`
already seeds every child with a fresh `apogeectx.NewTokenEstimator` and `newChildAgent` never
copies the parent's, so the stale-calibration hazard SwitchUpstream resets for cannot arise —
recorded as a comment at the construction site instead. (c) One line beyond the item's named file:
`subagent_test.go`'s header comment said the sub-agent "reuses the parent's Upstream" unconditionally,
which the Acceptance's "no comment still claims..." sweep catches, so it now names the unrouted
condition. No CHANGELOG bullet — this item's Acceptance omits one, routing being unreachable until
the item-6 wiring pushes a target.

**NOTES (2026-08-12):** retry — the Acceptance sweep ("no comment in `internal/agent` still claims the
child always shares the parent's upstream") had missed `agent.go:33`, whose concurrency paragraph still
read "The children share only the Upstream and the EventSink"; it is now conditional in the same
quote-reverse voice `subagent.go:147` uses (EventSink always, Upstream only while nothing is latched,
a routed child dialing its own client per ADR 0045). Sweep re-run over ALL of `internal/agent`
(comments matching upstream/responder/model/context-window/share claims about a child, test files
included): no other unconditional claim remains — `subagent.go:19` and `doc.go:48` speak of
PRIVILEGES (mode/approver/confiner/tools), which routing never widens, and
`routedspawn_test.go`/`subagent_test.go` already name the routed and unrouted conditions.

Commit: `feat(engine): routed sub-agents build upstream, window, profile, and posture from the delegation target`

## 5. Engine: routed fan-out width comes from the target's cap — ✅ DONE (2026-08-12)

**What:** Depends on item 4. In `internal/agent/dispatch.go` (width resolution around
lines 76-108) and the guided-decomposition batch sizing: when the latch snapshot at
dispatch time is non-nil, the depth-0 width is `target.ParallelAgents` (≥1); nil latch ⇒
`cfg.ParallelAgents` as today. One width everywhere (ADR 0039): the GD batch
`min(cap, remaining)` reads the same resolved number. Snapshot ONCE per reply dispatch
so one fan-out group never mixes widths mid-group.

**Tests:** fan-out width with latch present (target cap 3 ⇒ three concurrent) vs nil
(cfg cap governs) — extend `fanout_test.go`; GD batch width follows the target cap; a
latch cleared mid-group does not change that group's width.

**Acceptance:** `go test ./internal/agent/ -race` passes; `make check` passes.

**NOTES (2026-08-12):** four deviations from the item's literal text. (a) The guided-decomposition batch
sizing was NOT touched: GD's `min(cap, remaining)` reads `LoopView.ParallelAgents()`, which
`buildRequest`/`loopView` stamp from `delegationWidth()` — so routing the cap through that one function
delivers the same resolved number to the GD batch by construction, and a second rule in
`guideddecomposition.go` would be exactly the duplication ADR 0039's one-width-everywhere forbids. Pinned
at the stamping seam instead (`TestRoutedWidthReachesTheHookView`). (b) "Snapshot ONCE per reply dispatch"
likewise needed no code change: `dispatchTools` already resolved the width once and handed it DOWN as an
argument, so the property was structural — it is now documented on `fanOutWidth` and pinned by
`TestFanOut_LatchClearedMidGroupKeepsTheGroupWidth`. (c) Two doc comments in `agent.go` beyond the item's
named file: `SetParallelAgents` and `parallelAgentsCap` both claimed the session server's cap simply WAS
the depth-0 fan-out width, which this item falsifies; amended in the quote-reverse voice, pointing at
`delegationCap` as the choice. (d) `fanout_test.go`'s existing hand-built `&Agent{depth:…, parallelAgents:…}`
gained `delegation: &delegationLatch{}` — the new latch read nil-panics on a partially constructed Agent,
and `construct.go` is the only production constructor, so the test was corrected rather than the type made
nil-receiver-tolerant. No CHANGELOG bullet: this item's Acceptance omits one, routing staying unreachable
until item 6 wires a target.

Commit: `feat(engine): routed fan-out width comes from the delegation target's cap`

## 6. Wiring: a second heartbeat resolves the Delegation target from the flagged server — ✅ DONE (2026-08-12)

**What:** Depends on items 2, 4 (and 5 for the cap field being consumed). In
`cmd/apogee` (beside the session monitor wiring, `wire_server.go:65-91` /
`wire_live.go`): when `config.SubAgentServer` finds a flagged entry, run a SECOND
`heartbeat.NewMonitor(entry.Endpoint, entry.Model, entry.APIKey)` on the same beat
cadence as the session's. Each beat resolves a `apogee.DelegationTarget`:
pin-else-observe per field — `entry.Model` else the beat's bound model;
`entry.ContextWindow` else the beat's per-slot window; `entry.ParallelAgents` else
observed `total_slots` else 1 (the `caps.follow` ranks); `Profile` =
`profiles.Resolve(<resolved model>, userEntries, profiles.Shipped)` (ADR 0044 — the
composition root resolves, the engine never reads config); `Bypass` = `entry.Bypass`
verbatim; `Mechanisms` = nil when the entry has no map, else a factory the root builds
from `entry.Mechanisms` with the SAME builder `buildAgent` uses for the session's
catalogue. Push `engine.SetDelegationTarget(spec)` on a usable beat;
`SetDelegationTarget(nil)` on an unusable one (beat error / no model bound). No flagged
entry ⇒ no second monitor, latch never set — behaviour identical to today. A session
`/server`-switched ONTO the flagged entry is briefly double-observed — harmless, dedupe
optional (ADR 0045 consequences).

**Tests:** resolution unit tests at the wiring seam (pins beat observations; cap ranks;
profile filled from the resolved model; posture translated; unusable beat ⇒ nil push);
no-flag config ⇒ no monitor constructed. Live two-server verification is manual
(closing note).

**Acceptance:** `go test ./cmd/apogee/` passes; `make check` passes; CHANGELOG bullet
added (the feature's user-visible core).

**NOTES (2026-08-12):** five deviations from the item's literal "in `cmd/apogee`" scope. (a) The engine
gained an exported `BuildMechanisms(cfg, ids)` (`construct.go`, aliased on `apogee.go` with the
`example_test.go` guard line): the item's "the SAME builder `buildAgent` uses" is
`buildEnabledMechanisms` + the three stacking gates, and `Config.Mechanisms` takes a BUILT registry —
which the root cannot assemble without re-deriving the Deps (Library store, identity ladder) that
ADR 0015 §2 puts in the engine. It also reopens ADR 0031's benchable-all-the-way-up door: without it a
bench Driver latching its own target could not compose the Mechanisms posture at all. (b)
`lateEngine.SetDelegationTarget` REMEMBERS the target while unbound (the `pendingBypass` class) rather
than dropping it like `SetParallelAgents`: the second monitor beats from the moment the renderer
starts, which on a pre-bound session is before any Agent exists, so dropping it would leave a usable
Sub-agent server unrouted for up to a whole interval after the human picks their own. (c) The two
beats run SIDE BY SIDE — `delegationWiring.observe` starts the second one and `rootWiring.beat` joins
it — rather than in series: two five-second discoveries in series are exactly `heartbeat.Interval`,
and that package's no-overlap property rests on a beat staying strictly shorter than it. The cadence
is still the session's, driven by the renderer's one beat Cmd. (d) The `mechanisms:` catalogue is
built ONCE at startup (a defective map fails the run naming the entry, the posture `mechanismIDs`
already takes for the session's own block) and only the PROFILE is re-resolved per beat — a registry
is a per-run instance surface, not a per-model resolution, and a per-beat build error would land
where nobody can see it. (e) One line beyond this item's files in `CHANGELOG.md`: item 2's bullet
closed with "This release lands the config surface only — … the routing that consumes them follows",
which this item falsifies, so that sentence is gone and the new bullet states the routing. Also new:
`liveSettings.modelProfileEntries`, so the per-beat profile match reads the tier as it stands rather
than the launch snapshot.

Commit: `feat(apogee): second heartbeat resolves the delegation target from the flagged server`

## 7. Wiring: routing state-change notices and live config lifecycle

**What:** Depends on item 6. Two halves. (a) Notices: track the routing state
(engaged/lost) beside the second monitor; on each TRANSITION emit one session notice —
`sub-agents: routing to <name> (<model>)` on first usable beat and after recovery;
`sub-agents: <name> unavailable — delegations run on the session server` on loss —
through the same notice path the `context:` notice uses (ephemeral, not persisted).
Never per spawn (ADR 0045 §4). (b) Live config edits (ADR 0037/0041): a watcher reload
that adds the flag starts the monitor, removes it stops the monitor AND pushes
`SetDelegationTarget(nil)`, re-points it (different entry flagged) restarts against the
new entry; posture-only edits re-resolve on the next beat or immediately — implementer's
choice, but an edit must never leave a STALE posture latched past the next beat.

**Tests:** state machine emits exactly one notice per transition (usable→lost→usable);
reload add/remove/re-point drives monitor lifecycle and the nil push; posture edit
reaches the next resolved spec.

**Acceptance:** `go test ./cmd/apogee/` passes; `make check` passes; CHANGELOG bullet
added.

Commit: `feat(apogee): routing state-change notices and live config lifecycle for the sub-agent server`

## 8. Surfacing: the delegation line shows the child's model when it differs

**What:** Depends on item 4. Thread the routed child's bound model into the per-run
reading surfaces (CONTEXT.md "Sub-agent": the TUI's collapsed call block
`N tool calls · 12k/32k · <gist>` and the headless stderr
`sub-agent: <used>/<limit> · <name>` line): when the child's model differs from the
parent's, append ` · <model>` to both. Same-model delegations render exactly as today
(no noise when routing is off or identical). The vehicle is whatever event/record
already carries the per-run reading — add a model field where it travels; keep the
change minimal (ADR 0045 §7: minimal surfacing, first debugging clue).

**Tests:** TUI render test: differing model shows the suffix, same model shows none;
headless line test likewise; session-record round-trip keeps rendering after resume if
the reading is persisted (match however the existing reading persists).

**Acceptance:** `go test ./internal/tui/ ./cmd/apogee/` passes; `make check` passes;
CHANGELOG bullet added.

Commit: `feat(tui): delegation line shows the child model when it differs`

## 9. Docs closer: CONTEXT.md markers resolved, window claim made conditional

**What:** Depends on items 2-8. In `CONTEXT.md`: (a) remove the two
"wiring lands via the sub-agent-server plan" markers from the **Sub-agent server** and
**Delegation target** entries (the wiring now exists); (b) the **Sub-agent** entry's
"its **context window is not reduced**" sentence becomes conditional — inherited-verbatim
holds only for an UNrouted child; a routed child works against the Delegation target's
window (cite ADR 0045). Verify ADR 0045's Consequences match what landed (fix drift as
dated NOTES there only if found). No other doc changes.

**Tests:** none (docs).

**Acceptance:** `grep -c "wiring lands via the sub-agent-server plan" CONTEXT.md`
returns 0; `make check` passes.

Commit: `docs(context): sub-agent-server wiring landed — markers resolved, window claim conditional`

---

**Closing notes:**

- **Live verification (manual, owner-triggered):** two servers up (smart session server
  + flagged cheap server), one delegation-heavy task; expect the routing notice, the
  child model on the delegation line, child work on the cheap server (its logs), and a
  kill of the cheap server mid-session to produce the unavailable notice + parent
  fallback.
- **Deferred (dated 2026-08-11, ADR 0045):** model-chosen routing; launcher actuation
  for the Sub-agent server.
- **VERSION-SUGGESTION:** minor bump (new config surface + routing behaviour) — the
  next minor after whatever the profiles plan's own suggestion lands as; do not apply
  unasked.
