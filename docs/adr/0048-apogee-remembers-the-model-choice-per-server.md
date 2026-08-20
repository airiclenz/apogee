---
Status: accepted
---

# apogee remembers the model choice per server

## Context

The server a session starts on is remembered — every `/server` switch splice-writes `server: <name>`
so the next launch comes up where the last one ended
([ADR 0036](0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md)
decision 2). The **model** running on that server was not. On the machines this project targets that
is the more expensive half of the pair: a workstation with one GPU serves one small model at a time,
the human picks it deliberately, and every restart asked them to pick it again — a `/model` overlay on
a plain multi-model server, a Launch profile load and a thirty-second health wait on a
launcher-fronted one. The choice was already expressed at the keyboard; nothing in the system wrote
it down.

The two server classes keep that choice in different vocabularies, which is why one key could not
serve both. A **plain** multi-model server (llama.cpp with several models, LM Studio, a remote
provider) holds many models at once and the choice is a **wire model id** — and the entry already has
a home for it: `model:` is read at start-up and bound, as a HINT rather than a pin
([ADR 0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md) decision 6),
which is exactly the strength a remembered choice wants. A **launcher-fronted** entry (one carrying
`llama-launcher:`) holds one model at a time and the choice is a **Launch profile** — a name in
llama-launcher's own config, a launch-side recipe rather than a wire id, and on that class `model:`
is a deliberately empty discovery hint. Nothing in `servers:` could name a profile.

The tempting shortcut — write the choice into the launcher's config, where the profiles already live
— is closed by [ADR 0029](0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md)
decision 4: the launcher's config is the single profile store and apogee never defines, writes or
caches a profile in it. That rule is not merely a layering preference. The launcher's YAML is a
**library of presets** a human curates and copies between machines; which one *this* apogee session
happened to load last is **session state** belonging to one client of several. Merging them makes one
file mean two things, and the first casualty is the human's own recipe list.

## Decision

**apogee records the model choice into apogee's OWN config, on the `servers:` entry the choice was
made on — the wire model id into a plain entry's existing `model:` key, the Launch profile name into a
new per-entry `launch-profile:` pointer on a launcher-fronted one — gated by a single top-level
`remember-model` toggle that is OFF by default, and restores the profile at the next interactive
start-up through the existing actuation latch, yielding to anything already serving.**

**1 — One toggle, off by default, gating both halves.** `remember-model:` is a file-only depth-1 bool
(`*bool`, nil = off) with a key-registry row and a `/settings` row, live-applied through the session's
settings holder ([ADR 0037](0037-every-settings-edit-applies-to-the-running-session.md)) so a flip
governs the very next pick and the next boot without a restart. Off is the default because the feature
**writes into a hand-edited file**: recording is the deliberate-edit grain of
[ADR 0035](0035-the-settings-surface-persists-one-key-per-deliberate-edit.md) applied to a choice, and
a user who has not asked for it gets no writes — and, at start-up, no launcher I/O at all.

**2 — Two server classes, two keys, never both on one entry.** A plain entry remembers in `model:`,
whose start-up restore already existed. A launcher-fronted entry remembers in `launch-profile:`, and
**never** gets `model:` written: recording a wire id beside a profile would leave a second, staler
answer to "what runs here", pinned by apogee against a launcher that can change it at any moment. A
`launch-profile:` on an entry with no `llama-launcher:` key is a start-up refusal from
`ValidateServers` — a pointer with nothing to actuate it — as is a whitespace-only value. Whether the
named profile still **exists** is deliberately not validated: the launcher's config is read fresh at
use time (ADR 0029 decision 4), so the only honest moment to notice its absence is the boot restore,
which says so rather than refusing the file.

**3 — Only an explicit choice records.** The write hangs off the act: a `/model` pick that BOUND (the
overlay accept and the `/model <id>` argument path reach the same bind), and a Launch profile load that
COMMITTED (the same-server completion, and the move the session had to follow). A rebind the heartbeat
merely observed is news about the server rather than a choice; `--model` / `APOGEE_MODEL` are facts
about one invocation; a failed load, a timed-out health wait and an unfollowable move record nothing;
`/unload-model` and `/stop-server` leave the pointer alone, because freeing the GPU now is not
forgetting which model this server runs. The recording inherits ADR 0036 decision 2's posture whole:
it is **a recording, not a save prompt**, best-effort, and a write that could not land is a transcript
note under the act rather than an undo of it — the session is on the model either way.

**4 — The pointer's home is the ACTUATING entry.** For a profile load, the entry written is the one
whose `llama-launcher:` key the session's launcher path currently follows — not whatever endpoint the
session ends up bound to. A load that moves the session to an address `servers:` does not name still
records, onto the entry that can act on the launcher next time; that is ADR 0036 decision 2's "an
unlisted endpoint has no name to write" read one level up — the *launcher* has a name even when the
*endpoint* does not. If the actuating entry cannot be identified — none latched, or one the live
`servers:` list no longer carries — nothing is recorded, silently.

**5 — The start-up restore is TUI-only, and it decides in the binary.** An interactive start-up on an
entry carrying both keys asks **once**, as one command issued beside the first beat, and gets back a
**decision** — load this profile, state this line, or do nothing — never a pile of facts to judge. Every
question behind it (toggle, pointer, does the launcher still define the profile, is anything running)
is settled in the composition root, the only layer that may name the launcher facade (ADR 0029
decision 1); the renderer actuates or speaks, and the engine learns none of it
([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md):
it stays wire-silent). Headless and bench Drivers never restore — they actuate no servers at all, and a
bench run that quietly loaded a model would change what it measures.

