# Plan — security & crash fix wave (audit 2026-08-01, Waves 0+1)

**Date:** 2026-08-01
**Status:** not started
**Goal:** land every mechanical security/crash fix from the 2026-08-01 findings merge — the
confirmed hookrun panic plus the audit's exploitable holes — each with a regression test that
survives the later architecture refactors.

**Authoritative sources (precedence in this order):**

1. `docs/reviews/2026-08-01 - code-audit.md` — the finding evidence, probes, and prescribed
   fixes (section named per item below).
2. `docs/handoffs/2026-08-01 - 01 - merged-findings-roadmap.md` — sequencing rationale and the
   verified hookrun-crash facts (§2).
3. Repo conventions: `AGENTS.md`, `internal/tui/doc.go` (ADR 0011), and the ADRs named per
   item. Where the generic coding-standards skill conflicts with a repo convention, the repo
   convention wins.

**Standing requirements:**

- Invoke this plan with forwarded skills: `coding-standards` (Go) — every implementer and
  verifier applies it.
- `make check` green before every commit. One commit per item. Never bump VERSION/CHANGELOG
  headings (see closing note).
- No live LLM endpoint is needed for any item.

**Out of scope (deliberate):**

- The architecture candidates (C1–C11) and all smaller findings — they follow their grills per
  the roadmap §6. In particular do NOT refactor hookrun's runner structure (item 1 is a
  minimal hotfix; C1 deletes the site later) and do NOT introduce `security.Workspace` (C3).
- The decision-gated audit items: `autofix` formatter routing and skills load order (roadmap
  §5) — they await owner decisions.
- The engine/TUI correctness wave (session-record serialization, sub-agent mode composition,
  heartbeat/launcher, interjections, mechanism heuristics) — that is plan
  `2026-08-01 - 01 - engine-tui-correctness-wave-plan.md`.

---

## 1. Hotfix the post-tool-result acted-probe panic — ✅ DONE (2026-08-01)

**What:** In `internal/agent/hookrun.go` (`runPostToolResultHooks`, snapshot at `:256`,
compare at `:260`), replace the whole-struct compare `*result != before` — which panics with
`comparing uncomparable type domain.OpenedFile` whenever a successful `open_file` result
passes through and the hook did not act (roadmap §2; reproduced live) — with a panic-safe
helper `toolResultChanged(before, after domain.ToolResult) bool` that compares `CallID`,
`Content`, `IsError` directly and `Summary` via `reflect.DeepEqual`. Semantics unchanged:
"changed" still books the acted fire. Add a short comment naming this a hotfix that the C1
runner consolidation deletes. Do not touch the experimental loop (`:264-273` — it books
unconditionally, by design) and do not restructure the runners.

**Tests:** New `internal/agent/hookrun_test.go` (the file does not exist yet): (a) regression
— a catalogued post-tool-result no-op hook plus a tool result carrying
`domain.OpenedFile{LocatedOn: []int{…}}` driven through the real dispatch path completes
without panic and books no fire; (b) a hook that mutates `Content` books a fire; (c) a hook
that mutates only `Summary` books a fire (the DeepEqual half). Table-driven, `t.Run` subtests.

**Acceptance:** `go test ./internal/agent/ -run TestPostToolResult -count=1 -race` passes;
`make check` green. Reverting the helper to the struct compare makes test (a) panic.

**Commit:** `fix(agent): make the post-tool-result acted probe panic-safe on summary-carrying results`

## 2. A forced approval never writes the allow-for-session cache — ✅ DONE (2026-08-01)

