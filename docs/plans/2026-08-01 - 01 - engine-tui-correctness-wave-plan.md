# Plan — engine & TUI correctness wave (audit 2026-08-01, Wave 2)

**Date:** 2026-08-01
**Status:** not started
**Goal:** land the audit's correctness and concurrency fixes — the Critical session-record
lost-update race, the composing-state holes in the engine, the TUI/launcher concurrency pair,
and the two mechanism-heuristic defects — each with a regression test.

**Authoritative sources (precedence in this order):**

1. `docs/reviews/2026-08-01 - code-audit.md` — finding evidence and prescribed fixes (section
   named per item).
2. `docs/handoffs/2026-08-01 - 01 - merged-findings-roadmap.md` — sequencing and the owner
   decisions this plan must NOT pre-empt (§5).
3. Repo conventions: `AGENTS.md`, `internal/tui/doc.go` (ADR 0011 — the Bubble Tea `Model` is
   value-copied; never a `strings.Builder` by value), and the ADRs named per item. Where the
   generic coding-standards skill conflicts with a repo convention, the repo convention wins.

**Standing requirements:**

- Invoke with forwarded skills: `coding-standards` (Go).
- `make check` green before every commit (it runs `-race`; several items here are race fixes —
  add the deterministic regression tests, don't rely on `-race` alone to see file-level lost
  updates). One commit per item. Never bump VERSION/CHANGELOG headings (closing note).
- No live LLM endpoint needed.

**Out of scope (deliberate):**

- The architecture candidates (C1–C11): item fixes here are tactical; C7/C11 later rehome the
  code they touch (roadmap §4). Do not extract modules from `model.go` in this plan.
- The decision-gated items (`autofix` formatter seam, skills load order — roadmap §5).
- Everything in plan `2026-08-01 - 00 - security-fix-wave-plan.md`.

---

## 1. Serialize session-record writes — ✅ DONE (2026-08-02)

**NOTES (2026-08-02):** took the item's second (b) option — queue `Rename`/`Delete` behind the save
latch — rather than carrying the title on `savePayload`, so the `SessionHost` seam's "Save fixes the
title at create, Rename is the only writer" contract is untouched. Two deviations from that option's
literal wording: `saveBusy` is renamed `writeBusy` (it now latches all three writes, so the old name
would lie), and `pendingSave *savePayload` becomes a `pendingWrites []recordWrite` FIFO — saves still
coalesce latest-wins exactly as `pendingSave` did, but renames and deletes need to keep their order
rather than replace one another. (c) is implemented as retry-on-failure: a quiet title write that the
store refused goes back on the pending-title stash (`restashTitle`) and is applied at the next
successful save, which also covers the "saves have been failing" case, not just the first-Save window.
Also added a dated addendum to ADR 0022 (its Decision 1 claimed a crash loses at most one Turn, which
this defect made false) and a CHANGELOG `Fixed` entry; no version identifier touched.

**What:** Audit "Critical — Session-record writes are unserialized, so a title write can roll
a whole Turn off disk" (probe: 50/200 lost updates). Two layers, one item:
(a) `internal/session/store.go` — guard `Save`/`Rename`/`Delete` with a store mutex so the
Rename's Load→mutate→Save read-modify-write is atomic against a concurrent `Save`;
(b) `internal/tui` — bring the rename/delete Cmds into the existing save single-flight: carry
the pending title on `savePayload` and let `SessionHost.Save` apply it, or queue
`Rename`/`Delete` behind `saveBusy` exactly as `pendingSave` is queued
(`autotitle.go:176/:203/:244`, `sessions.go:281/:292/:310`, `model.go:1514/:1541-1549`) — in
particular `saveComplete` must stop batching the title flush beside the coalesced save
(`tea.Batch` members run on separate goroutines).
(c) Fix the adjacent silent drop: `applyTitle` branches on `ActiveID()`, which is non-empty
before the first atomic write lands, so a title answering in that window hits ENOENT and is
discarded — make that window either apply-on-next-save or retry, never a silent drop.
The structural question — which layer owns serialization long-term — is deliberately NOT
settled here; it feeds the C7 grill (roadmap §5.3).

