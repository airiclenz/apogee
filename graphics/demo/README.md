# Demo recording rig

Everything needed to re-record the README hero clip, or to record a new clip showing some
other feature, without rediscovering any of the groundwork.

The clip is a [VHS](https://github.com/charmbracelet/vhs) tape rather than a screen
recording on purpose: when the TUI changes, re-recording is one command instead of a
careful hand performance.

## Quick start

```sh
# vhs 0.11.0 or newer — hero.tape's knob 3 needs Wait+Screen
brew install vhs gifsicle        # vhs pulls ttyd + ffmpeg and fetches its own headless Chromium
export OPENROUTER_API_KEY=…      # the rig's default server is OpenRouter; the key is read from here
./setup.sh                       # build the rig (idempotent)
./record.sh hero                 # reset the stage, record tapes/hero.tape
./render.sh ~/.cache/apogee-demo/hero.mp4 ../demo.gif 1.25 3.8
```

**The server alias and the model id are on camera** in the footer for the whole clip, so pick
both deliberately. The defaults are the `openrouter` alias with `~deepseek/deepseek-v4-flash-latest`
(fast inference, so a take is short); `APOGEE_DEMO_ENDPOINT`, `APOGEE_DEMO_HOST_ALIAS`,
`APOGEE_DEMO_MODEL` and `APOGEE_DEMO_KEY_ENV` override them — set `APOGEE_DEMO_KEY_ENV=` (empty)
for a keyless local server. The key itself is never written anywhere: the generated config says
`api-key-env: OPENROUTER_API_KEY` and apogee reads the variable from the shell `record.sh` runs in,
which refuses to start a take while it is unset.

## Layout

| path | what it is |
|---|---|
| `setup.sh` | builds the rig: isolated apogee home, stage repo, generated `env.sh`, warm Go cache |
| `record.sh <tape>` | resets the stage, then records `tapes/<tape>.tape` |
| `render.sh` | raw take → shipping GIF (head trim, time-compression, palette encode) |
| `reset.sh` | restores the planted bug + CHANGELOG stub, wipes session state |
| `gen.sh <src> <dst>` | writes the work-dir copy of a tape, expanding the hero tape's typed lines into humanized typing |
| `type.sh <string>` | one typed string → its humanized typing block; `--check` asserts the profile's totals |
| `tapes/` | one tape per clip; `hero.tape` is the README clip |
| `stage/` | templates for the taskman stage repo (copied out by `setup.sh`) |
| `history/` | one folder per shipped clip: the GIF that shipped, its variants, and a `NOTES.md` of the recording facts |

Nothing is built inside this repo. The rig lands in `~/.cache/apogee-demo` (override with
`APOGEE_DEMO_WORK`), which keeps a 60 MB+ Go build cache and a generated apogee home out of
the tree. `setup.sh` also honours `APOGEE_DEMO_ENDPOINT` and `APOGEE_DEMO_HOST_ALIAS`.

## How the isolation works

Recording remaps `HOME` to the demo home rather than passing `--config`. Two reasons: the
on-screen command stays a bare `apogee --mode auto` with no distracting flags, and sessions,
prompts and recall state land in the demo home instead of the real `~/.apogee`.

The stage repo sits at `<demo home>/Repos/taskman` so that — with `HOME` remapped — the
footer renders the workdir as a clean `~/Repos/taskman`. Put the stage anywhere outside the
demo home and the footer shows a long absolute path that gives the staging away.

## Things that are settled, and why

**The endpoint is the server's base URL, without `/v1`.** apogee appends `/v1/chat/completions`
and `/v1/models` itself, so OpenRouter is `https://openrouter.ai/api` — spelled with the `/v1`
suffix, the chat and model-list requests land on `/api/v1/v1/…` and 404s. `apogee probe` from
inside the demo home (`HOME=~/.cache/apogee-demo/home apogee probe`) is the two-second check; its
`active:` line names the first advertised model rather than the pinned one, which is a probe
quirk, not a mis-pin — the session binds the entry's `model:` verbatim.

**The model id is pinned in the config, not picked on camera.** The entry carries `model:` so the
clip never needs a `/model` beat; an OpenRouter "latest" alias starts with `~`, which is why
`setup.sh` quotes the value.

**Use `--mode auto`, not `allow-edits`.** In `allow-edits`, apogee's own workspace edits run
freely but shell commands still gate on approval — a hands-off tape stalls forever on
`Approve terminal?` and the take is dead. Only `auto` runs the shell surface unsupervised
(fenced by confinement). This is the autonomy ladder working as designed, not a bug; it just
rules out `allow-edits` for an unattended recording.

**Go's build cache has to be moved inside the workspace.** In `auto` mode on macOS, seatbelt
confines writes to the workspace, but `GOCACHE` defaults to `~/Library/Caches/go-build`,
outside it. Left alone, the model burns roughly seven tool calls
(`GOFLAGS=… GOCACHE=… mkdir … whoami …`) discovering this before `go test` ever runs — noisy
on camera and ~40s of the clip. The generated `env.sh` sets `GOCACHE`, `GOPATH`, `GOMODCACHE`
and `TMPDIR` inside the stage repo, gitignored so they survive resets warm. This is genuine
friction any user hits running `auto` in a Go project, not a demo artifact.

**Prompt wording is load-bearing.** "also note the fix in the CHANGELOG" made the model read
`CHANGELOG.md` and then never write it. "also add a CHANGELOG entry for the fix" landed it
first try. If a beat silently doesn't happen, suspect the verb before suspecting the model.

**Pace is decided in post, never in the tape.** Tapes record at real speed with generous
`Sleep`s; `render.sh` trims and compresses. A re-pace or a different start point then costs
an ffmpeg run instead of another take of a nondeterministic model. The hero clip ships as
`1.25` speed from `3.8`s (dropping the shell + launch so it opens already inside apogee).

**`render.sh` cannot cut from the middle — ffmpeg can.** When a take carries dead air between
two beats (a knob 2 overshoot, say), splice it out before rendering and feed the spliced file
to `render.sh`:

```sh
ffmpeg -y -i hero.mp4 -filter_complex \
  "[0:v]trim=0:46.0,setpts=PTS-STARTPTS[a];[0:v]trim=61.9,setpts=PTS-STARTPTS[b];\
   [a][b]concat=n=2:v=1[o]" -map "[o]" -c:v libx264 -qp 0 -pix_fmt yuv420p hero-cut.mp4
```

Cut only where both boundary frames are the same frame, and prove it rather than eyeballing it:
`ffmpeg -i A.png -i B.png -filter_complex psnr -f null -` above ~60 dB means the splice is
invisible (h.264 noise is all that separates them). `freezedetect` finds the candidates —
`ffmpeg -i take.mp4 -vf freezedetect=n=-55dB:d=1.5 -map 0:v -f null -` — but it also reports
typing as frozen at that threshold, so confirm on real frames before trusting a window.

**The knobs are marked in `hero.tape`.** Knob 1 decides where the interjection
lands — it must arrive while tool cards are still streaming, or it reads as an ordinary next
message instead of a queued one; the session JSON is the tell, where the entry reads
`interjected` on a hit and `user` on a miss. Knob 2 must outlast the run's tail, but
overshooting is **not** free the way this file used to claim: `render.sh` trims only the HEAD
(`-ss`), so every idle second between the last beat and `/undo` ships as dead air in the middle
of the clip. Give it margin for a slow run, not an unbounded cushion. Knob 3 is when the fix's
card is opened: the tape opens that card, and the gesture only reaches the right one while the
edit is still the most recent block.

**Knob 3 waits for the fix's card, and the measurement is why.** Against OpenRouter
`deepseek-v4-flash` the window it has to hit is the gap between the fix's `Replace` card painting
and the queued interjection being *delivered*, and delivery lands at the very next tool boundary —
so the window is under a second, while the run itself varies ~19 s to ~29 s end to end and slides
that window by ~8 s. A fixed `Sleep` cannot track that: the `Sleep 10s` this knob used to be landed
about one take in seven, and both directions of miss cost the take. Too late merely opened the
CHANGELOG card instead (`+3 −0 ▼` while `task.go` stayed `+1 −1 · +8 more lines ▶`); too early left
the block cursor nothing settled to stand on, the leading ESC of the CSI was read alone, and the run
was **cancelled** — the session JSON ends `note: cancelled` with the stage tree clean. The knob is
now a screen wait, `Wait+Screen@40s /\+1 −1/`, which blocks on the fix card's own diffstat rather
than on a clock — the same stat the comment uses to tell that card from the CHANGELOG one. Two
things to know before touching it: the pattern is the *current* edit's diffstat, spelled with the
table's typographic minus (U+2212), so a reseeded bug or a differently-shaped fix needs it
re-derived; and the 40 s timeout is deliberately generous, because a timeout is a loud failure (vhs
exits non-zero and the take dies) where the old miss was silent.

**Nothing on camera opens itself.** Tool blocks paint collapsed, always (`layout.md`, "Collapsed
and expanded blocks"), so the fix arrives as a single `Replace ↳ task.go … +1 −1 · +N more lines
▶` row and the split diff — two panes, tinted add/del bands — is not on camera unless the tape
opens it. `hero.tape` opens it with the keyboard block cursor at knob 3 and then pages back to
the bottom: a toggle keeps the toggled row at its screen position, which detaches the viewport
from the live tail, and left detached every later beat paints below the fold.

**`/undo` previews; the clip never confirms.** Bare `/undo` prints a transcript note listing
every path the exchange wrote — the fix and the CHANGELOG entry in one group, because an
interjection joins the Exchange it steered rather than opening an undo group of its own — and
nothing about it is modal, so the `Escape` that follows it in the tape is a gesture for the
camera rather than a dismissal. What proves the fix survived is `git -C <stage> diff --stat`
after the take, not the frame. `/undo confirm` would unfix the bug on camera and is never typed.

**Expect 3–5 takes against a local model; budget more against OpenRouter.** The model is
nondeterministic, and the 2026-08-24 session needed 8 takes to bank one that carried all eight
beats — the keeper was take 2, and no later take beat it. Measured hit rates over those takes:
the model reproduces the failure with a red `go test` before fixing in roughly 3 of 8 runs
(beat 3), the interjection lands mid-run rather than as a fresh turn in about 6 of 8 (beat 4),
and knob 3 opens the right card in about 1 of 7 (beat 5). Judge every take on all three before
rendering. Check the outcome before judging a take by its video:

```sh
cd ~/.cache/apogee-demo/home/Repos/taskman && git diff && go test ./...
```

The saved session is the fastest way to see what really happened, including exactly where
the interjection landed in the tool sequence:

```sh
python3 -c "import json,sys; e=json.load(open(sys.argv[1]))['transcript']['entries']; \
[print(i, x.get('kind'), (x.get('tool') or {}).get('label',''), str(x.get('text',''))[:70]) \
 for i,x in enumerate(e)]" ~/.cache/apogee-demo/home/.apogee/sessions/*.json
```

**VHS pitfalls already paid for.** Its parser has been seen to reject very long `Output`
paths — `record.sh` writes the tape into the work dir (through `gen.sh`) and runs there so all
paths stay short and relative. `Set FontFamily` takes a family name only: there is no weight
setting, so `"Source Code Pro ExtraLight"` silently renders as regular-weight Source Code Pro
(verified by diffing frames against a deliberately bogus font name). A missing font falls back
silently rather than erroring — check a frame if you change it. And `Alt+<arrow>` does not
exist in the tape language: `Alt+up` parses, then types the literal text `up` into the
terminal. The way to send ⌥↑ is the raw sequence — `Escape` followed by `Type "[1;3A"` under
`Set TypingSpeed 0ms`, so the bytes arrive as one CSI; split by a typing delay the ESC is read
on its own, which during a run means "stop" and kills the take.

## Humanized typing

`record.sh` runs the tape through `gen.sh`, which replaces each of the four messages a person
types in `hero.tape` with the per-character block `type.sh` generates for it: a 0ms metronome
and one `Sleep` per gap, from a fixed profile at a fixed seed, so the rhythm is the same on
every machine. The source tape keeps its plain `Type "…"` lines so it stays readable, and the
machine-typed lines keep their exact speeds — which is what keeps `Escape` and its CSI tail one
keystroke (**VHS pitfalls already paid for**, above). `type.sh --check` pins the totals; run it
when a band, the seed or a typed string is edited, not per take. Two timing consequences: typed
strings now run **longer** than `40 ms × N` (25–45 ms a letter, plus pauses), so knob 1 is
retuned against a take; and the `render.sh` head trim above (`1.25 3.8`) is re-measured on the
next take: the expanded `apogee --mode auto` moves the shell+launch boundary by ~+0.2–0.5 s.

## Recording a new clip for a different feature

Add `tapes/<name>.tape`, then `./record.sh <name>`. The stage repo, isolated home, warm
cache and reset logic all come for free.

Worth knowing when scripting a new one:

- The stage repo is deliberately tiny (4 files, one planted single-line bug) so a small local
  model finishes fast and the diff is legible at a glance. Keep new scenarios equally small.
- Features the hero clip deliberately leaves out, each a good candidate for its own clip:
  `/sessions`, the `/model` launcher picker, Shift+Tab mode switching, MCP tools, and the
  `ask-before` approval flow (which, unlike `auto`, needs the tape to send an approval
  keystroke — time it against a recorded take rather than guessing).
- `Hide`/`Show` brackets anything that shouldn't be on camera.
- `stage/` carries its own `go.mod`, which keeps it out of the apogee module (`go build ./...`
  and `go list ./...` skip it). But `make check` runs `gofmt -l .` over the *whole tree*, so
  the stage's `.go` files must stay gofmt-clean even though they are never compiled — an
  unformatted planted bug fails the repo's gate. Deliberately broken *formatting* is not
  available as a demo scenario; broken *behaviour* is.
- Sanity-check the stage before any take: `./reset.sh` prints `stage reset: bug present,
  tests red` and exits non-zero if the stage is wrong.

## History

Every clip that ships gets a folder under `history/<date>-<slug>/` holding a copy of the shipped
GIF plus a `NOTES.md` with the facts a re-record needs and a screen recording cannot show: the
model and server alias that were on camera, the endpoint, the tape and apogee commit, the
`render.sh` arguments, and how many takes it took. Alternate renders of the same take (untimed,
with-launch) live beside it rather than loose in `graphics/`.

`graphics/demo.gif` stays the one path the README references; `history/` is the record, not the
link.

| folder | clip |
|---|---|
| `history/2026-08-05-hero/` | the first hero clip: red → green with a queued CHANGELOG interjection, local model |
| `history/2026-08-24-hero/` | the v0.16 refresh: same arc plus the split-diff edit card and a closing `/undo` preview, OpenRouter `deepseek-v4-flash` |
