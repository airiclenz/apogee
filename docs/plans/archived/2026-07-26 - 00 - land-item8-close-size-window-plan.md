# Plan — Land item 8 honestly, then make its claim true: one pinned handle from size bound to read

**Date:** 2026-07-26
**Status:** complete (investigation completed 2026-07-26 against the working tree; the payload it
lands is the uncommitted item-8 implementation already in the tree, verified green today —
`gofmt -l` clean, `go vet` clean, `go test ./internal/tools/... ./internal/security/...` pass).
**Source:** item 8 of `docs/plans/2026-07-25 - 03 - architecture-review-closeout-plan.md` —
attempted twice, failed verification both times on over-claiming doc sentences, then SKIPPED by
the owner with the remedy named in its status record ("scope the sentence to path resolution …
a real fix for the window itself would be ONE handle for both steps … decide that before
re-opening the item"). This plan is that decision, taken both ways in sequence: the honest
sentences land the item first; the one-handle fix then makes the strong claim true.
**Track:** post-`v0.8.0` architecture deepening close-out (the 2026-07-24 review's last open card).
**Public API:** none. Every touched Go file is `internal/`; `apogee.go` is untouched; no exported
name changes anywhere. The CHANGELOG `Unreleased → Security` bullet is user-visible prose, not
surface. No `VERSION` implication from this plan — the parent plan's minor-bump question (its
verification step 8) remains the owner's call and is recorded, not taken, in item 3.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`, no
PRs (owner directive — this deliberately overrides the version-control standard's "never commit
directly to main" rule).

Per-item green gate:

```
gofmt -l .                                              # empty
go vet ./... && go test ./... && go test -race ./...
GOOS=windows go build ./... && GOOS=darwin go build ./...
```

**Items 1 → 2 → 3 are a strict chain.** Item 2 rewrites sentences item 1 lands; item 3 records
both commits. Stopping after item 1 leaves a coherent tree (item 8 is landed and the parent plan
can close); item 2 is droppable on its own (D1), and item 3 says how its wording adapts if it is.

---

## The problem (investigated 2026-07-26 against the working tree)

Item 8's two failed attempts share one failure mode: **sentences that claim more than the code
does.** The code half is right — the TOCTOU **path-resolution** hole is genuinely closed, the
racing component-swap probes measured 0 escapes after against thousands before, and the whole
uncommitted payload (`read_file.go`, `open_file.go`, `path_safety.go`, both test files, the
CHANGELOG bullet) is green today. What is false is the claim that the **size bound** shares that
guarantee: `security.SafeStat` and `security.SafeReadFile` each open their **own** `os.Root`, so
an in-root name flipped between the two calls is stat'd as one file and read as another — an
11 MB read past `maxFileReadBytes` was reproduced, and `internal/security/safeio.go`'s own SCOPE
note (landed in `f45a30a`) documents the window with its measurement. Four sentences in the
uncommitted tree still assert the opposite:

1. `CHANGELOG.md` L176–177 — "the size check and the read that follows it resolve against one
   pinned root, so there is no window between them".
2. `internal/tools/read_file.go` L54–55 — "The stat that bounds the file by size and the read
   that materialises it resolve against the same pinned root, so there is no check/use gap
   between them."
3. `internal/tools/open_file.go` L57–58 — the identical sentence.
4. **Found this investigation, not in the parent's status record:** `internal/tools/path_safety.go`
   L44–47 — `safeStat`'s alias comment says "the same os.Root the read then uses … keeping the
   check and the use on one pinned root". Same falsehood, fourth location (D2).

Two more facts scoped the remedy:

- **The stat-then-read pair has exactly three production call sites:** the two read tools, plus
  `internal/agent/loop.go` `readFileRef` (L659–666, the @ref reader) — which already carries the
  *honest* version of the sentence ("the pair is not race-free … the cap binds ordinary oversized
  refs"). The one-handle fix (`os.Root.Open` is stdlib; `go.mod` says Go 1.26) covers all three,
  after which `security.SafeStat` has **zero** callers — and it has zero direct tests today
  (`safeio_test.go` tests Write/Read only), so deleting it orphans nothing (D4).
- **`internal/skills/load.go`** (L19–21, `readBounded` L138–150) cites read_file's
  "stat-or-limit-before-materialize discipline" as its model — the citation the parent plan's nit
  list says is worth a word if item 8 is revived. After item 2, `readBounded`'s
  open-then-`io.LimitReader` shape genuinely **is** read_file's discipline; the citation gets its
  word there.

---

## Decisions (taken 2026-07-26, from the parent's status record and the owner's standing directives)

- **D1 — two commits, honest first.** Item 1 executes the owner's post-attempt-1 directive ("fix
  the claims, keep the tightening") literally: the four sentences are scoped to what holds, and
  the twice-failed item lands with **no behaviour change at all**. Item 2 then closes the size
  window itself, which is the change to `internal/security`'s caller contract the status record
  says to decide before re-opening — decided **yes** here, as its own reviewable, droppable
  commit, per the owner's standing "best long-term architecture" directive. Rejected: one merged
  commit (it would re-widen exactly the claim-set that failed verification twice), and
  sentence-fix-only with no item 2 (leaves a documented-open window whose fix is one small
  primitive).
- **D2 — the fourth sentence is in scope.** The parent's status record lists three over-claims;
  `path_safety.go`'s `safeStat` comment is the fourth. Item 1 fixes it with the others (and item 2
  deletes the alias outright with `SafeStat`).
- **D3 — the one-handle primitive is `SafeOpen`, returning the handle; policy stays with callers.**
  `security.SafeOpen(root, input string) (*os.File, error)` does `rootRelative → os.OpenRoot →
  r.Open(rel) → mapRootEscape` and nothing else: the security package owns the **fence**, not file
  policy — the directory check, the size cap and the model-facing messages differ per caller and
  stay theirs. The two read tools share one package-local bounded-read body in `path_safety.go`
  (the item-7 `pathArgWriteTarget` pattern: twins keep their seams, the body exists once);
  `loop.go`'s @ref reader inlines its own bound (different cap constant, returns `error` for an
  ErrorEvent rather than a model-facing message string). Rejected: a
  `SafeReadFileBounded(root, input, max)` in `security` — it would pull "what is a directory
  worth refusing" and "how is too-large worded" into the fence package.
- **D4 — `SafeStat` is deleted, not kept.** After item 2 it has no callers and no direct tests,
  and its doc comment's primary use-pattern (stat-then-read) is precisely the misuse the SCOPE
  note warns against — an unused API that exists mainly as check-then-act bait. `internal/`
  packages carry no compatibility duty (ADR 0010 governs the facade only) and git history keeps
  the code. Rejected: keeping it for hypothetical future metadata needs — speculative generality.
- **D5 — the growth backstop is fstat-first, then a hard `LimitReader` bound.** The open
  descriptor pins *identity*, not *size* — a regular file can grow after the fstat passes (an
  appender, not a renamer). So: fstat the opened `*os.File` first (refusing a directory and an
  over-cap size with today's exact messages), then read through `io.ReadAll(io.LimitReader(f,
  cap+1))`; if that yields more than `cap` bytes the file grew past the bound mid-call — re-fstat
  the **same fd** and render the same `file too large: %d bytes (max %d)` shape with the fresh
  size. Net: no caller ever materialises more than `cap+1` bytes, by construction, under any
  interleaving — the `max+1` idiom `skills/load.go` `readBounded` already uses.
- **D6 — message deltas, enumerated up front** (parity over-claims are what killed attempts 1
  and 2). **Item 1 changes no behaviour and no message — prose only.** Item 2 changes exactly
  two rendered texts: (i) a symlink-shaped escape's detail after the uniform
  `security: path resolves outside the workspace root:` prefix becomes
  `openat <path>: path escapes from parent` where it was `statat <path>: …` — the escape now
  surfaces at open, not stat; the CHANGELOG bullet's quoted example (L188) updates in the same
  commit; (ii) the @ref reader's ErrorEvent, which surfaces the raw error, changes the same
  `statat` → `openat` op word. Everything else is byte-identical: success output, `file not
  found: <path>`, `not a file: <path>`, `file too large: %d bytes (max %d)` for a statically
  oversized file, and traversal / out-of-root absolute paths (caught before any fd is opened,
  unchanged). The one genuinely **new** refusal — a file grown past the cap mid-call — reuses the
  file-too-large shape (D5). No test today pins the `statat` spelling anywhere (verified:
  `grep -rn "statat" --include="*.go"` hits nothing).

---

## Explicit non-goals

- **The write tools' read-modify-write window** (`safeReadFile` → `safeWriteFile` in
  `find_replace.go` / `file_edit.go`). That is an atomicity question between two *operations*,
  not a bounding question inside one read; nothing in this plan's mechanism addresses it, and
  no sentence anywhere claims it is closed. Stays as-is.
- **Bounding `SafeReadFile` itself.** Its remaining callers read files they are about to rewrite
  whole; its doc already states it applies no bound. A caller needing a bound now has `SafeOpen`.
- **The `VERSION` bump** (parent verification step 8) — owner call; item 3 records it as
  outstanding, nothing more.
- **The two remaining nit-grade findings** from the parent's list that this plan does not touch
  (`toolsummary.go`'s `Total` comment; `workspace_scoped_test.go`'s top-level-fields /
  `DefaultTools` notes). They stay recorded in the parent. The other two nits (the safeio
  `849 of 4000` figure; the `skills/load.go` citation) die naturally in item 2 and item 3 says so.
- **The review's parked items** — the `/code-audit` on the live url-safety gap and the un-grilled
  `Request.InjectContext` — stay parked, named in the rewritten "Recommended next step".
- **Regenerating `docs/reviews/2026-07-24 - 00 - architecture-deepening-review.html`** — a dated
  render of the original review; the ledger rewrite touches the `.md` only.
- **The untracked spinner plan** (`2026-07-25 - 04 - spinner-redesign-plan.md`) is someone else's
  in-flight work: no item commits it.

---

## 1. Land item 8 — scope the four sentences to what holds — ✅ DONE (2026-07-26)

**The working tree already contains the payload.** The six modified files
(`internal/tools/read_file.go`, `open_file.go`, `path_safety.go`, `read_file_test.go`,
`open_file_test.go`, `CHANGELOG.md`) are the twice-verified item-8 implementation — do **not**
revert, stash, regenerate or re-derive any of it. This item's only edits are the four sentence
fixes below; every other uncommitted line is kept byte-for-byte and committed.

**What:**

- **`internal/tools/read_file.go` L51–55** — the second paragraph of `Execute`'s doc keeps its
  first sentence (fence at STAT and READ time, concurrent swap refused, security review H1) and
  replaces the "same pinned root … no check/use gap" sentence with the honest scope, in the same
  voice `loop.go`'s `readFileRef` and safeio's SCOPE note already use:

  ```
  Path RESOLUTION is what the pinned roots make window-free: the stat and the read each
  open their own root, so the size bound still has a check/use window between the two
  calls — it refuses ordinary oversized files, not a name flipped mid-call by an
  adversary who can rename inside the workspace (see the SCOPE note in
  internal/security/safeio.go).
  ```
- **`internal/tools/open_file.go` L54–58** — the identical replacement.
- **`internal/tools/path_safety.go` L44–47** — `safeStat`'s comment drops "the same os.Root the
  read then uses" and "the check and the use on one pinned root"; it now says the fence is
  enforced at STAT time (os.Root-pinned), that the metadata describes what the name resolved to
  **at stat time only** — the read that follows opens its own root — and that a size bound
  decided from this stat binds ordinary oversized files, not a name flipped between the calls.
- **`CHANGELOG.md` L176–178** — in the Security bullet, the clause "the size check and the read
  that follows it resolve against one pinned root, so there is no window between them, and the
  same loop now returns the outside file **zero** times" becomes: path resolution is pinned for
  both the size check and the read, and the same racing loop now returns the outside file
  **zero** times — followed by one honest parenthetical: the size bound itself is still decided
  from a separate stat and keeps a check/use window, so it refuses ordinary oversized files, not
  a file swapped mid-call by an adversary who can rename inside the workspace.
- No other file changes. In particular the two test files and the tighten-only symlink paragraphs
  in both tools' docs are untouched.

**Tests:** none new — this item is prose plus the already-written payload. The full gate proves
the payload is what the two attempts verified: all pre-existing escape/missing/dir/oversize cases
unchanged, plus the payload's own `RefusesEscapingSymlink`, `RefusesComponentSwappedMidRead`,
`RefusesAbsoluteInRootSymlink` and `ReadsRelativeInRootSymlink` on both tools.

**Acceptance:** gates green;
`grep -rn "no check/use gap between them" internal/tools/` → empty;
`grep -n "no window between them" CHANGELOG.md` → empty;
`grep -n "one pinned root\|os.Root the read then uses" internal/tools/path_safety.go` → empty;
`grep -c "ordinary oversized files" internal/tools/read_file.go internal/tools/open_file.go
internal/tools/path_safety.go CHANGELOG.md` → ≥1 in each (the honest scope is stated, not just
the false claim deleted). The commit contains exactly the six payload files — not the plan docs,
not the spinner plan. Commit: `refactor(tools): read_file and open_file read through the fence`
(body: lands the twice-attempted item 8 with its claims scoped to path resolution; the symlink
narrowing and message deltas are recorded in the CHANGELOG bullet).

---

## 2. One pinned handle from the size bound to the read — `security.SafeOpen` — ✅ DONE (2026-07-26)

NOTES (2026-07-26): `readAllBounded` carries a third `error` return (a mid-read failure must
propagate; the item authorises implementer-chosen signatures). The growth-backstop refusal is not
deterministically reachable through `readWorkspaceFileBounded` (no seam between its fstat and its
read to interleave a writer), so the item's authorised else-branch was taken: the bound-step table
plus a direct fstat-oversize case on the shared body (`TestReadWorkspaceFileBounded_RefusesOversizeFile`).

The real fix the parent's status record names: ONE handle for both steps, so the sentences item 1
weakened become true in their strong form. Closes the window at all **three** pair call sites.

**What:**

- **`internal/security/safeio.go`** — new `SafeOpen(root, input string) (*os.File, error)` beside
  `SafeReadFile`: `rootRelative` → `os.OpenRoot` → `r.Open(rel)` → `mapRootEscape`, root closed
  before return (a file opened through an `os.Root` stays valid after the root closes). Doc: the
  fence is enforced at OPEN time; the returned handle **pins the file's identity** — what is
  statted and read through it is the file that was opened, regardless of renames after; the
  caller owns `Close` and any size policy. **Delete `SafeStat`** (D4) and drop the
  "from an earlier SafeStat" cross-reference in `SafeReadFile`'s doc (L93). **Rewrite the SCOPE
  paragraph** (L30–42): what is closed is path resolution per call *and*, for bounded reads
  through `SafeOpen`, the size bound's check/use gap too — the caller fstats the very descriptor
  it then reads through an `io.LimitReader`, so no more than cap+1 bytes are ever materialised;
  `SafeReadFile` remains unbounded by contract and a caller needing a bound must use `SafeOpen`;
  a bound decided from a separate stat call is not a defence against an in-workspace renamer —
  the removed stat-then-read pattern had exactly that window, measured and probe-dependent (the
  `849 of 4000` figure goes with the paragraph, which retires that nit). Reconcile every other
  stat-then-read mention: `grep -rn "stat-then-read\|stat-or-limit" internal/ --include="*.go"`
  and update each hit to the one-handle reality.
- **`internal/tools/path_safety.go`** — the `safeStat` alias is **deleted** with its primitive.
  In its place: `safeOpen` (alias, same one-line style as `safeReadFile`) and the twins' one
  shared body plus its testable bound step:

  ```go
  // readWorkspaceFileBounded reads path within root through ONE pinned handle: open through
  // the fence, fstat the opened descriptor, refuse a directory or an over-cap size, then
  // read through a limit bounded to maxFileReadBytes+1 — the size check and the read cannot
  // disagree about which file they describe, and no more than the cap+1 bytes are ever
  // materialised even if the file grows mid-call (D5). On failure the second return is the
  // model-facing message, rendered exactly as the twins rendered it before.
  func readWorkspaceFileBounded(path, root string) ([]byte, string)

  // readAllBounded reads at most max bytes from r, reporting false when r holds more —
  // the max+1 idiom skills/load.go readBounded uses, here as its own step so the growth
  // backstop is table-testable without interleaving a writer.
  func readAllBounded(r io.Reader, max int64) ([]byte, bool)
  ```

  The exact signatures are the implementer's (a small result struct is equally acceptable);
  what is fixed: one shared body, the bound step independently testable, and byte-identical
  messages — `not a file: <path>`, `file too large: %d bytes (max %d)` (fstat refusal, and the
  growth-backstop refusal via re-fstat of the same fd), escape → `err.Error()`, anything else →
  `file not found: <path>` via the existing `readFileErrorMessage`.
- **`internal/tools/read_file.go` / `open_file.go`** — each `Execute` swaps its
  `safeStat → IsDir → Size → safeReadFile` block for one `readWorkspaceFileBounded` call. The
  doc sentence item 1 weakened returns in its strong form, **now true**: the open, the size
  check and the read share one descriptor, so there is no check/use gap — a rename mid-call
  changes nothing the call sees, and a file grown past the cap mid-read is refused. The
  tighten-only symlink paragraphs stay.
- **`internal/agent/loop.go` `readFileRef` (L647–671)** — `security.SafeOpen` → fstat →
  `maxRefFileBytes` check (message unchanged) → inline `io.ReadAll(io.LimitReader(f,
  maxRefFileBytes+1))` with the same over-bound refusal; doc comment's "the pair is not
  race-free" sentence (L652–654) replaced by the one-handle truth. `resolveFileRefs`' doc
  (L620–622) re-pointed: it no longer "reuses security.SafeReadFile".
- **`internal/skills/load.go`** — L21's citation gets its word: read_file now opens, fstats and
  limit-reads through one handle, so `readBounded` genuinely mirrors it (was: cited a two-handle
  stat-then-read as if it were this discipline).
- **`CHANGELOG.md`** — the same Unreleased Security bullet updates to the final truth: the size
  check and the read now share one pinned handle (no window — the claim is now made *because it
  became true*, replacing item 1's parenthetical); a file grown past the cap mid-call is
  refused; the quoted escape example's `statat` becomes `openat` (D6); one added line notes the
  @file-reference reader shares the mechanism and its raw error text changed the same way.

**Tests:**

- **`internal/security/safeio_test.go`** — `SafeStat`'s absence needs no test edits (it had
  none). New, all deterministic: `TestSafeOpen_ReadsWithinRoot`;
  `TestSafeOpen_RejectsTraversal` (plus absolute-outside);
  `TestSafeOpen_RefusesEscapingSymlink` (skip-guarded like the existing symlink cases);
  `TestSafeOpen_HandleSurvivesRename` — open A, rename B over A's name, read the handle →
  **A's** content. That last test is the identity pin: together with the bound-step table it is
  the deterministic proof the old racy probes could only make statistically, and its doc comment
  says so.
- **`internal/tools`** — a table test for `readAllBounded` (under cap / exactly cap / cap+1);
  one case per twin driving the growth-backstop refusal through `readWorkspaceFileBounded` if
  reachable, else the bound-step table plus the fstat cases cover both branches — the item-8
  payload's swap/symlink/parity tests and every pre-existing case must pass **unchanged** (that
  is the message-parity oracle; no existing assertion may be edited).
- **`internal/agent`** — the existing @ref tests (`TestReadFileRefRefusesOversizeRef` with its
  sparse over-cap file, `TestResolveFileRefsInjectsContent`, `…MissingRefEmitsErrorAndProceeds`)
  pass unchanged.

**Acceptance:** gates green; `grep -rn "SafeStat" --include="*.go" .` → empty;
`grep -rn "stat-then-read\|stat-or-limit" internal/ --include="*.go"` → empty;
`grep -n "io.LimitReader" internal/tools/path_safety.go internal/agent/loop.go` → ≥1 each;
`grep -n "safeStat\|safeReadFile" internal/tools/read_file.go internal/tools/open_file.go` →
empty (the twins call the shared body; `safeReadFile` itself stays, for the write tools);
`grep -n "statat" CHANGELOG.md` → empty and `grep -n "openat" CHANGELOG.md` → ≥1; no
pre-existing test assertion edited (check the diff). Commit:
`fix(security,tools): the size bound and the read share one handle` (body: names the @ref
reader, the `SafeStat` deletion, and the two `statat`→`openat` message deltas).

---

## 3. Close the parent plan and the 2026-07-24 review — ✅ DONE (2026-07-26)

NOTES (2026-07-26): the parent's execution-state record was already committed as `704bd8c` before
this run, so this commit carries only the close-out edits made here — the "until-now-uncommitted
execution-state record" clause is moot. Beyond the named sections, stale mid-run lines that would
have contradicted the close were dated-closed rather than left standing: the parent's header
**Status:** line, its "Nine of the ten items landed" table intro, and a superseded-pointer before
its verification preamble; and the review's title line and "Ledger" paragraph — the latter two
deferred to this rewrite by the parent's item 5 NOTES (b). Item 2 was NOT dropped, so the
bracketed clauses were used.

Docs only. This is the parent's own close-out (its verification steps), runnable at last because
item 8 is landed. **If the owner dropped item 2**, everything below still runs — the ledger and
CHANGELOG then keep item 1's honest-window wording and the safeio SCOPE nit stays open; the
bracketed clauses are the only wording that assumes item 2.

**What:**

- **`docs/plans/2026-07-25 - 03 - architecture-review-closeout-plan.md`** — flip item 8's
  heading to `✅ DONE (2026-07-26)` and append a short dated resolution note to its status
  record (do not rewrite the history): the four sentences — the record's three plus
  `path_safety.go`'s `safeStat` comment — were scoped to path resolution and the item landed in
  item 1's commit [; the window itself was then closed by one pinned handle in item 2's commit,
  the "real fix" the record named]. Fill the execution-state table's item-8 row; update the
  "Not archived, and what is still open" section to say the plan is now closing; annotate the
  nit list: the safeio `849` figure and the `load.go` citation [resolved by item 2's commit],
  the other two nits remain open on the record.
- **Run the parent's whole-plan verification** and append a dated results block to its
  verification section: steps 1–7 and 9 (step 6's grep now passes on `main`; step 9's per-package
  line deltas reported straight); step 8 recorded as **outstanding owner call** — `VERSION` still
  reads `v0.8.4`, the additive surface of parent items 1–5 rides the next release unless the
  owner bumps earlier; nothing decided here. Step 10 is the next bullet.
- **`docs/reviews/2026-07-24 - 00 - architecture-deepening-review.md`** — the ledger rewrite:
  candidate 06 gets its ✅ LANDED note (`6c1f458`, `foldEvent`) in the house card style; the four
  smaller deepenings get theirs (`workspaceWriteTarget` → `13681f4` + follow-up `c218670`;
  `read_file`/`open_file` → this plan's commits, noting the D9 widening to `open_file` [and the
  one-handle follow-through]; argv-wrap → `f013d4d`; self-regulator read model → `b9578a0`).
  "State of the tree" and "Recommended next step" get a dated close: the ledger is empty; the
  only parked items are the `/code-audit` on the live url-safety gap and the un-grilled
  `Request.InjectContext`. The `.html` sibling stays untouched (non-goal).
- **Archive the parent:** `git mv` it into `docs/plans/archived/`.
- This commit also carries the parent-plan doc's until-now-uncommitted execution-state record and
  this plan file itself (with its item markers) — the docs that describe the run belong to the
  run. The untracked spinner plan is **not** committed.

**Tests:** none — `.md` files only; the gate run stays green trivially.

**Acceptance:** parent plan exists under `docs/plans/archived/` and not under `docs/plans/`;
`grep -n "PLANNED (not yet built)" "docs/reviews/2026-07-24 - 00 - architecture-deepening-review.md"`
→ empty; the review carries ✅ notes for 06 and all four deepenings; the parent records the
step-8 owner call as outstanding; `git status --short` afterwards shows nothing but this plan's
own in-progress marker edits (if any) and the untracked spinner plan. Commit:
`docs(plans,reviews): item 8 lands and the 2026-07-24 review closes`.

---

## Whole-plan verification (run after item 3, before declaring done)

1. **Full gate**, all five commands.
2. **The claims are true now:** `grep -rn "SafeStat\|stat-then-read" internal/ --include="*.go"`
   → empty; `grep -n "statat" CHANGELOG.md` → empty;
   `grep -n "io.LimitReader" internal/tools/path_safety.go internal/agent/loop.go` → ≥1 each.
   (Skip this step's first two greps if the owner dropped item 2, and check instead that the
   honest-window wording from item 1 is intact.)
3. **The parent's step 6 passes on `main` at last:**
   `grep -n "os.Stat\|os.ReadFile" internal/tools/read_file.go internal/tools/open_file.go` →
   empty, on a clean checkout of HEAD.
4. **The review ledger is empty** (item 3's greps) and the parent plan is archived.
5. **The tree is clean** apart from the untracked spinner plan; then archive **this** plan under
   `docs/plans/archived/`. Commit: `docs(plans): archive the item-8 landing plan`.
6. **Report the outstanding owner items straight** in the final summary — nothing in this plan
   discharges them: the `VERSION` minor-bump call (parent step 8), and the parent's manual TUI
   verification (drive one Turn using all seven summary-bearing tools; confirm the cards and the
   status line).

## Manual verification (owner — the automated suite cannot do this)

Unchanged from the parent plan (its last section), plus one small addition from item 2: in a live
session, `read_file` a file larger than 10 MB once and confirm the refusal message reads exactly
as before (`file too large: <n> bytes (max 10485760)`).
