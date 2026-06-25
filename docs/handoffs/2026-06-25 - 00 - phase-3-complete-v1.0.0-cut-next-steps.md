# Handoff — Phase 3 complete, `v1.0.0` cut & pushed — next steps

**Date:** 2026-06-25. **Status: Phase 3 is DONE. `v1.0.0` is tagged and pushed to `origin`.**
This doc is the pickup point for whatever comes after v1.0.0. It deliberately does **not**
restate design rationale or per-finding detail — those live in the artifacts referenced below.

## Where things stand
- `main` → `eb0c27d`, pushed. Annotated tag **`v1.0.0`** → pushed, points at `eb0c27d`.
- Working tree clean; build + vet green; `go mod tidy` stable.
- The whole Phase-3 sub-agent loop ran to completion this session (orchestration brief:
  `docs/handoffs/2026-06-24 - 03 - phase-3-orchestration-brief.md`).

### What landed (reference, not a re-listing — see `git log` + the ✅ notes in the plan)
Hardening pass (7 carried `/code-review` findings) → P3.5 processing parity → P3.7 file-editing
tools → P3.8 execution tools (first `Confiner` consumers) → P3.9 git tool → P3.10 diagnostics →
P3.11 network + host tools (SSRF floor) → P3.13 sub-agent orchestrator (**ADR 0013**) → P3.14 TUI
`Depth>0` render → P3.15 MCP client (go-sdk v1.6.1) → `/security-review` checkpoint → P3.16
acceptance + release. Each is a `feat/fix/chore` commit on `main` with the session trailers; the
`#### ✅ P3.x result` notes in `docs/plans/phase-3-detail-plan.md` carry the decisions.

### Release artifacts
- `CHANGELOG.md` → `v1.0.0` section + **"Known post-release verification"** list.
- The v1 public surface (root `apogee` types) is now under semver — breaking changes are major;
  Events/hook points stay additive (minor).

## Security-review checkpoint (closed)
Report + per-finding fix-pass dispositions: `docs/handoffs/2026-06-24 - 04 - phase-3-security-review.md`.
Outcome: **0 Critical · 1 High · 3 Med · 4 Low** — all High+Med remediated with regression tests
(commits `13134f8`, `38eaca5`). Headline: **H1 symlink-swap TOCTOU** (workspace-fence escape) closed
via `os.Root`-pinned `SafeWriteFile`/`SafeReadFile` in `internal/security/safeio.go`, with the whole
write family routed through it. Two Lows deferred to `TODO.md` with rationale.

## ⚠️ Open items the NEXT session should own

### 1. Owner-run live-enforcement proofs (BLOCKING for a "verified" v1.0.0)
The dev host has `CONFIG_SECURITY_LANDLOCK` **off** and **no macOS**, so OS enforcement was proven
only hermetically + by cross-build. These four must run on real hardware (also in `CHANGELOG.md`
→ "Known post-release verification"):
1. **Live landlock-kernel enforcement** — Linux ≥5.13 with landlock on; run the escape-probe
   batteries in `internal/platform/confinetest` (they self-skip loudly when landlock is off) and
   confirm an out-of-box write is OS-denied.
2. **macOS seatbelt enforcement** — `sandbox-exec` profile actually denies an out-of-box write.
3. **Live Auto-confined deliverable run** — a real `--mode auto` task with `confine-to-workspace`
   on a landlock-enabled box; confirm the box includes toolchain cache/temp dirs (else confined
   `go`/`pip` fail — flagged repeatedly during the loop).
4. **`EvalRealPath` box-root canonicalization** — macOS `/tmp`→`/private/tmp`; confirm box roots
   are canonicalized so the fence isn't bypassed by the symlinked-tmp alias.

→ This is the natural content for a CI job on a landlock-enabled runner + a macOS runner. There is
no GitHub Actions workflow for these yet — consider adding one so the residuals close automatically.

### 2. Process deviation to be aware of (not blocking)
The orchestration brief told sub-agents **NOT to push**, but a sub-agent pushed mid-loop —
`origin/main` was already at the latest commit before the final push. No divergence/force-push;
local and remote stayed byte-identical. If you run more sub-agent loops, tighten the sandbox or the
prompt so agents can't push, or explicitly decide push-as-you-go is fine.

### 3. Deferred / known-thin areas surfaced during the loop (all non-blocking, post-v1 additive)
- **P3.5:** the loop adapt-seam hard-codes the native tool-calling path; the fenced/regex processor
  factory is built but unwired (needs a model-profile / `ToolCallingConfig` + `ThinkingConfig`
  source that doesn't exist in domain/config yet). Every shipped profile uses native tool calls.
- **P3.7:** "out-of-workspace *approved* Apogee write" is still the honest gate→error fallback;
  honouring an approved escape via `WorkspaceRoot ∪ box.WritablePaths` is the additive change the
  `workspaceWriteTarget` seam enables (needs the box threaded into in-process write tools).
- **P3.10:** diagnostics only has a Go provider; external linters (`tsc`, etc.) are a later additive
  slot behind the same read-only/graceful contract.
- **P3.13:** sub-agents share the parent's tool catalogue by default; a per-sub-agent catalogue is a
  noted later refinement. Guard state IS isolated (`Guards.ForSubAgent()`).
- **P3.15:** a configured-but-unreachable MCP server is **fatal at startup** (aborts) rather than
  degrade-and-warn — confirm that's the desired posture for flaky remote endpoints.
- **`TODO.md`** holds the remaining deferrals (tool×mode security matrix, url-safety allow/deny key,
  the two deferred Low security findings).

## Pointers (don't re-read into context unless you need them)
- Plan + ✅ result notes: `docs/plans/phase-3-detail-plan.md`
- Confinement policy: `docs/adr/0012-*.md`; mechanism: `docs/design/confinement-execution-contract.md`
- Sub-agent orchestration: `docs/adr/0013-*.md`
- Security review: `docs/handoffs/2026-06-24 - 04 - phase-3-security-review.md`
- Release notes + residuals: `CHANGELOG.md`
- Deferrals: `TODO.md`

## Suggested skills for the next session
- **`/verify` or `/run`** — once on a landlock-enabled / macOS host, drive a real Auto-confined run
  to close residual #3 (and confirm the toolchain-dir box construction).
- **`/code-review` (or `/code-review ultra`)** — if you start post-v1 feature work, review the diff
  before merge; the v1 surface is now under semver so breaking changes need a deliberate call.
- **`/security-review`** — re-run if you touch the network/MCP/confinement surface again.
- **`update-config` / settings hooks** — if you decide to harden the sub-agent loop against
  unintended pushes (process deviation #2).
- **`brew-release` / `pr-lifecycle`** — only when you cut the *next* version or move off
  commit-direct-to-main (Apogee is still pre-production: commit direct to `main`, no PRs).
