# Handoff — investigate the 2026-08-01 architecture-review findings

**Date:** 2026-08-01
**Prior session:** ran `/improve-codebase-architecture` over the whole repo (all 20 `internal/` packages + `cmd/apogee` + root facade). The deliverable — an HTML report with 11 deepening candidates, a smaller-findings list, and one flagged defect — is written and saved. The owner has **not yet picked a candidate**; this handoff sets up a session to *verify and deepen the findings*, then run the skill's grilling loop on whatever the owner picks.

## Primary artifact

- **`docs/reviews/2026-08-01 - architecture-review.html`** — the full report: per-candidate files, evidence, before/after diagrams, recommendation strengths, smaller findings, top recommendation. Readable as HTML source; the diagrams need internet (Tailwind/Mermaid CDNs) but the text does not. **Read this first — this handoff deliberately does not duplicate its evidence.** Uncommitted; committing is the owner's call.

## How the findings were produced (and why they need verification)

Five parallel `Explore` subagents swept: (1) `internal/tui`, (2) `internal/agent`, (3) `internal/tools`+`security`+`platform`+`mcp`, (4) `internal/mechanisms`+`domain`+`context`+`processing`+`provider`+`prompt`, (5) supporting packages + `cmd/apogee` + root facade. Their line references, counts, and claims were compiled into the report **without independent re-verification by the main agent**. The evidence is specific (file:line throughout) and internally consistent, but every load-bearing claim should be spot-checked against the code before design or implementation work builds on it.

## Candidate inventory (details + evidence in the report)

| # | Candidate | Strength | Anchor files |
|---|---|---|---|
| 1 | One Mechanism runner, one acted probe (shared `Revision()` on the five working values) | Strong | `internal/agent/hookrun.go`, `internal/domain/hooks.go`, `internal/domain/tools.go` |
| 2 | One declaration per tool (typed arg descriptors feed schema, guard payload-classification, TUI card) | Strong | `internal/tools/*.go` (`toolSpec`), `internal/security/dangerous.go` (`payloadKeys`), `internal/tui/toolpresent.go` |
| 3 | One workspace fence (`security.Workspace` replacing two entry families + five containment predicates) | Strong | `internal/security/{safeio,pathsafety}.go`, `internal/tools/path_safety.go`, `internal/agent/dispatch.go:402`, `internal/platform/host.go:155` |
| 4 | A request-projection module (prompt → context files → directives → tool menu, assembled once) | Strong | `internal/agent/{loop,wire,contextfiles,hookrun,state}.go` |
| 5 | Per-model bindings as one value (construct/Rebind/SwitchUpstream + `rebindSpecFor` mirror) | Strong | `internal/agent/{construct,rebind}.go`, `cmd/apogee/wire.go:538` |
| 6 | The config key table (four ~25-field isomorphic structs, 8 edit sites per key) | Strong | `cmd/apogee/config.go`, `cmd/apogee/root.go` |
| 7 | Home the TUI's homeless modules (441-line heartbeat block + 789-line View block out of `model.go`) | Strong | `internal/tui/model.go`, `heartbeat_test.go` (960 ln, no prod file) |
| 8 | Budget answers, not fields (+ `CapToolResult` decides-and-reports) | Worth exploring | `internal/domain/budget.go`, `internal/context/{budget,toolresult}.go` |
| 9 | Sub-agent privileges: derive per-field, don't struct-copy (makes ADR 0005 structure) | Worth exploring | `internal/agent/subagent.go:111-143`, `internal/domain/config.go` |
| 10 | Model identity: one module (probe owns record+version; library resolves via reader seam) | Worth exploring | `internal/{library,probe}/…`, `cmd/apogee/probemodel.go` |
| 11 | An overlay stack (three parallel overlay ladders in `model.go` → declarations) | Worth exploring | `internal/tui/model.go:689-782,2179-2234`, `popup.go` |

Smaller findings (12 one-liners, in the report): enter-running ritual, `validated.Decide`, tool classification into `domain`, slash-command policy table, `registry.Seal()`, one guarded HTTP client, `platform.Backend` value, `processing` over-wide exports, width-authority holes (`popup.go`/`interject.go`), `tui.Options` bag, `heartbeat.ModelSummary` alias, renderable-extension subset pin.

**Positive controls the explorers named (deep modules to preserve, useful as calibration in grilling):** `resolve()`/`resolution.go`, the Mechanism catalogue triangle (one file per Mechanism), `session.Record`, the transcript codec, `popup.go`'s painting half, `internal/title`, the root facade (ADR 0010 holds exactly), `fold.go`, `width.go`, `runSubprocess`.

## The flagged defect — verify first (highest-value single task)

Claim: the post-tool-result acted probe `*result != before` (`internal/agent/hookrun.go:256`, and its experimental twin ~`:260`) is a **struct compare of `domain.ToolResult`, whose `Summary` field is an interface**; `domain.OpenedFile` carries `LocatedOn []int` (uncomparable), so the compare **panics** whenever both sides hold an `OpenedFile` — i.e. plausibly whenever `open_file` succeeds *and any* post-tool-result Mechanism is enabled (the runner compares regardless of whether the Mechanism acted). `error_enrichment` is the only catalogued post-tool-result hook and reportedly ships in the gemma-4 Validated set (`internal/validated/shipped.json`); the compare allegedly sits *outside* the `recoverHook` boundary, so it would unwind the worker rather than degrade.

