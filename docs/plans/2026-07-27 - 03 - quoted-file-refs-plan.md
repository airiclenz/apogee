# Plan — Quoted `@`-refs: file references whose paths contain spaces

**Date:** 2026-07-27
**Status:** READY (not grilled — mechanical decisions recorded below with rationale; ground verified against the working tree 2026-07-27)
**Source:** ISSUES.md `[A]` (line 8): `@"docs/plans/2026-07-23 - 04 - version-build-number-plan.md"` fails with `loop: @"docs/plans/2026-07-23 could not be resolved and was ignored: statat "docs/plans/2026-07-23: no such file or directory`. The extractor (`internal/tui/command.go:172`) tokenizes on whitespace only, so a quoted path splits at its first space **and** the leading `"` rides along into the ref — the resolver then stats a file literally named `"docs/plans/2026-07-23`. There is no way today to reference any workspace file whose name contains a space, and this repo's own plan filenames all do.
**Track:** rides `[Unreleased]` (current `VERSION` v0.9.0; a fix + a small TUI affordance).
**Public API:** none — every change is inside `internal/tui`; `domain.UserInput.FileRefs` already carries plain workspace-relative paths and keeps doing exactly that. The agent-side resolver (`internal/agent/loop.go:657-732`) is untouched: `security.SafeOpen` has no problem with spaces — the failure is purely the parse.
**Standing requirement:** `/coding-standards` is forwarded to the implementer and verifier sub-agents.

Per-item green gate:

```
gofmt -l .                                              # empty
make check                                              # vet + lint + go test -race -count=1 ./...
GOOS=windows go build ./... && GOOS=darwin go build ./...
```

