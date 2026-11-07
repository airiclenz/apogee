# url-safety deny gaps for live MCP sessions — plan

**Goal:** A `url-safety:` edit that closes the endpoint a live MCP session runs on always closes that session on the keystroke, even when the config file does not parse. A server whose kept session it closes, and whose configured endpoint is still admitted, is re-dialled there.
**Date:** 2026-11-07
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** a373502a
**Sources:** ADR 0109 decisions 5 and 7 (`capcom know show "ADR 0109" --with-children`); `.capcom/issues/denied-endpoint-no-redial.md`; `.capcom/issues/load-error-skips-deny.md`; `docs/plans/archived/2026-11-07 - 00 - mcp-reconnect-off-loop-plan.md` (items 4 and 9); `docs/manual/configuration.md` (url-safety live apply); `docs/design/mcp-client.md`.
**Ratified design calls** (owner, 2026-11-07):
- **Re-dial on deny:** a deny that closes a server's kept session (its last dial failed, so the old session was serving) counts as touching that server. One dial to its configured endpoint is queued on the keystroke, mid-Turn included, when its last-requested verdict and the deny's lists both admit that endpoint and no dial for it is queued or running and no successful result is held. ADR 0109 D7 gets a dated amendment saying so.
**Standing requirements:**
- skills: coding-standards
- This box: never race-run a whole heavy package; narrow to `GOMEMLIMIT=2GiB go test -race -count=1 -run '<regex>' ./cmd/apogee/`, one run at a time.
- Durations in tests go through `internal/testtime`; `WaitQuiet` never stands in for readiness.
**Out of scope:**
- The `mcp-servers` row retry gesture and every other D7 path (a failed server whose session no deny closes stays un-dialled).
- The `/mcp` status view (`mcp-live-status-view`); headless and daemon Drivers.
**Regression check (2026-11-07, a373502a):**
- 1: recast — the re-dial predicate adds `state.isAdmitted` and admits a held failed result; the `sameServerNames` premise is dropped; tests, docs grep and Acceptance widened (owner decision).
- 2: guard folded — tests seed docs on its own configured endpoint; the load-error path may queue item 1's re-dial; comment rule widened and the in-flight-dial wording scoped to lists in force.
- 1 (re-check): guard folded — the ADR Acceptance grep names this plan's path (the `Amended 2026-11-07` grep already matches the 00-plan's D8 amendment); the refused-mid-Turn test seeds B denied through `allow-hosts [a.example.com]`; the docs grep adds `again only when`; every gatedDial seed closes `dial.release` before `drainMCP` (owner decision).

## 1. A deny that closes a failed server's kept session re-dials its configured endpoint

