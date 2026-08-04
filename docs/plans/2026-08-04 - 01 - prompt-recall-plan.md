# Prompt recall — per-workspace Up-arrow prompt history, plus the selection-delete fix

- **Goal:** the prompt box remembers every sent input per workspace; with an empty box,
  Up loads the newest sent prompt, further Up/Down walk older/newer (terminal-style).
  Folds in ISSUES.md's "backspace/del on selected text should delete it" since it lands
  in the same key-handling seam.
- **Date:** 2026-08-04 · **Status:** unexecuted
- **Authoritative sources:** the ratified decisions below (owner grill session
  2026-08-04); code facts pinned at commit `520c3ea` — cited `file:line` refs are from
  that commit, and if lines have drifted the named symbols/behaviours govern, not the
  numbers. Domain language: `CONTEXT.md` (glossary), `layout.md` (prompt box),
  ADR 0011 (value-copied Model), ADR 0031 (engine/driver seams).
- **Standing requirements:** `skills: coding-standards`. Run `make check` before each
  commit. Never bump VERSION/CHANGELOG release headings (suggestion-only, see closing
  note). The Bubble Tea Model is value-copied: new sub-state must be plain values,
  slices replaced never written through (ADR 0011, `internal/tui/doc.go`).
- **Out of scope:** recording ask-answers; cross-process file locking (library-store
  precedent: no locking claims in v1); recall search (Ctrl+R style); cross-workspace
  recall; keyboard cut/copy bindings; the first-line/draft-stash recall variant
  (rejected at the grill); every other ISSUES.md item.

## Ratified decisions (owner, 2026-08-04)

1. **Trigger:** Up recalls only when the box is empty, or when it holds a freshly
   loaded recalled prompt that the user has taken no other action in. Any other action
   (typing, editing, mouse in the box, paste) ends recall mode and returns arrows to
   cursor duty. Down walks newer; Down past the newest empties the box.
2. **Storage:** `~/.apogee/prompts/<hex(sha256(abs-workspace))[:8]>.jsonl` — one JSONL
   file per workspace under the config home (`probeRecordKey` filename precedent,
   `internal/library/proberecord.go:172-179`). Nothing written into the project tree.
3. **What is recorded:** regular prompts, slash-command lines, and interjections.
   Ask-answers are NOT recorded.
4. **Name:** the feature is **prompt recall** ("recall mode", `promptRecall`).
   CONTEXT.md's "Avoid: 'history'" line stays; the browser keeps that word.

Supplementary calls made while planning (change only by superseding this plan):
recall is available at `stateIdle` and `stateRunning` (interjection typing);
`stateAwaitingAsk` keeps its arrow = choice-highlight semantics untouched. A recalled
`/command` must not open the autocomplete overlay while recall mode is active (the
overlay would steal Up/Down). Loaded text seats the caret at the end. The store
dedups consecutive identical entries and caps what `Load` returns at the newest 1000.
A nil recall host disables the feature entirely (bench/tests unaffected — ADR 0031
wire-silence is untouched; this is pure driver-side state).

## 1. Backspace/Del delete the prompt selection — ✅ DONE (2026-08-04)

NOTES (2026-08-04): two deviations from the item's literal shape. (a) The chokepoint still clears
`m.sel` unconditionally and STASHES the span in a local; the carve-out consumes that stash inside
the `inputEditable()` block, immediately before the `popInterjection` case. Short-circuiting at the
chokepoint itself would have run ahead of the modal overlays that claim every keypress first — the
/sessions browser's rename edit owns `backspace` — and would have left a stale selection alive on
the paths that return early. (b) The guard is `sel.active && sel.anchorOff != sel.headOff`, adding
the `active` conjunct so the predicate is exactly what paints the highlight (`highlightInput`): a
release that copied nothing leaves the offsets standing with `active` false, and that invisible span
must not swallow the key. Item 4 extends the same chokepoint/carve-out seam as planned.

