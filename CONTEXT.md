# Apogee

Apogee is a terminal **coding agent** for small local LLMs (~4B–35B) that owns the
full agentic loop — provider, tools, context, and sessions — and runs a layer of
gated, self-regulating **Mechanisms** inside that loop to keep small models on track.
The hard constraint, inherited unchanged from the predecessor projects: **Apogee's
Mechanisms must never make the underlying model perform worse than the same agent with
those Mechanisms off.** That floor is **Bypass mode** (Mechanisms off, structure on) —
**not** a naked model, because Budget and Compaction are structural and load-bearing (a
truly naked model just overflows its context window). The constraint is **proved at bench
time** as a ground-truth, distributional non-inferiority gate against Bypass (see
[ADR 0009](docs/adr/0009-the-ab-decision-rule.md)); in production it is only
*approximated* by self-regulation (Adaptive Suppression + the Turn Budget), a weaker,
proxy-based safety net — not the guarantee.

This glossary is a fresh start, not a migration of `apogee-sim`'s `CONTEXT.md`. The
predecessor project was *middleware* between a coding tool and a model; Apogee **is**
the coding tool now. Terms that described the old middleware structure are retired (see
[Retired terms](#retired-terms)); the language below describes the agent.

## Language

### Identity and shape

**Apogee** (the coding agent):
The terminal-based agent that owns the agentic coding loop end-to-end for a small local
LLM: it builds each request, calls the Upstream, parses the response, dispatches tools,
and applies Mechanisms — all in one cross-platform Go binary. It is no longer a layer
between a coding tool and a model; it is the coding tool.
_Avoid_: "the proxy", "Apogee Core", "the extension", "middleware" (all describe the
retired predecessor structure).

**Embeddable agent** (the public API):
The public Go package other applications import to construct and run an Apogee agent
in-process. Apogee ships as **both** a ready-to-use terminal tool (the `cmd/apogee` TUI +
CLI — the headline product) **and** this reusable library: the TUI, the optional `apogee
headless` CLI, and the bench are all consumers of one public package over the same
engine. The repo is the whole tool, not just the library. The public surface is guarded
and versioned; everything else lives in `internal/`. See
[ADR 0001](docs/adr/0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md).
_Avoid_: "**Apogee Core**" (retired — it named the proxy-era transform engine, a
different thing; do not resurrect the name for the new library), "the SDK".

**Sub-agent**:
A nested, focused agent loop the top-level agent spawns for one delegated sub-task, with
its own Session and a reduced context Budget. It is itself an instance of the **Embeddable
agent**, spawned in-process; its events nest into the parent's event stream at **`Depth =
parent+1`**. Its privileges are always **≤ the parent's** (mode, guardrails, Confinement,
tool set) — see [ADR 0005](docs/adr/0005-sub-agent-privileges-are-bounded-by-the-parent.md).
The *shape* is [ADR 0013](docs/adr/0013-the-sub-agent-orchestrator-is-the-recursion-point-with-isolated-live-guard-state.md):
the model reaches it through a **`sub_agent` tool** that dispatch treats as a **recursion
point** (not a leaf — never confined/gated as a unit; each *child* call gets the per-call
disposition one level down), the orchestrator threads mode/approver/confiner/tool-subset
verbatim-or-stricter, the sub-agent's **live guard state is isolated** (a fresh
circuit-breaker + audit log — `Guards.ForSubAgent`) over a **shared, read-only
dangerous-action floor** (unloosenable one level down), and recursion is depth-bounded.
Bare "agent" means the **top-level** agent unless qualified as "sub-agent".
_Avoid_: "child agent" (says nothing about the privilege bound), "worker".

**The loop** (the agent loop):
Apogee's core control flow: build request → call Upstream → parse response → dispatch
tools → repeat, emitting typed events at each step. The loop owns tool execution and
conversation state — which is precisely what lets formerly lab-only Mechanisms (e.g.
`correct_tool_result`) become first-class. Lives under `internal/agent/loop/`.
_Avoid_: "the pipeline" (that was the proxy-era Transform chain — a narrower thing).

**Upstream**:
The local LLM server that runs the model — Ollama, llama.cpp, LM Studio, vLLM, or any
endpoint honouring the OpenAI HTTP surface. Apogee reaches the Upstream directly through
its `provider/` package; there is no intervening proxy.
_Avoid_: "the model server", "the backend" (a `backend` detector package may exist, but
it detects Upstreams — it is not the Upstream).

### Turns and stepping

**Turn**:
One iteration of the loop — a single *primary* Upstream call and the work that follows it
(parse → dispatch tools → apply Mechanisms). Compaction's summarisation call is *internal*
to a Turn, not a Turn of its own. The unit of self-regulation and of bench measurement.

**Exchange**:
One user input through to the final no-tool response — usually several Turns. The
user-facing unit of a conversation.

**Step**:
The bench/embedder primitive that advances the loop **one Turn** and returns at a
**quiescent boundary** — no in-flight stream or tool call, conversation state fully
serializable. Approval and streaming happen *inside* a Step; **snapshot, resume, and the
bench's fork are valid only at the quiescent boundary**. Cancellation is delivered through
Step and takes effect cleanly at that boundary. Sub-agent stepping is **top-level-only for
v1** (designed swappable for nested stepping later). See
[ADR 0007](docs/adr/0007-step-turn-and-the-quiescent-boundary.md).
_Avoid_: "tick", "cycle" (Turn is the loop iteration; Step is the externally-driven advance
of one Turn).

### Safety and autonomy

**Agent mode**:
The autonomy level governing which tool calls need human approval — a **monotonic
privilege ladder**. Four:
- **Plan** — read-only; no writes or command execution (explore and propose, touch nothing).
- **Ask-Before** — workspace reads run free; every write, command, and external reach
  requires an Approval (the human is the gate).
- **Allow-Edits** — Apogee's own **workspace-scoped edits** (path-safety-bounded) run
  without asking; shell/exec, network, MCP, and anything out-of-workspace still gate.
  Needs **no Confinement** — path-safety bounds the auto-approved writes and the human
  backstops the unbounded surface, so it is **identical on every OS**.
- **Auto** — adds the unbounded **shell/subprocess** surface to the auto-approved set, so it
  is the one **unsupervised** mode. Its blast radius is tuned by the global
  **`confine-to-workspace`** flag (ADR 0012): **on** (default) OS-**Confines** the subprocess
  surface to the workspace with the **network open**, and still gates **MCP**; **off** ("I am
  the sandbox") runs unconfined — safe only inside a VM. Apogee's own network tools
  (`web-fetch`/`http-request`) auto-run url-filtered in both (they no longer gate in Auto —
  ADR 0012 reversed ADR 0004 here).
_Avoid_: "permission level", "trust mode".

**Bypass mode**:
A `Config` flag **orthogonal to Agent mode** that turns Apogee's Mechanisms off while
leaving the agent's structure intact. It disables the `proactive-nudge` and
`response-repair` Mechanisms and makes the **Library inert** (no inject, no observe, no
write), but **keeps the exempt off-ramps** (e.g. `empty_response_recovery`) so the floor is
*functional* — a baseline that quit at the first stumble would pass the hard constraint
trivially. Budget, Compaction, and the rest of the loop still run: Bypass is the honest
"Mechanisms-off" floor, **not** a naked model. It is also the bench's **aggregate control
arm** — the same code path users can run — against which the hard-constraint non-inferiority
gate is proved. See [ADR 0006](docs/adr/0006-bypass-mode-is-the-mechanisms-off-floor.md).
_Avoid_: "naked model" (Bypass keeps the structural reducers on), "disabled mode", "raw mode".

**Approval**:
The human-in-the-loop gate on a single tool call — the primary safety guarantee in
Ask-Before mode. Delivered through a delegate the host (TUI, embedder) supplies.

**Ask-user**:
A free-text question the model puts to the human mid-task (via the `ask_user` tool), answered
through a host-supplied **`Asker`** delegate — the public analogue of the **Approver**, but
**not** a safety gate: it carries no allow/deny semantics, never bypasses the disposition, and
is `ReadOnly` (it runs even in Plan, mode-independent). A `nil` Asker means the tool is simply
not registered; a headless host must supply an Asker that **fails safe** (no hang). The TUI
implements it as an input-prompt rendezvous (the free-text sibling of the Approval prompt); the
bench as a scripted responder. Added in P3.11.
_Avoid_: conflating it with Approval — an answer is not a permission.

**Confinement**:
OS-level restriction of the **unbounded subprocess surface** (shell / subprocess), attaching to
**blast radius, not to a mode-wide binary** (ADR 0012, superseding ADR 0004): a tool runs
unsupervised only if its blast radius is bounded — **either** by OS confinement of the subprocess
surface (Linux **landlock** applied pre-`execve` on the child; macOS **`sandbox-exec`** wrapping
the child — one clean subprocess granularity on both OSes), **or** by Apogee's own
**path-safety-to-workspace** for its own in-process write tools (a third-party in-process tool,
whose scoping Apogee cannot vouch for, gates instead of running unsupervised). It is a **capability
matrix, not a one-bit flag**: each backend reports what it can enforce (`fs-write`, `network-egress`,
…). In **Auto** the network is **open by default**, so **`AutoEligible()` requires filesystem
confinement only** — Linux Auto needs landlock ABI ≥1 (kernel ≥5.13), not ABI v4. The unbounded
surface is tuned by the global **`confine-to-workspace`** flag (below). The per-tool teeth remain:
**MCP**, which executes in a server Apogee cannot fence, gates through Approval whenever
`confine-to-workspace` is on; and if fs-confinement is *unavailable* on the host, subprocess tools
gate too ("confine if you can, gate if you can't") rather than refusing Auto. Apogee never runs a
tool call both unsupervised *and* unbounded. Lives behind a `platform/` `Confiner` interface
(seatbelt / landlock / AppContainer); default box = workspace-write-only + **network-open** +
per-project allowlist. See
[ADR 0012](docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md).
_Avoid_: "sandbox" (that is the bench's term — see below), "jail".

**`confine-to-workspace`** (the Auto blast-radius flag):
A global-config key (`~/.apogee/config.yaml`, default **`true`**) that tunes **Auto**'s blast radius
(ADR 0012); meaningful only in Auto. **`true`** fences filesystem writes to the workspace —
a subprocess escape is **OS-blocked**, an Apogee in-process out-of-workspace write raises
**Approval** — with the network open and MCP gated. **`false`** ("I am the sandbox") runs Auto
unconfined, safe **only inside a VM** (the user's responsibility); it is **global-only** (a project
config cannot loosen it — the hostile-repo footgun is closed) and prints a per-session warning. The
**only blanket *loosen*** in the system — every other knob (the dangerous-action guard, the deferred
tool×mode matrix) is tighten-only.
_Avoid_: "YOLO mode" (informal; it is a flag on Auto, not a fifth mode),
"`--dangerously-skip-permissions`" (names Claude Code's analogue, not this flag).

**Safety guardrails**:
Apogee's production safety set: Agent modes, Approval, path-safety (TOCTOU-safe at use time via a
Go 1.26 `os.Root` pinned at the workspace root — `security.SafeWriteFile`/`SafeReadFile`, so an
escaping symlink component swapped after the check is refused at write/read time), **url-safety**
(the network tools' `URLGuard` — scheme/host allow-deny plus a **default-on SSRF floor** that denies
loopback / private / IMDS / link-local **plus** RFC-6598 CGNAT `100.64/10`, the whole `0.0.0.0/8`,
TEST-NET / benchmark ranges, and NAT64-embedded private/loopback `64:ff9b::/96` **by resolved IP**,
re-checked at dial time so DNS-rebinding is closed; tighten-only, never dissolvable by config),
tool-argument-guard (incl. the **Dangerous-action guard** floor, the `http_request` header filter,
a leading-`-` guard on git ref args, and **`web_search` API-key redaction** — only the bare endpoint
host, never the key-bearing request URL, reaches a model-facing error), circuit-breaker, and a
**bounded audit log surfaced on the `EventSink`** (`domain.AuditEvent`, so the trail is observable —
a sub-agent's records reach the parent observer at `Depth>0`, not lost with the discarded child).
The human-in-the-loop model — distinct from Confinement (OS-level) and from the bench's Sandbox.
_Avoid_: "the sandbox" (Apogee production is **not** sandboxed; "Sandbox" is a bench term
for the bench's `RealSandbox` that confines *unsupervised* sim runs — do not use it for
Apogee's production execution).

**Dangerous-action guard**:
A **footgun-guard — *not* a security boundary** — that refuses a small model's obvious
catastrophic *mistakes* before execution, in **every** mode independent of Confinement (ADR 0012;
lives in `internal/security`, P3.6). Two tiers: **hard-refuse** (`rm -rf` of a root/home/system
path, fork bombs, writes to `~/.ssh`/credential/persistence files — no per-call override) and
**force-approval** (`curl | bash`-class — sometimes a legit installer, so a speed-bump that forces
the Approver even in Auto). It is **tighten-only** and trivially bypassable by anything determined,
so it **never** makes `confine-to-workspace=false` "safe" — only the VM does. Default-on; the global
config may add *or* remove entries (it is the user's machine), a project config may only *add*.
_Avoid_: "malicious-action filter", "blacklist", "denylist" (all imply an adversary boundary it is
not — it guards against mistakes, not attackers).

**Resolution**:
The single, complete verdict for one tool call, computed in full before anything executes —
covering *every* rule that decides the call's fate: the tighten-only guardrail floor, the
autonomy-ladder × blast-radius table, confinement capabilities, and the contingencies for what
can only be discovered at run time. Dispatch *executes* a Resolution; it never decides.
Subsumes the Phase-3 term **"per-call disposition"**, which named only the ladder-table stage
that runs *after* the guard clears (ADR 0012/0013 and the confinement-execution contract use
"disposition" in that narrower sense).
_Avoid_: "disposition" in new code and docs (retired — it under-claimed what the verdict
covers); "policy decision" (vague).

**MCP client** (Model Context Protocol):
Apogee's client for external **MCP** servers, on the official Go SDK over **stdio / SSE /
streamable-http** (`internal/mcp`, P3.15). It connects the servers a host lists in `config.yaml`'s
`mcp-servers:` block (config-file-only, default-empty ⇒ dormant), discovers each server's tools, and
surfaces them into the registry as `ExternalEffectTool` of kind **`mcp`** named `<server>__<tool>`.
An MCP server is an **external, untrusted** process Apogee **cannot confine**, so its tools always
**gate through Approval in Auto** under `confine-to-workspace=true` (the per-tool teeth above) — and
their description / schema / result are untrusted data shown to the model, never executed. An http(s)
server (sse / streamable-http) passes the same **url-safety SSRF floor** as the network tools; a
stdio server is a trusted local launch (no URL floor), its calls still gate. **Resume reconnects
fresh** — no server-side state is restored (ADR 0008). The *client shape* is
[docs/design/mcp-client.md](docs/design/mcp-client.md); the *gating* is ADR 0004/0008/0012.
_Avoid_: "MCP plugin", "MCP proxy" (it is a client; there is no proxy).

### Mechanism and hook points

**Mechanism**:
A unit of gated, self-regulating behaviour that fires at a defined **Hook point** in
the loop to help a small LLM. The catalogue of Mechanisms is the current best guess at
what helps, decided by evidence (the external bench), not a fixed contract. Every
Mechanism is *gated* (by conversation state, resource pressure, prompt shape, or model
output) and subject to self-regulation unless declared exempt.
_Avoid_: "intervention" (that is the bench's per-Turn experiment — a different surface,
see [Intervention](#intervention)), "transform"/"analyzer"/"injector" as a *kind* (these
were the retired proxy-era taxonomy — see below), "rule".

**Hook point**:
*Where* in the loop a Mechanism fires — the primary classification of a Mechanism.
Four positions plus a cross-cutting capability:
- **pre-request** — shape the outgoing request before it is sent (subsumes the old
  Transforms *and* Pre-pipeline Injectors).
- **post-response** — inspect the model response and choose an action (see below)
  before the loop acts on it.
- **pre-tool-exec** — act between the decision to run a tool and its execution.
- **post-tool-result** — act on a tool result before the model next sees it (home of
  `correct_tool_result`). New to the loop; the proxy could not host it.
- **history-rewrite** — a capability that edits conversation state (home of
  `truncate_history`); may attach at more than one point.
_Avoid_: "stage" (a pre-request-only, pipeline-era word), "phase".

**Post-response decision**:
The action a post-response Mechanism chooses: **retry** (re-call the Upstream now),
**intercept** (alter the response before the loop acts on it), or **defer** (schedule a
correction into the *next* request). Streaming failures can only defer.
_Avoid_: "interceptor" (intercept is one decision, not the Mechanism).

**Deferred Response Action vs Request-prep Hint**:
Two sources of a pre-request injection, kept distinct because they are debugged
differently. A **Deferred Response Action** is a *defer* decision made by a
post-response Mechanism on the *previous* turn, consumed from session state this turn
(look in **session state**). A **Request-prep Hint** is derived fresh from conversation
history at the start of *this* request (look in **conversation history**). Both fire at
the pre-request hook and are tracked uniformly as Mechanisms.

**Mechanism descriptor**:
Per-Mechanism metadata orthogonal to its hook point: `Capability` (off-ramp /
proactive-nudge / response-repair), `SuppressionPolicy` (exempt or strikes-3), and the
set of Mechanisms it is declared incompatible with (constrains stacking). The single
source of truth for which Mechanisms are exempt and which can co-fire.

### Self-regulation

The runtime machinery that keeps a Mechanism from hurting the model — the operational
half of the hard constraint. All of it is per-Session; a new Session starts clean.

**Effectiveness tracking**:
Per-Mechanism, per-Session bookkeeping that records each time a Mechanism fires and judges
whether the next Turn was better for it. The data behind Adaptive Suppression and the Turn
Budget.

**Adaptive Suppression**:
The **per-Mechanism** withdrawal rule: a Mechanism judged not-helpful several consecutive
times in a Session is suppressed for the rest of it, with a configurable clear-path that
re-opens it on a productive Turn.

**Turn Budget**:
The **global** withdrawal rule: after several consecutive non-productive Turns (no new file
read, no file written), all non-exempt Mechanisms are suppressed for the rest of the
Session, cleared when productive activity resumes.

**Off-ramp** (Exempt Mechanism):
A Mechanism never subject to Adaptive Suppression or the Turn Budget, because suppressing
it would leave the model with **no way out of a failed Turn** (e.g. `empty_response_recovery`
— without it an empty response just ends the conversation). Exempt status is declared in
the Mechanism descriptor.
_Avoid_: "always-on Mechanism" (a structurally-always-on Transform is not the same as an
off-ramp — the former is just untracked, the latter is a deliberate recovery guarantee).

**Library**:
Apogee's **cross-session, per-model learning store**: it observes completed Turns and
records per-model observations with Bayesian confidence, then a pre-request Mechanism
injects qualifying observations to make Apogee better at a given model over time.
File-backed under `~/.apogee/library/`. It keys observations on a **confidence-tagged
`ModelFingerprint`**, resolved best-available — **weights-hash (high)** → **behavioral probe
(medium)** → **metadata label (low)** — where **confidence gates injection** ("prefer not to
inject under uncertainty"). The old "Library vs Failure library" ambiguity is **resolved by
the repo split**: the runtime Library lives in Apogee; the development-time **Failure
library** is now a [bench](#validation-and-the-bench) artifact.
_Avoid_: "the failure library" (that is the bench's term), "cache" (the Library is learned
evidence, not a cache), "keyed on the model name" (that was the predecessor's gap — keying
is now the fingerprint).

### Context and history

These four are distinct operations — "compress", "compact", and "truncate" must **not**
be used interchangeably.

**Budget**:
The allocation of the model's context window across the parts of a request — system
prompt, conversation history, file context, and response reserve. The single authority
on how much room each part gets; other reducers consume it. Lives in `context/`.
_Avoid_: "context limit" (that is the raw window; the Budget is the *allocation* within it).

**File reference (`@file`)**:
A workspace file the user names with an `@path` token in their message. The loop resolves
each reference at the start of the Turn — reading it within the workspace fence
(`security.SafeReadFile`, `os.Root`-pinned) and injecting its content into the user message
as that request's *file context* — and reports-and-skips a missing or escaping ref. Parsing
the token is the TUI's job; resolution is the agent's.
_Avoid_: "attachment", "upload" (a reference is read live from the workspace, not stored).

**Skill**:
A reusable block of instructions the user *attaches* to a message with `/skill` — a folder
holding a `SKILL.md` (YAML frontmatter — id, display name, summary — plus a Markdown body).
Skills are discovered from layered dirs (the global `~/.apogee/skills`, the project's
`.apogee/skills`, and — when `use-project-skills` is on — the project's `skills/`), the later
source winning a name clash. Like an `@file`, a skill is **turn-local**: the loop resolves the
attached IDs through `Config.Skills` and prepends each body to *that one* user message, so a
skill never persists as a system-prompt edit. The TUI picks and attaches; the agent resolves.
_Avoid_: "plugin", "tool" (a skill is prompt text, not executable; it adds no capability — it
steers the model). Distinct from a **Mechanism** (a catalogued, self-regulating loop behaviour).

**Tool-result capping**:
Per-tool-result truncation of any single result that exceeds its fraction of the Budget,
with head/tail preservation, protecting the most recent Turn. A **pre-request Mechanism**;
implemented **once** (the surviving half of the predecessor's `compress`).
_Avoid_: "compression", "compaction" (capping is per-result and non-generative).

**Compaction**:
The **default** conversation-level reducer: *generatively* summarising older Turns into a
summary via the model, when the conversation exceeds a threshold. Meaning-preserving but
costs an extra model call.
_Avoid_: "compression", "truncation" (Compaction is generative and summarises).

**History truncation**:
The **cheap alternative** to Compaction: *mechanically* dropping the middle of the
conversation, keeping the last N exchanges. Config-gated, **off by default**; a
history-rewrite Mechanism, validated against Compaction via the bench.
_Avoid_: "compaction" (truncation is mechanical and lossy, not generative).

### Validation and the bench

**The bench** (apogee-sim):
The external Go module that validates Apogee by **importing it as a library** and driving
the agent loop in-process — owning the sandbox, stepping turns, forking counterfactuals,
and scoring outcomes against real local LLMs. It is a development-time instrument; its
code is never linked into the shipped `apogee` binary. apogee-sim keeps its own glossary
(Sim, Baseline, Intervention, Trace archive, Frontier driver, …); those are *bench*
terms, not Apogee terms. See [ADR 0001](docs/adr/0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md).
_Avoid_: "the harness" inside Apogee's own docs (there is no harness in Apogee), "the
external service" (it's a sibling Go module, not a running service).

**Experimental hook**:
A temporary hook the bench registers in-process at a [Hook point](#mechanism-and-hook-points)
to test a behaviour that is **not (yet) a Mechanism**. It never ships in the binary; if it
earns its place on the evidence, it is promoted to a gated Mechanism in Apogee. The
in-process heir to the bench's portable-tier Interventions (`system_addendum`,
`inject_message`, `tool_filter`).
_Avoid_: "intervention" (that is the bench's term for its own experiment surface), calling
it a Mechanism (it is a candidate, not a catalogued one).

### Retired terms

These were canonical in `apogee-sim/CONTEXT.md` and are **deliberately dropped** because
they name a structure that no longer exists. Recorded here so a reader who knows the old
vocabulary can map forward:

- **Apogee Core** → there is no standalone transform engine + public facade; the loop
  is the source of behaviour.
- **Integration** / **Apogee Proxy** / **OpenCode Plugin** → retired; Apogee is a single
  integrated tool, not a Core exposed through peer integrations.
- **Coding tool** (the external client sense) → Apogee *is* the coding tool now; there is
  no external client to name.
- **OpenAI HTTP surface** / **Chat-completion shape** (as a public contract) → Apogee no
  longer promises a wire contract to external clients. (The provider still *speaks* the
  OpenAI chat schema to the Upstream, but that is an internal client concern, not a
  contract Apogee exposes.)
- **Transform** / **Response Analyzer** / **Pre-pipeline Injector** (as the three
  *kinds* of Mechanism) → retired as the taxonomy; Mechanisms are now classified by
  [Hook point](#mechanism-and-hook-points). The distinctions that still matter survive
  as attributes (post-response decisions; Deferred-Action vs Request-prep-Hint).
