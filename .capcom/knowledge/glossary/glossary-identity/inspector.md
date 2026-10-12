---
title: Inspector
---

**Inspector**:
The **Driver**-side view of what the loop actually put on the **Upstream** connection — the request
body a model call marshalled and the response payload it read back (`domain.WireEvent`) — opened in
the TUI with `/inspect` and held in a bounded ring beside the transcript, never in it. It is
**armed**: the engine captures nothing unless `ui.inspector` says so, read once at start-up
(ADR 0035). Each
captured half carries **two renderings** of the same payload, both computed when it is captured and
never on the paint path: the **readable** one the pane opens on — a request as the one line naming
its envelope (messages, tools, model), a response as the passages its deltas spell, the model's
thinking and its reply as wrapped prose and each tool call as a named row — and the pretty-printed
protocol that `ctrl+r` flips to and back. Which rendering is showing is Driver state, per pane and
never persisted. What the pane shows OF the ring follows the **Run view**: with one open it is the
viewed delegation's own wire stream and nothing else, named in the pane's title, and there is no key
for the scope — closing the view is what widens it back to the whole ring. The engine itself stays
wire-silent throughout: a WireEvent is data a Driver renders, not a surface the engine owns
(ADR 0031).
_Avoid_: "raw view" (raw is ONE of its two renderings and not the default one), "wire log" (a
bounded ring of the recent halves, not an appended record), "debug console" (it reads what was
captured and calls nothing).
