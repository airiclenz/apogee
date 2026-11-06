# Sessions pop-up opens instantly — plan

**Goal:** `/sessions` shows its pop-up on the next frame, with a `loading…` row, then fills it in when the
list arrives. Listing reads only each record's `meta` header, so the fill-in is fast on a slow box (RPi 4)
holding many large records too.
**Date:** 2026-11-06
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 0bdee3da
**Closes:** sessions-popup-open-lag

**Regression check (2026-11-06, 0bdee3da):**
- 1: guard folded (decision 1: every `Store.List` consumer; `--continue` fails with the load error; GC keeps the snapshots) plus the read cap, comment rule and test-regex guards.
- 2: recast (decision 2: `esc close`-only legend while loading) plus the sequence-counter and no-blocking-host guards.
- 3: recast (decision 3: document the loading legend; drift-test line dropped) plus the empty/error note grep.
- 2 (round 2): guard folded (paste inert while loading; open-claim comment grep plus `model.go`; explicit
  zero-value `loading bool`; unstamped re-list folds as today, watch-isolation test in the regex).
- 3 (round 2): guard folded (the `esc close` grep is tied to the loading sentence, one hit per file).

**Sources:**
- `.capcom/issues/sessions-popup-open-lag.md`
- ADR 0011 (TUI disk work runs in Cmds, never in Update); ADR 0103 (live and watched rows in the browser)
- `internal/session/store.go` — `List`, `scan`, `loadPath`, `decodeRecord`, `decodeAnyRecord`
- `internal/tui/sessions.go` — `openSessionBrowser`, `listSessions`, `foldSessionList`
- `internal/tui/workflows.go` — `workflowItemLoading`, the `listSeq` / `itemSeq` stale-reply guard

**Ratified design calls (owner, 2026-11-06):**
- **Scope:** both halves: instant open with a loading row, and a meta-only listing read.
- **Empty / error after an instant open:** the pop-up closes and the same transcript note as today appears
  (`no saved sessions`, `could not list sessions: …`).
- **Damaged body behind a valid header:** listed. Resuming it fails with the load error, as any unreadable
  record does today.