**Tests:** (a) store-level: concurrent `Save(turn2)` + `Rename` under `-race`, N iterations,
zero lost payloads (the audit's probe as a regression test); same for `Delete` vs an
in-flight save; (b) TUI-level: a fold test that a title flush arriving while `saveBusy` queues
rather than dispatching a parallel write — note the existing test fake serialises host calls
under one mutex, which is exactly why the race was invisible; the new test must assert
ordering/queueing at the fold layer, not rely on the fake's accidental safety; (c) the
`applyTitle` early-window case: title answered before the first save lands is not lost.

**Acceptance:** `go test ./internal/session/ ./internal/tui/ -run 'Rename|Save|Title' -count=1 -race`
passes; `make check` green. ADR 0022's "a crash loses at most one Turn" restored under
default config (auto-title on).

**Commit:** `fix(session,tui): serialize record writes so a rename can never roll back a turn`

## 2. Grandchildren compose the parent's effective mode — ✅ DONE (2026-08-02)

**What:** Audit "High — Sub-agent mode tightening stops composing at depth 1".
`internal/agent/subagent.go:141`: `child.liveMode = a.Mode` captures the direct parent's own
frozen mode, so a depth-2 grandchild never sees the top-level user's mid-run tighten
(Shift+Tab to Plan) — its effective privileges exceed its parent's, the exact
tighten-direction failure ADR 0005/0013 forbid. Fix: `child.liveMode = a.effectiveMode`,
which composes transitively through the same accessors and stays race-free. Same item owns
the doc correction: ADR 0013's "Post-v1 realisation" section says "captures the parent's
`modeMu`-guarded `Mode` accessor" — the ADR text encodes the hole; amend it to *effective*
mode (dated amendment note, per ADR house style).

**Tests:** Depth-2 chain test beside `setmode_test.go:65` (which covers depth 1 only): spawn
parent→child→grandchild, tighten the top-level mode mid-run, assert the grandchild's next
`effectiveMode()` reflects it.

**Acceptance:** `go test ./internal/agent/ -run 'Mode' -count=1 -race` passes; `make check`
green; ADR 0013 amendment present.

**Commit:** `fix(agent): grandchildren compose the parent's effective mode`

## 3. A faulted delegation reports as an error — ✅ DONE (2026-08-02)

**NOTES (2026-08-02):** took the item's first option — a fault marker (`StepResult.Faulted`, set by
`end()`'s `endAbandoned` row) — rather than an `endAbandoned`-specific status, so every existing
`Status` reader (Run's loop, the TUI worker's switch) keeps behaving identically and only a reader
that reports the Exchange's outcome onward consults the new field. The item's third test case ("a
genuinely completed child still returns its final message unchanged") is covered by the pre-existing
`TestSubAgent_DelegatesAndReportsBack` plus new `Faulted`-is-false rows added to the
`TestTurnEnd_Table` exit table, so no duplicate test was written.

**What:** Audit "High — A faulted sub-agent delegation is reported to the parent as
success". `internal/agent/subagent.go:94` + `internal/agent/dispatch.go:60`: an abandoned
child Exchange (`end(t, endAbandoned)`) returns `StatusExchangeComplete`, so `runSubAgent`
returns `IsError: false` with a placeholder or stale mid-task text, and
`noteToolProductivity` books the failure as a productive write (resetting self-regulation
strikes and the Turn Budget). Make the abandoned exit distinguishable at the seam — a fault
marker on `StepResult` or an `endAbandoned`-specific status — and have `runSubAgent` return
`IsError: true` with a message naming the child fault, so the parent model and
self-regulation both see the truth.

**Tests:** Child upstream fault mid-delegation → the parent's tool result has
`IsError: true`; `noteToolProductivity` books no productive write (strikes/budget
unaffected); a genuinely completed child still returns its final message unchanged; a
cancelled child keeps its existing `StatusCancelled` handling.

**Acceptance:** `go test ./internal/agent/ -run 'SubAgent|Delegat' -count=1` passes;
`make check` green.

**Commit:** `fix(agent): a faulted sub-agent delegation reports as an error, not success`

## 4. Emergency compaction works when the context window is unknown — ✅ DONE (2026-08-02)

**NOTES (2026-08-02):** the named constant is a TRANSCRIPT-token bound (`compactUnknownWindowTranscriptTokens`
= 3072, sized to fit llama.cpp's 4096-token default `n_ctx`) rather than an assumed window fed through the
`window - compactMaxTokens - compactPromptOverheadTokens` arithmetic — an assumed window would silently
collapse to the 256-token floor if either reserve were ever raised, whereas the direct bound is independent
of both. Two things the item's text does not spell out: (a) the give-up remedy is appended ONLY when the
window is unknown, which is what the item's own "a known-window session's behavior is unchanged" test
requires, and (b) `TestStepOverflowStillAbandonsTheTurnUnchanged` — the existing pin on the give-up being
byte-identical — runs on a config with NO window, so its assertion is narrowed to "leads with the provider's
message" and the amendment is recorded in its doc comment; byte-identity is still pinned for a known window
by `TestOverflowRecoveryGivesUpAfterASecondOverflow`. ADR 0018 gets a dated amendment because its decision 2
("giving up is byte-identical") and §8 (the `window - 4608` transcript arithmetic) both encoded the old
contract, plus a CHANGELOG `Fixed` entry; no version identifier touched.

**What:** Audit "Medium — With an unknown context window every growth bound is inert and the
session wedges". `internal/agent/compact.go:234-238/:150`: when neither discovery nor
`context-window:` reports a window, `compactTranscriptChars` returns 0 and the emergency
fold's summary call renders the WHOLE conversation — overflowing exactly like the request it
rescues, permanently wedging the session until `/clear`. Fix: with an unknown window, bound
the summarizer transcript by a conservative default (a few thousand tokens through the
calibrated ratio — named constant with a comment stating why the value), so the fold can
actually shed history; and make the give-up ErrorEvent name the remedy ("set
`context-window:` in config") instead of leaving a silently wedged session.

**Tests:** With `ContextLimit == 0` and a long history: the summarizer request is bounded by
the default (assert on the captured request size), the fold completes, and the give-up path's
ErrorEvent text names `context-window:`. A known-window session's behavior is unchanged.

**Acceptance:** `go test ./internal/agent/ -run 'Compact|Fold|Window' -count=1` passes;
`make check` green.

**Commit:** `fix(agent): emergency compaction stays functional when the context window is unknown`

## 5. Profile-load moves commit on the Update goroutine; one beat chain — ✅ DONE (2026-08-02)

**NOTES (2026-08-02):** three deviations from the item's literal text. (a) The resolved move crosses the
seam as ONE call — `ProfileLoadResult.Move func() (ServerSwitchResult, error)`, replacing `Moved`/`Switch`
— rather than as endpoint/alias/key fields: the binary closes over what it resolved and the fold commits
it on the Update goroutine, which keeps the per-server api key out of the renderer (the posture
`ServerChoice`'s doc states) and needs no fifth `Options` seam. (b) Gating `observeBinding` on
`actuation.inFlight` needs a release point or the stash strands until the next Exchange ends (the
immediate post-load beat cannot recover it — `observedModel` was already advanced at capture), so
`foldActuationDone` applies the pending rebind on every path that leaves the session where it was; a load
that MOVES discards it with the rest of the heartbeat state, as `foldServerSwitch` always has. This
inverts `TestActuationDoesNotShadowLandedBeats`, which pinned the old immediate bind — rewritten as
`TestActuationDefersABindingObservedUnderTheLatch` (the beat is still folded; only the binding defers).
(c) The generation bump is a new pointer-receiver `Model.armBeat` (the `spinnerAnim.arm` convention) used
by both immediate-beat sites, rather than folded into `beatCmd` — `Init` calls `beatCmd` on a value copy
it cannot return, so a bumping `beatCmd` there would arm a chain and then fire the beat on a generation
the Model never adopted. Test (a) "no engine call off the Update goroutine" is asserted on the
CROSS-server load (the only branch that ever called the engine) at both layers. ADR 0029 D2 gets a dated
amendment (it read "the composition root performs the … fold", which is where the defect lived) plus a
CHANGELOG `Fixed` entry; no version identifier touched.

**What:** Audit "High — The profile-load actuation path drives the engine off the Update
goroutine and forks a second heartbeat chain". Two defects, one path:
(a) `cmd/apogee/launcher.go:550` (`launcherWiring.load`) ends, on the cross-server branch, in
`sessionMover.move` → `agent.SwitchUpstream` on the actuation Cmd goroutine, racing
heartbeat-driven `Rebind` on the Update goroutine (the actuation latch gates commands and
sends but not observed-binding rebinds). Return the resolved move (endpoint/alias/key) in
`tui.ProfileLoadResult` and let the actuation completion fold (`internal/tui/actuation.go:366`)
drive `sessionMover.move` on the Update goroutine; additionally gate `observeBinding` on
`m.actuation.inFlight` exactly as it gates on `m.busy()`.
(b) `foldActuationDone`'s `verbLoad && !Moved` branch returns `m.beatCmd()` on the current
generation without retiring the running chain (unlike `foldServerSwitch`, which bumps
`hb.gen` first) — verified: two live chains per load, doubling `/v1/models` polling and
halving the offline debounce (`doc.go:246-248` states the opposite invariant). Bump
`m.hb.gen` before the immediate beat — or fold the bump into `beatCmd` as a pointer receiver
so callers must arm in a statement of their own, per the `spinnerAnim.arm` convention.

**Tests:** (a) a same-server load completion drives no engine call off the Update goroutine
(assert via the fold path — the move lands in the fold, not the Cmd); an observed-binding
change during `actuation.inFlight` is stashed, not applied; (b) chain-uniqueness: after
driving a load completion, injecting the pre-existing `heartbeatTickMsg` generation is
rejected and exactly one chain remains (the audit's probe accepted both).

**Acceptance:** `go test ./internal/tui/ ./cmd/apogee/ -run 'Actuation|Heartbeat|Beat|Load' -count=1 -race`
passes; `make check` green; `internal/tui/doc.go:246-248`'s invariant is true again.

**Commit:** `fix(tui): profile-load moves commit on the Update goroutine and never fork the beat chain`

## 6. Esc never discards a staged interjection — ✅ DONE (2026-08-02)

**NOTES (2026-08-02):** "the rows this Exchange delivered" needs a place to live, so the Model gains a
third, per-Exchange copy of the queue (`deliveredInterjections`, appended by `foldInterjected`, drained
by the new `Model.restageDelivered`) — a plain slice, ADR-0011-safe. It is cleared in `finishWorker`
beside `m.box`, which is the one boundary past which a delivered row is committed history: that is what
stops a LATER Exchange's stop from resurrecting an earlier one's delivery (pinned by
`TestNaturalCompletionKeepsDeliveredRowsDelivered`). The `errMsg` fold, which also calls
`AbortExchange`, deliberately does NOT re-stage: it is documented in `model.go` as a no-op guard rather
than a live path (a fault surfaces as an ErrorEvent at a boundary today, and the only reachable
`errMsg` — a failed `Submit` — precedes every drain), and the item is scoped to Esc; if `Step` ever
faults mid-Exchange that branch needs the same one-line call. ADR 0025 gets a dated amendment (its
"Delivery has fates" consequence recorded only the engine's half of the fate, which is where the hole
was) plus a `doc.go` narration fix and a CHANGELOG `Fixed` entry; no version identifier touched.

**What:** Audit "High — Staged interjections are drained into an Exchange that is being
cancelled". `internal/tui/worker.go:136` (`stepToBoundary`) drains and commits the
interjection mailbox without consulting `ctx`; a cancel then `AbortExchange()`-drops the
interjected message while the transcript already shows it delivered and the queue no longer
holds it — ADR 0025's "Esc stops everything, including what was waiting to go out" is only
sometimes true. Fix both windows: pass `ctx` into `deliverInterjections` and skip the drain
when `ctx.Err() != nil` (rows stay in the mailbox, the queue of record); and for a cancel
landing after a successful drain, have the `cancelledMsg` fold (`model.go:490`) re-stage the
rows this Exchange delivered rather than leaving them only as transcript entries the
conversation no longer contains.

**Tests:** (a) cancel before the drain: the staged remark is still in the queue and resends
on ⏎; (b) cancel after the drain, same Exchange: the remark is re-staged, not lost; (c) an
uncancelled Exchange delivers exactly as today. Drive through the worker/fold seams (the
rendezvous fakes already exist in the tui tests).

**Acceptance:** `go test ./internal/tui/ -run 'Interject|Cancel' -count=1 -race` passes;
`make check` green.

**Commit:** `fix(tui): Esc never discards a staged interjection`

## 7. One frame-row derivation for View, mouse and overlays

**What:** Audit "High — Mouse mapping and overlay rendering derive the frame's rows
independently" (verified: overlay clicks arm hidden-text selections copied via OSC 52; a
21-row frame composed on a 20-row terminal). Tactical fix (C11 later supersedes it
structurally — keep this minimal): give the Model a single "rows the transcript actually
occupies this frame" derivation — the shrink `View` computes at `model.go:2195-2201` — and
have `View`, `pointTranscriptRow` (`mouse.go:167-168`) and `contentLineAt` (`model.go:2306`)
all read it. Route `renderSessionBrowser` (`sessions.go:415`) and `renderPicker`
(`picker.go:606`) through `popupBudget` (`model.go:2856-2869`) like the prompts already are,
and change its floor from `max(6, …)` to `max(0, …)` so short windows shrink instead of
over-promising past the terminal height (D2).

**Tests:** (a) with an overlay open, a click on overlay rows maps to no transcript position
(no selection armed — the audit's 80×24 repro as a regression); (b) frame-height property:
on 12/16/20-row terminals with the session browser open and many sessions, the composed
frame never exceeds the terminal height (the audit measured +5/+9/+1 rows); (c) mouse
mapping and View agree on the boundary row (table over overlay heights).

**Acceptance:** `go test ./internal/tui/ -run 'Mouse|Frame|Popup|Budget' -count=1` passes;
`make check` green; `mouse.go`'s header claims become true again.

**Commit:** `fix(tui): mouse mapping and overlay rendering share one frame-row derivation`

## 8. One tool classification: the Plan menu keys on the ladder's fact

**What:** Review smaller-finding 3 + audit action-order item 7 (second half): the
Resolution's `classifyTool` consults 5 markers across 3 packages, while the Plan tool-menu
filter (`internal/agent/loop.go:815`) re-derives with `IsReadOnly` alone — so Plan offers
`git_diff_range`/`diagnostics` and the ladder then refuses them. Move `toolClass` into
`internal/domain` beside the markers it consults, and key BOTH the menu filter and the
resolution ladder on it, so the menu can never offer what the ladder refuses. Update the
documented drift note at `docs/design/confinement-execution-contract.md` §4 footnote 2 to
record the resolution (same item owns the doc edit).

**Design call (owner, at execution):** confirm the direction — the menu follows the ladder
(recommended; Plan stops offering the two tools) rather than the ladder loosening to match
the menu. Q: "Plan menu follows the ladder (drops git_diff_range/diagnostics in Plan) — or
keep the documented drift and only relocate the classification?"

**Tests:** The Plan-mode tool menu and the ladder agree for every registered tool (table
over the registry — the drifted pair included); classification behavior for every other
mode×tool cell unchanged (pin with a table against the current `resolve()` outcomes).

**Acceptance:** `go test ./internal/agent/ ./internal/domain/ -run 'ToolClass|Resolve|Menu' -count=1`
passes; `make check` green; contract §4 fn 2 updated.

**Commit:** `fix(agent,domain): the Plan tool menu and the resolution ladder key on one tool classification`

## 9. The syntax checker treats `//` as a comment only where it is one

**What:** Audit "High — The syntax checker treats `//` as a comment in every language".
`internal/mechanisms/syntaxcheck.go:139`: gate the `//` line-break-out to languages where
`//` opens a comment (js/ts/go/rust/java/c/cpp/csharp/swift/kotlin/php — NOT python/ruby,
where it is floor division / a regex literal and currently yields false "unclosed bracket"
reports that fire `ActionRetry` against correct code, violating the Bypass floor); and
either handle `/* */` blocks or skip bracket accounting inside them (a commented-out bracket
currently corrupts the stack). Mechanism is default-off — correctness fix, not urgent-path.

**Tests:** Give the checker the negative table it lacks: valid snippets per language
asserting `checkSyntax(...).valid` — Python floor division in call/index/range positions,
Python docstrings, a JS string containing brackets and apostrophes, a Rust lifetime `'a`, a
C-family `/* { */` comment — plus one broken snippet per error branch (today's coverage is
three files total).

**Acceptance:** `go test ./internal/mechanisms/ -run 'Syntax' -count=1` passes; `make check`
green.

**Commit:** `fix(mechanisms): the syntax checker treats // as a comment only where it is one`

## 10. Read-error detection reads a persisted marker, not the file body

**What:** Audit "High — Read-error sniffing over whole file bodies makes `read_loop` tell
the model to overwrite existing files". `internal/mechanisms/historyhints.go:51/:72` +
`readloop.go:109/:150`: the signals ("not found", "error:", …) are substring-matched against
a committed read result's entire content — which for a successful `read_file` is the file
body, so reading any source file containing error-handling strings classifies as a FAILED
read and steers the model to overwrite existing files with hallucinated reconstructions.
Root cause is acknowledged in-code (`:48-50`): the committed tool-result Message drops
`ToolResult.IsError`. Fix at the root: persist an error marker on the committed tool-result
Message (a `Message.Extra` flag or equivalent — smallest seam that survives
encode/decode/compaction) and have `resultIsReadError` read the marker; keep a first-line
anchored sniff only as the fallback for legacy records without the marker.

**Tests:** A successful read of a file whose body contains `error:` / "does not exist" is
classified successful (greenfield stays false; no STOP injection); a genuinely failed read
still classifies failed via the marker; a legacy record without markers falls back to the
anchored sniff; marker survives session encode→decode round-trip.

**Acceptance:** `go test ./internal/mechanisms/ ./internal/session/ -run 'ReadLoop|ReadError|Marker' -count=1`
passes; `make check` green.

**Commit:** `fix(mechanisms): read-error detection reads a persisted error marker, not the file body`

---

**Suggested version bump (not performed):** after this plan and the security wave both
complete, suggest `v0.11.0` (a sizeable all-fixes release) or two patch bumps if landed
separately — the owner decides.
