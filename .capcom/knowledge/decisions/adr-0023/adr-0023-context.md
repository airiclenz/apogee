---
title: ADR 0023 context
---

Apogee had **no built-in system prompt**. `buildRequest` projected `a.conv.Messages()` onto the
wire unchanged, so a conversation began at the user's first message, and the only text that ever
reached the system channel was engine-owned: the non-native profile's tool menu and
format-emission instructions, folded in at the wire seam
(ADR 0010's `processing` boundary), and
the catalogued nudge Mechanisms' directives, appended through `Request.AppendToSystem`
(ADR 0003).

That emptiness was a **scope guard, not an oversight**. The prompt-seam plan
(`docs/plans/archived/prompt-seam-wiring-plan.md`) shipped only the narrow profile-driven block and
deliberately parked the general template story in `TODO.md`, behind one hard anchor: a zero/native
profile must add **zero bytes** to the request, pinned by
`TestPromptSeam_NativeProfileByteIdentical`. The parked note also carried a design obligation — the
host-override knob for the rendered instruction block (the seam plan's *rejected hybrid*) must be
designed *together with* the general template so the two compose rather than fight.

What forces it now is the user-facing gap the guard was always going to leave. A user cannot tell
apogee anything standing — who it is, what this project's conventions are, that it is talking to a
person who wants terse answers — without retyping it into the first message of every session. The
apogee-code oracle assembled exactly this: a persona preamble plus `{{workspace}}` /
`{{datetime}}` / an agent-mode directive *around* the tools block. And the audience makes the frame
worth more, not less: a ~4B–35B local model reasons better with a short standing statement of who
it is, where it is, and what it is allowed to do — while every system token it spends competes with
the task, which is why the shipped default is five lines and not fifty.

This ADR records the design the owner ratified in the 2026-07-26 grill, implemented by
`docs/plans/2026-07-26 - 02 - configurable-system-prompt-plan.md`. It **supersedes nothing** and
closes the parked story.
