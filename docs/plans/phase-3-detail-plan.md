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
| **P3.4** ✅ | **Mode ladder + wire `Confine` into dispatch; Auto becomes real** (D5): add **`ModeAllowEdits`** (Plan→Ask-Before→Allow-Edits→Auto); rework `needsApproval` into the blast-radius disposition; `ErrAutoUnavailable` now conditional. **Also plumb the `ExternalEffects.Do` boundary** (ADR 0008) — currently declared on `Config` but never called; dispatch must route `ExternalEffectTool`s through it when set, so the bench-stub story (P3.11/P3.15/P3.16) is real before the first external tool ships. **Done 2026-06-24** — see result note below | P3.2, P3.3 | — | ADR 0004; ADR 0008; ADR 0012; dispatch.go |
| **P3.5** | **`processing/` parity finish** (D4): markdown-fenced + custom-regex parsers + full harmony channel set + `processor-factory`, TS-vector-gated | P3.0 | — | TDD §5 processing; broad §4 |
| **P3.6** | **Security guardrails** `internal/security` (D6): consolidate path-safety + url-safety + arg-guard + circuit-breaker + audit | P3.0 | — | broad §4; TDD §5 security |
| **P3.7** | **File-editing tool family**: find-replace (single + multi), `diff`, `patch`/apply-edit, `open-file` — pure-Go, stateless; carry the `workspaceScopedWriter` marker | P3.6, P3.4 | — | ADR 0002/0008; D1/D5; broad §4 tools |
| **P3.8** | **Execution tools**: `terminal` + `python-exec` (one-shot, stateless; first `Confiner` consumers; widen the `Shell` seam) | P3.4, P3.6 | `github.com/google/shlex` | ADR 0008; ADR 0012; §3a |
| **P3.9** | **`git` tool** (branch/commit/diff-range): system-`git` shell-out, §3a detected + graceful-degrading | P3.6, P3.8 | — | §3a; broad §4 tools |
| **P3.10** | **`diagnostics` tool**: in-process `go/parser` + `go vet` for Go; optional shell-out linters for other langs, graceful | P3.6, P3.8 | — | §3a; broad §4 tools |
| **P3.11** | **Network + host tools**: `web-fetch`, `web-search`, `http-request` (external-effect, Approval-gated in Auto, bench-stubbable) + `ask-user` (new `Asker` host delegate) | P3.6 | — | ADR 0008; D3; D7 |
| **P3.12** | *(reserved — folded into P3.6; kept for ID stability if guardrails split)* | — | — | — |
| **P3.13** ✅ | **Sub-agent orchestrator + ADR 0013** (D2): privilege threading, `Subset` tool set, top-level-only swappable driver, `Depth+1` event nesting, the `sub_agent` recursion point; **isolated live guard state, shared read-only dangerous floor** (`Guards.ForSubAgent`). **Done 2026-06-24** — see result note below | P3.7–P3.11, P3.4 | — | ADR 0005; **ADR 0013** |
| **P3.14** | **TUI `Depth > 0` rendering**: nested-event framing/indentation (Phase-2 "tolerate" → "render") | P3.13 | — | ADR 0011; TDD §5 TUI |
| **P3.15** | **MCP client** on the official Go SDK (stdio/SSE/streamable-http): surface server tools as `ExternalEffectTool`, Auto-gates-MCP, resume reconnects fresh | P3.4, P3.6 | `…/go-sdk` | ADR 0004/0008; D3 |
| **P3.16** | **Phase-3 acceptance + cut `v1.0.0`**: feature-parity vs apogee-code non-UI + bench; live Auto-confined run (Mac + Linux); freeze + tag + amend ADR 0001 §18 | all | — | broad §4 deliverable; ADR 0001 §18 |

> **On P3.12:** guardrails are a single task (**P3.6**); P3.12 is left reserved so the IDs don't
> renumber if a reviewer later splits audit/circuit-breaker out. Treat the live list as P3.0–P3.11,
> P3.13–P3.16.

#### ✅ Hardening pass result — landed 2026-06-24 (in-pillar `/code-review` findings closed before P3.5; gate GREEN)

The confinement pillar's consolidated `/code-review` findings (no dedicated task ID — the findings
*are* the spec). Closed **all 7**:

1. **[High] tighten-only dangerous-rule merge** (`security/rules.go`): `MergeDangerousRules` project-adds
   are now tighten-only — a same-ID project add replaces an existing rule **only if strictly stricter**
   (higher `Tier`); an equal-or-lower-tier same-ID add is **dropped**, so a hostile/careless repo can no
   longer replace-by-ID to dissolve or loosen a Tier-1 floor rule. Global adds keep their trusted
   replace-in-place semantics. Test: `TestMergeDangerousRules_ProjectCannotDissolveFloorByID` (lower-tier
   and neutered-equal-tier both rejected).
2. **[Med] fail-closed net-deny on landlock ABI<4** (`landlock_linux.go`): when a box opts into
   network-deny (`NetworkAllow` set) but the kernel can't enforce network rules (ABI<4), `applyLandlock`
   now **fails closed** (returns `ErrConfinementUnavailable`) instead of silently running network-open —
   so dispatch's "confine-if-you-can, gate-if-you-can't" net routes the call to Approval. Decision is
   **consistent with ADR 0012** (deny is a tightening the box requested; the network-OPEN default is
   unaffected) — no ADR change needed. Extracted `networkDenyDecision(box, abi)` as a pure helper; test
   `TestNetworkDenyDecision` (open/deny × enforceable/unenforceable).
3. **[Med] bounded audit log** (`security/audit.go`): `AuditLog` is now a **ring buffer** (`DefaultAuditCap
   = 10000`) with a `Dropped()` count so overflow is observable; `Records()` returns the most-recent
   window in append order. `NewAuditLogWithCap` added for small-ring tests. Tests:
   `TestAuditLog_CapEvictsOldestAndCountsDropped`, `TestAuditLog_UnderCapDropsNothing`.
4. **[Med] dead-code cleanup**: deleted the duplicated `internal/tools/path_safety_test.go` (canonical is
   `internal/security/pathsafety_test.go`) and removed the orphaned `evalRealPath` alias from
   `internal/tools/path_safety.go`.
5. **[Med] `confinetest` uses `domain.Confiner`**: retired the stale local `Confiner` interface in
   `internal/platform/confinetest`; `Probe`/`ProbeNetwork` now take `domain.Confiner` directly; dropped the
   "until P3.4" comments.
6. **[Med] dead `PreCheck.Decision`** (`security/guard.go`): confirmed never read (only written), removed.
7. **[Med] hermetic tests added**: nil-Confiner Auto → `ErrAutoUnavailable`
   (`TestAutoConstruction_NilConfinerRefused`); present-but-incapable Confiner Auto → constructs
   (`TestAutoConstruction_IncapableConfinerConstructs`); `ApplyLandlockAndExec` empty-argv refusal
   (`TestApplyLandlockAndExecRejectsEmptyArgv`); marker accessors false/empty for a non-marker tool
   (`TestMarkerAccessors_NonMarkerTool` + positive contrast `TestMarkerAccessors_MarkerTool`).

**Verify gate (§7) — all green:** `gofmt -l .` empty · `go vet ./...` + `GOOS=darwin GOARCH=arm64 go vet
./...` clean · `go build ./...` ok · `go test -race ./...` all `ok` · ADR-0010 grep empty · 6 cross-builds
OK (`CGO_ENABLED=0`) · `go mod tidy` no drift · `apogee --help` exit 0. The landlock **enforcement**
battery (`confinetest.Probe`) self-skips on this kernel (`CONFIG_SECURITY_LANDLOCK` off) as expected — the
new logic tests use injected ABI / pure helpers and run regardless.

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

#### ✅ P3.2 result — landed 2026-06-24 (landlock backend + shared escape-probe harness; gate GREEN; enforcement battery self-skips on this landlock-disabled host)

The Linux landlock backend is implemented mechanically against the contract §2.3 + §5 + §6.
**Decision: raw `golang.org/x/sys/unix` syscalls, NOT the `go-landlock` helper** — the raw surface
(`unix.Syscall(unix.SYS_LANDLOCK_*, …)` over `LandlockRulesetAttr`/`LandlockPathBeneathAttr`) proved
ergonomic enough at this scale (one create + N add-rule + restrict_self), keeping the single static
artifact dep-lean (§3a); the helper was not needed. **What landed:**

- **`internal/platform/landlock_linux.go`** (`//go:build linux`, CGO-free):
  `NewLandlockConfiner()` probes the ABI once via `landlock_create_ruleset(NULL,0,VERSION)`
  (`probeLandlockABI`, l.~96); `Capabilities()` (l.~120) is honest — `FSWrite` at ABI≥1, `NetworkEgress`
  at ABI≥4, `{false,false}` on a kernel without landlock. `Confine(ctx, box, *exec.Cmd)` (l.~138 — the
  **prepare-in-place** signature the contract gives P3.4; exposed here as a concrete method, the
  `domain.Confiner` interface keeps its closure form until P3.4) rewrites `cmd` into the re-exec wrapper
  `[self, "__confined-exec", <base64-JSON box>, "--", <orig argv…>]` and sets `Setpgid` for
  process-group teardown (§2.4). `ApplyLandlockAndExec(box, argv)` (l.~183) is the in-child half:
  `applyLandlock` builds the ruleset (write-class FS accesses handled, re-granted beneath
  `WorkspaceRoot ∪ WritablePaths`; `NO_NEW_PRIVS` then `landlock_restrict_self`), then `syscall.Exec`s
  the real argv — landlock inherits across `execve`, parent never restricted. `encodeBox`/
  `DecodeConfinedBox` + `ConfinedExecSentinel()` are the inline-argv seam for the P3.4 `main` dispatcher.
