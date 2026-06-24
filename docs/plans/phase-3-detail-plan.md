# Apogee — Phase-3 Detail Plan (P3): full subsystems, Auto confined, cut `v1.0.0`

**Date:** 2026-06-24 · **Status:** 🚧 **IN PROGRESS** — **P3.0 entry re-verify ✅ done** + **P3.1
confinement execution-model design ✅ done** (2026-06-24: ADR 0012 accepted, the implementation contract
written as [`docs/design/confinement-execution-contract.md`](../design/confinement-execution-contract.md);
see the P3.1 result note in §4); **P3.2 (Linux landlock) / P3.3 (macOS seatbelt) are next — now mechanical
against the contract** · **Branch:** `main` (commit directly — pre-production owner directive).
This document refines the broad plan's **Phase 3** ("Full subsystems") into numbered,
acceptance-tested tasks and **makes the load-bearing design calls Phase 3 lands into** (§3 — the
confinement execution model, the sub-agent orchestrator shape, the MCP non-confinable gating, and
the `processing/` parity gate). It is authoritative for the *order and acceptance* of Phase-3 work.
**Parent:** [`implementation-plan-apogee-merge.md`](./implementation-plan-apogee-merge.md) §4
(Phase 3 is intentionally coarse there). **Predecessor:**
[`phase-2-detail-plan.md`](./phase-2-detail-plan.md) (the TUI shell that now consumes the surface).
**Design of record:** [`../design/technical-design.md`](../design/technical-design.md) §5
(Tools / Confinement / Sub-agents / MCP / processing rows) and the governing ADRs:
[0002](../adr/0002-tools-are-an-open-extension-point-mechanisms-are-curated.md) (tools open),
[0004](../adr/0004-auto-mode-requires-os-level-confinement.md) (Auto ⇒ confinement, capability
matrix), [0005](../adr/0005-sub-agent-privileges-are-bounded-by-the-parent.md) (sub-agent ≤
parent), [0008](../adr/0008-stateless-tools-and-non-forkable-external-effects.md) (stateless
tools / non-forkable external effects), and [0010](../adr/0010-package-layout-domain-core-and-thin-root-facade.md)
(the `internal/*`-never-imports-root invariant — every new package below obeys it).
**Standing Requirements** (broad plan "⚠️ Standing requirements") apply to every task: **`/coding-standards`
is mandatory for all new Go** (`coding-standards.go.md` + `testing.go.md`); **the module graph stays
lean** (§3a — stdlib-first, every external program an *optional, detected, graceful-degrading*
enhancement, every direct dep a noted decision); and **the one bounded exception is Auto-mode
Confinement** (ADR 0004) — OS-specific and partly external (`sandbox-exec`), accepted because the
core loop + Plan + Ask-Before still run with zero external deps.

> **Why a detail plan now.** Phase 2 was the thin shell; **Phase 3 is the depth**, and it is the
> largest phase in the build. Three of its pieces carry real, OS-specific design weight that ADR
> 0004 itself flagged as *"hard enough to warrant its own dedicated session"*: **how a single tool
> call is actually executed under confinement** (landlock is irreversible per-thread; seatbelt
> wraps a subprocess — these are not the same shape, and in-process tools differ from subprocess
> tools), **how a sub-agent inherits — and never exceeds — its parent's privileges** while its
> events nest into one stream, and **how MCP tools (which Apogee cannot confine) stay gated through
> Approval even in Auto.** Settling these *before* the tool fan-out is the point of §3, the same way
> phase-2 §3 settled the concurrency seam before any pane was drawn. Phase 3 also ends at the
> **`v1.0.0` cut** (ADR 0001 §18): the public Go API has had no stability promise through Phase 3,
> and by its end every consumer — TUI, bench, optional headless — has exercised the surface, so
> semver begins. That makes **every public-surface addition in this phase a deliberate, freeze-aware
> decision** (§3 D7), not an incidental export.

---

## 0. Phase-3 entry state (where the repo stands)

| Backlog | Deliverable | State |
|---|---|---|
| P0.1–P0.6 | Phase 0 — facade, skeleton, detail plan + CI, `platform` seam (incl. `Confiner` iface + `denyConfiner` stub), capstone harness | ✅ complete |
| P1.0–P1.7 | Phase 1 — ADR-0010 layout, real provider, full Turn/Step state machine, `processing` (**one** format + thinking strip), **4 minimal tools**, hook-mutation bodies, concrete Session v1 schema, bench re-armed | ✅ complete |
| P2.0–P2.6 | Phase 2 — Cobra binary + state-root injection, the C1–C5 concurrency seam (ADR 0011), Model/Update/View, event fold, Approval UI, config + sessions, hermetic **and** live e2e | ✅ complete |
| — | the public API is **body-complete for an embedder** and has now been **exercised by two consumers** — the bench (programmatic, P1.7) and the **TUI (interactive, P2.6)** — under `-race` | ✅ |
| — | verify green: `gofmt -l .` · `go vet` · `go build` · `go test -race ./...` · `grep -rl '"github.com/airiclenz/apogee"' internal/` empty (ADR-0010) · 6-target cross-build · `apogee --help` exit 0 | ✅ |

**Readiness (re-verify against source at P3.0 before any code — same discipline as the Phase-2
entry).** Re-run the full verify gate from a clean tree, then confirm the seven inherited facts
below still hold field-by-field (a Phase-2 follow-up commit may have shifted a line). **Do not take
this table on trust at build time** — P3.0's first job is to reconfirm it.

**What Phase 3 inherits to build on (the surface to deepen — verified against source 2026-06-24):**