**What:** Audit "High — A forced (Tier-2) approval writes the allow-for-session cache".
`internal/agent/dispatch.go:283-287`: the `ApprovalAllowForSession` branch writes
`a.approved[cacheKey] = true` unconditionally while the read at `:264` is already gated on
`!force`. Gate the write on `!force` too, so a forced allow behaves as a plain
`ApprovalAllow` — matching `docs/design/confinement-execution-contract.md:439` ("a forced
gate … is never pre-allowable") and the comment at `dispatch.go:255-257`.

**Tests:** The write-direction mirror of `TestMCPServerGrain_ForcedGateStillPromptsOnCachedServer`:
a forced Tier-2 gate answered allow-for-session, then an ordinary sibling call for the same
tool — the Approver must be consulted twice (the audit's probe observed 1). Cover the MCP
server-grain key too: a forced prompt on one server tool must not pre-clear its siblings.

**Acceptance:** `go test ./internal/agent/ -run 'Forced' -count=1` passes; `make check` green.

**Commit:** `fix(security): a forced approval never writes the allow-for-session cache`

## 3. Terminate the ref position in `git_branch` switch/create — ✅ DONE (2026-08-01)

NOTES (2026-08-01): `create` emits the trailing `--` unconditionally, not only when
`start_point` is set, so both create shapes share one argv discipline (`checkout -b <name> --`
and `checkout -b <name> <start-point> --`); git 2.43 accepts both, verified by probe, and the
argv table test pins all three forms. Slightly wider than the item's literal "the `create`
start-point argument", never narrower.

**What:** Audit "High — `git_branch switch` omits `--`…". `internal/tools/git.go:223`
(`buildBranchArgs`): emit `checkout <name> --` so git can never resolve a non-ref,
path-shaped name as a pathspec and revert tracked files; apply the same `--` termination to
the `create` start-point argument (`:217-221`). Keep `looksLikeOption` as-is (it closes a
different class).

**Tests:** (a) table test on `buildBranchArgs` pinning the exact argv for switch and create
(trailing `--` present); (b) behavioral test in a temp repo: with an uncommitted edit in
`docs/notes.md`, switching to non-existent branch `docs` returns an error and the edit
survives byte-identical (the audit's probe showed it silently reverted).

**Acceptance:** `go test ./internal/tools/ -run 'Branch' -count=1` passes; `make check` green.

**Commit:** `fix(tools): terminate the ref position in git_branch switch and create with --`

## 4. Persist the Library store atomically — ✅ DONE (2026-08-01)

NOTES (2026-08-01): the item's two listed tests both pass against the old in-place
`os.WriteFile` (it strands no temp file either), so a third test was added that actually
regresses the fix: a descriptor held open on the previous store still reads the complete
previous bytes after a persist (rename-replaces vs truncate-in-place; skipped on Windows,
which refuses rename over an open file). Additive — the two listed tests are present as
written.

**What:** Audit "Medium — The Library store rewrites itself in place…".
`internal/library/store.go:285` (`persist`): write to a temp file in the store's directory
and `os.Rename` over `library.json`, so a crash mid-write can no longer truncate the store
and silently degrade the next `Load` to empty. Match the temp+rename idiom the session store
already uses.

**Tests:** `persist` leaves no temp file behind and the store round-trips; a pre-planted
stale temp file is not mistaken for the store.

**Acceptance:** `go test ./internal/library/ -run 'Persist|Store' -count=1` passes;
`make check` green.

**Commit:** `fix(library): persist the store via temp file + rename`

## 5. Strip the harmony open token from an unterminated tail — ✅ DONE (2026-08-01)

NOTES (2026-08-01): two combined vectors were added instead of one — the audit's exact shape
(`<|start|>assistant<|channel|>final<|message|>hello`) plus an analysis-channel tail preceded by
plain lead text, which pins that the lead is cut at the match start rather than at
`<|channel|>`. Both fail before the fix and pass after; additive, never narrower than the item.

**What:** Audit "High — `StripHarmony` leaks `<|start|>role`…".
`internal/processing/harmony.go:92`: the unterminated-tail path slices at
`strings.Index(tail, "<|channel|>")`, keeping the `<|start|>role` prefix in `Visible`
(e.g. `"<|start|>assistanthello"`). Use `harmonyOpen.FindStringSubmatchIndex(tail)` and take
`tail[:loc[0]]` as the visible lead, mirroring the terminated-message loop at `:77-82`.

**Tests:** Add the combined vector — unterminated tail WITH the `<|start|>role` prefix — to
`TestStripHarmony_FullChannelSet` (`internal/processing/harmony_test.go`; it fails today).

**Acceptance:** `go test ./internal/processing/ -run 'Harmony' -count=1` passes; `make check`
green.

**Commit:** `fix(processing): strip the harmony open token from an unterminated tail's visible lead`

## 6. Fence workspace context-file reads — ✅ DONE (2026-08-01)

NOTES (2026-08-01): the `SafeOpen` + `io.ReadAll` pair lives in a small `readContextFile` helper
rather than inline in the loop, and the escape case leads the loader's switch explicitly (it would
already fall into the generic `err != nil` branch — leading pins that a refusal can never be read
as absence). Tests are additive: the item's two cases plus a second escape shape (a symlinked
PARENT component, `docs -> outside` with name `docs/id_rsa`), both proven to fail against the old
`os.ReadFile`. Accepted consequence of reading through the fence: a `--workspace` pointing at a
directory that does not exist now yields one loud unreadable notice per configured name instead of
silence, because `os.OpenRoot` on a missing root surfaces as `ErrPathEscape` — a broken workspace
is reported rather than hidden.

**What:** Audit "High — Workspace context files are read with a bare `os.ReadFile`".
`internal/agent/contextfiles.go:58`: replace `os.ReadFile(filepath.Join(workspaceDir, name))`
with `security.SafeOpen(workspaceDir, name)` + `io.ReadAll`, mapping
`security.ErrPathEscape` onto the existing present-but-unreadable branch so the host reports
the skip loudly (a symlinked `AGENTS.md -> ../../.ssh/id_rsa` currently lands verbatim in the
standing system message; ADR 0026 leans on this fence). Keep `validateContextFileNames`
unchanged — it gates the configured names; this item gates what they resolve to.

**Tests:** (a) a context file that is a symlink escaping the workspace is skipped with the
loud unreadable notice, its content absent from the assembled request; (b) a normal file
still loads byte-identical (extend the existing context-seam tests in
`internal/agent/promptseam_test.go` or a sibling).

**Acceptance:** `go test ./internal/agent/ -run 'Context' -count=1` passes; `make check`
green.

**Commit:** `fix(agent): fence workspace context-file reads inside the workspace root`

## 7. `grep` opens walked files through the fence — ✅ DONE (2026-08-01)

NOTES (2026-08-01): the fence is anchored at the WORKSPACE root, not at the searched
subdirectory, so a link that leaves the searched subtree but stays inside the workspace keeps
matching — the walk's relative name is lifted to a workspace-relative one before the open, and
the reported location stays relative to the searched directory as before. That lift needs the
root resolved through symlinks (new `Grep.realRoot`, `security.EvalRealPath`), because
`resolveInRoot` returns real paths and the raw root is a prefix of none of them where the
workspace itself is reached through a link (macOS `/tmp`) — without it grep would return nothing
at all there. Tests are additive: the item's `TestGrep_RefusesEscapingSymlink` (both cases) plus
`TestGrep_Execute_SearchesSubdirectory`, which covers the subtree walk the path lift introduced
and which pins the unchanged location rendering (its three display assertions pass against the
pre-change code; only its escape assertion fails). Accepted narrowing, the same one `read_file`
took and recorded: an in-workspace symlink whose target is spelled ABSOLUTE is skipped by the
walk — verified that the target still matches under its own name, so only the duplicate hit is
lost. The single-file path also opens through the fence, but its refusal still comes from the
pre-existing `resolveInRoot` gate, so that half is a boundary pin rather than new behaviour.

**What:** Audit "High — `grep` follows workspace symlinks out of the workspace".
`internal/tools/grep.go:144` (walk) and `:169-177` (`searchFile`): open every walked entry
through `security.SafeOpen(root, rel)` (or walk via `os.OpenRoot`), and route the single-file
path through the same fence, so a planted `notes.txt -> ~/.ssh/id_rsa` symlink returns
nothing (the audit's probe read `SUPERSECRET_TOKEN` through it; `grep` is read-only, so it
runs unapproved in every mode).

**Tests:** `TestGrep_RefusesEscapingSymlink` alongside the existing
`TestReadFile_Execute_RefusesEscapingSymlink` shape: symlink to an outside file yields no
matches from its content, for both the directory walk and the explicit single-file path; an
in-workspace symlink to an in-workspace file still matches.

**Acceptance:** `go test ./internal/tools/ -run 'Grep' -count=1` passes; `make check` green.

**Commit:** `fix(tools): grep opens walked files through the workspace fence`

## 8. Pin `present_document`, `diagnostics` and `list_dir` to the root — ✅ DONE (2026-08-01)

NOTES (2026-08-01): all three refusal tests are BOUNDARY PINS, not pre-change failures —
verified by running them against the reverted sources, where all six subtests pass. Unlike
`grep`'s walk, every one of these three tools reaches its I/O through `resolveInRoot`, which
already refuses a static escaping symlink; what this item closes is the check-then-use gap
BEHIND that check (stat / `os.ReadDir` / parse-by-filename each re-walk the resolved string),
and that race has no deterministic test without an injection seam the item does not ask for.
Same shape item 7 recorded for its single-file half; the tests fail if either half is dropped.
Three consequences worth naming: (a) each tool opens by the workspace-relative form of the
ALREADY-RESOLVED path, so item 7's absolute-symlink narrowing does NOT recur here — an
in-workspace absolute symlink still presents/diagnoses/lists; (b) `list_dir` now sorts each
directory's entries by name explicitly, because a directory HANDLE yields filesystem order
where `os.ReadDir` sorted, and that order is pinned output (`TestListDir_Execute_ReportsEntryCounts`);
(c) `diagnostics` reports an unreadable Go file as `file not found: <path>` instead of
go/parser's `open <abs>: no such file or directory`. Shared helpers `statInRoot`,
`escapeOrMessage` and `workspaceRelative` (the last MOVED out of `present_document.go`) live in
`internal/tools/path_safety.go`; `readFileErrorMessage` now delegates to `escapeOrMessage`,
semantics identical.

**What:** The audit's sweep list minus `grep` (item 7) and `view_diff` (item 9): close the
same check-then-use gap in `internal/tools/present_document.go:95-108`,
`internal/tools/diagnostics.go:107/:171` (prefer handing the already-read source bytes down
instead of re-reading by path), and `internal/tools/list_dir.go:66-71` — each currently does
plain `os.*` I/O on a `resolveInRoot` string result. Use `security.SafeOpen`/`os.Root`-pinned
equivalents. Behavior otherwise unchanged.

**Tests:** One escaping-symlink refusal test per tool, following the item-7 shape (for
`list_dir`: a symlinked directory out of the workspace is not listed through).

**Acceptance:** `go test ./internal/tools/ -run 'PresentDocument|Diagnostics|ListDir' -count=1`
passes; `make check` green.

**Commit:** `fix(tools): pin present_document, diagnostics and list_dir I/O to the workspace root`

## 9. Bound and fence `view_diff` — ✅ DONE (2026-08-01)

NOTES (2026-08-01): the over-budget path DEGRADES (a success result carrying the diffstat plus a
sentence saying the rendering was withheld), the branch of the item's "refuse or degrade" its Tests
line requires. Its counts are approximate by construction and the sentence says so: everything
between the identical head and the identical tail counts as changed, since deciding which inner
lines survive is exactly the LCS work the cap avoids — an upper bound, never an under-count.
Nothing proportional to the line count is allocated on that path (neither side is even split; the
`strings.Split` slices alone would be 160 MiB for a 10 MiB file of short lines), pinned by an
allocation ceiling in the test. Reading through `readWorkspaceFileBounded` drops the `resolveInRoot`
pre-pass entirely, as `read_file` did, so this item carries the same recorded narrowing: an
in-workspace symlink whose target is spelled ABSOLUTE is now refused (CHANGELOG). Tests are
additive: the item's escaping-symlink case is a BOUNDARY PIN (it passes against the pre-change code,
which resolved symlinks at check time), so the fence half is regressed by
`TestViewDiff_RefusesComponentSwappedMidRead` instead — the check-then-use race, measured at 66
escapes in 2000 calls before the fix — plus a small table pinning the degraded stat's arithmetic
(including the head/tail overlap clamp). "A normal small diff is byte-identical to today's output"
needed no new test: `TestViewDiff_ReportsDiffStat` already asserts exact content and passes
unchanged. NOT done, deliberately out of the item's literal (a)/(b)/(c): the audit's side note that
the LCS fill loop takes no `ctx` — with the table capped at 25e6 cells the fill is sub-second.

