---
title: ADR 0071 D5 forbids a /settings toggle for Floor guard keys; all seven ship editable there
status: done
type: task
priority: 2
aliases: [apogee-tbs]
created: 2026-09-07T17:15:27Z
closed: 2026-09-11T10:28:50Z
reason: delivered by docs/plans/archived/2026-09-09 - 00 - deferred-findings-plan.md (17c105c1 090b0f63 480095e0 4cb5922e f3f9b321 94978659 5fe4fcc4 300336ff edb2bbe4 9ba9619e)
---

ADR 0071 Decision 5 states the Floor guard keys are 'File-only: no flag, no /settings toggle', so that turning a guard off is a deliberate edit rather than a posture reached by accident. The shipped code disagrees: all seven keys carry Editable: true in internal/config/registry.go:420-460 and render as editable rows in /settings (cmd/apogee/wire_settings.go:783-795, 1391-1421). CONTEXT.md's Floor guard entry already records the shipped behaviour as intended ('file-only (no flag, no env; editable live in /settings)'), so the concept map and the code agree and the ADR is the stale side. Owner call: amend ADR 0071 D5 to admit the /settings row, or make the rows read-only to match the record as written. Note D5 also names six keys; the 2026-09-07 amendment added tool-call-salvage, so whichever way this resolves the count needs to read seven.

## Design

Two exits. (a) Amend ADR 0071 D5: the /settings row stays, the ADR admits it, and the 'deliberate edit' rationale is restated for a toggle a user has to walk to. (b) Set Editable: false on the seven registry rows so the shipped surface matches the decision as ratified. (a) is the smaller change and matches what CONTEXT.md already documents; (b) is the one that honours the original reasoning about accidental postures.

## Acceptance

ADR 0071 D5 and the shipped /settings behaviour state the same thing, and the key count reads seven on both sides.
