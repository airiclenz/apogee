# Apogee — Technical Design Document (TDD)

**Status:** 🌱 v0.1 — sparse scaffold, to be densified. This is the consolidated
technical design (the *as-designed system* in one place). It **synthesizes** the
authoritative records; it does not replace them. For *why* a decision was made, follow
the ADR link — this doc records *what* the design is and *what is still undesigned*.

**Date:** 2026-06-23  **Repo state:** **✅ Phase 1 COMPLETE — P1.0–P1.7 all landed.** The
embeddable agent core is built and the bench (apogee-sim) is re-armed on it. The ADR-0010
layout is realised (P1.0); the real provider client (P1.1), `processing/` parse (P1.3), the
minimal tool set (P1.4), the hook-mutation bodies (P1.5), and **P1.2 — the convergence — the
full Turn/Step state machine** (stream → parse → hooks → tool dispatch through Approval →
quiescent boundary; `Run`; the `ActionDefer` feed-forward surviving a snapshot) are in; **P1.6
finalised the concrete v1 Session schema** — the engine-state envelope
(`internal/agent/state.go`) serializes the conversation *and* the loop counters (`turnIndex`,
the in-Exchange flag, pending input), and per-message `Extra` wire fields (`reasoning_content`,
…) round-trip, so Resume *continues* an Exchange rather than re-zeroing it; and **P1.7 re-armed
the bench** — apogee-sim's `internal/coreagent` drives the real library through the public API
against an ephemeral workspace, `Step`s a file-edit task to completion, observes Events as Go
values, and scores the workspace (proven under `-race` by a hermetic OpenAI-compatible
`httptest` model — the same code path a live model takes; the live-model run at
`http://192.168.64.1:1111` is a `RunConfig.Endpoint` swap run from the host). **No
`panic("sketch")` remains on the public surface.**
Verify stays green: `go test -race ./...`, `gofmt`/`vet`/`build`, 6-target cross-build,
`apogee --help` exit 0. Detail + acceptance:
[`../plans/archived/phase-1-detail-plan.md`](../plans/archived/phase-1-detail-plan.md). **Next: run the live
file-edit eval against the local model, then Phase 2 (the TUI consuming the Phase-1 Events).**

**Purpose of this revision:** a `/handoff` to the next session. The job next session is
to **raise the density** of the thin sections (marked **⏳ DENSIFY** with a concrete
"what's needed" note) — not to re-open settled decisions. The **§8 backlog** is the
prioritized worklist.

### Reading order / source map
| Artifact | Role | Path |
|---|---|---|
| `CONTEXT.md` | Glossary — the domain language (authoritative for terms) | [`../../CONTEXT.md`](../../CONTEXT.md) |
| ADRs 0001–0010 | Point decisions + rationale (authoritative for *why*) | [`../adr/`](../adr/) |
| Implementation plan | Phased build sequence (authoritative for *order*) | [`../plans/implementation-plan-apogee-merge.md`](../plans/implementation-plan-apogee-merge.md) |
| `apogee.go` | Public API **signature sketch** (Phase-0 keystone; builds + vets, panic-stub bodies) | [`../../apogee.go`](../../apogee.go) |
| **This TDD** | Consolidated design + gap register | you are here |

---

## 1. Overview & scope

Apogee is a single cross-platform Go binary: a terminal coding agent for **small local
LLMs (~4B–35B)** that owns the full agentic loop (build request → call Upstream → parse →
dispatch tools → apply Mechanisms) and runs gated, self-regulating **Mechanisms** inside
that loop to keep small models on track. It merges two predecessors — **apogee-code** (a
TypeScript VS Code agent: the loop, ~30 tools, processing/parsers) and **apogee-sim**
(Go: the small-model Mechanisms + the eval/simulation bench). The proxy and plugins are
retired; Apogee *is* the integration now.

**The hard constraint** (inherited, unchanged): Apogee's Mechanisms must never make the
model perform worse than the same agent with Mechanisms off. The referent floor is
**Bypass mode** (Mechanisms off, structure on — *not* a naked model), proved at bench time
as a non-inferiority gate ([ADR 0009](../adr/0009-the-ab-decision-rule.md)).

**Goals:** one static binary; library-first embeddable core; the bench drives the *real*
loop in-process; every Mechanism A/B-validated, never carried on faith; cross-platform
(POSIX v1, Windows fast-follow).

**Non-goals (v1):** no proxy / wire contract to external clients; no in-binary bench
subcommand; no fork API in the product; no record/replay (stub external effects);
external-dependent task validation out of scope.

---

## 2. What we already have

### 2.1 Decision corpus (complete & accepted)
| ADR | Decision (one line) |
|---|---|
| [0001](../adr/0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md) | The loop is an embeddable library; the bench imports it as a Go module and drives it in-process. Apogee exposes snapshot/resume + hygiene; the bench *composes* forking. |
| [0002](../adr/0002-tools-are-an-open-extension-point-mechanisms-are-curated.md) | Tools are an open public extension point; the Mechanism catalogue is curated. |
| [0003](../adr/0003-mechanisms-are-a-constraint-declared-registry-not-a-fixed-pipeline.md) | Mechanisms are a constraint-declared registry → deterministic total order (topo-sort + stable ID tiebreak). |
| [0004](../adr/0004-auto-mode-requires-os-level-confinement.md) | **Superseded by 0012.** (Was: Auto requires OS confinement of fs-write **and** network.) The capability matrix + the per-tool invariant (MCP gates) survive in 0012. |
| [0012](../adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md) | Confinement attaches to blast radius; Auto's network is **open by default**, `AutoEligible()` = fs-write only (Linux ≥5.13); global **`confine-to-workspace`** flag (`false` = unconfined VM opt-in); a **dangerous-action guard** floor (footgun-guard, *not* a security boundary). |
| [0005](../adr/0005-sub-agent-privileges-are-bounded-by-the-parent.md) | Sub-agent privileges ≤ parent (mode, guardrails, confiner, tool subset). |
| [0006](../adr/0006-bypass-mode-is-the-mechanisms-off-floor.md) | Bypass mode = honest Mechanisms-off floor = the bench's aggregate control arm. |
| [0007](../adr/0007-step-turn-and-the-quiescent-boundary.md) | Step/Turn + quiescent boundary; cancellation is a Phase-0 primitive; recover-at-extension-boundary. |
| [0008](../adr/0008-stateless-tools-and-non-forkable-external-effects.md) | Tools stateless across Turns; MCP/network non-forkable → disable-with-stub for v1. |
| [0009](../adr/0009-the-ab-decision-rule.md) | A/B decision rule: NI gate + superiority selection, A/A-calibrated δ, task-blocked, asymmetric MC. |
| [0010](../adr/0010-package-layout-domain-core-and-thin-root-facade.md) | Package layout: a domain core (`internal/domain`), the engine (`internal/agent`), a thin root alias facade; `internal/*` never imports root. Resolves §6 #7 + §6.1. |
| [0013](../adr/0013-the-sub-agent-orchestrator-is-the-recursion-point-with-isolated-live-guard-state.md) | Sub-agent = a nested `Agent` at a dispatch **recursion point** (`sub_agent`, no disposition marker; per-child disposition one level down); `≤ parent` threaded structurally (mode/approver/confiner/`Subset`); **`Guards.ForSubAgent()` isolates live breaker/audit, shares the dangerous floor read-only**; `Depth+1` event nesting; recursion bounded (`maxSubAgentDepth`); atomic-within-the-Turn. |

