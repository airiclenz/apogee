---
title: Advice span
---

**Advice span**:
One row of the **provenance ledger** — `domain.AdviceSpan`, `{reaction, origin, moment, turn,
offset}` — recorded on the message an **advise** **Reaction** advised,
one per injection in the order they landed
(ADR 0076 D6). The
fenced trailer the model reads (`[advice — reaction <id> (<origin> origin) at <moment>, turn <n>]`
… `[end advice — <id>]`) is rendered from the span, never from the handler's output, so nothing
out-of-process can forge an engine header — and a workspace **Context files** line that spells
either fence line is fenced `[workspace text] `, exactly as a forged orientation header is; the
text inside is secret-redacted and capped at 8 KiB with a marker. `offset` is where the fence begins in the message's content, so the content before
the first span's offset is the message as it stood before any advice — and that is what the
**Session record** keeps: a span is **ephemeral**, written to no record and
gone on resume, so a replay never re-reads a stale SHA or timestamp. The bench attributes an advise
reaction's effect by the span's reaction id; `/advice` shows what the model saw, by Turn — the
TUI's fourth report pane, fed by the `ReactionFiredEvent` each injection books, never by the ledger
itself (ADR 0076 D6, 2026-09-20 amendment). Its board is **Driver state** like the Thinking pane's,
and empties with it at every session boundary (`/clear`, `/new`, a `/sessions` resume, `/fork`).
_Avoid_: "injection" (the system-prompt fold the trailer replaced), "advice message" (a trailer on
the tool result, not a message of its own), "advice history" (nothing persists).
