# 2026-08-24 — hero clip (second shipped)

| fact | value |
|---|---|
| shipped as | `graphics/demo.gif` (2.2 MB, 39.0 s, 1250×680, 24 fps) |
| apogee | recorded on the installed `apogee v0.16.3`; repo `main` at `2f08f228` (v0.16.6). `git diff v0.16.3..HEAD -- internal/tui internal/agent internal/undo` showed zero `internal/tui` drift and no undo-grouping change, so every mechanism on camera matches HEAD |
| model / server | OpenRouter, alias `openrouter`, endpoint `https://openrouter.ai/api` (no `/v1`), model `~deepseek/deepseek-v4-flash-latest` — the footer renders it without the leading `~`, as `deepseek-v4-flash-latest`. Key travelled as `api-key-env: OPENROUTER_API_KEY`, never written to the work dir |
| tape | `tapes/hero.tape` as of `2f08f228` — red → green with the queued CHANGELOG interjection, the block-cursor gesture that expands the fix's edit card, and the closing bare `/undo` preview |
| knobs at record time | 8 s (interjection) / 10 s (open the edit card) / **45 s** (tail). The committed tape says 12 s for the tail — that retune landed *after* this take, which is why the raw take carries ~18.5 s of static frame; see "render" |
| render | two steps. 1. `ffmpeg` concat cut of the raw take, dropping source `46.0`–`61.9` s (the static stretch between the green `PASS` and `/undo`, verified identical across the cut at 66 dB PSNR) → `hero-cut.mp4`, 52.56 s. 2. `render.sh hero-cut.mp4 ../demo.gif 1.25 3.8` — 1.25× speed, first 3.8 s (shell + launch) dropped, `gifsicle -O3 --lossy=80` pass. A take recorded on the committed 12 s knob needs step 1 |
| takes | 8; the keeper was take 2. Per-beat hit rates over those takes: red test before the fix ~3/8, interjection landing mid-run ~6/8, the card gesture opening the right card ~1/7 — their product is why no later take beat take 2 |
| work dir | `APOGEE_DEMO_WORK=/tmp/taskman-demo`, so the `/undo` preview puts `/tmp/taskman-demo/home/Repos/taskman/…` on camera and the edit card's "resolves to" line the symlink-resolved `/private/tmp/…` |

Files here:

- `demo.gif` — copy of what shipped.

The raw take (`<work>/hero.mp4`, 68.48 s, 947 KB) is deliberately not committed; the cut and the
render are reproducible from the numbers above.

Beats verified on frames of the shipped GIF (times are shipped-clip times): 1 footer + `auto`
at 0.2 s, 2 the prompt on camera, 3 red `Tests → error: FAIL (go test) — exit code 1` at 13.8 s,
4 `thinking · 3s · 1 queued` with the pending row above the input box in the same frame,
5 the two-pane split diff with tinted add/del bands from 20.6 s to the end, 6 the CHANGELOG
`Replace` card plus green `Tests → PASS` and the `Done. Tests pass.` summary at 33.6 s, 7 the
`/undo` preview naming both `task.go` and `CHANGELOG.md` at 38.8 s with the input box back to
`Send a message…` (nothing reverted), 8 a 4.4 s hold on that final frame.