**What:**
Recast at the regression check (2026-11-07). Fixes `denied-endpoint-no-redial` (left over from the archived plan's item 9, 5ff32123). When a server's dial to its new endpoint B has failed, its old session on A is kept (D7). A `url-safety:` deny of A then closes that session, and nothing dials B, because `readmitMCP` sees the same admitted names and does not request.
**Regression guard.** The re-dial predicate is binding as: the server's state is known, `state.isAdmitted` (the last request's verdict, under the lists then in force), `mcpAdmits(state.config, guard)`, no job for it in `m.jobs`, no running dial (`state.cancel == nil`), and `state.result` either nil or a FAILED result (`result.err != nil`) — a held failed result is superseded by the new dial through `queueDialLocked`, so a deny landing after B's dial failed but before that failure was committed also re-dials (a "previous connection kept" note would be false once A is closed). Drop the "admitted under both lists (sameServerNames held)" premise everywhere in item 1: soundness rests on `state.isAdmitted` plus the after-guard check plus commit's re-admit under the live lists, on every path including item 2's load-error path; readmitMCP's "never dials under the old lists" rule is not reversed (`wire_settings.go` readmitMCP doc; `TestApplySettingURLSafetyWideningDialsUnderTheNewLists` doc in `wire_live_test.go`), because the dialled endpoint is admitted under both the in-force and the landing lists. Reword the header's "Re-dial on deny" ratified call to "…when its last-requested verdict and the deny's lists both admit that endpoint and no dial for it is queued or running and no successful result is held…" (owner, 2026-11-07). Re-check: the D7-amendment Acceptance grep is `grep -n "2026-11-07 - 01 - url-safety-mcp-deny-gaps-plan" .capcom/knowledge/decisions/adr-0109/adr-0109-decision.md` (the `Amended 2026-11-07` grep already matches the 00-plan's D8 amendment at base, line 48). The refused-mid-Turn test seeds B denied through the live `allow-hosts [a.example.com]`, never `deny-hosts [b]` (an `allow-hosts` edit's `after` keeps the live deny list, so the after-guard alone would suppress the dial and `state.isAdmitted` would go unpinned). The docs-rule grep adds `again only when` (the `request` doc splits "dialled" / "again only when" across lines). Every seed that drains a failed dial through gatedDial closes `dial.release` before calling drainMCP (drainMCP runs jobs inline and gatedDial.connect blocks on release).
**Goal:**
- A deny that closes a live session whose server's last-requested config was admitted at its last request and is admitted under the deny's guard, and that has no dial queued or running and holds no successful result, queues exactly one dial for that config under that guard. This holds at both deny sites: `liveMCP.dropDenied` (the `url-safety:` row) and the drop in `liveMCP.requestServerLocked`.
- A server with a dial already pending gets no second dial. A server whose configured endpoint is also denied gets no dial. A failed server whose session no deny closes is not re-dialled.
- ADR 0109 D7 and the user docs state the rule.
**Approach (assumed at the header base):**
- One private predicate on `liveMCP` (lock held): the state is known, `state.isAdmitted`, `mcpAdmits(state.config, guard)`, no job for the name in `m.jobs`, `state.cancel == nil`, and `state.result` nil or failed (`result.err != nil`; `queueDialLocked` supersedes it). Both deny sites call it after `dropLiveLocked` and then call `queueDialLocked(state, guard)`. In `requestServerLocked` this is a last `case` of the existing switch, keyed on "this call dropped the live entry". Extends `liveMCP` in `wire_mcp.go`; no new type, no new file.
- `denyMCP` and `readmitMCP` are unchanged. The dial is queued inside `dropDenied`, before the host-list `move`. `state.isAdmitted` admits the endpoint under the in-force lists, the after-guard check under the landing lists, and commit re-admits under the live lists, on every path including item 2's load-error path.
- Docs: a dated `> **Amended 2026-11-07 (<this plan's path>).**` blockquote under D7 in `.capcom/knowledge/decisions/adr-0109/adr-0109-decision.md`, and the amendment named on `adr-0109.md`'s Status line. One sentence each in `docs/manual/configuration.md` (url-safety live apply paragraph) and `docs/design/mcp-client.md` (the re-dial paragraph). Rule: every sentence listing what re-dials a failed server (or saying a `url-safety` edit never does) gains the exception — `wire_mcp.go`'s `requestServerLocked`/`request` doc ("dialled again only when …") and `docs/manual/configuration.md`'s "Nothing retries a failed server on its own" included; each new doc sentence uses the phrase "kept connection". Find them with `grep -rn "never re-dials\|untouched\|no automatic retry\|dialled again only\|again only when\|Nothing retries\|retried by" docs/manual docs/design .capcom/knowledge/decisions/adr-0109* cmd/apogee/wire_mcp.go cmd/apogee/wire_settings.go`.
**Files:** cmd/apogee/wire_mcp.go; cmd/apogee/wire_mcp_test.go; docs/manual/configuration.md; docs/design/mcp-client.md; .capcom/knowledge/decisions/adr-0109.md; .capcom/knowledge/decisions/adr-0109/adr-0109-decision.md
**Read first:** cmd/apogee/wire_mcp.go — liveMCP.dropDenied, requestServerLocked, queueDialLocked, supersedeLocked, storeDial, foldLocked, mcpFailed; cmd/apogee/wire_settings.go — applyURLSafetyHosts, settingsApplier.denyMCP, readmitMCP;
  cmd/apogee/wire_mcp_test.go — denyLiveEndpointThroughURLSafety, gatedDial, runJobsAsync, TestURLSafetyDenyOfTheLiveEndpointLeavesAFailedNewDialRetryable, denyOldEndpointMidDial; cmd/apogee/wire_helpers_test.go — drainMCP, requestAndDrainMCP, newMCPFixtureDialling, applySettingSpy;
  internal/tui/mcpreconnect.go — mcpAfterFold, startMCPDials; .capcom/knowledge/decisions/adr-0109/adr-0109-decision.md — decision 7
