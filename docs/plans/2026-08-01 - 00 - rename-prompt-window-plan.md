# Plan — `/rename` names from a bounded window of the session, not just the first prompt

**Goal.** A bare `/rename` typed late in a session currently re-derives a name from the very first
user message and nothing else, so a session that opened on one task and moved to another gets named
for the task it left. Widen the naming call's input to a **bounded, budget-driven window of the
user side of the transcript** — always the opening request plus the most recent ones, filled inward
from the newest while a rune budget lasts — and instruct the model to name the **dominant thread,
biased recent**. The automatic first-prompt call keeps reading exactly one prompt, because at the
moment it fires exactly one exists.

**Date.** 2026-08-01
**Status.** Ready to execute — not started.

**Authoritative sources.**

- The **Ratified design** block below governs. Where any other document disagrees with it —
  including ADR 0022, its 2026-07-31 addendum, the amendment item 1 writes, and the archived
  `2026-07-31 - 02 - session-auto-titling-plan.md` — the Ratified design block wins.
- Current behaviour is pinned at `HEAD` = `e201ee2` (the closeout of the auto-titling plan).
  Relevant ground truth: `internal/title/title.go`, `internal/tui/autotitle.go`,
  `internal/tui/transcript.go:236` (`firstUserText`), `internal/tui/tui.go:262`
  (`Options.GenerateTitle`), `cmd/apogee/title.go` (`titleWiring.generate`).
- ADR 0022's 2026-07-31 addendum
  (`docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md`) — the naming call's
  ratified posture. Nothing in this plan changes that posture: the call stays cosmetic, out-of-band,
  never a Turn, never a Mechanism, `Rename`-only, never-clobber.

**Standing requirements (every item).**

- Run `make check` before every commit.
- Never touch `VERSION` or any `CHANGELOG` release heading. No AI-attribution trailers.
- No `strings.Builder` (or any no-copy type) held by value anywhere reachable from the Bubble Tea
  `Model` (ADR 0011, `internal/tui/doc.go`).
- Commit directly to `main`, one commit per item.
- Invoke with the `coding-standards` skill forwarded to implementer and verifier.
- Any authorised deviation from an item's text lands as a dated `NOTES` line under that item.

**Out of scope.**

- `title.Sanitize` and the whole apply path — `applyTitle`, `flushPendingTitle`, `titleTouched`,
  `pendingSource`, the never-clobber rule, the stash. Untouched.
- The automatic call's firing rules — `maybeAutoTitle`'s four preconditions, the `autoTitleFired`
  latch, `/clear` and `/new` rotation, resumed sessions. Untouched.
- `/rename <text>` (the argument form). It never asks the model; untouched.
- The `auto-title:` config key, the 5-minute request timeout, `WithMaxRetries(0)`, the per-call
  client and its binding. Untouched.
- The heuristic `sessionTitle` in `internal/tui/model.go` and its `Session <date>` fallback.
- Any version bump (see the closing note).

---

## Ratified design

