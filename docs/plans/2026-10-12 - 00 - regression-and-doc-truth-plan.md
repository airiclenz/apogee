# Plan: the missing-mount regression and doc truth before the Knowledge migration

**Goal:** fix the announced-path regression where a missing library or scratch root is refused as a
workspace-fence escape, then make `CONTEXT.md`, the design docs and the ADRs tier 3 will cut into
capcom Notes agree with the code and with each other.
**Date:** 2026-10-12
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** a4863313

**Sources:**
- `docs/handoffs/2026-10-12 - 01 - refocus-findings-and-capcom-knowledge-migration.md` (tiers 1a–1c; local only)
- `capcom show missing-mount-misreported`
- `AGENTS.md` (regressions are never deferred: an announced path a tool refuses)
- `docs/adr/0006-*`, `0012-*`, `0062-*`, `0076-*`, `0089-*`, `0094-*`, `0097-*`

**Ratified design calls** (owner, 2026-10-12):
- **Auto gate:** the code is right. Any non-nil Confiner admits Auto; without `FSWrite` each subprocess call gates through Approval (ADR 0012); `AutoEligible()` refuses only unattended Auto. Docs follow the code.
- **Invariant wording:** keep "bare loop" in `README.md` and `AGENTS.md`; `CONTEXT.md` defines it as Bypass mode (Floor guards and structural reducers on, ADR 0006); the Floor guards are part of that baseline, not held to it.
- **Missing-root answer:** each tool's existing not-found message (`file not found: <path>`, `directory not found: <path>`, `path not found: <path>`); no new wording.
- **ADR edits:** dated `> **Amended 2026-10-12 (…).**` blocks under the paragraph they correct, in the ADRs' existing style, for ADRs 0076, 0079, 0089, 0094 and 0097. ADR 0012 has its own style, `## Amendment (<date>) — <title>` H2 sections or dated `> **Note …**` blocks, so its edit is a dated `> **Note 2026-10-12 …**` block under the stale line. Decision text is never rewritten in place.

**Standing requirements:**
- skills: coding-standards
- Pi 5 / 8 GB: never `go test -race` or `-cover` over a whole heavy package or a pattern; narrow with `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./pkg/`; never two test runs at once. Pass this to every sub-agent.
- Commit straight to `main`; no AI attribution anywhere.

**Out of scope:**
- Tier 6 drift (`reaction-core-greenfield.md`, retired-Mechanism mentions, manual, Makefile, IDEAS.md).
- The capcom Knowledge migration itself (tier 3); `publish-unsummed-archives` (tier 2).
- Stopping the orientation from announcing a root that does not exist yet (creation-deferred stays).
- ADR 0096 dangerous-rules: verified wired (`security.MergeDangerousRules`), nothing to do.

**Regression check (2026-10-12, a4863313):**
- 1: recast. Each of the six tools maps the missing-root error to its own not-found message at its own resolve/locate call site. Files and Tests now cover all six tools.
- 2: guard folded; yields to ADR 0031 invariant 1. The Driver entry names the webhook beside the Upstream dial and the network tools. The package-path rule covers live references only. Acceptance greps the edited lines.
- 3: guard folded. The Auto-gate and per-project comment rules each run off a grep (`apogee.go`, `cmd/apogee/wire_engine.go` and `internal/domain/config.go` added). The ADR 0012 edit takes that ADR's `> **Note**` style. Acceptance checks the contract §5, ADR 0012 and MCP-list clauses.
- 4: guard folded; yields to ADR 0071 Decision 1 clause (c). Every "bare loop" invariant use is settled. Acceptance checks the AGENTS/README keep and the ADR 0079 citation.
- 5: guard folded. ADR 0097 is amended wherever `detect.go`/`wire_update.go` contradicts it. Acceptance checks the 0089 gate and the 0097 Consequences.
- 1 (re-check): guard folded. The missing-root check matches the announced Scratch() spelling as well as its real path, with a dotfiles-link case. The comment grep catches wrapped comments. The new per-tool tests are named so Acceptance's `-run` selects them.

## 1. A missing mount root answers not-found, not the workspace fence — ✅ DONE (2026-10-12)

**What:** Recast at the regression check (2026-10-12). Regression (announced-path class, AGENTS.md) since `bbb99763`: orientation line 4 announces
every read mount root, missing ones included (`skills.readRoots` keeps a missing dir), and the
scratch dir. A read tool given a path under such a root that does not exist yet answers
`security: path resolves outside the workspace root`, and the model reads that as a fence.