- **`internal/platform/confinetest/`** — the shared escape-probe harness (`Probe`/`ProbeNetwork`, l. in
  `confinetest.go`) driving the §6.2 8-row battery (#1/#2 in-box & writable-path writes succeed; #3/#4
  out-of-box & `~/.ssh` writes OS-denied; #5 parent stays unrestricted; #6 domain inherits across a
  nested `sh -c` exec; #7 network-deny connect denied; #8 network-open connect allowed). Parameterised
  over a local `Confiner` interface (the `*exec.Cmd` shape) so P3.3 seatbelt reuses it unchanged; imports
  only `internal/domain` (ADR-0010-clean).
- **`internal/platform/landlock_linux_test.go`** — `TestMain` dispatches the `__confined-exec` sentinel
  (the standard `TestHelperProcess` idiom) so the test binary is its own confined child;
  `TestLandlockCapabilitiesHonest` table-tests caps across ABI −1/1/3/4/6 (the **5.13–6.6 kernel reports
  `NetworkEgress=false` with fs-confinement still available** → Auto-eligible per ADR 0012);
  `TestLandlockConfineRewritesCmd` asserts the re-exec argv shape + `Setpgid`; round-trip + sentinel tests.
- **`internal/domain/errors.go` + `apogee.go`** — added the **`ErrConfinementUnavailable`** sentinel (the
  "confine-if-you-can, gate-if-you-can't" safety net, §2.2) and re-exported it at the root. This is a
  pure additive `var` — it does **not** touch the `Confiner` interface signature (that change is P3.4) —
  needed now so the backend can honestly signal an unestablishable box.
- **`go.mod`/`go.sum`** — `golang.org/x/sys v0.45.0` promoted **indirect → direct** (now used by
  production code); `go mod tidy` clean (also dropped a few stale transitive `/go.mod`-only sum lines).

**Capability-honesty finding (this dev host):** the kernel is built **`# CONFIG_SECURITY_LANDLOCK is not
set`** (the boot cmdline lists `landlock` in `lsm=…` but the LSM is compiled out), so
`landlock_create_ruleset` returns `ENOSYS` and `Capabilities()` honestly reports `{false,false}`. The
enforcement battery (`Probe`/`ProbeNetwork`) therefore **self-skips** with a clear reason (standard
kernel-feature-gated idiom) — the OS-denial assertions (#3/#4/#6/#7) are unrunnable *here* but compile
and run for real on a landlock-enabled runner. The pure-logic acceptance (honest caps across ABIs, the
re-exec argv rewrite, box round-trip, parent-unrestricted) **does** run and passes here. *(The §0 entry
note's "fully testable hermetically here" did not hold for this specific sandbox kernel; the contract's
capability-honesty rule is exactly what absorbs that — the backend degrades to "gate", never to a false
"confined".)*

**Verify gate (§7) — all green:** `gofmt -l .` empty · `go vet ./...` clean · `go build ./...` ok ·
`go test -race ./...` all `ok` (no FAIL/panic/DATA RACE; landlock enforcement subtests SKIP on this host)
· ADR-0010 grep empty · 6 cross-builds OK (linux file `//go:build linux`-tagged; darwin/windows keep
`denyConfiner`) · `go mod tidy` clean (x/sys direct) · `apogee --help` exit 0. **Next: P3.3** (macOS
seatbelt, reuses `confinetest`) and **P3.4** (mode ladder + `Confine` into dispatch — adopts the
`*exec.Cmd` interface signature, wires the `__confined-exec` dispatcher in `main`).

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

#### ✅ P3.3 result — landed 2026-06-24 (seatbelt backend reuses `confinetest`; gate GREEN; profile + caps tests hermetic on Linux; macOS live-probe deferred to owner/CI)

The macOS seatbelt backend is implemented mechanically against the contract §2.3 + §5 + §6 and ADR
0012, mirroring the P3.2 landlock pattern (a concrete prepare-in-place `Confine(ctx,box,*exec.Cmd)`
+ honest `Capabilities()` driven by the shared `confinetest` harness; the `domain.Confiner` interface
change stays P3.4). **No new dep** — seatbelt shells out to the system `sandbox-exec`. **What landed:**

- **`internal/platform/seatbelt.go`** (`//go:build !windows`, host-agnostic): `seatbeltConfiner` with
  the shared `newSeatbeltConfiner(present)` constructor; `Capabilities()` honest — `{true,true}` when
  `sandbox-exec` is present (one profile enforces both fs-write + network), `{false,false}` when absent;
  `Confine` generates the profile and rewrites the cmd to `sandbox-exec -p <profile> <orig…>` + sets
  `SysProcAttr.Setpgid` (process-group teardown, §2.4), returning `ErrConfinementUnavailable` when
  `sandbox-exec` is absent (the "confine if you can, gate if you can't" net, §2.2); `seatbeltProfile(box)`
  is the **pure** profile generator — `(allow default)` then `(deny file-write*)` then
  `(allow file-write* (subpath …))` for `WorkspaceRoot ∪ WritablePaths`, empty roots skipped.
- **`internal/platform/seatbelt_darwin.go`** (`//go:build darwin`): only `NewSeatbeltConfiner()` and the
  real `os.Stat("/usr/bin/sandbox-exec")` presence probe — the lone darwin-tagged surface, so the
  profile/caps/rewrite logic compiles and unit-tests on Linux. **Build-tag decision:** `seatbelt.go` is
  `!windows`, not `darwin`-only, because `SysProcAttr.Setpgid` is POSIX-only (absent on Windows, where
  only `denyConfiner` exists — Phase 5); on Linux the type is compiled but never *selected* (P3.4 picks
  it on darwin only).
- **`internal/platform/seatbelt_test.go`** (`//go:build !windows`, runs on Linux): hermetic profile-string
  battery (in-workspace write allowed, out-of-workspace denied, `WritablePaths` honoured, empty-root
  skip, network-open default, network-deny tightening, quote/backslash escaping), caps-honesty table
  (absent⇒`{false,false}`⇒not Auto-eligible; present⇒`{true,true}`) via the injectable `present` seam (no
  real macOS needed), and the `Confine` cmd-rewrite (profiler path, `-p`, profile==pure-fn, argv,
  `Setpgid`, empty-argv reject, `ErrConfinementUnavailable` when absent).
- **`internal/platform/seatbelt_darwin_test.go`** (`//go:build darwin`): wires `confinetest.Probe`/
  `ProbeNetwork` against the real `sandbox-exec` child — the §6.2 escape battery — runnable only on a
  macOS runner. **`internal/platform/seatbelt_notdarwin_test.go`** (`//go:build !darwin && !windows`):
  same test names that **`t.Skip` LOUDLY** with a clear reason (macOS-only, deferred to owner/CI), so the
  deferral is visible in `go test` output on this Linux host.

**Network-open reconciliation (older P3.3 task text vs ADR 0012):** the task section above says "deny
network except `NetworkAllow`"; **ADR 0012 wins** — the network is **open by default** in Auto. The
generated profile therefore emits **no** network clause for the default box (network open) and adds a
single coarse `(deny network*)` clause **only** when the box opts into network-deny via a non-empty
`NetworkAllow` — exactly matching landlock's deny-all-TCP tightening (per-host allow is a later additive
change once a finer filter is wired, mirroring landlock's deferred per-port rule).

**macOS live escape-probe — deferred to owner/CI (not fakeable, not deleted).** The dev host is
`linux/arm64`; there is no macOS / `sandbox-exec` here, so the live battery (a real confined child whose
out-of-box write must be OS-denied and whose non-allowlisted connect must be denied while an open-network
connect succeeds, §6.2 #1–#5/#7/#8) is **owner-run / CI-only on a darwin runner**. It is wired in
`seatbelt_darwin_test.go` and **self-skips loudly with a clear reason on non-darwin**
(`seatbelt_notdarwin_test.go`), the same kernel-feature-gated idiom P3.2's landlock battery uses on this
landlock-disabled host. The hermetic profile-string + caps/presence acceptance **does** run and passes
here.

**Verify gate (§7) — all green:** `gofmt -l .` empty · `go vet ./...` clean (also `GOOS=darwin go vet`
and `GOOS=windows go vet` clean — the darwin file type-checks) · `go build ./...` ok · `go test -race
./...` all `ok` (seatbelt live-probe SKIPs loudly on this host; hermetic profile/caps tests PASS) ·
ADR-0010 grep empty · 6 cross-builds OK (`seatbelt.go` `!windows`-tagged, `NewSeatbeltConfiner`
`darwin`-tagged; windows keeps `denyConfiner`) · `go mod tidy` clean (no new dep) · `apogee --help` exit
0. **Next: P3.4** — adopt the `*exec.Cmd` `Confiner` signature, select the real backend per-OS
(`platform.NewConfiner()` ⇒ landlock on Linux, **seatbelt on macOS**, `denyConfiner` elsewhere), wire
`Confine` into dispatch, `AutoEligible()`→`FSWrite`-only, `__confined-exec` dispatcher in `main`.

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

#### ✅ P3.4 result — landed 2026-06-24 (mode ladder + Confine into dispatch; Auto is real; gate GREEN; disposition table hermetic; landlock enforcement self-skips on this kernel)

P3.4 wires the contract (§2.2 signature, §4 disposition, §5 `AutoEligible`, §2.6 host selection, §3 marker)
into running code. The disposition/wiring tests are **fake-Confiner** driven (caps injected), so the full
ladder is hermetic regardless of the host kernel (this dev host has landlock compiled out — the live
landlock/seatbelt escape batteries self-skip loudly, the disposition table runs and passes under `-race`).
**No new dep** (`go.mod`/`go.sum` unchanged). **What landed:**

- **Confiner signature flipped to prepare-in-place** (`internal/domain/confinement.go`):
  `Confine(ctx, box, *exec.Cmd) error` replaces the deleted closure form (`fn func(ctx) error`). `domain`
  gains an `os/exec` import (stdlib — ADR 0010's invariant is the root module path, not stdlib breadth).
  The real backends (`*landlockConfiner`/`*seatbeltConfiner`) already had this concrete method, so flipping
  the interface makes them satisfy it; `denyConfiner` (`internal/platform/platform.go`) now mirrors it,
  returning `ErrConfinementUnavailable` rather than running a cmd unconfined; `platform_test.go`'s former
  closure-form test became an `ErrConfinementUnavailable` assertion.
- **`AutoEligible()` → `FSWrite`-only**; **the construction gate is now conditional on a NIL Confiner**, not
  on caps (`internal/agent/loop.go`). A present-but-incapable Confiner (no fs-confinement) now ENTERS Auto
  and the subprocess surface gates ("confine if you can, gate if you can't" — ADR 0012 §4/§5), reversing
  ADR 0004's refuse-deny-all. `ErrAutoUnavailable` fires only for a nil Confiner. `apogee_test.go`'s
  "deny-all → refused" row became "deny-all → enters Auto (subprocess gates)".
- **`ModeAllowEdits`** added to the ladder (`internal/domain/config.go`) and re-exported (`apogee.go`); the
  `--mode` flag accepts `plan | ask-before | allow-edits | auto` (`cmd/apogee/wire.go`,`root.go`).
- **`workspaceScopedWriter` marker built** (`internal/tools/workspace_scoped.go`) exactly per contract §3.2:
  an **unexported** interface in `internal/tools` with the `workspaceWriteTarget(call)` seam, plus the
  exported `IsWorkspaceScopedWriter` / `WorkspaceWriteTarget` detectors. `write_file` carries it (the two
  unexported methods + a compile-time assertion); it survives `Subset` for free (a method set on the value).
- **`SubprocessTool` marker** added to `domain` (`tools.go`): the exported "I launch an OS subprocess, confine
  me" signal P3.8's `terminal`/`python-exec` implement — the "subproc" tool-class the disposition confines.
- **`needsApproval` reworked into the D5 blast-radius disposition** (`internal/agent/disposition.go`):
  `classifyTool` → {RO, WS-write, network, mcp, subproc, 3p-write}; `dispose(mode, tool, call)` →
  {run, confine, gate, refuse} keyed on `(mode, class, confine-to-workspace, caps)`. The dangerous-action
  guard still runs FIRST (P3.6, tighten-only), and a Tier-2 force-approval upgrades any non-refuse
  disposition to a gate. `dispatch.go`'s `resolveAndExecute`/`approve`/`executeTool` consume the disposition;
  the legacy `needsApproval`/`isExternalEffect` helpers are gone.
- **Confiner threaded to the subprocess tool via the context** (`domain.WithConfinement` /
  `ConfinementFromContext`, a `Confinement{Confiner, Box}` handle). A `dispoConfine` call runs with the handle
  installed; the subprocess tool builds its own `*exec.Cmd` and calls `Confine` on it (the contract's
  tool-owns-IO model, §2.2), keeping `domain.Tool.Execute(ctx, call)` unchanged (the open extension point,
  ADR 0002). The box is built from the injected `WorkspaceDir ∪ Config.ConfineWritablePaths` with
  `Config.ConfineNetworkAllow` as the box's `NetworkAllow` (new Config fields; box-construction of toolchain
  cache dirs is the host's concern, §7).
- **`ExternalEffects.Do` plumbed** (`dispatch.go` `runTool`): an `ExternalEffectTool` (network OR mcp kind)
  routes through `cfg.ExternalEffects.Do(ctx, call)` when set, else live `Execute` — kept SEPARATE from the
  gating, which keys on the effect KIND, not the bare interface.
- **Per-OS backend selector behind build tags** (`internal/platform/confiner_{linux,darwin,other}.go`):
  `platform.NewConfiner()` returns landlock on Linux, seatbelt on darwin, `denyConfiner` elsewhere. The
  selector is split per file because `NewLandlockConfiner` is linux-only and `NewSeatbeltConfiner` darwin-only.
  `cmd/apogee` injects it (no longer `denyConfiner`), so `--mode auto` works where fs-confinement exists.
- **`__confined-exec` sentinel dispatched before Cobra** (`cmd/apogee/main.go` →
  `maybeDispatchConfinedExec`, build-tagged `confined_exec_{linux,other}.go`): on Linux it decodes the box
  (`platform.DecodeConfinedBox`) and calls `platform.ApplyLandlockAndExec` (failing closed on error); off
  Linux it is a no-op (macOS confines via `sandbox-exec`, which is itself the wrapper).
- **`confine-to-workspace` is global-config-only** (`cmd/apogee/config.go`): a new `fileConfig` key resolved
  from the FILE layer alone (never env or flag — a hostile repo's invocation environment cannot loosen it),
  default `true`; a per-session startup warning prints when Auto runs unconfined (`wire.go`). The embedded
  `defaults/config.yaml` documents the ladder + the flag.

**Disposition realised (the D5 table, all `-race`, `internal/agent/dispatch_test.go`):** Auto/confine=true —
subproc runs **under Confine** no Approval; in-workspace Apogee write runs no Approval **no Confine**;
out-of-workspace Apogee write **gates**; native network **auto-runs**; mcp + 3p-write **gate**; subproc with
insufficient caps **gates**. Auto/confine=false — all auto-run **except** the Tier-1 floor. Allow-Edits —
in-workspace Apogee write auto-approves, `terminal` gates, **no Confine** invoked. Plan refuses writes;
Ask-Before gates writes/subproc, reads free. `ExternalEffects.Do` routes external tools, bypasses the rest.

**Verify gate (§7) — all green:** `gofmt -l .` empty · `go vet ./...` clean · `GOOS=darwin go vet` clean ·
`go build ./...` ok · `go test -race ./...` ok (landlock/seatbelt enforcement batteries self-skip loudly —
this kernel has `CONFIG_SECURITY_LANDLOCK` off; everything else passes) · ADR-0010 grep empty · 6/6
cross-builds ok · `go mod tidy` adds no dep (go.mod/go.sum unchanged) · `./apogee --help` exit 0 with the
ladder surfaced.

**v1 gap flagged (P3.7's job):** the "WS-write, target out of workspace → gate" row resolves correctly in the
disposition, but `write_file.Execute` still hard-rejects an out-of-root path via `resolveInRoot`, so an
*approved* out-of-workspace write would still error at Execute. The `workspaceWriteTarget` seam is what makes
the richer behaviour a later additive change (contract §4 v1-gap note); until P3.7, Apogee writes stay
strictly workspace-bounded and the gate→error is the honest fallback.

**Handoff to successors:** P3.7 (file-edit family) carries the `workspaceScopedWriter` marker (the seam is
built) and closes the out-of-workspace write gap. P3.8 (`terminal`/`python-exec`) implements `SubprocessTool`
and consumes the `ConfinementFromContext` handle (+ the §2.4 process-group teardown). P3.11 (`web-fetch`) ships
an `ExternalEffectTool{network}` that auto-runs in Auto. P3.15 (MCP) ships `ExternalEffectTool{mcp}` that
gates. P3.13 (sub-agents) inherits the marker for free through `Subset`.

### P3.5 — `processing/` parity finish
Add the remaining tool-call parsers (markdown-fenced, custom-regex) and the **full harmony /
thinking-channel set** behind a `processor-factory` that selects per model/response, each gated by
**ported apogee-code TS test vectors** (the riskiest-port discipline — the TS is the oracle). Keep
the package `domain`-only; the loop selects the processor at the existing adapt-seam. **Acceptance:**
every ported vector passes (golden, ANSI-/whitespace-normalised as the TS asserts); a malformed
payload in any format degrades to the parse-error path (never a panic, never a Turn failure — the
P1.3 contract); the factory picks the right parser for native vs fenced vs regex models; the bench
re-run shows no parsing regression. Record the parity result in the commit.

#### ✅ P3.5 result — landed 2026-06-24 (processing parity finished; the riskiest port; gate GREEN; all ported TS vectors pass)

The two remaining tool-call text formats, the full harmony channel set, and the processor-factory
are ported behind a new `ToolCallParser` interface — `domain`-only, stdlib-only, ADR-0010-clean.
**No architectural call was forced**, so no ADR (D4's posture held — a port, not a redesign). What
landed in `internal/processing`:

- **`parser.go` — the `ToolCallParser` interface** (`ParseToolCall(raw) (domain.ToolCall, found)` +
  `StripToolCall(raw) string`), the text counterpart to `ParseNativeToolCalls`. Single-call contract
  (the oracle parses at most one); no-call degrades to `found=false`, malformed never panics (P1.3
  contract). Text formats name no call ID — the empty ID is assigned downstream by the loop.
- **`markdown_fenced.go` — `MarkdownFencedParser`** (faithful port of the TS oracle): strict
  ```` ```tool ```` fenced-block parse over the last opener, then a marker-based (`BEGIN_ARG`/`END_ARG`)
  fallback; `parseBlock` line state-machine; `tryParseValue` coercion (valid-JSON kept, else trimmed
  string). **One deliberate divergence:** the TS fence-close `` ```(?!tool) `` negative lookahead has
  no RE2 equivalent, so it is an explicit scan with identical behaviour (a closing ``` that does not
  reopen the fence language) — noted in the doc comment. Defaults (`tool`/`TOOL_NAME`/`BEGIN_ARG`/
  `END_ARG`) match the oracle. **All 7 TS markdown-fenced vectors pass** (basic, multi-arg, multi-line,
  no-block, thinking-strip-then-parse, double-opening-fence, no-`TOOL_NAME`-marker).
- **`custom_regex.go` — `CustomRegexParser`** (port): named-group regex, args = a valid JSON object
  verbatim else `{raw:…}` (the oracle's graceful non-JSON path), empty group → `{}`. JS-style
  `(?<name>…)` groups are **rewritten to Go `(?P<name>…)`** so the apogee-code vector patterns work
  unchanged; `flags` (`s`/`i`/`m`) map to a Go `(?…)` prefix; an **invalid pattern degrades to a
  never-match parser** (the oracle's warn-and-fallback), never a construction error. **All 4 TS
  custom-regex vectors pass** (regex match, no-match, thinking-strip, non-JSON `{raw}`).
- **`harmony.go` — `StripHarmony` / `IsHarmonyThinking`** (the **full channel set**, the Phase-3
  generalisation beyond P1.3's single analysis-pair `StripThinking`): parses every
  `<\|channel\|>NAME<\|message\|>…` message by name, routes **analysis→reasoning,
  commentary→commentary, final→visible**, consumes an optional `<\|start\|>role` prefix, and honours
  the three harmony terminators `<\|end\|>` / `<\|call\|>` / `<\|return\|>`. A streaming
  (unterminated) non-final tail is captured as in-flight reasoning and never leaks into `Visible`;
  plain non-harmony text passes through. A consistency test pins that it agrees with the existing
  single-pair `StripThinking` on the analysis channel they both handle. (Format reference: the
  gpt-oss harmony three-channel spec, analysis/commentary/final.)
- **`factory.go` — `NewToolCallParser(ToolCallingConfig)`** the processor-factory (port of
  `ProcessorFactory.create`): selects native / markdown-fenced / custom-regex; **native is a text
  no-op** (`nativeTextParser`) because the structured path (`ParseNativeToolCalls`) owns native calls;
  an **unknown format errors** so a misconfigured model fails loudly.
- **`args.go`** — shared `tryParseValue` / `marshalArgs` (deterministic sorted-key JSON object) used
  by both text parsers.

**Scope note (honest, for the next agent):** the package-level port + factory are complete and
fully vector-gated, but the **loop adapt-seam still hard-codes the native path** (`parseToolCalls`
in `loop.go`). Wiring the factory to select fenced/regex per response needs a model-profile /
`ToolCallingConfig` *source* that **does not yet exist** in `domain`/config (apogee has no
model-profile layer — apogee-code's `defaults/model-profiles/*.json` were not ported). The factory
is the seam that consumes that source when it lands; building the source is out of P3.5's scope (and
out of the §6 plan for v1 — every shipped profile uses native tool calls). No bench regression: the
bench runs native structured `tool_calls`, the one path P3.5 leaves untouched.

**Verify gate (§7) — all green:** `gofmt -l .` empty · `go vet ./...` + `GOOS=darwin GOARCH=arm64 go
vet ./...` clean · `go build ./...` ok · `go test -race ./...` all `ok` (processing +26 subtests) ·
ADR-0010 grep empty · 6 cross-builds OK (`CGO_ENABLED=0`) · `go mod tidy` no drift (no new deps —
stdlib `regexp`/`encoding/json` only) · `apogee --help` exit 0.

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

#### ✅ P3.6 result — landed 2026-06-24 (guardrail layer built in `internal/security` + wired through the executor; gate GREEN; path-safety consolidated with 4-built-in parity)

`internal/security` is filled (was a Phase-0 `doc.go` stub) — the human-in-the-loop guardrail layer
(D6 / ADR 0012), distinct from the `Confiner`, running in **every** mode and threaded through the tool
executor so all tools — and a sub-agent (D2) — inherit it. **No new dep** (`go mod tidy` clean);
imports only `internal/domain` + stdlib (ADR-0010 grep stays empty). **What landed:**

- **Consolidated path-safety** (`internal/security/pathsafety.go`): `ResolveInRoot` / `EvalRealPath` /
  `ErrPathEscape` — the symlink-aware, traversal-rejecting guard moved **verbatim** from
  `internal/tools/path_safety.go` (the logic is byte-for-byte the former code, now exported and in one
  place). `internal/tools/path_safety.go` is reduced to thin aliases (`resolveInRoot` →
  `security.ResolveInRoot`, `ErrPathEscape = security.ErrPathEscape`, `evalRealPath` →
  `security.EvalRealPath`), so the **4 built-ins and their tests are untouched** and behaviour is
  identical. **Parity verified:** `go test ./internal/tools/...` passes unchanged; the path-safety table
  tests are duplicated at the guard's new home (`pathsafety_test.go`). New edge: `internal/tools` now
  imports `internal/security` (a clean `tools → security → domain` chain, no cycle — `security` never
  imports `tools`).
- **URL-safety** (`urlsafety.go`): `URLGuard{AllowSchemes, AllowHosts, DenyHosts}` + `Check(raw)` →
  `ErrURLBlocked`, **deny-first** precedence, scheme defaulting to http/https, exact-or-subdomain host
  matching (a sibling-prefix host does not match). Provided now for P3.11's `web-fetch`/`http-request`;
  **not yet wired** (no network tool exists) — the guard + its tests are the deliverable.
- **Dangerous-action guard** (`dangerous.go` + `rules.go`): `DangerousActionGuard` over a tighten-only
  ruleset, inspecting a call's tool name + every JSON string leaf, **whitespace-normalized + lower-cased**
  (the only normalization — **no obfuscation-chasing**, by ADR 0012). Two tiers — **Tier-1 hard-refuse**
  (no override) and **Tier-2 force-approval** — with the strictest matching tier winning. **Default
  ruleset** (`DefaultDangerousRules`, precision-over-recall): T1 = `rm -rf`/`rm -fr` of root/home/system
  (`/`, `~`, `$HOME`, `/etc|usr|bin|sbin|lib|boot|dev|var|sys|proc|root|home|opt`), fork bomb,
  `~/.ssh` writes, credential/persistence-file writes (`.bashrc`/`.zshrc`/`.aws/credentials`/`.netrc`/
  `.npmrc` *under $HOME*), raw block-device `dd of=/dev/sd…`; T2 = `curl|wget|fetch … | sh`-class,
  `sudo <cmd>`. **Precision asserted**: `rm -rf ./build`, `rm -rf node_modules`, `curl … | grep`, a
  project `.npmrc`, `go build` etc. are **not** blocked. **Config-merge** (`MergeDangerousRules`): base
  ⊕ global-add ⊕ global-remove(by id) ⊕ project-add — **global may add OR remove**, **project may only
  add** (same-id add tightens in place; a project can never remove). A malformed regex is dropped, never
  fatal.
- **Circuit-breaker** (`circuitbreaker.go`): `CircuitBreaker` trips after N (default **3**) consecutive
  identical failing calls keyed on `(tool, args-hash)`; a success clears the streak; `Tripped` short-
  circuits before re-running; `Record` reports the **trip edge once** so the executor surfaces a single
  `ErrorEvent`. Mutex-guarded (safe under `-race`).
- **Audit** (`audit.go`): `AuditLog` — append-only `AuditRecord{Time, Tool, CallID, Decision, Reason,
  IsError, Result}`; `Records()` returns a copy (no storage leak); large results truncated.
- **Executor bundle + wiring** (`guard.go` + `internal/agent/dispatch.go`): `security.Guards{Dangerous,
  Breaker, Audit}` (zero value inert) with `PreExecute` (breaker-tripped → refuse; then dangerous-action
  → refuse / force-approval; **runs ahead of the mode disposition**, tighten-only) and `RecordExecution`
  (breaker + audit post-run). The Agent holds a `guards` field (`security.NewDefaultGuards()` at
  construction); `resolveAndExecute` runs `PreExecute` **first** (Tier-1/breaker → error `ToolResult` +
  `ErrorEvent`, before the Plan/Approval gates and independent of the Confiner), forces the Approver on
  Tier-2 (a `nil` Approver ⇒ refuse), and records every call's outcome. **`needsApproval` is
  unchanged** — the per-mode blast-radius disposition rework (D5) is left to **P3.4**; P3.6 only adds the
  always-on guardrail layer beneath it and an extra `force` parameter on the existing `approve` helper.

**Dangerous-action ruleset + precision posture:** membership is *almost-never-legitimate* **and**
*catastrophic*; the rule patterns anchor on dangerous **targets** (absolute root/home/system paths,
`$HOME` dotfiles) so destructive-but-normal project operations (`rm -rf ./build`) never match — recall is
deliberately sacrificed for precision (it is a mistake-net, not an adversary boundary).

**Config-merge approach + deferral:** the merge **rule** (global add/remove, project add-only) is
implemented and table-tested as `MergeDangerousRules`; **the config.yaml file-surfacing is deferred** —
no `config.yaml` keys are read for custom rules yet (the executor wires the default ruleset via
`NewDefaultGuards`). Surfacing the rules + the breaker threshold into `~/.apogee/config.yaml` is a thin
later addition (the merge function is the hard part and is done + tested). url-safety is likewise built
but unwired (waits on P3.11's network tools).

**Verify gate (§7) — all green:** `gofmt -l .` empty · `go vet ./...` + `GOOS=darwin go vet` clean ·
`go build ./...` ok · `go test -race ./...` all `ok` (no FAIL/panic/DATA RACE) · ADR-0010 grep empty · 6
cross-builds OK · `go mod tidy` no drift · `apogee --help` exit 0.

**Downstream notes.** **P3.4:** key the mode disposition (D5) on top of `PreExecute` — the dangerous-
action guard already hooks "before the disposition" inside `resolveAndExecute` (`PreExecute` runs first,
tighten-only), so P3.4's `needsApproval`-rework only needs to handle the *clear* path; the `force`
parameter on `approve` is the seam a Tier-2 forces-Approval-even-in-Auto through, and P3.4 should leave
that override intact. **P3.11:** call `URLGuard.Check` from `web-fetch`/`http-request` before reaching out
(`network`-kind tools auto-run in Auto, url-filtered). **P3.13:** the sub-agent inherits guardrails by
threading the parent's `security.Guards` value into the nested Agent (it is a copyable value with no live
state); the marker/breaker/audit ride along verbatim.

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

#### ✅ P3.7 result — landed 2026-06-24 (file-editing family in `internal/tools`; gate GREEN; TS-oracle vectors ported and passing; marker rides the P3.4 disposition for free)

The pure-Go, stateless file-editing family is ported from the apogee-code oracle, behind the public
`domain.Tool` interface, scoped to a sandbox root with the consolidated path-safety guard (P3.6).
**No new dep** (`go.mod`/`go.sum` unchanged — stdlib `strings`/`regexp`/`encoding/json` only),
ADR-0010-clean (`internal/tools` imports only `domain` + `security`). **What landed in `internal/tools`:**

- **`find_replace.go` — `single_find_and_replace` + `multi_find_and_replace`** (oracle names, for parity).
  Single requires `oldText` to appear **exactly once** (0 → "not found", >1 → "found N times"); multi
  applies its `replacements` **sequentially in array order against an in-memory copy** and writes only if
  every step matches exactly once — **atomic** (any failure leaves the file byte-identical). Both honour
  the `maxFileContentBytes` ceiling. `countOccurrences` is the non-overlapping `strings.Count` port (empty
  needle → 0). Both carry the **`workspaceScopedWriter`** marker.
- **`file_edit.go` — `edit_existing_file`**: full-file replacement, OR — when `content` opens with
  `*** Begin Patch` — a **hunk-applied patch** (`@@` blocks of `-`/`+`/` ` lines; Begin/End/File markers
  skipped). `parsePatchHunks` + `applyPatch` are faithful ports of the oracle's `indexOf`-based applier: a
  non-matching hunk returns "did not match" and **never writes** (no corruption); an empty patch returns
  "no hunks". Carries the marker.
- **`diff.go` — `view_diff`** (`ReadOnly`, no marker): a **pure-Go Myers/LCS** line diff of the file's
  current content vs a proposed `newContent` (`-`/`+`/context prefixes), **no external `git`** (the older
  P3.7 task text said "myers diff — no external"; the oracle's `view_diff` shelled out to git, but the plan
  explicitly mandates pure-Go, so it diffs against supplied content instead). Output is **deterministic**
  (LCS table fully orders it; asserted by a repeat-call test). Identical content → "No changes detected".
- **`open_file.go` — `open_file`** (`ReadOnly`, no marker): the editor-affordance read tool. The oracle's
  VS Code "currently open file" has no TUI analogue, so this is a **path-named read + optional substring
  locate** (reports the 1-based line numbers where `locate` occurs). Bounded by `maxFileReadBytes`.
- **`registry.go`**: the five tools append to `DefaultTools` **after** the P1.4 base set (existing order
  preserved); `registry_test.go` updated to the 9-tool count / order / read-only map.

**Disposition — rides P3.4 for free.** `classifyTool`/`dispose` (`internal/agent/disposition.go`) key on
the **marker**, not the tool name, so the three writers classify as `classWorkspaceWrite` and inherit the
exact `write_file` rows: **gate in Ask-Before, auto-approve in Allow-Edits, run path-safety-bounded (no
`Confine`, no Approval) in Auto/confine=true** when in-workspace, **gate** when the target is
out-of-workspace; `view_diff`/`open_file` are `classReadOnly` (run in Plan). Proven by new rows in
`dispatch_test.go` (`TestClassifyTool` + an Auto/confine=true find-replace in/out-of-workspace pair) — the
marker's `workspaceWriteTarget` seam works for the whole family. **Statelessness holds** (ADR 0008): each
`Execute` reads, edits in memory, writes, returns — no handle survives the call.

**TS-oracle parity (the gate).** The apogee-code vectors are ported and pass: multi-find-replace's
sequential-dependent edit, atomic-failure-leaves-file-intact, duplicate-created-by-prior-edit, and
deletion-via-empty-newText; file-edit's single-hunk / multi-hunk / context-line-preservation /
non-matching-hunk-does-not-corrupt / empty-patch vectors. Path-escape is rejected with a clear error
result in every mode (the P3.6 path-safety guard), asserted per tool.

**v1 gap CLARIFIED (the P3.4 "out-of-workspace Apogee write" row).** P3.4 flagged that an *approved*
out-of-workspace write would still error because `resolveInRoot` hard-rejects any escape at Execute. P3.7
keeps Apogee's write tools **strictly workspace-bounded**: the disposition correctly *gates* an
out-of-workspace target (the marker's `workspaceWriteTarget` resolves it without containment for
classification), but Execute still rejects it via `resolveInRoot` — so an approved out-of-workspace write
results in an honest error, not a silent escape. Honouring an approved escape (resolving against
`WorkspaceRoot ∪ box.WritablePaths`) remains the deferred additive change the `workspaceWriteTarget` seam
enables; doing it now would require threading the box into the in-process write tools, which P3.7's scope
does not include. The gate→error fallback is the honest v1 behaviour (consistent with the contract §4 note).

**Verify gate (§7) — all green:** `gofmt -l .` empty · `go vet ./...` + `GOOS=darwin GOARCH=arm64 go vet
./...` clean · `go build ./...` ok · `go test -race ./...` all `ok` (tools +new file-edit subtests; agent
disposition +new rows; landlock/seatbelt enforcement batteries self-skip loudly on this kernel as expected)
· ADR-0010 grep empty · 6 cross-builds OK (`CGO_ENABLED=0`) · `go mod tidy` no drift · `./apogee --help`
exit 0. **Next: P3.8** (`terminal`/`python-exec` — first `SubprocessTool`/`Confiner` consumers).

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

#### ✅ P3.8 result — landed 2026-06-24 (first `Confiner` consumers; both carried findings closed; gate GREEN)
`terminal` + `python_exec` are the first `domain.SubprocessTool`s — they build and run their own
`*exec.Cmd`, consume the `ConfinementFromContext` handle, and honour the §2.4 teardown. One static
artifact still (`shlex` is the only new dep, tiny + transitive-free). **What landed:**

- **Shared subprocess runner** (`internal/tools/exec_common.go`): `runSubprocess(ctx, subprocessSpec)`
  owns the whole §2.4 contract once for every execution tool — builds the `*exec.Cmd` via
  `exec.CommandContext` (tool-builds-and-runs-the-cmd, contract §2.2), captures combined stdout+stderr
  through a `cappedBuffer` (256 KiB ceiling + truncation marker), enforces a per-call timeout (default
  120 s, max 600 s), and confines the cmd when a `Confinement` handle is on ctx. A clean non-zero exit
  is a normal **result** (exit code surfaced), not a Go error; only ctx-cancel and the demote signal are
  Go errors.
- **`terminal`** (`terminal.go`): runs a command line through `platform.Shell` (`sh -c` / `cmd /c`),
  `shlex`-validates the line (balanced quotes) before the shell sees it, path-scopes an optional
  `workdir` to the root. **`python_exec`** (`python_exec.go`): probes `python3`→`python` on PATH (a
  swappable `lookInterpreter` var), feeds the script on **stdin** (`<interp> -`, no temp file ⇒
  statelessness, ADR 0008), and degrades to a clear "python not available" result when absent (§3a — no
  hard dep). Both carry `domain.SubprocessTool` (`Subprocess()==true`), are write-capable (not
  `ReadOnly`), and do **not** carry the `workspaceScopedWriter` marker — they confirm the subproc row of
  the disposition (`TestClassifyTool` already classifies a `subprocTool` as `classSubprocess`). Both
  registered in `DefaultTools` after the P3.7 family.
- **CARRIED FINDING #1 — process-group lifecycle (CLOSED).** `setProcessGroupTeardown`
  (`exec_pgroup_unix.go`, `//go:build !windows`) pairs the backend's `Setpgid` with `cmd.Cancel` (SIGKILL
  to the **negative PID** = the whole group) + a 5 s `cmd.WaitDelay`, so a cancelled/timed-out command
  reaps its children — no orphaned child, no orphaned `sandbox-exec`. It also runs for an **unconfined**
  subprocess (lower modes / confine=false), giving every subprocess clean teardown. A Windows stub
  (`exec_pgroup_other.go`, `//go:build windows`) keeps the cross-build green (leader-kill + WaitDelay;
  real job-object teardown is Phase 5). **Tested:** `TestTerminal_TimeoutKillsCleanly` (a `sleep 30`
  with a 1 s timeout returns promptly) and `TestTerminal_CancelKillsChildProcessGroup` (a backgrounded
  grandchild `sleep` is reaped on ctx-cancel — a leader-only kill would orphan it).
- **CARRIED FINDING #2 — runtime confinement-unavailable net (CLOSED).** The subprocess tools return
  `ErrConfinementUnavailable` (wrapped) rather than running unconfined when `Confine` fails at run time.
  `internal/agent/dispatch.go` now lands it: `executeTool` maps `errors.Is(err,
  ErrConfinementUnavailable)` to a new `dispatchConfinementUnavailable` outcome; `resolveAndExecute`
  routes it to `executeWithApprovalFallback`, which **demotes the call to a forced Approval** and, only
  on allow, re-runs it unconfined (Approval is now the bound — the §4 "subproc, caps insufficient → gate"
  row applied at run time). This is the previously-missing "confine-if-you-can, gate-if-you-can't"
  **runtime** landing site (the construction-time caps gate already existed). **Tested:**
  `TestDisposition_RuntimeConfineUnavailable_DemotesToApproval` (approved → runs once; denied → refused,
  never runs unconfined; nil-Approver → refused) plus per-tool
  `Test{Terminal,PythonExec}_ConfinementUnavailablePropagates`.
- **Confine handoff proven hermetically.** `Test{Terminal,PythonExec}_RunsUnderConfine` inject a fake
  caps-`{FSWrite:true}` `Confiner` (the dev host has landlock compiled out) and assert the tool hands its
  cmd to `Confine` exactly once — the real landlock/seatbelt *enforcement* (an out-of-box write is
  OS-denied) is the owner/CI run on a landlock-enabled kernel + macOS, per the env caveat.

**Verify gate (§7) — all green:** `gofmt -l .` empty · `go vet ./...` + `GOOS=darwin GOARCH=arm64 go vet
./...` clean · `go build ./...` ok · `go test -race ./...` all `ok` (new terminal/python-exec + dispatch
demote tests; landlock/seatbelt enforcement batteries self-skip loudly on this kernel as expected;
`python_exec` live-run subtests skip when no interpreter is on PATH) · ADR-0010 self-import grep empty ·
6 cross-builds OK (`CGO_ENABLED=0`; teardown build-tagged so the Windows targets pass) · `go mod tidy`
adds `github.com/google/shlex` as the lone direct dep, no other drift · `./apogee --help` exit 0.
**Next: P3.9** (`git` tool — system-`git` shell-out, the next subprocess consumer).

### P3.9 — `git` tool
Branch / commit / diff-range over the **system** `git` (§3a — detected on PATH, graceful "git not
available" when absent — never a hard dep; this is a *convenience* dep, not inherent). Path-safe to
the workspace; arg-guarded; in Auto runs under `Confine` (subprocess) or Approval-gates if the box
can't be established. **Acceptance:** branch/commit/diff-range produce correct output against a
`t.TempDir()` repo; absence of `git` degrades to a clear unavailable result (not a crash); writes
(commit) gate/confine per mode; no network git op runs unconfined in Auto.

#### ✅ P3.9 result — landed 2026-06-24 (three git tools ported from the oracle; gate GREEN; rides the P3.8 SubprocessTool/Confiner pattern for free)
`git_branch` + `git_commit` + `git_diff_range` are the next `domain.SubprocessTool`s — they build a
`["git", …]` argv and run it through the shared `runSubprocess` (P3.8), so the §2.4 teardown, the
output cap, the timeout, and the `ConfinementFromContext` handoff are inherited verbatim. No dispatch
change: the disposition classifies them by their existing markers (`classSubprocess` for the writers,
`classReadOnly` for diff-range), so they confine/gate/run by the P3.4 table with zero new wiring. The
binary stays one static artifact — **no new dependency** (the system `git` is a detected, graceful
convenience dep, §3a). **What landed:**

- **`internal/tools/git.go`** — three tools mirroring the apogee-code oracle (`git-branch-tool.ts` /
  `git-commit-tool.ts` / `git-diff-range-tool.ts`):
  - **`git_branch`** (write-capable): `create` (`checkout -b`, optional `start_point`), `switch`
    (`checkout`), `list` (`branch -a --format`), `delete` (safe `-d`, which refuses an unmerged branch).
    Deletion of the protected mainline branches (`main`/`master`/`develop`/`development`, case-insensitive)
    is refused before the subprocess.
  - **`git_commit`** (write-capable): stages the named files (each **path-safe** to the workspace via the
    shared `resolveInRoot`) then commits; `amend` is **refused on a published commit** (a decoration ref
    `origin/…` from `git log -1 --format=%D`), so the tool never rewrites history a remote has seen;
    `allow_empty` supported; reports the new commit's one-line summary.
  - **`git_diff_range`** (`ReadOnly()` — runs in Plan): three-dot `base...head` diff with `stat` /
    `name_only` / path-scoped `paths`; refs are validated against a conservative character class
    (`^[A-Za-z0-9._\-/~^@{}]+$`) so a `head` like `x; rm -rf /` is rejected before git sees it.
- **Allowlisted environment.** Each git subprocess runs with `safeGitEnv()` — the `safeEnvKeys` allowlist
  ported from the oracle's `SAFE_ENV_KEYS` — so a surprising inherited variable cannot redirect git
  (config/auth/pager). This rides a **small additive field on `subprocessSpec`** (`env []string`, nil =
  inherit) in `exec_common.go`; the shell/interpreter tools keep nil (inherit), unchanged.
- **Disposition for free + the runtime net.** `git_branch`/`git_commit` confine in Auto under
  `confine-to-workspace=true` and return (wrapped) `ErrConfinementUnavailable` rather than running
  unconfined when `Confine` fails — `dispatch.go` already demotes that to forced Approval (P3.8's runtime
  net). `git_diff_range` is read-only, so the disposition runs it freely (read-only wins over the
  subprocess class) — a local diff is harmless inspection. None of the three exposes a network git op
  (no `push`/`fetch`/`clone`), so "no network git op runs unconfined in Auto" holds structurally.
- **Dangerous-floor decision (no addition).** The destructive footgun ops (`push --force`, `reset --hard`,
  `clean -fdx`) are **structurally unreachable** through these fixed-subcommand tools — there is no raw
  passthrough — so no git-specific rule was added to the dangerous-action floor (precision-over-recall,
  ADR 0012). The `terminal` tool can still run arbitrary `git`; that surface is the terminal's, already
  covered by the existing floor + the subprocess confinement.

**Tests** (`git_test.go`): markers (branch/commit write-capable + SubprocessTool, diff-range ReadOnly +
SubprocessTool, none a workspaceScopedWriter); graceful absence for all three (injected `lookGit`→absent
⇒ "git not available", not a crash); arg validation (invalid action, name-required, protected-delete
block, message-required, ref-class rejection, path-escape on commit-staging and diff-paths); live runs
against a `t.TempDir()` repo (create/switch/list/delete round-trip, stage+commit summary, three-dot diff
names the changed file) — these `t.Skip` when no `git` is on PATH (the graceful contract); confine
handoff proven hermetically (a fake caps-`{FSWrite:true}` Confiner is called exactly once) and the
unavailable-Confiner case propagates `ErrConfinementUnavailable` (the tool must not run unconfined).
`registry_test.go` updated to 14 built-ins (menu order + read-only nature).

**Verify gate (§7) — all green:** `gofmt -l .` empty · `go vet ./...` + `GOOS=darwin GOARCH=arm64 go vet
./...` clean · `go build ./...` ok · `go test -race ./...` all `ok` (the live git subtests run where git
exists, skip where it doesn't; landlock/seatbelt enforcement batteries self-skip on this kernel as
expected) · ADR-0010 self-import grep empty · 6 cross-builds OK (`CGO_ENABLED=0`) · `go mod tidy` no
drift (no new dep) · `./apogee --help` exit 0. **Next: P3.10** (`diagnostics` tool).

### P3.10 — `diagnostics` tool
In-process for Go — `go/parser` for syntax + the `go vet` that ships with the toolchain — and
**optional** shell-out linters (`tsc`, etc.) for other languages, **detected + graceful-degrading**
(§3a — an *enhancement*, never required). Read-only. **Acceptance:** a Go file with a syntax error /
a vet finding is reported in-process (no external dep); a non-Go file with no available linter returns
a clear "no diagnostics available" (not an error); the tool is `ReadOnly()` (runs in Plan).

#### ✅ P3.10 result — landed 2026-06-24 (single read-only `diagnostics` tool; gate GREEN; no new dependency)
`diagnostics` is the 15th built-in: one read-only tool that diagnoses a source file. The Go path is
split into the two halves the §3a stdlib-first rule asks for — a **dependency-free in-process syntax
check** (`go/parser` with `parser.AllErrors`, so a Go syntax error is reported even on a host with **no
`go` on PATH**) plus an **optional `go vet`** on the file's package that degrades gracefully when the
toolchain is absent (a "go vet skipped" note appended to the clean result, never an error, never a hard
dep). Languages with no built-in provider return a clear **"no diagnostics available"** result (not an
error) — the per-language external-linter slot (`tsc`, …) is left as a later additive extension behind
the same read-only/graceful contract. **What landed:**

- **`internal/tools/diagnostics.go`** — the `Diagnostics` tool (`NewDiagnostics(root)`):
  - **`ReadOnly()` + `Subprocess()`** — it only inspects (so the disposition runs it freely in **every**
    mode, including Plan), but it carries the `domain.SubprocessTool` marker because the vet half shells
    out, keeping the classification honest (read-only wins over the subprocess class — identical shape to
    P3.9's `git_diff_range`). It is **not** a `workspaceScopedWriter`.
  - **Syntax half** (`goSyntaxDiagnostics`): `go/parser` in-process, all syntax errors in one pass; a
    parse failure short-circuits (a file that does not parse cannot be vetted) and is surfaced as an
    **error result** the model can fix.
  - **Vet half** (`runGoVet`): `go vet <pkg-dir>` via the shared **`runSubprocess`** (P3.8) with the
    allowlisted **`safeGitEnv()`** environment (P3.9) and a 30s ceiling, working dir pinned to `root`.
    A non-zero exit with output is a finding (error result); a clean exit confirms the file looks clean.
    The target path is resolved through the shared **`resolveInRoot`** path-safety guard, so a path
    escape is refused before anything runs. `vet:false` skips the toolchain half (syntax-only).
  - **ctx discipline** (ADR 0007): the only Go-error return is ctx cancellation (the read-only diagnosis
    degrades on everything else); a vet build/setup failure (e.g. no `go.mod`) is surfaced as the finding
    text the model sees, not a crash.

**Tests** (`diagnostics_test.go`): markers (read-only + SubprocessTool, **not** a workspace-scoped
writer); path-required + path-escape rejection; unsupported language → graceful "no diagnostics
available" (not an error); a **Go syntax error reported with `go` faked absent** (proving the syntax
half needs no toolchain); a clean Go file with the toolchain-absent "go vet skipped" note; `vet:false`
syntax-only; and the live `go vet` cases (a `Printf`-format finding → error result, a clean file → "looks
clean") which seed a minimal `go.mod` and `t.Skip` when no `go` is on PATH (the graceful contract).
`registry_test.go` updated to **15 built-ins** (menu order + read-only nature; `diagnostics` runs in Plan).

**Disposition for free (no dispatch change).** Because `diagnostics` declares `ReadOnly()`, the P3.4
table classifies it `classReadOnly` → `dispoRun` in every mode — no Confine, no Approval, runs in Plan.
The `SubprocessTool` marker is inert for the disposition (read-only wins) but lets `runSubprocess`
honour a confinement handle if one were ever installed; none is, so confinement is moot here.

**Verify gate (§7) — all green:** `gofmt -l .` empty · `go vet ./...` + `GOOS=darwin GOARCH=arm64 go vet
./...` clean · `go build ./...` ok · `go test -race ./...` all `ok` (the live go-vet subtests run where a
`go` toolchain exists, skip where it doesn't; landlock/seatbelt enforcement batteries self-skip on this
kernel as expected) · ADR-0010 self-import grep empty · 6 cross-builds OK (`CGO_ENABLED=0`) · `go mod
tidy` no drift (no new dep — `go/parser`/`go/token` are stdlib) · `./apogee --help` exit 0.
**Next: P3.11** (network + host tools — `web-fetch`/`http-request`/`web-search` + `ask-user`).

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

#### ✅ P3.11 result — landed 2026-06-24 (network + host tools; SSRF floor closed by resolved IP + dial-time; gate GREEN; no new dependency)

The four P3.11 tools ship behind the public `Tool` interface, wired through `Config` via the new
`tools.NewDefaultRegistryWithHost`/`HostTools` seam (the loop's `resolveTools` threads the
`URLGuard`, the configured web-search endpoint, and the `Asker`). **No new dependency** (stdlib
`net/http`; `go mod tidy` clean; ADR-0010 grep stays empty). **What landed:**

- **Three network tools** (`internal/tools/{web_fetch,http_request,web_search}.go`, shared helpers
  in `network.go`): in-process `net/http` `ExternalEffectTool`s of kind **`network`** — they
  carry **no** `workspaceScopedWriter` marker and are **not** `SubprocessTool`s (no spawn, no
  Confiner lifecycle). The existing D5 disposition already **auto-runs `classNetwork` in Auto**
  (url-filtered) and routes external-effect tools through `ExternalEffects.Do` for the bench, so
  the gating/stub acceptance holds **with no dispatch change** — the tools just classify into the
  existing class (asserted: `web_fetch` is `EffectNetwork`, not workspace-writer/subprocess).
  `web_fetch` = GET; `http_request` = method/headers/body with a **method arg-guard** (CONNECT/TRACE
  refused); `web_search` posts a query (`q` param) to the **config'd, default-off** endpoint and
  reports a graceful "not configured" when absent (never a crash). Each caps the response body
  (2 MiB), bounds the timeout, and **does not auto-follow redirects** (a redirect to a private host
  is returned raw, not silently chased).
- **The carried SSRF finding — CLOSED.** `URLGuard` now carries a **default-on, tighten-only SSRF
  floor** (`internal/security/ssrf.go`): loopback (127/8, ::1), cloud **IMDS `169.254.169.254`** +
  link-local (169.254/16, fe80::/10), RFC-1918 (10/8, 172.16/12, 192.168/16) + ULA (fc00::/7),
  unspecified, and IPv4-mapped forms denied **by the RESOLVED IP** (so `localhost`, a
  private-resolving DNS name, and decimal/hex IP encodings are all caught). The floor is **never
  dissolvable by config** — config may only ADD denials; `DisableIPFloor()` is a code-only opt-out
  (mirrors the dangerous-rule tighten-only semantics). **DNS-rebinding/TOCTOU:** the pre-flight
  `CheckContext` resolve is the cheap first line; the real bound is **`SafeDialControl`**, a
  `net.Dialer.Control` hook installed on every network tool's transport that **re-validates the
  ACTUAL connected IP at dial time**, so a rebinding name that passes the pre-flight check still
  cannot connect to a private address. Tests (hermetic, injected resolver — no real DNS/network):
  a public IP passes; loopback-by-name, IMDS, a private-resolving hostname, and an IP-literal in
  **each** blocked range are all denied; an allow-listed-but-private host still hits the floor
  (tighten-only); the dial-time control blocks loopback/IMDS/private connects.
- **`ask_user` + the `Asker` host delegate** (`internal/tools/ask_user.go`; `domain/ask.go`;
  facade re-exports `Asker`/`AskRequest`/`AskAnswer`): a new **struct-typed** `Config.Asker`
  (`Ask(ctx, AskRequest) (AskAnswer, error)` — freeze-safe per D7, so a post-v1 `Choices` field is
  additive), the public analogue of `Approver` but free-text, **NOT** a safety gate. `ask_user` is
  **`ReadOnly()`** (runs in Plan, mode-independent — never routed through the disposition gate) and
  is **NOT** an `ExternalEffectTool`. A **nil Asker ⇒ the tool is not registered** (graceful), so
  the model is never offered a question it cannot have answered; the round-trip is covered by a
  scripted-asker tool test and the TUI seam test. **TUI wiring:** `uiAsker` (the free-text sibling
  of `uiApprover`) on the same late-bound `Bridge` programRef; a new `stateAwaitingAsk` +
  `askReqMsg` + an input-prompt rendezvous (the human types the answer into the input box, enter
  submits, esc cancels) — proven under `-race` (answer round-trips; a cancelled ctx returns
  promptly with no goroutine leak; an unbound program still unblocks on cancel — **fail-safe**).
- **`WebSearchEndpoint`** surfaced from `config.yaml` as a **file-only, default-off** key
  (`web-search-endpoint`; mirrors the global-only `confine-to-workspace` plumbing — no flag/env per
  the plan), documented (commented-out) in the embedded template. The **url-safety host allow/deny**
  config key is **deferred** (TODO.md) exactly as P3.6 deferred surfacing custom dangerous-rules —
  the SSRF floor (the security-relevant part) is on regardless.

**Verify gate (§7) — all green:** `gofmt -l .` empty · `go vet ./...` + `GOOS=darwin GOARCH=arm64 go
vet ./...` clean · `go build ./...` ok · `go test -race ./...` all `ok` (no FAIL/panic/DATA RACE) ·
ADR-0010 grep empty · 6 cross-builds OK (`CGO_ENABLED=0`) · `go mod tidy` no drift (stdlib only) ·
`apogee --help` exit 0. No landlock/seatbelt enforcement is exercised here (the network tools are
in-process — no Confiner), so there are no self-skips for this task.

**Downstream notes.** **P3.13:** a sub-agent inherits the network/host tools via `registry.Subset`
verbatim (the `URLGuard`/`Asker` ride the tool values — no extra threading); the `Asker`, like the
`Approver`, threads from the parent `Config` into the nested Agent. **P3.16:** `Asker` is a new
public v1 symbol to review at the freeze (D7 already named it). **Residual:** a user-tunable
url-safety host allow/deny config layer is parked (TODO.md, tighten-only when built).

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

#### ✅ P3.13 result — landed 2026-06-24 (sub-agent orchestrator + ADR 0013; carried Guards finding RESOLVED → isolate; gate GREEN; hermetic nested-loop tests under `-race`)

P3.13 makes `Depth > 0` real: a sub-agent is a nested `Agent` driven at a dispatch recursion point,
constructed bounded by the parent and running with **isolated** live guard state over a **shared,
read-only** dangerous floor. All tests are hermetic — the parent and sub-agent share one
`scriptedResponder` (the sub-agent reuses the parent's Upstream), so a scripted "parent delegates →
child Turns → parent finishes" drives the whole nested loop with **no real LLM and no real exec**.
**No new dep** (`go.mod`/`go.sum` unchanged). **What landed:**

- **The carried `/code-review` finding RESOLVED → ISOLATE** (`internal/security/guard.go`): `Guards`
  value-copy aliased the live breaker/audit through shared pointers. New **`Guards.ForSubAgent()`**
  returns a copy with a **fresh `CircuitBreaker`** (same threshold) + **fresh `AuditLog`**, but the
  **same `*DangerousActionGuard` shared by pointer** — read-only (only `Inspect`/`Rules`, no mutator),
  so the floor cannot be re-derived or loosened one level down. The misleading "threads verbatim / no
  live state" `Guards` comment is corrected to describe the aliasing honestly. Tests prove a sub-agent
  breaker trip does **not** trip the parent, a sub-agent audit append does **not** leak into the
  parent log, and the dangerous floor is the **same** guard instance (shared, unloosenable). Decision +
  rationale recorded in **ADR 0013**.
- **`sub_agent` is the recursion point, not a leaf** (`internal/tools/sub_agent.go`,
  `internal/agent/subagent.go`): a plain `domain.Tool` carrying **no disposition marker** (not
  read-only / workspace-writer / external / subprocess — asserted), registered in `DefaultTools` (now
  19 built-ins). `resolveAndExecute` recognises `SubAgentToolName` **after** the always-on guardrails
  (the dangerous floor still applies, tighten-only) but **before** the mode disposition, and drives a
  nested `Agent` — so each **child** call gets the full per-call blast-radius disposition one level
  down (a child subprocess confines, a child Apogee write is path-safety-bounded, a child MCP/external
  still gates), never the sub-agent "as a unit." The tool's own `Execute` errors if ever reached.
- **`newChildAgent` threads "≤ parent" structurally** (`subagent.go`): same Mode / Approver / Confiner /
  `confine-to-workspace` (verbatim, never loosened), a `registry.Subset` of the parent's tools
  (`defaultSubAgentTools` — built from the parent registry's own names, so an expansion is impossible),
  `Guards.ForSubAgent()`, the same Upstream + EventSink, fresh conversation (only the delegated task —
  no parent history/pending-input/approval-cache, the ADR-0008 boundary). The sub-agent's final
  assistant message is surfaced back to the parent as the `sub_agent` tool result.
- **`Depth` threaded** (`internal/agent/agent.go`, `loop.go`): `Agent` gains a `depth` field; the
  package-level `base(turn)` became the method **`(a *Agent) base(turn)`** stamping `Depth = a.depth`,
  so every event an Agent emits nests at its level with no per-call threading — top-level events stay
  `Depth == 0`, sub-agent events are `Depth == 1`. **P3.14 needs only this `Depth` on events.**
- **Recursion bounded (`maxSubAgentDepth = 2`), defence in depth**: a child constructed **at** the bound
  is never offered `sub_agent` (`defaultSubAgentTools` withholds it; `toolMenu` also lets `sub_agent`
  through in Plan, since it is bounded one level down), **and** the recursion point refuses defensively
  at the bound — so an unbounded sub-agent tower is structurally impossible (both paths tested).
- **Top-level-only stepping / atomic-within-the-Turn** preserved: the driver runs the nested Agent to
  its Exchange boundary in one shot; a cancel mid-sub-agent surfaces `dispatchCancelled` so the parent
  rolls the whole Turn back to its pre-`sub_agent` boundary; a child tool panic recovers at the parent
  boundary (ADR 0007) without killing the parent Exchange. The snapshot schema's "suspended sub-agent"
  slot stays reserved-but-empty (forward-compat for nested stepping).
- **Tests** (`internal/agent/subagent_test.go`, `internal/security/guard_test.go`): delegate-and-report,
  `Depth == 1` nesting, Plan-parent-cannot-write, subset-omits-tool, max-depth (withheld + refused),
  breaker isolation, dangerous-floor shared-read-only, child-panic-recovers-at-parent, arg validation —
  all green under `-race`. Registry count/order tests updated for the new `sub_agent` entry.

### P3.14 — TUI `Depth > 0` rendering
Turn the Phase-2 *tolerate* into *render*: frame/indent nested sub-agent events as a visually distinct
block in the transcript (a labelled, indented sub-section per sub-agent), keeping the C6 fold rules
per depth. No agent logic (ADR 0011 still holds — render only). **Acceptance:** a recorded nested event
sequence (`Depth 0 → 1 → 0`) renders with the sub-agent block indented/labelled and the parent stream
intact (golden); reflow at small sizes doesn't panic; the existing flat (`Depth==0`) goldens are
unchanged.

#### ✅ P3.14 result — landed 2026-06-24 (gate GREEN)

Turned the Depth-*tolerating* renderer into a Depth-*rendering* one — purely in `internal/tui`
render code, no Model state added (ADR 0011 holds: render only, no agent logic). The Phase-2 plumbing
was already complete (every `entry` carries `depth`, threaded from `e.Depth` through `transcript.apply`,
and `indentLines` indented 2 cols/level); P3.14 replaces "indent" with a **framed sub-section**:

- **Vertical-rail gutter** (`render.go` — `railLines` replaces `indentLines`; `railedWidth`): each
  physical line of a `Depth > 0` block is prefixed by one styled `│ ` gutter per nesting level
  (dim, `theme.subRail`), so a sub-agent block reads as a quoted/ruled sub-section rather than a bare
  indent. `railedWidth` narrows the wrap column by one gutter per level and floors at 1, so deep
  nesting at a tiny terminal width never divides by zero. **Depth 0 returns the lines/width untouched**
  — the flat top-level transcript renders byte-for-byte as before (the C6 tool-Turn golden is unchanged).
- **`⤷ sub-agent` label** (`render.go` — `renderSubAgentLabel`; emitted in `transcript.renderView`):
  a one-line header opens each *descent to a deeper level* (a `0→1`, and again a `1→2`, transition),
  framed inside the same rail as the block it announces. The run boundary is derived from each entry's
  `depth` inside the existing entry loop (a local `prevDepth`), so **no per-event state lands on the
  value-copied Model** — `TestModelNoBuilderByValue` stays structurally green.
- **Glyphs** (`theme.go`): `glyphSubRail = "│"`, `glyphSubLabel = "⤷"`, `subAgentLabel = "sub-agent"`,
  and the dim `subRail` style. No new ADR (the §5 line lists no ADR for P3.14 — ADR 0011 still governs).
- **Visual** (width-60 sample): the parent stream stays flat/unframed; the sub-agent run is
  `│ ⤷ sub-agent` then `│ ✦ …` / `│   ┕ …` lines; a `0→1→2` climb nests as `│ │ …`.

**Tests** (`transcript_test.go`, `render_test.go`, `model_test.go`): the old indent-only tolerance
asserts were updated to the framed/labelled treatment (the "tolerate→render" change *is* the spec, not
a weakening) — `TestTranscriptDepthRendersFramedBlock`, the `Depth 0→1→0` acceptance golden
(`TestTranscriptDepthNestedSequenceGolden`), the per-level-label golden
(`TestTranscriptDepthLabelsEachLevel`), small-width reflow safety (`TestSubAgentReflowAtSmallWidths`,
`TestRailedWidthFloors`), and the Model-level framed render (`TestModelRendersNestedDepth`) — all green
under `-race`. The flat goldens and `TestWrappedOffsetMatchesViewport` are unchanged. Full §7 gate green
(gofmt/vet/darwin-vet/build/`-race`/self-import-grep/6 cross-builds/mod-tidy/`--help`).

### P3.15 — MCP client
Build `internal/mcp` on the official Go SDK (pin from P3.0): connect over stdio / SSE / streamable-http
from config; **surface each server tool into the `ToolRegistry` as an `ExternalEffectTool`** (effect
kind `mcp`) so D3/D5 gate it through Approval in Auto under `confine=true` **for free**; **resume reconnects fresh**
(ADR 0008 — no server-side-state promise); clean shutdown on `Close`. Record the client shape (ADR
0014 or a design note — D3). **Acceptance:** a hermetic stdio MCP server (a test fixture) exposes a
tool that appears in the menu, is callable, and **raises Approval in Auto** (asserted); a resumed
session re-establishes the connection from scratch; the bench swaps a deterministic stub with no
process; `Close` tears down the server cleanly (no orphan). Cross-build green (the SDK is pure-Go).

#### ✅ P3.15 result — landed 2026-06-24 (MCP client on go-sdk v1.6.1; client shape → design note; gate GREEN; hermetic stdio fork-and-exec fixture under `-race`)

Built `internal/mcp` on the official Go SDK (`github.com/modelcontextprotocol/go-sdk` **v1.6.1**, now
a **direct** require — `go mod tidy` clean, idempotent; the 6 CGO_ENABLED=0 cross-builds stay green,
the SDK is pure-Go). The integration is exactly D3's "surface with the right effect kind": **no
dispatch change** — the existing disposition already classifies `EffectMCP` as `classMCP` and gates
it in Auto (proven in `dispatch_test.go`), so the gate held the moment a real MCP tool carried the kind.

- **`internal/mcp` (client.go / transport.go / tool.go / doc.go).** `Connect(ctx, []ServerConfig,
  URLGuard) → *Client` dials every configured server (stdio / SSE / streamable-http), lists its tools,
  and surfaces each as a `serverTool` — a `domain.ExternalEffectTool` of kind **`mcp`** named
  `<server>__<tool>` (collision-safe across servers). `Client.Tools()` hands the discovered set to the
  registry; `Client.Close()` joins every session's teardown (no orphan). **Connect is all-or-nothing:**
  a later server's failure rolls back the already-opened sessions and returns the error. **Zero servers
  ⇒ dormant** (no sessions, nil tools, no-op Close). **Resume reconnects FRESH** (ADR 0008 — the Client
  holds no serializable state; the composition root calls `Connect` on every launch, resume included).
- **Trust boundary (doc.go).** Server description / schema / result are **untrusted** — presented to
  the model, never executed. An SSE / streamable-http endpoint rides the same default-on, resolved-IP
  **SSRF floor** (`security.URLGuard`, pre-flight + dial-time) as the network tools; a stdio server is
  a trusted local launch (no URL floor), its tool calls still gate through Approval in Auto. A
  server-side tool error round-trips as an `IsError` `ToolResult` (ADR 0007 — Go error only on ctx cancel).
- **Composition wiring (`cmd/apogee`).** `config.yaml`'s `mcp-servers:` block (config-file-**only**,
  default-empty, no flag/env — like `web-search-endpoint`) → `mcp.ServerConfig` → `mcp.Connect` in
  `wire.go` → `registryWithMCP` registers the discovered tools atop the default registry → `Config.Tools`;
  `defer mcpClient.Close()`. A connect failure is **fatal** (a configured-but-unreachable server is a
  misconfiguration the user should see, not a silent drop); a qualified-name collision with a built-in
  is dropped with a stderr notice (built-in wins). `layer` gained a slice field, so the absent-config
  assertion moved from `!=` to `reflect.DeepEqual` (a compile-fix, not a behaviour change).
- **Client shape → design note, not ADR 0014** (the D3 judgement): the *decision* (MCP =
  ExternalEffect ⇒ Approval-gated) is already ADR 0004/0008/0012; only the *client shape* is new →
  [`docs/design/mcp-client.md`](../design/mcp-client.md) (transports, lifecycle, trust boundary,
  acceptance map). No new public root surface (ADR 0010 / D7): `internal/mcp` exports `Client` /
  `Connect` / `ServerConfig` / `Transport`, imports only the SDK + `domain` + `security`.

**Tests** (`internal/mcp/mcp_test.go`, all `-race`): a **hermetic stdio** MCP server via the SDK's
fork-and-exec trick (`TestMain` re-execs the test binary as a fixture server — no network, no external
binary, runs everywhere) drives the real `Connect → buildTransport(stdio) → CommandTransport →
ListTools → CallTool → Close` path. `TestConnect_SurfacesServerToolsAndCalls` (tool in the menu,
server schema, callable end to end); `TestServerTool_IsMCPExternalEffect` (the **real** surfaced tool
reports `EffectMCP` — the property the Auto gate keys on); error-result + cancelled-ctx round-trips;
`TestResume_ReconnectsFresh`; `TestClose_TearsDownSessions` (idempotent); dormant-client + bad-name +
SSRF-floor-block + transport-selection + all-or-nothing-rollback + `renderContent` branches.
`cmd/apogee` gained `TestApplyConfigMCPServers` (the `mcp-servers` YAML → `opts.mcpServers` mapping).
Full §7 gate green (gofmt/vet/darwin-vet/build/`-race`/self-import-grep/6 cross-builds/mod-tidy/`--help`).

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
