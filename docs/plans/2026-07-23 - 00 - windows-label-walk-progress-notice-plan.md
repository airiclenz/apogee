# Implementation plan — Windows label-walk progress notice (keep semantics, tell the user)

**Date:** 2026-07-23. **Status: NOT STARTED.** Owner decision recorded 2026-07-23 (the point-2
"OWNER CALL" from `docs/handoffs/2026-07-23 - 01 - linux-closeout-...md` and `TODO.md`
§"Phase-5 verification leftovers"): **"Notice, keep semantics."** Keep the ratified full-tree Low
labelling exactly as-is; add a user-visible notice so the one-time walk stops being a silent hang.
Real pruning (excluding `.git`/`node_modules`/ignored trees) was **rejected here** because it
changes ratified box semantics — the confined child could no longer write into excluded trees,
breaking `git commit` and `npm ci`/toolchain writes under Auto — and it belongs with the parked
**"Windows Auto: box-local `%TEMP%` / toolchain caches"** design session (`TODO.md`), where the
same expensive trees are the caches that work wants to relocate.

**Execute with `/implement-plan`, forwarding `coding-standards`.** This is Windows-tagged runtime
code: the two label-walk code paths live in `internal/platform/confiner_windows.go`
(`//go:build windows`) and can only be *run* on the Windows execution machine. This Linux devbox
cross-compiles `GOOS=windows` and runs the untagged decision/wording helpers, but not the walk
itself — so Item 1 (untagged) is fully verifiable on Linux, and Item 2 (the emission seam) carries
a **design call** the master must stop on before a sub-agent wires it.

**Precedence for design questions:** ADR 0012,
[ADR 0020](../adr/0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md),
[ADR 0021](../adr/0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md),
and `docs/design/confinement-execution-contract.md` govern everything confinement-shaped. The
`Confine` contract (§2 "performs no I/O", amended for this backend in §9) must stay intact: the
notice may not add blocking work, run the command, or change *what* is labelled — only *report*
the walk that already happens.

## Why

The Windows box labels the workspace tree Low so the low-integrity confined child can write inside
it (ADR 0020 §2). Phase-5 item 8 measured what ADR 0020 accepted but never quantified: the label
pass costs **~1 ms per object**, so a synthetic 5,051-object tree took **5.2 s to label and 2.2 s
to revert**. It is paid once per box per session — the first confined command labels, shutdown
reverts — but a workspace with a large `.git` or `node_modules` makes that first `Confine`
**block visibly with no explanation**: the exact click-through-frustration trap the
auto-confinement-degradation work (`internal/security/doc.go`, the `/confine` design) was built to
avoid. The fix is not to label less (that dissolves the fence for those trees) but to say what is
happening while it happens.

## Ground truth (verified 2026-07-23 — anchors, not vibes)

- **Where the walk lives (decides where tests run):** `internal/platform/winconfine.go` is
  **UNTAGGED** — journal read/write/list, residue/teardown wording, guardrails, `belowWindowsFloor`
  — Linux-table-testable, and the home of every pure wording helper
  (`ConfinementTeardownNotice`, `windowsResidueNotice`). `internal/platform/confiner_windows.go` is
  `//go:build windows` — the token, `Confine`, `labelBox`, `labelTree`, the `filepath.WalkDir`
  passes. The pure-helper-in-untagged-file + emit-at-a-seam split is the established idiom (Item 1
  of `docs/plans/archived/2026-07-22 - 02 - phase5-review-fixes-plan.md`).
- **The walk is lazy and once-per-box:** `Confine` (`confiner_windows.go:216`) calls `labelBox`
  (`:269`), which skips a root already in the `c.labelled` folded memo (`:289`) and otherwise walks
  it via `labelTree` (`:426`, the `filepath.WalkDir`). So the expensive pass runs at most once per
  root per session, on the **first** confined command that names it — **mid-session**, not at
  startup.
- **Every existing notice is pre-alt-screen stderr:** `wire.go` prints the teardown notice
  (`:144`), the unconfined-Auto warning (`:187`), `probe.DegradedNotice` (`:196`), and
  `contextWindowNotice` (`:203`) with `fmt.Fprintln(os.Stderr, …)` — all **before** the Bubble Tea
  alt-screen takes the terminal. A raw stderr write from inside `labelBox` during a live session
  would land **on top of the alt-screen** and corrupt the TUI. This is the seam problem Item 2
  resolves.
- **No object count is known before the walk.** `labelTree` streams via `WalkDir`; a pre-count is
  a second full walk (doubling the ~1 ms/object cost), so the "please wait" notice must be
  **indeterminate and upfront** (no live "N objects") or an after-the-fact summary — and an
  after-the-fact summary is useless as a wait notice because it prints *after* the wait. Item 1's
  helper therefore takes no count; the illustrative "5,051 objects" preview from the decision
  prompt is not the shipped wording.
- **`NewConfiner()` takes no args** (`confiner_windows.go:114`); the token constructor is
  `newTokenConfiner(home string)` (`:145`). Any writer/delegate the seam needs is a new
  construction parameter, not an interface change (`domain.Confiner` must not sprout an OS-specific
  hook — ADR 0010, the reason `Close` is an optional-interface assertion at the composition root).

## 1. Untagged progress-notice wording helper + emit threshold

