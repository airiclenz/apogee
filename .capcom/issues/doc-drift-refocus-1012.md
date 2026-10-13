---
title: Doc drift from the 2026-10-12 refocus (tier 6), unverified against code
status: done
type: task
priority: 4
created: 2026-10-13T18:25:09Z
closed: 2026-10-13T19:39:34Z
reason: "Every item verified against code: all were doc defects except the Floor-guard count (correct, now test-pinned in the manual pages by TestManualStatesTheFloorGuardCount); no code defects. IDEAS.md is gitignored, fixed locally."
---

Doc-to-doc drift found by the 2026-10-12 `/refocus` run (tier 6 of the local handoff `2026-10-12 - 01 - refocus-findings-and-capcom-knowledge-migration`), outside the files the capcom Knowledge migration cut into Notes. **None of it was checked against the code**: verify each item first — a mismatch can be a code bug, not a doc bug. Line numbers are as of 2026-10-12 and may have moved.

- **Design docs**
  - `docs/design/reaction-core-greenfield.md`: the body contradicts its header and the glossary — §3 :178 Bypass "builtins only"; :162 user shape(view) ✓; :72/:316 `mcp:` handler; :86-139 repo-proposed reactions plus `apogee reactions adopt`.
  - Retired "Mechanism" machinery still named as live in `docs/design/confinement-execution-contract.md` (:564, :581, :604, :1227, :1397, :1522), `docs/design/test-drivers.md:990` and `docs/design/tool-surface-findings.md:78,113,140`.
  - `docs/layout/tool-layout.md:48-50`: "expanding a member opens its span" vs a run view (ADR 0063).
- **Manual**
  - `docs/manual/configuration.md:65-71` says sixteen restart-only keys (confirmed against code), but dangerous-rules, update-check, env-scrub, webhook url-safety and context-files are also described as start-time, and `docs/manual/commands.md:660-664` lists only 13 of the sixteen. The surrounding text is what is off.
  - `docs/manual/commands.md:38-55`: "see below" links point into configuration.md.
  - `docs/manual/headless.md`: the exit-2 row (:195) is incomplete and overlaps exit 1; the `workflow_phase` text (:265) is narrower than its own row.
  - `docs/manual/building.md`: :3 lists winget without a "not live yet" caveat; :302-303 reads as if Scoop were pre-launch; :135 and :361 carry dated snapshots.
- **Makefile:** `Makefile:290,320` (and the :5 header comment) still say "Phase-2 acceptance gate".
- **Leftover example:** `layout.md:47` mocks the deleted ISSUES.md.
- **IDEAS.md:28:** a `[x] delivered` item remains.
- **Untested counts:** `docs/manual/reactions.md`, `docs/manual/daemon.md` and README hard-code "seven Floor guards"; only the README count is test-pinned (`TestReadmeStatesTheFloorGuardCount`).

## Acceptance

- Every item above is either fixed (doc follows code) or recorded here as not-a-defect with the reason.
- Any item that turns out to be a code defect is filed as its own issue.
- `make check` (slow mode on the Pi) stays green — the doc-count tests read some of these files.