**Dependencies.** Items 1 → 2 → 3 run in order; the tree is coherent and green after every item and you may stop after any completed one. (Item 2 builds on item 1's shared token scanner; item 3 documents what 1–2 shipped.)

**Deviations leave a trail.** Any authorized deviation gets a dated `NOTES (YYYY-MM-DD):` paragraph directly under the item heading.

**Authoritative sources**, in precedence order:
1. This plan.
2. CONTEXT.md **File reference (`@file`)** (line 487): parsing is the TUI's job, resolution the agent's — this plan stays on the TUI side of that line.
3. The code as it stands.

---

## Decisions taken (mechanical — grounded, with rationale)

1. **Grammar: `@"path with spaces"` — a quote directly after the `@` opens a quoted ref.** An `@` at a word boundary (start of input or after whitespace, exactly today's rule) followed immediately by `"` or `'` starts a quoted token; the path is everything up to the next same quote character; the closing quote ends the token (any text after it, e.g. a trailing comma, stays ordinary text). A bare `@path` keeps today's whitespace tokenization byte-for-byte. `@""` (empty path) is skipped like today's bare `@`. No escape sequences: a filename containing `"` can be quoted with `'` and vice versa; a filename containing both is out of scope (recorded below).
2. **Both quote characters are ACCEPTED on parse; only `"` is ever PRODUCED.** Users arrive with shell reflexes and shells take either; refusing `'` would fail exactly like today's bug, with quotes in the ref. The TUI's own autocomplete always inserts the canonical double-quoted form, so accepted-vs-produced never drifts into two dialects on screen.
3. **An unterminated quote extends to the end of the line** (`\n` or end of input), right-trimmed of trailing spaces/tabs. A word-boundary `@"` is unambiguous intent — nothing else in prose looks like that — and if the rest of the line is not the path, the existing per-ref ErrorEvent (`internal/agent/loop.go:673-677`) names exactly what was tried, which teaches the closing quote. The alternative (fall back to whitespace tokenization) re-creates this very bug: a ref with a quote glued to it. A quoted token never crosses a newline, so a stray `"` cannot swallow the rest of a multi-line message.
4. **One scanner, two callers.** The token grammar lives in a single helper in `command.go` used by BOTH `extractFileRefs` (submit-time extraction) and `trailingFileToken` (autocomplete detection, `internal/tui/autocomplete.go:130`). Today the two duplicate the whitespace rule; adding quoting to each separately is how they drift. This is the long-term-shape choice: the grammar has exactly one home.
5. **The literal token stays in the message text, quotes included** — unchanged policy (`command.go:169-171`): the model sees what the human pointed at, and the resolved content block already names the path unquoted (`loop.go:680`). De-dup keys on the extracted path, so `@x` and `@"x"` collapse to one ref (first-seen order, as today).
6. **The autocomplete quotes on the way out only when it must.** Accepting a suggestion whose path contains a space or tab splices `@"path" ` (quoted, trailing space); any other path splices today's bare `@path `. Suggestion labels mirror what accept will insert (`@"a b.md"` for spaced paths) so the dropdown teaches the syntax before the user ever types a quote.

---

## The ground (verified 2026-07-27 against the working tree)

**The parse layer.** `internal/tui/command.go`: `parseInput` (`:57-68`) routes every non-command line through `extractFileRefs` (`:172-193`) — whitespace-only tokenization over `isInputSpace` (`:196-198`), word-boundary check at `:179`, dedup map at `:174`. Both submit paths flow through this ONE function: the idle submit (`internal/tui/model.go:775-787`) and the interjection (`internal/tui/interject.go:158`, replay at `:269,290-304`) — so a single fix covers both, including snapshot round-trips (refs re-resolve on resume, `loop.go:663-664`).

**The resolution layer (no change).** `internal/agent/loop.go`: `resolveFileRefs` (`:665-683`) emits the per-ref ErrorEvent seen in the issue; `readFileRef` (`:693` on) goes through `security.SafeOpen`, which is indifferent to spaces — the `statat "docs/plans/2026-07-23` in the report is the *quote-mangled* path, not a spaces limitation.

**The autocomplete.** `internal/tui/autocomplete.go`: the file region triggers on `trailingFileToken` (`:91-98`, fn at `:130-137` — `strings.LastIndexAny(value, " \t\n")+1`, so a quoted partial with a space is cut at the space and the overlay dies mid-type); `fileSuggestions` (`:235-242`) labels rows `"@" + path`; `autocompleteExactMatch` (`:284-305`) compares `value[tokenStart:] == "@"+selected`; `acceptAutocomplete` (`:313-335`) splices `"@" + value + " "`. The filter itself is already space-proof: `filecache.go` `suggest` matches case-insensitive substrings (`:96-102`), so a partial containing spaces narrows correctly once the token detector lets it through.

**Tests today.** `TestExtractFileRefs` table (`internal/tui/command_test.go:61-85`) — no quoted cases; `TestMessageWithFileRefsSubmitsRefs` (`internal/tui/minilang_test.go:363`); `TestWorkspaceFiles` + `TestComputeAutocompleteFiles` (`minilang_test.go:553,580`) — temp-workspace files without spaces.

**Docs today.** `README.md:105-106` ("`@` completes a workspace file path, and an `@path` in a message hands that file to the model"); CONTEXT.md **File reference (`@file`)** entry (`:487-493`); `ISSUES.md:8` holds the `[A]` report.

---

## 1. The quote-aware token grammar — one scanner, wired into `extractFileRefs` — ✅ DONE (2026-07-27)

**What.** `internal/tui/command.go`: add the single token scanner (decision 4) — e.g. `scanRefToken(s string, start int) (path string, end int)`, where `start` sits on the byte after `@`: if `s[start]` is `"` or `'`, the token runs to the next same quote on the same line (path = the inner text; end = past the closing quote), an unterminated quote runs to `\n`/end-of-string with the path right-trimmed of spaces and tabs (decision 3); otherwise today's rule verbatim (run of non-`isInputSpace` bytes). Rewrite `extractFileRefs` (`:172-193`) around it: the word-boundary gate (`:179`) and the dedup map stay exactly as they are; the literal token — quotes included — stays in the returned text (decision 5). Update the function's doc comment (`:166-171`) to state the full grammar: bare tokens, both quote characters, closing-quote-ends-token, unterminated-runs-to-end-of-line, no escapes.

**Tests** (`internal/tui/command_test.go`, extend the `TestExtractFileRefs` table `:62-76`):
- `@"docs/plans/2026-07-23 - 04 - version-build-number-plan.md"` → exactly that path, unquoted — the ISSUES `[A]` reproduction.
- quoted ref mid-message with text after the closing quote (`see @"a b.md", thanks`) → `a b.md`.
- single-quoted ref with spaces → the inner path (decision 2).
- quoted ref without spaces (`@"main.go"`) → `main.go`; dedup across forms (`@x and @"x"` → one ref, decision 5).
- unterminated quote at end of input (`@"a b` → `a b`) and before a newline (`@"a b\nnext line` → `a b`, next line scanned normally).
- `@""` → no ref; email and mid-word `@` cases stay untouched (the existing rows must not change).
- Extend `TestMessageWithFileRefsSubmitsRefs` (`minilang_test.go:363`) with a quoted spaced ref: the submitted `UserInput.Text` keeps the literal quoted token, `FileRefs` carries the clean path — pinning the submit seam end to end.

**Acceptance.** Green gate; a live `apogee` session in this repo resolves `@"docs/plans/archived/2026-07-23 - 04 - version-build-number-plan.md"` (the issue's own reproduction) — content block injected, no ErrorEvent.

**commit.** `fix(tui): quoted @-refs — file references with spaces parse and resolve`

---

## 2. The autocomplete speaks the quoted form — detection, labels, splice, exact-match

**What.** `internal/tui/autocomplete.go`, all four seams, on top of item 1's scanner:
- `trailingFileToken` (`:130-137`): recognise a trailing *quoted* token — a word-boundary `@` + quote whose token (per `scanRefToken`) reaches the very end of value. Open quote ⇒ the partial is everything after it (spaces included), and the overlay stays alive across them; closed quote flush at end-of-value ⇒ the partial is the inner path (so the exact-match/Enter-submits flow works for a fully-typed quoted token, mirroring the bare case). Value ending in whitespace after a closed quote ⇒ no token, as today. Bare tokens: byte-for-byte today's behaviour. Keep it a pure function; extend its doc comment with the quoted shapes.
- `fileSuggestions` (`:235-242`): label is `@"p"` when `p` contains a space or tab, else `@p` (decision 6 — the row shows exactly what accept will splice); `value` stays the raw path.
- `acceptAutocomplete` (`:313-335`): for `acFile`, splice `@"` + value + `" ` when the value contains a space or tab, else today's `@` + value + ` `. (The `/` command splice is untouched.)
- `autocompleteExactMatch` (`:284-305`): for `acFile`, the typed token matches when `value[tokenStart:]` equals the bare, the `"`-quoted, or the `'`-quoted form of the selected path — Enter then submits instead of re-completing, whichever dialect the user typed.

**Tests** (`internal/tui/minilang_test.go`, following the existing harness at `:553-599`; a `trailingFileToken` table can live beside it):
- `trailingFileToken` table: open-quote partial with spaces (`see @"my pl` → start at the `@`, partial `my pl`, ok); closed quote at end (`@"a b.md"` → partial `a b.md`, ok); whitespace after the closing quote → no token; bare-token rows unchanged.
- Temp workspace gains a file with spaces (e.g. `my plan.md`): typing `@"my pl` keeps the overlay open and lists it, label `@"my plan.md"`; accept splices `look at @"my plan.md" ` and closes the overlay (trailing space).
- Accepting a spaced path from a *bare* partial (`@my` → select `my plan.md`) splices the quoted form — quoting is decided by the path, not by how the user started typing.
- Exact-match: with `@"my plan.md"` fully typed, Enter submits (the `TestAutocompleteEnterExactSubmits` pattern, `:524`); the submitted `FileRefs` carries `my plan.md`.

**Acceptance.** Green gate; live in this repo: type `@2026-07-27`, pick a plan file from the dropdown — the input shows the quoted token, submit resolves it.

**commit.** `feat(tui): the @ autocomplete completes across spaces and splices quoted paths`

---

## 3. Docs and the issue ledger

**What.**
- `README.md:105-106`: extend the sentence — a path containing spaces is written quoted, `@"docs/my plan.md"`, and the autocomplete inserts the quotes for you.
- `CONTEXT.md` **File reference (`@file`)** entry (`:487-493`): one sentence on the grammar — the token is bare or quoted (`@"path with spaces"`, `'` accepted too); parsing stays the TUI's job (the entry's existing seam sentence already says so).
- `CHANGELOG.md` `[Unreleased]` **Fixed**: `@`-references to paths containing spaces — quoted `@"…"` syntax, quote-aware autocomplete (labels, completion across spaces, quoted splice); previously the ref split at the first space and could never resolve.
- `ISSUES.md:8`: flip `[A]` → `[X]` (the file's own legend: Executed), keeping the line's text as the record.

**Tests.** None beyond the green gate — the behaviour is pinned by items 1–2; this item is the paper trail.

**Acceptance.** `grep -n 'spaces' README.md CHANGELOG.md CONTEXT.md` hits the three edits; `grep -n '^\- \[X\] File references' ISSUES.md` hits; green gate.

**commit.** `docs: quoted @-ref syntax — README, CONTEXT, changelog; ISSUES [A] executed`

---

## Explicitly NOT in this plan

- **Escape sequences** (`@my\ file.md`, `\"` inside quotes) — shell-style escaping is a second dialect for the same need; quoting covers it. A filename containing BOTH quote characters cannot be referenced; it can still be read by the model's own `read_file` tool.
- **The resolution layer** — `resolveFileRefs`/`readFileRef`/`security.SafeOpen` are correct as they stand; refs keep arriving as plain workspace-relative paths.
- **The `statat` wording in the error text** — that string is Go's `os.Root` `PathError.Op` surfacing through the ErrorEvent, not an apogee bug; with the parse fixed the confusing form no longer appears for this case.
- **Quoting anywhere else in the mini-language** — `/confine` arguments and `/skill` partials stay whitespace-tokenized; no skill or command takes a spaced argument today.
- **Windows path separators in refs** — unrelated surface; refs are workspace-relative and forward-slashed as today.

## Critical files

- `internal/tui/command.go` — the shared token scanner + `extractFileRefs` (the fix).
- `internal/tui/command_test.go` — the grammar table.
- `internal/tui/autocomplete.go` — `trailingFileToken`, `fileSuggestions`, `acceptAutocomplete`, `autocompleteExactMatch`.
- `internal/tui/minilang_test.go` — the file-autocomplete harness + submit-seam tests.
- `README.md`, `CONTEXT.md`, `CHANGELOG.md`, `ISSUES.md` — the documentation surface and the issue ledger.

## Verification (whole plan)

Manual, in this repo (its plan filenames are the natural fixture):

1. `@"docs/plans/archived/2026-07-23 - 04 - version-build-number-plan.md"` typed verbatim (the issue's reproduction) → the file's content reaches the model; no `could not be resolved` note.
2. Type `@version-build` → the dropdown lists the archived plan with a quoted label; accept → the input holds the quoted token; Enter submits and the ref resolves.
3. `@'docs/plans/archived/2026-07-23 - 04 - version-build-number-plan.md'` (single quotes) → resolves identically.
4. A deliberate unterminated quote (`@"docs/plans/nope`) → one ErrorEvent naming `docs/plans/nope`, turn proceeds — the report-and-skip contract intact.
5. Bare refs, emails (`foo@bar.com`), and mid-word `@` behave byte-identically to today (the untouched table rows are the CI proof).

Automated: the per-item green gate; the extended `TestExtractFileRefs` table and the quoted-autocomplete flows in `minilang_test.go` are the regression fence.
