# Refocus follow-ups — bookkeeping, network/program fences, docmap, doc fixes, signed thinking — plan

**Goal:** Webhook reactions go through the guarded HTTP client and clipboard/tmux lookups through
`security.ResolveProgram`; every package past the docmap threshold opts in; the verified-stale
doc lines are corrected; Anthropic signed thinking is carried and replayed.
**Date:** 2026-10-02
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** f68c5c56
**Sources:**
- `docs/handoffs/archived/2026-10-02 - 00 - refocus findings and plan for planned items 1-4.md`
- beads `apogee-windows-confiner-test-failures`, `apogee-refused-hold-followers-move`, `apogee-4kl`
- `SECURITY.md`; ADR 0012 Amendment 2026-07-26 (operator-named endpoints); ADR 0043 D4; ADR 0078 D4; ADR 0050
- `docs/design/confinement-execution-contract.md` (ResolveProgram amendment 2026-08-30)

**Ratified design calls (owner, 2026-10-02):**
- **Packaging:** two plans — this one, and `2026-10-02 - 01` for architecture review #9–#20.
- **Item-2 beads:** only `apogee-4kl` is ready; the rest are listed out of scope.
- **Webhook posture:** MCP-style — url-safety allow/deny pre-flight with the IP floor off, `DialPinDestination`, redirects never followed; url-safety lists snapshot at boot.
- **Program fence:** clipboard + tmux go through `ResolveProgram`; bwrap is the contract's stated exception; a bead records the direnv-PATH bwrap case.
- **Profiles:** `minimax-m3` / `qwen3.8` exact patterns are intended; the question closes.
- **4kl request:** `thinking: adaptive` only when an effort above off/none/minimal resolves, `disabled` otherwise.
- **4kl replay:** blocks replay only to the anthropic wire and the model that produced them; dropped silently elsewhere; compaction may drop them; the /thinking pane never shows redacted blocks.
- **4kl sampling:** under adaptive thinking the anthropic body drops profile temperature/top_p/top_k (owner, 2026-10-02).
- **Refused hold:** followers moving on a refused hold is intended; document it and close the bead.
- **Old ADRs:** Mechanism-era wording in ADRs 0001–0045 stays as history.

**Standing requirements:**
- skills: coding-standards
- Pi 5 machine rule: never `go test -race`/`-cover` over a package pattern or a whole heavy package (`./internal/tui/`, `./cmd/apogee/`); narrow those with `-run`. Never two test runs at once.

**Out of scope:**
- Releasing / any version change; the CHANGELOG preamble SemVer sentence (owned by the next release cut).
- Beads needing design or an owner call: apogee-sbj, -vi5, -l8s, -rw6, -03a, -8za, -4h5, -089, -zlg, -3fs, -afu, -bmj, -per-server-idle-timeout.
- Fencing bwrap; Status clauses on ADRs 0001–0045; archived plans' stale `**Status:**` lines.
- Architecture review #9–#20 (plan `2026-10-02 - 01`).

**Regression check (2026-10-02, f68c5c56):**
- 1: guard folded (plain `mv`; no tracked change of its own)
- 2: guard folded (writer decision: syncexec call site moves here); supersedes ADR 0012 Note 2026-09-30 "still the single production use"
- 3: guard folded (writer decision: test + prose only); supersedes the `domain.Config` URLAllowHosts/URLDenyHosts field doc
- 4: guard folded
- 5: guard folded
- 7: guard folded
- 9: guard folded
- 10: guard folded (writer decision folded)
- 11: recast — split; thinking request + compaction moved to new item 12
- 12: recast — new item split from 11
- all items: race Acceptance lines carry `GOMEMLIMIT=2GiB`; `-run` over agent/tui/cmd/apogee is anchored (writer decision)
- 4: rejected — `tr '\n' ' ' < …contract.md | grep -c "autofix's formatter probe"` as the check; it prints 0 at base because :578 opens with the `> ` blockquote marker (folded with the marker stripped)
- 9: rejected — `grep -n "refused" internal/session/live.go cmd/apogee/wire_session.go` as the check; both files already match at base (folded as a `2026-10-02` count)
- re-check round:
- 2: guard folded (writer decision: urlsafety.go/transport.go/ADR 0012 Note 2026-09-30 supersession prose moved to item 3; code + tests only)
- 3: guard folded (writer decision: owns the supersession prose moved from item 2); supersedes ADR 0012 Note 2026-09-30 "still the single production use"
- 11: guard folded (writer decision: replay gate reads a.cfg.Wire); supersedes CONTEXT.md "Thinking channel" glossary "never sends them back Upstream"
- 12: guard folded (seam-level compact test; compact.go prose rule; request-extra sampling caveat); supersedes ADR 0078 D4 and Consequences line :121
- decision round:
- 12: recast (writer decision: adaptive thinking drops profile temperature/top_p/top_k in `buildBody`; sampling bytes unchanged at effort off/unset; ADR 0078 amendment + manual say so; request-extra caveat kept)

## 1. Bookkeeping: archive superseded handoffs, close the Windows confiner bead — ✅ DONE (2026-10-02)

NOTES (2026-10-02): all four FILES paths are ignored by docs/handoffs/.gitignore (`*` / `!.gitignore`), so the plain `mv` leaves `git status --porcelain` empty — nothing to stage and no per-item commit; the bead close (closeout `bd close`, reason: fixed by 8c76961a, owner saw both tests pass on windows/arm64) carries the item's only tracked change.
NOTES (2026-10-02): the one repo link to an old handoff path is the plan header's Sources line; left unedited because implementers never write into the plan document — the verifier or closeout may repoint it to docs/handoffs/archived/.

