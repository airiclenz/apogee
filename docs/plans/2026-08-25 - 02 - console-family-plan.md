# Console family — implementation plan

**Goal:** ship the four-tool Console family ratified in ADR 0059 — `console_open`, `console_send`,
`console_read`, `console_close` — a persistent interactive process (a REPL, a dev server, a shell)
the model drives across Turns, held as live host state on the engine, default-off in the roster,
POSIX only in this plan.
**Date:** 2026-08-25
**Status:** unexecuted
**sized for:** ~200k-context host

**Authoritative sources (precedence in this order when an item disagrees with them):**

1. `docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md` — the design.
2. `docs/design/confinement-execution-contract.md` §2.2–2.4 (Confine prepare-in-place, teardown),
   §4 (per-tool classification); ADR 0056 §2 (kill-on-denial); ADR 0057 (roster, default-off).
3. ADR 0008 (as amended 2026-08-25) and ADR 0022 §8 / ADR 0051 (live host state precedent —
   the undo journal: `internal/agent/agent.go:220-229`, `internal/undo/context.go`).
4. Pinned commit at write time: `475c1608`.

**Ratified design calls** (owner, 2026-08-25, grill session → ADR 0059; the plan-writer's two
write-time questions are the last two):

- Lifetime: process-lived live host state; never survives snapshot/fork/resume (ADR 0059 §1).
- Shape: four tools split along the classification line — open/send Subprocess-marked, read/close
  read-only floor (§2).
- Roster: default-off, profile-enabled; first user of ADR 0057's default-off state (§3).
- Per-send Resolution; a mode or `/confine` change never touches a live process (§4).
- Kill-on-denial applies to confined Consoles (§5).
- Ownership: a delegation's end closes the Consoles it opened; fixed cap of **4** open Consoles per
  engine (a constant, not a knob); no idle TTL (§6).
- **Platform (owner, plan-writer question):** POSIX only, `github.com/creack/pty` is the
  dependency; Windows is a later plan. *Refinement by
  the plan-writer, from codebase evidence:* the four tools are registered on **every** OS and
  `console_open` returns an error result on Windows ("console is not supported on Windows yet")
  instead of being excluded by build tags — `tools.KnownToolNames()` reads `builtinTools`
  unconditionally (`internal/tools/registry.go:363-383`), so a per-OS omission would turn a
  legitimate `tools.enabled: [console_open]` into the unknown-name startup NOTICE. The
  platform split lives in `internal/console`'s build-tag pair, never in registration.
- **Shipped roster (owner, plan-writer question):** the family is enabled in a shipped profile for
  Qwen3.8 — ADR 0059 §3 is the ratification ADR 0057 §6 asked for. *Refinement:* no Qwen entry
  exists in `internal/profiles/shipped.go` today (gemma, gpt-oss, minimax-m3 only), so item 7 ADDS
  a tools-only `qwen3.8` entry (`SpellsTools: true`, no wire-shape axes — axis-wise resolution
  leaves those at the zero layer exactly as an absent entry does).

**Binding tool contract** (every item's **What** refers to this; a deviation is a dated NOTES
line under the item):

- IDs: small positive integers, monotonic per engine, never reused; rendered as `console 3`.
- `console_open` args: `command` (required — a shell command line run under the platform shell
  inside a pseudo-terminal, `sh -c` on POSIX; supports pipes and globs like `terminal`), `workdir`
  (optional, resolved within the workspace root like `terminal`), `wait_ms` (optional, default
  500, max 10000 — how long to collect initial output before returning). Result: first line
  `console <id> opened: <command>`, then the output collected so far; a non-alive process at
  return adds `exited with code N`.
- `console_send` args: `id` (required), `input` (required — sent verbatim **plus a trailing
  newline**, unless `raw: true`, which sends the bytes exactly as given; control characters may be
  given as JSON escapes, e.g. `"\u0003"` for Ctrl-C), `wait_ms` (optional, default 1000, max
  30000 — collect output for that long, returning early once the process exits). Result: the
  output produced in the window, then `alive` or `exited with code N`.
- `console_read` args: `id` (required), `wait_ms` (optional, default 0 = return what is buffered
  now; >0 waits up to that long for NEW output and returns as soon as some arrives; max 30000).
  Result: unread output since the last read/send, then `alive` or `exited with code N`. A Console
  whose process was stopped by the denial watch prefixes `confinementDenialStopLabel`.
- `console_close` args: `id` (required). Kills the process group, reaps, returns any unread tail
  and `exited with code N` (or `killed`). Closing an unknown or already-closed id is an error
  result, not a Go error.
- Buffering: one ring buffer of **1 MiB** per Console; unread overflow drops the oldest bytes and
  the next read is prefixed `[… N bytes of earlier output dropped …]`. A single read/send result
  is capped at `maxSubprocessOutputBytes` (256 KiB) the way `terminal`'s output is.
- Terminal: window **160×40**; env is `subprocessEnvScopedPath(root, secretEnv, "TERM=dumb")`;
  ANSI CSI/OSC escape sequences are stripped from output before it reaches the model.
