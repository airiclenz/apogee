# Close the two open ISSUES.md defects and slim the ledger's two record-heavy entries

**Goal:** empty the *Open defects* section of `ISSUES.md` (the undo journal's path-rule
contradiction; the untested `move_file` fallback) and relocate the two entries whose bodies are
completed-run records rather than open work (the security-Lows triage/closeout narration; the
tool-surface findings) into their authoritative documents, leaving pointers — per the file's own
convention that an entry is "the deferral trail's pointer, not the record".

**Date:** 2026-08-19 · **Status:** ready to execute · **sized for:** ~200k-context host

**Standing requirements:** skills: coding-standards. Any authorized deviation from item text must
land as a dated NOTES line under the item.

## Authoritative sources (ground truth — these win over any item text that disagrees)

- The two defect entries in `ISSUES.md` (currently at the top of *Open defects*; quote-match, do
  not trust line numbers).
- `internal/tools/path_safety.go:338–363` — `journalTarget` and its doc comment: the NAMED-path
  rule and the reason it is load-bearing (the fenced primitives relativise lexically against the
  workspace root; a resolved path would be refused as an escape on a host whose root sits behind
  a symlink, e.g. macOS `/tmp`). This comment is the intended-rule statement every doc fix
  follows.
- ADR 0049 (an approved escape's permit names the RESOLVED target — the one exception),
  ADR 0051 (`docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md`).
- `internal/tools/file_ops.go` — `move` and the doc comment on its copy-then-remove fallback
  (the split-failure contract: copy landed, removal refused → the destination alone is
  journaled).
- Pointer-entry precedent: the *Phase-4 mechanism catalogue* entry in `ISSUES.md` pointing at
  `docs/design/mechanism-catalogue.md` — the shape items 3 and 4 reproduce.
- `docs/reviews/archived/2026-08-11 - 01 - external-audit-triage.md` — the triage doc (note:
  the ISSUES.md entry's link is stale, missing the `archived/` segment).

## Ratified design calls

1. **The NAMED path is the intended journal rule; the docs follow the code** (owner, 2026-08-19,
   question round). An ordinary write's record identity is the argument's root-joined, cleaned
   spelling with nothing followed; only an approved escape records the permit's resolved target
   (ADR 0049). Rationale: `journalTarget`'s comment — a revert handed a resolved path would be
   refused by the lexical fence on a symlink-rooted host. No code behavior changes in this plan.

Author calls (plan author, 2026-08-19):

2. **Relocation boundary for the security-Lows entry:** the L1–L6 acceptance bodies STAY in
   `ISSUES.md` — they are the live deferral register the file exists for. Only the completed-run
   narration moves: the "Triage note, 2026-08-11" block and the "Closeout, 2026-08-12" block.
3. **Verbatim moves.** Relocated text moves unchanged apart from link-prefix fixes, under a dated
   heading naming its origin ("relocated 2026-08-19 from ISSUES.md"). No rewording, no
   summarising — the move must not alter the record.
4. **The tool-surface record's new home is `docs/design/tool-surface-findings.md`** (the
   mechanism-catalogue precedent: full record in `docs/design/`, pointer entry in ISSUES.md).
5. **The stale triage-doc link** in ISSUES.md gains its missing `archived/` segment as part of
   item 3.

## Out of scope (deliberate — do not re-file as gaps)

- Changing `journalTarget`'s behavior, or any code behavior at all (item 2 adds tests only).
- Relocating any other ISSUES.md entry — in particular the `Request.InjectContext` entry (its
  body is deliberate grill-prep) and the L1–L6 acceptance bodies (author call 2).
- Rewriting or re-triaging any relocated content.
- Version identifiers: docs and tests only — no bump suggested, none performed.

---

## 1. Align the four "resolved path" claims with the Named rule, close defect 1 — ✅ DONE (2026-08-19)

NOTES (2026-08-19): corrected a FIFTH site the item did not name — the `/undo` package header comment in `internal/tui/undo.go:19` ("discloses the RESOLVED path of every file the revert would touch"). It is the same false claim in a file the item already names, and defect 1's entry was removed in this item, so leaving it would have left the contradiction untracked.

**What:** Fix the four sites the defect entry names so they state the rule `journalTarget`
implements (ratified call 1), then remove the defect entry.

- `internal/undo/journal.go` — `Mutation`'s doc ("Path is the mutation's RESOLVED absolute
  path", near `:102`): state that Path is the record's identity — the argument's root-joined,
  cleaned spelling with nothing followed for an ordinary write; the permit's resolved target for
  an approved escape. Point at `journalTarget`'s comment as the rationale's home rather than
  duplicating the symlink argument.
- `internal/undo/journal.go` — `Preview`'s doc (near `:240`): same correction. Keep the
  disclosure sentence's intent: the preview shows the journal's recorded addresses — root-joined
  named paths, and for an escape the permit-pinned resolved path.
- `internal/tui/undo.go` — the listing comment ("the resolved path", near `:133`): same
  correction, one line.
- `docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md` — correct the two
  statements (the preview "always shows **resolved** paths"; "names every resolved path before
  anything moves") and add a dated **Amendment (2026-08-19)** note recording the correction:
  the journal records named paths, escapes record the permit's resolved target, and why (the
  lexical-fence rationale, ADR 0049's exception). An amendment note, never a silent rewrite.
- `ISSUES.md`: remove the whole defect entry ("The undo journal records the NAMED path…"),
  including its `---` separator, leaving the *Open defects* section well-formed.

**Files:** `internal/undo/journal.go`, `internal/tui/undo.go`,
`docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md`, `ISSUES.md`

**Tests:** none (comment and doc changes only); the build must stay green.

**Acceptance:**
`go build ./internal/undo ./internal/tui && ! grep -q "RESOLVED absolute path" internal/undo/journal.go && ! grep -q "always shows \*\*resolved\*\* paths" "docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md" && grep -q "Amendment (2026-08-19)" "docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md" && ! grep -q "records the NAMED path" ISSUES.md`

**Commit:** `docs(undo): state the named-path journal rule everywhere the docs claimed resolved`

---

## 2. Pin `move_file`'s copy-then-remove fallback in tests, close defect 2 — ✅ DONE (2026-08-19)

NOTES (2026-08-19): both deliverables landed in `internal/tools/undo_journal_test.go` and `internal/tools/file_ops_test.go` was left untouched — each test's binding assertion is the journal's record shape, which is that file's stated subject, and splitting one scenario's fixture across two files would have duplicated it.

NOTES (2026-08-19): the split failure is induced by a filesystem arrangement (the source's parent directory chmod'd 0555 so the unlink is refused), the item's explicitly-allowed mechanical choice; it skips on Windows and when running as root, where the write bit does not bind.

NOTES (2026-08-19): the fallback calls `security.SafeCopyFile`, not `SafeCopyFileFrom` as the item's text says — `SafeCopyFile` is that function's equal-roots wrapper, so the route is the one the item names.

**What:** Add the two missing tests the defect entry names, both driving the fallback route in
`move` (`internal/tools/file_ops.go`) — the route an approved escape (ADR 0049) makes the real
one. The permitted-escape harness that `file_ops_test.go`'s existing refusal test builds around
is the starting point.

- **Fallback lands successfully:** a permitted escape move that cannot take the `SafeRename`
  fast path completes via `SafeCopyFileFrom` + `SafeRemove` and journals **identically** to the
  rename path (the undo-plan item-3 contract): a source record (pre-image bytes, post-absent)
  and a destination record.
- **Split failure:** the copy lands but the removal is refused — per the fallback's own doc
  comment the journal must record the **destination alone**, no source record. How the refusal
  is induced is the implementer's mechanical choice (test-only seam or filesystem arrangement);
  the asserted journal shape is the binding part.
- `ISSUES.md`: remove the whole defect entry ("No test drives `move_file`'s copy-then-remove
  fallback route…"), including its `---` separator. If this item runs after item 1, the *Open
  defects* section is then empty — leave the section heading in place (the file's structure
  keeps both sections).

**Files:** `internal/tools/undo_journal_test.go`, `internal/tools/file_ops_test.go`, `ISSUES.md`

**Tests:** the two new tests above are the deliverable.

**Acceptance:**
`go test ./internal/tools -count=1 && ! grep -q "copy-then-remove fallback route" ISSUES.md`

**Commit:** `test(tools): drive move_file's copy-then-remove fallback and its split-failure journal shape`

---

## 3. Relocate the security-Lows triage and closeout narration into the triage review doc — ✅ DONE (2026-08-19)

NOTES (2026-08-19): the relocated triage note's self-citation of the triage doc was kept verbatim with only its `archived/` prefix fixed (author call 3 forbids rewording), so it now reads as a self-reference inside its new home.

**What:** Move the two completed-run narration blocks out of the *Deferred security-review Lows*
entry in `ISSUES.md`, verbatim (author calls 2 and 3), into
`docs/reviews/archived/2026-08-11 - 01 - external-audit-triage.md`:

- the **"Triage note, 2026-08-11"** block — from that bold heading through the three L2/L3/L4
  bullets and the closing "exclusion buckets" paragraph;
- the **"Closeout, 2026-08-12"** block — the "what the batch deliberately left" paragraph.

Append both to the review doc under one dated heading, e.g.
`## Addendum (relocated 2026-08-19 from ISSUES.md)`, preserving their own dated headings and
fixing any relative links for the new location. In `ISSUES.md`, replace the two blocks with a
pointer of at most five lines stating: the 2026-08-11 triage checked all 14 audit findings
against L2/L3/L4 and none dies on them; the batch's 2026-08-12 closeout and its two documented
residuals; full reasoning in the triage doc — cited with the CORRECT path
(`docs/reviews/archived/…`, author call 5). The L1–L6 bodies above the pointer stay untouched
(author call 2).

**Files:** `ISSUES.md`, `docs/reviews/archived/2026-08-11 - 01 - external-audit-triage.md`

**Tests:** none (docs).

**Acceptance:**
`grep -q "None of the fourteen dies on them" "docs/reviews/archived/2026-08-11 - 01 - external-audit-triage.md" && ! grep -q "None of the fourteen dies on them" ISSUES.md && grep -q "docs/reviews/archived/2026-08-11 - 01 - external-audit-triage.md" ISSUES.md`

**Commit:** `docs(issues): move the audit triage and closeout narration into the triage review doc`

---

## 4. Relocate the tool-surface findings record to a design doc, leave the pointer

**What:** Create `docs/design/tool-surface-findings.md` (author call 4) carrying the *Tool-surface
findings (4-poll round, 2026-08-10)* entry's full body verbatim (author call 3): both poll
rounds, bench arms (a)–(f) with their updates, the grill topics, the deferred candidates, the
engine-level notes, the denials with reasons, the method lessons, and the 2026-08-16 owner
framing. Open with a short provenance note (recorded 2026-08-10 / 2026-08-16; relocated
2026-08-19 from ISSUES.md) and fix relative ADR links for the `docs/design/` location.

Replace the ISSUES.md entry with a pointer entry in the mechanism-catalogue shape, keeping just
enough that nothing is re-derived (the file's own conventions require this): the status line;
the pointer to the new doc; a one-line-per-item list of the LIVE gates only — the six bench arms
by letter and subject, arm (c)'s watch-item status, the two grill topics (per-profile rosters —
promoted 2026-08-16 — and the unified `git` tool), the PTY-session grill, and the standing rule
that nothing leaves the roster on poll evidence alone. Denials, method lessons, deferred
candidates, and the second-round narrative live only in the design doc. Target: the pointer
entry stays under ~15 lines.

**Files:** `ISSUES.md`, `docs/design/tool-surface-findings.md`

**Tests:** none (docs).

**Acceptance:**
`test -f docs/design/tool-surface-findings.md && grep -q "Models discover capabilities by tool" docs/design/tool-surface-findings.md && ! grep -q "Models discover capabilities by tool" ISSUES.md && grep -q "tool-surface-findings.md" ISSUES.md`

**Commit:** `docs(issues): move the tool-surface poll record to docs/design, leave the pointer entry`

---

## Note on execution order

The items are independent, but all four touch `ISSUES.md` — run them serially (their `Files:`
sets overlap by design, so no wave qualifies). No version bump is suggested: tests and
documentation only.
