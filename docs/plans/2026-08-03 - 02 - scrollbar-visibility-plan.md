# Plan — The scrollbar obeys `ui.show-scrollbar`, and the terminal's own never shows

**Date:** 2026-08-03
**Status:** PLANNED
**Track:** one `ISSUES.md` bullet (lines 6-7) — *"The scrollbar should be visibile based on a
setting in apogee's config.yaml: show-scrollbar. Also, the terminal scrollbar should not be
visible at all when apogee is running. llama-launcher does that already properly today."*
Config plumbing (`cmd/apogee`), TUI rendering (`internal/tui`), and one startup-frame fix. No
engine, protocol, persistence, or Mechanism changes.

**Plan decisions (2026-08-03) — the owner ratifies these by reviewing this file; edit here to
veto before executing:**

1. **The key lives in the `ui:` block: `ui.show-scrollbar`.** The issue wrote it flat, but the
   `ui:` block (`defaults/config.yaml:357-394`, documented as the block "designed to grow") is
   where display toggles live — `ui.spinner-color` is the precedent this plan copies hop for
   hop. The key name the owner asked for is preserved; only its nesting follows the house.
   File-only, like the rest of `ui:` — no flag, no env.
2. **Default `true`** — absent key, today's look.
3. **`false` reclaims the column.** The gutter is not reserved and the body takes the window's
   full width (minus `bodyRightGutter`, which stays — text never touches the window edge). The
   no-mid-run-re-wrap invariant behind "the column is reserved unconditionally"
   (`internal/tui/theme.go:96-104`) is not violated: the setting is fixed for the process
   lifetime, so the wrap width is exactly as stable within a run as it is today. A hidden bar
   that still ate a column would read as a bug.
4. **On `tui.Options` the field is inverted: `HideScrollbar bool`.** Two dozen test sites
   construct `Options{...}` literally (`grep "Options{" internal/tui/*_test.go` → 24), and the
   zero value must keep today's behavior or every layout-pinning test silently changes wrap
   width. The config side stays positive (`showScrollbar`, default `true`); the composition
   root inverts once.
5. **The terminal-scrollbar fix is "alt screen from the very first frame", not the
   llama-launcher technique.** llama-launcher never scrolls the primary screen (hand-rolled
   ANSI, absolute addressing, no alt screen at all — `llama-launcher/internal/launcher/ui.go`).
   Apogee already owns the alternate screen, which is what suppresses a terminal's scrollbar —
   but only on laid-out frames. The leak is the pre-`ready` placeholder.

**Why the terminal's scrollbar shows today (the diagnosis item 3 implements):** Bubble Tea v2
applies alt screen as a *view field*, per frame, not as a program option (ADR 0011;
`tea.NewProgram` at `internal/tui/tui.go:545` passes only `tea.WithContext`). The laid-out
frame sets it — `v.AltScreen = true` at `internal/tui/model.go:2802` — but the `!m.ready`
placeholder branch (`model.go:2748-2753`) returns a `tea.View` that sets only `WindowTitle`.
So apogee's first frame(s) render on the **primary** screen, push lines into scrollback, and
the terminal keeps its own scrollbar alive for the whole run.

**ADR:** none needed. ADR 0011 still binds — every new Model-reachable field here is a plain
`bool`, safe under value copies. `layout.md` is the spec home for the visible change (item 4).
Bubble Tea anchor: `charm.land/bubbletea/v2 v2.0.7` (`go.mod:7`).

**Standing requirements:** forward skill `coding-standards` when executing
(`/implement-plan <this file> with skills: coding-standards`). Run `make check` before every
commit. Changelog bullets go under `## [Unreleased]` — never touch `VERSION` or a release
heading. Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:** every other `ISSUES.md` bullet (scroll-jump on send, prompt collapsing, …);
mouse-wheel/scroll *behavior* (this plan changes what is painted, not how scrolling works); a
flag or env var for the setting; hot-reload of config; any version bump. **Flagged for the
owner, deliberately untouched:** `layout.md:2-4` promises *two* free columns beside a painted
bar and *three* to the edge, while the code implements one/two (`theme.go:96-104`,
`bodyRightGutter = 1` + `scrollbarWidth = 1`, pinned by tests) — prose and code disagree by one
column. Item 4 edits neighbouring text and must **not** silently change those counts; whether
the spec or the code is right is a separate owner call.

File:line anchors below are as of the 2026-08-03 working tree (post-`b18cfa6`) and may drift a
few lines.

## 1. `ui.show-scrollbar` — from config.yaml to `tui.Options` — ✅ DONE (2026-08-03)