- Every error the model can act on is an error *result* (`errorResult`); only ctx cancellation
  and `ErrConfinementUnavailable` demotion are Go errors — the `terminal` convention.
- Unknown id → `no console <id> (open consoles: 1, 2)`; a Console the process no longer holds
  (after resume) is simply an unknown id — "console gone" is that message.

**Standing requirements**

- `skills: coding-standards`
- Package layering: `internal/console` imports `internal/platform` and the pty dependency only —
  never `internal/tools`, `internal/agent` or `internal/domain`. `internal/tools` prepares the
  `*exec.Cmd` (Confine, env, refusals) and hands it to the registry; process mechanics live in
  one place. The registry is held on the `Agent` (like `journal`), never on a tool instance
  (`SwapTools` rebuilds tool instances mid-session — `internal/agent/swaptools.go:66`).
- No `runtime.GOOS` in non-test code: build-tag pairs (`_unix.go` = `!windows`, `_other.go` =
  `windows`) as `internal/tools/exec_pgroup_*.go` do.
- A deviation from item text lands as a dated NOTES line under the item.
- The dangerous-action guard already scans every string leaf of a call's arguments
  (`internal/security/dangerous.go:236-264`): **do not** add `input` to `payloadKeys` and do not
  implement `domain.PromptTool` on any console tool — `console_send.input` must stay scanned
  the way `terminal.command` is.

**Out of scope**

- Windows ConPTY (its own plan); a per-Console idle TTL; a `MaxOpen` config knob; a
  `domain.ToolSummary` variant for consoles (prose floor is fine); bench arms of any kind;
  changes to `terminal`/`python_exec` beyond relocating two shared labels (item 4); any change
  to the undo journal; a default-prompt guidance line about consoles (tool descriptions carry
  it); ISSUES.md arms (a)(b)(d)(e)(f)(g).

---

## 1. `internal/console`: the PTY process handle — ✅ DONE (2026-08-25)

