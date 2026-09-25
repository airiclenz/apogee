# Demo recording rig

Everything needed to re-record the README hero clip, or to record a new clip showing some
other feature, without rediscovering any of the groundwork.

The clip is driven from a storyboard rather than performed by hand, on purpose: when the TUI
changes, re-recording is one command instead of a careful hand performance. `demorig`, the rig's
Go dev tool, runs the real `apogee` binary in a pty, performs every beat the storyboard lists —
humanized typing, keys, waits and real mouse clicks — while a recorded model answers, and renders
the shipped GIF from what the terminal showed.

## Quick start

From the repo root:

```sh
make install                     # the rig runs the apogee on your PATH — install the one you mean to film
make demorig                     # the rig's Go tool, built to ./demorig (never a release asset)
graphics/demo/setup.sh           # build the rig: isolated home, stage repo, env.sh, rig.env (idempotent)

# once per storyboard change — the only step that talks to a live model or needs a key
export OPENROUTER_API_KEY=…
./demorig capture graphics/demo/storyboards/hero.yaml \
    --upstream https://openrouter.ai/api --key-env OPENROUTER_API_KEY

# every take after that — offline, keyless, deterministic
./demorig record graphics/demo/storyboards/hero.yaml    # exit 1 = the check found a FAIL
./demorig render graphics/demo/storyboards/hero.yaml    # writes ../demo.gif (the storyboard's ship:)
```

`capture` is a whole take against the live model behind a recording proxy; once every beat has
run it saves the model's replies as the storyboard's `cassette:` (`storyboards/hero.cassette`),
then checks the take like `record` does. `record` replays that cassette as the model, so it needs
no network and no key, and a retake costs seconds of wall clock rather than a model's mood. Both
reset the stage first, write the take to `<work>/<clip>.take` and exit with the check's status,
so a retake loop is `until ./demorig record graphics/demo/storyboards/hero.yaml; do :; done`.

`render` reads that take by default (pass another take as its second argument), writes the
storyboard's `ship:` path unless `-o` names another, and `--dry-run` prints the ffmpeg command
line instead of running it. It needs `ffmpeg` on PATH and uses `gifsicle` when it finds it
(typically another 20–40% off with no visible loss). `./demorig lint <storyboard>` checks a
storyboard's schema without touching the rig; `./demorig check <storyboard> [<take>] --stage
<dir>` re-judges a take by hand. `go run ./cmd/demorig …` works in place of `make demorig`.

**The server alias and the model id are on camera** in the footer for the whole clip, so pick
both deliberately. `setup.sh` defaults them to the `openrouter` alias and
`~deepseek/deepseek-v4-flash-latest` (fast inference, so a capture is short);
`APOGEE_DEMO_HOST_ALIAS` and `APOGEE_DEMO_MODEL` override them. apogee itself never talks to the
live server: its one server entry is `http://127.0.0.1:<port>` (`APOGEE_DEMO_PORT`, default
18181), keyless, where `capture` serves its proxy and `record` its replayer. The key travels only
through `--key-env` — it names the variable, the proxy reads it and adds it to each forwarded
request, and it is never written anywhere. `--key-env` left off sends no key, for a keyless local
server; naming a variable that is unset is refused rather than capturing a cassette of 401s.

## Layout

| path | what it is |
|---|---|
| `setup.sh` | builds the rig: isolated apogee home, stage repo, generated `env.sh` and `rig.env`, warm Go cache |
| `reset.sh` | restores the planted bug + CHANGELOG stub, wipes session state; `capture` and `record` run it before every take |
| `storyboards/<clip>.yaml` | one storyboard per clip — the whole clip: the actions of every beat, its section timing, zoom and expects, and the director's notes; `hero.yaml` is the README clip |
| `storyboards/<clip>.cassette` | the model replies `capture` saved and `record` replays (JSON, committed beside its storyboard) |
| `fonts/` | the rasterizer's faces and their OFL licences (**Fonts and licences**, below) |
| `stage/` | templates for the taskman stage repo (copied out by `setup.sh`) |
| `history/` | one folder per shipped clip: the GIF that shipped, its variants, and a `NOTES.md` of the recording facts |
| `cmd/demorig` (repo root) | the rig's Go tool: `lint`, `capture`, `record`, `check`, `render` |