**What:** In `internal/platform/winconfine.go`, add a pure, exported wording helper mirroring
`ConfinementTeardownNotice` / `windowsResidueNotice` — e.g.
`WindowsLabelProgressNotice(root string) string` — returning the one-line "labelling the workspace
Low; a large `.git`/`node_modules` may take several seconds" wait notice, reusing the shared
`windowsLabelRemedy`/`icacls` wording constants where it references the fence so a third phrasing is
never invented. It takes **no object count** (see Ground truth — a pre-count doubles the walk) and
is worded as the fence working, never as a malfunction, matching the `probe.DegradedNotice` tone.
Keep it in the untagged file so it is table-testable on Linux. Do **not** wire it yet — Item 2 owns
the emission seam.

**Tests:** Untagged table test in `internal/platform/winconfine_test.go`: the notice names the
workspace root, is non-empty, and its fence wording is byte-identical to the shared remedy constant
(the `windowsResidueNotice` byte-identity assertion pattern). Runs on Linux.

**Acceptance:** `go test ./internal/platform/...` green on Linux; the wording is a pure function of
its input with no I/O and no OS calls.

**Commit:** `feat(confine): add the Windows label-walk progress-notice wording`

## 2. Emit the notice around the first label walk — DESIGN CALL (master must stop here)

**⚠️ needs-design-call — do not let a sub-agent pick the seam. Stop and confirm with the owner.**
The walk is lazy and mid-session; all existing notices are pre-alt-screen stderr. Two coherent
resolutions, each with a real cost:

- **(A) Eager pre-warm at startup (RECOMMENDED).** In `wire.go`, when the resolved config is
  **Auto + confinement asked-for + Windows token backend with `FSWrite == true`**, call `labelBox`
  once on the workspace root during startup — pre-alt-screen — printing
  `WindowsLabelProgressNotice` to `os.Stderr` first, exactly like the other startup notices. The
  first in-session `Confine` then hits the `c.labelled` memo and no-ops. *Keeps the stderr/pre-
  alt-screen invariant intact and needs no domain-seam change.* Cost: it moves the *timing* of the
  (already-ratified) disk mutation from "first confined command" to "startup", so apogee labels the
  disk before the user issues any tool call. Under Auto+confine a confined command is effectively
  certain, and `Close` still reverts at shutdown, so this is a timing change, not a *what-is-
  labelled* change — consistent with the owner's "keep semantics" (which was about the label set,
  incl. `.git`). Extra roots added mid-session (rare) still walk lazily and silently; that is
  acceptable since the workspace root is the bulk.

- **(B) Lazy with a progress delegate to the transcript.** Give `newTokenConfiner` an optional
  progress sink and thread it from the TUI so the notice renders **in the transcript** (like
  `/confine status`), leaving the walk lazy. *Keeps the exact current timing.* Cost: a domain-seam
  change that overlaps the **already-parked** "surface the startup notice in the transcript, not
  just stderr" residue (`TODO.md`, deferred follow-up 04, the in-transcript-banner work this repo
  explicitly has not built). Heavier, and it drags in a framework decision that is out of this
  plan's scope.

**Recommendation: (A).** It ships the whole user-visible win with zero domain-seam churn and keeps
the stderr/pre-alt-screen notice invariant every other notice already obeys; (B) waits for the
in-transcript-banner design that owns that surface. Confirm before wiring.

**What (assuming A, pending confirmation):** Add the startup pre-warm block to `wire.go` behind the
exact trigger cell above (reuse the `probe.DegradedNotice` gate's inputs — do not broaden it), and
`fmt.Fprintln(os.Stderr, platform.WindowsLabelProgressNotice(workspaceRoot))` immediately before
the pre-warm `Confine`/`labelBox` call. The pre-warm must be a genuine no-op on non-Windows and on
a Windows host that reports `FSWrite == false` (the walk never runs there). Do not change `labelBox`
or `labelTree`.

**Tests:** Untagged table test on the trigger predicate (the "should pre-warm?" decision extracted
as a pure function of mode × confine-asked × capabilities, the `contextWindowNotice`/
`DegradedNotice` seam pattern) — pre-warm iff Auto ∧ confine-asked ∧ FSWrite. Windows-tagged: a
first-`Confine`-after-pre-warm hits the memo (no second walk); a session with pre-warm disabled
still labels lazily on first `Confine`. macOS/Linux: the pre-warm predicate returns false, so
startup is byte-unchanged.

**Acceptance:** on the Windows execution machine under Auto+confine, launching apogee prints the
progress notice pre-alt-screen and the workspace is labelled before the first tool call; the first
confined command does not re-walk; `Close` reverts as today. On Linux/macOS, startup output is
unchanged. `go test ./...` green on Linux and natively on Windows.

**Commit:** `feat(confine): pre-warm the Windows label walk with a startup progress notice`

## After the plan

- Flip the `TODO.md` §"Phase-5 verification leftovers" bullet **"OWNER CALL — whether to prune the
  Windows disk-label walk"** to DONE, noting the decision (notice, keep semantics; pruning deferred
  to the box-local-`%TEMP%` session) and pointing at this plan.
- The manual Windows Auto-confined *deliverable* run (still owner-run, `TODO.md` / CHANGELOG) is the
  natural place to eyeball the new notice on a real large-tree workspace.
