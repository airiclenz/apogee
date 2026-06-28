# Phase-3 Orchestration Brief — resume the sub-agent loop (P3.5 → v1.0.0)

**Date:** 2026-06-24. **Read THIS file only.** Do NOT read the full
`docs/plans/phase-3-detail-plan.md` into your (orchestrator) context — the
sub-agents read their own plan section. This brief is the pre-digested state so
the orchestrator stays context-frugal.

## Your role (orchestrator)
Drive the remaining Phase-3 sub-phases **sequentially**, one **fresh** sub-agent
each, committing after each. Keep YOUR context minimal. Per turn: pick the next
item → spawn ONE sub-agent (template below) → on return, run the **minimal
trouble-check** → proceed. Autonomous; **stop only on trouble or at a checkpoint**.

## State — DONE (do not redo)
P3.0, P3.1 (design) [pre-session] · P3.2 landlock `30bc14f` · P3.3 seatbelt
`55a509a` · P3.6 guardrails `97fe131` · P3.4 mode-ladder+dispatch `d22aa32` ·
review-fix `4cf6dc2`. The **confinement pillar + its `/code-review` checkpoint
are complete.** All on `main`, unpushed.

## Remaining order (sequential; deps already verified — numeric-after-swap is valid)
0. **Hardening pass** — in-pillar `/code-review` findings (see *Carried findings → Hardening*)
1. P3.5  processing parity
2. P3.7  file-editing tools (find-replace/diff/patch/open-file; +marker on the write family)
3. P3.8  execution tools (terminal, python-exec) — first `Confiner` consumers
4. P3.9  git tool
5. P3.10 diagnostics tool
6. P3.11 network + host tools (web-fetch/http-request/web-search + ask-user `Asker`)
7. P3.13 sub-agent orchestrator + ADR 0013
8. P3.14 TUI `Depth>0` render
9. P3.15 MCP client (go-sdk v1.6.1)
10. **`/security-review` checkpoint** (guardrails + confinement + network/MCP)
11. P3.16 acceptance + cut `v1.0.0`

(P3.12 is folded into P3.6 — **skip it**.)

## Loop rules (decided with the owner)
- Each sub-agent: fresh context; applies **`/feature-implementation` + `/coding-standards` (go)**;
  implements **exactly one** item; updates docs (the `#### ✅ P3.x result` note in the plan +
  TODO/CONTEXT/technical-design + any ADR the task names); runs the §7 gate; **commits to `main`
  only if green** (NO push). Halts+reports if red/unverifiable.
- **You do NOT re-run the full gate.** After each return, ONE compact check:
  `git log --oneline -1 && go build ./... && go vet ./... 2>&1 | tail -3`.
  Commit missing, or build/vet broken → **STOP and report**. The full gate is the sub-agent's job.
- **Checkpoints** (`/security-review` before P3.16): instruct the review to **write its consolidated
  report to a file** and return only a 1-line summary + the path; then spawn a fix sub-agent against
  that file. Do NOT pull full specialist reports into your context.
- **Commit trailers** (every sub-agent commit):
  ```
  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01ANhZLxQC9biB4WA4QXsRBU
  ```

## Env caveats (pass to every confinement-touching agent)
- This dev-host kernel has `CONFIG_SECURITY_LANDLOCK` **OFF** → live landlock enforcement can't run
  here; the escape-probe batteries **self-skip loudly**. Test disposition/logic with **fake Confiners**
  (caps injected). Real Linux confinement proof = landlock-enabled CI / owner-run.
- **No macOS host** → seatbelt live probe + the P3.16 macOS run are **owner-run**. Hermetic
  profile-string tests run everywhere.

## Key code facts (so sub-agents don't re-derive)
- `domain.Confiner.Confine(ctx, box, *exec.Cmd) error` (closure form deleted in P3.4).
- `workspaceScopedWriter` marker: **unexported** in `internal/tools` (`IsWorkspaceScopedWriter` /
  `WorkspaceWriteTarget`); `write_file` carries it; **P3.7 adds the find-replace/patch family**.
- `domain.SubprocessTool` marker = the confinable class; such tools read `ConfinementFromContext`
  and **build+run their own `*exec.Cmd`** (P3.8 is the first consumer).
- Modes `ModePlan/ModeAskBefore/ModeAllowEdits/ModeAuto`; `--mode` flag. `confine-to-workspace`
  = **global-config-only** key (default `true`). `AutoEligible()` = **FSWrite-only**.
- Disposition: `internal/agent/disposition.go` + `dispatch.go`. `security.Guards`
  (path/url/dangerous/breaker/audit) threaded via `dispatch.go`; `PreExecute` runs **first / tighten-only**.
- Per-OS Confiner selector: `internal/platform/confiner_{linux,darwin,other}.go`. `__confined-exec`
  sentinel dispatched **before Cobra** in `cmd/apogee/main.go`.

