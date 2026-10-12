---
title: ADR 0055 considered options
---

- **Raw `endpoint:`/key fields in `schedules.yaml`** — rejected: it duplicates the key-source
  machinery, validation and migration story of the `servers:` list into a second hand-edited file,
  and puts secrets where none need to be.
- **Daemon-side actuation (load the named profile before firing)** — rejected: it swaps the single
  local slot out from under the interactive session, and it rebuilds the supply side of the slot
  broker inside the daemon — the exact split ADR 0034 decision 9 settled the other way.
- **Allow `model:` everywhere and let the server reject it** — rejected: single-model servers
  ignore or mangle an unknown model name inconsistently, so the misunderstanding would surface as
  a confusing Firing failure at 3am instead of a named validation error at edit time.
