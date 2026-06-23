# Apogee — Technical Design Document (TDD)

**Status:** 🌱 v0.1 — sparse scaffold, to be densified. This is the consolidated
technical design (the *as-designed system* in one place). It **synthesizes** the
authoritative records; it does not replace them. For *why* a decision was made, follow
the ADR link — this doc records *what* the design is and *what is still undesigned*.

**Date:** 2026-06-23  **Repo state:** P0.1 (`apogee.go` facade) and P0.2 (`go.mod` +
`cmd/apogee` + empty `internal/` skeleton) are committed; the facade builds and
`go vet`s in-tree with panic-stub bodies.

**Purpose of this revision:** a `/handoff` to the next session. The job next session is
to **raise the density** of the thin sections (marked **⏳ DENSIFY** with a concrete
"what's needed" note) — not to re-open settled decisions. The **§8 backlog** is the
prioritized worklist.

### Reading order / source map
| Artifact | Role | Path |
|---|---|---|
| `CONTEXT.md` | Glossary — the domain language (authoritative for terms) | [`../../CONTEXT.md`](../../CONTEXT.md) |
| ADRs 0001–0009 | Point decisions + rationale (authoritative for *why*) | [`../adr/`](../adr/) |
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

Plus `CONTEXT.md` (the glossary, with a retired-terms map) and the phased implementation
plan. **All four prior "open items" are resolved** (plan §6 #22–24).

### 2.2 Code
| Artifact | State |
|---|---|
| `apogee.go` | **Signature sketch** — public API facade. gofmt-clean, stdlib-only, **bodies are `panic` stubs**. As of **P0.2** it **builds + `go vet`s** in-tree. |
| skeleton (P0.2) | `go.mod` (`go 1.26`, no deps), `cmd/apogee` (stdlib `--help` stub), and empty `internal/{agent,provider,processing,tools,context,session,mcp,security,mechanisms,platform,tui}` (a `doc.go` per package). **No tests, no CI yet.** |

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
| Sessions | `Agent.Snapshot() → Session`, `Session.Encode`/`DecodeSession` (**no fork API**) | 0001 |
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
**S**=sketched (signatures only), **∅**=not started.

| Component | Status | Decided | Undesigned (→ §8) |
|---|---|---|---|
| Public API facade | S | shape, seams, naming (§4); hook mutation API (§6.2, done P0.1) | bodies |
| Loop / Turn engine | ∅ | Turn = one primary Upstream call; quiescent boundary; recover-at-boundary (0007) | internal state machine; how Steps interleave streaming/approval/tools |
| Provider / Upstream | ∅ | openai-compatible; model discovery; TS as oracle | client design, streaming, ret/timeouts, server-process mgr |
| processing/ (parsers) | ∅ | RISKIEST; TS oracle + ported test vectors *is* the gate (0024b) | parser architecture; harmony/thinking channels; vector extraction |
| Tools (~30) | S (iface) | open extension point; stateless-across-Turns; external-effect boundary | per-tool design; approval/path-safety wiring; pure-Go search vs ripgrep |
| Context (Budget/Compaction/capping) | ∅ | four-way split; Compaction default generative; capping = surviving half of `compress` | Budget allocation algorithm; Compaction trigger/strategy; token counting |
| Sessions | S | snapshot/resume at quiescent boundary; copyable value | concrete schema; versioning/migration; what's in `State` |
| Mechanisms + registry | S (iface) | constraint-declared; deterministic total order; descriptor; Bypass by Capability | topo-sort impl; cycle detection; self-regulation (Adaptive Suppression, Turn Budget, Effectiveness tracking); catalogue→hook mapping (deferred session) |
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

1. **Confiner package placement.** Sketch puts the interface + `ConfinementCaps`/
   `ConfinementBox` in the root `apogee` package, backends in `internal/platform`.
   Alternative: a public `apogee/platform` (or `apogee/confine`) subpackage. **Decide and
   reflect in plan §3 + ADR 0004.**
2. **Hook mutation API — the biggest gap.** `Request`, `Response`, `Conversation` are
   exposed to hooks as **opaque structs with unexported fields** (sketch lines ~507–525),
   but hooks must *mutate* them (pre-request shapes `Request`; history-rewrite edits
   `Conversation`; post-tool-result edits `ToolResult`). The **accessor/mutation surface is
   undesigned** and is the most likely churn point. Scope it from apogee-sim's actual
   Transform/Injector signatures, not speculation.
3. **Event delivery & backpressure.** `EventSink.Emit` must not block the loop; define the
   contract (buffering, drop policy, sub-agent fan-in). Channel adapter for Bubble Tea?
4. **`mechanisms/` package-per-hook layout** statically encodes the hook point, in tension
   with ADR 0003's *constraint-declared* (hook = descriptor field, dynamic order). Plan
   already calls it "provisional." Lean toward a flat `internal/mechanisms` with hook-point
   as data. **Resolve when the catalogue→hook mapping session runs.**
5. **`UserInput`/`FileRefs` resolution** — how file references become budgeted context
   (context-builder seam) is unspecified.
6. **Streaming + Approval interleave inside a Step** — confirm the control flow (sync
   `Approver` call mid-stream; what the EventSink sees around it).

---

## 7. What's still missing (inventory)

**Process / scaffolding (Phase 0):**
- ✅ **Done (P0.2):** `go.mod` (`go 1.26`, no deps) + `cmd/apogee` + empty `internal/` skeleton; `apogee.go` compiles and `go vet`/`go vet -race` pass in-tree.
- No CI (cross-compile Win/Mac/Linux, `gofmt`/`go vet`/`-race`), no build.
- No pinned dependency versions (Cobra, Bubble Tea/Lipgloss/Bubbles, MCP go-sdk, yaml.v3).
- No **Phase-0 detail plan** (the plan is broad-by-design; Phase 0 needs a task-level plan).
- No throwaway in-process harness proving construct→Step→snapshot→resume→register-hook (plan Phase 0 deliverable).
- No tests anywhere (testing strategy named but not concretized; `testing.go.md`: table-driven + golden files).

**Design depth (this TDD's §5 ∅/S rows):** loop engine, provider, processing/, context
reducers, security guardrails, sub-agent orchestrator, MCP, Library, platform, TUI — all
undesigned beyond ADR-level decisions. The **hook mutation API** (§6.2) is the priority gap
in the *public* surface.

**Deferred dedicated sessions (prerequisites, already flagged):**
- **Hook-point catalogue mapping** — map apogee-sim's Mechanisms onto the 5 hooks, driven by real sim traces (prereq to Phase 4).
- **Confinement design** — seatbelt/landlock/AppContainer across the capability matrix (ADR 0004).

**Doc hygiene:**
- `README.md:68` says the bench is "driven through Apogee's headless mode" — **contradicts
  ADR 0001** (bench drives the real loop via Go import; headless is an optional user
  surface). Fix.
- Ratify the five §4.1 sketch-decisions into the plan/ADRs (esp. public `Confiner`).

---

## 8. Densification backlog (next-session worklist, prioritized)

The handoff payload. Each item: raise a §5 row from ∅/S toward a real design, or close a §6/§7 gap.

**P0 — unblocks everything else**
1. ✅ **Hook mutation API** (§6.2) — **DONE (P0.1):** `Request`/`Response`/`Conversation` accessors designed from apogee-sim's Transform/Injector signatures (see `docs/design/hook-mutation-api.md`).
2. ✅ **Stand up `go.mod` + minimal `internal/` stubs** — **DONE (P0.2):** module + `cmd/apogee` + empty `internal/` skeleton; `apogee.go` compiles, `go vet`/`go vet -race` pass in-tree.
3. **Write the Phase-0 detail plan** (task-level, acceptance criteria, version pins, CI). **← next P0.** Then the Phase-0 *capstone* harness (construct→Step→Snapshot→Resume→`AddExperimental`) — needs minimal real bodies, not `panic` stubs.

**P1 — deepen the core design**
4. Loop/Turn engine internal state machine (how a Step interleaves stream → parse → hooks → tool dispatch → approval → boundary).
5. Provider/Upstream client (streaming, model discovery, server-process mgr) — with TS oracle notes.
6. processing/ architecture + **TS-oracle test-vector extraction plan** (golden files).
7. Session concrete schema + versioning (what serializes into `State`; copyability proof).
8. Context reducers: Budget allocation, Compaction trigger/strategy, tool-result capping, token counting.

**P2 — subsystems & validation**
9. Self-regulation design (Adaptive Suppression, Turn Budget, Effectiveness tracking) + deterministic topo-sort/cycle detection.
10. Security guardrails designs; sub-agent orchestrator (privilege threading); MCP client; Library (fingerprint resolution, Bayesian confidence, GGUF hash).
11. Platform shell/path abstraction; TUI model/update/view; CLI surface.

**Housekeeping (cheap, do alongside):**
12. Resolve §6.1 (Confiner placement) + §6.4 (mechanisms layout); ratify §4.1 into plan/ADRs; fix `README.md:68`.

### Suggested next-session entry point
**P0.1 (hook mutation API) and P0.2 (go.mod + skeleton, compiles + vets) are now done.**
Start at **P0.3** — the Phase-0 detail plan (task-level acceptance, version pins for
Cobra/Bubble Tea/MCP-SDK, CI). Then the Phase-0 *capstone* harness that exercises
construct→Step→Snapshot→Resume→`AddExperimental` (the first place the API runs for real —
it needs minimal real bodies, so it follows P0.2's compile checkpoint). P1+ can fan out now
that the keystone compiles.

---

## 9. Conventions
- **`/coding-standards` is mandatory for all new Go** (`coding-standards.go.md` +
  `testing.go.md`), every phase — a gate on every PR (plan Standing Requirement 1). Where a
  standard fights the plan or official Go, the plan/official Go wins (e.g. `Config` struct
  over functional options; package names not forced into single words where it harms clarity).
- Terminology is **authoritative in `CONTEXT.md`** — use those terms exactly; avoid the
  retired proxy-era vocabulary.
