# Apogee — Technical Design Document (TDD)

**Status:** 🌱 v0.1 — sparse scaffold, to be densified. This is the consolidated
technical design (the *as-designed system* in one place). It **synthesizes** the
authoritative records; it does not replace them. For *why* a decision was made, follow
the ADR link — this doc records *what* the design is and *what is still undesigned*.

**Date:** 2026-06-23  **Repo state:** **Phase 0 complete; Phase 1's core is built —
P1.0–P1.6 are done, only P1.7 remains.** The ADR-0010 layout is realised (P1.0); the
real provider client (P1.1), `processing/` parse (P1.3), the minimal tool set (P1.4), the
hook-mutation bodies (P1.5), and **P1.2 — the convergence — the full Turn/Step state machine**
(stream → parse → hooks → tool dispatch through Approval → quiescent boundary; `Run`; the
`ActionDefer` feed-forward surviving a snapshot) are in; and **P1.6 finalised the concrete
v1 Session schema** — the engine-state envelope (`internal/agent/state.go`) serializes the
conversation *and* the loop counters (`turnIndex`, the in-Exchange flag, pending input), and
per-message `Extra` wire fields (`reasoning_content`, …) round-trip, so Resume *continues* an
Exchange rather than re-zeroing it. **No `panic("sketch")` remains on the public surface.**
Verify stays green: `go test -race ./...`, `gofmt`/`vet`/`build`, 6-target cross-build,
`apogee --help` exit 0. Detail + acceptance:
[`../plans/phase-1-detail-plan.md`](../plans/phase-1-detail-plan.md). **Next: P1.7 (point the
bench at the API) — the Phase-1 deliverable.**

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
| [0004](../adr/0004-auto-mode-requires-os-level-confinement.md) | Auto requires OS confinement, reported as a capability matrix; needs fs-write **and** network; per-tool invariant (MCP gates through Approval even in Auto). |
| [0005](../adr/0005-sub-agent-privileges-are-bounded-by-the-parent.md) | Sub-agent privileges ≤ parent (mode, guardrails, confiner, tool subset). |
| [0006](../adr/0006-bypass-mode-is-the-mechanisms-off-floor.md) | Bypass mode = honest Mechanisms-off floor = the bench's aggregate control arm. |
| [0007](../adr/0007-step-turn-and-the-quiescent-boundary.md) | Step/Turn + quiescent boundary; cancellation is a Phase-0 primitive; recover-at-extension-boundary. |
| [0008](../adr/0008-stateless-tools-and-non-forkable-external-effects.md) | Tools stateless across Turns; MCP/network non-forkable → disable-with-stub for v1. |
| [0009](../adr/0009-the-ab-decision-rule.md) | A/B decision rule: NI gate + superiority selection, A/A-calibrated δ, task-blocked, asymmetric MC. |
| [0010](../adr/0010-package-layout-domain-core-and-thin-root-facade.md) | Package layout: a domain core (`internal/domain`), the engine (`internal/agent`), a thin root alias facade; `internal/*` never imports root. Resolves §6 #7 + §6.1. |

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
| `internal/platform` (P0.5) | `Shell`/`Path` interfaces + `Host` aggregate (POSIX impl, Windows stub, `Current()` selector), and `denyConfiner` — the deny-all `Confiner` stub (`AutoEligible()==false`) behind `NewDenyConfiner()`. **First tests in the tree** (white-box table tests). |

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
│   ├── agent/{loop,subagent,modes}   # the loop; sub-agent (≤parent); Plan/Ask-Before/Auto
│   ├── provider/        # openai-compatible client, model discovery, server-process mgr
│   ├── processing/      # PORT-RISK: tool-call parsers, thinking/harmony channels
│   ├── tools/           # ~30-tool suite + registry/executor
│   ├── context/         # Budget, Compaction (generative, default), tool-result capping
│   ├── session/         # snapshot/resume (= bench snapshot/restore)
│   ├── mcp/             # MCP client (official go-sdk)
│   ├── security/        # Safety guardrails (approval, audit, circuit-breaker, path/url, arg-guard)
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
| Autonomy | `Mode` (Plan/Ask-Before/Auto), `Config.Bypass` | 0004, 0006 |
| Drive the loop | `Submit(UserInput)`, `Step(ctx) → StepResult`, `Run(ctx)`, `StepStatus` | 0007 |
| Observe | `EventSink.Emit(Event)`; sealed `Event` + variants (token, message, tool-call, tool-result, approval, mechanism-fired, error); `Depth` carries sub-agent nesting | 0001, 0005 |
| Approve | `Approver.Approve(ctx, ApprovalRequest) → ApprovalDecision` | 0004 |
| Tools | `Tool`, `ExternalEffectTool`, `ToolCall`/`ToolResult`, `ToolRegistry` (`.Subset` for sub-agents) | 0002, 0005, 0008 |
| Mechanisms | 5 hook interfaces; `Mechanism` + `MechanismDescriptor` (`Capability`, `SuppressionPolicy`, incompatibilities) + `OrderingConstraints`; `MechanismRegistry` (`Add` / `AddExperimental`); `PostResponseDecision` | 0002, 0003, 0006 |
| Confinement | `Confiner` (interface **public**), `ConfinementCaps.AutoEligible()`, `ConfinementBox` | 0004 |
| Sessions | `Agent.Snapshot() → Session`, `Session.Encode`/`DecodeSession` (**no fork API**); v1 `State` = conversation + loop counters (P1.6) | 0001 |
| Errors | `ErrAutoUnavailable`, `ErrOrderingCycle`, `ErrSessionVersion`, `ErrInputPending` | 0003, 0004 |

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
| processing/ (parsers) | ∅→**P (P1.3)** | RISKIEST; TS oracle + ported test vectors *is* the gate (0024b); **P1.3: one format end-to-end — native/JSON tool-call parse (`ParseNativeToolCalls`→`domain.ToolCall`, args validated, empty→`{}`, malformed→`ErrMalformedToolCall` never panic) + inline thinking-channel strip (`StripThinking`/`IsThinking`; gemma `<think>`, gpt-oss harmony); ported thinking-stripper vectors are the gate; package depends only on `domain`. P1.2: the loop adapts `provider.ToolCall`→`NativeToolCall` and parses at the seam (a malformed call degrades to a parse-error path, not a Turn failure)** | markdown-fenced + custom-regex formats; full harmony channel set (→ Phase 3); inline thinking-strip wiring (needs a `ThinkingConfig` source) |
| Tools (~30) | S→**P (P1.4)** | open extension point; stateless-across-Turns; external-effect boundary; **P1.4: minimal local set — `read_file`/`write_file`/`list_dir`/pure-Go `grep` (`io/fs` walk + `regexp`, no external programs) in `internal/tools/`, each scoped to a sandbox root at construction with traversal-rejecting path-safety (symlink-aware); real `domain.ToolRegistry`; `NewDefaultRegistry(root)` seam; optional `ReadOnlyTool` interface (the Plan-mode/Approval signal). P1.2: dispatch/approval/executor wired — `Config.WorkspaceDir` resolves the default registry; Plan filters the menu to read-only; Ask-Before gates writes; Auto gates only external-effect tools; allow-for-session cached; tool panics → `ErrorEvent` + error result** | richer tools (patch-edit/terminal/web); ripgrep-optional |
| Context (Budget/Compaction/capping) | ∅ | four-way split; Compaction default generative; capping = surviving half of `compress` | Budget allocation algorithm; Compaction trigger/strategy; token counting |
| Sessions | S→**P (P1.6)** | snapshot/resume at quiescent boundary; copyable value; **P0.6: versioned JSON `Snapshot`/`Resume`/`DecodeSession`, future-version rejected; P1.2: `State` payload is `domain.Conversation` — full messages (tool calls + tool-call IDs) and the deferred-action queue round-trip; P1.6: concrete v1 schema finalised — the engine-state envelope (`internal/agent/state.go`) wraps the conversation with the loop counters (`turnIndex`, in-Exchange flag, pending input) so Resume continues at the right Turn, and per-message `Extra` wire fields (`reasoning_content`) round-trip via `Message` (un)marshal** | versioning/migration beyond reject (Phase 3+); the allow-for-session approval cache is deliberately not serialized (re-confirmed on resume) |
| Mechanisms + registry | S→**P (partial)** | constraint-declared; deterministic total order; descriptor; Bypass by Capability; **P0.6: cycle detection + experimental-hook slots real** | full topo-sort *order* (only cycle-check built); self-regulation (Adaptive Suppression, Turn Budget, Effectiveness tracking); catalogue→hook mapping (deferred session) |
| Security guardrails | ∅ | Approval, path/url safety, arg-guard, circuit-breaker, audit | designs; arg-guard policy; audit format |
| Confinement | S (iface) | capability matrix; Auto needs fs+net; per-tool; backends macOS/Linux v1 | backend impls (seatbelt/landlock); deferred design session |
| Sub-agents | ∅ | privileges ≤ parent; top-level-only stepping v1; events nest | orchestrator design (mode/approver/confiner/tool-subset threading) |
| MCP client | ∅ | official go-sdk; stdio/SSE/streamable-http; gates Approval in Auto | client design; re-verify SDK maturity at Phase 3 |
| Library | ∅ | cross-session per-model; confidence-tagged `ModelFingerprint`; inert under Bypass; longitudinal bench gate | store design; Bayesian confidence; fingerprint resolution; GGUF hash |
| Platform (shell/path) | ∅ | POSIX v1, Windows Phase 5; one interface | shell abstraction; path handling; Windows backend |
| TUI | ∅ | Bubble Tea; thin renderer over Events; supplies Approval delegate | model/update/view; panes |
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
5. **`UserInput`/`FileRefs` resolution** — how file references become budgeted context
   (context-builder seam) is unspecified.
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
   [`../plans/phase-1-detail-plan.md`](../plans/phase-1-detail-plan.md) §3.

