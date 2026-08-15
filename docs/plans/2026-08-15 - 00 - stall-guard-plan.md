# Stall guard + reasoning retention — implementation plan

**Goal:** the status line stops lying during a silent stall. When the worker claims
"thinking"/"responding" but no engine event has arrived for a configured quiet period, the
running phrase gains a warning-tinted `· quiet <elapsed>` suffix — the honest fact, not a
guess. Alongside, the TUI starts *retaining* the model's reasoning chunks (bounded,
escape-stripped) as the seam for a future reasoning display — **nothing renders reasoning
text yet**.

- **Date:** 2026-08-15
- **Status:** ready
- **Sized for:** ~200k-context host
- **Skills:** coding-standards

**Motivating incident (2026-08-14):** a slow turn (~43k-token prompt on a 27B) showed a
bare "thinking" for ~20 minutes with no way to tell progress from death; it in fact
completed normally into an interactive question the owner was not at the screen for. The
guard's job is therefore honest silence reporting — "quiet Xs" — never a stall verdict,
and waiting-on-user states (an open question or approval) must never show it.

**Authoritative sources:**

- `internal/domain/events.go:70-93` — `ReasoningEvent` contract (in-order chunks,
  observation-only, Text untrusted → must be escape-stripped exactly as
  `transcript.appendToken` does).
- `internal/tui/doc.go` — value-copied Model rules (ADR 0011: no `strings.Builder` by
  value; strings only) and the escape-strip-at-the-seam invariant (doc.go:674-700).
- ADR 0030 — one width authority: all measuring/truncating through `m.th.measure`, never
  `lipgloss.Width` / package-level `ansi.*`.
- ADR 0037 — settings live-apply (`settingsApplyLocal`, `internal/tui/settings.go:1404`).
- ADR 0040 — color schemes are embedded roles with user shadowing
  (`internal/scheme/scheme.go:29-99`, both yamls under `internal/scheme/schemes/`).
- `layout.md:109-131` (row budget), `layout.md:208-218` (give-way / compose-to-window),
  `layout.md:1103-1136` (status-line spinner + right slot).

**Ratified design calls** (owner, 2026-08-15, via AskUserQuestion):

1. **Scope:** the stall guard is the deliverable. Reasoning tokens are *retained* in the
   TUI as plumbing but **not rendered anywhere** — no tail row, no transcript entry, no
   config key for display. (A visible reasoning tail is a future plan.)
2. **Threshold:** a config key — `ui.stall-after`, duration, **default `90s`**, `0`
   disables the guard. (A hard-coded constant and a 3×-heartbeat rule were rejected;
   90s clears large-prompt ingestion, which is legitimately silent for 1–2 min.)
3. **Surfacing:** a quiet-duration suffix on the running phrase — e.g.
   `thinking 21m 03s · quiet 3m 10s` — once quiet time crosses the threshold. No
   transcript note, no "stalled?" wording; the suffix disappears as soon as an event
   arrives.
4. **Coverage:** the guard watches `actThinking` and `actResponding` only. Tool calls are
   excluded (long silent tools are normal); `actStopping` suppresses the suffix.
5. **Tint:** a new `warning` scheme role (amber), threaded to a `statusWarning` theme
   style. Reusing error-red and going tint-less were rejected.

**Out of scope:**

- Rendering reasoning text anywhere (status-adjacent tail row, transcript block,
  expandable entry) — future plan; this plan only lands the retention seam.
- Any engine/`internal/agent` change — this plan is TUI + config + scheme only.
- Version bumps (see closing note).

---

## 1. `ui.stall-after` config key, end to end — ✅ DONE (2026-08-15)

NOTES (2026-08-15): the registry has no duration kind, so the row is `KindString` with a
`validateStallAfter` hook — the plan's stated fallback. The hook delegates to the startup path
itself (`uiConfig.toUISettings().Validate()`) rather than parsing a second time.
NOTES (2026-08-15): `UISettings` gained one unexported companion field, `unparsedStallAfter`: the
yaml seam cannot return an error, so text no duration can be made of is carried as written for
`UISettings.Validate` to refuse and quote. An empty value reads as an absent key, the posture the
block's two other string keys already take.

