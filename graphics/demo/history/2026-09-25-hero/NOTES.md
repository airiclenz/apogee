# 2026-09-25 — hero clip (third shipped, first from the v2 rig)

| fact | value |
|---|---|
| shipped as | `graphics/demo.gif` (2.4 MB, 39.0 s, 1250×826, 24 fps, 517 frames) |
| apogee | built from repo HEAD `c4690e70` into `~/.cache/apogee-demo/bin` and put first on PATH through `setup.sh`; `apogee --version` → `v0.23.1+gc4690e70fdfa.dirty` (the `.dirty` is the then-untracked cassette; the capture ran on the same tree at `540b5667`, `v0.23.1+g540b5667d499`, with no apogee change between the two) — the brew `apogee` on the host's PATH is v0.23.0, which predates the run-view header band |
| model / server | OpenRouter, alias `openrouter`, upstream `https://openrouter.ai/api` (no `/v1`), model `~deepseek/deepseek-v4-flash-latest` — the footer renders it as `deepseek-v4-flash-latest`. The capture proxy read the key from its own environment (`--key-env OPENROUTER_API_KEY`, filled from the `openrouter-ds4` entry's `api-key-cmd`); it was never written to the work dir, and the cassette carries no key or auth header |
| storyboard | `storyboards/hero.yaml` as committed with this clip — beat 7 waits for the `Replace (2)` group row, clicks it open, then clicks the `task.go … +1 −1` member to open the split diff |
| cassette | `storyboards/hero.cassette` (1.4 MB, 10 exchanges, sha256 `bf0c063ec1ac…`), captured once and replayed by `demorig record` |
| render | `demorig render storyboards/hero.yaml` — per-clip ffmpeg palette (192 colours, bayer dither), then `gifsicle -O3 --lossy=80`. Beat speeds: 4 at 4.9× (19.4 s of take in 4 s), 7 at 7.0× (42.2 s in 6 s — the render warns above 6×), every other beat 1.0× with its last frame held |
| captures | 1 of a budget of 8: the first live take ran every beat as scripted; it failed the check only on a checker defect (a `contains` expect could not read the tool summary), fixed in `c4690e70` without re-capturing |
| records | 2 replays of that cassette. The first put the beat-7 click on the lone first `Replace` row, which the CHANGELOG edit then folded into a `Replace (2)` group — the click carried onto the group and the split diff never opened; the storyboard's beat 7 was re-aimed and the second replay is the keeper |
| work dir | default `~/.cache/apogee-demo`, so the footer reads `~/Repos/taskman` |

Files here:

- `demo.gif` — copy of what shipped.

The take (`<work>/hero.take`) is not committed; `demorig record` rebuilds it from the storyboard
and cassette in about 90 s, and `demorig render` the GIF from the take.

Beats as the take logged them: 1 apogee's first paint with `ask before`, 2 a click on the mode
marker and on `Auto` — the footer reads `auto · confined` at 0.78 s, 3 the prompt typed, 4 the
`Sub-Agent` row `diagnose failing tests`, 5 `also add a CHANGELOG entry for the fix` queued while
the sub-agent runs, 6 a click into the sub-agent's run view (`← main ›` band) and back, 7 `Replace
(2)` opened and the `task.go` split diff (`for i := 1` → `for i := 0`) on camera from the end of
the beat to the end of the clip, 8 a zoom on the `9k/1.3M 0%` gauge, 9 `Tests ┕ PASS` below the
CHANGELOG edit, 10 a hold on the final frame.
