# 2026-09-25 — hero clip, even pace on a dark canvas (sixth shipped)

The same cassette as `2026-09-25-hero-smooth`, replayed from an edited storyboard and rendered on
a new background. No re-capture: the typed text and the model's requests are unchanged.

| fact | value |
|---|---|
| shipped as | `graphics/demo.gif` (1.1 MB, 54 s, 1250×826, 24 fps, 810 frames) |
| apogee | the `~/.cache/apogee-demo/bin` build of `c4690e70` (`v0.23.1+gc4690e70fdfa.dirty`), unchanged |
| cassette | `storyboards/hero.cassette`, unchanged from `2026-09-25-hero-smooth` (sha256 `32f1e2e31e8c…`) |
| storyboard | beat 3 moves the cursor to the prompt box and clicks it before typing; beat 5 halts 1.8 s on the finished `@CHANGELOG.md` token so the `files` completion can be read; beat 6 clicks the `Sub-Agent` title and has no zoom (the clip has none); beat 8 aims its member click at `task.go`, not the dotted leader. Durations 3 → 6 s, 4 → 10 s, 5 → 6 s, 7 → 5 s |
| render | background `#0e1117` in place of Catppuccin Mocha's `#1e1e2e` (every other colour Mocha). The encode is now undithered and gifsicle runs lossless (`-O3`, no `--lossy`): the first render of this take, with the bayer dither and `--lossy=80`, speckled every blank area with four near-background shades — the clip-wide palette held no exact `#0e1117`, and lossy LZW scattered near colours on top. Every blank pixel is now exactly the background. Beat speeds: 4 at 2.36× (23.6 s in 10 s), 7 at 1.70× (8.5 s in 5 s), every other beat 1.00×; 9 cut |
| records | 2 replays. The first missed beat 8's member target: the pattern `[├└┕] task\.go` assumed a tree glyph the TUI does not draw there (it draws `┝`); `task\.go ⋯` hits it. The second passed every check row |

Files here:

- `demo.gif` — copy of what shipped.
