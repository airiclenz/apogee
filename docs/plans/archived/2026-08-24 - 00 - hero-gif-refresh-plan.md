# Hero-GIF refresh — storyboard and recording plan

**Goal:** re-record `graphics/demo.gif` on the current build (v0.16.3) so the clip shows what
the TUI actually looks like now — split-diff edit cards with tinted bands, the queued
interjection, and a closing `/undo` preview beat — recorded against OpenRouter
`deepseek-v4-flash-latest` for fast inference, with the demo folder gaining a `history/`
structure so past clips and their recording facts stop living loose in `graphics/`.

**Date:** 2026-08-24 · **Status:** complete — all five items done 2026-08-24 · **sized for:** any host; recording needs macOS
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

## 1. `graphics/demo/history/` — give past clips a home — ✅ DONE (2026-08-24)

NOTES (2026-08-24): git recorded the identical `demo_in_app_1_8x.gif` as a RENAME to
`history/2026-08-05-hero/demo.gif` (the shipped-copy the convention wants) rather than a delete +
copy — same tree, cleaner history. `ffprobe` is not on this machine's PATH, so NOTES.md carries no
clip durations.


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

## 2. Rig fixes — make `setup.sh` true on today's repo, pointed at OpenRouter — ✅ DONE (2026-08-24)

NOTES (2026-08-24), three findings that change item 3–5 inputs:
- **Endpoint is `https://openrouter.ai/api`, NOT `/api/v1`.** `internal/provider/client.go`
  does `baseURL + "/v1/chat/completions"` with no `/v1` dedup; the `/api/v1` form 404s on
  `/api/v1/v1/models` (verified with `apogee probe`). The shipped template's own OpenRouter
  example at `internal/config/defaults/config.yaml:176` spells `/api/v1` and is therefore wrong —
  an apogee bug, out of this plan's scope, flagged to the owner.
- **Exact model id is `~deepseek/deepseek-v4-flash-latest`** (OpenRouter's tilde-prefixed
  "latest" alias; queried live from `/api/v1/models`, ctx 1310720). That is the string the footer
  will render — 36 chars with a leading `~`. The dated sibling `deepseek/deepseek-v4-flash-0731`
  is the same model pinned and reads cleaner on camera; item 4's scout take decides. `setup.sh`
  quotes the value (YAML) and `APOGEE_DEMO_MODEL` overrides it.
- **`apogee probe`'s `active:` line ignores the entry's `model:`** (`internal/probe/host.go:84`
  discovers with no hint) and names the first advertised model; the session itself binds the
  entry's model verbatim (`cmd/apogee/wire_server.go:106`). Probe quirk, not a mis-pin.

Also: the key travels as `api-key-env` only — never written; `record.sh` refuses without it
(guard verified). `setup.sh` writes `<work>/rig.env` carrying the key-var NAME so `record.sh`
knows what to check. `gifsicle` is not installed here (`render.sh` treats it as optional; README
quick start now lists it). Real rig rebuilt at `~/.cache/apogee-demo`: `./reset.sh` prints
`stage reset: bug present, tests red`.


- **Stale template path (setup.sh:48):** `$REPO/cmd/apogee/defaults/config.yaml` no longer
  exists — the seeded template lives at `internal/config/defaults/config.yaml`. Fix the path.
- **Server entry gains key + model:** the appended `servers:` entry becomes
  `name` / `endpoint` / `api-key-env: OPENROUTER_API_KEY` / `model:` — the template documents
  exactly these keys (`api-key-env` even uses `OPENROUTER_API_KEY` as its example). New
  defaults: `APOGEE_DEMO_ENDPOINT` → `https://openrouter.ai/api` (see NOTES: no `/v1`),
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

## 3. The new tape — `tapes/hero.tape` evolves to the storyboard — ✅ DONE (2026-08-24)

