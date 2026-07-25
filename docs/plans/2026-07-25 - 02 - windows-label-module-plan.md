# Plan — Split the Windows Confiner: the label mechanism becomes a module

**Date:** 2026-07-25
**Status:** READY (shape resolved with the owner 2026-07-25 — **a new package**, not a file split).
Runs in `internal/platform`, the new `internal/platform/winlabel`, and the docs
(`internal/platform/doc.go`, `docs/design/technical-design.md`, `TODO.md`).
**No public API change.** Everything moved is `internal/`; `apogee probe host`, the TUI, the
CHANGELOG and every exported root alias are untouched. `cmd/apogee` does not change a line.
**Source:** candidate **05** of `docs/reviews/2026-07-24 - 00 - architecture-deepening-review.md`
("Split the Windows Confiner into three deep sub-modules", rated *Worth exploring ·
owner-flagged*), and the `TODO.md` entry *"`internal/platform`'s two Windows confinement files
exceed the file-size guideline"* it points at. Candidates 01, 02, 04 and 07 landed 2026-07-24/25.
**Track:** post-`v0.8.0` architecture deepening — the fourth card off the 2026-07-24 review.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`, no
PRs.

**⚠️ The gate is different for this plan.** Roughly half the code being moved is
`//go:build windows`, so `go test ./...` on the dev machine **does not compile it, let alone run
it**. Every item's gate therefore adds cross-compilation, and the plan ends with a **manual
Windows run** (verification step 4) that only the owner can perform. An item that is green on
Linux is *not* done until its `GOOS=windows` gate is green too.

Per-item green gate, all six commands:

```
gofmt -l .                                             # empty
go vet ./... && go test ./... && go test -race ./...    # the untagged half, run
GOOS=windows go build ./... && GOOS=darwin go build ./...
GOOS=windows go vet ./internal/platform/...             # compiles the tagged half + its tests
GOOS=windows go test -c -o /dev/null ./internal/platform
GOOS=windows go test -c -o /dev/null ./internal/platform/winlabel
```

---

## The problem (grounded, verified 2026-07-25)

**1. Two files past the size at which a file stops being navigable in one pass, split by build
tag rather than by concern.** `internal/platform/winconfine.go` is **804** lines and
`confiner_windows.go` is **777** (the house guideline is ~400). Both **grew ~40 %** since the
2026-07-24 review recorded them at 581/572 — the Phase-5 follow-ups (journal retention, own-label
guard, atomic writes, unreadable-journal residue, the recovery-free report constructor, the
build-floor predicate) landed in them, and nothing since has touched them. `TODO.md` still quotes
the stale 581/572 figures.

**2. Each file carries three or four unrelated concerns, because the tag is the only axis.**
Verified inventory:

| Concern | Lines | Where it is now |
| --- | --- | --- |
| Label journal — record, fold rules, atomic write, read/list/siblings | ~370 | `winconfine.go` L61–242, L400–519, L537–592 |
| Retention + revert-split decisions (`retireLabelJournal`, `revertibleRoots`, `restorablePriors`, `clearTreeOutcome`) | ~140 | `winconfine.go` L480–535, L594–686 |
| SDDL vocabulary + walk decisions (`isLowLabelSDDL`, `descendantLabelDecision`) | ~85 | `winconfine.go` L37–59, L133–163, L244–265 |
| Notice wording (`windowsLabelRemedy`, residue/teardown/progress) | ~120 | `winconfine.go` L688–804 |
| Version floor + labelling guardrails | ~170 | `winconfine.go` L23–35, L267–398 |
| OS label walk (`labelTree`, `clearLabelTree`, `readLabelSDDL`, `setLabelSDDL`) | ~150 | `confiner_windows.go` L408–474, L530–611, L730–761 |
| Revert + crash recovery | ~110 | `confiner_windows.go` L510–528, L613–677 |
| Token minting (`mintRestrictedLowToken`, `createRestrictedToken`) | ~95 | `confiner_windows.go` L50–62, L679–728 |
| The backend itself — selectors, `Confine`, `Close`, `labelBox`, root resolution | ~330 | the rest of `confiner_windows.go` |

**3. The invariant that matters most is a convention spread across two files.** ADR 0020 §2's
rule is *the one disk mutation apogee performs is only ever made against a record of how to undo
it* — journal first, label second, always. Today it is enforced by three cooperating sites in two
files: `labelBox` (`confiner_windows.go:296-300`) reads the root's prior, calls `journalLabel`,
then `labelTree`; `labelTree` (`:459-471`) repeats the pairing per descendant; and `labelBox`
(`:311-313`) undoes a just-journalled entry when the root write failed. Nothing structural stops a
fourth caller labelling without journalling — the rule lives in ~40 lines of defensive comment.

**4. The confiner is eight fields deep in mechanism it does not own.** `tokenConfiner`
(`confiner_windows.go:75-100`) holds `journal`, `journalHome`, `journalPath`, `labelled` and the
`mu` that guards them — five of its eight fields are the label journal's state — plus four thin
wrapper methods (`journalLabel`, `unwindRootLabel`, `flushJournal`, `restoreLabels`) that exist
only to reach the free functions in the other file.

**5. The label machinery is already almost dependency-free, which is what makes the cut clean.**
Of the ~615 lines that move out of `winconfine.go`, **none** touch `hostRules`, `Current()` or
anything else in `platform`; the OS half needs only `golang.org/x/sys/windows`. The one apogee
import is `domain.ErrConfinementUnavailable`, used purely for error wrapping — and every one of
those wraps can move to the single call site in the backend without changing an error string
(see D4). The guardrails (`windowsBoxRoots`, `windowsLabelGuardrail`, `windowsProtectedRoots`) are
the exception: they are the *host's path rules* applied to a box, they need `hostRules.split` and
`hostRules.Contains`, and they stay.

---

## Decisions (resolved with the owner 2026-07-25)

- **D1 — one new package, `internal/platform/winlabel`, not a file split.** The journal, the SDDL
  vocabulary, the tree walk and the notice wording move behind a compiler-enforced boundary;
  `platform` keeps the token, the guardrails and the composition. Rejected: more files inside
  `package platform` — it gets every file under ~400 lines, which is what `TODO.md` literally
  asked for, but nothing becomes hidden and problem 3 stays a comment. Also rejected: two
  packages (journal untagged, walk tagged) — the walk and the journal have to be co-located for
  D3 to be possible at all.
- **D2 — the package is a leaf: standard library plus `golang.org/x/sys/windows`, nothing else.**
  No `internal/domain`, no `internal/platform`. It is checkable (`grep -rn "airiclenz/apogee"
  internal/platform/winlabel/` returns nothing) and it is what keeps the boundary from rotting
  back into a two-way seam.
- **D3 — `LabelTree` owns journal-before-label.** `winlabel.LabelTree(root, j)` reads the root's
  prior, records it, labels the root — unwinding its own just-added entry if that write fails —
  then walks the tree, pairing read → record → label per descendant. The `rootLabelled` bool and
  the unwind stop being the caller's business. This is the deepening: ADR 0020 §2's invariant
  becomes a property of the module instead of a rule three call sites remember.
