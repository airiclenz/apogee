# 2026-09-25 — hero clip, slowed and smoothed (fifth shipped)

The re-paced clip (`2026-09-25-hero-repaced`) still read as rushed: the sub-agent beat ran at
4.9×, the fix wait at 5.25×, the cursor crossed the screen in 450 ms, often cut shorter by the
next press, and zooms snapped in over 400 ms. This take slows all three and names the CHANGELOG
as an `@` file reference, which is a new request and so a new cassette.

| fact | value |
|---|---|
| shipped as | `graphics/demo.gif` (2.5 MB, 54 s, 1250×826, 24 fps, 733 frames) |
| apogee | the `~/.cache/apogee-demo/bin` build of `c4690e70` the previous clips used (`v0.23.1+gc4690e70fdfa.dirty`); no apogee change since |
| model / server | OpenRouter, alias `openrouter`, upstream `https://openrouter.ai/api`, model `~deepseek/deepseek-v4-flash-latest`; key from the `openrouter-ds4` entry's `api-key-cmd`, passed as `--key-env OPENROUTER_API_KEY` |
| storyboard | `storyboards/hero.yaml` as committed with this clip. Beat 2: a pause of at least the cursor glide before every click, the picker double-click split into two clicks 700 ms apart. Beat 5: the queued message is `also add an entry to @CHANGELOG.md for the fix`. Beats 6 and 8: a 1 s pause before the first click, 3 s on the opened view. Durations: 2 → 5 s, 4 → 7 s, 5 → 5 s, 6 → 7 s, 7 → 11 s, 8 → 6 s |
| render | cursor glide 900 ms (was 450), ring pulse 500 ms, linger 1.5 s, fades 400 ms; zoom ramps 1 s (was 400 ms); every ramp and glide eased with smootherstep, and the zoom eases its factor's exponent. Beat speeds: 4 at 3.43× (24.0 s in 7 s), every other beat 1.00× — this capture's model wrote the fix in 10.3 s, so beat 7 no longer needed a fast-forward; 9 cut |
| cassette | `storyboards/hero.cassette` (1.1 MB, 9 exchanges, sha256 `32f1e2e31e8c…`) |
| captures | 2. The first timed out in beat 4: the model ran Tests and List itself, then thought past the 90 s wait without spawning the sub-agent. The second passed every check row |
| records | 1 replay of that cassette, every check row PASS |

Files here:

- `demo.gif` — copy of what shipped.