**What:** add the duration key `ui.stall-after` and thread it to the TUI as
`tui.Options.StallAfter time.Duration`.

- Schema: `uiConfig` (`internal/config/config.go:1523-1545`) gains
  `StallAfter *string \`yaml:"stall-after"\`` (pointer, so explicit-zero ≠ absent, the
  `show-scrollbar` pattern), mapped in `toUISettings()` (config.go:1550). Parse with
  `time.ParseDuration`; a bare `0` is accepted as disabled. Negative or unparseable →
  validation error.
- Resolved value: `UISettings.StallAfter time.Duration` (config.go:390-412), default
  `90 * time.Second` in `defaultUISettings()` (config.go:422), checked in
  `UISettings.Validate()` (config.go:438).
- Registry row (`internal/config/registry.go:295` region): `Path: "ui.stall-after"`,
  string/duration kind per the registry's existing kind set (follow the closest
  precedent — if no duration kind exists, a string kind with a `Validate` hook parsing
  `time.ParseDuration`), `Default: "90s"`, `Editable: true`, `Desc:` one sentence naming
  the quiet-suffix behaviour and `0` = off.
- Defaults template: `internal/config/defaults/config.yaml:565-569` ui comment block
  gains `#   stall-after: 90s     # warn "· quiet …" on the status line after this much
  engine silence; 0 = off`.
- Threading: `tui.Options` field (`internal/tui/tui.go:242` region), wired in
  `cmd/apogee/wire_options.go:144` region, settings-row value getter in
  `cmd/apogee/settingsrows.go:140` region, live apply in `settingsApplyLocal`
  (`internal/tui/settings.go:1404-1420`) with a `settingKeyStallAfter = "ui.stall-after"`
  constant beside `settingKeyShowScrollbar` (settings.go:1514).

**Files:** `internal/config/config.go`, `internal/config/registry.go`,
`internal/config/defaults/config.yaml`, `internal/config/config_test.go`,
`internal/tui/tui.go`, `internal/tui/settings.go`, `internal/tui/settings_test.go`,
`cmd/apogee/wire_options.go`, `cmd/apogee/settingsrows.go`

**Tests:** registry bijection and row-invariant tests
(`internal/config/registry_test.go:25,214`) must pass with the new row; add table cases
for: valid `90s`/`2m`, `0` disables, negative rejected, garbage rejected, absent key →
90s default. A settings live-apply test in the existing `settingsApplyLocal` pattern.

**Acceptance:**

```
go build ./... && go test ./internal/config ./internal/tui ./cmd/apogee
```

**Commit:** `feat(config): ui.stall-after duration key threaded to the TUI`

## 2. `warning` scheme role and `statusWarning` style — ✅ DONE (2026-08-15)

NOTES (2026-08-15): the item's Files line named `internal/tui/colorscheme_test.go` for the theme
assertion, but that file tests the `/color-scheme` command; the neighbouring style tests (the
role-to-style wiring table) live in `internal/tui/theme_test.go`, so the `statusWarning` case went
there — "in whatever pattern neighbouring style tests use", as the item's Tests line asks.
NOTES (2026-08-15): the role count is stated in prose in four places the item does not list —
`README.md`, `layout.md`, `CONTEXT.md` (all 28 → 29) and `newTheme`'s own doc comment in
`internal/tui/theme.go`, which was already stale at 26 and now reads 29. Adding a role without them
would leave four documents miscounting the vocabulary. ADR 0040 needs no amendment: it decides that
an additive role is safe and only a rename needs one.

**What:** add the role `warning` to the scheme (`internal/scheme/scheme.go:29-83` struct +
tag; `roleKeys` is reflected, so the field addition is the registration), give it an amber
value in **both** built-in scheme yamls under `internal/scheme/schemes/` — an amber that
sits visually between that scheme's `muted` and `error` (dark scheme: a desaturated gold
like `#d7af5f`-family; light scheme: a darker amber that passes on a light surface — match
each yaml's existing palette style). Then thread it to the theme: `theme` gains
`statusWarning` (warning foreground on the black status field, **not** bold — constructed
beside `statusBar`/`statusError` at `internal/tui/theme.go:389-391`, styles declared at
theme.go:203-205).

