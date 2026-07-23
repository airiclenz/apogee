# Handoff — /refocus audit: docs verified against code; next is still the Linux closeout pass

Date: 2026-07-23
Session type: read-only workspace review (`/refocus`). **No code changes.** The only edits are
the two owner-approved ADR date-stamps recorded below, committed together with this handoff.

**Update (2026-07-23, later the same day):** the prompt-box chrome plan (Open-work item 3) has
since been implemented and archived in a separate session — commits `1000db5`, `2db57e6`,
`0aee84e`. The forward-looking sections below (Repo state, Open work, Suggested skills) are
updated to match; the Linux closeout pass (item 1) remains the headline next step, and still
needs a landlock-capable box — this devbox kernel has landlock off, so a green `make check` here
self-skips the live-enforcement checks and does not count as the pass.

## What this session did

Full-repo state review: three parallel read-only surveys (documentation inventory, code map,
planned-work sweep) followed by direct spot-verification of the load-bearing documentation
claims against the code. Findings below; the raw priorities live in `TODO.md` and the prior
handoff — referenced, not duplicated.

## Repo state (verified 2026-07-23)

- `main` clean. It was level with `origin/main` at the /refocus session; the later prompt-box
  work then left **local commits unpushed again** (`1000db5`, `2db57e6`, `0aee84e`, plus this
  doc update) — push when convenient. No stashes, no other branches.
- Tags `v1.0.0` … `v1.7.0` exist; Phase 5 sits in CHANGELOG `[Unreleased]`.
- GitHub: zero open issues, zero open PRs (gh authenticated, remote `airiclenz/apogee`).
  All tracking is in-repo (`TODO.md`, `ISSUES.md`, `docs/plans/`).
- `docs/plans/implementation-plan-apogee-merge.md`: all five phases marked ✅ Complete, but
  the file still sits in the active plans dir — archival deliberately waits on the Linux pass.
- Go source contains **zero actionable inline TODO/FIXME/HACK/XXX** — all debt is routed
  through `TODO.md` (24 grep hits are test fixtures or cross-references to it).

## Documentation claims verified against code (all confirmed)

- 21-tool default suite = 19 unconditional + host-gated `ask_user`/`present_document`
  (`internal/tools/registry.go:85`).
- Mode ladder plan → ask-before → allow-edits → auto, Shift+Tab cycle
  (`internal/domain/config.go:205`).
- 11 sealed Event variants (`internal/domain/events.go`).
- Slash commands `clear/new/compact/continue/confine`; `/skill` via picker; `/server`
  deliberately absent (`internal/tui/command.go:52`).
- Shipped validated set: one entry, `gemma-4-e4b-it-qat`, the pruned 16 mechanisms
  (`internal/validated/shipped.json`).
- Windows floor `windowsFloorBuild = 17763` (`internal/platform/winconfine.go:28`).

## Discrepancies found

1. **ADR 0013** says "DefaultTools is now ~19 built-ins" — outdated (now 21 with host-gated
   tools, see above).
2. **ADR 0019** note (~line 167) "The Windows opener ships unexercised until Phase 5" —
   dated; Phase 5 shipped 2026-07-22.
3. **ISSUES.md vs TODO.md on context size**: ISSUES.md line 3 keeps "context size displayed
   incorrectly / gauge wrong" OPEN while TODO.md records a context-window read fix
   (2026-06-28, `llamacpp-props`). Unresolved: either the ISSUES entry is a distinct
   display/gauge bug or the fix didn't fully take. **Needs a live repro — do not "correct"
   either doc; the code may be the wrong party.**
4. `docs/design/technical-design.md` is stale but self-declares it (2026-07-19 notice) —
   no action needed.
5. Prior handoff's "13 commits unpushed" — resolved by pushing; handoffs are historical
   snapshots, leave as is.

**Doc corrections — owner-approved and APPLIED 2026-07-23** (same session, committed with
this handoff):
- (a) ADR 0013: dated note after "~19 built-ins" — now 21 with the host-gated
  `ask_user`/`present_document` (ADR 0019).
- (b) ADR 0019: dated note on the Windows-opener line — Phase 5 shipped; the live opener
  check is folded into the owner-run smoke passes.
Nothing else was proposed; discrepancies 3–5 above were deliberately left untouched.

## Open work (priority order — authority is TODO.md §"Phase-5 verification leftovers" and the 2026-07-22 handoff)

1. **Closeout Linux pass — `make check` on the Linux devbox. Still the headline item.**
   New fact from this session: the session ran on a Linux machine inside the repo, so the
   pass is runnable right there. Caveat (already recorded in CHANGELOG "Known post-release
   verification"): this devbox kernel has landlock off, so `confinetest.Probe` self-skips
   live enforcement — the pass still covers everything else. Archive the merge plan after.
2. **Live landlock enforcement needs a landlock-capable box.** Owner asked for a distro
   recommendation this session; answer given: **Ubuntu 26.04 LTS** (released 2026-04,
   kernel 7.0 — clears fs ABI ≥1/kernel 5.13 and network ABI v4/kernel 6.7 with room;
   24.04 LTS, Fedora, Debian 13 also fine; avoid container images for the proof — seccomp
   can mask the landlock syscalls). Check on the new box: `apogee probe` should report
   landlock `{FSWrite:true, NetworkEgress:true}`, then `make check`.
3. **✅ DONE (2026-07-23):** the prompt-box chrome plan (two TUI bottom-chrome items,
   decisions locked) is implemented and archived — commits `1000db5` (uniform thin rules in
   the bottom-chrome frame), `2db57e6` (top-edge hairline row above the input box), `0aee84e`
   (plan `git mv`'d to `docs/plans/archived/`). Executed via `/implement-plan` forwarding
   `coding-standards`; `make check` green.
4. **Open owner call (decide, don't code):** prune the Windows disk-label walk or not
   (~1 ms/object; details in TODO.md item and the 2026-07-22 handoff).
5. Remaining verification residue, ISSUES.md bugs, parity P1s (`/server`, session UI),
   design-session items, and the >400-line Windows-file refactor: all enumerated in
   `TODO.md` and `ISSUES.md` — read those, they are current and accurate.

## Suggested skills

- `/implement-plan` (forwarding `coding-standards`) — done for the prompt-box chrome plan
  (item 3, 2026-07-23); reach for it again when the next multi-item plan lands.
- `/coding-standards` — mandatory for any new Go code.
- `/archive-handoffs` — after the Linux pass closes, the 2026-07-22 handoff (and this one)
  become archivable; also move the merge plan to `docs/plans/archived/`.
- `/refocus` — not needed again soon; this doc plus `TODO.md` is the current state.
