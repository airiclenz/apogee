---
title: ADR 0035 considered options
---

- **Boot-time auto-sync of missing config keys into an existing `config.yaml` — REJECTED, not
  deferred.** The idea (an upgrade appends newly added keys, commented or active, to the user's
  file) motivated the original request, since a config seeded long ago silently lacks every later
  key. It is refused on grounds that are already ratified: it contradicts ADR 0023 §8's
  "an upgrade therefore changes nothing for anyone already running", `seedConfig`'s pinned
  never-overwrite invariant, the README's "an upgrade never touches it", and ADR 0021's
  evidence-not-preference posture — and it is precisely a write **apogee initiates**, the one class
  ADR 0012's fence exists to forbid. Decisions 4 and 2 dissolve the motivation instead: the pane
  renders the **whole schema from the registry**, so a key absent from the file still appears with
  its default and its documentation, and the first edit inserts it (below its example, decision 5).
  Discoverability was the real need; rewriting the user's document was never the only way to meet
  it. (Swept before rejecting: no such code exists in the repo, and `git log -S` finds none ever
  removed — nothing is being taken away.)
- **A full-screen (alternate-screen) takeover** — rejected: it drops apogee's status line and
  input box, orphans the transcript the user was reading, and needs its own key plumbing for
  everything the frame already provides. A pane that takes all the transcript rows delivers the
  same reading area with none of that.
- **Reusing the eight-row picker window** — rejected: forty keys through an eight-row viewport is
  a scroll-hunting exercise, and the picker's shape assumes a short flat list.
- **Batch the edits and save on close (or behind an explicit Save action)** — rejected: it invents
  a dirty-state model (what happens on esc, on a crash, on a second `/settings`), and it makes the
  write a *bulk* rewrite whose blast radius is the whole file rather than one named line. Per-edit
  writes keep every write small, individually named, and individually reversible.
- **Unmarshal → mutate → re-marshal through yaml.v3** — rejected for the reason
  `configwrite.go`'s header already records: it hands the user back a file with a couple of
  settings in it, having silently deleted every word of documentation they started with.
- **Structured-block editors in v1** (lists, maps, the multi-line system-prompt text) — rejected
  for scope; the read-only pointer is honest about where that editing happens today.
- **Editing `confine-to-workspace` / `unconfined-hosts` from the pane** — rejected per decision 8.
- **Live-applying `endpoint` / `model` edits by triggering a rebind** — rejected: it makes the
  settings pane a second, less-informed entrance to the rehoming flow ADRs 0024/0028 own, where
  `/model` and `/server` already carry the beat-completes-the-move semantics.
- **Reset by writing the default value explicitly** — rejected: it freezes today's default into
  the user's file, so a future changed default would not reach a user who once pressed "reset";
  deleting the line preserves the "no opinion expressed" state.
- **A read-only settings *viewer*** (render everything, edit nothing) — rejected: it is most of
  the work (registry, rows, pane, budget rule) for none of the payoff, and the write fence permits
  the deliberate edit, so declining it would be caution theatre. It survives as the first
  implementation step, not as the destination.
- **Rewriting the typed `layer`/`settings` copy chain into full table-driven resolution now** —
  rejected for scope (the 2026-08-01 architecture-review follow-up). The registry plus its
  bijection guard is the step toward it; the rewrite is its own effort.
