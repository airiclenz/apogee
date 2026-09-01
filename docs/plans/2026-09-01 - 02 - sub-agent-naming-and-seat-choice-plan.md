# sub-agent naming and seat choice — auto-named delegations, model-chosen seat

**Goal:** (A) A delegation the model leaves unnamed gets a short generated name from one
out-of-band completion on the child's own Upstream, applied everywhere the delegation is shown.
(B) With `sub-agents-choice: model`, the top-level model picks per delegation whether the child
runs on the session server or the Sub-agent server (`run_on`), guided by a per-entry
`description:` the orientation block relays.

**Date:** 2026-09-01
**Status:** unexecuted
**Sized for:** ~200k-context host

**Sources:**
- docs/adr/0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md (§3–§7, Deferred:108)
- docs/adr/0066-sub-agent-routing-follows-the-sub-agents-server-root-key.md (decision 7:88)
- docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md (decision 3:65)
- docs/adr/0022 addendum (naming call is no Mechanism, no Turn) · ADR 0031 (wire-silent engine)
- internal/title/title.go · cmd/apogee/title.go · internal/agent/subagent.go · cmd/apogee/delegation.go

**Ratified design calls** (owner, 2026-09-01):
- **Scope:** both features in one plan; the routing opt-out (`auto` row) already shipped and is struck from IDEAS.md.
- **Namer seat:** engine-injected `domain.DelegationNamer` (Approver pattern) + a `SubAgentNamedEvent`; the host implements it.
- **Namer server:** the child's own Upstream (routed ⇒ Sub-agent server, else session server); retries off, thinking off.
- **Timing:** concurrent with the child, bounded by its lifetime; a reply after the child finished is dropped.
- **Gate:** the existing `auto-title` key covers session titles AND delegation names; no new key.
- **Trigger:** only when the call gave no name; the `name` schema text is sharpened to ask for one.
- **Persistence:** the generated name replaces the view's retained target; a restored session paints it.
- **Choice set:** `run_on`: `session` | `sub-agents-server`; absent = the root key's rule; offered at depth 0 only.
- **Marker:** free-text `description:` per `servers:` entry; the orientation Delegations line describes both seats symmetrically.
- **Gate:** root `sub-agents-choice: fixed|model`, default `fixed`.
- **Mixed cap:** a reply fanning out to both seats is sized by min(session cap, target cap); a single-seat reply keeps its seat's cap.
- **Pickers:** `description:` shows in `/sub-agents-server` rows only.
- **Unusable ask:** `run_on: sub-agents-server` with no usable target falls back to the session server and appends one note line as the last line of the body, ahead of the steered trailer (ADR 0063 D3's trailer stays the final line) — never a prefix (amended at the regression checks 2026-09-01; the draft said "prefixes", the first re-check said "the result's LAST line").

**Regression check (2026-09-01, 5df3b032):**
- 2: recast — decision applied (the `SubAgentNamedEvent` fold row, its `foldCases` entry and internal/domain/doc.go land here so `TestFoldEventCoversEveryEventVariant` / `TestDocMapNamesEveryFile` stay green).
- 4: recast — decision applied (namer injected at wire_boot.go:171 AND `firingConfig`; Files: wire.go → wire_boot.go, + wire_firing.go, doc.go).
- 5: recast — guards folded (progressSaveTrigger cadence, no toolview.go:747 edit, frames live in cmd/apogee/testdata/frames); fold.go dropped per decision 2.
- 5: guard folded via item 2 — decision applied at the re-check (item 2's fold.go edit adds `domain.SubAgentNamedEvent` to the placeholder re-resolve switch at fold.go:68-70, so the `Message <name>…` placeholder follows the rename).
- 6: guard folded (the bite lives in run_test's eventTap; the headless test is the byte-identical pin).
- 7: guard folded (headless case on the real-runner pattern with item 4's firingConfig namer; title turn in naming.yaml; guard paths corrected); yields to docs/plans/archived/2026-08-09 - 00 - subagent-naming-and-newline-key-plan.md:57-60 (one-line `name` description).
- 8: recast — decision applied (note appended last; session-constant Delegations line; ADR 0023 §6 kept; header "Unusable ask" line amended).
- 9: recast — decision applied (the `sub-agents-choice` settingsTable apply lands here; acceptance runs the whole cmd/apogee package).
- 10: guard folded (locators: `HostTools` registry.go:21, `NewDefaultRegistryWithHost` :140, `builtinTools` :219, `NewSubAgent()` :246).
- 11: recast — decision applied; yields to internal/agent/subagent.go:44-46 / internal/tui/toolregistry.go:733-736 (the first-line marker is kept).
- 12: recast — decision applied; yields to ADR 0023 §6 (docs/adr/0023-…:292-295) and internal/agent/orientation.go:83-86 (per-session-constant rule kept).
- 13: recast — decision applied (`toolSetSpec.seatChoice`, `SetDelegationSeat` pushes, item 9's apply body → `setSeatChoice`).
- 14: recast — decision applied (hand-written two-entry home; note asserted as the LAST line; target-down beat case).

**Regression check (2026-09-01, 783a2704):**
- 2: decision applied (item 2's fold.go edit also adds `domain.SubAgentNamedEvent` to the placeholder re-resolve switch at fold.go:68-70).
- 4: recast — decision applied (`auto-title` is TUI-local: no settingsTable row; `tui.Options.OnAutoTitle` flips the namer's gate atomic; Files: − wire_settings.go, + tui.go, settingsapply.go, settingsapply_test.go); guard folded (the firing namer binds to `in.entry.Endpoint`/`spec.Model`/`apiKey`/`effortDialect`, wire_firing.go:201-212, `Routed` never true).
- 5: guard folded (open frame rule: every frame under cmd/apogee/testdata/frames that paints a sub_agent head — t04, t15, t17, t18 today — is byte-identical when no named event fires); the placeholder re-resolve moved to item 2 per decision 2.
- 8: recast — decision applied (the What now says the note is appended as the last line of the body, ahead of the steered trailer; ADR 0066's "every delegation routes" sentences at :32 and :37 are scoped to an absent `run_on` / `sub-agents-choice: fixed`).
- 9: decision applied (registry row carries a `Validate` enum hook; empty apply value ⇒ `fixed`; t16-settings-rows golden re-recorded here; row in registry order); guard folded (the apply records into `a.live` with `reaches: reachesTheHolder`, so the zero applier refuses).
- 11: recast — decision applied (note appended inside `delegationResult` after the outcome switch and BEFORE the steered-trailer append; `newChildAgent` unchanged over a new `newChildAgentOn`; a session-seated child spawns with a nil latch); yields to ADR 0063 D3 (docs/adr/0063-…:86 — the trailer is the final line), pinned by subagent_test.go:1641,1679 and toolregistry.go:742.
- 12: recast — decision applied (the What states the session-constant rendering once; the `unavailable now`, `DelegationTarget.Name/Description` and `fanOutWidth(calls …)` sentences are removed).
- 13: recast — decision applied (`ServerName`/`ServerDescription` filled at the wire_server.go bind site and on every `/server` switch; empty apply value ⇒ `fixed`; registry order); guards folded (wire_server_test.go's four whole-spec compares gain the fields; `lateEngine.SetDelegationSeat` remembers a `pendingSeat` while unbound — Files + wire_engine.go, wire_engine_test.go, wire_server.go, wire_server_test.go).
- 14: recast — decision applied (case (3) asserts the note as the last line of the body ahead of the steered trailer; case (4) drops the tool-schema assertion); guard folded (beats land every 10 s against a 5 s `tuitest.DefaultTimeout`: wait with `tuitest.Within(> heartbeat.Interval)` on the delegation.go:451 notice, gate case (2) on the :438 notice).

**Standing requirements:** skills: coding-standards. Deviations land as dated NOTES lines under the item.

**Out of scope:** routing to arbitrary entries or tiers (one Sub-agent server stays); per-entry monitors; launcher actuation; manual renaming of a delegation; session-title behaviour; bench campaign (apogee-sim); CHANGELOG (sidecars, closeout); VERSION.

---

## 1. ADR 0068 — delegations are named out of band + CONTEXT.md/manual wording — ✅ DONE (2026-09-01)

NOTES (2026-09-01): consequential edit — internal/title/title.go: made necessary by ADR 0068 — the package doc's blanket "the naming call … emits no events" claim is now scoped to the SESSION naming call, with 0068's event-emitting sibling named beside it (the item's regression guard). Comment text only; no code touched.
NOTES (2026-09-01): consequential edit — docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md: made necessary by ADR 0068 — the 2026-07-31 addendum's text is untouched and gains one `> **Amended 2026-09-01 by [ADR 0068](…)**` blockquote in ADR 0025's house idiom, so its "emits no TokenEvent or UsageEvent" claim reads as the session title's alone (the item's regression guard: "0068 named beside it").
NOTES (2026-09-01): the third `emits no events` grep hit, docs/plans/archived/2026-07-31 - 02 - session-auto-titling-plan.md:188, is left as written — it is an archived (executed) plan, i.e. historical record, and its sentence is already about the session-title generator specifically.
NOTES (2026-09-01): sessions.md takes the new paragraph as a list item of its own directly after the `/sessions` bullet that carries the auto-title text (plan cites :20-26) — a bare paragraph inside that bullet would have broken the list.

**What:** Write `docs/adr/0068-unnamed-delegations-are-named-out-of-band-on-the-childs-upstream.md`
(house form; read 0066 for shape; front matter `Amends: ADR 0022 (addendum — the naming call's
"emits no events" claim gains a sibling that emits one)`). Record calls: injected
`domain.DelegationNamer`, child's own Upstream, concurrent + lifetime-bounded, `auto-title` gate,
fires only when the call named nothing (Mechanism-synthesised delegations included), never under
a name the model gave, not a Mechanism (fires under Bypass, never touches the model's context),
one `SubAgentNamedEvent` per generated name, silent on every failure. Rejected: host-only
observer; naming before spawn; always-overwrite; a second key.
CONTEXT.md: **Sub-agent** (:106) gains the name rule — "the name its call gave, else a generated
one once it lands, else the task's first line"; add a **Delegation name** term beside it (Avoid:
"title" — that is the session's). `docs/manual/sessions.md:20-26` (auto-title) gains one paragraph:
the same switch names unnamed delegations; `docs/manual/headless.md:46` notes the generated name.
**Files:** docs/adr/0068-unnamed-delegations-are-named-out-of-band-on-the-childs-upstream.md, CONTEXT.md, docs/manual/sessions.md, docs/manual/headless.md
**Tests:** none (docs).
**Acceptance:** `test -f docs/adr/0068-*.md && grep -n 'Delegation name' CONTEXT.md && grep -n 'delegation' docs/manual/sessions.md`
**Regression guard.** Every sentence stating the naming call "emits no events" (grep `emits no events` docs/ CONTEXT.md internal/title/title.go) is either scoped to the SESSION title or re-worded; ADR 0022's addendum text stays as historical record with 0068 named beside it.
**Commit:** `docs(adr): 0068 — unnamed delegations are named out of band`

## 2. Domain seam + naming prompt: `DelegationNamer`, `SubAgentNamedEvent`, `title.DelegationPrompt` — ✅ DONE (2026-09-01)

NOTES (2026-09-01): the item's "add it to … `eventDepth()`" needed no edit — `eventDepth()` is a method on the embedded `EventBase`, so `SubAgentNamedEvent` satisfies the sealed `Event` interface by embedding it, as every other variant does. The "event doc comment list" half landed in internal/domain/doc.go's events.go line.

NOTES (2026-09-01): `titleWordBoundaryFloor` (a const derived from `MaxRunes`) became `wordBoundaryFloor(maxRunes int)` over a named `wordBoundaryFloorPercent = 60`, because `SanitizeTo`'s cap is now a parameter. The arithmetic is byte-identical at both caps (50 → 30, 40 → 24) and `Clip`'s own inline 60% rule was left untouched.

NOTES (2026-09-01): the `TestDelegationPrompt_*` tests are named with the plan's underscore form (matching `TestSanitizeTo_CapsAtTheGivenRunes`) rather than the package's usual unbroken style; `prompts/README.md`'s pin-test obligation names the new pin.

NOTES (2026-09-01): a second fold test beyond the plan's list — `TestFoldSubAgentNamedEventLeavesABorrowedBoxAlone` — pins the `decisionPending` half of the placeholder switch, so the new case cannot steal a box the human is answering an ask or approval with.

**What:** Recast at the regression check (2026-09-01). `internal/domain/naming.go` (new): `type DelegationNaming struct{ Task string; Routed bool }`,
`type DelegationNamer interface{ NameDelegation(ctx context.Context, req DelegationNaming) (string, error) }`;
`domain.Config` (config.go:82-86) gains `Namer DelegationNamer` beside `Approver` (nil ⇒ naming off).
`internal/domain/events.go`: `type SubAgentNamedEvent struct{ EventBase; Name string }` — EventBase is
the CHILD run's identity exactly as `SubAgentPhaseEvent`'s (Depth = child depth, CallID = spawn call);
add it to the event doc comment list and `eventDepth()`.
`internal/title`: `DelegationPrompt(task string, dialect provider.EffortDialect) provider.Request` —
system instruction from a new embedded `prompts/delegation-instruction.txt` ("name a delegated
sub-task for a status line: reply with 2–4 lowercase words naming the job, no punctuation, nothing
else"), user message = first `promptExcerptRunes` of the task, `titleTemperature`, `titleMaxTokens`,
`ThinkingEffort` off in `dialect`. `SanitizeTo(raw string, maxRunes int) (string, bool)` is the
cap-parameterised body of `Sanitize`; `Sanitize` stays as the `MaxRunes` wrapper; new exported
`MaxDelegateRunes = 40`. Package doc: "naming of a session or a delegation".
**Regression guard.** item 2 ALSO adds the `SubAgentNamedEvent` fold row in internal/tui/fold.go (paint-worthy) and its `foldCases` entry in internal/tui/fold_test.go so `TestFoldEventCoversEveryEventVariant` stays green after item 2; Files gain internal/tui/fold.go, internal/tui/fold_test.go and internal/domain/doc.go (docmap.Check: name naming.go there). Item 5 drops fold.go from its What and Files. Decision (2026-09-01 re-check): item 2's fold.go edit ALSO adds `domain.SubAgentNamedEvent` to the placeholder re-resolve switch at internal/tui/fold.go:68-70 (verified: today only SubAgentPhaseEvent/ToolResultEvent re-run setPlaceholder), so the `Message <name>…` placeholder follows the rename.
**Files:** internal/domain/naming.go, internal/domain/config.go, internal/domain/events.go, internal/domain/events_test.go, internal/domain/doc.go, internal/title/title.go, internal/title/title_test.go, internal/title/prompts/delegation-instruction.txt, internal/title/prompts/README.md, internal/tui/fold.go, internal/tui/fold_test.go
**Tests:** `TestDelegationPrompt_*` (excerpt cap, thinking off in each dialect, instruction text rides the system message); `TestSanitizeTo_CapsAtTheGivenRunes`; `TestSanitize_StillCapsAtMaxRunes`; events test that `SubAgentNamedEvent` reports the child depth; `foldCases` row for `SubAgentNamedEvent` (wantEntries 0, wantProgressSave true — progressSaveTrigger's arm, fold.go:199) so `TestFoldEventCoversEveryEventVariant` stays green; internal/domain/doc.go names naming.go (`TestDocMapNamesEveryFile`); fold_test: a `SubAgentNamedEvent` folded in the run view with no decision pending re-resolves the placeholder (the fold.go:68-70 switch gains the case).
**Acceptance:** `go build ./... && go test ./internal/domain/ ./internal/title/ && go test ./internal/tui/ -run FoldEvent`
**Regression guard.** `Sanitize`'s existing table tests pass byte-for-byte after the refactor; `title.Prompt` is untouched (diff shows no change to its body).
**Commit:** `feat(domain,title): DelegationNamer seam, SubAgentNamedEvent and the delegation naming prompt`

## 3. Engine: name an unnamed child concurrently, rename under lock, emit the event — ✅ DONE (2026-09-01)

NOTES (2026-09-01): consequential edit — internal/domain/approval.go: made necessary by the generated name reaching ApprovalRequest.SubAgentName; the field's doc said the name is only what the spawning call gave and is empty whenever the call carried none, which this item makes false.
NOTES (2026-09-01): consequential edit — internal/domain/ask.go: made necessary by the same rename reaching AskRequest.SubAgentName; identical false sentence corrected.
NOTES (2026-09-01): the event's `Turn` is the PARENT Turn the spawning call belongs to, read on the dispatch goroutine and handed to the goroutine — `runSubAgent` has no `turn` parameter, and adding one would have changed a signature four unlisted test files call. Depth+1 and CallID are exactly as the plan specifies.
NOTES (2026-09-01): the naming goroutine emits concurrently with the run, so the package's `recordingSink` (unsynchronised by the single-goroutine Agent contract) cannot be used for these tests; `naming_test.go` adds a locked `lockedSink` that also signals the first rename, and `TestDelegationName_RidesApprovalAndAsk` was switched to it. Tests that assert the rename ITSELF gate the child's own reply on that signal — a scripted child finishes in microseconds and would otherwise see its own name correctly dropped as late.
NOTES (2026-09-01): no `a.name` reader was found in the "usage-reading emitter" the plan names — `internal/agent` has exactly three sites (dispatch.go:838, :931 and the `newChildAgent` write), all now on `displayName()`/`setName()`. `run.SubAgentUsage.Name` is item 6's.

Depends on item 2.
**What:** `internal/agent/subagent.go` `runSubAgent`: after `a.children.register(call.ID, sub)`,
when `delegationName(args.Name) == ""` and `a.cfg.Namer != nil`, start ONE goroutine on a context
cancelled when `runSubAgent` returns (the child's lifetime): `name, err := a.cfg.Namer.NameDelegation(nctx,
domain.DelegationNaming{Task: args.Task, Routed: sub.ownsUpstream})`; on `err == nil`, fold through
`title.SanitizeTo(name, title.MaxDelegateRunes)`; an ok result calls `sub.setName(line)` and emits
`domain.SubAgentNamedEvent` through the PARENT's sink stamped like `emitSubAgentPhase`
(dispatch.go:351: Depth+1, CallID = call.ID); an error, an empty or a not-ok reply, or a cancelled
context drops silently (no event, no log). `cfg.Bypass` is never consulted. The child's `name`
(agent.go:290) becomes lock-guarded: `setName`/`displayName()` on `*Agent` (a `sync.RWMutex`), and
EVERY reader of `a.name`/`child.name` in internal/agent (grep `\.name\b` non-test: dispatch.go:838,
:931, subagent.go:504, the usage-reading emitter) goes through `displayName()`. Child config
inherits `Namer` verbatim (nested delegations are named too). The goroutine is joined before
`runSubAgent`'s deferred `sub.Close()` completes (a `sync.WaitGroup`), so no name lands on a closed child.
**Files:** internal/agent/subagent.go, internal/agent/agent.go, internal/agent/dispatch.go, internal/agent/delegationname_test.go, internal/agent/naming_test.go
**Tests:** `internal/agent/naming_test.go` with a stub namer: named call ⇒ namer never called; unnamed ⇒ called once with the task and `Routed` true for a routed spawn (routedspawn_test.go's `routingParent`) and false otherwise; reply ⇒ `SubAgentNamedEvent` at Depth 1 with the spawn CallID and `displayName()` == sanitised reply; a namer that blocks until the child finished ⇒ no event; error/empty reply ⇒ no event; nil `Namer` ⇒ nothing; approval/ask prompts after the rename carry the new name (`TestDelegationName_RidesApprovalAndAsk` gains a case). Run the package with `-race`.
**Acceptance:** `go build ./... && go test -race ./internal/agent/ -run 'Naming|DelegationName|RoutedSpawn|FanOut'`
**Regression guard.** `TestRoutedSpawnClosesItsOwnClient` and the fan-out suite unchanged; a spawn with `Namer == nil` produces the identical event stream to today (assert with the existing sink-stamp test).
**Commit:** `feat(agent): name an unnamed delegation out of band and emit SubAgentNamedEvent`

## 4. Host namer: one completion on the child's Upstream, gated by the live `auto-title`

Depends on item 2.
**What:** Recast at the regression check (2026-09-01). `cmd/apogee/naming.go` (new): `delegationNamer` implements `domain.DelegationNamer`.
Binding: `req.Routed` ⇒ the Sub-agent server's last-landed binding (endpoint, model, key, effort
dialect) — `delegationWiring` gains `routedBinding() (upstreamBinding, provider.EffortDialect, bool)`
recorded where `land` pushes a non-nil target (falls back to the session binding when none);
else `holder.Binding()` + `live.observedDialect`. Client exactly as `titleWiring.generate`:
`provider.NewClient(..., WithRequestTimeout(titleRequestTimeout), WithAPIKey, WithMaxRetries(0))`,
`respondDroppingThinkingOff`, `title.ErrTruncated` on a length-cut empty reply. Gate: an
`enabled func() bool` read at call time; when false the namer answers `"", nil` without a request.
The live value is the `auto-title` setting: the TUI applies that key LOCALLY (it never reaches
`Settings.Apply`), so `tui.Options.OnAutoTitle` — called from `settingsApplyLocal` — flips the
wiring's atomic (the TUI's own `settingKeyAutoTitle` handling stays for titles); seed the atomic
from `Options.AutoTitle`. Inject: `cfg.Namer = w.namer` where the engine Config
is assembled (the Approver's site), nil when `auto-title` is false at startup AND no live door
could flip it — i.e. always inject and let the gate answer. Registry Desc for `auto-title`
(registry.go:394-399) becomes "Name a new session from its first prompt, and an unnamed
delegation from its task, with one small extra completion each."
**Regression guard.** inject the namer at BOTH engine-Config sites — wire_boot.go:171 (TUI session) AND `firingConfig` (cmd/apogee/wire_firing.go:209, the Config headless.go:381 and Firings build) — so headless runs and Firings name delegations through the same namer; headless has no live settings door, so there the gate reads `Options.AutoTitle` at startup. Files gain cmd/apogee/wire_firing.go and cmd/apogee/doc.go (docmap.Check: name naming.go). Decision (2026-09-01 re-check): `auto-title` is applied LOCALLY by the TUI (internal/tui/settingsapply.go:250 `settingsApplyLocal`) and never reaches `Settings.Apply`, so item 4 adds NO settingsTable row for it; instead `tui.Options` gains `OnAutoTitle func(enabled bool)` (nil-safe), called from `settingsApplyLocal` beside `m.opts.AutoTitle = …`, and the host wires it to flip the namer's gate atomic (seeded from `Options.AutoTitle`). Files: add internal/tui/tui.go, internal/tui/settingsapply.go, internal/tui/settingsapply_test.go; drop cmd/apogee/wire_settings.go and wire_settings_test.go from item 4. Guard (folded): `firingConfig` composes no `delegationWiring` (wire_firing.go:294 passes MaxSteps only), so the firing namer binds to `in.entry.Endpoint` / `spec.Model` / `apiKey` / `effortDialect` (wire_firing.go:201-212) with `Routed` never true.
**Files:** cmd/apogee/naming.go, cmd/apogee/naming_test.go, cmd/apogee/delegation.go, cmd/apogee/wire_live.go, cmd/apogee/wire_boot.go, cmd/apogee/wire_firing.go, cmd/apogee/doc.go, internal/config/registry.go, cmd/apogee/settingsrows_test.go, internal/tui/tui.go, internal/tui/settingsapply.go, internal/tui/settingsapply_test.go
**Tests:** `naming_test.go` over `stubllm.New`: routed ⇒ the request lands on the target stub, unrouted ⇒ on the session stub; thinking off rides the dialect; gate off ⇒ no request; 4xx re-send once without the effort ask; length-cut empty ⇒ `title.ErrTruncated`; `firingConfig`'s Config carries the namer (a Firing/headless build with `auto-title: false` makes no naming request; with it true the request lands on the bound stub at `in.entry.Endpoint`/`spec.Model`, `Routed` false); cmd/apogee/doc.go names naming.go (`TestDocMapNamesEveryFile`). Registry/settings-row tests updated for the Desc. settingsapply_test: an `auto-title` edit calls `OnAutoTitle` with the parsed value and a nil hook is safe; naming_test: the host hook flips the gate (edit to false ⇒ the next naming makes no request).
**Acceptance:** `go build ./... && go test ./cmd/apogee/ -run 'Naming|SettingsRows|Delegation' && go test ./internal/config/ && go test ./internal/tui/ -run 'Settings'`
**Regression guard.** `titleWiring.generate` untouched; `TestSettingsRowsFormatEffectiveValues` pins the new Desc only where it pins Descs; retargeting between spawn and naming may name on the new box — acceptable, noted in naming.go.
**Commit:** `feat(apogee): host delegation namer on the child's Upstream, gated by auto-title`

## 5. TUI: fold `SubAgentNamedEvent` into the head, every reader, and the saved record

Depends on item 2.
**What:** Recast at the regression check (2026-09-01). `internal/tui/transcript.go`: fold `domain.SubAgentNamedEvent` (beside `addSubAgentPhase`,
:1251) — locate the head by CallID (`runHead`, subagentblock.go:131), set its `agentName` AND its
retained `target` to the name, mark the transcript dirty so the next progress save persists it
(session `ToolView.Target` already round-trips). The RULE: every TUI reader of a head's name —
grep `agentName\b|subAgentTarget\(|usageAgentName\(` in internal/tui — paints the folded name
after the event: collapsed block label, run-view breadcrumb, `Message <name>…` prompt placeholder,
activity slot (activity.go:449), `/usage` row, approval/ask prompt line. A restored session paints
the persisted target.
**Regression guard.** (i) There is no "mark the transcript dirty" mechanism: persistence rides progressSaveTrigger's arm (fold.go:199), which item 2's fold row supplies (wantProgressSave true) — item 5 touches fold.go nowhere (decision 2026-09-01). (ii) No edit at toolview.go:747 — that is the LIVE call presenter (`tv.agentName = subAgentName(args)`); the decode path is `fromWireToolView` (internal/tui/transcriptbridge.go:339), which already restores `w.Target` verbatim and needs no change; agentName stays off the wire (transcriptbridge_test.go:1173 pins a replayed agentName == "" and the member census). (iii) The frame rule is open, not a closed list: every frame under cmd/apogee/testdata/frames that paints a sub_agent head (today t04-step-cap-block.txt, t15-cancelled-delegation.txt, t17-run-view.txt and t18-run-view-finished.txt; there is no internal/tui/testdata) is byte-identical when no named event fires, proven by `go test ./cmd/apogee/ -run 'E2E'`. (iv) The `Message <name>…` placeholder re-resolves on `SubAgentNamedEvent` through the fold.go:68-70 case item 2 adds (decision 2026-09-01 re-check) — item 5 still touches fold.go nowhere; its placeholder test depends on item 2.
**Files:** internal/tui/transcript.go, internal/tui/toolview.go, internal/tui/subagentblock.go, internal/tui/activity.go, internal/tui/subagentblock_test.go, internal/tui/transcript_test.go
**Tests:** subagentblock_test: event before finish ⇒ block label, breadcrumb, placeholder and `/usage` row all show the name; event for an unknown CallID ⇒ no-op; save→restore round-trip (session codec) paints the generated name; a call that gave a name is unchanged by no event.
**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'SubAgent|Usage|Transcript|Restore' && go test ./internal/session/ && go test ./cmd/apogee/ -run 'E2E'`
**Regression guard.** `TestModelNoBuilderByValue` passes (no Builder introduced); existing subagentblock tests unchanged; every frame under cmd/apogee/testdata/frames that paints a sub_agent head is byte-identical when no named event fires.
**Commit:** `feat(tui): paint a generated delegation name everywhere the head is named, and persist it`

## 6. Run driver + headless: the generated name reaches `run.SubAgentUsage.Name`

Depends on item 2.
**What:** `internal/run/run.go` (SubAgentUsage :152): fold `SubAgentNamedEvent` by CallID into the
run's record so `Name` is the generated one when the child had none; `cmd/apogee/headless.go:540-552`
needs no change beyond the test — `headlessSubAgentTarget` already prefers `Name`. Document in
run.go's doc comment which events feed `Name`.
**Regression guard.** `TestHeadlessSubAgentLineUsesTheGeneratedName` cannot bite on its own — `headlessSubAgentTarget` (cmd/apogee/headless.go:638) already prefers `Name` and headless_test.go:894 already pins `sub-agent: 12k/32k · repo-scout` — so the headless test is the byte-identical pin only; the bite lives in run_test: the eventTap helpers `subAgentCall`/`usageAt`/`toolResult` (internal/run/run_test.go:633-675) with a `SubAgentNamedEvent` between call and result, asserting `Name` on `subAgentRuns()` (run.go:692).
**Files:** internal/run/run.go, internal/run/run_test.go, cmd/apogee/headless_test.go
**Tests:** run_test, through the eventTap: `subAgentCall`, `usageAt`, a `SubAgentNamedEvent` between call and `toolResult` ⇒ `subAgentRuns()` reports the generated `Name`; a name the call gave wins (no event). headless_test: `TestHeadlessSubAgentLineUsesTheGeneratedName` — the byte-identical pin that a `run.SubAgentUsage{Name: <generated>}` prints `sub-agent: <used>/<limit> · <generated name>`.
**Acceptance:** `go build ./... && go test ./internal/run/ && go test ./cmd/apogee/ -run 'Headless'`
**Regression guard.** `headlessSubAgentLines` output for a named/unnamed delegation without the event is byte-identical to today (pin both in the test).
**Commit:** `feat(run): fold the generated delegation name into the run's usage record`

## 7. Sharpen the `name` schema text + e2e journey (TUI frame and headless)

Depends on items 3, 4, 5, 6.
**What:** `internal/tools/sub_agent.go` `name` description becomes: "Short name for this
delegation, shown in the UI: 2–4 words naming the job, e.g. \"scout config keys\". Give one." (still
not required). E2E (`cmd/apogee/e2e_naming_test.go`, tuitest + stubllm per docs/design/test-drivers.md):
the session stub emits a `sub_agent` call WITHOUT a name, answers the child's Turn, and answers the
naming call (matched on the delegation-instruction system text) with `scout config keys`; assert the
frame's collapsed block reads `scout config keys` and, in a second headless run of the same script,
the stderr line carries it. A third case: `auto-title: false` ⇒ no naming request reaches the stub.
**Regression guard.** (i) No headless harness reaches a stubllm script (headless_test.go:66-98 swaps `runOnce` for a canned `stubRunner`): the headless case is built on the real-runner pattern — `runOnce = run.Once` + `writeConfigHome` (e2e_schedule_test.go:104-106) — and its namer is item 4's `firingConfig` injection. (ii) With `auto-title` on (registry.go:395, default true) the stub also receives the session-title request, and a naming.yaml answering only the three named calls 500s (`stubllm: no turn`, internal/stubllm/server.go:229): add a `last_message`-matched title turn (hostile.yaml:15-19 pattern) and assert the naming request by its `when.system` match and the request log. (iii) The item yields to docs/plans/archived/2026-08-09 - 00 - subagent-naming-and-newline-key-plan.md:57-60 — the `name` description stays one short line.
**Files:** internal/tools/sub_agent.go, internal/tools/sub_agent_test.go, cmd/apogee/e2e_naming_test.go, cmd/apogee/testdata/stubllm/naming.yaml
**Tests:** as above; the headless case runs the real runner (`runOnce = run.Once`) against the stub and naming.yaml carries the title turn; `sub_agent_test.go` pins the schema text.
**Acceptance:** `go build ./... && go test ./internal/tools/ -run SubAgent && go test ./cmd/apogee/ -run 'E2ENaming'`
**Regression guard.** internal/mechanisms/guideddecomposition.go:447's synthesised calls (`guidedDecompositionTaskArgs`) and internal/run/run.go:514 do not parse the description; the schema's `required` list is unchanged.
**Commit:** `feat(tools): ask the model for a delegation name; e2e proves the generated-name journey`

## 8. ADR 0069 — the top-level model picks the delegation seat; ADR 0045/0066/0039 amendments

**What:** Recast at the regression check (2026-09-01). Write `docs/adr/0069-the-top-level-model-picks-the-delegation-seat.md` (front matter
`Amends: ADR 0045 (Deferred: model-chosen routing), ADR 0066 (decision 7), ADR 0039 (decision 3 —
mixed-seat width)`). Decisions: two seats only (session server, Sub-agent server); `run_on`:
`session` | `sub-agents-server`, absent = the root key's rule; depth-0 only (nested delegations keep
identity, the child's tool carries no `run_on`); gate `sub-agents-choice: fixed|model` default
`fixed`; per-entry `description:` relayed by the orientation Delegations line for BOTH seats;
mixed reply width = min of both caps; `run_on: session` in a routed session runs unrouted with the
parent's posture (posture follows the routing, ADR 0045 §2 unchanged); an explicit
`sub-agents-server` ask with no usable target falls back per ADR 0045 §4 AND appends
`note: ran on the session server — the sub-agents server was unavailable` as the last line of the
result body, ahead of the steered trailer (ADR 0063 D3's trailer stays the final line). Rejected:
routing by entry name; closed tiers; error result on an unusable ask; two pools; a `seat` term.
Amend in place: ADR 0045:106-110 (Deferred item ⇒ "ratified by ADR 0069"), ADR 0066:88
(decision 7 ⇒ the consultation point is now used), ADR 0039 decision 3 note. CONTEXT.md: **Sub-agent
server** (:182) gains `run_on`; **Parallel agents** (:167) gains the mixed rule; **Orientation
block** (:673) gains the Delegations line; new term **Delegation seat** (the two places a delegation
may run; Avoid: "target" — the latched spec).
**Regression guard.** ADR 0069 records (a) the fallback note is APPENDED as the last line of the delegation result's body, ahead of the steered trailer (ADR 0063 D3's trailer stays the final line), never a prefix; (b) the orientation Delegations line renders session-constant facts only — entry name, `description:`, the entry's `model:` pin, the bound session model — with NO availability state, and moves only on the human doors (`/server`, `/model`, `/sub-agents-server`), so ADR 0023 §6's per-session-constant rule is KEPT (the scratch-dir move is the precedent; an unusable target is reported by the note, not the line). The header's "Unusable ask" ratified-call line is amended to say the note is appended as the last line of the body, ahead of the steered trailer (regression checks 2026-09-01). Decision (2026-09-01 re-check): item 8 also amends ADR 0066's "every delegation routes" sentences (lines ~32 and ~37) so they are scoped to a delegation whose `run_on` is absent (or `sub-agents-choice: fixed`).
**Files:** docs/adr/0069-the-top-level-model-picks-the-delegation-seat.md, docs/adr/0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md, docs/adr/0066-sub-agent-routing-follows-the-sub-agents-server-root-key.md, docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md, CONTEXT.md
**Tests:** none (docs).
**Acceptance:** `test -f docs/adr/0069-*.md && grep -n 'Delegation seat' CONTEXT.md && grep -n '0069' docs/adr/0045-*.md docs/adr/0066-*.md docs/adr/0039-*.md && grep -n 'run_on' docs/adr/0066-*.md`
**Regression guard.** Every doc sentence claiming routing is "human-chosen" or "model-chosen routing stays deferred" (grep -rn 'human-chosen\|stays deferred\|Model-chosen' docs/ CONTEXT.md) is amended or scoped to `fixed`.
**Commit:** `docs(adr): 0069 — the top-level model picks the delegation seat`

## 9. Config: `description:` on `servers:` entries and the `sub-agents-choice` root key

**What:** Recast at the regression check (2026-09-01). `internal/config/config.go`: `ServerEntry.Description string \`yaml:"description,omitempty"\``
(free text, trimmed in `canonicaliseServers`, never validated); root key `sub-agents-choice`
(fileConfig `SubAgentsChoice string`, `Options.SubAgentsChoice` typed `SubAgentsChoice` with
consts `SubAgentsChoiceFixed = "fixed"`, `SubAgentsChoiceModel = "model"`; absent ⇒ fixed;
any other value ⇒ startup error naming both values). Registry row: `Kind: KindEnum`, `EnumValues`
fixed|model, `Default: "fixed"`, `Editable: true`, file-only (no env/flag), Desc "Who picks where a
delegation runs: fixed = the sub-agents-server key; model = the top-level model may say run_on per
delegation." Registered under the sub-agents group beside `sub-agents-server`. Seeded template
(internal/config/defaults/config.yaml:196-208): a commented `# sub-agents-choice: fixed` with two
lines of prose, and `#     description: fast local 27B — search and edits` in the `servers:` example
(:166-180). `everyKeyFileConfig` and the registry bijection tests grow the key.
**Regression guard.** item 9 ALSO adds the `sub-agents-choice` `settingsTable` row in cmd/apogee/wire_settings.go — parse fixed|model, record the value, note "applies at the next roster build" — so every editable key has its apply at item 9 and `go test ./cmd/apogee/` stays green; item 13 replaces that apply's body with the `setSeatChoice` call. Files gain cmd/apogee/wire_settings.go and cmd/apogee/wire_settings_test.go. Decision (2026-09-01 re-check): the registry row carries a `Validate` (enum) hook; an empty apply value maps to `fixed`; the settings-screen golden frame the regression-3 report names is regenerated in item 9; the settingsTable row is inserted in registry order (TestSettingsTableIsInRegistryOrder). Guard (folded): the interim apply records into `a.live` with `reaches: reachesTheHolder` (`a.live != nil`, wire_settings.go:1599) so `TestApplySettingRefusesEveryKeyItCannotReach` (wire_settings_test.go:780) gets its refusal from the zero applier — the key is NOT added to `settingKeysWithNoMemberToReach` (:363); the golden is cmd/apogee/testdata/frames/t16-settings-rows.txt, re-recorded with `-update` (internal/tuitest/golden.go:114-126) once the row lands.
**Files:** internal/config/config.go, internal/config/options.go, internal/config/registry.go, internal/config/config_test.go, internal/config/registry_test.go, internal/config/defaults/config.yaml, cmd/apogee/settingsrows_test.go, cmd/apogee/wire_settings.go, cmd/apogee/wire_settings_test.go, cmd/apogee/testdata/frames/t16-settings-rows.txt
**Tests:** parse `sub-agents-choice: model`, absent ⇒ fixed, `banana` ⇒ error text names `fixed` and `model`; `description:` round-trips and is trimmed; settings rows show `fixed` by default; the edit transaction preserves a `description:` line on an entry edit (configedit tests); the `sub-agents-choice` apply records `fixed`/`model` (empty ⇒ `fixed`) and reports "applies at the next roster build" (`TestEveryEditableSettingKeyHasAnApply` stays green); the row's `Validate` hook refuses `banana` (`TestRegistryValidateHooksSitOnEditableKeys` stays green); the zero applier refuses the key (`TestApplySettingRefusesEveryKeyItCannotReach`); `TestSettingsTableIsInRegistryOrder` stays green; the t16-settings-rows golden re-recorded with the new row.
**Acceptance:** `go build ./... && go test ./internal/config/ && go test ./cmd/apogee/`
**Regression guard.** `config.SaveSubAgentsServer`/`ResetSubAgentsServer` splices leave a `description:` line untouched (pin in the splice tests); `ValidateServers` is unchanged.
**Commit:** `feat(config): servers description and the sub-agents-choice key`

## 10. Tools: the `run_on` schema variant and `OffersSeatChoice`

**What:** `internal/tools/sub_agent.go`: `SubAgentArgs` gains `RunOn string \`json:"run_on"\``;
exported consts `RunOnSession = "session"`, `RunOnSubAgentsServer = "sub-agents-server"`.
`NewSubAgentWith(opts SubAgentOptions{SeatChoice bool}) *SubAgent`; `NewSubAgent()` = the plain
variant. With `SeatChoice`, the schema adds `"run_on": {"type":"string","enum":["session","sub-agents-server"],
"description":"Optional; where this delegation runs — see the Delegations line of the host orientation. Leave unset for the configured default."}`.
`func (t *SubAgent) OffersSeatChoice() bool`. `HostTools` (registry.go:21) gains `SubAgentSeatChoice bool`
consumed by `builtinTools` (:219; its `NewSubAgent()` line :246). `PromptArgKeys` unchanged (`run_on` is not a prompt).
**Regression guard.** Locators corrected: `HostTools` is declared at internal/tools/registry.go:21 (:140 is `NewDefaultRegistryWithHost`); `builtinTools` is :219 and its `NewSubAgent()` line is :246 — the option is consumed there.
**Files:** internal/tools/sub_agent.go, internal/tools/sub_agent_test.go, internal/tools/registry.go, internal/tools/registry_test.go
**Tests:** schema with/without the parameter; the plain variant's schema is byte-identical to today's; `OffersSeatChoice` reports the variant; registry honours the option.
**Acceptance:** `go build ./... && go test ./internal/tools/`
**Regression guard.** Every consumer of `SubAgentToolName`/`SubAgentArgs` (subagent.go, loop.go:1380, guideddecomposition.go, run.go:514) compiles unchanged; a synthesised call without `run_on` decodes to `RunOn == ""`.
**Commit:** `feat(tools): sub_agent offers run_on when the host enables seat choice`

## 11. Engine: resolve the seat at spawn, fallback note, plain variant for children

Depends on item 10.
**What:** Recast at the regression check (2026-09-01). `internal/agent/subagent.go`: `runSubAgent` parses `args.RunOn`: `""` ⇒ default seat;
`RunOnSession` ⇒ unrouted spawn (skip the latch; parent's Upstream, window, posture, dialect);
`RunOnSubAgentsServer` ⇒ routed spawn when a target is latched, else unrouted with
`sub.seatFallback = true`; any other value ⇒ error tool result `invalid run_on %q: want "session"
or "sub-agents-server"`. `newChildAgent(spawnCallID, task, name)` stays UNCHANGED as the
default-seat wrapper over a new `newChildAgentOn(seat, spawnCallID, task, name)` (seat = an
unexported enum) that branches at :397 on the seat instead of on the latch alone; `runSubAgent`
calls the latter. The fallback note's placement is stated once, in the guard below. The child's roster (:569 filter) replaces the sub_agent tool with
`tools.NewSubAgent()` (plain) whenever the parent's offers seat choice, so `run_on` is offered at
depth 0 only; a `run_on` a child sends anyway is ignored (identity rule).
**Regression guard.** (a) owner-ratified 2026-09-01 (re-check): the fallback note is appended INSIDE `delegationResult` right after the outcome switch (subagent.go:265) and BEFORE the steered-trailer append — "the last line of the body, ahead of the steered trailer" — so ADR 0063 D3's trailer stays the final line and `delegationSteeredTail`/`delegationFailure` keep matching; the "prefixes … \n\n" sentence is deleted from the What so the placement is stated once; the header's "Unusable ask" ratified-call line reads the same words; the test pins body-last-line + trailer-final on a steered fallback child. (b) keep `newChildAgent(spawnCallID, task, name)` UNCHANGED as the default-seat wrapper over a new `newChildAgentOn(seat, spawnCallID, task, name)`; `runSubAgent` calls the latter; no test caller changes. (c) a session-seated child spawns with a nil latch of its own, so its nested delegations stay on the session server (identity rule). The note is never prefixed, so the head-anchored delegation recognisers in internal/tui/toolregistry.go keep matching; a test pins that a fallback result still classifies exactly as a plain one (the recognisers: `delegationStepCapHead`, internal/tui/toolregistry.go:736, and `delegationFailure`, :801). The item yields to the documented first-line marker — internal/agent/subagent.go:44-46 (`stepCapResultFormat`) and internal/tui/toolregistry.go:733-736 (anchored at the start, where the engine writes it) — and to ADR 0063 D3 (docs/adr/0063-sub-agent-runs-are-user-addressable-views.md:86: the trailer is the result's final line on every outcome; pinned by internal/agent/subagent_test.go:1641,1679 and read $-anchored by internal/tui/toolregistry.go:742) — neither is superseded.
**Files:** internal/agent/subagent.go, internal/agent/routedspawn_test.go, internal/agent/seat_test.go, internal/tui/toolregistry_test.go
**Tests:** `seat_test.go`: session ask under a latched target ⇒ child on the parent's client with parent posture; sub-agents-server ask ⇒ routed; sub-agents-server ask with nil latch ⇒ unrouted + the note is the last line of the result BODY (exact string), and on a steered fallback child the steered trailer is still the final line (body-last-line + trailer-final pinned); a session-seated child's nested delegation stays on the session server (nil latch); bad value ⇒ exact error; child roster's sub_agent schema lacks `run_on`; a child's `run_on` is ignored; every existing three-arg `newChildAgent` test caller compiles unchanged. internal/tui/toolregistry_test.go: a step-capped and a faulted fallback result (note appended) classify through `delegationStepCapHead` / `delegationFailure` exactly as the plain ones do.
**Acceptance:** `go build ./... && go test -race ./internal/agent/ -run 'Seat|RoutedSpawn|Unrouted' && go test ./internal/tui/`
**Regression guard.** All ten routedspawn tests pass unchanged with `RunOn == ""`; `delegationResult` for a non-fallback child is byte-identical (pin).
**Commit:** `feat(agent): resolve the delegation seat per call, with a fallback note`

## 12. Engine: mixed-seat width and the orientation Delegations line

Depends on items 10, 11.
**What:** Recast at the regression check (2026-09-01). New `fanOutWidthFor(calls []domain.ToolCall) int` beside the UNCHANGED `fanOutWidth(delegations int)` (dispatch.go:87; caller :56 moves to the new one): classify each
call's seat as item 11 does (with the latch state); a reply whose children land on BOTH seats is
sized `min(a.parallelAgentsCap(), target.ParallelAgents)`; single-seat ⇒ `fanOutWidth(len(calls))`;
`delegationWidth()`/`LoopView.ParallelAgents` (loop.go:877, :1408) stay the default seat's width
(documented as the batch hint). Orientation (orientation.go): asset gains line 5
`- Delegations: run_on "session" = %s; run_on "sub-agents-server" = %s; unset = %s. Keep judgment-heavy sub-tasks (review, design, ambiguous investigation) on the stronger seat and send mechanical ones (search, mechanical edits, running tests) to the other.`
where each seat renders `<model> on <entry name> — <description>` (description clause omitted when
empty; the sub-agents seat's `<model>` is the entry's `model:` pin only, clause omitted when unpinned;
no availability form — an unusable target is reported by item 11's note); `unset =` names
`sub-agents-server` when a seat is installed, else `session`. The line is rendered ONLY when the roster's
sub_agent tool `OffersSeatChoice()` (the roster is the single carrier of the gate). Engine facts:
the sub-agents seat is a `DelegationSeat{Name, Description, Model string}` installed through
`SetDelegationSeat(*DelegationSeat)` (`DelegationTarget` gains NO fields); `UpstreamSpec` (rebind.go:294) and
`domain.Config` gain `ServerName, ServerDescription string`; `SwitchUpstream` installs them;
`orientationLineCount` bumps.
**Regression guard.** (i) keep `fanOutWidth(delegations int)` UNCHANGED; add `fanOutWidthFor(calls []domain.ToolCall) int` that classifies each call's seat and returns `min(a.parallelAgentsCap(), target.ParallelAgents)` for a mixed reply, else `fanOutWidth(len(calls))`; dispatch.go:56 calls the new one; `TestFanOutWidth_BoundsTheGroup` untouched. (ii) The Delegations line renders SESSION-CONSTANT facts only: session seat = bound model + `ServerName` + `ServerDescription` (new `domain.Config` and `UpstreamSpec` fields, moving only on `/server`/`/model`); sub-agents seat = a new `DelegationSeat{Name, Description, Model string}` installed through a new `SetDelegationSeat(*DelegationSeat)` door (internal/agent/delegationseat.go, named in internal/agent/doc.go), held beside the latch but NEVER written by the beat — nil when the key is unset or names nothing; `Model` is the entry's `model:` pin only (clause omitted when unpinned); NO `unavailable now` form (item 11's note covers an unusable target); `unset =` names `sub-agents-server` when a seat is installed, else `session`. `DelegationTarget` gains NO fields — drop delegationtarget.go from Files. Decision (2026-09-01 re-check): the session-constant rendering is stated once — the pre-recast sentences (`unavailable now`, `DelegationTarget.Name/Description`, `fanOutWidth(calls …)`) are removed from the What so the What and this guard agree. ADR 0023 §6 is kept: the line moves only on the human doors (the scratch-dir precedent); a test pins the rendered block byte-identical across a target-down/up beat. The item yields to ADR 0023 §6 (docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md:292-295) and internal/agent/orientation.go:83-86 — the per-session-constant rule is not superseded.
**Files:** internal/agent/dispatch.go, internal/agent/orientation.go, internal/agent/prompts/orientation.txt, internal/agent/delegationseat.go, internal/agent/doc.go, internal/agent/rebind.go, internal/domain/config.go, internal/agent/fanout_test.go, internal/agent/orientation_test.go
**Tests:** `TestFanOutWidth_MixedSeatsTakeTheSmallerCap` over `fanOutWidthFor` (single-seat replies delegate to `fanOutWidth`; `TestFanOutWidth_BoundsTheGroup` untouched); orientation: line absent on the plain tool; present with both seats and description clauses, the `Model` clause only when the entry pins one, `unset =` per the installed seat, and after `SwitchUpstream` carries a new name; `SetDelegationSeat(nil)` drops the sub-agents clause; the rendered block is byte-identical across a target-down/up beat (latch cleared and re-landed, seat untouched); `TestOrientation_SubAgentInheritsTheBlock` extended (child sees no Delegations line).
**Acceptance:** `go build ./... && go test -race ./internal/agent/ -run 'FanOut|Orientation|Seat|Routed'`
**Regression guard.** With the plain tool the rendered orientation block is byte-identical to today (pin the full block in a test); `TestFanOutWidth_BoundsTheGroup` and `TestDelegationCapPicksTheGoverningServer` unchanged.
**Commit:** `feat(agent): mixed-seat fan-out width and the orientation Delegations line`

## 13. Host wiring: gate → roster, seat facts → engine, picker description cell

Depends on items 9, 10, 12.
**What:** Recast at the regression check (2026-09-01). `cmd/apogee/wire_tools.go`: `registryWithMCP` sets `HostTools.SubAgentSeatChoice` from
`Options.SubAgentsChoice == model`; `liveTools.setSeatChoice(on bool, engine)` → `rebuildWith`
(SwapTools, idle-only). `wire_settings.go`: a `sub-agents-choice` row with `reaches: reachesTheSwapDoor`
whose apply parses fixed|model and calls `setSeatChoice`; mid-run refusal lands on the row as
`tools.disabled`'s does. `delegation.go` pushes the seat facts (entry name, `description:`, `model:` pin) through
`SetDelegationSeat`; the session's `ServerName`/`ServerDescription` are filled where the session's
entry is BOUND (the cmd/apogee/wire_server.go bind site, :161, with the `entry` in hand) and on
every `/server` switch (`sessionMover.move`, upstream.go:245, from the `entry` in hand).
Picker: `tui.ServerChoice` gains `Description`; `Delegation.Targets()` fills it;
`subAgentsServerRows` (picker.go:539) renders the second cell `— <endpoint>` + ` · <description>`
when non-empty; `/server` rows untouched.
**Regression guard.** `toolSetSpec` (wire_live.go:122) gains `seatChoice bool` seeded from `Options.SubAgentsChoice`, and `registryWithMCP` takes it through the spec, not through `apogee.Config`; `delegation.go` pushes the seat facts through `SetDelegationSeat` at construction, in `relist` and in `Retarget` (nil on unset or a name matching nothing) — `resolveDelegationTarget` is untouched; the `sub-agents-choice` apply body added by item 9 now calls `setSeatChoice`. Decision (2026-09-01 re-check): `ServerName`/`ServerDescription` are filled where the session's entry is BOUND (cmd/apogee/wire_server.go bind site — added to Files) and on every `/server` switch; an empty `sub-agents-choice` apply value means `fixed`; the row keeps registry order. Guards (folded): (i) `sessionMover.move` filling the two fields lands in the four whole-`UpstreamSpec` `!=` compares in cmd/apogee/wire_server_test.go (:735, :937, :1468, :1508) — that file joins Files and the four wantSpecs gain `ServerName: <entry name>` (+ `ServerDescription`); (ii) `delegationSetter` (cmd/apogee/delegation.go:82) gaining `SetDelegationSeat` needs `*lateEngine` (cmd/apogee/wire_engine.go:381) to implement it: `lateEngine.SetDelegationSeat` remembers a `pendingSeat` while unbound and replays it at bind, exactly as `pendingDelegation` does (:383-388) — a push forwarded to a nil agent is never lost; Files gain cmd/apogee/wire_engine.go and wire_engine_test.go.
**Files:** cmd/apogee/wire_tools.go, cmd/apogee/wire_settings.go, cmd/apogee/delegation.go, cmd/apogee/wire_live.go, cmd/apogee/upstream.go, cmd/apogee/wire_server.go, cmd/apogee/wire_engine.go, cmd/apogee/delegation_test.go, cmd/apogee/wire_settings_test.go, cmd/apogee/wire_server_test.go, cmd/apogee/wire_engine_test.go, internal/tui/tui.go, internal/tui/picker.go, internal/tui/picker_test.go
**Tests:** delegation_test: `SetDelegationSeat` receives the entry's name, `description:` and `model:` pin at construction, on `relist` and on `Retarget`, and nil when the key is unset or names nothing (`resolveDelegationTarget` unchanged); settings apply `model` ⇒ SwapTools called with a roster whose sub_agent offers seat choice, `fixed` (and the empty value) ⇒ plain; `TestSettingsTableIsInRegistryOrder` stays green; wire_engine_test: a seat pushed before bind is replayed at bind (`pendingSeat`), and one pushed after bind reaches the Agent; wire_server_test: the bind site and the four `sessionMover.move` wantSpecs carry the entry's `ServerName`/`ServerDescription`; picker rows show the description cell only when set; `/server` rows unchanged (pin).
**Acceptance:** `go build ./... && go test ./cmd/apogee/ -run 'Delegation|Settings|Tools|Server|Engine' && go test ./internal/tui/ -run 'Picker|SubAgentsServer'`
**Regression guard.** Startup with `sub-agents-choice` absent builds the same roster as today (pin the tool list); `Retarget`/`relist` behaviour unchanged.
**Commit:** `feat(apogee): wire sub-agents-choice, seat facts and the picker description`

## 14. Manual + e2e: seat journey, fallback note, announced Delegations line

Depends on items 12, 13.
**What:** Recast at the regression check (2026-09-01). `docs/manual/configuration.md`: `description` joins the per-entry keys list (:562-570); a
`### Letting the model pick the seat` subsection after the `sub-agents-server` narrative (:600-650)
documents `sub-agents-choice`, `run_on`, the Delegations line, the fallback note and the mixed width;
`docs/manual/commands.md:45` mentions the description cell. E2E (`cmd/apogee/e2e_seat_test.go`, two
`stubllm` servers, `sub-agents-choice: model`, `sub-agents-server` naming the second): (1) the session
stub delegates with `run_on: "session"` ⇒ the child's request lands on the SESSION stub; (2)
`run_on: "sub-agents-server"` ⇒ the target stub; (3) target stub down ⇒ session stub + the parent's
next request carries the exact note line as the last line of the delegation result's body, ahead of
the steered trailer; (4) `sub-agents-choice: fixed` ⇒ the system text has no Delegations line (a
request's tool list is not observable through stubllm — the roster pin lives in item 13's unit test). Announced surface
(e2e_announced_test.go, per test-drivers.md:147): the `run_on` values the Delegations line emits are
taken FROM the rendered system text and fed to `sub_agent` verbatim; both must be accepted.
**Regression guard.** the two-entry e2e home with `description:` is hand-written after `parallelHome` (cmd/apogee/e2e_parallel_test.go:173), not through `appendHomeConfig`; decision (2026-09-01 re-check): case (3) asserts the note as the last line of the body ahead of the steered trailer; case (4) drops the tool-schema assertion (a request's tool list is not observable through stubllm) and asserts the absence of the Delegations line in the system text only — the roster pin lives in item 13's unit test; a further case (5) asserts the Delegations line is byte-identical across a target-down beat. Guard (folded): the beat cadence is a fixed 10 s (internal/heartbeat/heartbeat.go:29; internal/tui/heartbeat.go:216 re-arms with `tea.Tick(heartbeat.Interval)`, no key/env override) while `tuitest.DefaultTimeout` is 5 s (internal/tuitest/wait.go:19), so case (5) and case (2) after the first bind wait with `tuitest.Within(> heartbeat.Interval)` on the `sub-agents: <name> unavailable — delegations run on the session server` notice (cmd/apogee/delegation.go:451) before reading the next request's system text, and case (2)'s send is gated on `sub-agents: routing to <name> (<model>)` (:438).
**Files:** docs/manual/configuration.md, docs/manual/commands.md, cmd/apogee/e2e_seat_test.go, cmd/apogee/e2e_announced_test.go, cmd/apogee/testdata/stubllm/seat-session.yaml, cmd/apogee/testdata/stubllm/seat-target.yaml
**Tests:** as above; case (3) reads the note as the last line of the delegation result's body, ahead of the steered trailer; case (4) asserts only the absence of the Delegations line; case (2) sends after the `routing to` notice and case (5) waits `tuitest.Within(> heartbeat.Interval)` for the `unavailable` notice before asserting the Delegations line in the system text is byte-identical across the target-down beat.
**Acceptance:** `go build ./... && go test ./cmd/apogee/ -run 'E2ESeat|E2EAnnounced'`
**Regression guard.** Existing e2e suites (`E2ESubAgentView`, `E2EParallel`, `E2ESmoke`) pass unchanged; the announced test's existing cases keep their spellings.
**Commit:** `docs(manual),test(e2e): seat choice documented and proven end to end`

---

**Suggested version bump:** two micro bumps at closeout — one per shipped feature (auto-named
delegations; model-chosen seat) — owner's call.
