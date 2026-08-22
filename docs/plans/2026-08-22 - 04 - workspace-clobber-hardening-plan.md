# Workspace-clobber hardening — terminal fail-fast, session scratch dir, mutation warning

**Goal:** close the three harness faults behind the 2026-08-22 workspace-clobber incident
(`docs/handoffs/apogee-workspace-clobber-incident.md`): a confinement denial that did not
stop the script, no writable scratch location outside the workspace, and a masked exit
status that reported success for a call that destroyed a tracked file.

- **Date:** 2026-08-22
- **Status:** unexecuted
- **Sized for:** ~200k-context host
- **Authoritative sources:**
  - `docs/handoffs/apogee-workspace-clobber-incident.md` (the incident, faults, ranked fixes A–E)
  - `docs/design/confinement-execution-contract.md` (Confine contract, escape-probe battery §6, box-construction §7)
  - ADR 0012 (confinement attaches to blast radius; EPERM-on-escape, no prompt)
  - ADR 0006 (structural floors survive Bypass — governs item 5's always-on call)
  - ADR 0008 (stateless tools; fresh `sh -c` per call — unchanged by this plan)
- **Ratified design calls** (owner, via AskUserQuestion, 2026-08-22):
  1. Fail-fast = prepend `set -e` to every POSIX terminal script, plus `set -o pipefail`
     when a one-time probe shows the host `sh` accepts it. Not config-gated.
  2. Scratch dir = `~/.apogee/scratch/<session-id>/`, a new sibling root under the
     dotdir; added to the confinement box's `WritablePaths`; startup GC deletes scratch
     dirs older than 14 days. Sessions storage stays flat.
  3. Tracked-file mutation warning = **always-on structural floor** (runs in every mode
     including Bypass, like the tool-result clamp per ADR 0006), active only when the
     workspace root is a git repository.
  4. Prompt wiring = new `{{scratch}}` placeholder in the closed template set, plus a
     scratch-guidance line in the shipped default prompt. Per-session constant, KV-cache
     safe.
- **Skills:** coding-standards
- **Out of scope:**
  - Windows `cmd /c` fail-fast semantics (`cmd` has no `set -e` analogue) — item 1
    documents the asymmetry, nothing more.
  - Surfacing `confine-writable-paths` in user config and toolchain-cache seeding
    (contract §7) — existing open item at `ISSUES.md:626-646`, not this plan.
  - Hook-time and MCP subprocesses — the preamble and warning attach to the `terminal`
    tool path only.
  - Undo-journal coverage of subprocess writes (ADR 0051 boundary stands).
  - Any version bump (see closing note).

Any authorized deviation from item text must land as a dated NOTES line under the item.

---

## 1. Fail-fast preamble for POSIX terminal scripts — ✅ DONE (2026-08-22)

NOTES (2026-08-22): pipefail live-path test skips on this host — its `sh` is dash without `set -o pipefail`, so the probe correctly composes `set -e` alone; both compositions are pinned by the pure table test (`composeFailFastPreamble`), and `TestFailFastPreambleMatchesTheHostProbe` cross-checks the cached probe against a direct `sh -c "set -o pipefail"` run.

**What:** In `internal/tools/terminal.go` `Execute`, prepend a fail-fast preamble to the
model's command string before it reaches `shellHost.Command` (`terminal.go:106`):
`"set -e\n"` always, plus `"set -o pipefail\n"` when supported. Support is determined by
a one-time, cached probe in `internal/platform` (package-level `sync.Once`): run
`sh -c "set -o pipefail"`; exit 0 ⇒ supported. The preamble applies **only** to the
terminal tool's POSIX path — never to hook subprocesses, and never on Windows
(`cmd /c` lines pass through verbatim; state the asymmetry in the `Terminal` doc
comment). Export the composed preamble (constant or function, e.g.
`platform.FailFastPreamble()`) so item 6's battery can reuse it. The preamble must not
disturb `preflightCommandLine` (prepend after the preflight parse) or the
`[exit code N]` marker.

With `set -e`, a denied first command in an `&&` chain aborts the whole script before
any unguarded later line runs — this alone prevents the incident's clobber.

**Files:** `internal/platform/host.go`, `internal/tools/terminal.go`,
`internal/tools/terminal_test.go`, `internal/platform/host_test.go`

**Tests:**
- A multi-line script whose first command fails (`false` then `echo reached > f`) exits
  non-zero and does **not** create `f` (the incident shape, minus confinement).
- With pipefail supported on the test host, `false | cat` reports non-zero; without it,
  the test skips.
- A single successful command still reports exit 0 and unchanged output.
- Windows path (existing `terminal_windows_test.go` guards) unchanged: no preamble.

**Acceptance:** `go test ./internal/tools/... ./internal/platform/...`

**Commit:** `feat(tools): fail-fast preamble (set -e, probed pipefail) for terminal scripts`

## 2. Label confinement denials in the terminal result — ✅ DONE (2026-08-22)

NOTES (2026-08-22): the append site is the shared renderer `subprocessToolResult`, as the item's mechanism names — so a confined `python_exec` failure with denial-shaped output gains the same label as terminal; not a deviation, just the mechanism's reach stated.

Depends on item 1 (shares `terminal.go`).

**What:** When a terminal call ran confined and returns an error result, scan the
combined output for OS-denial signatures (`Operation not permitted`,
`operation not permitted`, `EPERM`) and append one labeled line to the result content:
`[likely blocked by workspace confinement: writes are allowed only inside the workspace
and the session scratch dir]`. Mechanism: `runSubprocess`
(`internal/tools/exec_common.go`) already resolves the confinement handle to call
`Confine`; record a `confined bool` on its result struct, and let
`subprocessToolResult` (`terminal.go:156-173`) do the match-and-append on `IsError`
results only. Best-effort heuristic (strerror text is locale-dependent) — a miss costs
nothing; never force an error on an exit-0 result.

**Files:** `internal/tools/exec_common.go`, `internal/tools/terminal.go`,
`internal/tools/terminal_test.go`, `internal/tools/exec_common_test.go`

**Tests:**
- A confined (fake-confiner) call whose output contains `Operation not permitted` and
  exits non-zero gets the label; the same output unconfined does not; a confined exit-0
  result never gets it.

**Acceptance:** `go test ./internal/tools/...`

**Commit:** `feat(tools): label confinement-denial failures in terminal results`

## 3. Per-session scratch dir inside the confinement box — ✅ DONE (2026-08-22)

NOTES (2026-08-22): "follows the **active** session" needed seams beyond the item's named files: session ids are minted lazily at first Save, after a session's first tool call — so `sessionHost` (cmd/apogee/wire_session.go) now pre-mints the next id at construction and at Rotate (Save adopts it; store behaviour, `ActiveID()`, and title/CreatedAt semantics unchanged) and pushes each identity boundary's dir through a `scratchMoved` listener; `lateEngine` (wire_engine.go) forwards it as `SetScratchDir` with the standard pending-while-unbound pattern; the engine gained the live field + `Agent.ScratchDir/SetScratchDir` (agent.go, mode/confine idiom), a `confinementBox()` live fold in dispatch.go used by both per-call box sites (resolutionInput and hookExecutionCtx), and live inheritance at spawn (subagent.go). Wire seeding lives in wire_live.go's `wireSession` (the item named wire.go, which holds the scratch root, GC, and `ensureScratchDir`) because that is where Config assembly meets the session host.

NOTES (2026-08-22): `ConfinementBox()` appends the scratch dir into a FRESH slice (never the host's `ConfineWritablePaths` backing array); a guard test pins it. Creation failure or a disabled seam yields "" — a path that does not exist is never advertised writable.

NOTES (2026-08-22): deriveDeps' mechanism-hook exec fence and a scheduled Firing's copied Config carry the construction-time scratch seed rather than the live value — hook/MCP subprocess surfaces are out of the plan's scope, and the seed is still a valid writable root.

**What:** New dotdir root `~/.apogee/scratch/<session-id>/` (sibling of `sessions/`,
`library/`, … — composed in `cmd/apogee/wire.go` next to `wire.go:349-368`).

- Add a `ScratchDir string` field to `domain.Config`
  (`internal/domain/config.go`); `Config.ConfinementBox()`
  (`internal/domain/confinement.go:84-90`) appends it to `WritablePaths` when non-empty.
- The dir is created (0700, `MkdirAll`) when the session ID is minted, before the first
  tool call, and follows the **active** session: the box handed to each tool call
  (`dispatch.go:404-419`) must carry the current session's scratch path. Binding: the
  scratch path lives on the Config/Agent seam that already knows the session identity;
  the engine stays wire-silent (ADR 0031) — no new Driver obligations.
- Startup GC in wire boot: best-effort removal of `~/.apogee/scratch/*` entries whose
  mtime is older than 14 days; errors ignored.
- Sessions storage stays a flat `<id>.json` — no layout migration.

**Files:** `internal/domain/config.go`, `internal/domain/confinement.go`,
`internal/domain/confinement_test.go`, `cmd/apogee/wire.go`,
`internal/agent/dispatch.go`, `internal/agent/construct_test.go`

**Tests:**
- `ConfinementBox()` includes `ScratchDir` in `WritablePaths` when set, omits when empty.
- GC removes an old dir, keeps a fresh one (temp-dir fixture with backdated mtime).
- An agent-level test asserts the box passed to a confined tool call contains the
  scratch path.

**Acceptance:** `go test ./internal/domain/... ./internal/agent/... ./cmd/apogee/...`

**Commit:** `feat(confine): per-session scratch dir ~/.apogee/scratch/<id> in the writable box`

## 4. `{{scratch}}` prompt placeholder and default-prompt guidance — ✅ DONE (2026-08-22)

NOTES (2026-08-22): the loop-level render test lives in `internal/agent/promptseam_test.go` (the existing seam harness), not in a file the item names — `TestPromptSeam_ScratchPlaceholderRendersSessionScratchDir` drives a Turn and asserts the seeded system message carries the scratch path verbatim.

NOTES (2026-08-22): four files beyond the item's list carry comment-only "known three"/placeholder-enumeration re-syncs the new fourth placeholder made stale (`internal/domain/config.go`, `internal/config/config.go`, `internal/config/registry.go`), and `internal/config/config_test.go`'s unknown-placeholder case now also expects `{{scratch}}` in the error's known list.

Depends on item 3.

**What:** Extend the closed placeholder set in `internal/prompt/prompt.go` (`:32-35`)
with `{{scratch}}`; add `Scratch` to `prompt.Inputs`; render it in
`Agent.systemPrompt()` (`internal/agent/loop.go:814-822`) from the session scratch
path. Update the shipped default prompt
(`internal/config/defaults/config.yaml:233-256`) with one guidance line, binding text:
"Scratch and test files go in {{scratch}} — it is writable; the workspace is for the
project's own files only. /tmp may not be writable." Re-sync the defaults tests
(the template is asserted verbatim — see commit `4ee319ef`). The placeholder is
per-session constant, so KV-cache stability (`prompt.go:13-16`) is preserved. Custom
prompts opt in by using the placeholder; `Validate` accepts it like the other three.

**Files:** `internal/prompt/prompt.go`, `internal/prompt/prompt_test.go`,
`internal/agent/loop.go`, `internal/config/defaults/config.yaml`,
`internal/config/defaults_test.go`

**Tests:**
- `Render` substitutes `{{scratch}}`; `Validate` accepts it and still rejects unknown
  placeholders.
- Defaults test matches the updated shipped prompt.
- A loop-level test asserts the rendered system prompt contains the scratch path.

**Acceptance:** `go test ./internal/prompt/... ./internal/config/... ./internal/agent/...`

**Commit:** `feat(prompt): {{scratch}} placeholder + scratch guidance in the default prompt`

## 5. Tracked-file mutation warning around terminal calls

**What:** Always-on structural floor (every mode, including Bypass — ADR 0006 class),
new file `internal/agent/treesnapshot.go`: when the workspace root is a git repository
(probed once per Agent construction via `git rev-parse --is-inside-work-tree`, cached),
snapshot `git status --porcelain` immediately before and after each `Subprocess()` tool
call in `dispatch.go`. If the two snapshots differ, append to the tool result content:
`[warning: this command changed workspace files: <paths>]` — listing changed tracked
paths and newly appeared untracked paths, capped at 10 with `… and N more`. Appended to
success **and** error results. Robustness, binding: each `git status` runs with a 2s
timeout and in the workspace root; on any git error or timeout the check is skipped
silently for that call — the floor must never break or slow a tool call's success path
beyond the two porcelain runs. Not a Mechanism; no gating, no config key.

**Files:** `internal/agent/treesnapshot.go`, `internal/agent/treesnapshot_test.go`,
`internal/agent/dispatch.go`

**Tests:**
- In a temp git repo: a subprocess call that overwrites a tracked file gets the warning
  naming the path; one that creates an untracked file gets it too; a no-op call gets no
  warning; a non-git workspace runs the call with no snapshot and no warning.
- Cap: >10 changed paths renders 10 + the `and N more` tail.

**Acceptance:** `go test ./internal/agent/...`

**Commit:** `feat(agent): warn in tool results when a subprocess changes workspace files`

## 6. Escape-probe battery: chained-script clobber probe

Depends on item 1.

**What:** Add one subtest to `Probe` in
`internal/platform/confinetest/confinetest.go` reproducing the incident shape under the
real backends: with cwd = the workspace box root and the exported fail-fast preamble
from item 1 prepended (exactly as the terminal tool would), run
`mkdir -p <outside>/srtest && cd <outside>/srtest && cat > SConstruct <<'EOF' … EOF`
followed by an unguarded relative write `echo clobbered > inside.txt`. Assert: non-zero
exit AND the workspace file `inside.txt` was **not** created. This extends the battery's
"non-zero exit and file absent" contract (§6.2) to multi-command scripts, which the
current single-line probes do not cover.

**Files:** `internal/platform/confinetest/confinetest.go`

**Tests:** the probe is the test; it runs under the existing Linux/macOS/Windows battery
drivers and skip-guards.

**Acceptance:** `go test ./internal/platform/...` (probe subtests skip loudly where the
backend lacks `FSWrite`, as today)

**Commit:** `test(confine): chained-script clobber probe in the escape battery`

## 7. Docs: ADR, contract, CONTEXT.md, incident closeout

Depends on items 1–6 (documents what they shipped).

**What:**
- New `docs/adr/0056-terminal-fail-fast-and-session-scratch.md` (0056 = next free)
  recording the four ratified calls: fail-fast preamble semantics, the scratch root and
  its GC, the mutation warning as a structural floor, and denial labeling. Extends the
  ADR 0012 posture (EPERM-on-escape stands; the escape now also aborts the script);
  supersedes nothing.
- `docs/design/confinement-execution-contract.md`: writable set is now
  workspace ∪ scratch ∪ `/dev/null`; battery §6.2 gains the chained-script row.
- `CONTEXT.md`: short "Scratch dir" entry in the domain language.
- `git mv "docs/handoffs/apogee-workspace-clobber-incident.md" docs/handoffs/archived/`
  — the incident is closed by this plan.

**Files:** `docs/adr/0056-terminal-fail-fast-and-session-scratch.md`,
`docs/design/confinement-execution-contract.md`, `CONTEXT.md`,
`docs/handoffs/apogee-workspace-clobber-incident.md` (moved)

**Tests:** none (docs-only).

**Acceptance:** `test -f docs/adr/0056-terminal-fail-fast-and-session-scratch.md && test -f "docs/handoffs/archived/apogee-workspace-clobber-incident.md" && ! test -f "docs/handoffs/apogee-workspace-clobber-incident.md"`

**Commit:** `docs(confine): ADR 0056 fail-fast + scratch dir; archive clobber incident`

---

**Suggested version bump:** minor (next 0.x feature release) — user-visible behavior
changes (fail-fast terminal scripts, new scratch root, new prompt placeholder, mutation
warnings). Not performed by this plan; the owner decides.