NOTES (2026-08-24): DECISION observation — ran apogee itself in the recording geometry
(1250×680, FontSize 15) under VHS against a stub OpenAI-compatible endpoint, driving one real
`single_find_and_replace` on the stage's planted bug. The edit card paints **COLLAPSED**: a
single `Replace ↳ task.go … +1 -1 · +8 more lines ▶` row, no diff visible. `layout.md`
("Collapsed and expanded blocks") states it as the rule — "Collapsed is the default, always …
nothing ever expands or collapses by itself". So the tape gained an open gesture (KNOB 3), per
the item's "only if the card is collapsed" clause; expanded, the same card paints the two-pane
split diff with tinted add/del bands exactly as the storyboard's beat 5 wants.
NOTES (2026-08-24): VHS cannot send ⌥↑, the block cursor's entry key — `Alt+up` parses and then
types the literal text "up" into the terminal (observed: it was sent as a message). The tape
sends the raw sequence instead — `Escape` then `Type "[1;3A"` under `Set TypingSpeed 0ms` — which
apogee reads as ⌥↑; verified mid-run, with the run still working and NOT stopped by the ESC.
NOTES (2026-08-24): a toggle keeps the toggled row at its screen position (`layout.md`), which
detaches the viewport from the live tail: the first dry take showed the interjection and the
`/undo` preview painting below the fold. Added `PageDown 4` after the hold to re-attach
(`m.detached = !m.viewport.AtBottom()`); the second dry take then showed every later beat.
NOTES (2026-08-24): the `/undo` preview is a plain transcript NOTE — not a card, not modal,
nothing to expand or dismiss. The plan's `Escape` beat is kept as written (it changed nothing on
the frame, and the working tree still carried the fix after it: `git diff --stat` non-empty), but
its tape comment now says what it actually is.
NOTES (2026-08-24): knob 1 (18s) and knob 2 (45s) keep their local-model values and their
comments verbatim — item 4 retunes them against the OpenRouter endpoint. Knob 2's comment gained
one line: its wait now counts from after the knob-3 gesture, so it measures the remaining tail.
NOTES (2026-08-24): the beat-7 acceptance ("the preview names `task.go` and `CHANGELOG.md`") was
verified by mechanism, not by frame — the stub wrote only one file. `internal/agent/loop.go`
opens an undo group once per depth-0 Exchange, and an interjection commits mid-Exchange (ADR
0025/0051), so both writes land in one group. Item 4's real take is where the frame confirms it.
NOTES (2026-08-24): fact for items 4–5 — the preview lists the journal's ABSOLUTE path, so beat 7
puts `/Users/<you>/.cache/apogee-demo/home/Repos/taskman/task.go` on camera and gives the rig
away. It fits one line at the recording width; pick `APOGEE_DEMO_WORK` with that frame in mind.

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

## 4. Scout, tune, record — ✅ DONE (2026-08-24)

