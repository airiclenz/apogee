---
title: "`confine-to-workspace` (the Auto blast-radius flag): the global key that tunes Auto's blast radius"
---

**`confine-to-workspace`** (the Auto blast-radius flag):
A global-config key (`~/.apogee/config.yaml`, default **`true`**) that tunes **Auto**'s blast radius
(ADR 0012); meaningful only in Auto. **`true`** fences filesystem writes to the workspace —
a subprocess escape is **OS-blocked**, an Apogee in-process out-of-workspace write raises
**Approval** — with the network open and MCP gated. **`false`** ("I am the sandbox") runs Auto
unconfined, safe **only inside a VM** (the user's responsibility); it is **global-only** (a project
config cannot loosen it — the hostile-repo footgun is closed) and prints a per-session warning. The
**only blanket *loosen*** in the system — every other knob (the dangerous-action guard, the deferred
tool×mode matrix) is tighten-only — and the **Host acknowledgement** (below) is that same loosen
scoped to one machine, not a second kind of it.
_Avoid_: "YOLO mode" (informal; it is a flag on Auto, not a fifth mode),
"`--dangerously-skip-permissions`" (names Claude Code's analogue, not this flag).