**6 — The restore YIELDS to any running instance.** ANY instance under that launcher — any profile,
any port — leaves the restore skipped with a note naming what serves. Loading a second model beside one
already resident is how a GPU is oversubscribed, and a server somebody started by hand is not this
feature's to displace. The single running instance that earns no note is the recorded profile itself:
the ordinary start-up bind IS the restore, and announcing it would report a thing that already
happened. A profile the launcher no longer defines is a note and a skip, with the pointer left exactly
where it is — it is the human's line in their own file to remove.

**7 — Actuation reuses the latch; no new ordering is invented.** The restore's load goes out through
the same `startProfileLoad` a user's pick takes: one actuation at a time, beats in its shadow ignored
(ADR 0029 decision 5), the beat binding a same-server load, the completion fold committing a move. This
is what makes the restore cheap to reason about — there is no second path into a bind, so there is no
new race between the restore and the first heartbeat generation.

**8 — The write is an entry-scoped splice with its own allow-list.** `SaveConfigSetting` addresses
depth-2 scalars and cannot reach into a `servers:` entry, so per-entry writing is its own writer:
exactly two writable keys (`model`, `launch-profile`), any other refused **before the file is opened**;
the entry located by `name:`; the line set or replaced in place with comments, ordering, sibling
entries and mode byte-identical around the splice; a re-set to the value already there writing nothing;
the result re-parsed and re-validated before it is persisted; atomic. It is **set-only** — apogee never
clears a pointer, and removal is a manual edit. This is ADR 0035's writer paranoia carried one level
deeper into a list entry, the same shape
[ADR 0047](0047-api-keys-resolve-through-a-per-entry-key-source.md) decision 11 already needed.

## Considered and rejected

- **Writing the launcher's own config — an "active profile" or autostart key in its YAML** — rejected
  on ADR 0029 decision 4 (apogee never writes the profile store) and on preset-vs-state grounds: that
  file is a curated library of recipes, shared across machines, read by more clients than this one, and
  what one apogee session loaded last is not a property of the recipe. Boot autostart, if it is wanted,
  is llama-launcher's own feature to build, in its own file, for every client at once.
- **One key for both classes — writing `model:` on launcher-fronted entries too** — rejected: on that
  class `model:` is an empty discovery hint, and a wire id recorded beside a profile is a second answer
  to "what runs here" that goes stale the first time the launcher loads anything else.
- **Validating that the recorded profile exists at config load** — rejected: it would make a config
  check depend on the launcher's file, which is read fresh at use precisely so edits in the launcher's
  own TUI are live. A profile deleted between two sessions is a boot-time note, not a file defect.
- **Insisting on the restore — unloading what runs, or starting a second instance beside it** —
  rejected: it would displace a server the human started by hand and oversubscribe the one GPU these
  hosts have. Yielding costs a line of transcript; insisting costs the session.
- **Restoring in headless / bench runs** — rejected: those Drivers actuate nothing, and a bench that
  silently loaded a model would be measuring a different machine than the one it was pointed at.
- **A per-server override of the toggle** — deferred, not denied: one top-level key until somebody
  wants a mixed posture, at which point the registry has the shape for it.
- **Recording passive rebinds too** ("remember whatever is bound at the end of the session") —
  rejected: a heartbeat-observed change is the server telling apogee what happened, and writing it back
  would let a stranger's `/load` on a shared box, or a launcher restart, rewrite this user's config
  under a feature they enabled to remember **their** choices.

## Consequences

- **The config file gains two documented keys**, taught by the seeded template in house voice:
  `remember-model:` beside the other top-level toggles, `launch-profile:` in the per-server key
  documentation next to `llama-launcher:`, stating plainly that plain servers persist into `model:`
  instead and that the launcher's own YAML is never written.
- **Recording is visible.** A written choice states itself on a transcript line of its own — the user
  can see that their file just changed — and a failed write says so where the act happened, without
  unwinding it.
- **Being remembered is opt-in and stays reversible by hand.** With the toggle off nothing is written
  and start-up touches no launcher; with it on, forgetting a recorded choice is deleting a line, since
  the writer has no delete form.
- **The toggle is the one applied setting that pushes at no engine seam and rides no rebind** —
  everything it gates is still in the future — so its live apply is a store into the session's settings
  holder and nothing else. Every reader of it (both record seams and the boot check) reads through that
  holder, because a value captured at launch would have made a `/settings` flip govern nothing until
  the next start.
- **Realisations.** The toggle and the pointer plus their refusals (`internal/config/config.go`,
  `internal/config/options.go`, `internal/config/registry.go`, `internal/config/defaults/config.yaml`);
  the entry-scoped writer (`internal/config/configwrite.go`, one concern per package,
  [ADR 0043](0043-files-split-by-concern-and-config-gets-a-package.md)); the three seams
  `RecordModelChoice`, `LauncherHost.RecordProfile` and `LauncherHost.Restore` with the `ProfileRestore` answer
  (`internal/tui/tui.go`), called at the explicit-pick site (`internal/tui/picker.go`), at the two
  load-commit folds and in the boot fold (`internal/tui/actuation.go`); their implementations and the
  restore's whole decision ladder in the composition root (`cmd/apogee/wire_verbs.go`,
  `cmd/apogee/launcher.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_options.go`), with the live
  toggle held in `cmd/apogee/wire_settings.go`.