**Closes:** denied-endpoint-no-redial
**Tests:** (in `wire_mcp_test.go`, beside `denyLiveEndpointThroughURLSafety`)
- Seed (the failed-B seed every test below names): docs is live on A, its config is on B, and B's dial is drained and committed (`drainMCP` returns `mcp reconnect failed: docs: … — previous connection kept`, A kept) before the deny; then `dial.err = nil` so the re-dial lands. A seed that drains through `gatedDial` closes `dial.release` before calling `drainMCP`.
- `TestURLSafetyDenyOfAKeptSessionRedialsTheConfiguredEndpoint`: the seed above (`hasFailedDial()`). The row commits `url-safety.deny-hosts [a.example.com]`. The note is `toolRosterNote + "; mcp server docs disconnected — its endpoint is denied"`, A is retired, and `isDialQueued()`. After the drain, docs is on B, A is closed, and the notes are `["mcp docs connected"]`.
- `TestURLSafetyDenyOfAKeptSessionWithBothEndpointsDeniedDialsNothing`: the same seed, denying A and B. docs is retired and no dial is queued.
- `TestURLSafetyDenyLeavesAnUntouchedFailedServerUndialled`: a failed server whose kept session's endpoint the edit does not deny queues no dial.
- `TestLiveMCPRequestDenyOfAKeptSessionRedialsTheConfiguredEndpoint`: the `requestServerLocked` site, with the failed-B seed; it calls `fixture.set.request([]mcp.ServerConfig{onB}, mcpGuard(nil, []string{"a.example.com"}), false)` directly (never `requestAndDrainMCP`, whose `retryFailed=true` would queue B on its own), then asserts `isDialQueued()` before draining.
- `TestURLSafetyDenyRefusedMidTurnOfAKeptSessionOnADeniedConfigDialsNothing`: docs live on A under the live lists `allow-hosts [a.example.com]` (`fixture.tools.setAllowHosts`); docs is requested onB under them (`state.isAdmitted` false, the dial fails, drained). Then the row commits `url-safety.allow-hosts [b.example.com]` — denying A, admitting B — refused mid-Turn (`spy.swapErr`). `!isDialQueued()`.
- `TestURLSafetyDenyOfAKeptSessionWithAnUncommittedFailedDialRedials`: B's dial has failed but its result is held, not yet committed (no `drainMCP` commit); the deny of A queues a dial that supersedes the held result, and after the drain docs is on B and A is closed.
- `TestURLSafetyDenyOfTheLiveEndpointRetiresItWhileTheNewDialLands` also asserts `!fixture.set.isDialQueued()` right after the row returns (no second dial). The existing `…LeavesAFailedNewDialRetryable` tests stay green.
**Acceptance:**
- `go build ./...`; `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestURLSafetyDeny|TestLiveMCP|TestApplySettingURLSafety' ./cmd/apogee/`
- `grep -n "2026-11-07 - 01 - url-safety-mcp-deny-gaps-plan" .capcom/knowledge/decisions/adr-0109/adr-0109-decision.md` and `grep -n "decision 7" .capcom/knowledge/decisions/adr-0109.md` each match (the D7 amendment and its Status-line mention)
- `grep -n "kept connection" docs/manual/configuration.md docs/design/mcp-client.md` matches in both files (the new sentences)
- `capcom check`
**Commit:** `fix(mcp): a url-safety deny that closes a failed server's kept session re-dials its configured endpoint`

## 2. A url-safety deny closes denied MCP sessions when the config does not load