**What:** Audit "High — `view_diff` reads unbounded and unfenced, then allocates a quadratic
table". `internal/tools/diff.go:69/:131-149`: (a) read the old side through
`readWorkspaceFileBounded` (cap + pinned descriptor in one step, closing the fence gap);
(b) reject `args.NewContent` larger than `maxFileContentBytes`; (c) when
`len(oldLines)*len(newLines)` exceeds a fixed budget (named constant, 25e6 cells), refuse or
degrade to a diffstat-only result instead of allocating the LCS table (measured: 6k×6k lines
= 282 MiB; the in-code claim at `:129-130` that read bounds apply here is false — correct it).

**Tests:** Oversized file refused with a clear tool error; oversized `newContent` refused;
both-sides-large input returns the diffstat degradation, allocation-free; escaping symlink
refused; a normal small diff is byte-identical to today's output.

**Acceptance:** `go test ./internal/tools/ -run 'Diff' -count=1` passes; `make check` green.

**Commit:** `fix(tools): bound view_diff reads, content and table size`

## 10. Validate session-record ids as filenames — ✅ DONE (2026-08-01)

NOTES (2026-08-01): the item's PARENTHETICAL rule was taken ("at minimum reject
`id != filepath.Base(id)`", hardened to: non-empty, ≤ 200 bytes, no separator of either OS, no
`.`/`..`/dot prefix, no control characters) rather than the strict minted shape, because the strict
shape would break two documented behaviours the plan does not authorise changing: pre-plan bare
envelopes synthesise their id from the FILENAME STEM (`wrapLegacy`), which is arbitrary, and a
legacy record renamed through `Rename` is then saved as a wrapper under that same stem — so a
shape check at `decodeRecord`/`Save` would make every legacy session unloadable and unrenamable.
The lenient rule closes the whole traversal/collision surface the finding describes (the id can no
longer leave the store directory) and admits both minted ids and legacy stems. Validation sits in
`decodeRecord` (covering both on-disk shapes, so `List` soft-skips a planted record), plus `Save`,
`Load` and `Delete`; the old empty-id error is now `ErrInvalidID`, and `TestSaveRejectsEmptyID` is
folded into the table-driven `TestSaveRejectsUnsafeID` rather than duplicated. Re-minting on
explicit-path resume is UNCONDITIONAL (`resolveResumeArg`'s `LoadPath` branch), including for a
path that happens to point at the store's own file for that id: "adopted from a path ⇒ new session"
is one rule with no store-internals leak, and `--resume <id>` remains the in-place resume.

**What:** Audit "Medium — Session-record IDs from an untrusted file become filesystem
paths". `internal/session/store.go:116/:154/:171` + `cmd/apogee/wire.go:855-865`: validate
`Meta.ID` at `decodeRecord`/`Save`/`Delete` — accept only the minted shape
(`20060102T150405Z-8hex`; at minimum reject `id != filepath.Base(id)`) — and re-mint the id
when adopting a record loaded via an explicit `--resume ./path` (`resolveResumeArg` →
`newSessionHost`), so a planted record can never steer autosaves onto an attacker-chosen
path or overwrite another session.

**Tests:** Hostile ids (`../../x`, absolute, other-session collision via path-load) are
refused at Save/Delete and re-minted on adoption; a legitimate stored session still
round-trips and `--resume <id>` still works.

**Acceptance:** `go test ./internal/session/ ./cmd/apogee/ -run 'Session|Resume|Record' -count=1`
passes; `make check` green.

**Commit:** `fix(session): validate record ids as filenames and re-mint on explicit-path resume`

## 11. Re-fence the document server per request and bound it — ✅ DONE (2026-08-01)

NOTES (2026-08-01): the item's PRIMARY branch was taken (grant = `(root, workspace-relative name)`,
re-opened per request through `security.SafeOpen`), not the `*os.File`-kept alternative, which would
have contradicted two documented behaviours: a deleted document must 404 rather than serve from a
held descriptor, and grants are append-only for the server's life, so held descriptors would
accumulate on the very budget the connection cap protects. Four things the item's text left open:
(a) the root arrives as a new REQUIRED `DocServer.Root` field set by the composition root
(`presentationRungs` now takes the workspace), so `Serve` fails closed on a server with no fence —
the type's "zero value is usable" claim is narrowed accordingly, and every construction site
(including `internal/tui`'s presenter tests) now passes a root; the alternative, passing a root per
`Serve` call, would have had to travel through `domain.PresentRequest`, whose `DisplayPath` is a
*display* string that need not be the real relative name. (b) The relative name is derived with
`security.WorkspaceRelative`, which is `internal/tools`' `workspaceRelative` MOVED into the
path-safety guard where the rest of the fence lives (the tools-local name now delegates, matching
that file's existing alias pattern) — measuring against the symlink-resolved root is required, not
cosmetic, since a real path under a symlinked root (macOS `/tmp`) will not relativise against the
configured one. This touches item 8's file; it is a consolidation, not a defect fix, and duplicating
a path-safety rule into a second package is exactly the drift the audit hunts. (c) The cap is a
local shedding wrapper (`internal/present/listener.go`), not `netutil.LimitListener`: netutil BLOCKS
accepting while full, which parks the flood in the kernel backlog rather than shedding it — the
item's own Tests line asks for shedding. No new dependency. (d) Numbers the item did not fix:
`maxConnections = 32`, `idleTimeout = 60s`, `writeTimeout = 2m`. The timeouts are pinned as
`http.Server` field values rather than by wall-clock behaviour (a test that waits out a 60-second
idle timeout is not worth its runtime); the connection cap IS pinned behaviourally — the cap is
saturated with real keep-alives, the next connection is closed rather than parked or answered, and
the agent's own fetch succeeds after they end.

**What:** Audit "Medium — The document server re-opens the granted path per request with no
fence". `internal/present/server.go:236/:115/:196/:201`: store the grant as
`(root, workspace-relative name)` and re-open through `security.SafeOpen(root, name)` on
every request (or keep the `*os.File` from `Serve` and re-`fstat` the descriptor), so a
post-grant symlink swap (`ln -s ~/.apogee/config.yaml report.html`) serves a refusal, not
the target. Set `IdleTimeout` and `WriteTimeout` beside the existing `ReadHeaderTimeout`,
and cap concurrent connections (`netutil.LimitListener`-style wrapper) so an unauthenticated
peer cannot pin unlimited keep-alives.

**Tests:** Grant then swap the path for a symlink → the request is refused; timeouts and the
connection cap are pinned by test (a saturated listener sheds, the agent's own request still
succeeds afterwards); a normal grant still serves byte-identical.

**Acceptance:** `go test ./internal/present/ -count=1` passes; `make check` green.

**Commit:** `fix(present): re-fence the served document per request and bound the server`

## 12. Derive landlock access masks from the probed ABI — ✅ DONE (2026-08-01)

NOTES (2026-08-01): `accessMaskForABI` is called ONCE, in `applyLandlock`, and the derived mask is
passed to `allowWriteBeneath` as a parameter rather than re-derived there — the item's "used by
both" in effect, but not by two calls. The kernel cross-checks the two masks (a rule's
`allowed_access` must be a subset of the ruleset's `handled_access_fs`, else `landlock_add_rule`
returns EINVAL), and `allowWriteBeneath` has no ABI of its own to derive from: re-probing inside
the loop would be a second syscall per writable root whose answer could, in principle, differ from
the one the ruleset was built with. Two other choices the item left open: (a) an `abi` below the
fs-write floor clamps to the ABI-1 baseline rather than returning zero — it is not a valid input
(`applyLandlock` refuses below the floor before reaching it), and zero would be a ruleset that
handles nothing, i.e. fences nothing; (b) `landlockFSWriteAccess` is renamed
`landlockFSWriteAccessABI1`, since it is now the baseline layer rather than the whole mask.
Tests are additive: the item's two (the exact-mask table, and `TestLandlockCapabilitiesHonest`
extended with a `wantAccess` column plus a new `abi2_kernel_5_19` row for Debian 12) plus
`TestAccessMaskForABIRightsTrackTheKernel`, which states the properties as invariants so a future
right cannot regress them by moving the pinned constants with it. All three fail against the
pre-change unconditional mask and pass after. The enforcement battery (`TestLandlockProbe`) SKIPS
on this dev host — it reports `FSWrite==false`, landlock is absent in this container — so the
real-kernel pass stays owner-run as the item's verifier note says.

