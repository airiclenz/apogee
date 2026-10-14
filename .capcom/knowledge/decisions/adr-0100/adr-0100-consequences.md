---
title: ADR 0100 consequences
---

- Docs and messages name `~/.apogee/...` once, with no per-OS variants.
- Tests point `--config` at a temp dir; `make live-eval` and `make home-census` can count what
  a run left in the real `~/.apogee`.
- Users who keep their dotfiles under XDG do not get apogee there. That cost is accepted.
