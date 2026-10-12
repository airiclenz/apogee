---
title: "Thinking pane: the Driver-side plain view of the Thinking channel"
---

**Thinking pane**:
The **Driver**-side *plain* view of the Thinking channel — the reasoning a
model emits beside its answer (`domain.ReasoningEvent`) — opened in the TUI with `/thinking` and
held in a bounded per-agent **board** beside the transcript, never in it. Unlike the
Inspector it is **unconditional**: no config key arms it, because the board
never touches the transcript, the model's context or the Upstream connection. One record per
completed **Turn**, newest last, each under one heading naming the Turn and — for a delegation —
the run; the pane opens on the newest and paints the text as plain wrapped prose, with ONE
rendering, no protocol and no prefixes (the wire, and its raw bytes, are the Inspector's). What it
shows follows the **Run view** exactly as the Inspector's ring does: with a view open it is that
delegation's thinking alone, named in the pane's title, and at the top level the main agent's
alone. It is **Driver state**, bounded (a record's last 64 KB; the board's 64 most recent records,
oldest dropped) and never persisted — it empties at every session boundary (`/clear`, `/new`, a
`/sessions` resume, `/fork`), so a new or resumed session opens with an empty board, while the
canonical reasoning of each committed Turn stays where it always was, on the engine's own message.
_Avoid_: "reasoning pane" (the channel is the **Thinking channel** and `/thinking` is the verb),
"thinking transcript" (a board beside the conversation, never an entry in it), "reasoning tail"
(the retired seam this replaced).