**What:** Audit "High — The landlock ruleset requests an ABI-3 access right while
advertising ABI-1 support". `internal/platform/landlock_linux.go:85/:204`:
`landlockFSWriteAccess` unconditionally includes `LANDLOCK_ACCESS_FS_TRUNCATE` (ABI 3), so
`landlock_create_ruleset` fails with EINVAL on ABI 1–2 kernels (Ubuntu 22.04, Debian 12,
RHEL 9) and every confined call dies while `Capabilities()` still reports `FSWrite: true` —
Auto becomes a mode in which nothing runs. Add a pure `accessMaskForABI(abi)` used by both
`applyLandlock` and `allowWriteBeneath`: drop `TRUNCATE` below ABI 3, and ADD
`LANDLOCK_ACCESS_FS_REFER` at ABI ≥ 2 (landlock denies cross-directory rename/link by
default when the right is unhandled, so today a confined child cannot `git mv` even inside
the workspace).

**Tests:** Table test pinning the exact masks for ABI 1/2/3+; extend
`TestLandlockCapabilitiesHonest` so the `abi1_kernel_5_13` case actually exercises the mask
derivation (today it pins `FSWrite=true` without ever calling `applyLandlock` below ABI 3).
NOTE for the verifier: end-to-end enforcement on a real ABI-1/2 kernel is owner-run
(roadmap §7) — the table tests are this item's acceptance.