**What:**
**Goal:** `docs/handoffs/` holds neither the 2026-09-30 nor the 2026-10-02 refocus handoff (both
live under `docs/handoffs/archived/`), and bead `apogee-windows-confiner-test-failures` is closed
with a reason naming commit `8c76961a` and the owner's passing windows/arm64 run.
**Regression guard.** `docs/handoffs/.gitignore` is `*` / `!.gitignore`, so neither handoff is
tracked: use plain `mv`, not `git mv`. The item has no tracked change of its own — no per-item
commit; its commit, if any, carries only the closeout's `bd close` export.
**Approach (assumed at the header base):** `mv` both handoffs into `docs/handoffs/archived/`;
fix any repo link to their old paths (`grep -rn "refocus briefing and pending doc fixes\|refocus findings and plan" --include=*.md .`).
Run `bd show apogee-windows-confiner-test-failures` first; the closeout closes it via **Closes:**.
**Files:** docs/handoffs/2026-09-30 - 00 - refocus briefing and pending doc fixes.md; docs/handoffs/2026-10-02 - 00 - refocus findings and plan for planned items 1-4.md; docs/handoffs/archived/
**Read first:** docs/handoffs/.gitignore — `*` rule; docs/handoffs/2026-10-02 - 00 - refocus findings and plan for planned items 1-4.md — Item 1 — Bookkeeping; docs/plans/archived/2026-09-30 - 00 - denial-label-and-windows-confiner-tests-plan.md — NOTES "ticket stays OPEN" until owner reports;
.beads/issues.jsonl — apogee-windows-confiner-test-failures; docs/plans/2026-10-02 - 00 - refocus-followups-plan.md — header Sources line (the only repo link to the old handoff path)
**Tests:** none (docs move).
**Acceptance:**
- `ls docs/handoffs/ | grep -c "refocus"` prints `0`
- `ls docs/handoffs/archived/ | grep -c "2026-09-30 - 00\|2026-10-02 - 00"` prints `2`
**Closes:** apogee-windows-confiner-test-failures
**Commit:** `chore(handoffs): archive the refocus handoffs superseded by the 2026-10-02 plans`

## 2. Webhook observe lane posts through the guarded client — ✅ DONE (2026-10-02)

NOTES (2026-10-02): consequential edit — internal/reactions/doc.go: made necessary by reactions now importing internal/security for the webhook guard too (the one-direction import line named only path resolution)
NOTES (2026-10-02): renamed TestWebhookPackageImportsOnlyDomainFromApogee to TestWebhookPackageImportsOnlyDomainAndSecurityFromApogee — relaxing it to domain + security made the old name false
NOTES (2026-10-02): firingHooks takes the guard as a new third parameter (raise builds it off in.opts); webhook.Post's reply body is wrapped so closing it cancels the send's timeout ctx and closes the per-send client's idle connection (the ctx must outlive Post for the sync lane's body read); a URL that does not parse is now refused as "the URL does not parse" before any request is built
NOTES (2026-10-02): added TestDefaultExecutorPostsThroughTheGuardItWasGiven (reactions) and TestWebhookPostWordsADeadlineInThePinAsATimeout (webhook) beyond the plan's named tests

**What:**
**Goal:** `webhook.Post` sends only through a `security.URLGuard`-built client: a host on the
url-safety deny list (or off a non-empty allow list) is refused before any dial, a 3xx comes back
as the response (`HTTP 30x`, never followed, so headers never reach a second host), and a
loopback/LAN endpoint still receives the post. The observe lane gets its guard from the boot
config's url-safety lists.
**Regression guard.** This item also changes the `runSyncWebhook` call site in
internal/agent/syncexec.go to pass `security.NewURLGuard(a.cfg.URLAllowHosts, a.cfg.URLDenyHosts)`,
so its commit builds; add internal/agent/syncexec.go to Files. `Post` opens with
`ctx, cancel := context.WithTimeout(ctx, timeout)` before the pre-flight and `GuardedClient` (the
pin's DNS lookup runs on ctx, and the sync lane's ctx has no deadline); a deadline hit in the pin
step is worded "timed out after <timeout>", like a client timeout. Both observe-lane sites build the
guard from options, not a projected cfg (both run before one exists): `w.opts.URLAllowHosts/
URLDenyHosts` in wire_boot.go, `in.opts` in `raise` (passed into `firingHooks`). Rule: amend every
comment or doc line claiming a webhook connection is pooled or reused (`grep -rn "reus\|connection
pool\|fresh socket" internal/webhook internal/reactions docs/manual/reactions.md`); the manual line
(:278) rides item 3. The supersession prose (urlsafety.go and transport.go comments, ADR 0012 Note
2026-09-30) is item 3's (writer decision); this item holds only the code change and its tests.
**Approach (assumed at the header base):** Fix for the refocus finding (2026-10-01): webhooks used a
plain `http.Client` that followed redirects and forwarded `headers-env:` headers cross-host.
`internal/webhook/webhook.go` `Post` takes a
`security.URLGuard`; it pre-flights with `guard.DisableIPFloor().CheckContext` and builds the
client with `guard.GuardedClient(..., Policy: DialPinDestination)` as `internal/mcp/transport.go`
`checkEndpoint`/the guarded-client call do; `CloseIdleConnections` after the send and the pooling
comment corrected. Refusal wording stays in webhook and never quotes the URL (`PostFailure`
rule). Relax `TestWebhookPackageImportsOnlyDomainFromApogee` and the package doc to domain +
security. `reactions.DefaultExecutor` takes the guard; `cmd/apogee/wire_boot.go` and
`wire_firing.go` build it with `security.NewURLGuard(opts.URLAllowHosts, opts.URLDenyHosts)`.
**Files:** internal/webhook/webhook.go; internal/webhook/webhook_test.go; internal/reactions/exec.go; internal/reactions/webhook.go; internal/reactions/webhook_test.go; internal/reactions/command_test.go; internal/agent/syncexec.go; cmd/apogee/wire_boot.go; cmd/apogee/wire_firing.go; cmd/apogee/wire_firing_test.go
**Read first:** internal/webhook/webhook.go — Post, PostFailure, MaxResponseDrain; internal/mcp/transport.go — vetEndpoint, checkEndpoint, endpointRefusal; internal/security/httpclient.go — GuardedClient, PinError, DialPinDestination;
internal/reactions/exec.go — DefaultExecutor, defaultExecutor; internal/reactions/webhook.go — webhookSender.Run; cmd/apogee/wire_boot.go — reactions.New Options (before projectConfig);
cmd/apogee/wire_firing.go — firingHooks, raise; internal/webhook/webhook_test.go — TestWebhookPackageImportsOnlyDomainFromApogee
**Tests:** in `internal/webhook/webhook_test.go`: a redirecting httptest server whose target
records requests — asserts the post returns the 3xx and the target saw nothing (fails before);
a deny-listed host refused before dial; a loopback endpoint under `URLGuard{}` still posts.
`cmd/apogee/wire_firing_test.go` `TestFiringHooksRefusesADenyListedWebhookEndpoint`: a deny-listed
loopback endpoint gets no request through `firingHooks` (pins the guard comes from the url-safety lists).
**Acceptance:**
- `go build ./... && go vet ./internal/webhook/ ./internal/reactions/ ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/webhook/ ./internal/reactions/`
- `GOMEMLIMIT=2GiB go test -count=1 -run '^TestFiringHooksRefusesADenyListedWebhookEndpoint$|^TestFiringConfigInstallsTheHookRunner$|^TestE2EHooksFireFromAHeadlessRun$|^TestDaemonFiringFiresHooks$|^TestRootWiringEmitsThroughTheHookRunner$' ./cmd/apogee/`
**Commit:** `fix(webhook): post reactions through the url-safety guarded client, never following redirects`

