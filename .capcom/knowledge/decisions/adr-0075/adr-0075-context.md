---
title: ADR 0075 context
---

`apogee headless` prints prose. Its answer is one `Fprintln` of `run.Result.FinalText`
(`cmd/apogee/headless.go:642`) and everything around it — written files, the undo verb, per-sub-agent
fills, usage, the one-line summary — is a post-hoc block composed from the same `run.Result`
(`headless.go:635-677`). The only live consumer of the engine's Event stream is `pruneNoticeSink`
(`headless.go:124-140`), which prints one stderr line per `PruneEvent` and forwards the rest. Nothing
a program can parse comes out of a run, so the only machine-readable signal today is the exit code
and a `· faulted` suffix the manual offers as a grep target (`docs/manual/headless.md:100-102`).

The contender gap assessment of 2026-09-06 ranked headless's plain-text-only output gap 3 of ten,
behind persistent undo and the tool-call salvage guard — both since shipped — and the owner accepted
it on 2026-09-07 as bead `apogee-tvw`. Every rival has closed it: opencode's TUI is a client of
`opencode serve` (OpenAPI 3.1 + SSE), pi ships `--mode json` / `--mode rpc`, qwen-code runs a daemon
over HTTP+SSE. The gap is ADR 0031's thesis cashed in — an engine sufficient for any Driver is worth
little while no program outside the repo can observe a run — and closing it is what an IDE extension
would later build on.

The door is already open. ADR 0031
made the engine sufficient for any **Driver**, and `domain.EventSink` (`internal/domain/events.go:18`)
is a sealed, additively-versioned, one-way stream of 18 variants that every Driver already reads. What
was missing was not a seam but a *contract*: a documented, versioned rendering of that stream a
program outside the repo can depend on.

ADR 0073 shipped a JSON
projection of the same events days earlier — `hooks.Payload` — but a deliberately lossy one: five
moments, flat, one synthesized (`file-changed` correlated a write call with its result), one
depth-filtered (`turn-finished`, `internal/reactions/match.go:68-70`), one phase-dropped
(`approval-waiting`). It answers "tell my script when something happened". It cannot answer "let a
program observe this run". (Re-checked 2026-10-02 against `internal/reactions`, the package ADR
0073's Hooks were renamed to: `file-changed` now reads the engine-stamped
`ToolResultEvent.WriteTarget` instead of correlating (`match.go:162-174`), and ADR 0076 A6 made both
approval phases events (`match.go:99-124`).)

The owner settled the scope on 2026-09-07: **JSONL first, `serve` later**. `apogee serve` as a fourth
wire-facing Driver is out of scope here and is dependency-blocked on this bead (`apogee-afu`), so the
event contract proves itself on a real consumer before an HTTP surface is designed around it.
