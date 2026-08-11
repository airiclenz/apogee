# Subprocess gate reason must name the cell, not the class — implementation plan

- **Goal:** stop the Approval prompt telling users that confinement is unavailable on hosts
  where it is fully available. A gated subprocess call names the reach being authorised
  (`subprocess execution`) and names the host's incapacity **only** in the one ladder cell where
  that is the actual cause (Auto + confine-on + `fsConfineAvailable == false`).
- **Date:** 2026-08-11 · **Status:** not started
- **Sized for:** ~200k-context host
- **Authoritative sources:** ADR 0012 (the autonomy ladder × blast radius);
  `docs/design/confinement-execution-contract.md` §4 (the Gate `Reason` mapping, ~line 446);
  `internal/agent/resolution.go` (`gateReason`, `finishGate`, `resolveLadder*`);
  `docs/layout/user-questions-layout.md` (the approval-prompt mockup).
- **The bug, reproduced 2026-08-11 (owner, macOS, capable seatbelt):** in **ask-before** mode a
  terminal call was gated with `Reason: subprocess execution (confinement unavailable on this
  host)` while `/confine status` on the same host reported
  `backend: seatbelt (fs-write: available · network: available)`. Both statements come from
  apogee; they contradict each other and the prompt is the wrong one. Root cause:
  `gateReason` (`internal/agent/resolution.go:497`) derives the reason from the tool's
  blast-radius **class** alone, so every gated `classSubprocess` call gets the
  confinement-failure sentence — but ask-before, allow-edits and unknown-mode gate the
  subprocess surface as a **mode** decision, where the host's confinement capability plays no
  part. Only `resolveLadderAuto`'s caps-insufficient row is a real confinement failure.
  The string reaches the screen untransformed (`internal/agent/dispatch.go:583` →
  `domain.ApprovalRequest.Reason` → `internal/tui/approval.go:172` prints `"Reason: " + reason`),
  so the prompt is the whole explanation a user gets. A second route to the same false sentence:
  `resolutionInput.mode` is `effectiveMode()` (`dispatch.go:354`), so a sub-agent spawned in Auto
  whose parent tightens mid-delegation lands on those same gate rows while the footer still reads
  `auto`.
- **Ratified design calls** (owner, 2026-08-11, via AskUserQuestion):
  1. **Wording of the mode-driven gate: `subprocess execution`** — the bare noun phrase, matching
     the family every other class already uses (`network reach`, `unfiltered network reach`,
     `unconfinable MCP tool`, `out-of-workspace write`, `write`). It states the reach being
     authorised and nothing more. Rejected alternatives: naming the mode in the reason (only
     reason that would reference a rung rather than a reach) and `command execution` (diverges
     from the vocabulary the contract doc and ladder table use throughout).
  2. **The Auto + caps-insufficient cell keeps the full sentence** —
     `subprocess execution (confinement unavailable on this host)`. There it is true, and it is
     what sends a user to `/confine`.
  3. **`confineDemoteGateReason` (`resolution.go:53`) is untouched.** The runtime-demote path — a
     `Confine` verdict whose box failed to establish — genuinely is a confinement failure, and
     deliberately tells the same story in the same words. The duplicated literal is pre-existing
     and intentional; do not extract it into a shared constant as part of this plan.
  - Mechanical pins by the plan author (2026-08-11): the new helper is
    `subprocessGateReason(in resolutionInput) string`, placed directly beneath `gateReason`;
    `gateReason`'s signature changes from `(tool domain.Tool)` to `(in resolutionInput)` — it has
    exactly one caller, `finishGate` (`resolution.go:425`), which already holds the input.
- **Standing requirements:** skills: coding-standards. Run `make check` before every commit.
  Never change VERSION or a CHANGELOG release heading (suggestion only, see close).
- **Out of scope:**
  - Adding a remedy pointer (`/confine off …`) to the Approval prompt for the Auto +
    incapable-backend case. Today that remedy text lives only in `/confine status`
    (`internal/tui/confine.go:101-105`), so a user staring at the prompt gets the diagnosis with
    no route to the fix. Real gap, separate UX change, not this plan.
  - `internal/tui/model_test.go:1148,1249`, which use the long sentence as approval-prompt
    fixture text and assert on the `Reason: subprocess execution` prefix. They pass either way
    and the string they use is still one apogee produces — leave them alone.
  - Any change to the ladder itself: which cells gate is correct and unchanged. This plan
    changes only what a gate *says*.
  - The fail-open notes surfaced while diagnosing (`internal/tools/exec_common.go:137` runs
    unconfined when a handle carries a nil `Confiner`; `internal/agent/dispatch.go:673` can drop
    a `dispatchConfinementUnavailable` from the `box == nil` path). Neither is reachable through
    in-repo tools today; both are follow-ups, not this plan.

## 1. `gateReason` reads the ladder cell for the subprocess class

**What:** in `internal/agent/resolution.go`, change `gateReason` to take the full
`resolutionInput` instead of just the tool, and split the subprocess arm into its own helper:

- `gateReason(in resolutionInput) string` — the five other arms keep their literals verbatim
  (`network reach`, `unfiltered network reach`, `unconfinable MCP tool`, `out-of-workspace
  write`, `write`); the `classSubprocess` arm returns `subprocessGateReason(in)`. It switches on
  `classifyTool(in.tool)` as before.
