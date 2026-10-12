---
title: ADR 0029 consequences
---

- Apogee gains its first non-vendored owner-repo dependency; build-from-source from a bare
  clone keeps working because the requirement is a pushed tag (decision 7).
- The public config surface gains `llama-launcher:`; the command table gains `/load`,
  `/unload`, `/stop`; `tui.Options` gains the nil-degrading launcher seams. Minor bump.
  (Amended 2026-09-30: per the amendments above, `/load` folded into `/model`, the other two are
  `/unload-model` and `/stop-server` (`internal/tui/command.go`), and `llama-launcher:` lives on
  the `servers:` entry, not at the top level.)
- CI covers the seams with a fake launcher behind the closures; the end-to-end pass (real
  launcher, real llama.cpp) is owner-run on a same-machine host — recorded in the integration
  plan, the `APOGEE_LIVE_ENDPOINT` convention's sibling.
- The launcher-side prerequisite recorded in TODO.md (export a public API) shipped as v1.6.0
  before this ADR; the v1.6.1 portability release this ADR additionally requires of that repo
  shipped the same day (2026-07-29, tagged and pushed), so nothing here is outstanding.
- CONTEXT.md gains **Launch profile**; "profile" unqualified is now an _Avoid_ in docs. The
  model-profile abstraction (request-side, still unstarted) is untouched by all of this and
  remains the [P1] entry's live half.
- Deliberate non-goals, so they are not re-opened as gaps: no auto-`/load` at startup (the
  session starts on the `servers:` entry `server:` names — ADR 0028 D5 as ADR 0036 D2 restates
  it); no log tailing inside apogee (unexported by the
  facade; the launcher TUI/CLI/MCP own it); no mid-flight cancel (decision 6); no launcher
  rows in `/server` (decision 2).
