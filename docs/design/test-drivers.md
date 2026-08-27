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

### Waiting

A driver test never sleeps. `WaitFor(t, cond, opts...)` polls at 20 ms against a 5 s deadline and,
when it gives up, prints the last frame *plain and styled*, so a colour bug is visible in the
failure output rather than only in a rerun. `WaitText`, `WaitGone` and `WaitQuiet` are the
shorthands almost every test uses. Options: `On(screen)` (which screen to print — pass it),
`Within(d)`, `Awaiting("the approval pane")`. Set `TUITEST_ARTIFACTS=<dir>` and a failure also
writes `<dir>/<test name>.{txt,ansi}`, for a CI run nobody is watching live.

`CheckLeaks(t)` is the other guard: called FIRST in a driver test, it fails the test if a goroutine
from `internal/tui`, bubbletea, `internal/tuitest`, `internal/filewatch` or `internal/heartbeat` is
still running 2 s after it ends. A driver test starts real workers; the interesting failure is not
one that crashes but one that never stops and makes some *later* test flaky.

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

Three things about it are decisions rather than details, and each of them was a bug first:

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

`cmd/apogee/e2e_support_test.go` is the half that cannot live in `internal/tuitest`, because the
launcher seam is in package `main`: `launchTUI(t, drv, stub, args...)` builds a temp home and a temp
workspace, runs `newRootCommand(launch)` with `--config` and `--workspace` pointing at them, and
returns an `e2eSession` — `Relaunch()` (same home, fresh driver), `Quit()`, `Workspace()`,
`Redactions()` (the default golden redaction set), and the screen's repaint counters.

The worked example is `TestE2ESmokeInProcess` (`cmd/apogee/e2e_smoke_test.go`), which is checklist
item T-25 — "the one pass a human makes over the most-used path end to end" — step for step: the
first frame and its footer, a prompt answered with a tool call, an approval pane and the `a` that
takes it, the file on disk, `/settings` walked a whole lap, `/usage`, `/skills`, `/version`, a
resize narrower and wider, Ctrl+C twice, a relaunch, `/sessions`, the restored transcript, and one
more prompt that makes the record on disk grow.

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

**The settle rule is the same** — no bytes for 150 ms means the frame is final — because it is a
property of the picture, not of how the bytes arrived. The two drivers' **byte counters are not**:
a real pty in raw mode does no newline translation and the in-process driver's output does, so a
flicker ceiling is pinned per driver.

**`--tui-trace` is exercised here and nowhere else.** `tui.Build` refuses a trace beside a driver
output (it would wrap an `os.Stdout` nothing paints into), so `launchTUI` never passes the flag and
`launchPTY` always does. `tuitest.ReplayTrace(t, path, size)` feeds the trace's quoted writes back
through a `Screen`, which gives the picture the terminal ended on and the two counters for free;
`ptySession.TraceBytes()` / `TraceFullRepaints()` are that, read fresh on every call.

**Windows.** `pty.go` is `//go:build !windows`; `pty_windows.go` provides the same type with a
`t.Skip` in every entry point, so a black-box test file compiles there and skips. The driver is
deliberately unix-only — a ConPTY stand-in would be a different mechanism asserting different
things under the same test's name — and the in-process driver, which has no platform gate, keeps
Windows covered for frames, keys, waits and goldens.

The worked example is `TestE2ESmokePTY` (`cmd/apogee/e2e_smoke_test.go`): T-25 again, through the
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

One round-trip, temperature 0, one vote. A majority of local votes costs N× the wall clock and buys
agreement rather than accuracy.

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
ratified for the irreducible halves (ADR 0062, decision 5). Plan item 16 re-checks every
example-test name against `go test -list 'TestE2E.*' ./cmd/apogee/`; names marked *(planned)* do
not exist yet, with the plan item that writes them.