- `subprocessGateReason(in resolutionInput) string` — returns
  `"subprocess execution (confinement unavailable on this host)"` when
  `in.mode == domain.ModeAuto && in.confineToWorkspace && !in.fsConfineAvailable`, else
  `"subprocess execution"`.
- Update the sole call site, `finishGate` (`resolution.go:425`): `gate.reason = gateReason(in)`.

Doc comments carry the reasoning, in this package's register: `gateReason`'s comment states that
six of the seven classes are a bare statement of the reach being authorised while the subprocess
class also reads the CELL, because only one of them is a confinement failure.
`subprocessGateReason`'s comment states both halves — in Auto with confinement asked for, a gate
means the backend could not give the fence ("gate if you can't"), so the host's incapacity IS the
reason and naming it is what points the user at `/confine`; every other rung gates the subprocess
surface as a mode decision, capable host or not, so claiming otherwise blamed a working
seatbelt/landlock backend for an approval the rung itself asked for and sent the user to
`/confine status` to be told the opposite (dated 2026-08-11).

Two conditions the implementer must NOT "simplify away", both deliberate:

- The three-term test spells out the ladder cell. `confineToWorkspace == false` in Auto cannot
  reach a subprocess gate today (`resolveLadderAuto:343-347` returns `resolveRun`), so that term
  is belt-and-braces documenting the cell, not live logic.
- A Tier-2 forced gate never reaches this function: `applyOverlays` sets `forceApprovalReason`
  first and `finishGate` only fills an EMPTY reason. No guard interaction is needed here.

**Tests:** `internal/agent/resolution_test.go`'s ladder table already varies
mode / confine / fsConfine per row, so it exercises the split directly. Change the expected
reason to `"subprocess execution"` on the six mode-decision rows — lines 87, 88, 92
(`subproc/ask-before`, `subproc/allow-edits`, `subproc/unknown-mode`) and lines 134, 135, 139
(the `RO+subproc/…` counterparts). Leave lines 90 and 137
(`subproc/auto-confine-caps-insuff`, `RO+subproc/auto-confine-caps-insuff`) asserting the full
confinement sentence: keeping both spellings in one table is what pins the split, and a run where
all eight rows agree would prove nothing. Update the section comment above the subprocess rows
(currently `// subprocess — confine when caps suffice, else gate ("confine if you can, gate if
you can't").`) so it also states that the gate's REASON names the host only in the caps-
insufficient cell. Add no new test file — the table is the right home.

**Acceptance:** `go test ./internal/agent/...` — the six retargeted rows fail before the change
and pass after; then `make check`.

**Commit:** `fix(agent): word a subprocess gate by its ladder cell, not its class`

## 2. Docs: the reason mapping and the two pinned mockups

Depends on item 1.

**What:** four documentation surfaces record the old class-keyed mapping and must follow the code.

- `docs/design/confinement-execution-contract.md` (~line 446) — the paragraph beginning
  **"A `Gate` carries `Reason` + `CacheKey`."** currently says `Reason` is "mapped from the
  tool's class" and lists one subproc spelling. Amend it so it records that subproc is the one
  class with TWO spellings and which cell each belongs to: `subprocess execution` on every
  mode-driven gate (ask-before, allow-edits, unknown-mode), and
  `subprocess execution (confinement unavailable on this host)` in the Auto + confine-on +
  caps-insufficient cell, where it is also the wording the runtime-demote fallback uses. Keep the
  rest of the paragraph (CacheKey, the MCP server grain, the Tier-2 override) untouched.
- `docs/layout/user-questions-layout.md:20-30` — the `# User Approval:` mockup pins a **terminal**
  approval carrying the confinement sentence, which is now the wrong example for the ordinary
  case. Change the reason line to `Reason: subprocess execution` and re-pad it to the frame's
  existing width; the border rows and every other line are unchanged. (The current line also
  carries a stray double space in `this  host` — it goes with the rewrite.)
- `layout.md` (~line 301-305) — the same three-line snippet inside **"What the approval prompt's
  body says is the call, in the call's own words."** Same edit, same frame width.
- `CHANGELOG.md` — one bullet under the EXISTING `### Fixed` heading in `## [Unreleased]`
  (line 629). Say what a user sees: an approval prompt for a terminal command in ask-before or
  allow-edits mode blamed the host for a gate the autonomy rung asked for, contradicting
  `/confine status` on a machine whose backend confines perfectly well; it now reads
  `subprocess execution`, and the confinement wording is kept for the case that earns it.
  Do NOT add a release heading and do NOT touch `VERSION`.

**Tests:** none — documentation only. Grep is the check (see Acceptance).

**Acceptance:**
`grep -rn "confinement unavailable on this host" docs/ layout.md CHANGELOG.md` returns only
prose that describes the Auto/caps-insufficient cell and the runtime demote — no mockup line and
no unconditional class mapping; `grep -n "Reason: subprocess execution" layout.md
docs/layout/user-questions-layout.md` shows the bare wording in both mockups; then `make check`.

**Commit:** `docs(confinement): record the two subprocess gate reasons and their cells`

## Close

**Suggested version bump: patch.** This is a user-facing bug fix with no API or behavioural change
beyond prompt wording — no ladder cell changes, nothing new is gated or ungated. Whether and when
to bump is the owner's call; no item in this plan touches `VERSION`.
