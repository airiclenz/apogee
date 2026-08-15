# Residual sweep + response-reserve config key — implementation plan

**Goal:** clear every actionable open defect from `ISSUES.md`'s 2026-08-14/15 run-residual
sections, and ship the parked `[P2]` context-budget fraction as a `response-reserve:` config
key (top-level + per-`servers:`-entry, fraction 0.0–1.0).

**Date:** 2026-08-15 · **Status:** not started · **sized for:** ~200k-context host

**Authoritative sources:** `ISSUES.md` (the entries each item closes, cited per item);
[ADR 0040](../adr/0040-color-schemes-are-embedded-roles-with-user-shadowing.md) (amendment
format and the "additive role" rule); [ADR 0031](../adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
(Driver sufficiency, item 10); [ADR 0046](../adr/0046-the-engine-bounds-every-reply-with-an-output-cap.md)
(the output-cap derivation the reserve feeds); `internal/config/config.go:1150-1163` (the
"reply ceiling is a property of the SLOT" rule the per-server half follows).

**Ratified design calls:**

- Owner, 2026-08-15: `toolshape_test.go` / `blocktarget_test.go` keep their subject names as a
  deliberate, documented exception to the `{source}_test.go` rule (each spans 4–5 sources; all
  plausible 1:1 targets are taken). A header comment records the exception (item 6).
- Owner, 2026-08-15: the response-reserve key lives **top-level AND per-`servers:`-entry**,
  mirroring `context-window:` / `max-output-tokens:` exactly, and takes a **fraction 0.0–1.0**
  (e.g. `response-reserve: 0.2`), matching the internal `defaultReserveFraction` it replaces.
- Plan author, 2026-08-15: ADR 0040 gets a **new dated amendment heading** for the four
  unrecorded roles (appending to the 2026-08-08 amendment would falsify its date).
- Plan author, 2026-08-15: the Event-count prose in `internal/tui/doc.go` and
  `fold_test.go` becomes **count-free** so it cannot go stale on the next Event addition.
- Plan author, 2026-08-15: reserve precedence in `Allocate` is **explicit tokens
  (`ResponseReserve` > 0) → configured fraction (0 < f < 1) → built-in 0.20**; the config
  layer validates the fraction's range, and `Allocate` treats an out-of-range fraction as
  unset (defensive, never a panic).

**Standing requirements:**

- skills: coding-standards
- House convention, every item: the implementer REMOVES the item's `ISSUES.md` entry (cited
  in its **What**) and puts the close's CHANGELOG entry text (under `[Unreleased]`) in the
  sidecar — `ISSUES.md` holds open work only; the changelog is the sole closed trail. When a
  removal empties one of the "Run residuals — open" sections, remove that section heading too.
- No version identifier changes (see the closing note — the bump is suggested, not performed).

**Out of scope:**

- The Windows job-object escape test (needs a Windows host — stays in `ISSUES.md`).
- The test-file size carve (`model_test.go` 4916 / `settings_test.go` 3217 / `mouse_test.go`
  3168) — its own future plan; the ISSUES entry stays.
- `CHANGELOG.md:2493`'s historical "28 semantic roles" — a dated record, left alone.
- Any Mechanism/bench work; any sampling-params work (still demand-driven-parked).

---

## 1. ADR 0040 amendment for the four unrecorded roles — ✅ DONE (2026-08-15)

NOTES (2026-08-15): the amendment cites both shipped hexes (dark / light) per role plus the retune
sha where the dark value moved after landing, extending the `tool-header` entry's dark-only form —
`tool-leader` and `tool-marker-bright` were both retuned after landing, so the landing sha alone
would have named a hex that no longer ships.

**What:** Append a new `## Amendment (2026-08-15) — …` section to
`docs/adr/0040-color-schemes-are-embedded-roles-with-user-shadowing.md`, in the house
amendment form (dated H2, lead paragraph, bullet list). One bullet per role, in the exact
form the existing `tool-header` entry (`:216-219`) uses — bold backticked key, what it is /
what it split off from, its first consumer, the shipped hex value(s) in backticks with the
landing commit sha in parentheses — for the four roles that landed after `tool-header`:
`success` (`internal/scheme/scheme.go:42`), `warning` (`:49`), `tool-marker-bright` (`:73`),
`tool-leader` (`:78`). Recover each role's hex and landing sha with `git log --oneline -S`
over `internal/scheme/`. Also refresh the "25th key" ordinal references at `:216` and `:222`
so the record stays coherent with 29 keys. Remove the ADR-0040 bullet from `ISSUES.md`'s
"Run residuals — open (2026-08-15, stall-guard run)" section.

