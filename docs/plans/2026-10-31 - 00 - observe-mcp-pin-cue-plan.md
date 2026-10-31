# Observe `mcp:` pin re-ask cue — plan

**Goal:** an adopted repo observe (`run:`) entry whose handler is `mcp:` and whose adoption pin
moved (or could not be checked) fires the Driver's re-ask cue (`domain.Config.PinMoved`) once per
refused call, as the sync-lane and argv project paths already do, so the adoption pane asks again
in-session.
**Date:** 2026-10-31
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 90fdc1b6

**Regression check (2026-10-31, 90fdc1b6):** all items SAFE.

**Sources:**
- `.capcom/issues/observe-mcp-pin-cue.md`
- ADR 0104 §4 (`capcom know show "ADR 0104"`), `adr-0076-amend-1029`
- Glossary: `capcom know show "Project pin"`, `capcom know show "Reaction"`
- `internal/domain/config.go` — `Config.PinMoved` doc (goroutine-safe, non-blocking, once per refused spawn)
- `internal/agent/syncexec.go` — `checkProjectPin`, `notePinMoved` (the reference behaviour)

**Ratified design calls:**
- **Cue seam:** `reactions.DefaultExecutor` takes the cue as a plain `func()` handed in by the root, beside its `resolve`; the MCP call is not routed through the Agent. (Writer's call, 2026-10-31 — no user-visible difference.)
- **What cues:** only a `*domain.PinMovedError` from the pin's `Admit` cues; a nil pin (`errNoProjectPin`) does not, matching `checkProjectPin`.

**Standing requirements:**
- skills: coding-standards
- This machine: never `go test -race` over a whole heavy package or pattern; narrow to one package and one test with `GOMEMLIMIT=2GiB` (AGENTS.local.md). Pass this to every sub-agent.

**Out of scope:**
- The argv project path and the sync lane (they already cue).
- A Firing / headless / daemon cue: those Drivers inject no MCP resolver, so an `mcp:` entry fails as not connected before its pin is read.
- Changing what the refusal reports or how the Runner suppresses repeat reports.

## 1. Cue `Config.PinMoved` when an observe `mcp:` repo entry's pin has moved

**Closes:** observe-mcp-pin-cue

**What:**
Regression-class gap deferred from plan `2026-10-29 - 01 - mcp-reaction-handler-plan`: the async
`mcp:` half refuses a moved pin but never cues the Driver.

**Goal:** `reactions.DefaultExecutor` accepts a pin-moved cue (`func()`, nil allowed); its `mcp:`
half calls that cue exactly once when an adopted repo entry is refused because its pin's `Admit`
returned a `*domain.PinMovedError`, and never for a nil pin, an admitted pin, or a global entry. The
root Driver (`cmd/apogee/wire_boot.go`) hands it the same cue it sets as `Config.PinMoved`
(`w.reloadWake.poke`); `firingHooks` hands it nil.

**Approach (assumed at the header base):**
- `internal/reactions/exec.go`: add a `pinMoved func()` field to `mcpCaller` and a matching last
  parameter to `DefaultExecutor` (doc comment paragraph beside `resolve`'s: what it is, nil drops
  the cue, must be goroutine-safe and non-blocking — it runs on the Runner's workers).
  `admitProjectPin` stays the one pin check; `mcpCaller.Run` tests its error with
  `errors.As(err, &*domain.PinMovedError)` and calls the cue when set. The command half is
  untouched: it reaches the pin through the Agent's `ProjectRun`, which already cues.
- Every `DefaultExecutor` caller gains the argument: `cmd/apogee/wire_boot.go` `resolveConfig`
  (`w.reloadWake.poke`, comment updated to name the cue), `cmd/apogee/wire_firing.go`
  `firingHooks` (nil), and the test callers in `internal/reactions/{command,webhook,runner}_test.go`
  (nil unless the test needs the cue).
- Prose guard: every comment that says the async/observe `mcp:` path does not cue, or that the
  cue is fired only by the sync lane and argv path, is corrected —
  `grep -rn "PinMoved\|re-ask cue\|reloadWake" internal/reactions internal/domain cmd/apogee --include=*.go`.

**Files:** internal/reactions/exec.go; internal/reactions/runner_test.go; internal/reactions/command_test.go; internal/reactions/webhook_test.go; cmd/apogee/wire_boot.go; cmd/apogee/wire_firing.go
**Read first:** internal/reactions/exec.go — DefaultExecutor, mcpCaller.Run, admitProjectPin; internal/agent/syncexec.go — checkProjectPin, notePinMoved;
internal/reactions/runner_test.go — TestMCPObserveProjectEntryPassesItsPinFirst, mcpPinStub; cmd/apogee/wire_boot.go — resolveConfig, newRootWiringWith;
cmd/apogee/wire_server.go — reloadWake.poke; cmd/apogee/wire_firing.go — firingHooks; internal/domain/config.go — Config.PinMoved; internal/domain/reaction.go — PinMovedError

**Tests:**
- Extend `TestMCPObserveProjectEntryPassesItsPinFirst` (`internal/reactions/runner_test.go`) with a
  counting cue: `moved pin` → 1 call; `nil pin`, `admitted` → 0 calls.
- New `TestMCPObserveGlobalEntryNeverCuesPinMoved`: a non-project entry whose resolver fails → 0 calls.
- Must fail against the pre-item tree (bite check): the moved-pin case counts 0 there.

**Acceptance:**
- `go build ./...`
- `go vet ./internal/reactions/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPObserve' ./internal/reactions/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFiring|TestStartupSkipsARepoServer' ./cmd/apogee/`

**Commit:** `fix(reactions): cue the re-ask when an observe mcp: repo entry's pin moved`
