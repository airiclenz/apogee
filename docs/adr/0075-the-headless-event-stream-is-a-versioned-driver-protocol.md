---
Status: accepted
---

# The headless Event lines are a versioned Driver protocol over the engine's Event stream

## Context

`apogee headless` prints prose. Its answer is one `Fprintln` of `run.Result.FinalText`
(`cmd/apogee/headless.go:642`) and everything around it — written files, the undo verb, per-sub-agent
fills, usage, the one-line summary — is a post-hoc block composed from the same `run.Result`
(`headless.go:635-677`). The only live consumer of the engine's Event stream is `pruneNoticeSink`
(`headless.go:124-140`), which prints one stderr line per `PruneEvent` and forwards the rest. Nothing
a program can parse comes out of a run, so the only machine-readable signal today is the exit code
and a `· faulted` suffix the manual offers as a grep target (`docs/manual/headless.md:100-102`).

The contender gap assessment of 2026-09-06 ranked headless's plain-text-only output gap 3 of ten,
behind persistent undo and the tool-call salvage guard — both since shipped — and the owner accepted
it on 2026-09-07 as bead `apogee-tvw`. Every rival has closed it: opencode's TUI is a client of
`opencode serve` (OpenAPI 3.1 + SSE), pi ships `--mode json` / `--mode rpc`, qwen-code runs a daemon
over HTTP+SSE. The gap is ADR 0031's thesis cashed in — an engine sufficient for any Driver is worth
little while no program outside the repo can observe a run — and closing it is what an IDE extension
would later build on.

