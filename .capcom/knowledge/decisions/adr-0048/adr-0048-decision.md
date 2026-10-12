---
title: ADR 0048 decision
---

**apogee records the model choice into apogee's OWN config, on the `servers:` entry the choice was
made on — the wire model id into a plain entry's existing `model:` key, the Launch profile name into a
new per-entry `launch-profile:` pointer on a launcher-fronted one — gated by a single top-level
`remember-model` toggle that is OFF by default, and restores the profile at the next interactive
start-up through the existing actuation latch, yielding to anything already serving.** (Amended
2026-09-30: the toggle now defaults ON — see decision 1's note and the 2026-09-16 Amendment below;
`remember-model` in `internal/config/registry.go`.)

**1 — One toggle, off by default, gating both halves.** *(Amended 2026-09-16 — the default is now ON,
matching the starter template's active `remember-model: true` line; see the Amendment section
below.)* `remember-model:` is a file-only depth-1 bool
(`*bool`, nil = off) with a key-registry row and a `/settings` row, live-applied through the session's
settings holder (ADR 0037) so a flip
governs the very next pick and the next boot without a restart. Off is the default because the feature
**writes into a hand-edited file**: recording is the deliberate-edit grain of
ADR 0035 applied to a choice, and
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
(ADR 0031:
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
ADR 0047 decision 11 already needed.
