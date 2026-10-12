---
title: "Prompt recall: the per-workspace list of sent inputs the prompt box walks with up and down"
---

**Prompt recall**:
The per-workspace list of inputs the human has already **sent** from the prompt box, and the walk
through it the box offers on **↑/↓** — a terminal's own gesture, brought to the box. ↑ on an
**empty** box loads the newest entry with the caret at its end; further ↑ steps older and stops at
the oldest; ↓ steps newer; ↓ one past the newest empties the box again, so the walk is always
reversible. Recall owns the arrows only while the box holds a **freshly recalled entry the human
has taken no other action in** — typing, editing, pasting, or a click in the box ends recall mode
and hands ↑/↓ back to the caret, and a recalled `/command` deliberately does **not** open the
suggestion pane, which would claim those arrows first. It is live where the box is the human's own:
at idle, and while the agent runs (where ⏎ stages an Interjection). It is
**not** live under an Ask-user prompt — there ↑/↓ move the choice
highlight. What is recorded is what was *sent*: ordinary messages, whole-line `/command`
invocations, and Interjections — **not** Ask answers, which answer the model's question rather than
speak the human's own input, and **not** the session-reset pair `/new`/`/clear`, which is
deliberately never recorded so a walk cannot hand back a line whose ⏎ wipes the session. Storage is one JSONL file per workspace under the config home
(`~/.apogee/prompts/<digest-of-workspace-path>.jsonl`, `internal/recall`) — nothing is written into
the project tree; consecutive duplicates collapse, and a start-up load hands back the newest 1000.
It is **driver-side state only**
(ADR 0031):
the engine never sees it, and a Driver that wires no recall seam has the feature simply off.
_Avoid_: "prompt history" / "history" (that word belongs to the Session
browser's list of records — see its own Avoid line), "command history" (`/command` lines are one of
the three things recorded, not the point), "the buffer" (the box's live text is a draft; recall is
what has already left it).
