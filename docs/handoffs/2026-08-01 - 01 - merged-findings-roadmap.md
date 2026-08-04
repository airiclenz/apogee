# Roadmap — merged findings from the 2026-08-01 architecture review + code audit

**Date:** 2026-08-01
**Supersedes:** `2026-08-01 - 00 - architecture-review-findings-investigation.md` (its verification
step is done; archive it).
**Sources of record (this doc deliberately does not duplicate their evidence):**

- `docs/reviews/2026-08-01 - architecture-review.html` — 11 deepening candidates + 12 smaller
  findings. **All load-bearing claims independently re-verified against the code on 2026-08-01**;
  corrections applied in place (verification stamp in the report header).
- `docs/reviews/2026-08-01 - code-audit.md` — whole-repo audit: 1 Critical, ~14 High, 4 Medium,
  with probes already run for the key claims and a recommended action order.

This doc is the merge: what is confirmed, what still needs review/research, what depends on
what, and the implementation order. It feeds the implementation plans (§6).

---

## 1. Verification outcome (the review's findings are now trustworthy)

The review's explorer-produced claims were re-checked by five parallel verification passes.
Verdicts:

| Item | Verdict |
|---|---|
| **Flagged defect: hookrun acted-probe panic** | **CONFIRMED with live repro** (see §2) |
| C1 one Mechanism runner | CONFIRMED (5 runner pairs = 10 hand-written loops, 3 probe flavours, no `hookrun_test.go`) |
| C2 one declaration per tool | CONFIRMED (`payloadKeys` has zero cross-references; comments are the only sync) |
| C3 one workspace fence | CONFIRMED; counts corrected (9 string-family sites; `contextfiles.go:58` is worse — no validation at all; 11 Safe* sites + 2 raw `os.OpenRoot` walks) |
| C4 request projection | CONFIRMED; minor corrections (20 tests, 19 full round-trips) |
| C5 per-model bindings | CONFIRMED; **one false claim fixed** (a fake does implement `SetModel`; the narrower gap — no positive fake-observed Rebind→SetModel — stands) |
| C6 config key table | CONFIRMED exactly (8 code sites per key + a 9th doc site; silent-noop real: non-strict YAML, no exhaustiveness guard) |
| C7 TUI homeless modules | CONFIRMED exactly (441-line heartbeat block, 789-line View block, 960-line `heartbeat_test.go` with no production file) |
| C8 budget answers | CONFIRMED (five arithmetic sites, two packages) |
| C9 sub-agent privileges | CORRECTED (27 Config fields, not 24; the 4 "poked" fields are Agent fields) |
| C10 model identity | CONFIRMED (line drift only) |
| C11 overlay stack | CONFIRMED (ladders' member sets overlap but differ — strengthens the finding) |

The audit's findings were probe-backed at authoring time and were not re-verified here; one
(`subagent.go:141 child.liveMode = a.Mode`) was independently re-confirmed verbatim during the
C9 pass.

## 2. The confirmed crash — report to owner, hotfix in Wave 0

`internal/agent/hookrun.go:256/:260` (snapshot/compare, catalogued loop of
`runPostToolResultHooks`): `*result != before` struct-compares `domain.ToolResult`, whose
`Summary` interface holds `domain.OpenedFile` (`LocatedOn []int` → uncomparable) after any
successful `open_file`. Go short-circuits struct compares, so the panic fires **exactly when the
hook did not act** — and `error_enrichment` (the only catalogued post-tool-result hook, shipped
in the **gemma-4 Validated set**) never acts on a non-error result. Reproduced through the full
real path (Submit → Step → dispatchTools → runPostToolResultHooks):
`runtime error: comparing uncomparable type domain.OpenedFile`. No recover on the path; Bubble
Tea's default catchPanics turns it into `ErrProgramPanic` — **the whole TUI exits mid-Exchange**
(in-flight Exchange lost; per-Turn saves survive). No existing test drives a summary-carrying
result through that dispatch (the fakes all carry `Summary: nil`, which compares safely — why CI
never tripped).

**Decision taken (handoff Q3):** minimal hotfix now (Wave 0), C1 deletes the site later.

## 3. The merged picture — three tracks

**Track F — fix waves.** The audit's defects plus the confirmed crash: concrete, known fixes,
each with a regression test. No grilling needed. The tests are the durable artifact — later
refactors (Track A) must keep them green, so fixing point-first is not wasted churn.

**Track D — owner decisions (short discussions, not research).** Three audit findings are
design calls, not mechanical fixes; they block only their own items (§5).

**Track A — architecture candidates.** The review's 11 candidates each need the
`/improve-codebase-architecture` grilling loop with the owner before a plan exists. The fix
waves deliberately precede them; several candidates then make a whole defect class
unrepresentable (§4).

## 4. Dependencies (what depends on what)

**Fixes before refactors, refactors absorb fixes:**

- **Hookrun hotfix → C1.** The hotfix is deliberately minimal; C1's shared `Revision()` acted
  probe deletes the compare entirely. C1 is the review's top recommendation and unlocks C4/C5
  (same files, calmer ground).
- **Fence point-fixes → C3.** Wave 1 closes the exploitable holes now (`grep` walk,
  `contextfiles`, `view_diff`, doc server, + the sweep of `present_document`/`diagnostics`/
  `list_dir`/single-file `grep`); C3 (`security.Workspace`) then replaces the two entry families
  and five predicates so the class cannot rot again. The Wave-1 regression tests survive C3
  unchanged.
- **`liveMode`/fault fixes → C9.** The audit's depth-2 mode-composition fix and the
  faulted-delegation error marker land as point fixes; C9 (derive per-field, don't struct-copy)
  later makes ADR 0005's "≤ parent" bound structural. C9's corrected arithmetic: 27 Config
  fields, 22 inherited verbatim.
- **TUI tactical fixes → C7 → C11.** The heartbeat-chain retirement, the actuation/`SwitchUpstream`
  goroutine fix, the session-save serialization, and the single "rows this frame" derivation all
  live in `model.go` territory; C7 (extract the heartbeat + View banner modules) then gives them
  homes, and C11 (overlay stack) supersedes the tactical row derivation. Order: tactical fixes →
  C7 → C11 (the report names C11 as C7's natural follow-on).
- **Escape-strip seam (Wave 2) is independent of C7/C11** but touches the same files — land it
  before the C7 extraction so the extraction moves already-fixed code.
- **`MergeDangerousRules` fix ⟂ candidates, but ⟸ config surfacing.** It must land before the
  dangerous-rule config-key surfacing parked in `TODO.md` (L1's "re-verify when wired" promise
  would today pass against tests that encode the loophole).
- **C5 ties to the `TODO.md` `HostTools` trap** (composed in both `construct.go` and `wire.go`;
  a new field added in one place is silently dropped). The binding-set grill should settle that
  trap's fate too. C5 also shrinks `runRoot`, which C6 (config table) further shrinks — grill
  C5 and C6 together.
- **Plan-menu vs Resolution drift** (documented in the confinement contract §4 fn 2) is fixed by
  smaller-finding 3 (move `toolClass` into `domain`, key both menu and ladder on it) — a Wave-2
  item with a one-line decision: the menu stops offering what the ladder refuses.

**No other cross-dependencies:** everything in Waves 0–2 is independently landable, one commit
per item.

## 5. Owner decisions needed (Track D — each blocks only its own item)

1. **`autofix` external formatter** (audit High). Recommendation: route it through the same
   execution seam tools use (`Confine` + workspace box), refuse in Plan mode, set
   `cmd.WaitDelay`, make the timeout injectable — and **drop the two prettier rungs** (prettier's
   config-file `require()` walk is arbitrary code execution from a hostile repo) unless a
   workspace-basename substitute is wanted. Decide: drop vs. sanitize.
2. **Skills load order** (audit Medium). A repo's `.apogee/skills/` currently outranks and
   silently shadows the user's global library on ID collision — the audit says this inverts the
   repo-trust posture and warrants revisiting the documented load-order decision. Recommendation:
   global library wins cross-source collisions; every shadowed pair recorded as a `SkipError`.
   Wants a short grill (it reverses a recorded oracle-parity decision) + possibly an ADR note.
3. **Session-record serialization ownership** (audit Critical, structural half). The tactical fix
   (Wave 2: store-level mutex + title carried on `savePayload`/queued behind `saveBusy`) lands
   regardless; whether the *store* or the *TUI single-flight* owns write serialization long-term
   folds into the C7 grill (the save pipeline is one of `model.go`'s banner sections).

## 6. Implementation order

**Wave 0+1 — plan `2026-08-01 - 00 - security-fix-wave-plan.md`** (written; crash + exploitable
holes, each item small and independently committable):
hookrun hotfix; forced-gate approval-cache write; `git checkout --`; `library` temp+rename;
StripHarmony tail; `contextfiles` SafeOpen; `grep` walk fence + the four-tool sweep;
`view_diff` bounds; `--resume` id validation; doc-server per-request fence + timeouts;
landlock ABI mask + `REFER`; winlabel hard-link rung; `MergeDangerousRules` coexistence;
escape-strip at the transcript seam.

**Wave 2 — plan `2026-08-01 - 01 - engine-tui-correctness-wave-plan.md`** (written; correctness +
concurrency):
session-record write serialization (tactical, both layers); sub-agent effective-mode + ADR 0013
wording; faulted-delegation error marker; unknown-window summarizer bound; launcher/heartbeat
pair (move `SwitchUpstream` back to the Update goroutine, retire the forked beat chain);
interjection drain ctx check; single frame-row derivation (tactical); `toolClass` into domain
(Plan menu = ladder); syntaxcheck `//` language gate; readloop error marker on the committed
Message.

**Wave 3 — decision-gated fixes** (after §5 decisions; add to Wave-2 plan or a small follow-on):
`autofix` seam routing; skills load order.

**Wave 4+ — architecture (one grill session → one plan each; proposed order):**

| # | Grill | Why this order |
|---|---|---|
| G1 | **C1** one Mechanism runner + `Revision()` | Top recommendation; deletes the hotfixed compare; unlocks C4/C5 |
| G2 | **C3** `security.Workspace` | Biggest security payoff; Wave-1 tests already pin behavior |
| G3 | **C7 then C11** TUI homes, then overlay stack | Paired by the report; absorbs the Wave-2 TUI tactical fixes' territory |
| G4 | **C5 + C6** binding value + config table | Overlapping shrink of `runRoot`; settles the `HostTools` trap; C6 overlaps the `tui.Options` smaller finding |
| G5 | **C2** typed tool declarations | Feeds guard + TUI card + MCP registration |
| G6 | **C4** request projection | Report suggests after C1 |
| — | C8 / C9 / C10 + smaller findings | Worth-exploring tier; batch opportunistically or ride along with their paired grills (C9 after the Wave-2 sub-agent fixes) |

Smaller findings placement: #3 (tool classification) is a Wave-2 item; #9 (width-authority
leaves) is a trivial rename batch unblocked since the popup plan archived — ride it along with
any TUI wave; the rest wait for their paired grills (#1, #4, #10, #11 → C7/C11; #2, #7 →
composition-root work near C5/C6; #5, #6, #8, #12 → standalone smalls batch).

**Addendum 2026-08-04 — constraints from ADRs 0031/0033 (post-date the review, which was
written against ADRs 0001–0030).** Checked candidate-by-candidate: no collisions — every
candidate is in-process structure work, and the north star's invariants gate surfaces and
layering, which the review does not touch. Three grills gain a siting constraint:

- **G4 / C5:** the binding-set value must live engine-side, not in `cmd/apogee` — the
  scheduler plan's `internal/run` (`docs/plans/2026-08-03 - 08`) constructs a fresh Agent per
  Firing, a third construction site the review did not know about, and it must apply the same
  validated path. Sequencing pressure both ways: whichever of C5 / the scheduler plan lands
  second absorbs the other's construction site.
- **G4 / C6:** the key table lives in `cmd/apogee` (config layout is a Driver concern per
  ADR 0031), but its `apply(*options)` step must not weld engine-bound keys to the TUI's
  options struct — `apogee headless` in the same binary would inherit a TUI-shaped config path.
- **G3 / C7:** the extracted upstream-binding module stays Driver orchestration *over* C5's
  engine value (the split the report already draws, now with an ADR reason): beat fold,
  rebind timing, and server-switch policy are the interactive Driver's composition; what swaps
  together is the engine's.

## 7. Additional review / research still needed

- **Grills G1–G6** (§6) — the only "additional review" the candidates need; the report carries
  the evidence, now verified.
- **Landlock ABI live proof** — the Wave-1 fix is table-testable (`accessMaskForABI`), but the
  end-to-end proof needs a real ABI-1/2 kernel (Ubuntu 22.04 / Debian 12 box). Owner-run; joins
  the CHANGELOG's pending owner-run proofs.
- **Windows hard-link labeling** — policy half table-testable off-OS; the on-host confirmation
  joins the owner-run Windows pass.
- **Track D decisions** (§5).
- Nothing else: every other finding is verified and has a plan item.

## 8. Conventions for the implementing sessions

- Plans execute via `/implement-plan`, one commit per item, `make check` before each commit;
  commit/push only when the owner asks. No AI attribution trailers.
- **No version bumps** — when a wave completes, *suggest* a bump (VERSION-SUGGESTION line per
  the skill).
- Live-LLM endpoint not needed for any Wave 0–2 item.
- Settled, do not re-open: `~/.apogee` home; ADR 0010 layout; ADR 0011 threading (no
  `strings.Builder` by value); the ADRs listed per candidate in the report.
- Open owner question carried forward (handoff Q2): commit the review report, audit, this
  roadmap, and the two plans to `main`?