NOTES (2026-08-24): the Keychain read works only BEFORE `HOME` is remapped — `security
find-generic-password` resolves the login keychain through `$HOME`, so reading it after
`export HOME=<demo home>` returns empty and apogee then fails with `api-key-env:
OPENROUTER_API_KEY is set but empty`. `record.sh` is safe because it exports the key in the
invoking shell and VHS inherits it; anything that remaps HOME first is not. The key was never
echoed, never written, and a `grep -rl` for its literal value across both `/tmp/taskman-demo`
and the repo returns nothing.
NOTES (2026-08-24): recorded on the installed `apogee v0.16.3` (`/opt/homebrew/bin/apogee`),
24 commits behind HEAD (v0.16.6). Checked before trusting it: `git diff --stat v0.16.3..HEAD --
internal/tui internal/agent internal/undo` shows ZERO `internal/tui` drift and no undo-grouping
change in `internal/agent/loop.go`, so every mechanism the tape leans on (block cursor, split
diff, one undo group per Exchange) is identical to HEAD. The footer therefore reads `version
v0.16.3`, which is what the plan's goal line names as the current build.
NOTES (2026-08-24): SCOUT TAKE (take 1, knobs as item 3 left them: 18s / 12s / 45s) — outcome
passed (both files written, tests green) but the VIDEO failed two beats, and the session JSON
said why. Entry 11 read `user`, not `interjected`: the exchange had already CLOSED before the
18s knob fired, so beat 4 read as an ordinary next turn. That also breaks beat 7 by construction
— a second Exchange opens its own undo group, so `/undo` would have named only `CHANGELOG.md`.
Session ran 20.8s end to end against the 18s knob. The old values were tuned for the local
model and are simply too slow for OpenRouter.
NOTES (2026-08-24): KNOB 1 RETUNED 18s → 8s, measured. The exchange runs ~19–29s end to end, so
submitting at ~10s (8s + 1.5s of typing + 0.5s) lands mid-stream with margin either side. Verified
on the real frame at t=21s of the keeper: `Tests → error: FAIL (go test) — exit code 1` on camera
with the status line reading `thinking · 3s · 1 queued` and the pending row `⧗ also add a CHANGELOG
entry for the fix` held above the input box. That is beat 4 visibly queued, and beat 3's red test
in the same frame.
NOTES (2026-08-24): KNOB 2 RETUNED 45s → 12s, and the reason overturns a documented fact. The
README said "overshooting is free (render.sh trims)" — it is not: `render.sh` trims only the HEAD
(`-ss START`), with no mid- or tail-trim, so every idle second between the last beat and `/undo`
ships as dead air in the MIDDLE of the clip. At 45s the keeper carries ~27s of it. The measured
tail after the knob-3 gesture is ~3s, so 12s still overshoots ~4x for a slow take. README updated.
NOTES (2026-08-24): KNOB 3 measured and left at 10s, but it is a coin toss rather than a setting,
and this is the item's main finding. The window it must hit is the gap between the fix's `Replace`
card painting and the queued interjection being DELIVERED — delivery lands at the very next tool
boundary, so the window is UNDER A SECOND, while the run varies ~19s to ~29s and slides that window
by ~8s. A fixed `Sleep` cannot track it: 10s hit on the one ~29s run and missed on the ~19s ones,
opening the CHANGELOG card (`+3 -0 ▼`) while `task.go` stayed collapsed (`+1 -1 · +8 more lines ▶`).
Measured hit rate ~1 in 7. Tuning DOWNWARD is worse, not better — see the next line.
NOTES (2026-08-24): tuning knob 3 down to 3s to chase the fast run CANCELLED the take. With nothing
settled for the block cursor to stand on, the leading ESC of the `Escape` + `"[1;3A"` CSI is read
alone, which mid-run means stop: the session JSON ended `note: cancelled` after the `interjected`
entry and the stage tree was clean — no fix, no CHANGELOG. Reverted to 10s and recorded the hazard
in both the tape comment and the rig README so nobody retries that direction.
NOTES (2026-08-24): DECISION (c) — CONFIRMED ON REAL FRAMES, replacing item 3's source-only reading.
Scanning every scanline of the keeper for the theme's selection tone: at t=29.5s rows 369–388 paint
`#3b5fd3`-ish across the opened card's header, and the frame shows the bar as a full-width band over
`Replace` with the split diff already expanded beneath it — so ⏎ does leave the mode standing, exactly
as claimed. At t=30.2, 31.5, 45.0 and 60.0 the same scan returns NO selection-tone row anywhere on
the frame. The `Sleep 800ms` + `Escape` fix is what drops it, the card stays open afterwards, and the
run was not stopped (the tail completed and the outcome checks pass). The bar is in fact gone.
NOTES (2026-08-24): DECISION (a) — CONFIRMED ON A REAL FRAME. Beat 7's preview names BOTH files. At
t=66s the keeper paints `· /undo — exchange 1, the most recent one that wrote files:` followed by
`restore /tmp/taskman-demo/home/Repos/taskman/task.go` and `restore /tmp/taskman-demo/home/Repos/
taskman/CHANGELOG.md`, then `/undo confirm applies this; anything else leaves the files alone`. Item
3's mechanism argument (one undo group per depth-0 Exchange) is now frame-verified, and it holds only
because the interjection is `interjected` rather than `user` — the scout take proved the other half.
NOTES (2026-08-24): DECISION (b) — `APOGEE_DEMO_WORK=/tmp/taskman-demo` exercised; `setup.sh`
untouched, item 2 stays closed. Both on-camera paths fit one line at the recording width, but they
are NOT the same string: the `/undo` preview prints `/tmp/taskman-demo/…` while the edit card's
"resolves to" line prints the symlink-resolved `/private/tmp/taskman-demo/…`. Neither is long enough
to wrap; flagging the `/private` prefix only because it is a macOS tell on camera.
NOTES (2026-08-24): footer verified on camera — `openrouter ✦ deepseek-v4-flash-latest ✦
~/Repos/taskman` with `auto` at the right. The alias renders WITHOUT the leading `~` of the config's
`~deepseek/deepseek-v4-flash-latest`, so it reads clean at 24 chars; no change needed, as instructed.
NOTES (2026-08-24): KEEPER is take 2 of 8, installed at `/tmp/taskman-demo/hero.mp4` (68.48s,
1250×680, 947 KB). Outcome checks passed live right after it ran: `git diff --stat` = `CHANGELOG.md
| 3 +++`, `task.go | 2 +-`, and `go test ./...` = `ok taskman`. All eight beats verified on frames —
1 footer/auto, 2 the prompt, 3 red `go test` FAIL, 4 `1 queued` + pending row, 5 the two-pane split
diff with tinted add/del bands held from t=29.5 to the end, 6 the CHANGELOG write plus green `Tests
→ PASS`, 7 the `/undo` preview naming both files, 8 the closing hold.
NOTES (2026-08-24): deviation — the plan budgets 3–5 takes; this needed 8 and the keeper was still
take 2. Rates over those takes: red test before the fix ~3/8, interjection landing mid-run ~6/8,
knob 3 opening the right card ~1/7. Their product is why no later take beat take 2. Recorded in the
rig README so the next session budgets honestly.
NOTES (2026-08-24): deviation — the keeper predates the knob-2 retune, so it was recorded at 45s and
carries ~27s of dead air between the green PASS (~t=38s) and the `/undo` (~t=65s). The committed tape
now says 12s, which is the right value for the NEXT take but not the one that made this file. Item 5
inherits the consequence: `render.sh` cannot trim that middle stretch, so a straight
`render.sh hero.mp4 ../demo.gif 1.8 3.8` yields ~36s of which ~15s is a static frame. Item 5 either
cuts the stretch with a two-segment ffmpeg concat or re-records on the committed tape.
NOTES (2026-08-24): the stage tree at `/tmp/taskman-demo/home/Repos/taskman` currently holds take 8's
run, not the keeper's — `record.sh` resets the stage at the start of every take. Take 8's outcome also
passes (2 files changed, tests green), but anyone re-running the outcome check is reading the last
take, not the shipped one. The keeper's own check is the one quoted above.
NOTES (2026-08-24): the knob comments were re-marked with the measured values as item 3's last bullet
asks, and each now carries what failure looks like rather than only the number. `vhs validate
hero.tape` exits 0. `graphics/demo/README.md` gained the corrected knob-2 fact, a new "Knob 3 is a
coin toss" paragraph, and the real take budget.


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