- **D4 — `winlabel` returns plain errors; the backend wraps the sentinel.** Today's messages are
  `fmt.Errorf("%w: cannot label %q Low: %v", domain.ErrConfinementUnavailable, …)`. After the move
  `winlabel` returns `cannot label %q Low: %v` and `labelBox` wraps once with `%w: %v` — the
  rendered string is **byte-for-byte identical** and `errors.Is(err,
  domain.ErrConfinementUnavailable)` still holds at every call site. That is what buys D2.
- **D5 — `Journal` is a value with a mutex, and it absorbs the once-per-root memo.** The
  `labelled` map is not a performance detail of the backend: it is journal-lifecycle state (today
  `restoreLabels` resets it, `confiner_windows.go:495`). Moving it in is what lets `labelBox`
  become a bare loop. `Journal` documents itself as safe for concurrent use; `LabelTree` and
  `Retire` hold the lock for their whole duration, exactly as `labelBox` holds `c.mu` across the
  whole label pass today, so the serialization of a multi-second walk is unchanged. The backend
  keeps **no** mutex.
  **Lock discipline, and why it is a decision rather than a style note:** an **exported** method
  takes `j.mu`; an **unexported** one assumes the caller holds it, and says so on its first doc
  line. No locked path may call an exported method — `sync.Mutex` is not reentrant, so a single
  slip is a self-deadlock that compiles clean, passes every Linux gate, and hangs **only** on a
  real Windows run (`LabelTree`, `Retire` and the walk they drive are all `//go:build windows`).
  Every exported method that an internal caller also needs is therefore written as a two-line
  locking wrapper over the unexported body. This is the one class of defect this plan's gate
  cannot catch, which is what earns it a D.
- **D6 — the journal-file API and the SDDL primitives are exported for the backend's tests, and
  that is stated in the doc comment.** `confiner_windows_test.go` (1378 lines, Windows-tagged)
  asserts against real SACLs and real journal files: **42** `readLabelSDDL`/`setLabelSDDL` calls
  plus `listLabelJournals`, `readLabelJournal`, `writeLabelJournal`, `labelJournalPath`,
  `processAlive`. Those tests stay with the backend they test (D7), so those verbs become
  `winlabel.ReadSDDL`, `SetSDDL`, `ListJournals`, `ReadJournal`, `WriteJournal`, `JournalPath`,
  `ProcessAlive`. They are the module's honest file/OS surface, not an accident — `Recover`,
  `Residue` and `Open` are production consumers of the same verbs — but the package doc says
  plainly which exports exist for the neighbouring test package, so a later `/code-audit` does not
  re-litigate it.
- **D7 — `confiner_windows_test.go` stays in `platform` and keeps testing the backend end to
  end.** Only its call sites are renamed. Rewriting ~900 lines of real-SACL lifecycle tests to
  drive `winlabel.Journal` directly would trade a proven safety net for an unproven one, on a
  machine that cannot run either.
- **D8 — the three exported notices keep their names and their home in `platform`.**
  `ConfinementResidue`, `ConfinementTeardownNotice` and `WindowsLabelProgressNotice` become
  one-line delegations to `winlabel.Residue`, `TeardownNotice`, `ProgressNotice`. They read
  naturally beside `platform.NewConfiner()`, `cmd/apogee` is untouched, and nothing outside
  `platform` learns that a Windows-specific label package exists.
- **Names:** package `winlabel` · `Journal` / `Entry` / `Record` (the on-disk shape) · `Open` ·
  `Home` · `JournalPath` · `LabelTree` / `ClearTree` · `Retire` · `Recover` · `Residue` /
  `TeardownNotice` / `ProgressNotice` · `ReadSDDL` / `SetSDDL` / `ProcessAlive`.

## Explicit non-goals

- **No behaviour change anywhere.** Not one error string, not one label, not one journal byte, not
  one walk order. Every existing test must pass **unchanged** except for the mechanical renaming
  of moved identifiers. If an item needs an assertion edited to pass, the item is wrong — stop and
  flag it.
- **The guardrails do not move.** `windowsProtectedRoots`, `windowsBoxRoots`,
  `windowsLabelGuardrail` and `windowsNetworkDenyDecision` need `hostRules` and stay in
  `platform`. Exporting `hostRules.split` to drag them out would widen a seam to narrow a file.
- **`confiner_windows_test.go` stays large (D7).** After this plan it is the one file in the
  package over the guideline, by choice. Record that in `TODO.md` (item 7) rather than leaving it
  to be re-discovered as a finding.
- **No CHANGELOG entry.** Everything here is `internal/`; nothing a user or an embedder can
  observe changes. (Contrast candidate 04, which broke `apogee.Mechanism` and said so.)
- **No CONTEXT.md term.** `winlabel` packages an existing concept — the mandatory label and its
  journal, already carried by ADR 0020 and CONTEXT.md's confinement section — it does not name a
  new one.
- **No ADR.** ADR 0020 decides *what* the Windows backend does and names no files; this plan
  changes only where the code lives. If an item finds itself wanting to amend ADR 0020, the item
  is wrong — stop and flag it.
- **No new tests beyond the ones that move**, except the two the new boundary makes possible
  (items 4 and 5) and the leaf-dependency guard (item 1).

---

## 1. Create the package — SDDL vocabulary, walk decisions, notice wording — ✅ DONE (2026-07-25)

NOTES (2026-07-25): three deviations, all forced by the intermediate state this item leaves the
tree in.

**(a) Six declarations the item's bullets call unexported had to be EXPORTED here**, because
`package platform` still calls every one of them until items 2–5 move their callers, and the item's
own What requires exactly that (*"now resolve through `winlabel.` at their remaining call sites in
`winconfine.go` and `confiner_windows.go`"* — an unexported name cannot be resolved through a
package qualifier). They are `DirSDDL` / `FileSDDL` / `ClearSDDL` (called by `labelTree` and
`clearLabelTree`, which move in item 5), `LabelACEPrefix` (`readLabelSDDL`, item 5), `FoldPath`
(`journalLabelEntry` and `unwindLabelEntry` in item 2, `revertibleRoots` and `restorablePriors` in
item 3, `labelBox` in item 5), `DescendantDecision` (`labelTree`, item 5) and `ResidueNotice`
(`confinementResidue`, item 2). Each is the plan's name with the leading letter capitalised, so the
item that removes its last external caller unexports it by lowercasing one identifier: item 2 takes
`ResidueNotice` → `residueNotice`, item 5 takes `DirSDDL`/`FileSDDL`/`ClearSDDL`/`LabelACEPrefix`/
`DescendantDecision` and, with `labelBox`, `FoldPath`. Only `IsLowLabel`, `Remedy`,
`ProgressNotice` and `TeardownNotice` are exported by decision (D6/D8); the other six are transitional
and item 6's acceptance should confirm they are gone. `lowLabelSIDs` and `residueIndent` have no
caller outside the package and are unexported as written.

**(b) `confiner_windows_test.go` has NINE `isLowLabelSDDL` call sites, not the one at `:1267`** the
item names (`:189`, `:292`, `:509`, `:519`, `:647`, `:730`, `:1132`, `:1167`, `:1270`), plus a
comment at `:578` naming `descendantLabelDecision`. All ten were renamed — required both to compile
the Windows-tagged file and to satisfy this item's own acceptance grep, which covers `*_test.go`.
Nothing else in that file changed.

