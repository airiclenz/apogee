# Approved escape writes — the Gate's allow becomes executable

**Goal:** land ADR 0049 — an approved out-of-workspace write actually writes, bounded to exactly
the resolved target the approval pane disclosed, carried by a context write-escape permit; the
`confine=false` run cell and the `box.WritablePaths` union stop being nullified by the
workspace-pinned fence; the whole WS-write family behaves uniformly.

**Date:** 2026-08-14 · **Status:** unexecuted · **sized for:** ~200k-context host

**Authoritative sources** (an item that disagrees with these follows these):
- [ADR 0049](../adr/0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md)
  — the ratified decision, all seven calls.
- ADR 0012 — the ladder × blast radius; the Gate is the bound.
- `docs/design/confinement-execution-contract.md` §4 (the WS-write rows + the "decided, unbuilt"
  gap note), §10 (the `SubprocessPermit` idiom this permit mirrors).
- `ISSUES.md`, the `[P]` entry "an *approved* out-of-workspace write still errors at Execute".

**Ratified design calls (owner, 2026-08-14, grill session via AskUserQuestion):**

- **A over B:** honour the approval; strict fencing rejected (Q1).
- **Channel:** a context **write-escape permit** pinned to the classified `writeTarget.Real` —
  no permit ⇒ today's fence byte-for-byte; permit ⇒ exact-`Real` match only, written through an
  `os.Root` pinned at the target's parent, final component non-following (Q2/a).
- **Union semantics:** `WorkspaceRoot ∪ box.WritablePaths` is in-fence at *classification*
  (runs ungated in Allow-Edits/Auto); at *Execute* the union is honoured via the same permit,
  minted by dispatch for a WritablePaths-classified run — the fence itself keeps one rule (Q3/i,
  mechanism refined at plan-write).
- **Approval is final:** no hard-deny set; the dangerous-action floor stays a forced look and the
  yes runs, `~/.apogee` included (Q4/α).
- **Uniform family:** `write_file`, `file_edit`, `find_replace` (both), `file_ops` copy/move
  destination **and delete** honour the permit; `move_file`'s undisclosed **source** keeps its
  unconditional in-workspace refusal (Q5/I).
- **Cache grain unchanged:** allow-for-session stays tool + canonical-arguments digest; no path
  grain (Q6/x).
- **`confine=false` mints too:** the Auto · `confine=false` WS-write-out *run* verdict stamps the
  permit from dispatch's own classification — one mechanism, two minters (Q7/p).

**Standing requirements:** skills: `coding-standards`. `make check` once at closeout; per-item
acceptance below. No `VERSION`/CHANGELOG-release-heading change — close with a
VERSION-SUGGESTION line instead.

**Out of scope (deliberately):** a hard-deny tier above the Gate (rejected Q4); a path-grain
allow-for-session (rejected Q6, sighting-driven revisit); giving `ConfineWritablePaths` its first
writer (the parked Windows box-local `%TEMP%` entry); the parked security-matrix design; any
change to what the approval pane renders (the resolved-path disclosure already landed with the
hostile-bytes batch).

---

## 1. `domain`: the write-escape permit — ✅ DONE (2026-08-14)

NOTES (2026-08-14): getter named `WriteEscapePermitFrom` exactly as the item text specifies, not `...FromContext` as the two neighbouring seams (`SubprocessPermitFromContext`, `ConfinementFromContext`) are named — plan text taken as binding over local naming symmetry.

NOTES (2026-08-14): added beyond the item's three named tests — a key-distinctness test (write-escape vs SubprocessPermit vs Confinement, mirroring the existing `TestSubprocessPermitAndConfinementAreDistinctKeys`) and a revocation case pinning that an empty-`Real` permit installed over a granted one reads absent rather than inheriting the outer grant.

NOTES (2026-08-14): `internal/domain/doc.go`'s `confinement.go` map line gained the new carrier (repo convention: doc.go maps every non-test file's role; no new file was added, so the file count stands).

**What:** add `WriteEscapePermit{Real string}` beside `SubprocessPermit`, with
`WithWriteEscapePermit(ctx, p)` / `WriteEscapePermitFrom(ctx) (WriteEscapePermit, bool)`
mirroring the §10 idiom (unexported context key, zero-value-safe). Doc comment states the
contract: the permit authorises **one** resolved absolute path for the duration of one tool
execution; absence means the workspace fence alone governs. No consumer yet — this item is the
type and its plumbing only.

**Files:** `internal/domain/` (beside the existing permit; follow its file placement)

**Tests:** round-trip through a context; absent key reports `ok=false`; an empty-`Real` permit
is never returned as present.

**Acceptance:**
- `go test ./internal/domain/`