**Files:** `docs/adr/0040-color-schemes-are-embedded-roles-with-user-shadowing.md`,
`ISSUES.md`, `CHANGELOG.md`

**Tests:** none (docs only).

**Acceptance:**
- `grep -c 'tool-leader\|tool-marker-bright' "docs/adr/0040-color-schemes-are-embedded-roles-with-user-shadowing.md"` ≥ 2
- `grep -n 'Amendment (2026-08-15)' "docs/adr/0040-color-schemes-are-embedded-roles-with-user-shadowing.md"` hits
- the ISSUES.md bullet is gone: `grep -c 'have no entries of their own' ISSUES.md` → 0

**Commit:** `docs(adr): record the four post-tool-header roles in ADR 0040's amendment trail`

---

## 2. Pin the scheme role count to the struct — ✅ DONE (2026-08-15)

**What:** In `internal/scheme/scheme_test.go`, extend `TestRoleTableCoversEveryRole` (`:63`,
which already compares `len(roleTable)` to `len(roleKeys)` but pins no absolute number) with
an assertion `len(roleKeys) == 29`, whose failure message names the three prose sites to
update on drift: `README.md:187`, `layout.md:94`, and `newTheme`'s comment at
`internal/tui/theme.go:267`. `roleKeys` (`internal/scheme/scheme.go:92`) is built by
reflection off `Scheme`'s `yaml:` tags, so this one assertion catches the next silent drift
between struct and prose. Remove the role-count bullet from `ISSUES.md`'s stall-guard
residuals section.

**Files:** `internal/scheme/scheme_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** the extended assertion itself.

**Acceptance:** `go test ./internal/scheme/` passes; `grep -n '29' internal/scheme/scheme_test.go` hits.

**Commit:** `test(scheme): pin the 29-role count so prose drift fails the suite`

---

## 3. Fix `Warning`'s stale doc comment — ✅ DONE (2026-08-15)

**What:** Rewrite the doc comment above `Scheme.Warning`
(`internal/scheme/scheme.go:43-48`): the sentence naming its first consumer "the status
line's quiet-time suffix" describes a form that never shipped — the stall guard renders as a
`quiet` qualifier before the single activity clock. Replace that clause with the shipped
form (e.g. "the status line's quiet qualifier — rendered before the activity clock — …");
keep the rest of the comment (the muted/error rung framing, "named for the meaning") intact.
Do not touch the separate `type Warning struct` at `:118`. Remove `ISSUES.md`'s
"Run residuals — open (2026-08-15, quiet-qualifier single-clock run)" section (this is its
only bullet).

**Files:** `internal/scheme/scheme.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** none (comment only).

**Acceptance:** `go build ./internal/scheme/`; `grep -c 'quiet-time suffix' internal/scheme/scheme.go` → 0.

**Commit:** `docs(scheme): describe Warning's consumer as the quiet qualifier it shipped as`

---

## 4. Count-free Event-count prose — ✅ DONE (2026-08-15)