Plus `CONTEXT.md` (the glossary, with a retired-terms map) and the phased implementation
plan. **All four prior "open items" are resolved** (plan §6 #22–24).

### 2.2 Code
| Artifact | State |
|---|---|
| `apogee.go` | Public API facade. **Every public method now has a real body** — `New`/`Resume`/`Submit`/`Step`/`Run`/`Snapshot`/`DecodeSession`/`AddExperimental`/`Add` + registry, tools (P1.4), hook-mutation surface (P1.5); **`Run` (the last `panic` stub) landed with the full state machine (P1.2)**. No `panic("sketch")` remains on the public surface. Thin delegators to sibling files. |
| capstone bodies (P0.6) | `loop.go` + `conversation.go` + `registry.go` (package `apogee`) — single non-streaming Turn, JSON snapshot/resume, ordering-cycle detection, experimental pre-request hook + `MechanismFiredEvent`, ctx-cancel→`StatusCancelled`, recover-at-boundary→`ErrorEvent`. **12 tests pass under `-race`** (black-box `apogee_test` + white-box harness). |
| `internal/agent` (P0.6) | the provider seam (Decision C): `Responder` + root-type-free `Request`/`RawResponse`/`Message`. Imported one-way by the root facade; the real HTTP provider implements `Responder` in Phase 1. |
| skeleton (P0.2) | `go.mod` (`go 1.26`, no deps), `cmd/apogee` (stdlib `--help` stub), and `internal/{agent,provider,processing,tools,context,session,mcp,security,mechanisms,platform,tui}` (a `doc.go` per package). `doc.go`-only **except `internal/platform`** (P0.5) and **`internal/agent`** (P0.6 seam). |
| CI (P0.4) | `.github/workflows/ci.yml` — `check` (gofmt/vet/build/`test -race`) + `cross` (Win/Mac/Linux × amd64/arm64, CGO off). Verified green locally. |
| `internal/platform` (P0.5) | `Shell`/`Path` interfaces + `Host` aggregate (POSIX impl, Windows stub, `Current()` selector), and `denyConfiner` — the deny-all `Confiner` stub (`AutoEligible()==false`) behind `NewDenyConfiner()`. **First tests in the tree** (white-box table tests). **P3.2 adds `landlock_linux.go`** — the Linux landlock backend (`//go:build linux`, raw CGO-free `x/sys` syscalls): ABI probe, honest caps, `Confine(*exec.Cmd)` re-exec wrapper + `ApplyLandlockAndExec` in-child half; plus the shared `internal/platform/confinetest` escape-probe harness. **P3.3 adds `seatbelt.go`** (`//go:build !windows`, host-agnostic): the macOS seatbelt backend — `seatbeltProfile(box)` pure-fn `sandbox-exec` profile (deny-default file-write, allow under `WorkspaceRoot`/`WritablePaths`, network open by default), honest caps, `Confine(*exec.Cmd)` rewriting to `sandbox-exec -p <profile> …` — with `seatbelt_darwin.go` (`//go:build darwin`) holding only `NewSeatbeltConfiner()` + the `/usr/bin/sandbox-exec` presence probe; reuses `confinetest` (macOS live-probe owner/CI-deferred, self-skips loudly on non-darwin). windows keeps `denyConfiner` (Phase 5). |

The sketch covers: `Agent`/`Config`/lifecycle; `Step`/`Run`/`Submit`/`StepResult`;
sealed `Event` + 8 variants + `EventSink`; `Approver`; `Tool`/`ExternalEffectTool`/
`ToolRegistry`; the five hook interfaces + `Mechanism`/descriptor/`OrderingConstraints`/
`MechanismRegistry`/`PostResponseDecision`; `Confiner`/`ConfinementCaps`/`ConfinementBox`;
`Session` snapshot/resume; sentinel errors. See §4.

---

## 3. Architecture (target)

Proposed Go layout (from plan §3 — **provisional**, see gaps in §6/§8):

```
apogee/
├── apogee.go            # PUBLIC API facade (this is the keystone; sketch exists)
├── cmd/apogee/          # Cobra entrypoint: TUI + subcommands (run, probe, headless…)
├── internal/
│   ├── agent/{loop,subagent,modes}   # the loop; sub-agent (≤parent); Plan/Ask-Before/Allow-Edits/Auto
│   ├── provider/        # openai-compatible client, model discovery, server-process mgr
│   ├── processing/      # PORT-RISK: tool-call parsers, thinking/harmony channels
│   ├── tools/           # ~30-tool suite + registry/executor
│   ├── context/         # Budget, Compaction (generative, default), tool-result capping
│   ├── session/         # snapshot/resume (= bench snapshot/restore)
│   ├── mcp/             # MCP client (official go-sdk)
│   ├── security/        # Safety guardrails (audit, circuit-breaker, path/url safety, dangerous-action guard)
│   ├── mechanisms/      # constraint-declared registry (layout-by-hook is PROVISIONAL — §6.4)
│   └── platform/        # shell + path (POSIX/Windows) + Confiner BACKENDS (interface is public — §6.1)
└── go.mod               # github.com/airiclenz/apogee
```

**Key seams (decided):** (1) the **public Go API** — the contract the bench + embedders
depend on, must be embeddable/steppable with no ambient state; (2) **five Mechanism hook
points** in a constraint-declared registry; (3) the **platform abstraction** (shell/path +
Confiner). See ADR 0001/0003/0004.

**Dependency policy (plan §3a, decided):** single static binary; external programs
(ripgrep, formatters, linters, `git`) are runtime-detected optional enhancements that
degrade gracefully — never hard prerequisites for core function. One bounded exception:
Auto-mode Confinement (and on macOS, the system `sandbox-exec`). Go module graph kept lean
(Cobra, Bubble Tea/Lipgloss/Bubbles, MCP go-sdk, yaml.v3, small utils); stdlib-first.

---

## 4. Public API surface (from the sketch)

The shape is in [`apogee.go`](../../apogee.go). Summary:

| Concern | Surface | ADR |
|---|---|---|
| Construct / resume | `New(Config)`, `Resume(Config, Session)`, `Agent.Close()` | 0001 |
| Autonomy | `Mode` (Plan/Ask-Before/Allow-Edits/Auto), `Config.Bypass`, global `confine-to-workspace` | 0006, 0012 |
| Drive the loop | `Submit(UserInput)`, `Step(ctx) → StepResult`, `Run(ctx)`, `StepStatus` | 0007 |
| Turn-local context | `UserInput.FileRefs` (`@file`) + `UserInput.SkillIDs` (`/skill`) → resolved and prepended to the user message; `Config.Skills SkillResolver` (`ResolvedSkill`) resolves attached skill IDs (disk catalog stays in `internal/skills`) | 0001, 0010 |
| Observe | `EventSink.Emit(Event)`; sealed `Event` + variants (token, message, tool-call, tool-result, approval, mechanism-fired, error); `Depth` carries sub-agent nesting | 0001, 0005 |
| Approve | `Approver.Approve(ctx, ApprovalRequest) → ApprovalDecision` | 0004 |
| Ask | `Asker.Ask(ctx, AskRequest) → AskAnswer` — free-text Q&A host delegate for `ask_user` (P3.11), distinct from Approver; nil ⇒ `ask_user` not registered; struct-typed for freeze-safety | 0001 |
| Tools | `Tool`, `ExternalEffectTool`, `ReadOnlyTool`/`SubprocessTool`, `ToolCall`/`ToolResult`, `ToolRegistry` (`.Subset` for sub-agents) | 0002, 0005, 0008 |
| Mechanisms | 5 hook interfaces; `Mechanism` + `MechanismDescriptor` (`Capability`, `SuppressionPolicy`, incompatibilities) + `OrderingConstraints`; `MechanismRegistry` (`Add` / `AddExperimental`); `PostResponseDecision` | 0002, 0003, 0006 |
| Confinement | `Confiner` (interface **public**), `ConfinementCaps.AutoEligible()` (fs-write only), `ConfinementBox` | 0012 |
| Sessions | `Agent.Snapshot() → Session`, `Session.Encode`/`DecodeSession` (**no fork API**); v1 `State` = conversation + loop counters (P1.6) | 0001 |
| Errors | `ErrAutoUnavailable`, `ErrConfinementUnavailable` (P3.2 — confine-if-you-can/gate-if-you-can't net), `ErrOrderingCycle`, `ErrSessionVersion`, `ErrInputPending` | 0003, 0012 |

### 4.1 Design calls the sketch made (decided here; need ratifying into plan/ADRs)
1. **`Confiner` interface is public** (host injects it via `Config`); only backends stay
   `internal/platform`. **Corrects** plan §3 which filed all of `platform/` under internal.
2. **`EventSink` is push, not a channel** — streaming + Approval happen *inside* a `Step`
   (ADR 0007), so a push sink composes; TUI/bench adapt it.
3. **`Event` is a sealed interface** (unexported marker) — variant set stays Apogee-owned
   and additively versioned (ADR 0001 §consequences).
4. **`Config` is a struct, not functional options** — matches the ADRs' "injected via
   `Config`" language; every field a reviewable seam.
5. **Curated-vs-open is structural:** a `Mechanism` carries descriptor + ordering and
   *separately* implements a hook interface (registry type-asserts); a bench experimental
   hook is a bare hook interface (`AddExperimental`), no descriptor (ADR 0002).

---

## 5. Component design status

Spine of the TDD: each component, what's decided, what's undesigned. **D**=decided,
**S**=sketched (signatures only), **P**=partial real bodies (the P0.6 capstone path), **∅**=not started.

| Component | Status | Decided | Undesigned (→ §8) |
|---|---|---|---|
| Public API facade | S→**P** | shape, seams, naming (§4); hook mutation API (§6.2, designed P0.1); **capstone-path bodies real (P0.6); hook-mutation working-value bodies real (P1.5); P1.2: `Run` real + every hook point engine-wired — no `panic("sketch")` left on the public surface** | (none — public surface is body-complete for Phase 1) |
| Loop / Turn engine | S→**P (P1.2)** | Turn = one primary Upstream call; quiescent boundary; recover-at-boundary (0007); **P1.2: the full Step — stream → parse → post-response hooks → tool dispatch through Approval → post-tool-result → boundary, emitting typed Events; `Run` steps until the Exchange ends; streaming+Approval interleave (§6 #6) and event delivery (§6 #3) settled; ActionDefer feed-forward survives snapshot; cancel mid-stream + mid-tool roll back; tool/hook panics recover — all under `-race`; engine adopts `domain.Conversation`; P1.6: the snapshot envelope now serializes the loop counters (`turnIndex`, in-Exchange flag, pending input) so Resume continues** | inline thinking-strip wiring (needs a `ThinkingConfig` source — Phase 2/3); sub-agent nesting (Phase 3) |
| Provider / Upstream | S→**P (P1.1)** | openai-compatible; model discovery; TS as oracle; **P1.1: real `internal/provider.Client` — non-streaming `Respond` + streaming `Stream` (`iter.Seq[Delta]`), bounded retries/timeouts, `/v1/models` discovery, `ServerManager`; httptest-hermetic; replaces `Placeholder`. P1.2: the `Responder` seam is now streaming-only (`Stream`) — the loop consumes it; `Respond` stays a concrete `Client` method** | ollama/llama.cpp `/props` discovery + PID-file orphan adoption (deferred) |
| processing/ (parsers) | ∅→**P (P3.5)** | RISKIEST; TS oracle + ported test vectors *is* the gate (0024b); **P1.3: one format end-to-end — native/JSON tool-call parse (`ParseNativeToolCalls`→`domain.ToolCall`, args validated, empty→`{}`, malformed→`ErrMalformedToolCall` never panic) + inline thinking-channel strip (`StripThinking`/`IsThinking`; gemma `<think>`, gpt-oss harmony); ported thinking-stripper vectors are the gate; package depends only on `domain`. P1.2: the loop adapts `provider.ToolCall`→`NativeToolCall` and parses at the seam (a malformed call degrades to a parse-error path, not a Turn failure). P3.5: parity finished — the two text formats behind the `ToolCallParser` interface (`MarkdownFencedParser` fenced-block + marker-fallback; `CustomRegexParser` named-group regex with JS→Go group rewrite + `{raw}` non-JSON fallback), `NewToolCallParser` processor-factory selecting native/markdown-fenced/custom-regex (native = text no-op; unknown format errors), and `StripHarmony`/`IsHarmonyThinking` for the full gpt-oss channel set (analysis/commentary/final; `<\|end\|>`/`<\|call\|>`/`<\|return\|>` terminators). All ported apogee-code TS vectors pass; malformed input degrades to the no-call path, never a panic. **Parser-seam wiring: `ParserFor(domain.ModelProfile)` translates the `Config.Profile` (Model profile) onto the frozen `ToolCallingConfig`/`ThinkingConfig` and returns the text `ToolCallParser` + a unified `ContentStripper` (none/delimited/harmony); the loop selects both once in `newAgent` and applies them at the parse seam — strip inline thinking/harmony, then recover a fenced/custom-regex call from the stripped content when the native path found none (native wins; text call gets a deterministic `text_call_<turn>` ID). A native/zero profile selects no-op parsers, so the content path is byte-identical.** | live-stream channel suppression (the in-flight `TokenEvent` hold) is the following item; fenced marker overrides / probe auto-profiling are deferred (TODO.md) |
| Tools (~30) | S→**P (P1.4)** | open extension point; stateless-across-Turns; external-effect boundary; **P1.4: minimal local set — `read_file`/`write_file`/`list_dir`/pure-Go `grep` (`io/fs` walk + `regexp`, no external programs) in `internal/tools/`, each scoped to a sandbox root at construction with traversal-rejecting path-safety (symlink-aware); real `domain.ToolRegistry`; `NewDefaultRegistry(root)` seam; optional `ReadOnlyTool` interface (the Plan-mode/Approval signal). P1.2: dispatch/approval/executor wired — `Config.WorkspaceDir` resolves the default registry; Plan filters the menu to read-only; allow-for-session cached; tool panics → `ErrorEvent` + error result. **P3.4: the per-call gate is now the D5 blast-radius disposition (mode × tool-class × confine-to-workspace × caps); `write_file` carries the unexported `workspaceScopedWriter` marker; subprocess tools (P3.8) get a `domain.Confinement` context handle to confine the cmd they build; external-effect tools route through `ExternalEffects.Do`. P3.7: the file-editing family — `single_find_and_replace`/`multi_find_and_replace` (exact-once, multi atomic), `edit_existing_file` (full-replace or `*** Begin Patch` hunks), `view_diff` (pure-Go Myers LCS, no external), `open_file` (read + substring-locate); ported from the apogee-code oracle, stateless (ADR 0008), each path-safe; the three writers carry the `workspaceScopedWriter` marker (diff/open-file are `ReadOnly`). P3.8: the execution tools — `terminal` (command line via `platform.Shell`, `shlex`-validated, optional path-scoped `workdir`) and `python_exec` (detected `python3`/`python`, script on stdin, graceful when absent); both are `domain.SubprocessTool`s sharing `runSubprocess` which owns the §2.4 teardown (`Setpgid`+negative-PID-kill on cancel + `WaitDelay`, build-tagged unix/windows), the output cap, the timeout, and the `ConfinementFromContext` handoff; a runtime `ErrConfinementUnavailable` demotes the call to forced Approval in `dispatch.go` (re-run unconfined only on allow) — the "confine-if-you-can, gate-if-you-can't" RUNTIME net. P3.9: the git tools — `git_branch` (create/switch/list/delete; protected-branch delete block; safe `-d`), `git_commit` (path-safe staging + commit; amend refused on a published `origin/…` commit), `git_diff_range` (three-dot `base...head`; conservative ref-class validation; `ReadOnly`); ported from the apogee-code oracle, `domain.SubprocessTool`s on the shared `runSubprocess`, run with an allowlisted env (`safeEnvKeys`), detect the system `git` on PATH and degrade gracefully when absent (§3a, no hard dep). P3.10: the `diagnostics` tool — a read-only inspector that checks Go in-process (`go/parser` with `AllErrors` for syntax, dependency-free and toolchain-free) plus an optional `go vet` on the file's package (via the shared `runSubprocess`, allowlisted env, graceful "go vet skipped" note when no `go` on PATH), and returns a clear "no diagnostics available" for languages with no provider; `ReadOnly()` (runs in Plan) yet carries the `domain.SubprocessTool` marker (vet shells out) — read-only wins in the disposition, so it runs freely and is never confined/gated (same shape as `git_diff_range`); no new dependency (`go/parser`/`go/token` are stdlib). P3.11: the network + host tools — `web_fetch` (GET), `http_request` (method/headers/body, method arg-guard), `web_search` (a config'd `WebSearchEndpoint`; empty ⇒ the built-in **DuckDuckGo default**, `off` ⇒ graceful "disabled", never a crash) are in-process `net/http` `ExternalEffectTool`s of kind **network** (NOT `SubprocessTool`s, carry NO `workspaceScopedWriter`): the D5 disposition **auto-runs** them in Auto url-filtered and routes them through `ExternalEffects.Do` for the bench; each filters every URL through a `security.URLGuard` whose **default-on, resolved-IP SSRF floor** blocks loopback/private/IMDS/link-local **pre-flight AND at dial time** (`SafeDialControl` re-checks the connected IP — DNS-rebinding closed; no redirect auto-follow). `ask_user` routes a free-text question to the host's new `Asker` delegate — `ReadOnly()` (runs in Plan, mode-independent, never gated, NOT an `ExternalEffectTool`), registered **only** when an `Asker` is supplied (`NewDefaultRegistryWithHost` threads the `URLGuard`/endpoint/`Asker` from `Config`; the TUI implements `Asker` as an input-prompt rendezvous, the bench as a scripted responder); no new dependency (stdlib `net/http`). P3.15: external **MCP** server tools surface into the registry as `ExternalEffectTool{mcp}` (the `internal/mcp` client — see the MCP-client row); a discovered server tool is named `<server>__<tool>` and gates Approval in Auto via the existing `classMCP` disposition** | a user-tunable url-safety host allow/deny config key (deferred, TODO.md); ripgrep-optional |
| Context (Budget/Compaction/capping) | ∅ | four-way split; Compaction default generative; capping = surviving half of `compress` | Budget allocation algorithm; Compaction trigger/strategy; token counting |
| Sessions | S→**P (P1.6)** | snapshot/resume at quiescent boundary; copyable value; **P0.6: versioned JSON `Snapshot`/`Resume`/`DecodeSession`, future-version rejected; P1.2: `State` payload is `domain.Conversation` — full messages (tool calls + tool-call IDs) and the deferred-action queue round-trip; P1.6: concrete v1 schema finalised — the engine-state envelope (`internal/agent/state.go`) wraps the conversation with the loop counters (`turnIndex`, in-Exchange flag, pending input) so Resume continues at the right Turn, and per-message `Extra` wire fields (`reasoning_content`) round-trip via `Message` (un)marshal** | versioning/migration beyond reject (Phase 3+); the allow-for-session approval cache is deliberately not serialized (re-confirmed on resume) |
| Mechanisms + registry | S→**P (partial)** | constraint-declared; deterministic total order; descriptor; Bypass by Capability; **P0.6: cycle detection + experimental-hook slots real** | full topo-sort *order* (only cycle-check built); self-regulation (Adaptive Suppression, Turn Budget, Effectiveness tracking); catalogue→hook mapping (deferred session) |
| Security guardrails | ∅→**P (P3.6)** | Approval, path/url safety, dangerous-action guard, circuit-breaker, audit; **P3.6: `internal/security` filled (D6 / ADR 0012) — human-in-the-loop layer, all modes, distinct from the `Confiner`. Path-safety **consolidated** here (`ResolveInRoot`/`EvalRealPath`/`ErrPathEscape` — verbatim move; the 4 built-ins delegate via thin aliases, parity preserved); `URLGuard` (scheme/host allow-deny, deny-first) for P3.11; the **dangerous-action guard** (two tiers — Tier-1 hard-refuse / Tier-2 force-approval — default-on, precision-over-recall, whitespace-normalized literal/regex, no obfuscation-chasing) with config-merge (`MergeDangerousRules`: global add OR remove; **project add is tighten-only** — a same-ID project add replaces a default only if strictly stricter, so a repo cannot dissolve/loosen a Tier-1 floor rule); a circuit-breaker (trips on N identical failing calls); a **bounded** audit log (ring buffer, `DefaultAuditCap`, `Dropped()` count so overflow is observable). `security.Guards` bundles dangerous-action + breaker + audit and is wired through `internal/agent/dispatch.go` (`PreExecute` runs **first**, tighten-only, ahead of the mode disposition — **P3.4** replaced `needsApproval` with the D5 blast-radius disposition, and a Tier-2 force-approval upgrades any non-refuse disposition to a gate) so all tools — and sub-agents (D2) — inherit them. **Hardening pass (2026-06-24):** landlock **fails closed** when a box opts into network-deny but the kernel can't enforce it (ABI<4) — returns `ErrConfinementUnavailable` so dispatch gates, never runs network-open silently. **P3.11: `URLGuard` is wired** — the network tools (`web_fetch`/`http_request`/`web_search`) call `URLGuard.CheckContext` before each request, and the guard now carries a **default-on, tighten-only SSRF floor** (`ssrf.go`): loopback (127/8, ::1), cloud IMDS `169.254.169.254` + link-local (169.254/16, fe80::/10), RFC-1918 (10/8, 172.16/12, 192.168/16) + ULA (fc00::/7), unspecified, and IPv4-mapped forms are denied **by RESOLVED IP** (so `localhost` and a private-resolving DNS name are caught), with an injectable resolver for hermetic tests. **P3 security-review hardening (2026-06-24, SEC-01/02):** the floor's deny-list is **widened** to also reject RFC-6598 carrier-grade NAT (`100.64.0.0/10`, which `net.IP.IsPrivate()` does NOT cover), the whole `0.0.0.0/8` "this host/network" block (not just the exact unspecified address), the TEST-NET / benchmark documentation ranges (`192.0.2/24`, `198.51.100/24`, `203.0.113/24`, `198.18/15`), and the IPv4 **embedded in a NAT64 well-known-prefix** address (`64:ff9b::/96` — decoded and re-checked, since its `To4()` is nil and the v6 predicates wrongly read it as public) — explicit `net.IPNet.Contains` checks parsed once as package vars. The classifier (`ipBlockedByFloor`) is table-tested directly (`TestIPBlockedByFloor`) as the single bound both the pre-flight resolve and the dial-time control share. Note (SEC-02): a numeric-only IP encoding `net.ParseIP` does NOT decode (decimal/octal/hex `inet_aton` forms) is **not** normalized to an IP — it falls through to DNS, where the Go resolver does not numeric-decode it and the lookup fails, so the URL is blocked as unresolvable (the floor binds every form `net.ParseIP` decodes; the numeric-encoding safety rests on resolution failing). The floor is **never dissolvable by config** (config may ADD denials; `DisableIPFloor` is a code-only opt-out) and is re-checked **at dial time** via `URLGuard.SafeDialControl` (the connected IP is validated regardless of the pre-flight resolve — **DNS-rebinding TOCTOU closed**). **TOCTOU-safe workspace I/O (P3 SEC-05/H1):** the write tools no longer re-walk a check-time-validated path string at write time (a confined subprocess could swap an intermediate component to an outside-pointing symlink between check and use); `security.SafeWriteFile`/`SafeReadFile` (`safeio.go`) perform the operation through a Go 1.26 `os.Root` pinned at the workspace root, so an escaping symlink component is REFUSED at use time (the validated path and the opened path are the same fd-anchored object) — `write_file`/`edit_existing_file`/the find-replace family route through it. **`http_request` header filter (P3 SEC-04):** caller-supplied headers are tighten-only filtered — hop-by-hop / framing controls and a forged `Host` are rejected, the header count and per-value size are capped. **git ref guard (P3 SEC-06):** a model-supplied branch name / start-point / diff ref that begins with `-` (which git would read as an option flag) is rejected. **web_search key redaction (P3 SEC-03/M2):** a config'd search endpoint may carry an API key in its query; every model-facing `web_search` error now renders only the bare endpoint **host** and URL-scrubs any transport `*url.Error` (`endpointHost`/`scrubURLError` in `web_search.go`), so the key never reaches a model-facing or logged string — this was a LIVE leak on the transport-error path (`*url.Error` stringifies the full request URL), not merely latent. **Audit trail observable (P3 M1):** the bounded audit log is now surfaced on the `EventSink` as a new `domain.AuditEvent`, emitted from `dispatch.go` (`recordExecuted`/`recordBlocked`) wherever a record is appended, so the trail is inspectable by an observer/snapshot rather than living only in a volatile in-process ring; a sub-agent emits through the parent's `EventSink` at `Depth>0`, so a delegated call's audit record reaches the same observer (at its nesting depth) instead of vanishing with the discarded child.** | config.yaml surfacing of custom dangerous-rules + breaker threshold (deferred); a user-tunable url-safety host allow/deny config key (deferred, TODO.md) |
| Confinement | S (iface)→design done (P3.1)→P3.2 landlock + P3.3 seatbelt→**P3.4 dispatch-wired (Auto real)** | capability matrix; **ADR 0012**: blast-radius, Auto fs-only (`confine-to-workspace`-tunable, network open); **[`confinement-execution-contract.md`](confinement-execution-contract.md)** pins the `Confine(*exec.Cmd)` prepare-in-place signature, the `workspaceScopedWriter` marker, the per-call disposition table, capability honesty, the escape-probe harness. **P3.2** landlock + **P3.3** seatbelt backends (reuse `confinetest`). **P3.4: the interface adopts `Confine(ctx,box,*exec.Cmd)` (closure form deleted); `AutoEligible()`→`FSWrite`-only; the construction gate refuses only a NIL Confiner (a present-but-incapable one enters Auto and gates the subprocess surface — `ErrAutoUnavailable` now conditional); `needsApproval` is reworked into the D5 blast-radius disposition (`internal/agent/disposition.go` — `classifyTool`→{RO,WS-write,net,mcp,subproc,3p-write}, `dispose`→{run,confine,gate,refuse}); the `Confiner`+box thread to a subprocess tool via a `domain.Confinement` context handle (tool builds+runs the cmd, §2.2); `platform.NewConfiner()` selects the real backend per-OS behind build tags; `cmd/apogee` dispatches `__confined-exec` before Cobra; `ExternalEffects.Do` is routed for external-effect tools. **P3.8: the first consumers — `terminal`/`python_exec` build+run their own `*exec.Cmd`, take the `Confinement` handle, and own the §2.4 teardown (`Setpgid`+negative-PID-kill+`WaitDelay`); a runtime `ErrConfinementUnavailable` demotes to forced Approval in `dispatch.go` (the "confine-if-you-can, gate-if-you-can't" runtime net)** | toolchain-cache box construction (host concern, §7); macOS/landlock live enforcement (owner/CI) |
| Sub-agents | ∅→**P (P3.13)** | privileges ≤ parent; top-level-only stepping v1; events nest; **P3.13 (ADR 0013): the `sub_agent` recursion point (no disposition marker) drives a nested `Agent`; `newChildAgent` threads mode/approver/confiner/`confine-to-workspace` verbatim + a `registry.Subset` of the parent's tools (expansion impossible) + `Depth=parent+1`; `Guards.ForSubAgent()` isolates the live breaker/audit and shares the dangerous floor read-only (the carried `/code-review` finding — isolate); recursion bounded `maxSubAgentDepth=2` (tool withheld at the bound + defensive refusal); atomic-within-the-Turn (cancel rolls the parent Turn back, child panic recovers at the parent boundary, no snapshot lands mid-sub-agent)** | nested (suspendable) sub-agent stepping (later, snapshot-schema-additive) |
| MCP client | ∅→**P (P3.15)** | official go-sdk **v1.6.1** (direct dep, pure-Go ⇒ cross-builds green); stdio/SSE/streamable-http; gates Approval in Auto. **P3.15 (`internal/mcp`): `Connect(ctx,[]ServerConfig,URLGuard)→*Client` dials every configured server, lists its tools, surfaces each as an `ExternalEffectTool{mcp}` named `<server>__<tool>` (so the existing D5 `classMCP` disposition gates it in Auto — NO dispatch change); `Tools()` registers them atop the default registry, `Close()` tears down (all-or-nothing Connect rollback, no orphan). Zero servers ⇒ dormant. SSE/streamable-http endpoints ride the url-safety SSRF floor; stdio is a trusted local launch. Untrusted server description/schema/result shown to the model, never executed. Resume reconnects FRESH (ADR 0008). `cmd/apogee` wires `config.yaml`'s `mcp-servers:` (file-only, default-empty) → `mcp.Connect`; client shape → [`mcp-client.md`](mcp-client.md)** | server-grain "allow for session" cache (post-v1); user-tunable url-safety host allow/deny (TODO.md) |
| Library | ∅ | cross-session per-model; confidence-tagged `ModelFingerprint`; inert under Bypass; longitudinal bench gate | store design; Bayesian confidence; fingerprint resolution; GGUF hash |
| Platform (shell/path) | ∅ | POSIX v1, Windows Phase 5; one interface | shell abstraction; path handling; Windows backend |
| TUI | ∅→**P (P2.x / P3.14)** | Bubble Tea; thin renderer over Events; supplies Approval delegate; **P3.14: sub-agent (`Depth > 0`) events now *render* (no longer just *tolerate*) — each nested block is framed by a `│ ` rail gutter per level and opened by a `⤷ sub-agent` label, derived purely from each event's `Depth` in the renderer (no Model state; ADR 0011 holds); the flat `Depth==0` transcript is byte-for-byte unchanged** | panes; live token gauge (Phase 4) |
| CLI / `headless` / `probe` | ∅ | Cobra; headless optional (NOT bench contract); probe doubles as fingerprint | command surface |

---

## 6. Notable open design questions (decide before/while densifying)

1. ✅ **Confiner package placement — RESOLVED ([ADR 0010](../adr/0010-package-layout-domain-core-and-thin-root-facade.md)).**
   The `Confiner` interface + `ConfinementCaps`/`ConfinementBox` move into `internal/domain`;
   the root re-exports them via type aliases (so the interface stays *public* — the host
   injects it via `Config` — while its definition sits where both the loop and the backends
   see it without an upward import). **No** public `apogee/platform` subpackage; the single
   root facade stays the only public package. `internal/platform` imports `internal/domain`,
   not root.
2. ✅ **Hook mutation API — RESOLVED (designed P0.1, bodies P1.5, engine-integrated P1.2).**
   `Request`, `Response`, `Conversation` stay **opaque structs with unexported fields**, but
   hooks now mutate them through a real accessor/mutation surface scoped from apogee-sim's
   actual Transform/Injector signatures (`docs/design/hook-mutation-api.md`):
   `AppendToSystem`/`InjectContext`/`SetTools`/`SetMessageContent` on `Request`,
   `SetText`/`SetToolCallArguments` on `Response`, `DropRange`/`Insert`/`Replace`/`Append`/
   `Defer` on `Conversation`, each reading cross-Turn state via `LoopView`. The loop builds one
   `Request` from conversation state, runs pre-request hooks against it (mutations compose), and
   drains it onto the provider wire. **P1.2 wires the remaining four hook points:** post-response
   (`ActionRetry`/`ActionIntercept`/`ActionDefer`), pre-tool-exec, post-tool-result, and
   history-rewrite all fire in the loop now; the `ActionDefer` feed-forward drains on the next
   request and survives a snapshot end-to-end (the engine adopted `domain.Conversation` as its
   storage; P1.6 wrapped it in the Session envelope that also serializes the loop counters).
3. ✅ **Event delivery & backpressure — RESOLVED (P1.2; canonical record [ADR 0007 §Phase-1
   realisation](../adr/0007-step-turn-and-the-quiescent-boundary.md)).** The loop emits
   synchronously and in Turn order through `EventSink.Emit`; it neither buffers nor drops. The
   non-blocking contract is the host's to honour (the `EventSink` doc states it) — the bench
   consumes Events as Go values in order (reproducibility wants exactly that), and a buffered
   channel adapter with a drop policy for the Phase-2 TUI sits behind the same interface.
   Sub-agent fan-in (Depth > 0) is Phase 3; every Phase-1 Event is Depth 0.
4. **`mechanisms/` package-per-hook layout** statically encodes the hook point, in tension
   with ADR 0003's *constraint-declared* (hook = descriptor field, dynamic order). Plan
   already calls it "provisional." Lean toward a flat `internal/mechanisms` with hook-point
   as data. **Resolve when the catalogue→hook mapping session runs.**
5. ⏳ **`UserInput` turn-local context resolution — `@file` + `/skill` RESOLVED, budgeting
   still deferred (2026-06-26; handoffs `… - 00 - chat-mini-language-core.md` and
   `… - 01 - skills-system.md`).** The TUI parses `@file` tokens into `UserInput.FileRefs` and
   attaches `/skill` picks into `UserInput.SkillIDs`; the loop now resolves both at Turn start —
   `resolveFileRefs` reads each file within the workspace fence (`security.SafeReadFile`), and
   `resolveSkillRefs` maps each ID through `Config.Skills` (the `internal/skills` catalog) —
   prepending the blocks to the user message (order: skills → file refs → text), replacing the
   old `noteUnresolvedFileRefs`/`noteUnresolvedSkillIDs` "ignored" stubs. The **budgeting** half
   (token-aware trimming via the context-builder seam) remains deferred to that seam.
6. ✅ **Streaming + Approval interleave inside a Step — RESOLVED (P1.2; canonical record
   [ADR 0007 §Phase-1 realisation](../adr/0007-step-turn-and-the-quiescent-boundary.md)).** The
   stream is consumed to its terminal Delta and the SSE body closed **before** any tool call is
   dispatched; Approval is then consulted synchronously at a sub-step boundary, so a blocking
   `Approver` never holds an open Upstream connection. The EventSink sees, per Turn:
   `TokenEvent`s (live, as content arrives) → [stream ends] → `ToolCallEvent` → `ApprovalEvent`
   (around the blocking `Approve`) → `ToolResultEvent`, for each call. A cancel mid-stream
   surfaces as a terminal stream error the loop distinguishes from a real fault via `ctx.Err()`
   and rolls the Turn back to a serializable boundary.
7. ✅ **Facade ↔ `internal/agent` placement — RESOLVED ([ADR 0010](../adr/0010-package-layout-domain-core-and-thin-root-facade.md)).**
   Adopted a **domain-core / engine / thin-facade** layout with one hard rule: **`internal/*`
   never imports the root `apogee` package; dependencies flow down to `internal/domain`.** The
   public types/interfaces/enums/errors live in `internal/domain` (the ubiquitous language as
   Go); the engine (loop, Turn state machine, conversation, modes, sub-agents) lives in
   `internal/agent`; the root `apogee` package is a thin facade of type aliases + re-exported
   consts/errors + forwarding constructors. Chose this over (a) fat-root — which would force
   the tool + Mechanism catalogues *into* root to avoid the seeding cycle (a god-package) — and
   over a halfway `internal/core` that collapses into the same shape. **Realised by P1.0**
   (the first Phase-1 task; a pure move, verify stays green). See
   [`../plans/archived/phase-1-detail-plan.md`](../plans/archived/phase-1-detail-plan.md) §3.

**Process / scaffolding (Phase 0):**
- ✅ **Done (P0.2):** `go.mod` (`go 1.26`, no deps) + `cmd/apogee` + empty `internal/` skeleton; `apogee.go` compiles and `go vet`/`go vet -race` pass in-tree.
- ✅ **Done (P0.4):** CI — `.github/workflows/ci.yml` cross-compiles Win/Mac/Linux × amd64/arm64 and gates `gofmt`/`go vet`/`go build`/`go test -race`.
- ✅ **Done (P0.3):** dependency versions pinned-by-decision (Cobra, Charm v2 stack, MCP go-sdk v1.6.1, yaml.v3, shlex, ulid) in the detail plan §1 — added per-task, graph still empty.
- ✅ **Done (P0.3):** the **Phase-0 detail plan** — [`../plans/archived/phase-0-detail-plan.md`](../plans/archived/phase-0-detail-plan.md) (task-level breakdown, acceptance criteria).
- ✅ **Done (P0.5):** `internal/platform` seam — `Shell`/`Path` interfaces (real POSIX impl, Windows stub) + a deny-all `denyConfiner` stub (`AutoEligible()==false`) so New's Auto gate is testable before the real backends (Phase 3).
- No throwaway in-process harness proving construct→Step→snapshot→resume→register-hook yet (the **P0.6 capstone harness** — spec'd in the detail plan, awaiting build).
- Tests exist only in `internal/platform` (P0.5 table tests); the rest of the tree is untested until the **P0.6 capstone harness** — the first cross-cutting test (`testing.go.md`: table-driven + golden files).

**Design depth (this TDD's §5 ∅/S rows):** loop engine, provider, processing/, context
reducers, security guardrails, sub-agent orchestrator, MCP, Library, platform, TUI — all
undesigned beyond ADR-level decisions. The **hook mutation API** (§6.2) is the priority gap
in the *public* surface.

**Deferred dedicated sessions (prerequisites, already flagged):**
- **Hook-point catalogue mapping** — map apogee-sim's Mechanisms onto the 5 hooks, driven by real sim traces (prereq to Phase 4).
- **Confinement design** — seatbelt/landlock/AppContainer across the capability matrix (ADR 0004).

**Doc hygiene:**
- ✅ **Done (`ff2c3f6`):** the old `README.md:68` "bench is driven through Apogee's headless
  mode" wording — which contradicted ADR 0001 — is gone; the README now describes the bench
  as importing Apogee as a Go library and driving the real loop in-process. No fix outstanding.
- Ratify the five §4.1 sketch-decisions into the plan/ADRs (esp. public `Confiner`).

---

## 8. Densification backlog (next-session worklist, prioritized)

The handoff payload. Each item: raise a §5 row from ∅/S toward a real design, or close a §6/§7 gap.

**P0 — unblocks everything else**
1. ✅ **Hook mutation API** (§6.2) — **DONE (designed P0.1, bodies P1.5):** `Request`/`Response`/`Conversation`/`LoopView`/`ConversationView` accessors+mutators designed from apogee-sim's Transform/Injector signatures (`docs/design/hook-mutation-api.md`) and now implemented in `internal/domain` (panic stubs replaced). **Pre-request hook mutations flow into the Upstream request** (`buildRequest`→hooks→`toProviderRequest` in `loop.go`), closing the P0.6 gap. `Conversation` carries a deferred-action queue with JSON round-trip so an `ActionDefer` survives a snapshot; the loop integration of the post-response + history-rewrite hooks is P1.2.
2. ✅ **Stand up `go.mod` + minimal `internal/` stubs** — **DONE (P0.2):** module + `cmd/apogee` + empty `internal/` skeleton; `apogee.go` compiles, `go vet`/`go vet -race` pass in-tree.
3. ✅ **Phase-0 detail plan + CI** — **DONE (P0.3+P0.4, `c7d4f61`):** [`../plans/archived/phase-0-detail-plan.md`](../plans/archived/phase-0-detail-plan.md) (version pins, CI spec, acceptance-tested task list) + `.github/workflows/ci.yml`.
3a. ✅ **`platform/` seam** — **DONE (P0.5):** `internal/platform` `Shell`/`Path` interfaces (real POSIX, Windows stub) + deny-all `denyConfiner` (`AutoEligible()==false`); cross-matrix builds, table-tested (detail plan §3).
3b. ✅ **Capstone harness** — **DONE (P0.6):** four gate decisions confirmed (Charm v2, MCP verdict, the `Responder` seam, P0.6 scope); construct→Step→Snapshot→Resume→`AddExperimental` runs for real over the `internal/agent.Responder` seam — 12 tests under `-race`, 6-target cross-build, `apogee --help` exit 0 (detail plan §3 "as built"). **Phase 0 is complete.**

**P1 — deepen the core design**
4. ✅ **Loop/Turn engine state machine** — **DONE (P1.2):** the full Step runs stream (P1.1) → parse (P1.3) → post-response hooks → tool dispatch (P1.4) through Approval → post-tool-result → quiescent boundary, emitting typed Events; `Run` steps until the Exchange completes. All five hook points fire (pre-request P1.5 + the four wired here); the `ActionDefer` feed-forward drains on the next request and survives a snapshot end-to-end. Streaming+Approval interleave (§6 #6) and event delivery (§6 #3) settled (stream-then-gate; synchronous in-order emit). The engine adopts `domain.Conversation` (rich messages + deferred queue + JSON round-trip) as its storage; `Config.WorkspaceDir` + `tools.NewDefaultRegistry` wire the default tools. Cancellation (mid-stream + mid-tool → Turn rollback) and recover-at-boundary (tool/hook panic → `ErrorEvent`) intact under `-race`. **+11 test funcs** (`statemachine_test.go` + harness/hookmutation migrated to the streaming seam).
5. ✅ **Provider/Upstream client** — **DONE (P1.1):** `internal/provider.Client` (non-streaming `Respond` + streaming `Stream`, bounded retries/timeouts), `/v1/models` discovery, `ServerManager`; httptest-hermetic, replaces `Placeholder`. TS oracle ported (`openai-compatible-provider` / `model-discovery` / `server-process-manager`).
6. ✅ **processing/ — one tool-call format** — **DONE (P1.3):** native/JSON tool-call parse (`ParseNativeToolCalls`→`domain.ToolCall`; empty args→`{}`; malformed→`ErrMalformedToolCall`, never panic) + inline thinking-channel strip (`StripThinking`/`IsThinking`; gemma `<think>`, gpt-oss harmony `<|channel|>…<|end|>`). **Finding:** the bench (apogee-sim) and the deliverable run on native structured `tool_calls` (grammar-forced JSON when a server lacks support), so "the most common native/JSON tool-call shape" is literal; the provider already extracts the wire shape and keeps args verbatim, so processing parses args + strips thinking. Ported apogee-code thinking-stripper vectors are the parity gate; the package depends only on `domain` (loop adapts `provider.ToolCall`→`NativeToolCall` at the seam — ADR 0010). markdown-fenced/custom-regex + full harmony channels are Phase 3; loop wiring is P1.2.
7. ✅ **Session concrete schema + versioning** — **DONE (P1.6):** the engine-state envelope (`internal/agent/state.go`) is the v1 `State` schema — it wraps `domain.Conversation` (messages with tool-call/result pairing + the deferred-action queue) with the loop's full quiescent-boundary counters: `turnIndex` (so Resume *continues* the Exchange rather than re-zeroing — the documented P0.6 gap), the in-Exchange flag (a resumed Agent rejects a mid-Exchange `Submit`), and pending input (a `Submit`→`Snapshot`→`Resume` keeps the queued message). Per-message `Extra` wire fields round-trip via `Message`'s own (un)marshal — unknown siblings (`reasoning_content`, …) are flattened at the top level and collected back on decode; the loop records the model's reasoning channel on the committed assistant message. `Session.Version` future-version rejection kept. The allow-for-session approval cache is deliberately **not** serialized (re-confirmed on resume — the safer human-in-the-loop default). **+7 test funcs** (`state_test.go`, `session_test.go`, `Message` round-trip).
8. Context reducers: Budget allocation, Compaction trigger/strategy, tool-result capping, token counting.

**P2 — subsystems & validation**
9. Self-regulation design (Adaptive Suppression, Turn Budget, Effectiveness tracking) + deterministic topo-sort/cycle detection.
10. Security guardrails designs; sub-agent orchestrator (privilege threading); MCP client; Library (fingerprint resolution, Bayesian confidence, GGUF hash).
11. Platform shell/path abstraction; TUI model/update/view; CLI surface.

**Housekeeping (cheap, do alongside):**
12. ✅ §6.1 (Confiner placement) + §6 #7 (facade↔engine layout) **resolved** ([ADR 0010](../adr/0010-package-layout-domain-core-and-thin-root-facade.md)); §4.1 #1 (public `Confiner`) ratified there too. **Still open:** §6.4 (mechanisms package-per-hook layout — Phase-4 catalogue-mapping session). *(`README.md:68` fix already done — `ff2c3f6`.)*

### Suggested next-session entry point
**✅ Phase 1 is COMPLETE — P1.0–P1.7 all landed.** The ADR-0010 layout is realised (P1.0),
the real provider client is built (P1.1), `processing/` parses one tool-call format (P1.3), the
minimal tool set + registry are built (P1.4), the hook-mutation bodies are real (P1.5), **P1.2 —
the convergence — landed the full Turn/Step state machine** (a Step streams the Upstream reply
emitting `TokenEvent`s, parses tool calls, runs the post-response/pre-tool-exec/post-tool-result/
history-rewrite hooks, dispatches tools through Approval, and returns at a quiescent boundary;
`Run` steps until the Exchange ends), **P1.6 finalised the concrete v1 Session schema** (the
engine-state envelope `internal/agent/state.go` serializes `turnIndex`, the in-Exchange flag, and
pending input alongside `domain.Conversation`, and per-message `Extra` wire fields round-trip, so
Resume *continues* an Exchange), and **P1.7 re-armed the bench** (apogee-sim's
`internal/coreagent` drives the real library through the public API and scores a file-edit task,
proven under `-race` by a hermetic OpenAI-compatible `httptest` model). The `Responder` seam is
streaming-only; §6 #6 (stream-then-gate) and §6 #3 (synchronous in-order emit) are settled.
The latest state lives in the handoffs.
**Two immediate next actions:** (1) the **live-model eval** — point
`coreagent.RunConfig.Endpoint` at the local server `http://192.168.64.1:1111` (MCP control
`http://192.168.64.1:7331/mcp`) and run the file-edit task against a real model from the host
(the build container does not route there); (2) **Phase 2 — the TUI**, a consumer of the
Phase-1 Events. The only throwaway P0.6 internal still standing is the cycle-check-only Mechanism
registry (Phase 4 replaces it); the minimal `conversation` is gone (P1.2 adopted
`domain.Conversation`, P1.6 wrapped it in the Session envelope).

---

## 9. Conventions
- **`/coding-standards` is mandatory for all new Go** (`coding-standards.go.md` +
  `testing.go.md`), every phase — a gate on every PR (plan Standing Requirement 1). Where a
  standard fights the plan or official Go, the plan/official Go wins (e.g. `Config` struct
  over functional options; package names not forced into single words where it harms clarity).
- Terminology is **authoritative in `CONTEXT.md`** — use those terms exactly; avoid the
  retired proxy-era vocabulary.
