# Confinement gates carry their remedy; the two fail-open paths close — implementation plan

- **Goal:** the one approval prompt that truthfully blames the host (confinement unavailable)
  also names the way out, and the two paths where apogee could silently skip confinement — or
  silently swallow a tool's claim that confinement failed — fail closed and say so. Follow-ups
  from the 2026-08-11 gate-reason fix; the wording fix itself is done and confirmed
  (`e199025`, verified on the owner's macOS host 2026-08-12 after a source build).
- **Date:** 2026-08-12 · **Status:** not started
- **Sized for:** ~200k-context host
- **Authoritative sources:** ADR 0012 (autonomy ladder × blast radius);
  `docs/design/confinement-execution-contract.md` §2.2 (a subprocess tool that cannot establish
  the box returns `ErrConfinementUnavailable` rather than running unconfined) and §4 (the Gate
  `Reason` mapping); the archived plan
  `docs/plans/archived/2026-08-11 - 03 - subprocess-gate-reason-plan.md`, whose out-of-scope
  list is where items 2 and 3 were first recorded. Code pins (at `e199025`+):
  `internal/agent/resolution.go:525` (`subprocessGateReason`), `resolution.go:474`
  (`confineFallback`), `internal/agent/dispatch.go:641` (`areq := domain.ApprovalRequest{…}`,
  the single construction site in non-test code), `dispatch.go:731` (the
  `ErrConfinementUnavailable` → `dispatchConfinementUnavailable` translation),
  `dispatch.go:468` (`executeRun`'s nil-box `executeTool` call),
  `internal/tools/exec_common.go:137` (the `conf.Confiner != nil` guard),
  `internal/tui/confine.go:101-105` (the `/confine status` remedy wording items here mirror).
- **Ratified design calls** (owner, 2026-08-12, via AskUserQuestion):
  1. **Scope triage:** remedy pointer IN; the two fail-open truth gaps IN; a release item OUT —
     the gate-reason fix reaches Homebrew users with the next regular release, and no item in
     this plan performs any release or version act.
  2. **Transport is a structured field:** `Remedy string` on the gate resolution and on
     `domain.ApprovalRequest`; the TUI renders it as its own line under Reason. Rejected:
     appending remedy text to the Reason string (mixes diagnosis with remedy and bakes one
     Driver's phrasing into the engine's reason).
  3. **Coverage is BOTH confinement gates:** the Auto + confine-on + caps-insufficient ladder
     cell and the runtime-demote gate (a Confine verdict whose box failed to establish). Same
     cause, same remedy. No other gate carries a remedy — a mode-driven gate has nothing to fix.
  4. **Wording:** the field carries the bare sentence
     `/confine off runs commands unconfined this session (disposable machines only)`; the TUI
     prefixes its display label `Fix: ` the same way it prefixes `Reason: ` — the label is
     presentation, so it stays out of the engine (wire-silent, ADR 0031).
- **Standing requirements:** skills: coding-standards. Run `make check` before every commit.
  Never change VERSION or add a CHANGELOG release heading (suggestion only, see close).
- **Execution precondition:** the working tree currently carries the in-flight skill-read-roots
  plan (`2026-08-12 - 00`). Start executing THIS plan only on a clean tree after that plan
  lands — the files are disjoint, but a dirty tree stops the executor at preflight. There is no
  code dependency between the two plans.
- **Out of scope:**
  - Any release, tag, VERSION change, or CHANGELOG release heading (design call 1). The stale
    brew binary the owner hit is release staleness, not a code defect — v0.13.0 was cut
    2026-08-11 hours before `e199025` landed.
  - The gate Reason wording itself — settled by the archived 2026-08-11 plan; both spellings and
    their cells are correct on main and pinned by the resolution ladder table.
  - The `/confine status` text (`internal/tui/confine.go`) — already carries the remedy; items
    here mirror its wording, not change it.
  - The approval-pane mockups (`docs/layout/user-questions-layout.md`, `layout.md`) — both pin
    the ask-before example, a mode-driven gate that carries no remedy, so they are correct
    unchanged. Only the contract doc records the new field (item 1).
  - `resolutionInput.mode` being `effectiveMode()` for sub-agents — truthful since the wording
    fix; nothing to do.

## 1. Remedy field: the confinement-unavailable gates name their fix — ✅ DONE (2026-08-12)

NOTES (2026-08-12): the TUI half landed as a NEW test (`TestModelApprovalDrawsRemedyUnderReason`)
carrying both halves — Fix-under-Reason with a remedy, no Fix line without — rather than by editing
an existing fixture in place: the nearest fixture belongs to
`TestModelApprovalTerminalShowsCommandBlock`, which is about argument rendering, and mutating it
would have changed what that test is about. Existing remedy-less fixtures still pin the absence
implicitly; the new test pins it explicitly.

**What:** plumb one optional remedy sentence from the resolver to the approval prompt.

- `internal/domain/approval.go` — add `Remedy string` to `ApprovalRequest` beneath `Reason`,
  with a doc comment in the file's register: the optional one-line route out of the condition
  that forced this approval; empty on every gate whose cause is the autonomy rung itself, so
  most prompts read exactly as before.
- `internal/agent/resolution.go` — add `remedy string` to the `resolution` struct and a
  package-level constant holding the ratified sentence (design call 4) beside
  `confineDemoteGateReason`. Populate it in exactly two places:
  - the Auto + confine-on + caps-insufficient **subprocess** cell — the same cell that selects
    the long reason in `subprocessGateReason`. Binding invariant: the reason and the remedy must
    derive from ONE cell predicate written once, so they can never disagree (e.g.
    `subprocessGateReason` returns both, or `finishGate` calls one helper that yields the pair —
    the shape is the implementer's choice under coding-standards; two independent copies of the
    three-term test is the one shape that is wrong).
  - `confineFallback`'s gate branch (`resolution.go:483`) — the runtime-demote gate, beside
    `confineDemoteGateReason` (design call 3).
- `internal/agent/dispatch.go` — carry the remedy into `areq` at the single
  `ApprovalRequest` construction site (`dispatch.go:641`). `approve()` today takes the reason as
  a bare string; extend its parameters so both call sites pass their verdict's remedy through
  (`executeGate` → `verdict.remedy`, `executeConfineFallback` → `fb.remedy`).
- `internal/tui/approval.go` — in `approvalPrompt`, directly after the Reason part:
  when `req.Remedy != ""`, append `"Fix: " + stripEscapes(req.Remedy)` as its own part, so the
  existing pane layout machinery (wrapping, elision, row budgeting) handles it like every other
  prose part.

**Tests:** a focused test in `internal/agent/resolution_test.go` beside the ladder table —
do NOT add a remedy column to the table (three non-empty cells do not justify churning ~40
rows; the table's `wantReason` already pins which cell is which). Assert: the subprocess and
RO+subprocess caps-insufficient cells carry the remedy; the subprocess ask-before cell and the
3p-net auto-confine cell carry none; `confineFallback`'s gate carries it and its refuse branch
does not. In `internal/tui/model_test.go`, extend one approval fixture with a `Remedy` and
assert the `Fix: ` line renders under the Reason line; existing fixtures without a remedy pin
that the line is absent.

**Acceptance:** `go test ./internal/domain/... ./internal/agent/... ./internal/tui/...`;
`grep -n "Remedy" internal/domain/approval.go internal/agent/dispatch.go
internal/tui/approval.go` shows the field, the carry, and the render; then `make check`.
Docs owned by this item: the §4 paragraph of
`docs/design/confinement-execution-contract.md` that records what a Gate carries gains the
`Remedy` field and its two cells; one CHANGELOG bullet under `### Added` in `## [Unreleased]`
(the prompt that says confinement is unavailable now also says `/confine off` is the way out).

**Commit:** `feat(agent): confinement-unavailable gates carry their /confine remedy`

## 2. A nil-Confiner confinement handle fails closed — ✅ DONE (2026-08-12)

NOTES (2026-08-12): the canary test lives in a new `internal/tools/exec_common_test.go` (the file
under test had none) and skips on Windows, because the canary command is a POSIX shell line — the
same shape and skip every sibling confinement test in the package uses (`TestTerminal_RunsUnderConfine`).
The guard it pins is platform-independent.

**What:** `internal/tools/exec_common.go:137` — the guard
`if conf, ok := domain.ConfinementFromContext(ctx); ok && conf.Confiner != nil` silently runs
the command UNCONFINED when a handle is installed but its `Confiner` is nil. That is the
fail-open the contract's §2.2 forbids: a Confine verdict wired with a nil Confiner would escape
the fence without a word. Change: when `ok && conf.Confiner == nil`, return
`domain.ErrConfinementUnavailable` (wrapped with the `argv[0]` context, matching the existing
`confine %s: %w` error shape) instead of running. The no-handle case (`!ok`) still runs
unconfined by design — that is every gated/approved run. With dispatch's demote net, the broken
wiring now surfaces to the human as the truthful forced-Approval demote instead of a silent
unconfined run. Not reachable through in-repo tools today (resolve() only installs a handle
with the agent's non-nil Confiner) — this closes the door for embedders and future wiring.

**Tests:** in `internal/tools`, a test that installs
`domain.Confinement{Confiner: nil}` in the context, runs the subprocess path, and asserts the
result is an error satisfying `errors.Is(err, domain.ErrConfinementUnavailable)` and the
command did not run (canary: the command would create a file; assert it is absent). Follow the
existing exec test harness in the package.

**Acceptance:** `go test ./internal/tools/...`; then `make check`. One CHANGELOG bullet under
`### Fixed` in `## [Unreleased]`.

**Commit:** `fix(tools): a nil-Confiner confinement handle fails closed instead of running unconfined`

## 3. An unconfined run never swallows a confinement-unavailable claim — ✅ DONE (2026-08-12)

**What:** `internal/agent/dispatch.go:731` — `executeTool` translates a tool's
`ErrConfinementUnavailable` into `dispatchConfinementUnavailable` regardless of whether a box
was installed. On the nil-box path (`executeRun`, `dispatch.go:468` — plain Run verdicts and
post-approval unconfined re-runs) the caller ignores that outcome: the call is recorded as
EXECUTED with a zero `ToolResult`, no event fires, and the model gets an empty result — the one
place a "could not confine" claim vanishes silently. A third-party or host-registered tool
returning that sentinel outside a Confine verdict hits it. Change: gate the translation on
`box != nil`; with a nil box the error takes the branch below it (ErrorEvent + `errorToolResult`
to the model), like any other tool error. Update `executeTool`'s doc comment — "This only
arises on a Confine call" stops being an assumption and becomes what the code enforces.

**Tests:** in `internal/agent`, a fake tool whose Execute returns
`domain.ErrConfinementUnavailable`, dispatched through a **Run** verdict: assert an error tool
result reaches the model, an `ErrorEvent` fires, and no demote event fires. The Confine-verdict
demote path is already pinned by existing tests — they must stay green.

**Acceptance:** `go test ./internal/agent/...`; then `make check`. One CHANGELOG bullet under
`### Fixed` in `## [Unreleased]`.

**Commit:** `fix(agent): an unconfined run surfaces a confinement-unavailable claim as an error`

## Close

**Suggested version bump: patch/micro** (the usual between-release `VERSION` micro-bump, e.g.
v0.13.4 → v0.13.5) — item 1 is a small user-facing prompt improvement and items 2–3 are
defensive fixes on paths unreachable in-repo today. Whether and when to bump is the owner's
call; no item in this plan touches `VERSION`. Reminder recorded at write time: the 2026-08-11
gate-reason fix reaches Homebrew users only when the next release is cut — the owner chose not
to fold a release item into this plan.
