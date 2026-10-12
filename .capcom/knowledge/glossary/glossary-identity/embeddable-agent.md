---
title: "Embeddable agent (the public API): the Go package that runs an apogee agent in-process"
---

**Embeddable agent** (the public API):
The public Go package other applications import to construct and run an Apogee agent
in-process. Apogee ships as **both** a ready-to-use terminal tool (the `cmd/apogee` TUI +
CLI — the headline product) **and** this reusable library: the TUI, `apogee daemon`, the `apogee headless`
CLI (one prompt run unattended over the shared core, beside
`apogee probe` on the subcommand surface), and the bench are all consumers of one
public package over the same engine. The repo is the whole tool, not just the library. The public surface is guarded
and versioned; everything else lives in `internal/`. See
ADR 0001.
_Avoid_: "**Apogee Core**" (retired — it named the proxy-era transform engine, a
different thing; do not resurrect the name for the new library), "the SDK".