**What:** Fix ISSUES.md line 5. Today `handleKey` unconditionally clears the mouse
selection as its first act (`m.sel = promptSel{}`, `internal/tui/model.go:789`), then
the textarea deletes one rune — the highlight vanishes and the selected text survives.
Carve `backspace` and `delete` out of that chokepoint: when `inputEditable()`
(`internal/tui/mouse.go:125-128`) and a non-empty selection exists
(`m.sel.anchorOff != m.sel.headOff`), delete the selected rune range instead of
forwarding the key. Implementation shape — a `promptEditor` helper
(`internal/tui/prompteditor.go`), following the rebuild-and-reseat pattern of
`removeCompletionToken` (`internal/tui/autocomplete.go:631-643`): normalise the span
(reversed drags — same posture as `selectionText`, `internal/tui/mouse.go:312`), slice
`[]rune(m.input.Value())`, `SetValue(head+tail)`, reseat with
`caretToOffset` (BYTE offset — bridge with `byteOffsetOf`, `internal/tui/mouse.go:296`)
at the span start, clear `sel`, then `recomputeAutocomplete()` + `layout()` exactly as
the generic edit branch does (`internal/tui/model.go:922-926`). Branch order stays
deliberate: the selection-delete case runs before the empty-box backspace→
`popInterjection` case (`internal/tui/model.go:913`) — a selection implies a non-empty
box so they cannot collide, but the order must say so. Update the doc comment on
`TestKeypressClearsSelection` (`internal/tui/mouse_test.go:292`), which pins the
chokepoint it no longer fully describes. Remove line 5 from `ISSUES.md`.

**Tests:** in `internal/tui/mouse_test.go` (harness: `modelWithInput`, `leftClick`/
`leftDrag`/`leftRelease`): backspace deletes the selected range and the caret lands at
the span start; `delete` behaves identically; a backwards (right-to-left) drag deletes
the same range; multi-byte/CJK content deletes by runes not bytes; deletion works while
`stateRunning`; with no selection, backspace still pops the queued interjection on an
empty box and still deletes one rune on a non-empty box (existing behaviour pinned).

**Acceptance:** `go build ./... && go vet ./...`;
`go test ./internal/tui/ -run 'Selection|Keypress|Interject'` passes;
`grep -n "backspace/del" ISSUES.md` finds nothing; `make check` passes.

**Commit:** `fix(tui): backspace/del delete the prompt selection`

## 2. Prompt-recall store (`internal/recall`) — ✅ DONE (2026-08-04)

NOTES (2026-08-04): three refinements to the item's literal text. (a) Consecutive dedup compares
against the newest record *for that workspace* rather than the file's last line outright, so a
hash-colliding stranger's line cannot hide a genuine duplicate — dedup then answers for exactly the
view `Load` returns. (b) `Append`/`Load` run the workspace through `filepath.Clean` before hashing
and stamping it, so `/w` and `/w/` are one workspace and one file; the store still resolves nothing
itself (ADR 0001) — the caller passes an absolute path. (c) The filename takes the first 8 hex
characters of the digest as written here (the `probeRecordKey` precedent hex-encodes 8 *bytes*);
collisions are a correctness non-event because every record names its workspace and `Load` filters.
Compaction triggers on the file's raw line count (so a file of pure junk still gets trimmed) and
keeps the newest 1000 well-formed records, dropping the malformed lines `Load` was already skipping.

**What:** New package `internal/recall` owning persistence. `Store` rooted at a
directory (the wire layer will pass `<config-home>/prompts`), API:
`New(dir string) *Store`, `Append(workspace, text string) error`,
`Load(workspace string) ([]string, error)` (oldest→newest of at most the newest 1000).
File per workspace: `<dir>/<hex(sha256(abs-workspace-path))[:8]>.jsonl`. One JSON
object per line — `{"ws":"<abs workspace>","t":"<RFC3339 UTC>","text":"…"}` — the `ws`
field makes files self-describing and lets `Load` filter out hash-collision strangers;
JSON string encoding keeps multi-line prompts one-line-per-record. `Append` opens
`O_APPEND|O_CREATE`, skips when `text` equals the file's current last entry
(consecutive dedup), and when the file exceeds 2000 lines compacts it to the newest
1000 via the temp-file+rename pattern (`internal/session/store.go:353` shape). Dir
`0700`, file `0600` (`internal/session/store.go:22-25` constants as precedent).
In-process `sync.Mutex` on the Store; no cross-process locking claims, matching the
library store's documented v1 posture. `Load` tolerates and skips malformed lines and
returns empty (not error) for a missing file. Package doc comment states the term
("prompt recall") and the invariants.

**Tests:** `internal/recall/store_test.go` over a `t.TempDir()`: append→load
round-trip preserves order and multi-line text; consecutive dedup skips only adjacent
duplicates (A,B,A all survive; A,A collapses); compaction triggers past 2000 lines and
keeps the newest 1000; `Load` filters records whose `ws` mismatches; malformed lines
are skipped without error; missing file loads empty; two workspaces map to two files.

**Acceptance:** `go build ./... && go vet ./...`; `go test ./internal/recall/`
passes; `make check` passes.

**Commit:** `feat(recall): per-workspace prompt-recall store`

## 3. Recall host seam and startup load — ✅ DONE (2026-08-04)

