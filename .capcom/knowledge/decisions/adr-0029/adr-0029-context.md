---
title: ADR 0029 context
---

ADR 0028 shipped
`/model` and `/server`, and its consequences named what remained of the TODO's "[P1] Server /
model switching": `/server` moves between servers that are **already running** — nothing in
apogee starts one, loads a model into one, or frees one. The machinery that does exists as the
owner's **llama-launcher** (`github.com/airiclenz/llama-launcher`): three backend protocols
(llama.cpp, Ollama, LM Studio) behind its `LLMServer` interface, a profile store llama.cpp
itself lacks, and the lifecycle orchestration (idempotent activation, drift notices, stop
escalation) that took that repo eleven ADRs to get right. The owner decided 2026-07-28: one
code home — apogee imports the launcher as a **library**; porting is rejected; MCP is not the
primary integration.

By the time this ADR's grill ran (2026-07-29), the launcher side had already moved: its
**v1.6.0** shipped a curated public facade (`launcher/`, its ADR-0011, apogee named the first
client) — `LoadConfig`, `DefaultConfigPath`, `DiscoverRunningInstances`, `LoadProfile`, `Stop`,
`Unload`, the types, two `errors.Is`-crossing sentinels, and a documented contract: verbs
block (activation up to ~30 s health wait plus ~20 s stop escalation; model loads up to 5 min),
progress and notices are synchronous callbacks, read verbs are concurrency-safe, lifecycle
verbs are serialized per address **by the caller**, and one `Config` per process (last
`LoadConfig`'s API keys win). Deliberately excluded: `TailLog`, `StartServer`/`EnsureServer`,
live-params query, PID/log detail. What v1.6.0 did *not* cover is portability: the facade
drags `internal/launcher` in, and that package compiled darwin-only (a darwin `syscall.Select`
in its TUI poll; unix `Setsid`/`syscall.Kill` in process control) — while apogee's `make
check` runs on Linux and ADR 0020 keeps the Windows cross-build green.

Two prior decisions bound this design. The dead `provider.ServerManager` stays dead
(ADR 0024
deleted it): apogee grows no process manager of its own. And the glossary already owns **Model
profile** — the *request-side* description of how a model speaks the wire — so the launcher's
launch-side "profile" needed a name that cannot collide.