## Carried findings (address in the named phase — from the pillar `/code-review`)
**Hardening pass (do FIRST, before P3.5):**
- **[High]** `MergeDangerousRules` (`security/rules.go`): a *project* add can replace-by-ID and
  **dissolve a Tier-1 floor rule** → make project-adds **tighten-only** (reject same-ID, or accept
  only a strictly-stricter tier). Add a test.
- **[Med]** Network-deny silently ignored on landlock ABI<4 (`landlock_linux.go` ~210): when
  `NetworkAllow` is set but the kernel can't enforce, **fail closed or surface** (don't run net-open silently).
- **[Med]** `AuditLog.records` unbounded (`security/audit.go`): cap (ring + dropped-count).
- **[Med]** Delete duplicated `internal/tools/path_safety_test.go` + the orphaned `evalRealPath` alias.
- **[Med]** Retire the now-stale `confinetest` local `Confiner` interface → use `domain.Confiner`;
  drop the "until P3.4" comments.
- **[Med]** Remove dead `PreCheck.Decision` field (`security/guard.go`).
- **[Med]** Add cheap hermetic tests: nil-Confiner Auto → `ErrAutoUnavailable`; present-but-incapable
  Confiner Auto → constructs; `ApplyLandlockAndExec` empty-argv refusal; marker accessors → false for a non-marker tool.

**P3.8:** pair `Setpgid` with `cmd.Cancel` (negative-PID kill) + `WaitDelay` (else orphaned process
groups on cancel); wire `errors.Is(err, ErrConfinementUnavailable)` → **demote to Approval** in dispatch
(the "confine-if-you-can, gate-if-you-can't" runtime net currently has no landing site).

**P3.11:** give `URLGuard` a **default-on SSRF floor** — deny loopback / IMDS `169.254.169.254` /
private ranges by **resolved IP**, not hostname strings.

**P3.13:** `security.Guards` copy **aliases** live breaker/audit state — decide share-vs-isolate for
sub-agents; add `Guards.ForSubAgent()` (fresh breaker/audit, shared read-only Dangerous ruleset) if
isolation is wanted, else fix the misleading "no live state" comment.

**Owner/CI (not a phase):** canonicalize box roots via `EvalRealPath` (macOS `/tmp`→`/private/tmp`);
confirm enforcement on a landlock-enabled kernel + macOS.

## Sub-agent prompt template (fill {PHASE}, {SCOPE}, {NOTES})
```
You are implementing exactly one Apogee sub-phase: {PHASE} — {SCOPE}. Fresh context.
Repo /workspace/repos/apogee (Go 1.26, module github.com/airiclenz/apogee), branch main,
pre-production: commit direct to main, NO branch/PR, NO push.

Read first (only what you need):
- Your task section "{PHASE}" in docs/plans/phase-3-detail-plan.md (+ that file's relevant §3 D-row
  and §7 gate). Skim the prior "✅ P3.x result" notes for handoffs.
- docs/handoffs/2026-06-24 - 03 - phase-3-orchestration-brief.md → its "Key code facts",
  "Env caveats", and "Carried findings → {PHASE}" sections — address any finding listed for {PHASE}.
- If confinement-related: docs/adr/0012-...flag.md + docs/design/confinement-execution-contract.md.

Method: apply /feature-implementation (/root/.claude/skills/feature-implementation/SKILL.md) and
/coding-standards go (/root/.claude/skills/coding-standards/references/coding-standards.go.md +
testing.go.md). Strict scope — only {PHASE}.
{NOTES}

Verify gate (ALL green before commit): gofmt -l . (empty) · go vet ./... · GOOS=darwin GOARCH=arm64
go vet ./... · go build ./... · go test -race ./... · grep -rl '"github.com/airiclenz/apogee"'
internal/ (empty) · 6 cross-builds (linux/darwin/windows × amd64/arm64, CGO_ENABLED=0) · go mod tidy
(no drift) · ./apogee --help. Landlock-enforcement probes self-skip on this kernel — expected, not a
failure; test logic with fake Confiners.

Docs: append "#### ✅ {PHASE} result" to your plan section (match prior style); update
TODO/CONTEXT/technical-design as relevant; author the ADR if your task names one. (No CHANGELOG here.)

Commit (only if gate green): conventional message + the two trailers from the brief. NO push.
If red/unverifiable: STOP, report, do not weaken tests.

Report back TERSELY (≤8 lines — this is all the orchestrator needs):
1) COMMITTED/HALTED  2) commit hash + subject  3) gate: one line (note any self-skips)
4) deferred/unverifiable: one line  5) downstream risks: ≤2 bullets.
Do NOT list every file or paste decisions — those live in the commit body + the result note.
```