NOTES (2026-08-04): three notes on the literal text. (a) The Tests line asks that `stateRoots`
"creates the prompts dir"; it cannot — `resolveRoots` computes paths only by documented design
("directory creation is deferred to the writer that needs them") and item 2's store already creates
it lazily on the first `Append`. So `TestResolveRootsOverride` pins the PATH (`<home>/prompts`) and
a new `TestRecallHostBindsWorkspace` pins that the first append is what makes the directory, that a
second host over the same roots reads the same file back, and that another workspace recalls
nothing. (b) `appendRecallCmd` landed with this item — the What describes appends as
fire-and-forget swallowed-error Cmds, so the seam is incomplete without it — but it is wired to NO
call site: item 4 owns every send path, so "no key behaviour yet" holds. It is covered by its own
test rather than left dead. (c) `promptRecall` is introduced here carrying only `entries` (the
"recall state" this item's load writes into); item 4 adds the position and `active` flag its walk
needs. The type lives in the new `internal/tui/recall.go` beside the Msg and the Cmds, with the
field on `promptEditor` — the `promptSel`/`mouse.go` precedent — and `doc.go`'s
"names every file in it" narration gained its line.

**What:** Depends on item 2. Give the TUI a recall seam shaped like `SessionHost`
(`internal/tui/tui.go:37-62`): a `RecallHost` interface defined in
`internal/tui/tui.go` — `AppendPrompt(text string) error`,
`LoadPrompts() ([]string, error)` — with the workspace pre-bound by the host side, and
an `Options.Recall RecallHost` field (nil ⇒ feature off; the renderer resolves no
paths, per the `Options.ConfigHome` doc posture at `internal/tui/tui.go:179-185`). In
`cmd/apogee/wire.go`: extend `stateRoots` (`wire.go:1032-1043`) with the
`<home>/prompts` dir, construct `recall.New` there, and pass a small adapter binding
the resolved workspace (`resolveRoots`, `wire.go:1010-1044`) into `Options.Recall`.
In the TUI: `Init` gains a `tea.Cmd` that calls `LoadPrompts` and delivers a
`recallLoadedMsg{entries []string}`; the update handler stores the slice into the
prompt editor's recall state by replacement (never in-place mutation — ADR 0011
posture). Appends are fire-and-forget `tea.Cmd`s; an append error is swallowed
(non-fatal, renderer stays wire-silent). This item lands the seam and the load only —
no key behaviour yet.

**Tests:** in `internal/tui`: nil `Options.Recall` produces no load cmd and no recall
state; a fake `RecallHost` in tests (returning canned entries) results in the entries
landing in the editor's recall state after `Init`'s cmd message is stepped through
`Update`; a host `LoadPrompts` error leaves the TUI functional with empty recall
state. In `cmd/apogee`: `stateRoots` creates the prompts dir (extend the existing
roots test if one exists, else assert via the wire test harness).

**Acceptance:** `go build ./... && go vet ./...`;
`go test ./internal/tui/ ./cmd/apogee/` passes; `make check` passes.

**Commit:** `feat(tui): recall host seam and startup prompt load`

## 4. Up/Down recall navigation in the prompt box — ✅ DONE (2026-08-04)

NOTES (2026-08-04): five notes on the literal text. (a) The chokepoint clears recall UNCONDITIONALLY and
stashes the walk in a local (`rec := m.recall; m.dropRecall()`) — item 1's posture for `sel`, for item 1's
reason: the branches above it (session browser, picker, autocomplete) claim keys and return early, so a
conditional clear would leave the mode alive behind a modal. `recallKey` reads its position out of that
stash. (b) A recorded send is APPENDED to `entries`, not prepended: entries run oldest→newest (item 3),
so the end of the slice IS the front of the walk. (c) `recordSend` mirrors the store's consecutive dedup
in memory, so the in-memory view is exactly what a reload would return — without it, re-sending a
recalled line grew the walk a duplicate the store had refused. It also records nothing at all when
`Options.Recall` is nil, keeping "a nil host disables the feature entirely" true of the in-memory half
too. (d) No suppression branch was added to `recomputeAutocomplete`: `showRecall` dismisses the overlay
and deliberately does not recompute, and every site that DOES recompute has already ended recall (the
chokepoint, or the paste/resize/ask/click clears), so the invariant is structural rather than a dead
guard in a hot path. `TestRecallCommandOpensNoAutocomplete` pins it, with a typed-`/clear` contrast so
the assertion cannot pass vacuously. (e) BOTH placeholders gained `↑ recall`, not just the idle one:
recall is live at `stateIdle` and `stateRunning`, and a placeholder is only ever drawn on an empty box —
which is precisely the box where ↑ starts a walk.