## 3. Webhook sync lane fenced too; SECURITY.md and the manual say so — ✅ DONE (2026-10-02)

NOTES (2026-10-02): runSyncWebhook already passed the url-safety guard (item 2 moved the call site); this item added only the two sync-lane tests and the prose, per the item's regression guard
NOTES (2026-10-02): the two new tests drive runSyncWebhook through the gate stage using gate_test.go's helpers (gateAgent, userGateWebhook, gateEndpoint); the syncexec_test.go header comment now says so
NOTES (2026-10-02): configuration.md also rewords "A configured MCP endpoint is the one deliberate exemption" to "the first of two" and the section's opening line to cover webhooks — both made false/incomplete by the new webhook paragraph
NOTES (2026-10-02): pre-existing debt, untouched — internal/security/urlsafety.go disableFloor field doc and internal/security/ssrf.go floorEnabled doc still say the floor is turned off only "for a test or a deliberately-unfenced embedder" (stale since the MCP exemption); internal/config/defaults/config.yaml's url-safety comment still names only the network tools and MCP, not webhooks

**What:** Depends on item 2.
**Goal:** advise/gate webhook reactions (`runSyncWebhook`) post through the same guard built from
the engine config's url-safety lists, and `SECURITY.md` plus `docs/manual/reactions.md` state that
webhook reactions obey url-safety allow/deny, keep the private-range floor off (operator-named,
like MCP), never follow redirects, and pick up url-safety edits at the next start.
**Regression guard.** Item 2 already changed the syncexec call site — this item adds only the
sync-lane redirect test (internal/agent/syncexec_test.go) and the SECURITY.md /
docs/manual/reactions.md prose; drop internal/agent/syncexec.go from Files. Verified reviewer guards
add: a sync deny-list test (`a.cfg.URLDenyHosts = ["127.0.0.1"]` → gate escalates to ask, endpoint
sees no request); reactions.md:278's "so the connection can be reused" is corrected (item 2's rule);
docs/manual/configuration.md names webhooks in the url-safety binds list (:317-319), as a second
operator-named floor exemption beside MCP (:352), and in the "Both lists are live" paragraph (:367)
as picking up edits at the next start. Supersedes the internal/domain/config.go URLAllowHosts/
URLDenyHosts field doc ("the network tools'", "DEFAULT tool set only"): it now says sync-lane
webhooks read them too, injected Config.Tools or not. Owns the supersession prose moved from item 2:
internal/security/urlsafety.go:103 (`DisableIPFloor` "at ONE call: internal/mcp") and
internal/mcp/transport.go:409 comments naming MCP as the single `DisableIPFloor` production use, and
an inline amendment on ADR 0012's Note 2026-09-30 ("still the single production use") — reword each
to name webhooks as the second production use, per the owner's ratified webhook posture.
**Approach (assumed at the header base):** `internal/agent/syncexec.go` `runSyncWebhook` passes
`security.NewURLGuard(a.cfg.URLAllowHosts, a.cfg.URLDenyHosts)` to `webhook.Post`.
**Files:** internal/agent/syncexec_test.go; SECURITY.md; docs/manual/reactions.md; docs/manual/configuration.md; internal/domain/config.go; internal/security/urlsafety.go; internal/mcp/transport.go; docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md
**Read first:** internal/agent/syncexec.go — runSyncWebhook, syncTimeout; internal/agent/gate.go — gateAnswer; internal/agent/gate_test.go — TestGateWebhookFailureEscalatesToAsk, gateAgent, userGateWebhook, gateEndpoint;
internal/agent/advise_webhook_test.go — TestAdviseWebhookFailsOpenOnNon2xx; docs/manual/reactions.md — Sending a webhook; docs/manual/configuration.md — url-safety section;
SECURITY.md — Network fencing bullet; internal/domain/config.go — URLAllowHosts field doc
**Tests:** `TestSyncWebhookRedirectIsAFailedPost`: a gate webhook whose endpoint redirects reports
the 3xx (gate treats it as a failed post per its existing failure rule) and the redirect target
sees no request. `TestSyncWebhookDenyListedEndpointIsNeverPosted`: the deny-list case above.
**Acceptance:**
- `go vet ./internal/agent/ ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestSyncWebhookRedirectIsAFailedPost$|^TestSyncWebhookDenyListedEndpointIsNeverPosted$|^TestGateWebhookFailureEscalatesToAsk$|^TestGateWebhookAnswersAllowDenyAsk$|^TestAdviseWebhookFailsOpenOnNon2xx$' ./internal/agent/`
- `grep -n "webhook" SECURITY.md`
- `grep -n "redirect" docs/manual/reactions.md` and `grep -n "url-safety" docs/manual/reactions.md` each print a line
**Commit:** `fix(agent): fence advise and gate webhooks with the url-safety guard`

## 4. Clipboard and tmux programs resolve through ResolveProgram — ✅ DONE (2026-10-02)

