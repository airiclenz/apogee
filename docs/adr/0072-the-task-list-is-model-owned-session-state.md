---
Status: accepted
Amends: ADR 0023 (the 2026-08-25 per-session-constant bullet); ADR 0057 decision 3 is untouched
---

# The task list is model-owned session state

## Context

A long run forgets what it set out to do. Compaction is the mechanism — it summarises the
conversation and drops the messages behind it, and the plan a model wrote in its third reply is
exactly the kind of prose a summary compresses into a sentence. The standing system content is the
one place that survives that, because it is re-seeded at position 0 on every request
([ADR 0023](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md) §6), which is
why the host's own facts already live there.

Nothing in that content is the model's. The rendered template is the user's persona, the
orientation block and the delegate report block are the engine's statements about the host, and
the workspace context files are the repository's. A model that decomposes a job into eight steps
has nowhere durable to put the eight steps, so it re-derives them, drops one, or re-does one it had
already finished.

The obvious answer was denied once. The first tool-surface poll recorded "task/todo persistence" as
**Mechanism territory (guided decomposition), not a tool**
(`docs/design/tool-surface-findings.md`), and the denial was right about what it was refusing: a
**Mechanism** that inspects a request and injects a decomposition the model did not ask for is
tuning, and tuning is gated on bench evidence. It was wrong about the thing being asked for. A tool
the model calls when it judges the work worth tracking prescribes no decomposition, fires on no
Turn the model did not open itself, and is exactly the same shape as every other affordance on the
roster: a name, a schema, and nothing that happens unless the model calls it. The owner reversed
the denial on 2026-09-02 on that distinction.

Two adjacent records already answered most of the design questions this raises.
[ADR 0059](0059-a-console-is-live-host-state-the-model-drives-across-turns.md) settled how a tool
reaches state the engine holds — the engine owns it, the call context carries it — and
[ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md) §8 settled what is and
is not written into a Session record. The one genuinely new question is the standing block: every
engine-owned part of that content has so far been a per-session constant, and a checklist is not.

## Decision

**A task list is the MODEL's own state, held by the engine, persisted with the Session, and
re-rendered into the standing system content on every request.** The `task_list` tool is its only
writer.

**1 — The list is the model's, and only the model writes it.** `task_list` is the sole write path
(`internal/tools/task_list.go`); the engine never appends a task, the human has no command that
edits one, and no **Mechanism** injects one. The engine's job is to hold the list and keep it in
front of the model, not to decide what belongs on it. This is what makes the tool a tool rather
than guided decomposition: a model that never calls it runs an agent byte-identical to the one it
ran before, and a model that does call it wrote every word it reads back. The corollary is that the
list is not a plan the host can trust — it is model-authored text, and it is fenced as such
(decision 4).

**2 — One call carries the COMPLETE list; there are no item ids.** `Replace` is the only mutation:
the tool takes an array of `{text, done}` and swaps the whole list for it, so ticking a task off is
resending it with `done: true` and clearing the list is sending `[]`. An id the model must mint,
remember across turns and quote back correctly is the failure mode this removes — it is precisely
the state a compaction eats, and a stale id is a call that silently edits the wrong row or fails
for a reason the model cannot see. Whole-list replace costs more tokens per call and cannot be got
wrong. Caps (`tasklist.MaxItems`, `tasklist.MaxTextChars`) are enforced by the list and quoted in
the schema from the same constants, and a rejected replace leaves the held list exactly as it was.

**3 — It is SESSION state on `agentState`, not live host state.** The list is serialized into the
Session record's payload (`internal/agent/state.go`) and restored on `--resume`.
[ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md) §8's line falls the
other way for Consoles — a Console holds running processes, which cannot be written to a file at
all, so a resumed session has none — but the same line put the other way is what admits a
checklist: what the list holds is the sentences the model wrote about its own work, nothing that
can go stale against a filesystem and nothing that has to be running to mean anything. A checklist
whose entire value is surviving a long run would be worth very little if it did not survive the
resume that ends the longest runs. §8's rule is therefore unchanged and this is its stated
counter-example, not an exception to it.

**4 — It renders as a standing block, LAST of the engine's parts and ahead of the context files.**
The wire order becomes **prompt → orientation → delegate report → task list → context files →
mechanism directives → tool block** (`internal/agent/tasklistblock.go`). Position is the
[ADR 0023](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md) 2026-08-26
forgery argument applied in both directions: every engine-owned part still precedes the
repo-controlled blocks, so no workspace text can arrive after the host's facts and read as a
correction of them; and the list, being model-authored, goes last of the engine's four rather than
anywhere among the host's own statements. `tasklist.Fence` is the other half of that guard — a
context-file line spelling the block's opening is prefixed `[workspace text] ` exactly as a forged
orientation header is ([ADR 0026](0026-workspace-context-files-are-session-scoped-prompt-data.md)'s
2026-08-26 addendum). Ride-along is the orientation block's rule verbatim: the block is composed in
only when the configured sources already put something in the message, and an empty list renders
`""`, so §6's "no prompt AND no context files seeds nothing" anchor stays byte-identical.

