---
Status: accepted
Amends: ADR 0078 (Amendment 2026-10-02: on the preserved-thinking models a reply's signed thinking blocks go back only over the prefix they were produced on)
---

# Replayed signed thinking is guarded by a prefix digest

## Context

On `wire: anthropic` apogee keeps a reply's signed reasoning blocks (`thinking` with its
`signature`, `redacted_thinking` with its `data`) verbatim on the committed assistant message and
sends them back in their original places on later requests to the same model
([ADR 0078](0078-a-servers-wire-is-a-per-entry-codec-inside-the-provider-client.md), Amendment
2026-10-02; `internal/agent/wire.go` `replayedThinking`).

A signature does not cover only its own block. It binds the request the reply was produced on:
the top-level `system`, the `tools`, and every message before the reply. The models that preserve
thinking across turns check that binding, and a replayed block whose prefix has changed since it
was signed is refused with a 400.

apogee rebuilds that prefix for every request, and it is not stable. The system prompt carries
standing blocks that change mid-session (`internal/agent/standingblocks.go`): the task-list
block after a `task_list` call, and the orientation block. A mode change filters the tool menu.
Tail notes (`Request.NoteOnTail`) ride the newest message of one request and are gone from it in
the next. The pre-request seam rewrites earlier messages in the projected request: `floor.CapToolResults` (`internal/floor/resultcap.go`) elides old tool results
once the window fills, pruning replaces an old result with a stub, and a cancelled Exchange's last
tool result carries the engine's cancel note (`internal/agent/turn.go` `turnLifecycle.settle`). A
session record never stores advice (`internal/domain/advice.go` `Message.recordContent`), so a
resumed conversation is not byte-for-byte the one the replies were produced on. Any of these, once
a reply carries signed thinking, made the next request a 400 (bead
`apogee-anthropic-prefix-rebuilt-per-request`).

## Decision

**A reply's signed thinking blocks go back upstream only when the request's prefix before that
reply digests identically to the prefix the reply was produced on. Otherwise the codec drops that
reply's thinking blocks client-side.**

1. **What the digest covers.** The prefix before a reply is the top-level `system`, the `tools`,
   and every message before that reply, as the anthropic codec encodes them. It is the same set a
   signature binds. Fields merged after the codec (`request-extra:`) are not part of it.
2. **Where it is computed and kept.** The codec computes the digest at encode time, over the
   request it is sending. The decoder stamps that digest into the reply's reasoning entries, both
   whole and streamed, so it is saved and resumed with the blocks themselves. On a later request
   the codec computes the digest of the prefix before each assistant message as it encodes, and
   compares it with the one the message's entries carry.
3. **A mismatch drops, it never fails.** When the digests differ, the codec leaves that reply's
   thinking and redacted-thinking blocks off the request and sends the rest of the message
   unchanged. It never sends the blocks and lets the server answer with a 400, and it never sends a
   server-side beta field asking the server to discard them. The blocks stay in history. The drop
   is silent, like every other replay rule in ADR 0078.
4. **An entry without a digest is dropped.** Reasoning entries persisted before this ADR carry no
   digest. Nothing can show that their prefix still holds, so they are dropped the same way.
5. **The drop cascades by construction.** An earlier assistant message is part of every later
   reply's prefix, encoded with whatever thinking blocks it actually sends. Once one reply's blocks
   are dropped, every later reply's prefix differs from the one it was produced on, and those drop
   too. That is what the server would refuse anyway.
6. **The digest travels per request, never as state.** It passes from `Client.encode` to the whole
   and stream decoders on each call, the way the carried-effort flag already does
   (`Client.encode` → `parseSSE`). It is never a field on the `Client` or the codec: an unrouted
   delegate shares its parent's client and codec (`internal/agent/subagent.go`
   `newChildAgentOn`) and streams at the same time, so shared state would stamp one request's
   digest onto another's reply.

### Scope: the preserved-thinking models only

The guard applies only to model ids in the provider's **preserved-thinking set**: ids starting,
ignoring case, with `claude-fable-5-1`, `claude-mythos-5-1`, `claude-opus-5-5` or
`claude-sonnet-5-5`. The set lives in the same wire-local prefix table in `internal/provider` that
holds the no-effort thinking shapes (ADR 0078, Amendment "the no-effort shape is per model"), and
a future model joins it with a table row.

Every other model replays as before this ADR: older Claude models, unknown ids, and non-Anthropic
servers on the anthropic wire. Older Claude models require the last assistant turn's thinking
blocks in a tool loop, so a client-side drop there would cause the 400 the guard exists to prevent.

### Churn sources: a rule, not a list

The guard reacts to any change to `system`, `tools` or an earlier message between the request that
produced a reply and a later request, whatever caused it. The sources known today are accepted as
churn, not removed:

- the standing task-list block, which changes after a `task_list` call;
- the orientation block, and the tool menu a mode change filters;
- the tail notes (`Request.NoteOnTail`);
- pruning and tool-result capping (`floor.CapToolResults` is one example);
- the cancel note a settled Exchange puts on its last tool result.

A source added later needs no change here: the digest catches it.

### Compaction

`context.Compact` (`internal/context/compact.go`) keeps only the conversation's prefix (its system
messages and opening user message) and a summary message that carries no thinking. No assistant
reply with signed blocks survives a fold, so nothing replays across one, and the guard has nothing
to compare.

### Resume

The digest is saved with the blocks, so a resumed session replays a reply's thinking only when the
resumed prefix re-encodes identically. It often does not: `recordContent` saves a message without
the advice that rode it, so any prefix that carried advice differs after a resume, and its replies'
thinking drops. ADR 0078's resume promise therefore narrows on the preserved-thinking models: the
blocks survive a save and resume in the session record, but they go back upstream only where the
resumed prefix still matches.

## Considered and rejected

- **An append-only prefix** (owner, 2026-10-02). Freezing the system prompt and every earlier
  message once sent, and moving the changing parts to the end of the request, would keep every
  signature valid. It would change how the task list, mode, orientation, tail notes, capping and
  pruning reach the model on every wire, to serve one wire's replay. Rejected.
- **Letting the server decide.** Sending the blocks and recovering from the 400, or using a
  server-side beta field to discard stale thinking, spends a round trip or ties apogee to a beta.
  The client already knows the prefix changed.
- **Dropping all thinking once anything changes.** Simpler, but it throws away valid replays on
  every unchanged prefix, which is the common case inside a tool loop.

## Consequences

- On the preserved-thinking models a request no longer 400s because an earlier reply's prefix
  changed. The cost is that the model loses the thinking of that reply and of every later one in
  the request.
- Reasoning entries gain a digest field. Entries saved before this change still decode, but on the
  preserved-thinking models their blocks are not replayed.
- CHANGELOG.md's apogee-4kl engine-half entry ("so they survive a session save and resume, and go
  back upstream") is narrowed: on these models a resumed session replays thinking only where the
  resumed prefix matches.
- `CONTEXT.md`'s Thinking channel entry is qualified to match.
- Implementation and its tests are plan `docs/plans/2026-10-02 - 02 - refocus-findings-plan.md`
  item 5.