**Commit:** `feat(domain): a write-escape permit carries one approved resolved target`

## 2. `security`: the fence honours a permitted target — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the permitted target is a trailing `permitted string` parameter on the four mutating primitives rather than a set of sibling functions — one entry point per verb keeps the ADR's "the fence becomes one rule" shape and makes every call site answer the question at compile time, where a sibling would let a caller that needs a permit silently keep the failing behaviour ADR 0049 exists to fix. The single decision point is `openMutationRoot`, which every primitive pins its root through.

NOTES (2026-08-14): `SafeRename` deliberately takes NO permit, against the item's "the delete/copy helpers" reading of the family: one rename is one syscall through one pinned root, so it cannot span the workspace fence and an approved target outside it (the kernel refuses the cross-device move regardless). An approved escape MOVE is the copy-then-remove pair — `SafeCopyFileFrom` carries the permit to the destination, `SafeRemove` unlinks the in-workspace source — which is stated on `SafeRename`'s doc comment for item 4.

NOTES (2026-08-14): the new rule lives in `internal/security/writepermit.go` rather than growing `safeio.go` (677 lines, already past the ~400-line house limit), with `doc.go`'s file map extended for it — the package-map convention `docmap_test.go` enforces.

NOTES (2026-08-14): two of item 4's files were touched minimally so the tree keeps building under the new signatures — `internal/tools/path_safety.go` and `internal/tools/file_ops.go` pass `""` (no permit) at their five call sites, which is today's behaviour exactly; item 4 replaces those with the execution context's permit.

**What:** extend the shared TOCTOU-safe write core (`security.SafeWriteFile` and the delete/copy
helpers the family funnels through) with the permitted-target path: callers pass the permit's
`Real` (empty = none). Behaviour: no permit, or target within the workspace root → existing
os.Root path, unchanged. Permit present and the input re-resolves to **exactly** the permitted
`Real` → write through an `os.Root` opened at the deepest **existing** ancestor of `Real`,
creating missing parents and the final component non-following inside that root; any divergence
(re-resolution mismatch, symlinked final component) → error, no write. Keep the refusal message
naming both the resolved path and the rule, matching the fence's existing legibility style.

**Files:** `internal/security/` (the safe-write/path-safety core and its tests)

**Tests:** table-driven — permitted exact match writes (existing and missing parents); mismatch
refuses; symlinked final component refuses; workspace-internal writes bit-identical to today
(existing suites stay green); a permit never widens reads.

**Acceptance:**
- `go test ./internal/security/`

**Commit:** `feat(security): the safe-write fence honours one permitted resolved target`

## 3. `agent`: dispatch mints the permit at its three seams — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the dispatch method `writeTargetInWorkspace` became `classifyWriteTarget`, returning BOTH facts (in-fence bool, escape target) from ONE `tools.WorkspaceWriteTarget` resolution. The item's "`writeTargetInWorkspace` learns the union" is honoured on the `resolutionInput` FIELD, which keeps its name and its meaning-by-union; the method was renamed rather than joined by a sibling because a second accessor would resolve the same path a second time (EvalRealPath is the one I/O in this seam) and could hand resolve() two different answers about one call.

NOTES (2026-08-14): the escape target rides a Gate that the Tier-2 dangerous-action force produced, not only one the WS-write-out ladder row asked for — a forced look on that row is still that row's gate, and ADR 0049 Q4 ("approval is final … the yes runs") makes the human's yes to the disclosed path executable in both cases. Pinned by a table case.

NOTES (2026-08-14): tests beyond the item's six — the remembered allow-for-session case (the item's parenthetical "including a cache-cleared allow": the Approver is consulted once and the second, unprompted execution still carries the same target), a read-only negative control (a permit widens no read), and a pure-resolver table over the minting rule beside the dispatch-level end-to-end ones.

**What:** the resolution carries the classified `writeTarget.Real` for WS-write targets; the
execution tails stamp `WithWriteEscapePermit` before `executeTool` in exactly three cases: (1) an
**approved Gate** on the WS-write-out row (including a cache-cleared allow — same key, same
target); (2) the **Auto · `confine=false` run** verdict on that row; (3) a **WritablePaths
in-fence run** — classification treats `WorkspaceRoot ∪ box.WritablePaths` as in-workspace
(`writeTargetInWorkspace` learns the union), and a union member outside the workspace root gets
the permit so Execute can land it. No other verdict stamps; a denied or refused call never
reaches a permit.

**Files:** `internal/agent/resolution.go`, `internal/agent/dispatch.go`, tests beside them

**Tests:** dispatch-level — approved WS-write-out gate executes with permit and the tool sees it;
Ask-Before deny leaves no permit; `confine=false` run stamps; union-member target classifies
in-fence and stamps; in-workspace writes never carry one; a Firing's fail-safe denier still
refuses the gate (existing behaviour pinned).

