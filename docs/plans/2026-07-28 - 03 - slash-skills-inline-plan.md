# Plan — One slash namespace: direct skill invocation, inline tokens, caret-aware completion

**Date:** 2026-07-28
**Status:** READY (grilled 2026-07-28 — every *Design decision* below is an owner answer from
the grill session, or a consequence derived from one and marked as such; the mechanical design
is grounded against the working tree at `7c1941e` + uncommitted edits).
**Source:** `ISSUES.md:12` — "slash command like /skills do not work when typing a message to
be scheduled and they also don't work if I want to use a slah command after I already typed
something in the promt editor. I also would like to see skills and files in-line in the prompt
text. Skills should also directly be call-able with / (without the need to call the list with
/skills )."
**Track:** post-`v0.9.5`. `### Added`/`### Changed`/`### Fixed` entries under
`## [Unreleased]`; rides the next cut. `VERSION` untouched.
**Public API:** none. Everything lives in `internal/tui` (`domain.UserInput.SkillIDs` and the
agent-side resolution already exist and are reused unchanged); the `Engine` interface,
`apogee.Config` and all exported names are untouched.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`,
no PRs (owner directive). No AI attribution trailers.

Per-item green gate:

```
gofmt -l .                # empty
make check                # vet + lint + go test -race -count=1 ./...
```

**Dependencies.** Linear: 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9. Item 1 is a pure refactor
(one table). Item 2 adds `/skills` (idle-only until item 7). Item 3 flips skills from chips to
inline tokens — the pivot of the whole plan. Item 4 adds the typo guard. Item 5 merges the
menu and makes commands execute at accept with the draft preserved (this alone fixes the
"after I already typed something" symptom for the common end-of-buffer case). Item 6 makes
completion caret-aware (mid-string). Item 7 opens the running state (dropdown + per-command
policy — the "message to be scheduled" symptom). Item 8 paints the tokens. Item 9 closes out
docs and records the ADR. `/implement-plan` may stop after any completed item and the tree is
coherent.

**Deviations leave a trail.** Any authorized deviation from an item's text must land as a
dated `NOTES:` line under that item's heading in this file, per the sub-agent templates.

**Authoritative sources**, in precedence order, for every item:

1. This document (the *Design decisions* are owner rulings — do not re-litigate them).
2. ADR 0025 (interjections commit at the between-Steps boundary) and `CONTEXT.md:151-172`
   (Interjection vocabulary — "staged"/"held", never "scheduled message"); `CONTEXT.md:508-530`
   (File reference + Skill vocabulary).
3. `internal/tui/doc.go` + ADR 0011 — the value-copied `Model` holds no no-copy type by
   value. Every piece of new state in this plan is a plain value or a slice header;
   `TestModelNoBuilderByValue` must pass untouched.
4. The apogee-code oracle (`TODO.md:17-20`): the webview posts `{text, skillIds, fileRefs}` —
   parity of payload, not of UI.

---

## Design decisions (2026-07-28, grill session)

- **One inline mini-language.** A skill is invoked by typing its ID as a `/token` directly in
  the prompt text — `/code-audit please check the parser` — exactly parallel to `@file` refs:
  the token sits at a word boundary (start of input or after whitespace), is
  whitespace-delimited, stays **in** the text at submit (owner override of the strip default:
  the model sees the token *and* the injected skill block), and the extracted IDs travel as
  `UserInput.SkillIDs`. Only a token that exactly matches a catalog ID is a skill reference;
  any other `/word` mid-message is plain text (paths like `/usr/bin` must survive). The
  attached-skill **chips above the box are retired**, and with them `pendingSkills`, the
  backspace chip-pop, and the chip strip renderer. The transcript's sent-block chip row
  survives — it is fed display names resolved from the parsed IDs, as today from the attached
  ones.
- **Merged menu; the picker survives; `/skills` exists.** Typing a `/` token offers ONE
  dropdown listing commands (first, prefix-matched, with summaries) and skills
  (substring-matched on ID + display name, visually marked as skills). The two-step
  `/skill ` → picker flow is kept as an alternate entry point — it now **inserts an inline
  `/id ` token** at the strip point instead of popping a chip — and it is the only slash route
  to a skill whose ID collides with a command verb (commands shadow skills in the namespace;
  collision is resolved menu-side, the parser never sees it). A new `/skills` command prints
  the catalog (ID, display name, summary — reloaded first) as a transcript note, purely for
  browsing.
- **Commands execute at accept; the draft survives.** Accepting a command from the dropdown
  runs it immediately: the `/verb` token is removed from the editor, the rest of the draft
  stays untouched, caret restored. Enter with the menu open and the token typed out exactly
  behaves the same — EXCEPT when the token is the whole input, where Enter falls through to
  submit and the existing whole-input parse (`matchCommand`) runs it, unchanged. The
  whole-input form remains the only way to pass arguments (`/confine off --save`);
  dropdown-accept of an arg-taking verb (only `/confine`) splices `/confine ` and does not
  fire. `runCommand`'s unconditional `input.Reset()` moves to the call sites accordingly.
- **Caret-aware completion, everywhere.** The completion region is the token AT the caret —
  mid-string edits included — for all three regions: `/` (commands+skills), `@` (files, same
  upgrade, owner-confirmed), and `/skill <partial>`. This deliberately un-defers
  `TODO.md:50-52` ("Mid-string (non-trailing) token completion", kept deferred until now);
  accept splices over the token's `[start,end)` range and re-seats the caret after the splice.
- **Per-command while-running policy.** Each command declares `whileRunning`. Safe/read-only
  verbs run immediately mid-run: `/version`, `/skills`, `/confine` in its status form.
  Mutating verbs stay refused with the existing `commandsAtIdleNote`: `/clear`, `/new`,
  `/sessions`, `/compact`, `/continue`, `/confine off|on`. The dropdown opens while running
  and lists everything, idle-only verbs visibly tagged; accepting a tagged one prints the
  note and leaves the draft. Skill and file tokens are message content and simply ride the
  interjection — which also fixes the pre-existing silent drop of `SkillIDs` on the
  interjection path (`interject.go:145,158` discards them today).
- **Highlight only what resolves.** In the prompt box, a `/token` gets the skill accent only
  when it exactly matches a catalog ID; an `@path` gets the file accent only when the path is
  in the workspace file cache. Everything else stays plain — the styling doubles as live
  validation (a typo'd skill visibly fails to light up). The staged-interjection band keeps
  its uniform faint style (chrome, not content — out of scope).
- **Sole-token typo guard.** If the ENTIRE trimmed input is one `/word` that matches no
  command verb, no menu verb, and no skill ID, ⏎ refuses with a note naming the token
  ("unknown command or skill: /code-adit") and leaves the text in the editor — today's silent
  send of `/skills` to the model is exactly the confusion that spawned this issue. Any input
  with more than that one token is an ordinary message. A bare `/skill` gets its own usage
  note (it is a menu verb, not a typo).
- **Edge defaults (owner-confirmed):** an input that is ONLY a skill token is a valid submit
  ("just run the skill" — the text is the token itself, since tokens stay in the text);
  command verbs shadow colliding skill IDs; `/confine` args are whole-input only.
- **Derived consequences (not separately grilled):** `/continue`'s chip-carry
  (`model.go:1007-1014` reads `pendingSkills` into the canned turn) is removed with the chips —
  skills now ride real messages as tokens; `/compact`'s chip drop (`model.go:1044`) likewise;
  `skillSuggestions`' already-attached dedup keys off the skill tokens already present in the
  buffer instead of `pendingSkills`; `joinedInterjections` unions `SkillIDs` exactly as it
  unions file refs.

---

## The ground (verified 2026-07-28 against the working tree)

**The parse layer** (`internal/tui/command.go`) is pure and Model-free. `knownCommands`
(`command.go:53`) — clear, new, sessions, compact, continue, confine, version — feeds
`matchCommand` (`command.go:76-91`): the whole trimmed input must begin with `/verb`, verb
delimited by space/tab (never newline — a multi-line message whose first line is `/clear`
stays a message, a documented rule that survives this plan). `parseInput` (`command.go:57-68`)
routes to command or message + `extractFileRefs` (`command.go:179-197`, word-boundary `@`,
bare/quoted grammar in `scanRefToken` `:211-231`, token left in the text, refs deduped
first-seen). `/confine`'s grammar: `parseConfine` (`command.go:136-164`), usage line
`command.go:129`.

**The dropdown** (`internal/tui/autocomplete.go`). `commandMenu` (`autocomplete.go:190-199`)
is a second registry — a superset holding summaries plus the menu-only `skill` verb; drift
between it and `knownCommands` is structural. `computeAutocomplete` (`autocomplete.go:66-101`)
gates: skill-arg region (`skillArgToken` `:223-235`) idle-only; command region
(`autocomplete.go:83`) idle-only AND whole-value `/partial` with no whitespace anywhere; file
region (`trailingFileToken` `:142-165`) live wherever the box is editable, trailing-token
only. `recomputeAutocomplete` (`:112-124`) edge-triggers the catalog reload on `/skill` region
open, idle-gated at `:117`. Key claim: `handleKey` consults the overlay at idle or running
(`model.go:621-625`); `autocompleteKey` (`:291-320`) — tab/enter accept,
`autocompleteExactMatch` (`:326-349`) lets Enter fall through to submit on a fully-typed
token (skills always complete, never fall through — chip semantics). `acceptAutocomplete`
(`:361-383`) splices from `tokenStart` **to the end of the value** and never submits;
`attachSkill` (`:389-403`) pops a chip onto `pendingSkills` and strips the `/skill <partial>`
run. `maxAutocompleteItems = 8` (`:26`).

**The editor cluster** (`internal/tui/prompteditor.go`). `promptEditor` embeds anonymously
into `Model`; `pendingSkills` (`prompteditor.go:57`) carries chip attachments to submit;
`submitParse` (`:147-149`) = `parseInput(value)` + `pendingSkills`; `reset` (`:161-165`)
drops chips. Caret machinery that item 6 builds on: `reseatCaret` (`:184-189`), `caretTo`
(`:195-207`), `reseatInput` (`:219-229`), plus `caretOffset`/`visualSubline`/
`cellToRuneOffset` (mouse.go) — the row+cell→rune direction exists; the inverse
(value offset → visual row+cell) does not yet.

**Dispatch** (`internal/tui/model.go`). Enter by state: `model.go:646-668` — idle `submit()`,
running `stageInterjection()`. `submit` (`model.go:802-845`): command → `runCommand`;
empty-guard at `:811` already treats "skills attached, no text" as sendable; held rows join
via `joinedInterjections`; launch carries `SkillIDs: attached` (`:844`). `runCommand`
(`model.go:970+`): resets the input unconditionally at `:971`; doc contract says "reached only
from submit (stateIdle)" — item 7 revises that contract; `/continue` chip-carry at
`:1007-1014`; `/compact` chip-drop at `:1044`; `/confine` → `runConfine`
(`confine.go:30-…`: status form reads `m.eng.ConfineToWorkspace()` + `m.opts`, mutating form
calls `SetConfineToWorkspace`).

**Interjections** (`internal/tui/interject.go`). `stageInterjection` (`:144-167`): a
recognised command earns `commandsAtIdleNote` (`:132`) and stays in the box (pinned by
`TestCommandWhileRunningRefusedWithNote`, `interject_test.go`); `parsed, _ :=` at `:145`
discards staged skills and the built `UserInput` (`:158`) carries none — the silent drop.
`joinedInterjections` (`:286-306`) unions file refs only. Delivery resolves skills fine if
given them: `agent/interject.go:62-68` prepends skill blocks exactly as `agent/loop.go:71-73`
does at open.

**Rendering hooks.** `inputView` (`model.go:2021-2023`) =
`inputBorder.Render(m.highlightInput(m.input.View()))`; `highlightInput` (`mouse.go:338-366`)
shades visual-cell ranges over the wrapped block using `ScrollYOffset`, via `shadeCells`
(`mouse.go:373-379`, ANSI-safe `ansi.Cut`; its doc notes the span's own styling is stripped —
so the composition order must be tokens first, selection last, selection wins). Chip strip:
`renderSkillChips` (`autocomplete.go:465-478`), slot accounting and stack order in `View`
(`model.go:1886-1909`). Theme: `skillChip` style (white on violet, `theme.go:99,146`).

**Skills plumbing.** `tui.SkillCatalog` (`tui.go:20-23`), `Options.Skills` /
`Options.ReloadSkills`; one `skills.Provider` feeds both the picker and the agent resolver
(`cmd/apogee/wire.go:89-92,183,396,401`). Skill IDs are directory names — no whitespace, so
the bare-token grammar needs no quoting.

**Docs that must move.** `README.md:120-137` (command table), `layout.md:43-53` (prompt box —
no dropdown/token section exists yet), `CONTEXT.md:508-530` (Skill/File-reference vocabulary),
`TODO.md:50-52` (the deferred mid-string completion), `internal/tui/doc.go` narrative
(chips), `ISSUES.md:12`. Next ADR number: **0027**.

---

## 1. One command table

**What.** Merge the two registries — `knownCommands` (`command.go:53`) and `commandMenu`
(`autocomplete.go:190-199`) — into a single `commandSpec` table in `command.go`:
`{name, summary string; takesArgs, whileRunning, menuOnly bool}`. Rows: the seven parser
verbs (confine: `takesArgs`; version + confine: `whileRunning` per the policy decision —
honored by nothing yet) plus `skill` (`menuOnly`: offered by the dropdown, never parsed —
today's contract, `autocomplete.go:185-189`). `matchCommand` matches non-`menuOnly` names;
`commandSuggestions` derives its rows (label `"/name  summary"`) from the same table. Pure
refactor: parser and menu behavior byte-identical.

**Tests.** Existing command/autocomplete tests pass unchanged. New: a table-driven test
asserting `matchCommand` recognises exactly the non-`menuOnly` names and that
`commandSuggestions("")` lists every row in table order — the one-registry guarantee.

**Acceptance.** Green gate passes. `grep -n "knownCommands\|commandMenu" internal/tui`
shows only the one table (or thin derivations of it).

**Commit.** `refactor(tui): one command table drives both the parser and the menu`

## 2. `/skills` — list the catalog

**What.** Add `skills` to the table (`whileRunning: true` — inert until item 7; summary:
"list the available skills"). `runCommand` gains the case: call `m.opts.ReloadSkills` (nil-safe)
then render one transcript note from `m.opts.Skills.List()` — one line per skill,
`/id  DisplayName — Summary`, plus a header naming the count; an empty/nil catalog notes that
no skills were found and where apogee looks (the three source dirs, phrased as in
`skills/load.go:58-70`). Update the README command table in this item (it documents the verb
the moment it exists).

**Tests.** `/skills` at idle produces the note with the catalog rows (fake `SkillCatalog`);
reload is invoked before listing (edge-observable via a counting stub); nil catalog → the
empty-note path, no panic.

**Acceptance.** Green gate passes. Typing `/skills` ⏎ at idle prints the catalog instead of
sending "/skills" to the model.

**Commit.** `feat(tui): /skills lists the skill catalog`

## 3. Skills become inline `/tokens` — the chip flow retires

**What.** The pivot. Parse layer first (`command.go`), pure and Model-free:

- `extractSkillRefs(s string, known func(string) bool) []string` — scans for word-boundary
  `/token` runs (start of input or after whitespace, whitespace-delimited, `isInputSpace`
  boundaries — the `extractFileRefs` posture), collects tokens whose bare name `known`
  confirms as a catalog ID, deduped first-seen. Tokens stay in the text (owner override —
  uniform with `@refs`). `parsedInput` gains `skillIDs []string`; `parseInput` gains the
  `known` predicate (nil ⇒ no skill refs) and fills it for `kindMessage`. The whole-input
  command rule runs FIRST, unchanged — commands shadow skills (`/clear` alone is the command
  even if a skill is named "clear").
- `submitParse` (`prompteditor.go:147-149`) passes a predicate backed by `m.opts.Skills`
  (nil-safe) and returns `parsed.skillIDs` as the attached set; the second return value
  (`pendingSkills`) and the field itself are DELETED, along with: `attachSkill`'s chip body
  (it becomes `insertSkillToken` — splice `/id ` over the `[tokenStart,end)` region, caret to
  end, recompute), `renderSkillChips` + its View slot (`model.go:1886-1909` accounting),
  `reset`'s chip drop, the backspace chip-pop branch (`model.go:728-732`), `skillSuggestions`'
  `pendingSkills` dedup (now: exclude IDs already present as tokens in the buffer, via
  `extractSkillRefs` on the current value), `/continue`'s chip-carry (`model.go:1007-1014` —
  the canned turn sends no skills now; its `addUser` chip row goes empty) and `/compact`'s
  chip drop (`model.go:1044`).
- `submit` (`model.go:802-845`) uses `parsed.skillIDs` as `attached` — the transcript's
  sent-block chips (`skillDisplayNames`) and the launch's `SkillIDs` flow unchanged.
- `stageInterjection` (`interject.go:144-167`) keeps the full parse: the staged
  `domain.UserInput` carries `SkillIDs` (fixing the silent drop at `:145,158`), and
  `joinedInterjections` (`:286-306`) unions them first-seen alongside the file refs;
  `flushInterjections`' launch passes them through. The agent side already resolves them
  (`agent/interject.go:62-68`).
- `autocompleteExactMatch` (`autocomplete.go:326-349`): the acSkill always-complete rule
  (`:333-335`) now applies only inside the `/skill <partial>` picker region; a directly-typed
  skill token's exact match is handled in item 5 (until then skills still enter only via the
  picker, which now splices a token).

ADR 0011 discipline: everything added is a plain slice/func value; no no-copy types.

**Tests.** Parse: table-driven `extractSkillRefs` (boundaries, dedup, unknown tokens ignored,
`/usr/bin` untouched, newline boundaries, email-style non-boundary); `parseInput` precedence
(whole-input command beats skill token). Model: picker accept splices `/id ` into the text
(no chip state anywhere); submit on `/grill-me check this` sends
`text="/grill-me check this"`, `SkillIDs=["grill-me"]`, transcript chips show the display
name; submit on a bare `/grill-me` token sends (edge default #2); staging while running
carries `SkillIDs` on the row's `UserInput`; a flush of two rows with the same skill unions
to one ID. Revise the existing chip tests (attach/pop/render) into their token equivalents;
`TestModelNoBuilderByValue` untouched.

**Acceptance.** Green gate passes. A skill is invocable by typing its `/id` anywhere in a
message; the text keeps the token; no chip row renders anywhere; an interjection typed with a
skill token reaches the engine with that skill resolved.

**Commit.** `feat(tui): skills are inline /tokens in the prompt mini-language`

## 4. Sole-token typo guard

**What.** `parseInput` classifies a new kind: `kindUnknownSlash` — the ENTIRE trimmed input
is one whitespace-free `/word` token matching no non-`menuOnly` command, no `menuOnly` verb,
and no known skill ID. `submit` and `stageInterjection` both handle it: transcript note
`unknown command or skill: /word — nothing sent`, input left exactly where it was (the
`blockedUpstream` refusal posture, `model.go:814-823`). A bare `/skill` (menu verb, not a
command) gets its own note teaching the picker: `type /skill <name> to pick a skill — or
/skills to list them`. Multi-token input never trips the guard.

**Tests.** `/code-adit` alone at idle → note, text stays, nothing launched; same while
running → note, nothing staged; `/code-adit the parser` → ordinary message; `/skill` alone →
the usage note; a valid `/grill-me` alone still submits (item 3's edge default).

**Acceptance.** Green gate passes; the exact `ISSUES.md:12` reproduction (`/skills` before
item 2, any typo after) can no longer silently reach the model.

**Commit.** `feat(tui): unknown sole /verb is refused with a note`

## 5. Merged menu; commands execute at accept; the draft survives

**What.** The `/` region becomes token-scoped and merged; commands become actions at accept.
Still end-of-buffer-triggered (caret-awareness is item 6); still idle-gated (item 7 lifts it).

- **Trigger** (`computeAutocomplete`, `autocomplete.go:83`): replace the whole-value rule
  with a trailing `/`-token rule — the trailing whitespace-delimited word starts with `/` at
  a word boundary (the `trailingFileToken` posture). The `/skill <partial>` region keeps
  winning first (checked before, as today).
- **Rows:** commands from the table (prefix match, tagged with their summaries), then skills
  (`skillSuggestions` matching, rows marked as skills — label `⚡ /id  DisplayName  Summary`
  or the theme's equivalent; pick one marker and reuse it in item 8's accent rationale).
  Commands shadow skills: a skill whose ID equals a command verb is omitted from the merged
  rows (reachable via the `/skill` picker — decision above). Extend `recomputeAutocomplete`'s
  edge-triggered `ReloadSkills` to fire when the merged `/` region opens, not only the picker
  region (`autocomplete.go:112-124` — same edge flag, region redefined).
- **Accept** (`acceptAutocomplete` + `autocompleteKey`, signatures gain a `tea.Cmd`):
  - skill row → splice `/id ` over `[tokenStart, tokenEnd)` (this item: tokenEnd = end of
    value), caret after the splice — insertSkillToken from item 3;
  - command row, `takesArgs` or `menuOnly` (`confine`, `skill`) → splice `/verb ` (today's
    behavior; `/skill ` chains into the picker as now);
  - command row otherwise → **execute**: remove the `/partial` token from the value
    (splice to ""), collapse any doubled separator, keep the rest of the draft and re-seat
    the caret, close the overlay, then `runCommand` — whose unconditional `input.Reset()` +
    overlay clear (`model.go:971-972`) move OUT to its two call sites: the submit path keeps
    them (whole-input invocation — reset == strip), the accept path has already prepared the
    editor. `runCommand` itself must not touch the editor again.
- **Enter/exact-match** (`autocompleteExactMatch`): falls through to submit ONLY when the
  token under completion is the whole trimmed input (whole-input parse takes over — args
  included); otherwise Enter accepts (= executes a command, completes a skill/file). A
  directly-typed exact skill token mid-draft reports exact-match true so ⏎ submits the
  message (the token is already complete text).

**Tests.** Draft + trailing `/comp` opens the merged menu; accept on `compact` runs the
command (worker launched) and the editor still holds the draft minus the token, caret sane;
accept on a skill splices the token into the middle of the draft flow; `/confine` accept
splices and does not fire; whole-input `/confine off` ⏎ still parses args (existing tests);
whole-input exact `/clear` ⏎ still runs via submit; mid-draft exact `/clear` + ⏎ executes via
accept and preserves the draft — the message is NOT sent; menu shows commands before skills;
colliding skill ID omitted.

**Acceptance.** Green gate passes. The second `ISSUES.md:12` symptom is fixed for the
forward-typing case: with a draft in the box, typing `/…` at the end opens the menu and
invoking a command never destroys the draft.

**Commit.** `feat(tui): merged command+skill menu; commands run at accept and keep the draft`

## 6. Caret-aware completion — the token at the caret

**What.** Lift the end-of-buffer restriction for all three regions (un-defers
`TODO.md:50-52`; owner chose the caret-aware branch explicitly, files included).

- A helper on the editor derives the caret's byte offset in the value from the widget's own
  state (`caretOffset(value, m.input.Line(), m.input.Column())` — already exists, mouse.go)
  and the token containing/abutting it: scan left to the nearest `isInputSpace` boundary,
  right to the next; quoted `@` tokens use `scanRefToken`'s grammar from the `@`. The region
  match then runs against that `[start,end)` token instead of the trailing word —
  `computeAutocomplete` takes the offset as an argument so it stays a pure function
  (tests construct value+offset directly).
- The caret must sit inside or immediately after the token (a caret elsewhere on the line
  must not resurrect a menu for a token far away).
- Accept splices over `[start,end)` and re-seats the caret to just after the spliced text via
  the existing walk (`reseatCaret`/`SetCursorColumn` — the `caretTo` idiom in reverse:
  compute target line/column from the new value and offset; add the one missing helper
  `offsetToLineCol(value, off)` next to `caretOffset`, unit-tested against it as inverses).
- `skillArgToken`'s trailing-word rule gets the same treatment (partial = the caret token,
  the word before it must be `/skill`).
- Executing a command mid-string (item 5's accept) splices its token range out and preserves
  everything on both sides.

**Tests.** Offset↔line/col inverses property-tested over multiline + CJK/emoji values;
completion opens for `/gri` with the caret mid-buffer and trailing prose after; accept
splices in place, caret lands after the splice, surrounding text untouched; `@` completion
mid-buffer (bare + quoted); no menu when the caret is outside the token; end-of-buffer
behavior of items 3–5 unchanged (regression suite reruns).

**Acceptance.** Green gate passes. Editing the middle of a long draft offers the same
completion the end does, for `/`, `@`, and the picker region alike.

**Commit.** `feat(tui): caret-aware completion for the /, @ and /skill regions`

## 7. The running state opens — dropdown live, per-command policy

**What.** The first `ISSUES.md:12` symptom. Honor `whileRunning` end to end:

- **Dropdown:** drop the `idle` gates in `computeAutocomplete` (`:74,83`) and
  `recomputeAutocomplete` (`:117` — the picker region reloads while running now too); the
  overlay already claims keys at running (`model.go:621-625`). While running, idle-only
  command rows render with a trailing tag (e.g. `— idle only`, faint); skills and files are
  untagged (they ride the interjection).
- **Accept while running:** a `whileRunning` command executes exactly as at idle (item 5's
  accept path; `runCommand`'s "reached only from submit (stateIdle)" doc contract is
  rewritten — the three safe verbs are boundary-free: `/version` and `/skills` are
  synchronous notes, `/confine` status reads `m.eng.ConfineToWorkspace()` which the
  implementer must confirm is goroutine-safe like `SetMode`, demoting the verb to idle-only
  with a dated NOTES line here if it is not). An idle-only command's accept prints
  `commandsAtIdleNote` and leaves the draft.
- **⏎ while running** (`stageInterjection`, `interject.go:144-167`): a parsed
  `kindCommand` routes by the same policy — `whileRunning` verbs run now (the box keeps the
  rest of its content only via the accept path; whole-input invocation clears as at idle),
  others keep today's refusal note. `kindUnknownSlash` already notes (item 4). Messages
  stage as today, `SkillIDs` riding (item 3).
- Placeholder and hint text stay as they are (the legend already says what ⏎ does).

**Tests.** Revise `TestCommandWhileRunningRefusedWithNote`: `/clear` while running still
notes and stays; `/version` while running prints the version note without disturbing the
worker or the queue; `/skills` while running lists; the dropdown opens on `/` while running
with the idle-only tag present; accepting `clear` from it while running notes and preserves
the draft; accepting a skill while running splices the token and the staged row carries the
ID.

**Acceptance.** Green gate passes. While the model works: `/skills` answers immediately,
`/clear` refuses with the note, a skill token queues with the interjection — nothing is
silently swallowed.

**Commit.** `feat(tui): dropdown and safe commands live while the model runs`

## 8. Inline accents — tokens light up when they resolve

**What.** The "see skills and files in-line" half. A second post-render pass over the
textarea block, composed inside `inputView` (`model.go:2021-2023`) BEFORE the selection
overlay (selection strips and rewins — `shadeCells` doc, `mouse.go:368-379`):

- Tokenize the current value once per render: `extractSkillRefs`-shaped scan yielding
  resolving skill-token byte ranges, plus `@`-ref ranges whose path is in the workspace file
  cache (`m.files` — reuse the cache's walked listing; a cache miss/expiry renders plain and
  self-heals on the next walk, never triggering a synchronous walk from the render path).
- Map each `[start,end)` byte range to visual (row, col-cell) spans with `offsetToLineCol`
  (item 6) + the widget's wrap geometry, offset by `ScrollYOffset` — the `highlightInput`
  posture (`mouse.go:338-366`); shade with two new theme styles `skillToken` and `fileToken`
  (accent foreground on the input's black — derive `skillToken` from the retired chip
  violet, `theme.go:99,146`; keep both styles background-black so the box stays one band).
  Multi-row tokens (a wrap mid-token) shade each spanned row's cells.
- Non-resolving tokens, prose, placeholders: untouched. The staged band stays uniform.

**Tests.** Render-level: a resolving skill token's cells carry the accent (assert on the
styled frame with a fake catalog), a typo'd one does not; an existing `@path` lights, a
missing one does not; token + active drag-selection compose with selection winning over the
overlap; a token wrapped across rows shades on both; scrolled textarea (content taller than
`maxInputRows`) shades only visible rows. Guard: the render path performs no filesystem walk
(counting-stub cache).

**Acceptance.** Green gate passes. Typing `/grill-me check @internal/tui/model.go and
/code-adit` shows the first two spans accented and the typo plain, live as you type.

**Commit.** `feat(tui): inline accents for resolving /skill and @file tokens`

## 9. Close-out — ADR 0027 and the documentation sweep

**What.**

- **ADR 0027** (`docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md`): records the
  settled questions so none re-opens — one `/` namespace (commands shadow skills), skills as
  inline text tokens carried in `SkillIDs` (chips retired), commands execute at accept with
  draft preservation (whole-input form owns arguments), caret-aware completion supersedes the
  cursor-position-free design (amending the rationale the old design documented), per-command
  `whileRunning` policy, resolve-gated inline accents, the sole-token typo guard. Note the
  ADR-0025 interaction: command policy at the staging boundary, skills riding interjections.
- **`CONTEXT.md`**: the Skill entry (`:508-530`) gains the token grammar and the picker's new
  splice behavior; the Interjection section (`:151-172`) notes the per-command policy
  replacing the blanket "commands run at idle".
- **`layout.md`**: the prompt-box section (`:43-53`) gains a short spec: the merged dropdown,
  idle-only tags while running, inline token accents; the chip strip's paragraph (if any
  remains) is deleted.
- **`README.md:120-137`**: command table rows for `/skills`, `/skill`, direct `/skill-id`
  invocation, and the while-running column.
- **`internal/tui/doc.go`**: the input-cluster narrative (`:113-160` region) rewritten —
  chips out, tokens in, accept-executes, caret-aware regions.
- **`TODO.md:50-52`**: the mid-string completion entry moves to the closed trail, pointing
  here. **`ISSUES.md:12`** → `[X]`. **`CHANGELOG.md`**: Added (direct skill invocation,
  `/skills`, merged menu, inline accents, while-running commands), Changed (chips → inline
  tokens, caret-aware completion, unknown-verb guard), Fixed (interjections dropping staged
  skills; slash input silently sent to the model). `VERSION` untouched.

**Tests.** None new; `make check` green.

**Acceptance.** Green gate passes; `grep -rn "pendingSkills\|renderSkillChips\|commandMenu"
internal/tui` returns nothing live; an owner-run live smoke (type a draft, run `/compact`
from the menu, invoke a skill mid-run, watch the accents) confirms the behavior in a real
terminal — owner verification, not a gate.

**Commit.** `docs: ADR 0027 + close-out for the one-slash-namespace mini-language`
