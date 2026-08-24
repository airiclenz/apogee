# 2026-08-05 — hero clip (first shipped)

| fact | value |
|---|---|
| shipped as | `graphics/demo.gif` (2.5 MB), landed in `be3d83b1` |
| apogee | v0.11.0 era, `main` at `be3d83b1` |
| model / server | a local model on the llama.cpp server behind the `mac-studio` alias (`http://localhost:1111`) |
| tape | `tapes/hero.tape` as of `be3d83b1` — red → green with the queued CHANGELOG interjection |
| render | `render.sh <take> ../demo.gif 1.8 3.8` — 1.8× speed, first 3.8 s (shell + launch) dropped |
| takes | 3 (take 2 fixed the bug but skipped the CHANGELOG; take 3 did everything) |

Files here:

- `demo.gif` — copy of what shipped.
- `demo_in_app.gif` — the same take at 1.0× speed, launch still trimmed; kept as the untimed
  reference.
- `demo_with_launch.gif` — the same take with the shell and `apogee --mode auto` launch left in.

Both variants were committed loose in `graphics/` in `235c5ecb` and moved here on 2026-08-24; a
third file, `demo_in_app_1_8x.gif`, was byte-identical to the shipped clip and was deleted.
