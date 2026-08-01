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

## 3. Terminate the ref position in `git_branch` switch/create

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

## 4. Persist the Library store atomically

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

## 5. Strip the harmony open token from an unterminated tail

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

## 6. Fence workspace context-file reads

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

## 7. `grep` opens walked files through the fence

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

## 8. Pin `present_document`, `diagnostics` and `list_dir` to the root

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

## 9. Bound and fence `view_diff`

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

## 10. Validate session-record ids as filenames

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

## 11. Re-fence the document server per request and bound it

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

## 12. Derive landlock access masks from the probed ABI

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

## 13. Never label hard-linked files in the low-integrity walk

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

## 14. A project rule add can no longer dissolve a shipped rule

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

## 15. Strip terminal escapes at the transcript and popup seams

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