**(c) `TestPackageImportsNothingFromApogee` is an ALLOW-list, not a ban on one module path.** It
accepts the standard library (first import-path element carries no dot) plus
`golang.org/x/sys/windows` and fails everything else, which is what D2 actually decides — a deny-list
on `airiclenz/apogee` would wave through any third-party package that happened not to be apogee.
It also keeps this item's acceptance grep literally empty: a guard that spelled its own needle would
be the single hit in `grep -rn "airiclenz/apogee" internal/platform/winlabel/`. The guard is
negative-tested (a planted `internal/domain` import fails it) and refuses to pass over zero parsed
files. Moved test FUNCTION names are kept verbatim; only identifiers inside them were renamed,
including the function names quoted in failure messages.

The three smallest, most self-contained concerns first, so the package exists, its doc comment is
written, and the boundary is proven before anything intricate crosses it.

**What:**

- **New `internal/platform/winlabel/doc.go`** — the package doc: what a mandatory label is on this
  backend, that the journal is written before any label and why (ADR 0020 §2), that everything
  outside `walk_windows.go` is pure or plain JSON file I/O and therefore table-testable on Linux
  and macOS exactly as `seatbelt.go` is, that the package imports nothing from apogee (D2), and
  which exports exist for `package platform`'s Windows-tagged tests (D6). Point at ADR 0020 and
  `confinement-execution-contract.md` §9, as the two headers being replaced do.
