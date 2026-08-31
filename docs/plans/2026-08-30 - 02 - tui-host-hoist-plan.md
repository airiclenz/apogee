# Hoist the TUI's Driver-neutral host rules into sibling libraries

**Goal.** Five host rules that only `internal/tui` (or `package main`) can reach today, and that every other Driver re-spells by hand, move into the `internal/<pkg>` that already owns half the concept — the `internal/sanitize` / `internal/format` pattern. Each move deletes the copies it replaces; two of them give headless and daemon runs a capability they lack (`@file` references, the retired-Mechanism notice). Nothing model-visible changes.

**Date:** 2026-08-30 · **Status:** unexecuted · **sized for:** ~200k-context host

**Authoritative sources:** `docs/reviews/architecture-review-20260830.html` (candidates A1–A3, B1–B2; A4–A8, B3–B5 deferred) · `docs/adr/0010-*` (dependencies flow toward `internal/domain`; `internal/*` never imports root) · `docs/adr/0011-*` (no agent logic in the TUI) · `docs/adr/0031-*` (engine sufficient for any Driver) · `docs/adr/0033-*` (Firing runner; "the surface that offers Auto refuses it") · `docs/adr/0043-*` (`internal/tui` stays flat — hoists go to siblings; every file added/renamed in a mapped package updates its `doc.go` in the same commit) · `CONTEXT.md:1062-1078` (File reference) · base commit `a9ec2131`.

**Ratified design calls** (owner, 2026-08-30):
- **Scope:** A1 session-title rule, A2 delegate-naming rule, A3 `@file`/`/skill` grammar (+ Firings resolve `@file`), B1 unattended-Auto verdict, B2 mechanism-id validation + retired notice; Group C parity gaps → `ISSUES.md` only.
- **A1 shape:** `title.Derive(text, max, now)` with the cap as a parameter; tui and run pass `title.MaxRunes` (50), schedule keeps its 40 — behaviour-preserving; `internal/schedule` gains the `title` import.
- **A2 home:** `internal/title` — `FirstLine(s)` and `DelegateLabel(name, task)` over already-stripped strings; escape-stripping and clipping stay at each render seam.
- **A3 scope:** grammar → new `internal/refs`; `internal/run` fills `UserInput.FileRefs` from a Firing's prompt; `filecache.go` (the lister) stays in tui — one adapter, hypothetical seam.
- **B1 home:** `internal/probe`, beside `DegradedNotice`; `daemon.Host` drops the injected `AutoEligible` bool and carries the confinement facts instead.
- **B2 home:** `internal/mechanisms` — one call `ResolveEnabled(enabled, known) (ids, notices, err)`; headless prints notices to stderr, the daemon logs them at startup, the sub-agents posture discards them explicitly.
- **Writer's calls (mechanical, 2026-08-30):** `title.Clip` trims the first line on both sides (schedule already did; tui kept trailing spaces — a title never shows them); the code-fence fallback stays a `Derive` concern, so schedule names (`Clip`) are byte-identical to today; `usageAgentName` (`internal/tui/usage.go:261`) is untouched — it reads the header's result, it is not a copy; `SkillIDs` for Firings stay out.

