# Test drivers

**Date:** 2026-08-27 · **Status:** 🚧 **skeleton** — each section is filled by the plan item named
under its heading · **Owner ADR:**
[ADR 0062](../adr/0062-test-drivers-are-drivers.md) ("test drivers are Drivers") ·
**Realised by:** `docs/plans/2026-08-27 - 02 - test-drivers-kit-plan.md`

> **How to read this file.** It is the practical counterpart to ADR 0062: the ADR says *why* the
> kit exists and what it may not do, this document says *how to use it*. If you are about to write
> a test — or about to write a numbered human step — start at
> [Which driver observes which claim](#which-driver-observes-which-claim).

## Why

Before this kit, a claim about apogee's screen had no assertion form. The TUI had no driver, so
tests reached `tui.Model` with a `fakeEngine` and proved the reducer rather than the program;
every upstream was a hand-rolled `httptest` closure, so streaming, tool calls, usage and failure
modes were re-invented per test; and claims about wording or shape had no observer at all. The
result was a release checklist with 19 of 25 items marked manual — most of them mechanical claims
that only *looked* subjective because nothing could look at them.

The kit closes that gap with three packages and one rule. `internal/stubllm` is the single scripted
upstream, `internal/tuitest` drives the real composition and reconstructs what the terminal shows,
and `internal/judge` renders binding verdicts on the judgment halves. The rule is ADR 0062's: a
test step may be **manual** only where the table below says no driver observes its claim.

## stubllm

`internal/stubllm` is the ONE scripted upstream apogee's tests talk to (ADR 0062). A test names
the replies it wants as a `Script` and gets an OpenAI-compatible HTTP server that plays them back
through the wire shapes a real llama.cpp or OpenRouter endpoint uses — SSE content deltas, a
reasoning channel, two-fragment tool calls, a terminal usage object with the cached-prompt
breakdown, plain HTTP failures, and a stall. The package imports nothing of apogee's: the code
under test reaches it through `internal/provider` exactly as it reaches a real server.

```go
server := stubllm.New(t, stubllm.Script{
	Model: "stub-model",
	Turns: []stubllm.Turn{
		{ToolCalls: []stubllm.ToolCall{{Name: "list_dir", Arguments: `{"path":"."}`}}},
		{When: &stubllm.Match{ToolResult: "list_dir"}, Text: "There are three files."},
	},
})
// server.URL is what provider.NewClient — or apogee's own `servers:` config — is pointed at.
```

`stubllm.New(t, script, opts...)` starts the server on a loopback port and closes it on
`t.Cleanup`; `stubllm.Serve(ctx, addr, script, opts...)` is the same server for a binary (see
[`cmd/stubllm`](#the-binary-cmdstubllm)). Options: `WithRequestLog(bool)` (on by default), `WithAPIKey(key)` (401s a
request without `Authorization: Bearer key`), `WithLatency(d)` (time-to-first-token for every
reply).

### The script format

A `Script` is a model id and an ordered list of `Turn`s. The Go structs carry `yaml:"…"` tags, so
a fixture on disk and a `Script` built in a test are one format; `stubllm.Load(path)` /
`stubllm.Parse(data)` read it and `stubllm.Marshal` writes it. Parsing is strict — an unknown key
is an error, because a misspelled `chunk_rune:` would otherwise stream with the default chunking
and the test would pass for the wrong reason.

```yaml
model: stub-model
turns:
  - reasoning: "The user wants the file list."
    tool_calls:
      - name: list_dir
        arguments: '{"path":"."}'
  - when:
      tool_result: list_dir
    text: "There are three files: a.txt, b.txt and c.txt."
    chunk_runes: 3
    token_delay: 1ms
    usage: {prompt: 812, completion: 14, cached: 640}
  - repeat: true
    http: {status: 503, body: "upstream busy"}
```

That script is loaded and parsed by `internal/stubllm/script_test.go`, so it cannot rot away from
the format it documents.

### Turn kinds

A Turn is **exactly one** kind. Setting two is a validation error; setting none is the
**empty-reply** turn — the thing a real model produces when it abandons a reply mid-flight, and
the only way to script it.

| Key | What it plays |
| --- | --- |
| `text` | assistant content, streamed in `chunk_runes` (default 4) rune deltas with `token_delay` between them |
| `tool_calls` | one or more calls, each split into the id-bearing head and an argument tail real servers send |
| `http` | a raw HTTP reply — `status` (required), `body`, `location`, `content_type` — and not one SSE event |
| `hang` | stalls for the duration, then answers as the empty-reply turn does; a cancelled request context releases it at once |
| *(none of the above)* | the empty-reply turn |

`reasoning` (the `reasoning_content` channel, streamed before the content) and `usage` accompany a
text or tool-call turn; they are refused on an `http` or `hang` turn, which never reach the
completion shape at all. `usage.cached` reaches the wire as `prompt_tokens_details.cached_tokens`
**only above zero** — an absent breakdown means "this server does not report caching" while a
present zero means "nothing was cached", and both shapes have to be scriptable.
`finish_reason` defaults to `stop`, or to `tool_calls` when the turn emits any.

### Matching

A request takes **the first unconsumed turn whose `when:` matches it**, and failing that **the
next unconsumed turn with no `when:` at all**. Ordered turns are therefore the script's spine —
request 1 takes turn 0, request 2 takes turn 1 — while a `when:` turn is an interrupt that jumps
the queue for the requests it recognises, which is what lets a fixture answer a sub-agent without
knowing where in the run its question lands. `repeat: true` keeps a turn available forever: it is
never consumed.

A `when:` block sets `last_message` (a regexp over the text of the request's last message,
whatever its role), `tool_result` (a tool NAME — resolved by following the last message's
`tool_call_id` back to the assistant turn that issued the call, because the wire shape of a tool
result carries no name), or both, in which case both must match. A `when:` that sets neither is
refused: an always-true matcher is an ordered turn written the confusing way.

**The stub is strict.** A request no turn answers is an HTTP 500 (`stubllm: no turn for request
N`) and a logged entry with `Unmatched` set — never a plausible improvised reply. A silent
fallback would turn the most interesting failure a driver test can surface, "the agent asked
something the test did not anticipate", into a green run.

### Captures

A turn can echo text the request itself carried. `captures:` is a list of `{name, from, pattern}`,
and every `{{name}}` in that turn's `text` and in each of its `tool_calls[].arguments` is replaced
by what the capture lifted out of the request the turn answers.

```yaml
model: stub-model
turns:
  - captures:
      - {name: scratch, from: system, pattern: 'scratch directory[^\n]*?(/\S+)'}
    tool_calls:
      - name: terminal
        arguments: '{"command":"mkdir -p {{scratch}}/tmp && echo ok"}'
```

This is how a fixture scripts **"the model uses exactly what it was told"**: the path in the tool
call is the one apogee announced on that very request — an orientation line, a skill header's
`files:` path — rather than one the fixture guessed and would silently stop testing the day the
announcement changed. `from: system` reads the request's system messages' text concatenated in
wire order with a newline; `from: last_message` reads the same text `when.last_message` matches.
The value is group 1 of the pattern's first match.

Captures are strict, for the reason the rest of the stub is — a fixture that quietly substituted
nothing would send `mkdir -p /tmp` and fail far from the missing announcement that caused it:

- a `pattern` that does not compile, or that has anything other than **exactly one** capture
  group, is a parse error;
- a `{{name}}` naming no capture on the same turn is a parse error, so there is no way to put a
  literal `{{x}}` on the wire;
- two captures with one name on a turn is a parse error, and `captures` on an `http` or `hang`
  turn is refused as `reasoning` is;
- a capture that matches nothing in the request is an HTTP 500 (`stubllm: capture <name>
  unmatched for request N`) and a logged entry with `Unmatched` set — and the turn is **not**
  spent, so it still answers the next request.

The recorder never writes captures: a recorded fixture is a transcript of what a real server
said, while a capture is something a test author adds.

### The request log

Every served request lands in the log, which is the stub's half of an assertion: what the agent
actually sent, in order, and which turn answered it.

- `server.Requests() []Request` — `N`, `Model`, `Messages`, `Tools`, `Stream`, `Unmatched`,
  `TurnIndex`, `At`.
- `server.LastMessage(n)` — the text of request *n*'s last message; the shortest way to assert
  what was asked.
- `server.Unmatched()` — the requests the script did not anticipate.
- `server.AssertConsumed(t)` — fails when a non-repeat turn never played, which is how a run that
  stopped early is caught by a test that would otherwise only look at the final frame.

### The binary: `cmd/stubllm`

`internal/stubllm` is a library; `cmd/stubllm` is the thin binary around it, for the two things a
human does at a command line. `make stubllm` builds it to `./stubllm` — it is a dev tool, so
`make dist` never ships it and `make build` never mentions it.

```console
$ stubllm serve --script cmd/apogee/testdata/stubllm/example.yaml --listen 127.0.0.1:8080
listening http://127.0.0.1:8080
```

`serve` plays a fixture at a real address until it is interrupted (Ctrl-C exits 0), so a fixture
can be driven by hand — `apogee --endpoint http://127.0.0.1:8080` — or by a shell test. The
`listening <url>` line is the contract: it is printed once the listener is up, which is what makes
`--listen 127.0.0.1:0` usable, because an ephemeral port has no other way of being found. A bad
command line exits **2** with the usage text; a run that started and failed — an unreadable
script, a taken port — exits **1** with just the error.

### Recording a fixture

A fixture is something you **capture**, not something you write. `record` stands between a client
and a real server, forwards `/v1/*` verbatim, and writes everything it saw as a Script:

```console
$ stubllm record --upstream http://127.0.0.1:1111 --out smoke.yaml
listening http://127.0.0.1:41234
recording http://127.0.0.1:1111 -> smoke.yaml
$ apogee --endpoint http://127.0.0.1:41234        # in another terminal: drive the run you want
^C                                                 # back here: the fixture is written on the way out
```

Each completed `/v1/chat/completions` request becomes one turn, carrying what the reply actually
did: the content and reasoning deltas re-joined, the tool-call fragments reassembled into whole
calls, the `usage` object including `cached_tokens`, the measured pacing (the **median** gap
between deltas as `token_delay`, the median delta size as `chunk_runes`), and a non-2xx reply as
an `http` turn. A non-streamed reply is recorded as a text turn with no `token_delay` — there were
no chunks to space out. `model` is taken from the first request. Every turn gets a `when:`
matcher over that request's last message, regexp-quoted, so the fixture answers the same question
in the same way even when a replayed run reorders its requests.

The medians are the point: they make one recording reproduce the server's *behaviour* rather than
one run's jitter. A recorded delay is rounded to 10 µs, because `token_delay: 10.37ms` reads as a
decision and `token_delay: 10.372413ms` reads as a mistake.

Closing the recorder waits for the replies still in flight, and that wait is load-bearing. A
streaming client stops at the `[DONE]` event and hands control straight back to its caller, while
the proxy is still one `Read` short of the EOF that files the turn — so a fixture written the
instant the last client returned would be short its last turn, with nothing in the file to say so.
`Close` holds until every begun request has filed or given up, with a two-second backstop for a
request whose reply is never coming.

Two fixtures in `cmd/apogee/testdata/stubllm/` are hand-written anyway, and say so at the top of the
file: one documents the format (`example.yaml`), and `cached-usage.yaml` reports a prefix-cache
share the recorder can only capture from a server that has prefix caching switched on. Re-record it
with `stubllm record` the day such a server is at hand — the numbers in it are what the T-06 test's
expectations are computed from, so a re-recording is a fixture change and a test change together.

Recording is always an explicit human act at a command line, never something a `go test` run does
(ADR 0062): a test that silently re-records its own fixture cannot fail. A recording a real server
answered in a shape the format refuses — content alongside tool calls, say — is still written and
then reported as an error naming the turn, because a fixture a human can fix by hand beats a
deleted one.

**Where fixtures live.** Under `cmd/apogee/testdata/stubllm/`, named `<server>-<what>.yaml` — the
server they were recorded from and the run they pin, e.g. `qwen3-smoke.yaml`. The one exception is
`example.yaml`, the hand-written worked example of the format that this document shows and that
`cmd/stubllm`'s own test serves.

## tuitest

`internal/tuitest` is the driver kit: it launches the terminal UI, types into it, and answers the
question every "manual" checklist step was really asking — *what did the terminal show?*

### Frame

The answer is a `Frame`: one immutable snapshot of a terminal, reconstructed by a real VT emulator
(`github.com/charmbracelet/x/vt`, pinned at pseudo-version
`v0.0.0-20260823001701-96af6d2cb5f6` because it holds this repo's exact `x/ansi v0.11.7`). Both
drivers — in-process and PTY — feed their bytes to the same emulator and get the same `Frame` type
back, so an assertion means the same thing whichever one produced it (ADR 0062).

One behaviour of that emulator is ours rather than its. `Screen` clamps the margins a program asks
for — DECSTBM, DECSLRM — to the size the buffer actually has. A resize is the one moment the
renderer is behind the terminal: the frame still on the wire carries the scroll region of the size
it was laid out for, `x/vt` applies that bottom margin without clamping it, and the next delete-line
then indexes past the end of a buffer that has already shrunk, panicking whichever goroutine was
painting. `Screen` registers its own CSI handlers ahead of `x/vt`'s, lowers an out-of-range margin
in place and lets the real handler apply it — the margin a real terminal would have given — so
`Screen.Resize` is safe over its whole input range, a height shrink under live output included.

```go
scr := tuitest.NewScreen(100, 30)   // the terminal the renderer paints into
defer scr.Close()                   // stops the answer pump; CheckLeaks counts it
_, _ = scr.Write(rendererBytes)

f := scr.Snapshot()
f.String()          // the whole picture, plain, trailing spaces trimmed
f.Row(3)            // one row
f.Find("Deny")      // the COLUMN and row of some text, or ok=false
f.Cell(12, 3).Width // how many columns that grapheme occupies — the emulator's answer
f.StyleRuns(3)      // the row split into maximal spans sharing one Style
f.Cursor()          // where the caret is
```

**Cells, not strings.** A `Frame` is a grid of `Cell{Style, Rune, Width}`, and every accessor above
is derived from it. That is the whole reason the emulator is in the loop: a claim about the screen
is a claim about *columns*, and a Go string does not have columns. `len("世界")` is 6, its rune count
is 2, and the terminal gives it 4 — only the last number can settle whether a box drawn beside it
lines up (T-20). The same goes for colour: `View()` returns a string with SGR sequences in it, and
asking whether the error tone is red by searching for `\x1b[31m` proves only that the renderer
*emitted* something. `StyleRuns` reports what the terminal *ended up showing*, in spans with real
bounds, after every reset, overwrite and re-paint in between.

`Style` carries the two colours and the three attributes apogee's scheme actually uses (bold,
faint, reverse). Colours compare by resolved RGBA (`tuitest.SameColor`), so an indexed colour and
the literal it resolves to are one style — a renderer may spell a colour either way and no test
should care. A nil colour is "the terminal's own default", which is a real value: leaving the
foreground alone and painting it grey are different decisions.

`Screen` also counts what it was told to do: `BytesWritten()` and `FullRepaints()` (writes carrying
a full-screen erase or a cursor-home) are the T-24 flicker proxy, and `Quiet(d)` is the settle rule
— no bytes for `d` — that a test waits on before pinning a frame. `Answers()` is the other
direction: a renderer asks the terminal questions (DA1, DECRQM for mode 2027, DSR/CPR) and the
emulator answers them, which a driver pumps back into the program's input. That pump is not
optional — an undrained answer blocks the emulator mid-write and with it the program painting into
it — so `Screen` drains it always and `Close()` is what stops the drain.

**A terminal that measures in graphemes.** By default the emulator answers the mode-2027 (Unicode
core) query with "not recognized", which is the honest answer for a terminal that has not been told
otherwise — and it leaves apogee's width authority on `ansi.WcWidth` for the whole run
(`internal/tui/width.go`). A test about wide-rune layout has to move it, and the way to move it is
the way any program moves it: write `\x1b[?2027h` into the `Screen` before the launch.

```go
drv := tuitest.NewDriver(t, size)
_, _ = drv.Screen().Write([]byte("\x1b[?2027h")) // a Ghostty, not an Apple Terminal
```

Two things follow, and a width test needs both: the emulator measures graphemes the way it measures
them, and it answers bubbletea's start-up DECRQM with "set", which moves the *painter* — and with it
apogee's authority — onto `ansi.GraphemeWidth`. Pass `--tui-diag` and the run says so in its own log
(`mode 2027: 1 (set)`, `width-method: grapheme`); `TestE2EWidthSurvivesAColourSchemeSwitch` asserts
those two lines before it asserts anything about columns, because on a terminal that never answered
the query the layout cannot move and a green test would mean nothing. The checklist says the same
thing to a human (T-20's preconditions: on such a terminal, record step 5 as *not covered* rather
than as a pass).

### Waiting

A driver test never sleeps. `WaitFor(t, cond, opts...)` polls at 20 ms against a 5 s deadline and,
when it gives up, prints the last frame *plain and styled*, so a colour bug is visible in the
failure output rather than only in a rerun. `WaitText`, `WaitGone` and `WaitQuiet` are the
shorthands almost every test uses. Options: `On(screen)` (which screen to print — pass it),
`Within(d)`, `Awaiting("the approval pane")`. Set `TUITEST_ARTIFACTS=<dir>` and a failure also
writes `<dir>/<test name>.{txt,ansi}`, for a CI run nobody is watching live.

`CheckLeaks(t)` is the other guard: called FIRST in a driver test, it fails the test if a goroutine
from `internal/tui`, bubbletea, `internal/tuitest`, `internal/filewatch` or `internal/heartbeat` is
still running 2 s after it ends. Only goroutines the test itself started: the call snapshots the
ones already running and reports the ids absent from that snapshot, so a parallel neighbour's
straggler is never charged to whichever cleanup looks next. A driver test starts real workers; the
interesting failure is not one that crashes but one that never stops and makes some *later* test
flaky.

### Goldens

`Golden(t, name, frame, redactions...)` compares `frame.String()` against
`testdata/frames/<name>.txt` in the calling package and prints a line diff on mismatch;
`go test ./cmd/apogee -update` records them. Goldens are for **rendering surfaces only** (ratified
call 13): the transcript pane, hostile rows, the fold, outcome slots, settings rows — surfaces
whose whole point is how they look. Everything else is asserted semantically with `WaitFor`,
because a golden that pins behaviour fails on every unrelated wording change and is then updated
without being read, which is worse than no test.

Redactions are not optional for a frame that carries a `t.TempDir()` path, the build version, a
session title with today's date in it, or a relative age: without them the golden churns on every
run. `Redact(pattern, with)` builds one; `-update` records the **redacted** text, so what is on
disk is what a later comparison sees. The in-process driver supplies the default set (plan item 5).

### The composition seam: `tui.Build`

Drivers are Drivers in the ADR 0031 sense — they enter through the composition, not beside it.
`tui.Run` is split for exactly that:

```go
func Build(ctx context.Context, eng Engine, br *Bridge, opts Options,
	out io.Writer, extra ...tea.ProgramOption) (*tea.Program, func(), error)
```

`Build` is everything `Run` did between `newModel` and `tea.NewProgram` — the event flush, the
`--tui-diag` log, the program options, `br.Bind` — and it stops one step short of running the
program. `Run` is now the terminal half: claim the alternate screen, `Build(…, nil)`, run, clean up.

`out` is the whole of the difference between the two callers. `nil` is the binary's path and is
byte-for-byte what it was before the split: `--tui-trace` and the Windows sync-query stripper wrap
`os.Stdout` exactly as they always have. A non-nil `out` is a driver's: the output half is skipped
entirely and `tea.WithOutput(out)` installed instead. A `--tui-trace` path alongside a driver output
is **refused**, not honoured — the trace wraps `os.Stdout`, which the driver's own output wins over,
so honouring it would hand back an empty file that looks exactly like a run that painted nothing.
The PTY driver, which does drive a real stdout, is where the trace seam is exercised.

`extra` options are appended last, so a caller's `WithInput` / `WithWindowSize` /
`WithColorProfile` / `WithEnvironment` beats anything built inside.

### In-process Driver

`tuitest.Driver` is a terminal for a Bubble Tea program running inside the test binary. It owns the
[`Screen`](#frame) the renderer paints into and the input the key parser reads, and nothing else —
what program it drives is the caller's business, which is what lets the same driver serve
`cmd/apogee`'s real composition and a three-line probe model.

```go
drv := tuitest.NewDriver(t, tuitest.Size{W: 100, H: 30})
program, cleanup, err := tui.Build(ctx, eng, br, opts, drv.Output(), drv.ProgramOptions()...)
drv.Attach(program, cancel)          // the Send target for Resize, the cancel for Kill
go func() { _, err := program.Run(); drv.Finished(err) }()
```

- `ProgramOptions()` — `WithInput` (the pipe), `WithWindowSize`, `WithoutSignals`,
  `WithoutSignalHandler`, `WithColorProfile(TrueColor)`, `WithEnvironment(TERM/COLORTERM)`. The
  OUTPUT is deliberately not among them: it is `tui.Build`'s own `out` argument, `drv.Output()`.
- `Type(text)`, `Press(key)` — bytes, not `tea.KeyPressMsg` values. The `Key` constants are the
  sequences a real xterm sends, each pinned by `TestKeysDecodeAsIntended` against a live program.
- `Resize(w, h)` — resizes the emulator, sends the `WindowSizeMsg`, and waits for the repaint.
- `Frame()`, `Screen()`, `WaitText`, `WaitGone`, `WaitQuiet`, `WaitFor` — the assertion surface.
- `Quit()` — Ctrl+C twice, then the run's error. `Kill()` — the context, and nothing tidied.
- `Done()` / `Finished(err)` — how the run's result reaches whoever is waiting for it.

Four things about it are decisions rather than details, and each of them was a bug first:

- **The input is one `os.Pipe`.** Bubble Tea wraps its input in a cancel reader, and the
  epoll-backed one is only available for an `*os.File`; anything else falls back to a reader it
  cannot cancel, costing every quit 500 ms and a leaked goroutine.
- **The output maps LF to CR LF.** With a non-tty input Bubble Tea puts the renderer in
  map-newline mode (`tea.go:1075`, a workaround for emulated ptys left in cooked mode): it then
  moves the cursor down with a bare LF *and assumes the column reset to 0*. A raw terminal does no
  such thing, so the driver is the terminal Bubble Tea thinks it is talking to — a line discipline
  with ONLCR. The PTY driver must NOT do this, which is one reason the two drivers' byte counters
  are not comparable.
- **A lone `Esc` is followed by a 70 ms gap.** No terminal can tell the Escape KEY from the start
  of an escape SEQUENCE by looking; every reader resolves it on a timeout (ultraviolet's is 50 ms).
  Press `Esc` and type `/` five milliseconds later and the program is handed one `alt+/`. This is
  the only place a driver waits on a clock instead of on the screen.
- **Teardown joins the read loop.** Ending the input is not enough: Bubble Tea's read loop parks in
  its cancel reader's `EpollWait`, and the reader is closed out from under it — a KILLED program
  skips `waitForReadLoop` altogether (`tea.go:1249-1255`) and a graceful one gives it 500 ms.
  Closing an epoll descriptor does not wake the `EpollWait` already parked on it, so a loop still
  parked when the reader closes is parked for the life of the process. `Close()` and `Kill()` end
  the input, wait for the loop to actually take the EOF, and only then let the cancel or the close
  proceed. Without the wait, the straggler is left running for the rest of the process, and
  `CheckLeaks` fails the test that stranded it rather than the driver teardown that should have
  joined it.

`cmd/apogee/e2e_support_test.go` is the half that cannot live in `internal/tuitest`, because the
launcher seam is in package `main`: `launchTUI(t, drv, stub, args...)` builds a temp home and a temp
workspace, runs `newRootCommand(launch)` with `--config` and `--workspace` pointing at them, and
returns an `e2eSession` — `Relaunch()` (same home, fresh driver), `Quit()`, `Workspace()`,
`Redactions()` (the default golden redaction set), and the screen's repaint counters.
`launchTUIConfigured(t, drv, stub, extraConfig, args...)` is the same launch with lines appended to
the home's `config.yaml` first, which is the only way to reach a **file-only** key: `delegate-max-steps`
(T-04) has no flag and no environment variable and the Agent reads it when it is CONSTRUCTED, so a
test that needs one cannot set it once the run is up.
`e2eSession.RelaunchWith(extra...)` is `Relaunch()` with arguments added for this launch and every
one after it — the shape a REOPEN takes, since `--continue` names the record the first run wrote and
so cannot be passed at the launch that creates it (T-06 step 8).
`launchTUIIn(t, drv, stub, ws, extraConfig, args...)` takes a WORKSPACE the caller built (T-12's
hostile tree names its escape in the root's own name, and a root is named when it is created), and
`launchTUIOn(t, drv, stub, home, ws, args...)` takes a HOME the caller wrote — the only way to reach
a key that sits INSIDE the `servers:` entry, since nothing appended to the file afterwards can get
in there. `llama-launcher:` is that key (T-16 step 11).

`openerLookPath` (`cmd/apogee/wire_present.go`) is the same family of seam as `tuiScheduleClock`,
`liveLauncherOps` and `configWatchTiming`: a package var that is nil in production, where
`present.Opener` falls back to `exec.LookPath`. It exists because the Opener is built inside
`presentationRungs` and installed into the TOOL layer through `livePresentation.install`, so the
launcher closure a driver enters through never sees it. A test points it at a script that appends
its argv to a log — the ratified proxy for the desktop hand-off (T-19): what an OS handler does with
a file is not observable from a test process, but WHICH program apogee handed WHICH path to is, and
that is the whole of rung 1's allow-list claim.

The worked example is `TestE2ESmokeInProcess` (`cmd/apogee/e2e_smoke_test.go`), which is checklist
item T-25 — "the one pass a human makes over the most-used path end to end" — step for step: the
first frame and its footer, a prompt answered with a tool call, an approval pane and the `a` that
takes it, the file on disk, `/settings` walked a whole lap, `/usage`, `/skills`, `/version`, a
resize narrower and wider, Ctrl+C twice, a relaunch, `/sessions`, the restored transcript, and one
more prompt that makes the record on disk grow.

Between it and `TestE2ESmokePTY` below, **checklist item T-25 is covered end to end and has no
manual step left**: steps 1–9 and 11–13 are asserted in process, step 10's "the terminal is not
left in a broken state" is the black-box test's — a property of the terminal rather than of any
frame — and the streamed-reply half that T-25 shares with T-24 (a long answer arriving, a resize
landing mid-stream, a cancel) is `cmd/apogee/e2e_stream_test.go`.

### PTYDriver

`tuitest.PTYDriver` runs the **shipped binary** under a real pseudo-terminal
(`github.com/creack/pty`) and reads it back through the same [`Screen`](#frame). Nothing about the
program under test is arranged for the test: no launcher seam, no injected program options, no
in-process anything. It is the binary, in a terminal.

```go
sess := launchPTY(t, stub)          // cmd/apogee/e2e_support_test.go
drv := sess.drv                     // *tuitest.PTYDriver
drv.WaitText("Send a message")
drv.Resize(60, 20)                  // TIOCSWINSZ + SIGWINCH, for real
code := drv.Quit()                  // Ctrl+C twice, then the child's exit status
echo, canonical := drv.TTYState()   // the line discipline the shell gets back
```

The surface mirrors the in-process driver — `Type`, `Press`, `Resize`, `Frame`, `Screen`, the four
waits — and adds the four things only a real terminal has:

- `Bytes()` — every byte the child wrote to the terminal, unconsumed. Frames are the picture; this
  is the wire, and the teardown claims are about sequences no frame can show: the alternate-screen
  release, the cursor-show, the final SGR reset.
- `TTYState()` — echo and canonical mode, read off the pty. This is "no `stty sane` needed" as a
  mechanical fact. It is read through the MASTER fd, because `pty.StartWithAttrs` closes the slave
  once the child holds it; a pty pair has one line discipline, and a mode ioctl on the master is
  answered for the pair.
- `Pid()`, `Exited()`, `Kill()` — a real pid, a real exit status, and `SIGKILL` to the child's whole
  session. `Kill` is the black-box half of "reopen after an abrupt end" (T-03); the in-process
  driver cancels a context for the other half.
- `Resize(w, h)` — `pty.Setsize` **and** an explicit `SIGWINCH`, then a wait for the repaint that
  answers it. The in-process driver sends a `WindowSizeMsg`; here the kernel does it.

**When to use it.** Colour and wide runes (the child negotiates `TERM=xterm-256color` itself), a
resize that has to be a signal, the state the terminal is left in on exit, a real pid to kill, and
anything about the binary's own argv, environment or exit code. Everything else belongs in the
in-process driver, which is roughly seven times cheaper per step.

**The binary is built once per `go test` run.** `cmd/apogee`'s existing `TestMain` — the one that
redirects `HOME` to a suite temp dir — also runs `go build -o <suiteTempHome>/apogee .`, without
`-race`, because what the black-box driver drives is the binary as it ships. The build uses the
DEVELOPER's `HOME` rather than the suite's throwaway one, or it would miss the module and build
caches and recompile the world on every run. On failure `e2eBinary` stays empty, `e2eBuildErr` says
why, and `launchPTY` skips with it: a test that cannot build the binary has not found a bug in it.
The build is unconditional for an ordinary run of the suite — one cached `go build` is cheaper than
a second thing to get wrong — with exactly one exception: `keysource_test.go` re-invokes the test
binary as an `api-key-cmd:` program several times per run, and a child that only prints a key never
drives the binary. Building there multiplies one build by every re-exec (+10 s on the package,
measured), so `TestMain` recognises the fixture's marker argument and skips it.

**The environment is stated, not inherited.** `launchPTY` gives the child `HOME` (the suite's temp
one), `PATH`, and nothing else, plus the `TERM`/`COLORTERM` the driver appends. An `APOGEE_*`
variable in the developer's shell cannot reach a driven run.

**`ptySession.Relaunch()`** starts the binary again on the same home and workspace under a fresh
pty — the black-box twin of `e2eSession.Relaunch()`, and the reopen half of every "what did the
killed run leave behind?" claim. The new run gets a trace file of its own: appending two runs'
paint into one stream would leave `TraceBytes()` answering about neither.

**The settle rule is the same** — no bytes for 150 ms means the frame is final — because it is a
property of the picture, not of how the bytes arrived. The two drivers' **byte counters are not**:
a real pty in raw mode does no newline translation and the in-process driver's output does, so a
flicker ceiling is pinned per driver.

**`--tui-trace` is exercised here and nowhere else.** `tui.Build` refuses a trace beside a driver
output (it would wrap an `os.Stdout` nothing paints into), so `launchTUI` never passes the flag and
`launchPTY` always does. `tuitest.ReplayTrace(t, path, size)` feeds the trace's quoted writes back
through a `Screen`, which gives the picture the terminal ended on and the two counters for free;
`ptySession.TraceBytes()` / `TraceFullRepaints()` are that, read fresh on every call.

`launchPTYConfigured(t, stub, extraConfig, args...)` is `launchPTY` with lines appended to the
home's `config.yaml` before the binary is spawned — the black-box twin of `launchTUIConfigured`, and
the only way a PTY run reaches a file-only key. The Console family is why it exists: it ships OFF
(ADR 0057), a `tools.enabled:` list is what lifts it, and the tool registry is built once at
startup, so a run that has Consoles is a run whose config said so before it launched (T-14).

`launchPTYWithEnv(t, stub, extraConfig, env, args...)` is the same helper with entries appended to
the child's whole environment (`ptyEnv`). It exists for the one class of setting no in-process
driver can reach: a variable the standard library reads ONCE per process. `HTTP_PROXY` /
`HTTPS_PROXY` / `NO_PROXY` are that class — `net/http.ProxyFromEnvironment` memoises the environment
on first use — and other tests in the `cmd/apogee` binary have already made requests through it, so
a `t.Setenv` there would be silently ignored and an egress test would pass without apogee ever
proxying anything. A variable of that class reaches a program only by being in its environment
before it starts, which is a child, which is this driver (T-18).

**The network a driven run is allowed to have** is `internal/tuitest/netfix.go`:
`ForwardProxy(t, routes)` is a real forward proxy on loopback with an access log, `PageServer(t,
body)` a page with a hit counter, and `MCPEcho(t)` a streamable-http MCP server exposing one `echo`
tool. Two mechanics make them enough for T-18. The proxy is a REAL one — addressed by absolute
request URI, and refusing anything else — because a fake that only counted calls would pass whether
or not apogee ever set `http.Transport.Proxy`. And its `routes` table is what lets a driven run
reach a PUBLIC destination without a packet leaving the machine: the SSRF floor refuses a private
destination in the pre-flight, before the proxy question is asked at all and with no seam in the
composition to relax it, so the destinations are public-but-unroutable literals (`240.0.0.0/4`,
reserved and in none of `floorDeniedV4Nets`) that the proxy dials loopback for. A hit counter on the
stand-in server is what makes "the redirect target got no request" a claim rather than a hope.

**Windows.** `pty.go` is `//go:build !windows`; `pty_windows.go` provides the same type with a
`t.Skip` in every entry point, so a black-box test file compiles there and skips. The driver is
deliberately unix-only — a ConPTY stand-in would be a different mechanism asserting different
things under the same test's name — and the in-process driver, which has no platform gate, keeps
Windows covered for frames, keys, waits and goldens.

The second worked example is `TestE2EStreamPTY` (`cmd/apogee/e2e_stream_test.go`): the 400-line
reply of T-24 through the binary, with a genuine `TIOCSWINSZ` and `SIGWINCH` landing while it is
still arriving, and the flicker measure read back out of the `--tui-trace` file that only this
driver can write.

The first worked example is `TestE2ESmokePTY` (`cmd/apogee/e2e_smoke_test.go`): T-25 again, through the
binary, deliberately shorter than the in-process walk — the first frame and its footer, a prompt
answered with a tool call, the approval pane and the `a` that takes it, the file on disk,
`/version`, a real SIGWINCH resize, and then step 10 as a property of the terminal: exit code 0,
the last alternate-screen sequence is the leave, the last cursor sequence is the show, the last SGR
is a reset, and echo and canonical mode are back.

## judge

`internal/judge` is the oracle for the halves a cell cannot settle: whether a pane READS right,
whether a tone carries, whether a sentence a human will act on says what it meant to. Everything
else is settled by a cell — a rubric asking "is `Fix:` on row 4" is a rubric written in the wrong
package (ADR 0062, decision 4).

### The gate

Judge calls need a model, so they are opt-in exactly as the live tests are:

| Variable | Meaning |
| --- | --- |
| `APOGEE_JUDGE_ENDPOINT` | the OpenAI-compatible server the judge talks to; **this is the gate** |
| `APOGEE_LIVE_ENDPOINT` | used as the endpoint when the judge one is unset, so one variable turns both on |
| `APOGEE_JUDGE_MODEL` | the judging model; falls back to `APOGEE_LIVE_MODEL`, then to the endpoint's first advertised model |
| `APOGEE_API_KEY` | the bearer token, when the server is keyed |

`judge.Enabled()` reports the gate and `judge.Skip(t)` skips with the line that says how to turn it
on. A plain `go test` never sets it, so an ungated run **skips** and never passes silently.
`make live-eval` sets it from `JUDGE_ENDPOINT` (default: the same `LIVE_ENDPOINT` the live tests
use) and runs `./internal/judge/` **first**, so a broken judge is reported as a broken judge rather
than as twenty rubric failures further down the run.

### A rubric, beside its test

```go
func TestE2EOutcomeSlotReadsAsOneRow(t *testing.T) {
	drv := tuitest.NewDriver(t, e2eSize)
	launchTUI(t, drv, stub)
	// … drive the TUI, then take the frame the claim is about.
	frame := drv.Frame()

	judge.Require(t, t.Context(), judge.Rubric{
		Item:     "T-15",
		Claim:    "the outcome slot reads as one row, in the tone the result deserves",
		PassWhen: "each finished step shows one outcome line whose wording matches what happened",
		FailsIf:  "an outcome line contradicts the step above it, or a failure reads as a success",
		Extra:    []string{"row 0 is the header; the run had no network"},
	}, judge.FrameArtifact("the transcript", frame, true, judge.Tone{Name: "red", Color: scheme.Error}))
}
```

`Rubric` has four fields for one reason: `PassWhen` and `FailsIf` are copied **verbatim** from the
checklist step the test replaces. A rubric paraphrased in the test drifts from the thing the
release actually promises, and the drift is invisible — both texts still read fine. `Extra` carries
what the artifacts do not: which row is the header, that the run was offline, that the model was a
stub.

**One claim per call.** Two claims in one rubric produce a verdict that cannot say which of them
failed and a reason list nobody can act on. Two claims are two `Require` calls.

### Artifacts

A verdict is rendered on named texts, never on a screenshot and never on the raw bytes:

```go
judge.Artifact{Name: "the approval pane", Kind: judge.KindFrame, Text: frame.String()}
```

`Kind` is one of `KindFrame`, `KindStdout`, `KindFile`, `KindTranscript`, `KindTrace`; the `Name` is
what a reason will point at, so `"the outcome slot"` beats `"frame2"`.
`judge.FrameArtifact(name, frame, withStyles, tones…)` serialises a `tuitest.Frame`: plain text
without styles, and with them, every style run wrapped in named tags — `⟨red⟩failed⟨/red⟩`,
`⟨bold⟩Fix:⟨/bold⟩`. The tone names come from the CALLER, because only the caller knows which
scheme is loaded; a colour no `Tone` matches is left unnamed rather than given an invented name the
rubric could then assert against.

### The API

| Call | What it does |
| --- | --- |
| `judge.Enabled() bool` | is a judge endpoint configured |
| `judge.Skip(t)` | skip with the line that says how to enable it |
| `judge.Require(t, ctx, rubric, artifacts…)` | the assertion: skips when ungated, `t.Fatal` when the judge could not answer, `t.Errorf` with the reasons when the verdict is fail |
| `judge.Ask(ctx, rubric, artifacts…) (Verdict, error)` | the verdict without the assertion, for a test that wants both directions |
| `judge.Pairwise(ctx, rubric, before, after) (Verdict, error)` | "is `after` no worse than `before` under this rubric" |
| `judge.Client(ctx) (*provider.Client, string, error)` | the same client, key and model the gate resolves, for a test that needs the judge model as an AGENT rather than as an assessor; the caller owns it and must `Close` it |

One round-trip, temperature 0, one vote. A majority of local votes costs N× the wall clock and buys
agreement rather than accuracy.

`Client` is the one exception to "the judge assesses, it does not act". `TestNewcomerFollowsTheDocs`
(checklist T-23) has to put the model in the reader's chair: it drives a `run` tool over
`docker exec` inside a clean `debian:stable-slim` container that holds ONLY `README.md`,
`docs/manual/` and one release archive, for at most twenty steps, and the model's report of what did
not work as written is then handed back to `Require` for the actual verdict. Both gates must be
open — `docker` on PATH and a judge endpoint — and the test is outside the suite's wall-clock budget.

Use `Pairwise` where no absolute oracle exists but a comparison does — "nothing regressed since
v0.17.1", "the reworded pane is no worse than the shipped one". Give it the two artifacts and let
the rubric describe the property; a difference that is not *worse* is a pass.

### The verdict is binding

With the gate set, a `fail` **fails the Go test** and prints the model's reasons. An advisory judge
is a judge nobody reads. Two consequences follow, and both are deliberate.

**A weak local judge is a reason to sharpen the rubric, not to soften the verdict.** If a small
model cannot settle a claim, the claim is usually under-specified — say which row, say what "reads
right" means here, move the mechanical half into a cell assertion and leave the judgment half to
the judge. Every one of those edits makes the test better; making the verdict advisory makes it
disappear.

**A single fail is not yet a fail.** Temperature 0 on a local server is *not* bit-reproducible —
the sampler seed and the batch the request lands in both move the sampled token — so a judge
failure is re-run ONCE by hand before it is believed:

```
go test -run TestE2EOutcomeSlotReadsAsOneRow -count=1 -v ./cmd/apogee/
```

Two fails in a row are a real fail. The failure message says so itself, because the person reading
it is mid-release. `-count=1` is load-bearing: the model behind the endpoint is not a Go-visible
input, so a cached PASS would survive a model swap.

`TestJudgeSelfCheck` is the kit's own probe of the configured judge — the rubric "the text says
hello" put to the real model against `hello` and against `goodbye`. A judge that passes both, or
fails both, is reported there.

## Which driver observes which claim

This table is the answer to "how do I test this?" — and it is also the gate on the word *manual*.
A claim is manual ONLY when its class sits in the **Not observable** column; everything else has a
driver, and the test gets written. The rows below cover every claim class
`docs/test-checklists/2026-08-27 - 00 - since-v0.17.1.md` needed a human for, including the proxies
ratified for the irreducible halves (ADR 0062, decision 5). Every example-test name in it was
re-checked against `go test -list` on 2026-08-28 and resolves to a real function.

Read the **Not observable** column knowing it carries two kinds of cell, and that neither kind is
an open gap.

A **pointer** cell names the instrument that asserts the claim INSTEAD of the driver on that row —
the session record, the stub's request log, a unit test. The claim is covered, just not where the
row's own driver could see it: a hostile listing's rows read out of the stub's request log (T-12),
the record standing as the completeness authority behind a paged walk (T-24), the 2 MiB body cap
held in `internal/tools/network_funnel_test.go` (T-18), the moved session's re-follow covered in
`upstream_test.go` (T-16).

A **limit** cell names something the suite has decided not to chase, together with the proxy it
pins instead. Whether a deliberate press is *felt* (T-13 — a millisecond ceiling on the paint is
the proxy); whether the reader's own terminal theme renders a role legibly (T-15 — the assertion is
on the scheme's role, never on a colour); traffic to a real remote host, and what a third-party MCP
server does with a call (T-18 — nothing in the suite leaves loopback, so the proxies are "no
loopback traffic went through the proxy" and an in-test server of the shape `internal/mcp`'s own
fixture uses); whether the user's shell prompt looks right after exit (T-25 — the tty state read
back through `PTYDriver.TTYState()`); the Landlock residual itself (T-11 — the negative direction
on a modern kernel); a live tool-capable endpoint (T-22 — the env-gated `make live-eval` run); and
whether the prose READS well (T-23 — a grep pins the claim, not the sentence around it). Each of
those is an accepted limit, and its cell says so.

Four of the limit cells are irreducible, the claim leaving the machine altogether: font tofu
(T-20), felt flicker (T-24), what a real desktop application does with the file (T-19), and
`brew upgrade` before its release exists together with the newcomer walk's Homebrew and OpenRouter
steps (T-21, T-23). Those four are recorded in `ISSUES.md` under "Test drivers — residue" as
accepted proxies with no open work.

| Claim class | Driver | Example test | Not observable by any driver |
| --- | --- | --- | --- |
| Stream order and completeness (T-24) | in-process — the committed transcript, read from the frame and from the session record; stubllm sets the chunking (`ChunkRunes`, `TokenDelay`) | `TestE2EStreamCommitsCompleteAndInOrder` | Every line of a long answer by ⇞ paging alone: the sticky prompt header covers the top row of each window (layout.md), so a page-at-a-time walk leaves one line per window unseen. The record is the completeness authority; the walk's own claim is that no *run* of lines is missing |
| Streamed text belongs to one block (T-24) | in-process — the session record's own `depth` and `spawnCallID` per entry, with the frame as the second reading; each child streams a marker word the other never uses | `TestE2EDelegationsStreamIntoTheirOwnBlocks` | — |
| A cancelled reply (T-24) | in-process — the frame at the moment of the cancel, and the record afterwards for "the next prompt starts a new entry" | `TestE2EStreamCancelKeepsWhatArrived` | — |
| Pane and block text, and its wording (T-04, T-10, T-12, T-13) | in-process — `Frame.Find` / `Frame.Row` for the text, `Golden` for a whole surface, a judge rubric for the wording half only | `TestE2EDelegationStepCap`; `TestE2EApprovalForcesALookAtTheControlPlane`; `TestE2EHostileWrapsUnderItsOwnIndent`; `TestJudgeForcedApprovalPaneReadsAsHelp` | — |
| Untrusted text on a rendering surface (T-12) | in-process — the fixture is hostile file and skill names on a REAL filesystem, and the claim is made on CELLS: `StyleRuns` for the colour a leaked sequence would have painted, row indents for the rows a section authored, `Golden` for the block. `apogee probe`'s report is asserted on its bytes | `TestE2EHostileSurfacesKeepTheirOwnRows`; `TestE2EHostileProbeKeepsItsOwnRows`; `TestJudgeHostileRowsReadAsOneRow` | A listing's own rows: the transcript paints a `list_dir`/`grep`/`find_files` block's target and outcome slot and never its result body, so the rows are the ones the MODEL is handed and are asserted out of the stub's request log (`TestE2EHostileToolResultsKeepOneRowPerEntry`) |
| Token accounting across surfaces (T-06) | in-process — the `/usage` pane's rows and the `/sessions` spend cell, split back into cells and asserted against the counts the FIXTURE told the server to report; stubllm's `usage:` carries the `cached` share that no live server can be relied on to produce | `TestE2EUsageReportsCachedTokensAndDelegateSpend`; `TestE2EUsageHidesTheCachedColumnWithoutABreakdown` | — |
| A decision key that arrives too early (T-13) | PTY only — the key is written into the terminal from the moment the prompt is sent until the pane is on screen, and the `--tui-trace` file is the evidence that the pane was PAINTED before anything removed it; an in-process run has no trace (item 4) | `TestE2EApprovalKeysAreArmedAfterPaint` | Whether the latency of a deliberate press is *felt* — the test pins a ceiling in milliseconds instead |
| Colour and tone of a run (T-15) | PTY for the real terminal's SGR, or in-process `Frame.StyleRuns` — assert the run, never the raw escape. The scheme's own role is the comparand (`scheme.Default().Error`), so "painted red" means the failure role and not any red | `TestE2EOutcomeTonePTY`; `TestE2EOutcomeSlotsCarryTheToolsVerdict`; `TestE2EOutcomeCancelledDelegationCarriesTheFailureTone` | Whether the reader's own terminal theme renders that colour legibly |
| A diff body's elision rule (T-15) | in-process — the rule is a ROW that is nothing but `⋯`, counted across a paged walk of the expanded block, since a twelve-line file's diff is taller than the terminal. The leader every tool row runs to its outcome slot is the same glyph and is not a rule: it has text either side of it | `TestE2EOutcomeSlotsCarryTheToolsVerdict` | — |
| The rung, the blast radius and the rows that read them (T-16) | in-process — the footer's mode marker (`footerRow` + `StyleRuns` for the `unconfined` tone), the `/confine` notes, and the `/settings` `mode` and `confine-to-workspace` rows walked to by registry path. The word the host earns is read from `apogee probe`, never assumed | `TestE2ELiveStateFollowsTheRunningSession` | — |
| A launcher profile move (T-16) | in-process — the package var `liveLauncherOps` takes the bridge's own `fakeLauncher` behind the REAL wiring, so the picker's rows, the load and the rebind through `sessionMover` all run; a second stubllm server IS the profile's address, and its request log is what says the session moved | `TestE2ELiveStateLauncherMoveKeepsTheSessionWorking` | More than one delegation live at once after the move: the fan-out width resolves from the entry's pin and the server's `total_slots`, and stubllm answers no `/props`, so a moved session's cap is 1. The re-follow itself is unit-covered (`upstream_test.go`) |
| Wide-rune and glyph alignment (T-20) | `tuitest` — the emulator's cell width is the authority; assert `Frame` cells, never rune counts. A layout claim needs a terminal in Unicode-core mode (see *Frame*) or the two measures cannot disagree | `TestE2EWidthTicksMultiSelectChoices`; `TestE2EWidthSurvivesAColourSchemeSwitch` | Font tofu — whether the reader's font has the glyph at all |
| Resize and reflow (T-24, T-25) | in-process `Driver.Resize` (emulator resize + a `tea.WindowSizeMsg`, then a wait for the repaint it caused); PTY `PTYDriver.Resize` = `pty.Setsize` + a real `SIGWINCH` | `TestE2EStreamPTY` | — |
| tty state on the way out (T-25) | PTY only — `PTYDriver.TTYState()` after `Quit()`: echo and canonical mode restored, no dangling SGR, exit code 0 | `TestE2ESmokePTY` | Whether the user's own shell prompt looks right afterwards — no shell runs inside the PTY |
| Real process lifetime, SIGKILL, exit code (T-03, T-07, T-14) | PTY only — `PTYDriver.Pid()`, `Kill()` (a real `SIGKILL`), `Exited()`, and the same for a Console's own children; the in-process driver has no process to kill | `TestE2EDelegationRecordSurvivesSIGKILL`; `TestE2EConsolesDieWithTheirOwner` | — |
| Session record on disk (T-03, T-06, T-19) | either driver — read the run's own `sessions/` records under its temp home; the record, not the frame, is the authority on what was persisted | `TestE2ESmokeInProcess` | — |
| What a killed run left behind (T-03) | either driver — a stubllm turn that `hang`s holds the run mid-delegation for as long as the test needs, then `Kill()` and a relaunch on the SAME home; the reopened frame is where "closed as interrupted" is read | `TestE2EDelegationRecordSurvivesAKill`; `TestE2EDelegationRecordSurvivesSIGKILL` | — |
| A scheduled Firing and its block (T-07) | in-process — the package var `tuiScheduleClock` (the daemon's `daemonClock`, one Driver over) puts the Scheduler on a clock the test ticks, so the thirty-second `MinCycle` floor costs a microsecond | `TestE2EFiringMarksAnAbandonedFinalTurn` | — |
| Config file watch and live apply (T-16) | in-process — write the key into the run's temp-home `config.yaml` and wait for the transcript line; the package var `configWatchTiming` shortens the poll to 50 ms for every driven launch, so a watcher step costs a tenth of a second rather than the production second-and-a-quarter | `TestE2ELiveStateFollowsTheRunningSession` | — |
| Daemon and headless output (T-07) | no driver needed — run the binary against stubllm and assert stdout, the record and the exit code | `TestHeadlessExitCodes`; `TestDaemonFaultedVerbColumn` | — |
| Network egress: proxy honoured, url-safety live, a stream nothing deadlines (T-18) | PTY only — the proxy variables reach a program only in the environment it STARTS with (`launchPTYWithEnv`); an in-test forward proxy with a route table onto loopback servers (`internal/tuitest/netfix.go`) is the instrument, and its access log is the evidence. The 2 MiB body cap is the one claim of this row that stays unit-covered (`internal/tools/network_funnel_test.go`): a driven run would have to carry two megabytes through the conversation to say the same thing | `TestE2EEgress`; `TestE2EEgressLongStreamIsNotDeadlined` | Traffic to a real remote host — nothing in the suite leaves loopback, so a `NO_PROXY` host dialling DIRECT has no hermetic form; what is asserted instead is that no loopback traffic, the model conversation included, ever went through the proxy |
| MCP server behaviour (T-18) | an in-test streamable-http MCP server with one `echo` tool (`tuitest.MCPEcho`, the shape `internal/mcp`'s own fixture uses) reached at a proxied endpoint; the refused reconnect is read off the tool that STILL answers under its old alias, and the denied ENDPOINT off the raw pty stream of a launch that never reached a frame | `TestE2EEgress`; `TestE2EEgressDeniedMCPEndpointStopsTheLaunch` | What a third-party MCP server actually does with a call |
| Flicker during streaming (T-24) | `--tui-trace` counters: bytes written and full-frame repaints per streamed token, pinned against a ceiling | `TestE2EStreamRepaintCeiling` | Felt flicker — the repaint ceiling is the accepted proxy |
| Desktop hand-off (T-19) | a logging fake opener installed through the `openerLookPath` package var, which is what `present.Opener.LookPath` resolves to; assert argv and wording. The refused half is the log's ABSENCE of a launch | `TestE2EPresentOpensOnlyTheAllowedFormats`; `TestE2EPresentServesWithoutLeakingTheToken` | What a real desktop application does with the file |
| Upgrade path of an installed apogee (T-21) | post-release `make release-smoke` — archives, `SHA256SUMS`, `--version`, `brew upgrade` when `brew` is present | `make release-smoke` | `brew upgrade` before the release it upgrades to exists |
| Tag job and action pins (T-21) | `actionlint` plus `scripts/check-pins.sh`, both run from `make check` | `make check` | — |
| Landlock residual honesty on an older ABI (T-11) | the negative direction only — the `ubuntu-latest` check job asserts `probe` discloses no residual on a modern kernel | CI job `check` | The residual itself: it needs a 5.13–6.1 kernel, and GitHub offers no runner in that window (the `ubuntu-22.04` image runs the 6.8 HWE kernel) |
| Behaviour against a real model (T-22) | stays env-gated under `make live-eval`; unset means skip, never a silent pass | `TestE2ELiveModel` | — (needs a live tool-capable endpoint) |
| A claim the manual makes about the environment (T-23) | no driver needed — read the manual's own section and the source that ANSWERS it. The read set is `internal/config`'s `Env… = "APOGEE_…"` constants widened by whatever `cmd/apogee` spells out; the drift is asserted in BOTH directions, and the section's own count word with it | `TestManualListsEveryEnvironmentOverride` | — |
| A documented flag, root or refusal (T-23) | the shape that owns the claim: headless for a value that cannot parse, PTY for the hidden `--tui-trace`/`--tui-diag` files (an in-process launch supplies its own output and `tui.Build` refuses a trace beside it), in-process for `APOGEE_CONFIG`/`APOGEE_WORKSPACE` — launched by hand, since every helper passes both as FLAGS and refuses the variables | `TestDocsEnvBadValuesNameTheVariableAndTheValue`; `TestDocsEnvTraceFlagsWriteFilesAndStayOutOfHelp`; `TestDocsEnvRootsMoveTheHomeAndTheFence`; `TestDocsEnvURLSafetyProseIsLiveAndCoversMCP` | Whether the prose READS well — a grep pins the claim, not the sentence around it |
| The docs work for a newcomer (T-23) | judge-driven agent in a clean container with only `README.md` + `docs/manual/`, driving apogee against stubllm | `TestNewcomerFollowsTheDocs` | The Homebrew and OpenRouter steps — they need a published release and a real API key |

## Writing a new e2e test

A new end-to-end test is `cmd/apogee/e2e_<topic>_test.go`, and it follows this checklist.

1. **Pick the driver from the table above.** In-process unless the claim is about a real terminal,
   a real process, or the tty state on the way out.
2. **Script stubllm; never write a handler.** The fixture goes in `cmd/apogee/testdata/stubllm/`.
   Match on `when:` rather than on order wherever the conversation has a shape — apogee makes calls
   of its own (the session title, whatever a Mechanism needs), and an ordered script silently hands
   one of them the turn meant for the user's prompt. End the fixture with one `repeat: true` turn
   for those, and let `AssertConsumed` prove the scripted turns fired.
3. **Own the home and the workspace.** `launchTUI` — and `launchPTY` for the black-box driver —
   gives the run a `t.TempDir()` home and a `t.TempDir()` workspace and passes both as flags; both
   refuse to run with `APOGEE_CONFIG` set. No test may read or write the real `~/.apogee`.
4. **Wait for the thing, never for a duration.** `WaitText` / `WaitGone` / `WaitFor` are bounded and
   print the failing frame. A settle (`WaitQuiet`) is for a frame you are about to READ — and only
   ever after a content wait, because a screen that has not started painting yet is quiet too.
5. **Assert on cells.** `Frame.Find`, `Frame.Row`, `Frame.Cell(...).Width`, `Frame.StyleRuns`. A
   `Golden` is for a rendering surface whose whole layout is the claim, and always with
   `sess.Redactions()...` — a golden carrying a temp path or today's date churns on every run. The
   footer needs one redaction of its own on top: it truncates the workdir cell from the left to the
   room it has and drops the mode marker beside it when there is none, so how long the machine's
   temp root happens to be decides what survives on that row (`goldenRedactions`,
   `cmd/apogee/e2e_delegation_test.go`).
6. **Open a block before asserting inside it.** A delegation and a firing block are COLLAPSED by
   default (layout.md): the child's cards, its error lines and the result handed back to the parent
   are all elided until the block is opened, and a test asserting on them against the default paint
   asserts on nothing. `⌥↑` enters the block cursor on the last stop and `⏎` toggles it
   (`expandLastBlock`) — then LEAVE the cursor with Esc, because while it is active every repaint
   re-anchors the view on the line it stands on and a `⇟` scrolls only to be yanked back. An
   expanded block is usually taller than the terminal, so a line near its end is read by walking
   pages (`scrollTranscript`), waiting for each press to actually PAINT before reading the next.
7. **Close what you open.** Press Esc and then WAIT for the pane to be gone. A test that carries on
   after a pane that never closed types its next command into that pane and fails somewhere else.
8. **Add a judge rubric only for a judgment half** — wording, tone, "reads as one row". Everything
   a cell can settle is settled by a cell.
9. **Stay inside the budget below.** Every wait bounded, no `t.Parallel` (the e2e tests use
   `t.Setenv` and package-var seams), and every swapped package var restored via `t.Cleanup`.

## Gates and budgets

**Measured 2026-08-28, the final pass over the whole kit.**
`go test -race -count=1 -run 'TestE2E' ./cmd/apogee/` — **36 tests, all PASS, 121.7 s** of package
wall clock (**89.3 s** without `-race`). Roughly 120 s of that is test time; the rest is the one-off
`go build` every run of the package now pays. Per file, under `-race`:

| File | s | File | s |
| --- | --- | --- | --- |
| `e2e_egress_test.go` | 31.6 | `e2e_console_test.go` | 5.8 |
| `e2e_stream_test.go` | 20.7 | `e2e_width_test.go` | 5.4 |
| `e2e_livestate_test.go` | 11.2 | `e2e_hostile_test.go` | 5.2 |
| `e2e_outcome_test.go` | 9.4 | `e2e_usage_test.go` | 4.9 |
| `e2e_smoke_test.go` | 8.5 | `e2e_approval_test.go` | 4.6 |
| `e2e_delegation_test.go` | 8.3 | `e2e_present_test.go` | 3.2 |
|  |  | `e2e_schedule_test.go` | 1.4 |

`e2e_egress_test.go` is three-quarters one test — `TestE2EEgressLongStreamIsNotDeadlined`, **25.7 s**,
whose twenty-five seconds ARE the claim (see below).

| Set | Runs in a plain `go test` | Ways it does not run |
| --- | --- | --- |
| in-process e2e | yes, every platform | — |
| PTY e2e | yes | Windows; a failed `TestMain` build (both say so in the skip) |
| judge | no | always, unless `APOGEE_JUDGE_ENDPOINT` / `APOGEE_LIVE_ENDPOINT` is set — it joins `make live-eval` |
| newcomer container | no | needs BOTH `docker` on PATH and the judge gate, and Linux (`--network host` shares loopback there only); local-only, outside the budget |
| workflow gates | not a Go test | `scripts/check-pins.sh` and `actionlint` run from `make check` and from the CI check job |
| release smoke | not a Go test | `make release-smoke VERSION=vX.Y.Z`, by hand, only once the release is published |

No build tags and no `-short`: a test that only runs when someone remembers a flag is a test nobody
runs. The in-process e2e set runs in **every plain `go test`**, on every platform, with no gate at
all — it needs no terminal, no model and no network beyond loopback.

The PTY set runs in every plain `go test` too, and has exactly two ways not to run, both of which
say so: it skips on Windows (the driver is unix-only) and it skips when `TestMain` could not build
the binary, with the build error in the skip message. It needs no terminal of its own — it makes
one — and no model beyond the same loopback stub.

The e2e tests are **serial**: they use `t.Setenv` and package-var seams, so none of them calls
`t.Parallel`, and every swapped package var is restored through `t.Cleanup`. The budget is therefore
serial wall clock, and the measurement above is the whole of it: **≈ 122 s** under `-race`, on top
of the 5.5 s the package cost before this work. That is well past the ~15 s the kit's first slice
budgeted for itself, and the excess is not waste — it is nine later sets, each measured and narrated
below, plus one test whose twenty-five seconds are its assertion. Nothing has been moved behind an
env flag to buy the number down, and the rule two paragraphs up is why: a test that runs only when
someone remembers a flag is a test nobody runs. Gating the single slowest one would delete the only
observation that the provider client sets no response-wide timeout and still leave ~96 s. The knobs
that genuinely trade time for fidelity are named per set below, and the first to reach for is the
streamed fixture's `chunk_runes`. The per-set figures below were each taken when that set landed;
the table above is the authority when they disagree. `TestE2ESmokeInProcess` — thirteen checklist steps,
two launches and a restore — measures **≈ 7.5 s** of that under `-race`, and `TestE2ESmokePTY`
**≈ 1 s** on top of the one-off `go build` (**≈ 1.5 s**) every run of the package now pays.

A **streamed** e2e test is the expensive kind, and the reason is arithmetic rather than waste: a
fixture that streams the checklist's 400-line answer three runes at a time with a millisecond
between deltas spends ~2 s of scripted delay before the composition and the renderer are paid for
at all, and ~5 s of wall clock under `-race`. The T-24 set (`cmd/apogee/e2e_stream_test.go`) runs
three near-complete passes over that answer — in process, through the pty, and once more for the
repaint ceiling — and measures **≈ 20 s** under `-race`, ≈ 16 s without it. Two knobs trade
fidelity for time if that becomes the package's problem: the fixture's `chunk_runes` (3 is what cuts
most lines mid-word, which is the point of the fixture) and how far into the answer the ceiling
test's last mark sits.

`TestE2EEgressLongStreamIsNotDeadlined` is the one test in the package that is deliberately allowed
past that norm: it measures **≈ 26 s**, and the twenty-five of them are the claim. The provider
client sets no client-level `Timeout` because `http.Client.Timeout` bounds the whole response
INCLUDING the body and so would cut a healthy stream at its own duration — a bound that is only
absent in a comment is a bound somebody re-adds — and the only way to observe its absence is a
stream that outlives every plausible value of it. It stands as its own test rather than inside
`TestE2EEgress` (**≈ 5 s** on top of the shared build) so that cost is attributable and skippable by
name.

Two mechanics belong with those tests. **Resize mid-stream changes the WIDTH only.** Reflow is a
claim about width, so width is what the claim needs — but the resize started out width-only for a
second reason, a fragility of the emulator underneath both drivers: a scroll region the program set
straight after the resize (DECSTBM, which bubbletea uses for its scrolling area) could name a row
past the end of a buffer that had already shrunk, and `x/vt` indexed it without clamping. `Screen`
now clamps it at the source ([Frame](#frame)), so the height shrink is safe and the width-only
resize rests on the claim alone. T-25's own 60×20 resize is asserted where the session is idle and
nothing is in flight. And **a
live frame is asserted by waiting, not by snapping**: a repaint takes more than one write, so any
single snapshot of a streaming terminal may catch a row half-written. What a reflow must not do is
LEAVE one, so the assertion is that the picture converges within a bounded wait
(`waitWholeStreamRows`) rather than that it was never briefly torn.

The T-03/T-04/T-07 set (`e2e_delegation_test.go`, `e2e_schedule_test.go`) measures **≈ 9.5 s**
under `-race` across five launches, and two of its costs are worth naming because every later block
test inherits them. Reading a line near the END of an expanded block means walking pages, and each
page costs a settle; and the walk only learns it has reached the bottom by pressing once more and
finding nothing painted, which is a bounded half-second per walk. A `hang` turn costs nothing — the
stub sleeps until the request's context ends, and killing the program ends it.

The T-06/T-10/T-13 set (`e2e_usage_test.go`, `e2e_approval_test.go`) measures **≈ 9.5 s** under
`-race` across six launches, and its one avoidable cost is worth naming: a subprocess that actually
RUNS under Auto's confinement box costs about five seconds under the race detector, so the Auto half
of T-10 asserts the rung from the footer's mode marker and cancels its pane rather than running the
command. Where a test needs the same prompt more than once — T-13 sends one line five times — one
exchange is told from the next by waiting for the prompt box's idle placeholder, never by waiting
for reply TEXT, which the exchange before it already put on the screen.

The T-12/T-20 set (`e2e_hostile_test.go`, `e2e_width_test.go`) measures **≈ 12 s** under `-race`
across six launches, and the settings pane is most of it: a value two-thirds of the way down the key
registry is reached one arrow at a time, each press waiting for the selection to move, and the width
item opens that pane twice (there and back, T-20 step 7).

The T-15/T-16 set (`e2e_outcome_test.go`, `e2e_livestate_test.go`) measures **≈ 25 s** under
`-race` across seven launches, and the settings pane is again most of it — which is why this set
walks the key list DOWNWARDS (`settingsGoDown`): the rows it wants sit near the top, and the shared
`settingsGoTo` walks up for rows that sit near the bottom. One cost here is not avoidable: the
verdict a CANCELLED delegation's outcome slot carries is written by the replay that closes it, so the
claim costs a relaunch and a `/sessions` restore.

Two rules keep it there. Every wait is a bounded `WaitFor` (5 s default) on a condition, never a
sleep; and a settle is 150 ms of no bytes, taken only when a frame is about to be READ. The second
rule has a corollary worth stating, because it is the quiet way a driver test lies: **`WaitQuiet` is
not a wait for a keypress to land**. The screen has been quiet since before the key was sent, so the
check passes at once, on a frame the program has not answered yet. Wait for the paint the key caused
(`awaitRepaint`, or a `WaitText` on what the key was supposed to produce) and settle after that.
