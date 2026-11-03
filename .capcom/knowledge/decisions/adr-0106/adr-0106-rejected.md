---
title: ADR 0106 considered options
---

- **Wrap the query and the shapes in tmux passthrough.** tmux would forward them to the outer
  terminal, whose pointer is shared by every pane, so a shape set for apogee's pane would leak
  into its neighbours and stay there after apogee lost focus. Under tmux the feature stays off.
- **Always-on all-motion tracking.** It would send a report for every cell the pointer crosses,
  on every terminal, for a feature most terminals cannot show and the setting leaves off by
  default. All-motion is on only while the gate is open.
- **A timer backstop on the query.** A slow box or a slow ssh hop can answer late, and a deadline
  would turn that into a wrong verdict (the slow-box rule in `AGENTS.md`). The DA1 sentinel gives
  a definite "no" from any terminal that answers DA1. A terminal that answers nothing just keeps
  the feature off, which costs nothing.
- **Probe while the setting is off.** It would put a query on the wire for every user, for a
  feature they did not ask for. The query goes out only once the setting is on.
- **Use X11 cursor names.** xterm speaks X11 names, but the kitty specification, which defines
  the query this gate relies on, speaks CSS names. One vocabulary keeps the gate's query
  and the shapes it writes the same three names.
- **Treat a partial answer as support.** A terminal answering `1,0,1` cannot show the I-beam, so
  the prompt and the transcript would show the wrong shape. The gate takes `1,1,1` only.