**Regression guard.** The premise that each tool's not-found branch fires unchanged off a resolve error is false for four of six tools; keep item 1 as one item, recast its Approach so the new not-found error is mapped to each tool's existing not-found message at each tool's own resolve/locate call site (read_file, list_dir, find_files, grep, present_document, copy_file source) using the existing builders (notFoundMessage / directoryNotFoundMessage / notFoundOrRefusal), list every touched tool source and test file on **Files:**, and extend Tests so each of the six tools is driven at least once with a path under a missing configured root; the ratified wording call (each tool's existing not-found message, no new wording) stands.
- Re-check: `extraRoots` folds `security.EvalRealPath(Scratch())`, so the missing-root check also compares the cleaned input against `Scratch()`'s announced spelling. A missing scratch under a dotfiles-linked home must answer not-found, not `ErrPathEscape`.
- Re-check: the comment grep is line-bound. It must also catch wrapped comments: `present_document.go` "a path no root / accepts" and `path_read.go` `readRoot` "When NO / root accepts".
- Re-check: each new per-tool test carries `MissingMountRoot` in its name, and Acceptance's `-run` pattern includes it.

**Goal:** for an absolute path lexically inside a configured read mount root (`ReadMounts.Roots()`
or `Scratch()`) whose root does not exist, `read_file`, `list_dir`, `find_files`, `grep`,
`present_document` and `copy_file`'s source each answer their own existing not-found message naming
the path. A path outside the workspace and every configured root still gets `ErrPathEscape`. No path
outside the configured roots gains an existence probe.

**Approach (recast at the regression check, base a4863313):** in `internal/tools/path_read.go`, when
`readScope.resolve` (or `locate`) fails the workspace and `matchRoot`, check whether the cleaned input
sits under a configured root that `matchRoot` skipped because the root itself is missing (`os.Lstat`
→ `fs.ErrNotExist`, not merely `!rootUsable`). For `Scratch()`, compare against both its announced
spelling and its `EvalRealPath`, because `extraRoots` folds in only the real path. If so, return an error that wraps `fs.ErrNotExist` only
(e.g. `fmt.Errorf("%w: %s", fs.ErrNotExist, root)`). It must never wrap `ErrPathEscape` or
`security.ErrRootInaccessible`, because `escapeOrMessage` tests `ErrRootInaccessible` first. Four call
sites print a resolve error raw or fall back to the workspace. At each one, map
`errors.Is(err, fs.ErrNotExist)` to that tool's existing not-found message:
- `ListDir.Execute` (`list_dir.go`, `directory not found: `)
- `PresentDocument.Execute` (`present_document.go`, `file not found: `)
- `readScope.searchTarget` (`path_read.go`, `path not found: `, for both find_files and grep)
- `CopyFile.Execute` (`file_ops.go`, `file not found: `). It must not fall back to `(t.root, source)` on that error.

`read_file` reaches the error through `readBounded`. Its workspace re-read must not turn the error back
into a fence refusal. Build every message with the existing builders (`notFoundMessage`,
`directoryNotFoundMessage`, `notFoundOrRefusal`). The missing-root answer carries no near-miss
suggestions: no `suggestSiblings` probe runs outside the configured roots. Some comments say a path
under no root, or under an unusable root, gets the workspace escape refusal, or that a missing root is
skipped "never an error of its own". Amend each of those comments for the missing-root case. That
covers `internal/domain/readmounts.go` and the hits of `grep -rn -i 'no root accepts\|under no root\|unusable root\|not exist yet is skipped\|never an error of its own' internal/tools internal/domain`.
It also covers the wrapped comments that grep misses: the hits of `grep -rn -i -A1 'no root$\|when no$\|path no root\|uniform escape refusal' internal/tools internal/domain`.
`escapeOrMessage` still never turns `ErrPathEscape` into not-found. The write side (`writeScopeOf`) is
not touched.