- **New `winlabel/sddl.go`** — moved verbatim from `winconfine.go`: the three SDDL constants
  (L51–55, renamed `dirSDDL` / `fileSDDL` / `clearSDDL`, unexported — no caller outside the
  package), `labelACEPrefix` (L59), `lowLabelSIDs` (L136), `IsLowLabel` (was `isLowLabelSDDL`,
  L144–163, **exported**: `confiner_windows_test.go:1267` asserts with it that no journal entry
  records apogee's own label — D6), `foldPath` (was `foldLabelPath`, L131) and
  `descendantDecision` (was `descendantLabelDecision`, L260–265). Every doc comment moves with its
  declaration, verbatim.
- **New `winlabel/notice.go`** — moved verbatim: `Remedy` (was `windowsLabelRemedy`, L735, exported
  because `Residue`, `TeardownNotice` and `ProgressNotice` must all quote the one spelling),
  `residueIndent` (L739), `residueNotice` (L749–769), `ProgressNotice` (was
  `WindowsLabelProgressNotice`, L783–788), `TeardownNotice` (was `ConfinementTeardownNotice`,
  L798–804).
- **`internal/platform/winconfine.go`** — those declarations are deleted; `ConfinementTeardownNotice`
  and `WindowsLabelProgressNotice` become one-line delegations (D8) keeping their existing doc
  comments, trimmed to name `winlabel` as the wording's home. `isLowLabelSDDL`,
  `descendantLabelDecision`, `foldLabelPath` and the SDDL constants now resolve through
  `winlabel.` at their remaining call sites in `winconfine.go` and `confiner_windows.go`
  (`journalLabelEntry`, `unwindLabelEntry`, `labelBox`, `labelTree`, `clearLabelTree`,
  `revertibleRoots`, `restorablePriors`, `confinementResidue`).
- **`internal/platform/confiner_windows_test.go`** — its one `isLowLabelSDDL` call (`:1267`)
  becomes `winlabel.IsLowLabel`. Nothing else in that file changes in this item.

**Tests:**

- **Move** to `winlabel/sddl_test.go`: `TestIsLowLabelSDDL` (`winconfine_test.go:1238`),
  `TestDescendantLabelDecision` (`:1179`) — bodies unchanged but for the renamed identifiers.
- **Move** to `winlabel/notice_test.go`: `TestWindowsResidueNoticeWordsBothFindings` (`:893`),
  `TestConfinementTeardownNoticeWordsTheFailure` (`:772`),
  `TestWindowsLabelProgressNoticeNamesRootAndFence` (`:794`). The latter two assert today on the
  exported `platform` spellings; their bodies move to call `winlabel.TeardownNotice` /
  `winlabel.ProgressNotice` (a test in `winlabel` cannot reach `platform` — D2 runs both ways),
  with the assertions themselves unchanged. Leave **one** short test in `winconfine_test.go` per
  delegation, asserting that `platform.ConfinementTeardownNotice` and
  `platform.WindowsLabelProgressNotice` still return the module's wording: a delegation that
  silently stopped delegating is the only new failure mode this item introduces, and it is
  invisible to every test that moved.
- **New** `winlabel/deps_test.go` — `TestPackageImportsNothingFromApogee`: walk the package's own
  `.go` files (including the tagged ones, via `go/parser` over the directory, not the build
  context) and fail on any import path containing `airiclenz/apogee`. This is D2's guard and it
  must be written in item 1, before there is anything to violate it.

**Acceptance:** all six gates green; `internal/platform/winlabel/` exists with `doc.go`,
`sddl.go`, `notice.go` and their tests; `grep -rn "airiclenz/apogee" internal/platform/winlabel/`
is empty; `grep -n "windowsLabelRemedy\|windowsResidueNotice\|isLowLabelSDDL\|descendantLabelDecision"
internal/platform/*.go` is empty. Commit: `refactor(platform): winlabel — the SDDL vocabulary,
walk decisions and notice wording get a package`.

---

## 2. Move the label journal — record, fold rules, atomic write, read/list/siblings — ✅ DONE (2026-07-25)

NOTES (2026-07-25): deviations, all forced by callers the item's literal text does not account
for. Everything else moved verbatim.

**(a) `residue` is EXPORTED as `ResidueIn(home string)`.** The item names it unexported, but
`confiner_windows_test.go` — which stays in `platform` by D7 — calls `confinementResidue(home)`
at **seven** sites against a `t.TempDir()` home (`:814`, `:888`, `:927`, `:968`, `:1005`,
`:1042`, `:1201`), and an unexported `residue` is unreachable from `package platform`. It is a
permanent D6-class export, not a transitional one, and doc.go now names it. The no-arg `Residue()`
is exactly as planned (`ResidueIn(Home())`), and `ConfinementResidue` is still the one-line
delegation to `winlabel.Residue()` with its doc comment kept.

**(b) `journalDir` is EXPORTED as `JournalDir`** for the same reason: `confiner_windows_test.go`
calls `labelJournalDir(home)` at `:371` and `:1024`, and the plan forbids rewriting those bodies
beyond identifier renames. Also a permanent D6 export, also named in doc.go.

**(c) Three TRANSITIONAL exports** (item 1's NOTES(a) pattern — the plan's name with the leading
letter capitalised, unexported again by the item that removes the last `platform` caller):
`RecordEntry` and `UnwindEntry`, called by `journalLabel`/`unwindRootLabel` in
`confiner_windows.go` — **item 4** deletes both wrappers and lowercases these two to
`recordEntry`/`unwindEntry`; and `SiblingJournals`, called by `revertSparingLiveSiblings` —
**item 5** moves that function and lowercases it to `siblingJournals`. Item 6's acceptance should
confirm all three are gone.

**(d) The journal-identifier renames in `confiner_windows_test.go` landed HERE, not in item 5.**
This item deletes the declarations, so both the `GOOS=windows go test -c` gate and this item's own
acceptance grep (`internal/platform/*.go` matches `_test.go` too) require them now. Renamed:
`labelJournal`→`winlabel.Record`, `labelJournalEntry`→`winlabel.Entry`,
`labelJournalPath`→`winlabel.JournalPath`, `labelJournalDir`→`winlabel.JournalDir`,
`writeLabelJournal`→`winlabel.WriteJournal`, `readLabelJournal`→`winlabel.ReadJournal`,
`listLabelJournals`→`winlabel.ListJournals`, `confinementResidue`→`winlabel.ResidueIn`,
`priorLabels()`→`PriorLabels()`. **No assertion, scenario or comment changed** beyond those
identifiers. Item 5's remaining share is the `readLabelSDDL`/`setLabelSDDL`/`processAlive` set.
The six retention/revert tests in `winconfine_test.go` (item 3's) were renamed here for the same
compile reason; they are otherwise untouched and still move in item 3.

**(e) `ResidueNotice` → `residueNotice`**, as item 1's NOTES(a) assigned to this item.
`DescendantDecision`'s doc comment in `sddl.go` said "remains `journalLabelEntry`'s decision" and
now says `RecordEntry`'s — a staleness this item's rename created.

**(f) Small doc edits:** `Record`'s doc gains one sentence naming its JSON tags as the on-disk
compatibility surface (the item's own rationale for keeping them), and `Record`'s methods take
receiver `r` rather than the old `j`, which item 4's `*Journal` needs.

The biggest single chunk (~370 lines) and the reason the file is 804 lines long. Still free
functions at this point: the stateful `Journal` type is item 4, so this item is a pure move and
can be reviewed as one.

**What:**

- **New `winlabel/journal.go`**, moved verbatim from `winconfine.go` with the renames below:
  - the journal file names (L69–75) — `journalDirName`, `journalPrefix`, `journalSuffix`,
    `journalTempPrefix`, `journalTempSuffix`;
  - `Record` (was `labelJournal`, L81–90) and `Entry` (was `labelJournalEntry`, L94–102), with
    their JSON tags **unchanged** — the on-disk format is a compatibility surface with every
    journal a previous apogee left on a user's disk, and with `cmd/apogee/probe_test.go`, which
    plants one by hand;
  - `(Record).Roots()` (L105) and `(Record).PriorLabels()` (L117);
  - `recordEntry` (was `journalLabelEntry`, L188–212) and `unwindEntry` (was `unwindLabelEntry`,
    L227–242) — the two decisions whose doc comments are the longest in the file; they move
    **verbatim**, including the injected `fold` parameter that makes them testable off Windows;
  - `Home` (was `confinementJournalHome`, L407–413), `journalDir` (L416), `JournalPath` (was
    `labelJournalPath`, L419–421);
  - `WriteJournal` (was `writeLabelJournal`, L438–463) and `writeAndSync` (L468–478);
  - `ReadJournal` (was `readLabelJournal`, L538–548), `ListJournals` (was `listLabelJournals`,
    L553–568), `siblingJournals` (was `siblingLabelJournals`, L576–592);
  - `Residue` (was `ConfinementResidue`, L696) and `residue` (was `confinementResidue`, L712–729).
- **`winconfine.go`** — those declarations are deleted; `ConfinementResidue` becomes a one-line
  delegation to `winlabel.Residue()` keeping its doc comment (D8).
- **`confiner_windows.go`** — `journalHome`/`journalPath` construction, `flushJournal`,
  `journalLabel`, `unwindRootLabel`, `recoverLabelJournals`, `revertSparingLiveSiblings` and
  `restoreLabels` now name `winlabel.` types and functions. The field `journal labelJournal`
  becomes `journal winlabel.Record` — still a value on the backend; item 4 replaces it.

**Tests:**

- **Move** to `winlabel/journal_test.go`, bodies unchanged but for renames:
  `TestLabelJournalRoundTripAndAccessors` (`winconfine_test.go:275`),
  `TestWriteLabelJournalPublishesAtomically` (`:320`),
  `TestSiblingLabelJournalsExcludesOwnAndUndecodable` (`:738`),
  `TestJournalLabelEntryNeverRecordsApogeesOwnLabel` (`:962`),
  `TestJournalLabelEntryUsesTheInjectedFold` (`:1067`), `TestUnwindLabelEntry` (`:1082`),
  `TestUnwindLabelEntryUsesTheInjectedFold` (`:1164`),
  `TestConfinementResidueReportsOnlyForeignJournals` (`:817`),
  `TestConfinementResidueReportsAnUnreadableJournal` (`:854`).
- `cmd/apogee/probe_test.go` passes **unchanged** — it plants a journal by path literal. Its
  comment naming *"platform's confinementJournalHome/labelJournalPath"* is updated to name
  `winlabel.Home`/`winlabel.JournalPath` in item 7, not here.

**Acceptance:** gates green; `grep -n "labelJournal\|writeAndSync\|confinementJournalHome"
internal/platform/*.go` is empty; the on-disk JSON is unchanged — `TestProbeReportsConfinementResidueWithoutHealingIt`
in `cmd/apogee` still passes with its hand-written `{"pid":…,"entries":[{"path":…,"root":true}]}`.
Commit: `refactor(platform): winlabel owns the label journal — record, atomic write, siblings`.

---

## 3. Move the retention and revert-split decisions — ✅ DONE (2026-07-25)

NOTES (2026-07-25): three deviations. The moved code is otherwise byte-identical — a mechanical
rename of the original `winconfine.go` block diffs **empty** against `retire.go`, and the same
holds for the six moved test bodies.

**(a) All four declarations are TRANSITIONAL EXPORTS, not unexported.** The item's *"Everything
this item moves stays unexported"* cannot hold yet: its own What says *"`restoreLabels` and
`recoverLabelJournals` call the moved names"*, and `package platform` still calls every one of the
four until items 4–5 move those callers — `Retire` from `restoreLabels` and `recoverLabelJournals`,
`ClearTreeOutcome` from `clearLabelTree`, `RevertibleRoots` and `RestorablePriors` from
`revertSparingLiveSiblings`. `GOOS=windows go vet ./internal/platform/...` is the gate that proves
it. They follow item 1's NOTES(a) pattern — the plan's name with the leading letter capitalised —
so **item 5** unexports each by lowercasing one identifier as it moves the last caller, and item
6's acceptance should confirm all four are gone. **One trap for item 5:** a package-level
`func Retire(...)` and a `func (j *Journal) Retire()` coexist legally in one package, so the
compiler will NOT flag a leftover `Retire` when item 5 adds the method — the lowercasing must
happen in the same edit.

**(b) The `Record` parameter is `r`, not `j`.** Item 2's NOTES(f) already renamed `Record`'s
receivers `j` → `r` so `j` stays free for item 4's `*Journal`; that rename is extended here to the
four free functions that take a `Record` (and to the one closure parameter in the moved
`TestRetireLabelJournalKeepsTheFileWhenTheRevertFails`), with the doc comments following it ("r's
roots"). A `j Record` beside a `j *Journal` in one package is exactly the confusion D5's lock
discipline cannot afford.

**(c) Two consequential edits.** `DescendantDecision`'s doc in `sddl.go` said *"the
retireLabelJournal seam pattern"* and now says `Retire`'s — staleness this item's rename created
(item 2's NOTES(e) pattern). `winconfine_test.go` lost its now-unused `os` import when the six
tests left. Moved test FUNCTION names are kept verbatim, per item 1's NOTES(c).

The rules that decide what a revert may touch and whether a journal file may be deleted. They are
pure, they are already behind injected seams (`revert`, `alive`), and they are the last untagged
piece before the `Journal` type can be assembled.

**What:**

- **New `winlabel/retire.go`**, moved verbatim from `winconfine.go`:
  - `retire` (was `retireLabelJournal`, L501–519) — keeps its injected `revert func(Record)
    ([]Entry, error)` parameter, which is what makes the retention rule table-testable on Linux;
    it stays **unexported**, and item 4's `(*Journal).Retire()` is the exported way in (the
    `Build`/`buildFrom` seam shape the mechanisms catalogue already uses);
  - `clearTreeOutcome` (L529–535);
  - `revertibleRoots` (L610–632) — keeps its injected `alive func(int) bool`;
  - `restorablePriors` (L655–686).
- **Everything this item moves stays unexported.** Its only consumer is item 5's
  `revertSparingLiveSiblings`, which lands in the Windows-tagged file of this same package;
  `confiner_windows_test.go` touches none of it. The general rule for items 2–5: **default to
  unexported, and export only what fails to compile** — the exported surface D6 catalogues is the
  maximum, not a target.
- **`confiner_windows.go`** — `restoreLabels` and `recoverLabelJournals` call the moved names.

**Tests:** **move** to `winlabel/retire_test.go`, bodies unchanged but for renames:
`TestRetireLabelJournalKeepsTheFileWhenTheRevertFails` (`winconfine_test.go:373`),
`TestRetireLabelJournalWithoutAJournalFile` (`:436`),
`TestRetireLabelJournalRewritesTheFileToTheHandedOffEntries` (`:457`),
`TestRestorablePriorsHandsOffSiblingClaimedTrees` (`:512`), `TestClearTreeOutcome` (`:624`),
`TestRevertibleRootsSparesOnlyALiveSiblingsRoots` (`:653`).

**Acceptance:** gates green; `internal/platform/winconfine.go` is **under 400 lines** and holds
only the version floor, the labelling guardrails and the three notice delegations;
`grep -n "retireLabelJournal\|revertibleRoots\|restorablePriors\|clearTreeOutcome"
internal/platform/*.go` is empty. Commit: `refactor(platform): winlabel owns the retention and
revert-split decisions`.

---

## 4. `winlabel.Journal` — the stateful recorder the backend composes — ✅ DONE (2026-07-25)

NOTES (2026-07-25): deviations. No behaviour, wording or error string changed anywhere.

**(a) The record field is `rec`, not `record`.** A struct cannot carry a field and a method of
the same name, and item 5's `LabelTree` pseudocode calls `j.record(...)` — so the METHOD keeps the
plan's name and the field is `rec`. Item 5's `retire(j.path, j.record, …)` means `j.rec`.

**(b) The type and its methods live in a NEW `winlabel/session.go`, not in `journal.go`.**
`journal.go` is 339 lines and the type plus its eleven methods is ~220; keeping them together
would leave a ~560-line file, and nothing after this item removes a line from it — so item 6's
acceptance (*every* non-test `winlabel` file under 400) would already be unsatisfiable. The two
new test files keep the plan's names (`journal_state_test.go`, `journal_race_test.go`).

**(c) `restoreLabels` is deleted HERE, not in item 5**, and its body and doc comment became
`(*Journal).Retire`. This item removes `mu`, `labelled`, `journalHome`, `journalPath` and the
`Record` field from the confiner, which leaves `restoreLabels` with nothing to reach through;
keeping it would have meant exporting home/path/record accessors that item 5 deletes again. Item
3's own What already assigns `(*Journal).Retire()` to *item 4*. One transitional difference from
item 5's shape: the method takes the revert as a parameter — `Retire(revert func(home, own string)
func(Record) ([]Entry, error))` — because the production revert is `revertSparingLiveSiblings`,
which is Windows-tagged and stays in `platform` until item 5. `Close` calls
`c.journal.Retire(revertSparingLiveSiblings)`; **item 5 drops the parameter** and inlines it. The
keep-the-journal error wrapping moved verbatim.

**(d) Four TRANSITIONAL exports** (item 1's NOTES(a) pattern), all of which **item 5 removes**
when `LabelTree` absorbs the pass: `Record` and `Unwind` (two-line locking wrappers over the
unexported `record`/`unwind`, called by `labelBox` and by `labelTree`, which is still in
`platform`), and `Labelled` / `MarkLabelled` (the once-per-root memo, called by `labelBox`).
Item 6's acceptance should confirm all four are gone.

**(e) `RecordEntry` → `recordEntry` and `UnwindEntry` → `unwindEntry`**, as item 2's NOTES(c)
assigned to this item. `unwindEntry`'s doc said "It is labelBox's undo" and now names
`Journal.unwind` (item 2's NOTES(e) pattern). `SiblingJournals` is item 5's and is untouched.

**(f) `FoldPath` lost its last `platform` caller HERE** — `labelBox`'s `key`, plus the two deleted
wrappers — one item earlier than item 1's NOTES(a) predicted. It is left **exported**: lowercasing
it is still item 5's assignment and reaches into item 2's and 3's test files. Item 6's acceptance
should confirm it happened.

**(g) The package-level `Retire` and `(*Journal).Retire` now coexist** — exactly the trap item 3's
NOTES(a) flagged, and the compiler will not point at the leftover. `recoverLabelJournals` is still
the package-level function's caller, so it stays exported until item 5 moves that function in.

**(h) Test reach-ins.** The two `[]winlabel.Record{onDisk, c.journal}` loops became
`{Entries: c.journal.Entries()}` composite literals, so the loop bodies and every assertion stay
byte-identical; the `first.mu` trio became `first.journal.ForgetLabelled()`; three
`c.journal.Entries` field reads became `Entries()` calls. The `PriorLabels()` sites were already
method-shaped and did not change. `prewarm_windows.go`'s doc comment named `c.labelled`, a field
this item deletes, and now names the journal's memo.

The first item that is not a move. Five of `tokenConfiner`'s eight fields collapse into one
(D5), and four wrapper methods disappear.

**What:**

- **`winlabel/journal.go`** gains the type:

  ```go
  // Journal is one session's label journal: the on-disk Record, the file it lives in, and the
  // roots this session has already labelled. Safe for concurrent use — LabelTree and Retire
  // hold the lock for their whole duration, which is what serializes two goroutines confining
  // at once, exactly as the backend's own mutex did before this type existed.
  type Journal struct {
      mu       sync.Mutex
      home     string          // for sibling reads at Retire; "" when no user profile resolved
      path     string          // "" ⇒ not writable ⇒ nothing may be labelled (ADR 0020 §2)
      record   Record
      labelled map[string]bool // folded roots already walked this session
  }
  ```

  - `func Open(home string) *Journal` — resolves `path = JournalPath(home, os.Getpid())` and sets
    `record.PID = os.Getpid()` when `home != ""`; performs **no I/O**, exactly as
    `newTokenConfinerWithoutRecovery` promises today (`confiner_windows.go:153-165`).
  - `func (j *Journal) Writable() bool` — `path != ""`. The backend's refusal (`labelBox`,
    `:270-273`) stays in `platform` and keeps its wording verbatim, so a no-profile host still
    refuses the box **before** roots are resolved rather than after.
  - `func (j *Journal) record(entry Entry) (bool, error)` (unexported) — `recordEntry` + flush,
    returning whether the journal newly changed. Assumes the lock is held.
  - `func (j *Journal) unwind(path string)` (unexported) — `unwindEntry` + best-effort re-flush.
    Assumes the lock is held.
  - `func (j *Journal) flush() error` — `WriteJournal(j.path, j.record)`, a no-op when `path ==
    ""`.
  - `func (j *Journal) Entries() []Entry` and `func (j *Journal) PriorLabels() map[string]string`
    — **copies** taken under the lock, for `confiner_windows_test.go`'s existing assertions
    against `c.journal.Entries` (4 sites) and `c.journal.priorLabels()` (1 site). Doc comments say
    they return snapshots and why (D6).
  - `func (j *Journal) forgetLabelled()` (unexported, **assumes the lock is held**) — drops the
    once-per-root memo; called by `Retire` after a successful revert (today
    `confiner_windows.go:495`), from inside `Retire`'s own critical section. `func (j *Journal)
    ForgetLabelled()` is the two-line locking wrapper over it, for
    `TestWindowsRelabellingNeverJournalsApogeesOwnLabel` (`confiner_windows_test.go:1238-1240`),
    which reopens the memo to re-walk an already-labelled tree. Split in two **per D5's lock
    discipline** — a single exported `ForgetLabelled` called from a locked `Retire` is a
    self-deadlock no gate in this plan can see.
- **`confiner_windows.go`** — `tokenConfiner` loses `mu`, `labelled`, `journal`, `journalHome`,
  `journalPath` and gains `journal *winlabel.Journal`. `journalLabel`, `unwindRootLabel`,
  `flushJournal` are **deleted**; `labelBox` calls `j.record`-equivalents through the walk in item
  5 — until then it calls the exported shims it still needs. `newTokenConfinerWithoutRecovery`
  becomes `c.journal = winlabel.Open(home)`. `labelBox` keeps `c.journal.Writable()` as its first
  refusal and keeps holding the label pass under… **nothing**: the backend's `mu` is gone, and the
  serialization moves inside `Journal` (D5).

**Tests:**

- **New** `winlabel/journal_state_test.go`: `Open("")` is not `Writable` and flushing is a no-op;
  `Entries()` returns a copy (mutating the returned slice does not change the journal);
  `ForgetLabelled` clears the memo; a `record` of an entry that changes nothing does not rewrite
  the file (assert on the file's mtime or content, not on a counter).
- **New** `winlabel/journal_race_test.go`: two goroutines recording entries concurrently, run
  under `-race`, proving D5's claim rather than asserting it in a comment.
- Every existing test passes unchanged except `confiner_windows_test.go`'s five reach-ins, which
  become `c.journal.Entries()`, `c.journal.PriorLabels()` and `first.journal.ForgetLabelled()`
  (the `first.mu.Lock()` / `first.labelled = make(...)` / `first.mu.Unlock()` trio at `:1238-1240`
  becomes one line).

**Acceptance:** gates green (including `go test -race ./...`); `tokenConfiner` has **four** fields
(`caps`, `token`, `rules`, `protected`) plus `journal`; `grep -n "sync.Mutex\|c.mu"
internal/platform/confiner_windows.go` is empty. Commit: `refactor(platform): the label journal is
a Journal, not five fields on the confiner`.

---

## 5. Move the OS label walk, the revert and crash recovery — ✅ DONE (2026-07-25)

NOTES (2026-07-25): the seam chosen for the item's flagged design call, plus the deviations it
and the four inherited rename assignments forced. Everything else moved verbatim — a mechanical
rename of the original `confiner_windows.go` blocks for `revertSparingLiveSiblings`,
`revertLabelJournal`, `clearLabelTree`, `recoverLabelJournals`, `processAlive`, `readLabelSDDL`
and `setLabelSDDL` diffs **empty** against `walk_windows.go` but for (c), and
`confiner_windows_test.go` diffs as identifier renames only (verified by re-applying the rename
map to the HEAD version — the diff is empty).

**(a) The `Retire` seam is a per-`Journal` FIELD, not a stub and not a package-level var** —
the owner's design call, taking the item's own preferred shape. `Retire` is declared **once**,
unguarded, in `session.go`; it reaches the OS through `j.revert`, a `revertFunc` field fixed at
`Open` from the build-tagged `osRevert()` (`walk_windows.go` returns
`revertSparingLiveSiblings`, `walk_other.go` returns nil). This EXTENDS item 4's injected-revert
parameter rather than replacing it with a global: a per-value field cannot be raced or clobbered
between two journals, and it is what lets a test hand one journal a stand-in — which is exactly
how `TestJournalRetireKeepsTheHandoffAndReopensTheMemo` now injects, instead of passing an
argument. The nil case is explicit and is a **no-op returning nil**, not an error: nothing off
Windows can label anything (every other stub reports `errors.ErrUnsupported`), so there is no
disk mutation to undo, and erroring would only invent a failure. It is covered twice on Linux —
`TestJournalRetireWithoutARevertSeamDoesNothing` (seam cleared explicitly, so it also runs on
Windows) and `TestNonWindowsJournalHasNoRevertSeam` (`osRevert()` and `Open(...).revert` are
nil). This is the item's one literal deviation: its stub list names `Retire`, and `walk_other.go`
does **not** stub it. One method, one doc comment, one contract.

**(b) Item 4's four transitional exports are gone, which forced three test-body edits.**
`(*Journal).Record` and `Unwind` are simply deleted (`LabelTree` absorbed both). `Labelled` /
`MarkLabelled` became the lock-held unexported `isLabelled` / `markLabelled`: `LabelTree` calls
them from **inside** its own critical section, where an exported wrapper is D5's self-deadlock.
`journal_state_test.go` and `journal_race_test.go` therefore wrap those calls in
`j.mu.Lock()`/`Unlock()`, exactly as they already do for `j.record` — **no assertion changed**.

**(c) The `Record` parameters of the moved `revertSparingLiveSiblings` and `Recover` are `r`,
not `j`** — item 3's NOTES(b) convention, now unavoidable: both sit beside `j *Journal` in one
package, and `j Record` next to `j *Journal` is the confusion D5's lock discipline cannot
afford. That is the whole of the diff against the originals.

**(d) All four inherited rename assignments landed here**, all now unexported: item 1's
`DirSDDL`/`FileSDDL`/`ClearSDDL`/`LabelACEPrefix`/`DescendantDecision`, item 4's `FoldPath`,
item 2's `SiblingJournals`, and item 3's `Retire`/`ClearTreeOutcome`/`RevertibleRoots`/
`RestorablePriors` (the coexisting package-level `Retire` and `(*Journal).Retire` trap item 3's
NOTES(a) flagged — lowercased in the same edit). Doc comments naming the moved `platform`
identifiers followed them (`labelTree`→`LabelTree`, `clearLabelTree`→`ClearTree`,
`recoverLabelJournals`→`Recover`, `processAlive`→`ProcessAlive`,
`revertLabelJournal`→`revertJournal`), including `prewarm_windows.go`'s and the four in
`confiner_windows_test.go`. Item 6's acceptance can confirm the whole transitional set is gone.

**(e) `doc.go` was updated, though the item does not ask.** Its D6 export list would otherwise
have omitted `ReadSDDL`/`SetSDDL`/`ProcessAlive`, and its "the one Windows-tagged file here"
sentence predated `walk_other.go`; it also now states that `LabelTree` owns journal-before-label
outright, which is D3 and the point of the whole plan.

**Lock-discipline review (D5), done before commit as the item requires:** `LabelTree`'s and
`Retire`'s critical sections were read end to end. Every `j.` call inside them resolves to an
unexported body — `isLabelled`, `record`, `unwind`, `markLabelled`, `flush`, `forgetLabelled` —
and the injected revert closure (`siblingJournals`, `restorablePriors`, `revertibleRoots`,
`revertJournal`, `retire`) touches only `Record` VALUES, never the `Journal`. No exported method
is reachable from either locked path. `labelBox`'s `Writable()` call precedes the loop and does
not nest.

The Windows-tagged half, and the item that lands D3. **Nothing in this item can be run on the dev
machine** — its gate is the two cross-compiles plus verification step 4.

**What:**

- **New `winlabel/walk_windows.go`** (`//go:build windows`), moved from `confiner_windows.go`:
  - `ReadSDDL` (was `readLabelSDDL`, L734–744) and `SetSDDL` (was `setLabelSDDL`, L750–761) —
    exported for `confiner_windows_test.go` (D6);
  - `ProcessAlive` (was `processAlive`, L662–677) — exported for the same reason;
  - `ClearTree` (was `clearLabelTree`, L570–611);
  - `LabelTree(root string, j *Journal) error` — was `labelTree` (L426–474), **absorbing** the
    root's read/record/unwind from `labelBox` (L287–317) per **D3**:

    ```
    LabelTree(root, j):
      j.mu.Lock(); defer j.mu.Unlock()
      if j.labelled[foldPath(root)] { return nil }        // the memo, moved in with D5
      prior, err := ReadSDDL(root)                        // was labelBox:292
      if err != nil { return fmt.Errorf("cannot read the mandatory label of %q: %v", …) }
      journalled, err := j.record(Entry{Path: root, Root: true, PriorSDDL: prior})
      if err != nil { return err }
      if err := SetSDDL(root, dirSDDL); err != nil {
          if journalled { j.unwind(root) }                // was labelBox:311-313
          return fmt.Errorf("cannot label %q Low: %v", root, err)
      }
      … the existing WalkDir body, verbatim, calling j.record per descendant …
      j.labelled[foldPath(root)] = true
      return nil
    ```

    The `rootLabelled bool` return is **deleted** — it existed only to tell `labelBox` whether to
    unwind, and the unwind is now inside. Every doc comment on the deleted `labelBox` lines moves
    onto `LabelTree`; nothing explaining *why* a rung is tolerated may be lost.
  - `revertSparingLiveSiblings` (L519–528) and `revertJournal` (was `revertLabelJournal`,
    L546–559);
  - `(*Journal).Retire() error` — was `restoreLabels` (L487–497): takes the lock, calls
    `retire(j.path, j.record, revertSparingLiveSiblings(j.home, j.path))`, stores the remaining
    entries, calls the **unexported** `forgetLabelled` (D5 — calling the exported wrapper from
    here would deadlock). The error wrapping (*"could not revert every mandatory label; the
    journal %q is kept so the next run retries"*) moves with it, verbatim.
  - **Lock-discipline review is part of this item, not a follow-up.** `LabelTree` and `Retire`
    are the only two methods that hold the lock across real work, and every `j.` call inside them
    must resolve to an unexported body. Read both critical sections against D5 before committing;
    a violation is invisible to all six gates and surfaces as a hung Windows session in
    verification step 4.
  - `Recover(home string)` — was `recoverLabelJournals` (L637–658).
- **New `winlabel/walk_other.go`** (`//go:build !windows`) — the stubs that keep the package
  compiling and the untagged tests honest on Linux and macOS: `ReadSDDL`/`SetSDDL` return an
  `errors.ErrUnsupported`-wrapped "no mandatory label facility on this OS"; `ProcessAlive` returns
  `false`; `ClearTree`, `LabelTree`, `Retire` and `Recover` are the same shape. Each carries a doc
  comment saying it exists so the package's decision half is table-testable off Windows and that
  no production path on a non-Windows host ever reaches it — the same posture
  `seatbelt_notdarwin_test.go` and `confiner_other.go` already take.
  *Design call to flag if it bites:* `Retire` must be declared in **one** place if its non-Windows
  body would differ; prefer an unexported `revertFunc` variable set by the tagged files over
  duplicating the method.
- **`confiner_windows.go`** — `labelTree`, `clearLabelTree`, `readLabelSDDL`, `setLabelSDDL`,
  `processAlive`, `revertLabelJournal`, `revertSparingLiveSiblings`, `recoverLabelJournals` and
  `restoreLabels` are **deleted**. `labelBox` becomes: the `Writable()` refusal, `resolveBoxRoots`,
  `windowsBoxRoots`, then

  ```go
  for _, root := range roots {
      if err := winlabel.LabelTree(root, c.journal); err != nil {
          return fmt.Errorf("%w: %v", domain.ErrConfinementUnavailable, err)   // D4
      }
  }
  ```

  `Close` becomes `err := c.journal.Retire()` + the token release. `newTokenConfiner` calls
  `winlabel.Recover(home)`.

**Tests:**

- `confiner_windows_test.go` — the ~60 renames (D6/D7): `readLabelSDDL` → `winlabel.ReadSDDL`
  (42 sites), `setLabelSDDL` → `winlabel.SetSDDL`, `listLabelJournals` → `winlabel.ListJournals`,
  `readLabelJournal` → `winlabel.ReadJournal`, `writeLabelJournal` → `winlabel.WriteJournal`,
  `labelJournalPath` → `winlabel.JournalPath`, `processAlive` → `winlabel.ProcessAlive`,
  `labelJournal{…}` → `winlabel.Record{…}`. **No assertion, no scenario and no comment changes**
  beyond the identifiers. `TestWindowsFailedRootLabelWriteUnwindsItsJournalEntry` (`:818`) is the
  acceptance oracle for D3 — it must pass with its body untouched.
- **New** `winlabel/walk_other_test.go` (`//go:build !windows`) — the stubs report unsupported
  rather than silently succeeding. A stub that returned `nil` would make a future Linux test pass
  over a label that was never written.

**Acceptance:** `GOOS=windows go vet ./internal/platform/...` and both `go test -c` compiles green;
`GOOS=darwin go build ./...` green; `internal/platform/confiner_windows.go` is **under 400 lines**;
`grep -n "labelTree\|clearLabelTree\|readLabelSDDL\|setLabelSDDL\|processAlive\|recoverLabelJournals"
internal/platform/confiner_windows.go` is empty; `grep -c "rootLabelled" internal/platform/ -r`
is 0. Commit: `refactor(platform): winlabel owns the label walk — LabelTree journals before it
labels`.

---

## 6. Split what is left in `platform`, and rename the file that no longer confines

**What:**

- **New `internal/platform/wintoken_windows.go`** (`//go:build windows`) — moved from
  `confiner_windows.go`: the `createRestrictedToken` LazyProc (L53), `disableMaxPrivilege` (L62),
  `mintRestrictedLowToken` (L684–728) and `userProfileRoot` (L764–770), with the header comment
  explaining why a restricted token needs no `SeAssignPrimaryToken`. This is the one concern in
  the file that is about the **token**, not the box.
- **Rename `winconfine.go` → `winguard.go`** — after items 1–3 it holds the version floor, the
  labelling guardrails and the three notice delegations, and nothing named "confine". Update the
  file header to say so and to point at `winlabel` for the label mechanism.
  **Rename `winconfine_test.go` → `winguard_test.go`** with it.
- **`confiner_windows.go`** keeps exactly the backend: the header, `tokenConfiner`, `NewConfiner`,
  `NewReportConfiner`, `selectWindowsConfiner`, `newTokenConfiner`,
  `newTokenConfinerWithoutRecovery`, `Capabilities`, `Confine`, `Close`, `labelBox`,
  `resolveBoxRoots`, `resolveBoxRoot` and the two interface assertions. Its header comment is
  re-read end to end: the paragraphs describing the journal and the label walk now describe a
  module the file *composes*, and must say so rather than restating its internals.

**Tests:** none new. Every test in the package must pass unchanged; the renames are file-level.

**Acceptance:** gates green; `wc -l internal/platform/*.go` shows **every non-test file under 400
lines** (expected: `confiner_windows.go` ~340, `winguard.go` ~190, `wintoken_windows.go` ~95) and
`wc -l internal/platform/winlabel/*.go` likewise (expected: `journal.go` ~360, `walk_windows.go`
~230, `retire.go` ~155, `notice.go` ~135, `sddl.go` ~80, `doc.go` ~45, `walk_other.go` ~35). The
one file over the guideline is `confiner_windows_test.go`, by decision D7. Commit:
`refactor(platform): the token gets its own file; winconfine becomes winguard`.

---

## 7. Documentation — say where the label mechanism lives now

**What:**

- **`internal/platform/doc.go`** — the paragraph beginning *"The Windows backend is the one that
  mutates the machine"* gains a sentence naming `internal/platform/winlabel` as the module that
  owns the mandatory label, its journal and the wording, and `platform` as the composer that owns
  the token, the guardrails and the `Confiner` seam. Keep it to the existing register: what the
  reader needs to know before opening a file, not a changelog.
- **`docs/design/technical-design.md:105`** — the Phase-5 sentence names *"`confiner_windows.go` +
  `winconfine.go`"*. Update to `confiner_windows.go` + `wintoken_windows.go` + `winguard.go` +
  `internal/platform/winlabel`, in one clause, without re-describing the backend.
- **`TODO.md`** — the entry *"`internal/platform`'s two Windows confinement files exceed the
  file-size guideline"* (L659–…) is **removed** and replaced by a short dated note recording that
  it landed 2026-07-25 as `internal/platform/winlabel`, that the stale 581/572 figures it quoted
  had reached 804/777 by then, and that `confiner_windows_test.go` remains over the guideline **by
  decision** (D7) with the reason — so the next reader does not re-file it as a finding.
- **`cmd/apogee/probe_test.go`** — the comment naming *"platform's
  confinementJournalHome/labelJournalPath"* names `winlabel.Home` / `winlabel.JournalPath`. Comment
  only; the test body does not change.
- **`docs/design/confinement-execution-contract.md` §9** — read §9.2 (backend obligations) end to
  end and correct any sentence that names a file or locates a behaviour in one. If §9 names no
  files (it did not at the time of writing), change **nothing** and say so in the item's notes.
- **`docs/reviews/2026-07-24 - 00 - architecture-deepening-review.md`** — candidate 05's card gets
  its ✅ LANDED note in the ledger's house style: what was built against the card's sketch (the
  card said "three sub-modules"; the answer was one package with three internal seams plus the
  confiner as composer, because the walk and the journal must be co-located for the
  journal-before-label invariant to be internal), the D3 deepening, and the file counts before and
  after. Update the header line, the ledger paragraph and the *Recommended next step* section —
  after this, **03 and 06** are the outstanding cards and 03 is the strongest pick.

**Tests:** none. `go test ./...` still green (the doc changes touch one Go comment).

**Acceptance:** gates green; `grep -rn "winconfine.go" docs/ internal/ cmd/ --include="*.md"
--include="*.go"` returns only the archived plan docs, which are historical records and are **not**
edited. Commit: `docs(platform,todo,review): the Windows label mechanism is a module`.

---

## Whole-plan verification (run after item 7, before declaring done)

1. **Gates.** All six commands green from a clean tree.
2. **Nothing moved changed.** `git diff <plan-base>..HEAD -- internal/platform/confiner_windows_test.go`
   shows **only** identifier renames — no assertion, no scenario, no comment rewording. Same for
   the test bodies that moved into `winlabel`. This is the plan's central claim and the one thing
   a reviewer should check line by line.
3. **The boundary holds.** `grep -rn "airiclenz/apogee" internal/platform/winlabel/` is empty, and
   `TestPackageImportsNothingFromApogee` proves it for the tagged files too.
   **Every test is accounted for.** `winconfine_test.go` holds 26 tests today: **20 move**
   (5 in item 1, 9 in item 2, 6 in item 3) and **6 stay** — the three `TestWindowsBoxRoots…`,
   `TestWindowsProtectedRootsFromEnvironment`, `TestWindowsNetworkDenyDecisionFailsClosed` and
   `TestBelowWindowsFloor`, plus the `winTestRules` helper and item 1's two delegation tests.
   `go test ./internal/platform/... -run . -v 2>&1 | grep -c "^=== RUN"` must not drop.
4. **Manual, owner-only: a real Windows run.** On a Windows host at or above build 17763:
   `go test ./internal/platform/...` (the ~30 Windows-tagged lifecycle tests — real SACLs, real
   junctions, real journals), then one interactive run: `apogee` in a scratch workspace, an Auto
   edit, `apogee probe host` while it runs (expect the `labels:` line), quit, and `apogee probe
   host` again (expect no residue). **Nothing in items 1–6 can substitute for this** — the dev
   machine compiles that code and never executes it.
5. **Line counts, reported straight.** Record the before/after per file, and state plainly whether
   the total across `internal/platform` went up or down. The win here is a boundary and an
   invariant made structural, not a line count — if the total rose because doc comments were moved
   rather than deleted, say so, as candidate 04's ledger entry did.
