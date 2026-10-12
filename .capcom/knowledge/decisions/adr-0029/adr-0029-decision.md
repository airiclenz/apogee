---
title: ADR 0029 decision
---

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

> **Amendment 2026-08-02 — the composition root RESOLVES the move; the completion fold COMMITS
> it.** As first built, the `LoadProfile` seam performed the move itself, which put
> `SwitchUpstream` + Monitor-swap on the actuation Cmd goroutine — where it ran unsynchronized
> against a heartbeat-driven `Agent.Rebind` on the Update goroutine, because the latch of decision
> 5 gates lifecycle calls and sends but not observed-binding rebinds. On a minutes-long load of a
> large model that window is wide, and any server that swaps its model underneath the session
> (Ollama on demand, a second client, an LM Studio action) can land a rebind inside it. Revised:
> the seam resolves the move — endpoint, alias, key — and hands it back as ONE call in
> `tui.ProfileLoadResult.Move`, which the actuation completion fold invokes on the Update
> goroutine; the same closure, the same three steps, the same `/server` fold afterwards, moved to
> the boundary that orders it. Decision 1 is untouched — still only a Beat binds — and decision 5
> gains its missing half: a binding observed while the latch is held is STASHED and applied at the
> completion, the posture a change observed mid-Exchange already had. Found by the 2026-08-01 code
> audit; recorded in `docs/plans/2026-08-01 - 01 - engine-tui-correctness-wave-plan.md`, item 5.

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

> **Amendment 2026-07-29 (second) — the two hidden verbs are renamed, and offered again.** The
> live pass that followed the amendment above found the hiding aimed at the wrong target. The
> hazard was never the verbs: it was the **silence of their names**. `/stop` names no object, so
> its reflex reading is "stop the running turn" — which is Esc — and `/unload` names none either,
> which is why an accidental keystroke was a plausible way to lose a server. Name them for what
> they act on and the ambiguity leaves with the hazard, while a verb the human cannot discover
> stays a verb they will not find. Revised: `/unload` becomes **`/unload-model`** and `/stop`
> becomes **`/stop-server`**; both are **offered** in the `/` dropdown again, in their
> alphabetical slots, so the `hidden` posture the amendment above introduced is withdrawn (and
> the `commandSpec` flag that carried it is removed — an unused flag in the registry is a claim
> about the surface the surface no longer makes). The old spellings are **removed outright, not
> aliased**: an alias would put the ambiguous names back in the parser, which is the one place
> this change exists to remove them from, and pre-production (AGENTS.md) is why no deprecation
> shim is owed. The verbs' logic, their latch, their folds and their degrades are untouched —
> only the spelling, which the footer and the latch refusal now echo back (`stop-server…`,
> `unload-model in flight`). The first amendment is left standing as written: it is the record of
> why hiding was tried, which is the reason this one exists. Recorded in
> `docs/plans/2026-07-29 - 00 - llama-launcher-integration-plan.md`, items 13–14.

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

> **Amendment 2026-08-07 — the key moves onto the `servers:` entry, and the global one is
> refused.** A global launcher key makes a claim about the whole config that stops being true the
> moment the list holds more than one server: a machine with llama-launcher installed captured
> `/model` on **every** entry, so a session moved onto a remote provider — an OpenRouter, a rented
> box — was offered this machine's Launch profiles instead of the hundreds of models that server
> advertises, and ADR 0028's advertised-model discovery was unreachable for as long as the key was
> set. The enablement belongs where the launcher's reach ends: on the entry it fronts. Revised by
> the owner's four calls of 2026-08-07. **(1)** Each `servers:` entry may carry its own
> `llama-launcher:` field, and `/model` offers Launch profiles only while the session is on that
> entry; address-matching auto-detection was rejected. **(2)** The field has three shapes — absent
> (the launcher is off for that server: the default, and what every remote entry wants), `auto`
> (read `DefaultConfigPath()`), or a path — and the top-level key retires from the schema together
> with its `/settings` registry row and live-apply case (ADR 0037).
> **(3)** A config still carrying the top-level key **fails startup** with a ready-to-paste
> per-entry example rather than migrating itself — ADR 0036's
> refusal-over-silence, and pre-production (AGENTS.md) is why no shim is owed. **(4)** On a server
> with no launcher `/model` never offers profiles, and coming home is two steps: `/server <name>`,
> then `/model`.
>
> Two rules of this decision change with it. **Auto-detect by existence is superseded by an
> explicit `auto`**: the old empty key stat-gated `DefaultConfigPath()` precisely because it lit up
> silently, while `auto` is written by hand, so a missing config now degrades at the first verb
> naming the path — the posture the named-path case already had. And **enablement follows the
> session, not the process**: startup entry selection, a `/server` switch and a bind each install
> the entry's resolved path, while a Launch-profile load that MOVES the session (possibly to an
> endpoint no entry names) leaves it untouched, so the integration stays on for the session that
> just used it; the `--endpoint` override entry (ADR 0036 D6) carries no launcher key, so the
> integration is off there. Everything else here stands: the launcher's config is still the single
> profile store, apogee still never defines, writes or caches profiles, every user-facing operation
> still does a fresh `LoadConfig`, and the container case is unchanged — no entry carries the key
> there, and the launcher's MCP adapter as an ordinary `mcp-servers:` entry remains the remote
> answer; the two still compose. Recorded in
> `docs/plans/2026-08-07 - 00 - per-server-llama-launcher-plan.md`.

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
