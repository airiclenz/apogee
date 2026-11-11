# Fence escape through same-user spawners, served-PDF policy, odd hex strings, Windows journal evidence

- **Goal:** Auto's write fence stops being silently escapable through same-user process-spawning services: a decision record sets the posture, the backends that can close the reachable sockets close them, and every backend that cannot discloses it as write-class. Also gives served PDFs a policy browsers render under, fixes the odd-digit hex panic, and closes the Windows journal check on its CI evidence.
- **Date:** 2026-11-11
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **base:** 82251cbc
- **Sources:** `.capcom/issues/fence-escape-local-spawner.md`; `.capcom/issues/served-pdf-csp-sandbox.md`; `.capcom/issues/pdf-odd-hex-panic.md`; `.capcom/issues/windows-journal-remove-check.md`; `docs/reviews/code-audit-2026-11-10.md`; ADR 0012, ADR 0019, ADR 0081 (`capcom know show "ADR NNNN" --with-children`); `docs/design/confinement-execution-contract.md` §5, §6.2; kernel landlock docs (https://docs.kernel.org/userspace-api/landlock.html): `LANDLOCK_ACCESS_FS_RESOLVE_UNIX` is ABI 9, UDP net rights ABI 10.
- **Ratified design calls** (owner, 2026-11-11):
  - **Scope:** the four ready issues only; the rest are out of scope (below).
  - **PDF policy:** `application/pdf` gets its own CSP without `sandbox` and without `default-src`/`object-src`; .html/.htm/.svg and every other type keep the bare-sandbox `documentCSP`. The browser check stays a manual follow-up on the narrowed issue, not a gate.
  - **Landlock ABI < 9:** cannot close pathname AF_UNIX; `connect(2) AF_UNIX` becomes write-class and is disclosed at Auto start on every ABI. Rung order (landlock, then bwrap) is unchanged.
  - **Landlock ABI ≥ 9:** handles `RESOLVE_UNIX` and mirrors bwrap's closed set — grants it beneath every entry of `/` and of `/run` (canonical; `/var/run` resolves there) except the user runtime dir(s) and the docker/podman sockets, re-granting the agent-socket allowlist beneath a masked dir; reuses item 8's helper; every other pathname socket stays reachable, so `connect(2) AF_UNIX` stays disclosed at every ABI.
  - **Abstract sockets:** no `LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET` — scoping breaks X11's abstract socket (xclip, xdg-open, headed browser tests); abstract sockets stay reachable on both Linux rungs and are covered by the AF_UNIX disclosure.
  - **bwrap:** `--tmpfs` over the user runtime dir, re-binding only the agent-socket allowlist (ssh-agent, gpg-agent); masks the system docker and podman sockets. It still discloses `connect(2) AF_UNIX` (sockets elsewhere, e.g. tmux under /tmp, stay reachable).
  - **Docker:** masked wherever the backend can close it (bwrap, landlock ≥ 9); docker commands then fail inside Auto's box, and the docs say so.
  - **Seatbelt:** disclose only — residuals `connect(2) AF_UNIX` and `mach-lookup`; profile unchanged.
  - **Localhost TCP:** stays open under ADR 0012; disclosed in prose (manual, SECURITY.md, ADR), not as a residual token.
  - **Env scrub:** none — dropping `DBUS_SESSION_BUS_ADDRESS`/`DOCKER_HOST` is cosmetic, a child names the socket path itself.
- **Standing requirements:**
  - skills: coding-standards
  - This box is a Raspberry Pi: tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run '<TestX>' ./<one package>/`, one run at a time, never a package pattern; pass this rule to every sub-agent.
  - ADR numbering: 0107 is reserved by `docs/plans/2026-11-05 - 00 - v1-placeholder-retraction-plan.md`; this plan's record is ADR 0112.
- **Out of scope:** `serve-http-driver` (needs a grilled ADR first); `user-view-reaction-cell` (a grill); `context-fill-bench-arm` (apogee-sim repo); `lsp-symbol-navigation` (owner-deferred); closing UDP egress at landlock ABI 10 and the Windows token backend's named-pipe/COM posture (filed as issues by item 4); an env scrub; a macOS profile change.
- **Regression check (2026-11-11, 82251cbc):**
  - 1: guard folded (Acceptance greps the evidence line, not `errno 32`, which the 2026-11-09 log already carries)
  - 3: guard folded (restrictive-policy test, configuration.md claim, `capcom know show` Acceptance); supersedes `adr-0019-amend-0812`'s every-document-carries-the-bare-sandbox sentence
  - 4: guard folded, with owner decision (ADR 0112's one closed set and its collateral; amendment Acceptance)
  - 5: guard folded, with owner decision (backend-neutral AF_UNIX clause; per-row headless expectation; comment sites)
  - 6: guard folded, with owner decision (AF_UNIX at every ABI ≥ 1, 9 included; host-ABI test; namespace files; probe `--endpoint`)
  - 7: guard folded (Windows arm; listener-witnessed branches; pipefail Acceptance)
  - 8: guard folded (runtime-dir validation; collateral named in items 4 and 10; sockets under the masked dir)
  - 9: recast (owner, 2026-11-11) — mirror bwrap's closed set, no abstract scoping; replaces the header's ABI ≥ 9 bullet
  - 10: guard folded, with owner decision (scoped prose rule and Acceptance; split "no kernel closes" sentence)
  - 8 (re-check): guard folded, with owner decision (item 8's helper also returns the container-socket list; hermetic row for it)
  - 9 (re-check): guard folded, with owner decision (live witness's listener bound by the unconfined parent; a socket file may be the RESOLVE_UNIX `parent_fd`), plus the re-check report's three guards: (a) the sibling grant recurses down masked paths' ancestors, never granting an ancestor; (b) one pure rule planner the hermetic test drives; (c) planner inputs computed in the parent and carried in the encoded box, never read from the child's scratch `TMPDIR` env

## 1. Record the Windows RemoveJournal CI evidence — ✅ DONE (2026-11-11)

NOTES (2026-11-11): no CHANGELOG entry — the classifier narrowing and the Windows test already have one under [Unreleased] ("Windows: a session journal an ACL refuses to remove is reported…"); this item only records evidence.

**What:**
**Goal:** `windows-journal-remove-check` carries a log line with the CI evidence (run 42825516741, commit a5c4f2db, `errno 32`) and is ready to close.
**Approach (assumed at the header base):** the archived plan `docs/plans/archived/2026-11-09 - 02 - windows-journal-remove-check-plan.md` item 2 named the green-errno-32 branch: `capcom log` the line `RemoveJournal watcher case: os.Remove = … being used by another process. (errno 32)` from job 129861674167 of run 42825516741 (headSha a5c4f2db, which contains d6a88372), with the run URL. The classifier already accepts ERROR_SHARING_VIOLATION only; no code changes.
**Regression guard.** The issue's 2026-11-09 log line already ends "…logs errno 32.", so a grep for `errno 32` passes before any evidence is recorded: Acceptance greps the evidence itself (`RemoveJournal watcher case: os.Remove`, `42825516741`). `gh run view 42825516741 --log` returns empty here; read the job log through `gh api repos/airiclenz/apogee/actions/jobs/129861674167/logs`.
**Closes:** windows-journal-remove-check
**Files:** .capcom/issues/windows-journal-remove-check.md
**Read first:** .capcom/issues/windows-journal-remove-check.md — Log, Acceptance; internal/session/journal_windows_test.go — TestWindowsRemoveJournalUnderAWatcher; internal/session/rename_windows.go — isRemoveSharingError, isRenameSharingError; docs/plans/archived/2026-11-09 - 02 - windows-journal-remove-check-plan.md — item 2
**Tests:** none (records evidence).
**Acceptance:**
- `capcom show windows-journal-remove-check | grep -q 'RemoveJournal watcher case: os.Remove'`; `capcom show windows-journal-remove-check | grep -q 42825516741`
- `capcom check`
**Commit:** `chore(issues): record the Windows RemoveJournal CI evidence`

## 2. Read a closed odd-digit hex string with a final zero — ✅ DONE (2026-11-11)

NOTES (2026-11-11): consequential edit — internal/doctext/pdfparse/doc.go: made necessary by the readHexString patch in lex.go (the package comment's "Local patches" list names every change to the copied files); added an "Odd-digit hex string" entry, the "End of input" bullet unchanged
NOTES (2026-11-11): the error case is its own test, TestBufferReadObject_NonHexDigit_ReportsMalformedHexString, beside the table test TestBufferReadObject_OddDigitHexString_ReadsFinalZero; the table test was confirmed to fail against lex.go at HEAD

**What:**
**Goal:** a closed hex string with an odd number of digits reads as if a final `0` followed its last digit (`<414>` → `"A@"`, `<4>` → `"@"`), without a panic; a non-hex digit still reports a malformed hex string.
**Approach (assumed at the header base):** fixes the pre-existing defect `pdf-odd-hex-panic` in `buffer.readHexString` (`internal/doctext/pdfparse/lex.go`): when the second read of a pair is `>`, append `unhex(c)<<4` (still erroring if `unhex(c) < 0`) and end the string. Extends the existing function; no new helper.
**Closes:** pdf-odd-hex-panic
**Files:** internal/doctext/pdfparse/lex.go; internal/doctext/pdfparse/eof_test.go
**Read first:** internal/doctext/pdfparse/lex.go — buffer.readHexString, unhex, buffer.readToken; internal/doctext/pdfparse/eof_test.go — TestBufferReadObject_EndOfInput_ClosesOpenSyntax, readWithin, programBuffer; internal/doctext/pdfparse/doc.go — package comment "End of input" bullet (EOF lone-digit drop, `<414` → "A", stays)
**Tests:** a table test beside `TestBufferReadObject_EndOfInput_ClosesOpenSyntax`: `<414>`, `<4>`, `<41 4>`, `<4 1>` read correctly; `<4G>` still errors; the test fails at the header base (panic).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestBuffer' ./internal/doctext/pdfparse/`
**Commit:** `fix(pdfparse): read a closed odd-digit hex string with a final zero`

## 3. Serve PDFs under a policy without the bare sandbox

**What:**
**Goal:** rung 2 serves `application/pdf` with `Content-Security-Policy: frame-ancestors 'none'; form-action 'none'; base-uri 'none'` (no `sandbox`, no `default-src`); every other type, .html/.htm/.svg included, keeps `documentCSP` unchanged; ADR 0019 carries a dated amendment saying why; the issue is narrowed to the manual browser check.
**Approach (assumed at the header base):** in `internal/present/server.go`, a `pdfCSP` constant beside `documentCSP` and one pure function choosing the policy from the content type `contentType(granted.name)` already returned; the handler sets that. Add `adr-0019-amend-1111` (`capcom know add --id adr-0019-amend-1111 --parent adr-0019`): Chromium's history of blank PDFs under a CSP `sandbox` (crbug 413851, 1187122), a PDF cannot carry the meta refresh `sandbox` stops, `nosniff` pins the type, not yet confirmed in a browser. Name it on ADR 0019's Status line and reading-order list. Rewrite the body of `served-pdf-csp-sandbox` so it asks only for the browser confirmation (Chrome, Firefox, Safari) and names this commit. Prose rule: every comment stating that every served document carries `documentCSP` (grep `documentCSP` in `internal/`) is corrected.
**Regression guard.** `TestDocServerServesEveryDocumentUnderARestrictivePolicy` (server_test.go:189) asserts the bare sandbox for `report.pdf`: drop `report.pdf` from its map (add `report.htm`), rewrite its "policy is a property of the server rather than of the extension" comment (192-193), and pin the exact PDF policy in its own test. The prose rule covers every claim that every served document carries one policy (grep `documentCSP\|default-src\|same headers` over `internal/ cmd/ docs/manual/ SECURITY.md`), including docs/manual/configuration.md:3250-3251. The amendment supersedes `adr-0019-amend-0812`'s sentence that every document the doc server answers carries the bare-sandbox CSP.
**Files:** internal/present/server.go; internal/present/server_test.go; .capcom/knowledge/decisions/adr-0019.md; .capcom/knowledge/decisions/adr-0019/adr-0019-amend-1111.md; .capcom/issues/served-pdf-csp-sandbox.md; internal/tui/presenter.go; docs/manual/configuration.md
**Read first:** internal/present/server.go — documentCSP, DocServer.handle, contentType; internal/present/server_test.go — TestDocServerServesEveryDocumentUnderARestrictivePolicy, cspDirectives, TestDocServerServesAGrantedDocument; docs/manual/configuration.md — present section (served-policy sentence);
  .capcom/knowledge/decisions/adr-0019/adr-0019-amend-0812.md; .capcom/issues/served-pdf-csp-sandbox.md — title, Acceptance
**Tests:** extend the served-document test in `server_test.go`: a `.pdf` response carries exactly the PDF policy (no `sandbox`, no `default-src`); `.html`, `.htm`, `.svg` keep the bare sandbox and `default-src 'none'`; `TestDocServerServesEveryDocumentUnderARestrictivePolicy` loses `report.pdf`, gains `report.htm`, and its comment follows.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestDocServer' ./internal/present/`
- `capcom know show "ADR 0019" | grep -q amend-1111`; `capcom check`
**Commit:** `fix(present): serve PDFs under a policy without the bare sandbox`

## 4. Decide the fence's posture on same-user spawning services (ADR 0112)

**What:**
**Goal:** ADR 0112 exists and states, per backend (landlock by ABI, bwrap, seatbelt, Windows token), which same-user spawning services Auto's fence closes and which it discloses; ADR 0081's 2026-09-22 AF_UNIX note carries a dated amendment reclassing it write-class; two follow-up issues exist.
**Approach (assumed at the header base):** `capcom know add --parent decisions` an `adr-0112` record (Context, Decision, Consequences as child Notes in the house shape — copy the layout of the newest record). The Decision is the header's ratified calls, verbatim in substance: the escape (systemd-run --user over the user bus, docker run -v /:/host, launchctl/osascript), the agent-socket allowlist, docker masked, tmux-style sockets outside the runtime dir still reachable under bwrap, localhost TCP open under ADR 0012 and disclosed in prose. Add `adr-0081-amend-1111` and name it on ADR 0081's Status line and reading-order list. Add `adr-0112` to `.capcom/knowledge/decisions.md`. File `landlock-udp-abi10` (UDP egress closable at landlock ABI 10; the "no kernel closes" claims) and `windows-token-spawner-posture` (whether a low-integrity token reaches same-user named pipes or COM servers that spawn) with `capcom create --id`.
**Regression guard.** ADR 0112 states the one closed set both Linux rungs share (user runtime dir(s), docker/podman sockets; landlock only at ABI ≥ 9), what stays reachable and disclosed (other pathname sockets such as tmux under /tmp, postgres, the system bus; abstract sockets incl. X11, and why no abstract scoping), seatbelt disclose-only, localhost TCP open per ADR 0012. Beside docker it names the other collateral of masking the runtime dir (item 8's finding): the session bus and IPC sockets that `gh` with a keyring-stored token, git-credential-libsecret and VS Code's git askpass (`$VSCODE_GIT_IPC_HANDLE`) need, so `git push`/`gh pr` fail in Auto's box; the behaviour stays as ratified. Acceptance checks the ADR 0081 amendment exists and is named on the record.
**Files:** .capcom/knowledge/decisions/adr-0112.md; .capcom/knowledge/decisions/adr-0112/; .capcom/knowledge/decisions/adr-0081.md; .capcom/knowledge/decisions/adr-0081/adr-0081-amend-1111.md; .capcom/knowledge/decisions.md; .capcom/issues/landlock-udp-abi10.md; .capcom/issues/windows-token-spawner-posture.md
**Read first:** .capcom/knowledge/decisions/adr-0081.md — Status line, reading-order sentence; .capcom/knowledge/decisions/adr-0081/adr-0081-decision.md — "Amended 2026-09-22" blockquote (not write-class, no startup banner); .capcom/knowledge/decisions/adr-0081/adr-0081-amend-1103.md — amendment shape;
  .capcom/knowledge/decisions/adr-0111.md — newest record layout (context, decision, rejected, consequences children); .capcom/knowledge/decisions.md — record list; .capcom/issues/fence-escape-local-spawner.md — Acceptance
**Tests:** none (decision record).
**Acceptance:**
- `capcom know show "ADR 0112" --with-children | grep -q 'docker'`
- `capcom know show adr-0081-amend-1111 && grep -q 'adr-0081-amend-1111' .capcom/knowledge/decisions/adr-0081.md`
- `capcom show landlock-udp-abi10 && capcom show windows-token-spawner-posture`; `capcom check`
**Commit:** `docs(adr): record ADR 0112, Auto's fence and same-user spawning services`

## 5. Class `connect(2) AF_UNIX` as write-class and word it at Auto start

**What:**
**Goal:** `ResidualNotice` speaks for `connect(2) AF_UNIX` with a specific clause naming the spawn-elsewhere escape (worded to hold on every backend that discloses the token — see the regression guard); `connect(2) UDP` alone stays silent; a new domain token `mach-lookup` exists, is write-class, and has its own clause (launchd/Mach services such as `launchctl submit`, `osascript`).
**Approach (assumed at the header base):** depends on item 4. In `internal/domain/confinement.go` add `ResidualMachLookup = "mach-lookup"` beside `ResidualUDPEgress`/`ResidualUnixEgress` and update `ResidualUnixEgress`'s comment. In `internal/probe/confinement.go`, `writeClassResiduals` drops only `ResidualUDPEgress`; `residualConsequence` gains arms for the AF_UNIX and mach-lookup tokens (referencing the domain constants); `ResidualNotice`'s doc comment follows. The notice's first line keeps its wording. Prose rule: every comment in `internal/` calling AF_UNIX "network-egress-class" or "not write-class" (grep `ResidualUnixEgress`, `AF_UNIX`) is corrected here — except those in `internal/platform/`, which item 6 owns.
**Regression guard.** The AF_UNIX clause must hold on every backend that discloses the token — word it as "a same-user service listening on a unix socket the box can still reach (such as tmux, or the user bus's systemd-run and docker where this backend does not mask them) can run commands outside the fence" or equivalent; no backend-specific claim. `TestHeadlessAutoDisclosesTheFenceResidual` hard-codes `truncate(2)` for every `want` row (headless_test.go:530): give each row its own expected clause (e.g. a `wantClause` field) and drop that check. The prose rule also covers every comment describing `ResidualNotice`'s cell or `residualConsequence`'s arms (grep `ResidualNotice`, `residualTruncate` in `internal/` and `cmd/apogee/`): `residualTruncate`'s "the one residual token" comment and the "landlock ABI 1–2 and truncate(2)" cell in headless.go, daemon.go, wire_boot.go and daemon_test.go.
**Files:** internal/domain/confinement.go; internal/domain/confinement_test.go; internal/probe/confinement.go; internal/probe/confinement_test.go; cmd/apogee/headless_test.go; cmd/apogee/headless.go; cmd/apogee/daemon.go; cmd/apogee/wire_boot.go; cmd/apogee/daemon_test.go
**Read first:** internal/probe/confinement.go — ResidualNotice, writeClassResiduals, residualConsequence, residualTruncate; internal/domain/confinement.go — ResidualUnixEgress; internal/probe/confinement_test.go — TestResidualNoticeIsSilentForNetworkOnlyResiduals, TestResidualNoticeWordsEachTokenOnItsOwnTerms;
  cmd/apogee/headless_test.go — TestHeadlessAutoDisclosesTheFenceResidual; cmd/apogee/wire_boot.go — rootWiring.announceConfinement
**Tests:** flip `TestResidualNoticeIsSilentForNetworkOnlyResiduals` to UDP-only silence and AF_UNIX speaking; `TestResidualNoticeWordsEachTokenOnItsOwnTerms` pins the exact AF_UNIX and mach-lookup clauses; `TestResidualNotice` matrix and its `holdsWriteClassToken` helper follow; `TestHeadlessAutoDisclosesTheFenceResidual` gains an AF_UNIX case asserting the exact emitted clause, each row carrying its own expected clause in place of the shared `truncate(2)` check.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/probe/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestHeadlessAutoDisclosesTheFenceResidual' ./cmd/apogee/`
**Commit:** `fix(probe): disclose connect(2) AF_UNIX as a write-class residual at Auto start`

## 6. Disclose AF_UNIX on every landlock ABI and AF_UNIX plus mach-lookup on seatbelt

**What:**
**Goal:** landlock's `Capabilities` lists `connect(2) AF_UNIX` at every available ABI ≥ 1, 9 included (beside `truncate(2)` at 1–2 and `connect(2) UDP` at ≥ 4); seatbelt's present-backend caps list `connect(2) AF_UNIX` and `mach-lookup`; `apogee probe`'s `unfenced:` field shows them.
**Approach (assumed at the header base):** depends on item 5. `landlockConfiner.Capabilities` (`internal/platform/landlock_linux.go`) appends `domain.ResidualUnixEgress` at every ABI ≥ 1; `seatbeltConfiner.Capabilities` (`internal/platform/seatbelt.go`) returns both tokens. Prose rule: every comment and test message in `internal/platform/` and `.github/workflows/ci.yml` calling AF_UNIX egress-class or "not write-class" is corrected.
**Regression guard.** AF_UNIX is disclosed at every landlock ABI ≥ 1, including ≥ 9 (item 9 does not drop it). `TestLandlockResidualsMatchHostABI` pins AF_UNIX only at ABI ≥ 4 and goes red on ABI 1–3 hosts: it expects AF_UNIX at every ABI ≥ 1, in the order `Capabilities` appends it. The prose rule's sites include namespace_linux.go (the "it is not write-class, so probe.ResidualNotice filters it out" comment) and namespace_linux_test.go (grep `write-class\|AF_UNIX` over `internal/platform/`). `apogee probe` refuses with "no servers are configured" on an unconfigured host: Acceptance passes `--endpoint http://127.0.0.1:9`, as ci.yml does.
**Files:** internal/platform/landlock_linux.go; internal/platform/landlock_linux_test.go; internal/platform/seatbelt.go; internal/platform/seatbelt_test.go; internal/platform/confiner_linux_test.go; .github/workflows/ci.yml; internal/platform/namespace_linux.go; internal/platform/namespace_linux_test.go
**Read first:** internal/platform/landlock_linux.go — landlockConfiner.Capabilities; internal/platform/landlock_linux_test.go — TestLandlockCapabilitiesHonest, TestLandlockResidualsMatchHostABI; internal/platform/seatbelt.go — seatbeltConfiner.Capabilities;
  internal/platform/namespace_linux.go — namespaceConfiner.Capabilities; .github/workflows/ci.yml — "probe discloses no write-class residual" step; internal/probe/confinement.go — CapabilityLine, writeClassResiduals
**Tests:** `TestLandlockCapabilitiesHonest` rows abi 1, 2, 3, 4, 6 expect AF_UNIX; `TestLandlockResidualsMatchHostABI` expects AF_UNIX at every ABI ≥ 1; `TestSeatbeltCapabilitiesHonest` expects both tokens; `TestSelectLinuxConfiner`'s `wantLine` strings follow.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestLandlockCapabilitiesHonest|TestLandlockResidualsMatchHostABI|TestSeatbelt|TestSelectLinuxConfiner' ./internal/platform/`
- `go run ./cmd/apogee probe --endpoint http://127.0.0.1:9 | grep 'unfenced:.*connect(2) AF_UNIX'`
**Commit:** `fix(platform): disclose AF_UNIX on every landlock ABI and AF_UNIX and mach-lookup on seatbelt`

## 7. confinetest row: a confined connect to a unix socket outside the box

**What:**
**Goal:** `confinetest.Probe` runs a row that listens on a pathname unix socket outside the box and connects to it from inside: the connect must fail unless the backend discloses `connect(2) AF_UNIX`, in which case the row logs the residual; the contract's §6.2 table lists it as row #15 (subtest `unix_connect_outside_box`).
**Approach (assumed at the header base):** depends on item 6. Follows the disclosure-keyed pattern of rows #12/#13 (`discloses`). The in-box client is the test binary re-run as a helper process (an exported confinetest helper entry, called from one `TestConfinetestHelper` in `internal/platform`), never a host tool (socat, nc, python).
**Regression guard.** `Probe` also runs on Windows CI (`TestWindowsTokenProbe`, ci.yml's Windows `internal/platform` step), where the token backend discloses no AF_UNIX: the row skips on the token backend citing `windows-token-spawner-posture`, and §6.2/§9.3 carry the row's Windows column. Both branches are keyed on the parent's listener, as rows #12/#13 are: disclosed ⇒ an accept is observed, silent ⇒ no accept within a wait; the helper argv is built explicitly (`-test.run` only), never from `os.Args`. The socket path stays short (macOS `sun_path` is 104 bytes) — not a subtest `t.TempDir()`. Acceptance uses `set -o pipefail` and greps the row's `--- PASS:` line (the ci.yml landlock-net pattern).
**Files:** internal/platform/confinetest/confinetest.go; internal/platform/confinetest_helper_test.go; docs/design/confinement-execution-contract.md
**Read first:** internal/platform/confinetest/confinetest.go — Probe, probeNetwork (udp_egress_under_network_deny), discloses, acceptAll, unrunnable; internal/platform/landlock_linux_test.go — TestMain, runConfinedExecChild, TestLandlockProbe;
  internal/platform/confiner_windows_test.go — windows Probe caller; docs/design/confinement-execution-contract.md — §6.2 table, §9.3
**Tests:** the row itself, run through the existing `TestLandlockProbe` / `TestNamespaceProbe`; on Windows it skips under `TestWindowsTokenProbe`.
**Acceptance:**
- `set -o pipefail; GOMEMLIMIT=2GiB go test -race -count=1 -v -run 'TestLandlockProbe$' ./internal/platform/ | tee /dev/stderr | grep -q -- '--- PASS: TestLandlockProbe/unix_connect_outside_box'`
- `go vet ./internal/platform/...`
**Commit:** `test(confinetest): probe a confined connect to a unix socket outside the box`

## 8. bwrap: mask the user runtime dir and the system container sockets

**What:**
**Goal:** under the bwrap rung a confined command cannot connect to a socket under the user runtime dir (`$XDG_RUNTIME_DIR`, `/run/user/<uid>`) or to `/run/docker.sock` / `/run/podman/podman.sock`, while `$SSH_AUTH_SOCK` and the gpg-agent sockets stay reachable; bwrap still discloses `connect(2) AF_UNIX`.
**Approach (assumed at the header base):** depends on item 7. One pure helper in a new `internal/platform/unixsockets_linux.go` computes the agent-socket allowlist (`$SSH_AUTH_SOCK` when absolute and existing; `<runtime dir>/gnupg`; `$GNUPGHOME` or `~/.gnupg`) and the runtime dirs to mask; item 9 reuses it. `namespaceArgv` stays pure (inputs injected like `exists`) and emits, after the fixed prefix and before the root binds: `--tmpfs <runtime dir>` per existing canonical runtime dir, `--ro-bind /dev/null <sock>` per existing canonical container socket, then `--ro-bind <p> <p>` for each allowlist path beneath a masked dir (after the root binds, so a root under the runtime dir survives too).
**Regression guard.** A runtime dir is masked only when it is owned by the uid, mode 0700 (XDG spec), and is not `/`, `os.TempDir()`, `$HOME` or an ancestor of a box root — `XDG_RUNTIME_DIR=/tmp` must not `--tmpfs /tmp`. Masking also cuts the session bus and IPC sockets `gh` (keyring token), git-credential-libsecret and VS Code's git askpass use; items 4 and 10 name that beside docker, and the behaviour stays as ratified. The live test creates both sockets under the masked runtime dir (`t.Setenv` `XDG_RUNTIME_DIR` to a 0700 temp dir and `SSH_AUTH_SOCK` to a socket in it; the helper reads them at Confine time), with item 7's listener as the witness; new tests are named `TestUnixSocket*`/`TestNamespace*`. The helper in internal/platform/unixsockets_linux.go also returns the canonical, existing container sockets to mask (`/run/docker.sock`, `/run/podman/podman.sock`), so bwrap (item 8) and landlock ≥ 9 (item 9) read one list from one place.
**Files:** internal/platform/unixsockets_linux.go; internal/platform/unixsockets_linux_test.go; internal/platform/namespace_linux.go; internal/platform/namespace_linux_test.go
**Read first:** internal/platform/namespace_linux.go — namespaceArgv, probeNamespace, namespaceConfiner.Confine, pathExists, canonicalWritableRoot; internal/platform/namespace_linux_test.go — TestNamespaceArgv, namespaceBaseFlags, fencingNamespaceConfiner
**Tests:** `TestNamespaceArgv` rows for the mask, the re-bind and the missing-path skip (`namespaceBaseFlags` follows); a hermetic allowlist table test, with rows refusing to mask a runtime dir that is not uid-owned 0700 or is `/`, `os.TempDir()`, `$HOME` or an ancestor of a box root, and rows returning `/run/docker.sock` / `/run/podman/podman.sock` as container sockets only when they exist; a live bwrap test (skips without bwrap) asserting a socket under the masked `$XDG_RUNTIME_DIR` is unreachable and one at `$SSH_AUTH_SOCK`'s path beneath it is reachable, via item 7's helper.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestNamespace|TestUnixSocket' ./internal/platform/`
**Commit:** `fix(platform): mask the user runtime dir and container sockets under bwrap`

## 9. landlock: close the user runtime dir and container sockets with RESOLVE_UNIX at ABI ≥ 9

**What:** Recast at the regression check (2026-11-11).
**Goal:** at ABI ≥ 9 a confined connect to a socket under the user runtime dir or to the docker/podman sockets fails, while sockets elsewhere (e.g. /var/run/postgresql, sssd, the system bus, /tmp) and the allowlisted agent sockets stay reachable; Capabilities keeps `connect(2) AF_UNIX`; no abstract scoping. ABI < 9 is unchanged from item 6.
**Approach (assumed at the header base):** depends on item 8. Add `landlockABIResolveUnix = 9` beside `landlockABINetwork`; `accessMaskForABI` handles `unix.LANDLOCK_ACCESS_FS_RESOLVE_UNIX` from ABI 9; it is granted beneath every entry of `/` and of `/run` except the masked runtime dir(s) and container sockets, and re-granted for the agent-socket allowlist beneath a masked dir — both sets from item 8's helper. `attr.Scoped` stays unset.
**Regression guard.** **Landlock ABI ≥ 9:** handles `RESOLVE_UNIX` and mirrors bwrap's closed set — grants it beneath every entry of `/` and of `/run` (canonical; `/var/run` resolves there) except the user runtime dir(s) and the docker/podman sockets, re-granting the agent-socket allowlist beneath a masked dir; reuses item 8's helper; every other pathname socket stays reachable, so `connect(2) AF_UNIX` stays disclosed at every ABI. **Abstract sockets:** no `LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET` — scoping breaks X11's abstract socket (xclip, xdg-open, headed browser tests); abstract sockets stay reachable on both Linux rungs and are covered by the AF_UNIX disclosure. Allowlist and sibling rules carry exactly `LANDLOCK_ACCESS_FS_RESOLVE_UNIX` via their own add-rule (never the write-class mask of `allowWriteBeneath`); state and hermetically test whether a socket file or its parent dir is the rule's `parent_fd`. This replaces the header's former "Landlock ABI ≥ 9" bullet (granted only beneath roots, scratch and allowlist; abstract scoping at ABI ≥ 6). Any live witness's listener is bound by the unconfined parent, never inside the confined domain (landlock exempts servers created in the same domain); a socket file may be a RESOLVE_UNIX rule's parent_fd (RESOLVE_UNIX is a file-applicable right per the kernel docs). (a) Read the "every entry of `/` and of `/run`" grant as a rule, since a rule inherits beneath its `parent_fd`: the sibling grant recurses down every masked path's ancestors, and at each level (`/`, `/run`, `/run/user`, or `/tmp` for a runtime dir there) it grants beneath every entry except the one on the way to a masked path. So `/run/user/<other-uid>` and `/run/podman` (except `podman.sock`'s mask) are granted, while the runtime dir and the container sockets are not, and no ancestor dir itself ever gets a recursive grant. (b) The rule set comes from one pure planner function returning `[]{path, mask, isFile}` from the roots, the scratch dir, the allowlist, the masked paths and an injected dir-lister (as `namespaceArgv` injects `exists`). The hermetic test drives it, and `applyLandlock`'s syscalls only apply its output. (c) The planner's inputs (runtime dir, allowlist, masks) come from item 8's helper, computed in the parent before the landlock child execs and carried in the payload `encodeBox` serialises. They are never re-read from the child's env, whose `TMPDIR` is the scratch dir (`subprocess.ScratchEnv`).
**Files:** internal/platform/landlock_linux.go; internal/platform/landlock_linux_test.go; internal/domain/confinement.go (when the planner inputs ride on `ConfinementBox`)
**Read first:** internal/platform/landlock_linux.go — applyLandlock, allowWriteBeneath, accessMaskForABI, encodeBox, landlockConfiner.Capabilities; internal/platform/landlock_linux_test.go — TestAccessMaskForABI, TestLandlockResidualsMatchHostABI;
  golang.org/x/sys@v0.49.0/unix zerrors_linux.go — LANDLOCK_ACCESS_FS_RESOLVE_UNIX
**Tests:** `TestAccessMaskForABI`'s `abi99_future` row gains RESOLVE_UNIX; `TestAccessMaskForABIRightsTrackTheKernel`'s loop extends to ABI 9+ and covers RESOLVE_UNIX from 9; `TestLandlockCapabilitiesHonest` gains an abi 9 row that keeps AF_UNIX (no ABI-6 scope row); `TestLandlockResidualsMatchHostABI` keeps AF_UNIX at ≥ 9; a hermetic `TestLandlockResolveUnixPlan` table test driving the planner with a fake dir-lister: the allowlist and sibling rules carry exactly RESOLVE_UNIX on the stated `parent_fd` (socket file or parent dir), and the runtime dir(s), the container sockets and their ancestor dirs get no rule. It has rows for `/run/user/<uid>` beside a granted `/run/user/<other-uid>`, for `/run/podman/podman.sock` beside a granted `/run/podman` sibling, and for a runtime dir under `/tmp`. A round-trip test checks that the planner inputs computed in the parent survive `encodeBox`/`DecodeConfinedBox`. This box's kernel is ABI < 9: the live path runs where CI's kernel allows, through item 7's row, its listener bound by the unconfined test process.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestLandlock|TestAccessMask' ./internal/platform/`
**Commit:** `feat(platform): close the user runtime dir and container sockets under landlock where the kernel can`

## 10. Disclose the spawn-elsewhere residual in the manual and SECURITY.md

**What:**
**Goal:** `docs/manual/configuration.md` §Auto mode's blast radius, `docs/manual/probe.md`'s `backend:` paragraph, `SECURITY.md` and the contract's §5 state per backend what items 6–9 close and disclose, that docker fails inside Auto's box where masked, that localhost TCP services stay reachable (ADR 0012), and cite ADR 0112; no doc says no kernel closes AF_UNIX.
**Approach (assumed at the header base):** depends on item 9. Prose rule: every sentence in `docs/` and `SECURITY.md` claiming AF_UNIX is network-only, not write-class, or closed by no kernel (grep `AF_UNIX`, `No kernel closes`, `no kernel closes`) is corrected; UDP's claim is item 4's follow-up issue, left as is. Public docs use the shipped vocabulary (`auto`, `reactions:`).
**Regression guard.** The docs say both Linux rungs close the same set (landlock only at ABI ≥ 9) and that X11/abstract sockets stay reachable. They name, beside docker, the session-bus/IPC collateral item 8 found (`gh` keyring token, git-credential-libsecret, VS Code's git askpass). The prose rule is limited to docs/manual/, docs/design/ (excluding docs/design/archived/) and SECURITY.md — archived plans are point-in-time records (AGENTS.md). probe.md's `backend:` paragraph and configuration.md's network-deny paragraph bundle UDP and AF_UNIX in one "no kernel closes" sentence: split it, keeping "no kernel closes" for UDP only. Acceptance greps the four Files for the phrases they actually carry, never `docs/` (this plan matches) and never a phrase that wraps across lines.
**Closes:** fence-escape-local-spawner
**Files:** docs/manual/configuration.md; docs/manual/probe.md; SECURITY.md; docs/design/confinement-execution-contract.md
**Read first:** docs/manual/configuration.md — §Auto mode's blast radius (truncation and network-deny paragraphs); docs/manual/probe.md — `backend:` paragraph; SECURITY.md — What counts, Auto-mode confinement bullet;
  docs/design/confinement-execution-contract.md — §5 Capability honesty (landlock/bwrap residual bullets), §6.2 row #13 note
**Tests:** none (docs).
**Acceptance:**
- `grep -n 'ADR 0112\|adr-0112' docs/manual/configuration.md SECURITY.md`
- `grep -n 'ADR 0112' docs/manual/probe.md docs/design/confinement-execution-contract.md`
- `! grep -n 'stay off the startup notice' docs/manual/configuration.md`
- `! grep -n 'AF_UNIX. — egress no kernel closes' docs/manual/probe.md`
**Commit:** `docs(security): disclose same-user spawning services as Auto fence residuals`