**What:** The sentence at `internal/tui/doc.go:527` reads "a twelfth Event has to be
answered for" — stale: `internal/domain/events.go` declares 12 Event types and `foldCases()`
(`internal/tui/fold_test.go:54`) carries 13 rows (TokenEvent twice, depth 0 and 1). Rewrite
it count-free ("a new Event variant has to be answered for — including with 'deliberately
nothing'"), and do the same for the test comment at `internal/tui/fold_test.go:53` ("the
assertion that there is no twelfth" → count-free). No numeral survives in either sentence.
Remove the doc.go bullet from `ISSUES.md`'s stall-guard residuals section.

**Files:** `internal/tui/doc.go`, `internal/tui/fold_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** existing `TestFoldEventCoversEveryEventVariant` still passes (comment-only change).

**Acceptance:** `grep -rc 'twelfth' internal/tui/` → 0; `go test ./internal/tui/ -run TestFoldEvent` passes.

**Commit:** `docs(tui): make the fold-coverage prose count-free so it cannot go stale`

---

## 5. README busy-command prose catches up with the table — ✅ DONE (2026-08-15)

NOTES (2026-08-15): per the dispatch DECISION the README prose edit already present in the working
tree from the batched attempt was kept as-is (it satisfies the item's Acceptance); this pass
restored `ISSUES.md` from HEAD and removed only this item's bullet, leaving item 6's bullet for its
own dispatch.

**What:** The prose sentence at `README.md:243-246` lists the commands that answer while the
model works as `/version`, `/skills`, `/usage` and `/confine`'s status report — but the
table below marks `/effort` (`:260`), `/schedule` (`:261`) and `/schedule-stop` (`:262`) ✅
too. Extend the prose sentence to name all seven commands. Leave the `/<skill-id>` and
`@<path>` rows out of that sentence — they are a different class (they ride the queued
message rather than answering immediately), and the table already says so. Remove the
README bullet from `ISSUES.md`'s thinking-effort-knob residuals section.

**Files:** `README.md`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** none (docs only).

**Acceptance:** the prose sentence (README.md ~:246) now contains `/effort`, `/schedule` and
`/schedule-stop`: `grep -n 'schedule-stop' README.md` hits inside the prose paragraph, not
only the table.

**Commit:** `docs(readme): list every busy-safe command in the menu prose, not just four`

---

## 6. Record the subject-named test files as a deliberate exception — ✅ DONE (2026-08-15)

NOTES (2026-08-15): per the dispatch DECISION the two header comments already in the working tree
from the batched attempt were reviewed and kept — each records the exception, cites the 2026-08-15
ratification and names the item's sources verbatim. One wording deviation stands:
`blocktarget_test.go`'s why-clause says no single source can lend the suite its name rather than the
item's "every 1:1 name is taken", because `blockstate_test.go` does not exist — the item's literal
clause would have been false there. `toolshape_test.go` carries the taken-names clause, which is
true for all four of its sources.

**What:** Per the ratified call (owner, 2026-08-15): `internal/tui/toolshape_test.go` and
`internal/tui/blocktarget_test.go` keep their subject names. Add a short header comment atop
each file recording the exception to the coding-standards `{source}_test.go` rule and naming
the sources the suite spans — for `toolshape_test.go`: `toolblock.go`, `toolbranch.go`,
`render.go`, `toolpresent.go`; for `blocktarget_test.go`: `render.go`, `blockstate.go`,
`transcript.go`, `mouse.go`, `toolbranch.go` — with one line of why (every 1:1 name is taken
by an existing test file; the suite's subject is cross-file behaviour, ratified 2026-08-15).
Remove the rename bullet from `ISSUES.md`'s "Test-file layout residuals" section (the
size-debt bullet in that section STAYS — it is out of this plan's scope).

**Files:** `internal/tui/toolshape_test.go`, `internal/tui/blocktarget_test.go`,
`ISSUES.md`, `CHANGELOG.md`

**Tests:** the files still compile and pass: comment-only change.

**Acceptance:** `go test ./internal/tui/ -run 'TestEveryToolShape|TestBlockMarksAgree' ` passes;
both files open with a header comment naming their sources.

**Commit:** `docs(tui): record the subject-named test files as a ratified naming exception`

---

## 7. Restamp the quiet clock only for watched kinds — ✅ DONE (2026-08-15)

NOTES (2026-08-15): two prose sites outside the item's Files list were corrected in the same pass
because this change made them inaccurate — `internal/tui/doc.go`'s stall-guard paragraph and the
`Model.lastEvent` field comment (`internal/tui/model.go:240`) both said an activity MOVE stamps the
clock, unqualified; each now names the watched-kind gate. Comment-only, no behaviour.

**What:** `moveActivity` (`internal/tui/activity.go:225`) calls `m.noteEngineHeard()`
unconditionally — every activity kind restamps the quiet clock, `actCompacting`/`actStopping`
(`:41-42`) included — while `quiet` (`:147`) reports only for `actThinking`/`actResponding`
(`:151`). The two seats are coupled by hand, so a future watched kind would inherit the
restamp silently. Fix: extract ONE shared predicate for "kind the quiet guard watches"
(e.g. an unexported method on the kind or a small helper in `activity.go`), use it both in
`quiet`'s gate (`:151`) and to guard the `noteEngineHeard()` call in `moveActivity`
(`:231`) — the restamp fires only for watched kinds. The per-Event stamp at
`internal/tui/model.go:680` is untouched (it is the primary seat). Add a test pinning that a
move to `actCompacting` or `actStopping` does NOT restamp `lastEvent`, while a move to
`actThinking`/`actResponding` does. Remove the `moveActivity` bullet from `ISSUES.md`'s
stall-guard residuals section.

**Files:** `internal/tui/activity.go`, `internal/tui/activity_test.go`, `ISSUES.md`,
`CHANGELOG.md`

**Tests:** the new restamp test; existing `TestActivityQuiet` (`activity_test.go:151`) and
`TestStatusLineQuietSuffix` (`model_test.go:3730`) still pass.

**Acceptance:** `go test ./internal/tui/ -run 'TestActivity|TestStatusLineQuiet'` passes.

**Commit:** `fix(tui): share one watched-kind predicate between quiet and the restamp`

---

## 8. Narrow the title drop-the-flag fallback to the kwarg it sets — ✅ DONE (2026-08-15)

NOTES (2026-08-15): the guard and its comment moved out of `generate` into a new unexported
`respondDroppingThinkingOff(ctx, client, req)` in the same file — `generate` builds its request via
`title.Prompt`, which hardcodes `EffortOff`, so there was no seam through which a LEVEL-carrying
request could reach the guard and the item's required test could not otherwise be written. The
extraction is behaviour-identical, keeps the comment's substance (extended with the narrowing's
rationale), and the new table-driven test drives the real POST path against `scriptedTitleServer`
rather than a predicate.
NOTES (2026-08-15): the new test is named `TestTitleGeneratorDropsOnlyTheThinkingOffKwarg` (rather
than a `TestTitleFallback…` form) so the item's Acceptance filter `-run TestTitleGenerator` actually
picks it up, matching the neighbouring fallback tests' naming family.

**What:** The guard at `cmd/apogee/title.go:90` fires for ANY non-empty
`req.ThinkingEffort`, so a future namer carrying a level would silently have it stripped on
any 4xx. Narrow it to `req.ThinkingEffort == provider.EffortOff` — the `enable_thinking:false`
kwarg this call actually sets (`internal/title/title.go:166` is the only setter today, so
this is behaviour-preserving). Adjust the comment (`:91-98`) to say the one re-send exists
specifically for the thinking-off kwarg. Add a test beside
`TestTitleGeneratorFallsBackWhenTheThinkingKwargIsRejected` (`cmd/apogee/title_test.go:295`)
pinning that a request carrying a LEVEL (e.g. `provider.EffortLow`) is NOT re-sent stripped
on a 4xx. Remove the `title.go` bullet from `ISSUES.md`'s thinking-effort-knob residuals
section.

**Files:** `cmd/apogee/title.go`, `cmd/apogee/title_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** the new level-preserving test; the two existing fallback tests (`:295`, `:329`)
still pass.

**Acceptance:** `go test ./cmd/apogee/ -run TestTitleGenerator` passes.

**Commit:** `fix(title): drop the thinking flag on 4xx only when it was the off-kwarg`

---

## 9. Carry the template hint on the in-band error path

**What:** `statusError` (`internal/provider/client.go:359-370`) appends `thinkingEffortHint`
(`:349`) when the request carried `chat_template_kwargs`, but the in-band path — a server
that wraps its error in an HTTP 200 — does not: `inBandError` (`:376-383`, called at `:214`)
and its streaming twin `inBandErrorDelta` (`internal/provider/stream.go:116`, called at
`:167`) produce the bare failure. Give both the same `hasTemplateKwargs bool` parameter and
pass `len(wire.ChatTemplateKwargs) > 0` at each call site (`wire` is in scope at both);
append the hint on the non-overflow `*StatusError` branch exactly as `statusError:367-369`
does — via the wrapping error, never inside `StatusError.Body`
(`client_test.go:308` pins that). Extend `TestRespond_InBandErrorSurfaces`
(`client_test.go:121`) and the streaming in-band test with a kwargs-carrying case asserting
the hint appears, and keep/add a no-kwargs case asserting it does not. Remove the
`inBandError` bullet from `ISSUES.md`'s thinking-effort-knob residuals section.

**Files:** `internal/provider/client.go`, `internal/provider/stream.go`,
`internal/provider/client_test.go`, `internal/provider/stream_test.go`, `ISSUES.md`,
`CHANGELOG.md`

**Tests:** the extended in-band tables (hint present with kwargs, absent without, absent on
overflow); existing non-2xx hint tests (`client_test.go:302`, `stream_test.go:209`) untouched.

**Acceptance:** `go test ./internal/provider/` passes.

**Commit:** `fix(provider): append the template hint on in-band errors too`

---

## 10. Root alias for ThinkingEffort

**What:** `apogee.go` re-exports `ThinkingProfile`/`ThinkingStyle` with their constants
(`:118-128`) but not `domain.ThinkingEffort`, so an out-of-module Driver must pass untyped
strings to `Agent.SetEffortOverride` (`internal/agent/agent.go:543`) — an ADR 0031
Driver-sufficiency gap. Add, in the same pattern and adjacent to the thinking aliases:
`type ThinkingEffort = domain.ThinkingEffort` with a one-line doc comment, and a `const`
block re-exporting `EffortOff`, `EffortLow`, `EffortMedium`, `EffortHigh`
(`internal/domain/config.go:330-341`). Pin the surface in `example_test.go`: add
`_ apogee.ThinkingEffort` to the alias block (`:23-36`) and the four `Effort*` consts to the
re-exported-const block (`:117-130`). Remove the root-alias bullet from `ISSUES.md`'s
thinking-effort-knob residuals section.

**Files:** `apogee.go`, `example_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** `example_test.go` compile-pins the new surface.

**Acceptance:** `go build ./... && go test . ` passes.

**Commit:** `feat(api): re-export ThinkingEffort and its levels at the root`

---

## 11. Response-reserve fraction — engine half

**What:** Give the engine a configurable reserve fraction with the ratified precedence.
Add `ResponseReserveFraction float64` to `domain.ContextConfig`
(`internal/domain/config.go:224-238`, beside `ResponseReserve int` at `:226`), doc comment
stating the precedence: explicit `ResponseReserve` tokens win; else a fraction in (0,1) of
the window; else the built-in default. In `internal/context/budget.go`, extend `Allocate`
(`:66-85`) to take the fraction (signature change, e.g.
`Allocate(window, reserve int, fraction float64)`): `reserve > 0` behaves exactly as today;
else `0 < fraction < 1` reserves `int(float64(window)*fraction)`; else
`defaultReserveFraction` (`:40`). An out-of-range fraction (≤0, ≥1) is treated as unset —
never a panic (config validates the range in item 12; this is the defensive floor). Keep the
`reserve >= window` clamp. Update the sole engine caller `internal/agent/loop.go:1019` to
pass `a.cfg.Context.ResponseReserveFraction`. `apogee.ContextConfig` is already a root alias
(`apogee.go:93`) — no root change. No ISSUES.md change in this item (the entry closes in
item 13).

**Files:** `internal/domain/config.go`, `internal/context/budget.go`,
`internal/context/budget_test.go`, `internal/agent/loop.go`

**Tests:** table-driven `Allocate` cases: explicit reserve wins over fraction; fraction
applied when reserve is 0; fraction 0 / negative / ≥1 → built-in default; clamp unchanged.

**Acceptance:** `go build ./... && go test ./internal/context/ ./internal/agent/` passes.

**Commit:** `feat(context): configurable response-reserve fraction with explicit-tokens precedence`

---

## 12. `response-reserve:` — top-level config key

Depends on item 11.

**What:** Surface the fraction as a top-level `response-reserve:` key, mirroring
`context-window:` (`internal/config/config.go:953`) at every step:
- struct field `ResponseReserve float64 \`yaml:"response-reserve"\`` beside `ContextWindow`,
  with a doc comment in the house style (what it is, 0 = unset → built-in 0.20);
- validation mirroring `:1323-1330`: reject `< 0` and `>= 1` with a plain-language error
  naming the key and the accepted range (0 exclusive to 1 exclusive; 0 meaning unset);
- options flattening: a field in `internal/config/options.go` beside `ContextWindow` (`:37`),
  flattened where `:2149`/`:2156` flatten the analogues;
- plumb into the engine at BOTH boot sites: `cmd/apogee/wire_boot.go:223-227` and the
  headless mirror `cmd/apogee/headless.go:392-399`, setting
  `ContextConfig.ResponseReserveFraction`;
- document the key in the embedded template `internal/config/defaults/config.yaml` with a
  comment block in the same voice as `context-window:` (`:446-450`) — placed beside it, with
  the fraction semantics and the 0.20 default spelled out. The template is seeded first-run
  and never overwritten, so the comment is the key's primary discovery surface.

No ISSUES.md change in this item.

**Files:** `internal/config/config.go`, `internal/config/options.go`,
`internal/config/config_test.go`, `internal/config/defaults/config.yaml`,
`cmd/apogee/wire_boot.go`, `cmd/apogee/headless.go`

**Tests:** config parse + validation cases (valid 0.2; rejected -0.1, 1.0, 1.5; absent →
zero value); options-flattening case.

**Acceptance:** `go build ./... && go test ./internal/config/ ./cmd/apogee/` passes.

**Commit:** `feat(config): top-level response-reserve key for the context-budget fraction`

---

## 13. `response-reserve:` — per-server override riding every rebind

Depends on item 12.

**What:** Mirror `max-output-tokens:`'s per-entry override for the reserve fraction:
- `ServerEntry` field `ResponseReserve float64 \`yaml:"response-reserve,omitempty"\`` beside
  the pair at `internal/config/config.go:1188-1189`, with the entry-doc block (`:1108-1163`)
  gaining its row;
- a resolver mirroring `config.ResolveContextWindow` (used at
  `cmd/apogee/wire_server.go:110`, `:118`): entry value wins over top-level, 0 = fall
  through;
- re-state the fraction at EVERY rebind site the analogues ride: the `/server` switch
  (`cmd/apogee/wire_server.go:110-118` area), scheduled firings
  (`cmd/apogee/schedule.go:102`, `:112-113`), delegation (`cmd/apogee/delegation.go:445`),
  and the upstream spec (`cmd/apogee/upstream.go:255-256`) — enumerated here so none is
  silently missed; the implementer verifies each site's shape before editing and notes any
  site where the analogues do not actually flow (dated NOTES line);
- per-entry validation: same range rule as item 12;
- extend the template's commented `servers:` example (`internal/config/defaults/config.yaml`
  `:452-468` area) with the per-entry key, following the three-state legend style.

