# Code audit 2026-09-29: fixes

**Goal:** Close every finding of the 2026-09-29 code audit in the owner's action order: the read
cache stops hiding files, recipe and guard fences hold, MCP and git probes resist hostile input,
lifecycle edges restore what they drain, and the merged-stdout denial kill stops killing data.
**Date:** 2026-09-29
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 97bfaf0d

**Sources:**
- docs/reviews/code-audit-2026-09-29.md (every item cites its finding by title)
- docs/adr/0056-terminal-fail-fast-and-session-scratch.md (2026-09-16 and 2026-09-26 amendments)
- docs/adr/0088-cancel-settles-and-never-rewinds-finished-work.md
- docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md

**Ratified design calls** (owner, 2026-09-29):
- **Order:** the audit's Recommended Action Order, groups 1–8, as items 1–20.
- **Merged-stdout kill (ADR 0056):** arm the merged-stdout kill only when the model's line chains
  more commands after the merge; a single command or pipeline keeps label-only judgement.
- **Fanout `out:`:** `Validate` requires `{item}` in a fanout stage's `out:`; no per-key fallback.
- **Git probe:** add ctime to the fingerprint (not a content hash).
- **degenerateRepeat:** a content floor on counted lines, not a consecutive-run rule.
- **Console cancel:** console_open and console_send stop their wait promptly on cancel and return
  the tail gathered so far plus a line saying the wait was cut short by cancel, with a nil error;
  console_read returns ctx.Err() only when its read gathered nothing.

**Standing requirements:**
- skills: coding-standards
- Pi 5 host: never `go test -race` or `-cover` over a package pattern or a whole heavy package;
  narrow to one package and `-run` the named tests, e.g.
  `GOMEMLIMIT=2GiB go test -count=1 -run TestX ./internal/floor/`. Never two test runs at once.
  Pass these rules to every sub-agent.
- Any deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- New findings beyond the audit; golangci-lint / govulncheck runs.
- Windows-host verification of item 18 (no Windows runner here; test by path spelling).

