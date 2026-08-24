# Hero-GIF refresh — storyboard and recording plan

**Goal:** re-record `graphics/demo.gif` on the current build (v0.16.3) so the clip shows what
the TUI actually looks like now — split-diff edit cards with tinted bands, the queued
interjection, and a closing `/undo` preview beat — recorded against OpenRouter
`deepseek-v4-flash-latest` for fast inference, with the demo folder gaining a `history/`
structure so past clips and their recording facts stop living loose in `graphics/`.

**Date:** 2026-08-24 · **Status:** unexecuted · **sized for:** any host; recording needs macOS
with `vhs` + an `OPENROUTER_API_KEY` in the environment

**Authoritative sources** (an item that disagrees with these follows these):
- `graphics/demo/README.md` — the rig doc: isolation model, settled decisions (auto mode,
  GOCACHE-in-workspace, prompt wording, pace-in-post, the two timing knobs, VHS pitfalls).
  Update it where this plan changes facts; do not re-litigate what it settles.
- `graphics/demo/tapes/hero.tape` — the current tape; the new tape is an evolution of it.
- `internal/config/defaults/config.yaml` — the seeded template and the documented server-entry
  keys (`endpoint`, `api-key-env`, `model`).
- The saved-session inspection one-liner in `graphics/demo/README.md` — the ground truth for
  where an interjection actually landed in a take.

**Decisions made this session (owner, 2026-08-24):**
- **Model/endpoint:** OpenRouter, model `deepseek-v4-flash-latest`, key via
  `api-key-env: OPENROUTER_API_KEY` (never a literal key in the demo config). Both the server
  alias and the model name are **on camera** in the footer — alias becomes `openrouter`, which
  is honest; the old `mac-studio` alias would be a lie against a cloud endpoint.
- **Story skeleton stays:** red tests → find → fix → green, with the queued interjection
  mid-run. It is proven, legible, and still demos the loop + the headline UX.
- **Two new beats ride for free / cheap:** the split-diff tinted edit card (free — it is just
  how edits render now) and a closing bare-`/undo` preview, dismissed without reverting
  (cheap — one typed command, shows the safety net without unfixing the bug on camera).
- **History:** past shipped clips move to `graphics/demo/history/<date>-<slug>/` with a
  `NOTES.md` each; the shipping path stays `graphics/demo.gif` so the README link never moves.

**Standing requirements:** skills: `coding-standards` for any script edits. No `VERSION` bump,
no CHANGELOG release heading. Do not commit unless asked. The three stray GIFs are tracked in
git — move them with `git mv`, never `git stash`/sweep.

**Out of scope (deliberately):** new clips for `/sessions`, `/model` picker, sub-agents, MCP,
ask-before approvals (the rig README already lists them as future tapes); code changes to
apogee itself; any change to the stage repo's planted bug.

---

## Storyboard — the clip, beat by beat

Target: ~35–45 s shipped at 1250×680, same VHS look (FontSize 15, Source Code Pro,
Catppuccin Mocha). Footer on camera throughout: `openrouter` + `deepseek-v4-flash-latest` +
`~/Repos/taskman`.

| # | On camera | Why it's in the clip |
|---|---|---|
| 1 | Clip opens already inside apogee (launch trimmed in post), autonomy footer shows **auto** | No dead air; OS-confined Auto is a README pillar |
| 2 | User types: `the test suite is failing - find the bug, fix it, and prove the tests pass` | One natural sentence, no flags, no ceremony |
| 3 | Tool cards stream: file reads / grep, first `go test` returns **red** | The agentic loop is real — it runs the tests itself |
| 4 | **While cards still stream**, user types `also add a CHANGELOG entry for the fix` → it visibly queues | Headline UX: type while it works, interjection delivered at the next tool boundary |
| 5 | The fix lands in `task.go` — the edit card paints the **split diff, two panes, tinted add/del bands** | The biggest visual upgrade since the last clip (v0.16 diff work); keep this card expanded and on screen for a beat |
| 6 | CHANGELOG entry written (the queued instruction, honoured), `go test` runs **green PASS** | Red → green closes the arc; the interjection provably mattered |
| 7 | User types `/undo` → the preview lists the exchange's file writes → **Esc**, nothing reverted | Closing beat: "you're always in control" — preview only, the fix stays |
| 8 | ~2 s hold on the final frame | Loop-read comfort |

Beat-4 wording is settled ("also add a CHANGELOG entry for the fix" — the rig README explains
why the imperative form is load-bearing). Beat 7 must be **bare `/undo`** — it previews; do not
confirm the revert.

Timing reality: deepseek-v4-flash on OpenRouter is much faster than the local model the 18 s /
45 s knobs were tuned for. Both knobs WILL need retuning from a scout take (item 4); expect the
interjection window (knob 1) to shrink to a few seconds.

---

## 1. `graphics/demo/history/` — give past clips a home

- Create `graphics/demo/history/2026-08-05-hero/` and `git mv` into it:
  - `graphics/demo_in_app.gif`
  - `graphics/demo_with_launch.gif`
- `git rm graphics/demo_in_app_1_8x.gif` — it is byte-identical to the shipped
  `graphics/demo.gif` (same size 2559396; confirm with `cmp` before deleting).
- Write `graphics/demo/history/2026-08-05-hero/NOTES.md`: date, apogee commit
  (`be3d83b1` landed the clip), model + endpoint used (local, `mac-studio` alias), tape
  (`hero.tape` as of that commit), render args (`1.8` speed from `3.8` s), and one line on what
  each kept variant shows (`_in_app` = untrimmed full take, `_with_launch` = includes shell
  launch).
