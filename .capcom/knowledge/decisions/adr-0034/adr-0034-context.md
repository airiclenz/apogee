---
title: ADR 0034 context
---

ADR 0031
anticipates a platform daemon but defers its packaging; ADR 0033
built the libraries it composes (`internal/run`, `internal/schedule`), scoped TUI Schedules to
die with the TUI, and named durable schedules as "the future daemon's value-add over this same
library". A 2026-08-05 grill settled the daemon's shape — and, because a second apogee process
makes cross-process contention on a single-slot local server real for the first time, the shape
of the **slot broker** that resolves it.

One force redirected the broker's supply side out of this repo: llama-launcher's identity is a
**process manager, not a request router** (its ADR-0002 — one-shot, zero resident memory, no
proxying), so slot brokerage cannot land in the launcher CLI. Its repo already holds the
template for the exception: the MCP adapter (its ADR-0008), an optional resident *sibling
binary* beside an untouched CLI. The supply side follows that template as an optional
**Gateway** sibling binary, recorded in llama-launcher's ADR-0013; this record covers apogee's
side and cross-references it.
