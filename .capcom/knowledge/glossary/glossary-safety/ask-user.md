---
title: "Ask-user: a free-text question the model puts to the human mid-task"
---

**Ask-user**:
A free-text question the model puts to the human mid-task (via the `ask_user` tool), answered
through a host-supplied **`Asker`** delegate — the public analogue of the **Approver**, but
**not** a safety gate: it carries no allow/deny semantics, never bypasses the disposition, and
is `ReadOnly` (it runs even in Plan, mode-independent). A `nil` Asker means the tool is simply
not registered; a headless host must supply an Asker that **fails safe** (no hang). The TUI
implements it as an input-prompt rendezvous (the free-text sibling of the Approval prompt); the
bench as a scripted responder. Added in P3.11. An optional **`multi_select`** flag on the request
opts one question into *several-of-the-above* answering: the Driver lets the human tick any number
of the offered choices — the TUI draws a `[✔]`/`[ ]` box on each row (`[x]` under the `safe` glyph
set; the transcript record always spells `[✔]`), `␣` toggles the highlighted
one and `⏎` sends — and the reply carries **every ticked label on its own line** inside the single
`AskAnswer.Text`, in the order the choices were offered (labels, never indices). The flag is
additive and **off by default**: absent or false is the single-select question that was always
there, byte-identical on the wire and on the screen, and neither the `Asker` interface nor
`AskAnswer` changes shape for it.
_Avoid_: conflating it with Approval — an answer is not a permission.
