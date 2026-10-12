---
title: ADR 0059 bounds (stated, not separately ratified)
---

- `console_open` takes a command line run under the platform shell inside a pseudo-terminal,
  with `terminal`'s environment scrub and PATH scoping (`subprocessEnvScopedPath`) and the
  `argv[0]`-inside-the-box refusal; **no** fail-fast preamble (the shell is interactive).
- Output is a bounded ring buffer per Console; `console_read` returns the bytes since the last
  read, capped like `terminal`'s output, plus whether the process is still alive and its exit
  code once it is not. Sizes and the settle/wait parameters are plan-level.
- Windows: ConPTY creates the process itself and cannot take the restricted token, so the
  contract's "confine if you can, gate if you can't" row makes `console_open` gate in Auto there
  — a consequence of the existing table, not a new cell. The Job Object container (§2.4) still
  applies. *(Superseded by the "a Windows Console runs confined" Amendment (2026-10-04) below:
  the launch now takes the restricted token.)*
  *Amended 2026-10-03:* the ConPTY backend exists — on Windows a Console runs `cmd /c` under a
  pseudoconsole, and a non-raw `console_send` ends its input with `\r`, the Enter key there. Auto
  still fails closed: a confined open reports `ErrConfinementUnavailable` and is demoted to
  Approval. Because no Windows Console is ever fenced, **each** `console_send` made under a
  confinement box to a Console that was not opened confined also reports
  `ErrConfinementUnavailable` and is demoted to Approval on its own. This supersedes, for Windows
  only, the assumption behind §2 and §4 that the fence set when `console_open` opened a Console is
  what binds every later send to it. POSIX is unchanged: a Console opened unconfined (in
  Ask-Before) and sent to after a switch to Auto still takes the send — that pre-existing gap is
  filed as its own follow-up bead. *(Superseded for POSIX by the 2026-10-04 Amendment below, and
  for Windows by the "a Windows Console runs confined" Amendment (2026-10-04) below.)*
- The bench needs no stub: a Console is not an `ExternalEffectTool`; a fork simply inherits none.