**Files:** internal/tools/path_read.go, internal/tools/list_dir.go, internal/tools/present_document.go, internal/tools/file_ops.go, internal/tools/read_file.go (comment), internal/domain/readmounts.go (comment), internal/tools/path_read_test.go, internal/tools/read_file_test.go, internal/tools/list_dir_test.go, internal/tools/find_files_test.go, internal/tools/grep_test.go, internal/tools/present_document_test.go, internal/tools/file_ops_test.go, internal/agent/extrareadroots_test.go
**Read first:** internal/tools/path_read.go — readScope.resolve, readScope.extraRoots, readScope.readBounded, readScope.searchTarget, matchRoot, escapeOrMessage; internal/tools/path_suggest.go — notFoundOrRefusal, suggestSiblings, notFoundMessage;
internal/tools/list_dir.go — ListDir.Execute, directoryNotFoundMessage; internal/tools/file_ops.go — CopyFile.Execute, checkFileOpsPathsFrom; internal/tools/present_document.go — PresentDocument.Execute;
internal/tools/path_read_test.go — TestReadScopeSkipsUnusableExtraRoot, TestReadScopeScratchRootIsLive; internal/agent/extrareadroots_test.go — mountFixture, readThrough; internal/agent/orientation.go — orientationBlock

**Tests:**
- Rewrite `TestReadScopeSkipsUnusableExtraRoot` and its doc comment so that:
  - a missing root → `fs.ErrNotExist`, and neither `ErrPathEscape` nor `security.ErrRootInaccessible`;
  - an unrelated outside path → still `ErrPathEscape`.
- Add a missing-scratch case to the dotfiles-link fixture of `TestReadScopeScratchRootIsLive`. The announced `<link>/<missing>/x` → `fs.ErrNotExist`, not `ErrPathEscape`. A plain `t.TempDir` cannot catch this on Linux.
- Drive all six tools in their own test files: `read_file`, `list_dir`, `find_files`, `grep`, `present_document` and `copy_file`'s source. Each test name carries `MissingMountRoot`.
  - Call each with a path under a missing `Roots()` root and with a path under a missing `Scratch()` dir. Each answers exactly its own `<prefix><path>`, with no suggestions.
  - Call each with an unrelated outside path. Each answers `ErrPathEscape`'s message.
- Agent-level journey test `TestMissingMountRootAnswersNotFound` in `internal/agent/extrareadroots_test.go`:
  - Configure a read mount root that does not exist.
  - Set `cfg.ReadMounts.Scratch` to a closure over the Agent's `ScratchDir()`. Only the host wires it (`cmd/apogee/wire_live.go`).
  - Take the root string from the rendered orientation block exactly as emitted.
  - Through the tool-dispatch path, call `list_dir` on the bare root and on `<that string>/x`, and `read_file` on `<that string>/x`. Assert `directory not found:` / `file not found:`.
  - Do the same for a scratch path when the scratch dir is absent.

**Acceptance:**
- `go build ./... && go vet ./internal/tools/ ./internal/agent/ ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestReadScope|ListDir|FindFiles|Grep|PresentDocument|CopyFile|ReadFile|MissingMountRoot' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'ReadRoot|Mount|Skill' ./internal/agent/`
- both comment greps from the Approach, the line-bound one and the wrapped `-A1` one: each remaining hit read and true for the missing-root case

**Closes:** missing-mount-misreported

**Commit:** `fix(tools): a missing mount root answers not-found instead of the workspace fence`

## 2. CONTEXT.md agrees with the code — ✅ DONE (2026-10-12)

NOTES (2026-10-12): the Driver entry also says an observe webhook is posted by the Driver's Reaction runner, not the engine (verified: `cmd/apogee/wire_boot.go` builds the guarded executor), so "the one Reaction wire the engine opens itself" cannot be misread as covering it

**What:** four drifts in `CONTEXT.md`, plus one stale ADR 0076 sentence.

**Goal:** `CONTEXT.md` names Go packages by their real paths, lists all confinement backends, counts
the test drivers, records the sync-lane webhook as the one Reaction wire the engine opens itself, and lists `file-changed` as an advise
Moment; ADR 0076's 2026-09-19 amendment no longer says the sync webhook has no URL guard.