**Acceptance:** `go test ./internal/platform/ -run 'Landlock|ABI' -count=1` passes;
`make check` green.

**Commit:** `fix(platform): derive landlock access masks from the probed ABI and handle REFER`

## 13. Never label hard-linked files in the low-integrity walk — ✅ DONE (2026-08-01)

NOTES (2026-08-01): three choices the item's text left open. (a) A link count that could not be
READ takes the tolerated rung too (`hardLinkCount` returning an error ⇒ skip): an unknown count
cannot rule the shared-descriptor case out, and the walk must not widen the fence on a guess —
the same direction the item prescribes for an unreadable prior. It costs the label on a path
whose attributes cannot be opened at all, which stays read-only to the confined child rather than
gating the box. (b) The decision seam takes a `descendantFacts` value (prior, priorErr, links,
linksErr) instead of growing to four positional parameters, so `descendantDecision` stays the ONE
place a descendant's fate is decided — the alternative, a second predicate the walk ANDs with the
first, would split the tolerated rung across two functions. (c) `hardLinkCount` opens with
`FILE_READ_ATTRIBUTES` + `FILE_FLAG_BACKUP_SEMANTICS` + `FILE_FLAG_OPEN_REPARSE_POINT` and every
share mode, so one call answers for directories too (NTFS never hard-links them; they report 1)
and never disturbs or follows anything. Out of scope and left as-is: `ClearTree` still clears
every descendant unconditionally, so teardown writes a NULL SACL over a hard-linked path the
label walk now skips — the posture it already takes for the unreadable-prior rung, and a change
to it belongs to a revert-side item, not this one.