**Acceptance:**
- `go test ./internal/agent/`

**Commit:** `feat(agent): approved and confine=false escape writes carry the permit`

## 4. `tools`: the WS-write family honours the permit uniformly — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the read-modify-write half of the family reads its target through the permit too — a new `readWriteTarget(ctx, …)` pins the read at the permitted target's own parent directory (where the write's own `os.Root` is pinned), replacing `safeReadFile` in `file_edit` and both `find_replace` verbs. Without it the item's required "permitted patch/find-replace succeeds" is unreachable: those verbs must see the bytes they rewrite, and the workspace-fenced read refuses an approved target. This is not the read widening ADR 0049 forbids — `security.SafeReadFile` still takes no permit, the READ tools still call `safeReadFile` and are handed none (pinned by `TestPermitWidensNoRead`), and the write re-resolves the argument against the same permitted path, so bytes and write can never part company.

NOTES (2026-08-14): the file-operation tools' pre-flight stats needed the same pin (`statWriteTarget`), or an approved copy/move/delete would die on a friendly-message check before reaching the permitted primitive. The DESTINATION half reads the permit; the SOURCE half deliberately does not (`checkFileOpsPathsFrom` keeps the plain workspace-rooted stat there) — that is what keeps `move_file`'s undisclosed source in-workspace. A permitted target whose parent does not exist yet reports ordinary absence rather than a fence refusal, so a copy to a missing directory still lands (`TestApprovedEscapeCreatesMissingParents`).

NOTES (2026-08-14): `MoveFile.move` now takes `ctx` and, under a permit, does NOT treat the rename's escape refusal as terminal — it falls through to the copy-then-remove pair item 2's `SafeRename` note prescribes (permit on the copy's destination, none on the source's removal). Unpermitted behaviour is unchanged: an escape refusal is still terminal, and a symlinked-parent refusal is terminal in both cases.

NOTES (2026-08-14): tests live in one new `internal/tools/write_permit_test.go` rather than scattered across the five tools' own suites — one table drives all eight verbs three ways (permit lands / mismatched permit refuses / no permit refuses as today), which is the uniformity the item is about; `doc.go`'s `path_safety.go` map line gained the permit clause (repo package-map convention).

**What:** thread the execution context's permit into the shared funnel — `safeWriteFile` (and
siblings) take `ctx` and pass the permit's `Real` to the security core — so `write_file`,
`file_edit` (patch + full), `find_replace` (single + multi) inherit it with no per-tool logic.
`file_ops`: copy/move **destination** and **delete** honour the permit through the same core;
`move_file`'s **source** keeps its unconditional in-workspace refusal, now pinned by a test
stating the ADR 0049 reason (undisclosed at the Gate). The success summary's resolved-target
note keeps saying the same thing the pane said.

**Files:** `internal/tools/path_safety.go`, `internal/tools/write_file.go`,
`internal/tools/file_edit.go`, `internal/tools/find_replace.go`, `internal/tools/file_ops.go`,
tests beside them

**Tests:** per-verb — a permitted out-of-workspace write/patch/find-replace/copy/move/delete
succeeds against a temp target and refuses on mismatch; unpermitted calls behave exactly as
today (existing suites green); the move-source refusal test.

**Acceptance:**
- `go test ./internal/tools/`

**Commit:** `feat(tools): the WS-write family executes a permitted escape`

## 5. Docs realisation + register close

**What:** contract §4's "decided, unbuilt" note becomes the landed description (drop the
unbuilt flag, keep the ADR 0049 pointer; update the `write_file.go` line reference); §10 gains
one sentence naming the write-escape permit as the second permit. Remove the `[P]` ISSUES.md
entry (this file holds open work only); `CHANGELOG.md` `[Unreleased]` records the close in one
entry. Close with a `VERSION-SUGGESTION:` line in the closeout report — no bump in this plan.

**Files:** `docs/design/confinement-execution-contract.md`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** none — docs only.

**Acceptance:**
- `grep -n "decided, unbuilt" docs/design/confinement-execution-contract.md` → no matches
- `grep -n "approved.*out-of-workspace write still errors" ISSUES.md` → no matches

**Commit:** `docs(confinement): the Gate's allow is executable — contract, register, changelog`

---

## Verification (whole plan)

- `make check`
- Manual: in Ask-Before, `write_file` to a path outside the workspace → approve → file exists
  with the disclosed content; deny → no file. In Auto (`confine=true`), same call gates; in a
  `confine=false` session it runs. An in-workspace session transcript is byte-identical to
  pre-plan behaviour.
