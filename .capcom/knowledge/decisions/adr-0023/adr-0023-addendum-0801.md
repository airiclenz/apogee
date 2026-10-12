---
title: "ADR 0023 addendum of 2026-08-01: the restore seam ENFORCES \"no system message in committed history\""
---

Decision §6 makes the prompt request-scoped, so apogee never *writes* a `RoleSystem` message into
history or a snapshot — pinned by `TestPromptSeam_ConfiguredPromptNeverEntersHistoryOrSnapshot`.
That covers everything apogee produces. It did not cover what apogee **consumes**: a legacy record
from before this ADR, or a hand-edited snapshot, whose stored conversation already *begins* with a
system message. `restoreState` installed such a conversation wholesale, `buildRequest` then
prepended the freshly rendered standing content unconditionally, and the wire carried **two** system
messages — with the profile's tool block folded into the first (the stale stored one), because
`injectSystemInstructions` merges into the first system message only *(since 2026-09-16 that
merge is `domain.MergeSystem`, with the same first-system-message rule)*.

`restoreState` (`internal/agent/state.go`) now drops the restored conversation's **leading run** of
`RoleSystem` messages before installing it. The invariant §6 maintained is therefore *enforced* at
the one seam where outside bytes become history, and every later reader — the request projection,
the next snapshot — is clean by construction.

- **A no-op on the happy path.** A well-formed apogee snapshot has never contained a system message,
  so the normalization drops nothing and the restore path is byte-identical to before.
- **Only the LEADING run.** That is the position the seeded message and the tool-block fold collide
  at. A hook-authored mid-history system message is left alone — `Conversation.PrefixEnd`
  deliberately keeps tolerating leading system messages *in a request*, which is a different
  question from what may be committed.
- **The Exchange boundary moves with the drop.** Removing messages shifts the rest of the history
  down, so the restored `exchangeStart` is shifted by the dropped count (clamped at 0); otherwise
  `AbortExchange` on a normalized legacy snapshot would roll back past that Exchange's own start.
- **Discarding beats preserving.** The dropped message is a stale rendering of standing content that
  §5 says must be re-rendered from live inputs anyway; keeping it would either duplicate the
  standing content on the wire or require the seam to guess which of the two is authoritative.
