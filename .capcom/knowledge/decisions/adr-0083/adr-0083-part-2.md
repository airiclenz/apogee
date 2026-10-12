---
title: "ADR 0083 part 2: The ten live settings stay ten fields, not one Posture value (2026-09-15, review 09-14 #5)"
---

**Proposed.** One `Posture` value on the Agent — mode, confinement, scratch, generation,
compaction, prune, context files, parallel cap, seat, effort — swapped whole through
`SetPosture` / `Posture()`, the shape `Generation()` already has; `lateEngine` would hold one
pending value and replay it whole at `Bind`, `run.Spec` would hand one field over, and the child
spawn would copy `parent.Posture()`.

**Denied** (owner, 2026-09-15; plan `2026-09-15 - 00`, verdict table and *Out of scope*: "a
Posture value"). It contradicts ADR 0037
decision 2's consequence text — a live seam is **anytime-safe** (one mutex, one field, consumed at
a boundary the loop crosses constantly) or **idle-only validate-then-commit**, and there is no
third class — and a value swapped whole invites lost updates: two anytime-safe setters that
today touch two fields under two locks would, on one value, race to overwrite each other's field
with a stale copy. The 2026-09-20 review measured the residue at ten settings × six places
(field and mutex, getter, setter, cfg seed, the child re-copy in `subagent.go`, the `lateEngine`
pending-plus-`Bind`-plus-apply ladder); that cost is accepted. One exception already exists and
is recorded where it lives: `internal/agent/agent.go` states that ADR 0037 D2's "one mutex, one
field" is SUPERSEDED for the Generation by ADR 0076
A8, because Bypass and the Floor enable set must swap together — a value whose halves *must*
move on one clock is the one case a whole-value swap is the safe shape, and it stays the only one.

**Reopens on.** A second pair of settings that must move on one clock (the Generation argument
applying to another pair), or a demonstrated lost-update or torn-read across today's per-field
locks that a single value would close rather than open.

> **Note 2026-09-24 (review 09-20 #15; plan `2026-09-24 - 00`, *Ratified design calls*).** The
> 2026-09-20 review proposed the `lateEngine` half of this residue again in a smaller shape: its
> typed `pending*` fields folded into one ordered replay list of `func(*Agent) error`, refusable
> entries last. **Declined** (owner, 2026-09-24): the typed pending fields are kept, as the
> `lateEngine` pending-plus-`Bind`-plus-apply ladder this section already accepts as residue. Only
> the card's defect was taken: `Bind` replayed the delegation target before the seat, so the seat
> forgot the far width the target had just stated; it now replays the seat first (ADR 0069
> decision 6, 2026-09-24 note).
