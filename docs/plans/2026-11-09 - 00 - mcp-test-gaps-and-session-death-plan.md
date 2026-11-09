# MCP report and resume test gaps, and MCP session-death detection — plan

- **Goal:** close the two MCP test gaps left by ADR 0111's plan (`/mcp` in the shared report-pane tests; the
  seed on a resumed session's journal), and notice an MCP server that dies mid-session so `/mcp`, the `mcp`
  panel section and the transcript show it as `disconnected (lost)` without waiting for a tool call.
- **Date:** 2026-11-09
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **base:** 1eaef158
- **Sources:** ADR 0111 (`capcom know show "ADR 0111" --with-children`, D8 and D9); ADR 0110 D4; ADR 0008;
  glossary `mcp-client`, `Event`; `docs/plans/archived/2026-11-08 - 03 - adr-0111-navigation-and-mcp-status-plan.md`
  items 15–18; `docs/design/test-drivers.md`; issues named on `**Closes:**` lines.
- **Ratified design calls** (owner, 2026-11-09):
  - **Detection:** one watcher per live session on the SDK's `ClientSession.Wait()`; no keep-alive ping, no fixed
    deadline (SDK `ClientOptions.KeepAlive` is rejected: tight timeout, kills on first miss).
  - **State:** a death is `disconnected` with the new reason `lost` and a redacted `Cause`; no new phase.
  - **Tools:** the dead server is dropped like a removed one: retired at once (calls answer "not connected"),
    its tools leave the menu at the next idle-point commit.
  - **Reconnect:** never automatic; the death marks the server failed so the `/settings` retry re-dials it.
  - **Model:** not told (no notice, no Reaction). **Human:** a one-line transcript note, on the path the
    removed/denied disconnect already uses.
  - **Scope:** stdio, SSE and streamable-http (while its standalone stream is up). An HTTP server without a
    standalone stream is noticed only on its next call — documented, not fixed. `list_changed` is out.