Verify each link: (a) `ToolResult` field set and `OpenedFile` comparability, (b) whether the compare precedes/escapes the recover, (c) an actual trigger path (does `open_file` attach the summary before post-tool-result hooks run?), (d) whether any existing test covers a summary-carrying result through that dispatch (explorer says none). If confirmed: it's a real crash in a shipped Validated set — report to the owner; the structural fix is candidate 1, but a one-line hotfix may be wanted first. `/code-audit` scoped to `internal/agent/hookrun.go` + `internal/domain/toolsummary.go` is the right instrument.

Two adjacent known/knowable items, for completeness: the Plan-menu vs Resolution disagreement (`loop.go:815` offers `git_diff_range`/`diagnostics` in Plan; the ladder refuses them) is **documented** in `docs/design/confinement-execution-contract.md` §4 fn 2 — drift to discuss, not a discovery; the `/skill` "— idle only" tag bug is already in `ISSUES.md`.

## Suggested verification passes (beyond the defect)

Spot-check the one load-bearing fact per Strong candidate before grilling on it:

1. **C1:** count the runner pairs in `hookrun.go`; confirm three probe flavours and the missing `hookrun_test.go`.
2. **C2:** confirm `payloadKeys` (`security/dangerous.go:154-166`) has zero cross-references, and that only the `"path"` key is pinned by test (`tools/workspace_scoped_test.go:122,175`).
3. **C3:** confirm the 10 string-family call sites do unfenced I/O after `resolveInRoot`, and the five predicates' differing symlink semantics.
4. **C4:** confirm `promptseam_test.go` is ~870 lines of full-Agent round-trips and that the three appender idioms are as described.
5. **C5:** confirm the anonymous `interface{ SetModel(string) }` assertion (`rebind.go:132`) and that no test fake implements it; confirm the duplicated reset pair (`rebind.go:135-136` vs `:193-194`).
6. **C6:** trace one key (`auto-compact`) through the eight sites listed in the report and confirm the silent-noop failure mode.
7. **C7:** confirm `model.go` banner boundaries/line counts and that `heartbeat_test.go`/`paint_test.go` have no production counterparts.

Also worth checking (the explorers' one visible misalignment): C9's field arithmetic — "24 Config fields / 22 inherited verbatim / 5 overridden / 4 poked" doesn't sum cleanly; recount `domain.Config` and `subagent.go:111-143` before using the numbers in a grilling session.

## What the next session actually does (per the skill)

1. Verify (above), correcting the report in place if anything is wrong (`docs/reviews/2026-08-01 - architecture-review.html` — edit the HTML, it's the artifact of record).
2. Ask the owner which candidate to explore (top recommendation was **C1**; sequencing note: C1 absorbs the `domain/hooks.go` working-value-pattern finding; C7 and C11 pair; C5 shrinks `runRoot`; C6 and the `tui.Options` finding overlap).
3. Run the **grilling loop** from `/improve-codebase-architecture` on the pick: constraints, the deepened module's shape, what sits behind the seam, which tests survive. Side effects inline: new/sharpened terms → `CONTEXT.md`; a rejected candidate with a load-bearing reason → offer an ADR; interface alternatives → the skill's `INTERFACE-DESIGN.md`.
4. Decisions land as a **saved plan doc** in `docs/plans/` in the house format (`## N.` items with What/Tests/Acceptance/commit) — plans of any size are saved, never implemented in-session, and `ExitPlanMode` is never called. Implementation is a later `/implement-plan` session.

## Conventions the next session must honour

- Pre-production: commit directly to `main`, but **only when the owner asks**; run `make check` first; **no AI attribution trailers**.
- **Never** bump `VERSION`/`CHANGELOG` headings unasked — suggest instead.
- Settled, do not re-open: `~/.apogee` config home (no XDG); ADR 0010 layout invariant (`internal/*` never imports root); ADR 0011 threading model (incl. the no-`strings.Builder`-by-value rule, `internal/tui/doc.go`); the ADRs listed per candidate in the report.
- The owner prefers the best long-term architecture over lowest churn — lead with the best-for-the-future shape in grilling.
- Live-LLM tests gate on `APOGEE_LIVE_ENDPOINT`; not needed for any of this.

## Suggested skills

- `/code-audit` — scoped run to confirm the hookrun panic (and only that; the review already covered structure).
- `/improve-codebase-architecture` — re-read its `LANGUAGE.md` (module/interface/depth/seam/leverage/locality vocabulary) before grilling; its `INTERFACE-DESIGN.md` when exploring interface alternatives; `CONTEXT-FORMAT.md`/`ADR-FORMAT.md` (under the sibling `grill-with-docs` skill dir) for the inline doc updates.
- `/grill-with-docs` — if the owner wants the candidate stress-tested against CONTEXT.md/ADRs rather than the lighter grilling loop.
- `/implement-plan` — **write mode only** in that session: save the agreed refactor as `docs/plans/2026-08-01 - NN - <slug>-plan.md` and stop.

## Open questions for the owner

1. Which candidate(s) to grill first (session recommendation: C1).
2. Commit the review report (and this handoff) to `main`?
3. If the hookrun panic confirms: hotfix now or fold into C1's refactor?