NOTES (2026-08-25): `Close` joins the WAITER goroutine as well as the reader, so `Alive`/`ExitCode` are final when it returns — the binding contract's `console_close` ("kills the process group, reaps, returns … `exited with code N`") needs that; both joins are bounded by a 5s deadline.
NOTES (2026-08-25): `ErrUnsupported` is declared in BOTH halves of the build-tag pair rather than in a new shared file, keeping the item's file list exact — the `internal/tools/exec_pgroup_*.go` `processWaitDelay` precedent.
NOTES (2026-08-25): `Process.Read` returns `(string, int)` (the ring's `([]byte, int)` after `stripEscapes`); the ring's own signature is the item's.
NOTES (2026-08-25): the group-kill test backgrounds `sleep 60 &` inside `sh -c`, not inside an INTERACTIVE shell: an interactive shell turns job control on and puts each background job in a process group of its own — the contract's documented "descendant that left the group" residual, which no negative-pid kill reaches. Recorded in the test's doc comment.

**What:** new package `internal/console` holding one Console's process mechanics, with no
registry yet. `process.go` (POSIX, `//go:build !windows`): `type Process` wrapping a
`*exec.Cmd` started under a pseudo-terminal via `pty.StartWithAttrs(cmd, &pty.Winsize{Rows: 40,
Cols: 160}, attrs)`, where `attrs` is a copy of `cmd.SysProcAttr` (Confine may have set
`Setpgid`) with `Setpgid=false`, `Setsid=true`, `Setctty=true` — a session leader cannot also
`setpgid`, and after `setsid` the pgid equals the pid, so `kill(-pid, SIGKILL)` still reaps the
whole group (contract §2.4). The Process builds the cmd itself with `exec.CommandContext` on a per-Console
`context.WithCancel(context.Background())` it owns (never a per-call ctx — `cmd.Cancel` fires
from the cmd's own ctx), sets `Dir`/`Env` from the Spec, then calls the caller's
`Prepare(cmd) error` (the tool's Confine and refusals — item 4) BEFORE copying `SysProcAttr`
into `attrs` and starting; a Prepare error aborts Start with that error. `cmd.Cancel` kills the negative pid,
`cmd.WaitDelay = 5s`. A reader goroutine copies the master fd into `ring.go`'s 1 MiB ring buffer
(drop-oldest, dropped-byte counter, `Read(wait time.Duration) ([]byte, dropped int)` returning
unread bytes since the last read, waiting up to `wait` for NEW bytes when the buffer is empty,
returning early on process exit); the write side goes through `platform.NewDenialKillWriter(ring,
process.Kill)` when the Process was opened with `Confined: true`, and `Process.DenialStopped()`
reports `Detected()`. `Write(p []byte)` writes to the master fd. `Kill()` cancels the ctx; `Wait`
happens on a goroutine started at Start that records `ExitCode()` / `Alive()` and reaps the
group again on the clean-exit path (the §2.4 amendment: teardown on every exit). `ansi.go`:
`stripEscapes(s string) string` removing CSI (`ESC [ … final`) and OSC (`ESC ] … BEL|ST`)
sequences and lone ESC; applied by `Read`. `process_other.go` (`//go:build windows`): the same
exported surface, `Start` returning `ErrUnsupported` ("console is not supported on Windows
yet"). Keep the exported surface minimal: `Start(spec Spec) (*Process, error)` with
`Spec{Argv []string; Dir string; Env []string; Confined bool; Prepare func(*exec.Cmd) error}`
(nil Prepare = no preparation), `Read`, `Write`, `Kill`, `Close` (Kill + join the reader),
`Alive`, `ExitCode`, `DenialStopped`. Add `github.com/creack/pty` to `go.mod` (`go get`, tidy).
Deep module: the file boundary is the process; nothing about ids, owners or tools leaks in.

**Files:** `go.mod`, `go.sum`, `internal/console/doc.go`, `internal/console/process.go`,
`internal/console/process_other.go`, `internal/console/ring.go`, `internal/console/ansi.go`,
`internal/console/process_test.go`, `internal/console/ring_test.go`, `internal/console/ansi_test.go`

**Tests:** ring: drop-oldest with counter, unread-since-last-read, wait-returns-early-on-new-bytes,
wait-returns-on-close. ansi: CSI colour, OSC title, cursor moves stripped; plain text untouched.
process (POSIX, real `sh`): start `sh` → `Write("echo hi\n")` → `Read` contains `hi`; `Write("exit
3\n")` → `Alive()` false, `ExitCode()==3`; Kill on a `sleep 60` returns promptly and `Alive()`
false; a `Confined: true` Process whose program prints `Permission denied` is killed and
`DenialStopped()` is true (feed the signature through the program's own output — no real
confinement needed here); a child that backgrounds `sleep 60 &` does not survive `Close` (group
kill). Windows stub compiles (`GOOS=windows go vet ./internal/console/`).

**Acceptance:** `go build ./... && go test ./internal/console/ && GOOS=windows go vet
./internal/console/`

**Commit:** `feat(console): PTY process handle with ring buffer, denial watch and group teardown`

---

## 2. `internal/console`: the Registry and its context seam — ✅ DONE (2026-08-25)

NOTES (2026-08-25): `Registry.Close` tears the process down with the process layer's `Close` (kill + reap) rather than the item's literal "Kill + remove", so `Alive`/`ExitCode` are final when it returns — the binding contract's `console_close` ("kills the process group, reaps, returns … `exited with code N`") and item 5's read-the-code-after-close order both need the reap. The Console is removed from the map first and torn down outside the lock, so no registry call is ever blocked behind a teardown; `CloseOwnedBy`/`CloseAll` use the same order and swallow the teardown error by design (there is no caller left to report it to, and the error is the terminal's, not a still-running process).
NOTES (2026-08-25): an id is issued only when the process actually starts, so a failed `Open` — `ErrUnsupported` on Windows, a refusing `Prepare` — consumes no id; "monotonic, never reused" is unaffected.
NOTES (2026-08-25): the Windows `ErrUnsupported` case and the real-process cases share the single `registry_test.go` the item's file list names, split by `runtime.GOOS` guards rather than a build-tag pair (which would have needed a second test file the item does not name). GOOS in TEST code is what the standing requirement permits; `GOOS=windows go vet ./internal/console/` compiles it.
NOTES (2026-08-25): `doc.go` gained the two file-map lines the item asks for, a paragraph on what the registry is, and one correction the registry made necessary — the package no longer "knows nothing about ids, owners, tools or the engine", so that sentence now reads "nothing about tools, models or the engine's exchange" with the owner named as an opaque string.

Depends on item 1.

**What:** `registry.go`: `type Registry` (mutex-guarded map id → `*Console`, next id counter,
`const MaxOpen = 4`); `type Console struct{ ID int; Owner string; Command string; proc *Process
}` with `Read/Write/Kill/Alive/ExitCode/DenialStopped` forwarding. `func New() *Registry`.
`Open(spec OpenSpec) (*Console, error)` with `OpenSpec{Owner, Command string; Argv []string;
Dir string; Env []string; Confined bool; Prepare func(*exec.Cmd) error}` (the last five forwarded
to item 1's `Spec`) — refuses beyond `MaxOpen` with `ErrTooMany` carrying the open ids; a Console
whose process has exited stays in the map (its tail is readable) until closed, and counts toward
the cap. `Get(id int) (*Console, bool)`; `OpenIDs() []int` (sorted); `Close(id int) error`
(Kill + remove, `ErrUnknown` otherwise); `CloseOwnedBy(owner string)` and `CloseAll()` — both
best-effort, never error, safe on an empty registry and on a nil `*Registry` (nil = "no
consoles", mirroring `undo.Journal`'s nil contract). `context.go`: `WithRegistry(ctx, *Registry)`
/ `FromContext(ctx) *Registry` with an unexported key type, nil installs nothing — the exact
shape of `internal/undo/context.go`. `doc.go` gains the two files.

**Files:** `internal/console/registry.go`, `internal/console/context.go`,
`internal/console/doc.go`, `internal/console/registry_test.go`, `internal/console/context_test.go`

**Tests:** ids monotonic and never reused after Close; cap: the 5th Open returns `ErrTooMany`
naming ids 1–4; an exited-but-unclosed Console still counts; `CloseOwnedBy("call-7")` closes only
that owner's Consoles and leaves the top-level (`Owner ""`) ones; `CloseAll` on nil receiver and
empty registry is a no-op; `FromContext` on a bare ctx is nil; `Open` on Windows-stub surfaces
`ErrUnsupported` (guard the real-process cases with `!windows`).

**Acceptance:** `go build ./... && go test ./internal/console/`

**Commit:** `feat(console): registry with owner-scoped close, a fixed cap and a context seam`

---

## 3. Engine wiring: the registry is live host state on the Agent — ✅ DONE (2026-08-25)

NOTES (2026-08-25): the delegation-end and engine-exit close live in a `closeConsoles` helper that `Close` calls, rather than inline in `Close` — the depth split needed a doc comment of its own and `Close` was a one-line method whose comment already carried three paragraphs about the upstream client.
NOTES (2026-08-25): the "snapshot carries no console state" test asserts over the state object's KEYS rather than the raw JSON: the conversation legitimately quotes the fake tool's name and its result text, so a substring scan of the payload fails on content that has nothing to do with the registry.
NOTES (2026-08-25): the sub-agent lifetime tests drive `newChildAgent` + `child.Close()` directly rather than a real `sub_agent` call — that is the same handle `subagent.go`'s deferred `sub.Close()` holds, and the direct form lets the test hold both Consoles and assert on their liveness after the delegation ends. The real-process tests skip on Windows via a `runtime.GOOS` guard (GOOS in test code, per the standing requirement); `GOOS=windows go vet ./internal/agent/` compiles them.

Depends on item 2.

**What:** mirror the undo journal. `internal/agent/construct.go:123` area: `consoles:
console.New()` built unconditionally in `newAgent`; field `consoles *console.Registry` on `Agent`
(`internal/agent/agent.go:220-229` neighbourhood) with a doc comment stating "LIVE HOST STATE, not
session state (ADR 0022 §8, ADR 0059) — never serialized". `internal/agent/dispatch.go:793`
(beside `undo.WithJournal`): install `console.WithRegistry(ctx, a.consoles)` on every tool
dispatch ctx. `internal/agent/subagent.go:328-336`: `child.consoles = a.consoles` (shared by
pointer). `internal/agent/agent.go:340` `Close()`: a child (`a.depth > 0`) calls
`a.consoles.CloseOwnedBy(a.callID)`; the top-level agent calls `a.consoles.CloseAll()` — this is
the single delegation-end site (`subagent.go:95`'s deferred `sub.Close()` covers normal, error,
cancel and faulted exits) and the engine-exit site (`cmd/apogee/wire_engine.go:478-483`,
`internal/run/run.go:218`). `internal/agent/agent.go:760-767` `ClearContext()`: after the
mid-Exchange refusal, `a.consoles.CloseAll()` — `/new` closes Consoles (ADR 0059 §1); this is
the one place Console lifetime diverges from the journal's, say so in the comment.
`internal/agent/state.go:47-51`: extend the "withheld from the snapshot" comment to name the
console registry. Owner key for a tool: `domain.SpawnCallIDFromContext(ctx)` (empty at top level)
— item 4 reads it; this item only guarantees the ctx carries both the registry and the id.

**Files:** `internal/agent/agent.go`, `internal/agent/construct.go`, `internal/agent/dispatch.go`,
`internal/agent/subagent.go`, `internal/agent/state.go`, `internal/agent/console_test.go`

**Tests:** (using a fake tool that opens a `sh` Console via `console.FromContext(ctx)` with the
`SpawnCallIDFromContext` owner) a Console opened by a sub-agent is closed when the delegation
ends and the parent's own stays open; `ClearContext` closes everything; `Close` on the top-level
agent closes everything; the registry pointer is identical on parent and child; the Session
snapshot carries no console state (assert the snapshot type has no such field / the round trip
leaves `OpenIDs()` unaffected).

**Acceptance:** `go build ./... && go test ./internal/agent/ -run 'Console|Clear|SubAgent|Close'`

**Commit:** `feat(agent): hold the console registry as live host state, closed per delegation, on /new and at exit`

---

## 4. Tools: `console_open` and `console_send` — ✅ DONE (2026-08-25)

NOTES (2026-08-25): the exec fence is applied to the LookPath-RESOLVED shell rather than to the bare `argv[0]` the item names. `platform.Host.Command` hands back a relative `"sh"`, and `security.RefuseExecFromWritablePath` measures a relative program against apogee's OWN working directory — which in production is the workspace — so the literal form would have refused every `console_open` on a normal run. Resolving first also puts the fence where it belongs (on the program PATH actually leads to) and matches what the confinement wrapper re-execs, since `wrapArgvUnderLauncher` carries `cmd.Path`.
NOTES (2026-08-25): `consoleTail` is split into two entry points over one renderer, because the binding contract gives `console_read` and `console_send` DIFFERENT wait rules that a single-signature helper cannot both satisfy: `consoleTail(c, wait)` returns as soon as some output arrives (read's polling shape, item 5's caller) and `consoleWindowTail(c, wait)` collects the whole window and ends early on exit (open's and send's shape — a terminal echoes the line it was sent before the program answers it, so returning at the first byte would hand the model back its own keystrokes).
NOTES (2026-08-25): `console_open`'s result omits the `alive` line through a third thin wrapper, `consoleOpenTail`. The binding contract's open result is "first line, then the output collected so far; a non-alive process at return adds `exited with code N`" — the first line has already said the Console is open, so the only liveness fact left worth adding is the one that contradicts it.
NOTES (2026-08-25): `collectConsoleWindow` waits out the millisecond gap between a Console's terminal going quiet and its reaper recording the exit (`awaitConsoleExit`, bounded by the caller's own window). Without it a command that was over before the call returned reported `alive` and no exit code — observed as a flaking `TestConsoleOpen_ExitedProgramIsReportedNotHidden`, since `Alive()` legitimately lags the ring's close.
NOTES (2026-08-25): `console_send.input` decodes as a `*string` so an ABSENT input stays distinguishable from an EMPTY one: the first is a malformed call and the second presses Enter at a prompt, which is a real thing to do to a console and which an empty-means-missing check would have refused.
NOTES (2026-08-25): the live confinement proof needed a THIRD test file the item's list does not name, `console_confine_linux_test.go` (`//go:build linux`). The landlock backend confines by re-exec'ing `os.Executable()`, which under `go test` is the TEST binary, so the proof only means anything with a `TestMain` playing cmd/apogee's `__confined-exec` dispatcher — and `platform.ApplyLandlockAndExec` is linux-only, so it cannot live in the portable test files. The proof writes to a second `t.TempDir()` rather than the item's literal `/tmp/apogee-console-escape`: parallel-safe and self-cleaning, same assertion.
NOTES (2026-08-25): `consoleTail` itself has no caller until item 5 (`console_read` is its only one). It lands here because item 4's file list puts the shared floor in `console_common.go`; `go vet` and `make check` do not flag an unused unexported function, and the package runs no dead-code linter.
NOTES (2026-08-25): `Terminal.resolveWorkdir` was extracted to the shared `resolveWorkdirInRoot` in `exec_common.go` as the item asks. `PythonExec` keeps its own identical method — `python_exec.go` is not in this item's file list, so the third copy was not created but the second was not removed either.
NOTES (2026-08-25): a Console's output still reaches the model with its pseudo-terminal `\r\n` line endings; the process layer strips ANSI escapes only. Nothing in the binding contract asks for CR normalisation and item 1's shipped behaviour is unchanged here — noted as a readability question, not a defect.

Depends on item 3.

**What:** `internal/tools/console_common.go`: the shared bits — `consoleArgsID` decoding
(`id` integer, also accept a numeric string), `lookupConsole(ctx, callID, id)` returning the
Console or the `no console <id> (open consoles: …)` error result, `consoleTail(c, wait)`
rendering `output + "\n" + ("alive" | "exited with code N")` with the dropped-bytes prefix and
the `confinementDenialStopLabel` prefix when `DenialStopped()`, and the `raw`/newline rule.
Move `confinementDenialLabel` / `confinementDenialStopLabel` from `internal/tools/terminal.go:
179-221` to `internal/tools/exec_common.go` (beside the funnel; behaviour unchanged, tests
unchanged). `internal/tools/console_open.go`: `ConsoleOpen` (toolSpec `console_open`, description
must say it opens a PERSISTENT interactive program and name `console_send`/`console_read`/
`console_close`, and that `terminal` stays the tool for one-shot commands); `ReadOnly() false`,
`Subprocess() true`, `DefaultOff() true`. Execute: decode → `shellHost.CommandLine` +
`preflightCommandLine` (POSIX) → `resolveWorkdir` (reuse `Terminal`'s helper: extract it to a
shared func if it is a method) → **no** `FailFastPreamble` → argv `shellHost.Command(command)` →
`refuseExecFromWritablePath(argv[0], root, confinementBox(ctx))` → `handle, confined :=
domain.ConfinementFromContext(ctx)`; a present handle with a nil Confiner is the funnel's
fail-closed `ErrConfinementUnavailable` Go error → `console.FromContext(ctx).Open(OpenSpec{Owner:
domain.SpawnCallIDFromContext(ctx), Command: command, Argv: argv, Dir: dir, Env:
subprocessEnvScopedPath(root, secretEnv, "TERM=dumb"), Confined: confined, Prepare: func(cmd
*exec.Cmd) error { return handle.Confiner.Confine(ctx, handle.Box, cmd) }})` (Prepare is nil when
no handle is present — the unconfined `confine-to-workspace: false` and gated-then-approved
cases); `ErrTooMany` and
`ErrUnsupported` become error results → collect `wait_ms` → result per the binding contract.
`internal/tools/console_send.go`: `ConsoleSend` (`console_send`; `ReadOnly() false`,
`Subprocess() true`, `DefaultOff() true`) — the Subprocess marker is deliberate although send
spawns nothing: sending bytes to a live shell IS command execution, and the marker is what makes
it confine-or-gate (ADR 0059 §2). Implements `domain.ApprovalScoper`: `ApprovalScope(call)` returns
the one line `→ console <id>` built from the arguments alone — the approval path hands the tool
no ctx, so the registry (and the Console's command line) is out of reach there by design
(`domain/tools.go:175-181`: "derive from the call's arguments, do not run the tool's work");
say so in the method comment. Execute: lookup → `Write(input [+"\n"])` → `consoleTail(c, wait_ms)`.
Constructors `NewConsoleOpen(root string, secretEnv []string)` / `NewConsoleSend()`; the package
`var _ domain.SubprocessTool = (*ConsoleOpen)(nil)`-style assertions; `doc.go` file map lines.
Mechanical: schemas via `toolSpec` like `terminal.go:16-28`; the tests capture argv/env through a
package var seam like `runTerminalSubprocess` where a real PTY is not wanted.

**Files:** `internal/tools/console_common.go`, `internal/tools/console_open.go`,
`internal/tools/console_send.go`, `internal/tools/exec_common.go`, `internal/tools/terminal.go`,
`internal/tools/doc.go`, `internal/tools/console_open_test.go`, `internal/tools/console_send_test.go`

**Tests:** open `sh` → result first line `console 1 opened: sh`; env carries `TERM=dumb` and no
apogee secret var; `argv[0]` inside the workspace is refused; workdir escaping the root is an
error result; a nil Confiner in ctx is a Go error (`ErrConfinementUnavailable`); 5th open → error
result naming the open ids; send `echo hi` → output contains `hi` and `alive`; `raw: true` sends
no newline (the shell shows no output until a later newline); send to unknown id → error result;
ApprovalScope is one line with no newline; both tools report `DefaultOff()`, Subprocess, not
read-only. **Live confinement proof** (skips when `platform.Current()` reports no usable
Confiner): open a confined `sh`, send `echo x > /tmp/apogee-console-escape` → the next read
carries `confinementDenialStopLabel`, the Console is not alive, and the file does not exist.

**Acceptance:** `go build ./... && go test ./internal/tools/ -run 'Console|Terminal|Subprocess'`

**Commit:** `feat(tools): console_open and console_send — the Subprocess-marked half of the Console family`

---

## 5. Tools: `console_read` and `console_close` — ✅ DONE (2026-08-25)

NOTES (2026-08-25): two files beyond the item's Files list were touched, both because the item's own text asks for what is in them. `internal/tools/console_common.go` gains `consoleReadWaitDefaultMS`/`consoleReadWaitMaxMS` beside the family's other four wait ceilings — the shared floor is where item 4 put them, and splitting one tool's ceilings off into its own file would hide the contrast the const block exists to show. `internal/agent/planmenu_test.go` gains the Plan-admission assertion the item's Tests line requires: `planAdmits` and `classifyTool` are unexported in `internal/agent`, so no test in `internal/tools` can make it, and the file's existing whole-registry agreement test cannot cover a family that is registered default-off (item 6) — hence a direct four-tool assertion rather than a new registry row.
NOTES (2026-08-25): `console_close` takes the unread tail AFTER `registry.Close(id)` rather than draining before it as the item's literal "drain unread → registry.Close(id)" order says. The registry's close waits for the process's output to finish draining into the ring, and the ring stays readable once closed (`Process.Close`), so reading afterwards is the only order that returns the WHOLE tail: a program's parting line — or output the kill itself produced — falls between a pre-drain and the signal and is lost. The result shape the binding contract names is unchanged.
NOTES (2026-08-25): a teardown error from `registry.Close` (the pseudo-terminal refusing to be released, not a program still running) is reported on a line ABOVE the tail inside an ok result, not as an error result. The Console is out of the registry whatever the teardown said, so an error result would invite a retry that can only answer "no console N" — while swallowing the error silently would hide a host leaking terminals from the transcript.

Depends on item 4.

**What:** `internal/tools/console_read.go`: `ConsoleRead` (`console_read`; `ReadOnly() true`,
NO Subprocess marker, `DefaultOff() true`) — Execute: lookup → `consoleTail(c, wait_ms)` (wait
semantics per the binding contract: 0 = immediate). `internal/tools/console_close.go`:
`ConsoleClose` (`console_close`; `ReadOnly() true`, no Subprocess marker, `DefaultOff() true`) —
Execute: lookup → drain unread → `registry.Close(id)` → result `output + "exited with code N"`
or `killed`. Both descriptions say "read-only: never prompts" and, for read, "poll a running
program; use `wait_ms` to wait for new output instead of polling in a loop". Constructors
`NewConsoleRead()` / `NewConsoleClose()`; `doc.go` lines. Plan-mode admission follows from the
RO class automatically (`planAdmits`, `internal/agent/resolution.go`) — assert it in the tests,
do not add engine code.

**Files:** `internal/tools/console_read.go`, `internal/tools/console_close.go`,
`internal/tools/doc.go`, `internal/tools/console_read_test.go`,
`internal/tools/console_close_test.go`

**Tests:** read with `wait_ms: 0` on a quiet Console returns empty output + `alive`; read with
`wait_ms: 2000` returns as soon as `echo late` (sent from the test through the Console's `Write`)
lands, well under 2 s; the dropped-bytes prefix appears after overflowing the ring with `yes |
head -c 2000000`; close returns the exit code and a second close is an error result; both tools
are `IsReadOnly`, not `IsSubprocessTool`, `DefaultOff()`; `classifyTool` (or `planAdmits`) admits
both and refuses `console_open`/`console_send` in Plan.

**Acceptance:** `go build ./... && go test ./internal/tools/ -run 'Console' && go test
./internal/agent/ -run 'Plan|Classify'`

**Commit:** `feat(tools): console_read and console_close — the read-only half of the Console family`

---

## 6. Registration, the default-off rung, and the roster tests — ✅ DONE (2026-08-25)

NOTES (2026-08-25): the `TestWave4WriteToolsCoversEveryWorkspaceWritingBuiltin` pin
(`internal/mechanisms/decompose_test.go`) was amended by LIFTING the whole build rung
(`Enabled: tools.KnownToolNames()`) rather than by comparing against a composed set as the item's
text suggests. The pin's job is to fail when a new write-capable built-in reaches the codebase
unclassified; walking the composed menu would have let a default-off write tool land unclassified
and only fire the day a roster turned it on. Lifting by name keeps the existing menu-length
assertion meaningful (menu == KnownToolNames) and needs no second set.
NOTES (2026-08-25): `TestKnownToolNamesCoversTheComposedSet` derives the default-off set from
`builtinTools` rather than hard-coding four, and asserts the family against it — so a future
default-off tool that forgets its `DefaultOff()` declaration, or one that leaks onto the composed
menu, fails here too rather than only shifting a count.
NOTES (2026-08-25): pre-existing prose drift left untouched (out of this item's scope) —
`internal/tools/doc.go`'s file map opens "Thirty files carry the built-ins" while the package holds
43 non-test files; it already read "Thirty" at 39 files before this plan run began. The structural
`TestDocMapNamesEveryFile` pins the NAMES, not the count, so nothing fails.

Depends on item 5.

**What:** `internal/tools/registry.go:189-224` `builtinTools`: append the four tools (open/send
built with `root` and `host.SecretEnvVars`). Because they are the first `DefaultOffTool`s, the
composed default menu is unchanged (still 25) but `KnownToolNames()` grows by four — amend the
tests that pinned "no built-in ships default-off": `internal/tools/registry_test.go:355`
`TestKnownToolNamesCoversTheComposedSet` (known = composed + the default-off set; assert the
four are known and absent from the composed menu), `internal/tools/roster_test.go:194-201`
`TestDefaultToolsHonourTheRoster` (DefaultTools == builtinTools minus default-off; add the
positive case: `HostTools{Enabled: []string{"console_open", …}}` lifts them, a profile
`Enabled` lifts them over a global `Disabled`, and a profile `Disabled` keeps them off);
`internal/tools/registry_test.go:216` read-only map gains four rows (asserted on a lifted
roster); `internal/mechanisms/decompose_test.go:340-342` (menu length vs `KnownToolNames` — use
the composed set the way the registry test now does) and add `console_open`/`console_send` to
`writeCapableNonFileBuiltins` (:303). Update the `HostTools.Enabled` doc comment
(`registry.go:41-48`, "no built-in ships default-off" is now false: name the family) and the
`DefaultOffTool` comment in `internal/domain/tools.go:135-160` ("first user: the Console family,
ADR 0059"). `internal/tools/doc.go` package overview: one paragraph on the family.

**Files:** `internal/tools/registry.go`, `internal/tools/registry_test.go`,
`internal/tools/roster_test.go`, `internal/tools/doc.go`, `internal/domain/tools.go`,
`internal/mechanisms/decompose_test.go`

**Tests:** as amended above, plus: `NewDefaultRegistry` without any lift has no `console_*` tool;
`EffectiveRoster` with global `Enabled` has all four in deterministic menu order (snapshot
updated); `RosterConflicts` on `{enabled: [console_open], disabled: [console_open]}` reports the
conflict and disabled wins.

**Acceptance:** `go build ./... && go test ./internal/tools/ ./internal/domain/
./internal/mechanisms/ ./internal/config/`

**Commit:** `feat(tools): register the Console family default-off — the first tools on ADR 0057's build rung`

---

## 7. The shipped Qwen3.8 roster, ADR 0057 §6 amendment, and the config template

Depends on item 6.

**What:** `internal/profiles/shipped.go:13` — add an entry `{Pattern: "qwen3.8", Profile:
domain.ModelProfile{Tools: domain.ToolRosterDelta{Enabled: []string{"console_open",
"console_send", "console_read", "console_close"}}}, SpellsTools: true, Note: "qwen3.8: the Console
family — the model that asked for it by name (ADR 0059 §3)"}` (matching is case-insensitive
substring, `internal/profiles/match.go:124-129`). Update `internal/profiles/entry.go:28`'s
comment (a shipped entry MAY carry a roster once ratified — this one is). ADR 0057 decision 6:
append a dated amendment line — *(Amended 2026-08-25 by ADR 0059 §3: the first shipped tools
axis is the Console family on the qwen3.8 entry; the rule stands — each shipped roster is its own
ratification.)* `CONTEXT.md:273` (Model profile entry, "shipped table carries wire-shape axes
only — never a roster"): reword to "carries a roster only where an ADR ratified one (first:
the Console family for Qwen3.8, ADR 0059)". `internal/config/defaults/config.yaml:395-420`
(`tools:` block comment): name the four default-off tools and show the `enabled:` lift;
`:768-825` (`model-profiles:` `tools:` docs): one line noting the shipped qwen3.8 roster and
how a user entry `tools: {disabled: [console_open, …]}` turns it back off.

**Files:** `internal/profiles/shipped.go`, `internal/profiles/entry.go`,
`internal/profiles/shipped_test.go`, `docs/adr/0057-the-tool-roster-is-a-third-model-profile-axis-resolved-axis-wise.md`,
`CONTEXT.md`, `internal/config/defaults/config.yaml`

**Tests:** `Match("Qwen3.8-27B-Instruct")` resolves a roster enabling the four and zero wire-shape
axes; `Match("qwen3-32b")` (no entry) has an empty roster; a user entry for `qwen3.8` with
`tools: {disabled: [console_open]}` disables only that one (axis-wise: profile beats shipped);
the rebind notice (`cmd/apogee/modelprofile.go:80` `rosterDeltaNotice`) for a switch to a qwen3.8
model reads `tools: +console_open +console_send +console_read +console_close (profile)` — add or
extend the existing notice test; `go test ./internal/config/` still passes with the template
edit (the embedded default must parse).

**Acceptance:** `go build ./... && go test ./internal/profiles/ ./internal/config/ ./cmd/apogee/
-run 'Profile|Roster|Notice|Default'`

**Commit:** `feat(profiles): ship the Console family enabled for qwen3.8 — the first shipped tools axis`

---

## 8. TUI presenters for the four tools

Depends on item 5. Files are disjoint from items 6 and 7.

**What:** `internal/tui/toolregistry.go`: four `toolRegistry` entries following `terminal`'s
(`:289`) and `python_exec`'s (`:296`) shape — `console_open` (label "console", verb "open",
target = the `command` argument), `console_send` (verb "send", target = `console <id>`, detail =
the `input` argument single-lined and truncated by the row budget), `console_read` (verb "read",
target `console <id>`), `console_close` (verb "close", target `console <id>`); `failure`/`stat`
hooks key on the result's trailing `exited with code N` / `killed` line the way
`exitCodeFailure`/`exitCodeStat` key on `terminal`'s exit line (extend or reuse those helpers —
one helper, two spellings, is duplication; parametrise it). Approval rendering needs no change:
`internal/tui/approval.go:273-275` already flattens `ApprovalRequest.Scope` to one line — add
a test that a `console_send` request renders `Scope: → console 3`. Row-budget invariant per
`docs/layout/tool-layout.md` and `internal/tui/toolshape_test.go:226` must hold for all four.

**Files:** `internal/tui/toolregistry.go`, `internal/tui/toolshape_test.go`,
`internal/tui/approval_test.go`

**Tests:** each entry's label/verb/target for a sample call; the exited-with-code stat on a send
result; the row-budget walk covers the four names; the approval Scope line.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'Tool|Shape|Approval|Console'`

**Commit:** `feat(tui): present the Console family's calls and exit stats`

---

## 9. User docs and the register

Depends on items 7 and 8.

**What:** `docs/manual/configuration.md:66-90` — the `tools:` section: "built-in tools are all on
by default" becomes "all but the default-off Console family"; document the lift (`tools.enabled:`
or a profile) and the shipped qwen3.8 roster; `:329` env-scrub sentence adds `console_open`.
Add a short "Console" subsection to whichever manual page documents the tool surface (find it by
`grep -l terminal docs/manual/*.md`; create `docs/manual/tools.md` only if none exists): what
the four tools do, that a Console never survives a restart or `/new`, the cap of 4, Windows not
yet. `README.md:45` ("a 27-tool suite …"): "… plus a default-off Console family for interactive
programs"; `:160` per-model roster line mentions the shipped qwen3.8 example. `docs/design/
technical-design.md`: the tool table gains the four rows (default-off column or note).
`ISSUES.md`: remove the `[P] Console family` line from the tool-surface entry (the work is done —
the closed trail is `CHANGELOG.md`); leave arm (g) and the rest untouched. Docs-only item: no
`make check` needed for its commit.

**Files:** `docs/manual/configuration.md`, `docs/manual/tools.md` (or the existing tool page),
`README.md`, `docs/design/technical-design.md`, `ISSUES.md`

**Tests:** none (docs). Verifier checks: every tool name in the docs matches `KnownToolNames()`
(`grep -o 'console_[a-z]*' docs/manual/*.md README.md | sort -u` equals the four); ISSUES.md
has no `Console family` line; `git diff --stat` touches only the listed files.

**Acceptance:** `grep -c 'console_open' docs/manual/configuration.md README.md && ! grep -n
'Console family' ISSUES.md`

**Commit:** `docs: document the Console family and close its register line`

---

## Suggested version bump

A new tool family with a new dependency and a shipped-roster change warrants a **minor** bump
(v0.17.0 was already suggested for the pending `[Unreleased]` rollup; this plan's work lands
under the same heading and strengthens that case). No item changes `VERSION` — the owner decides.