**Regression check (2026-09-29, 97bfaf0d):**
- 1: guard folded
- 2: guard folded
- 3: guard folded (supersedes ADR 0049's 2026-09-15 amendment)
- 4: guard folded, writer decision (supersedes the per-line bound in docs/design/mcp-client.md);
  round 2: writer decision (over-cap body error is errMCPMessageTooLarge)
- 5: recast, writer decision; round 2: guard folded ((c) needs a many-short-line body)
- 6: guard folded
- 8: guard folded (supersedes the AbortExchange comment in internal/agent/interject.go)
- 12: recast, writer decision (yields to ADR 0088 D4); round 2: recast, writer decision
  (ratified Console cancel call; holds ADR 0088 D1 and D4)
- 13: guard folded (supersedes the indexOf-applier comments in internal/tools/file_edit.go)
- 14: guard folded (supersedes CHANGELOG's "one line repeated 50 times" rule and subagent.go's comments)
- 15: guard folded, writer decision
- 17: guard folded
- 18: guard folded, writer decision (supersedes resolveRecipePrompts' Dir-joined form in internal/skills/load.go)
- 19: guard folded
- 20: guard folded, writer decision

## 1. Read cache: only an unpruned full read counts as the cached copy — ✅ DONE (2026-09-29)

**What:** Fix two audit findings in one function ("Read cache counts a ranged or capped earlier
read as a full copy", Medium; "Read cache caps a re-read of a file whose earlier result was pruned
to a stub", High). Both break the never-worse-than-bare-loop invariant.
**Regression guard.** A capped re-read never reaches history: `prepareCall` (`internal/agent/dispatch.go`)
edits a local copy of the call, so history keeps `{"path":"a.go"}`. Count only the EARLIEST bare
successful read since `lastChange` and require it unpruned. Add `locate` to the prior-read skip (a
`locate` read returns ±10-line windows), not to `capReadArguments`' pending-call key list.
**Goal:** `CacheRead` caps a bare read only when an earlier successful read of the same unchanged
path carried none of `start_line`, `end_line`, `max_lines`, was not itself capped by this guard,
and its result content does not start with the prune stub prefix.
**Approach (assumed at the header base):** In `internal/floor/readcache.go`
`priorSuccessfulReadUnchanged`, skip a prior call whose arguments carry any range key — reuse the
key list `capReadArguments` uses (one shared unexported slice, not a second copy) — and skip one
whose result content starts with `pruneStubPrefix` from `internal/context/prune.go`; export a
predicate there (e.g. `context.IsPruneStub(string) bool`) rather than duplicating the literal.
**Files:** internal/floor/readcache.go, internal/floor/readcache_test.go, internal/context/prune.go
**Read first:** internal/floor/readcache.go — priorSuccessfulReadUnchanged, capReadArguments, CacheRead;
internal/agent/dispatch.go — prepareCall; internal/context/prune.go — pruneStubPrefix, pruneCandidates;
internal/tools/read_file.go — renderFile; internal/floor/readcache_test.go — TestCacheReadLeavesRangedReadUntouched
**Tests:** in `readcache_test.go`: ranged first read (`start_line`/`end_line`) then bare read →
not capped; `max_lines` first read → not capped; bare first read whose result was rewritten to a
`[pruned: N lines — …]` stub → not capped; pruned r1, bare r2 with a 1-line result, pending r3 →
not capped; a prior `locate` read then a bare read → not capped; existing full-read case still capped.
**Acceptance:**
- `go build ./internal/floor/ ./internal/context/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/floor/ ./internal/context/`
**Commit:** `fix(floor): count only an unpruned full read as the read cache's copy`

## 2. Recipe files: fence disk skills against escaping symlinks — ✅ DONE (2026-09-29)

NOTES (2026-09-29): consequential edit — internal/workflow/recipe.go: made necessary by skillFiles serving a non-nil error FS for an unopenable disk folder (the Recipe.Files comment said "Nil when the folder cannot be opened")
NOTES (2026-09-29): the os.Root handle is not stored on the catalog entry: Catalog.Recipe opens it per call and the returned FS keeps it alive; os.Root's own finalizer closes it once the recipe run drops the FS (documented on skillFiles)
NOTES (2026-09-29): confirmed readPrompt and stageSkillFile read only through recipe.Files, never by host path; stageSkillFile runs only for shipped skills today (a disk skill's {{SKILL_DIR}} script still runs by host path under the terminal gate), so its fence test drives it directly over a disk recipe's fenced FS

**What:** Fix "Repo skill recipes can read host files through symlinks" (Medium): `skillFiles`
hands the recipe engine `os.DirFS(dir)`, which follows symlinks out of the skill folder.
**Regression guard.** For an absolute Dir `skillFiles` never returns nil: an `os.OpenRoot` error yields an fs.FS whose
Open returns it, so `readPrompt` never falls back to the workspace. The fence covers `prompt:` files; a
disk skill's `{{SKILL_DIR}}/x.sh` still runs by host path under the terminal gate (`recipeScripts.skillDir`).
`os.Root` refuses absolute symlink targets: name that in the item's CHANGELOG line.
**Goal:** A recipe `prompt:` or staged script whose file is a symlink resolving outside its skill
folder is refused with an error; in-folder files and shipped (embedded) skills read as before.
**Approach (assumed at the header base):** In `internal/skills/catalog.go` `skillFiles`, for a
non-shipped Dir build the FS from `os.OpenRoot(dir)` and `(*os.Root).FS()`; the root handle's
lifetime follows the FS's (leave it open with the catalog entry; document it). `readPrompt`
(`internal/agent/workflowspawn.go`) and `stageSkillFile` (`internal/agent/recipe.go`) read through
that FS and so inherit the fence — confirm neither re-opens by host path.
**Files:** internal/skills/catalog.go, internal/skills/catalog_test.go, internal/agent/workflowspawn_test.go, internal/agent/recipe_test.go
**Read first:** internal/skills/catalog.go — skillFiles, Catalog.Recipe; internal/agent/workflowspawn.go —
workflowSpawner.readPrompt; internal/agent/recipe.go — recipeScripts.skillDir, stageSkillFile;
internal/skills/catalog_test.go — TestCatalogRecipeServesOnlyRecipeSkills; internal/skills/load_test.go — mustSymlink, TestLoadSymlinkEscapeRefused
**Tests:** skill folder with `p.md` → symlink to a temp file outside: `readPrompt` errors and the
outside bytes never appear; same for `stageSkillFile` as a unit test over the fenced FS; in-folder
symlink with a relative target still reads (symlink tests skip on Windows, as
`TestLoadSymlinkEscapeRefused` does); a removed skill folder → `readPrompt` errors, never reads the
workspace; `TestCatalogRecipeServesOnlyRecipeSkills`' `recipe.Files == nil` check stays green.
**Acceptance:**
- `go build ./internal/skills/ ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/skills/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'ReadPrompt|StageSkillFile|Symlink' ./internal/agent/`
**Commit:** `fix(skills): fence disk recipe files through os.Root`

## 3. Scratch-dir exemption never masks a `..` escape — ✅ DONE (2026-09-29)

NOTES (2026-09-29): the `<scratch>/../../.ssh` rows earn TierForceApproval via write-apogee-control-plane, not TierHardRefuse: that is the tier the unexempted path gets (the .ssh rule is home-anchored and does not see a path spelled through the scratch dir); the test asserts each judged row equals the no-exemption decision
NOTES (2026-09-29): the whole-token prose grep also hits CHANGELOG.md's released 2026-09-15 entry ("masks the whole shell token"); left as history — the CHANGELOG is the closeout's to write, and the new entry above qualifies it

**What:** Fix "Scratch-dir exemption masks `..` traversal out of the writable area" (Medium).
**Regression guard.** A `..` token that stays inside the scratch dir (`ls <scratch>/repo/../out`) is left unmasked
too, so its `/.apogee` spelling now asks a Tier-2 approval via `write-apogee-control-plane` — intended;
pin it. Supersedes ADR 0049's 2026-09-15 amendment ("everything under the token is inside the box"):
add a dated amendment; qualify every whole-token prose site (`grep -rn 'WHOLE\|whole shell token' docs/adr internal/security CHANGELOG.md`).
**Goal:** `Inspect` of `rm -rf <scratch>/../../..` and `echo x > <scratch>/../../.ssh/authorized_keys`
reaches the Tier-1/Tier-2 rules as if no exemption applied; `<scratch>/sub/file` stays exempt.
**Approach (assumed at the header base):** In `internal/security/dangerous.go`, `maskSpelling`
(with `exemptTokenTail`) refuses to mask a matched token that contains a `..` path segment — the
whole token is left unmasked. Binding: refuse, do not truncate at `..` or path-clean.
**Files:** internal/security/dangerous.go, internal/security/dangerous_test.go, docs/adr/0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md
**Read first:** internal/security/dangerous.go — maskExempt, maskSpelling, exemptTokenTail, Inspect;
internal/security/rules.go — write-apogee-control-plane; internal/security/dangerous_test.go —
TestInspectMasksTheSessionScratchDir, TestMaskExempt; internal/agent/agent.go — guardExemptions
**Tests:** table rows through `Inspect` with the exact scratch spelling the Orientation announces:
`<scratch>/../../.ssh` → `TierHardRefuse` (or the tier the unexempted path gets); `<scratch>/a/b` →
still exempt; a `..` inside a file name (`<scratch>/a..b`) still exempt; `<scratch>/a/../b` →
`TierForceApproval` via `write-apogee-control-plane`.
**Acceptance:**
- `go build ./internal/security/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/security/`
**Commit:** `fix(security): refuse the scratch exemption for a token with a .. segment`

## 4. MCP: cap each message cumulatively — ✅ DONE (2026-09-29)

NOTES (2026-09-29): one reader type keeps a `framing` mode field (frameJSONLines / frameSSEEvents / frameWholeBody) rather than a separate non-SSE reader, so `boundedBody` stays the single body type; the non-SSE mode is the whole-body cumulative bound erroring with errMCPMessageTooLarge. The name `lineBoundedReader` is kept (no renames), though it now bounds messages.
NOTES (2026-09-29): consequential edit — internal/mcp/tool.go: made necessary by the bound becoming per message (the maxMCPResultBytes comment said "line bound").
NOTES (2026-09-29): the per-byte JSON-depth scan resets only at a newline at depth 0 outside a string, per the plan; a server that sends back-to-back values with no newline (`{}{}...`) is charged cumulatively across them — off-spec for MCP stdio, so it fails safe.

**What:** Fix "MCP size cap is per line, so one hostile server can exhaust memory" (Medium).
**Regression guard.** HTTP mode shape fixed so item 5 can test it: a response whose Content-Type is not
text/event-stream is read through a reader that returns an error (not silent truncation) past
maxMCPMessageBytes; an SSE body keeps lineBoundedReader with a cumulative per-event cap reset at each
blank line; stdio keeps lineBoundedReader with a cumulative cap reset at each completed message line.
The mode comes from the base type of `mime.ParseMediaType(Content-Type)`, as the SDK's `baseMediaType`
does; a stdio message completes at a newline at JSON depth 0 outside a string (the SDK's json.Decoder
accepts newlines inside a value); an SSE blank line may be `\r\n`. Supersedes the "per line, never
cumulative" bound in docs/design/mcp-client.md:74-80, the CHANGELOG.md:545 entry and the
`boundedBodyTransport` doc: update the design doc and the doc comments. The non-SSE body reader's
over-cap error is (or wraps) the existing errMCPMessageTooLarge sentinel, so
TestGuardedClient_AnOversizeBodyFailsTheRead's errors.Is keeps holding.
**Goal:** A stdio message, an SSE event, or a plain `application/json` body larger than
`maxMCPMessageBytes` in total ends the read with an error, however it is split into lines.
**Approach (assumed at the header base):** In `internal/mcp/bounded.go` `lineBoundedReader`, count
bytes since the last message boundary — a newline that completes a JSON value on stdio (the SDK's
stdio framing is one message per line; confirm), a blank line on SSE — and error past the cap. In
`internal/mcp/transport.go`, wrap a non-SSE JSON body in a reader that errors (not silently
truncates) past `maxMCPMessageBytes` before `ReadAll`.
**Files:** internal/mcp/bounded.go, internal/mcp/bounded_test.go, internal/mcp/transport.go, internal/mcp/transport_test.go, docs/design/mcp-client.md
**Read first:** internal/mcp/bounded.go — lineBoundedReader, maxMCPMessageBytes; internal/mcp/transport.go —
boundedBodyTransport.RoundTrip, stdioTransport.Connect; go-sdk@v1.6.1 mcp/transport.go — ioConn read loop;
go-sdk mcp/event.go — scanEvents; go-sdk mcp/streamable.go — baseMediaType; internal/mcp/transport_test.go — TestGuardedClient_PinsTheEndpointAndRefusesEverythingElsePrivate
**Tests:** `bounded_test.go`: many short lines inside one SSE event over the cap → error; stdio
many short lines of one value over the cap → error; many small messages totalling more than the
cap pass (stdio and SSE, `\n` and `\r\n` line ends); a `text/event-stream; charset=utf-8` reply
gets the SSE mode. JSON body over the cap → an error that `errors.Is` `errMCPMessageTooLarge`. `TestLineBoundedReader`'s "two 3 MiB lines pass"
row moves to its framing mode; `TestGuardedClient_PinsTheEndpointAndRefusesEverythingElsePrivate`'s
`*boundedBody` assertion for a text/plain reply is updated (or one body type keeps a mode field).
**Acceptance:**
- `go build ./internal/mcp/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/mcp/`
**Commit:** `fix(mcp): bound each message cumulatively, not per line`

## 5. MCP HTTP transport: behavioural tests for pinning, redirects and body bound — ✅ DONE (2026-09-29)

NOTES (2026-09-29): (b) proxy pinning is already covered by TestGuardedClient_ProxiedEndpointPinsBothHosts (the endpoint connects only through the pinned loopback proxy; an unproxied private address is refused), so no new test was added; checked by hand that it fails when the proxy host is dropped from the pin list.
NOTES (2026-09-29): (a) extended TestGuardedClient_DoesNotFollowRedirects into a table with a Location pointing at another private address (10.9.8.7); (c) extended TestGuardedClient_AnOversizeBodyFailsTheRead into a table adding a many-short-line JSON body and a many-short-line SSE event with no blank line. Checked by hand: dropping CheckRedirect fails both redirect cases; a per-line bound fails both many-short-line cases.

**What:** Recast at the regression check (2026-09-29). Test-only; closes "MCP HTTP transport pinning, redirect and body-bound lane has no
behavioural test" (Medium). Depends on item 4.
**Regression guard.** add only the behaviours transport_test.go does not already cover at run time (check (a)
redirect, (b) proxy pinning, (c) oversize JSON body and oversize SSE event against item 4's shape); a
behaviour already covered gets a dated NOTES line instead of a duplicate test. Existing coverage is
`TestGuardedClient_*` — extend it. Each new test swaps `proxyForRequest` and stays serial, or calls
`t.Parallel()`, so `TestVetEndpoint_TheEgressProxyComesFromTheEnvironment` still sees the env read first.
(c) must send many short lines totalling more than maxMCPMessageBytes — a JSON body with no event
boundary, and an SSE event (text/event-stream) with no blank line; the single newline-free line of
`TestGuardedClient_AnOversizeBodyFailsTheRead` trips the per-line bound already and is not coverage for (c).
**Tests:** new loopback `httptest` tests in `internal/mcp/transport_test.go`: (a) a 302 to another
private address is not followed; (b) with `proxyForRequest` stubbed, the dial is pinned to the
proxy address and any other address is refused; (c) a JSON body and an SSE event (text/event-stream),
each many short lines with no message boundary totalling more than `maxMCPMessageBytes`, make the
read error. Each must fail if its guard is removed (check by hand).
Only the gaps the guard leaves; an existing `TestGuardedClient_*` test is extended, not duplicated.
**Files:** internal/mcp/transport_test.go
**Read first:** internal/mcp/transport_test.go — TestGuardedClient_AnOversizeBodyFailsTheRead, TestGuardedClient_DoesNotFollowRedirects,
TestGuardedClient_ProxiedEndpointPinsBothHosts, TestVetEndpoint_TheEgressProxyComesFromTheEnvironment; internal/mcp/transport.go —
boundedBodyTransport.RoundTrip, proxyForRequest; internal/mcp/bounded.go — lineBoundedReader.exceeds, errMCPMessageTooLarge
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Transport|GuardedClient|VetEndpoint' ./internal/mcp/`
**Commit:** `test(mcp): cover HTTP pinning, redirect refusal and body bound`

## 6. Git config probe: fingerprint includes ctime — ✅ DONE (2026-09-29)

NOTES (2026-09-29): ctime is read in holds() from the os.FileInfo each print already keeps (info.Sys()), via a new sameChangeTime helper, rather than stored as a separate fileprint field; takeFileprint is unchanged.
NOTES (2026-09-29): gitexec_test.go left unchanged — the new coverage lives in fileprint_internal_test.go (linux || darwin), which the item allows in place of a Capture-level test.
NOTES (2026-09-29): the BSDs fall to ctime_other.go (no ctime) per the item's three-file regression guard, although their Stat_t exposes one (Ctimespec on freebsd/netbsd, Ctim on openbsd/dragonfly); no BSD is in CROSS_TARGETS. Adding them would take one more build-tagged file.

**What:** Fix "Memoised git config probe can be defeated by a same-length rewrite" (Medium).
**Regression guard.** `syscall.Stat_t` spells ctime `Ctim` on linux and `Ctimespec` on darwin: use
`ctime_linux.go`, `ctime_darwin.go` and `ctime_other.go` (`!linux && !darwin`), not one `ctime_unix.go`.
`gitexec_test.go` is `package gitexec_test`: test the unexported print in `fileprint_internal_test.go`
(package gitexec) or through Capture. Sleep ≥ 20 ms (or loop until ctime moves) before the rewrite.
**Goal:** A same-size, same-mtime, same-inode in-place rewrite of a probed config file makes
`fileprint.holds` report false on platforms that expose ctime (Linux, macOS, BSD).
**Approach (assumed at the header base):** In `internal/gitexec/gitexec.go`, `takeFileprint`
records ctime from `syscall.Stat_t` via a build-tagged helper (`Ctim` on Linux, `Ctimespec` on
darwin/BSD); `holds` compares it. Windows keeps the current fingerprint (no userland ctime).
**Files:** internal/gitexec/gitexec.go, internal/gitexec/ctime_linux.go, internal/gitexec/ctime_darwin.go, internal/gitexec/ctime_other.go, internal/gitexec/gitexec_test.go, internal/gitexec/fileprint_internal_test.go
**Read first:** internal/gitexec/gitexec.go — fileprint, takeFileprint, fileprint.holds, probeCommandConfig;
internal/gitexec/gitexec_test.go — TestCapture_ReprobesWhenTheConfigChanges, appendToConfig, realGit;
Makefile — CROSS_TARGETS
**Tests:** write config, take print, sleep past a coarse ctime tick (≥ 20 ms, or loop until the
Stat ctime moves), rewrite same length, restore mtime with `os.Chtimes` → `holds()` false (unix
only; in `fileprint_internal_test.go`, or via Capture as `TestCapture_ReprobesWhenTheConfigChanges`
does); untouched file → true.
**Acceptance:**
- `go build ./internal/gitexec/ && GOOS=windows go build ./internal/gitexec/ && GOOS=darwin go build ./internal/gitexec/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/gitexec/`
**Commit:** `fix(gitexec): add ctime to the config probe fingerprint`

## 7. Background: markStopped leaves a done folder done — ✅ DONE (2026-09-29)

**What:** Fix "Re-run queued behind another workflow is marked stopped, blocking later retries".
**Goal:** Stopping a queued `RerunFailed` run whose folder phase is `done` leaves `status.json`
at `done`, so a later `RerunFailed(id)` is accepted; a new or mid-run folder still becomes stopped.
**Approach (assumed at the header base):** In `internal/agent/background.go` `markStopped`, read
the folder's current phase and return without writing when it is `done`.
**Files:** internal/agent/background.go, internal/agent/background_test.go
**Read first:** internal/agent/background.go — markStopped, StopWorkflow, RerunFailed, openWorkflowFolder,
startBackground; internal/agent/background_test.go — TestBackground_RerunFailedRefusesALiveOrUnfinishedWorkflow,
blockedScript, launchBackground
**Tests:** queue a `RerunFailed` behind a running workflow, `StopWorkflow(id)` → status stays
done, second `RerunFailed(id)` accepted.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Background|Rerun|MarkStopped' ./internal/agent/`
**Commit:** `fix(agent): keep a finished workflow folder done when its queued re-run stops`

## 8. Agent: an aborted Exchange puts its drained workflow notes back — ✅ DONE (2026-09-29)

NOTES (2026-09-29): the per-Exchange record lives on backgroundManager (`delivered`, guarded by its mutex) rather than on the Exchange's loop state, since Wake takes the notes before the Exchange opens; step's opening and TakeWorkflowNotes take through `takeIntoExchange`, Wake records via `markDelivered` once its submit succeeds; `exchangeAborted` restores, `exchangeClosed` forgets.

**What:** Fix "A finished background workflow's note is lost when the opening Exchange is aborted".
**Regression guard.** Under the default `workflow-wake: on`, `Wake` (background.go) takes the notes as the opening
input, not via loop.go's `takeNotes`: record them on the same per-Exchange state so an abort puts them
back too. A put-back note may let `wakeAfterFold`/`wakeIfIdle` open an Exchange right after a cancel —
intended (ADR 0089 D3); state it. Supersedes interject.go's "AbortExchange discards it" comment: amend it.
**Goal:** After an aborted Exchange, every workflow finish note drained into it (at open, and by
`TakeWorkflowNotes` interjections the abort drops) is held again and reaches the next Exchange.
**Approach (assumed at the header base):** In `internal/agent/loop.go`, keep the notes returned by
`a.background.takeNotes()` on the Exchange's state; `exchangeAborted` (`internal/agent/construct.go`)
calls `a.background.putBack` with them, and clears them on a normal close. Same for interjection
notes dropped by the abort.
**Files:** internal/agent/loop.go, internal/agent/construct.go, internal/agent/background.go, internal/agent/interject.go, internal/agent/background_test.go
**Read first:** internal/agent/loop.go — step; internal/agent/construct.go — exchangeAborted, exchangeClosed;
internal/agent/turn.go — turnLifecycle.abort; internal/agent/background.go — Wake, takeNotes, putBack;
internal/agent/wake_test.go — newPairParent
**Tests:** a finished workflow note held; open an Exchange, abort it → next Exchange's opening
message carries the note once; a completed Exchange does not re-deliver it; with `workflow-wake:
on`, Esc during the wake reply's Turn 0 → the note is held again (fixtures `newPairParent`,
`assertNoteMessage` live in `wake_test.go`).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Note|Abort' ./internal/agent/`
**Commit:** `fix(agent): put drained workflow notes back when an Exchange aborts`

## 9. TUI: `^r` re-run obeys the actuation latch — ✅ DONE (2026-09-29)

**What:** Fix "`^r` re-run bypasses the actuation latch that guards `/bg`" (data race).
**Goal:** `^r` in a workflow detail is refused with the actuation block note while an actuation is
in flight, through the same predicate that refuses `/bg`.
**Approach (assumed at the header base):** In `internal/tui/workflows.go` `workflowsVerb`, the
`workflowRerunKey` case first checks `m.actuation.inFlight && actuationBlocked("bg")` and shows
`m.actuationBlockNote()`; then the existing busy/bgLaunching check.
**Files:** internal/tui/workflows.go, internal/tui/workflows_test.go
**Read first:** internal/tui/workflows.go — workflowsVerb, workflowRerunNotIdle; internal/tui/actuation.go —
actuationBlocked, actuationBlockNote; internal/tui/commandrun.go — runBg; internal/tui/workflows_test.go —
TestWorkflowsViewCtrlRRerunsTheFailedItems, workflowsPaneModel; internal/tui/actuation_test.go — startLoad
**Tests:** actuation in flight + `^r` → note shown, `bgLaunching` unset, no launch cmd.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Rerun|Actuation' ./internal/tui/`
**Commit:** `fix(tui): refuse the ^r re-run while an actuation is in flight`

## 10. TUI: outside click on the boundary confirm cancels through answerBoundary — ✅ DONE (2026-09-29)

**What:** Fix "Outside click on the workflow boundary confirm strands queued commands".
**Goal:** An outside click while the boundary confirm is open has the same effect as esc: deferred
commands drain and the held flush runs.
**Approach (assumed at the header base):** In `internal/tui/mouse.go` `handlePickerClick`, when
`m.boundaryConfirmOpen()` and the click is outside the picker rect, return
`m.answerBoundary(boundaryCancel)` instead of zeroing `m.picker`.
**Files:** internal/tui/mouse.go, internal/tui/mouse_test.go
**Read first:** internal/tui/mouse.go — handlePickerClick (doc comment names esc's zeroing; answerBoundary returns
tea.Model, needs .(Model) + claimed=true); internal/tui/commandrun.go — answerBoundary, boundaryConfirmOpen, runDeferredCommands;
internal/tui/interject.go — drainThenFlush; internal/tui/commandrun_test.go — TestQueuedClearWithAWorkflowRunningHoldsTheFlushUntilAnswered; internal/tui/mouse_test.go — TestListPaneClickOutsideTheBoxClosesItAndReachesNothing, leftClick
**Tests:** boundary confirm open with a deferred command queued; outside click → picker closed and
the deferred command ran (same observable as the esc test).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'PickerClick|Boundary' ./internal/tui/`
**Commit:** `fix(tui): cancel the boundary confirm on an outside click like esc`

## 11. Workflow: a verify child counts only when it ended ok — ✅ DONE (2026-09-29)

**What:** Fix "A blocked or partial verify child still counts as confirmed or refuted".
**Goal:** `verdictOf` returns `VerdictUnclear` for any receipt whose status is not `StatusOK`.
**Approach (assumed at the header base):** In `internal/workflow/stages.go` `verdictOf`, check
`item.Receipt.Status == StatusOK` before reading `Fields["verdict"]`.
**Files:** internal/workflow/stages.go, internal/workflow/stages_test.go
**Read first:** internal/workflow/stages.go — verdictOf, runVerify, Verdict consts (comment names only blocked →
unclear); internal/workflow/runner.go — tallyOf; internal/workflow/plan.go — StatusOK, StatusPartial, StatusBlocked;
internal/workflow/stages_test.go — TestVerifyTalliesVerdicts
**Tests:** blocked and partial receipts with `verdict: confirmed` → unclear; ok → confirmed.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/workflow/`
**Commit:** `fix(workflow): treat a verify child that did not end ok as unclear`

## 12. Console tools settle on cancel during their wait window — ✅ DONE (2026-09-29)

NOTES (2026-09-29): consoleTail now returns (tail, gathered) so console_read can return ctx.Err() only when its read gathered nothing; console_close discards the bool (its wait of 0 drains before looking at ctx).
NOTES (2026-09-29): the console_send cancel test cancels after 250 ms rather than the plan's 50 ms, so the shell's output reliably lands before the cancel on a loaded host; the under-1 s budget is still measured from the cancel.

**What:** Recast at the regression check (2026-09-29). Fix "Console tools ignore cancel during their wait window" (ADR 0088 cancel settles
promptly).
**Regression guard.** Ratified design call **Console cancel** (owner, 2026-09-29): console_open and
console_send stop their wait promptly on cancel and return the tail gathered so far plus a line saying
the wait was cut short by cancel, with a nil error; console_read returns ctx.Err() only when its read
gathered nothing. So ADR 0088 D1 (the nil-error result is committed as the real result) and D4 (a
started console is finished work the model is told about) hold, and output drained before the cancel
is never lost. A `ring.ReadContext` (internal/console/ring.go) selects on ctx.Done() and returns
WITHOUT draining on cancel; `Read(wait)` stays as `ReadContext(context.Background(), wait)` so
ring_test.go, process_test.go and registry_test.go keep compiling. console_close keeps its up-front
ctx check; its test calls `consoleTail(cancelledCtx, c, 0)` directly after closing the console (wait 0
drains what is buffered before looking at ctx).
**Goal:** `console_open` and `console_send` end their wait promptly (under 1 s) of ctx cancellation,
including the exit wait, and return the tail gathered so far plus a line saying the wait was cut short
by cancel, with a nil error; `console_read` ends as promptly and returns `ctx.Err()` only when its
read gathered nothing.
**Approach (assumed at the header base):** Thread ctx into `collectConsoleWindow`,
`awaitConsoleExit` and `consoleTail` (`internal/tools/console_common.go`; `console_read` reaches
`consoleTail`, not `collectConsoleWindow`) and their callers (`console_open.go`, `console_send.go`,
`console_read.go`, `console_close.go`). Add a ctx-aware read to `internal/console`:
`ring.ReadContext(ctx, wait)` selects on `ctx.Done()` and returns without draining on cancel, surfaced
as `(*Process).ReadContext` and `(*Console).ReadContext`; `Read(wait)` stays as
`ReadContext(context.Background(), wait)`. The exit poll selects on `ctx.Done()` and a ticker.
**Files:** internal/console/ring.go, internal/console/ring_test.go, internal/console/registry.go, internal/console/process.go, internal/console/process_other.go, internal/tools/console_common.go, internal/tools/console_open.go, internal/tools/console_send.go, internal/tools/console_read.go, internal/tools/console_close.go, internal/tools/console_send_test.go, internal/tools/console_open_test.go, internal/tools/console_read_test.go, internal/tools/console_close_test.go
**Read first:** internal/tools/console_common.go — collectConsoleWindow, awaitConsoleExit, consoleTail;
internal/console/ring.go — ring.Read, drainLocked; internal/console/process_other.go — Process.Read;
internal/tools/console_close.go — ConsoleClose.Execute; internal/tools/console_send_test.go — TestCollectConsoleWindow_HoldsNoMoreThanTheCeiling
**Tests:** program that prints then goes quiet, `console_send` with `wait_ms: 30000`, cancel after
50 ms → nil error in under 1 s, result carries the output printed before the cancel and the cut-short
line; `console_read` on a quiet console cancelled mid-wait → `context.Canceled` in under 1 s;
`console_open` cancelled mid-wait → nil error in under 1 s, header carries the console id and the
cut-short line; `consoleTail(cancelledCtx, c, 0)` called directly after closing the console still
reports its tail. `ring_test.go`: `ReadContext` cancelled mid-wait returns in under 1 s and leaves the
buffered bytes for the next `Read`. The direct `collectConsoleWindow` call in
`TestCollectConsoleWindow_HoldsNoMoreThanTheCeiling` gains a ctx.
**Acceptance:**
- `go build ./internal/console/ ./internal/tools/`
- `GOOS=windows go build ./internal/console/ ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Console' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/console/`
**Commit:** `fix(tools): settle console waits on cancel`

## 13. apply_patch: whole-line, unique matches and newline-safe inserts — ✅ DONE (2026-09-29)

NOTES (2026-09-29): edit_existing_file's model-facing description left unchanged — a golden tool-menu byte pin covers it; the ambiguity and mismatch errors carry the guidance instead.
NOTES (2026-09-29): the rewritten TestEditExistingFile_RegionsFollowWhereThePatchLanded asserts the regions editRegions already cuts (Trailing ["end", ""] — the file's final newline yields an empty trailing line), unchanged behaviour of the region cutter.

**What:** Fix "`apply_patch` edits land mid-line and glue text onto unterminated files" (silent
corruption).
**Regression guard.** Strip trailing empty context lines from oldLines and newLines before matching; a match
ending before `\r\n` ends at a line end; the appended text's trailing newline applies only to the
no-final-newline case. Supersedes the "indexOf-based applier" docs: update every comment
`grep -n 'indexOf\|verbatim\|FIRST occurrence' internal/tools/file_edit*.go` finds.
**Goal:** A hunk applies only where its old lines match whole lines (start at a line start, end at
a line end or EOF); a hunk matching more than once is refused with an error naming the ambiguity,
as `find_replace` does; a pure insertion into non-empty text lacking a trailing newline inserts one
separator first and ends the appended text with a newline.
**Approach (assumed at the header base):** In `internal/tools/file_edit.go` `applyPatch`, replace
the raw `strings.Index` with a line-boundary search that counts matches; fix the pure-insertion
branch's join.
**Files:** internal/tools/file_edit.go, internal/tools/file_edit_test.go
**Read first:** internal/tools/file_edit.go — applyPatch, parsePatchHunks; internal/tools/file_edit_test.go —
TestEditExistingFile_RegionsFollowWhereThePatchLanded, TestEditExistingFile_PreservesContextLines,
TestEditExistingFile_PureInsertionWhereItHasAPosition, patchTestFile; internal/tools/find_replace.go — SingleFindReplace; internal/tools/nearmatch.go — occurrenceNote
**Tests:** `-x = 1` against a file with `x = 10` → no match error; hunk present twice → ambiguity
error; Add hunk onto `a` (no final newline) → `a\nnew\n`; `-x = 1`/`+x = 2` on `"x = 1\r\n"` →
applies. `TestEditExistingFile_RegionsFollowWhereThePatchLanded` becomes the ambiguity case (unique
anchor in its regions assertion, "FIRST occurrence" doc comment rewritten);
`TestEditExistingFile_PreservesContextLines` and `TestEditExistingFile_PureInsertionWhereItHasAPosition`
pass unchanged; other existing patch tests pass.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Patch|FileEdit|Edit' ./internal/tools/`
**Commit:** `fix(tools): match apply_patch hunks on whole lines and keep newlines`

## 14. Sub-agent: degenerateRepeat counts only content lines — ✅ DONE (2026-09-29)

NOTES (2026-09-29): CHANGELOG.md's "one line repeated 50 times" wording sits under the released [0.22.0] heading, so it is left as history; the superseding rule and its content floor are stated in this item's CHANGELOG entry instead of rewording the released line.

**What:** Fix "Delegate reports with many repeated lines are rejected as degenerate".
**Regression guard.** A "content line" is a trimmed line of 4+ runes with a letter or digit: `end` and "GO." no
longer count, so "GO."×2000 alone is no longer faulted. `TestSubAgent_DegenerateNarrationIsAFault`
keeps its 2000 count via "Emit."/"write_file.". Supersedes CHANGELOG.md's "one line repeated 50
times" rule and subagent.go's "most frequent non-blank line" comments: reword both; the CHANGELOG entry names the floor.
**Goal:** A reply of real code with 50+ `}` / `)` / `end` / `---` lines is not degenerate; a reply
repeating one content line 50+ times still is.
**Approach (assumed at the header base):** In `internal/agent/subagent.go` `degenerateRepeat`,
skip trimmed lines under 4 runes or made only of punctuation/symbols before counting.
**Files:** internal/agent/subagent.go, internal/agent/subagent_test.go
**Read first:** internal/agent/subagent.go — degenerateRepeat, degenerateRepeatThreshold, degenerateResultFormat,
completedResult; internal/agent/subagent_test.go — TestSubAgent_DegenerateNarrationIsAFault, numberedReport, completedChild
**Tests:** a ~500-line Go-like body → not degenerate; 60 identical content lines → degenerate.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Degenerate|Subagent|SubAgent' ./internal/agent/`
**Commit:** `fix(agent): ignore punctuation-only lines in the degenerate-repeat check`

## 15. Workflow: a recipe's prompt file contents enter the item key

**What:** Fix "Editing a recipe prompt file does not change the resume key" (ItemKey promises a
changed context file gives a new key).
**Regression guard.** the prompt-contents read uses the same folder-relative stage prompt path readPrompt opens
(after recipe.go's prefix strip, which item 18 later replaces); item 15 runs before item 18 and both
touch internal/agent/recipe.go, where `newRecipeRunner` wires the Runner — set the field from
`recipe.Files` there; when nil, fall back to `Workspace` with readPrompt's clean and `ValidPath`. Every
`stageKeyBrief` caller rekeys once across the upgrade (a resumed or re-run pre-upgrade folder redoes
those stages' finished items): state it in the item and its CHANGELOG entry.
**Goal:** Editing a stage's `prompt:` file between two runs of the same recipe on the same scope
changes every affected item key, so no finished item of that stage is resumed.
**Approach (assumed at the header base):** Give the `Runner` (`internal/workflow/runner.go`) the
recipe's `Files` fs.FS; `stageKeyBrief` folds the prompt file's contents (not only the path) into
the key brief. `PlanHash` is unchanged (the folder still resumes; its items redo). A missing file
is an error, not a silent path-only key.
**Files:** internal/workflow/runner.go, internal/workflow/runner_test.go, internal/agent/workflowspawn.go, internal/agent/recipe.go
**Read first:** internal/workflow/runner.go — stageKeyBrief, Runner, runFanout; internal/workflow/stages.go —
runVerify, runMerge; internal/agent/recipe.go — newRecipeRunner, bindPlanInputs; internal/agent/workflowspawn.go — readPrompt
**Tests:** run, edit prompt file, re-run → items re-run (not "resumed"); unchanged file → resumed
(within one build).
**Acceptance:**
- `go build ./internal/workflow/ ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/workflow/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Recipe' ./internal/agent/`
**Commit:** `fix(workflow): fold prompt file contents into item keys`

## 16. Workflow: a fanout `out:` must name `{item}`

**What:** Fix "A fanout `out:` without `{item}` makes concurrent children share one file".
**Goal:** `Validate` rejects a fanout stage whose `out:` lacks `{item}`, with an error naming the
stage; the shipped recipes still validate.
**Approach (assumed at the header base):** In `internal/workflow/validate.go`, alongside the
existing "`out` only on a fanout" check.
**Files:** internal/workflow/validate.go, internal/workflow/validate_test.go
**Read first:** internal/workflow/validate.go — stageFields, validateStage, childStageProblems;
internal/workflow/runner.go — outputPath; internal/workflow/validate_test.go — fanoutStage; internal/skills/load_test.go —
TestLoadShippedSkillsAllParse; internal/skills/parse.go — parseRecipe; internal/tools/fan_out.go — out schema description
**Tests:** `out: results/report.md` on fanout → error; `out: "{item}/x.md"` → ok; every shipped
skill under `internal/skills/shipped/` still loads.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/workflow/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/skills/`
**Commit:** `fix(workflow): require {item} in a fanout stage's out path`

## 17. Daemon: scheduled recipe firings apply the recipe-failure judgement

**What:** Fix "Scheduled `run: workflow:` firings never apply the recipe-failure judgement".
**Regression guard.** "The same error text" means the same judgement: the shared helper returns it unprefixed;
headless wraps "apogee headless: ", the daemon its own "apogee: daemon: the %q schedule's" clause.
`TestDaemonFireRunsTheEntrysRecipe` fires against a zero stubRunner Result ("did not run"): its stub
returns `run.Result{Workflow: run.WorkflowOutcome{ID: "wf-1", End: domain.WorkflowFinished, Items: 1}}`.
**Goal:** A scheduled recipe firing whose workflow ends stopped, failed, or all-blocked is recorded
and notified as a failed firing, with the same error text headless exits 1 on.
**Approach (assumed at the header base):** Move `recipeWorkflowFailure` from
`cmd/apogee/headless.go` to `cmd/apogee/wire_firing.go` (one shared judgement; headless calls
it there). In `daemonfire.go` `fire`, after `raise` returns nil, return `out` with that error
(plus `partialRunSuffix`) when it is non-nil.
**Files:** cmd/apogee/headless.go, cmd/apogee/wire_firing.go, cmd/apogee/daemonfire.go, cmd/apogee/daemonfire_test.go
**Read first:** cmd/apogee/daemonfire.go — daemonWiring.fire; cmd/apogee/headless.go — recipeWorkflowFailure;
cmd/apogee/wire_firing.go — partialRunSuffix, firingOutcome; cmd/apogee/daemonfire_test.go — TestDaemonFireRunsTheEntrysRecipe;
cmd/apogee/headless_test.go — stubRunner, TestHeadlessRecipeWithEveryItemBlockedExits1; cmd/apogee/daemon.go — daemonLog.notify
**Tests:** daemon fire with an all-blocked workflow result → failed outcome with the recipe error;
`TestDaemonFireRunsTheEntrysRecipe`'s stub returns a finished workflow outcome.
**Acceptance:**
- `go build ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Fire|Firing|RecipeWorkflowFailure' ./cmd/apogee/`
**Commit:** `fix(daemon): judge scheduled recipe firings like headless`

## 18. Skills: recipe stage prompts stay folder-relative on every OS

**What:** Fix "Windows disk recipe skills cannot find their stage prompts".
**Regression guard.** Keep a separator-tolerant strip (Dir+"/" or Dir+`\`) in `bindPlanInputs` for an embedder's
Dir-prefixed prompt, and rewrite the `workflow.Recipe` contract (public `apogee.Recipe`) to "folder-
relative, slash-separated". The bite is the skills test; the agent backslash-Dir test is a same-both-
sides open check through Files. Supersedes `resolveRecipePrompts`' Dir-joined doc (load.go) and load_test.go's.
**Goal:** A disk recipe's stage prompt reaches the agent as a slash-separated path relative to the
skill folder on every OS, and `readPrompt` opens it through the recipe `Files`.
**Approach (assumed at the header base):** In `internal/skills/load.go`, store the stage prompt
folder-relative (`path` form); drop the
`strings.CutPrefix(stage.Prompt, recipe.Dir+"/")` strip in `internal/agent/recipe.go`.
**Files:** internal/skills/load.go, internal/skills/load_test.go, internal/agent/recipe.go, internal/agent/recipe_test.go, internal/workflow/recipe.go
**Read first:** internal/skills/load.go — resolveRecipePrompts; internal/skills/parse.go — normalizePromptPath;
internal/agent/recipe.go — bindPlanInputs; internal/agent/workflowspawn.go — workflowSpawner.readPrompt; internal/workflow/recipe.go — Recipe;
internal/skills/load_test.go — TestLoadRecipePromptsResolveUnderTheSkillDir, TestShippedAuditPromptsAreEmbeddedAndFinishShaped; internal/agent/recipe_test.go — TestBindPlanInputs
**Tests:** loaded disk recipe → `Stage.Prompt == "prompts/x.md"` (red at base); a backslash-`Dir`
agent test opens it through Files (same both sides); a Dir-prefixed prompt still opens; rewritten to
the folder-relative form: `TestLoadRecipePromptsResolveUnderTheSkillDir`,
`TestShippedRecipePromptsStayVirtual`, `TestShippedAuditPromptsAreEmbeddedAndFinishShaped` (reads
`shippedFiles` at `shippedDir+"/audit/"+stage.Prompt`), `TestBindPlanInputs` (feeds
`"prompts/f.md"`); cross-build for Windows.
**Acceptance:**
- `go build ./... && GOOS=windows go build ./...`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/skills/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Recipe' ./internal/agent/`
**Commit:** `fix(skills): keep recipe stage prompts folder-relative`

## 19. Skills: bound the lenient frontmatter fold

**What:** Fix "Hostile SKILL.md frontmatter causes a quadratic stall on every skill load".
**Regression guard.** `normalizeTriggers` dedupes after the fold, so cap raw items at a looser bound (e.g.
4*maxTriggers) or stop only once the normalized unique count reaches maxTriggers — never at
maxTriggers raw entries. The value's first append stays space-free (today's `TrimSpace(""+" "+line)`).
**Goal:** Folding a lenient frontmatter value is linear in input size: a value stops growing past
`maxDescriptionLen` runes and a list past `maxTriggers` entries.
**Approach (assumed at the header base):** In `internal/skills/parse.go`'s lenient fold, accumulate
continuation lines in a `strings.Builder` per key with those ceilings; stop appending once reached.
**Files:** internal/skills/parse.go, internal/skills/parse_test.go
**Read first:** internal/skills/parse.go — scanFrontmatterFields, sequenceItem, splitTriggers, normalizeTriggers,
maxDescriptionLen, maxTriggers; internal/sanitize/sanitize.go — ClampRunes; internal/skills/load.go — maxSkillFileBytes
**Tests:** 1 MiB frontmatter with an unterminated quote and ~500k one-char lines parses in < 1 s;
existing lenient-fold outputs unchanged, including a 33+-entry `triggers:` list with a case-variant
repeat in its first 32.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/skills/`
**Commit:** `fix(skills): bound the lenient frontmatter fold`

## 20. Terminal: arm the merged-stdout kill only on a chained line

**What:** Fix "Merged-stdout denial watch re-opens the incident ADR 0056 D2 closed" per the
ratified call: `cat build.log 2>&1` is killed on a data line ending in `permission denied`.
**Regression guard.** A trailing separator with nothing after (`cat log 2>&1\n`, `… 2>&1;`, `… 2>&1 &`) is not
chained. Every comment or doc sentence saying a stream-merging line arms the stdout watch gets the
"only when chained" qualifier: `grep -rn -i 'merge' internal/tools/terminal.go internal/platform/denialkill.go
internal/subprocess/subprocess.go docs/adr/0056* docs/design/confinement-execution-contract.md CHANGELOG.md`.
**Goal:** A terminal line with a stream merge arms the merged-stdout kill only when a command
separator (`&&`, `||`, `;`, `&` as a separator, newline) follows the merge on the model's line; a
single command or pipeline with a merge keeps the stderr-only watch and the post-run label. ADR
0056 records the change as a dated amendment.
**Approach (assumed at the header base):** In `internal/tools/terminal.go`, next to
`mergedStreamsPattern`, compute `watchMergedStdout` from the merge plus a later separator on the
model's own line (before the fail-fast preamble joins). Update the `mergedStreamsPattern` and
`WatchMergedStdout` doc comments (`internal/subprocess/subprocess.go`). Amend ADR 0056 under the
2026-09-26 amendment.
**Files:** internal/tools/terminal.go, internal/tools/terminal_test.go, internal/subprocess/subprocess.go, docs/adr/0056-terminal-fail-fast-and-session-scratch.md, internal/platform/denialkill.go, docs/design/confinement-execution-contract.md
**Read first:** internal/tools/terminal.go — Terminal.Execute, mergedStreamsPattern, Terminal (type doc);
internal/tools/terminal_test.go — TestTerminal_ArmsMergedStdoutWatchOnlyForAStreamMergingLine, capturedRunHost;
internal/subprocess/subprocess.go — SubprocessSpec.WatchMergedStdout; internal/platform/denialkill.go — DenialKillWriter, NewAnchoredDenialKillWriter
**Tests:** `cat log 2>&1` and `cat log 2>&1 | tail` → not armed; `mkdir /x 2>&1 && cd /x` → armed;
`a 2>&1; b` → armed; `cat log 2>&1;` and `cat log 2>&1 &` → not armed. In
`TestTerminal_ArmsMergedStdoutWatchOnlyForAStreamMergingLine` (retitled, doc comment too) the rows
`make 2>&1 | tail -n 20`, `echo oops >&2`, `make &> build.log`, `make |& tee build.log` want false,
each with a chained twin wanting true. This arm-decision table is the proof.
**Acceptance:**
- `go build ./internal/tools/ ./internal/subprocess/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Merged|Terminal' ./internal/tools/`
**Commit:** `fix(tools): arm the merged-stdout denial kill only on chained lines`