- **Prune keeps the full decode** (writer's call, no behaviour change): retention never sweeps a record
  whose body is damaged, exactly as today.

**Standing requirements:**
- skills: coding-standards
- This box: `GOMEMLIMIT=2GiB go test -race -count=1 -run <TestX> ./<one package>/`; never a package
  pattern, never two runs at once. Full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test`.

**Out of scope:**
- A persistent session index or meta cache file.
- Speeding up `Prune`, `Load`, or `Store.Live`'s journal probe.
- Opening the pop-up while busy (still refused).

## 1. Store.List reads only each record's meta header — ✅ DONE (2026-11-06)

NOTES (2026-11-06): `TestListSkipsCorruptFile` gained a third file, cut off inside `meta`, so the "corrupt before or in meta is still skipped" claim is asserted rather than assumed.
NOTES (2026-11-06): `TestListLegacyEnvelopeStillListed` also covers a wrapper whose keys come in another order (meta before recordVersion), the other shape that takes the whole-decode fallback; `BenchmarkListLargeRecords` runs `header` and `whole` sub-benchmarks so the gain is on record.
NOTES (2026-11-06): the header read stops after `meta`, so a file with a valid header that is broken further on in any way (trailing bytes, a later duplicate `recordVersion` or `meta` key) is listed, the same class as a damaged body. Load still refuses it.

**What:**
**Goal:** `Store.List` returns each wrapped
record's `Meta` without decoding its `transcript` or `session`. A file whose bytes after a valid `meta` are
damaged is listed. Legacy bare envelopes, a too-new `recordVersion`, an invalid id, an oversize file and a
foreign file behave as today. `Prune` still skips a record that does not fully decode.
**Approach (assumed at the header base):** add one meta-only reader in `internal/session/store.go`, next to
`loadPath`. Keep the `maxRecordBytes` stat refusal. Stream the file through `json.Decoder` over a
`bufio.Reader`. Read the opening `{`, then keys in order. When the first key is `recordVersion` and the next
is `meta`, decode the version (refuse > `RecordVersion` with `ErrRecordVersion`) and the `Meta`, then stop
reading. In any other shape (legacy envelope, different key order, a non-object), fall back to the full
`loadPath` decode, so `wrapLegacy` and every refusal stay on one path. Run the decoded `Meta` through the
same id validation and `ParentID` clear that `decodeRecord` applies: extract that tail into one helper that
both call, never a copy. `scan` takes the reader as a parameter or a mode: `List` uses meta-only, `Prune`
keeps the full decode. Rewrite the `List` and `scan` doc comments: which reads skip which files.
**Regression guard.** The meta-only read applies to every `Store.List` consumer, not the browser alone: the
`/sessions` browser (`sessionHost.List`), watched homes (`watchedHome.sessions`/`finished`), `--continue`
(`resolveContinue`) and snapshot GC (`gcSnapshotDirs`). A damaged-body newest record makes `--continue` fail
with its load error (the ratified "resume fails with the load error" call); snapshot GC keeps its snapshots.
Stream through `bufio.NewReader(io.LimitReader(f, maxRecordBytes+1))`, so the meta-only read keeps both
bounds `loadPath` has. Fix every comment saying List reads through `loadPath`/`decodeRecord` or skips any file
that fails to decode (`grep -n 'decode\|List' internal/session/store.go`: `maxRecordBytes`, `loadPath`,
`ReadOnlyStore.List`, `decodeRecord`).
**Files:** internal/session/store.go, internal/session/store_test.go, cmd/apogee/wire_session_test.go
**Read first:** internal/session/store.go — scan, List, loadPath, decodeRecord, Prune; cmd/apogee/wire_session.go — resolveContinue,
watchedHome.finished; cmd/apogee/wire.go — gcSnapshotDirs
**Tests:** new in `store_test.go`: `TestListReadsMetaOnlyListsDamagedBody` (valid header, truncated
`session` → listed), `TestListMetaOnlyRefusesTooNewVersion`, `TestListMetaOnlyClearsBadParentID`,
`TestListMetaOnlySkipsInvalidID`, `TestListLegacyEnvelopeStillListed`, `TestPruneSkipsDamagedBody`
(the same damaged file is not swept). `BenchmarkListLargeRecords` (several multi-MB records). Existing
`TestListSortsByUpdatedDescending`, `TestListSkipsCorruptFile` (a file corrupt *before or in* `meta` is
still skipped) and `TestListMissingDirIsEmpty` stay green. New in `cmd/apogee/wire_session_test.go`:
`TestResolveContinueFailsOnDamagedNewestBody` (the newest workspace record has a damaged body → `--continue`
returns its load error, never the next-newest). `TestLegacyBareEnvelopeIsWrapped`,
`TestAnUnsafeParentIDLoadsCleared`, `TestParentID*` and `TestResolveContinue*` stay green.
**Acceptance:**
- `go build ./internal/session/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestList|TestPrune|TestLoad|TestLegacyBareEnvelopeIsWrapped|TestAnUnsafeParentIDLoadsCleared|TestParentID' ./internal/session/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestResolveContinue' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run '^$' -bench BenchmarkListLargeRecords -benchtime 3x ./internal/session/`
**Commit:** `perf(session): list reads only each record's meta header`

## 2. /sessions opens its pop-up at once with a loading row

**What:**
Recast at the regression check (2026-11-06).
**Goal:** right after `/sessions` + enter (idle, with a host), the next frame shows the sessions pop-up with
one `loading…` row, before the store has answered. When the list arrives, the rows replace it. An error
or empty result closes the pop-up and adds today's note. A reply that arrives after the pop-up was closed
(esc), or that belongs to an earlier opening, changes nothing and adds no note. The re-list after a
rename or delete still repaints the open pane.
**Approach (assumed at the header base):** `openSessionBrowser` sets `sessionBrowser` to a fresh
open-and-loading state and bumps a list sequence that `listSessions` stamps on its `sessionListMsg`.
`foldSessionList` drops a stamped reply whose sequence is not current or that arrives while the pane is
closed. The unstamped re-list (`recordWrite.relist` in `sessionsave.go`) folds as it does today. While
loading, the pane paints a single `loading…` row (the same spelling as `workflowItemLoading`; share one
constant if both live in `internal/tui`) with the legend `esc close` alone. `esc` closes the pane. Every other key and
every mouse click on the row does nothing. The pane respects the frame-height rule. Fix the `sessions.go`
package comment ("a slow disk never stalls the UI") and `openSessionBrowser`'s doc so they describe
open-then-fill.
**Regression guard.** While loading, the pane's legend reads only `esc close` (every other key is inert, so
no other key is advertised); the full legend returns when the rows arrive. So every e2e wait on the hint
still means "rows are up". The list sequence lives on `Model` beside `sessionBrowser`, never reset by a
`m.sessionBrowser = sessionBrowser{}` close (sessions.go, mouse.go), so a reopen never reuses a sequence.
Loading is an explicit `loading bool` whose zero value means "not loading": only `openSessionBrowser` sets
it and every fold that seats rows clears it (the unstamped re-list included), so the five
`sessionBrowser{open: true}` fixtures (chromelayout, framechords, clearscreen, pointershape, mouse tests)
still paint a loaded, empty pane. A zero-sequence (unstamped) `sessionListMsg` folds as today, even into a
closed pane (the watch's held listing). Keys, clicks and pastes are inert while loading:
`sessionBrowserPaste` claims and drops the paste, as during the delete confirm. Every comment that says the
list's arrival opens the pane is fixed, not only the two named: `grep -n 'opens the overlay\|opens no
overlay\|aborts the open\|opens the pane when\|never stalls the UI' internal/tui/*.go`, plus `model.go`'s
`case sessionListMsg:` comment.
**Files:** internal/tui/sessions.go, internal/tui/sessions_test.go, internal/tui/sessionsave.go, internal/tui/model.go
**Read first:** internal/tui/sessions.go — openSessionBrowser, foldSessionList, browserListContent, sessionBrowserKey, sessionBrowserPaste;
internal/tui/mouse.go — handleBrowserClick; internal/tui/sessionsave.go — recordWrite.relist;
internal/tui/watch_test.go — TestWatchIsolatesHoldsTheOwnersSurfacesUntilEsc
**Tests:** new in `sessions_test.go`, driven through `fakeSessionHost` / `newBrowserModel` as they are (no
blocking host): `step` never runs a Cmd, so hold the enter's Cmd unrun to see `loading…`, and fold
`cmdMsg(cmd)` as the release. `TestSessionBrowserOpensLoadingBeforeList` (the frame after enter holds
`loading…` and the legend `esc close` alone; a paste then leaves no `filter:` on the title row; after
the fold, the rows and the full legend), `TestSessionBrowserEscWhileLoadingDropsReply` (no pane, no note),
`TestSessionBrowserStaleReplyIgnored` (close, reopen, first reply lands → still loading),
`TestSessionBrowserLoadingErrorClosesWithNote`, `TestSessionBrowserLoadingEmptyClosesWithNote`.
`TestSessionBrowserEmptyStoreNotesNoOverlay`, `TestSessionBrowserRefusesToOpenWhileBusy`,
`TestFrameNeverExceedsTheTerminalHeightSessionBrowser`, `TestWatchIsolatesHoldsTheOwnersSurfacesUntilEsc`
and the rename/delete tests stay green.
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSessionBrowser|TestFrameNeverExceedsTheTerminalHeightSessionBrowser|TestWatchIsolatesHoldsTheOwnersSurfacesUntilEsc' ./internal/tui/`
**Commit:** `fix(tui): /sessions opens its pop-up at once with a loading row`

## 3. Document the loading row

**What:**
Recast at the regression check (2026-11-06).
**Goal:** the manual and the layout spec say that the `/sessions` pop-up opens at once with a `loading…`
row and closes with a note when the list comes back empty or fails.
**Approach (assumed at the header base):** add one sentence to `docs/manual/sessions.md` where it
describes the browser and its row cells. In `layout.md`, add the loading state to the sessions pop-up's
section. Every doc sentence that says the browser "opens when the list is read", or that implies so, is
updated (`grep -n "sessions" docs/manual/sessions.md layout.md`).
Depends on item 2.
**Regression guard.** Document the `esc close`-only loading legend alongside the loading row in `layout.md`'s
`/sessions` browser paragraph ("The `/sessions` browser types too", which states the full hint) and in
`docs/manual/sessions.md`. No docs drift test covers either file, so the docs are checked by greps only.
The `esc close` check is tied to the loading sentence (a bare grep already matches `layout.md`'s full hint at
base): `esc close` stands within two lines after `loading…`, one hit per file.
**Files:** docs/manual/sessions.md, layout.md
**Read first:** layout.md — "The `/sessions` browser types too" paragraph; docs/manual/sessions.md — the `/sessions` bullet;
internal/tui/sessions.go — sessionBrowserHint, foldSessionList (the `no saved sessions` / `could not list sessions: ` wordings)
**Tests:** none (docs only).
**Acceptance:**
- `grep -n "loading…" docs/manual/sessions.md layout.md`
- `grep -n -A2 'loading…' docs/manual/sessions.md layout.md | grep 'esc close'` — one hit per file
- `grep -n "no saved sessions" docs/manual/sessions.md layout.md`
- `grep -n "could not list sessions" docs/manual/sessions.md layout.md`
**Commit:** `docs(sessions): the browser opens with a loading row`