Owner-ratified in the 2026-08-01 session unless marked *(author's call)*. Author's calls are
defaults chosen to keep the run moving — overrule them at plan review, not mid-run.

1. **Bare `/rename` reads the user side of the session, not just the first prompt.** The verb means
   "name this for what it is", and late in a session that is no longer the opening request.
2. **One budget-driven selection, not a two-tier scheme.** The earlier sketch — whole transcript,
   falling back to first-plus-last-3 when too long — was rejected because the fallback is not
   reliably smaller than what it falls back from (at four or fewer user entries it *is* the whole
   transcript, and one pasted stack trace defeats an entry-count rule outright). Instead: one rule,
   always bounded, under which "the whole user side" is simply what a short session yields.
3. **The selection.** Drop empty entries. The **opening request and the last three** are mandatory
   and always included. Remaining entries are added **newest-first** while a total rune budget
   lasts. Every included entry is excerpted to a per-entry cap.
4. **Rendered chronologically, numbered by original position, with at most one elision marker.**
   Because the mandatory set is a head plus a tail and the fill runs newest-first, the dropped
   entries always form exactly one contiguous run — that invariant is pinned by a test, not
   assumed.
5. **The model is instructed to name the dominant thread, biased recent.** The browser row exists so
   a session can be found again, and people look for what they were last doing. So: name the main
   thread rather than enumerate requests, and when the session has moved on to a different task,
   name what it moved to. This must be explicit in the system prompt and pinned by a test — small
   models answer the last thing they read, so leaving it implicit yields recency by accident rather
   than by instruction.
6. **The automatic call still reads exactly one prompt.** At first-prompt submit exactly one user
   entry exists, so this is not a restriction but an identity: the same selection rule applied to a
   one-element window. The automatic path's rendered user message is byte-for-byte what it is
   today.
7. **One shared system prompt for both forms** *(author's call)*. Worded to read correctly at N=1
   rather than duplicated per form. This does change the automatic call's system message — a
   deliberate, accepted consequence; the alternative is two instructions that drift apart.
8. **Numbers** *(author's call)*: per-entry cap **400 runes** in the multi-entry form; total budget
   **6000 runes**; mandatory tail **3** (the owner's number). The single-entry form keeps today's
   **1500**-rune excerpt unchanged. Rationale: 6000 runes is roughly a 4× prefill increase over
   today on a single-slot local server, paid only on a call the human explicitly asked for and
   waited on; the mandatory set is exempt from the budget, since dropping a mandatory entry to
   satisfy a budget would defeat the selection.
9. **Interjections are not requests** *(author's call)*. Only `entryUser` entries ride the window;
   `entryInterjected` (mid-Exchange steering, deliberately not an `entryUser` —
   `internal/tui/transcript.go:114`) is excluded, following `firstUserText`'s precedent.

---

## 1. ADR 0022 addendum — record the naming call's input window — ✅ DONE (2026-08-01)

**What.** Amend the existing `### Addendum (2026-07-31)` section of
`docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md` with one new bullet,
placed after the "A user's title always wins (never-clobber)" bullet, recording that the two naming
forms read different amounts of the session: the automatic call reads the first prompt (the only one
that exists when it fires), while an explicitly requested regeneration reads a bounded, budget-capped
window of the user side — the opening request plus the most recent, filled newest-first — and is
instructed to name the dominant thread, biased recent. State the *why* in one clause: a session that
moved on must be findable by what it moved to. Do not restate the numbers (they are implementation
detail and will drift); do not touch the Decision, the Consequences, or any other bullet. Keep the
file's 98-column wrap and its cross-link style. Date the amendment 2026-08-01 in the bullet's own
text, since the section heading keeps its original date.

**Tests.** None (documentation).

**Acceptance.**
- `grep -n "2026-08-01" docs/adr/0022-*.md` → hits the new bullet.
- `sed -n '/### Addendum (2026-07-31)/,/^## /p' docs/adr/0022-*.md` → shows the new bullet in
  position, with the six pre-existing bullets intact and unedited.
- `awk 'length > 98' docs/adr/0022-*.md` → no output.
- `make check` → passes.

**Commit.** `docs(adr): record the naming call's input window`

## 2. `internal/title`: multi-prompt selection, budget, and the revised instruction — ✅ DONE (2026-08-01)

NOTES (2026-08-01): three details the item's text left open, all decided in the direction the item
implies. (a) A multi-entry excerpt has its whitespace collapsed (reusing `collapseWhitespace`)
before the 400-rune cap, so "one numbered line per included entry" holds literally even when a
request pasted a stack trace. (b) The 1-based numbering counts positions in the *non-empty*
requests — empties are dropped first, as the item orders — so the numbering gap and the elision
marker's count always agree. (c) `selectWindow` takes the budget as a parameter (`windowBudgetRunes`
at the one call site) because the Tests bullet "mandatory entries are included even when they alone
exceed the budget" is otherwise unreachable: 4 mandatory entries × the 400-rune cap = 1600, which
can never exceed 6000.

**What.** In `internal/title/title.go`:

- Change `Prompt` to `Prompt(prompts []string, workspaceBase string, date time.Time) provider.Request`.
- Add the constants of Ratified design 8: per-entry cap (400), total budget (6000), mandatory tail
  (3). Keep `promptExcerptRunes = 1500` for the single-entry form. Every count is in **runes**.
- Add unexported selection: drop entries that are empty after `TrimSpace`; if one remains, render
  today's single-entry user message **unchanged** (`The user's first request:` header, 1500-rune
  `excerpt`); if several remain, always include index 0 and the last three, then add the rest
  newest-first while the running excerpt total stays within budget, and render chronologically.
- Multi-entry rendering: `Workspace:` / `Date:` context as today, then a header naming the block as
  the user's requests in this session oldest first, then one numbered line per included entry using
  its **original 1-based position**, with a single elision marker line standing where the omitted
  run sits and saying how many were omitted, then the existing closing `userInstruction`.
- Revise `systemInstruction` per Ratified design 5: it must (a) read correctly for one request as
  well as many, (b) ask for the main thread of the work rather than a list of the requests, and
  (c) say that when the session has moved to a different task the title names what it moved to.
  Keep the existing constraints verbatim in spirit — 3-to-8 words, one line, plain text, task only
  (never project/folder/date), nothing but the title.
- Update the package doc comment to describe the window rather than "the first prompt".
- **`Sanitize` and every helper below it are untouched.**

Callers are NOT updated here — item 3 owns the seam. This item leaves `internal/title` compiling and
fully tested on its own; `internal/tui` and `cmd/apogee` still pass a single string and will not
build until item 3 lands, so **items 2 and 3 must be verified as a pair**: run item 2's acceptance
with `go test ./internal/title/`, and defer `make check` to item 3.

**Tests.** Extend `internal/title/title_test.go` (table-driven, `t.Parallel()`, per the repo's
testing conventions). Update the existing `TestPrompt*` tests to the new signature. Add:

- Single-entry equivalence: the rendered user message for a one-element slice is byte-identical to
  the current output for that prompt (pin the expected string literally, including the
  `The user's first request:` header and the 1500-rune cap).
- Empty and whitespace-only entries are dropped before selection; a slice of nothing but those
  behaves as the empty case.
- Empty slice: define and pin the behaviour (the callers guard, so this is a contract test, not a
  path anyone reaches).
- Mandatory set: with many entries, the first and the last three always appear.
- Budget fill: entries are added newest-first, and the total excerpt runes stay within budget;
  a session long enough to trip it omits from the middle only.
- Exactly one elision marker, and it reports the correct count.
- Chronological render order with original 1-based numbering across the gap.
- Per-entry cap applies in the multi-entry form; a single huge entry cannot blow the window.
- Rune-not-byte counting on a CJK window (the cap is characters, not bytes).
- Mandatory entries are included even when they alone exceed the budget.
- The system instruction carries the dominant-thread and moved-on clauses (grep-style assertion on
  the constant, so the intent cannot be silently reworded away).

**Acceptance.**
- `go test -count=1 ./internal/title/` → ok.
- `go vet ./internal/title/` → clean; `gofmt -l internal/title` → no output.
- `go build ./internal/title/` → exit 0.

**Commit.** `feat(title): name a session from a bounded window of the user's requests`

## 3. Widen the naming seam to a prompt window (mechanical — no behaviour change)

**Depends on item 2.**

**What.** Ripple the `[]string` signature through both sides of the seam, changing *what the seam
carries* and nothing about *what any caller sends*:

- `internal/tui/tui.go`: `GenerateTitle func(ctx context.Context, prompts []string) (string, error)`,
  with its doc comment updated to describe a window of the user's requests and to state that the
  automatic path sends exactly one.
- `internal/tui/autotitle.go`: `titleCmd` takes `prompts []string`; `maybeAutoTitle` passes
  `[]string{firstUserText}`; `runRename`'s bare form passes `[]string{first}` — **still the first
  prompt only**, so this item is behaviour-preserving and item 4 makes the single behavioural change
  in one reviewable diff.
- `cmd/apogee/title.go`: `titleWiring.generate(ctx context.Context, prompts []string)`, forwarding to
  `title.Prompt`. Its doc comment loses "firstUserText".
- Update every construction of a fake seam in the tests of both packages, and
  `TestLiveGenerateTitle` in `cmd/apogee/title_test.go` (gated by `APOGEE_LIVE_ENDPOINT`).

**Tests.** No new behavioural tests; the existing suites must pass with only signature edits. Where
a test's fake seam captured the string it was called with, it now captures the slice — assert the
one-element slice explicitly, so item 4's change to `runRename` shows up as a failing assertion
rather than passing silently.

**Acceptance.**
- `make check` → all Phase-2 gates pass (this is where item 2's tree-wide green is proven).
- `go test -count=1 ./internal/tui/ ./cmd/apogee/` → ok.
- `git diff --stat HEAD~1` shows no test asserting a *changed* title outcome — this item moves no
  behaviour.

**Commit.** `refactor(title): widen the naming seam to a prompt window`

## 4. Bare `/rename` names from the session's user side

**Depends on item 3.**

**What.**

- `internal/tui/transcript.go`: add `userTexts() []string` beside `firstUserText` — every
  `entryUser` entry's text, in order, interjections excluded (Ratified design 9). Document why
  interjections are out.
- `internal/tui/autotitle.go`: `runRename`'s bare form calls `m.transcript.userTexts()` instead of
  `firstUserText()`. The "nothing to name yet" refusal now triggers on an empty window rather than
  an empty first entry — same note text, same behaviour when nothing has been asked.
- `maybeAutoTitle` is unchanged: still exactly one prompt (Ratified design 6).
- Update the file-header comment block at the top of `autotitle.go`: it currently says a record is
  "named from the first thing the human asked", which stays true of the automatic call and becomes
  false of bare `/rename`. This item owns that wording; no other item may touch it.

**Tests.** In `internal/tui/autotitle_test.go`:

- Bare `/rename` after several user turns hands the seam every user entry, in order.
- The automatic call still hands the seam exactly one entry, even when the transcript already holds
  several (drive it through a fresh record, then assert the slice length).
- Interjections do not appear in the window.
- Bare `/rename` with no user entry still takes the "nothing to name yet" refusal path.
- The existing bare-`/rename` tests (`TestRenameBareRegeneratesOverAManualName`,
  `TestRenameBareFailureNotesAndKeepsTheTitle`, `TestRenameBareRefusals`) keep passing — the fold,
  the never-clobber interaction, and the notes are untouched by this item.

**Acceptance.**
- `go test -count=1 ./internal/tui/` → ok.
- `go test -count=1 -race ./internal/tui/` → ok.
- `make check` → all Phase-2 gates pass.
- `grep -n "firstUserText" internal/tui/autotitle.go` → no hit in `runRename` (the automatic path
  may still reference the first prompt).

**Commit.** `feat(tui): bare /rename names from the whole user side of the session`

## 5. README and CHANGELOG

**Depends on items 1, 2, 3, 4.**

**What.**

- `README.md`: the `/sessions` bullet (around lines 173-180) describes automatic naming and its
  fallback — leave that intact. Add, in the same voice and at most one clause or short sentence,
  that a bare `/rename` re-reads the session and names it for what it has become, so a session that
  moved on can be renamed for where it ended up. Check the command table's `/rename` row (around
  line 151) and extend it only if it claims something now false.
- `CHANGELOG.md`: a **Changed** entry under `## [Unreleased]` (create the `### Changed` subsection
  if the Unreleased block does not have one; do not reorder existing subsections). House voice —
  bold lead, sub-bullets if warranted. Cover: bare `/rename` now names from a bounded window of the
  user's requests rather than the first one alone; the window is the opening request plus the most
  recent, capped so a long session cannot balloon the call; the model is asked for the dominant
  thread, biased recent; automatic naming is unchanged. Link the ADR as the existing auto-titling
  entry does. **Do not add or alter a release heading, and do not touch `VERSION`.**

**Tests.** None (documentation).

**Acceptance.**
- `grep -n "rename" README.md` → the new wording is present in the `/sessions` bullet.
- `grep -n "Unreleased" -A 400 CHANGELOG.md | grep -n "rename"` → the new entry sits inside
  `## [Unreleased]`.
- `git diff HEAD~1 -- VERSION CHANGELOG.md | grep -E '^\+## \['` → no output (no release heading
  added).
- `make check` → passes.

**Commit.** `docs: describe /rename naming from the session's user side`

---

## Suggested version bump

No item in this plan changes a version identifier, and none may. Once items 1-5 land, the
outstanding suggestion from the auto-titling plan still stands — a **minor bump to 0.11.0** at the
owner's next release cut — and this plan's change folds into it rather than warranting one of its
own: it refines a feature that has not yet been released.