Then close the register: remove the "[P2] Context-budget % config key" sub-bullet from
`ISSUES.md`'s parked "apogee-code feature parity" entry (the surrounding entry and its other
bullets stay), and put the whole feature's CHANGELOG entry text in the sidecar.

**Files:** `internal/config/config.go`, `internal/config/config_test.go`,
`internal/config/defaults/config.yaml`, `cmd/apogee/wire_server.go`,
`cmd/apogee/schedule.go`, `cmd/apogee/delegation.go`, `cmd/apogee/upstream.go`,
`ISSUES.md`, `CHANGELOG.md`

**Tests:** resolver cases (entry wins, 0 falls through to top-level, both 0 → engine
default); per-entry validation cases; a rebind-site test where the existing analogues have
one (mirror the nearest `ResolveContextWindow` test).

**Acceptance:** `go build ./... && go test ./internal/config/ ./cmd/apogee/` passes;
`grep -c 'Context-budget % config key' ISSUES.md` → 0.

**Commit:** `feat(config): per-server response-reserve override rides every rebind`

---

## Suggested version bump

One user-facing feature ships here (the `response-reserve:` key, items 11–13); the rest is
residual hygiene that rides it. Suggest a micro bump (v0.14.8 → v0.14.9) at the run's end —
the user decides; no item performs it.