- **Standing requirements:**
  - skills: coding-standards
  - Tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run '<TestX>' ./<one package>/`; never a package
    pattern, never two test runs at once; pass this rule to every sub-agent.
  - Bubble Tea v2 names (`tea.KeyPressMsg`, `msg.String()`).
  - Durations in tests go through `internal/testtime`; no fixed deadline in shipped code (slow-box rule).
  - MCP server names and causes are untrusted text: redacted and escape-stripped as existing causes are.
- **Out of scope:** `list_changed` / `ToolListChangedHandler`; automatic reconnect; a keep-alive prober for
  HTTP servers without a standalone stream; telling the model; headless/daemon MCP (MCP lives only in the TUI's
  `rootWiring`).

**Regression check (2026-11-09, 1eaef158):**
- 1: guard folded (the two other `paneFixtures()` walkers join the Acceptance run).
- 2: guard folded (writer's decision: the `mcpSeed` assignment lives in `wireSession`, `cmd/apogee/wire_live.go`).
- 3: guard folded (writer's decision on the nil-`Wait` cause; reviewer guards on the in-memory case, `ProcessState` and a once-only close).
- 4: guard folded (writer's decision on SDK-ended connections; reviewer guards on watcher lifetime, refused rebuild, root aliases/docs and the startup window).
- 5: guard folded (cause kept on a lost row via `internal/sidepanel/mcp.go` and the `mcpRows` gate; transcript path named); supersedes the "no … transcript entry" comments at `internal/tui/fold.go:50-52` and `internal/tui/model.go:1322-1324`.
- 6: guard folded (writer's decision on the wait budget; reviewer guard: a prompt births the journal before the kill).
- 7: guard folded (writer's decision on what the docs state; reviewer guards: wider sweep, three more files, the sweep as Acceptance).

## 1. Cover `/mcp` in the shared report-pane and frame-order tests — ✅ DONE (2026-11-09)

NOTES (2026-11-09): the window height of the renamed frame-order test stays at 60 — the five panes seat at 80x60; the reportpane_test.go section header now names /mcp among the reports through one body.
NOTES (2026-11-09): shipped-code comments still say "the four reports" (internal/tui/mouse.go:338, internal/tui/model.go:3662, internal/tui/reportpane.go:106) and pointershape_test.go's fourReportsModel rows omit /mcp — pre-existing, outside this test-only item.

**What:** test-only. `reportCases` (`internal/tui/reportpane_test.go`) gains a `{"/mcp", mcpReport, mcpPaneModel(t, 40)}`
case; `mcpPaneModel(t, n)` folds `n` `mcpConnected(fmt.Sprintf("s%02d", i), 1)` Events through `foldMCPEvents`,
opens the pane (`m.mcpPane = reportPane{open: true}`) and calls `m.layout()`, with enough servers to scroll.
`TestTheFourReportsPaintInTheFramePaneOrder` (`internal/tui/panes_test.go`) is renamed
`TestTheFiveReportsPaintInTheFramePaneOrder` and stacks all five, `/mcp` (title `mcpTitle`) last, raising the
window height if the five panes do not seat. The adjacent gap goes too: `paneFixtures()` and
`overlayHeightQueryGroupPanes[overlayHeightReports]` gain their `paneMCP` row, so the overlay-height sweeps
claim every row of the pane table.
**Regression guard.** The new `paneMCP` row of `paneFixtures()` also feeds `TestOverlayPanesRenderOncePerUpdateAndView`
(`internal/tui/paintcache_test.go`) and `TestSlotPanesFitBesideTheSidePanel` (`internal/tui/model_test.go`); both walkers,
and `TestEveryFramePaneHasASpec`, join the Acceptance run so a red there surfaces at this item, not at closeout.
**Files:** internal/tui/reportpane_test.go; internal/tui/panes_test.go; internal/tui/mcppane_test.go
**Read first:** internal/tui/reportpane_test.go — reportCases, TestRenderReportLaysItsRowsOutOnce; internal/tui/panes_test.go —
TestTheFourReportsPaintInTheFramePaneOrder, paneFixtures, overlayHeightQueryGroupPanes; internal/tui/mcppane_test.go — foldMCPEvents;
internal/tui/paintcache_test.go — TestOverlayPanesRenderOncePerUpdateAndView; internal/tui/model_test.go — TestSlotPanesFitBesideTheSidePanel
**Tests:** every test ranging over `reportCases` now runs a `/mcp` case; the renamed frame-order test; the two other
`paneFixtures()` walkers, `TestOverlayPanesRenderOncePerUpdateAndView` and `TestSlotPanesFitBesideTheSidePanel`, now run a
`paneMCP` row.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestReport|TestTheFiveReportsPaintInTheFramePaneOrder|TestOverlayHeight|TestRenderReport|TestOverlayPanesRenderOncePerUpdateAndView|TestSlotPanesFitBesideTheSidePanel|TestEveryFramePaneHasASpec' ./internal/tui/`
- `grep -n '"/mcp"' internal/tui/reportpane_test.go` prints the case.
**Closes:** mcp-report-order-untested
**Commit:** `test(tui): cover /mcp in the shared report-pane and frame-order tests`

## 2. E2E: a resumed session's journal opens on the MCP seed — ✅ DONE (2026-11-09)

NOTES (2026-11-09): mutation check run — moving `w.host.mcpSeed = w.mcpSet.seedJournal` below `attachEventJournal` in `wireSession` (cmd/apogee/wire_live.go) fails the new test ("the resumed journal opens on []"); reverted, wire_live.go untouched.

**What:** test-only; the code already seeds at `openEventJournalLocked` (`cmd/apogee/wire_session.go`) on the
resume path through `attachEventJournal`. A new e2e test `TestE2EMCPResumeSeedsTheJournal` in
`cmd/apogee/e2e_mcpreconnect_test.go` launches with at least two configured MCP servers (the
`mcpSeedScript` / `newPingMCPServer` setup the startup-seed test uses), sends a prompt so a record is saved,
quits, relaunches with `RelaunchWith("--continue")`, and asserts the resumed journal (`liveJournal`) carries one
`mcp_server` line per configured server in the run of lines immediately after `run_started`, ahead of any
other `mcp_server` line. A new helper reads that run without requiring a `prompt` line (`journalSeed` requires
one, and a resumed journal is born at boot before any prompt).
**Regression guard.** the `mcpSeed` assignment the mutation check moves lives in `wireSession` (cmd/apogee/wire_live.go), not wire_session.go
**Files:** cmd/apogee/e2e_mcpreconnect_test.go
**Read first:** cmd/apogee/wire_live.go — wireSession; cmd/apogee/wire_session.go — attachEventJournal, openEventJournalLocked;
cmd/apogee/e2e_mcpreconnect_test.go — liveJournal, journalSeed, mcpSeedScript, TestE2EMCPStartupStateSeedsTheJournal;
cmd/apogee/e2e_support_test.go — e2eSession.RelaunchWith
**Tests:** the new e2e test; it must fail if the `mcpSeed` assignment in `wireSession` (`cmd/apogee/wire_live.go`) is moved below
`attachEventJournal` (check by mutation, then revert).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EMCPResumeSeedsTheJournal|TestE2EMCPStartupStateSeedsTheJournal' ./cmd/apogee/`
**Closes:** mcp-seed-resume-untested
**Commit:** `test(apogee): pin the MCP seed on a resumed session's journal`

