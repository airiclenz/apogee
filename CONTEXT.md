# Apogee

Apogee is a terminal **coding agent** built for smaller local models — while working
even better with bigger ones — that owns the full agentic loop — provider, tools,
context, and sessions — and runs six **Floor guards** inside that loop: engine behaviour
every model runs with, above which a gated, self-regulating **Mechanism** lab surface can be
armed for measurement.
The hard constraint, inherited unchanged from the predecessor projects: **nothing Apogee puts in
front of a model may make that model perform worse than the same agent without it.** That floor is
**Bypass mode** (catalogued Mechanisms off, structure and Floor guards on) —
**not** a naked model, because Budget, Compaction and Pruning are structural and load-bearing (a
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
CLI — the headline product) **and** this reusable library: the TUI, the `apogee headless`
CLI (one prompt run unattended over the shared core, beside
[`apogee probe`](#probing-and-model-identity) on the subcommand surface), and the bench are all consumers of one
public package over the same engine. The repo is the whole tool, not just the library. The public surface is guarded
and versioned; everything else lives in `internal/`. See
[ADR 0001](docs/adr/0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md).
_Avoid_: "**Apogee Core**" (retired — it named the proxy-era transform engine, a
different thing; do not resurrect the name for the new library), "the SDK".

**Driver**:
A program that embeds the **Embeddable agent** and drives the loop through its public
API. Four exist today — the TUI, `apogee daemon`, `apogee headless` and the bench.
The engine must stay sufficient for *any* Driver: nothing model-visible or
safety-relevant may exist only in one Driver's surface, and a wire surface (HTTP,
webhooks) is always composed by a Driver — the engine itself is wire-silent. See
[ADR 0031](docs/adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md).
_Avoid_: "embedder" (names the linking, not the responsibility of pacing the loop),
"frontend" / "client" (a Driver owns the loop's pace and state roots, not just a view).

**Schedule**:
A standing instruction a **Driver** holds for its lifetime — a prompt, a cycle (how often it
re-runs) and an Agent mode — created in the TUI with `/schedule` and ended with
`/schedule-stop`. Everything that decides *when and how* it runs (cycle timing, the
skip-on-overlap policy, the wait for a quiescent host, lifecycle) lives in the scheduler
**library** (`internal/schedule`); the TUI is merely its first Driver surface, owning only
input and display, and `apogee daemon` composes the same library. A TUI-hosted Schedule **dies
with the TUI** — "while apogee is open, re-run this every N minutes" is the whole promise;
nothing persists to config. A Schedule's mode is **Plan or Auto only** and is chosen explicitly
at creation, independent of the host session's mode (Auto still gated by the same eligibility
ladder that gates launching in Auto), so no **Firing** can ever park on an Approval. See
[ADR 0033](docs/adr/0033-the-scheduler-is-a-library-and-the-tui-is-its-first-driver-surface.md).
_Avoid_: "cron job" / "job" (nothing is queued and nothing survives quit), "recurring task",
"timer" (the timer is one part; the Schedule is the standing instruction).

**Firing**:
One run of a **Schedule**'s prompt: a **fresh** agent constructed through the **Embeddable
agent**'s public API, the prompt submitted, the loop driven to the quiescent boundary, and the
result saved as an ordinary **Session record** marked with its Schedule's identity (id and name
on browsable `Meta`) so the `/sessions` browser can label it. A Firing *is* a **headless run** —
the same act as the `apogee headless` runner, over the shared core (`internal/run`)
both use. It carries nothing over from the previous Firing (fresh context; no summary is
injected — model-visible content would be a Mechanism to bench, not a scheduler feature), its
Approver is a **fail-safe denier** (a gated action fails visibly and nothing waits for a human),
its **Asker** and **Presenter** are `nil`, and it saves **once, at completion** — a deliberate
scoping of ADR 0022's per-Turn cadence to a bounded, unattended run. Firings of one Schedule are
strictly serial: a tick landing while one is in flight is **skipped**, never queued. See
[ADR 0033](docs/adr/0033-the-scheduler-is-a-library-and-the-tui-is-its-first-driver-surface.md).
_Avoid_: "background task" / "detached process" (a Firing is a Session in this process, and it
ends), "job run", "cron run".

**Daemon**:
The always-on **Driver** — `apogee daemon`, an in-repo subcommand (same binary as
the TUI) composing the same scheduler and runner libraries, but holding **durable** Schedules
read from a declarative `~/.apogee/daemon/schedules.yaml`: validated and atomically swapped on
change, an invalid file keeps the old set running. Each entry is a **trigger+action envelope**
— `on:` (v1: a cycle) + `run:` (v1: a prompt with mode, workspace, and an optional named
server binding) — so webhook triggers and workflow actions arrive later as new keys, never a
schema break. A daemon Schedule binds to a `servers:` entry by name, may select a model only
where that is a per-request act, and its Firings **never actuate a local model load**
([ADR 0055](docs/adr/0055-daemon-schedules-bind-to-named-servers-and-never-actuate-a-model-load.md)). Its Firings take the
**Firing** posture unchanged and save into the shared sessions store, so the TUI's `/sessions`
browser is the results window; the two processes share libraries and stores, never IPC. Runs
foreground under the OS's own supervisor (`apogee daemon install` generates the unit),
single-instanced by a lock file. See
[ADR 0034](docs/adr/0034-the-daemon-is-an-in-repo-subcommand-over-a-declarative-trigger-action-file.md).
_Avoid_: "server" (the engine is wire-silent and v1 has no listener; the future webhook
surface is this Driver's composition), "the scheduler" (that names the library both Drivers
share, not this Driver).

**Sub-agent**:
A nested, focused agent loop the top-level agent spawns for one delegated sub-task, with its
own Session. It is itself an instance of the **Embeddable agent**, spawned in-process; its
events nest into the parent's event stream at **`Depth = parent+1`**. An **unrouted** child's
**context window is not reduced**: it inherits the parent's `Config` verbatim
(`internal/agent/subagent.go`), so it works against a window — and a Budget over it — of the same
size the parent has. A child **routed** to the **Sub-agent server** works against the **Delegation
target's** window instead (that entry's `context-window:` pin, else the per-slot window the target
server advertises), which is a different model's window and may be smaller or larger than the
parent's ([ADR 0045](docs/adr/0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md),
[ADR 0066](docs/adr/0066-sub-agent-routing-follows-the-sub-agents-server-root-key.md)).
When that entry names **neither** — no pin, and nothing observed — the routed child keeps the
**parent's** window rather than running windowless: no window at all would leave its Budget and
automatic Compaction inactive and its readings unmeasurable.
How full that window got is **visible per run**: the TUI paints the run's own reading on its
collapsed call block (`N tool calls · 12k/32k · <gist>`) and `apogee headless` prints one
`sub-agent: <used>/<limit> · <the delegation's name, else the task>` line on stderr per run.
Each reading belongs to the agent that filled it — it never moves the parent's gauge, never
accrues to an enclosing run, and is spelled against **that agent's own window** (a routed child's
`<limit>` is the Delegation target's, not the session's). Its privileges are always **≤ the parent's** (mode, guardrails, Confinement,
tool set) — see [ADR 0005](docs/adr/0005-sub-agent-privileges-are-bounded-by-the-parent.md).
The *shape* is [ADR 0013](docs/adr/0013-the-sub-agent-orchestrator-is-the-recursion-point-with-isolated-live-guard-state.md):
the model reaches it through a **`sub_agent` tool** that dispatch treats as a **recursion
point** (not a leaf — never confined/gated as a unit; each *child* call gets the per-call
disposition one level down), the orchestrator threads mode/approver/confiner/tool-subset
verbatim-or-stricter, the sub-agent's **live guard state is isolated** (a fresh
circuit-breaker + audit log — `Guards.ForSubAgent`) over a **shared, read-only
dangerous-action floor** (unloosenable one level down), and recursion is depth-bounded.
When one reply carries several `sub_agent` calls, the **top-level** agent runs them
**concurrently** up to the server's **Parallel agents** cap (depth-0 only — a sub-agent's
own delegations run serially inline; a reply split across **Delegation seats** takes the
smaller of the two caps); every event a sub-agent emits carries the **call-ID**
of the `sub_agent` call that spawned it, so interleaved streams stay attributable
([ADR 0039](docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md)).
What a delegation is CALLED is its **Delegation name**, and the rule is three-deep: the name its
call gave — an optional `name` argument on the `sub_agent` call, normalised to a trimmed first
line — else a **generated** one that lands once the run is under way, else the delegated task's
first line. It is display identity only, never privilege.
A **running** child is **addressable**, and by the handle it already has: the spawning call-ID.
`Agent.InterjectChild(spawnCallID, in)` appends a message to that child's engine-side **mailbox**,
recursing into registered children so a grandchild is reachable from the top-level agent and
answering `domain.ErrNoSuchChild` when no such child is running; the goroutine driving that child
pops the mailbox before every Step after the first and delivers each message through
`Agent.Interject`, so it commits at the child's own between-Steps boundary as an ordinary
**Interjection** — the one place a `Run` drains for an embedder, because per ADR 0013 D5 nobody
else can drive a child's Steps. Every queued message earns one
`domain.ChildInterjectionEvent{Input, Landed}` on the shared sink, so a Driver never has to guess
whether it arrived, and a child that received such messages returns its result with the trailer
`(the user sent N message(s) to this sub-agent while it ran)` on every outcome but a cancelled
dispatch — a parent reading a result shaped by instructions it never issued must be able to see
that from the result alone. Addressing a child buys no privilege (ADR 0005 stands), and all of
it exists at **depth > 0 only**
([ADR 0063](docs/adr/0063-sub-agent-runs-are-user-addressable-views.md)); the TUI's surface for it
is the **Run view**.

One shape to know when reading events: `domain.AuditEvent` carries a `CallID` of its own — the
**audited** call, the tool call that record is about — which **shadows** the promoted
`EventBase.CallID`, so an observer reading `ev.CallID` on an audit record gets the audited call;
the spawning one still travels, reached as `ev.EventBase.CallID`.
Bare "agent" means the **top-level** agent unless qualified as "sub-agent".
_Avoid_: "child agent" (says nothing about the privilege bound), "worker".

