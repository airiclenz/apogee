# Handoff — README freshness sweep (Status drift)

**Date:** 2026-08-03
**Origin:** noticed during the ADR 0031 north-star docs pass (committed as `8fe54b6`). That pass
fixed only the factual error (`v0.9.x` → `v0.10.x` in Status) and deliberately left the editorial
drift below for its own session.
**Size:** small — one focused session; mostly README.md.

## Known-stale spots

1. **README "Status", the "Newest on `main`" paragraph (~line 69).** It still features the
   session system (landed 2026-07-24). Since then, per CHANGELOG and ADRs: the one-`/`
   namespace with inline skill tokens (2026-07-28, ADR 0027), the `/model` + `/server`
   pickers (2026-07-28, ADR 0028), llama-launcher integration (2026-07-29, ADR 0029), the
   TUI width authority (2026-07-31, ADR 0030), and the session name on the top rule
   (2026-08-03). Verify the actual newest against `CHANGELOG.md` at fix time — more may have
   landed by then.
2. **Same paragraph: "Current work is per-model bench validation of the mechanism
   catalogue."** Current work is actually the fix waves from the 2026-08-01 merged findings
   (`docs/handoffs/2026-08-01 - 01 - merged-findings-roadmap.md`). Consider whether README
   should name in-flight work at all — a generic pointer to `CHANGELOG.md` + `docs/plans/`
   is what does not go stale; recommended.

## The sweep (beyond the two known spots)

Read README.md against the top of `CHANGELOG.md` and ADRs 0027–0031, and spot-check any
countable claim against the code (e.g. the "~21-tool suite", the in-chat commands table vs.
the actual command set). Fix only claims that are **wrong or stale**; this is a freshness
pass, not a rewrite.

## Constraints (do not violate)

- **No platform-vision text in README.** ADR 0031's non-goals say the platform direction is
  consciously unbuilt; README describes what exists. AGENTS.md / CONTEXT.md / ADR 0031 were
  all updated 2026-08-03 — do not re-touch them for this.
- **Never bump `VERSION` or add CHANGELOG release headings** (owner directive; suggest, never
  do). A docs-only commit needs neither.
- Verify every claim against code or CHANGELOG before writing it; do not describe features
  from memory.
- `make check` before committing; commit directly to `main`, **only when the owner asks**; no
  AI attribution trailers.

## Acceptance

- The Status paragraph names the actual newest landed work, and the "current work" sentence
  is either accurate or replaced by the generic pointer.
- No remaining README claim contradicts the code or CHANGELOG.
- One commit, e.g. `docs(readme): sync the Status narrative and sweep stale claims`.

## Out of scope

The `internal/tui/autotitle*.go` working-tree changes and the untracked
`docs/plans/2026-08-03 - 04 - table-cell-wrapping-plan.md` (other sessions' work-in-flight);
TODO.md / ISSUES.md content; anything user-facing beyond doc text.