## 5. Render, ship, record history — ✅ DONE (2026-08-24)

NOTES (2026-08-24): DECISION, the dead-air choice — took the **ffmpeg concat**, not a re-record.
The concat is verifiable in seconds and deterministic; a re-record would have to win item 4's take
lottery again (red-test ~3/8 × interjection ~6/8 × card-open ~1/7 ≈ 1 in 25 takes) to merely tie a
keeper whose eight beats are already frame-verified, and it would spend a live API key for the
privilege. Cut source `46.0`–`61.9` s out of `/tmp/taskman-demo/hero.mp4` (68.48 s → 52.56 s) with
`trim`/`concat` into `/tmp/taskman-demo/hero-cut.mp4`, then rendered from that. The splice is
provably invisible: both boundary frames sit inside a single `freezedetect` window and compare at
**66.0 dB PSNR** (h.264 noise only), and I read both as images before cutting — identical down to
the `7k/1.3M 0%` counter. The longest remaining still stretch in the shipped GIF is 4.6 s, the
deliberate hold on the finished split diff before `/undo`.
NOTES (2026-08-24): rendered `render.sh /tmp/taskman-demo/hero-cut.mp4 ../demo.gif 1.25 3.8` →
**39.0 s, 2.2 MB, 1250×680, loop forever**, inside the item's 35–45 s window and under the old
2.5 MB. Speed is `1.25`, not the old clip's `1.8`: with the dead air spliced out the take is
already lean (48.8 s of content after the head trim), so `1.8` would have shipped a 27 s clip and
raced the split-diff card. The head trim stays at `3.8` s — measured, apogee's UI is up at 1.8 s
and typing starts at 5.6 s, so 3.8 opens on a settled frame with 1.4 s to read the footer.
NOTES (2026-08-24): deviation — installed `gifsicle` (`brew install gifsicle`, 1.96). The plan asks
for the gifsicle pass and the rig README already lists it in the quick start, but item 2 recorded it
as missing on this machine. Without it the render came out **3.3 MB** (the new clip has far less
static content than the old one, so GIF inter-frame compression buys less); with it, 2.2 MB. No
other change to the render path.
NOTES (2026-08-24): deviation — the item names only `README.md:13`, but `graphics/demo/README.md` is
the rig doc the plan lists as authoritative and this change moves three of its facts, so it got the
minimum: the quick-start and pace-paragraph render args (`1.8` → `1.25`), a new "render.sh cannot
cut from the middle — ffmpeg can" paragraph carrying the exact concat command plus the PSNR and
`freezedetect` checks that make a splice safe, and the `history/2026-08-24-hero/` row in the History
table. No other rig-doc edits.
NOTES (2026-08-24): verified on real frames of the SHIPPED GIF (not the take): beat 1 footer
`openrouter ✦ deepseek-v4-flash-latest ✦ ~/Repos/taskman` + `auto` at 0.2 s; 2 the prompt on camera;
3 red `Tests → error: FAIL (go test) — exit code 1` at 13.8 s; 4 `thinking · 3s · 1 queued` with the
pending `⧗ also add a CHANGELOG entry for the fix` row in that same frame; 5 the two-pane split diff
with tinted add/del bands from 20.6 s onward; 6 the CHANGELOG `Replace` card, green `Tests → PASS`
and the `Done. Tests pass.` summary at 33.6 s; 7 the `/undo` preview naming both `task.go` and
`CHANGELOG.md` at 38.8 s with the input box back to `Send a message…`; 8 a 4.4 s hold. The splice
lands at 33.9 s and is unnoticeable — the frame before it is the same static frame the clip had been
holding, and the frame after it is the `/undo` command palette opening.
NOTES (2026-08-24): the keeper `/tmp/taskman-demo/hero.mp4` and the derived `hero-cut.mp4` stay
uncommitted and live in `/tmp`, so they will not survive a reboot; `history/2026-08-24-hero/NOTES.md`
carries every number needed to reproduce both the cut and the render.
NOTES (2026-08-24): `coding-standards` loaded; its topic references were not pulled in — this item
ships a rendered asset and three prose edits, no code.


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
