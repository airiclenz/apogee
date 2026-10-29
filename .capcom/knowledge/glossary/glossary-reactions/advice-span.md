---
title: "Advice span: one provenance-ledger row recording an advise Reaction's injection"
---

**Advice span**:
One row of the **provenance ledger** — `domain.AdviceSpan`,
`{reaction, origin, moment, turn, offset, topic, leading, end}` — recorded on the message an
**advise** **Reaction** advised,
one per injection in the order they landed
(ADR 0076 D6). On a tool-shaped **Moment** the fence is a trailer on the closing tool result; on
`session-started` it is a leading fence inside the user message that opens the session's next
**Exchange**, ahead of the user's own text — still one user message, never an added one (ADR 0076,
2026-10-19 amendment). On `compaction-finished` it is the same leading fence on the next user-role
message after a **Compaction** fold — the next Exchange's opening, or the bridge an emergency fold
closes with mid-Exchange — never the summary, never the system prompt; and a span that lands there
retires the same reaction's earlier leading fences from the protected prefix's first user message
(`Conversation.StripReactionAdvice`), leaving every other reaction's spans standing, so a re-brief
replaces the briefing the fold kept rather than stacking beside it (ADR 0076, 2026-10-21
amendment). The
fence the model reads (`[advice — reaction <id> (<origin> origin) at <moment>, turn <n>]`
… `[end advice — <id>]`) is rendered from the span, never from the handler's output, so nothing
out-of-process can forge an engine header — and a workspace **Context files** line that spells
either fence line is fenced `[workspace text] `, exactly as a forged orientation header is; the
text inside is secret-redacted and capped at 8 KiB with a marker. `offset` is where the fence begins in the message's content; a leading span's `end` is where it and
the blank line after it end. The content with every span's range removed — the leading fences cut
from the front, everything from the first trailer on cut from the back — is the message as it stood
before any advice: a Floor guard judges the user's prompt by it, never by the advised content, and
it is what the **Session record** keeps: a span is **ephemeral**, written to no record and
gone on resume, so a replay never re-reads a stale SHA or timestamp. The bench attributes an advise
reaction's effect by the span's reaction id; `/advice` shows what the model saw, by Turn — the
TUI's fourth report pane, fed by the `ReactionFiredEvent` each injection books, never by the ledger
itself (ADR 0076 D6, 2026-09-20 amendment). Its board is **Driver state** like the Thinking pane's,
and empties with it at every session boundary (`/clear`, `/new`, a `/sessions` resume, `/fork`).

The same ledger also carries the engine's own **engine notes** (`Message.WithEngineNote`: a capped
delegate's wrap-up directive, the step and token budget notices, the cut a settled Exchange marks).
Such a row has `topic` set and origin `engine`, with no reaction, moment or turn, and its fence is
`[engine — <topic>]` … `[end engine — <topic>]`, never the advice fence. One strip and one staleness
guard cover both kinds (ADR 0076, 2026-09-19 addendum).

**In a test** (2026-10-21, docs/plans/archived/2026-10-21 - 00 - user-session-advice-plan.md): build
the expected fence with `domain.RenderAdvice` and the span's real shape — `Leading: true` for
session-started and compaction-finished advice on a user-role message. Without it `RenderAdvice`
renders the trailing tool-result form, its blank line on the other side of the fence, and the
comparison fails.
_Avoid_: "injection" (the system-prompt fold the trailer replaced), "advice message" (a fence
inside a tool result or a user message, not a message of its own), "advice history" (nothing
persists).