NOTES (2026-10-02): tmux moved to present — `present.Clipboard.WriteTmux` owns the gate, the resolved `tmux load-buffer -w -` argv, the 2s deadline and the runner; internal/tui/clipboard_test.go is deleted and its three TestTmuxClipboard{StartsNothingOutsideTmux,LoadsTheBufferInsideTmux,ReportsARunnerFailure} tests live on under the same names in internal/present/clipboard_test.go (argv[0] now the resolved absolute path), so the Acceptance `-run` over ./internal/tui/ no longer matches those three there.
NOTES (2026-10-02): the tui seams changed signature to `func(workspace, text string) error` and the Cmds to `systemClipboardCmd(workspace, text)` / `tmuxClipboardCmd(workspace, text)`; copyFlash passes `m.opts.Workspace`, and the PATH lookup runs inside the Cmd body. The seams are assigned only in tests.
NOTES (2026-10-02): a candidate needs only its copy program on PATH — atotto also demanded the paste partner (wl-paste, termux-clipboard-get, powershell.exe), which a write never runs; the system write keeps atotto's no-deadline run; a refused candidate stops the walk (returns the fence's error) rather than falling to the next candidate.
NOTES (2026-10-02): bead `apogee-bwrap-direnv-path` opened with `bd create` (P3 bug) for the direnv-PATH bwrap case; its export is the .beads/issues.jsonl line.
NOTES (2026-10-02): the contract's 2026-08-30 amendment list drops the formatter probe (autofix was retired in eaa340de) and a new 2026-10-02 amendment records the clipboard/tmux fence and names bwrap as the one exception.

