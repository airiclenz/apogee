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

*Filled by plan items 4, 5 and 6.* The `Frame` type over `github.com/charmbracelet/x/vt` (text,
cell styles, cursor, cell width), the in-process `Driver` over `runRoot`'s launcher seam, the
black-box `PTYDriver`, and the golden-frame convention under `cmd/apogee/testdata/frames/`
(`go test ./cmd/apogee -update`).

## judge

*Filled by plan item 7.* The `APOGEE_JUDGE_ENDPOINT` / `APOGEE_JUDGE_MODEL` gate, how a rubric is
written beside its test, the named text artifacts a verdict is rendered on, `judge.Pairwise`, and
what a binding `fail` looks like in `go test` output.

## Which driver observes which claim

Seeded here with the proxies ratified for the irreducible halves (ADR 0062, decision 5); **the rest
of the rows are filled by plan item 8**, and plan item 16 re-checks every example-test name against
`go test -list 'TestE2E.*' ./cmd/apogee/`. Names marked *(planned)* do not exist yet.

| Claim class | Driver | Example test | Not observable by any driver |
| --- | --- | --- | --- |
| Wide-rune and glyph alignment (T-20) | `tuitest` — the emulator's cell width is the authority; assert `Frame` cells, never rune counts | `TestE2EWidth…` *(planned, item 12)* | Font tofu — whether the reader's font has the glyph at all |
| Flicker during streaming (T-24) | `--tui-trace` counters: bytes written and full-frame repaints per streamed token, pinned against a ceiling | `TestE2EStreamRepaintCeiling` *(planned, item 9)* | Felt flicker — the repaint ceiling is the accepted proxy |
| Desktop hand-off (T-19) | a logging fake opener installed through `present.Opener.LookPath`; assert argv and wording | `TestE2EPresent…` *(planned, item 14)* | What a real desktop application does with the file |
| Upgrade path of an installed apogee (T-21) | post-release `make release-smoke` — archives, `SHA256SUMS`, `--version`, `brew upgrade` when `brew` is present | `make release-smoke` *(planned, item 15)* | `brew upgrade` before the release it upgrades to exists |
| Tag job and action pins (T-21) | `actionlint` plus `scripts/check-pins.sh`, both run from `make check` | `make check` *(planned, item 15)* | — |
| Landlock residual honesty on an older ABI (T-11) | a dedicated `ubuntu-22.04` CI job running the probe assertions | CI job `landlock-abi-1-2` *(planned, item 15)* | — |
| Behaviour against a real model (T-22) | stays env-gated under `make live-eval`; unset means skip, never a silent pass | `TestE2ELiveModel` | — (needs a live tool-capable endpoint) |
| The docs work for a newcomer (T-23) | judge-driven agent in a clean container with only `README.md` + `docs/manual/`, driving apogee against stubllm | `TestNewcomerFollowsTheDocs` *(planned, item 15)* | The Homebrew and OpenRouter steps — they need a published release and a real API key |

## Writing a new e2e test

*Filled by plan item 8.* The checklist a new `cmd/apogee/e2e_<topic>_test.go` follows: pick the
driver from the table, script stubllm instead of writing a handler, assert on `Frame` cells (or a
golden for a rendering surface), add a judge rubric only for a judgment half, and keep inside the
budget below.

## Gates and budgets

*Filled by plan items 5, 6, 7 and 16.* Which tests run in a plain `go test`, what skips where (PTY
on Windows or without a pty; judge without its gate), which join `make live-eval`, and the measured
wall clock of the whole e2e set under `-race` against its ~15 s ceiling. No build tags, no `-short`.