- Add a `## History` section to `graphics/demo/README.md`: the convention — every shipped clip
  gets `history/<date>-<slug>/` holding a copy of the shipped GIF plus a `NOTES.md` with model,
  endpoint alias, tape, commit, and render args; `graphics/demo.gif` remains the only path the
  README references.

**Acceptance:** `git status` shows only renames/deletes under `graphics/`; `graphics/` root
holds exactly the two logo SVGs, `demo.gif`, and `demo/`; the NOTES.md facts match `git log`.

## 2. Rig fixes — make `setup.sh` true on today's repo, pointed at OpenRouter

- **Stale template path (setup.sh:48):** `$REPO/cmd/apogee/defaults/config.yaml` no longer
  exists — the seeded template lives at `internal/config/defaults/config.yaml`. Fix the path.
- **Server entry gains key + model:** the appended `servers:` entry becomes
  `name` / `endpoint` / `api-key-env: OPENROUTER_API_KEY` / `model:` — the template documents
  exactly these keys (`api-key-env` even uses `OPENROUTER_API_KEY` as its example). New
  defaults: `APOGEE_DEMO_ENDPOINT` → `https://openrouter.ai/api/v1`,
  `APOGEE_DEMO_HOST_ALIAS` → `openrouter`, new `APOGEE_DEMO_MODEL` →
  `deepseek-v4-flash-latest`. **Verify the exact OpenRouter model id before recording**
  (OpenRouter ids are usually `vendor/model`, e.g. `deepseek/deepseek-v4-flash-latest`) — the
  id is what the footer shows, so check both that it works AND how it renders on camera.
- `setup.sh` fails fast with a clear message if `OPENROUTER_API_KEY` is unset when the endpoint
  is the OpenRouter default.
- `record.sh`/`env.sh`: `env.sh` remaps HOME, so the key must still reach apogee — export
  `OPENROUTER_API_KEY` through `env.sh` (value read from the recording user's real environment
  at setup time is NOT acceptable in a generated file; instead have `env.sh` re-export it from
  the invoking shell: `export OPENROUTER_API_KEY="${OPENROUTER_API_KEY:?set it before recording}"`).
- Update `graphics/demo/README.md`: quick-start now mentions the key, the endpoint default, and
  that the Go-cache-in-workspace note only matters for local/confined runs (it still applies —
  the stage still runs `go test` under seatbelt in auto mode — keep it).

**Acceptance:** on a clean `APOGEE_DEMO_WORK`, `./setup.sh && ./reset.sh` prints
`stage reset: bug present, tests red`; `<demo home>/.apogee/config.yaml` carries the
OpenRouter entry with `api-key-env` and no literal key anywhere in the work dir.

## 3. The new tape — `tapes/hero.tape` evolves to the storyboard

- Keep: resolution/font/theme block, the `Hide`d `source ./env.sh`, bare `apogee --mode auto`
  on camera, the two prompts' exact wording, generous sleeps + pace-in-post.
- Add after the green-test tail settles: `Type "/undo"` → `Enter` → sleep long enough to read
  the preview (≥3 s at record speed) → `Escape` → 2 s hold. Verify on a frame that the preview
  names `task.go` and `CHANGELOG.md` and that Esc leaves the working tree fixed
  (`git -C <stage> diff --stat` non-empty after the take).
- Re-mark the two knob comments with the new tuned values once item 4 measures them; the knob
  *comments* (what each knob is for, what failure looks like) carry over verbatim.
- If the split-diff card streams past too fast to register, prefer lowering render speed for
  that stretch over touching the tape (pace is decided in post — settled decision). Only if the
  card is *collapsed* by default does the tape gain an interaction to expand it; check a scout
  frame first.

**Acceptance:** `./record.sh hero` completes unattended; the take contains all 8 beats; the
session JSON (inspection one-liner in the rig README) shows the interjection delivered
mid-run, not as a separate turn.

## 4. Scout, tune, record

- Scout take at current knob values against the real endpoint; then read the session JSON to
  find where the interjection actually landed and how long the run tail was; retune knob 1
  (interjection must land while tool cards stream) and knob 2 (must outlast the tail;
  overshoot is free).
- Expect 3–5 takes (rig README). Judge a take by outcome first:
  `cd <stage> && git diff && go test ./...` → fix present, tests green, CHANGELOG entry
  written; then by video.
- Watch the two OpenRouter-specific risks: rate-limit/latency spikes mid-take (just retake) and
  the model narrating instead of calling tools (if persistent, the model id is wrong or the
  Model profile mis-matches — check the shape-table notice line at startup).

**Acceptance:** one keeper take at `<work>/hero.mp4` whose outcome checks pass and whose video
shows every beat.

## 5. Render, ship, record history

- `./render.sh <work>/hero.mp4 ../demo.gif <speed> <start>` — start trims the shell+launch as
  before; pick speed so shipped length lands ~35–45 s and the split-diff card is readable;
  gifsicle pass keeps it well under the old 2.5 MB if possible.
- Update the README hero `alt` text (README.md:13): it currently says "against a local model"
  and describes the old story — rewrite to name the actual beats (find/fix/prove, queued
  interjection, split diff, `/undo` preview) without claiming a local model.
- Create `graphics/demo/history/2026-08-24-hero/` per the item-1 convention: copy of the
  shipped GIF + `NOTES.md` (model id as rendered in the footer, endpoint, tape, apogee commit,
  render args, take count).
- Suggest — do not apply — a `docs(readme)`-style commit for the user to make.

**Acceptance:** `graphics/demo.gif` plays the new clip; GitHub-rendered README shows it; alt
text matches the new content; history folder complete.
