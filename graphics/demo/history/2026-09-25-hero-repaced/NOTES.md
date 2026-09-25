# 2026-09-25 — hero clip, re-paced (fourth shipped)

The same cassette as `2026-09-25-hero`, replayed and re-rendered after a storyboard re-pace
(apogee-hero-beat-pacing). No re-capture: the beats' actions are unchanged, only where they split.

| fact | value |
|---|---|
| shipped as | `graphics/demo.gif` (1.4 MB, 42 s, 1250×826, 24 fps, 585 frames) |
| apogee | the `~/.cache/apogee-demo/bin` build of `c4690e70` the previous clip used; no apogee change since |
| cassette | `storyboards/hero.cassette`, unchanged from the previous clip |
| storyboard | `storyboards/hero.yaml`, now eleven beats: the old beat 7 is split into 7 (the wait for `Replace (2)`, fast-forwarded) and 8 (the two clicks that open the split diff, at 1×), and the context-gauge zoom (now beat 9) is `cut: true` |
| render | beat speeds: 4 at 4.86× (19.4 s in 4 s), 7 at 5.25× (42.0 s in 8 s), every other beat 1.00× with its last frame held; 9 cut. The previous clip played the whole old beat 7 at 7.0×, so the split diff flashed past in under half a second |
| records | 1 replay, every check row PASS |

Why the gauge is cut: on this model's 1.3M-token window the zoom landed on `9k/1.3M 0%`. Pinning
a small `context-window:` would read better but misstate the model on camera. The beat is still
performed, so a clip on a small-window model brings it back by dropping `cut:` and giving it a
`duration:`.

Files here:

- `demo.gif` — copy of what shipped.