NOTES (2026-08-03): a third field on `uiSettings` forced touches beyond the item's named files —
every existing `uiSettings` literal that stands for a *resolved* block now carries
`showScrollbar` (`config_test.go` `wantUIDefault` + the resolveSettings and partial-block tables,
`wire_test.go`'s spinner fixtures), or struct equality would fail against the new default; the
`uiSettings` doc comment's "The two are INDEPENDENT keys" now reads "The two spinner keys", since
three keys live there. `defaults_test.go` is unmodified, and the file-only merge
(`config.go:586-588`) needed no change as the plan predicted.

**What:**

Copy the `ui.spinner-color` path (`cmd/apogee/config.go`) hop for hop:

- `uiConfig` (`config.go:995-1004`): add `ShowScrollbar *bool \`yaml:"show-scrollbar"\`` — a
  pointer, with the house comment: so an explicit `show-scrollbar: false` is distinguishable
  from an absent key.
- `uiSettings` (`config.go:379`): add `showScrollbar bool`. `defaultUISettings()`
  (`config.go:394`) sets it `true`. `toUISettings` (`config.go:1010`) copies it when the
  pointer is non-nil. `validate()` (`config.go:405-409`) needs nothing for a bool — leave it.
- The layer merge already carries the whole `ui` block file-only (`config.go:586-588`,
  "env/flag never carry the UI block") — verify, no change expected.
- Composition root (`cmd/apogee/wire.go:454-455`, beside `Spinner`/`SpinnerColor`): pass
  `HideScrollbar: !opts.ui.showScrollbar` — the one place the polarity flips.
- `tui.Options` (`internal/tui/tui.go:184`, beside `SpinnerColor`): add `HideScrollbar bool`
  with a doc comment stating *why* it is inverted (the zero value must mean today's behavior —
  plan decision 4). Consumption is item 2; this item only lands the field.
- Defaults template `cmd/apogee/defaults/config.yaml` (`ui:` block, lines ~357-394): document
  the key in the house style — extend the block's banner prose to say what the switch does
  (the transcript's scrollbar *and* its reserved column go together; default on; config-file
  only) and add `#   show-scrollbar: true` beside the commented spinner keys. It must stay
  commented out — `cmd/apogee/defaults_test.go:58-112` pins that the template sets exactly one
  thing (the system prompt).

**Tests:** in `cmd/apogee`, beside the existing `ui.spinner-color` config tests and in their
style: absent key → `showScrollbar == true`; explicit `show-scrollbar: false` → `false`;
explicit `true` → `true`; the ui block still merges file-only. `defaults_test.go` stays green
unmodified (the template still parses and still sets only the system prompt).

**Acceptance:** `go test ./cmd/apogee/...` green; `make check` green. A config file carrying
`ui: {show-scrollbar: false}` resolves to `uiSettings.showScrollbar == false`, proven by a
test; the wire hop to `Options.HideScrollbar` is one reviewed line.

**Commit:** `feat(config): ui.show-scrollbar is the transcript scrollbar's switch`

## 2. The scrollbar and its column obey the switch

Depends on item 1.

**What:**

The scrollbar today is a hand-rolled one-column gutter: reserved in `layout()`
(`internal/tui/model.go:2475`, `m.viewport.SetWidth(max(1, m.width-scrollbarWidth))`;
`scrollbarWidth = 1` at `model.go:2443`), painted by `renderScrollbar` (`model.go:2935`) and
hung on by `joinScrollbar` (`model.go:2986`) from `View` (`model.go:2773`). Wrap width derives
from the viewport width (`model.go:2516`, `viewport.Width()-bodyRightGutter`), so it follows
the reservation automatically.

- `layout()` (`model.go:2475`): reserve the column only when `!m.opts.HideScrollbar`; hidden →
  `m.viewport.SetWidth(max(1, m.width))`. `bodyRightGutter` stays in the wrap-width derivation
  untouched — text keeps one free column to the window edge in both states.
- `View` (`model.go:2773`): gate the join — hidden → `rows = append(rows, body)`, no
  `renderScrollbar` call. `renderScrollbar` and `joinScrollbar` internals stay untouched;
  their blank-when-no-overflow behavior is still the default state's contract.
- `grep -n scrollbarWidth internal/tui/` for any consumer beyond `model.go:2443/2475` — the
  known set is those two plus the tests below; any surprise consumer gets the same gate and a
  NOTES line.
- `theme.go:96-104` (`bodyRightGutter` rationale): amend the comment — the column is reserved
  unconditionally *while the scrollbar is enabled* (`ui.show-scrollbar`, default on); turning
  it off removes column and bar together, which cannot re-wrap mid-run because the setting is
  process-constant.

**Tests:** the two pinning tests stay green **unmodified** — `TestTranscriptBodyLeavesRightGutter`
(`internal/tui/model_test.go:2275`) and `TestPaintedScrollbarHoldsOneColumn`
(`internal/tui/paint_test.go:171`) both exercise the default (shown) state, and the inverted
Options field guarantees that without touching them. New, beside them:
`TestHiddenScrollbarYieldsTheColumn` — with `Options{HideScrollbar: true}` and content tall
enough to overflow: the viewport takes the full window width; the painted frame carries no
track (`│`) or thumb (`█`) column at the right edge; wrap width is
`m.width - bodyRightGutter`; and scrolling still works (the bar is hidden, not the scrolling).

**Acceptance:** `make check` green. Both states are pinned: default frames are byte-identical
to today's (the unmodified pinning tests prove it), and the hidden state yields its column.

**Commit:** `feat(tui): the scrollbar obeys ui.show-scrollbar`

## 3. The placeholder frame stays on the alt screen

Independent of items 1-2.

**What:**

- The `!m.ready` early return in `View` (`internal/tui/model.go:2748-2753`): set
  `v.AltScreen = true` beside the `WindowTitle` it already carries, so *every* frame apogee
  ever emits — including the first — is on the alternate screen and nothing enters the
  terminal's scrollback. `MouseMode` stays laid-out-frame-only: the placeholder has nothing to
  click, and this item changes exactly the field the diagnosis names.
- Audit the startup happy path for other primary-screen writes that would land in scrollback
  before the program takes over: `cmd/apogee/main.go`, `cmd/apogee/root.go` (`runRoot`), and
  `cmd/apogee/wire.go` up to `tui.Run`. Expected result: none on the happy path (errors and
  subcommands print, but those paths never reach the TUI). Record what the audit found as a
  dated NOTES line on this item — a finding beyond one-line reach becomes a FOLLOW-UP, not
  scope creep here.

**Tests:** `TestViewStaysOnAltScreen` in `internal/tui/model_test.go` (or beside the
`TestViewCarriesWindowTitle` precedent, which already reads `m.View()` in both branches):
`m.View().AltScreen` is `true` both before the first `WindowSizeMsg` (`!m.ready` placeholder)
and on a laid-out frame.

**Acceptance:** `make check` green; the test pins both branches. Manual owner check, which no
test can see (recorded here as expectation, not gate): launching apogee in a fresh terminal
shows no terminal scrollbar for the whole run, and quitting restores the shell exactly as it
was — the pre-launch scrollback intact, no apogee frame left behind.

**Commit:** `fix(tui): the placeholder frame stays on the alt screen`

## 4. The docs say what the switch does

Depends on items 1-3.

**What:**

- `layout.md` (the scrollbar's spec home; it mentions the bar only in passing at lines 2-4 and
  393-395): state the switch once, where the wrap rule speaks of the bar — the gutter and the
  bar exist only while `ui.show-scrollbar` holds (default on); off, the body takes the column
  and the wrap rule measures to the window edge (less the free column). Do **not** change the
  free-column counts in that prose — they are flagged to the owner in this plan's header, and
  this item must leave the discrepancy standing.
- `README.md`: add `show-scrollbar` wherever the `ui:` block (spinner settings) is documented.
  If the README does not document the `ui:` block at all, skip with a dated NOTES line saying
  so — do not invent a new README section for it.
- `ISSUES.md`: retire the bullet at lines 6-7 — both sentences; items 1-3 close them.
- `CHANGELOG.md` under `## [Unreleased]`: **Added** — `ui.show-scrollbar`, what it hides and
  that the column comes back with it; **Fixed** — the first frame no longer touches the
  primary screen, so the terminal's own scrollbar stays away for the whole run.

**Tests:** none beyond `make check` (docs only).

**Acceptance:** `layout.md` specifies both states, the `ISSUES.md` bullet is gone,
`CHANGELOG.md` carries the Added and Fixed entries under `[Unreleased]`, `VERSION` untouched.
`make check` green.

**Commit:** `docs(tui): the scrollbar's switch is on record`

---

**VERSION-SUGGESTION:** a patch bump (`v0.10.10`) would be reasonable when the owner next cuts
`[Unreleased]` — it already carries the window-title wave, and one bump covers both. Not part
of this plan; the owner decides.
