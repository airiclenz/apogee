---
title: ADR 0075 rejected
---

- **One serializer shared with Hooks** — redefining `hooks.Payload` as a filtered projection of an
  Event line. It buys one document at the cost of breaking a contract shipped days earlier and
  forcing a lossless 17-variant rendering into a shape designed for five flattened ones.
- **A pure event stream, no frames** — the bead's literal wording. Rejected because `run.Result`
  carries facts no Event emits (session id, written files, undo note, context files, denied count)
  and because the not-started paths would produce no output at all.
- **Re-rendering the human output from the Event lines** — the opencode shape, and it would fix
  headless printing nothing until a run completes. It cannot honour "the human output is unchanged":
  the post-hoc block prints `run.Result`-only facts ordered as a summary, which a live rendering
  reproduces only by buffering to the end. Filed as its own bead; cheaper later *because* the lines
  exist.
- **A bounded queue that drops** — see decision 9.
- **Timestamps and sequence numbers on `EventBase`** — see decision 7.
- **One kebab-case vocabulary across Hooks and the Event lines** — see decision 4.
- **The version only in `run_started`** — see decision 10.
- **`apogee serve`** — explicitly out of scope by owner call; its own ADR and plan, dependency-blocked
  on this contract (`apogee-afu`).
