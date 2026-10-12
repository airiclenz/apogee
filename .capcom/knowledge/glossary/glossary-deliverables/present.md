---
title: "Present / Presentation: surfacing a finished document to the user"
aliases: [presentation]
---

**Present / Presentation**:
The act of **surfacing a finished document to the user** — a deliverable file (a report, a
review, a plan) shown at the end of the work that produced it, instead of left on disk where a
one-line `write_file` card is the only trace of it. The model reaches it through the
**`present_document`** tool and supplies nothing but a path (and an optional title): the **host**
decides the mechanism (see presentation ladder), so the model
never reasons about platforms. Like Ask-user the tool is
**mode-independent**, `ReadOnly` (it runs even in Plan), and **not** a safety gate. A
presentation never fails the call — the baseline rung already happened — so the tool result names
the outcome (`opened` / `served` / `shown`) for the model to relay truthfully. See
ADR 0019.
_Avoid_: "open the document" (opening is *one rung* of the ladder and always the host's act — a
remote session never opens anything), "export", "publish", "render" (nothing is converted).
