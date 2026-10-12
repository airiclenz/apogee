---
title: "ADR 0022 addendum of 2026-08-01: the scrollback splits into persisted and EPHEMERAL entries"
---

Decision 2 says resume "repaints the scrollback exactly … sub-agent `Depth`, tool cards, notes,
presented documents". That was read as *every* entry persists, and it produced a real defect
(`ISSUES.md`): the `resumed: <title>` notice was itself a persisted note, so a record collected
another copy of it on every reopen — five resumes, five stored `resumed:` lines — and the
`context: …` notice did the same at every startup, `/clear` and resume. Decision 2 is unchanged in
substance; what is added is the distinction it left implicit.

- **What qualifies as ephemeral: any entry RE-DERIVED at startup or resume time.** The test is
  whether the entry is recomputed from live state each time the view is rebuilt, not whether it
  looks incidental. A notice derived from the record being opened (`resumed: <title>`, its
  no-scrollback degrade variant, the interrupted-mid-exchange note) or from the workspace as it is
  right now (the `context: …` files notice — session-scoped prompt data re-read on restore, per
  ADR 0026) adds nothing on the
  way back in and accumulates a duplicate on the way out. An entry that records something that
  actually *happened* in the session — a cancellation, a failed save, a server switch — is earned
  by the conversation and stays persisted.
- **The mechanism generalizes the `entryStartup` precedent rather than minting a kind.** The
  start-up box was already excluded from persistence by being absent from `entryKindNames`; the
  general form is an `entry.ephemeral` flag that `encodeTranscript` skips, with kind, styling and
  position in the scrollback untouched. Only persistence differs, so rendering and the save gate
  need no per-kind duplication — the gate (`hasPrompt`, per the second 2026-08-01 addendum below)
  agrees with the blob a fortiori: a scrollback of nothing but ephemeral notices holds no prompt,
  so it is still an empty session. A `context: …` notice in a repo that has context files, or a
  degrade note over a blob that would not decode, does not on its own turn a quit into a stored
  record holding an empty scrollback.
- **The wire format does not move.** Skipping is **encode-side only** — nothing ephemeral ever
  reaches the blob, so decode needs no counterpart, no kind name is added, and the transcript
  blob's version under Decision 5 is *not* bumped. An older record keeps decoding identically, and
  a record written by this build is readable by an older one.
- **Decision 6's degrade wording is now emitted ephemerally.** "resumed, no scrollback recorded —
  the model still remembers" is still shown on a corrupt or future-version transcript blob, exactly
  as specified; it is simply not written back into the record, which is the point — a record that
  failed to decode must not accumulate notes about having failed to decode.