| Claim class | Driver | Example test | Not observable by any driver |
| --- | --- | --- | --- |
| Stream order and completeness (T-24) | in-process — the committed transcript, read from the frame and from the session record; stubllm sets the chunking (`ChunkRunes`, `TokenDelay`) | `TestE2EStreamCommitsCompleteAndInOrder` *(planned, item 9)* | — |
| Pane and block text, and its wording (T-04, T-10, T-12, T-13) | in-process — `Frame.Find` / `Frame.Row` for the text, `Golden` for a whole surface, a judge rubric for the wording half only | `TestE2EApproval…` *(planned, item 11)* | — |
| Colour and tone of a run (T-15) | PTY for the real terminal's SGR, or in-process `Frame.StyleRuns` — assert the run, never the raw escape | `TestE2EOutcomeTonePTY` *(planned, item 13)* | Whether the reader's own terminal theme renders that colour legibly |
| Wide-rune and glyph alignment (T-20) | `tuitest` — the emulator's cell width is the authority; assert `Frame` cells, never rune counts | `TestE2EWidth…` *(planned, item 12)* | Font tofu — whether the reader's font has the glyph at all |
| Resize and reflow (T-24, T-25) | in-process `Driver.Resize` (emulator resize + a `tea.WindowSizeMsg`, then a wait for the repaint it caused); PTY `PTYDriver.Resize` = `pty.Setsize` + a real `SIGWINCH` | `TestE2EStreamPTY` *(planned, item 9)* | — |
| tty state on the way out (T-25) | PTY only — `PTYDriver.TTYState()` after `Quit()`: echo and canonical mode restored, no dangling SGR, exit code 0 | `TestE2ESmokePTY` | Whether the user's own shell prompt looks right afterwards — no shell runs inside the PTY |
| Real process lifetime, SIGKILL, exit code (T-03, T-07, T-14) | PTY only — `PTYDriver.Pid()`, `Kill()` (a real `SIGKILL`), `Exited()`, and the same for a Console's own children; the in-process driver has no process to kill | `TestE2EDelegationRecordSurvivesSIGKILL` *(planned, item 10)*; `TestE2EConsole` *(planned, item 14)* | — |
| Session record on disk (T-03, T-06, T-19) | either driver — read the run's own `sessions/` records under its temp home; the record, not the frame, is the authority on what was persisted | `TestE2ESmokeInProcess` | — |
| Config file watch and live apply (T-16) | in-process — write the key into the run's temp-home `config.yaml` and wait for the transcript line; `configWatchTiming` shortens the poll | `TestE2ELiveState…` *(planned, item 13)* | — |
| Daemon and headless output (T-07) | no driver needed — run the binary against stubllm and assert stdout, the record and the exit code | `TestHeadlessExitCodes`; `TestDaemonFaultedVerbColumn` *(planned, item 10)* | — |
| Network egress: proxy honoured, url-safety, bounded bodies (T-18) | an in-test Go forward proxy plus stubllm as the upstream (`internal/tuitest/netfix.go`) — assert what reached the proxy | `TestE2EEgress` *(planned, item 14)* | Traffic to a real remote host — nothing in the suite leaves loopback |
| MCP server behaviour (T-18) | an in-test `httptest` MCP server (the shape `internal/mcp/transport_test.go` already uses) | `TestE2EEgress` *(planned, item 14)* | What a third-party MCP server actually does with a call |
| Flicker during streaming (T-24) | `--tui-trace` counters: bytes written and full-frame repaints per streamed token, pinned against a ceiling | `TestE2EStreamRepaintCeiling` *(planned, item 9)* | Felt flicker — the repaint ceiling is the accepted proxy |
| Desktop hand-off (T-19) | a logging fake opener installed through `present.Opener.LookPath`; assert argv and wording | `TestE2EPresent…` *(planned, item 14)* | What a real desktop application does with the file |
| Upgrade path of an installed apogee (T-21) | post-release `make release-smoke` — archives, `SHA256SUMS`, `--version`, `brew upgrade` when `brew` is present | `make release-smoke` *(planned, item 15)* | `brew upgrade` before the release it upgrades to exists |
| Tag job and action pins (T-21) | `actionlint` plus `scripts/check-pins.sh`, both run from `make check` | `make check` *(planned, item 15)* | — |
| Landlock residual honesty on an older ABI (T-11) | a dedicated `ubuntu-22.04` CI job running the probe assertions | CI job `landlock-abi-1-2` *(planned, item 15)* | — |
| Behaviour against a real model (T-22) | stays env-gated under `make live-eval`; unset means skip, never a silent pass | `TestE2ELiveModel` | — (needs a live tool-capable endpoint) |
| The docs work for a newcomer (T-23) | judge-driven agent in a clean container with only `README.md` + `docs/manual/`, driving apogee against stubllm | `TestNewcomerFollowsTheDocs` *(planned, item 15)* | The Homebrew and OpenRouter steps — they need a published release and a real API key |

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
   `sess.Redactions()...` — a golden carrying a temp path or today's date churns on every run.
6. **Close what you open.** Press Esc and then WAIT for the pane to be gone. A test that carries on
   after a pane that never closed types its next command into that pane and fails somewhere else.
7. **Add a judge rubric only for a judgment half** — wording, tone, "reads as one row". Everything
   a cell can settle is settled by a cell.
8. **Stay inside the budget below.** Every wait bounded, no `t.Parallel` (the e2e tests use
   `t.Setenv` and package-var seams), and every swapped package var restored via `t.Cleanup`.

## Gates and budgets

*The final-measurement row is filled by plan item 16.*

| Set | Runs in a plain `go test` | Ways it does not run |
| --- | --- | --- |
| in-process e2e | yes, every platform | — |
| PTY e2e | yes | Windows; a failed `TestMain` build (both say so in the skip) |
| judge | no | always, unless `APOGEE_JUDGE_ENDPOINT` / `APOGEE_LIVE_ENDPOINT` is set — it joins `make live-eval` |

No build tags and no `-short`: a test that only runs when someone remembers a flag is a test nobody
runs. The in-process e2e set runs in **every plain `go test`**, on every platform, with no gate at
all — it needs no terminal, no model and no network beyond loopback.

The PTY set runs in every plain `go test` too, and has exactly two ways not to run, both of which
say so: it skips on Windows (the driver is unix-only) and it skips when `TestMain` could not build
the binary, with the build error in the skip message. It needs no terminal of its own — it makes
one — and no model beyond the same loopback stub.

The e2e tests are **serial**: they use `t.Setenv` and package-var seams, so none of them calls
`t.Parallel`, and every swapped package var is restored through `t.Cleanup`. The budget is therefore
serial wall clock. The whole set added by this plan has ~15 s under `go test -race ./cmd/apogee/`,
on top of the 5.5 s the package cost before it. `TestE2ESmokeInProcess` — thirteen checklist steps,
two launches and a restore — measures **≈ 7.5 s** of that under `-race`, and `TestE2ESmokePTY`
**≈ 1 s** on top of the one-off `go build` (**≈ 1.5 s**) every run of the package now pays.

Two rules keep it there. Every wait is a bounded `WaitFor` (5 s default) on a condition, never a
sleep; and a settle is 150 ms of no bytes, taken only when a frame is about to be READ.