The door is already open. [ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
made the engine sufficient for any **Driver**, and `domain.EventSink` (`internal/domain/events.go:18`)
is a sealed, additively-versioned, one-way stream of 18 variants that every Driver already reads. What
was missing was not a seam but a *contract*: a documented, versioned rendering of that stream a
program outside the repo can depend on.

[ADR 0073](0073-hooks-are-observe-only-driver-side-reactions-to-engine-events.md) shipped a JSON
projection of the same events days earlier — `hooks.Payload` — but a deliberately lossy one: five
moments, flat, one synthesized (`file-changed` correlates a write call with its result,
`internal/hooks/match.go:144` and `:158-172`), one depth-filtered (`turn-finished`, `match.go:87-89`),
one phase-dropped (`approval-waiting`, `match.go:116`). It answers "tell my script when something
happened". It cannot answer "let a program observe this run".

The owner settled the scope on 2026-09-07: **JSONL first, `serve` later**. `apogee serve` as a fourth
wire-facing Driver is out of scope here and is dependency-blocked on this bead (`apogee-afu`), so the
event contract proves itself on a real consumer before an HTTP surface is designed around it.

## Decision

**1. Terms: Event, Event stream, Event lines.** `CONTEXT.md` gains **Event** and **Event lines**.
An **Event** is the engine's sealed sum type on `EventSink`. The **Event stream** keeps the meaning
the glossary and ADRs 0005, 0013, 0033 and 0068 already give it — the engine's sequence of Events on
`EventSink`, whichever Driver reads it — and gains one sentence pinning it. The **Event lines** are
the one-line-per-Event JSON rendering of that stream headless writes to stdout. The glossary
previously named only **Hook event** — the lossy derivative — and not the source.

**2. The lines carry every variant except the Inspector's.** All 17 variants are serialized, at
every Depth. `WireEvent` is excluded: it is raw provider protocol by construction
(`internal/domain/events.go:470`), fires only when `Config.Inspector` is armed, and putting it on a
documented stdout contract would make the wire format part of a public surface — which invariant 1
forbids.

**3. A separate encoder, sharing convention with Hooks but not shape.** `hooks.Payload` is untouched.
The Event lines are a new package, `internal/eventjson`, re-exported on the public facade. A line is
`{"event":…,"v":1,"seq":…,"time":…,"session":…,"turn":…,"depth":…,"call_id":…,"data":{…}}` — variant
members nested under `data`, never flattened into the envelope. Flat works for five hook events and
`jq -r .path`; across 17 variants it becomes a union of some sixty optional keys whose names
genuinely collide. Binding details of the envelope:

- Every envelope member is always present, and is `null` where the line has no value for it: a
  frame's `turn`, `depth` and `call_id`, and an Event's empty `call_id`. A consumer never tests for
  a missing key.
- `time` is RFC3339Nano, matching what `hooks.Runner` stamps.
- `seq` starts at 1 and counts every line, so the two frames consume a `seq` each.
- `AuditEvent` embeds `EventBase` and declares its own `CallID`, which shadows the embedded one
  (`events.go:423-434`). The envelope therefore reads `ev.EventBase.CallID` explicitly — the
  spawning delegation, as on every other line — and `data.call_id` carries the outer field, the
  audited call.

**4. snake_case names, deliberately a different vocabulary from a Hook event's kebab-case.** Nineteen
line kinds: `token`, `reasoning`, `stream_reset`, `message`, `tool_call`, `tool_result`,
`sub_agent_phase`, `sub_agent_named`, `child_interjection`, `approval`, `turn`, `mechanism_fired`,
`floor_guard`, `error`, `prune`, `usage`, `audit`, plus the two frames. The case difference is the
signal: a Hooks name and an Event-line name for the same moment are *not* the same moment —
`turn-finished` is Depth-0 only, `turn` is every depth. `error` collides benignly and means the same
thing in both.

**5. Bracketed by two frames that are not Events.** `run_started` opens (session, workspace, model,
server, mode, bypass, confined, version) and `run_finished` closes (turns, denied, faulted, fault,
exit code, title, written files, context files, undo note, usage, sub-agents, `saved`, and
`final_text`). The rule is **exactly one `run_finished` on every exit path**, whether or not a sink
ever existed, so a consumer never faces an empty stdout it must interpret. `runHeadless` refuses
through `notStarted` at thirteen points (exit 2): the flag, argument, config, workspace and mode
checks before the session id is minted (`headless.go:489`); `firingConfig` failing (`:511`) and the
server heartbeat (`:541`) after the id exists but before any sink does; and `run.Once`'s own pre-run
exits (`:624`, `runErr != nil && res.Turns == 0`), which happen *after* the sink is installed
(`:579`) yet emit nothing. `session` is present exactly when the id was minted, and its source is
headless's own `recordID`, never `run.Result.SessionID` — that is empty under `--no-save`, where the
run still has an id. `saved` says whether a record was written, so a consumer does not feed the id
of an unsaved run to `apogee undo`. A run cancelled by Ctrl-C still writes `run_finished`, with the
exit code the text path gives that case (`runFailed`, exit 1, `headless.go:567-570`, `:676-684`).
The lines are therefore not purely the engine's, and that is stated rather than hidden.
`final_text` is duplicated from the `message` lines on purpose. `MessageEvent.Text` is already
committed text (`internal/run/run.go:605-611`, `:842-846`), so a consumer reading `message` needs
no accumulator; but the simplest consumer of all reads `token`s, and that one must implement both an
accumulator and the `stream_reset` rule to reach the answer. `final_text` spares it both.

**6. The human-readable path is untouched.** `--format text` is the default and is byte-identical to
today; `--format json` replaces stdout entirely. stderr keeps its prose diagnostics and is explicitly
**not** part of the contract — hook failures in particular never re-enter the stream (ADR 0073 §8).
The one exception: `pruneNoticeSink`'s stderr line is suppressed under `--format json`, because the
`prune` line carries the same fact. Re-rendering the text output *from* the Event lines was rejected
here and filed as its own bead.

**7. Identity is Driver-stamped.** The encoder adds `time`, `seq` and `session`; `EventBase`
(`events.go:37-58`) is not extended. A timestamp is neither model-visible nor safety-relevant, so
ADR 0031's "nothing in one Driver's surface alone" does not reach it, and `hooks.Runner` already
stamps `time` and `workspace` itself (`internal/hooks/runner.go:220-221`). A sequence number is
meaningful only per-stream, so it has no single owner on the engine. `workspace` appears in
`run_started` only — one run has one workspace for life.

**8. No parent call id, no exchange id.** The call tree is rebuildable by correlating `ToolCallEvent`
ids, which a consumer must do anyway; the exchange boundary is already observable as
`TurnEvent.Status`. Inventing an Exchange identity would give the engine one it does not have.

**9. Lossless and ordered; it blocks rather than drops.** Writes happen inside `Emit`, through a
`bufio.Writer` flushed per line; the engine already serializes emission (`serialEventSink`,
`internal/agent/construct.go:156-165`), so ordering is free. The asymmetry with Hooks — which drop
under overload (ADR 0073 §7) — is the point: a lost hook costs a notification, a lost `tool_result`
silently corrupts the consumer's model of the run and no `seq`-gap detection recovers the content. On
a write error the encoder reports once on stderr, stops emitting, and lets the run finish; a run that
has already edited files is not half-killed because a pipe closed.

Two consequences of blocking are owned here rather than discovered later:

- **SIGPIPE.** Go kills the process on a write to fd 1 that fails with `EPIPE` unless `SIGPIPE` is
  ignored, and nothing in `headless.go` handles it — only `signal.NotifyContext` for interrupt and
  TERM (`:569`). `apogee headless --format json | head -1` would therefore kill the run mid-edit. On
  Unix the `--format json` path ignores `SIGPIPE` (`signal.Ignore(syscall.SIGPIPE)`; Windows has
  none), so the write returns `EPIPE` and the rule above holds.
- **A stalled reader stalls the run.** `serialEventSink` is a plain mutex around `inner.Emit`, so a
  reader that stops draining parks the writer inside `write(2)` under that mutex; every fan-out
  child then stalls on its next `Emit`, and Ctrl-C cancels a context nobody is observing. Lossless
  stays, and the ADR owns the cost: (a) a second interrupt exits hard; (b) the encoder is the
  **outermost** sink wrapper — never inside `hooks.Runner`, whose `Report` callback is documented
  must-not-block (`internal/hooks/runner.go:66-70`).

**10. Versioned `v:1`, per line, additive within it.** Every line carries the version, because JSONL
lines are tailed, split, grepped and merged across runs — a version living only in a frame is invisible
in all four cases. New line kinds and new `data` members may appear in any release; a consumer must
ignore unknown names, members and enum values (`StepStatus`, `ApprovalPhase` and `SubAgentPhase` are
open sets today, `apogee.go:35-36`). A removal, rename or changed meaning bumps `v` and is a CHANGELOG
entry.

**11. Full fidelity, and not secret-scrubbed.** `tool_call.data.call.arguments` is the model's raw
argument JSON and `tool_result.data.result.content` is the full result — which the capped-tool-outputs Floor
guard has already bounded before the model saw it, so "full" means exactly the bytes the model got.
Bounding it would make the live lines strictly worse than the on-disk transcript for no security
gain. Same trust posture ADR 0073 §6 accepted: it goes to the user's own pipe. The manual carries the
sharper warning that stdout is routinely redirected into CI logs, where a run publishes every file
the agent read.

**12. One engine fix: cancelled delegations close their bracket.** `SubAgentPhaseEvent`'s `finished`
phase is today never emitted for a cancelled group (`internal/domain/events.go:174-176`), leaving a
permanently open bracket in any log. It is emitted with a cancelled status. The skip is deliberate,
not an oversight: both dispatch paths (`internal/agent/dispatch.go:425-430` pool, `:865-870` serial)
say a cancelled delegation is one the parent Turn is about to roll back, so closing the bracket
changes stated semantics for every Driver, not only this one. That is why it is done here, in the
ADR that first needs the bracket closed, rather than slipped in later as the additive enum value it
also is — and why the TUI is checked, as part of the same change, that a rolled-back delegation
renders no spurious finished line.

**13. `--format` reaches headless only.** The flag is installed in `runHeadless` and nowhere else:
the daemon composes its Firings through `firingConfig` and `runOnce` directly (`daemonfire.go`) and
never passes through `runHeadless`, and `probe` runs no loop. The daemon keeps its prose log
(ADR 0034 decision 7): its stdout multiplexes N Firings, which needs a per-stream identity this
envelope has no field for. `probe` stays a prose report. Both are separate beads if wanted.

**14. The contract is protected by goldens.** A scripted `stubllm` upstream drives a real
`--format json` run end to end against a checked-in `.jsonl` golden with `time` and `session`
normalized, plus a second golden for the not-started path asserting exactly one `run_finished` with
`exit_code: 2`. Per-variant unit tests cover the encoders, including sub-agent nesting. Unit tests
alone cannot catch a changed envelope, a dropped frame, reordered lines, or an event that silently
stopped being emitted — which is the entire set of things `v:1` promises will not happen quietly.

**15. ADR 0031 is refreshed, not superseded.** Its Driver list still reads "TUI today, bench today,
a scheduling/workflow daemon tomorrow" while four Drivers ship today; the list is corrected. No
invariant is touched: headless composes bytes on a file descriptor it owns, and the engine still
hands out Go values only.

## Consequences

- Every Driver, not only headless, sees the cancelled `finished` phase of decision 12: the TUI's
  transcript and the bench's tap both gain a closing bracket they never received before.
- A consumer that stops reading can stall a run (decision 9), which is why a second interrupt exits
  hard instead of waiting on a pipe nobody drains.
- A `v` bump is a CHANGELOG entry, and the goldens are the tripwire that makes an accidental one
  visible before it ships.
- `apogee serve` (`apogee-afu`) builds on this contract rather than designing its own event shape;
  the envelope and the two frames are its starting point.
- The reach is headless only. A daemon Firing and a probe still print prose; each is its own bead.

## Rejected

- **One serializer shared with Hooks** — redefining `hooks.Payload` as a filtered projection of an
  Event line. It buys one document at the cost of breaking a contract shipped days earlier and
  forcing a lossless 17-variant rendering into a shape designed for five flattened ones.
- **A pure event stream, no frames** — the bead's literal wording. Rejected because `run.Result`
  carries facts no Event emits (session id, written files, undo note, context files, denied count)
  and because the not-started paths would produce no output at all.
- **Re-rendering the human output from the Event lines** — the opencode shape, and it would fix
  headless printing nothing until a run completes. It cannot honour "the human output is unchanged":
  the post-hoc block prints `run.Result`-only facts ordered as a summary, which a live rendering
  reproduces only by buffering to the end. Filed as its own bead; cheaper later *because* the lines
  exist.
- **A bounded queue that drops** — see decision 9.
- **Timestamps and sequence numbers on `EventBase`** — see decision 7.
- **One kebab-case vocabulary across Hooks and the Event lines** — see decision 4.
- **The version only in `run_started`** — see decision 10.
- **`apogee serve`** — explicitly out of scope by owner call; its own ADR and plan, dependency-blocked
  on this contract (`apogee-afu`).
