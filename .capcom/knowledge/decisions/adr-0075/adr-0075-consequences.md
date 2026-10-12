---
title: ADR 0075 consequences
---

- Every Driver, not only headless, sees the cancelled `finished` phase of decision 12: the TUI's
  transcript and the bench's tap both gain a closing bracket they never received before.
- A consumer that stops reading can stall a run (decision 9), which is why a second interrupt exits
  hard instead of waiting on a pipe nobody drains.
- A `v` bump is a CHANGELOG entry, and the goldens are the tripwire that makes an accidental one
  visible before it ships.
- `apogee serve` (`apogee-afu`) builds on this contract rather than designing its own event shape;
  the envelope and the two frames are its starting point.
- The reach is headless only. A daemon Firing and a probe still print prose; each is its own bead.
