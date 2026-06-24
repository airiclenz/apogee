# Handoff 01 (2026-06-24) — Phase-3 plan grilled: confinement reframed to blast-radius + a 4-mode ladder; ADR 0004 reopened

**Date:** 2026-06-24 · **Branch:** `main` (commit directly — pre-production owner directive) ·
**Type:** design/planning only — **no code changed**, docs only (so the verify gate is untouched;
`make check` not re-run because nothing under `internal/`/`cmd/` moved).

## What this session was

A `grill-with-docs` pass over the **Phase-3 detail plan**
([`docs/plans/phase-3-detail-plan.md`](../plans/phase-3-detail-plan.md)), challenging it against the
glossary ([`CONTEXT.md`](../../CONTEXT.md)) and the load-bearing ADRs (0002/0004/0005/0008/0010) and
cross-checking the real code at the seams (`internal/agent/dispatch.go`, `internal/domain/{tools,config,confinement}.go`,
`internal/platform/`). Eight design calls were resolved and **written into the docs as they landed**;
two were **reopened** and deferred to next session at the owner's request.

**Do not re-derive these — read the updated docs.** All decisions are captured in:
- [`CONTEXT.md`](../../CONTEXT.md) — **Agent mode** (now a 4-rung ladder) and **Confinement** (now
  blast-radius framed) entries rewritten.
- [`docs/plans/phase-3-detail-plan.md`](../plans/phase-3-detail-plan.md) — §3 **D1** (rewritten), **D5**
  (rewritten), **D2** (sub-agent), **D7** (freeze surface); tasks **P3.1–P3.4, P3.7, P3.11, P3.13**;
  §1 exit #3; §7 deliverable; and the new **§5 "⚠️ Reopened 2026-06-24"** block.
- Memory: [`phase3-mode-ladder-and-auto-reopen.md`](../../../.claude/projects/-workspace-repos-apogee/memory/phase3-mode-ladder-and-auto-reopen.md)
  (one-paragraph recap + the two reopened items).

## The decisions that landed (resolved)

Short pointers — full text in the docs above:

1. **`ExternalEffects.Do` boundary is declared but UNWIRED** in `dispatch.go` (`executeTool` calls
   `tool.Execute` directly). The bench-stub story (P3.11/P3.15/P3.16) depends on it → **folded into P3.4**.
2. **Confinement reframed to BLAST RADIUS** (not "Auto ⇒ confine everything"). OS confinement attaches
   to the unbounded **subprocess/shell/network** surface (single, all-OS subprocess wrap: Linux landlock
   pre-`execve`, macOS `sandbox-exec`). Apogee's **own** in-process writes are **path-safety-bounded** at
   every rung. **This deleted** the `LockOSThread` thread-discard trick, the goroutine-escape contract,
   and the macOS in-process asymmetry — P3.2/P3.3 are now much simpler.
3. **Third-party in-process tools + unconfinable external (arbitrary-URL fetch, MCP) Approval-gate even
   in Auto.** "Workspace-scoped writer" is an unexported marker (`workspaceScopedWriter`) only Apogee's
   own tools carry.
4. **4-mode ladder** (was 3): **Plan → Ask-Before → Allow-Edits → Auto** (adds `ModeAllowEdits`).
   **Allow-Edits** = auto-approve Apogee's workspace-scoped edits, gate shell/network/MCP/out-of-workspace;
   **needs no confinement, identical on every OS** — the practical daily default.
5. **`sub_agent` tool is dispatch-transparent** — never `Confine`-wrapped/gated as a unit; its child
   calls each get the per-call disposition with the parent's threaded `Confiner`/`Approver`/mode.
6. **Sub-agent execution is atomic within the parent Turn** — no mid-sub-agent snapshot; cancel rolls
   back the whole parent Turn (parent resumable from the pre-`sub_agent` boundary).
7. **`ask-user` → new `Asker` `Config` delegate**, **struct-typed** (`AskRequest`/`AskAnswer`) for
   v1-freeze safety; ReadOnly, mode-independent, never through Approval, blocks via the C-seam.
8. Dependency fix: **P3.7 now also depends on P3.4** (its acceptance tests the ladder disposition).

## ⚠️ MUST resolve FIRST next session (before ADR 0012/0004 / any P3.1 code)

Both are in **plan §5 "Reopened 2026-06-24"** with full option space. They change what P3.1–P3.6 build:

1. **Auto-mode confinement strictness — reopens accepted ADR 0004.** Owner wants **Auto to allow
   *everything* out of the box**, security being the user's responsibility (run in a VM/container);
   a network-default-deny Auto is "useless." This is apogee-code's original posture / Claude Code's
   `--dangerously-skip-permissions`. Options: (a) confined but **network-allow-by-default**; (b) explicit
   **unconfined "YOLO" rung** (refuses unless "I am the sandbox" ack); (c) **configurable** strictness.
   Grill lean: (a)+(b). **The blast-radius rewrite currently assumes confined-autonomous / network-deny;
   if (b)/(c) wins, Auto's box defaults + `--mode` surface + CONTEXT.md need another pass and ADR 0004 is
   amended more deeply.**
2. **All-modes known-malicious denylist ("blacklist") — extends D6/P3.6.** Refuse `rm -rf /`, fork bombs,
   `curl … | bash`, `~/.ssh` writes, … in **every mode regardless of confinement** (arg-guard generalised
   into a mode-independent floor; the complement that keeps even a permissive Auto safe-ish). Confirm
   scope + seed list, fold into P3.6.

## Then the normal Phase-3 order

Once the two reopened calls are settled: **P3.0** (entry re-verify + pins — note `apogee-code` TS oracle
**is** reachable at `/workspace/repos/apogee-code`) → **P3.1** writes **ADR 0012** (blast-radius model +
ladder) and **amends ADR 0004** → backends (P3.2/P3.3) → P3.4 (ladder + Confine + ExternalEffects wiring).
Task table + acceptance in the plan §4.

## Suggested skills

- **`/grill-me`** or **`grill-with-docs`** — to settle the two reopened calls at session start (they're
  exactly the "one wrong call cascades" kind).
- **`/coding-standards`** (`go`) — mandatory for every Phase-3 Go body once code starts.
- **`/code-review`** after the confinement pillar (P3.1–P3.4); **`/security-review`** before the freeze
  (the denylist + confinement + network/MCP surface is its target).
- **`manage-llm-server`** / llama-launcher MCP `http://192.168.64.1:7331/mcp` — load a tool-capable model
  for any live run.
- **`/handoff`** at session end; **`archive-handoffs`** — handoff `2026-06-23 - 18` (Phase-2→3 entry) and
  `2026-06-24 - 00` are now consumed by this doc + the updated plan.