**Regression guard.** The engine also dials the Upstream (`Dialer`/`dialProvider`, `internal/agent/agent.go`), and `http_request`, `web_fetch` and `web_search` dial out. So the Driver entry says: the engine composes no inbound surface, and the sync-lane webhook POST (`runSyncWebhook`) is the one Reaction wire it opens itself, beside the Upstream dial and the network tools. It never says "one outbound wire". This stays inside ADR 0031 invariant 1 (wire-silent: no network-facing surface in the engine), and the item yields to that invariant. The package-path rule covers live package references only. The retired-term records (`internal/validated`, `internal/mechanisms`, `internal/library`) and the non-Go directories (`sessions/`, `skills/`) stay. The real sites are CONTEXT.md :712, :743, :1395 and :2046.

**Approach (assumed at the header base):**
- **Driver entry** (`**Driver**`): four *product* Drivers ship (TUI, `apogee daemon`, `apogee headless`, the bench), and the `internal/tuitest` in-process and PTY drivers are Drivers too (ADR 0062). Drivers compose every inbound wire surface and the engine has none. The sync-lane advise/gate webhook Reaction is the one Reaction wire the engine opens itself, beside the Upstream dial and the network tools: it POSTs through the url-safety guard (`(*Agent).runSyncWebhook`; ADR 0076's 2026-09-19 amendment).
- **Package paths:** the rule is every backticked live Go package path names a directory that exists (`` `provider/` `` → `` `internal/provider/` ``, `` `context/` `` → `` `internal/context/` ``). The "`platform/` `Confiner` interface" line becomes the `domain.Confiner` interface, with backends in `internal/platform/`. Find every site with `grep -nE '\`[a-z]+/\`' CONTEXT.md` and check each against `ls`.
- **Backends** (the confinement default-box line): seatbelt, landlock (else bwrap user+mount namespaces), Windows token; AppContainer rejected (ADR 0020). Replace "per-project allowlist" with an optional embedder-set network tightening list (`Config.ConfineNetworkAllow`, no config key).
- **`file-changed`:** in the notice-Moment paragraph, add that it is also one of the two Moments an advise Reaction fires at, beside `post-tool-result`.
- **ADR 0076:** add an `Amended 2026-10-12` block under the 2026-09-19 amendment. It says "No URL guard is added" is superseded: both the sync and observe webhooks post through the url-safety guarded client (commit `2d9e79e6`).

**Files:** CONTEXT.md, docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md
**Read first:** CONTEXT.md — **Driver**, **Confinement** default-box line, **Moment** notice paragraph; internal/agent/syncexec.go — runSyncWebhook; internal/agent/agent.go — Dialer, dialProvider;
docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md — 2026-09-19 amendment; docs/adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md — invariant 1

**Tests:** none (docs).

**Acceptance:**
- `grep -nE '\`(provider|context|platform)/\`' CONTEXT.md` prints nothing
- `grep -n 'per-project allowlist' CONTEXT.md` prints nothing
- `grep -n 'seatbelt.*bwrap\|bwrap.*seatbelt' CONTEXT.md` prints the backend line (nothing at base); `grep -n 'tuitest' CONTEXT.md` includes the Driver entry
- `grep -n -A16 '^A point the loop passes' CONTEXT.md | grep advise` prints the `file-changed` advise sentence (nothing at base)
- `grep -n 'Amended 2026-10-12' docs/adr/0076-*.md` prints the new block
- non-regression run only (it reads `docs/manual/README.md`, not `CONTEXT.md`): `GOMEMLIMIT=2GiB go test -count=1 -run TestManualStatesTheMomentCount ./internal/domain/`

**Commit:** `docs(context): align the Driver, confinement and Moment entries with the code`

## 3. Confinement and MCP design docs agree with the code — ✅ DONE (2026-10-12)

NOTES (2026-10-12): consequential edit — internal/domain/errors.go: made necessary by the ratified Auto gate; the `ErrAutoUnavailable` godoc said "the gate is FSWrite-only", which the Auto-gate grep surfaced
NOTES (2026-10-12): consequential edit — internal/platform/platform.go: made necessary by the ratified Auto gate; `denyConfiner` comments said it "never satisfies the Auto gate", now "is never AutoEligible" (the historical P0.6 "New's Auto gate" sentence stays)
NOTES (2026-10-12): consequential edit — internal/daemon/file.go: made necessary by the ratified Auto gate; the `Caps` comment now names the unattended-Auto gate (`probe.AutoUnattendedBlocked`) as the reader of `AutoEligible`
NOTES (2026-10-12): the `errAutoUnavailable` string in cmd/apogee/wire_engine.go is unchanged (announced wording); only its comment and `friendlyConstructErr`'s comment changed
NOTES (2026-10-12): confinement-execution-contract.md §2.6 and §7's 2026-08-22 amendment still say "per-project `WritablePaths`/`NetworkAllow` from config", though no config key exists (ADR 0012's 2026-10-02 note); left as is, since that phrase is outside this item's grep (allowlist only) and internal/domain scope

