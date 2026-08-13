# Open-defects fix wave — bare-accept pickers, clipboard fallback, prompts as embedded files

**Goal:** close the three actionable open defects in `ISSUES.md`: (1) accepting the `/model` or
`/server` autocomplete runs the command (opens the picker) the way `/settings` does; (2) copying
selected text reaches the system clipboard even when the terminal ignores OSC 52; (3) the four
cited hard-coded prompt literals (plus their same-file sibling prompt consts) become plain `.txt`
files embedded into their packages.

**Date:** 2026-08-13
**Status:** unexecuted
**Sized for:** ~200k-context host
**Skills:** coding-standards

**Authoritative sources:**
- `ISSUES.md` "Open defects" (as of commit `60e5e4b`) — the three defect entries this plan closes.
- Scout reports 2026-08-13 (this plan's write session) — all file:line citations below were
  verified there.
- House embed convention: `internal/scheme/scheme.go:21` (`embed.FS` over a subdirectory),
  `internal/scheme/doc.go:11` (rationale), `internal/config/defaults.go:24`,
  `internal/tui/logo.go:13`.

**Ratified design calls (owner, 2026-08-13, via AskUserQuestion at plan-write time):**
1. "Plain files" = **`//go:embed` in-package assets**, not user-overridable config files. Single
   binary preserved; the probe battery's fingerprint invariant preserved; prompts editable in the
   repo, never at runtime.
2. `toolloop.go`'s directive builder keeps its branching and placeholder substitution **in Go**;
   only the fixed sentence fragments move into embedded asset files.
3. Prompt scope = the four ISSUES-cited sites **plus same-file sibling prompt consts** (title's
   `userInstruction`/`windowHeader`, compact's inline tail sentence, battery's `candidatePrompt`),
   so no converted file is left half-converted. The ~9 other mechanism prompt literals
   (`cot.go`, `decompose.go`, `library.go`, `emptyresponse.go`, `internal/agent/compact.go`'s
   `overflowBridge`, …) are **out of scope** — a follow-up sweep.
4. Clipboard fallback = **`github.com/atotto/clipboard` promoted from indirect to direct**
   (already CGO-free and already linked into the binary via `bubbles/textarea`), wrapped behind an
   injectable package-level func in `internal/tui` for testability. OSC 52 stays first and
   unchanged; the system write is best-effort with errors swallowed; the `copied N chars` flash
   stays unconditional.
5. Autocomplete fix = a new **`runsBareAtAccept` bool on `commandSpec`**, set on `model` and
   `server` only. `takesArgs` is untouched (the parser needs it at `internal/tui/command.go:185`);
   `/confine`, `/rename`, `/schedule`, `/color-scheme` keep today's splice-in-place behavior.
   Authority: the ISSUES item text ("update this so that /server and /model behave the same way
   as /settings") plus the 2026-08-13 scout's approach assessment.
6. Plan-author mechanical decisions (binding for uniformity): each converted package gets a
   `prompts/` subdirectory embedded via `//go:embed prompts/*.txt` into an unexported `embed.FS`;
   every asset file ends with exactly one trailing newline and the loader strips exactly that
   final newline, so the in-memory string is **byte-identical** to today's const; the embed var
   and loader carry a doc comment naming why the text is a file (the ISSUES defect) and, for the
   battery, restating the `BatteryVersion` invariant.

**Out of scope:**
- The `SetSampling` field-wise merge (owner deferred it 2026-08-13).
- The approved-out-of-workspace-write Execute gap (pending owner call; needs a grill).
- The security/hygiene and test-gap residuals in `ISSUES.md` (declined at scope ratification,
  2026-08-13).
- The remaining ~9 mechanism/agent prompt literals (design call 3).
- Any user-overridable prompt surface (design call 1).
- Version bumps (see the closing note).

---

## 1. Accepting the bare `/model` or `/server` completion runs the command — ✅ DONE (2026-08-13)

NOTES (2026-08-13): also refreshed the stale `commandByName` doc comment (`command.go:254`), which
described the same two-way accept branch as the four sites the item named.

**What:** Add `runsBareAtAccept bool` to `commandSpec` (`internal/tui/command.go:82-88`) with a
doc comment stating its meaning: the verb takes args, but its bare form is meaningful and safe to
fire from the completion menu (it opens a chooser and mutates nothing until the picker's own
accept). Set it on `model` (`command.go:156`) and `server` (`command.go:161`) only. In
`acceptAutocomplete` (`internal/tui/autocomplete.go:747`), splice-in-place only when
`spec.takesArgs && !spec.runsBareAtAccept`; a `runsBareAtAccept` verb falls through to the
existing run-at-accept path, which already synthesizes the bare `parsedInput` (nil args) that
`runModelCommand`/`runServerCommand` (`internal/tui/picker.go:172`, `:266`) turn into their
pickers, and already routes through `commandRunnable` for the idle-only refusal. Manual
`/model qwen` is untouched — an argument token never reaches the command-accept path
(`caretSlashToken`, `autocomplete.go:329-335`). Update the stale doc comments that describe the
old two-way branch: `command.go:62-67`, `autocomplete.go:30-32`, `autocomplete.go:726-728`, and
the accept-behavior prose in `layout.md:1444-1447`.

**Files:** `internal/tui/command.go`, `internal/tui/autocomplete.go`,
`internal/tui/minilang_test.go`, `layout.md`

**Tests:**
- New: accepting the `/mod` completion (Tab and Enter) runs `/model` — the picker opens
  (`m.picker.open` with the expected kind) and no `/model ` text is spliced into the draft;
  same for `/serv` → `/server`. Model the tests on `TestAutocompleteAcceptWithTab`
  (`minilang_test.go:610`) and `TestAcceptCommandRunsItAndKeepsTheDraft` (`:715`).
- New: a registry pin that exactly `{model, server}` carry `runsBareAtAccept` (drift guard, in
  the style of the existing registry tests in `internal/tui/command_test.go`).
- Existing stays green: `TestAcceptConfineSplicesWithoutFiring` (`minilang_test.go:767`) — the
  splice branch is still exercised by `/confine`; `TestModelCommandIsIdleOnly` /
  `TestServerCommandIsIdleOnly` (`picker_test.go:312`, `:827`) — `takesArgs` remains true.

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `fix(tui): accepting the bare /model or /server completion opens its picker`

---

## 2. System-clipboard fallback for copy — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the item's Files list named `go.sum`, but it needed no change — the module's
hashes were already recorded there from the indirect dependency, so `go mod tidy` only moved the
`require` line in `go.mod`.
NOTES (2026-08-13): retry — per the dispatch DECISION the unrelated `ISSUES.md` change was set
aside with `git stash push -- ISSUES.md` (now `stash@{0}`, message "ISSUES.md: unrelated in-band
provider-error defect entry, set aside during item 2 of the 2026-08-13 - 03 fix wave"). It held two
things, neither part of this plan: a new open-defect entry on mid-stream in-band provider errors,
and a raw draft of a marking-convention note.
NOTES (2026-08-13): `ISSUES.md` is being edited BY HAND CONCURRENTLY — it went dirty again ~26s
after the stash with a reworked `## Marking Convention` section and `[P]` markers on this plan's
three defects. That live edit was deliberately left in place (not stashed, not reverted): it is not
item 2's file, item 7 owns `ISSUES.md`, and a concurrent write would collide with the human's
editor buffer. Item 2's own five files are unaffected and the tree is otherwise clean.
NOTES (2026-08-13): ACTION FOR THE RUN — the stashed provider-error defect entry is NOT in the
current `ISSUES.md` (`grep -c provider_unavailable ISSUES.md` = 0); it now survives only in
`stash@{0}`. Restore it (`git stash pop stash@{0}`, resolving against the hand edit) before the run
closes, and note that item 7's "touches only ISSUES.md" acceptance is measured against whatever the
hand edit leaves.

**What:** Promote `github.com/atotto/clipboard v0.1.4` from `// indirect` to a direct dependency
(`go.mod:26`; run `go mod tidy`). Add `internal/tui/clipboard.go`: an unexported package-level
seam `var writeSystemClipboard = clipboard.WriteAll` (doc comment: the injectable seam mirrors
`Options.ExternalEditSpec`, `internal/tui/tui.go:432`) and a `systemClipboardCmd(text string) tea.Cmd`
that calls the seam inside the returned Cmd (Cmds already run off the Update goroutine),
swallows any error, and returns a nil msg — a failed fallback is not a failed copy. In
`copyFlash` (`internal/tui/mouse.go:582`), extend the batch at `:589-592` to three Cmds:
`tea.SetClipboard(text)` first and unchanged (OSC 52 keeps working over SSH and in capable
terminals), `systemClipboardCmd(text)` second, the flash tick third. The flash message stays
unconditional. Amend the doc comment at `mouse.go:578-580`: OSC 52 remains the primary,
SSH-safe channel; the system write is the fallback for terminals that ignore OSC 52 (the
ISSUES defect). If `internal/tui`'s `doc.go` file map is enforced by a docmap test, add the new
file there.

**Files:** `internal/tui/clipboard.go` (new), `internal/tui/mouse.go`, `internal/tui/mouse_test.go`,
`internal/tui/doc.go`, `go.mod`, `go.sum`

**Tests:**
- New: substitute `writeSystemClipboard` with a recorder, drive a drag-select-release (reuse the
  harness from `TestDragSelectsAndCopies`, `mouse_test.go:166`), execute the returned batch's
  Cmds, and assert the recorder received exactly the selection text.
- New: a `writeSystemClipboard` that returns an error still leaves the `copied N chars` flash
  and panics nothing.
- Existing mouse suite (`mouse_test.go`) stays green — it asserts only non-nil Cmd + flash text.

**Acceptance:** `go build ./... && go test ./internal/tui/ && go vet ./internal/tui/`

**Commit:** `fix(tui): copy falls back to the system clipboard when OSC 52 is ignored`

---

## 3. `internal/context`: compaction prompts become embedded files — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the embed FS, loader (`mustPrompt`) and the three prompt vars live in `compact.go` — the item's Files line names that file, and the package stays at four non-test .go files, so no `prompts.go` was added.
NOTES (2026-08-13): `internal/context` carries no doc.go file map and no docmap test (4 non-test .go files, under the house ~10-file threshold), so the doc.go update names the three new prompt assets in the package narration instead of adding a map.
NOTES (2026-08-13): the loader also normalises CRLF→LF before stripping the trailing newline (beyond design call 6's literal wording), matching `internal/tui/logo.go` — without it a `core.autocrlf=true` checkout would bake `\r` into every prompt and break the byte-identity the design call requires.
NOTES (2026-08-13): `summaryMessagePrefix`'s `"\n\n"` joiner stays in code (as the plan allows for the tail fragment), so its asset holds no trailing blank line — the item's own loader test requires every asset to load without a trailing newline. The var's value is byte-identical to the old const.

**What:** Create `internal/context/prompts/` with the prompt text as `.txt` assets and an
`//go:embed prompts/*.txt` `embed.FS` in `compact.go` (or a small new `prompts.go` if cleaner
under the package's file map). Move: `summaryInstruction` (`internal/context/compact.go:94-100`),
the inline user-message tail `"\n\nSummarize the conversation above as instructed."` at
`compact.go:68` (as its own asset; the `\n\n` joiner may stay in code), and `summaryMessagePrefix`
(`compact.go:104`). Each const becomes a package var loaded from the FS at package init, with the
loader stripping exactly one trailing newline so the strings are byte-identical to today (design
call 6). Update the package `doc.go` file map for the new files (docmap test).

**Files:** `internal/context/compact.go`, `internal/context/prompts/` (new assets),
`internal/context/doc.go`

**Tests:** existing `internal/context/compact_test.go:172` (asserts the request's system message
is identical to the summary-instruction var) stays green unmodified — that is the byte-identity
proof. Add one loader test: every embedded prompt asset is non-empty and carries no trailing
newline after load.

**Acceptance:** `go build ./... && go test ./internal/context/`

**Commit:** `refactor(context): compaction prompts live in embedded plain files`

---

## 4. `internal/title`: title prompts become embedded files — ✅ DONE (2026-08-13)

NOTES (2026-08-13): `internal/title` has no `doc.go` — its package doc comment lives at the head of `title.go` and the package holds one non-test .go file, far under the house ~10-file docmap threshold (four other internal packages are the same). The item's "update the package doc.go file map" was therefore satisfied by extending the existing package doc in `title.go` to narrate the three new prompt assets; no `doc.go` was added and no content was moved between files.
NOTES (2026-08-13): the loader (`mustPrompt`) also normalises CRLF→LF before stripping the trailing newline, matching item 3's `internal/context` loader and `internal/tui/logo.go` — without it a `core.autocrlf=true` checkout would bake `\r` into every prompt and break the byte-identity design call 6 requires.
NOTES (2026-08-13): byte-identity was verified mechanically, not only by the existing contains-phrase tests: the three loaded assets were compared against the concatenated const values extracted from `git show HEAD:internal/title/title.go` — all three identical, each asset ending in exactly one newline.

**What:** Same conversion as item 3 for `internal/title/title.go`: `systemInstruction`
(`title.go:99-105`), `userInstruction` (`title.go:109`), `windowHeader` (`title.go:114`) move to
`internal/title/prompts/*.txt` behind an `embed.FS`; consts become vars, byte-identical per
design call 6. The `fmt.Sprintf`/`strings.Join` assembly around them (`title.go:177-215`) is
untouched. Update the package `doc.go` file map.

**Files:** `internal/title/title.go`, `internal/title/prompts/` (new assets),
`internal/title/doc.go`

**Tests:** existing `internal/title/title_test.go:437-441` (five contains-phrase assertions on
the system instruction) and `:102`/`:246`/`:256` (identity assertions on
`userInstruction`/`windowHeader`) stay green unmodified — the wording-drift pin the test's own
comment demands now also guards the asset files. Add the same non-empty/no-trailing-newline
loader test as item 3.

**Acceptance:** `go build ./... && go test ./internal/title/`

**Commit:** `refactor(title): title prompts live in embedded plain files`

---

## 5. `internal/mechanisms`: the tool-loop directive fragments become embedded files — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the sentence-separating space that ended five of the eight literals is appended in Go (`mustPrompt(...) + " "`) instead of living as trailing whitespace at the end of an asset file — following item 3's `internal/context` precedent (`compact.go`'s `summaryMessagePrefix + "\n\n"`). Byte-identity per design call 6 is unaffected: each package-level var equals its old const exactly, and only the on-disk asset differs, by the one invisible character an editor that trims line ends would silently delete.
NOTES (2026-08-13): the loader (`mustPrompt`) normalises CRLF→LF before stripping the trailing newline, matching items 3 and 4 and `internal/tui/logo.go`, so a `core.autocrlf=true` checkout cannot bake `\r` into a directive fragment.
NOTES (2026-08-13): byte-identity was verified mechanically, not only by the existing behaviour tests — a scratch test compared all eight loaded vars against the literals extracted from `git show HEAD:internal/mechanisms/toolloop.go` (all identical; the scratch test was deleted afterwards).
NOTES (2026-08-13): the item's Files line does not name `toolloop_test.go`, but its Tests line mandates the new loader test; it was appended there (the package's existing tool-loop tests are unmodified) and is listed above.

**What:** `buildToolLoopDirective` (`internal/mechanisms/toolloop.go:117-136`) keeps its
`strings.Builder`, its six branches, and its `fmt.Fprintf` placeholder substitution (design
call 2). The fixed text moves: one `.txt` asset per fragment under
`internal/mechanisms/prompts/`, named for its role (the `:122` header format string with its
`%s`, the `:123` fixed sentence, the `:125` task format, the `:128`/`:131` file-list formats,
and the three mutually exclusive tail sentences at `:129`, `:132`, `:134`), embedded via one
`embed.FS`. Format verbs (`%s`) live inside the asset text and are preserved verbatim;
byte-identity per design call 6 — the built directive's output must be unchanged for every
branch. Update the package `doc.go` file map.

**Files:** `internal/mechanisms/toolloop.go`, `internal/mechanisms/prompts/` (new assets),
`internal/mechanisms/doc.go`

**Tests:** existing `internal/mechanisms/toolloop_test.go:24-25` (directive contains
`"in a loop"`) and `internal/mechanisms/writedetection_test.go:220-235`
(`TestToolLoopDirectiveCreditsEditToolWrite`, the `filesWritten` branch) stay green unmodified.
Add the loader test (non-empty, no trailing newline, each format asset contains the expected
number of `%s` verbs).

**Acceptance:** `go build ./... && go test ./internal/mechanisms/`

**Commit:** `refactor(mechanisms): tool-loop directive fragments live in embedded plain files`

---

## 6. `internal/probe`: battery prompts become embedded files, fingerprint invariant intact — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the loader (`mustPrompt`) normalises CRLF→LF before stripping the trailing newline, matching items 3–5 and `internal/tui/logo.go` — beyond design call 6's literal wording, but without it a `core.autocrlf=true` checkout would bake `\r` into a prompt and move the probe fingerprint, which is exactly what the item's invariant forbids.
NOTES (2026-08-13): no separate "assets load non-empty and without a trailing newline" loader test was added here (items 3–5 have one). The item mandates the pin test instead, and the pin is strictly stronger: exact equality against the hard-coded literal fails on an empty asset, a stray newline or any other drift.
NOTES (2026-08-13): byte-identity was verified mechanically as well as by the pin — both asset files were compared against the const literals extracted from `git show HEAD:internal/probe/battery.go`: identical, each asset ending in exactly one newline.
NOTES (2026-08-13): the marker const block keeps a shortened form of the old comment (the markers are equally part of the fingerprint), so the "a BatteryVersion bump is required" sentence now stands in three places — the embed var doc, `prompts/README.md`, and that block — one per set of strings it governs.

**What:** Move `batterySystemPrompt` (`internal/probe/battery.go:307`) and `candidatePrompt`
(`:308`) to `internal/probe/prompts/*.txt` behind an `embed.FS`; consts become vars,
byte-identical per design call 6. The marker consts (`chainSecret`, `harmonyMarker`,
`thinkOpen`, `thinkClose`, `:309-312`) are fingerprint markers, not prompts — they stay in code.
The invariant comment at `battery.go:303-305` (every string here feeds the fingerprint;
rewording requires a `BatteryVersion` bump) must survive the move — but assets stay pure prompt
text (a comment line inside a `.txt` would enter the prompt), so the invariant lives on the
embed var's doc comment and in `prompts/README.md`
(one paragraph, not embedded, stating: editing any file here changes the probe fingerprint and
requires bumping `BatteryVersion`, `battery.go:18`, mirrored at
`internal/library/proberecord.go:36`). Update the package `doc.go` file map.

**Files:** `internal/probe/battery.go`, `internal/probe/prompts/` (new assets + `README.md`),
`internal/probe/doc.go`, `internal/probe/battery_test.go`

**Tests:** new pin test in `battery_test.go`: the two loaded prompt vars equal their exact
current literal text (hard-coded in the test), so an asset-file edit that forgets the
`BatteryVersion` bump fails loudly — this enforces the invariant the old block comment could
only state. Existing `internal/library/proberecord_test.go` version pins stay green unmodified.

**Acceptance:** `go build ./... && go test ./internal/probe/ ./internal/library/`

**Commit:** `refactor(probe): battery prompts live in embedded plain files, fingerprint-pinned`

---

## 7. `ISSUES.md`: remove the three resolved defects

Depends on items 1–6.

**What:** Delete the three now-resolved entries from `ISSUES.md` "Open defects": the
hard-coded-prompts entry (its four citations are closed by items 3–6), the clipboard entry
(item 2), and the `/server`+`/model` accept entry (item 1). Nothing else in the file moves; the
closed trail is the per-item `CHANGELOG.md` entries the run's verifiers already wrote (house
rule: `ISSUES.md` holds open work only).

**Files:** `ISSUES.md`

**Tests:** none (docs-only).

**Acceptance:** `grep -c "summaryInstruction\|copyFlash\|acceptAutocomplete" ISSUES.md` returns
`0`, and `git diff --stat` for the item touches only `ISSUES.md`.

**Commit:** `docs(issues): close the prompts-as-files, clipboard, and bare-accept defects`

---

**Suggested version bump:** the plan ships two user-facing fixes (items 1–2) and one
user-invisible refactor (items 3–6); per the house per-feature micro-bump policy a bump to
`v0.13.14` (or one micro-bump per shipped fix) is warranted after execution — the owner decides;
no plan item changes `VERSION`.
