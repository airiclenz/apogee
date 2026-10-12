---
title: The hero tape's knob 3 is a clock where a screen-state trigger is needed
status: done
type: bug
priority: 3
labels: [demo]
plan: 2026-09-03 - 02 - deferred-items-sweep-plan
aliases: [apogee-h64]
created: 2026-09-03T18:41:08Z
closed: 2026-09-04T21:28:30Z
reason: Delivered by docs/plans/2026-09-03 - 02 - deferred-items-sweep-plan.md item 8 (the hero tape's knob 3 waits for the fix card)
---

**Status:** parked 2026-08-24 — owner call at the close of the hero-GIF refresh plan
(`docs/plans/archived/2026-08-24 - 00 - hero-gif-refresh-plan.md`): the fix is a mechanism, not a
knob value, and the run had a frame-verified keeper in hand.

- [ ] Knob 3 is the fixed `Sleep 10s` at `graphics/demo/tapes/hero.tape:95` that precedes the
  block-cursor gesture (`Escape` + `Type "[1;3A"`, `graphics/demo/tapes/hero.tape:96-98`) which
  opens the fix's collapsed edit card for beat 5. The window it must hit is the gap between the
  `task.go` `Replace` card painting and the queued interjection being DELIVERED — delivery lands at
  the very next tool boundary, so the window is UNDER A SECOND, while the run varies ~19s to ~29s
  end to end and slides that window by ~8s. Measured hit rate ~1 take in 7. Tuning downward is the
  wrong direction, not a better guess: at 3s nothing is settled for the block cursor to stand on,
  the leading ESC of the CSI is read alone, and the run is CANCELLED (session JSON ends
  `note: cancelled`, stage tree clean). Both failure shapes are written into the knob comment
  (`graphics/demo/tapes/hero.tape:49-94`) and into `graphics/demo/README.md:116` ("Knob 3 is a coin
  toss, not a setting"). A clock cannot track a window that slides with run length: what this needs
  is a trigger keyed to screen state — the card having painted — rather than to elapsed time.
