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
headless` CLI (deferred — no subcommands ship yet), and the bench are all consumers of one
public package over the same engine. The repo is the whole tool, not just the library. The public surface is guarded
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
`correct_tool_result`) become first-class. Lives in `internal/agent/loop.go`.
_Avoid_: "the pipeline" (that was the proxy-era Transform chain — a narrower thing).

**Upstream**:
The local LLM server that runs the model — Ollama, llama.cpp, LM Studio, vLLM, or any
endpoint honouring the OpenAI HTTP surface. Apogee reaches the Upstream directly through
its `provider/` package; there is no intervening proxy.
_Avoid_: "the model server", "the backend" (a `backend` detector package may exist, but
it detects Upstreams — it is not the Upstream).

**Model profile**:
The per-model description Apogee carries of *how a given small model speaks the wire* — two
**orthogonal** axes: its **tool-call format** (native structured `tool_calls`, or a text format
the model emits inline in its content — **markdown-fenced** or a **custom regex**) and its
**thinking channel** style. Orthogonal because a model can emit native tool calls *and* inline
thinking (gpt-oss does both). The profile drives **both directions at the seams**: on the
**parse** side it selects which parser and content-stripper the loop applies to incoming
content; on the **emit** side, for a non-native tool-call format, the engine tells the model
how to speak — rendering the tool menu and format-emission instructions as text into the
request and suppressing the native `tools` array (a non-native template would otherwise be
double-told, or choke on an array it cannot render). A **zero profile is the native,
no-inline-thinking default** (today's behaviour) — it adds nothing to the request in either
direction. It is a `domain` type on `Config` (declarative data — [ADR 0010](docs/adr/0010-package-layout-domain-core-and-thin-root-facade.md)),
translated to the `processing` parsers at the boundary, not the parsers' own config.
_Avoid_: "model config" (overloaded with sampling/endpoint knobs), "adapter", "format" alone
(there are two axes, not one).

**Thinking channel** (a model's private reasoning):
The reasoning stream a model emits separately from its user-facing answer — either **delimited**
inline (`<think>…</think>`), **harmony** (gpt-oss's `<|channel|>analysis…<|message|>…`), or split
out by the Upstream into a `reasoning_content` field. Apogee **strips** inline channels from
visible content and preserves them as reasoning in history; it never sends them back Upstream.
Harmony is a *content-stripping* concern only — a harmony model's tool calls arrive **native**
(the Upstream parses harmony server-side), so there is no harmony tool-call text parser.
_Avoid_: "chain-of-thought" (a prompting technique, not the wire channel), "commentary" (that is
one harmony sub-channel, not the whole concept).

### Turns and stepping

**Turn**:
One iteration of the loop — a single *primary* Upstream call and the work that follows it
(parse → dispatch tools → apply Mechanisms). Compaction's summarisation call is *internal*
to a Turn, not a Turn of its own. The unit of self-regulation and of bench measurement.

**Exchange**:
One user input through to the final no-tool response — usually several Turns. The
user-facing unit of a conversation. In code the Exchange is derived from the conversation —
the messages strictly after the last user message — as a domain working value
(`internal/domain`'s `ExchangeView`) consumed by the loop and by Mechanisms. One engine
exception: the abort-rollback boundary stays a cached field read through
`Agent.exchangeBoundary()`, because a mid-Exchange truncation can drop the opening user
message the derivation would need
([ADR 0017](docs/adr/0017-the-exchange-is-a-derived-domain-working-value.md) §2's recorded
fallback).

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
tool×mode matrix) is tighten-only — and the **Host acknowledgement** (below) is that same loosen
scoped to one machine, not a second kind of it.
_Avoid_: "YOLO mode" (informal; it is a flag on Auto, not a fifth mode),
"`--dangerously-skip-permissions`" (names Claude Code's analogue, not this flag).

**Host acknowledgement** (`unconfined-hosts:`):
The user's recorded claim that **one named machine** is disposable, so Auto may run unconfined
*there* — the same loosen as `confine-to-workspace: false` at the grain the claim is actually true
at (ADR 0012, amendment 2026-07-21). A global-config-only list of entries (`id`, `acknowledged`,
`note`) matched against the current **host id**; resolution is: explicit
`confine-to-workspace: false` → unconfined everywhere; else a host-id match → unconfined here; else
confined. It exists because the flag is **global** while the claim is **host-specific**, so a
throwaway container's acknowledgement must not follow `~/.apogee/config.yaml` onto a laptop. The
host id is a **safety interlock, not authentication** — it stops an acknowledgement travelling
unnoticed, it does not resist forgery (anyone who can edit the config can write any id) — and it
fails **closed**: an unmatched host is simply confined again, and a machine with no identity to
match by (no hostname *and* no machine id, so its id is shared by every such machine) is refused as
an identity in both directions — the match is ignored and `--save` will not write it. Written only
by an explicit user act (`/confine off --save`); an unknown id is "not this host", never an error.
_Avoid_: "trusted host" (it is not a trust store, and nothing is verified), "whitelist",
"per-host confine-to-workspace" (the key is global; only the acknowledgement is host-scoped).

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
The action a post-response Mechanism chooses: **retry** (re-call the Upstream now, **in
place** — the correction rides `ActionRetry`'s `Inject` onto the in-flight request and
re-streams **within the same Turn**, R1), **intercept** (alter the response before the loop
acts on it), or **defer** (schedule a decision into the *next* request — a correction, or
carried work a Mechanism consumes across coming Turns, such as a queue of
decided-but-not-yet-delegated steps). Corrections deliver
by **retry-in-place**: the loop owns the stream and can reset it (`StreamResetEvent`), so —
unlike the proxy-era predecessor, which had already streamed the response downstream and could
only defer — a streaming response is **not** forced to defer. `defer` remains available but the
wave-1 repairs no longer use it.
_Avoid_: "interceptor" (intercept is one decision, not the Mechanism).

**Deferred Response Action vs Request-prep Hint**:
Two sources of a pre-request injection, kept distinct because they are debugged
differently. A **Deferred Response Action** is a *defer* decision made by a
post-response Mechanism on the *previous* turn, consumed from session state this turn
(look in **session state**). A **Request-prep Hint** is derived fresh from conversation
history at the start of *this* request (look in **conversation history**). Both fire at
the pre-request hook and are tracked uniformly as Mechanisms. A Deferred Response Action is
**Exchange-scoped**: it is a decision about the *next request of the same conversation flow*, so
the queue is cleared whenever an Exchange ends (a completed final answer, a fault, or an abort) and
is truncated-then-restored when a cancelled Turn is rolled back — a stale directive never crosses
an Exchange boundary or survives as two contradictory copies.

**Mechanism descriptor**:
Per-Mechanism metadata orthogonal to its hook point: `Capability` (off-ramp /
proactive-nudge / response-repair), `SuppressionPolicy` (exempt or strikes-3), and the
stacking relations — the set of Mechanisms it is declared incompatible with, and the set
it **requires** enabled (an enable-time constraint: switching a Mechanism on without its
requirements is a config error, so dependent Mechanisms are benched and shipped as a
stack). The single source of truth for which Mechanisms are exempt, which can co-fire,
and which only make sense together.

### Self-regulation

The runtime machinery that keeps a Mechanism from hurting the model — the operational
half of the hard constraint. All of it is per-Session; a new Session starts clean.

**Effectiveness tracking**:
Per-Mechanism, per-Session bookkeeping that records each time a Mechanism **acts** — an
intervention (a non-zero decision or a mutated working value), **not** a bare inspect-only
invocation (R4, so `LoopView.Fired` counts actions, matching the sim's `FiredCounts`) — and
judges the **next** Turn for it. That judgment is **three-way** (R3): a Turn is **productive**
(a novel file read, or a successful write/action), **harmful** (a tool-result error, or an
empty final response), or **neutral** (neither), with productive winning when signals mix. The
data behind Adaptive Suppression and the Turn Budget.

**Adaptive Suppression**:
The **per-Mechanism** withdrawal rule: a Mechanism whose next Turn is judged **harmful** several
consecutive times in a Session (a strike advances only on a harmful Turn; a neutral Turn freezes
the count, R3) is suppressed for the rest of it, with a configurable clear-path that re-opens it
on a productive Turn.

**Turn Budget**:
The **global** withdrawal rule: after several consecutive **harmful** Turns (the streak advances
only on a harmful Turn; a neutral Turn freezes it, R3), all non-exempt Mechanisms are suppressed,
cleared when productive activity resumes.

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

These five are distinct operations — "compress", "compact", "truncate", and "decompose"
must **not** be used interchangeably.

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
implemented **once** (the surviving half of the predecessor's `compress`). Beneath it sits a
**structural floor** — not a Mechanism, so never off and never withdrawn: a single result
whose estimate exceeds the *entire* History allocation is clamped as it enters the
conversation, because a result that large survives no reducer and would overflow even the
emergency fold that exists to rescue the Turn. Both render the same head/tail-plus-marker
elision (`context.TruncateToolResult`), so the model reads one idiom; the Mechanism's tighter
cap fires first when it is enabled.
_Avoid_: "compression", "compaction" (capping is per-result and non-generative).

**Compaction**:
The **default** conversation-level reducer: *generatively* summarising older Turns into a
summary via the model, when the conversation exceeds a threshold. Meaning-preserving but
costs an extra model call. One fold, **two automatic triggers**: the *estimate-driven* one
fires at an Exchange boundary when the history outgrows its Budget allocation, and the
*overflow-driven* **emergency fold** fires when a request will not fit the window — the only
fold allowed to run **mid-Exchange**, closing with a user-role bridge so the retried request
stays template-legal. `auto-compact: false` opts out of both; the on-demand `/compact` stays
boundary-only. See
[ADR 0018](docs/adr/0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md).
_Avoid_: "compression", "truncation" (Compaction is generative and summarises).

**History truncation**:
The **cheap alternative** to Compaction: *mechanically* dropping the middle of the
conversation, keeping the last N exchanges. Config-gated, **off by default**; a
history-rewrite Mechanism, validated against Compaction via the bench.
_Avoid_: "compaction" (truncation is mechanical and lossy, not generative).

**Guided decomposition**:
The Mechanism (`guided_decomposition`) that **avoids** context growth rather than reducing
it: when measured Budget signals show the task cannot fit — resolved file context exceeding
its allocation at the first Turn, or history exceeding its allocation mid-Exchange — it
steers the model's **own primary call** to enumerate the remaining subtasks, then converts
that enumeration into `sub_agent` delegations, **one per Turn**, carrying the
not-yet-delegated items as a Deferred Response Action. The work happens in child Sessions;
only their bounded reports come home. It is a proactive-nudge (off under Bypass), requires
`tool_result_cap` (the only reducer that *shapes* a request mid-Exchange — the emergency fold
also acts there, but reactively, lossily, and once per Turn), fires at top level only
(`Depth == 0`), and no-ops benignly when `sub_agent` is not offered or the model ignores the
steer. Because the enumeration is the model's own visible response, the queue survives
suppression in honest history. See
[ADR 0014](docs/adr/0014-guided-decomposition-steers-the-primary-call-and-serializes-delegation.md).
_Avoid_: "planner" (Plan is an autonomy mode), "orchestrator" (that is the sub-agent spawn
machinery, ADR 0013), "auto-decomposition" (the model performs the semantic split; the
Mechanism only decides when to ask and serializes the follow-through). Not to be conflated with
the **`decompose`** Mechanism (a prompt-shaping nudge; steers wording, not delegation — the two
are declared incompatible).

### Deliverables and presentation

**Present / Presentation**:
The act of **surfacing a finished document to the user** — a deliverable file (a report, a
review, a plan) shown at the end of the work that produced it, instead of left on disk where a
one-line `write_file` card is the only trace of it. The model reaches it through the
**`present_document`** tool and supplies nothing but a path (and an optional title): the **host**
decides the mechanism (see [presentation ladder](#deliverables-and-presentation)), so the model
never reasons about platforms. Like [Ask-user](#safety-and-autonomy) the tool is
**mode-independent**, `ReadOnly` (it runs even in Plan), and **not** a safety gate. A
presentation never fails the call — the baseline rung already happened — so the tool result names
the outcome (`opened` / `served` / `shown`) for the model to relay truthfully. See
[ADR 0019](docs/adr/0019-documents-are-presented-not-opened.md).
_Avoid_: "open the document" (opening is *one rung* of the ladder and always the host's act — a
remote session never opens anything), "export", "publish", "render" (nothing is converted).

**Presenter**:
The **host-supplied delegate** a presentation routes through (`domain.Presenter`, on `Config`
beside `Approver`/`Asker`/`Confiner`) — the sibling of the **Asker**: the same host-decides shape,
for showing a document rather than asking a question, and carrying no allow/deny semantics. A
`nil` Presenter means `present_document` is simply **not registered** (a headless host supplies
none, so the model is never offered an affordance nobody can honour). It is **not** an
`ExternalEffectTool` — the user's own display is not a non-forkable remote to stub — and it holds
**no live state across a Turn**: the tool keeps a delegate reference, the host owns the
mechanisms (ADR 0008).
_Avoid_: "opener" (that is one mechanism *inside* the ladder, not the delegate), "viewer",
"renderer".

**Presentation ladder**:
The **host-side mechanism ladder** the Presenter walks per call; the highest applicable rung
runs *in addition to* rung 0, never instead of it. **Rung 0 (baseline, always)** — a prominent
transcript entry carrying the workspace-relative path as **plain text on its own line**
(cmd+clickable in Zed/VS Code/iTerm2/WezTerm/kitty, copyable everywhere else); it is the rung
that is never wrong. **Rung 1 (local desktop)** — the OS opener auto-opens the file when the
session is local (no `SSH_CONNECTION`/`SSH_TTY`/`SSH_CLIENT`) *and* a desktop is detected.
**Rung 2 (remote + browser-renderable)** — the [doc server](#deliverables-and-presentation)
serves the file and the URL joins the entry, also as plain text. **Rung 3** — the
`present.command` config template replaces rung 1's opener. It **fails visible**: any rung above
0 that fails degrades to rung 0 and the entry says what happened.
_Avoid_: "fallback chain" (rung 0 is not a fallback — it always runs), "auto-open" for the whole
thing (that is rung 1 only).

**Doc server**:
The embedded, **lazily started** HTTP server that makes a presented file reachable from the
user's machine when Apogee runs remotely (rung 2). A **capability-token allowlist, not a file
server**: only explicitly presented files, each under a random token at `/d/<32-hex>/<basename>`,
no directory listing, 404 for everything else (including prefix walks and `..`), the file
**re-read from disk per GET**, and closed on app shutdown. Its advertised address is the server
IP from `$SSH_CONNECTION` → the `present.host` override → an outbound-dial probe → `127.0.0.1`;
its port is `present.port`, default **0** (ephemeral), because the URL is printed fresh per
presentation. There is deliberately **no host back-channel** anywhere in this path (ADR 0019).
_Avoid_: "web server" / "file server" (both suggest a served tree; this serves an allowlist of
individually granted files), "preview server".

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

**Validated set**:
A **per-model** enable set of catalogued Mechanisms that has passed the aggregate
non-inferiority gate against Bypass **on that model** (ADR 0009) — proven *safe* there;
benefit is deliberately **not** part of the claim (non-inferiority is the bar, superiority is
not required). Keyed exactly as the [Library](#self-regulation) keys its observations: the
confidence-tagged `ModelFingerprint`, resolved best-available — the evidence attaches to the
precise model measured, and any carry-over to a sibling quant or family member is an explicit
human decision, never automatic. A model with no Validated set runs the catalogue's global
defaults (the D1 floor). An entry is produced only by a completed, pre-registered aggregate
Campaign passing the gate on that model — with engagement verified — regardless of who runs it.
A matching set applies **whole or not at all** — a subset, or a merge with hand-picked
Mechanisms, is a different, *unvalidated* stack — and applies *automatically* only at ≥ medium
fingerprint confidence; below that it is **offered**, and applying it (like carrying it over to
an aliased model) is an explicit config decision. Explicit mechanism config and Bypass take
precedence over auto-application.
_Avoid_: "recommended set" (promises help; the bar is safety), "default set" (an unknown model's
default is the floor, not a set), "per-architecture set" (the key is the fingerprint, not the
family).

**Curation**:
The operator decision layer above the evidence stream: what the global catalogue contains
(membership, port verdicts, global defaults) and what each model's
[Validated set](#validation-and-the-bench) contains. Strictly separate from evidence: a
completed Campaign appends a **ledger entry only** (the L9 discipline); a curation action is a
distinct, later decision that cites ledger entries. Scope follows evidence: a single-model
campaign can license only that model's Validated set; global actions (deleting a catalogue row,
flipping a global default) need cross-model evidence.
_Avoid_: treating a ledger entry as a behaviour change (evidence records; curation acts).

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