- **Tools — 4 built-ins, the open extension point is live.** `internal/tools/` ships
  `read_file` · `write_file` · `list_dir` · pure-Go `grep`, each scoped to a sandbox root at
  construction with symlink-aware, traversal-rejecting path-safety; assembled by
  `tools.NewDefaultRegistry(root)`. The public contract is already shaped for the suite:
  `domain.Tool` (`Name`/`Description`/`Schema`/`Execute`), the optional **`ReadOnlyTool`**
  (`ReadOnly() bool` — the Plan/Approval signal) and **`ExternalEffectTool`**
  (`ExternalEffect() ExternalEffectKind` — the non-forkable / unconfinable marker), and
  `domain.ToolRegistry` with `Register`/`Lookup`/`All`/**`Subset(names…)`** (the sub-agent
  narrowing seam, ADR 0005). *Phase 3 grows this from 4 to the full suite — `Subset` and the two
  optional interfaces are the seams it was built around.*
- **Confinement — the interface and the gate exist; the backends and the `Confine` call do not.**
  `domain.Confiner` = `Capabilities() ConfinementCaps` + `Confine(ctx, ConfinementBox, fn func(ctx) error) error`;
  `ConfinementCaps{FSWrite, NetworkEgress}` with `AutoEligible()` = *both true* (P3.4 narrows this to
  **`FSWrite`-only** per ADR 0012 — network is open in Auto by default);
  `ConfinementBox{WorkspaceRoot, WritablePaths, NetworkAllow}`. `internal/platform` ships only the
  **`denyConfiner`** stub (`AutoEligible()==false`, runs `fn` unchanged). `agent.New` already
  **refuses Auto** when `!autoEligible(cfg.Confiner)` → `domain.ErrAutoUnavailable`. **Critically:
  `dispatch.go` does *not* yet call `Confine` — tool execution is unconfined today**, which is sound
  only because Auto is currently unreachable (no eligible backend exists). *Phase 3 builds the
  backends and threads `Confine` into dispatch — that is what makes Auto real.*
- **Approval/mode wiring — already per-tool-aware (3 modes today).** `needsApproval` (dispatch.go)
  is: **Plan** ⇒ read-only tools only; **Ask-Before** ⇒ gate every non-read-only tool; **Auto** ⇒
  gate **only `ExternalEffectTool`s** (the per-tool invariant in embryo). `approved[tool]` caches
  allow-for-session. *(ADR 0012 refines the Auto half: `network`-kind external tools auto-run, only
  `mcp`-kind keeps gating under `confine=true`.)* *Phase 3 inserts the
  **Allow-Edits** rung (Plan→Ask-Before→Allow-Edits→Auto), reworks this into the blast-radius
  disposition (D5), makes the "run confined" half real for the subprocess surface, and proves the
  gating end-to-end.*
- **processing — one format + thinking strip; the TS oracle is the parity gate.**
  `ParseNativeToolCalls` (native/JSON `tool_calls`) + `StripThinking`/`IsThinking` (gemma `<think>`,
  gpt-oss harmony `<|channel|>…<|end|>`). **Markdown-fenced + custom-regex parsers and the full
  harmony channel set are NOT built** (TDD §5 "Undesigned"). The package depends only on `domain`;
  the loop adapts `provider.ToolCall`→`NativeToolCall` at the seam. *Phase 3 finishes the riskiest
  port, ported test vectors from the apogee-code TS source remaining the gate (ADR-0024b posture).*
- **Events — 8 variants carry `Depth`; the TUI tolerates `Depth > 0` but does not render it richly.**
  Every Phase-1/2 event is `Depth == 0`; the renderer indents a continuation line when `Depth > 0`
  and otherwise ignores nesting. *Phase 3 emits real `Depth > 0` (sub-agents) and renders it.*
- **MCP — bare `doc.go` stub.** `internal/mcp` holds only the Phase-0 scaffold comment
  ("re-verify SDK maturity at Phase 3"). *Phase 3 builds the client on the official Go SDK.*
- **Platform seam — `Shell`/`Path` interfaces (POSIX real, Windows stub).** `Shell.Command(line)`
  and `Path.ExecExt()` are the minimal surface; the execution + git + diagnostics tools widen it
  (PATH lookup, env-scoped exec, process-group kill). **Windows confinement + shell stay Phase 5** —
  the cross-build must stay green throughout via the `denyConfiner`/Windows-stub fallbacks.
- **Mechanisms — registry with cycle-detection only; the catalogue is Phase 4.** Hook points and
  descriptor types exist; **no Mechanism is built**, and the deterministic topo-sort / self-regulation
  are Phase 4. *Phase 3 adds no Mechanisms* — `MechanismFiredEvent` stays behind the TUI's debug view.

---

## 1. Phase-3 deliverable & exit definition

Broad plan §4 Phase-3 deliverable, verbatim: *"feature-parity with apogee-code's non-UI behavior,
with Auto mode confined on Mac/Linux. **Cut `v1.0.0` of the public Go API here**."* Phase 3 is
**done** when all hold:

1. **The tool suite is feature-complete vs apogee-code's non-UI behaviour.** The
   ~30-tool surface is built behind the public `Tool` interface: the file-editing family
   (find-replace single/multi, diff, patch/apply-edit, open-file), `terminal`, `python-exec`,
   `git` (branch/commit/diff-range), `web-fetch`/`web-search`/`http-request`, `diagnostics`,
   `ask-user`, and the existing read/write/list/grep — each honouring the stateless-across-Turns
   contract (ADR 0008) and §3a (in-process default, external programs optional + detected +
   graceful-degrading). Parity is judged against the **TS oracle behaviour + the bench**, not by
   line-count.
2. **`processing/` is parity-complete.** All apogee-code tool-call formats parse (native/JSON,
   markdown-fenced, custom-regex) and the **full harmony / thinking-channel set** is handled, each
   gated by **ported TS test vectors** (the riskiest-port discipline). A `processor-factory`
   selects the format per model/response.
3. **The autonomy ladder is complete and Auto is real on Mac + Linux.** The mode ladder is Plan →
   Ask-Before → **Allow-Edits** → Auto (CONTEXT: Agent mode). The `platform/` `Confiner` backends
   exist — macOS seatbelt and Linux landlock — reporting an honest capability matrix and confining
   the **unbounded subprocess/network surface** (the single, all-OS subprocess granularity).
   The **blast-radius invariant holds** (ADR 0012, superseding ADR 0004): under `confine-to-workspace=true`
   (default) a subprocess/shell tool runs unsupervised in Auto *only* under confinement (escape OS-blocked),
   Apogee's own workspace-scoped in-process writes run path-safety-bounded (out-of-workspace ⇒ Approval; no
   per-thread landlock — **no thread-discard, no macOS asymmetry**), the **network is open** so native
   `web-fetch`/`http-request` **auto-run** (url-filtered), and **MCP gates through Approval** (unfenceable
   server); if fs-confinement is unavailable, subprocess tools gate. Under `confine-to-workspace=false`
   ("I am the sandbox", global-config-only, VM-only) nothing fences except the dangerous-action floor.
   **Allow-Edits needs no confinement and is identical on every OS.** **`AutoEligible()` requires
   fs-confinement only**, so Linux Auto now needs kernel **≥5.13** (not ≥6.7); a host with no
   fs-confinement at all gates the subprocess surface rather than refusing Auto. `ErrAutoUnavailable`
   becomes reachable-but-conditional, not a permanent refusal.
4. **Sub-agents work, privileges bounded.** A sub-agent is constructed with the parent's mode,
   approval delegate, confiner, and guardrails threaded in, and a **tool subset ≤ the parent's**
   (ADR 0005 — never the gate-less apogee-code port); its events **nest into the parent stream**
   (`Depth > 0`) and the TUI **renders the nesting**. Stepping is top-level-only for v1 via a
   swappable driver (broad plan #15).
5. **MCP client works on the official Go SDK.** `internal/mcp` connects over stdio / SSE /
   streamable-http (`modelcontextprotocol/go-sdk`, pin re-confirmed at P3.0), surfaces server tools
   into the registry as `ExternalEffectTool`s, and **resume reconnects fresh** with no
   server-side-state promise (ADR 0008). MCP tool calls gate through Approval in Auto under
   `confine-to-workspace=true` (#3; free under `confine=false`).
6. **Security guardrails are in place.** `internal/security` provides path/url safety, arg-guard,
   circuit-breaker, and an audit record — the human-in-the-loop guardrails (NOT a sandbox; the
   sandbox is the `Confiner`). Path-safety from the Phase-1 tools is consolidated here.
7. **The ADR-0010 invariant still holds.** `grep -rl '"github.com/airiclenz/apogee"' internal/`
   stays **empty**: every new package (`internal/mcp`, `internal/security`, `internal/agent/subagent`,
   the new `internal/tools/*`, the `platform` backends) imports **down** to `internal/domain`, never
   the root module path. The cross-build stays green on all 6 targets (OS-specific confinement behind
   build tags + the `denyConfiner`/Windows fallbacks).
8. **`v1.0.0` is cut.** Every public-surface addition this phase is reviewed as a freeze decision
   (§3 D7); the facade is frozen; `v1.0.0` is tagged; ADR 0001 §18's "v0.x, no stability promise"
   clause is amended to record that semver now begins (Events/hook-points stay additively
   extensible — a new variant is a minor bump).

**The exit gate is the deliverable run** (§7): a real coding conversation against a **live local
model**, in **Auto** mode, with confinement enforced (a **shell/subprocess** write outside the
workspace is blocked by the OS, an MCP tool call still raises Approval), a sub-agent delegated and its
nested work rendered — plus the hermetic, reproducible proofs under `-race`, plus the bench
feature-parity run.

---

## 2. Dependency additions (pins, decided per task — §3a: a pin is a decision)

A pin is a decision; the dependency is added by the *task that first needs it*. Phase 3's additions:

| Module | Pin | Added by | Note |
|---|---|---|---|
| `golang.org/x/sys` | latest stable @ P3.2 | **P3.2** (landlock) | Landlock syscalls (`unix.Landlock*`). Likely already transitive via Charm — P3.2 promotes it to a **direct** dep and re-runs `go mod tidy`. ABI-v4 / kernel-≥6.7 detection is runtime, not build-time. A thin landlock helper (`github.com/landlock-l/go-landlock`) is a **fallback** only if raw syscalls prove unergonomic — decide in P3.2, record in the commit. |
| `github.com/google/shlex` | `v0` pinned-by-decision (phase-0 §1) | **P3.8** (`terminal`) | POSIX-correct command-line splitting for the `terminal` tool. Tiny, no transitive deps. |
| `github.com/modelcontextprotocol/go-sdk` | **`v1.6.x`** — re-confirm exact patch at P3.0 (TDD recorded `v1.6.1`; GA-verified at the P0.6 gate, Decision B) | **P3.15** (MCP client) | The official Go SDK; stdio / SSE / streamable-http. `mark3labs` is a **break-glass fallback only**, no longer co-evaluated. Re-confirm the pin + the transport surface at P3.0 before P3.15. |

**No new dep for:** seatbelt (the macOS backend shells out to the **system** `sandbox-exec` with a
generated profile — no Go module), `web-fetch`/`http-request` (stdlib `net/http`), `web-search`
(a config'd search endpoint — no hard-wired provider; the backend URL is injected, defaulting off),
`git` (shell-out to the **system** `git`, §3a optional + detected), `diagnostics` (in-process
`go/parser` + the `go vet` that ships with the toolchain; other linters optional shell-outs),
`diff`/`patch` (stdlib + a tiny in-package myers diff, no external). Each addition is re-justified
when its `go get` lands; the binary stays one static artifact for the core loop + Plan + Ask-Before.

---

## 3. The design calls Phase 3 lands into (the hard part)

These are the calls that must be made (or explicitly routed to an ADR) **before** the tool fan-out,
because every tool, the sub-agent, and the MCP client are shaped by them. Phase-2 §3 settled C1–C8
inline; here the OS-specific pieces (D1) and the cross-cutting ones (D2, D3) are settled by a
**dedicated ADR landed with the first task that needs them** — but this section makes the
**recommendation** each ADR should ratify, so the order and the acceptance gates are pinned now.

### D1 — The confinement execution model: confinement attaches to *blast radius*, not to a mode (→ ADR 0012, landed by **P3.1**; refines ADR 0004)

This is the single hardest call, and the **autonomy ladder** (Plan → Ask-Before → **Allow-Edits** →
Auto; CONTEXT: Agent mode) reframes it decisively. The old framing — *"Auto ⇒ every tool must be
OS-confined"* — forced a naïve `Confine(fn)` to wrap in-process writes on a per-thread landlock,
which is irreversible-per-thread on Linux and **has no equivalent on macOS at all** (seatbelt confines
a *subprocess*, not a thread). That route produced a thread-discard trick (poison a `LockOSThread`'d
thread, let the runtime kill it), an unenforceable no-goroutine contract, and a macOS-gates-every-edit
asymmetry. **All of that is now deleted.**

**The insight: confinement is required exactly where an action's blast radius is *unbounded and
unsupervised* — which is the shell / subprocess / arbitrary-network surface, and *only* in Auto.**
Everything else is bounded by something cheaper:

- **Apogee's own in-process write tools (`write_file`, find-replace, patch)** are workspace-scoped by
  `internal/tools/path_safety.go` — code Apogee writes and tests. Their blast radius is bounded to the
  workspace by path-safety **at every rung, including Auto**. They need **no** OS confinement; the
  same trusted boundary that lets **Allow-Edits** auto-approve them is what bounds them in Auto.
- **The unbounded surface — shell/subprocess (`terminal`, `python-exec`, optional `git`) and arbitrary
  network** — is what Auto runs *without a human*, so it is what must be OS-confined. And this is the
  **clean subprocess case that confines identically on both OSes**: macOS execs the child under
  `sandbox-exec -p <profile>` (workspace-write-only + **network open by default**, a deny-list only
  when the user opts back into network-deny via `NetworkAllow`); Linux
  applies landlock to the child *after fork, before `execve`* (the domain inherits across exec), parent
  unrestricted. `fn` builds + runs the `*exec.Cmd`; the backend owns the wrapping. **No per-thread
  in-process landlock anywhere ⇒ no thread-discard, no goroutine-escape hole, no macOS asymmetry.**

**Recommendation the ADR should ratify — the invariant, refined from ADR 0004:**

> *A tool call runs without a human gate only if its blast radius is bounded — **by OS confinement**
> for the unbounded subprocess/network surface, **or by Apogee's own path-safety-to-workspace** for
> its own in-process write tools. Apogee never runs a tool call both unsupervised and unbounded.*

This is consistent with what ADR 0004 actually closed (the *"escape the workspace **and** reach the
network, unsupervised"* hole — a path-safety-bounded edit does neither), and it is the **blast-radius
amendment ADR 0012 records and ADR 0004 points to.** Per-call disposition in Auto:

- **Subprocess/shell tool, backend caps sufficient** → run under `Confine` (confined child); no Approval.
- **Apogee's own workspace-scoped in-process write** → run directly, **bounded by path-safety**; no Approval.
- **Third-party in-process tool** → Apogee cannot vouch for its scoping ⇒ **Approval-gate** (treated
  like external-effect). "Workspace-scoped writer" must be a signal **only Apogee's own tools can
  carry** — an unexported marker (e.g. `workspaceScopedWriter`) the built-ins implement and a
  third-party tool structurally cannot fake from outside `internal/`.
- **External reach** (superseded by ADR 0012 — network is now open in Auto): native arbitrary-URL
  `web-fetch`/`http-request` **auto-run** url-filtered in Auto (a subprocess could `curl` the same host
  anyway, and the native tool is *safer* for passing url-safety). **MCP** still **Approval-gates** under
  `confine-to-workspace=true` (it runs in a server Apogee cannot fence — the per-tool teeth, intact),
  and runs free under `confine-to-workspace=false`.

**Capability honesty (ADR 0012):** `Capabilities()` reports `{FSWrite, NetworkEgress}` *as enforceable
on this host now* — for the **subprocess surface** (landlock ABI probed at startup; `sandbox-exec`
presence probed). Since the network is open by default, **`AutoEligible()` requires `FSWrite` only**
(Linux kernel ≥5.13; `NetworkEgress` is an optional tightening for users who opt back into network-deny).
If fs-confinement is unavailable, Auto is *not* refused — subprocess tools gate through Approval
("confine if you can, gate if you can't"). **Acceptance the ADR pins:** under `confine-to-workspace=true`
in Auto, a *subprocess* tool's write outside `WorkspaceRoot` is OS-blocked on both Linux and macOS; an
Apogee in-process write outside the workspace raises Approval; a third-party in-process write and an MCP
call raise Approval; native `web-fetch` auto-runs (url-filtered).

### D2 — The sub-agent orchestrator (→ ADR 0013, landed by **P3.13**)

ADR 0005 fixed the *policy* (privileges ≤ parent); D2 fixes the *shape*. **Recommendation:**

- A sub-agent **is the embeddable `Agent`** (ADR 0001), constructed through an internal
  `subagent` orchestrator that **threads the parent's `Mode`, `Approver`, `Confiner`, and
  guardrails verbatim** (or stricter) and passes a **`registry.Subset(names…)` ≤ the parent's tool
  set** — never an expansion. The signature *requires* these (a compile-time-obvious change from the
  gate-less TS source), so a privilege leak is structurally hard.
- The sub-agent is exposed to the model as a **`sub_agent` tool** that is **dispatch-transparent**:
  it is **never `Confine`-wrapped or gated as a unit** and carries **no disposition marker** (neither
  `ExternalEffectTool` nor `workspaceScopedWriter`). Its `Execute` drives a **nested dispatch where
  each child tool call gets the full per-call blast-radius disposition (D5)** using the parent's
  threaded `Confiner` / `Approver` / mode / guardrails — so inside an Auto sub-agent a child
  subprocess tool confines, a child Apogee write is path-safety-bounded, and a child MCP/arbitrary-URL
  call still raises Approval, exactly the parent's rules one level down. Dispatch recognises
  `sub_agent` as the **recursion point**, not a leaf tool. Its events are re-emitted into the parent's
  `EventSink` with **`Depth = parent.Depth + 1`** so the TUI and bench observe them in one stream.
- **Stepping is top-level-only for v1** (broad plan #15): the parent Step drives the sub-agent to
  completion *within* the parent's tool-dispatch step, behind a **swappable driver** so nested
  stepping (suspend/resume a sub-agent at its own boundary) drops in later without a snapshot-schema
  break (the schema already "leaves room for a suspended sub-agent").
- **Sub-agent execution is atomic within the parent Turn** (the ADR-0007 consequence of top-level-only
  stepping). While the sub-agent runs, the parent is mid-tool-dispatch — **not** at a quiescent
  boundary — so: (a) **no snapshot can land mid-sub-agent**; the parent's next boundary is *after*
  `sub_agent` returns, and the schema's "suspended sub-agent" slot is **reserved-but-always-empty in
  v1** (forward-compat only). (b) **Cancel mid-sub-agent rolls back the whole parent Turn**: cancel
  stays *responsive* (it propagates to the nested loop's next boundary, which returns), but the
  recovery point is the parent's **pre-`sub_agent` quiescent boundary** — the sub-agent's progress is
  discarded, no partial result is surfaced. (c) Resume is therefore coarse by design: *before* or
  *after* a sub-agent, never inside it.
- **Acceptance the ADR pins:** a sub-agent in a Plan-mode parent cannot write (inherits Plan); a
  sub-agent given `Subset("read_file","grep")` cannot call `write_file` even though the parent can;
  an Auto sub-agent still routes MCP/external tools through Approval; nested events arrive at
  `Depth==1` and render indented.

### D3 — MCP is non-confinable ⇒ Approval-gated in Auto under `confine=true` (→ ADR 0014 *or* a P3.15 note)

MCP tools execute in an **external server Apogee cannot confine** (ADR 0012 per-tool teeth; ADR 0008
"non-forkable external effects"). The integration call: MCP tools surface into the registry as
**`ExternalEffectTool`** of effect kind **`mcp`**, which means the `needsApproval` logic gates them
through Approval in Auto **under `confine-to-workspace=true`** (free under `confine=false`) — D3 is
mostly *surfacing them with the right effect kind* (distinct from `network`-kind tools, which auto-run)
so the
invariant holds for free, plus: **resume reconnects fresh** (no server-side-state promise), and the
bench swaps deterministic stubs behind the single injectable external-effect boundary (ADR 0008).
Transports: stdio / SSE / streamable-http on the official SDK; the **client lifecycle** (connect on
config, reconnect on resume, clean shutdown on `Close`) is the design surface. Whether this needs a
full ADR or a design note is a P3.15 judgement — the *decision* (MCP = ExternalEffect ⇒
Approval-gated) is already settled by ADRs 0004/0008; P3.15 records the *client* shape.

### D4 — `processing/` parity is an oracle-gated port, not a redesign (**P3.5**)

No new architectural decision — the riskiest *port*. The gate is **ported apogee-code TS test
vectors** for each format (native already done; markdown-fenced, custom-regex, the full harmony
channel set to add) plus a `processor-factory` that selects per model/response. Record the parity
result in the P3.5 commit; raise an ADR **only if** a format forces a structural call (e.g. a parser
that needs loop-state it shouldn't see). The package stays `domain`-only (ADR 0010).

### D5 — The per-call disposition lives in dispatch, keyed on mode ⨯ blast radius (realises the ADR-0004/0012 invariant)

`needsApproval` (and its Auto sibling) is the one place the ladder and the blast-radius invariant
become code. Per mode, per call, dispatch computes from `(mode, effect-kind, workspace-scoped-writer,
backend-caps)`:

- **Plan** → read-only tools only (writes refused; the existing path).
- **Ask-Before** → workspace reads free; every write / exec / external reach gates.
- **Allow-Edits** → **Apogee's own workspace-scoped writes auto-approve** (keyed on the
  `workspaceScopedWriter` marker, D1); shell/exec, `ExternalEffectTool` (network/MCP), third-party
  in-process tools, and any out-of-workspace write still gate. **No `Confine` call** — path-safety is
  the bound. Identical on every OS.
- **Auto** (per ADR 0012 — see §5 Resolved; tuned by `confine-to-workspace`):
  - `confine-to-workspace=true` (default): **subprocess/shell tool, caps sufficient** ⇒ run under
    `Confine` (no Approval), or **gate** if fs-confinement is unavailable; **Apogee's own
    workspace-scoped write** ⇒ run directly path-safety-bounded if in-workspace (no Approval, no
    `Confine`), **Approval** if out-of-workspace; **native network tools** (`web-fetch`/`http-request`)
    ⇒ **auto-run** url-filtered (network is open — they no longer gate); **MCP** ⇒ **Approval**
    (unfenceable; "allow for session" caches at server grain); **third-party in-process tool** ⇒
    **Approval** (can't vouch for its scoping).
  - `confine-to-workspace=false` (VM-only): everything auto-runs unfenced **except** the
    dangerous-action floor (Tier-1 refuse / Tier-2 force-approval).

"Workspace-scoped writer" is the unexported marker only Apogee's own tools carry (D1). P3.4 builds
this; every later tool task asserts its own row (e.g. P3.8's `terminal` confines in Auto; P3.11's
`web-fetch` Approval-gates in Auto; P3.7's `write_file` auto-approves in Allow-Edits and runs
path-safety-bounded in Auto).

### D6 — Security guardrails are the human-in-the-loop layer, distinct from the sandbox (**P3.12**)

`internal/security` = path/url safety + arg-guard + circuit-breaker + audit. It is **not** the
sandbox (that is the `Confiner`); it is the layer that runs in **all** modes (path-safety already
does, per-tool). P3.12 **consolidates** the Phase-1 per-tool path-safety into one reusable guard and
adds url-safety (for `web-fetch`/`http-request`), arg-guard (reject dangerous tool arguments before
execution), a circuit-breaker (halt a runaway tool-loop), and an audit record. These guardrails are
threaded by the tool executor, so a sub-agent inherits them (D2) for free.

### D7 — Public-API freeze discipline (every export this phase is a `v1.0.0` decision)

Phase 3 ends at the `v1.0.0` cut, so each new public symbol is reviewed against the freeze. **New
public surface expected:** new `Tool` *implementations* (fine — tools are an open extension point,
ADR 0002, and live in `internal/tools` exposed via the registry, not as root types); **one new host
delegate** for `ask-user` — an **`Asker`** on `Config` (P3.11), **struct-typed for freeze-safety**
(`Ask(ctx, AskRequest) (AskAnswer, error)`, structs so multiple-choice is an additive post-v1 field),
distinct from `Approver`; a new `Mode` constant **`ModeAllowEdits`** (P3.4); the `Confiner` (already
public). **No** new public Mechanism surface (Phase 4). The rule: prefer **not** to widen the root
facade — add behaviour behind existing seams (registry, `Config` delegates) so the v1 surface stays
minimal. P3.16 does the final review + freeze.

---

## 4. Phase-3 task list

IDs use the `P3.x` scheme. **P3.0 (entry re-verify + pins) blocks all.** Three pillars then fan out
in parallel — **confinement** (P3.1→P3.4, the design-heavy critical path), **processing parity**
(P3.5, derisk the riskiest port early), and **guardrails** (P3.12, underpins the risky tools) — and
the **tool suite** (P3.6–P3.11) fans out behind guardrails + confinement. **Sub-agents** (P3.13) need
the tool suite mature; **MCP** (P3.15) needs the Auto-gating real; **P3.16 is last** (it needs
everything, and it cuts `v1.0.0`).

| ID | Task | Depends | New deps | Resolves |
|---|---|---|---|---|
| **P3.0** ✅ | Phase-3 entry: re-verify gates, re-confirm the §0 inheritance, re-confirm pins (MCP go-sdk `v1.6.x`, landlock approach), refresh dep/ADR posture, confirm processing-oracle access | — | — | this §0; §2 |
| **P3.1** ✅ | **Confinement execution-model design + ADR 0012** (D1): the blast-radius invariant + the Allow-Edits ladder rung, the single subprocess granularity, the per-call decision, capability-honesty; amend/cross-ref ADR 0004. **Done 2026-06-24** — policy in ADR 0012; impl contract in [`docs/design/confinement-execution-contract.md`](../design/confinement-execution-contract.md) (see result note below) | P3.0 | — | ADR 0004; **ADR 0012** |
| **P3.2** | **Linux landlock `Confiner` backend**: fs-write + network-egress, ABI-v4/kernel-≥6.7 probe, honest caps; build-tagged `linux` | P3.1 | `golang.org/x/sys` | ADR 0004; ADR 0012 |
| **P3.3** | **macOS seatbelt `Confiner` backend**: `sandbox-exec` profile from the box, fs+net in one, presence-probed; build-tagged `darwin` | P3.1 | — | ADR 0004; ADR 0012 |
| **P3.4** | **Mode ladder + wire `Confine` into dispatch; Auto becomes real** (D5): add **`ModeAllowEdits`** (Plan→Ask-Before→Allow-Edits→Auto); rework `needsApproval` into the blast-radius disposition; `ErrAutoUnavailable` now conditional. **Also plumb the `ExternalEffects.Do` boundary** (ADR 0008) — currently declared on `Config` but never called; dispatch must route `ExternalEffectTool`s through it when set, so the bench-stub story (P3.11/P3.15/P3.16) is real before the first external tool ships | P3.2, P3.3 | — | ADR 0004; ADR 0008; ADR 0012; dispatch.go |
| **P3.5** | **`processing/` parity finish** (D4): markdown-fenced + custom-regex parsers + full harmony channel set + `processor-factory`, TS-vector-gated | P3.0 | — | TDD §5 processing; broad §4 |
| **P3.6** | **Security guardrails** `internal/security` (D6): consolidate path-safety + url-safety + arg-guard + circuit-breaker + audit | P3.0 | — | broad §4; TDD §5 security |
| **P3.7** | **File-editing tool family**: find-replace (single + multi), `diff`, `patch`/apply-edit, `open-file` — pure-Go, stateless; carry the `workspaceScopedWriter` marker | P3.6, P3.4 | — | ADR 0002/0008; D1/D5; broad §4 tools |
| **P3.8** | **Execution tools**: `terminal` + `python-exec` (one-shot, stateless; first `Confiner` consumers; widen the `Shell` seam) | P3.4, P3.6 | `github.com/google/shlex` | ADR 0008; ADR 0012; §3a |
| **P3.9** | **`git` tool** (branch/commit/diff-range): system-`git` shell-out, §3a detected + graceful-degrading | P3.6, P3.8 | — | §3a; broad §4 tools |
| **P3.10** | **`diagnostics` tool**: in-process `go/parser` + `go vet` for Go; optional shell-out linters for other langs, graceful | P3.6, P3.8 | — | §3a; broad §4 tools |
| **P3.11** | **Network + host tools**: `web-fetch`, `web-search`, `http-request` (external-effect, Approval-gated in Auto, bench-stubbable) + `ask-user` (new `Asker` host delegate) | P3.6 | — | ADR 0008; D3; D7 |
| **P3.12** | *(reserved — folded into P3.6; kept for ID stability if guardrails split)* | — | — | — |
| **P3.13** | **Sub-agent orchestrator + ADR 0013** (D2): privilege threading, `Subset` tool set, top-level-only swappable driver, `Depth+1` event nesting, the `sub_agent` tool | P3.7–P3.11, P3.4 | — | ADR 0005; **ADR 0013** |
| **P3.14** | **TUI `Depth > 0` rendering**: nested-event framing/indentation (Phase-2 "tolerate" → "render") | P3.13 | — | ADR 0011; TDD §5 TUI |
| **P3.15** | **MCP client** on the official Go SDK (stdio/SSE/streamable-http): surface server tools as `ExternalEffectTool`, Auto-gates-MCP, resume reconnects fresh | P3.4, P3.6 | `…/go-sdk` | ADR 0004/0008; D3 |
| **P3.16** | **Phase-3 acceptance + cut `v1.0.0`**: feature-parity vs apogee-code non-UI + bench; live Auto-confined run (Mac + Linux); freeze + tag + amend ADR 0001 §18 | all | — | broad §4 deliverable; ADR 0001 §18 |

> **On P3.12:** guardrails are a single task (**P3.6**); P3.12 is left reserved so the IDs don't
> renumber if a reviewer later splits audit/circuit-breaker out. Treat the live list as P3.0–P3.11,
> P3.13–P3.16.

### P3.0 — Phase-3 entry (re-verify + re-confirm pins)
Re-run the full verify gate from a clean tree (§7). Re-confirm the **seven §0 inheritance facts**
against source (a Phase-2 follow-up may have moved a line — especially `needsApproval`/`dispatch.go`
and the `Confiner`/`denyConfiner` surface). **Re-confirm the pins:** `go-sdk` `v1.6.x` exact patch +
its transport API (stdio/SSE/streamable-http still GA), and the landlock approach (raw
`golang.org/x/sys` vs a helper). Confirm the **apogee-code TS source is reachable** for ported
processing vectors (the P3.5 gate). Refresh this plan's §0 table if anything drifted.
**Acceptance:** verify gate green; pins reconfirmed in a short note; no code change beyond doc
refresh. This task is the Phase-3 analogue of the Phase-2 "Readiness" re-verification.

#### ✅ P3.0 result — re-verified 2026-06-24 (entry gate GREEN, 7/7 facts confirmed, pins held)

Run on the dev host (`go1.26.4`, `linux/arm64`; module `go 1.26`). **No production code changed —
this note is the only edit.**

**Verify gate (§7) — all green:** `gofmt -l .` empty · `go vet ./...` clean · `go build ./...` ok ·
`go test -race ./...` all `ok`, no FAIL / panic / `DATA RACE` · ADR-0010 grep
(`grep -rl '"github.com/airiclenz/apogee"' internal/`) empty · 6 cross-builds OK
(linux/darwin/windows × amd64/arm64, `CGO_ENABLED=0`) · `go mod tidy -diff` no drift ·
`apogee --help` exit 0.

**Seven §0 inheritance facts — all CONFIRMED, zero drift** (verified against source, file:line):
(1) tools — exactly 4 built-ins via `NewDefaultRegistry(root)`; `Tool`/`ReadOnlyTool`/`ExternalEffectTool`
+ `ToolRegistry.Subset` all present, no current tool implements `ExternalEffect()`.
(2) confinement — `Confiner`/`ConfinementCaps{FSWrite,NetworkEgress}` (`AutoEligible()` = **both true**
today) / `ConfinementBox{WorkspaceRoot,WritablePaths,NetworkAllow}` exactly as documented; `denyConfiner`
(`internal/platform/platform.go`) the only backend; `agent.New` refuses Auto (`loop.go:60`); **dispatch
still does not call `Confine`** (`dispatch.go` `executeTool` → `tool.Execute` directly — unconfined, sound
only because Auto is unreachable). (3) approval — `needsApproval` 3-mode logic + `approved[tool]` cache;
**`ModeAllowEdits` does not exist** (only a forward-ref comment at `tui/model.go:464`); domain has exactly
`ModePlan`/`ModeAskBefore`/`ModeAuto`. (4) processing — only `ParseNativeToolCalls` + `StripThinking`/
`IsThinking`; markdown-fenced + custom-regex + full harmony set absent; imports `domain` only. (5) events —
8 variants, all embed `EventBase.Depth`. (6) mcp — `doc.go` stub only. (7) platform — `Shell.Command(line)`
/ `Path.ExecExt()` (POSIX real + Windows stub); mechanisms — `doc.go` stub, cycle-detection lives in
`domain`, no concrete Mechanism. *(Aside: `internal/security` and `internal/context` already exist as
Phase-0 `doc.go` stubs — filled by P3.6 / Phase 4.)* §0 table needs no content change.

**Pins reconfirmed:**
- **MCP `go-sdk` → `v1.6.1`** is the latest stable (proxy `@latest` = `v1.6.1`; only a `v1.7.0-pre.1`
  prerelease exists above it) — **unchanged** from the P0.6 GA-verified pin. All three transports present
  in the `mcp` package at `v1.6.1`: stdio (`StdioTransport`/`CommandTransport`/`IOTransport`), SSE
  (`SSEClientTransport`/`SSEServerTransport`/`SSEHandler`), streamable-http (`StreamableClientTransport`/
  `StreamableServerTransport`/`StreamableHTTPHandler`) — plus `InMemoryTransport` (hermetic bench stub for
  P3.15). Added **direct** in P3.15.
- **landlock → `golang.org/x/sys v0.45.0`** is already present (currently **indirect** via Charm) and
  carries the full Landlock surface: consts `LANDLOCK_*` incl. ABI-v4 net (`LANDLOCK_ACCESS_NET_CONNECT_TCP`,
  `LANDLOCK_CREATE_RULESET_VERSION`); types `LandlockRulesetAttr` (`Access_fs`/`Access_net`/`Scoped` —
  current through ABI-v6) + `LandlockPathBeneathAttr`; syscall numbers `SYS_LANDLOCK_CREATE_RULESET`/
  `_ADD_RULE`/`_RESTRICT_SELF`. **Caveat for P3.2:** x/sys exposes **no high-level func wrappers** (`go doc`
  finds no `LandlockCreateRuleset`/`AddRule`/`RestrictSelf`), so "raw x/sys" means
  `unix.Syscall(unix.SYS_LANDLOCK_*, …)` over the typed attrs — workable but low-level; this is the concrete
  input to P3.2's "raw vs `github.com/landlock-l/go-landlock` helper" call. x/sys promoted to **direct** in
  P3.2. (`shlex`, P3.8, not yet added — expected.)
- **TS oracle reachable:** `/workspace/repos/apogee-code` exists locally → the P3.5 ported-vector source is
  available.

**Next: P3.1** — Confinement execution-model design + **ADR 0012** (no backend code yet). Handoff
`docs/handoffs/2026-06-23 - 18 - phase-2-complete-next-phase-3-entry.md` is consumed by this landing
(per §8 — archive when convenient).

### P3.1 — Confinement execution-model design + ADR 0012 (blast-radius framing + the mode ladder)
Settle D1 as **ADR 0012** before any backend code: the **blast-radius invariant** (OS-confine the
unbounded subprocess/network surface; path-safety bounds Apogee's own in-process writes; third-party
in-process + unconfinable-external gate), the **autonomy ladder** Allow-Edits adds below Auto, the
per-call disposition (D5), the capability-honesty rule (probe at startup), and the `Confine` contract
(`fn` builds + runs the confined `*exec.Cmd` — confinement is a **single subprocess granularity** on
both OSes; there is **no** in-process per-thread landlock, hence no thread-discard). **ADR 0012
records the refinement to ADR 0004**, and ADR 0004 gets a short amendment pointing to it (its core
"escape-workspace-and-reach-network is forbidden when unsupervised" claim is preserved, not reversed).
Define the **acceptance harness shape** the backends share (a hermetic "try to escape the box" probe
for a *subprocess* tool: write outside `WorkspaceRoot`, reach a non-allowlisted host — assert OS
denial). **Acceptance:** ADR 0012 committed (status accepted) + ADR 0004 amended/cross-referenced; the
`workspaceScopedWriter` marker is specified; the shared confinement-probe contract is specified
(signatures, escape attempts) so P3.2/P3.3 are mechanical. **No production code yet** — the design
pass ADR 0004 asked for, now simpler because the ladder removed the in-process-confinement problem.

#### ✅ P3.1 result — landed 2026-06-24 (ADR 0012 was already accepted; this pass wrote the implementation contract)

ADR 0012's policy was already accepted + ADR 0004 amended in the prior grill-with-docs session
(commit `54b363c`). P3.1's remaining deliverable — the *implementation contract* ADR 0012's own
closing line defers to "the P3.1 design pass" — is now written as
**[`docs/design/confinement-execution-contract.md`](../design/confinement-execution-contract.md)**
(precedent: `hook-mutation-api.md`). **No production code changed.** It pins, grounded against source:

- **The `Confine` signature (the load-bearing call).** The Phase-0 stub `Confine(ctx, box, fn func(ctx)
  error)` **cannot express ADR 0012's subprocess-granularity model** — a backend cannot wrap an opaque
  closure, and the only way a closure *could* confine a child is the per-thread in-process landlock ADR
  0012 deleted (impossible on macOS). So the closure form is **deleted**. Replacement (lands in P3.4):
  `Confine(ctx, box, cmd *exec.Cmd) error` — **prepare-in-place**: the tool builds + runs an idiomatic
  `*exec.Cmd`; the backend rewrites it to launch confined (macOS `sandbox-exec -p` prefix; Linux a
  landlock **re-exec wrapper** via a hidden `__confined-exec` self-subcommand, CGO-free raw `x/sys`
  syscalls) and sets `Setpgid` for process-group teardown. `domain` gains an `os/exec` import (stdlib —
  ADR-0010-clean); `ErrConfinementUnavailable` is the "confine-if-you-can, gate-if-you-can't" safety net.
- **The `workspaceScopedWriter` marker.** An **unexported** interface in `internal/tools` (the only home
  where Apogee's own write tools can satisfy it *and* a third-party module structurally cannot fake it),
  with a `workspaceWriteTarget(call)` seam so dispatch classifies in- vs out-of-workspace *before*
  `Execute`. Detected via `tools.IsWorkspaceScopedWriter` (a **pre-existing** `agent`→`tools` edge —
  `loop.go` already imports it). Rides the tool value through `registry.Subset`, so sub-agents inherit it
  for free. Today only `write_file` carries it (the other 3 built-ins are read-only); P3.7 adds the
  find-replace/patch family.
- **The per-call disposition table (D5)** — the full `(mode × tool-class × confine-to-workspace × caps)`
  grid dispatch computes (P3.4 builds it), dangerous-action guard running first/tighten-only. Flags one
  honest **v1 realisation gap** for P3.7: the "out-of-workspace Apogee write → Approval" row needs the
  write tool to actually perform an *approved* escape (today `resolveInRoot` hard-rejects it); the marker
  seam makes that a later additive change.
- **Capability honesty** (startup probe; `AutoEligible()` → **`FSWrite`-only**, Linux Auto ≥5.13) and the
  **shared escape-probe harness** `internal/platform/confinetest` (`Probe`/`ProbeNetwork`, an 8-row
  battery: in-box write succeeds, out-of-box/`~/.ssh` writes OS-denied, parent stays unrestricted, domain
  inherits across exec, network open-by-default with deny as a tightening) — so P3.2/P3.3 differ only in
  which `Confiner` they pass. Per-backend acceptance checklists are now mechanical.

ADR 0012's closing bullet was updated to point at the contract doc (policy in the ADR, *how* in the
contract). **Next: P3.2** (Linux landlock backend) and P3.3 (macOS seatbelt) — now mechanical against §2.3 + §6.

### P3.2 — Linux landlock backend
Implement the landlock `Confiner` (`//go:build linux`): probe the landlock ABI at startup
(`landlock_create_ruleset` with `LANDLOCK_CREATE_RULESET_VERSION`); report `FSWrite=true` when ABI
≥1 (kernel ≥5.13) and `NetworkEgress=true` **only** when ABI ≥4 (kernel ≥6.7 — an *optional*
tightening now, since Auto's network is open by default per ADR 0012); build a ruleset from
the `ConfinementBox` (workspace-write-only + the `WritablePaths` + **network open by default**, adding a
landlock TCP-connect restriction only when the box opts into network-deny via `NetworkAllow`). Realise the **single subprocess granularity** from ADR 0012:
the child thread applies the landlock domain *after fork, before `execve`* (the domain inherits across
exec), so the spawned process is confined while the parent stays unrestricted. **No in-process
per-thread landlock, no thread-discard** — Apogee's own in-process writes are path-safety-bounded (D1).
**Acceptance (Linux runners):** the shared escape-probe denies an out-of-box write and a
non-allowlisted connect *for a confined subprocess*; the confined subprocess inherits the domain across
exec; `Capabilities()` is honest across a ≥6.7 and a 5.13–6.6 kernel (the latter reports
`NetworkEgress=false` but **`AutoEligible()=true`** — fs-confinement alone satisfies Auto now per ADR
0012; network-egress is an optional tightening); the parent process stays unrestricted after a confined
child runs. Cross-build stays green (the file is `linux`-tagged; other OSes keep `denyConfiner`).

### P3.3 — macOS seatbelt backend
Implement the seatbelt `Confiner` (`//go:build darwin`): generate a `sandbox-exec` profile from the
`ConfinementBox` (deny default; allow file-write under `WorkspaceRoot`/`WritablePaths`; deny network
except `NetworkAllow`), probe `sandbox-exec` presence, and report `{FSWrite:true, NetworkEgress:true}`
when present (else deny-all). Subprocess tools exec under `sandbox-exec -p <profile>` — the **same
single granularity as Linux**, so there is **no macOS in-process asymmetry** (Apogee's own in-process
writes are path-safety-bounded in every mode, D1). **Acceptance (macOS, opt-in like P2.6's live test
— no macOS in the dev env):** the escape-probe denies an out-of-box write and a non-allowlisted
connect for a subprocess tool; `sandbox-exec`-absent ⇒ no fs-confinement ⇒ **subprocess tools gate
through Approval** (Auto is *not* refused — "confine if you can, gate if you can't", ADR 0012); the
generated profile is unit-tested as a pure string from a box (hermetic,
runs everywhere). Cross-build green (`darwin`-tagged).

### P3.4 — The mode ladder + wire `Confine` into dispatch; Auto becomes real
Add **`ModeAllowEdits`** to `domain` and the `--mode` flag (the ladder Plan → Ask-Before →
Allow-Edits → Auto), and rework `needsApproval` into the D5 disposition keyed on
`(mode, effect-kind, workspaceScopedWriter, backend-caps, confine-to-workspace)`. Read the global
**`confine-to-workspace`** flag (ADR 0012; default `true`, global-config-only — a project config cannot
set it `false`). Thread the `Confiner` into the tool executor: in **Auto** with `confine=true`, a
**subprocess/shell** tool with sufficient caps runs inside `Confiner.Confine(ctx, box, …)` (or **gates**
if fs-confinement is unavailable); an **Apogee workspace-scoped write** runs directly path-safety-bounded
if in-workspace (no `Confine`, no Approval) or **raises Approval** if out-of-workspace; **native network
tools** auto-run (network open); **MCP** raises Approval; a **third-party in-process tool** raises
Approval. In **Auto** with `confine=false` everything auto-runs unfenced **except** the dangerous-action
floor (P3.6). In **Allow-Edits**, Apogee's workspace-scoped writes auto-approve and everything unbounded
gates — **no `Confine` call at all** (all-OS). Update `ConfinementCaps.AutoEligible()` to require
**`FSWrite` only** (network no longer gated). `cmd/apogee` now selects the **real** backend for the host
OS (landlock/seatbelt) instead of `denyConfiner`, so `--mode auto` **works** when fs-confinement exists
(Linux kernel ≥5.13) and, when it does not, **gates the subprocess surface** rather than refusing Auto.
The box is built from the injected `WorkspaceDir` + per-project allowlist (config). **Plumb `ExternalEffects` here too:** `executeTool`
currently calls `tool.Execute` directly and never consults `cfg.ExternalEffects` (the seam is declared
on `Config` and documented in `tools.go` but unwired). Route an `ExternalEffectTool` through
`cfg.ExternalEffects.Do(ctx, call)` when `cfg.ExternalEffects != nil` (else live `Execute`), so the
single injectable boundary ADR 0008 promises is real before P3.11 ships the first network tool.
**Acceptance (all `-race`):** a table test covers every ladder row — in Auto/`confine=true` a subprocess
tool runs **without** Approval and **under** `Confine`, an in-workspace Apogee write runs **without**
Approval and **without** `Confine` (path-safety-bounded) while an out-of-workspace one **raises
Approval**, a native `web-fetch` **auto-runs** (no Approval), an MCP tool and a third-party in-process
tool each **raise Approval**; in Auto/`confine=false` all of those auto-run **except** a dangerous-action
(P3.6); in Allow-Edits an Apogee write auto-approves while a `terminal` call gates and **no `Confine` is
invoked**; an out-of-box write from a confined subprocess is denied by the backend (hermetic on Linux);
`--mode auto` on a host with no fs-confinement **gates the subprocess surface** (not refuse), on an
eligible host (kernel ≥5.13) enters Auto. `AutoEligible()` is `FSWrite`-only; `ErrAutoUnavailable` is now
conditional, not constant.

### P3.5 — `processing/` parity finish
Add the remaining tool-call parsers (markdown-fenced, custom-regex) and the **full harmony /
thinking-channel set** behind a `processor-factory` that selects per model/response, each gated by
**ported apogee-code TS test vectors** (the riskiest-port discipline — the TS is the oracle). Keep
the package `domain`-only; the loop selects the processor at the existing adapt-seam. **Acceptance:**
every ported vector passes (golden, ANSI-/whitespace-normalised as the TS asserts); a malformed
payload in any format degrades to the parse-error path (never a panic, never a Turn failure — the
P1.3 contract); the factory picks the right parser for native vs fenced vs regex models; the bench
re-run shows no parsing regression. Record the parity result in the commit.

### P3.6 — Security guardrails (`internal/security`)
Build the human-in-the-loop guardrail layer (D6), distinct from the `Confiner` sandbox: **consolidate**
the Phase-1 per-tool path-safety into one reusable, symlink-aware guard; add **url-safety** (scheme/
host allow-deny for `web-fetch`/`http-request`), the **dangerous-action guard** (below), a
**circuit-breaker** (halt a runaway repeated-tool / tool-loop), and an **audit** record (append-only
tool-call log). Wire them through the tool executor so all tools — and sub-agents (D2) — inherit them.

The **dangerous-action guard** (ADR 0012; the renamed "denylist") is a **footgun-guard, NOT a security
boundary** — it catches a small model's obvious catastrophic *mistakes*, in **every** mode, before
execution, independent of the Confiner, and is **tighten-only** (runs ahead of the mode disposition; can
only make a call stricter). Membership: *almost-never-legitimate* **and** *catastrophic/compromising*
(precision-over-recall — never block `rm -rf ./build`). **Two tiers:** **Tier-1 hard-refuse** (`rm -rf`
of a root/home/system path, fork bombs, writes to `~/.ssh`/credential/persistence files — clear
`ToolResult` error, **no** per-call override) and **Tier-2 force-approval** (`curl | bash`-class — a
legit installer idiom, so a speed-bump that forces the Approver even in Auto; `nil` Approver ⇒ refuse).
Matching is deliberately simple (narrow, whitespace-normalized literal/regex — **no** obfuscation-chasing;
that is the adversary game this explicitly is not). Default-on; the **global** config may add *or* remove
entries (it is the user's machine — this is a footgun-guard, not a boundary), a **project** config may
only *add*. It **never** makes `confine-to-workspace=false` "safe" (only the VM does).

**Acceptance:** table tests for each guard (path traversal rejected; a denied url blocked; a Tier-1
action refused with a clear `ToolResult` error in **Plan/Ask-Before/Allow-Edits/Auto alike**, before
execution and independent of the Confiner; a Tier-2 action forces Approval even in Auto and refuses on
`nil` Approver; a near-miss like `rm -rf ./build` is **not** blocked — precision; the breaker trips after
N identical failing calls and surfaces an `ErrorEvent`, not a crash); the audit log records
call/decision/result; guardrails run in **all** modes (not just Auto). Path-safety parity with the
Phase-1 tools (no regression on the 4 built-ins).

### P3.7 — File-editing tool family
The pure-Go, stateless editing tools (ADR 0008): **find-replace** single + multi (literal + anchored,
the apogee-code semantics), **`diff`** (a small in-package myers diff — no external), **`patch`/
apply-edit** (apply a unified-diff/edit-block to a file under path-safety), **`open-file`** (read +
locate, the editor-affordance read tool). Each scoped to the sandbox root, path-safe (via P3.6),
`ReadOnly()` where applicable (open-file/diff read-only; find-replace/patch are writes). The write
tools carry the unexported **`workspaceScopedWriter`** marker (D1/D5) — Apogee's own
path-safety-bounded writes. **Acceptance:** golden round-trips (find-replace edits the right span and
only it; patch applies + rejects a non-applying hunk cleanly; diff is stable/deterministic); a
path-escape is rejected by the guard (error result, every mode); the write tools **gate in
Ask-Before, auto-approve in Allow-Edits, and run path-safety-bounded (no `Confine`, no Approval) in
Auto** (P3.4 disposition); statelessness holds (no handle survives the call). TS-oracle parity for
find-replace/patch semantics where vectors exist.

### P3.8 — Execution tools (`terminal`, `python-exec`)
The first real `Confiner` consumers and the first `Shell`-seam wideners. Both **one-shot / stateless**
(ADR 0008 — fresh process per call, process-group kill, no persistent shell/REPL): `terminal` runs a
command line (`shlex`-split) via the `platform.Shell`; `python-exec` runs a script via a detected
interpreter (§3a — absent ⇒ graceful "python not found", never a hard dep). Widen the `Shell`/`Path`
seam as needed (PATH lookup, env-scoped exec, process-group kill, timeout). In Auto they run **under
`Confine`** (subprocess granularity, D1); arg-guarded + audited (P3.6). **Acceptance:** a command runs
and its output/exit is captured; a timeout/cancel kills the process group cleanly (no orphan); in Auto
an out-of-workspace write from the child is OS-denied (Linux hermetic), a non-allowlisted network
reach denied; `python-exec` degrades gracefully when no interpreter is present; statelessness holds.

### P3.9 — `git` tool
Branch / commit / diff-range over the **system** `git` (§3a — detected on PATH, graceful "git not
available" when absent — never a hard dep; this is a *convenience* dep, not inherent). Path-safe to
the workspace; arg-guarded; in Auto runs under `Confine` (subprocess) or Approval-gates if the box
can't be established. **Acceptance:** branch/commit/diff-range produce correct output against a
`t.TempDir()` repo; absence of `git` degrades to a clear unavailable result (not a crash); writes
(commit) gate/confine per mode; no network git op runs unconfined in Auto.

### P3.10 — `diagnostics` tool
In-process for Go — `go/parser` for syntax + the `go vet` that ships with the toolchain — and
**optional** shell-out linters (`tsc`, etc.) for other languages, **detected + graceful-degrading**
(§3a — an *enhancement*, never required). Read-only. **Acceptance:** a Go file with a syntax error /
a vet finding is reported in-process (no external dep); a non-Go file with no available linter returns
a clear "no diagnostics available" (not an error); the tool is `ReadOnly()` (runs in Plan).

### P3.11 — Network + host tools
**`web-fetch`** (stdlib `net/http` GET with url-safety), **`http-request`** (general request, url-
safety + arg-guard), **`web-search`** (against a **config'd, default-off** search endpoint — no
hard-wired provider; absent config ⇒ unavailable, not a crash) — all marked **`ExternalEffectTool`**
(effect kind **`network`**). Per **ADR 0012** the Auto disposition keys on the **effect *kind***, not the
bare interface: **`network` tools auto-run in Auto** (url-filtered — the network is open; they no longer
gate), while only **`mcp`** kind gates under `confine-to-workspace=true`. The `ExternalEffectTool` marker
*still* routes **both** kinds through the single **bench-stubbable** external-effect boundary (ADR 0008) —
the stub purpose and the gating purpose have diverged and must be keyed separately. Plus **`ask-user`**: a tool that asks the human
a question mid-task, routed through a **new `Asker` host delegate** on `Config` (a deliberate v1
surface addition, D7) — **distinct from `Approver`** (free-text Q&A, not a safety-gate enum). Pin its
**freeze-aware shape**: `Ask(ctx, AskRequest) (AskAnswer, error)` with `AskRequest{Question string}` /
`AskAnswer{Text string}` for v1 — **structs, not bare strings**, so a post-v1 multiple-choice field
(`Choices`/`Choice`) is an *additive* change, not a breaking one. `ask-user` is **`ReadOnly()` (runs
in Plan), mode-independent (always routes to the `Asker`, never through the Approval gate — and it is
**not** an `ExternalEffectTool`), and **blocks the worker goroutine via the C-seam** (ADR 0011) like
`Approver`; `nil` Asker ⇒ the tool is not registered (graceful). The TUI implements it as an input
prompt (analogous to the approval-prompt flow); the bench as a canned/scripted responder.
**Acceptance:** web (`network`-kind) tools **auto-run in Auto** (no Approval) but are **url-safety
filtered** (a denied host is blocked) and still **bench-stubbable** (the stub returns a fixed result with
no network); an `mcp`-kind tool still Approval-gates in Auto under `confine=true` (asserted in P3.15);
`ask-user` round-trips a question→answer through the delegate (TUI prompt; bench script) **and is callable
in Plan without Approval**; resume makes no network promise (ADR 0008).

### P3.13 — Sub-agent orchestrator + ADR 0013
Build `internal/agent/subagent` per D2: construct a nested `Agent` threading the parent's `Mode` /
`Approver` / `Confiner` / guardrails (or stricter) and a `registry.Subset(names…)` **≤ the parent's**
tools; expose it as the **`sub_agent` tool**; re-emit nested events at **`Depth = parent+1`**; drive
it **top-level-only** behind a swappable driver (broad plan #15). Land **ADR 0013** recording the
shape (and confirming the schema "leaves room for a suspended sub-agent" so nested stepping is a
later additive change). **Acceptance (all `-race`):** a Plan-parent sub-agent cannot write; a
`Subset`-narrowed sub-agent cannot call a tool the parent has but the subset omits; an Auto sub-agent
confines a child subprocess tool, runs a child Apogee write path-safety-bounded, and still
Approval-gates child MCP/external tools (the per-call disposition, one level down); nested events
arrive at `Depth==1`; a sub-agent panic recovers at the parent boundary (ADR 0007) without killing
the parent Exchange; **a cancel during a sub-agent rolls back the whole parent Turn — the parent is
resumable from the pre-`sub_agent` quiescent boundary with byte-identical state, and no snapshot
contains suspended sub-agent state** (atomic-within-the-parent-Turn, D2).

### P3.14 — TUI `Depth > 0` rendering
Turn the Phase-2 *tolerate* into *render*: frame/indent nested sub-agent events as a visually distinct
block in the transcript (a labelled, indented sub-section per sub-agent), keeping the C6 fold rules
per depth. No agent logic (ADR 0011 still holds — render only). **Acceptance:** a recorded nested event
sequence (`Depth 0 → 1 → 0`) renders with the sub-agent block indented/labelled and the parent stream
intact (golden); reflow at small sizes doesn't panic; the existing flat (`Depth==0`) goldens are
unchanged.

### P3.15 — MCP client
Build `internal/mcp` on the official Go SDK (pin from P3.0): connect over stdio / SSE / streamable-http
from config; **surface each server tool into the `ToolRegistry` as an `ExternalEffectTool`** (effect
kind `mcp`) so D3/D5 gate it through Approval in Auto under `confine=true` **for free**; **resume reconnects fresh**
(ADR 0008 — no server-side-state promise); clean shutdown on `Close`. Record the client shape (ADR
0014 or a design note — D3). **Acceptance:** a hermetic stdio MCP server (a test fixture) exposes a
tool that appears in the menu, is callable, and **raises Approval in Auto** (asserted); a resumed
session re-establishes the connection from scratch; the bench swaps a deterministic stub with no
process; `Close` tears down the server cleanly (no orphan). Cross-build green (the SDK is pure-Go).

### P3.16 — Phase-3 acceptance + cut `v1.0.0`
The deliverable proof + the freeze. **(1) Feature-parity:** the bench (apogee-sim) drives the full
tool suite against the TS-oracle behaviour and shows parity on the non-UI surface; the hermetic e2e
(extending P2.6's harness) exercises a sub-agent + an MCP tool + a confined Auto subprocess write.
**(2) Live Auto-confined run** (opt-in, `APOGEE_LIVE_ENDPOINT`, like P2.6): a real coding conversation
against a live local model in **Auto** mode — confinement enforced (a **shell/subprocess** write
outside the workspace OS-denied, an MCP tool still raising Approval), a sub-agent delegated and its
nested work rendered — on **Linux** (landlock, runnable in the dev env) and **macOS** (seatbelt,
owner-run). **(3) Freeze + tag:** review
every public symbol added this phase against D7, freeze the facade, **tag `v1.0.0`**, and amend ADR
0001 §18 to record that semver now begins (Events/hook-points stay additively extensible). **Acceptance:**
the full verify gate green; the bench parity run passes; the live Auto-confined run completes on Linux
(macOS owner-confirmed); `v1.0.0` tagged; ADR 0001 amended. **Phase 3 is complete.**

---

## 5. Open design calls to resolve *within* Phase 3 (→ ADRs / notes)

Record each as it lands (don't pre-decide in the abstract):

- **Confinement execution model → ADR 0012** (settled by **P3.1**, before any backend) — the
  blast-radius invariant, the Allow-Edits ladder rung, the single (subprocess) confinement
  granularity, the per-call decision, capability honesty; **refines ADR 0004** (§3 D1). The
  load-bearing call; ADR 0004 explicitly asked for this dedicated pass.
- **Sub-agent orchestrator shape → ADR 0013** (settled by **P3.13**) — privilege threading, the
  `sub_agent` tool, top-level-only swappable driver, `Depth+1` nesting (§3 D2; realises ADR 0005).
- **MCP client integration → ADR 0014 or a design note** (settled by **P3.15**) — transports, tool
  surfacing as `ExternalEffectTool`, reconnect-on-resume; the *gating* decision is already ADR
  0004/0008 (§3 D3).
- **`processing/` parity** (settled by **P3.5**) — a port, not a redesign; ported TS vectors are the
  gate; an ADR only if a format forces a structural call (§3 D4).
- **The `ask-user` host delegate** (settled by **P3.11**) — a new `Asker` on `Config`, the public
  analogue of `Approver`; a deliberate v1-surface addition reviewed at the freeze (§3 D7).
- **`v1.0.0` API freeze + ADR 0001 §18 amendment** (settled by **P3.16**) — what the frozen surface
  is, and the semver-begins record.

### ✅ Resolved 2026-06-24 (grill-with-docs) — settled into ADR 0012 + CONTEXT.md

Both reopened calls were settled in a grilling session and written into
**[ADR 0012](../adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md)**
(which **supersedes ADR 0004**) and the CONTEXT.md Agent-mode / Confinement / Dangerous-action-guard
entries. **ADR 0012 is the source of truth; where §3 D1/D5 below predate it on the network / kernel /
web-tool / MCP specifics, ADR 0012 wins** (the surviving D1/D5 frame — blast-radius, the
`workspaceScopedWriter` marker, the single subprocess granularity — is unchanged). Summary:

- **Auto strictness → the `confine-to-workspace` flag** (global-config key, default `true`; meaningful
  only in Auto). **`true`:** subprocess surface OS-fenced to the workspace (escape = OS-blocked, no
  prompt), Apogee's own out-of-workspace in-process write raises **Approval**, **network open**
  (subprocess net + native `web-fetch`/`http-request` auto-run, url-filtered), **MCP gates** (server-grain
  "allow for session"); if fs-confinement is *unavailable*, subprocess tools **gate** ("confine if you
  can, gate if you can't"). **`false` ("I am the sandbox"):** nothing fenced/gated except the
  dangerous-action floor — **VM-only**, global-config-only (a project config cannot loosen it), with a
  per-session startup warning. **`AutoEligible()` drops to fs-confinement only** → Linux Auto now needs
  kernel **≥5.13** (not ≥6.7); network-egress confinement is an optional tightening. The 4-mode ladder is
  unchanged (the unconfined opt-in is a *flag on Auto*, not a 5th rung).
- **Dangerous-action guard** (the renamed "denylist" — a **footgun-guard, NOT a security boundary**;
  folds into **P3.6**). Both-(a)-never-legit-**and**-(b)-catastrophic membership, precision-over-recall.
  **Two tiers:** *hard-refuse* (`rm -rf` of root/home/system, fork bombs, `~/.ssh`/credential/persistence
  writes — clear `ToolResult` error, no per-call override) and *force-approval* (`curl | bash`-class —
  forces the Approver even in Auto). **Tighten-only**, runs before the mode disposition, independent of
  the Confiner, all modes. Default-on; global config may add *or* remove, project config may only *add*.
  It is trivially bypassable and **never** makes `confine=false` "safe."
- **Deferred to [`TODO.md`](../../TODO.md):** the user-configurable **tool × mode security matrix**
  (post-v1, additive, **tighten-only**) and the related command-pattern / per-host allowlist precision
  knobs. v1 ships the *internal* disposition table + the `confine-to-workspace` flag + the existing
  narrow allowlists.

---

## 6. Out of scope for Phase 3 (explicit non-goals)

- **The Mechanism catalogue, self-regulation, and the catalogue→hook mapping** — **Phase 4** (its own
  sim-data session first). Phase 3 adds **no Mechanism**; `MechanismFiredEvent` stays behind the TUI
  debug view; the registry keeps only cycle-detection (the deterministic topo-sort + Adaptive
  Suppression + Turn Budget + Effectiveness tracking are Phase 4).
- **The Library** (cross-session per-model learning, `ModelFingerprint`, `apogee probe`) — **Phase 4**.
- **Context reducers beyond what exists** (Budget allocation, generative Compaction, tool-result
  capping, token counting) — **Phase 4** (the four-way split).
- **Windows confinement + Windows shell/path backend** (AppContainer / Job Objects / restricted
  tokens) — **Phase 5**. Phase 3 keeps the cross-build green via the `denyConfiner` + Windows-stub
  fallbacks; Auto is simply unavailable on Windows until Phase 5.
- **Nested sub-agent stepping** (suspend/resume a sub-agent at its own boundary) — later; Phase 3 is
  top-level-only behind a swappable driver, and the snapshot schema leaves room (broad plan #15).
- **`apogee headless` / `apogee probe`** — headless is an *optional* scripting surface (Phase 4/5,
  not the bench contract — ADR 0001); `probe` is Phase 5 (doubles as fingerprint).
- **Record/replay for external-effect tools** — deferred behind the injectable stub seam (ADR 0008);
  Phase 3 ships the stub boundary + deterministic stubs, not record/replay.

---

## 7. Acceptance-criteria summary (quick gate)

A reviewer can check Phase 3 with:

```
gofmt -l .                          # empty
go vet ./...                        # clean
go build ./...                      # ok
go test -race ./...                 # tools + processing parity + confinement probes + sub-agent + MCP + security
grep -rl '"github.com/airiclenz/apogee"' internal/   # empty (ADR-0010; incl. mcp/security/subagent/tools)
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build ./...   # + the other 5 cross targets (OS confinement build-tagged)
go mod tidy                         # no drift; x/sys + shlex + go-sdk justified
./apogee --help                     # cobra usage, exit 0

# the opt-in live confirmation (a tool-capable local model up; Auto on Linux is hermetic-confinable):
APOGEE_LIVE_ENDPOINT=http://192.168.64.1:1111 go test -race -count=1 -run TestE2ELiveModel -v ./internal/tui/
```

…plus the **deliverable**: a real coding conversation with a **live local model** in **Auto** mode —
tokens stream, subprocess tools run **confined** (a shell/subprocess write outside the workspace is
OS-denied; an MCP tool still raises Approval), a sub-agent is delegated and its nested work renders,
the Exchange completes —
driven entirely over the (now-frozen) public API, with `internal/tui` holding no agent logic. The
hermetic e2e + the bench parity run are the reproducible proofs; the live Auto-confined run (Linux in
the dev env; macOS owner-run) is the final confirmation. **`v1.0.0` is tagged and ADR 0001 §18 amended.**

---

## 8. Suggested skills

- **`Plan`** / **`/grill-me`** / **`grill-with-docs`** — pressure-test **§3 D1 (confinement model)**,
  **D2 (sub-agent shape)**, and the **task order** before P3.1 commits ADR 0012. These are the calls
  that, if wrong, cascade through every tool. ADR 0004 itself asked for this dedicated design pass.
- **`/coding-standards`** (`go`) — **mandatory** for every Go body here (`coding-standards.go.md` +
  `testing.go.md`); the package idiom (section dividers + symbol-first doc comments) wins over the
  base rule, and the plan/Go/SDK idiom wins where it fights a standard (TDD §9).
- **`/code-review`** — at minimum after the confinement pillar (P3.1–P3.4) and again before the
  `v1.0.0` cut (P3.16); the confinement + sub-agent + MCP code is the highest-stakes in the build.
- **`/security-review`** — before the freeze: the guardrails (P3.6), the confinement backends, and
  the network/MCP tools are exactly the security-sensitive surface this skill targets.
- **`manage-llm-server`** / the llama-launcher MCP at **`http://192.168.64.1:7331/mcp`** — to load a
  tool-capable model (gpt-oss-20b / Qwen3.6-27B / Gemma-4) for the P3.16 live Auto-confined run.
- **`/handoff`** at session end; **`archive-handoffs`** — handoff 18 is consumed once P3.0 lands.
```