**What:** Audit "High — The Windows Low-integrity walk labels hard links…".
`internal/platform/winlabel/walk_windows.go:100/:127`: the reparse-point skip does not catch
hard links, and labelling an in-box hard link marks the shared MFT record Low everywhere it
is linked (pnpm's global store under `%LOCALAPPDATA%` being the concrete casualty). Stat
each descendant through `GetFileInformationByHandle`; a file with `NumberOfLinks > 1` takes
the existing tolerated-descendant rung — skip it, label nothing, journal nothing — the same
posture `descendantDecision` already takes for an unreadable prior.

**Tests:** Extract the links-count decision into the policy predicate so it is table-tested
off-OS alongside the existing policy tests; add a native Windows test (build-tagged, joins
the existing 20+ winlabel native tests) creating a hard link inside the box and asserting
the target's descriptor is untouched. NOTE for the verifier: the native test compiles here
but runs only on Windows (owner-run pass, roadmap §7) — off-OS acceptance is the policy
table test plus a clean cross-compile.

**Acceptance:** `go test ./internal/platform/... -count=1` passes on Linux;
`GOOS=windows go build ./...` succeeds; `make check` green.

**Commit:** `fix(platform): never label hard-linked files in the low-integrity walk`

## 14. A project rule add can no longer dissolve a shipped rule — ✅ DONE (2026-08-01)

NOTES (2026-08-01): `TestMergeDangerousRules_ProjectAddTightensInPlace` is RENAMED to
`…_ProjectAddTightensAlongside` — its old name asserts the semantics this item removes, and
the item's `-run 'MergeDangerousRules'` acceptance still selects it. Two doc corrections the
new contract forces are included: `Rule.ID`'s "unique within a ruleset" claim in
`dangerous.go` (a project tighten now coexists) and the same-ID sentence in
`docs/design/technical-design.md:201`. `internal/security/doc.go` and TODO.md L1 stay correct
as written.

**What:** Audit "High — The tighten-only project rule merge can be dissolved by tier
promotion". `internal/security/rules.go:147` (`MergeDangerousRules`): a same-ID project add
with a strictly higher Tier currently replaces the whole rule — Pattern included — so
`{ID: "sudo-escalation", Tier: TierHardRefuse, Pattern: "zzz-never-fires"}` dissolves the
shipped pattern. On a same-ID strictly-stricter add, keep BOTH rules (Inspect already
reports the strictest matching rule, so coexistence tightens without shrinking coverage).
Latent today (no production caller), but it must land before the dangerous-rule config
surfacing parked in `TODO.md` (L1).

**Tests:** Update `TestMergeDangerousRules_ProjectAddTightensInPlace` (it currently asserts
the replacement as intended) and add a tier-promotion case to the dissolve test: after the
merge, the shipped pattern still matches `sudo …` and the project rule coexists.

**Acceptance:** `go test ./internal/security/ -run 'MergeDangerousRules' -count=1` passes;
`make check` green.

**Commit:** `fix(security): a project rule add can no longer dissolve a shipped rule by tier promotion`

## 15. Strip terminal escapes at the transcript and popup seams — ✅ DONE (2026-08-01)

NOTES (2026-08-01): three of the named producers are covered by a seam instead of being wrapped
individually, which is the finding's own "strip at the seam rather than per producer" rule applied
one step further. `toolActivityLabel` builds its phrase only from `presentToolCall`'s view, which
now sanitizes on the way out (`toolView.sanitize`), so wrapping it again would be a redundant
strip; the resume note (`sessions.go:370`) and the rebind note (`model.go:1885`) reach the screen
only through `addEphemeralNote` / `addNote`, both of which now strip. `addEphemeralNote` was added
to the stripping seam list for that reason (it is `addNote`'s sibling and carries the resume
notices and the context-file notice). Each of the three carries a doc line saying which seam covers
it, and all three are pinned by the tests. Two producers the item did not name are stripped as
well, because leaving them out would reproduce the exact defect the item fixes: `addApproval`
(builds an entry directly, naming the model's own tool) and `addToolResult`'s orphan branch (the
one result path that never passes `enrichWithResult` — the item's What calls for that branch to be
covered, and stripping inside `enrichWithResult` alone would not do it). Package rule recorded as a
second invariant in `internal/tui/doc.go`.

**What:** Audit "High — Terminal-escape stripping is enforced per call site, and several
untrusted producers were missed". Move the discipline to the seams in `internal/tui`:
`stripEscapes` inside `addNote`/`addError` and inside `presentToolCall`/`enrichWithResult`
(target, summary, every detail line — covering the orphan-result branch), then wrap the
remaining non-transcript producers at their one function each: `toolActivityLabel`
(`activity.go`), the three `popupRow` builders (`autocomplete.go:350/:406/:426`), and the
resume-note/rebind-note producers (`sessions.go:370`, `model.go:1885`). Per-producer strip
calls that become redundant may be removed in the same change. The threat is concrete: an
unterminated OSC 8 opener in a file's first line or a tool-call argument turns the rest of
the frame into a clickable attacker link (ultraviolet honours OSC 8 across cells).

**Tests:** Extend `TestTranscriptStripsTerminalEscapes` (which today pins only the three
already-passing paths) to: tool-call target from model JSON, tool-result summary/detail
lines, `addError`, the skills note, the activity label, autocomplete rows, resume notes, and
the rebind note — the audit's verified vector (an OSC 52 payload through
`ToolCallEvent`/`ToolResultEvent`/`ErrorEvent` surviving `renderLines`) must fail before and
pass after.

**Acceptance:** `go test ./internal/tui/ -run 'StripsTerminalEscapes|Escape' -count=1`
passes; `make check` green.

**Commit:** `fix(tui): strip terminal escapes at the transcript and popup seams`

---

**Suggested version bump (not performed):** after this plan completes, a patch-level bump
(`v0.10.9`) is warranted — it is all fixes, several security-relevant; the owner decides
whether to fold it together with the companion correctness wave into one `v0.11.0`.