Nothing is built inside this repo. The rig lands in `~/.cache/apogee-demo` (override with
`APOGEE_DEMO_WORK`, or `--work` on `capture` and `record`), which keeps a 60 MB+ Go build cache,
a generated apogee home and the takes out of the tree.

## How a take works

`capture` and `record` run the same take; only the model source differs. The take resets the
stage, serves the model on the rig's port, launches `apogee` in a pty sized to the storyboard's
`frame.cols` × `frame.rows` (135 × 44 for the hero) under the generated `env.sh`, waits for the
first paint, and performs the beats in order. The terminal is emulated with
`charmbracelet/x/vt`; every change of the screen is kept as a full-grid snapshot stamped on the
take's clock (sampled no faster than `frame.fps`, identical grids collapsed), beside a log of what
the rig did — each beat's start, each typed string, key and click, and the cell every target
resolved to. That is the take file: the render draws from it and never goes back to apogee or the
pty. The take starts at apogee's first paint, so there is no shell launch to trim, and it ends
with the last beat: the rig then waits for apogee to go idle (a second without a paint, at most
15 s) and quits it with ⌃c⌃c, so the session is flushed to disk before the check reads it —
killing apogee only if it has not exited 10 s later. Nothing apogee paints on its way out
reaches the take.

A beat whose action fails — a wait that times out, a click target not on screen — ends the take
with that error; the take is still written, because it is what shows why, but it is not checked
and `capture` saves no cassette. APOGEE_* variables that would steer apogee away from the rig
(another config, server, endpoint, model, mode, bypass or workspace) are stripped from the
environment the take launches in: the rig's `config.yaml` is the whole of what a take runs on.

**Checking a take.** The check runs at the end of every `capture` and `record`, and on demand as
`demorig check`. Each expect is one `beat N | PASS/FAIL | detail` row, then a `stage` row, and
exit 1 on any FAIL. Entry expects judge the session the take saved (the newest
`<work>/home/.apogee/sessions/*.json`, whose path the take carries); `seen` expects judge the
screens the take recorded inside the beat; `expect: {stage: dirty}` judges the stage repo, which a
hand-run `check` reports as `SKIP` unless `--stage <work>/home/Repos/taskman` names it. A missed
queued message fails beat 5 with the entry saved as `user` instead of `interjected`. For the deep
dive — exactly where the queued message landed in the tool sequence, and what the stage looks
like — read the session and the stage directly:

```sh
python3 -c "import json,sys; e=json.load(open(sys.argv[1]))['transcript']['entries']; \
[print(i, x.get('kind'), (x.get('tool') or {}).get('label',''), str(x.get('text',''))[:70]) \
 for i,x in enumerate(e)]" ~/.cache/apogee-demo/home/.apogee/sessions/*.json
cd ~/.cache/apogee-demo/home/Repos/taskman && git diff && go test ./...
```

## Capture and replay