**What:** `docs/design/confinement-execution-contract.md` §5 says the `agent.New` Auto gate in
`loop.go` reads `AutoEligible()`. `docs/design/mcp-client.md`'s "depends only on" list misses two
imports. Code comments repeat the stale gate.

**Goal:** the contract doc, ADR 0012 and the code comments state the ratified Auto gate. The MCP
client's dependency list matches `internal/mcp`'s non-test imports. No comment in `internal/domain`
or `internal/agent` says Auto requires `AutoEligible()`, or names a "per-project allowlist".

**Regression guard.** ADR 0012 amends in its own style — `## Amendment (2026-10-12) — <title>` H2 sections or dated `> **Note …**` blocks — so item 3's ADR 0012 edit uses a dated `> **Note 2026-10-12 …**` block under the stale line in that ADR's style. The Auto-gate rule covers every comment saying construction refuses Auto on a missing `FSWrite`/`AutoEligible()`. Find them with `grep -rn 'Auto gate\|AutoEligible\|ErrAutoUnavailable' --include=*.go . | grep -v _test` and read each hit, `apogee.go` (`ErrAutoUnavailable` godoc) and `cmd/apogee/wire_engine.go` (`friendlyConstructErr`) included. Change comments only: the `errAutoUnavailable` string is announced wording and stays. The per-project rule covers the whole phrase family: `grep -rn 'per-project.*\(allowlist\|network\|tightening\)\|NetworkAllow.*per-project' internal/domain/` prints nothing (`confinement.go` :149/:154/:165, `config.go` :163).

**Approach (assumed at the header base):**
- **Contract §5:** agent construction (`buildAgent`) refuses Auto only for a nil Confiner (`ErrAutoUnavailable`). Without `FSWrite`, each subprocess call gates through Approval (`fsConfinementAvailable`). `AutoEligible()` is read only by `internal/probe`: the `apogee probe host` verdict, and `AutoUnattendedBlocked`, which refuses unattended Auto in `apogee headless` and Schedule Firings.
- **ADR 0012:** add a dated `> **Note 2026-10-12 …**` block, in that ADR's own style, under the "`agent.New` Auto gate and `ErrAutoUnavailable` follow" line, stating the same.
- **`internal/domain/confinement.go`:** rewrite the `AutoEligible` doc comment so it no longer contradicts itself: it reports whether the capabilities can fence Auto's subprocess surface (FSWrite under ADR 0012); it does not gate interactive Auto; only unattended Auto is refused on false. Fix the "per-project allowlist" wording near `NetworkAllow`.
- **`internal/agent/construct.go`:** fix the comment that calls the gate "FSWrite-only AutoEligible".
- **`mcp-client.md`:** add `internal/mcpauth` and `golang.org/x/net/http/httpguts`, each with its role as read from its call sites in `internal/mcp/client.go` and `transport.go`.

**Files:** docs/design/confinement-execution-contract.md, docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md, internal/domain/confinement.go, internal/domain/config.go, internal/agent/construct.go, apogee.go, cmd/apogee/wire_engine.go, docs/design/mcp-client.md
**Read first:** internal/agent/construct.go — buildAgent; internal/domain/confinement.go — ConfinementCaps.AutoEligible, ConfinementBox; docs/design/confinement-execution-contract.md — §5 "P3.4 changes AutoEligible()" paragraph;
internal/agent/dispatch.go — fsConfinementAvailable; internal/probe/confinement.go — AutoUnattendedBlocked; cmd/apogee/wire_engine.go — friendlyConstructErr; internal/mcp/transport.go — admitHeaderName

**Tests:** none (docs and comments).

