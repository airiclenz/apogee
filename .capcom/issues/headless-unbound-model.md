---
title: Headless and daemon Firings never bind the served model when no model is configured
status: active
type: bug
priority: 1
labels: [driver-parity]
plan: 2026-11-02 - 02 - turn-total-and-open-bugs-plan
created: 2026-11-02T06:57:58Z
---

On a `servers:` entry with no `model:` (and no --model / APOGEE_MODEL), `apogee headless "…"` exits 2 in about 1 ms with:

```
apogee: submit the firing's prompt: apogee: no model is bound — Rebind to the model the Upstream serves before submitting
```

`bindFiringConfig` (cmd/apogee/wire_firing.go) takes `in.model` else `in.entry.Model`, and nothing after it binds the model the beat in `raise` observed; `run_started` reports `"model":""`. The TUI binds the served model through the heartbeat's Rebind, so the same config works in a session. `apogee --help` promises the opposite: "With no model set anywhere, apogee asks the server for its active model". Reproduced 2026-11-02 on v0.29.1+4788.g2417e850b397 against Splash 1.5 (single model, `GET /v1/models` answers it and `apogee probe` resolves it as active). With `--model` set the same run works.

## Why

A single-model local server (llama.cpp, Splash) needs no model named, by the help's own contract; unattended Drivers break on a config the TUI accepts (ADR 0031 Driver parity).

## Acceptance

- With an entry that names no model, `apogee headless` binds the active model the composition's beat observed and runs; `run_started.model` names it
- A daemon Firing on the same entry does the same
- A beat that observed no model still refuses with a clear sentence (exit 2), not the Rebind error
- Test covers an unpinned entry against a fake single-model server