A model is nondeterministic; a clip should not be. So the model is recorded once and replayed:
`capture` puts a recording proxy (`stubllm`'s cassette recorder) between apogee and the live
server, and files every completion reply — its raw bytes, chunked as they arrived and stamped with
when — plus the answers to the `/v1/models` and `/props` discovery probes. `record` serves that
cassette back at the captured pace, so the replayed run streams exactly like the live one did.

**The cassette key names the conversation, not the bytes.** A replayed request never matches its
captured bytes — the system prompt carries a scratch directory and a session id, tool results
carry timings and paths — so each reply is filed under a digest of: the wire (chat or messages),
the stream flag, the sorted tool-name set (which tells a sub-agent from its parent on the same
first message), the first user message with its ISO dates normalized to `<date>` (so a cassette
captured one day replays the next), and every earlier assistant turn whole. System text, tool
results and the model id are left out. A key asked for twice answers from its queue in capture
order; a retried request was captured once, as the reply that finally answered it.

**When to re-capture: an unknown-key 500.** A replayed request whose key the cassette does not
hold is answered HTTP 500, naming the key and the request's first user message, and logged — the
replay never improvises, because an improvised reply would hide the moment the run drifted from
the capture. apogee retries a 5xx, gets the same 500, reports a failed turn; the next wait times out and the
take fails.
That is the signal to run `capture` again. It happens when something the key reads has changed:
a typed prompt (beat 3's sentence *is* the first user message), apogee's tool menu, the
orientation text in the first message, or the model's own earlier replies — which is to say any
storyboard change that makes the model answer differently. A change the key does not read
replays silently, so re-capture on purpose after changing the model id or the alias, too: the
footer would show a model the replies never came from.

A cassette is committed beside its storyboard, so a re-record on another machine is offline from
the first take.

## Storyboards

`storyboards/<clip>.yaml` is the whole clip: the actions the recorder performs at each beat, how
long each beat lasts in the render and where it zooms, what a keeper take must show, and the
director's notes — the *why* behind every choice. **A re-record starts by editing the
storyboard.** It is read by humans and by `demorig` (`make demorig`, never a release asset), and
the decode is strict: an unknown field is an error, so `demorig lint` catches a typo before a take
does. The schema, with `hero.yaml`'s header and beat 6 as the worked example:

```yaml
clip: hero
ship: ../../demo.gif             # relative to this storyboard file's own directory
cassette: hero.cassette          # likewise; written by `demorig capture`, replayed by `demorig record`
fonts: ../fonts                  # likewise
frame: {cols: 135, rows: 44, padding: 32, font_size: 30, line_height: 1.2, scale: 2, width: 1250, fps: 24, max_colors: 192}
expect: {stage: dirty}           # the stage repo still carries the exchange's writes after the take
beats:
  - id: 6
    title: click into the sub-agent's run view and back
    why: every child run is inspectable — one click in, one click out
    do:
      - click: {target: {text: "┕ .*tool calls", nth: last, area: transcript}}
      - wait: {screen: "← main ›", timeout: 5s}
      - pause: {for: 2500ms}
      - click: {target: {text: "← main ›"}}
      - wait: {screen: "← main ›", gone: true, timeout: 5s}
    duration: 6s
    zoom: {target: {text: "← main ›"}, factor: 1.5}
    notes: |
      The member row (`┕ … tool calls`, the LAST one on screen — the newest run) opens the run
      view; its header band reads `← main › <run>`, and clicking the band goes back up.
```

**The header.** `clip` names the clip and its take file. `ship`, `cassette` and `fonts` are paths
relative to the storyboard's own directory. `frame` is both the recorded terminal and the shipped
geometry: `cols` × `rows` is the pty, `padding` (pixels), `font_size` (points) and `line_height`
(a multiple of the font size) lay the cells out, `scale` is what frames are rasterized at —
padding and font size multiplied through, so a zoom has real pixels to draw on — and `width`,
`fps` and `max_colors` are the shipped GIF's width, frame rate and palette size. The hero's
135 × 44 cells rasterize to ≈ 2494 × 1648 px at scale 2 and ship 1250 wide (≈ 1.5 : 1).

**A beat** carries an `id` (positive, unique, ascending), a `title`, `why` and `notes` for the
humans, a `do` list of actions, its section timing (`duration`, `hold`, `cut` — **Section
durations**, below), an optional `zoom` and its `expect`s.

**Actions**, one form per list item, performed in order:

| action | does |
|---|---|
| `type: {text, humanize}` | types the text with the humanized profile (below); `humanize: false` types it flat |
| `key: {name, repeat}` | presses a key `repeat` times (once when absent); names are spelled as Bubble Tea's `msg.String()` spells them — `enter`, `esc`, `tab`, `shift+tab`, `space`, `backspace`, `ctrl+c`, the arrows, `pgup`, `pgdown`, `alt+up`, `alt+down`, `f1`–`f12` |
| `click: {target, times}` | resolves the target on the current screen and clicks its centre cell `times` times (once when absent) |
| `wait: {screen, gone, timeout}` | blocks until the regex matches the screen — or, with `gone: true`, no longer does; `timeout` is required and at most 180 s, and running out fails the take |
| `pause: {for}` | idles |

**Clicks are real.** Every on-camera click is an SGR mouse press and release (apogee enables mode
1002 + SGR 1006 and toggles blocks on release) sent at the target's computed cell; nothing is
faked by keyboard. The same resolved cell is where the drawn cursor glides to in the render.

**Humanized typing.** Each character is followed by a gap drawn from a fixed profile — 25–45 ms
after a letter, 60–90 ms after a space, 90–140 ms after `.,-!`, and one space in eight (never
more than twice a string) a 300–500 ms thinking pause — from a seeded MINSTD generator
(`cmd/demorig/typing.go`), so the rhythm is identical on every machine and every take.

**Targets and areas.** A target — `{text, nth, area}` — is a place on the screen a click or a
zoom resolves. `text` is a regex over the screen's rows (a match never spans rows); `nth` picks
which match counts, in reading order: `first` (the default), a positive integer, or `last` (the
lowest row's rightmost match). `area` limits the rows searched, read off apogee's own chrome
(`layout.md`):

| area | rows |
|---|---|
| `footer` | the row directly above the `▁` floor hairline — mode marker, server alias, model, workdir |
| `status` | the row directly below the lowest `▔` top rule above the prompt box — the context gauge's line |
| `transcript` | everything above that top rule |
| `any` (or absent) | every row |

A target not on screen, or an `nth` past the matches, fails the action with the pattern and area
named. Every on-screen minus the TUI draws in a diffstat is U+2212 (`−`), never an ASCII hyphen,
and every pattern that matches one spells it so — `\+1 −1`.

**Expects** are what a keeper take must show. An expect sets `entry`, `seen` or both. `entry`
selects one session entry in the session-anchor grammar — `{kind, text, tool, target, nth}`, where
`kind` is an entry kind as the session JSON spells it (`user`, `toolCall`, `interjected`, …),
`text`/`tool`/`target` are prefix matches on the entry's text, tool label and tool target, and
`nth` is a positive integer or `last` — and the entry clauses judge it: `contains` (a substring of
its text, tool label or stat), `before: <id>` and `after: <id>` (ordering against the entry
another beat's first entry expect locates). `seen` is a regex that must match at least one screen
the take recorded inside the beat. The top-level `expect: {stage: dirty}` asserts the stage repo
still carries the exchange's writes after the take.

## Section durations

Every beat carries a target `duration:` — how long its section lasts in the shipped clip — and the
render derives the speed, never the other way round. A beat's stretch of the take runs from its
start to the next beat's start (the last one to the take's end). A beat longer than its duration
plays 1× over its `hold` (leading stretch, default 0) and then at `(L − H) / (D − H)` for the
rest; a beat that fits plays 1× throughout and **freezes its last frame** for the remainder, which
is how the closing hold gets a still frame before the loop. `cut: true` drops a beat from the
render entirely (it is still performed and checked), and needs no duration. The clip's length is
the sum of the kept durations, to within one frame at `frame.fps`.

`render` prints the clip's path, size and length, then one row per beat with the speed its section
plays at, and warns about any beat faster than 6× — past that, typing and streaming blur, and the
beat wants a longer duration or a shorter take. A re-pace is therefore an edit to `duration:` and
a re-render, never another take.

## Zoom and cursor

**Zoom.** A beat's `zoom: {target, factor, in, out}` pushes into an on-screen target for the
whole of its section: eased in over `in`, held, eased back out over `out` (400 ms each when
absent, shrunk in proportion when together they outrun the section). `factor` is the
magnification, 1–3; left off, the render fits it so the target plus two cells each side fills 70%
of the frame width, clamped to 1.25–2.5. The target resolves where a click of the same beat on the
same target landed (as the take logged it), else on the latest of the beat's snapshots it is on,
so the push lands where the beat settles.

**Cursor.** Every click draws a translucent white dot with a dark outline (Catppuccin Mocha's
crust), kept at constant size whatever the zoom. Per beat that clicks, it fades in (300 ms) at the
previous click point — the frame's centre column on the last row before the first — glides to
each click over 450 ms ending on the press, grows a ring pulse (350 ms) on it, lingers 1.2 s after
the beat's last press and fades out over 300 ms. The dot always sits on the cell the real click
was sent to.

## Fonts and licences

Frames are rasterized in Go from the take's cells — no terminal, browser or screen recorder in
the loop — on the Catppuccin Mocha theme: its foreground and background for the terminal
defaults, its sixteen ANSI colours for palette entries 0–15, anything else as sent. The chrome is
a plain padded terminal on the theme background, no window bar. The faces, all under
`fonts/`:

| file | role | licence |
|---|---|---|
| `SourceCodePro-Regular.ttf`, `-Bold.ttf`, `-It.ttf` | the terminal face; bold italic draws bold | `SourceCodePro-OFL.txt` |
| `NotoSansSymbols2-Regular.ttf` | first fallback for a rune Source Code Pro lacks | `NotoSansSymbols-OFL.txt` |
| `NotoSansMath-Regular.ttf` | second fallback | `NotoSansMath-OFL.txt` |
| `NotoSansSymbols-Regular.ttf` | last fallback, for the few runes (`⌃` among them) the others lack | `NotoSansSymbols-OFL.txt` |

All are under the SIL Open Font License 1.1, which allows redistributing them here; the licence
text travels beside the files and must stay there. Box-drawing and block-element glyphs are drawn
procedurally rather than from any font, so rules and borders join seamlessly at every scale.

## How the isolation works

A take remaps `HOME` to the demo home rather than passing `--config`. Two reasons: the launch
stays a bare `apogee` with no distracting flags, and sessions, prompts and recall state land in
the demo home instead of the real `~/.apogee`.

The stage repo sits at `<demo home>/Repos/taskman` so that — with `HOME` remapped — the footer
renders the workdir as a clean `~/Repos/taskman`. Put the stage anywhere outside the demo home and
the footer shows a long absolute path that gives the staging away.

## Things that are settled, and why

**The endpoint is the server's base URL, without `/v1`.** apogee appends `/v1/chat/completions`
and `/v1/models` itself, and so does the capture proxy, so OpenRouter is `--upstream
https://openrouter.ai/api` — spelled with the `/v1` suffix, the requests land on
`/api/v1/v1/…` and 404. A capture that 404s records a cassette of 404s; check the upstream
before spending a take on it.

**The model id is pinned in the config, not picked on camera.** The server entry carries
`model:` so the clip never needs a `/model` beat; an OpenRouter "latest" alias starts with `~`,
which is why `setup.sh` quotes the value.

**Mode before prompt, and the mode is Auto.** apogee launches in its default mode, ask before,
and beat 2 clicks the footer's mode marker and picks Auto *before* the prompt is typed: a
sub-agent keeps the mode it was spawned in (ADR 0013), so a switch made after the fan-out would
leave the children gated. Auto, not allow-edits: in allow-edits apogee's own workspace edits run
freely but shell commands still gate on approval, so an unattended take stalls on `Approve
terminal?`. Only Auto runs the shell surface unsupervised, fenced by confinement — and the footer
word after the marker is Auto's blast radius on the recording host, so a take that shows
`unconfined` or `gated` was recorded where apogee cannot fence, and is not a keeper.

**Go's build cache has to be moved inside the workspace.** In Auto, confinement fences writes to
the workspace, but `GOCACHE` defaults outside it (`~/Library/Caches/go-build` on macOS). Left
alone, the model burns roughly seven tool calls (`GOFLAGS=… GOCACHE=… mkdir … whoami …`)
discovering this before `go test` ever runs — noisy on camera and ~40 s of the clip. The
generated `env.sh` sets `GOCACHE`, `GOPATH`, `GOMODCACHE` and `TMPDIR` inside the stage repo,
gitignored so they survive resets warm. This is genuine friction any user hits running Auto in a
Go project, not a demo artefact.

**Prompt wording is load-bearing.** "also note the fix in the CHANGELOG" made the model read
`CHANGELOG.md` and then never write it. "also add a CHANGELOG entry for the fix" landed it first
try. If a beat silently doesn't happen during a capture, suspect the verb before suspecting the
model — and remember that a reworded prompt is a new cassette key, so it needs a re-capture.

**Nothing on camera opens itself.** Tool blocks paint collapsed, always (`layout.md`, "Collapsed
and expanded blocks"), so the fix arrives as a single `Replace ↳ task.go … +1 −1 …` row and the
split diff is not on camera unless a beat opens it. Beat 7 clicks the row open, then pages back to
the bottom: a toggle keeps the toggled row at its screen position, which detaches the viewport
from the live tail, and left detached every later beat paints below the fold.

**Budget captures, not takes.** The live model is nondeterministic: the 2026-08-24 session needed
8 takes to bank one that carried every beat (the red test before the fix landed in roughly 3 of 8
runs, the queued message mid-run in about 6 of 8). Under capture/replay that variance is paid only
at capture — keep capturing until the check passes, then every `record` of that cassette replays
the keeper.

## Recording a new clip for a different feature

Add `storyboards/<name>.yaml`, `demorig lint` it, then `capture` and `record` it as above. The
stage repo, isolated home, warm cache and reset logic all come for free.

Worth knowing when writing a new one:

- The stage repo is deliberately tiny (4 files, one planted single-line bug) so a model finishes
  fast and the diff is legible at a glance. Keep new scenarios equally small.
- Features the hero clip deliberately leaves out, each a good candidate for its own clip:
  `/sessions`, the `/model` launcher picker, Shift+Tab mode switching, MCP tools, `/undo`, and the
  `ask-before` approval flow (which, unlike Auto, needs a beat that waits for the approval prompt
  and answers it).
- Wait on text that stays put. A glyph that blinks or animates (the sub-agent card's `✦` lead,
  a spinner) can be missed by a wait; its steady label cannot.
- `stage/` carries its own `go.mod`, which keeps it out of the apogee module (`go build ./...`
  and `go list ./...` skip it). But `make check` runs `gofmt -l .` over the *whole tree*, so the
  stage's `.go` files must stay gofmt-clean even though they are never compiled — an unformatted
  planted bug fails the repo's gate. Deliberately broken *formatting* is not available as a demo
  scenario; broken *behaviour* is.
- Sanity-check the stage before any take: `<work>/reset.sh` prints `stage reset: bug present,
  tests red` and exits non-zero if the stage is wrong.
- `APOGEE_DEMO_E2E=1 go test -run TestE2ERecordCheckRender ./cmd/demorig/` drives the whole
  capture → record → check → render pipeline against a scripted upstream, no network or key — the
  check to run after changing the rig itself.

## History

Every clip that ships gets a folder under `history/<date>-<slug>/` holding a copy of the shipped
GIF plus a `NOTES.md` with the facts a re-record needs and a screen recording cannot show: the
model and server alias that were on camera, the upstream, the storyboard, cassette and apogee
commit, the render line, and how many captures it took. Alternate renders of the same take live
beside it rather than loose in `graphics/`.

`graphics/demo.gif` stays the one path the README references; `history/` is the record, not the
link.

| folder | clip |
|---|---|
| `history/2026-08-05-hero/` | the first hero clip: red → green with a queued CHANGELOG interjection, local model |
| `history/2026-08-24-hero/` | the v0.16 refresh: same arc plus the split-diff edit card and a closing `/undo` preview, OpenRouter `deepseek-v4-flash` |