**What:**
Fixes `load-error-skips-deny`. `settingsApplier.denyMCP` returns `mcpReadmit{loadErr: err}` before any drop when the layered config fails to load, so a live session on a newly denied endpoint keeps serving. That breaks ADR 0109 decision 5 (the deny lands on the keystroke). With item 1 in, the load-error path may queue a re-dial from the last-requested config, which is expected.
**Regression guard.** Item 2's tests seed docs live on its own configured endpoint A, so item 1's re-dial queues nothing on the load-error path; the tests assert the note and the closed session only (a dial reports by its own note). State in item 2's What that, with item 1 in, the load-error path may queue a re-dial from the last-requested config, which is expected. Every comment saying a load error leaves the MCP connections untouched or retires nothing is corrected (`wire_settings_test.go`'s "a file it cannot parse leaves the connections where they are" included); find them with `grep -n "leaves the connections\|nothing was planned" cmd/apogee/wire_settings*.go`. The updated comments say the commit closes an in-flight dial to a now-denied endpoint only once the lists are in force (a refused `move` leaves the old lists live, so it lands); the retire-half gap on the load-error path stays named in `readmitMCP`'s comment, not claimed closed.
**Goal:**
- With the layered config unreadable, a `url-safety:` edit that denies a live session's endpoint closes that session before Apply returns.
- The row's note is `toolRosterNote` + the deny clause (`; mcp server <name> disconnected — its endpoint is denied`) + `mcpNoteFor(mcpReconnectFailed(loadErr))`, in that order. A refused `move` carries the deny clause too (`mcpReadmit.refused`).
- With nothing denied, the load-error note is byte-identical to today's.
**Approach (assumed at the header base):**
- In `denyMCP`, the `liveMCP.dropDenied` loop and its note clause run on the load-error path too, under the `after` guard. Extract it into one private helper used by both paths (one reason to change). The returned plan carries both `loadErr` and `denyNote`.
- `readmitMCP`'s `loadErr` branch returns `plan.denyNote + mcpNoteFor(mcpReconnectFailed(plan.loadErr))`. The retire half (newly denied configured servers) still needs the config and stays skipped, and `readmitMCP`'s comment keeps naming that gap. A dial still in flight to a now-denied endpoint is closed by the commit's re-admit under the live lists once those lists are in force; after a refused `move` the old lists stay live and it lands.
- Update the comments on `denyMCP`, `readmitMCP` and `mcpReadmit.loadErr` ("nothing was planned or retired under it"), and every other comment the guard's grep finds, to match.
**Files:** cmd/apogee/wire_settings.go; cmd/apogee/wire_settings_test.go
**Read first:** cmd/apogee/wire_settings.go — settingsApplier.denyMCP, settingsApplier.readmitMCP, mcpReadmit.refused; cmd/apogee/wire_mcp.go — liveMCP.dropDenied, liveMCP.dropLiveLocked; cmd/apogee/wire_settings_test.go — TestSettingsApplierReloadsRefuseAnUnparseableFile;
  cmd/apogee/wire_live_test.go — TestApplySettingURLSafetyDenyRefusedMidTurnNamesTheServer; cmd/apogee/wire_helpers_test.go — applySettingSpy, resolvedMCPServer (loads the file: resolve before breaking it, or build the config by hand)
**Closes:** load-error-skips-deny
**Tests:** (in `wire_settings_test.go`, beside `TestSettingsApplierReloadsRefuseAnUnparseableFile`)
- Both tests seed docs live on its own configured endpoint A (no dial queued by item 1's re-dial) and assert the note and the closed session only.
- `TestURLSafetyDenyWithAnUnreadableConfigClosesTheDeniedSession`: docs is live on A, the config file does not parse, and the row commits `url-safety.deny-hosts [a.example.com]`. The A session is retired and no longer in the holder. The note equals the deny clause followed by `mcpNoteFor(mcpReconnectFailed(loadErr))`, composed exactly.
- `TestURLSafetyDenyRefusedMidTurnWithAnUnreadableConfigNamesTheServer`: a busy engine refuses the `move`. The session is still closed, and the error carries `mcp server docs disconnected — its endpoint is denied`.
- `TestSettingsApplierReloadsRefuseAnUnparseableFile` keeps its pin unchanged; only its "leaves the connections where they are" comment is corrected.
**Acceptance:**
- `go build ./...`; `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestURLSafetyDeny|TestSettingsApplierReloadsRefuseAnUnparseableFile|TestApplySettingURLSafety' ./cmd/apogee/`
**Commit:** `fix(settings): a url-safety deny closes denied MCP sessions when the config does not load`