**Files:** `internal/scheme/scheme.go`, `internal/scheme/schemes/` (both yamls),
`internal/scheme/scheme_test.go`, `internal/tui/theme.go`, `internal/tui/colorscheme_test.go`

**Tests:** the reflected role tests in `internal/scheme` must cover the new role (add a
case only if the tests enumerate roles explicitly); a theme test asserting
`statusWarning` renders with the warning role's color, in whatever pattern neighbouring
style tests use.

**Acceptance:**

```
go test ./internal/scheme ./internal/tui
```

**Commit:** `feat(scheme): warning role and statusWarning status-line style`

## 3. Stall guard: last-event clock and the quiet suffix — ✅ DONE (2026-08-15)

NOTES (2026-08-15): the worker-start stamp went into `moveActivity` (activity.go) rather than into the
submit path the item names. There are four launch sites, not one — `launchExchange`, `/continue`'s
resume and canned turns, `/compact` — and every one of them already moves the activity; stamping at
that single funnel makes "a new exchange never inherits the last one's silence" structural instead of
a line each launch site must remember. The eventMsg stamp the item asks for is unchanged and still
unconditional, so Events the folds ignore (usage, audit) count as life too.
NOTES (2026-08-15): a zero `lastEvent` reports no silence at all (the `activity.elapsed` posture),
so a model that has never heard from an engine cannot render a gap measured from the epoch.

Depends on item 1 and item 2.

**What:** the TUI tracks when the last engine event arrived and surfaces crossing the
`ui.stall-after` threshold as a warning-tinted suffix on the running phrase.

- `Model` gains `lastEvent time.Time` (a plain value — safe under the ADR 0011
  value-copy rule). It is set to `time.Now()` in the `eventMsg` Update case
  (`internal/tui/model.go:657-660`) — **every** engine event resets it, any depth, any
  variant, including `ReasoningEvent` (a reasoning stream is life; the incident's
  signature is *no* events at all). It is also set when a worker starts (submit path),
  so the quiet clock never counts from a previous exchange. `beatMsg` /
  `heartbeatTickMsg` / spinner ticks do **not** touch it — they are not engine events.
- Quiet duration: computed at render time in the status line's left slot
  (`statusLeft`, model.go:3238 region / `runningPhrase`, model.go:3289 region) from
  `time.Since(m.lastEvent)` — no new tick and no stored flag; the spinner tick
  (`spinnerTickMsg`, `internal/tui/spinner.go:407`, Update case model.go:901-923)
  already repaints the status line every frame while running, so the suffix appears and
  counts up with zero additional scheduling.
- Firing rule: suffix renders iff `Options.StallAfter > 0` AND
  `m.act.kind ∈ {actThinking, actResponding}` AND `quiet ≥ StallAfter`. `actStopping`,
  `actTool`, and every other kind never show it.
- Rendering: ` · quiet <formatElapsed(quiet)>` appended after the phrase and its
  elapsed clock, styled with `statusWarning` (item 2); the phrase and clock keep their
  existing styles. The left slot stays composed **to** the window per
  `layout.md:208-218` — measure with `m.th.measure` (ADR 0030), and when the row is
  tight the suffix gives way before the phrase does (drop the suffix whole; never
  truncate it mid-word).
- `layout.md`: amend the status-line prose — one sentence in the left-slot/give-way
  region (layout.md:208-218) and a short paragraph under the spinner section
  (layout.md:1103-1112) naming the quiet suffix, its threshold key, and its give-way
  rule.
- `internal/tui/doc.go`: extend the activity narration (doc.go:310-330 region) with the
  last-event clock and the suffix, per the docmap discipline.

**Files:** `internal/tui/model.go`, `internal/tui/activity.go`,
`internal/tui/activity_test.go`, `internal/tui/model_test.go`, `internal/tui/doc.go`,
`layout.md`

**Tests:** in the `activity_test.go` / `model_test.go` patterns:

- suffix absent below threshold, present at/after it (inject the clock — follow how
  `runningPhrase(now)` is already tested with an explicit now).
- an arriving event resets the clock and clears the suffix.
- `actTool` and `actStopping` never show it, whatever the quiet time.
- an open interactive question/approval (an open tool call awaiting the user) shows no
  suffix regardless of quiet time — the incident's corrected shape, pinned.