**Acceptance:**
- `go build ./internal/domain/ ./internal/agent/ . ./cmd/apogee/ && go vet ./internal/domain/ ./internal/agent/ . ./cmd/apogee/`
- `grep -rn 'per-project allowlist' internal/ docs/design/` prints nothing
- `grep -rn 'per-project.*\(allowlist\|network\|tightening\)\|NetworkAllow.*per-project' internal/domain/` prints nothing
- `grep -n -A3 'depends only on' docs/design/mcp-client.md | grep -c 'mcpauth\|httpguts'` reports 2 (0 at base)
- `go list -f '{{join .Imports "\n"}}' ./internal/mcp/` lists no non-stdlib import missing from the doc's list
- `grep -n 'Auto gate (.loop.go.)' docs/design/confinement-execution-contract.md` prints nothing
- `grep -n 'Note 2026-10-12' docs/adr/0012-*.md` prints the new block
- the Auto-gate grep from the guard: each hit read and agrees with the ratified gate

**Commit:** `docs(confinement): state the Auto gate the code enforces`

## 4. One wording for the invariant — ✅ DONE (2026-10-12)

NOTES (2026-10-12): README.md left unchanged: it keeps "bare loop" and never says the Floor guards are "held to" the invariant ("the rule behind them" reads as the admission rule), so the approach's "only if" condition did not apply.
NOTES (2026-10-12): bare-loop hits read and left as they are, because they already match the Bypass definition: ADR 0077:37 (the notice is off under Bypass, so a bench arm beating the bare loop is a test against Bypass), docs/design/workflow-bench-experiment.md:21 (the invariant stated plainly), the plain-English winlabel doc.go and hook-talkback-findings.md, and reaction-core-greenfield.md:235 (tier 6).
NOTES (2026-10-12): ADR 0082's two hits (lines 69 and 151) are settled by one dated Amended block under the Decision 1 paragraph that covers both, rather than two blocks; the decision text is untouched.

**What:** `AGENTS.md` and `README.md` say "worse than the bare loop", and `AGENTS.md` says the Floor
guards "are held to it". `CONTEXT.md` and ADR 0006 define the floor as Bypass mode (guards on, not a
naked model). ADR 0079 attributes "bare loop" to ADR 0076, which never says it.

**Goal:** `CONTEXT.md` defines "bare loop" as Bypass mode (Floor guards and structural reducers on;
ADR 0006). `AGENTS.md` and `README.md` keep "bare loop", and neither says the Floor guards are held
to the invariant: the guards are part of the baseline. ADR 0079's citation points to ADR 0006.

**Regression guard.** The item yields to ADR 0071 Decision 1 clause (c): a Floor guard is admitted only if it cannot regress Bypass. So the reworded `AGENTS.md` sentence says the guards form the bare-loop baseline *and* are admitted to it only under ADR 0071 D1(c). Every "bare loop" use naming the invariant's baseline is settled against the Bypass definition, found with `grep -rn 'bare loop' --include=*.go --include=*.md . | grep -v 'docs/reviews/\|CHANGELOG\|docs/plans/\|\.capcom/'`. That covers `internal/floor/readcache.go:83`, ADR 0082:69,151, ADR 0077:37 and `docs/manual/reactions.md:361`. Plain-English uses (winlabel `doc.go`, `hook-talkback-findings.md`) stay, and so does `reaction-core-greenfield.md:235`, which is tier 6.

**Approach (assumed at the header base):** in `CONTEXT.md`'s opening invariant paragraph, add that
"the bare loop" is the plain-language name for that Bypass floor. In `AGENTS.md`'s first paragraph,
reword "The seven **Floor guards** and the structural reducers … are held to it" so they *form* the
bare loop that every model-facing behaviour above them is measured against. Adjust `README.md` only
if it carries the same "held to it" claim. In ADR 0079, add an `Amended 2026-10-12` block correcting
the citation to ADR 0006.

**Files:** CONTEXT.md, AGENTS.md, README.md, docs/adr/0079-context-cost-is-a-first-class-engine-report.md; and, where a "bare loop" hit needs settling: internal/floor/readcache.go, docs/adr/0077-the-context-fill-notice-is-the-first-engine-advise-reaction.md, docs/adr/0082-a-silent-stream-is-cut-and-a-transient-fault-is-ridden-out-under-a-budget.md, docs/manual/reactions.md
**Read first:** AGENTS.md — opening paragraph; CONTEXT.md — opening invariant paragraph (Bypass floor); docs/adr/0079-context-cost-is-a-first-class-engine-report.md — Context paragraph citing ADR 0076; docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md — Decision 1 clause (c);
docs/adr/0006-bypass-mode-is-the-mechanisms-off-floor.md — Decision; internal/floor/readcache.go — priorSuccessfulReadUnchanged doc comment; docs/adr/0082-a-silent-stream-is-cut-and-a-transient-fault-is-ridden-out-under-a-budget.md — ten-minute default rationale;
README.md — Why apogee floor bullet (readmeFloorGuardCount)

