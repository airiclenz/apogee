# Handoff: record the README hero demo GIF on the host machine

**Goal:** produce the hero demo GIF for the README placeholder (`README.md` line 9:
`<!-- demo GIF / screenshots land here -->`). A devbox attempt got the full toolchain
working and validated the pipeline end-to-end on a minimal tape, but the real recording
produced a 0-byte GIF and a retry crashed the devbox container (working diagnosis:
container memory pressure — headless Chromium + 75 s of 1250×680 frames + the ffmpeg
GIF render). The run moves to the host Mac, which has the memory, native VHS support,
and the LLM server on localhost.

**Context:** this follows the README repositioning (landed on `main` this session) and
the v0.11.0 release with prebuilt binaries + Homebrew formula (see README → Install).
The GIF is the top item of the visibility plan: nobody adopts a TUI they haven't seen.

## The storyboard (the ratified script for the clip)

One scene, one task, loopable. Target 30–45 s recorded; 1250×680 px at FontSize 15.
The arc is red→green with one mid-run interjection — the feature no other agent demos.

| beat | ~time | what the viewer sees |
|---|---|---|
| 1 | 0:00–0:02 | `$ apogee --mode allow-edits` typed in a shell; the TUI opens; the footer binds **mac-studio · gemma-4-26B** — the local-model proof, on screen the whole clip |
| 2 | 0:02–0:06 | prompt typed: `the test suite is failing - find the bug, fix it, and prove the tests pass` → ⏎ |
| 3 | 0:06–0:20 | tool cards stream: `go test` (red FAIL: `Pending returned 1 tasks, want 2`), read `task.go`, edit card with the one-line diff (`i := 1` → `i := 0`) |
| 4 | ~0:12 | **money shot**, while cards still stream: `also note the fix in the CHANGELOG` typed into the live prompt box → ⏎ → shown *queued* → delivered into the running task at the next tool boundary |
| 5 | 0:20–0:35 | CHANGELOG edit card; `go test` re-run goes **green PASS**; short closing summary |
| 6 | 0:35–0:40 | the automatic naming call lands: the top rule now carries the session title; hold 2–3 s, loop |

Why this script: the red→green arc is legible without reading a word; the queued
interjection is apogee's differentiator; the footer proves it's local; auto-title is a
quiet flourish that costs nothing. Everything else (`/sessions`, the `/model` launcher
picker, Shift+Tab→Auto confinement) is deliberately **not** in the hero GIF — those are
later screenshots.

## Demo materials (recreate verbatim on the host)

Layout: a `demo/` directory anywhere short-pathed (e.g. `~/demo`). Four parts.

**1. `demo/taskman/` — the stage repo.** Planted single-line bug, verified to fail with
exactly the message in beat 3.

`go.mod`:
```
module taskman

go 1.26
```

`task.go`:
```go
package taskman

// Task is one item on the list.
type Task struct {
	Title string
	Done  bool
}

// Pending returns the tasks still open, oldest first.
func Pending(tasks []Task) []Task {
	var open []Task
	for i := 1; i < len(tasks); i++ {
		if !tasks[i].Done {
			open = append(open, tasks[i])
		}
	}
	return open
}
```

`task_test.go`:
```go
package taskman

import "testing"

func TestPendingIncludesFirstTask(t *testing.T) {
	tasks := []Task{
		{Title: "write the proposal"},
		{Title: "review the diff"},
		{Title: "ship v0.11", Done: true},
	}
	got := Pending(tasks)
	if len(got) != 2 {
		t.Fatalf("Pending returned %d tasks, want 2", len(got))
	}
	if got[0].Title != "write the proposal" {
		t.Fatalf("first pending task = %q, want %q", got[0].Title, "write the proposal")
	}
}
```