**What:**
**Goal:** the TUI's clipboard write (unix/darwin helper programs) and its tmux buffer load run
only a program `security.ResolveProgram` resolved against the session workspace, so a workspace
directory on PATH cannot supply them; Windows keeps the Win32 clipboard. The confinement contract
says so, names bwrap as the one exception (resolved once per process before any workspace
exists), and no longer names "autofix's formatter probe". A bead records the direnv-PATH bwrap case.
**Regression guard.** Never assign `writeSystemClipboard`/`writeTmuxClipboard` outside tests (the
serial mouse tests install recorders BEFORE `newTestModel`): `copyFlash` (mouse.go) passes
`m.opts.Workspace` into `systemClipboardCmd`/`tmuxClipboardCmd`, resolving inside the Cmd body, off
Update. Carry atotto v0.1.4's per-candidate argv verbatim (`xclip -in -selection clipboard`, `xsel
--input --clipboard`, …) and its `clip.exe` rung; run with nil stdout/stderr as `runWithStdin` does.
Say whether the tmux tests (clipboard_test.go, argv pinned `tmux load-buffer -w -`) move to present
or keep argv[0] bare with resolution inside the runner.
**Approach (assumed at the header base):** a `present.Clipboard{LookPath, WorkspaceRoot}` beside
`present`'s opener (which already uses `ResolveProgram` with a LookPath seam): an explicit
candidate list in atotto's order (`WAYLAND_DISPLAY` → `wl-copy`, then `xclip`, `xsel`,
`termux-clipboard-set`; darwin `pbcopy`), each resolved then run with stdin. `internal/tui/clipboard.go`
builds `writeSystemClipboard`/`writeTmuxClipboard` as closures over it with `opts.Workspace`,
keeping the `func(string) error` seams the mouse tests swap. Open the bead with `bd create`.
**Files:** internal/present/clipboard.go; internal/present/clipboard_test.go; internal/present/doc.go; internal/tui/clipboard.go; internal/tui/clipboard_test.go; internal/tui/mouse.go; internal/tui/mouse_test.go; docs/design/confinement-execution-contract.md
**Read first:** internal/tui/clipboard.go — writeSystemClipboard, writeTmuxClipboard, loadTmuxBuffer, runWithStdin, systemClipboardCmd, tmuxClipboardCmd; internal/tui/mouse.go — Model.copyFlash; internal/tui/mouse_test.go — recordSystemClipboard, recordTmuxClipboard;
internal/tui/clipboard_test.go — fakeRunner, TestTmuxClipboardLoadsTheBufferInsideTmux; internal/present/opener.go — Opener.resolveProgram, Opener.LookPath; internal/security/execsafety.go — ResolveProgram;
internal/tui/seams_guard_test.go — TestNoParallelTestSwapsAPackageSeam; ~/go/pkg/mod/github.com/atotto/clipboard@v0.1.4/clipboard_unix.go — init, writeAll
**Tests:** `internal/present/clipboard_test.go`: a LookPath fake answering a path inside the
workspace → refused, nothing run; outside → run with the text on stdin; tmux the same.
`internal/tui/mouse_test.go` recorders and the `systemClipboardCmd("hello")`/`tmuxClipboardCmd("hello")`
calls follow the new Cmd signatures.
**Acceptance:**
- `go vet ./internal/present/ ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/present/`
- `GOMEMLIMIT=2GiB go test -count=1 -run '^TestDragCopyAlsoWritesTheSystemClipboard$|^TestSystemClipboardFailureStillConfirmsTheCopy$|^TestTmuxClipboardReceivesTheDragCopy$|^TestTmuxClipboardFailureStillConfirmsTheCopy$|^TestTmuxClipboardStartsNothingOutsideTmux$|^TestTmuxClipboardLoadsTheBufferInsideTmux$|^TestTmuxClipboardReportsARunnerFailure$|^TestNoParallelTestSwapsAPackageSeam$' ./internal/tui/`
- `tr '\n' ' ' < docs/design/confinement-execution-contract.md | sed 's/ > / /g' | grep -c "autofix's formatter probe"` prints `0` (1 at base; the :1279/:1399 `autofix` lines stay as history)
**Commit:** `fix(present): resolve clipboard and tmux programs through ResolveProgram`

## 5. Every package past the docmap threshold opts in — ✅ DONE (2026-10-02)

**What:**
**Goal:** `internal/provider`, `internal/stubllm` and `cmd/demorig` each carry a
`docmap_test.go` calling `docmap.Check`, and their `doc.go` maps name every non-test file
(including `doc.go`), so ADR 0043 D4 holds for every package with ten or more non-test files.
**Regression guard.** `cmd/demorig` has 16 non-test files today and `docmap.unmapped` counts
`doc.go`, so its map is a 17-name map (the 16 files, record_windows.go/term_windows.go included,
plus doc.go); a 16-name map fails `TestDocMapNamesEveryFile`.
**Approach (assumed at the header base):** copy `internal/probe/docmap_test.go`'s shape. Extend
`internal/provider/doc.go`'s prose with a file map; add the `doc.go` line to `internal/stubllm/doc.go`;
create `cmd/demorig/doc.go` holding the command comment moved out of `main.go` plus a 17-name map
(build-tagged files count).
**Files:** internal/provider/doc.go; internal/provider/docmap_test.go; internal/stubllm/doc.go; internal/stubllm/docmap_test.go; cmd/demorig/doc.go; cmd/demorig/main.go; cmd/demorig/docmap_test.go
**Read first:** internal/docmap/docmap.go — Check, unmapped, names; internal/probe/docmap_test.go — TestDocMapNamesEveryFile; internal/provider/doc.go — package comment (names only wire_openai.go, wire_anthropic.go);
internal/stubllm/doc.go — package comment (lacks doc.go only); cmd/demorig/main.go — "Command demorig" package comment
**Tests:** the three new `TestDocMapNamesEveryFile` tests.
**Acceptance:**
- `go vet ./internal/provider/ ./internal/stubllm/ ./cmd/demorig/`
- `go test -count=1 -run TestDocMapNamesEveryFile ./internal/provider/ ./internal/stubllm/ ./cmd/demorig/`
**Commit:** `docs(docmap): opt provider, stubllm and demorig into the file-map rule`

## 6. Manual and code-comment corrections — ✅ DONE (2026-10-02)

NOTES (2026-10-02): configuration.md names the retired `endpoint:`/`api-key:`/`host-alias:`/`model:` shape beside the "Five keys" count as folded once at start-up and refused when the fold cannot be made safely (and on a live re-read) — `migrateLegacyConfig`/`legacyRefusal` fold it rather than refuse it outright, so "refused" alone would misstate the code; the count stays five because the quadruple is described as a shape, linked to its own section.
NOTES (2026-10-02): the `ui:` count reads "Nine more keys" (registry: ten `ui.*` rows, `ui.skill-suggestions` documented above it), seven of them visual plus `ui.inspector` and `ui.stall-after`, which the section already describes further down.

**What:**
**Goal:** each of these says what the code does: `docs/manual/daemon.md` (a daemon that refuses
to start — lock held, config does not resolve, `schedules.yaml` invalid — prints why and exits
`1`, per `runDaemon`); `docs/manual/reactions.md` (gate reason cap is "240 runes", per
`gateReasonRunes`); `docs/manual/configuration.md` (the top-level key count names the retired
`endpoint:/api-key:/host-alias:/model:` shape as refused; the `ui:` count matches the registry's
`ui.*` keys); `docs/manual/building.md` (race-test notes name stock Pi 4 and Pi 5 kernels as
refusing `-race` — 39-bit and 47-bit VA — with timing attributed to "a 4-core arm64 box", no
custom-kernel detail); the `internal/config/reactions.go` comment on combined handlers (only
`run:` beside `advise:` on `[file-changed]` validates; `run:`+`gate:` cannot).
**Approach (assumed at the header base):** edit the prose; counts are recounted from the code
(`configmigrate.go` refusal, the settings registry) at edit time, never copied from this plan.
**Files:** docs/manual/daemon.md; docs/manual/reactions.md; docs/manual/configuration.md; docs/manual/building.md; internal/config/reactions.go
**Read first:** cmd/apogee/daemon.go — runDaemon, runDaemonWith; internal/agent/gate.go — gateReasonRunes; internal/config/configmigrate.go — retiredShapeRefusal; internal/config/reactions.go — entryReactions comment;
docs/manual/configuration.md — "Keys apogee migrates for you", "The terminal UI — ui:"; docs/manual/building.md — Testing (APOGEE_TEST_RACE, Pi notes); cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey
**Tests:** none (prose and comment only).
**Acceptance:**
- `go vet ./internal/config/`
- `grep -n "exits \`1\`" docs/manual/daemon.md`
- `grep -c "240 characters" docs/manual/reactions.md` prints `0`
**Commit:** `docs(manual): correct daemon exit status, rune caps, key counts and race-test notes`

## 7. ADR amendments for verified-stale lines — ✅ DONE (2026-10-02)

NOTES (2026-10-02): ADR 0063 carries both a Status clause (ADR 0088 supersedes the restated "a cancel still rolls the whole Turn back") and an inline "(Amended 2026-10-02: …)" note at that sentence, per the item's regression guard.

**What:**
**Goal:** these ADRs carry an inline `(Amended 2026-10-02: …)` note (or a Status clause) stating
the current fact, originals never rewritten: 0010 (the root facade also imports
`internal/config`, `eventjson`, `profiles`, `reactions`, `workflow` as forwarders; no engine
logic); 0057 (six tools ship default-off: `console_open/send/read/close`, `fan_out`, `workflow`);
0063 (Status names ADR 0088 superseding "a cancel rolls the whole Turn back"); 0075 (sub-run
events go by run id, per ADR 0086); 0076 (`advise:`/`gate:` are live, not rejected at load);
0071 (the "each of the six answers a failed Turn" sentence: read-cache intercepts and
tool-result-cap caps).
**Regression guard.** ADR 0063 carries only `Status: accepted` frontmatter, so a Status-clause-only
edit leaves the Acceptance grep at 0: every one of the six files carries the literal
"Amended 2026-10-02", Status clause or not.
**Approach (assumed at the header base):** follow 0010's existing inline-amendment form; verify
each fact against the code before writing it.
**Files:** docs/adr/0010-package-layout-domain-core-and-thin-root-facade.md; docs/adr/0057-the-tool-roster-is-a-third-model-profile-axis-resolved-axis-wise.md; docs/adr/0063-sub-agent-runs-are-user-addressable-views.md; docs/adr/0075-the-headless-event-stream-is-a-versioned-driver-protocol.md; docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md; docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md
**Read first:** docs/adr/0010-package-layout-domain-core-and-thin-root-facade.md — "(Amended 2026-09-30" form; apogee.go — root imports (agent, config, domain, eventjson, profiles, reactions, workflow);
internal/tools/console_open.go, console_send.go, console_read.go, console_close.go, fan_out.go, workflow.go — DefaultOff; docs/adr/0088-cancel-settles-and-never-rewinds-finished-work.md — Supersedes/Amends frontmatter;
docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md — "rejected at load"; docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md — "Each of the six answers a failed Turn"
**Tests:** none (docs).
**Acceptance:**
- `grep -c "Amended 2026-10-02" docs/adr/00{10,57,63,75,76,71}-*.md` — each ≥ 1
**Commit:** `docs(adr): amend stale lines in ADRs 0010, 0057, 0063, 0071, 0075 and 0076`

## 8. Glossary, layout docs and IDEAS corrections — ✅ DONE (2026-10-02)

NOTES (2026-10-02): IDEAS.md was edited in place (the 2026-09-26 handoff link now reads `docs/handoffs/archived/…`; the master-agent stop/message line is marked delivered — the `workflow` tool's `stop`/`message` actions, v0.23.4, bead `apogee-engine-run-delegation` closed) but it is gitignored (`.gitignore:14`) and untracked, so it is deliberately left off FILES and out of the commit; IDEAS.md's own rule says a resolved item is removed, but the Goal's "marks as delivered" plus the Acceptance grep on the archived link keep the line.
NOTES (2026-10-02): tool-layout.md — the per-tool table now carries the code's Title-Case labels (`toolregistry.go`), `exit 0` / `PASS` / `FAIL` with no durations, `ask_user`'s slot as the human's own answer and `git_diff_range`'s `base...head` target; the "As implemented" paragraph now says the table states the shipped form, and the Vocabulary example "exit 0 · 1.2s" became "exit 0".
NOTES (2026-10-02): split-diff-layout.md carried `siff-layout.md` as plain code text (no markdown link) at base already; it now names it as an earlier sketch that was never committed.

**What:**
**Goal:** `CONTEXT.md`'s Plan-mode entry agrees with its later entry that Plan runs a recipe's
script stage, confined (ADR 0012 Amendment 2026-09-27); `docs/layout/split-diff-layout.md` names
`siff-layout.md` as an uncommitted earlier sketch, not a file; `docs/layout/tool-layout.md`'s
table matches the code's Title-Case labels, shows no durations the code does not render, and
agrees with its "As implemented" prose; `IDEAS.md` links the 2026-09-26 handoff at its archived
path and marks the `apogee-engine-run-delegation` idea as delivered (workflow tool stop/message).
**Approach (assumed at the header base):** labels and slots are read from `internal/tui`
(`toolregistry.go` and the layout code) at edit time.
**Files:** CONTEXT.md; docs/layout/split-diff-layout.md; docs/layout/tool-layout.md; IDEAS.md
**Read first:** CONTEXT.md — Agent mode (Plan bullet), Scratch dir entry (Recipe script stage, ADR 0012 amendment 2026-09-27); docs/layout/tool-layout.md — "As implemented" note, per-tool table;
internal/tui/toolregistry.go — tool registry `label:` entries (Title Case: "Find Files", "Git Status", "Sub-Agent", "Task List", "Ask User"); docs/layout/split-diff-layout.md — siff-layout.md sentence;
IDEAS.md — master-agent stop/message idea line (apogee-engine-run-delegation); internal/tools/workflow.go — WorkflowActionStop, WorkflowActionMessage
**Tests:** none (docs).
**Acceptance:**
- `grep -n "handoffs/archived/2026-09-26" IDEAS.md`
- `grep -n "siff-layout" docs/layout/split-diff-layout.md` shows no markdown link
**Commit:** `docs: align CONTEXT, layout docs and IDEAS with the shipped behaviour`

## 9. A refused hold moving the followers is documented as intended — ✅ DONE (2026-10-02)

NOTES (2026-10-02): comment-only; `Live.Activate`'s doc already says followers move "even when the hold was refused", so it is unchanged; the pin stays `TestLiveActivateRefusedStillMovesFollowers`, whose comment now cites the 2026-10-02 owner call. The implementer's `go test -race ./internal/session/` run was refused by the host's permission classifier, so the Acceptance test run is left to the verifier (go vet ./internal/session/ and gofmt are clean).

**What:**
**Goal:** the doc comments on `session.Live.Activate` and on the scratch/journal follower move in
`cmd/apogee/wire_session.go` state that a refused hold still moves the followers (owner call
2026-10-02), and a test pins it: after `Activate` returns a refused hold, the followers point at
the activated session id.
**Regression guard.** The pin already exists: `TestLiveActivateRefusedStillMovesFollowers`
(live_test.go) asserts the *HeldError and the follower move, and `Live.Activate`'s doc already says
followers move "even when the hold was refused". Add no duplicate: that test's comment cites the
2026-10-02 owner call; edit only the comments at `sessionHost.Activate`/`followScratch`/`followJournal`.
**Approach (assumed at the header base):** comment edits; the existing `Live` hold test is the pin.
**Files:** internal/session/live_test.go; cmd/apogee/wire_session.go
**Read first:** internal/session/live.go — Live.Activate, Live.move, Live.holdLocked, NewLive; internal/session/live_test.go — TestLiveActivateRefusedStillMovesFollowers, newTestLive;
cmd/apogee/wire_session.go — sessionHost.Activate, sessionHost.followScratch, sessionHost.followJournal; .beads/issues.jsonl — apogee-refused-hold-followers-move
**Tests:** none new — `TestLiveActivateRefusedStillMovesFollowers` is the pin (comment only).
**Acceptance:**
- `go vet ./internal/session/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/session/`
- `grep -c "2026-10-02" internal/session/live_test.go cmd/apogee/wire_session.go` — each ≥ 1 (0 at base)
**Closes:** apogee-refused-hold-followers-move
**Commit:** `docs(session): record that a refused hold still moves the followers`

## 10. Anthropic thinking blocks are carried verbatim through the provider — ✅ DONE (2026-10-02)

NOTES (2026-10-02): approach deviation — instead of `Signature`/`Data` members on `anthropicBlock`, the block gains an unexported `raw` field with `UnmarshalJSON` (keeps a thinking/redacted_thinking block's bytes as received) and `MarshalJSON` (writes them back as-is), so the empty-`thinking` guard holds by construction; stream-built thinking blocks marshal through `anthropicThinkingBlock`, whose members carry no `omitempty`.
NOTES (2026-10-02): signature_delta and redacted_thinking `data` bytes are charged to maxReplyTextBytes (new `anthropicStream.charge`) so an endless signature cannot grow unbounded; pinned by a `signature_delta` case in TestAnthropicParseSSE_ReplyTextIsCapped.
NOTES (2026-10-02): a reasoning block still open when the stream ends (no content_block_stop) is dropped — its signature may be cut short; pinned in TestAnthropicParseSSE_Thinking.
NOTES (2026-10-02): stream-rebuilt thinking blocks are re-encoded by encoding/json (`<`,`>`,`&` escaped as < etc.), value-identical to the wire; whole-reply and redacted blocks keep their exact bytes until the request marshal compacts them.
NOTES (2026-10-02): the OpenAI-ignores-the-field check, TestOpenAICodecIgnoresThinkingBlocks, lives in wire_anthropic_test.go beside the carrier's other tests.
NOTES (2026-10-02): race Acceptance run narrowed to an anchored -run over the touched tests per the Pi 5 machine rule; the whole package passed without -race.
NOTES (2026-10-02): prepending every block reorders interleaved thinking (thinking, tool_use, thinking, tool_use) to thinking-first, since the seam Message keeps no block order; the plan calls for prepend — worth confirming against the API once replay (item 11) is live.

**What:**
**Goal:** the anthropic codec keeps `thinking` and `redacted_thinking` blocks — text, `signature`,
`data` — byte-verbatim: the SSE parser and the whole-response decoder surface them on the
provider response as an opaque raw block list, and the encoder prepends a `provider.Message`'s
raw blocks to that assistant message's content. The OpenAI codec ignores the field.
**Regression guard.** Any file this item adds to internal/provider is named in
internal/provider/doc.go's file map (item 5's docmap test), and the replay encoder keeps an empty
`thinking` member for blocks that arrive with empty text (display omitted) — `anthropicBlock`'s
`thinking,omitempty` must not drop it. Extend `rawResponseEqual` to compare the raw-block field
(`bytes.Equal` per block), or that decodeWhole case passes with the blocks dropped.
**Approach (assumed at the header base):** `internal/provider/wire.go` `Message`/`RawResponse`
gain an opaque raw-blocks field; `wire_anthropic_stream.go` `startBlock`/`deltaBlock` track
thinking blocks and `signature_delta`, yielding the finished block at `content_block_stop` via a
new `Delta` kind (`stream.go`); `wire_anthropic.go` `anthropicBlock` gains `Signature`/`Data`,
`toRawResponse` keeps the blocks beside the folded reasoning text.
**Files:** internal/provider/wire.go; internal/provider/stream.go; internal/provider/wire_anthropic_stream.go; internal/provider/wire_anthropic.go; internal/provider/wire_anthropic_stream_test.go; internal/provider/wire_anthropic_test.go; internal/provider/doc.go (only when a file is added)
**Read first:** internal/provider/wire_anthropic_stream.go — anthropicStream.startBlock, deltaBlock, event (content_block_stop arm), finish; internal/provider/wire_anthropic.go — anthropicBlock, assistantBlocks, anthropicMessages, anthropicResponse.toRawResponse;
internal/provider/stream.go — DeltaKind consts, Delta; internal/provider/wire.go — Message, RawResponse; internal/provider/wire_anthropic_stream_test.go — TestAnthropicParseSSE_Thinking, parseAnthropicSSE, dumpDeltas;
internal/provider/wire_anthropic_test.go — TestAnthropicCodecEncode, TestAnthropicCodecDecodeWhole, rawResponseEqual; internal/agent/collect.go — collectCompletion (ignores unknown kinds)
**Tests:** extend `TestAnthropicParseSSE_Thinking` with `signature_delta` and `redacted_thinking`
(`parseAnthropicSSE`, `dumpDeltas`); an encode golden replaying blocks, including an empty-text
signed `thinking` block and a `redacted_thinking` `data` block; a decodeWhole case with signatures
(`rawResponseEqual` extended to the raw blocks).
**Acceptance:**
- `go vet ./internal/provider/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/provider/`
**Commit:** `feat(provider): carry anthropic thinking blocks and signatures verbatim`

## 11. Signed thinking persists and replays

**What:** Recast at the regression check (2026-10-02). Depends on item 10.
**Goal:** an anthropic assistant turn's thinking blocks persist on the committed message (they
survive a session save/resume) and replay on the next request only when the wire is anthropic
and the next request's model equals the model that was requested when they were produced.
**Regression guard.** collect.go accumulates the blocks; domain.Response carries them through a
setter/accessor pair on *Response with NewResponse's signature unchanged; loop.go assistantMessage
stores them with msg.WithExtra together with the REQUESTED model (st.Model / a.cfg.Model at request
time), and agent/wire.go toProviderRequest replays them only when the wire is anthropic and the next
request's st.Model equals that stored model; tests: wire_test.go projection cases (same model, other
model, openai wire) and a session round-trip driven with collect_test.go's scriptedDeltas yielding
the new Delta kind (not stubllm); no Closes line. The replay gate's "wire is anthropic" reads
a.cfg.Wire (domain.Config.Wire, "" folds to openai per provider/wire.go), and the wire_test cases
set cfg.Wire explicitly. Prose rule: every comment saying Extra/reasoning never reaches the provider
is rewritten (`grep -n "not re-sent\|drops Extra\|never sends them back" internal/agent/*.go
CONTEXT.md` — loop.go:1046-1047 assistantMessage doc); supersedes CONTEXT.md:779 "Thinking channel"
glossary ("it never sends them back Upstream") for anthropic signed blocks — amend it there.
**Approach (assumed at the header base):** `internal/agent/collect.go` `collectCompletion`
accumulates the blocks; `domain.Response` carries them; `loop.go` `assistantMessage` stores them
with `msg.WithExtra` beside `reasoning_content` (key + requested model); `internal/agent/wire.go`
`toProviderRequest` projects that Extra into the provider message under the replay rule.
**Files:** internal/agent/collect.go; internal/domain/hooks.go; internal/agent/loop.go; internal/agent/wire.go; internal/agent/wire_test.go; internal/agent/state_test.go; CONTEXT.md
**Read first:** internal/agent/loop.go — assistantMessage, assembleResponse, streamResponse; internal/agent/wire.go — toProviderRequest, resolvedEffort; internal/agent/collect.go — completion, collectCompletion; internal/domain/hooks.go — Response, NewResponse, Message.WithExtra, Message.MarshalJSON;
internal/agent/collect_test.go — scriptedDeltas, TestCollectCompletionFoldsEveryDeltaKind; internal/agent/state_test.go — TestSnapshot_PreservesReasoningContent; internal/agent/wire_test.go — TestProviderRequestOmitsInterjected; internal/agent/serverbinding.go — serverBinding.applyTo (cfg.Wire)
**Tests:** `wire_test.go` `TestProviderRequestReplaysSignedThinkingOnlyToItsModel` (each case sets
cfg.Wire explicitly): replayed to the same anthropic model, dropped for another model and for the
openai wire; `state_test.go`
`TestSnapshot_RoundTripsSignedThinking`: a session round-trip driven by `scriptedDeltas` yielding
the new Delta kind.
**Acceptance:**
- `go vet ./internal/agent/ ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestProviderRequestReplaysSignedThinkingOnlyToItsModel$|^TestSnapshot_RoundTripsSignedThinking$|^TestSnapshot_PreservesReasoningContent$|^TestCollectCompletionFoldsEveryDeltaKind$|^TestProviderRequestOmitsInterjected$' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/domain/`
**Commit:** `feat(agent): persist and replay anthropic signed thinking`

## 12. Thinking is requested with effort

**What:** Recast at the regression check (2026-10-02). Depends on item 11.
**Goal:** a resolved ThinkingEffort above off/none/minimal requests `thinking: {type: "adaptive"}`
on the anthropic wire, otherwise `disabled` (default request byte-identical); the compaction
summary never requests thinking there; ADR 0078 D4 carries an amendment saying so.
**Regression guard.** wire_anthropic.go requests `thinking: {type: "adaptive"}` when effort above
off/none/minimal resolves, `disabled` otherwise; internal/agent/compact.go compactCompleter.Complete
forces provider.EffortOff when the wire is anthropic (+ a compact_test.go case);
TestAnthropicCodecEffort in internal/provider/wire_anthropic_test.go recast to expect adaptive for
low..max and disabled for ""/off/none/minimal; prose rule: every comment in
internal/provider/wire_anthropic*.go saying thinking is never requested or always disabled is
rewritten (`grep -n "disabled\|apogee-4kl\|never" internal/provider/wire_anthropic*.go`). Supersedes
ADR 0078 D4 ("v1 never requests thinking", docs/adr/0078-…md:61-66) and its Consequences line :121
("filed as a follow-up bead") by one amendment. The compact test asserts at the seam: a capturing
provider.Responder with cfg.Wire="anthropic" checks the summary's preq.ThinkingEffort==EffortOff
(scriptResponder dials openai only; stubllm logs no `thinking`). Prose rule widened to compact.go:
every comment enumerating which wires/dialects the summary override fires on is rewritten (`grep -n
"two dialects\|three dialects\|other three\|EffortDialectNone because" internal/agent/compact.go`).
The ADR amendment and the manual's `request-extra:` section say sampling knobs there (temperature,
top_k — merged after the codec, not refused) conflict with thinking at a resolved effort.
Owner call 2026-10-02 — thinking wins: when the anthropic body requests `thinking: {type: "adaptive"}`,
`buildBody` omits the model profile's sampling knobs (temperature, top_p, top_k — today copied at
`body.Temperature = s.Temperature` etc.); at effort off/unset the body keeps today's sampling bytes;
the ADR 0078 amendment and docs/manual/configuration.md say profile sampling is dropped on the
anthropic wire while thinking is requested (request-extra: knobs merged after the codec still
conflict — keep that note); add a TestAnthropicCodecEffort-style case asserting no
temperature/top_k/top_p under adaptive and present under disabled.
**Approach (assumed at the header base):** `wire_anthropic.go` widens the thinking request type
(`buildBody`, `anthropicThinking`); `compactCompleter.Complete` adds the anthropic-wire case beside
its kwargs/reasoning `EffortOff` override.
**Files:** internal/provider/wire_anthropic.go; internal/provider/wire_anthropic_test.go; internal/agent/compact.go; internal/agent/compact_test.go; docs/adr/0078-a-servers-wire-is-a-per-entry-codec-inside-the-provider-client.md; docs/manual/configuration.md
**Read first:** internal/provider/wire_anthropic.go — file comment, buildBody, anthropicEffort, anthropicThinking, anthropicThinkingDisabled; internal/agent/compact.go — compactCompleter.Complete, cappedSummaryErrFmt, cappedSummaryNotAskedCause; internal/provider/wire_anthropic_test.go — TestAnthropicCodecEffort, TestAnthropicCodecEncode;
internal/agent/compact_test.go — TestCompactSummarizerAsksForNoReasoning, summaryEffortResponder, assertNoEffort, foldOnce; internal/agent/harness_test.go — scriptResponder; internal/stubllm/wire_anthropic.go — anthropicRequest.logEntry; internal/provider/client.go — Client.encode
**Tests:** `TestAnthropicCodecEffort` recast (adaptive for low..max, disabled for ""/off/none/minimal);
a `TestAnthropicCodecEffort`-style case with a profile carrying temperature/top_p/top_k: no
temperature/top_k/top_p in the body under adaptive, all three present under disabled;
`compact_test.go` `TestCompactSummarizerAsksForNoThinkingOnTheAnthropicWire`: at effort high, a
capturing provider.Responder with cfg.Wire="anthropic" sees the summary's ThinkingEffort==EffortOff.
**Acceptance:**
- `go vet ./internal/provider/ ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/provider/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestCompactSummarizerAsksForNoThinkingOnTheAnthropicWire$|^TestCompactSummarizerAsksForNoReasoning$|^TestCompactSummarizerKeepsTheResolvedEffortOnAnUndialledServer$' ./internal/agent/`
- `grep -n "never requested\|always disabled\|only ever {" internal/provider/wire_anthropic*.go` prints nothing
- `grep -n "adaptive" docs/adr/0078-a-servers-wire-is-a-per-entry-codec-inside-the-provider-client.md` prints a line
**Closes:** apogee-4kl
**Commit:** `feat(provider): request anthropic adaptive thinking when an effort resolves`
