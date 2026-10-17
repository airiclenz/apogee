# Demo recording rig

Everything needed to re-record the README hero clip, or to record a new clip showing some
other feature, without rediscovering any of the groundwork.

The clip is driven from a storyboard rather than performed by hand, on purpose: when the TUI
changes, re-recording is one command instead of a careful hand performance.
[photon](https://github.com/airiclenz/photon), a standalone terminal-clip recorder, runs the real
`apogee` binary in a pty, performs every beat the storyboard lists — humanized typing, keys,
waits and real mouse clicks — while a recorded model answers, checks the take, and renders the
shipped GIF from what the terminal showed. This file covers apogee's side of that: the rig, the
model service and what a keeper take must show. photon's own behaviour — its commands, the
storyboard and `photon.yaml` schemas, section timing, zoom, cursor and fonts — is documented once,
in [photon's README](https://github.com/airiclenz/photon#readme), and not restated here.

## Install

```sh
make install                                              # the apogee the rig films (below)
go install github.com/airiclenz/photon/cmd/photon@latest  # the recorder
```

photon's `render` needs `ffmpeg` on `PATH` and uses `gifsicle` for a lossless pass when it finds
it; `setup` and `record` need a unix pty, so they run on Linux and macOS
([photon's Install](https://github.com/airiclenz/photon#install)). The rig builds its own two
tools from this checkout, so there is nothing else to install.

## Quick start

From the repo root:

```sh
make install          # the rig runs the apogee on your PATH — install the one you mean to film
photon setup graphics/demo/storyboards/hero.yaml    # build the rig: isolated home, stage repo, env.sh, tools (idempotent)

# once per storyboard change — the only take that talks to a live model or needs a key
export OPENROUTER_API_KEY=…
photon record graphics/demo/storyboards/hero.yaml --profile capture

# every take after that — offline, keyless, deterministic
photon record graphics/demo/storyboards/hero.yaml   # exit 1 = a beat or an expect FAILed
photon render graphics/demo/storyboards/hero.yaml -o /tmp/hero.gif   # review it first
```

`make install` comes first because `setup.sh` bakes the directory of `command -v apogee` into the
rig's `env.sh`: the rig films the apogee on your `PATH`, so install the build you mean to film
before you set up.

`record --profile capture` is a whole take against the live model behind a recording proxy; once
every beat has passed it copies the model's replies over `storyboards/hero.cassette`, the cassette
every later take replays. A plain `record` replays that cassette as the model, so it needs no
network and no key, and a retake costs seconds of wall clock rather than a model's mood. Both
reset the stage first, write the take to `<work>/hero.take` and exit with the check's status, so
a retake loop is `until photon record graphics/demo/storyboards/hero.yaml; do :; done`.

`render` writes `graphics/demo.gif` — the storyboard's `ship:` path, the one the README shows —
unless `-o` names another file, so pass `-o` to review a render before it replaces the shipped
clip. `photon lint graphics/demo/storyboards/hero.yaml` checks the storyboard against
`photon.yaml` without touching the rig; `photon check graphics/demo/storyboards/hero.yaml`
re-judges the last take without recording it again.

**The server alias and the model id are on camera** in the footer for the whole clip, so they are
chosen deliberately and fixed in `setup.sh`: the `openrouter` alias and
`~deepseek/deepseek-v4-flash-latest` (fast inference, so a capture is short). Both must match the
committed cassette — change either in `setup.sh` and re-capture. apogee itself never talks to the
live server: its one server entry is `http://127.0.0.1:18181`, keyless, where `photon.yaml`'s
`model` service runs. The key travels only through `--key-env OPENROUTER_API_KEY` on the capture
proxy's command line — it names the variable, the proxy reads it and adds it to each forwarded
request, and it is never written anywhere; naming a variable that is unset is refused rather than
capturing a cassette of 401s.

## Layout

| path | what it is |
|---|---|
| `photon.yaml` | photon's project config: the rig's setup and reset, apogee's launch, the `model` service and its `capture` profile, apogee's quit keys and the `footer`, `status` and `transcript` areas |
| `setup.sh` | builds the rig: isolated apogee home, stage repo, generated `env.sh`, the rig's tools in `<work>/bin`, warm Go cache; `photon setup` runs it |
| `reset.sh` | restores the planted bug + CHANGELOG stub, wipes session state; `photon record` runs it before every take |
| `storyboards/<clip>.yaml` | one storyboard per clip — the whole clip: the actions of every beat, its section timing and expects, and the director's notes; `hero.yaml` is the README clip |
| `storyboards/<clip>.cassette` | the model replies a capture saved and the `model` service replays (JSON, committed beside its storyboard) |
| `stage/` | templates for the taskman stage repo (copied out by `setup.sh`) |
| `history/` | local only (gitignored): one folder per shipped clip — the GIF that shipped, its variants, and a `NOTES.md` of the recording facts |
| `cmd/stubllm` (repo root) | the model service: replays a cassette, or captures one through a recording proxy |
| `cmd/sessionexpect` (repo root) | judges one entry of the session a take saved; the storyboard's session expects run it |

Nothing is built inside this repo. The rig lands in photon's work dir,
`~/.cache/apogee-demo-photon` (photon's default for `photon.yaml`'s `name: apogee-demo`), which
keeps a 60 MB+ Go build cache, a generated apogee home, the rig's tools and the takes out of the
tree. `--work <dir>` on any photon command, or `PHOTON_WORK`, moves it; photon exports the work
dir to `setup.sh` and `reset.sh` as `PHOTON_WORK`, and both fall back to the same default when run
by hand. A rig built anywhere else needs the same `--work` on every later command — `setup.sh`'s
closing hint names it.

## How a take works

`photon record` and `photon record --profile capture` run the same take; only the model service
differs. The take runs `reset.sh`, starts the `model` service on port 18181 and waits for it to
accept connections, then launches `apogee` in a pty sized to the storyboard's `frame.cols` ×
`frame.rows` (135 × 44 for the hero) under the generated `env.sh`, waits for the first paint, and
performs the beats in order. The take starts at apogee's first paint, so there is no shell launch
to trim. After the last beat photon waits for apogee to go idle and quits it with ⌃c⌃c (`end.keys`
in `photon.yaml`): the first press arms apogee's quit and the second confirms it, and a clean quit
flushes the session to disk before the check reads it. Nothing apogee paints on its way out
reaches the take. photon's own account of a take — the take file, the end of a take, what a
failed beat leaves behind — is [The take and the retake
loop](https://github.com/airiclenz/photon#the-take-and-the-retake-loop).

**The launch is isolated from the operator.** `launch.strip: ['APOGEE_*']` removes every APOGEE_*
variable from the environment the take launches in, so no other config, server, endpoint, model,
mode, bypass, workspace or API key steers apogee away from the rig: the rig's `config.yaml` is the
whole of what a take runs on. The one variable the launch sets, `APOGEE_NO_UPDATE_CHECK=1`, turns
the boot update check off, so a recording neither calls the release server nor paints its notice.

**Checking a take.** The check runs at the end of every `record`, and on demand as `photon
check`. Each expect is one `beat N | PASS/FAIL | detail` row (`take | …` for the storyboard-level
ones), and the exit status is 1 on any FAIL. A `seen` expect judges the screens the take recorded
inside the beat. A `run` expect is a shell command whose exit status is the verdict, and the hero
uses two kinds:

- **Session expects** run `"$PHOTON_WORK/bin/sessionexpect"`, which judges one entry of the newest
  session the take saved (`$PHOTON_WORK/home/.apogee/sessions/*.json`). `--entry` selects it with
  comma-separated `key=value` pairs — `kind` (required, an entry kind as the session JSON spells
  it: `user`, `toolCall`, `interjected`, …), `text`, `tool` and `target` (prefix matches on the
  entry's text, tool label and tool target) and `nth` (a positive integer or `last`) — and the
  clauses judge it: `--contains` (a substring of its text, tool label, tool stat or tool summary —
  where apogee saves an edit's diffstat and a test run's verdict), `--before` and `--after` (each a
  second selector the entry must come before or after). Exit 0 is a pass, 1 a fail, 2 a mistyped
  command line.
- **The stage expect** at the top of `hero.yaml` is a `git status --porcelain` of the stage repo:
  the fix and the CHANGELOG entry must still be there after the take.

A missed queued message fails beat 5, because the entry is saved as `user` instead of
`interjected`. For the deep dive — exactly where the queued message landed in the tool sequence,
and what the stage looks like — read the session and the stage directly:

```sh
python3 -c "import json,sys; e=json.load(open(sys.argv[1]))['transcript']['entries']; \
[print(i, x.get('kind'), (x.get('tool') or {}).get('label',''), str(x.get('text',''))[:70]) \
 for i,x in enumerate(e)]" ~/.cache/apogee-demo-photon/home/.apogee/sessions/*.json
cd ~/.cache/apogee-demo-photon/home/Repos/taskman && git diff && go test ./...
```

## Capture and replay

A model is nondeterministic; a clip should not be. So the model is recorded once and replayed:
under `--profile capture` the `model` service is `stubllm capture`, a recording proxy between
apogee and the live server, and it files every completion reply — its raw bytes, chunked as they
arrived and stamped with when — plus the answers to the `/v1/models` and `/props` discovery
probes into `<work>/hero.cassette`. Only a take whose every beat passed copies that file over
`storyboards/hero.cassette` (the profile's `on_success`). Without the profile the service is
`stubllm replay`, which serves the committed cassette back at the captured pace, so the replayed
run streams exactly like the live one did.

**The cassette key names the conversation, not the bytes.** A replayed request never matches its
captured bytes — the system prompt carries a scratch directory and a session id, tool results
carry timings and paths — so each reply is filed under a digest of: the wire (chat or messages),
the stream flag, the sorted tool-name set (which tells a sub-agent from its parent on the same
first message), the first user message with its ISO dates normalized to `<date>` (so a cassette
captured one day replays the next), and every earlier assistant turn whole. System text, tool
results and the model id are left out. A key asked for twice answers from its queue in capture
order; a retried request was captured once, as the reply that finally answered it.

**When to re-capture: an unknown-key 500.** A replayed request whose key the cassette does not
hold is answered HTTP 500, naming the key and the request's first user message, and logged to
`<work>/model.log` — the replay never improvises, because an improvised reply would hide the
moment the run drifted from the capture. apogee retries a 5xx, gets the same 500, reports a failed
turn; the next wait times out and the take fails.
That is the signal to run `photon record --profile capture` again. It happens when something the
key reads has changed: a typed prompt (beat 3's sentence *is* the first user message), apogee's
tool menu, the orientation text in the first message, or the model's own earlier replies — which
is to say any storyboard change that makes the model answer differently. A change the key does
not read replays silently, so re-capture on purpose after changing the model id or the alias,
too: the footer would show a model the replies never came from.

A cassette is committed beside its storyboard, so a re-record on another machine is offline from
the first take.

## Storyboards

`storyboards/<clip>.yaml` is the whole clip: the actions photon performs at each beat, how long
each beat lasts in the render, what a keeper take must show, and the director's notes — the *why*
behind every choice. **A re-record starts by editing the storyboard.** The schema is photon's
([Storyboard reference](https://github.com/airiclenz/photon#storyboard-reference)), and the decode
is strict: an unknown field is an error, so `photon lint` catches a typo before a take does.
`hero.yaml`'s header comment and beat notes are the worked example. What is apogee's own:

- **No `cassette:` and no `fonts:`.** The model is a service in `photon.yaml`, not a storyboard
  field, and the faces are the ones embedded in photon.
- **The frame.** `hero.yaml`'s `frame` is both the recorded terminal (135 × 44 cells) and the
  shipped geometry: rasterized at scale 2 and downscaled to a 1250-wide GIF, so a zoom has real
  pixels to draw on. `photon.yaml` sets no `theme`, so frames draw on photon's defaults: Catppuccin
  Mocha's foreground and ANSI colours on a near-black `#0e1117`.
- **Areas.** `photon.yaml` declares the three bands of apogee's screen a target or a `seen` is
  searched in, read off apogee's own chrome (`layout.md`) and found afresh on every screen:

  | area | rows |
  |---|---|
  | `footer` | the row directly above the lowest `▁` floor hairline — mode marker, server alias, model, workdir |
  | `status` | the row directly below the lowest `▔` top rule above the prompt box — the context gauge's line |
  | `transcript` | everything above that top rule |
  | `any` (or absent) | every row |

- **Keys and clicks.** Key names are spelled as Bubble Tea's `msg.String()` spells them, which is
  also photon's spelling (`enter`, `esc`, `shift+tab`, `pgdown`, …). Every on-camera click is a
  real SGR mouse press and release, which apogee receives because it enables mode 1002 + SGR 1006
  and toggles blocks on release; nothing is faked by keyboard.
- **The minus sign.** Every on-screen minus the TUI draws in a diffstat is U+2212 (`−`), never an
  ASCII hyphen, and every pattern that matches one spells it so — `\+1 −1`.
- **Expects** are `seen` and `run` only; session and stage expects are the `run` forms under
  **Checking a take**, above.

A section's speed, the zoom and the drawn cursor are photon's ([Sections, zoom and
cursor](https://github.com/airiclenz/photon#sections-zoom-and-cursor)): a re-pace is an edit to a
beat's `duration:` and a re-render, never another take.

## How the isolation works

A take remaps `HOME` to the demo home rather than passing `--config`. Two reasons: the launch
stays a bare `apogee` with no distracting flags, and sessions, prompts and recall state land in
the demo home instead of the real `~/.apogee`.

The stage repo sits at `<demo home>/Repos/taskman` so that — with `HOME` remapped — the footer
renders the workdir as a clean `~/Repos/taskman`. Put the stage anywhere outside the demo home and
the footer shows a long absolute path that gives the staging away.

## Things that are settled, and why

**The endpoint is the server's base URL, without `/v1`.** apogee appends `/v1/chat/completions`
and `/v1/models` itself, and so does the capture proxy, so the capture profile's upstream is
`--upstream https://openrouter.ai/api` — spelled with the `/v1` suffix, the requests land on
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
and expanded blocks"), and consecutive tool calls fold into one `Tools` group: the fix and the
CHANGELOG edit land as a single `Replace (2)` row, and the split diff is not on camera unless a beat
opens it. Beat 7 waits for that `(2)`; beat 8 clicks the row open, clicks its `task.go … +1 −1` member
open, then pages back to the bottom. Clicking the lone first `Replace` row instead does not work:
when the second edit joins it the click carries onto the group row and the card stays shut. The
page-down matters too: a toggle keeps the toggled row at its screen position, which detaches the
viewport from the live tail, and left detached every later beat paints below the fold.

**Budget captures, not takes.** The live model is nondeterministic: the 2026-08-24 session needed
8 takes to bank one that carried every beat (the red test before the fix landed in roughly 3 of 8
runs, the queued message mid-run in about 6 of 8). Under capture/replay that variance is paid only
at capture — keep capturing until the check passes, then every `record` of that cassette replays
the keeper.

## Recording a new clip for a different feature

Add `storyboards/<name>.yaml`, `photon lint` it, then capture and record it as above. The stage
repo, isolated home, warm cache, reset logic and areas all come for free. One thing does not:
`photon.yaml`'s `model` service replays `storyboards/hero.cassette` and its `capture` profile
writes that file, so a clip with a conversation of its own needs its own cassette — give it a
pair of profiles in `photon.yaml`, one replaying `storyboards/<name>.cassette` and one capturing
it, and record it with `--profile`.

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
- After changing the rig itself — `setup.sh`, `reset.sh`, `photon.yaml`, `stubllm` or
  `sessionexpect` — re-run `photon setup` and a `photon record` of the hero: a take that passes
  every expect proves the rig, the model service and the session expects together.

## History

Every clip that ships gets a folder under `history/<date>-<slug>/` holding a copy of the shipped
GIF plus a `NOTES.md` with the facts a re-record needs and a screen recording cannot show: the
model and server alias that were on camera, the upstream, the storyboard, cassette and apogee
commit, the render line, and how many captures it took. Alternate renders of the same take live
beside it rather than loose in `graphics/`.

`graphics/demo.gif` stays the one path the README references; `history/` is the record, not the
link.

`history/` is **local only**: `/.gitignore` keeps it out of the tree, because each clip weighs
megabytes and every clone would carry it forever. The folders below were tracked until 2026-10-08
and stay readable from git history — `git show 52e1264e:graphics/demo/history/<folder>/NOTES.md`,
or `git restore --source=52e1264e -- graphics/demo/history/` to bring them back into a working
tree. A clip recorded since lives only on the machine that recorded it.

The history folders below predate photon: those clips were filmed by `demorig`, the rig's own Go tool until it was retired for photon in 2026-10.

| folder | clip |
|---|---|
| `history/2026-08-05-hero/` | the first hero clip: red → green with a queued CHANGELOG interjection, local model |
| `history/2026-08-24-hero/` | the v0.16 refresh: same arc plus the split-diff edit card and a closing `/undo` preview, OpenRouter `deepseek-v4-flash` |
| `history/2026-09-25-hero/` | the v2 rig's first clip (v0.23 surface): a mode click to Auto, a sub-agent fan-out and its run view, a queued CHANGELOG message, the split-diff card opened by click, a zoom on the context gauge — captured once from OpenRouter `deepseek-v4-flash`, replayed and rendered by `demorig` |
| `history/2026-09-25-hero-repaced/` | the same take re-paced: the fix wait split into its own fast-forwarded beat so the split-diff clicks play at 1×, the context-gauge zoom cut |
| `history/2026-09-25-hero-smooth/` | slowed and smoothed: pauses before every click, eased 900 ms cursor glides and 1 s zoom ramps, at most 3.4× anywhere, and the queued message names `@CHANGELOG.md` — re-captured from OpenRouter `deepseek-v4-flash` |
| `history/2026-09-25-hero-dark/` | the same cassette on a `#0e1117` background, no zoom, the cursor to the prompt before typing, a readable `@` completion, clicks on titles, and an even 1–2.4× pace |