## 3. `mcp.Client` reports a session that ends on its own — ✅ DONE (2026-11-09)

NOTES (2026-11-09): the in-memory case uses the factored tracking step `Client.track(liveSession, redactor)` (connectOne now records every session through it) rather than an httptest server; a nil `Wait` error over a stdio session reads "exit status 0" (the ladder hands back `cmd.Wait`'s own error, nil only on a zero exit), over any other transport "connection closed". No CHANGELOG entry: the signal has no user-visible effect until items 4–5 consume it.

**What:** **Goal:** an `mcp.Client` exposes a signal that fires exactly when one of its sessions ends while the
Client is neither retired nor closed, carrying the ending's cause; a deliberate `Retire` or `Close` never
fires it and leaves no goroutine behind.
**Approach (assumed at the header base):** extends `internal/mcp/client.go`; no new module. `connectOne`
starts one goroutine per `liveSession` on `session.Wait()`; on return it checks `retired` and, if unset,
records the cause (the `Wait` error, or the stdio process's exit status from `cmd`) and closes a channel.
Exposed as `Lost() <-chan struct{}` and `LostCause() error` on `*mcp.Client`. The cause is redacted the way
`serverTool.call` redacts transport errors. `Close` waits for the goroutine.
**Regression guard.** when `ClientSession.Wait()` returns a nil error (clean EOF), the cause is the stdio process's exit status when there is one, else the fixed text "connection closed" — `LostCause` is never nil once `Lost` has closed.
Reviewer guards, checked at 1eaef158: that exit status is read from `Wait`'s own error (the stdio ladder's `cmd.Wait`,
`stdinLadder.Close`), never from `cmd.ProcessState`, which the ladder's goroutine may still be writing (a `-race` read);
the lost channel closes under a `sync.Once`, since a Client holds several sessions (`Client.sessions`). A Client is
built only from a `ServerConfig` (no in-memory transport), so the in-memory case needs the "record session + start
watcher" step factored out of `connectOne` (e.g. `c.track(liveSession)`) for a hand-built Client, or is replaced by an
`httptest` SSE/streamable server the test shuts down.
**Files:** internal/mcp/client.go; internal/mcp/mcp_test.go
**Read first:** internal/mcp/client.go — connectOne, Close, Retire, liveSession; internal/mcp/transport.go — stdinLadder.Close;
go-sdk@v1.6.1 internal/jsonrpc2/conn.go — Connection.wait; internal/mcp/mcp_test.go — stdioServerConfig, assertNoGoroutineIn
**Tests:** kill the stdio fixture's process (`stdioServerConfig`, `c.sessions[0].cmd.Process.Kill()`) → `Lost`
closes, `LostCause` non-nil; an SDK session from `NewInMemoryTransports` registered on a hand-built Client through the
factored tracking step (or an `httptest` SSE/streamable server the test shuts down), closed from the server side →
`Lost` closes, `LostCause` reads "connection closed"; two sessions of one Client ending → no double close; `Retire` then
`Close` → `Lost` never closes; `Close` alone → never closes, goroutine gone (leak check as the package does).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestClientLost' ./internal/mcp/`
- `go vet ./internal/mcp/`
**Commit:** `feat(mcp): report a session that ends on its own`

## 4. `liveMCP` drops a lost server and journals `disconnected (lost)` — ✅ DONE (2026-11-09)

NOTES (2026-11-09): watchers are kept per session in the holder (`liveMCP.watches`, a stop channel per installed session, closed in `closeOffLoopLocked` and `close`) rather than a stop field on `mcpEntry`, since an installed entry map is never written again and test fixtures build entries as literals; `close` stops every watcher and waits for them (`watching`).
NOTES (2026-11-09): committed dials are watched from `settleLocked` on (never while pending); a refused rebuild's restore (`liveMCP.restore`) drops a put-back session whose `Lost()` has closed as lost and re-arms the rest. A loss marks the server failed only when nothing else is bringing it back — `canRedialDroppedLocked`'s guard-free half was factored out as `isUnservedLocked` and shared — so a pending successful dial or one in flight decides the server's state instead.
NOTES (2026-11-09): the lost cause goes through `mcpSentence` (de-labelled, as a failed dial's cause); escape-stripping stays at render time, as for every other cause. The TUI, panel and docs wording ("closed on purpose" in layout.md, docs/layout/side-panel-layout.md, docs/design/mcp-client.md, the mcp-client glossary Note) are items 5 and 7.
NOTES (2026-11-09): fix-retry — a commit is marked in flight under the lock (`liveMCP.isCommitting`, set in `commit`, cleared in `restore` and before `settleLocked`); a loss watcher that fires while it is stands down (unwatches) instead of acting on the provisional entry set. The commit's end takes the loss in through one shared `rewatchLocked` (restore and settleLocked): a session whose `Lost()` has closed is dropped as lost there and then, synchronously — not by an async re-watch, which raced a caller's `closes.Wait` — so a refused commit reports it once and a landed one leaves the rebuild wanted.
NOTES (2026-11-09): `TestMCPLostUnchangedServerDuringACommit` (refused and landed) kills an unchanged server inside SwapTools while another server's dial lands, and holds the swap until the watcher has acted (`isMCPWatched`); without the stand-down it fails with two `lost` events (refused) and no rebuild wanted with the dead tool still swapped in (landed).

**What:** **Goal:** when an installed MCP session is lost, the composition root posts one `MCPServerEvent`
`{Phase: disconnected, Reason: lost, Cause: <redacted>}`, retires the server, withdraws its tools at the next
idle-point commit and marks it failed so the `/settings` retry re-dials it; a loss during or after shutdown,
supersede, removal or commit-replacement posts nothing. Depends on item 3.
**Approach (assumed at the header base):** `internal/domain/events.go` gains `MCPDisconnectLost` and its doc
comment stops saying disconnected is only "closed on purpose". `mcpSession` (`cmd/apogee/wire.go`) gains the
item-3 methods; `fakeMCPSession` (`cmd/apogee/wire_helpers_test.go`) gains `die(err)`. In
`cmd/apogee/wire_mcp.go`, `liveMCP` starts a watcher per installed session (startup entries and committed
dials); on loss it re-checks under `m.mu` that `!isStopped` and `m.entries[name].session` is still that
session, then `dropLiveLocked`, sets the server's failed state (`hasFailed`) and calls
`postDisconnectedLocked(lost)` with the cause. `commit`'s restore of `pending.previous` after a refused rebuild
must not re-install a session already lost; a dial in flight for the server finishes with `Kept=false`.
**Regression guard.** any ending the SDK reports on its own counts as lost — process exit, an oversize stdio message (the bounded reader) and a non-transient streamable POST failure all end the whole connection and post `disconnected (lost)` with that cause; item 4's tests include the oversize-message case.
Reviewer guards, checked at 1eaef158: each watcher selects on `Lost()` AND a per-entry stop channel closed when the
entry is dropped, superseded or closed (`mcpEntry.close` / `closeOffLoopLocked`), and `liveMCP.close` waits for the
watchers — Retire/Close never fire `Lost`, so a watcher on `Lost()` alone leaks one goroutine per reconnect. On a
refused rebuild's restore of `pending.previous`, drop every restored session whose `Lost()` is closed and post its
`disconnected(lost)` + `hasFailed`; a loss of a session that is `state.result`'s clears `state.result` (or start a
committed dial's watcher only after `settleLocked`). `wire_mcp.go` names reasons through the root package's aliases, so
`apogee.go` gains the `MCPDisconnectLost` alias and its "closed on purpose" comment is fixed; the `Cause` doc on
`MCPServerEvent` (`internal/domain/events.go`) and `mcpServerData`'s doc (`internal/eventjson/encode.go`), both "cause on
failed only", name the lost cause. Startup watchers start in `emitThrough` (after the outbox exists), or `emitThrough`
seeds a server already lost as `disconnected(lost)`: `newLiveMCP` runs before `emitThrough` in `wire_live.go`.
**Files:** internal/domain/events.go; apogee.go; internal/eventjson/encode.go; cmd/apogee/wire.go; cmd/apogee/wire_mcp.go; cmd/apogee/wire_helpers_test.go; cmd/apogee/wire_mcp_test.go
**Read first:** cmd/apogee/wire_mcp.go — liveMCP.dropLiveLocked, commit, settleLocked, postDisconnectedLocked, emitThrough, close;
apogee.go — MCPDisconnectRemoved/MCPDisconnectDenied aliases; cmd/apogee/wire_helpers_test.go — fakeMCPSession
**Tests:** unit in `cmd/apogee`: `die` → one `disconnected(lost)` with cause, `hasFailed`, rebuild wanted; `die`
with an oversize-message error → the same, carrying that cause; `die` after `close()` → nothing; `die` after a
supersede → nothing; no watcher goroutine remains after a supersede or after `close()`; die during a refused-rebuild
commit → not restored, and posts `disconnected(lost)`; a startup session lost before `emitThrough` → seen as
`disconnected(lost)`, not vanished; `RetryFailedMCP` re-dials the lost server.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPLost' ./cmd/apogee/`
- `go vet ./internal/domain/ ./cmd/apogee/`
**Commit:** `feat(apogee): notice an MCP server that dies mid-session`

## 5. Show a lost server in `/mcp`, the panel and the transcript

**What:** **Goal:** a `disconnected (lost)` Event renders in `/mcp` as `disconnected (lost)` with its cause, keeps
the server's row in the `mcp` panel section with zero tools, and writes one transcript note naming the server
and cause. Depends on item 4.
**Approach (assumed at the header base):** `mcpStateWord` (`internal/tui/mcppane.go`) shows the cause for a lost
disconnect as it does for `failed`; `sidepanel.MCPServers.Fold` (`internal/sidepanel/mcp.go`) already keeps
non-`removed` rows; the transcript note reuses the path that announces a removed/denied disconnect (find it from
`MCPDisconnectRemoved`'s TUI consumers); the note text is escape-stripped.
**Regression guard.** Checked at 1eaef158: `foldMCPRow` (`internal/sidepanel/mcp.go`) keeps `Cause` only on `failed`, and
`mcpRows` prints a cause only when `Phase == failed`; so `foldMCPRow` keeps `Cause` on a lost `disconnected` (fix the
`MCPServerRow` doc) and the `mcpRows` gate admits it — `mcpStateWord` stays as it is (it already reads `disconnected
(lost)`; a cause in the state cell would widen the column, which `mcppane.go`'s doc rules out). No removed/denied
transcript path exists to reuse (`foldEventAgainst` returns before any transcript write): the note is written either
live-only in `model.go`'s `MCPServerEvent` branch, or as an `mcpOutcome` via `CommitMCP`→`commitMCP` (lands only at an
idle commit); `TestMCPFoldEventIsNotEngineActivity` and the seeds-from-a-snapshot test stay green. This supersedes the
"no … transcript entry" comments at `internal/tui/fold.go:50-52` and `internal/tui/model.go:1322-1324`; the item amends them.
**Files:** internal/tui/mcppane.go; internal/tui/mcppane_test.go; internal/sidepanel/mcp.go; internal/sidepanel/mcp_test.go; internal/tui/fold.go; internal/tui/model.go; internal/tui/fold_test.go
**Read first:** internal/tui/mcppane.go — mcpStateWord, mcpRows; internal/sidepanel/mcp.go — foldMCPRow, MCPServerRow;
internal/tui/fold.go — foldEventAgainst; internal/tui/model.go — eventMsg MCPServerEvent branch; internal/tui/mcpreconnect.go — commitMCP;
internal/tui/fold_test.go — TestMCPFoldEventIsNotEngineActivity
**Tests:** fold a `disconnected(lost)` Event: `/mcp` row text with its cause on the wrapped cause line, panel row kept
with 0 tools and its `Cause`, one transcript note; a cause carrying an ANSI escape renders stripped;
`TestMCPFoldEventIsNotEngineActivity` and the seeds-from-a-snapshot test stay green.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCP' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCP' ./internal/sidepanel/`
**Commit:** `feat(tui): show a lost MCP server in /mcp, the panel and the transcript`

## 6. E2E: a killed MCP server reads `disconnected (lost)` in `/mcp`

**What:** test-only. `TestE2EMCPServerDeathReachesMCPPane` in `cmd/apogee/e2e_mcpreconnect_test.go` launches with a
streamable-http MCP server (a `newPingMCPServer` variant returning the `*httptest.Server`), waits for
`connected`, closes the server (`CloseClientConnections` then `Close`), and with no tool call waits
(`testtime.Eventually`, sized past the SDK's reconnect backoff) until `/mcp` reads `disconnected (lost)` and the
journal carries the matching `mcp_server` line. Depends on item 5.
**Regression guard.** size the wait as `testtime.Budget` well past the SDK's ~13 s standalone-GET retry backoff (5 tries at 1 s × 1.5^n)
Reviewer guard, checked at 1eaef158: a fresh session's Event journal is born only at its first prompt
(`attachEventJournal` returns while `ActiveID()` is ""), so before closing the server the test runs `mcpSeedScript`,
submits one prompt, waits for the reply and for `journalSeed` to report the seed, then kills the server; the lost
`mcp_server` line must follow the seed.
**Files:** cmd/apogee/e2e_mcpreconnect_test.go; cmd/apogee/mcp_cmd_test.go
**Read first:** cmd/apogee/e2e_mcpreconnect_test.go — liveJournal, mcpSeedScript, TestE2EMCPStartupStateSeedsTheJournal;
cmd/apogee/mcp_cmd_test.go — newPingMCPServer; cmd/apogee/wire_session.go — attachEventJournal, sessionHost.Prompt;
go-sdk@v1.6.1 mcp/streamable.go — calculateReconnectDelay; internal/testtime/testtime.go — Eventually
**Tests:** the new e2e test (one prompt seeds the journal before the kill; the lost line follows the seed).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EMCPServerDeathReachesMCPPane' ./cmd/apogee/`
**Closes:** mcp-session-death-undetected
**Commit:** `test(apogee): a killed MCP server reads disconnected (lost) in /mcp`

## 7. Docs: the lost reason in ADR 0111, the glossary and the manual

**What:** **Goal:** every doc that says an MCP `disconnected` is only "closed on purpose" or that death goes
unnoticed names the `lost` reason and its limits. Depends on item 6.
**Approach (assumed at the header base):** a dated amendment `adr-0111-amend-1109` (`capcom know add --id
adr-0111-amend-1109 --parent adr-0111`) records the `lost` reason, the watcher, the no-reconnect stance (D9
holds) and the HTTP-without-standalone-stream limit; named on ADR 0111's Status line and reading order. Glossary
`mcp-client` amended. `docs/manual/configuration.md` (MCP section) and the `/mcp` description in
`docs/manual/commands.md` name `lost`. Rule for the sweep: every doc site matching
`grep -rn 'closed on purpose\|not noticed\|dies mid-session' docs/ .capcom/knowledge/`.
**Regression guard.** the docs state that any connection the SDK ends on its own (crash, oversize message, failed POST) reads `disconnected (lost)`, and that an HTTP server without a standalone stream is noticed only on its next call.
Reviewer guards, checked at 1eaef158: the sweep rule widens to `grep -rn 'closed on purpose\|not noticed\|dies
mid-session\|not detected\|closed a live connection' docs/ .capcom/knowledge/`, which also hits `docs/manual/headless.md`
(the `mcp_server` row: reason `removed|denied`, cause only on failed), `docs/design/mcp-client.md`,
`docs/layout/side-panel-layout.md` and ADR 0111's Consequences ("still not detected", answered by the amendment); the
sweep runs as an Acceptance command.
**Files:** .capcom/knowledge/decisions/adr-0111.md; .capcom/knowledge/decisions/adr-0111/adr-0111-amend-1109.md; .capcom/knowledge/glossary/ (mcp-client Note); docs/manual/configuration.md; docs/manual/commands.md; docs/manual/headless.md; docs/design/mcp-client.md; docs/layout/side-panel-layout.md
**Read first:** .capcom/knowledge/decisions/adr-0111.md — Status line, reading order; .capcom/knowledge/decisions/adr-0111/adr-0111-consequences.md;
.capcom/knowledge/glossary/glossary-safety/mcp-client.md; docs/manual/headless.md — mcp_server row; docs/manual/commands.md — /mcp row;
docs/manual/configuration.md — mcp-servers section; docs/design/mcp-client.md; docs/layout/side-panel-layout.md
**Tests:** none (docs); `capcom check` clean; the sweep grep.
**Acceptance:**
- `capcom check`
- `capcom know show "ADR 0111" | grep -n 'amend-1109'`
- `grep -rn 'closed on purpose\|not noticed\|dies mid-session\|not detected\|closed a live connection' docs/ .capcom/knowledge/`
  prints only lines this item amended, ADR 0111 text the amendment answers, this plan's own lines, and the unrelated
  ADR 0042 hit.
**Commit:** `docs: record the lost MCP disconnect reason`
