# Demo recording rig

Everything needed to re-record the README hero clip, or to record a new clip showing some
other feature, without rediscovering any of the groundwork.

The clip is a [VHS](https://github.com/charmbracelet/vhs) tape rather than a screen
recording on purpose: when the TUI changes, re-recording is one command instead of a
careful hand performance.

## Quick start

```sh
brew install vhs gifsicle        # vhs pulls ttyd + ffmpeg and fetches its own headless Chromium
export OPENROUTER_API_KEY=…      # the rig's default server is OpenRouter; the key is read from here
./setup.sh                       # build the rig (idempotent)
./record.sh hero                 # reset the stage, record tapes/hero.tape
./render.sh ~/.cache/apogee-demo/hero.mp4 ../demo.gif 1.8 3.8
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
suffix, every request lands on `/api/v1/v1/…` and 404s. `apogee probe` from inside the demo home
(`HOME=~/.cache/apogee-demo/home apogee probe`) is the two-second check; its `active:` line names
the first advertised model rather than the pinned one, which is a probe quirk, not a mis-pin — the
session binds the entry's `model:` verbatim.

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
`1.8` speed from `3.8`s (dropping the shell + launch so it opens already inside apogee).

**The two timing knobs are marked in `hero.tape`.** Knob 1 decides where the interjection
lands — it must arrive while tool cards are still streaming, or it reads as an ordinary next
message instead of a queued one. Knob 2 must outlast the run's tail; overshooting is free.

**Expect 3–5 takes.** The model is nondeterministic. Take 2 fixed the bug but skipped the
CHANGELOG; take 3 did everything. Check the outcome before judging a take by its video:

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
paths — `record.sh` copies the tape into the work dir and runs there so all paths stay short
and relative. `Set FontFamily` takes a family name only: there is no weight setting, so
`"Source Code Pro ExtraLight"` silently renders as regular-weight Source Code Pro (verified
by diffing frames against a deliberately bogus font name). A missing font falls back
silently rather than erroring — check a frame if you change it.

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
