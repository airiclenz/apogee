---
title: ADR 0104 considered options
---

- **Pin the whole reaction entry.** Simpler, but a timeout or `on:` edit would re-ask, and ADR 0076
  decision 10 already named those fields parameters.
- **Pin the YAML as written.** A slice of the file cannot pin some fields and not others, and a
  reformat would re-ask for nothing.
- **Repo overrides the global entry by name** (decision 10 as first written). A repo could replace a
  server the user configured, and the user's existing Allow rule for that name would approve it.
- **Keep refusing in-repo programs.** It leaves `sh scripts/x.sh` open anyway and makes repo
  reactions near-useless; pinning the file closes the gap instead.
- **Allow in-repo programs, pin the text only.** A script edit — a teammate's or the model's — would
  run without asking.
- **Inherit the whole environment, warn on the pane.** A repo-chosen command does not earn the
  trust the global default rests on.
- **Stricter trust for adopted repo entries** (keep the resolved-IP floor, forbid `headers-env`).
  It blocks the common case of a team server behind an API key, and the pane already shows the
  endpoint and the variable names.
- **Repo reactions in the TUI only.** Headless and the daemon would then run a different set from
  the TUI for the same workspace (ADR 0031).