**Process / scaffolding (Phase 0):**
- ✅ **Done (P0.2):** `go.mod` (`go 1.26`, no deps) + `cmd/apogee` + empty `internal/` skeleton; `apogee.go` compiles and `go vet`/`go vet -race` pass in-tree.
- ✅ **Done (P0.4):** CI — `.github/workflows/ci.yml` cross-compiles Win/Mac/Linux × amd64/arm64 and gates `gofmt`/`go vet`/`go build`/`go test -race`.
- ✅ **Done (P0.3):** dependency versions pinned-by-decision (Cobra, Charm v2 stack, MCP go-sdk v1.6.1, yaml.v3, shlex, ulid) in the detail plan §1 — added per-task, graph still empty.
- ✅ **Done (P0.3):** the **Phase-0 detail plan** — [`../plans/phase-0-detail-plan.md`](../plans/phase-0-detail-plan.md) (task-level breakdown, acceptance criteria).
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
3. ✅ **Phase-0 detail plan + CI** — **DONE (P0.3+P0.4, `c7d4f61`):** [`../plans/phase-0-detail-plan.md`](../plans/phase-0-detail-plan.md) (version pins, CI spec, acceptance-tested task list) + `.github/workflows/ci.yml`.
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
**Phase 0 is complete (P0.1–P0.6); Phase 1's core is built — P1.0–P1.6 are done. Only P1.7
(point the bench at the API) remains.** The ADR-0010 layout is realised (P1.0), the real
provider client is built (P1.1), `processing/` parses one tool-call format (P1.3), the minimal
tool set + registry are built (P1.4), the hook-mutation bodies are real (P1.5), **P1.2 — the
convergence — landed the full Turn/Step state machine** (a Step streams the Upstream reply
emitting `TokenEvent`s, parses tool calls, runs the post-response/pre-tool-exec/post-tool-result/
history-rewrite hooks, dispatches tools through Approval, and returns at a quiescent boundary;
`Run` steps until the Exchange ends), and **P1.6 finalised the concrete v1 Session schema**: the
engine-state envelope (`internal/agent/state.go`) serializes `turnIndex`, the in-Exchange flag,
and pending input alongside `domain.Conversation`, and per-message `Extra` wire fields round-trip,
so Resume *continues* an Exchange instead of restarting it. The `Responder` seam is streaming-only;
§6 #6 (stream-then-gate) and §6 #3 (synchronous in-order emit) are settled. The latest state lives
in the handoffs.
The remaining Phase-1 work is the task-level breakdown in
[`../plans/phase-1-detail-plan.md`](../plans/phase-1-detail-plan.md) §4: **P1.7** points
`apogee-sim` at the Go API (`go.mod replace github.com/airiclenz/apogee => ../apogee`, construct
an `Agent` against isolated dirs, `Submit`/`Step`/score a file-edit task) — the Phase-1
deliverable. The only throwaway P0.6 internal still standing is the cycle-check-only Mechanism
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
