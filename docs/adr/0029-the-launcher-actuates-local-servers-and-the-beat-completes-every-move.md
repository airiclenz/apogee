---
Status: accepted
---

# The launcher actuates local servers, and the beat completes every move

## Context

[ADR 0028](0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md) shipped
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
([ADR 0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md)
deleted it): apogee grows no process manager of its own. And the glossary already owns **Model
profile** — the *request-side* description of how a model speaks the wire — so the launcher's
launch-side "profile" needed a name that cannot collide.

## Decision

**1. The launcher actuates; the heartbeat observes; the beat completes every move.** Apogee
imports the launcher facade at the composition root only — the engine and `internal/tui` never
see the dependency, they see closures behind nil-degrading `tui.Options` seams, the ADR 0024
posture. An actuation (load a profile, unload, stop) is *fire-and-observe*: apogee asks the
launcher to change the world, then the existing heartbeat notices what the world became — a
profile load is completed by the next Beat binding what it finds, exactly the ADR 0028 shape,
one code path with the cold start and the `/server` switch. No actuation result is ever
trusted as a binding; only a Beat binds.

**2. The session follows the loaded profile.** `/load` activates a **Launch profile** (the
CONTEXT.md term this ADR introduces: launch-side — model file, server, flags — owned by the
launcher's config, opposite the request-side Model profile). If the profile resolves to the
session's current endpoint, nothing moves — the next beat observes the model change and
rebinds through the ordinary path. If it resolves elsewhere, the composition root performs the
same `SwitchUpstream` + Monitor-swap fold as `/server` (alias = the profile name) and the new
server's first beat completes it. Launch profiles therefore never ride in `servers:` — the
TODO's "how is a launcher-backed `servers:` entry marked" question dissolves; `servers:` stays
what ADR 0028 D5 made it, and `/server` keeps meaning "move among running endpoints" while
`/load` means "make the world serve this".

**3. Three idle-only verbs, acting on the session's endpoint.** `/load` (bare = a picker over
the launcher's profiles in the launcher's own order, favourites first — rows carry the
backend, the profile's merged `context_size` when configured, and a running attribution from
discovery; one argument = exact-match activation, the `/model`/`/server` grammar). `/unload`
frees the current server's model (on a managed llama.cpp server the launcher's semantic is a
full stop — the transcript note says so). `/stop` kills the server at the session's endpoint
(launcher ADR-0001: unconditional), after which the heartbeat's existing offline handling owns
the display. Neither `/unload` nor `/stop` takes a picker. All three are ordinary command-table
rows: idle-only, not `whileRunning`, one-line degrades when the integration is absent or the
endpoint is not one the launcher's config implies.

> **Amendment 2026-07-29 — the three verbs become one offered verb.** The owner's first live
> run against a single-model llama.cpp server found the surface wrong at its most-used point:
> `/model` opened a one-row picker (the model already bound) under a `⏎ switch` hint that
> switched nothing, while the list the human actually thinks in — the Launch profiles — sat
> behind a second verb. The two lists describe **one** world on this host, so asking "switch
> model" twice was the error. Revised: **`/load` is retired and its whole behaviour becomes
> `/model`'s** when the launcher seams are wired (picker, argument form, degrade ladder, the
> actuation latch), while a host without a launcher keeps the advertised offering unchanged;
> **whichever offering answers, the thing the session is already on is not offered**, which is
> what makes the hint honest and ends the one-row picker; and `/unload`/`/stop` keep their
> logic and their typed forms but go **hidden** — parsed, never listed — being rarely wanted
> and easy to hit by accident. Decisions 1, 2 and 4–7 are untouched: the same latch, the same
> folds, the same freshness rule, the verb renamed under them. Where this ADR says `/load`,
> read "`/model` on a host with a launcher". Recorded in
> `docs/plans/2026-07-29 - 00 - llama-launcher-integration-plan.md`, items 9–12.

**4. The launcher's config is the single profile store.** Apogee never defines, writes, or
caches launch profiles. One new **file-only** key, `llama-launcher:`: empty or absent means
auto-detect (`DefaultConfigPath()`, integration lights up only if that file exists — silently
absent in a container, where the launcher's MCP adapter as an ordinary `mcp-servers:` entry
remains the remote answer; the two compose); `off` disables; a path overrides. Every
user-facing operation does a fresh facade `LoadConfig` — never cached, never `Config.Reload`
(the facade documents it as CLI-flavoured) — so edits made in the launcher's own TUI are live
by the next `/load`. Config warnings arrive through the notice callback and land as transcript
notes. A configured-but-missing path degrades at use ("no launcher config at …"), not at
startup — the `servers:` posture of not probing reachability at load.

**5. One actuation latch, and beats in its shadow are ignored.** At most one actuation runs at
a time, TUI-owned: while it is in flight, sends and further actuations are refused with a
one-line note. The latch is simultaneously the per-address serialization the facade contract
demands — apogee holds it for the whole verb, so two lifecycle calls can never overlap. And in
its shadow the heartbeat's offline accounting is suspended: a failed beat during an actuation
counts nothing toward the offline crossing — the server is *expected* to be down mid-restart —
the same shape as ADR 0024 D7(a)'s "a failed beat while an Exchange is in flight is ignored".

**6. Load latency is narrated, not modal, and never cancelled mid-flight.** The blocking verb
runs in a background Cmd; each launcher progress step lands as one transcript note as it
happens, and the footer's model slot shows `loading <profile>…` until the post-load beat binds
(then ADR 0028 D6's announced seed prints `connected: …`). There is no mid-flight cancel — the
facade's own cancel is `Stop(addr)`, available the moment the verb returns. A health-wait
timeout is not a dead end: the launcher deliberately leaves the server running and names the
PID and log path; apogee prints that error plus the honest coda — the heartbeat will bind the
server if it comes up, which decision 1 makes true for free.

**7. Portability is the launcher's obligation, not every client's.** The launcher's v1.6.1
(shipped 2026-07-29; "compiles everywhere, actuates where it can" — its own ADR-0012, and the
fix stayed in the 1.6.x line) build-tags the platform-specific
seams: Linux becomes fully functional, Windows compiles with the HTTP-based verbs working and
the managed-fork/kill paths returning a clean unsupported error. Apogee therefore imports
unconditionally — **no build tags in apogee**, ADR 0020's cross-build stands untouched.
Module mechanics as settled 2026-07-28: dependency direction strictly apogee → llama-launcher;
`go.mod` requires a **tagged** release (≥ v1.6.1), never a `replace` to the sibling checkout;
local cross-repo dev via an untracked `go.work`.

## Consequences

- Apogee gains its first non-vendored owner-repo dependency; build-from-source from a bare
  clone keeps working because the requirement is a pushed tag (decision 7).
- The public config surface gains `llama-launcher:`; the command table gains `/load`,
  `/unload`, `/stop`; `tui.Options` gains the nil-degrading launcher seams. Minor bump.
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
  session starts at `endpoint:`, ADR 0028 D5); no log tailing inside apogee (unexported by the
  facade; the launcher TUI/CLI/MCP own it); no mid-flight cancel (decision 6); no launcher
  rows in `/server` (decision 2).