**Depends on:** item 2 (both edit `CONTEXT.md`).

**Tests:** none (docs).

**Acceptance:**
- `grep -n 'bare loop' CONTEXT.md` prints the definition
- `grep -n 'held to it' AGENTS.md README.md` prints nothing
- `grep -c 'bare loop' AGENTS.md README.md` reports ≥ 1 per file
- `grep -n '0006' docs/adr/0079-context-cost-is-a-first-class-engine-report.md` prints the amendment (nothing at base)
- the "bare loop" grep from the guard: each remaining hit read and consistent with the Bypass definition
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestReadmeStatesTheToolCounts' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestReadmeStatesTheFloorGuardCount' ./internal/config/`

**Commit:** `docs: define the bare loop as the Bypass floor`

## 5. ADRs 0089, 0094 and 0097 carry their corrections

**What:** ADR 0089 D1 (and its 2026-10-05 amendment) gates the background switch on bench approval.
ADR 0094 D1's body says the same, against its own 2026-10-07 Note. ADR 0097 never records that `dev`
and `.dirty` builds skip the update check, and its "today" lines are stale.

**Goal:** each ADR carries a dated amendment that matches the code. ADR 0089 D1 and ADR 0094 D1 name
the gate as the Driver's `OffersBackground` opt-in plus the roster lifting `workflow`, not bench
approval. ADR 0097 records the `dev`/`.dirty` exemption, dates its single-channel "today", and
records that `cmd/release publish` replaced the two publish steps (ADR 0098).

**Regression guard.** The 0097 amendments follow a rule, not a list of three: every 0097 sentence that
`internal/update/detect.go` (`Detect`, `Method.UpgradeCommand`) or `cmd/apogee/wire_update.go` contradicts
gets an `Amended 2026-10-12` block. That includes point 6's five channels, point 7's unstamped-is-source and the
Consequences lines on `go install` and the two publish steps: the code has a `GoInstall` channel (`go install …@latest`) and checks install path before stamp, as `docs/manual/updating.md` says.

**Approach (assumed at the header base):** read the gate from `internal/tools/registry.go`
(`OffersBackground`, `rosterDeltas().lifts(WorkflowToolName)`) and the exemption from
`cmd/apogee/wire_update.go` (`devBaseVersion`, `dirtyVersionSuffix`). Write one `Amended 2026-10-12`
block under each stale paragraph: 0089 D1, 0094 D1, and ADR 0097's context line, its decision on
the boot check, and its consequences line on the publish steps.

**Files:** docs/adr/0089-a-workflow-may-run-in-the-background-and-wakes-the-agent-when-it-ends.md, docs/adr/0094-sub-agent-may-run-as-a-one-item-background-workflow.md, docs/adr/0097-the-tui-checks-for-a-newer-release-and-names-the-upgrade-command.md
**Read first:** internal/tools/registry.go — HostTools.OffersBackground, RosterDeltas.lifts; cmd/apogee/wire_update.go — devBaseVersion, dirtyVersionSuffix; internal/update/detect.go — Detect, Method.UpgradeCommand;
docs/adr/0089-a-workflow-may-run-in-the-background-and-wakes-the-agent-when-it-ends.md — D1 and its 2026-10-05 amendment; docs/manual/updating.md — channel table

**Tests:** none (docs).

**Acceptance:**
- `grep -c 'Amended 2026-10-12' docs/adr/0089-*.md docs/adr/0094-*.md docs/adr/0097-*.md` reports ≥ 1 per file
- `grep -n 'dirty' docs/adr/0097-*.md` prints the amendment
- `grep -n 'OffersBackground' docs/adr/0089-*.md` prints the new amendment (nothing at base)
- `grep -n '@latest' docs/adr/0097-*.md` prints the `GoInstall` amendment (nothing at base)
- `grep -n 'cmd/release publish' docs/adr/0097-*.md` prints a line below `## Consequences` (at base only point 6's 2026-10-11 block matches)

**Commit:** `docs(adr): record the background gate and update-check corrections`