**Delegation name**:
What one **Sub-agent** run is called wherever it is shown — the collapsed call block, the
`✦ Sub-Agent (N)` umbrella's rows, a **Run view**'s breadcrumb, `apogee headless`'s
`sub-agent:` line and the **Session record** it is saved into. Three-deep, in order: the `name`
the `sub_agent` call carried (normalised to a trimmed first line), else a **generated** one, else
the delegated task's first line. The generated one comes from a single out-of-band completion on
the CHILD's own **Upstream** (routed ⇒ the **Sub-agent server**, else the session's), fired only
when the call named nothing — Mechanism-synthesised delegations included, never over a name the
model gave — concurrent with the child and bounded by its lifetime, so a reply that lands after the
run finished is dropped. It is gated by the same `auto-title:` key that names a **Session**, silent
on every failure, and announced as one `SubAgentNamedEvent` so every **Driver** folds the rename by
the road it already reads its delegations on. It is **not a Mechanism** — it fires at no Hook
point, runs under Bypass, and adds nothing to any model's context — and it is saved with the run,
so a resumed session paints it. Ratified 2026-09-01
([ADR 0068](docs/adr/0068-unnamed-delegations-are-named-out-of-band-on-the-childs-upstream.md)).
_Avoid_: "title" (that is the **Session**'s — a delegation has a name, and no `^r` to change it),
"label", "sub-agent title".

**Parallel agents**:
The per-server cap on how many sub-agents the top-level agent may run **concurrently**.
Resolved per `servers:` entry, pin-else-discover-else-1: an explicit `parallel-agents: N`
is a **pin** discovery never overrides (the `context-window` idiom); absent, the cap is
discovered from the live server (`/props` `total_slots`); no signal means **1** — strictly
serial, today's behavior. It is **structural, not a Mechanism** — it only executes calls
the model already made, so it is on under Bypass — but it is also the width of a guided
decomposition **batch** (`min(cap, remaining)` delegations per Turn). More parallel agents
means a **smaller window each**: a llama.cpp `--parallel N` server splits its context into
N slots, and the reported window is the per-slot share. One reply has ONE width even when its
children do not share a **Delegation seat**: all-on-one-seat is bounded by that seat's cap, a
**mixed** reply by `min(session cap, target cap)` — small enough that neither server is
oversubscribed, and never a pool per seat. See
[ADR 0039](docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md)
(mixed width amended 2026-09-01 by
[ADR 0069](docs/adr/0069-the-top-level-model-picks-the-delegation-seat.md)).
_Avoid_: "slots" (the server's own term for its side of the trade), "concurrency limit"
(names the bound, not the thing bounded), "fan-out width" (fan-out is the act; this is the
cap).

**Sub-agent server**:
The `servers:` entry the root `sub-agents-server:` key names — the server **every delegation
routes to**, so a cheap grunt model does delegated work while a smarter model orchestrates.
Every entry is an eligible target, including the one the session itself runs on, and the choice
**moves in a running session**: `/sub-agents-server` re-points the delegations spawned from then
on and records the name back into the file, while children already in flight keep the server they
were spawned against. ANY entry may also carry the children's **posture** — `bypass:` and
`mechanisms:` overrides that apply to every child *routed there* whenever that entry is the
target (present key replaces whole, absent key inherits the parent's live value; where the
*parent* runs is irrelevant). Delegations there also speak that server's own
[Thinking-effort](#identity-and-shape) **wire dialect** — its `effort-dialect:` pin, else
what it advertises — because the dialect is a property of the server a request lands on; an entry
that names neither leaves its delegates speaking the SESSION server's dialect, and apogee says so
once when routing engages. An unset key means today's behavior: children share the parent's
Upstream. A name no entry carries is not a startup error — apogee says which name went missing,
names the entries the file does carry, and routes to the session's own server. ANY entry may also
carry a free-text `description:` — what the owner keeps that box for — which the **Orientation
block**'s Delegations line relays and the `/sub-agents-server` picker shows in its rows. With root
`sub-agents-choice: model` the key stops being the last word: a depth-0 `sub_agent` call may carry
`run_on: session | sub-agents-server` and pick its own **Delegation seat**, and the key's rule
covers every call that carries no `run_on` — which, under the default `sub-agents-choice: fixed`,
is all of them. Ratified 2026-08-11, re-shaped as a root key 2026-08-31, opened to the model
2026-09-01
([ADR 0045](docs/adr/0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md),
[ADR 0066](docs/adr/0066-sub-agent-routing-follows-the-sub-agents-server-root-key.md),
[ADR 0069](docs/adr/0069-the-top-level-model-picks-the-delegation-seat.md)).
_Avoid_: "grunt server" (colloquial), "worker server", "delegation server" (too close to the
**Delegation target**, which is the latched spec rather than the entry), "flagged server" (there
is no flag any more — the root key names it).

**Delegation target**:
The engine-side latched spec a sub-agent spawn reads to build its own Upstream: the
Sub-agent server's endpoint and key plus its *observed* facts — model, per-slot window,
Parallel-agents cap, model profile, **effort dialect** — kept fresh by a second heartbeat monitor
and pinned by the entry's `model:` / `context-window:` / `effort-dialect:` where set. A fact the
target names is what the routed child gets; a fact it names *not at all* falls back to the
parent's (the dialect, like the window). Mutex-read at spawn (never idle-gated:
beats land mid-Exchange; each spawn snapshots). An **unusable** target (no beat yet, server
down, no model) is not an error: the spawn **falls back** to the parent's Upstream *and*
parent posture, with one notice per routing state change. Ratified 2026-08-11 (ADR 0045).
_Avoid_: "child upstream" (the target outlives any one child), "sub-agent endpoint" (it
carries far more than an address).

**Delegation seat**:
One of the **two** places a delegation may run — the session's own server, or the **Sub-agent
server** — and, with root `sub-agents-choice: model`, something the top-level model picks per
call through the `sub_agent` tool's optional `run_on` (`session` | `sub-agents-server`). Absent,
the root `sub-agents-server:` key's rule stands, so the default gate value (`fixed`) is today's
behavior with the parameter not even in the schema. The choice exists at **depth 0 only** — a
child's own tool carries no `run_on`, and a delegation keeps the seat it landed on
([ADR 0039](docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md)'s
depth-0 rule, ADR 0045's identity-once-there rule). The seat carries everything the seat means:
`run_on: session` in a routed session runs unrouted with the parent's posture, window and
**effort dialect**, while `sub-agents-server` is routing exactly as ADR 0045/0066 already build
it. The model chooses from session-constant facts the **Orientation block**'s Delegations line
states for BOTH seats symmetrically — entry name, the entry's `description:`, its `model:` pin,
the bound session model — never from availability, which is why an ask the far seat cannot honour
**falls back** to the session server and says so in ONE appended note line, the last line of the
result body ahead of a steered child's trailer
([ADR 0063](docs/adr/0063-sub-agent-runs-are-user-addressable-views.md) D3 keeps the final line).
Ratified 2026-09-01
([ADR 0069](docs/adr/0069-the-top-level-model-picks-the-delegation-seat.md)).
_Avoid_: "target" (the **Delegation target** is the latched spec of ONE seat, not the pair),
"routing choice" (routing is what one of the two seats is), "worker pool".

**Run view**:
The surface one **Sub-agent** run is read and addressed in. Expanding a framed delegation — from the
block cursor or a click on its row — opens the run view rather than flipping a fold flag: the TUI's
transcript slot paints that run alone, rooted at its own task, opened on its latest line and
following it as it grows, while the status line, prompt box and footer stay exactly where they are
(a pane inside apogee's own frame, never an alternate screen — ADR 0035 stands). A clickable
breadcrumb header (`← main › planner › repo-scout`) and `esc` each go **one** level up, the status
line's right slot reads `esc back` while a view is open, and stopping stays whole-run from the top
level. Inside a view of a **running** child the prompt box addresses that child (see
**Interjection**); a view of a finished or scheduled one opens **read-only**. A run therefore has
exactly two shapes — the collapsed row and the run view — while the `✦ Sub-Agent (N)` umbrella
still opens inline to its member rows. It is **Driver state**: a stack of open runs in the TUI's
`Model`, never encoded in the transcript, never written to a **Session record** and never restored,
so a resumed session opens at the top level. Ratified 2026-08-30
([ADR 0063](docs/adr/0063-sub-agent-runs-are-user-addressable-views.md)).
_Avoid_: "full screen" (the frame's other rows stay — only the transcript slot is taken),
"expanded sub-agent" (the inline expanded shape is gone; a run has no fold state), "drill-down".

**Inspector**:
The **Driver**-side view of what the loop actually put on the **Upstream** connection — the request
body a model call marshalled and the response payload it read back (`domain.WireEvent`) — opened in
the TUI with `/inspect` and held in a bounded ring beside the transcript, never in it. It is
**armed**: the engine captures nothing unless `ui.inspector` says so, read once at start-up
([ADR 0035](docs/adr/0035-the-settings-surface-persists-one-key-per-deliberate-edit.md)). Each
captured half carries **two renderings** of the same payload, both computed when it is captured and
never on the paint path: the **readable** one the pane opens on — a request as the one line naming
its envelope (messages, tools, model), a response as the passages its deltas spell, the model's
thinking and its reply as wrapped prose and each tool call as a named row — and the pretty-printed
protocol that `ctrl+r` flips to and back. Which rendering is showing is Driver state, per pane and
never persisted. What the pane shows OF the ring follows the **Run view**: with one open it is the
viewed delegation's own wire stream and nothing else, named in the pane's title, and there is no key
for the scope — closing the view is what widens it back to the whole ring. The engine itself stays
wire-silent throughout: a WireEvent is data a Driver renders, not a surface the engine owns
([ADR 0031](docs/adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)).
_Avoid_: "raw view" (raw is ONE of its two renderings and not the default one), "wire log" (a
bounded ring of the recent halves, not an appended record), "debug console" (it reads what was
captured and calls nothing).

**Thinking pane**:
The **Driver**-side *plain* view of the [Thinking channel](#identity-and-shape) — the reasoning a
model emits beside its answer (`domain.ReasoningEvent`) — opened in the TUI with `/thinking` and
held in a bounded per-agent **board** beside the transcript, never in it. Unlike the
[Inspector](#identity-and-shape) it is **unconditional**: no config key arms it, because the board
never touches the transcript, the model's context or the Upstream connection. One record per
completed **Turn**, newest last, each under one heading naming the Turn and — for a delegation —
the run; the pane opens on the newest and paints the text as plain wrapped prose, with ONE
rendering, no protocol and no prefixes (the wire, and its raw bytes, are the Inspector's). What it
shows follows the **Run view** exactly as the Inspector's ring does: with a view open it is that
delegation's thinking alone, named in the pane's title, and at the top level the main agent's
alone. It is **Driver state**, bounded (a record's last 64 KB; the board's 64 most recent records,
oldest dropped) and never persisted — a resumed session opens with an empty board, while the
canonical reasoning of each committed Turn stays where it always was, on the engine's own message.
_Avoid_: "reasoning pane" (the channel is the **Thinking channel** and `/thinking` is the verb),
"thinking transcript" (a board beside the conversation, never an entry in it), "reasoning tail"
(the retired seam this replaced).

**Session** / **Session record**:
A **Session** is one conversation the engine holds — the versioned `domain.Session` envelope
`{Version, State}`, opaque to everything outside the engine. A **Session record** is how that
Session is *persisted*: the on-disk `session.Record` wrapper (`internal/session`) around **two
opaque payloads** — the untouched engine Session **and** the versioned **transcript blob**
written by the neutral codec in `internal/session` (the scrollback: user/assistant text, tool
cards, notes, sub-agent `Depth` and the
**call-ID** of the `sub_agent` call that spawned each delegated entry, so a resumed fan-out
regroups per child) — plus
browsable `Meta` (title, timestamps, workspace, model, message count, last context fill). Not
every scrollback entry is persisted: an **ephemeral** entry is display-only — rendered exactly
like its kind, skipped by the encoder — because it is *re-derived* at each startup or resume
rather than earned by the conversation. Today those are the start-up box, the `resumed: <title>`
notice (with its no-scrollback degrade variant and the interrupted-mid-exchange note) and the
`context: …` notice; persisting any of them would append a fresh copy on every resume. The
record is saved **per-Turn** (at each quiescent boundary, so a crash loses at most one Turn),
listed and resumed from inside the TUI through the `/sessions` **browser**, and replayed on
resume so the view repaints instead of showing a bare box over a remembering engine. A
**progress save** is the one write that does not wait for the boundary: while a delegation runs
the TUI re-persists the record at the child's tool boundaries, pairing the LIVE transcript with
the last boundary snapshot (never a fresh one), so a record read mid-run shows the delegation
instead of ending at the previous tool call — and a resumed one closes those still-open calls as
*interrupted*. Each of the three schema versions is rejected/degraded only by its owning layer
(store / TUI / engine). Agent mode, approvals, Confinement, and MCP connections are **not** in the
record — live host state, re-confirmed on resume
(ADR [0008](docs/adr/0008-stateless-tools-and-non-forkable-external-effects.md)).
See [ADR 0022](docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md).
_Avoid_: "session file" for the Session itself (the *record* is the file; the Session is its
engine payload), "history" (that is the browser's list of records, not one Session — and it is not
[Prompt recall](#turns-and-stepping) either, which is the prompt box's own list of sent inputs).

**The loop** (the agent loop):
Apogee's core control flow: build request → call Upstream → parse response → dispatch
tools → repeat, emitting typed events at each step. The loop owns tool execution and
conversation state — which is precisely what lets formerly lab-only Mechanisms (e.g.
`correct_tool_result`) become first-class. Lives in `internal/agent/loop.go`.
_Avoid_: "the pipeline" (that was the proxy-era Transform chain — a narrower thing).

**Upstream**:
The local LLM server that runs the model — Ollama, llama.cpp, LM Studio, vLLM, or any
endpoint honouring the OpenAI HTTP surface. Apogee reaches the Upstream directly through
its `provider/` package; there is no intervening proxy. A session is not married to one: the
[Heartbeat](#probing-and-model-identity)'s Rebind half moves it to another configured server
mid-session (`/server`), unbound until that server's first Beat says what it serves.
The Upstreams apogee knows are exactly the entries of config's **`servers:` list — the single
definition of what servers exist** (one entry = a `name`, an `endpoint`, an optional `api-key`, an
optional `model` discovery hint). The `name` is also the **host alias** the footer shows, so no
standalone `host-alias:` key exists. A session starts on the entry the `server:` key names — the
last one a `/server` switch chose, recorded automatically — and asks with the picker when that key
is unset or names an entry that is gone; a raw `--endpoint`/`APOGEE_ENDPOINT` override builds an
unlisted, unpersisted entry for one run
([ADR 0036](docs/adr/0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md)).
_Avoid_: "the model server", "the backend" (a `backend` detector package may exist, but
it detects Upstreams — it is not the Upstream).

**Key source**:
The one place a server entry's API key comes from: a literal `api-key`, a command (`api-key-cmd`)
whose output is the key, or a named environment variable (`api-key-env`). An entry has at most one;
having none is the keyless state. **Key migration** is the startup offer that moves a plaintext key
into the OS secret store and turns the entry into a command source. See
[ADR 0047](docs/adr/0047-api-keys-resolve-through-a-per-entry-key-source.md).
_Avoid_: "credential provider", "keychain support" (apogee keeps no secret of its own and links no
keychain library — a source is a line in the entry), "key fallback" (an entry names one source, not
a chain).

**Model profile**:
The per-model description Apogee carries of *how it equips and speaks to a given model* — three
**orthogonal** axes: its **tool-call format** (native structured `tool_calls`, or a text format
the model emits inline in its content — **markdown-fenced** or a **custom regex**), its
**thinking channel** style, and its **tool roster** (delta lists — `disabled`/`enabled` —
against the default tool set, so a tool can be off for the small class and on for a big model,
and a new tool can ship default-off/profile-enabled). Orthogonal because a model can emit native
tool calls *and* inline thinking (gpt-oss does both); the roster axis is capability tuning, not
wire shape. The profile drives **both directions at the seams**: on the
**parse** side it selects which parser and content-stripper the loop applies to incoming
content; on the **emit** side, for a non-native tool-call format, the engine tells the model
how to speak — rendering the tool menu and format-emission instructions as text into the
request and suppressing the native `tools` array (a non-native template would otherwise be
double-told, or choke on an array it cannot render). A **zero profile is the native,
no-inline-thinking default** (today's behaviour) — it adds nothing to the request in either
direction. It is a `domain` type on `Config` (declarative data — [ADR 0010](docs/adr/0010-package-layout-domain-core-and-thin-root-facade.md)),
translated to the `processing` parsers at the boundary, not the parsers' own config.
Which profile a model gets is **resolved per model in three layers** — the user's
`model-profiles:` pattern map ▸ apogee's **shipped shape table** ▸ the zero profile — matched by
case-insensitive substring on the model name (longest pattern wins within a layer; any user
entry beats any shipped one) and resolved **axis-wise**: each axis takes the nearest layer that
spells it, an absent axis defers downward, an explicitly spelled zero overrides
([ADR 0057](docs/adr/0057-the-tool-roster-is-a-third-model-profile-axis-resolved-axis-wise.md),
amending ADR 0044's whole-replacement rule). The thinking axis itself resolves in **two halves**
— the channel **style**, carrying its delimiter tokens, and the **effort** dial — each on its own
through the same three layers, so an entry that dials only `effort:` keeps the layer's channel
style ([ADR 0058](docs/adr/0058-the-thinking-axis-resolves-as-two-sub-axes-style-and-effort.md)).
The shipped table carries a roster only where an ADR ratified that one — first the Console family
for Qwen3.8
([ADR 0059](docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md), amending
ADR 0057 decision 6) — and wire-shape axes everywhere else. A shipped match announces itself
with a one-line notice (`model profile: <pattern> (built-in) — thinking: <style>`); a user match
applies silently, except that a switch
whose roster deltas are non-empty announces them in one line. The resolution rides every model switch — the profile is
one of the per-model bindings Rebind applies at the boundary
([ADR 0024](docs/adr/0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md)) —
and the retired *global* `model-profile:` block is refused at startup with the map spelling to
paste. See [ADR 0044](docs/adr/0044-model-profiles-are-per-model-and-mostly-shipped.md).
_Avoid_: "model config" (overloaded with sampling/endpoint knobs), "adapter", "format" alone
(there are two axes, not one), "global profile" (the single global block is retired — the profile
is per-model, and mostly shipped).

**Launch profile**:
The recipe for *getting a model running* — which model file, which [Upstream](#identity-and-shape)
hosts it (llama.cpp, Ollama, LM Studio) and under what launch flags — defined and stored by
**llama-launcher**, the owner's separate server-lifecycle tool, never by apogee. Apogee imports the
launcher as a library, and activating a Launch profile is what "switch model" means on a host that
has one: the launcher **actuates** (starts and
stops servers, loads and unloads models), the [Heartbeat](#probing-and-model-identity) **observes**,
and every actuation is completed by the next Beat binding what it finds — the session follows the
loaded profile to its server. Which sessions get that answer is a property of the **server entry**:
a `servers:` entry may carry `llama-launcher: auto` (the launcher's own default config) or a path to
one, and absent — the default, and what every remote entry wants — means the launcher is off for
that server. The integration therefore **follows the session**: `/model` offers Launch profiles only
while the session is on such an entry, every other server keeps the models it advertises, and a
Launch profile load that moves the session to an endpoint no entry names keeps the launcher it just
used. There is no global launcher key; a `config.yaml` still carrying the retired top-level one is
refused at startup with the per-entry line to paste. The deliberate contrast is the [Model profile](#identity-and-shape):
a Launch profile is **launch-side** (how a model comes to exist at an endpoint), a Model profile is
**request-side** (how apogee speaks to whatever exists). The two never touch — loading a Launch
profile changes what runs; the Model profile only says how apogee speaks to whatever now runs, and
is re-resolved for that model when the load lands. See
[ADR 0029](docs/adr/0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md).
**remember-model** is what makes such a choice outlive the session. With that top-level toggle on
(off by default), an *explicit* choice — a `/model` pick that bound, or a Launch profile load that
**committed** — is recorded into apogee's own `servers:` entry: a wire model id into a plain server's
existing `model:` key, a profile name into a launcher-fronted entry's `launch-profile:` pointer. The
pointer's home is the **actuating entry**, the one whose `llama-launcher:` key the session's launcher
path follows, even when the load moved the session to an endpoint no entry names; the launcher's own
config is never written. A heartbeat-observed rebind, a `--model` override, an unload and a stop all
record nothing. At the next *interactive* start-up the recorded profile is loaded back through the
same actuation latch a pick takes — but only while nothing is already running under that launcher
(any instance, any profile, any port, yields with a note). See
[ADR 0048](docs/adr/0048-apogee-remembers-the-model-choice-per-server.md).
_Avoid_: "profile" unqualified in docs (two profile namespaces exist; inside the launcher's own
picker the short word is fine — context disambiguates), "launcher profile" (owner-named where the pair is
axis-named), "server profile" (collides with the launcher's own `servers:` config notion).

**Thinking channel** (a model's private reasoning):
The reasoning stream a model emits separately from its user-facing answer — either **delimited**
inline (`<think>…</think>`), **harmony** (gpt-oss's `<|channel|>analysis…<|message|>…`), or split
out by the Upstream into its own wire field — spelled `reasoning_content` by llama.cpp, vLLM and
LM Studio, and `reasoning` by Ollama and OpenRouter. Apogee decodes both spellings as the one
channel, preferring `reasoning_content` wherever it is non-empty. Apogee **strips** inline channels from
visible content and preserves them as reasoning in history; it never sends them back Upstream.
Harmony is a *content-stripping* concern only — a harmony model's tool calls arrive **native**
(the Upstream parses harmony server-side), so there is no harmony tool-call text parser.
_Avoid_: "chain-of-thought" (a prompting technique, not the wire channel), "commentary" (that is
one harmony sub-channel, not the whole concept).

**Thinking effort** (how hard a model thinks):
The **emit-side dial** on the [Model profile](#identity-and-shape)'s thinking axis — `off | low |
medium | high`, widened by the levels real servers report to `minimal`, `xhigh` and `max` (and
`none`, which the OpenRouter dialect spells for `off`) — carried to the server in whichever
**wire dialect** that endpoint speaks: llama.cpp's `chat_template_kwargs`, OpenRouter's
`reasoning` object, or OpenAI/Groq's top-level `reasoning_effort`. **Absent means send nothing**:
the request stays byte-identical, and a model whose template reads no dial simply ignores a sent
one. Whether the dial exists at all is **detected passively**, from the discovery payloads the
heartbeat already fetches — a `/props` chat template that mentions the kwarg, or a `/v1/models`
`reasoning` object naming the model's own levels — never by probing; a `servers:` entry's
`effort-dialect:` key forces the dialect for a provider that advertises no such tell. What the
next request will actually carry reads in the **footer**, and `/effort` opens a **picker** of
the levels this model reports; on a model with no dial the footer segment and the menu row are
both absent. The dial resolves as its **own half of the thinking axis**: a profile entry that
spells only `effort:` keeps the channel style the layer below carries, so dialling a shipped
model's effort never drops its parsing
([ADR 0058](docs/adr/0058-the-thinking-axis-resolves-as-two-sub-axes-style-and-effort.md)).
A session can overlay the resolved profile with the **`/effort` override** — the human's "keep it
brief" intent, which rides above whatever profile each model switch resolves, is never persisted,
and is dropped only when a switch binds a model whose reported levels exclude it. Effort is
**configuration, not a Mechanism** — it holds under Bypass. The deliberate contrast is the
[Thinking channel](#identity-and-shape): the channel says what the reasoning stream *is* and how
apogee parses it; effort says how much of it to ask for. See
[ADR 0050](docs/adr/0050-thinking-effort-is-a-profile-axis-with-one-canonical-wire-mapping.md) and
[ADR 0060](docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md).
_Avoid_: "reasoning effort" as the term (that is a wire kwarg's spelling, not the concept),
"thinking budget" (llama.cpp's `--reasoning-budget` is a launch flag — launcher territory, not
this per-request dial), "/thinking" for the command (that verb opens the
[Thinking pane](#identity-and-shape); `/effort` is this dial's).

### Turns and stepping

**Turn**:
One iteration of the loop — a single *primary* Upstream call and the work that follows it
(parse → dispatch tools → apply Mechanisms). Compaction's summarisation call is *internal*
to a Turn, not a Turn of its own. The unit of self-regulation and of bench measurement. The
Turn's lifecycle — its opening, its one permitted overflow fold, and its five exits (complete,
Exchange-complete, abandoned, cancelled, step-capped) — is owned by the loop's `turnLifecycle` module
(`internal/agent/turn.go`), the way the Exchange anchors on `ExchangeView`.

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

**Step cap**:
The number of **Turns** a **delegate** may take in its one Exchange before the engine ends it —
the `delegate-max-steps` key, default **80**, `0` = unbounded. It bounds child agents ONLY: the
main loop is the human's to stop, a delegate's is nobody's, and an uncapped delegation is how a
single `/code-audit` run reached 633 Turns and a billion prompt tokens. A `sub_agent` call's
optional `max_steps` argument may LOWER the cap for that one delegation, never raise it. On a
hit the child's Exchange ends **cleanly, not faulted** (`StepResult.StepCapped`) — but not
before the engine spends one further Turn on the child's CLOSING REPORT: that request goes out
with the tool menu withdrawn, telling the delegate why its tools are gone and asking it to
report to the agent that delegated the task, unfinished work included. That Turn is EXTRA — it
sits outside the cap, so `delegate-max-steps: 3` still buys three working Turns plus this one
reply — and when it faults, errors or answers with a tool call the result falls back to the
child's last visible text. The parent receives a non-error result whose first line marks it
partial, followed by that report, so Turns of real work are not thrown away, and what the
parent reads is authored rather than scavenged from whatever the child happened to narrate
alongside its last tool call. It is a **structural floor**
([ADR 0006](docs/adr/0006-bypass-mode-is-the-mechanisms-off-floor.md)), not a Mechanism — it stays on
under **Bypass** and is never withdrawn by Adaptive Suppression. Enforced in exactly one place,
`Agent.Run`.
_Avoid_: "turn limit", "iteration cap" (the unit is the Step/Turn the model spends).

**Interjection**:
A message the human interjects into a **running** Exchange — the remark that reaches the model
mid-task ("also check the tests") instead of waiting for the Exchange to end. It is committed
history, not a request-scoped injection: `Agent.Interject` appends it as a real `RoleUser`
message at a **between-Steps boundary** — the class of engine call the worker's `Snapshot`
already occupies, where the driving goroutine owns the conversation and the boundary is the
synchronization, no mutex — so it survives Turns, compaction, and session save/restore. It
carries `Message.Interjected`, and the derived Exchange **opening skips it**, so the remark
joins the running Exchange's body rather than starting a new one and every Mechanism reading the
boundary keeps seeing the whole task as shared context. It reaches the model after the tool
results already in the tail (legal OpenAI chat; strict Gemma-class templates are a model-profile
concern, ADR 0025). A message typed while the model works is **staged** (queued for the next
boundary), and a queue left standing by Esc or a loop error is **held** (nothing auto-sends after
a stop; the next ⏎ sends it, Backspace on an empty box pops the newest back into the editor).
Staged and held rows are session-ephemeral — sessions record what was committed (ADR 0022).
Mid-run delivery is 1:1 (one row, one marked message); a flush at idle joins the rows into ONE
**unmarked** message, because exactly one unmarked user message opens an Exchange. A **Skill** or
a **File reference** named in a staged message is message content and rides the Interjection with
it; a `/command` never queues, and which ones may run mid-run is a **per-command** policy rather
than a blanket "at idle" rule — the reporting verbs (`/version`, `/skills`, `/confine` status) and
the Schedule pair (`/schedule`, `/schedule-stop`, which touch only the scheduler library and never
this session's engine — [ADR 0033](docs/adr/0033-the-scheduler-is-a-library-and-the-tui-is-its-first-driver-surface.md)) run
immediately, every other verb is offered *tagged* in the menu and refused with a note that leaves
the line in the box (ADR 0027, amending ADR 0025's decision 10). See
[ADR 0025](docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md).
The same message shape reaches a **Sub-agent**. Inside its **Run view** the prompt box addresses
that child: `Agent.InterjectChild` queues the message into the child's own engine-side mailbox and
the goroutine driving that child delivers it at its next between-Steps boundary, so what lands is
an ordinary Interjection in the child's conversation with the child's tools, mode and Confinement
unchanged. It is ADR 0025's boundary unmoved, and the child is the one exception to that ADR's
"drive `Step` yourself" advice — nobody else can drive a child's Steps — so the two options it
rejected (a Run drain, an interjection Event) are superseded for **depth > 0 only**
([ADR 0063](docs/adr/0063-sub-agent-runs-are-user-addressable-views.md)). A staged row addressed to
a child names its run (`queued for <name> — …`), and the child's own delivery report is what takes
it off the band: a message that landed becomes that child's user block inside its run, and one the
child finished before reading becomes the note `<name> finished before your message landed`.
_Avoid_: "steering" / "steer" (ADR 0014's guided-decomposition sense — a Mechanism shaping the
model's own primary call, not a human speaking), "scheduled message" (nothing is clock-timed;
it means deliver-at-the-next-boundary), "queued input" alone (the queue is the staging, the
Interjection is the message).

**Prompt recall**:
The per-workspace list of inputs the human has already **sent** from the prompt box, and the walk
through it the box offers on **↑/↓** — a terminal's own gesture, brought to the box. ↑ on an
**empty** box loads the newest entry with the caret at its end; further ↑ steps older and stops at
the oldest; ↓ steps newer; ↓ one past the newest empties the box again, so the walk is always
reversible. Recall owns the arrows only while the box holds a **freshly recalled entry the human
has taken no other action in** — typing, editing, pasting, or a click in the box ends recall mode
and hands ↑/↓ back to the caret, and a recalled `/command` deliberately does **not** open the
suggestion pane, which would claim those arrows first. It is live where the box is the human's own:
at idle, and while the agent runs (where ⏎ stages an [Interjection](#turns-and-stepping)). It is
**not** live under an [Ask-user](#safety-and-autonomy) prompt — there ↑/↓ move the choice
highlight. What is recorded is what was *sent*: ordinary messages, whole-line `/command`
invocations, and Interjections — **not** Ask answers, which answer the model's question rather than
speak the human's own input, and **not** the session-reset pair `/new`/`/clear`, which is
deliberately never recorded so a walk cannot hand back a line whose ⏎ wipes the session. Storage is one JSONL file per workspace under the config home
(`~/.apogee/prompts/<digest-of-workspace-path>.jsonl`, `internal/recall`) — nothing is written into
the project tree; consecutive duplicates collapse, and a start-up load hands back the newest 1000.
It is **driver-side state only**
([ADR 0031](docs/adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)):
the engine never sees it, and a Driver that wires no recall seam has the feature simply off.
_Avoid_: "prompt history" / "history" (that word belongs to the [Session](#identity-and-shape)
browser's list of records — see its own Avoid line), "command history" (`/command` lines are one of
the three things recorded, not the point), "the buffer" (the box's live text is a draft; recall is
what has already left it).

**Undo journal**:
The engine's record of what the agent's file writes **replaced** — one **pre-image** (the bytes
that were at the path, or the fact that nothing was) plus the hash of what the mutation left, per
path, grouped per [Exchange](#turns-and-stepping). It is what the human-facing `/undo` puts back:
one Exchange per step, most recent first, previewed before it applies. Capture sits at the shared
write funnel (`internal/tools`' `safeWriteFile` and the copy/move/delete sites), so the covered
set is exactly the workspace-scoped write tools and nothing else — subprocess, git-checkout, MCP
and embedder-registered writes are outside it by construction. Groups materialize **lazily** (an
Exchange that wrote nothing is never a step to walk past), an [Interjection](#turns-and-stepping)
opens none, and a delegated sub-agent writes into its parent's group, so one undo takes back one
instruction. A path whose content no longer matches what the agent left is **skipped and
reported** — the human's own later edit outranks the undo. The journal lives on the engine
(`internal/undo`), is **live host state, never [Session](#identity-and-shape) state** (ADR 0022
§8): it is per process, dies with it, and holds no redo. See
[ADR 0051](docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md).
_Avoid_: "undo history" (it is a stack of Exchange groups, not a per-keystroke history), "undo
stack" for the record itself (the stack is how `/undo` walks it; the journal is what is written),
"snapshot" (nothing whole-tree is copied — only the paths a write touched), "rollback" (that word
belongs to the abort-rollback boundary in [Exchange](#turns-and-stepping)).

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
  (`web_fetch`/`http_request`) auto-run url-filtered in both (they no longer gate in Auto —
  ADR 0012 reversed ADR 0004 here).
_Avoid_: "permission level", "trust mode".

**Floor guard**:
Plain **engine behaviour** that changes only what the model sees **after its own failure**, or
shapes the request **without steering it** — so it needs no per-model proof and cannot regress
Bypass ([ADR 0071](docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md)).
A Floor guard is **not a Mechanism**: no catalogue row, no `MechanismID`, no descriptor, no
Capability, no strikes and no Turn-Budget throttle — the per-Turn `maxPostResponseRetries` bound
is the only limiter the post-response guards share. **Six ship, on in every arm** — Bypass
included — each switched off by exactly one top-level, **file-only** boolean (no flag, no env;
editable live in `/settings`) whose key names it:
- **tool-call repair** (`tool-call-repair`) — an unknown tool, malformed arguments or a missing
  required parameter answered with the correction the Turn re-streams with. A call naming a tool the
  engine *has* but this request's menu **withdrew** (Plan's filtered menu, a delegate's wrap-up Turn)
  is left alone: that call belongs to the mode, and the mode's own refusal is the answer that reaches
  the model.
- **tool-loop breaker** (`tool-loop-breaker`) — a response repeating the previous Turn's exact
  calls answered with a directive that names the repeat and steers at the remaining work.
  **[Exchange](#turns-and-stepping)-scoped**, scan and recap both: a human re-asking for the same
  thing opens a new Exchange, and its first call is the work just asked for rather than a loop.
- **empty-response recovery** (`empty-response-recovery`) — a reply carrying neither text nor a
  tool call answered with the completion-check nudge.
- **tool-use enforcer** (`tool-use-enforcer`) — a second narration where an action was asked for
  answered with the tool menu and the instruction to call one of it.
- **read cache** (`read-cache`) — a redundant re-read of a file already read successfully and not
  written since, capped to a header slice so the copy already in the conversation stands.
- **tool-result cap** (`tool-result-cap`) — every older tool result that outgrew its fraction of
  the Budget trimmed in the **request projection**, the conversation itself untouched (see
  [Tool-result capping](#tool-result-capping)).
The decision logic is **pure policy** in `internal/floor`; the seams that call it, the live on/off
gate and the events a firing emits are `internal/agent`'s. `domain.FloorConfig` spells the six as
`Disable…` bools, so an embedder handing `New` a bare `Config` gets the **whole floor**. A firing
reaches every Driver as a **`FloorGuardEvent`** keyed by the guard's config key
(`MechanismFiredEvent` stays for the lab rows).
_Avoid_: "Mechanism" for a guard (a guard is not catalogued, gated or self-regulating, and no bench
arm switches it on), "off-ramp" (the Capability that named two of them — see
[Retired terms](#retired-terms)), "always-on Mechanism".

**Bypass mode**:
A `Config` flag **orthogonal to Agent mode** that turns the catalogued **Mechanisms** — the lab
rows — off while leaving the agent's structure intact. It says nothing about the
[Floor guards](#floor-guard), which stay on in **every** arm, so the floor is *functional* — a
baseline that quit at the first stumble would pass the hard constraint trivially. With the shipped
catalogue frozen **empty** ([ADR 0071](docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md)),
`--bypass` on a stock install switches nothing off in effect: the honest floor it names is now the
engine's own — guards on, nothing armed above them — and the control arm and the shipped
default are literally the same agent unless a bench arm arms something. Budget, Compaction, and the
rest of the loop still run: Bypass is the honest
"Mechanisms-off" floor, **not** a naked model. It is also the bench's **aggregate control
arm** — the same code path users can run — against which the hard-constraint non-inferiority
gate is proved. See [ADR 0006](docs/adr/0006-bypass-mode-is-the-mechanisms-off-floor.md) and
ADR 0071 above.
_Avoid_: "naked model" (Bypass keeps the structural reducers and every Floor guard on), "disabled
mode", "raw mode".

**Approval**:
The human-in-the-loop gate on a single tool call — the primary safety guarantee in
Ask-Before mode. Delivered through a delegate the host (TUI, embedder) supplies.
An *allow for this session* grant is keyed on the call's canonical, key-folded arguments, so one
executed call is one remembered decision however the model spelled it; a call whose argument keys
COLLIDE under that fold — two spellings of one parameter — is refused before it is resolved, since
no single reading of it would describe what the tool would run.

**Ask-user**:
A free-text question the model puts to the human mid-task (via the `ask_user` tool), answered
through a host-supplied **`Asker`** delegate — the public analogue of the **Approver**, but
**not** a safety gate: it carries no allow/deny semantics, never bypasses the disposition, and
is `ReadOnly` (it runs even in Plan, mode-independent). A `nil` Asker means the tool is simply
not registered; a headless host must supply an Asker that **fails safe** (no hang). The TUI
implements it as an input-prompt rendezvous (the free-text sibling of the Approval prompt); the
bench as a scripted responder. Added in P3.11. An optional **`multi_select`** flag on the request
opts one question into *several-of-the-above* answering: the Driver lets the human tick any number
of the offered choices — the TUI draws a `[x]`/`[ ]` box on each row, `␣` toggles the highlighted
one and `⏎` sends — and the reply carries **every ticked label on its own line** inside the single
`AskAnswer.Text`, in the order the choices were offered (labels, never indices). The flag is
additive and **off by default**: absent or false is the single-select question that was always
there, byte-identical on the wire and on the screen, and neither the `Asker` interface nor
`AskAnswer` changes shape for it.
_Avoid_: conflating it with Approval — an answer is not a permission.

**Confinement**:
OS-level restriction of the **unbounded subprocess surface** (shell / subprocess), attaching to
**blast radius, not to a mode-wide binary** (ADR 0012, superseding ADR 0004): a tool runs
unsupervised only if its blast radius is bounded — **either** by OS confinement of the subprocess
surface (Linux **landlock** applied pre-`execve` on the child; macOS **`sandbox-exec`** wrapping
the child; Windows a restricted **low-integrity token** handed to process creation, with the box
expressed as a mandatory label on the disk and reverted on teardown — one clean subprocess
granularity on all three), **or** by Apogee's own
**path-safety-to-workspace** for its own in-process write tools **and url-safety for its own
network tools** (a third-party tool of either kind, whose scoping Apogee cannot vouch for, gates
instead of running unsupervised). It is a **capability
matrix, not a one-bit flag**: each backend reports what it can enforce (`fs-write`, `network-egress`,
…). In **Auto** the network is **open by default**, so **`AutoEligible()` requires filesystem
confinement only** — Linux Auto needs landlock ABI ≥1 (kernel ≥5.13), not ABI v4. The unbounded
surface is tuned by the global **`confine-to-workspace`** flag (below). The per-tool teeth remain:
**MCP**, which executes in a server Apogee cannot fence, gates through Approval whenever
`confine-to-workspace` is on; and if fs-confinement is *unavailable* on the host, subprocess tools
gate too ("confine if you can, gate if you can't") rather than refusing Auto. Apogee never runs a
tool call both unsupervised *and* unbounded. Lives behind a `platform/` `Confiner` interface
(seatbelt / landlock / Windows token — AppContainer was **rejected**, ADR 0020); default box =
workspace-write-only + **network-open** + per-project allowlist. Only `fs-write` is ever claimed
on Windows: `network-egress` is not enforceable there, so it is not advertised, and a box that
asks for it fails closed. See
[ADR 0012](docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md)
and [ADR 0020](docs/adr/0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md).
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

**Scratch dir**:
The per-session writable directory **outside the workspace** —
`~/.apogee/scratch/<session-id>/`, a dotdir sibling of `sessions/` — that the **Confinement**
box carries as an extra writable root (ADR 0056). It exists because a workspace-only fence left
a confined agent nowhere safe for scratch work, so improvisations landed in the workspace (the
2026-08-22 clobber incident): now scratch tests, probes, and temp files have a home the fence
allows, named to the model via the **`{{scratch}}`** prompt placeholder — for a user's own prose —
and, unconditionally, by the **Orientation block** that rides on every standing system message.
Created `0700` when the session id is minted, follows the **active** session across rotation,
advertised writable only once it actually exists, and swept by a
best-effort 14-day startup GC. Per-session constant, so prompt use is KV-cache safe. A **Firing**
gets one of its own on every **Driver** — the in-session Schedule, the daemon and
`apogee headless` each mint the record id before the run and create that id's dir, so the dir and
the saved record share one name and the same sweep reclaims it.
_Avoid_: "temp dir" (`/tmp` is exactly what confinement may deny), "cache" (it is disposable
work space, not a cache with an invalidation story).

**Orientation block**:
The engine-composed part of the **standing system content** that states the host facts a model
needs to get oriented — the **workspace** path, its **Scratch dir**, the `/tmp` caveat, the
read-only library roots with the tools that reach them, and — only under `sub-agents-choice:
model` — a **Delegations** line describing both **Delegation seats** symmetrically (each seat's
entry name, its `description:`, its `model:` pin and the bound session model) so the model can
choose a `run_on`. It is **harness text, not persona text**:
the engine writes it, so no edit to the user's `system-prompt-text` — and no install whose config
was seeded before a fact existed — can lose it. It **rides along**: appended only when a standing
system message exists anyway (a rendered template and/or **Context files** blocks), never
on its own, so the no-prompt-**and**-no-context-files posture — since ADR 0064 reached only through
`use-default-prompt: false` or an explicitly empty configured template, no longer by deleting the
key — stays byte-identical on the wire and the **Bypass** floor is untouched. Wire position is
directly after the prompt —
prompt → orientation → delegate block (delegations only) → context files → mechanism directives →
tool block — so no workspace text precedes it and a repo file cannot open with a forged copy the
real one then reads as a correction of; every fact it states is a per-session constant, so it is prefix-KV-cache safe —
the Delegations line carries no availability state and moves only on the human doors (`/server`,
`/model`, `/sub-agents-server`), the way the **Scratch dir** moves at a session boundary. A
fact the session does not have is omitted rather than rendered empty. See
[ADR 0023](docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md) §6
and [ADR 0069](docs/adr/0069-the-top-level-model-picks-the-delegation-seat.md).
_Avoid_: "system prompt" (that is the user's configured template; this is the engine's own text),
"preamble" (the terminal tool's fail-fast preamble is a different thing).

**Delegate report block**:
The engine-composed part of the **standing system content** that a **delegated** agent — and only a
delegated one — carries: it tells the child that the agent which delegated the task sees nothing of
this conversation and receives only its **final reply**, so anything not written there is lost, and
asks it to report what it found, what it changed and what remains unfinished by citing `path:line`
rather than pasting file contents. Like the **Orientation block** it is **harness text, not persona
text** — engine-owned, no config key, no **Mechanism** gate, so it is on under **Bypass** and no
edit to `system-prompt-text` and no workspace **Context files** can remove it (a context line that
spells its opening sentence is fenced `[workspace text] `, exactly as a forged orientation header
is). It is gated on **depth**, not on configuration: every agent below the top level gets it, at any
depth, routed or unrouted, and a top-level session's first system message is byte-identical without
it. It **rides along** under the Orientation block's rule — composed in only when a standing system
message exists anyway — and sits between the orientation and the **Context files** blocks, so every
engine-owned part still precedes the repo-controlled text. It does not compete with the
**Sub-agent** step cap's wrap-up directive: that says which turn is the last, this says what the
final reply is for. See
[ADR 0023](docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md)'s
2026-09-02 addendum.
_Avoid_: "wrap-up directive" (that is the step cap's one-turn notice), "sub-agent prompt" (a child
inherits the same **System prompt**; this is a separate engine-owned block).

**Console**:
A persistent interactive program — a REPL, a dev server, a shell — the model opens and drives
across [Turns](#turns-and-stepping) through `console_open` / `console_send` / `console_read` /
`console_close`; the `terminal` tool stays one-shot. A Console is **live host state, never
[Session](#identity-and-shape) state** (like the [Undo journal](#turns-and-stepping)): it lives
until closed, `/new`, a session restore, or engine exit; a snapshot, fork, or resume inherits
**none**; a delegation's end closes the ones it opened. `open` and `send` carry the Subprocess marker and each
`send` takes its own **Resolution**; `read` and `close` sit on the read-only floor. Ships
**default-off**, profile-enabled (ADR 0057) — the first tool to use that state. See
[ADR 0059](docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md).
_Avoid_: "terminal session" / "PTY session" (the mechanism, and "session" is the saved record),
"persistent terminal" (`terminal` is the one-shot tool), "shell" (a Console can host any program).

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

**Settings surface** (`/settings`):
The **full-height pane over the key registry** — the in-app view of `~/.apogee/config.yaml`. The
**key registry** is one declarative table describing every config key (path, kind, default,
env-var and flag names, global-only, editability, masking, validation hook, one-line description);
both the pane and [Resolution](#safety-and-autonomy)'s multi-source precedence read their metadata
from it, and a reflection **bijection guard** against `fileConfig`'s yaml tags makes a schema key
without a registry row a test failure — the screen cannot drift from the schema. The pane claims
the **entire transcript row budget** while the frame floor (status line, input box, footer) stays
drawn — a new pane class, `layout.md`'s first surface allowed to take all of it — lists every key
with its resolved value and source marker under white section headings and a fixed two-line
description header, and **persists one key per committed edit**: spliced
into the file comment-preserving, re-parsed and verified against the original apart from the target
path, written atomically; an inserted key lands below its commented example, and **reset deletes
the key's active line** rather than writing today's default.
Every committed edit is also **applied to the running session** (ADR 0037, superseding ADR 0035's
mode-only live apply): the ⏎ that persists routes the key to whatever puts it into effect — an
anytime-safe engine setter, an idle-only validate-then-commit rebind, or the composition root — so
**no key waits for a restart** and no row says "(next launch)". What a row keeps instead is the
**session edit journal**'s ` *` marker: *this surface changed this key this session*, cleared only
by relaunch. The one deferral wording left is a **boundary note** for a key that lands at a
boundary the session crosses anyway ("· applies at next clear", the `context-files:` pair, whose
KV-prefix stability is deliberate); a pane edit **outranks an env/flag override** for the running
session (the row notes that the override wins again at the next start, startup precedence
unchanged), and a persist whose apply then failed says so ("saved — live apply failed: …").
Editing is **hybrid**: simple keys are edited in the pane — a bool toggles, a 3-plus-option key
opens a selection popup, a string or an int opens a real single-line field on its row (cursor keys
and mouse), the inline system prompt a multi-line field (⏎ inserts a newline, ctrl+s commits) —
while the six nested structures take an **external edit**: ⏎ opens the human's own editor at that
key's line, and every changed key is applied through those same two homes (a changed `mcp-servers:`
**reconnects**, validate-then-commit: the new set is dialled first and the old sessions keep serving
on failure; startup connect stays fatal). Which editor is the **editor ladder** — the `editor`
config key, then `$VISUAL`, then `$EDITOR`, then the platform's **OS opener** (`open` / `xdg-open` /
`cmd /c start`) — an explicit setting outranking an ambient one, so a command set for apogee is
never quietly beaten by a variable the row cannot show. How it is started splits on that answer: a
**terminal editor** (the nine names that draw over the tty) takes a **foreground** launch — the TUI
suspends, and its exit still triggers the re-read —
while everything else takes a **detached** launch: nothing is suspended, nothing is waited on, the
pane stays standing and the row says "opened in your editor". The `server` row performs the full
**live switch**, identical to `/server` — and takes no
reset, since its line is the switch's own recording (ADR 0036) and deleting it would leave the
session on a server the file no longer names, so `⌫` is inert there and its hint line says so;
`confine-to-workspace` / `unconfined-hosts` stay display-only with a "use /confine" pointer (that
loosen stays single-homed in **`/confine`**, ADR 0012). Idle-only, and the external edit is
offered between runs only.

What makes an edit apply is no longer the editor's exit but the **config watch**: a poll of
`~/.apogee/config.yaml`'s mtime and size on a ticker, in-process and dependency-free, whose report
is one **wait** the renderer parks (the Heartbeat posture) and re-arms per landed report. Any save
of that file applies live — the pane's own jump, a GUI editor in another window, a `vim` in a second
terminal — through the same re-read/diff/apply the round trip already used, so the two triggers
cannot land a key differently. Two rules bound it: the **last-good rule** (a file that does not
parse is ignored and the previous projection kept, because a poll will read a half-written save; a
run of three consecutive unreadable reports surfaces one transcript note, not repeated until the
file parses again), and **baseline refresh on every apply**, which is what keeps a pane write from
double-applying when the watch sees the file apogee itself just wrote. `server` is the one key a
re-read never reports (ADR 0036 decision 2). It **reconciles** the
standing "apogee never writes your config" claims (seeding never overwrites, Probe prints
paste-ready YAML, `/model` does not rewrite the file): never *unprompted* — a settings-screen edit
is a deliberate user act and names the file and entry it changed, the same fence ADR 0012 applies
to `/confine off --save`. See
[ADR 0035](docs/adr/0035-the-settings-surface-persists-one-key-per-deliberate-edit.md) (the
persistence contract) and
[ADR 0037](docs/adr/0037-every-settings-edit-applies-to-the-running-session.md) (the live apply),
as amended by [ADR 0041](docs/adr/0041-the-config-file-is-watched.md) (the editor ladder and the
config watch, superseding 0037's `$VISUAL`→`$EDITOR`→`vi` ladder and its diff-on-exit trigger).
_Avoid_: "settings menu" / "preferences dialog" (it is a pane inside the frame, not a modal
takeover), "config editor" (it edits declared keys through a verified splice, and hands the file
itself to the external edit for the rest; it is never a text editor of its own), "auto-sync" (no
boot-time key syncing exists — rejected in ADR 0035), "pending edit" / "(next launch)" (nothing is
pending — an edit applies on the ⏎ that persists it; the marker ADR 0035 introduced is abolished,
not narrowed), "`$EDITOR` round-trip" as the name of the whole mechanism (`$EDITOR` is one rung of
four, and a detached launch has no round trip to come back from), "file watcher" in the
`fsnotify`/inotify sense (it polls; there is no OS notification and no daemon).

**Color scheme**:
The **palette apogee draws with, as a file of semantic roles** — `ui.color-scheme` names it, and
the name resolves to `~/.apogee/schemes/<name>.yaml` first and an **embedded built-in** second, so
a user file **shadows** a shipped scheme of the same name rather than replacing it (ADR 0040). A
scheme file is YAML with **one key per role — 29 of them**, named for meaning rather than place
(`error`, `code`, `tool-header`, `file-ref`, `skill`, `muted` / `muted-bright`, the four `mode-*`,
the four `spinner-*`, …) — and **every key is optional**: a missing one inherits the built-in
**`dark`** default, which is the palette apogee has always drawn with apart from three roles
retuned for legibility while the scheme system was being built (`code`, `tool-header` and
`tool-marker`), and remains the default scheme. Two schemes ship, `dark` and a `light` one *for
light terminals*; both are compiled into
the binary with `go:embed` and are **never installed on disk and never downloaded** —
`/color-scheme export <name>` is the only way one reaches the user's disk, verbatim comments and
all, and it refuses to overwrite. Loading is **forgiving**: a bad hex, an unknown key, an
unreadable file or an unknown name each cost a default plus a **warning as a transcript ephemeral
note**, never the run. The scheme in force is switched live — a picker row on the
[Settings surface](#safety-and-autonomy) and `/color-scheme <name>`, both rebuilding the `theme`
and clearing the block paint cache — and schemes recolor **only what apogee already colors**: no
full-screen background is painted and no glyph, marker or layout is themed.
_Avoid_: "theme" (that is the internal struct of built lipgloss styles the scheme is compiled
into), "dark mode" / "light mode" (nothing is auto-detected — the key is the user's declaration
about their own terminal), "color palette" for the file (a palette is a bag of colors; a scheme
assigns one to each named role).

**Safety guardrails**:
Apogee's production safety set: Agent modes, Approval, path-safety (TOCTOU-safe at use time via a
Go 1.26 `os.Root` pinned at the workspace root — `security.SafeWriteFile`/`SafeReadFile`, so an
escaping symlink component swapped after the check is refused at write/read time), **url-safety**
(the network tools' `URLGuard` — scheme/host allow-deny plus a **default-on SSRF floor** that denies
loopback / private / IMDS / link-local **plus** RFC-6598 CGNAT `100.64/10`, the whole `0.0.0.0/8`,
TEST-NET / benchmark ranges, NAT64-embedded private/loopback under the `64:ff9b::/96` well-known
prefix (decoded, since RFC 6052 fixes it at `/96`), and — denied **wholesale**, since no decode of
them is sound or wanted — the RFC 8215 NAT64 local-use prefix `64:ff9b:1::/48` (which fixes no
translation length, so the embedded v4's bit offset is the operator's choice) plus the obsolete v6
transition / site-local ranges (6to4 `2002::/16`, IPv4-compatible `::a.b.c.d`, `fec0::/10`) — all
**by resolved IP**, re-checked at dial time so DNS-rebinding is closed; tighten-only, never
dissolvable by config —
and applied through the **one network funnel** every one of Apogee's own network tools reaches the
network through, so such a tool cannot reach the network unfiltered, and one that does not route
through it is not vouched for and gates; the network tools and the MCP transports honour the
process's `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` the way the LLM client already does, and the
order of judgement is unchanged by one: the guard judges the **destination** before the request
leaves, while the dial-time control pins the **proxy's own** resolved addresses, since the proxy
is what a proxied transport actually dials), tool-argument-guard (incl. the **Dangerous-action guard**
floor, the `http_request` header filter, a leading-`-` guard on git ref args, and **network
failure-message redaction** — every network tool's failure message names only the bare host, never
the key-bearing request URL), circuit-breaker, and a
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
**force-approval** (`curl | bash`-class — sometimes a legit installer — and a write under
`~/.apogee`, apogee's own control plane, which the operator legitimately curates by hand
(ADR 0049 §4) — except the session's own scratch dir, which the box already declares writable
(ADR 0049 amendment 2026-08-28): a speed-bump that forces the Approver even in Auto, carrying the rule's way out to
the prompt and to a denied call's result; a forced look on a call Auto would have confined stays
confined once allowed — approval decides *whether*, confinement *where*). Rules match the call's **action text** — the tool, its target paths, its
command lines and code — and never the **payload** a call carries (a file body, a replacement string,
a search pattern, a commit message), so writing or grepping a document that merely *quotes* `~/.ssh`
is not an action. It is **tighten-only** and trivially bypassable by anything determined,
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
server (sse / streamable-http) passes **url-safety**'s scheme/host allow-deny and dials **pinned to
the configured endpoint's own addresses** — the endpoint is exempt from the resolved-IP SSRF floor
(you named it in your own config; the floor is the anti-*model* control), while any *other* private
address that connection is pointed at is still refused, redirects are not followed and the
connection goes out through the configured egress proxy when one applies (ADR 0012,
Amendment 2026-07-26); a stdio server is a trusted local launch (no URL check), its calls still gate. **Resume reconnects
fresh** — no server-side state is restored (ADR 0008). The *client shape* is
[docs/design/mcp-client.md](docs/design/mcp-client.md); the *gating* is ADR 0004/0008/0012.
_Avoid_: "MCP plugin", "MCP proxy" (it is a client; there is no proxy).

### Mechanism and hook points

**Mechanism**:
A unit of gated, self-regulating behaviour that fires at a defined **Hook point** in
the loop to help a small LLM. Since [ADR 0071](docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md) the
term names the **lab surface**: the hook API, the registry, `Config.EnableMechanisms`, the
`mechanisms:` key, the `/settings` row and `--bypass` all stand, because they are how a bench arm
arms an intervention without patching the engine — but the **shipped catalogue is frozen and
empty**. The six rows every model benefited from became [Floor guards](#floor-guard); the other
fourteen retired onto the **retired roll** (`internal/mechanisms/retired.go`), which answers an old
config naming any of them with the release that retired the ID and, for a promoted one, the config
key that succeeds it. A stock install therefore runs **zero Mechanisms**, and that is the intended
end state, not a transitional one. Every catalogued Mechanism is *gated* (by conversation state,
resource pressure, prompt shape, or model output), is subject to self-regulation unless declared
exempt, and **ships off** until an A/B bench run turns it on (rule D1) — a gate that binds
catalogued rows alone, never a Floor guard.
_Avoid_: "intervention" (that is the bench's per-Turn experiment — a different surface,
see [Intervention](#intervention)), "transform"/"analyzer"/"injector" as a *kind* (these
were the retired proxy-era taxonomy — see below), "rule", "Mechanism" for a
[Floor guard](#floor-guard) (a guard is engine behaviour, not a catalogue row).

**Hook point**:
*Where* in the loop a Mechanism fires — the primary classification of a Mechanism.
Four positions plus a cross-cutting capability:
- **pre-request** — shape the outgoing request before it is sent (subsumes the old
  Transforms *and* Pre-pipeline Injectors).
- **post-response** — inspect the model response and choose an action (see below)
  before the loop acts on it.
- **pre-tool-exec** — act between the decision to run a tool and its execution.
- **post-tool-result** — act on a tool result before the model next sees it
  (`correct_tool_result` is **deferred** — owner-ratified 2026-07-04, a bench-side experimental
  hook until a production trigger is found). New to the loop; the proxy could not host it.
- **history-rewrite** — a capability that edits conversation state; may attach at more than one
  point. No shipped row attaches here or at post-tool-result: both are lab positions now.
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
proactive-nudge / response-repair — `off-ramp` survives as a lab value carried by no shipped row,
since the two that carried it are [Floor guards](#floor-guard) now),
`SuppressionPolicy` (exempt or strikes-3), and the
stacking relations — the set of Mechanisms it is declared incompatible with, and the set
it **requires** enabled (an enable-time constraint: switching a Mechanism on without its
requirements is a config error, so dependent Mechanisms are benched and shipped as a
stack). The single source of truth for which Mechanisms are exempt, which can co-fire,
and which only make sense together.
It is **catalogue data supplied when the Mechanism is registered**, not something a
Mechanism says about itself: the catalogue holds one entry per Mechanism carrying its
descriptor, its ordering constraints and the way to build it, and that one entry is
read both by the running registry when the Mechanism is enabled and by the public
catalogue query used to plan bench arms. A Mechanism and the description of it
therefore cannot disagree.

### Self-regulation

The runtime machinery that keeps a Mechanism from hurting the model — the operational
half of the hard constraint. All of it is per-Session; a new Session starts clean.

**Effectiveness tracking**:
Per-Mechanism, per-Session bookkeeping that records each time a Mechanism **acts** — an
intervention (a non-zero decision or a mutated working value), **not** a bare inspect-only
invocation (R4, so `LoopView.Fired` counts actions, matching the sim's `FiredCounts`) — and
judges the **next** Turn for it. That judgment is **three-way** (R3): a Turn is **productive**
(a novel file read, or a successful write/action), **harmful** (a tool-result error), or
**neutral** (neither), with productive winning when signals mix. The data behind Adaptive
Suppression and the Turn Budget.

An **empty final response** was a second harmful signal until the engine's empty-reply guard
made it a **fault**: a reply with no visible text and no tool calls is an upstream failure — an
aggregator's in-band error on an HTTP 200, a stream that ended before its first token — so the
Turn faults visibly instead of committing a blank assistant message, and a faulted Turn is
**discarded unjudged**. Self-regulation therefore never sees it. That is deliberate: the signal
was a proxy for *the model going quiet*, and it now indicts the Upstream, not the Mechanism that
fired the Turn before. The tool-result error is R3's harmful proxy alone.

**Adaptive Suppression**:
The **per-Mechanism** withdrawal rule: a Mechanism whose next Turn is judged **harmful** several
consecutive times in a Session (a strike advances only on a harmful Turn; a neutral Turn freezes
the count, R3) is suppressed for the rest of it, with a configurable clear-path that re-opens it
on a productive Turn.

**Turn Budget**:
The **global** withdrawal rule: after several consecutive **harmful** Turns (the streak advances
only on a harmful Turn; a neutral Turn freezes it, R3), all non-exempt Mechanisms are suppressed,
cleared when productive activity resumes.

### Context and history

These are distinct operations — "compress", "compact", "cap", and "prune" must **not** be
used interchangeably.

**Budget**:
The allocation of the model's context window across the parts of a request — system
prompt, conversation history, file context, and response reserve. The single authority
on how much room each part gets; other reducers consume it. Lives in `context/`.
It reads TWO ceilings: the **advertised window** (`Budget.Window`, the wall the server
enforces — read by overflow detection alone, since whether a request *will not fit* is the
server's question) and the **working ceiling** (`Budget.ContextLimit`, the room the session
chose to live in — what the allocation, the reply ceiling, the tool-result cap and the
compaction triggers all read).
They are the same number unless the **`working-window:`** key (top-level or per server
entry) names a smaller room, which is how a model advertising a very large window is made
affordable: bound the room rather than lie about the window.
_Avoid_: "context limit" for the advertised window (the Budget is the *allocation* within a
ceiling, and since the split its `ContextLimit` field names the **working** ceiling — the
advertised window is `Window`).

**System prompt**:
The standing instructions apogee sends ahead of the user's own messages — the **first system
message of every request**, rendered fresh per request from a **template** the user configures
in `~/.apogee/config.yaml` (`system-prompt-text` / `system-prompt-file`, plus per-model
overrides in `system-prompt-models` and the additive `system-prompt-layers`; file-only — no
flag or env) — or, when the user configures
none, from the **embedded default** apogee compiles into the binary and refreshes with every
upgrade, unless `use-default-prompt: false` turns it off. Resolution is a four-rung ladder, first
hit winning and supplying the *whole* prompt: matching per-model entry > top-level text/file >
embedded default > nothing. **Layers** are the one additive channel beside that ladder:
`system-prompt-layers:` is an ordered list whose entries each state exactly one of `text:` or
`file:` (a `file:` resolving like `system-prompt-file`), appended to whatever the ladder
picked — selected prompt first, then the layers in listed order, joined by a blank line. They
are **not a rung**: a per-model entry replaces the selected prompt but never the layers, layers
alone are sent alone, and they never fire the embedded default, which stays the fallback for a
wholly unconfigured prompt. The template's whole vocabulary is four strictly-spelled
placeholders — `{{workspace}}`, `{{datetime}}` (the **date** only, so a local server's prefix
cache survives a turn), `{{mode}}`, `{{scratch}}` — and an unknown one is a startup error, never
raw braces on the wire. It is **request-scoped**: seeded into the
request projection at position 0 and never committed to the conversation, so it appears in no
history and in no Session record, and a Mechanism's directives and the Model profile's rendered
tool menu fold in **after** it within that one message, as does the engine's
**Orientation block** (prompt → orientation → delegate block (delegations only) → context files
→ directives → tool block). A
**Sub-agent inherits** it. Distinct from apogee's two **internal** prompts, which it never
reaches by construction: the Compaction summariser's instruction and the probe battery's. It is
**config-tier**, part of the Bypass floor in both arms, never a Mechanism — and which home a new
sentence of guidance belongs in (host fact → Orientation block, standing steering → this template,
floor-wide and failure-shaped → [Floor guard](#floor-guard), model-gated and measured → Mechanism,
task-shaped → Skill) is ADR 0064's placement rule. See
[ADR 0023](docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md) and
[ADR 0064](docs/adr/0064-the-system-prompt-ships-an-embedded-default.md) and
[ADR 0067](docs/adr/0067-system-prompt-layers-are-an-explicit-additive-channel.md).
_Avoid_: "persona", "preamble" (either may be its *content*; the term names the channel),
"prompt" alone (in TUI speech that is the user's own input line), "instructions block" (that is
the profile's engine-owned tool menu, which follows it), "rung" for a **layer** (a layer appends
after the ladder's pick and never competes with it). Distinct from a **Skill**, which is
turn-local and invoked from one message.

**Context files**:
The **project's** standing text: files apogee **discovers** in the workspace root — configured by
NAME (`context-files:`, default `AGENTS.md`; file-only, root-only, no walk-up) — whose content is
folded into the standing system content beside the [System prompt](#context-and-history). Every
listed name that exists is included, in list order, each **fenced** between a
`## Workspace context: <name>` header and a `## End of workspace context: <name>` footer, and the
merged first system message reads **prompt → Orientation block → Delegate report block
(delegations only) → context files → Mechanism directives → tool menu** — the engine's own blocks
first, so no workspace text precedes them; either configured source alone seeds the message. Content is **data, never a template**: it
bypasses the placeholder language entirely, so a repo's own `{{braces}}` travel verbatim and can
never fail startup — the one rewrite is the fence, which prefixes a content line spelling that
header, that footer or the Orientation block's own header with `[workspace text] `. The read is
**session-scoped** — at construction and at each session boundary
(`/clear`, `/new`, a restore), never per request and never mid-conversation — so the bytes are
fixed for a session (the server's prefix cache survives) and an edit lands on the next `/new`; a
**Sub-agent inherits the parent's bytes**, copied rather than re-read. A missing or empty file is
skipped silently, a present-but-unreadable one **loudly** (a note, never a startup failure — it was
discovered, not named); a malformed *name* is a startup error. Oversize is **advisory only**: the
host names each loaded file and warns when the standing content exceeds the
[Budget](#context-and-history)'s system-prompt share — nothing is ever capped or truncated. See
[ADR 0026](docs/adr/0026-workspace-context-files-are-session-scoped-prompt-data.md).
_Avoid_: "project prompt", "workspace prompt" (the System prompt is the user's, these are the
project's — the terms must stay separable), "AGENTS.md support" (that is the default name, not the
concept), "loaded into context" (they are request-scoped standing content, not history). Distinct
from a **File reference** (`@file`, turn-local and user-named) and from a **Skill** (invoked by
`/token` in one message).

**File reference (`@file`)**:
A workspace file the user names with an `@path` token in their message. The loop resolves
each reference at the start of the Turn — reading it within the workspace fence
(`security.SafeOpen`, `os.Root`-pinned) and injecting its content into the user message
as that request's *file context* — and reports-and-skips a missing or escaping ref. The token
is **bare** (`@path`, a run of non-whitespace) or **quoted** (`@"path with spaces"` — `'` is
accepted too, only `"` is ever produced), where the closing quote ends the token and an
unterminated one runs to the end of that line; there are no escape sequences. Parsing
the token belongs to `internal/refs` — a Driver-neutral grammar every Driver reads, not a TUI-private
one; resolution is the agent's. A **Firing**'s prompt is read on that same grammar, so a headless
or scheduled run resolves its references exactly as a session does — except that a missing or
escaping one is skipped *without* the notice, a Firing having no event sink to carry it.
It is the same inline grammar a **Skill**
`/token` uses, and the prompt box accents both on the same rule: a token lights up exactly when it
resolves — the path is in the workspace listing, the id is in the catalog — so a typo visibly
fails to light instead of failing at submit (ADR 0027). A reference is judged by its BYTES, not
its name: content opening `%PDF-` is injected as the document's *extracted text* — the same
extraction `read_file` performs (`internal/doctext`), `[Page N]` marker lines and all — under the
annotation `(PDF, N pages; extracted text, read-only)`, and a document holding no extractable text
(a scan) is reported-and-skipped like any other unresolvable ref, while a text file someone named
`notes.pdf` still injects its text under the plain header. Each block also carries a **structural
floor** (ADR 0006, no config key, never disabled under Bypass): `resolveFileRefs` clamps a
reference's content — before its header is added — against the [Budget](#context-and-history)'s
History allocation *split across every reference of that one message* — @file blocks and attached
**Skill** bodies alike, since the two kinds land in one message and spend one allocation — so
anything past that share is elided to the same head/tail-plus-marker shape a capped tool result
gets and no reference can hand the emergency fold a most-recent message it cannot shed.
_Avoid_: "attachment", "upload" (a reference is read live from the workspace, not stored).

**Skill**:
A reusable block of instructions the user *invokes* from a message — a folder holding a `SKILL.md`
(YAML frontmatter — id, display name, summary, optional triggers — plus a Markdown body).
Skills are discovered from layered dirs (the project's `.apogee/skills`, — when
`use-project-skills` is on — the project's `skills/`, and the global `<apogee home>/skills`) plus
a fourth, **lowest-priority** source: the skills apogee **ships embedded in the binary**
(`debugging`, `planning`, `code-review`, `commit-hygiene`), never installed to disk, refreshed by
every upgrade and switched off wholesale by `use-shipped-skills`. The **user's global library wins
any cross-source id clash** while the two project dirs keep their order between themselves, and a
shipped skill is the weakest claim on an id in the system; a repo may contribute a new skill but
never replace one of the user's, and every shadowed copy is recorded so `/skills` names both
files. A skill is invoked by naming its id as a **`/token`** in the message
text — `/code-audit please check the parser` — at a word boundary and whitespace-delimited,
exactly parallel to an `@path`. The token **stays in the text** the model reads, and only a token
the catalog confirms is a reference: any other `/word` inside a message is prose (a path survives
untouched), and a **command verb shadows** a skill whose id OPENS with that verb — the cut the
command parser makes, not whole-id equality, so an id carrying arguments cannot slip past the
shadow and be parsed as the command it names. An id is **one token**: the loader refuses one
holding whitespace or a control character, because a repo writes both the frontmatter and the
folder name an id can come from. Like an `@file`, a skill is
**turn-local**: the loop resolves the extracted IDs (`UserInput.SkillIDs`) through `Config.Skills`
and prepends each body to *that one* user message, so a skill never persists as a system-prompt
edit; each body meets the same **structural floor** a **File reference** block does, elided
head/tail before its header against that message's shared reference split, so an outsized
`SKILL.md` cannot wedge the Turn it was invoked to steer. The TUI parses and offers (one merged
`/` menu, and `/skills` to browse the catalog — every row labelled with the source it came from,
and `/skills export <id>` copying a shipped skill's folder into the global library, refusing to
overwrite one already there); the agent resolves. The
`/token` is not the model's only door: **`load_skill`** is a default-on **tool** — an ordinary
`tools.enabled`/`tools.disabled` entry, never a Mechanism — with which the model fetches a body on
its own initiative, one adaptive call returning an exact id's body, a confident match's body plus
the other ids that matched, or id-and-summary candidates to call again with. The catalog itself
still never enters the standing prompt. See
[ADR 0027](docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md),
[ADR 0032](docs/adr/0032-the-user-skill-library-outranks-the-workspace.md),
[ADR 0061](docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md) and
[ADR 0065](docs/adr/0065-shipped-skills-and-the-load-skill-door.md).
A **Suggestion** is a Driver-side hint naming the catalog skills whose id, name, description or
`triggers:` best match the draft (`skills.Catalog.Suggest`, BM25 + evidence gate); painted by the
TUI in the suggestion band above the input box, accepted via Tab into a `/token`, and spent for the
session once a message is sent with it showing. A suggestion never reaches the model — see
ADR 0061 above.
_Avoid_: "plugin", "tool" (a skill is prompt text, not executable; it adds no capability — it
steers the model; `load_skill` is a tool that *fetches* that text, the way `read_file` fetches a
file without making a file a tool), "attachment"/"chip" (a skill is text *in* the message, not
state beside it — chips are retired from every surface: the strip above the box went with ADR 0027
and the sent block's `✦ name` row with its 2026-08-04 addendum, which paints the `/token` in the
skill violet where it stands instead). Distinct from a **Mechanism** (a catalogued,
self-regulating loop behaviour).

**Tool-result capping**:
Per-tool-result truncation of any single result that exceeds its fraction of the Budget,
with head/tail preservation, protecting the most recent Turn. A **[Floor guard](#floor-guard)**
(`tool-result-cap`) since [ADR 0071](docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md),
on in every arm and applied to the **request projection** — the committed conversation is never
rewritten — implemented **once** (the surviving half of the predecessor's `compress`). Beneath it
sits a **structural floor** — not even a guard, so no key turns it off: a single result
whose estimate exceeds the *entire* History allocation is clamped as it enters the
conversation, because a result that large survives no reducer and would overflow even the
emergency fold that exists to rescue the Turn. Both render the same head/tail-plus-marker
elision (`context.TruncateToolResult`), so the model reads one idiom; the guard's tighter
cap fires first unless `tool-result-cap: false` stands it down.
_Avoid_: "compression", "compaction" (capping is per-result and non-generative), "Mechanism"
(the cap was a catalogued row until ADR 0071; it is engine behaviour now).

**Pruning**:
The **structural** conversation-level reducer that collapses *stale tool results* — and nothing
else — to one-line stubs when history outgrows its share of the Budget. Like Compaction and unlike
Tool-result capping it is **not a Mechanism**: it stays on under Bypass and is gated only by the
file-only `prune-tool-results:` key (default on). It runs at a quiescent **Turn boundary**,
rewriting committed history: above **60%** of the History allocation it stubs the oldest results
first, largest first within a Turn, until the fill is back under **40%**, and it protects the four
most recent tool-calling Turns entirely. Each stub reads
`[pruned: N lines from <tool> <argument> — re-run the call if you need it]` — the recovery is the
model's own re-run — and a pass reports itself to every Driver as one **`PruneEvent`** carrying the
results stubbed and the tokens freed. It skips whenever the context window is unknown, since a
fraction of an unknown allocation means nothing. Because it rewrites committed history it
invalidates the upstream prefix cache
([ADR 0023](docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md) §6), which is why the
band is wide rather than a single threshold.
_Avoid_: "compaction" (Pruning is mechanical and drops nothing but tool output; Compaction is
generative and summarises everything), "truncation" (the tool-result cap shortens text in place;
a prune replaces a whole result with a stub naming the call that produced it).

**Tool summary**:
The **structured half** of a tool's outcome, carried beside the prose half on the same
tool result: `Content` is the prose half and is written for the **model** (its wording is
free to change); a `ToolSummary` is written for a **host** — the TUI's tool card today, a
headless or bench renderer later — and carries the facts the tool already computed for its
own header (a read span, a match count, a diffstat) **as data** rather than as a sentence a
reader has to parse back out. It is a **sealed sum** in `domain`, exactly like an Event: the
marker method is unexported, so an embedder can *read* every variant and *add* none.
**Optional by construction**, so tools stay an open extension point
([ADR 0002](docs/adr/0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)) — a
tool that emits none renders from prose exactly as before, and only the **ten** built-ins
whose outcome the view used to re-derive carry one (`read_file`, `list_dir`,
`grep`, `view_diff`, `web_search`, `git_status`, and — as **Edit regions** — the four writing tools
`write_file`, `edit_existing_file`, `single_find_and_replace`, `multi_find_and_replace`) — `read_file`'s
carries the locate facts too, the substring asked for and the absolute line numbers it fell
on. The model-facing complement is the **syntax trailer**: the four writing tools append their
in-process syntax verdict on the bytes they just wrote to the prose half of the same success result
(`internal/tools/syntaxtrailer.go` over `internal/syntaxcheck`), so the model reads what its edit
did to the file while the host's card still shows only the tool's own first line — the write always
lands, and the trailer is feedback, never a refusal.
A summary is **never sent to the model** — it is display data a host consumes, and the
summary *value* has no wire form; what the transcript codec may mirror into the session record are
the **facts** a variant carries, which is what the neutral codec in `internal/session` does for the
**Edit regions** below
([ADR 0052](docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md) §5).
A Mechanism that rewrites `Content` on the `PostToolResult` seam does not invalidate it: a
summary records what the tool *did*, not what the text *says*.
_Avoid_: "tool metadata", "tool result type" (the result already has a type; this is its
structured outcome).

**Edit regions**:
The typed summary an edit tool records **at apply time**: each changed region's before/after
start lines, its removed and inserted lines, and up to three merged unchanged lines of context
each side — counted from the applied change itself, so a view never re-reads the file or
re-derives positions from arguments. It rides the **Tool summary** contract unchanged — display
data, never sent to the model, and no wire form for the summary value — and the region facts are
the one part of a summary the neutral codec in `internal/session` mirrors onto a wire type of
its own, so a resumed session renders the same split diffs
([ADR 0052](docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md) §5).
A result carrying none renders the argument-derived list exactly as before, which is what keeps
tools an open extension point ([ADR 0002](docs/adr/0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)).
The [Split diff](#deliverables-and-presentation) is its consumer. See
[ADR 0052](docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md).
_Avoid_: "hunks" (a hunk is a unit of the *patch input format* `edit_existing_file` accepts;
a region is a unit of the *recorded outcome*), "edit summary" (every Tool summary summarizes;
this one is the regions).

**Compaction**:
The **default** conversation-level reducer: *generatively* summarising older Turns into a
summary via the model, when the conversation exceeds a threshold. Meaning-preserving but
costs an extra model call. One fold, **two automatic triggers**: the *estimate-driven* one
fires at an Exchange boundary when the history outgrows its Budget allocation, and the
*overflow-driven* **emergency fold** fires when a request will not fit the window — the only
fold allowed to run **mid-Exchange** on the main agent, closing with a user-role bridge so the
retried request stays template-legal. A **child agent** also folds mid-Exchange, at any quiescent
Turn boundary under budget pressure, because its whole life is one Exchange and the boundary the
main agent waits for never comes. A fold that **faults** leaves the history untouched, so it stands
the estimate-driven trigger down for the rest of that Exchange rather than re-running the identical
failing call at the next boundary — the main agent re-arms when its next Exchange opens, a child
stands down for the delegation, and the emergency fold and `/compact` keep their own shot.
`auto-compact: false` opts out of all of them; the on-demand
`/compact` stays boundary-only. **Pruning** runs first: the cheap structural collapse of stale tool
results clears what it can at a Turn boundary before the estimate-driven trigger is ever reached, so
a model call is spent only on history that pruning could not relieve. See
[ADR 0018](docs/adr/0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md).
_Avoid_: "compression", "truncation" (Compaction is generative and summarises).

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
session is local (no `SSH_CONNECTION`/`SSH_TTY`/`SSH_CLIENT`), a desktop is detected, *and* the
extension is one an OS handler **renders rather than runs** (documents, images, text — its own
allow-list, which **crosses** rung 2's rather than containing it, since `.html`/`.htm`/`.svg` are
rung 2's alone and `.xhtml` is on neither: a browser runs a page as much as it shows one, and only
a served response can carry a policy that bounds it; a `report.bat` degrades to rung 0 instead of
executing). **Rung 2 (remote + browser-renderable)** — the
[doc server](#deliverables-and-presentation) serves the file and the URL joins the entry, also as
plain text; every served document is answered under a restrictive `Content-Security-Policy`
(`default-src 'none'`, bare `sandbox`) plus `nosniff`, which is what makes rung 2 the rung that
may still show active content. **Rung 3** — the
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

**Split diff** / **Stacked diff**:
The two readings of the one change body every diff-bodied tool block shares. A **Split diff**
is the two-pane reading: before on the left with its own line numbers and `-`-marked removals,
after on the right with its numbers and `+`-marked additions, unchanged context on both sides,
the panes row-aligned and long lines wrapped in place. A **Stacked diff** is the same regions
read vertically — context, removals, insertions, numbered the same way — and is what a
terminal too narrow for two readable panes shows. **Width decides between them** (a per-pane
minimum, not a magic terminal width), and **color never carries a change alone**: the `-`/`+`
markers stay, and additions wear turquoise rather than green so the pairing with red survives
red-green-weak vision. Fed by [Edit regions](#context-and-history) for the edit tools; layout in
`docs/layout/split-diff-layout.md`. See
[ADR 0052](docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md).
_Avoid_: "siff" (retired working name), "side-by-side view" (that is the split reading only,
not the pair), "unified diff" for the stacked reading (unified is the wire format `view_diff`
emits; stacked is a rendering).

### Probing and model identity

**Probe** (`apogee probe`):
The **diagnosis command** — what this machine and this model can actually do, answered **without
running an agent**. Two halves with deliberately asymmetric cost
([ADR 0021](docs/adr/0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md)):
the **host report** (`apogee probe`, and `apogee probe host` for scripts) is free, offline and
read-only — OS/arch, the Confiner backend and its capability matrix, the `AutoEligible()` verdict,
the effective [`confine-to-workspace`](#safety-and-autonomy) after the Host acknowledgement,
workspace root, config home, endpoint reachability and model discovery — *reporting* ADR 0012's
verdict, never re-deciding it, and sharing its logic with `/confine status` so the two cannot
drift. The **model battery** (`apogee probe model`) is an **explicit act**: it spends live model
calls (native tool call, JSON/structured output, multi-step tool chain) and **writes** a probe
record, so it never fires as a side effect of the bare noun. It prints suggested **model
profile** knobs as paste-ready YAML and never edits the user's config; `--no-save` runs the
battery and writes nothing.
_Avoid_: "benchmark" (the battery measures capability, not quality — scoring is [the
bench](#validation-and-the-bench)'s job), "health check" (it diagnoses, it does not monitor —
monitoring is the [Heartbeat](#probing-and-model-identity)'s job), "auto-configure" (nothing is
written into config).

**Heartbeat** (and its **Beat**):
The continuous **monitor** of the [Upstream](#identity-and-shape): every **ten seconds** apogee asks
the server which model it is serving, in which context window, and what else it advertises, and
reports the answer as one **Beat**. Deliberately the opposite pole of [Probe](#probing-and-model-identity)
on every axis — a probe diagnoses **once, on demand**, and prints a report a *human* reads; the
heartbeat runs **unasked for the life of a session** and its output is *consumed* (the footer's
model and window, the gauge's denominator, the offline state, and the rebind that follows an
observed change). A Beat is **never an error**: an unreachable server sets `Reachable: false` and
says why, because that is a finding about the server, not a failure of the observation. It is also
what **starts** a session — the first beat fires immediately and completes discovery late, so
apogee paints before the server has answered and can be started **before** its server exists.
**Rebind** is the heartbeat's apply half: `Agent.Rebind` swaps *all* the per-model bindings
together — wire model id, [System prompt](#context-and-history) template, context window, and the
[Mechanism](#mechanism-and-hook-points) set — at a **quiescent boundary** (idle, or deferred to the
end of the running [Exchange](#turns-and-stepping)), never mid-Exchange. A configured
`context-window:` is a **pin** the heartbeat never overrides; a `servers:` entry's `model`
is a **trusted** id, never substituted: whenever it is set it is the active model verbatim, and an
advertised entry supplies only its context window (an id the server does not list runs as
configured, with no window known). Only an empty `model` falls back to the first model the server
advertises — and the resolved id **follows the binding**, restated on every commit, so discovery
keeps resolving the model the session actually runs rather than the one config named at launch.
The same two halves are what the human's own switches are made of, never a third path to bind.
**`/model`** picks among the models the beat reported and drives *Rebind by hand* — same seam, same
words, and the pick is recorded as the last observation so the next beat measures it as nothing
new. **`/server`** moves the whole [Upstream](#identity-and-shape): `Agent.SwitchUpstream` binds a
fresh provider client at the new endpoint and leaves the session with **no model bound**, the
per-server heartbeat Monitor is swapped whole behind the unchanged seam, and the new server's
**first Beat completes the move** through that same Rebind — one code path with the cold start. A
switch guesses nothing about the new server and destroys nothing about the session: the
conversation, Turn counters, mode, approvals and confinement all stand. What a switch *does* carry
is what the new entry **pins** about its own server — its `context-window:` and its
`max-output-tokens:` reply ceiling, both facts about the slot rather than about the session, and a
pin is not a guess: the entry's window outranks the global `context-window:` key while the session
sits there, and an entry pinning neither leaves the window to that global key and the ceiling to the
engine's own derivation. And the launcher: moving onto an entry that names a llama-launcher config turns the
[Launch profile](#identity-and-shape) verbs on for as long as the session sits there, and moving
onto an entry without one turns them off again. The servers it can reach
are the `servers:` list (plus the unlisted one an `--endpoint` override started the session on),
and a switch **onto a listed entry records the choice**: `server: <name>` is spliced into
`config.yaml` through the [Settings surface](#safety-and-autonomy)'s writer, so the next launch
starts where the last session ended, while a move onto an unlisted endpoint — a
[Launch profile](#identity-and-shape) load, an override row — has no name and records nothing. With
`server:` unset (first boot) or naming an entry that is gone, the session starts **pre-bound** —
no engine constructed, `errMissingEndpoint` never risked — and the picker opens by itself to
complete the startup; an empty list opens the Settings surface with its "edit `config.yaml` and
restart" pointer instead. See
[ADR 0024](docs/adr/0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md),
[ADR 0028](docs/adr/0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md) and
[ADR 0036](docs/adr/0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md).
_Avoid_: "health check" (it observes what is served, not merely that something answers), "poller"
(says nothing about what is observed, and the chain re-arms from the landed beat, not off a clock),
"probe" for this (the other noun, the other job), "reconnect" for a Rebind (the connection never
moved — the bindings did), "rehome" as a noun for a server switch (the operation is
`SwitchUpstream`; these nouns carry the rest).

**Behavioral fingerprint**:
The model identity a completed **model battery** earns — the model's own advertised label, at
**medium** confidence. The battery raises an identity's *tier*; it never re-spells it (ADR 0021,
Amendment 2026-07-22), because that label is the key [Validated set](#validation-and-the-bench)
entries and user aliases are both filed under, and a
probe that renamed the model would orphan every one of them. It is the middle rung of the
`ModelFingerprint`'s own best-available ladder — weights-hash (**high**) → behavioral fingerprint (**medium**) →
metadata label (**low**) — and the *only* source of `ConfidenceMedium`, because identity is
resolved offline at startup: it reaches later sessions **through a persisted probe record**
(versioned, owner-private, keyed on endpoint + advertised label + probe timestamp; any defect is
skipped with a warning, never a blocked startup). What the battery observed is recorded beside
the claim as the **behavioral signature** — a **fuzzy feature match over battery outcomes**
(which capabilities were observed; logprobs preferred where the Upstream exposes them), **never a
hash of response text**, so sampling noise or a re-worded prompt does not move it. The signature
is *evidence*, never a match key: comparing it across probes is what makes a swapped model behind
an unchanged label detectable. Consequence worth knowing before running the battery: at medium
confidence a matching Validated set **auto-applies** instead of being offered (ADR 0016 §5) — so
probing is the act that switches that automatism on, and deleting the record (or `--no-save`) is
the off-switch.
_Avoid_: "response hash" / "output signature" (explicitly rejected — noise, not identity),
"model detection" (it identifies the model *behind* a label, it does not name it), calling the
signature a fingerprint (the signature is the evidence; the fingerprint is the identity).

**Capability tier**:
The ordinal summary of a battery run — what the model can be **asked** to do (native tool calls,
structured output, multi-step chaining). Today it is a **reported signal only**: it carries no
automatism, gates nothing, and changes no request. The *adaptive prompt complexity* idea it
exists for — slimming tool descriptions and system prompts for a lower tier — is a parked
follow-on (`ISSUES.md`), because that is a model-facing **Mechanism** and a Mechanism ships on the
non-inferiority gate, not on plausibility ([ADR 0009](docs/adr/0009-the-ab-decision-rule.md)).
_Avoid_: "model tier" / "model size" (it describes observed behaviour, not parameters or
quality), treating it as a config knob (it is an observation).

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
not required). Keyed on the
confidence-tagged `ModelFingerprint`, resolved best-available — the evidence attaches to the
precise model measured, and any carry-over to a sibling quant or family member is an explicit
human decision, never automatic. A model with no Validated set runs the catalogue's global
defaults (the D1 floor) — and with the shipped catalogue empty, that is the
[Floor guards](#floor-guard) and nothing above them. An entry is produced only by a completed, pre-registered aggregate
Campaign passing the gate on that model — with engagement verified — regardless of who runs it.
A matching set applies **whole or not at all** — a subset, or a merge with hand-picked
Mechanisms, is a different, *unvalidated* stack — and applies *automatically* only at ≥ medium
fingerprint confidence; below that it is **offered**, and applying it (like carrying it over to
an aliased model) is an explicit config decision. Explicit mechanism config and Bypass take
precedence over auto-application. The **shipped roster is empty**: the one curated entry apogee
shipped (gemma) retired with the fourteen rows, because its evidence was a leave-one-out campaign
over a fifteen-member stack nine of whose members no longer exist and a measured set is its members
([ADR 0071](docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md)).
The surface itself stays whole — a **user's own** `~/.apogee/validated/*.json` entry still resolves,
still applies, and sheds any id that has since retired rather than being skipped as a set.
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

The rest were canonical in **this** glossary and retired with the mechanism wave of
[ADR 0071](docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md),
which promoted six catalogue rows to [Floor guards](#floor-guard) and retired the other fourteen
in **v0.20.0** on ratified verdicts rather than on ADR 0016's older "inert by construction"
precondition. Their source, tests and assets are deleted; every id stays on the **retired roll**
(`internal/mechanisms/retired.go`) with the release that retired it, so an old `mechanisms:` block
naming one gets a message rather than an unknown-id failure, and the archived
[catalogue](docs/design/archived/mechanism-catalogue.md) records the per-row verdict.

- **Off-ramp** (Exempt Mechanism) → retired as a shipped concept: a Mechanism exempt from Adaptive
  Suppression and the Turn Budget because suppressing it would leave the model with no way out of a
  failed Turn. The two rows that carried the Capability — `empty_response_recovery` and
  `tool_use_enforcer` — are Floor guards now, on in every arm, so there is no D1 default-off rule
  left for them to be the exception to. The `off-ramp` **Capability value** survives in the
  descriptor enum as lab vocabulary, carried by no shipped row.
- **Library** (the cross-session, per-model **learning store** that observed completed Turns and
  injected qualifying observations through a pre-request Mechanism) → retired with the `library`
  Mechanism: nothing observes Turns or injects learned text any more, and `~/.apogee/library/` on
  disk is never touched. `internal/library`'s other half stays and is not this term — the
  confidence-tagged `ModelFingerprint` and the persisted probe record, which serve
  [Validated sets](#validation-and-the-bench) and `probe model`. The **Failure library** was always
  the bench's own term and is unaffected.
- **History truncation** (`truncate_history`) → retired unshipped: the cheap alternative to
  Compaction, mechanically dropping the middle of the conversation and keeping the last N
  exchanges. [Compaction](#context-and-history) is the conversation-level reducer, with
  [Pruning](#context-and-history) as the cheap structural pass beneath it.
- **Guided decomposition** (`guided_decomposition`) and **`decompose`** → retired. The first steered
  the model's own primary call to enumerate remaining subtasks under measured Budget pressure and
  converted that enumeration into paced `sub_agent` delegations; the second was a prompt-shaping
  nudge that steered wording, not delegation. A model still delegates through the `sub_agent` tool
  on its own initiative — nothing prompts it to.
  [ADR 0014](docs/adr/0014-guided-decomposition-steers-the-primary-call-and-serializes-delegation.md)
  stands as history and binds nothing shipped.
- The rest of the nudge catalogue — `stall_nudge`, `list_nudge`, `tool_use_directive`, `filehint`,
  `read_loop`, `read_repeat`, `toolfilter`, `error_enrichment`, `syntax`, `autofix` → retired
  unshipped, their catalogue verdicts never having left `pending`. Two names survive elsewhere
  and are not these rows: the always-on **syntax trailer** the four writing tools append
  (`internal/tools/syntaxtrailer.go`) is unrelated to the `syntax` Mechanism, and the
  [read cache](#floor-guard) is `cached_content_intercept` promoted, not `read_repeat`, whose twin
  it was.