- `StallAfter == 0` never shows it.
- width guard: with the suffix painted, `ansi.StringWidth(m.statusLine()) == m.width`
  (the model_test.go:3657+ pattern), including a narrow-width case where the suffix
  gives way.

**Acceptance:**

```
go test ./internal/tui
```

**Commit:** `feat(tui): status line surfaces engine silence as a quiet-time suffix`

## 4. Reasoning retention plumbing (nothing rendered) — ✅ DONE (2026-08-15)

NOTES (2026-08-15): the submit-side reset landed in `launchExchange` (commandrun.go), a file the item's Files line does not name — that function is the tail both send paths share, so it is where "submit" exists as one seam rather than two. The other three worker launches (`/continue`'s resume and canned turns, `/compact`) need no reset of their own: `finishWorker` has already cleared the tail at the end of the Exchange before them.
NOTES (2026-08-15): the item asks that the `foldCases()` row make `TestFoldEventCoversEveryEventVariant` state the new answer, but that test only checks a row EXISTS; its sibling `TestFoldEventFoldsEveryVariant` is what asserts a row's content. The table therefore gained a `wantReasoning` column asserted there, so all thirteen variants now say what they do to the tail — the ReasoningEvent row retains its chunk, every other row retains nothing.
NOTES (2026-08-15): `reasoning.go` makes the reasoning tail a FOURTH fold behind `foldEvent`, so the "three folds a view update is made of" prose in `fold.go`'s header and in `doc.go`'s fold paragraph — both files the item names — now reads four.

Depends on item 3 (shares `model.go`/`doc.go`; keeps the last-event semantics settled
first).

**What:** the TUI keeps a bounded, escape-stripped tail of the current turn's reasoning —
the seam a future display will read — and renders none of it.

- New file `internal/tui/reasoning.go`: a `reasoningTail` value type holding
  `text string` (a string, never a `strings.Builder` — ADR 0011), plus the acting
  delegate's identity `(depth int, spawn string)` in the same one-delegate-at-a-time
  rule the activity line uses (`activity.go:58-66`). Methods: `append(chunk, depth,
  spawn)` — strips via `stripEscapes` (`internal/tui/transcript.go:1455`, the seam rule
  of doc.go:674-700 and events.go:87-89), resets the buffer when the delegate identity
  changes, then bounds the buffer to its cap by dropping the *front* on a rune
  boundary; and `reset()`.
- Cap: 4096 bytes (a named constant with a doc comment; generous for a status-adjacent
  tail, bounded so an hour of reasoning cannot grow the Model — the `previewTailLines`
  bounding idea from `render.go:438-470`, applied to bytes).
- Wiring in `foldEvent` (`internal/tui/fold.go:31`) / Update: `ReasoningEvent` appends;
  `StreamResetEvent` resets (superseded turn); `MessageEvent` resets (turn committed —
  the canonical copy already lives on the engine's message as `reasoning_content`);
  submit and `finishWorker` reset. Update the `foldCases()` row for `ReasoningEvent` in
  `internal/tui/fold_test.go:52-148` so `TestFoldEventCoversEveryEventVariant` states
  the new answer.
- `internal/tui/doc.go` must name the new file (docmap: `TestDocMapNamesEveryFile`,
  `internal/tui/docmap_test.go:12`) and say explicitly that nothing renders the buffer
  yet and what it is for.

**Files:** `internal/tui/reasoning.go`, `internal/tui/reasoning_test.go`,
`internal/tui/fold.go`, `internal/tui/fold_test.go`, `internal/tui/model.go`,
`internal/tui/doc.go`

**Tests:** table tests in `reasoning_test.go`: chunks accumulate in order; ESC/C0 bytes
stripped; front-dropped at the cap on a rune boundary (multi-byte rune straddling the
cut); delegate change resets; each reset trigger clears. Plus the `foldCases()` row
update.

**Acceptance:**

```
go test ./internal/tui
```

**Commit:** `feat(tui): retain a bounded reasoning tail behind the fold (not rendered)`

---

**Suggested version bump:** micro (0.12.x → next), per the per-shipped-feature policy —
the stall guard is a user-visible feature. The bump is the owner's call; no item in this
plan changes VERSION or CHANGELOG release headings.