**What:** Depends on items 1, 2, 3 (item 1 because both edit the same
`handleKey` chokepoint). Add a plain-value `promptRecall` struct to `promptEditor`
(`internal/tui/prompteditor.go:33-60`) — entries slice, position, `active` flag; zero
value = off (sub-model precedent). Behaviour, per ratified decision 1: in `handleKey`
(`internal/tui/model.go:785`), before the generic textarea fall-through
(`model.go:912-928`) and after the existing overlay claims (autocomplete, pickers,
session browser — untouched), when `stateIdle || stateRunning` and `inputEditable()`:
Up on an empty box activates recall and loads the newest entry; Up while active loads
older (clamp at oldest); Down while active loads newer; Down past the newest empties
the box and deactivates. Loading an entry: `SetValue` + `MoveToEnd` + `layout()`, and
deliberately NOT `recomputeAutocomplete()` — a recalled `/command` must not pop the
overlay that would steal the arrows; recall mode also suppresses the overlay while
active. Any other action ends recall: extend the item-1-carved chokepoint at
`model.go:789` so every keypress except the handled Up/Down (and the selection-delete
carve-out) clears recall state, and clear it at the same non-key sites where `sel` is
cleared (paste `model.go:483`, resize `:449`, ask-borrow `:529`, mouse click in the
box). `stateAwaitingAsk` is untouched — the empty-box arrow guard at
`model.go:892-906` keeps choice-highlight duty and runs before any recall check.
Recording, per ratified decision 3: append via the host cmd in `m.submit()`
(the reset at `model.go:1043`), the whole-input `/command` path (`model.go:996`), and
both interjection staging paths (`internal/tui/interject.go:158`, `:180`); NOT the
ask-answer path (`model.go:1430`). Each recorded send is also prepended to the
in-memory entries (slice replacement) so Up immediately after sending recalls it
without a reload; re-sending a recalled prompt re-records (the store's consecutive
dedup absorbs it). Update the placeholder legend (`internal/tui/prompteditor.go:72`)
to advertise `↑ recall`.

**Tests:** in `internal/tui` (helpers `keyUp`/`keyDown`,
`internal/tui/minilang_test.go:31-32`; fake `RecallHost` from item 3): Up on an empty
box loads the newest entry with the caret at the end; repeated Up walks older and
clamps at the oldest; Down walks newer and Down past the newest empties the box; after
loading, typing a rune ends recall mode and a subsequent Up moves the cursor instead
(multi-line entry); Up with a typed draft does nothing recall-related; a recalled
`/command` line opens no autocomplete overlay; Enter on a recalled entry submits it
and the append cmd fires with that text; sends from `m.submit()`, the `/command` path,
and interjection staging each record; ask answers do not record and awaitingAsk arrow
behaviour is unchanged (existing ask tests stay green); prompt-selection tests from
item 1 stay green; a sent prompt is immediately recallable before any reload.

**Acceptance:** `go build ./... && go vet ./...`; `go test ./internal/tui/` passes
(including `TestModelNoBuilderByValue`); `make check` passes.

**Commit:** `feat(tui): up-arrow prompt recall in the prompt box`

## 5. Docs: prompt recall enters the domain language

**What:** Depends on item 4 (documents landed behaviour). CONTEXT.md: add a glossary
entry **Prompt recall** near the prompt-box/interjection material — definition (the
per-workspace list of sent inputs the prompt box can walk with Up/Down), what is
recorded (decision 3), the empty-box/untouched trigger rule (decision 1), storage home
(decision 2), and an explicit cross-reference note that the session browser's list
keeps the word "history" (the existing "Avoid: 'history'" line at ~CONTEXT.md:125-126
stays, gaining a pointer to this entry). `layout.md`: a short paragraph in the prompt
box section describing recall mode, the legend hint, and the no-autocomplete-while-
recalling rule. `README.md`: mention `↑` recall wherever the key/interaction summary
lives (implementer locates the existing table/section; do not invent a new one).
`CHANGELOG.md`: entries for items 1–4 under the file's existing unreleased/current
convention — no release heading, no VERSION change.

**Tests:** none (docs). Verifier checks the claims against the landed behaviour of
items 1–4 rather than against tests.

**Acceptance:** `grep -n "Prompt recall" CONTEXT.md layout.md` hits both;
`grep -n "recall" CHANGELOG.md` hits; CONTEXT.md still contains its Avoid-"history"
line; `make check` passes (docs must not break lint/format hooks).

**Commit:** `docs: prompt recall in the domain language and layout spec`

## Suggested version bump (not performed)

Items 1–4 are user-visible feature work; when the owner next cuts a release, a minor
bump (v0.8.0 → v0.9.0) would fit. No version identifier is changed by this plan.