`CHANGELOG.md` (the interjection's landing pad):
```markdown
# Changelog

## Unreleased
```

Sanity check before any take: `cd demo/taskman && go test ./...` must print
`Pending returned 1 tasks, want 2`.

**2. `demo/home/` — isolated apogee home.** Copy the repo's seed template
`cmd/apogee/defaults/config.yaml` to `demo/home/.apogee/config.yaml`, then set two keys
(uncomment/edit):

```yaml
endpoint: http://localhost:1111
host-alias: mac-studio
```

Isolation via `HOME` (not `--config`) keeps the on-screen command pristine — the clip
shows bare `apogee --mode allow-edits`, and sessions/prompts land under the demo home,
never the real `~/.apogee`.

**3. `demo/env.sh` — hidden setup the tape sources:**
```sh
export HOME=<abs-path>/demo/home
cd <abs-path>/demo/taskman && clear
```
If the demo apogee binary is not on PATH, prepend its dir here too.

**4. `demo/demo.tape` — the VHS tape (take-1 timings, deliberately generous):**
```
Output take1.gif

Set Shell bash
Set FontSize 15
Set Width 1250
Set Height 680
Set Padding 16
Set TypingSpeed 40ms
Set Theme "Catppuccin Mocha"

Hide
Type "source ./env.sh"
Enter
Show

Sleep 500ms
Type "apogee --mode allow-edits"
Sleep 500ms
Enter
Sleep 4s

Type "the test suite is failing - find the bug, fix it, and prove the tests pass"
Sleep 800ms
Enter

Sleep 18s

Type "also note the fix in the CHANGELOG"
Sleep 500ms
Enter

Sleep 45s
Sleep 5s
```

The two mid-run `Sleep`s are the knobs: the 18 s one decides where the interjection
lands (it must arrive while tool cards are still streaming — tune per take), the 45 s
one must outlast the run's tail. Expect 3–5 takes; the model is nondeterministic.

## Host-run procedure

1. `brew install vhs` (formula pulls ttyd + ffmpeg; VHS fetches its own headless
   Chromium on first run — native on macOS, no sandbox workaround needed).
2. apogee: the released 0.11.0 via the new formula, or a dev build — owner's call on
   which the clip should show (a dev build shows newest TUI polish).
3. Verify the model: gemma-4-26B-A4B-it-QAT was serving on `:1111` at handoff time.
   Confirm (or deliberately switch) before recording — the model name is *in the shot*
   (footer, beat 1).
4. Reset between takes: restore `task.go`'s bug and the CHANGELOG stub, and wipe
   `demo/home/.apogee/sessions/` + `prompts/` so recall/session state can't leak into
   the clip (`git init` in `taskman` + `git checkout .` is the comfortable way, and it
   also makes the footer/status of a repo workspace realistic).
5. Post-processing once a take is right: if the raw run is long, time-compress the
   waiting stretches (`ffmpeg -filter:v "setpts=PTS/1.6"` on an `.mp4` output, then
   GIF via gifski), and optimize (`gifsicle -O3 --lossy=80`) — README target well
   under 10 MB. Adding `Output take1.mp4` beside the `.gif` line costs nothing and
   gives the better source for post.
6. Landing: GIF into `graphics/` (beside the logo), tape + taskman into the repo too —
   the tape was chosen over a screen recording precisely so the demo is reproducible
   when the TUI evolves. Replace the README placeholder line with the image embed.
   Suggested paths: `graphics/demo.gif`, `graphics/demo/` for tape + stage repo.

## Pitfalls already paid for (do not rediscover)

- **VHS parser rejects very long `Output` paths** — keep the tape's paths short/relative,
  run `vhs` from the demo dir.
- The 0-byte GIF + container crash came from the *full-length* recording; the identical
  pipeline passed on a 3 s tape. If anything similar appears on the host (it should
  not), the levers are `Set Framerate 24`, smaller Width/Height, or recording `.mp4`
  only and making the GIF in post.
- Devbox-only, for the record: container Chromium needed a `--no-sandbox` wrapper
  (and writing that wrapper through the `/usr/bin/chromium` symlink clobbered the real
  binary — write wrappers to a fresh path); a `pkill -f` pattern can match your own
  command line.
- The devbox scratchpad copy of all of this lives under the session scratchpad
  (`…/scratchpad/demo`, `/demo` symlink) — disposable, nothing to preserve.

## Open calls for the owner

- Theme (`Catppuccin Mocha` is a placeholder — pick against the README's dark logo)
  and font (`Set FontFamily`, e.g. JetBrains Mono).
- Exact prompt wording in beats 2/4 (current wording tested well for greppability but
  was not model-tested end-to-end).
- Whether the hero clip shows `ask-before` with one approval instead of `allow-edits`
  fluidity (recommendation: `allow-edits`; approvals belong in a later screenshot).
- Whether beat 6 (auto-title on the top rule) makes the cut — it depends on the naming
  call landing inside the recording window; if it misses, ship without it.

## Suggested skills

- `manage-llm-server` — before recording: confirm what `:1111` serves, switch profiles
  deliberately, tail the server log if a take stalls.
- `handoff` — if the host session ends with takes recorded but the README embed or
  repo landing still open, hand the remainder back the same way.
