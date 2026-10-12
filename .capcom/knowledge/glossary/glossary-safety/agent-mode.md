---
title: Agent mode
---

**Agent mode**:
The autonomy level governing which tool calls need human approval — a **monotonic
privilege ladder**. Four:
- **Plan** — read-only — including the hardened git read tools (`git_status`, `git_log`,
  `git_diff_range`, `git_show`), which are read-only by construction and so take the read-only row in every
  mode (the **RO-subproc** class, ADR 0012 amendment 2026-09-06) — **plus its own Scratch dir**:
  Apogee's own writers run there unprompted and every other target is refused with a reason
  naming the dir (ADR 0012's second loosen, 2026-09-14); no other writes, and no other command
  execution bar a Recipe's script stage — the engine's own call, confined so it writes only its own
  workflow folder (ADR 0012 amendment 2026-09-27) — (explore, propose, draft into the scratch dir,
  touch nothing else).
- **Ask-Before** — workspace reads run free, the hardened git reads above included, and so do
  Apogee's own writes into the session **Scratch dir** (the one write it does not gate); every
  other write, every other command, and every external reach requires an Approval (the human is
  the gate).
- **Allow-Edits** — Apogee's own **workspace-scoped edits** (path-safety-bounded) run
  without asking; shell/exec other than those reads, network, MCP, and anything
  out-of-workspace still gate.
  Needs **no Confinement** — path-safety bounds the auto-approved writes and the human
  backstops the unbounded surface, so it is **identical on every OS**.
- **Auto** — adds the unbounded **shell/subprocess** surface to the auto-approved set, so it
  is the one **unsupervised** mode. Its blast radius is tuned by the global
  **`confine-to-workspace`** flag (ADR 0012): **on** (default) OS-**Confines** the subprocess
  surface to the workspace with the **network open**, and still gates **MCP**; **off** ("I am
  the sandbox") runs unconfined — safe only inside a VM. Apogee's own network tools
  (`web_fetch`/`http_request`) auto-run url-filtered in both (they no longer gate in Auto —
  ADR 0012 reversed ADR 0004 here).
_Avoid_: "permission level", "trust mode".
