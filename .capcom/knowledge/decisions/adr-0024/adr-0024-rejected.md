---
title: ADR 0024 considered options
---

- **Guard `cfg` with a mutex and rebind whenever the beat lands** — rejected. It would put a lock
  in the loop's hottest read paths (`budget()` runs per Mechanism call) to serve an event that
  happens at most once every ten seconds, and it would still not be *correct*: a model swapped
  mid-Turn would change the system prompt and the Mechanism set between two requests of the same
  Turn. The boundary discipline is both cheaper and stronger.
- **Cancel the in-flight Exchange when the model changes** — rejected by the owner's decision that
  a heartbeat never kills work in progress. A deferred apply costs at most one Exchange's worth of
  staleness and loses nothing.
- **Refuse a mid-Exchange change outright** (make the user re-trigger it) — rejected: the change
  is upstream reality, not a request; there is nothing for the human to re-trigger, and the
  transcript would carry a refusal for something that did happen.
- **A fixed `time.Ticker` independent of the beats** — rejected: a slow server would let a second
  beat start while the first was still running, which is exactly the overlap the landed-beat re-arm
  makes structurally impossible.
- **A `heartbeat-interval:` config key** — rejected with the other non-knobs. Ten seconds is the
  owner's decision; a field can be added later without changing anything decided here.
- **Refresh the display only, without rebinding the engine** — rejected as dishonest: the footer
  would name a model the requests are not using, and the Budget would keep measuring against a
  window that no longer exists. If the display follows the server, the bindings must follow it too.
- **Reuse `provider.ServerManager` for reachability** — rejected, and the type is deleted. Its
  liveness half is superseded by the heartbeat, which observes strictly more (the model and the
  window, not just "up"); its launch half belongs to the parked local-server start/stop work and
  should be rebuilt over `heartbeat.Beat` when that lands. Two reachability concepts would violate
  the sharp-nouns rule for no gain.
- **A `/health` endpoint check instead of `/v1/models`** — rejected: apogee speaks exactly three
  endpoints (`/v1/chat/completions`, `/v1/models`, `/props`), not every OpenAI-compatible server
  serves `/health`, and "answers with a usable model list" is the reachability that actually
  matters — a server that is up but serving nothing apogee can call is not reachable in any useful
  sense.
- **Keep synchronous startup discovery with a shorter timeout** — rejected: it trades one defect
  (a five-second stall) for another (a hard failure when the server is not up yet) and keeps the
  two code paths, seeded and switched, that the async design collapses into one.
- **Teach the renderer about the `context-window:` pin** so it can tell a real change from a
  suppressed one — rejected: it would push config knowledge into the thin renderer. Comparing
  against the last observation gets the same result with no knowledge at all.
- **Unbind the model when the server goes offline** — rejected. `Rebind` *binds*; an unreachable
  server is an observation to report, not a reason to tear a session's bindings down. The
  conversation, its history and its window survive an outage intact.