**This block is the first ENGINE-OWNED standing part whose inputs are not per-session constants,
and [ADR 0023](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md)'s
2026-08-25 amendment is amended to admit it.** That amendment's "Live inputs, per-session
constants" bullet said the engine's blocks read live inputs that are nevertheless constant within a
session, so the block is prefix-KV-cache stable exactly as `{{scratch}}` is. A checklist is not:
every `task_list` call changes the standing content and invalidates the server's prefix cache from
this block onward. The amendment now admits an engine block whose volatility is **under the model's
own control**, which is the property that makes the cost payable rather than imposed — a run that
never calls the tool pays nothing, and a run that calls it is spending a re-encode it asked for.
Note also that the standing content was never wholly constant: the rendered template varies with
its live `{{mode}}`, so a Shift+Tab already moved the prefix. What is new is an *engine-composed*
part that does.

**5 — The tool is `ReadOnly`, because `IsReadOnly` measures BLAST RADIUS.** `task_list` declares
`domain.ReadOnlyTool`, so it is ungated in every mode and offered in Plan. Writing down what you
intend to do touches no file, starts no process and reaches no network; the axis
`domain.IsReadOnly` sits on is the one
[ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md) drew, and
`ask_user` and `console_close` are the standing precedent that a tool which changes something can
still be on the read-only side of it. Plan mode is where the classification earns itself: a model
deciding what it is about to do is exactly the model that most wants to write the list down.

**6 — It ships DEFAULT-ON for every model, with no config key of its own.** The tool is on the
default roster, carries no default-off marker, and is turned off the way any tool is — a `disabled:`
entry, global or per-profile ([ADR 0057](0057-the-tool-roster-is-a-third-model-profile-axis-resolved-axis-wise.md)).
Decision 3 of that ADR — the build-level default-off state — is **untouched**: `task_list` simply
does not use it. The reasoning is the one that governs the roster generally: this is the same
affordance for a 4B model and a frontier one, it costs one slot, and a new root key would be a
second way to say what `tools.disabled:` already says.

**7 — A delegation gets its OWN empty list; nothing is inherited and nothing is shared.** A child
agent is constructed with a fresh `tasklist.New()` (`internal/agent/subagent.go`), so a parent's
checklist is neither visible to the child nor writable by it, and the child's list dies with the
child. A delegated task is a different job with a different decomposition; handing the child the
parent's rows would put work it cannot do on a list it can tick off, and letting two agents write
one list would make the standing block a channel between them — a shared mutable surface with no
ADR behind it. What the parent learns from a delegation is its final reply
([ADR 0023](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md)'s 2026-09-02
addendum), and that is the whole channel.

## Considered options

**A — A Mechanism that decomposes the request into a checklist.** Rejected, and it is the thing the
original denial was right to refuse: it injects a decomposition the model did not ask for, which is
tuning under the project's hard invariant and would ship off pending a bench arm. It also gets the
ownership backwards — the value here is that the model wrote the list.

**B — Item ids with add/update/remove verbs.** Rejected per decision 2: it makes the model carry
state across turns that compaction is most likely to eat, and every failure it introduces is
silent. The token cost of resending the list is real and is the price of never being wrong about
which row was meant.

**C — Live state, dropped on resume, like a Console.** Rejected: it would delete the list at exactly
the boundary a long run most needs it, and the ADR 0022 §8 reason for dropping a Console — a
running process cannot be serialized — does not apply to a list of sentences.

**D — Render it into the user message, or as a tool result the model re-reads.** Rejected: both
live in the conversation, which is what compaction rewrites. The standing content is re-seeded per
request and is the only position that survives, which is the whole point of the feature.

**E — A config key to turn it on or off, or default-off/profile-enabled like the Console family.**
Rejected per decision 6: `tools.disabled:` already expresses it, and the Console family's default-off
posture was bought by its blast radius, which a checklist does not have.

**F — One list shared across a delegation tree.** Rejected per decision 7: it turns the standing
block into an inter-agent channel and puts rows on a list held by an agent that cannot act on them.

## Consequences

- **A prefix re-encode per `task_list` call.** Accepted and recorded here so no later reader has to
  re-derive it. The list sits ahead of the context files, so a change invalidates the cached prefix
  from that point through the tool block. Sessions whose model never calls the tool are unaffected,
  because an empty list renders `""`.
- **The standing content is no longer wholly engine- and user-authored.** Model text now rides in
  it. `tasklist.Fence` and the context-file fencing keep a workspace file from forging the block,
  and decision 4's position keeps the model's own text behind the host's statements.
- **`--resume` restores the checklist.** A resumed session reads back what it had left to do, which
  is the first session-state addition since the record's own fields.
- **The `task_list` roster slot is spent for every model.** A profile that would rather not have it
  writes one `disabled:` line.
- **The tool-surface denial is reversed in place.** `docs/design/tool-surface-findings.md` keeps its
  dated record and carries the reversal beside it, so the reasoning that produced the denial stays
  readable next to the distinction that overturned it.
