---
title: Background workflow
---

**Background workflow**:
A **Workflow** that runs while the conversation goes on — launched by the user (a **Recipe**'s
skill run in the background) or by the top-level model through `fan_out`'s background switch or
`sub_agent`'s, which model classes see only where the bench has shown it helps. A background
`sub_agent` is one delegation run as a one-item workflow: its child works exactly as a blocking
`sub_agent` child does, the call is answered at once, and the child's report comes back in the
finish note, under the blocking result's cap and with its transcript path; a named one is retained
on finish as a **Retained delegation**, and continuing it runs blocking
(ADR 0094, 2026-10-05).
It keeps one server slot free
for the conversation; a second one on the same server waits in line. When it finishes, a note —
one line, or a background `sub_agent`'s report — reaches the parent — at the next pause between tool calls if a reply is under way, otherwise
by apogee **waking** the agent with a reply of its own, bounded by the **Mode** as any reply is.
Quitting apogee stops it; resuming the session resumes it. While it is still driving its folder, a
blocking launch of the same workflow — a `fan_out`, a typed `/<id>` or a foreground recipe — is
refused rather than run beside it (2026-09-30). Ratified 2026-09-27
(ADR 0089; ADR 0087).
_Avoid_: "async delegation" (ADR 0086's rejected option A — a spawn/message/wait tool family; a
background `sub_agent` adds a switch to the one tool, not that family), "detached run".
