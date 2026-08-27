# Quick wins, posture drift, resource exhaustion, and CI supply chain

**Goal:** close the three interaction-free code-audit defects (a Bypass-floor breach in the
syntax Mechanism, a width-authority reset on a colour-scheme switch, a launch-profile move that
keeps the departed server's fan-out pin), the four posture-drift findings where the code
contradicts a written invariant (opener allow-list, skill-cap priority, `internal/security`
package doc), bound what a streamed completion may cost in bytes on both sides of the wire, and
pin the CI supply chain.

**Date:** 2026-08-26 · **Status:** unexecuted · **sized for:** ~200k-context host

**Evidence:** merged audit findings `docs/handoffs/2026-08-26 - 00 - merged-audit-findings.md`
§3.5, §3.8, §5 waves 8/9/12. Code audit (`docs/reviews/code-audit-2026-08-25.md`): C-02
independently verified — `const re = /['"]/g;` written to a `.js` file returns `valid=false`
and `syntax.go` turns that into `domain.ActionRetry` against correct code, the exact class the
engine's own comment forbids; C-14 (not independently verified) — every live `ui.color-scheme`
switch rebuilds the theme and re-arms `measure` at `ansi.WcWidth` after the terminal moved the
painter to `GraphemeWidth`; C-19 verified — `sessionMover.move` never calls `caps.follow`, so a
profile load from an entry pinning `parallel-agents:` keeps that pin for the new server. Security
audit (`docs/skill-runs/security-audit/2026-08-25/report.md` §1, all fire on a stock install):
F-04 the rung-1 opener admits `.odt/.ods/.odp/.epub`, the same handler class ADR 0019's third
amendment removed; F-06 the 1024-skill cap fills from the workspace first while `Catalog.set` is
last-write-wins, so a repo can starve the user's library and own its ids; F-09 the package doc
says `internal/security` imports only `internal/domain` and the standard library while
`urlsafety.go` imports `golang.org/x/net/idna`; F-27 a streamed reply has no total byte cap and
the TUI re-copies its whole buffer on every append; F-33/F-34 every GitHub Action is pinned by a
mutable major tag and the `contents:write` tag job persists its push credential into
`.git/config` before running a repo-authored script.

**Authoritative sources:**
- `internal/mechanisms/syntaxengine.go:135-253` (`checkBrackets`; comment gate `:184-194`, quote
  branch `:196-201`; the floor sentence at `:88-92`), `internal/mechanisms/syntax.go:61-70`
  (`PostResponse` → `ActionRetry`), `internal/mechanisms/robustness.go:32` (`syntaxID = "syntax"`),
  `:88` (`isWriteTool`); `internal/mechanisms/syntaxengine_test.go:14-174` (negative table),
  `:178` (`TestCheckSyntaxReportsEachBrokenShape`).
- `internal/tui/theme.go:125-130` (`theme.measure`), `:286-321` (`newTheme`, `measure:
  newWidthAuthority()` at `:321`), `internal/tui/settingsapply.go:305-322` (`applyColorScheme`,
  `m.th = newTheme(s)` at `:313`), `internal/tui/width.go:44-80` (`widthAuthority`, `observe`),
  `:95-108` (`foldModeReport`), `internal/tui/model.go:476` (the construction-time `newTheme`).
  ADR 0030 (one width authority mirroring the painter), ADR 0011 (value-copied Model).
- `cmd/apogee/upstream.go:200-214` (`sessionMover`), `:238-297` (`move`), `:392-431`
  (`parallelAgentsCap`, `follow`), `cmd/apogee/wire_verbs.go:132-155` (`switchServer`, the
  `caps.follow` at `:153`), `cmd/apogee/wire_server.go:114` (`serverBinder.bind`'s follow),
  `cmd/apogee/launcher.go:530` (`launcherWiring` embeds `sessionMover`), `:695-697` (the
  profile load's `Move` closure), `cmd/apogee/wire_live.go:184,248,282`,
  `internal/config/config.go:1621` (`ResolveParallelAgents`). ADR 0039 (the cap follows the
  server), ADR 0029 (a load follows the profile).
- `internal/provider/stream.go:61-87` (`Stream`), `:140-225` (`parseSSE`; content/thinking yield
  at `:200-205`), `:231-257` (`accumulateToolCalls`, cap at `:249`),
  `internal/provider/client.go:17-29` (the bound constants: `maxToolCallBytes` `:22`,
  `maxErrorBodyBytes` `:28`), `:267` (`Respond`'s unbounded `json.NewDecoder(resp.Body)`),
  `:340-356` (`attemptContext`, `do` — the request carries the caller's ctx),
  `internal/agent/loop.go:640-660` (`streamResponse`), `:1203` (`maxOutputTokenCap = 32768`).
  ADR 0046 (the engine bounds every reply).
- `internal/tui/transcript.go:27-45` (`pending`, `parked`), `:86-89` (`parkedText`), `:429-503`
  (`displace`, `park`, `stash`, `unpark`, `takePending`), `:919-927` (`appendToken`, the
  `t.pending += …` at `:926`), `:940-948`, `:960`, `:990`, `:1006`, `internal/tui/render.go:186-194`
  (`paintPreview`), `:472` (`previewTailLines = 256`), `:487` (`previewTail`),
  `internal/tui/sink.go:62` (`tokenCoalesceWindow = 30ms`), `:88-102` (`emitToken`).
- `internal/present/opener.go:254-296` (the allow-list rule), `:297-339`
  (`openerRenderableExts`; `.epub` `:320`, `.odt` `:322`, `.ods` `:324`, `.odp` `:326`), `:354`
  (`OpenerRenderable`); `internal/present/opener_test.go:336-370`; ADR 0019 amendments at
  `docs/adr/0019-documents-are-presented-not-opened.md:172,216,253,284` (file ends `:333`).
- `internal/skills/load.go:25-28` (`maxSkills`), `:64-70` (`Load`), `:89-115` (`sourceAnchors`),
  `:192-201` (the cap), `:267` (`cat.set`), `internal/skills/catalog.go:36-52` (`set`),
  `internal/skills/doc.go:26`, `internal/tui/skills.go:303-327` (`shadowedBy`, `partitionSkips`);
  ADR 0032 (`docs/adr/0032-the-user-skill-library-outranks-the-workspace.md`, Context `:7-12`,
  Consequences `:80-95`); `CONTEXT.md` **Skill** (`:1025-1047`).
- `internal/security/doc.go:55-57`, `internal/security/urlsafety.go:12,190,259`, `go.mod:23`;
  ADR 0010 invariant (`docs/adr/0010-…:46-47`).
- `.github/workflows/ci.yml:20-21,54-55`, `.github/workflows/tag-on-version-bump.yml:24,35`,
  `.github/scripts/version-bump.sh`; `docs/manual/building.md:46-47`; `AGENTS.md` (the tag rule).
- `CONTEXT.md` **Bypass mode** (`:511`), **Mechanism** (`:816`); ADR 0006.

**Ratified design calls (owner, 2026-08-26, via AskUserQuestion):**
1. F-04 is FIXED with an ADR 0019 amendment (not denied): `.odt`, `.ods`, `.odp` and `.epub`
   leave the rung-1 opener allow-list, recorded as the fifth amendment.
2. F-33 and F-34 are FIXED (not denied): every action pinned to a full commit SHA; the tag job
   persists no credential and hands its token to the one step that needs it.
3. (Author call — listed under OPEN CALLS) F-06 is fixed by walking the HIGHEST-priority source
   FIRST with a keep-first collision set, so the cap can never evict a higher-priority skill —
   not by per-source budgets. The same keep-first rule governs a same-source collision.
4. The Windows pair F-08/C-20 and the design-discussion items C-13/C-03/C-04 are OUT of this
   plan (parked in `ISSUES.md` by the coordinator).
5. (Author, no user-visible alternative) F-27 is two items: the provider's byte bound and the
   TUI's buffer are different failure modes on different sides of the seam.
6. (owner, 2026-08-26) F-33 ships with a github-actions-only dependabot config; majors stay
   current, not upgraded.

**Standing requirements:**
- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- No version identifier changes (see closing note).
- Every item's Acceptance is targeted; `make check` runs once at closeout.
- The Bubble Tea `Model` is value-copied on every `Update` (ADR 0011): no `strings.Builder` or
  other no-copy type by value anywhere it reaches — item 5 states its buffer shape for that
  reason.
- Hard invariant: a Mechanism must never make any model perform worse than Bypass mode. Item 1
  is a live breach of it and lands first.
- Every item that ships a user-visible change adds a `CHANGELOG.md` `[Unreleased]` line under
  the heading named in the item (`### Fixed` at `:210`, `### Changed` at `:171`).

**Out of scope:**
- The Windows pair F-08 (winlabel journal replay) and C-20 (`tokenConfiner.Close` race) —
  parked in `ISSUES.md`; both need a Windows box to verify.
- The two §3.5 owner-call leads (ADR 0049 tier vs `rules.go:131`; the Tier-2 gate discarding
  the Confine box) — plan `2026-08-26 - 01` owns them.
- C-13 / C-03 / C-04 — design discussion first (`ISSUES.md`).
- A per-source skill budget (rejected by call 3); changing `maxSkills`.
- Moving IDNA normalisation behind a domain-level seam (F-09's alternative — the doc is fixed
  instead, item 8).
- A Go-modules Dependabot entry (item 9 ships a github-actions-only config); a major-version
  upgrade of any action (item 9 pins the CURRENT majors).
- An inter-chunk idle timeout on the stream (a local model's first token can take minutes on a
  large prompt; a default would break the primary persona — the caller's ctx is the deadline).
- The agent loop's own `strings.Builder` accumulation in `streamResponse` (a Builder is
  amortised; only the TUI's string concat is quadratic).

---

## 1. `syntax` Mechanism: `/` opens a regex literal in JS/TS, never an "unclosed string" — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the item's under-reported-unterminated-literal row (`a.js` `const r = /abc;`)
asserts `valid`, so it landed in `TestCheckSyntaxAcceptsValidCode` rather than in
`TestCheckSyntaxReportsEachBrokenShape`, which asserts `valid == false` for every row.
NOTES (2026-08-27): beside `regexOpeners` and its `regexOpenerKeyword` string the change adds one
package-level predicate, `isIdentifierRune` — the keyword clause needs to know where an identifier
token starts; the rule itself stays data, and the scan stays inside `checkBrackets`.

**What:** the bracket/string heuristic in `internal/mechanisms/syntaxengine.go` `checkBrackets`
(`:135-253`) learns JavaScript/TypeScript regex literals, so a quote or bracket inside one can
no longer be read as a string opener or a bracket. This is a Bypass-floor breach today
(`syntax.go:61-70` turns the false positive into `domain.ActionRetry` on a correct
`write_file`), so it lands before any bench arm enables the `syntax` Mechanism (catalogue id
`syntax`, `robustness.go:32`; the audit's `syntax_correction`).

- **The preceding-token rule (binding).** For `lang == "javascript" || lang == "typescript"`
  only, after the existing comment gate (`:184-194`) has ruled out `//` and `/*`, a `/` OPENS a
  regex literal when the last significant rune on the current line (the last rune consumed
  outside a string, comment or regex, ignoring spaces and tabs; `0` at line start) is one of
  `=` `(` `,` `[` `{` `:` `!` `&` `|` `?` `;` or there is none (line start), OR the last
  identifier token on the line, with only whitespace between it and the `/`, is exactly
  `return`. Any other predecessor — an identifier, a digit, `)`, `]`, a closing quote — leaves
  `/` as the division operator it is today (consumed, no state change). The rule is a
  package-level table (`regexOpeners` for the runes, the keyword string beside it), and the
  doc comment on it states the rule in this sentence's terms.
- **Inside the literal (binding).** The scan consumes runes until the closing unescaped `/`:
  `\` escapes the next rune; inside a `[ … ]` character class a `/` does not close the literal
  (and `\]` escapes the class closer); quotes, backticks, brackets and `//` inside the literal
  are inert. A regex literal never spans lines: reaching end of line while inside one simply
  ends it (no error — the checker under-reports rather than invents, per the file's own
  contract at `:18-22`). Trailing flags after the closing `/` are ordinary identifier runes and
  need no handling. `stripTrailingComment` (`:284-314`) is left as is: a `//` inside a
  trailing regex on the LAST line of a file is out of scope for truncation detection, which
  already tolerates the Ruby `//` case by language.
- `hasCStyleComments`'s comment (`:88-92`) gains one sentence naming the regex-literal class
  as the second false-positive family the gate must not create.
- `CHANGELOG.md` `[Unreleased]` `### Fixed`: one entry — the syntax Mechanism no longer
  retries correct JS/TS that holds a regex literal with a quote or bracket inside it.

Binding standards: the change is confined to `checkBrackets` and one table; no new
language detection, no change to Go/Python/Ruby paths (their negative rows must stay green
unchanged); the rule is data, not a second scanner.

**Files:** `internal/mechanisms/syntaxengine.go`, `internal/mechanisms/syntaxengine_test.go`,
`CHANGELOG.md`

**Tests:** `TestCheckSyntaxAcceptsValidCode` (`syntaxengine_test.go:14`) gains these rows, all
asserting `valid == true`: `app.js` `const re = /['"]/g;\n`; `esc.ts` `const out =
s.replace(/'/g, "\\\\'");\n`; `tick.js` `` const t = /`[^`]*`/.test(x);\n ``;
`ret.ts` `function blank(s: string) {\n  return /^\\s*$/.test(s);\n}\n`; `not.js`
`if (!/^#/.test(line)) {\n  go();\n}\n`; `list.ts` `const rules = [/a\\(/, /b[/]x/, /"/];\n`
(covers `,` `[` and a class holding `/`); `div.js` `const half = total / 2;\nconst r = (a) /
(b) / c;\nconst s = "x" / 1;\n` (division after an identifier, `)` and a closing quote stays
division). `TestCheckSyntaxReportsEachBrokenShape` (`:178`) gains two rows proving division
did not become a literal: `a.js` `const x = (a / 2;\n` → `unclosed parenthesis '('` line 1;
`a.js` `const s = "a / b;\n` → `unclosed string literal (opened with ")`. One row proving an
unterminated literal is under-reported, not invented: `a.js` `const r = /abc;\n` → `valid`.

**Acceptance:** `go build ./... && go test ./internal/mechanisms/`

**Commit:** `fix(mechanisms): syntax checker reads JS/TS regex literals instead of retrying correct code`

---

## 2. A colour-scheme switch keeps the width authority the terminal chose — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the item's line references had drifted — `m.th = newTheme(s)` is `settingsapply.go:322` (not `:313`), the construction-time `newTheme` is `model.go:502` (not `:476`), and `TestSettingsPaneAppliesAColorSchemeLive` is `settings_test.go:3267` (not `:2976`). Same sites, no approach change.
NOTES (2026-08-27): the new test is table-driven with `t.Run`/`t.Parallel` over the two cases the item names (mode report / no mode report) rather than two separate functions — the house Go testing standard, and the pattern `settings_test.go:1424` already uses. Verified it fails without the fix (`want 1, got 0` on the reported case) and passes with it.

**What:** `internal/tui/settingsapply.go` `applyColorScheme` (`:305-322`) carries the live
measure across the theme rebuild. The theme is the home of `measure` by design (`theme.go:125-130`,
ADR 0030), and `newTheme` (`:286`) is the ONE constructor of a theme, so the fix is at the one
site that rebuilds a theme on a live screen:

- `applyColorScheme`: read `measure := m.th.measure` before `m.th = newTheme(s)` and re-arm
  `m.th.measure = measure` immediately after — the same posture `foldModeReport`
  (`width.go:95-108`) takes when the painter moves. No new parameter on `newTheme`: the
  construction-time call at `model.go:476` correctly starts at the painter's `WcWidth`, and a
  second constructor shape for one caller would be a seam nobody else uses.
- `newTheme`'s comment at `theme.go:319-321` ("It moves only when the terminal tells the
  program the painter moved") gains the second mover: a live scheme switch rebuilds the theme
  and MUST carry the measure across (`applyColorScheme`), because the painter did not move.
- `CHANGELOG.md` `[Unreleased]` `### Fixed`: a live `ui.color-scheme` switch no longer resets
  the width measure to `WcWidth` on a Unicode-core terminal.

Binding standards: `widthAuthority` stays a value type (ADR 0011 — `TestWidthAuthoritySurvivesAValueCopy`
pins it); no field is added to the Model; the fix is two lines and a comment.

**Files:** `internal/tui/settingsapply.go`, `internal/tui/theme.go`,
`internal/tui/settings_test.go`, `CHANGELOG.md`

**Tests:** `settings_test.go` — `TestSettingsPaneKeepsTheWidthAuthorityAcrossASchemeSwitch`,
built on `settingsSchemeModel` exactly as `TestSettingsPaneAppliesAColorSchemeLive` (`:2976`)
is: first `step` the model through `tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value:
ansi.ModeSet}` (the shape `width_test.go:281` uses) and assert `m.th.measure.Method() ==
ansi.GraphemeWidth`; then open the sub-list, pick the second scheme, commit; assert the switched
model's `th.errorFg` moved (the theme WAS rebuilt) AND `th.measure.Method()` is still
`ansi.GraphemeWidth`. A second case with no mode report asserts the measure stays `WcWidth`
across the same switch (the carry-over does not invent a move).

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `fix(tui): a live colour-scheme switch carries the width authority across the theme rebuild`

---

## 3. `sessionMover.move` re-follows the Parallel agents cap on every arrival — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the item's test-fixture line references had drifted by the two landed items — the six `sessionMover{…}` literals are `wire_test.go:2443`, `:4885`, `:4971`, `keysource_test.go:77`, `:113` and `launcher_test.go:563` (not `:2432`, `:4874`, `:4960`, `:73`, `:109`). Same six sites, no approach change.
NOTES (2026-08-27): `launcherWiringFixture` returning the spy widens its tuple to five, so all 14 of its call sites in `wire_test.go` gained a `_` — mechanical, and the only edit outside the sites the item names.
NOTES (2026-08-27): `move`'s doc comment said "the three follow-ups"; it now reads "the follow-ups" and names the cap as the fourth, since the count was part of the sentence the change invalidates.

**What:** the fan-out cap follows the server on EVERY arrival (ADR 0039), and the shared move
is the one fold every arrival that is not a bind goes through — so the follow moves INTO it.

- `cmd/apogee/upstream.go` `sessionMover` (`:200-214`): add `caps *parallelAgentsCap` with a
  doc line in the struct's own register ("the session's Parallel agents cap; a move is an
  arrival, so it re-follows here, after the engine switch succeeded"). `move` (`:238-297`):
  after `m.live.followEntry(entry)` and before the return, `m.caps.follow(entry)` — after the
  switch SUCCEEDED, so a refused move leaves the cap where the session still is (the order
  `switchServer` already keeps). A profile load's entry (`launcher.go:695-697`) pins nothing,
  so `follow` resolves it to the serial floor 1 and the new server's first beat widens it
  (`observe`); that is the honest width for a server no entry describes, and `follow`'s own
  comment (`:416-423`) already says so. The field is REQUIRED (non-nil): the composition root
  wires it and every fixture supplies one; no nil guard.
- `cmd/apogee/wire_live.go:248`: `caps: w.caps` on the `sessionMover` literal (`w.caps` is
  built at `:184`, above it).
- `cmd/apogee/wire_verbs.go` `switchServer` (`:132-155`): DELETE the `w.caps.follow(entry)`
  at `:153` and its comment paragraph — the move now does it, and a second follow would be a
  second engine push per switch. `serverBinder.bind` (`wire_server.go:114`) is a bind, not a
  move, and keeps its own follow.
- Every hand-built `sessionMover{…}` literal gains `caps: newParallelAgentsCap(&parallelAgentsSpy{})`
  (the spy is `upstream_test.go:778`): `cmd/apogee/wire_test.go:2432` (`launcherWiringFixture`,
  which also RETURNS the spy so a load test can read it), `:4874`, `:4960`,
  `cmd/apogee/keysource_test.go:73`, `:109`, `cmd/apogee/launcher_test.go:563` (`restoreWiring`).
  These six are every non-production literal (`grep -n 'sessionMover{' cmd/apogee/*.go`).
- `CHANGELOG.md` `[Unreleased]` `### Fixed`: a `/load` onto another server no longer keeps the
  departed entry's `parallel-agents:` pin.

Binding standards: the follow lives in ONE place (`move`); `switchServer` loses its copy rather
than keeping two; the seam is the existing `*parallelAgentsCap`, not a new interface.

**Files:** `cmd/apogee/upstream.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_verbs.go`,
`cmd/apogee/wire_test.go`, `cmd/apogee/keysource_test.go`, `cmd/apogee/launcher_test.go`,
`cmd/apogee/upstream_test.go`, `CHANGELOG.md`

**Tests:** `wire_test.go` — `TestLoadProfileMoveReFollowsTheParallelAgentsCap`: fixture caps
followed onto `config.ServerEntry{Name: "rig", ParallelAgents: 3}` then `caps.observe(6)`;
`wiring.load("there", nil)` and commit `result.Move()`; assert the spy's last width is `1`
(pin dropped, observation forgotten) and that `caps.current()` reads `1`; then `caps.observe(4)`
and assert `4` (the new server's own beat widens it). `upstream_test.go` — a `move` onto an
entry pinning `parallel-agents: 2` installs `2` and a subsequent `observe(8)` still reads `2`
(the pin outranks). `TestApplySettingServersReResolvesTheParallelAgentsCap` (`:4811`) and every
`/server` switch test stay green with the follow moved (a switch test that asserted the number of
`SetParallelAgents` pushes asserts the same count — one per arrival).

**Acceptance:** `go build ./... && go test ./cmd/apogee/`

**Commit:** `fix(root): a launch-profile move re-follows the Parallel agents cap like every other arrival`

---

## 4. Provider: a streamed reply and a non-streamed body are byte-bounded; ctx is the deadline — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the stream cap's error text is built with `fmt.Sprintf` from
`maxReplyTextBytes >> 20` rather than hard-coding "8 MiB"; the emitted string is byte-identical to
the item's literal text but cannot drift if the constant changes.
NOTES (2026-08-27): two doc comments beyond the item's named `Stream` one were updated to match
the new behaviour — `Respond`'s (the body limit) and the `DeltaError` kind's (a reply past the
text cap is now a terminal fault). No behaviour outside the item.
NOTES (2026-08-27): `chunkedTextServer` writes its 64 KiB chunks in a loop bounded at
`maxReplyTextBytes/64KiB + 32` alongside the item's `r.Context().Done()` check, so a missed
disconnect fails the assertion instead of hanging the suite.

**What:** `internal/provider` bounds what one completion may hand the agent, in the constant
family `client.go:17-29` already keeps (`maxToolCallBytes`, `maxErrorBodyBytes`).

- `internal/provider/client.go` const block: add `maxReplyTextBytes = 8 << 20` — the cap on the
  content plus reasoning bytes one streamed completion may yield — with a comment deriving it:
  the engine's own reply ceiling is `maxOutputTokenCap` 32,768 tokens (`loop.go:1203`, ADR
  0046), at most 4 bytes per UTF-8 rune ⇒ ~128 KiB for a server that honours `max_tokens`; 8 MiB
  is sixty-fold that, past any honest reply, and small enough that a server ignoring the cap
  cannot exhaust the agent. Add `maxResponseBodyBytes = 16 << 20` — the cap on a NON-streamed
  200 body (`Respond`, `:267`), which must hold the same text plus a 1 MiB tool call plus
  metadata.
- `stream.go` `parseSSE` (`:140-225`): keep a running `textBytes` sum of
  `len(choice.Delta.Content) + len(choice.Delta.ReasoningContent)`; when the sum would exceed
  `maxReplyTextBytes`, yield `Delta{Kind: DeltaError, Err: "apogee: streamed reply exceeded the
  8 MiB text limit"}` (not `Retryable` — a re-stream would overflow again) and return, so the
  deferred body close and wire-capture flush run as on every other terminal path. The check
  sits BEFORE the content/thinking yields at `:200-205`, so the consumer never sees the byte
  that crossed the line. Tool-call bytes keep their own cap (`:249`) and are not summed here.
- `client.go` `Respond` (`:267`): `json.NewDecoder(io.LimitReader(resp.Body,
  maxResponseBodyBytes))`. A body cut at the limit fails the decode, which already surfaces as
  the existing decode error — no new error kind.
- Deadline: the request carries the caller's ctx end to end (`do` `:349` uses
  `http.NewRequestWithContext`; `attemptContext` `:340` derives from it), so a ctx deadline or
  cancel already ends `resp.Body.Read` inside the scanner loop and surfaces as the `read stream`
  DeltaError at `:214-217`. This item PINS that contract with a test (below) and states it in
  `Stream`'s doc comment (`:55-60`): "the caller's ctx is the stream's only deadline; a
  cancelled or expired ctx ends the body read and surfaces as a terminal DeltaError". No idle
  timeout is added (Out of scope).
- `CHANGELOG.md` `[Unreleased]` `### Fixed`: a streamed reply is capped at 8 MiB of text and a
  non-streamed reply body at 16 MiB.

Binding standards: constants live in the one const block; the cap is checked at the one seam
that yields text (`parseSSE`), not in the consumer; the error text names the limit.

**Files:** `internal/provider/client.go`, `internal/provider/stream.go`,
`internal/provider/stream_test.go`, `internal/provider/client_test.go`, `CHANGELOG.md`

**Tests:** `stream_test.go` — `TestStream_ReplyTextIsCapped`: an `sseServer` handler that
writes content chunks of 64 KiB in a loop until the client disconnects (check `r.Context().Done()`
between writes); `collectStream` ends with exactly one `DeltaError` naming the limit,
`Retryable == false`, no `DeltaDone`, and the summed content bytes delivered before it are
`<= maxReplyTextBytes`. `TestStream_ThinkingCountsTowardTheCap`: the same with
`reasoning_content` chunks. `TestStream_CtxCancelEndsTheBody`: a handler that sends one content
chunk, flushes, then blocks on a channel; the test cancels the ctx after the first
`DeltaContent` arrives and asserts a terminal `DeltaError` within 2 s and that the handler
observes `r.Context().Done()` (the body was closed). `client_test.go` —
`TestRespond_BodyIsCapped`: a 200 body of `maxResponseBodyBytes+1` bytes of valid-looking JSON
padding yields an error, not a hang or a reply.

**Acceptance:** `go build ./... && go test ./internal/provider/`

**Commit:** `fix(provider): cap streamed reply text and non-streamed bodies; pin ctx as the stream deadline`

---

## 5. TUI: the streaming buffer appends without re-copying the whole reply — ✅ DONE (2026-08-27)

NOTES (2026-08-27): `append` is copy-on-write over the chunk HEADER slice (a copy bounded by the number of chunks, never by their bytes) rather than a bare in-place `chunks[last] += s` — the plan's own `TestStreamBufSurvivesAValueCopy` requires it, since a value-copied Model shares the backing array (ADR 0011), exactly as `stash` already rebuilds `parked`.
NOTES (2026-08-27): `internal/tui/paintcache_test.go` needed no edit after all — its `coldRender` copies `pending` from one `transcript` into another, which compiles unchanged with the new type — so it is not in FILES though the plan listed it.
NOTES (2026-08-27): `internal/tui/doc.go` was added to the item's files: its ADR 0011 invariant asserted "the in-progress assistant buffer is a string, not a Builder", a claim this item's own change made stale; it now names `streamBuf` and its copy-on-write appends.
NOTES (2026-08-27): `takePending` gained `streamBuf.appendBuf` so a run's parked buffer and the live buffer join chunk-wise and render to a string exactly once, at that commit point; `closeRun` (the third commit point) calls `String()` once on the unparked buffer.

Depends on item 4 (the bound on how large the buffer can grow is the provider's; this item
removes the quadratic cost below that bound).

**What:** `internal/tui/transcript.go` `appendToken` (`:919-927`) grows `pending` with
`t.pending += stripEscapes(text)` — one whole-buffer copy per sink flush (every 30 ms,
`sink.go:62`) — and `park`/`unpark` (`:448-458`, `:477`) move the same whole string every time
concurrent siblings alternate through the slot. Replace the buffer's SHAPE, not the logic:

- New value type in `transcript.go`, `streamBuf`: `chunks []string` and `n int` (total bytes).
  Methods: `append(s string)` (if the last chunk is shorter than `streamChunkBytes = 16 << 10`,
  concatenate into it — a bounded copy — else start a new chunk), `Len() int`, `String()
  string` (`strings.Join`, one allocation, called only at a commit point), `tail(lines int)
  string` (walks chunks from the END, joining only as many as hold `lines+1` newlines — the
  preview's input), `empty() bool`. It holds a slice header and an int and NO pointer to
  itself, so the Model's value copy on every `Update` stays legal (ADR 0011; the existing
  `entries []entry` slice has the same shape). **Binding:** no `strings.Builder`, no `bytes.Buffer`,
  no `*streamBuf` anywhere in `transcript` or `parkedText`.
- `transcript.pending` becomes `streamBuf` and `parkedText.text` (`:86-89`) becomes
  `streamBuf`, so `park`/`stash`/`unpark` move slice headers instead of copying text.
  Readers: `park` `:452-456`, `takePending` `:497-501`, `:648`, `appendToken` `:922-926`,
  `discardPending` `:946`, `render.go:192` (`previewTail(t.pending)` → `previewTail(t.pending.tail(previewTailLines))`
  — `previewTail` itself is unchanged and still trims and cuts, now over a tail-sized string).
  `previewTail`'s comment at `render.go:182-185` is amended to say the buffer hands it a tail,
  so a repaint costs a viewport, not a reply — which is what that comment already promises.
- The field comment at `:27-29` is rewritten: a chunk list, copy-safe, appended in bounded
  copies; joined once at its commit point.
- Tests that read `pending` as a string (`transcript_test.go:421,1764,1791,2684`,
  `transcriptcodec_test.go:497`, `clearscreen_test.go:43`, `fold_test.go:308`,
  `paintcache_test.go:54`) read `pending.String()` / seed with a `streamBuf` built by a test
  helper `bufOf(s string) streamBuf`.
- No CHANGELOG line: nothing user-visible changes (the preview paints the same bytes).

Binding standards: the buffer is a value type owned by `transcript`; the renderer reads it
through `tail`, never through `chunks`; commit paths call `String()` exactly once each.

**Files:** `internal/tui/transcript.go`, `internal/tui/render.go`,
`internal/tui/transcript_test.go`, `internal/tui/transcriptcodec_test.go`,
`internal/tui/clearscreen_test.go`, `internal/tui/fold_test.go`,
`internal/tui/paintcache_test.go`

**Tests:** `transcript_test.go` — `TestStreamBufAppendsInBoundedChunks`: 1,000 appends of 100
bytes yield `Len() == 100_000`, `String()` equal to the concatenation, and `len(chunks) <=
100_000/streamChunkBytes + 1`; `TestStreamBufTailReturnsOnlyTheLastLines`: a buffer of 5,000
lines across many chunks — `tail(256)` equals the last 257 lines of `String()` and touches
fewer than all chunks (assert via a chunk count the test builds deliberately);
`TestStreamBufSurvivesAValueCopy`: copy a `transcript` by value, append on the copy, the
original's `String()` is unchanged (mirrors `TestWidthAuthoritySurvivesAValueCopy`). Existing
stream/park/reset tests (`:196`, `:217`, `:413`, `:1542-1791`) stay green with the accessor
change and prove the routing is untouched. `TestModelNoBuilderByValue` stays green.

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `refactor(tui): streaming buffer is a chunk list, appended in bounded copies and joined once at commit`

---

## 6. Opener allow-list drops `.odt`, `.ods`, `.odp` and `.epub` (ADR 0019, fifth amendment) — ✅ DONE (2026-08-27)

NOTES (2026-08-27): also updated `docs/manual/configuration.md` "Showing a finished document" (not in the item's Files list) — the manual promised a local desktop opens "documents, images and text", which this item makes untrue for the four extensions; added one sentence naming them beside the existing web-page carve-out.

**What:** the rung-1 rule is "formats whose default handler DISPLAYS the file rather than
executing it", and the shipped set disagrees with it again: the ODF trio is the LibreOffice
container that carries Basic macros with no macro-free variant (the exact class the third
amendment removed `.doc/.xls/.ppt` for), and `.epub` is a zip of XHTML the reader renders with
script enabled in several handlers (the class the fourth amendment removed `.html` for).

- `internal/present/opener.go` `openerRenderableExts` (`:297-339`): delete the `.epub` (`:320`),
  `.odt` (`:322`), `.ods` (`:324`) and `.odp` (`:326`) rows. The "Documents" comment at
  `:316-317` names the four as deliberately absent — ODF beside the pre-2007 trio under the
  macro rule, `.epub` beside `.html` under the active-content rule — and cites "ADR 0019, fifth
  amendment 2026-08-26". The rule paragraph at `:264-270` gains ODF in its list of
  macro-carrying containers.
- `docs/adr/0019-documents-are-presented-not-opened.md`: append `## Amendment (2026-08-26) —
  the allow-list refuses the ODF formats and `.epub`` after the fourth amendment (file ends at
  `:333`), MIRRORING the third and fourth amendments' shape exactly: a **Why now** paragraph
  (raised by the security audit of 2026-08-25, F-04, ratified by the owner 2026-08-26; the
  rule stands and the set moves, as in the third amendment), **(a)** the four extensions are
  out and degrade as any refused extension does — no argv, `ErrNoOpener`, rung 0, the tool
  result still `shown`, never an error — **(b)** what stays: `.docx/.xlsx/.pptx` (OOXML states
  its macro-freeness in the extension), `.pdf`, `.rtf`, the text and image sets, **(c)** nothing
  else moves — rung 2's crossing on `.pdf` is untouched (none of the four was ever in
  `browserRenderableExts`, `presenter.go:187-192`), rung 3 stays unbounded, the Windows name
  bound is untouched.
- `CHANGELOG.md` `[Unreleased]` `### Changed`: `present_document` on a local desktop no longer
  launches the OS handler for `.odt/.ods/.odp/.epub`; the path is still presented.

Binding standards: the set is the ONLY code change; no new rule machinery; ADR and code land in
one commit.

**Files:** `internal/present/opener.go`, `internal/present/opener_test.go`,
`docs/adr/0019-documents-are-presented-not-opened.md`, `CHANGELOG.md`

**Tests:** `TestOpenerRenderableAllowsDocumentsAndRefusesPrograms` (`opener_test.go:336`): move
`report.odt` and `book.epub` from `renderable` to `programs` and add `sheet.ods`, `deck.odp`,
`BOOK.EPUB` (case fold) beside them with the comment "ODF carries macros; EPUB is scripted
XHTML (ADR 0019, fifth amendment)"; `report.docx`, `sheet.xlsx`, `deck.pptx`, `report.pdf` stay
in `renderable`. `presenter_test.go:448-458`'s crossing test stays green unchanged.

**Acceptance:** `go build ./... && go test ./internal/present/ ./internal/tui/ && grep -c "Amendment (2026-08-26)" docs/adr/0019-documents-are-presented-not-opened.md`

**Commit:** `fix(present): opener allow-list refuses the ODF formats and .epub (ADR 0019, fifth amendment)`

---

## 7. Skill discovery walks the highest-priority source first; a collision keeps the first — ✅ DONE (2026-08-27)

NOTES (2026-08-26): `internal/tui/skills.go:130` and `internal/tui/skill_test.go:856` were listed as "no change"; both carried comments asserting the empty-catalog note lists dirs "in the order sourceDirs walks — increasing priority", which the inverted walk falsifies. Comment-only rewording, no logic or rendered output change (the note still ends with the global library).
NOTES (2026-08-26): `internal/skills/doc.go`'s Layering paragraph (`:22-25`) was not in the item's file list but stated "later sources override earlier … the user's global library is walked LAST" — the exact claim the item inverts. Rewritten to the new rule; `doc.go:26` unchanged as the item says.
NOTES (2026-08-26): `TestReadRootsRefuseARelocatedWorkspaceAnchor` and `TestReadRootsListADirThatDoesNotExist` hard-coded readRoots' output order and failed on the new anchor order. Expectations reordered only — the item's "order is immaterial to a read-root mount" holds, the same roots are mounted.
NOTES (2026-08-26): `assertShadowed` was split so the collision assertion is reusable from a scan that also records a cap skip: `assertShadowedAmong` holds the body, `assertShadowed` adds the clean-scan "exactly one skip" check and delegates. Existing callers keep their assertions unchanged.

**What:** ADR 0032 says the user's library outranks the workspace, but the cap (`load.go:192-201`)
is first-come and `Catalog.set` (`catalog.go:47-52`) is last-write-wins, with the home library
walked LAST — so a repo shipping `maxSkills` folders fills the catalog and the home library
never loads. Invert the walk and the collision rule together, so the cap and the priority agree:

- `internal/skills/load.go` `sourceAnchors` (`:103-115`): return the anchors in DECREASING
  priority — the home library (`trusted: true`) FIRST, then the workspace's bare `skills/`
  (when `UseProjectSkills`), then `.apogee/skills`. The comment at `:89-102` is rewritten: the
  walk visits sources highest-priority first so the global cap can never evict a
  higher-priority skill, and a collision keeps the FIRST copy — which is the same "home wins,
  bare `skills/` beats `.apogee/skills`" ordering ADR 0032 fixed, reached from the other end.
  `sourceDirs` (`:120-127`) follows unchanged, so `Provider.SourceDirs` and the `ExtraReadRoots`
  it feeds (`wire_boot.go:226`, `wire_firing.go:226`) list the same dirs, now highest-priority
  first — order is immaterial to a read-root mount.
- `internal/skills/catalog.go` `set` (`:47-52`): keep-first. On an existing id, record the
  NEWCOMER as shadowed — `c.addSkip(SkipError{Path: path, Err: ShadowedError{By: c.pathByID[s.ID]}})`
  — and return without touching `byID`/`pathByID`. Comment `:36-46` rewritten to state
  keep-first and why (the cap). The same-source case follows the same rule: the folder the
  walk reaches first (lexical order) is the live copy, the later one is recorded.
- `Load` (`:64-70`), `loadDir`'s cap message (`:195-199`) and `doc.go:26` need no logic change;
  the cap message already names the dir it stopped in, which under the new order is always a
  workspace dir when the home library fits.
- `internal/tui/skills.go`: no change — `shadowedBy`/`partitionSkips` (`:303-327`) read `Path`
  and `By` and never the walk order; the displaced report renders the same two paths.
- `docs/adr/0032-the-user-skill-library-outranks-the-workspace.md`: append `## Amendment
  (2026-08-26) — the walk runs highest-priority first and a collision keeps the first copy`
  (≤ 12 lines): why (F-06 — the global cap was first-come while priority was last-write, so a
  repo could fill the cap and take the ids), the new rule, and that the Context's description
  of `sourceDirs` as "increasing priority" describes the pre-amendment code. Precedence is
  unchanged; only the mechanism that enforces it moved.
- `CONTEXT.md` **Skill** (`:1025-1047`): no change — it states precedence, not walk order.
- `CHANGELOG.md` `[Unreleased]` `### Fixed`: a repository shipping thousands of skills can no
  longer crowd the user's own library out of the catalog or take over its ids.

Binding standards: one ordering in one function (`sourceAnchors`); one collision rule in one
method (`set`); no per-source budgets, no second cap.

**Files:** `internal/skills/load.go`, `internal/skills/catalog.go`,
`internal/skills/load_test.go`, `internal/skills/catalog_test.go`,
`docs/adr/0032-the-user-skill-library-outranks-the-workspace.md`, `CHANGELOG.md`

**Tests:** `load_test.go` — `TestLoadCapNeverEvictsTheHomeLibrary`: home holds `home-a` and
`home-b`; `.apogee/skills` holds `maxSkills` skills (`writeSkill` in a loop, ids `r0000…`, the
shape `TestLoadWalkWidthBounded` `:488` uses for 4,096 dirs); after `Load(Sources{Home, Workspace})`
both home skills resolve, `cat.Len() == maxSkills`, and exactly one skip names the "skill cap"
under the WORKSPACE dir. `TestLoadCapAndCollisionStillFavourHome`: the same plus a repo `dup`
and a home `dup` — `dup` resolves to the home body and the repo copy is recorded as shadowed
by the home path. `TestLoadHomeOverridesWorkspaceOnIDCollision` (`:53`) and
`TestLoadBareProjectSkillsStillBeatDotApogeeOnCollision` (`:74`) stay green with their
assertions unchanged (`assertShadowed(loser, winner)` — the winner is now the first-walked
file). `TestLoadRecordsCollisionWithinOneSourceDir` (`:92`): its expectation flips to the
lexically FIRST folder being live and the later one recorded; its comment updated.
`catalog_test.go` — `set` twice with one id keeps the first Skill and records the second with
`ShadowedError{By: <first path>}`.

**Acceptance:** `go build ./... && go test ./internal/skills/ ./internal/tui/ && grep -c "Amendment (2026-08-26)" docs/adr/0032-the-user-skill-library-outranks-the-workspace.md`

**Commit:** `fix(skills): walk the highest-priority source first and keep the first copy on a collision`

---

## 8. `internal/security` package doc names its one third-party import — ✅ DONE (2026-08-27)

NOTES (2026-08-27): none — the item warrants no CHANGELOG entry (a Go doc comment; nothing user-visible), so that heading is omitted.

**What:** `internal/security/doc.go:55-56` says the package "imports only internal/domain and
the standard library (ADR 0010)"; `urlsafety.go:12` imports `golang.org/x/net/idna` and uses
it at `:259` to mirror `net/http`'s own host mapping (the comment at `:190` explains why). The
doc is fixed, not the import (binding — the alternative, moving IDNA behind a domain seam, was
weighed and is out of scope: the mapping exists precisely to match what `net/http` will do, so
it belongs beside the dial-time check).

- Rewrite the sentence: the package imports `internal/domain`, the standard library and exactly
  one third-party module, `golang.org/x/net/idna` (the IDNA mapping `urlsafety.go`'s
  `NormalizeURL` applies so its verdict matches the host `net/http` will actually dial); ADR
  0010's direction rule is untouched — it is imported BY `internal/tools` and `internal/agent`,
  never the other way.
- No code change; `TestDocMapNamesEveryFile` (`docmap_test.go`) stays green (no file is added).
- No CHANGELOG line (a Go doc comment; nothing user-visible). Not a docs-only commit in the
  make-check sense — it is a `.go` file — but the Acceptance is vet plus grep.

**Files:** `internal/security/doc.go`

**Tests:** none added; `go vet` proves the comment still parses as package doc.

**Acceptance:** `go vet ./internal/security/ && grep -n "golang.org/x/net/idna" internal/security/doc.go && ! grep -n "imports only internal/domain and the standard" internal/security/doc.go`

**Commit:** `docs(security): package doc names the golang.org/x/net/idna import`

---

## 9. Every GitHub Action is pinned to a full commit SHA

**What:** the six `uses:` lines across `.github/workflows/ci.yml` (`:20-21`, `:54-55`) and
`.github/workflows/tag-on-version-bump.yml` (`:24`, `:35`) reference mutable major tags
(`actions/checkout@v4`, `actions/setup-go@v5`, `actions/github-script@v7`). Pin each to the
40-character commit SHA of the CURRENT major's latest patch release, with the tag in a trailing
comment — the form `uses: actions/checkout@<sha> # v4.x.y`.

- **Resolving the SHAs (binding procedure).** For each of the three actions, stay on its
  current major (no `v5` checkout, no `v8` github-script — an upgrade is a separate decision):
  `tag="$(gh api repos/actions/checkout/tags --paginate --jq '.[].name' | grep -E '^v4\.[0-9]+\.[0-9]+$' | sort -V | tail -1)"`,
  then `gh api "repos/actions/checkout/commits/$tag" --jq .sha` (the commits endpoint
  dereferences an annotated tag to its commit). Same for `actions/setup-go` (`^v5\.`) and
  `actions/github-script` (`^v7\.`). Every SHA must be 40 hex characters; the implementer
  pastes the resolved value and the resolved tag into the comment.
- **If `gh api` cannot reach github.com** (no network, no auth), the item STOPS with
  `BLOCKED: cannot resolve action SHAs offline` and changes NOTHING. A placeholder, a
  `TODO-SHA` marker, a SHA typed from memory, or a partial pin of one file is NOT acceptable
  — a wrong SHA breaks every CI run and a placeholder is a mutable tag under another name.
- Add a two-line comment at the top of each workflow's `steps:` (or beside the first `uses:`)
  stating the pin rule: actions are pinned by commit SHA with the release tag beside it; bump
  by resolving the new tag's commit, never by editing the tag alone.
- **Dependabot keeps the pins current (ratified call 6).** Add `.github/dependabot.yml` with
  exactly this content and nothing more: `version: 2`; one `updates:` entry —
  `package-ecosystem: github-actions`, `directory: "/"`, `schedule: { interval: weekly }`. NO
  Go-modules entry (keeps the noise down). A dependabot PR bumps the SHA AND the trailing
  version comment together, which is what keeps a SHA pin from rotting into a stale one; it
  stays on the pinned major unless a human accepts a major bump, so "majors stay current, not
  upgraded" holds by the same rule the comment states. A three-line comment at the top of the
  file says this.
- `docs/manual/building.md:46-47` names the tag workflow by path; no text change needed.
- `CHANGELOG.md` `[Unreleased]` `### Changed`: CI actions are pinned to commit SHAs, and a
  github-actions-only Dependabot config keeps the pins current.

Binding standards: all three files change in one commit; the workflows' logic is untouched
(same steps, same `with:` blocks, same permissions); the dependabot file carries one
ecosystem only.

**Files:** `.github/workflows/ci.yml`, `.github/workflows/tag-on-version-bump.yml`,
`.github/dependabot.yml`, `CHANGELOG.md`

**Tests:** none in Go. The Acceptance greps prove every `uses:` carries a 40-hex SHA and a
version comment, that no bare major tag remains, and that the dependabot file names exactly
one ecosystem, `github-actions`, weekly. CI/docs-only commit, no `make check`.

**Acceptance:** `test "$(grep -hE '^\s*(- )?uses: ' .github/workflows/*.yml | wc -l)" -eq "$(grep -hE '^\s*(- )?uses: [^@]+@[0-9a-f]{40} # v[0-9]+\.[0-9]+\.[0-9]+$' .github/workflows/*.yml | wc -l)" && ! grep -nE 'uses: [^@]+@v[0-9]+\s*$' .github/workflows/*.yml && test "$(grep -c 'package-ecosystem:' .github/dependabot.yml)" -eq 1 && grep -n 'package-ecosystem: github-actions' .github/dependabot.yml && grep -n 'interval: weekly' .github/dependabot.yml && ! grep -n 'gomod' .github/dependabot.yml`

**Commit:** `ci: pin every action to a commit SHA; dependabot keeps the github-actions pins current`

---

## 10. The tag job persists no credential; the tag step alone receives the token

Depends on item 9 (same file; the SHA-pinned `uses:` lines are the base this edits).

**What:** `.github/workflows/tag-on-version-bump.yml` checks out with the default
`persist-credentials: true`, which writes the job's `contents: write` `GITHUB_TOKEN` into
`.git/config` as an `extraheader` — and the next step runs a repo-authored script
(`.github/scripts/version-bump.sh`) with that credential in reach. The tags are created through
the REST API by the `github-script` step, so nothing in the job needs the git credential.

- `tag-on-version-bump.yml` checkout step (`:24-29`): add `persist-credentials: false` under
  `with:` beside `fetch-depth: 0`, with a one-line comment: the script only READS history; the
  token is handed to the API step below and to nothing else.
- The `github-script` step (`:35-55`): pass the token explicitly —
  `github-token: ${{ secrets.GITHUB_TOKEN }}` under `with:` — so the one step that writes is
  the one step that visibly holds the credential (the action's default is the same token; the
  explicit line is what makes the scope legible and survives a future default change). The job
  keeps `permissions: contents: write` (permissions are job-scoped in GitHub Actions; there is
  no per-step form).
- `ci.yml` checkout steps (`:20`, `:54`): `persist-credentials: false` too — the job is
  `contents: read`, but a read token in `.git/config` is still a token in a tree `go test`
  walks. Its steps run only Go tooling and need no git credential.
- `.github/scripts/version-bump.sh`: no change — it runs `git log`/`git show`/`git rev-parse`
  against the local clone only.
- `CHANGELOG.md` `[Unreleased]` `### Changed`: the tag workflow no longer persists its push
  credential into the checkout.

**Files:** `.github/workflows/tag-on-version-bump.yml`, `.github/workflows/ci.yml`, `CHANGELOG.md`

**Tests:** none in Go; the Acceptance greps pin both edits. CI/docs-only commit, no `make check`.

**Acceptance:** `test "$(grep -c 'persist-credentials: false' .github/workflows/ci.yml)" -eq 2 && test "$(grep -c 'persist-credentials: false' .github/workflows/tag-on-version-bump.yml)" -eq 1 && grep -n 'github-token: \${{ secrets.GITHUB_TOKEN }}' .github/workflows/tag-on-version-bump.yml && bash -n .github/scripts/version-bump.sh`

**Commit:** `ci: tag job persists no credential; the API step alone receives the token`

---

**Suggested version bump (not performed):** patch — `0.17.4`. Every item is a fix or a hardening
of existing behaviour; the two user-visible changes (the opener refusing four more extensions,
the skill walk order) narrow or reorder without adding a key, an event or a surface. The bump is
the owner's call, after the run.