**Regression check (2026-08-30, a9ec2131):**
- 1: guard folded (byte-index `Clip` + package doc rewrite, writer's decision; `TestSessionTitle` at `internal/tui/model_test.go:3633` is the only tui pin) — supersedes `internal/title/title.go:5`.
- 2: guard folded (`run.go:75` keeps its ADR 0010 citation; indirect tui pins named + control-only-name case; the task-fallback trim admitted).
- 4: guard folded (`parseInput` keeps the spans; every call and comment of a moved name, `autocomplete.go:548` included).
- 5: guard folded (a Firing skips a missing ref without notice — no event sink; `domain/config.go:676` rewritten).
- 6: guard folded (`cmd/apogee/doc.go` step dropped, writer's decision; acceptance greps `so auto falls back to`).
- 7: guard folded (zero-value `Host{}` keeps refusing `mode: auto` via `Unconfined bool`; `daemonfire.go:65` comment) — keeps `internal/daemon/file.go:99-102`.
- 8: guard folded (`TestMechanismIDsConstructsUnderBypass` stays in cmd — root import cycle; own `captureStderr`).
- 9: guard folded (`validatedsets.go:202`; the Bypass test kept and retargeted; every comment re-pointed).
- 10: guard folded (TUI boot-path test + `ISSUES.md:72-76` removal, writer's decision; `daemonfire_test.go:54` caller; `daemon.md:76-77` anchor).
- 11: guard folded (`daemonfire.go:207` and `:237` are the sites).

**Standing requirements:** `skills: coding-standards` · deviations land as dated NOTES lines under the item · no version identifier changes (closing note only) · every file added/renamed/deleted in `internal/tui`, `cmd/apogee`, `internal/mechanisms` or `internal/probe` updates that package's `doc.go` map in the same commit (`docmap_test`).

**Out of scope:** sub-packages under `internal/tui` (ADR 0043) · A4 blast-radius wording → probe · A5 skill-source disclosure · A6 transcript codec → session · A7 heartbeat monitor · A8 effort ladder · B3 `rebindSpecFor` · B4 state roots / scratch GC / plaintext-key notice · B5 settings apply corpus (one adapter) · `filecache.go` · `resolveMode`'s refusal wording · Group C feature work (item 11 records it).

---

## 1. `title.Clip` and `title.Derive` replace three session-title copies — ✅ DONE (2026-08-31)

NOTES (2026-08-31): consequential edit — internal/run/run.go: `firstTaskLine`'s doc closed with "— the same reason promptTitle is duplicated", naming the function this item deletes; the clause is dropped (the rest of that sentence is item 2's). Made necessary by deleting `promptTitle`.
NOTES (2026-08-31): the tui's `sessionTitle` now trims trailing spaces off the first line (`Clip` trims both sides, as `internal/schedule` always did) — the item's stated writer's call, admitted rather than preserved; every other input yields a byte-identical title.

**What.** `internal/title/title.go`: export `MaxRunes = 50` (today `titleMaxRunes`; `titleWordBoundaryFloor` derives from it unchanged) and add two pure functions. `Clip(text string, max int) string`: `TrimSpace` the text, take its first line, `TrimSpace` that, return it when ≤ `max` runes, else truncate to `max` runes at the last space past `max*6/10` (hard cut otherwise) and append `…`. `Derive(text string, max int, now time.Time) string`: `"Session " + now.Format("2006-01-02")` when the trimmed text is empty or opens with "```", else `Clip(text, max)`. `Derive` formats `now` in whatever zone it carries (never relocates — the caller's stated choice, `internal/run/run.go:303-311`). Rewrite the comment at `title.go:85` that names `internal/tui/model.go` as the copy it tracks: the rule now lives here.
Callers: `internal/tui/sessionsave.go:410-435` — delete `sessionTitleMax` and the body; `sessionTitle(text)` stays as a one-line delegate `title.Derive(text, title.MaxRunes, time.Now())` (the `sanitize.go` precedent: the delegate is the package's vocabulary); `sessionrule.go:72` unchanged. `internal/run/run.go:296-345` — delete `titleMax` and `promptTitle`; `Spec.title` calls `title.Derive(s.Prompt, title.MaxRunes, now.Local())`; drop the ADR 0010 apology paragraph. `internal/schedule/schedule.go:536-561` — `deriveName` becomes `title.Clip(prompt, nameMax)` (`nameMax` 40 stays, with its comment); delete the "duplicates neither more nor less" paragraph. `schedule` gains `internal/title` (→ `provider`, `sanitize`): confirm no cycle with `go build ./...`.
**Regression guard.** `internal/title`'s package doc (`title.go:1-26`, "the naming completion and nothing else") is rewritten in this item to say the package owns naming — the LLM completion, its sanitizer, and the pure title and label rules Drivers share — and item 2 does not touch it again; `Clip` reproduces the existing `strings.LastIndex(truncated, " ") > max*6/10` byte-index comparison verbatim (the quirk all three copies share), so the move is behaviour-identical on every input, ASCII or not — the test table pins one non-ASCII case against today's output rather than an idealised rune boundary. The `title.go:5` sentence ("two pure functions … and nothing else") is the documented decision this supersedes.

**Files:** `internal/title/title.go`, `internal/title/title_test.go`, `internal/tui/sessionsave.go`, `internal/run/run.go`, `internal/schedule/schedule.go`

**Tests.** `internal/title/title_test.go`: a table for `Clip` (fits; over-cap breaks at the last space past 60%; hard cut when the last space is at or before 60%; trailing spaces on the first line dropped; one non-ASCII first line pinned to today's output — e.g. `"日"×20 + " " + "x"×40` — through the byte-index boundary, not a rune one) and for `Derive` (empty → dated; "```go" → dated; zone of `now` honoured — a UTC and a UTC+9 instant at the day boundary spell different dates). Existing `TestSpecTitle*` (`internal/run/run_test.go:1076-1140`) and `TestDeriveName` (`internal/schedule/schedule_test.go:814`) pass unchanged; `TestSessionTitle` (`internal/tui/model_test.go:3633`, the only tui pin; ASCII-only table) passes unchanged — the `-run 'Session|Title|Rule'` acceptance reaches it.

**Acceptance.**
- `go build ./... && go test ./internal/title/ ./internal/run/ ./internal/schedule/ && go test ./internal/tui/ -run 'Session|Title|Rule'`
- `grep -rn '6/10' --include=*.go internal cmd | grep -v _test | grep -v internal/title/` → no output (one copy of the boundary rule).
- `grep -n 'internal/tui/model.go' internal/title/title.go` → no output.

**Commit.** `refactor(title): one session-title rule in internal/title replaces the tui, run and schedule copies`

## 2. `title.FirstLine` and `title.DelegateLabel` replace the delegate-naming copies — ✅ DONE (2026-08-31)

NOTES (2026-08-31): the tui's task fallback gains a trim (`firstLineArg("task")` returned `clipDetail(firstLine(v))` untrimmed; `title.FirstLine` trims) — the item's stated, admitted change, pinned by a new case in the added `TestSubAgentTargetFallsBackWhenTheNameStripsToNothing`.
NOTES (2026-08-31): `strings` became unused in `internal/agent/subagent.go` and `internal/run/run.go` once the Cut/Trim bodies went; both imports are dropped (the build requires it).
NOTES (2026-08-31): the new tui case is a direct table on `subAgentTarget` placed beside `model_test.go`'s approval-pane delegation tests, rather than a second indirect pin — the item asked for a case where a control-character-only name shows the task, and the direct table states exactly that on the header's own function.

**What.** `internal/title/title.go`: `FirstLine(s string) string` — text before the first `\n`, `TrimSpace`d; `DelegateLabel(name, task string) string` — `FirstLine(name)` when non-empty, else `FirstLine(task)`. Both take strings the caller has ALREADY escape-stripped where a render seam demands it (the fallback is decided on the rendered form — `internal/tui/toolregistry.go:1130-1148`, `cmd/apogee/headless.go:606-620`).
Callers: `internal/agent/subagent.go:101` `delegationName` → returns `title.FirstLine(raw)` (keep the function and its doc; delete only the body's own Cut/Trim). `internal/run/run.go:620-660` `firstTaskLine` / `delegationName` keep their JSON decode and return `title.FirstLine(decoded.X)`; delete both ADR 0010 apology sentences. `cmd/apogee/headless.go` `headlessSubAgentTarget` → `clipSubAgentTask(title.DelegateLabel(sanitize.StripEscapesToLine(r.Name), sanitize.StripEscapesToLine(r.Task)))`; rewrite its doc so it names `title.DelegateLabel` as the shared rule instead of "kept on the Driver that has no header to paint". `internal/tui/toolregistry.go:1143` `subAgentTarget` → `title.DelegateLabel(stripEscapes(subAgentName(args)), firstLineArg("task")(args))`; its doc's "headlessSubAgentTarget decides the same question the same way" sentence becomes "both call `title.DelegateLabel`". `usageAgentName` untouched (writer's call). The tui task fallback thereby gains a trim (`firstLineArg("task")` returns `clipDetail(firstLine(v))` untrimmed; `title.FirstLine` trims): a task `" audit"` heads its row as `audit` after, `" audit"` today — admitted, not preserved.
**Regression guard.** `internal/run/run.go:75` (`FinalText`'s doc, "the answer crosses as plain data") cites ADR 0010 legitimately and stays; the acceptance greps the two apology phrases (`duplicated from internal/agent`, `cannot be imported from it`) instead of `ADR 0010`. No tui test calls `subAgentTarget(` — its pins are indirect (`internal/tui/model_test.go:1437` `…ByName`, `render_test.go:470` `delegationCall`), so a control-character-only-name case is added beside them (headless has one at `cmd/apogee/headless_test.go:946`). The trim on the tui task fallback (`toolregistry.go:997-1003`) is stated in **What**, not hidden.

**Files:** `internal/title/title.go`, `internal/title/title_test.go`, `internal/agent/subagent.go`, `internal/run/run.go`, `cmd/apogee/headless.go`, `internal/tui/toolregistry.go`, `internal/tui/model_test.go`

**Tests.** `title_test.go`: `FirstLine` (multi-line keeps the first, trims, `\r\n` leaves no `\r`); `DelegateLabel` (name wins; empty name → task's first line; whitespace-only name → task; both empty → `""`). `internal/agent/subagent_test.go:792` table passes unchanged. Existing `cmd/apogee/headless_test.go` sub-agent-target cases (`:946` is the control-only name) pass unchanged; the tui's indirect pins (`internal/tui/model_test.go:1437`, `render_test.go:470`) pass unchanged and gain one case beside `:1437` where the delegation name is control characters only and the header shows the task — a control-character-only name falls back to the task on both Drivers.

**Acceptance.**
- `go build ./... && go test ./internal/title/ ./internal/agent/ -run 'Delegat|Name' && go test ./internal/run/ ./cmd/apogee/ -run 'SubAgent|Delegat|Title' && go test ./internal/tui/ -run 'SubAgent|Target'`
- `grep -n 'duplicated from internal/agent\|cannot be imported from it' internal/run/run.go` → no output (`run.go:75`'s ADR 0010 citation is not an apology and stays).

**Commit.** `refactor(title): one delegate-naming rule replaces the agent, run, headless and tui copies`

## 3. `internal/refs` owns the `@file` / `/skill` reference grammar — ✅ DONE (2026-08-31)

NOTES (2026-08-31): doc.go carries the package narration only — no `Files:` map. `internal/refs`
holds two non-test files, far under the ~10-file threshold that earns a map, and the plan's file
list has no `docmap_test.go`, so an unguarded map that can rot silently was left out rather than a
fourth file added.

**What.** New package `internal/refs` (files `doc.go`, `refs.go`, `refs_test.go`; imports stdlib only) holding the grammar now at `internal/tui/command.go:612-770`, moved verbatim in behaviour: `type Span struct { Start, End int; Name string }`; `FileSpans(s string) []Span`; `SkillSpans(s string, known func(string) bool) []Span` (nil `known` → nil); `Names(spans []Span) []string` (first-seen de-dupe); `ScanToken(s string, start int) (string, int)`; `IsSpace(b byte) bool`; `FileRefs(s string) []string` = `Names(FileSpans(s))`; `SkillRefs(s string, known func(string) bool) []string`. The doc comments travel with the functions (they are the grammar's spec: word boundary, bare/quoted forms, no escapes, a token never crosses a newline). `doc.go` names the package the parse half of CONTEXT.md's **File reference** and **Skill** `/token`, and says resolution is the agent's. This item ADDS the package only; tui keeps its copy until item 4 (no behaviour change here).

**Files:** `internal/refs/doc.go`, `internal/refs/refs.go`, `internal/refs/refs_test.go`

**Tests.** `refs_test.go` ports every grammar case from `internal/tui/command_test.go` that drives `fileRefSpans`, `skillRefSpans`, `scanRefToken`, `extractFileRefs`, `extractSkillRefs` (copied, not moved — item 4 deletes the originals): email `foo@bar.com` is not a ref; `@"a b"`, `@'a b'`, unterminated quote runs to end of line and is right-trimmed; bare `@` and `@""` skipped; `/usr/bin` and `and/or` are prose; `/x` counts only when `known` says so; `@x @"x"` → one name; spans carry byte offsets of the literal token, quotes included.

**Acceptance.**
- `go build ./... && go test ./internal/refs/ && go vet ./internal/refs/`
- `go list -f '{{join .Imports " "}}' ./internal/refs | grep -c 'apogee/internal'` → `0`.

**Commit.** `feat(refs): the @file and /skill reference grammar becomes its own package`

## 4. The TUI reads the grammar from `internal/refs` and drops its copy — ✅ DONE (2026-08-31)

NOTES (2026-08-31): consequential edit — CONTEXT.md: made necessary by deleting the TUI's grammar copy — the "File reference (`@file`)" entry said "Parsing the token is the TUI's job", which this item makes false; it now names `internal/refs` as the Driver-neutral grammar (item 3's verifier flagged the sentence; the run's DECISION assigned it here, and CONTEXT.md is not in the item's declared Files list).
NOTES (2026-08-31): consequential edit — docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md: made necessary by deleting `extractSkillRefs` / `refSpan` / `skillRefSpans` — decisions 2 and 7 named those symbols as the scanner they rest on; they now read `refs.SkillRefs` / `refs.Span` / `refs.SkillSpans`. The decisions themselves are untouched.
NOTES (2026-08-31): `command_test.go` also drops the now-stale half of the `knownSkills` doc comment (it named `extractSkillRefs`); the helper itself stays — five other test files use it.

**What.** Depends on item 3. Delete from `internal/tui/command.go` the `refSpan` type, `fileRefSpans`, `skillRefSpans`, `spanNames`, `extractFileRefs`, `extractSkillRefs`, `scanRefToken`, `isInputSpace` (`:612-770`); keep `skillTokenSpans` there, now `func skillTokenSpans(spans []refs.Span) []skillSpan` (the one render-typed adapter). Switch every reader: `command.go` `parseInput` (`extractFileRefs` returned `(s, names)` — it now uses `refs.FileRefs(s)` and `refs.SkillRefs(s, known)`); `autocomplete.go:323,326,372,378,928,972,976` (`isInputSpace` → `refs.IsSpace`, `scanRefToken` → `refs.ScanToken`); `inputaccent.go:69,72`; `suggestband.go:64,86`. Delete the ported grammar tests from `command_test.go` (item 3 holds them); tests that exercise `parseInput`, the accents or the band stay. `internal/tui/doc.go`: the `command.go` map entry (`:101`), the accent/band sentences naming `scanRefToken` / `extractSkillRefs` (`:182`, `:200`, `:1000`) now name `internal/refs` as the one scanner; the escape-set invariant paragraph is untouched.
**Regression guard.** `parseInput` needs the skill SPANS, not names: it calls `refs.SkillSpans(trimmed, known)` once, then `refs.Names(spans)` for `skillIDs` and `skillTokenSpans(spans)` for `skillSpans` (`internal/tui/skill_test.go:522` and `transcriptcodec_test.go:66` pin the spans); the local `refs` at `command.go:289` is renamed `fileRefs` so it no longer shadows the package. The reader list above is a rule, not a closed list: every call of a deleted name is switched (`autocomplete.go:548` `extractSkillRefs` → `refs.SkillRefs` is the one the list omitted) and every comment naming a moved identifier now names `internal/refs` (`transcript.go:302`, `skills.go:26`, `autocomplete.go:318,357,643`, `inputaccent.go:145-146`, `suggestband.go:79,83`, `command.go:287,309`); `grep -n 'extractSkillRefs\|extractFileRefs\|skillRefSpans\|fileRefSpans\|scanRefToken\|isInputSpace\|refSpan' internal/tui/*.go` finds both kinds and ends empty.

**Files:** `internal/tui/command.go`, `internal/tui/command_test.go`, `internal/tui/autocomplete.go`, `internal/tui/inputaccent.go`, `internal/tui/suggestband.go`, `internal/tui/skills.go`, `internal/tui/transcript.go`, `internal/tui/doc.go`

**Tests.** Existing: `go test ./internal/tui/ -run 'Parse|Input|Accent|Autocomplete|Suggest|Skill|Minilang|Codec|Transcript'` passes unchanged — the paint of `@ref` and `/skill` tokens, the band's hint draft and the sent block's `skillSpan`s are pinned by those tests. New: none beyond the ports (behaviour-preserving move).

**Acceptance.**
- `go build ./... && go test ./internal/tui/ && go test ./internal/refs/`
- `grep -n 'extractSkillRefs\|extractFileRefs\|skillRefSpans\|fileRefSpans\|scanRefToken\|isInputSpace\|refSpan' internal/tui/*.go` → no output (definitions, calls and comments alike).
- `grep -c 'refs' internal/tui/doc.go` ≥ 1 and `go test ./internal/tui/ -run Docmap`.

**Commit.** `refactor(tui): the prompt box parses references through internal/refs`

## 5. A Firing's prompt resolves its `@file` references — ✅ DONE (2026-08-31)

NOTES (2026-08-31): the item's known prose site `CONTEXT.md:1073` ("Parsing the token is the TUI's job") was already rewritten by item 4, so the guard grep found only `internal/domain/config.go:676` ("chat input") to fix; CONTEXT.md instead gained the new capability sentence (a Firing's prompt reads the same grammar, missing ref skipped without notice).
NOTES (2026-08-31): the `run_test.go` fixture writes its own `go.mod` (first line `module github.com/airiclenz/apogee`) into `t.TempDir()` rather than pointing the workspace at the repo — "never a fixture path"; falsification checked (with `FileRefs` unset the block-and-first-line assertions fail).
NOTES (2026-08-31): the headless.md sentence was placed inside the prompt paragraph, straight after "Empty from both is a usage error.", so the `@path` prose sits with the prompt rather than ahead of the flag resolution sentence.

**What.** Depends on item 3. `internal/run/run.go:236`: submit `domain.UserInput{Text: spec.Prompt, FileRefs: refs.FileRefs(spec.Prompt)}` — the loop resolves them as a session's (`internal/domain/config.go:675-685`; a missing or escaping ref is skipped by the agent — without notice, since a Firing has no event sink). `SkillIDs` stay unset (writer's call). Docs: `docs/manual/headless.md` gains one sentence after the prompt paragraph (`:16-18`): "`@path` tokens in the prompt are **file references** — bare or quoted (`@"a b.md"`) — read from the workspace and attached to the message as in a session; a missing ref is skipped without notice — a Firing has no event sink." `docs/manual/daemon.md:33` comment gains `# @file references resolve as in a session`. `internal/run/doc.go`: one sentence that the prompt's `@file` tokens are parsed here through `internal/refs`, and that a missing ref is skipped without notice — a Firing has no event sink. **Prose guard — the rule:** every sentence in `CONTEXT.md`, `docs/manual/`, `internal/run`, `internal/domain/config.go` stating that parsing `@file` tokens is the TUI's job or that references exist only in a session is rewritten to name the Driver-neutral parse (`internal/refs`); `CONTEXT.md:1073` ("Parsing the token is the TUI's job; resolution is the agent's.") is the known site. Find them with `grep -rn -i "TUI's job\|only the TUI\|only in a session\|interactive session\|chat input" CONTEXT.md docs/manual internal/run internal/domain/config.go`.
**Regression guard.** Headless and daemon runs leave `Config.Events` nil (`cmd/apogee/wire_firing.go:88-92`) and the run tap forwards to no inner sink (`internal/run/run.go:481-482`), so a missing or escaping ref in a Firing is skipped with NO notice — "exactly as in a session" would overstate: the `headless.md` and `run/doc.go` sentences say "a missing ref is skipped without notice — a Firing has no event sink", and the `@"no such.md"` test asserts the first request's user text is the prompt verbatim. `internal/domain/config.go:676` ("FileRefs (@file tokens parsed from the chat input)") escapes the guard grep as first written: `chat input` is in the grep and `:676` is rewritten to "parsed by `internal/refs` from the prompt".

**Files:** `internal/run/run.go`, `internal/run/run_test.go`, `internal/run/doc.go`, `internal/domain/config.go`, `docs/manual/headless.md`, `docs/manual/daemon.md`, `CONTEXT.md`

**Tests.** `internal/run/run_test.go`, on the existing scripted-upstream harness (`harness_test.go:77 newUpstream`): a Firing whose prompt is the manual's own form — `summarise @go.mod` — records a first request whose user content carries `go.mod`'s first line (`module github.com/airiclenz/apogee`) in the loop's file-context form (take the exact shape from `internal/agent`'s `resolveFileRefs` tests, never a fixture path); a second case `@"no such.md"` completes with no error from `run.Once` and a first request whose user text is the prompt verbatim — the skip leaves no notice a Firing could carry.

**Acceptance.**
- `go build ./... && go test ./internal/run/`
- `grep -rn -i "TUI's job" CONTEXT.md docs/manual` → no output; `grep -n 'chat input' internal/domain/config.go` → no output; `grep -n '@' docs/manual/headless.md | grep -c 'file reference'` ≥ 1.

**Commit.** `feat(run): a Firing's prompt resolves @file references like a session's`

## 6. `probe.AutoUnattendedBlocked` is the one unattended-Auto verdict — ✅ DONE (2026-08-31)

NOTES (2026-08-31): deviation — the item says `cmd/apogee/daemon.go:559` is unchanged "because it calls `scheduleAutoBlocked`"; it in fact called the moved `autoUnattendedBlocked("a firing", …)` directly, so deleting the function forced the line. It was retargeted to `scheduleAutoBlocked(probe.BackendName(…), …)` — the plan's own stated belief about that line, byte-identical noun and wording — leaving item 7's daemon reshape untouched. Its neighbouring comment (`daemon.go:227`) was re-pointed to the surviving name.
NOTES (2026-08-31): consequential edit — internal/probe/doc.go: made necessary by adding AutoUnattendedBlocked to confinement.go, whose file-map line enumerates that file's wording functions.
NOTES (2026-08-31): the moved function is appended after `ResidualNotice` rather than inserted between it and `DegradedNotice`, so the "SIBLING of DegradedNotice, never its overlap" pairing those two doc comments build stays adjacent; "beside `DegradedNotice`" is satisfied by the file.
NOTES (2026-08-31): the moved table (`TestAutoUnattendedBlockedMirrorsTheAutoLadder`) gained the noun axis the plan asked for — it now runs its four ladder cells across both subjects ("a firing", "a headless run") and both backends (`deny`, `landlock`) and asserts the exact sentence, so the wording is pinned once in probe rather than at each surface. `cmd/apogee`'s two kept tests are unchanged in intent: `TestScheduleAutoBlockedFollowsTheHostBackend` still reads the host backend through `scheduleAutoBlocked`, and `TestHeadlessAutoRefusalSharesTheScheduleSentence` now calls `probe.AutoUnattendedBlocked`.

**What.** Move `autoUnattendedBlocked` (`cmd/apogee/schedule.go:254-280`) to `internal/probe/confinement.go` as `AutoUnattendedBlocked(subject, backend string, caps domain.ConfinementCaps, confineToWorkspace bool) string`, beside `DegradedNotice` whose gate it mirrors; the doc paragraph and the exact wording move with it (`"the %s backend on this host reports no filesystem confinement, so auto falls back to approval — and %s has nobody to ask"`). `cmd/apogee/schedule.go`: delete the moved function; `scheduleAutoBlocked` stays as the one-line delegate that fixes the noun `"a firing"`. `cmd/apogee/headless.go:306` calls `probe.AutoUnattendedBlocked("a headless run", …)`. `cmd/apogee/daemon.go:559` and `wire_options.go:258` unchanged (they call `scheduleAutoBlocked`; item 7 reshapes the daemon side).
**Regression guard.** The `cmd/apogee/doc.go` step is dropped (the `schedule.go` map entry never claimed the verdict lives there) and `cmd/apogee/doc.go` leaves its **Files:** line. `nobody to ask` is not the sentence's fingerprint — seven other non-test lines carry it and survive the move (`internal/daemon/file.go:339`, `internal/run/run.go:361`, `cmd/apogee/headless.go:193,241`, `internal/tui/tui.go:1054`, `internal/tui/prebound.go:17`, `internal/config/config.go:2952`); the acceptance greps the sentence's own fragment `so auto falls back to` (today only `cmd/apogee/schedule.go:278`).

**Files:** `internal/probe/confinement.go`, `internal/probe/confinement_test.go`, `cmd/apogee/schedule.go`, `cmd/apogee/schedule_test.go`, `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`

**Tests.** Move the wording table at `cmd/apogee/schedule_test.go:1000-1020` to `internal/probe/confinement_test.go` (blocked iff `confineToWorkspace && !caps.AutoEligible()`; the exact sentence with `deny` / `landlock` backends and both nouns). Keep `cmd/apogee/headless_test.go:335-350` (the two surfaces differ only in the noun) and `schedule_test.go:1094` (mirror of `caps.AutoEligible()`), retargeted to the new names.

**Acceptance.**
- `go build ./... && go test ./internal/probe/ -run 'Unattended|Auto' && go test ./cmd/apogee/ -run 'AutoBlocked|Unattended|Headless.*Auto'`
- `grep -n 'func autoUnattendedBlocked' cmd/apogee/*.go` → no output; `grep -rn 'so auto falls back to' --include=*.go internal cmd | grep -v _test` → exactly one line, in `internal/probe/confinement.go`.

**Commit.** `refactor(probe): the unattended-Auto verdict lives beside the confinement notices`

## 7. `daemon.Host` carries the confinement facts and asks probe itself — ✅ DONE (2026-08-31)

NOTES (2026-08-31): `cmd/apogee/daemon.go:227`'s comment — the item says to delete it — was RETARGETED, not deleted: only the clause naming the `scheduleAutoBlocked` verdict `daemonHost` hands over is now false, while the enclosing block's reason for printing only the residual notice at startup still holds. `daemonHost`'s own doc comment is retargeted the same way (facts handed over, verdict asked of probe).
NOTES (2026-08-31): `cmd/apogee/daemon_test.go`'s two host cases assert the VERDICT rather than a field, because the facts alone invert the old meaning: `TestDaemonHostLooksUpTheStartupDefault`'s options set no `ConfineToWorkspace`, so its host is Auto-eligible by the waiver, not by a fence — it now checks `scheduleAutoBlocked(...) == ""`. `TestDaemonHostRefusesAutoOnAHostThatCannotFence` now asserts through `daemon.Load` on a `mode: auto` entry, mirroring its launcher-rule sibling, so the test delivers the refusal its name promises.

**What.** Depends on item 6. `internal/daemon/file.go:84-103`: replace `AutoEligible bool` with `Confinement HostConfinement` where `type HostConfinement struct { Backend string; Caps domain.ConfinementCaps; Unconfined bool }` — `Unconfined` is true ⇔ `confine-to-workspace: false`, stored inverted so the zero value is confined-and-unproven, i.e. refuses (doc: the three facts the unattended-Auto verdict needs, ADR 0012 / ADR 0033 decision 3). `resolveMode(label, mode, host)` (`:322-343`) computes `probe.AutoUnattendedBlocked("a firing", host.Confinement.Backend, host.Confinement.Caps, !host.Confinement.Unconfined) == ""` in place of the bool; its refusal wording is UNCHANGED (pinned by `file_test.go`). `internal/daemon` gains the `internal/probe` import (`go build` confirms no cycle: probe imports domain/library/processing/provider, none of which import daemon). `cmd/apogee/daemon.go:552-561`: `Host{…, Confinement: daemon.HostConfinement{Backend: probe.BackendName(wiring.confiner), Caps: wiring.confiner.Capabilities(), Unconfined: !opts.ConfineToWorkspace}}`; delete the comment at `daemon.go:227` that describes the bool hand-off. **Guard — the rule:** every literal `daemon.Host{` / `Host{` in `internal/daemon/*_test.go` and `cmd/apogee/*_test.go` that sets `AutoEligible` is rewritten to the facts (`Caps: domain.ConfinementCaps{FSWrite: true}` for eligible, the zero `Confinement` for blocked); find them with `grep -rn 'AutoEligible' internal/daemon cmd/apogee`.
**Regression guard.** The zero value must keep refusing: today `Host{}` refuses `mode: auto` (`internal/daemon/file.go:325-333`, `AutoEligible` false — the contract `file.go:99-102` records), but a facts struct carrying `ConfineToWorkspace: false` would read as the user's "I am the sandbox" (`cmd/apogee/schedule.go:274` returns `""`), so a Driver that builds a `Host` without `Confinement` would get an unattended auto Firing accepted. The flag is therefore stored inverted as `Unconfined bool` (zero = confined + no `FSWrite` = blocked) and `file_test.go` gains the case `Host{}` refuses `mode: auto`; `file.go:99-102`'s refusal contract is kept, not superseded. `cmd/apogee/daemonfire.go:65` is a comment naming `Host.AutoEligible`: `cmd/apogee/daemonfire.go` joins **Files:** and the rule covers every mention the `grep -rn 'AutoEligible' internal/daemon cmd/apogee` sweep returns, comments included, retargeted to `Host.Confinement`.

**Files:** `internal/daemon/file.go`, `internal/daemon/file_test.go`, `internal/daemon/doc.go`, `cmd/apogee/daemon.go`, `cmd/apogee/daemon_test.go`, `cmd/apogee/daemonfire.go`

**Tests.** `file_test.go`: `mode: auto` refused with the existing wording when `Caps.FSWrite=false` and `Unconfined=false`, and by the bare `Host{}` (the zero value fails closed); accepted when `FSWrite=true`; accepted when `Unconfined=true` (the user's own "I am the sandbox", ADR 0033 decision 3 — new case, previously unreachable through a bool). `cmd/apogee/daemon_test.go` cases constructing the host pass with the facts.

**Acceptance.**
- `go build ./... && go test ./internal/daemon/ && go test ./cmd/apogee/ -run 'Daemon'`
- `grep -rn 'AutoEligible' internal/daemon cmd/apogee` → only `caps.AutoEligible()` / `ConfinementCaps.AutoEligible` mentions remain (no `Host.AutoEligible`, in code or comment).

**Commit.** `refactor(daemon): the schedules file asks probe for the Auto verdict instead of taking a bool`

## 8. `mechanisms.ResolveEnabled` validates the `mechanisms:` block and words the retired-id notice — ✅ DONE (2026-08-31)

NOTES (2026-08-31): the ported unknown-key error helper is named `knownIDList` (not `knownMechanismList`) because `internal/mechanisms/catalogue.go:246` already owns `knownList` over the registry table; the rendered text, `(none)` included, is byte-identical.
NOTES (2026-08-31): ported tests renamed onto the new symbol (`TestResolveEnabled*`), so they still match the item's `-run 'Resolve|Retired'` acceptance; `fakeKnown` is now `[]domain.MechanismID` since the library cannot import the root alias.
NOTES (2026-08-31): three cases added beyond the port, all pinning behaviour the merge makes newly reachable: `(none)` for an empty catalogue, and no notices returned when an unknown key errors (matching both cmd callers, which return before printing).
NOTES (2026-08-31): the item's acceptance command spells `-run Docmap`; the test is `TestDocMapNamesEveryFile`, so that spelling matches nothing and passes vacuously. Ran `-run DocMap` as well — it passes.

**What.** `internal/mechanisms/retired.go`: add `ResolveEnabled(enabled map[string]bool, known []domain.MechanismID) (ids []domain.MechanismID, notices []string, err error)` — the body of `mechanismIDs` (`cmd/apogee/wire_tools.go:272-300`: sorted keys, retired ids skipped, unknown id → `fmt.Errorf("apogee: unknown mechanism %q; known: %s", id, list)` with `(none)` for an empty catalogue, nil ids when nothing is enabled) PLUS `retiredMechanismNotices` (`:316-338`: one line per retired id set ON, sorted, exact text `"apogee: mechanism %q was retired in %s and is ignored; remove it from mechanisms:"`) returned together, so a caller cannot take the ids and forget the lines. `RetiredRelease = "v0.18.7"` moves in as an exported const with its comment. The doc paragraph at `wire_tools.go:262-271` (why retired ids are tolerated silently and why the producer prints nothing) travels with the function. This item ADDS the function only; `cmd/apogee` switches in item 9. `internal/mechanisms/doc.go` map: `retired.go`'s entry names the resolver.
**Regression guard.** `TestMechanismIDsConstructsUnderBypass` (`cmd/apogee/wire_tools_test.go:288-310`) calls `validCfg` + `apogee.New`, and `internal/mechanisms` cannot import root (`apogee.go:42` imports `internal/mechanisms` — a cycle, ADR 0010): it is NOT ported. Only `:221-287` and `:311-370` are copied; that one test stays in `cmd/apogee` (item 9 retargets it to `ResolveEnabled`). `retired_test.go` gets its own `captureStderr` (`:328` uses the cmd one at `cmd/apogee/wire_helpers_test.go:22`, unreachable from here; `internal/library/store_test.go:414` is the in-tree pattern).

**Files:** `internal/mechanisms/retired.go`, `internal/mechanisms/retired_test.go`, `internal/mechanisms/doc.go`

**Tests.** `retired_test.go` ports `cmd/apogee/wire_tools_test.go:221-287` and `:311-370` (copied; item 9 deletes the originals; `TestMechanismIDsConstructsUnderBypass` at `:288-310` stays in cmd) with its own `captureStderr`: enabled/disabled filtering with a fake `known`; nil when nothing enabled; unknown enabled AND unknown disabled both refused with the exact error tail; a retired id tolerated on and off; notices only for retired ids set ON, sorted, exact text; no notices → nil.

**Acceptance.**
- `go build ./... && go test ./internal/mechanisms/ -run 'Resolve|Retired' && go test ./internal/mechanisms/ -run Docmap`

**Commit.** `feat(mechanisms): ResolveEnabled validates the mechanisms block and words the retired-id notice`

## 9. `cmd/apogee` resolves Mechanism ids through the library

**What.** Depends on item 8. Delete `mechanismIDs`, `retiredMechanismRelease`, `retiredMechanismNotices`, `knownMechanismList` from `cmd/apogee/wire_tools.go` and their tests from `wire_tools_test.go:221-287` and `:311-370` (the Bypass construction test at `:288-310` stays — see the guard). Switch every caller to `mechanisms.ResolveEnabled(…, mechanisms.KnownIDs())`, converting `[]domain.MechanismID` to `[]apogee.MechanismID` where the composer wants the alias: `wire_live.go:158-169` (the boot path: it already prints the notices — now the second return value); `wire_settings.go:1657-1662` (`reloadMechanisms`: join the returned notices as today); `delegation.go:487` (sub-agents posture: `ids, _, err :=` with a one-line comment that a child's posture never prints — the alt screen is up); `headless.go:341` and `daemonfire.go:93` bind the notices to a local this item DISCARDS with `_` and a `// item 10 prints these` marker — behaviour unchanged here. `guided_decomposition_test.go:22` retargets. `cmd/apogee/doc.go`: the `wire_tools.go` entry drops the id-validation clause.
**Regression guard.** `cmd/apogee/validatedsets.go:202` (`retiredSetMemberNotice`) reads `retiredMechanismRelease`: it switches to `mechanisms.RetiredRelease` (notice text unchanged — same `v0.18.7`) and the file joins **Files:**, or `go build ./cmd/apogee` fails at this item's own acceptance. `TestMechanismIDsConstructsUnderBypass` (`wire_tools_test.go:288-310`) is the only config → `EnableMechanisms` → `apogee.New` coherence proof and item 8 cannot carry it: it is KEPT, retargeted to `mechanisms.ResolveEnabled(…, mechanisms.KnownIDs())`; only `:221-287` and `:311-370` are deleted. Every comment naming a deleted identifier is re-pointed — `delegation.go:131,476,489`, `wire_settings.go:1643`, `guided_decomposition_test.go:4,17`, `internal/config/config.go:3030` (`knownMechanismList`), `internal/config/configwrite_mechanism.go:23` (the two `internal/config` edits are comment-only) — so `grep -rn 'mechanismIDs\|knownMechanismList\|retiredMechanismNotices\|retiredMechanismRelease' cmd internal` ends empty (`ISSUES.md:75` names the loop too; item 10 removes that entry).

**Files:** `cmd/apogee/wire_tools.go`, `cmd/apogee/wire_tools_test.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/delegation.go`, `cmd/apogee/headless.go`, `cmd/apogee/daemonfire.go`, `cmd/apogee/validatedsets.go`, `cmd/apogee/guided_decomposition_test.go`, `internal/config/config.go`, `internal/config/configwrite_mechanism.go`, `cmd/apogee/doc.go`

**Tests.** Existing `cmd/apogee` tests around boot notices, `reloadMechanisms`, the validated-set retired-member notice and the guided-decomposition posture pass unchanged, and the kept `TestMechanismIDsConstructsUnderBypass` passes over `ResolveEnabled` (`go test ./cmd/apogee/ -run 'Mechanism|Retired|Guided|Reload|ValidatedSet'`).

**Acceptance.**
- `go build ./... && go test ./cmd/apogee/ -run 'Mechanism|Retired|Guided|Reload|Docmap'`
- `grep -rn 'mechanismIDs\|knownMechanismList\|retiredMechanismNotices\|retiredMechanismRelease' cmd internal` → no output (definitions, calls and comments alike; `ISSUES.md` is item 10's).

**Commit.** `refactor(cmd): mechanism ids resolve through mechanisms.ResolveEnabled`

## 10. Headless and daemon runs say when a retired Mechanism is ignored

**What.** Depends on item 9; a fix for a gap the review found (`wire_tools.go:271` "only the pre-TUI callers print"). `cmd/apogee/headless.go:341`: print each returned notice to stderr (`cmd.PrintErrln`, the channel the plaintext-key and confinement notices use at `:276-336`) before the run starts. `cmd/apogee/daemonfire.go:93`: `newDaemonWiring` returns the notices beside the wiring, and `runDaemon` (`cmd/apogee/daemon.go`) writes each through `daemonLog` at startup, before the schedules file is loaded. Remove the `// item 10 prints these` markers. `docs/manual/headless.md` and `docs/manual/daemon.md`: no new section — one clause each, anchored on `headless.md:35` (the "resolution notices" sentence) and on the "`config.yaml` is read **once**, at startup" paragraph at `daemon.md:76-77` (the daemon manual has no line matching `notice` today).
**Regression guard.** the item ALSO pins the TUI boot path — a test drives the startup notice loop (`cmd/apogee/wire_live.go:158-169` after item 9) with a retired id and asserts the exact stderr line — and removes the `ISSUES.md:72-76` entry ("No test drives the startup path that prints the grammar-retirement notice") which that test closes; `ISSUES.md` and the wire_live test file join its **Files:** line; the commit stays `fix(cmd)`. `newDaemonWiring` gains a second return, and its callers are `daemon.go:210`, `daemon_test.go:602,639,658,821` AND `daemonfire_test.go:54` — `cmd/apogee/daemonfire_test.go` joins **Files:** or `go test ./cmd/apogee/` stops compiling. The manual anchors are named in **What** because `grep notice` finds nothing in `daemon.md`.

**Files:** `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/daemonfire.go`, `cmd/apogee/daemonfire_test.go`, `cmd/apogee/daemon.go`, `cmd/apogee/daemon_test.go`, `cmd/apogee/wire_live_test.go`, `ISSUES.md`, `docs/manual/headless.md`, `docs/manual/daemon.md`

**Tests.** Journey tests with the EXACT emitted line: a headless run whose options carry `Mechanisms: {"grammar": true}` (a retired id, `internal/mechanisms/retired.go`) writes `apogee: mechanism "grammar" was retired in v0.18.7 and is ignored; remove it from mechanisms:` to stderr and still runs; a daemon startup with the same option logs the same line once; a TUI boot (`wire_live.go:158-169`, `cmd/apogee/wire_live_test.go`, stderr captured with `captureStderr` — sequential, not parallel) prints the same line once. Use the existing headless / daemon test harnesses in `cmd/apogee` (`headless_test.go`, `daemon_test.go`); the string is taken from `mechanisms.RetiredRelease` + the notice format, never retyped.

**Acceptance.**
- `go build ./... && go test ./cmd/apogee/ -run 'Retired|Headless|Daemon|Boot|Live'`
- `grep -c 'grammar-retirement notice' ISSUES.md` → `0`.

**Commit.** `fix(cmd): headless and daemon runs report a retired mechanism instead of arming nothing silently`

## 11. `ISSUES.md` records the Driver-parity gaps; the review report joins the repo

**What.** Under `## Parked / deferred work` add `### Driver-parity gaps — behaviour only the TUI can reach (architecture review 2026-08-30)` with one `- [ ]` line per gap, each with its TUI site and the Driver that lacks it: context-files report (`internal/tui/model.go:742`; `run.Result` carries none); token spend + sub-agent fill in the daemon (the `schedule.Outcome` at `cmd/apogee/daemonfire.go:237` carries no `res.Usage`); validated-set / profile / roster notices in the daemon (`daemonfire.go:207`, `cfg, _, err := firingConfig(`, discards the slice); unconfined-Auto warning in the daemon (`wire_boot.go:278`); Windows label pre-warm on headless/daemon (`wire_boot.go:315`); offline refusal before send (`internal/tui/heartbeat.go:620`); "model not advertised" hint (`cmd/apogee/upstream.go:559`); undo surface for an auto run (`internal/tui/undo.go:45`); `--server` / `--bypass` on `headless` (`root.go:105,117`). Close with a pointer to `docs/reviews/architecture-review-20260830.html`. Stage that report file (already written, untracked) in this commit. No entry for the deferred hoist candidates — the report and this plan's out-of-scope list are their record.
**Regression guard.** Two locators as first drafted pointed at prose, not the site: `daemonfire.go:200` is the model-overlay comment (the notice slice is discarded at `:207`) and `:236` is a comment (the Outcome without `res.Usage` starts at `:237`); the entry cites `:207` and `:237`. Every other cited site (`model.go:742`, `wire_boot.go:278/:315`, `heartbeat.go:620`, `upstream.go:559`, `undo.go:45`, `root.go:105/:117`) resolves at `a9ec2131`.

**Files:** `ISSUES.md`, `docs/reviews/architecture-review-20260830.html`

**Tests.** None (docs only; no `make check`).

**Acceptance.**
- `grep -c 'Driver-parity gaps' ISSUES.md` → `1`; `grep -c '^- \[ \]' ISSUES.md` grew by 9; `git ls-files docs/reviews/architecture-review-20260830.html` → listed.

**Commit.** `docs: the Driver-parity gaps enter ISSUES.md and the 2026-08-30 architecture review is saved`

---

**Suggested version bump:** micro (0.x.y+1) at closeout — two user-visible gains (`@file` in Firings, the retired-Mechanism notice on headless/daemon); the owner decides.
