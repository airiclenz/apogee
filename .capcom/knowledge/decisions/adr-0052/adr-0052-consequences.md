---
title: ADR 0052 consequences
---

- Expanded `view_diff` stops painting the whole file: same regions as every other diff block.
  Anyone wanting the full content has `read_file`.
- The three edit tools compute a little display data per apply; headless and bench hosts read
  or ignore it freely, and `CONTEXT.md`'s Tool summary count moves from six to nine.
- The scheme comments that pitch `success` relative to a green `diff-add` are reworded with the
  turquoise family.
- The stacked rows stay the codec's `Details` payload, so scrollback replayed on an older build
  keeps its shape — the same additive rule every codec field change has followed.
