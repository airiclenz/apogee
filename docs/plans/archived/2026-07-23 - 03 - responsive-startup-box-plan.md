# Plan — responsive start-up box: right-aligned two-column wide layout + `context` fact

**Date:** 2026-07-23. **Status: ✅ DONE (2026-07-23)** — implemented directly this session (not via
`/implement-plan`) at the owner's request, applying `coding-standards`; uncommitted (owner commits).

One owner-requested change to the one-time start-up box (shipped in
`docs/plans/archived/2026-07-23 - 02 - version-command-and-startup-box-plan.md`): when there is
**enough horizontal space**, paint the logo on the left and a **right-aligned** session-info block
on the right — host / model / **context** / version, four rows beside the logo's four lines — and add
a **new `context` fact** (the model's context-window size, e.g. `32k`). When the width does **not**
allow it, fall back to today's stacked layout **exactly as it is now** (logo, blank line, then
`host` / `model` / `version` — **no** `context` row).

Two owner decisions locked in this session (do not re-litigate):

- **Wide-layout placement = right-aligned block.** The box still spans the full content width; the
  logo hugs the left, the info block hugs the right content edge, and the gap between them widens as
  the terminal widens. (Alternatives considered and rejected: a fixed gap after the logo, and a
  centered logo+info group.)
- **The narrow fallback stays exactly as today** — `host` / `model` / `version` stacked, **no
  `context` row**. The `context` fact appears **only** in the wide layout.

Target look — **wide** (enough horizontal space; info block right-aligned, flush to the right
padding; block-art glyphs are width-1):

```
╭────────────────────────────────────────────────────────────────────────────────╮
│  ▀▀█▄ ████▄ ▄███▄ ▄████ ▄███▄ ▄███▄           host     apollo-2                 │
│ ▄█▀██ ██ ██ ██ ██ ██ ██ ██▄█▀ ██▄█▀           model    gemma-4-E4B-it-QAT-Q4_0  │
│ ▀█▄██ ████▀ ▀███▀ ▀████ ▀█▄▄▄ ▀█▄▄▄           context  32k                      │
│       ██           ▄▄█▀                       version  v1.7.0-72-ge2a7c2d       │
╰────────────────────────────────────────────────────────────────────────────────╯
```

Target look — **narrow fallback** (unchanged from today; note: **no** `context` row):

```
╭─────────────────────────────────────────╮
│  ▀▀█▄ ████▄ ▄███▄ ▄████ ▄███▄ ▄███▄     │
│ ▄█▀██ ██ ██ ██ ██ ██ ██ ██▄█▀ ██▄█▀     │
│ ▀█▄██ ████▀ ▀███▀ ▀████ ▀█▄▄▄ ▀█▄▄▄     │
│       ██           ▄▄█▀                 │
│                                         │
│ host     apollo-2                       │
│ model    gemma-4-E4B-it-QAT-Q4_0        │
│ version  v1.7.0-72-ge2a7c2d             │
╰─────────────────────────────────────────╯
```

## Where things stand (grounded, verified 2026-07-23)

**The start-up box is one transcript entry, re-rendered fresh at the live width on every repaint.**
- `startupView` (`internal/tui/transcript.go:90-95`) holds display-ready strings: `Logo`, `Host`,
  `Model`, `Version` (no `Context` yet). `addStartup` (`transcript.go:138-142`) escape-strips the
  untrusted `Host`/`Model` (they trace to config/CLI) and passes `Logo`/`Version` through as trusted.
- Seeded once in `newModel` (`internal/tui/model.go:140-145`): `Logo` = `strings.TrimRight(apogeeLogo,
  "\n")`, `Host` = `hostDisplay(opts)` (`model.go:1060`), `Model` = `displayModel(opts.Model)`
  (`model.go:1082`), `Version` = `opts.Version`.
- Rendered via the per-kind switch `renderEntryLines` (`render.go:127-128`,
  `case entryStartup: … renderStartupBox(th, e.startup, inner)`), where `inner = railedWidth(width,
  e.depth)` and the box is always depth 0 (top-level), so `inner` is the full transcript content width.

**`renderStartupBox` today (`internal/tui/render.go:226-255`)** builds the logo lines, one blank
spacer line, then the `startupRows = {"host","model","version"}` (`render.go:226`) label-aligned rows
(labels dim via `th.noteText`, values plain), and wraps the whole thing in
`th.startupBorder.Width(width).Render(...)`. It has **no** width-adaptive branch — one layout only.

**Chrome + width math.**
- `th.startupBorder` (`internal/tui/theme.go:168-171`): `Border(lipgloss.RoundedBorder())`,
  `BorderForeground(colDarkGray)`, `Padding(0, 1)`, **no** `Background`/`BorderBackground` (transparent,
  self-closing card). `.Width(n).Render(...)` folds the border (1 col each side) and padding (1 col
  each side) into `n`, so the **content region inside the frame is `n - 4` columns**. Use the lipgloss
  accessor `th.startupBorder.GetHorizontalFrameSize()` (= 4 here) rather than a literal, so the math
  tracks the style if the padding ever changes.
- `th.noteText` (`theme.go:155`) = dim `colFaint`, the info labels' style.
- `lipgloss.Width` is ANSI-aware (styling never perturbs the arithmetic) and counts the block-art
  glyphs `▀ ▄ █` as width-1, so `logoW` = the widest logo line = **36** (lines 0-2 are 36 wide, line 3
  is 23; `internal/tui/logo.txt`, embedded via `internal/tui/logo.go`).

**The `context` value already exists in `Options`.**
- `Options.ContextWindow int` (`internal/tui/tui.go:91-95`) is the active model's context-window size
  in tokens (0 when unknown). The footer already renders it as `formatTokens(m.opts.ContextWindow)`
  (`model.go:1036`).
- `formatTokens` (`model.go:1318-1326`) renders it compactly: `""` for `n <= 0`, bare below 1000,
  else `"<n>k"` (32768 → `"32k"`). `nonEmpty` (`model.go:1329-…`) is the footer's skip-empty helper —
  the wide layout mirrors it so an unknown context simply omits its row.

**Existing tests.**
- `render_test.go:651-705` `TestRenderStartupBox` — width 80; asserts a logo fragment, the three facts
  + dim labels, the rounded corners, the absence of the input box's black-bg SGR, and that every line
  is exactly `width` columns with top/bottom closing on `╮`/`╯`. **At width 80 the box will now render
  in the WIDE layout**, so this test must be updated (below).
- `model_test.go:131-171` — `TestNewModelSeedsStartupBox` (one seeded `entryStartup` at `entries[0]`
  carrying the `Options` host/model/version) and `TestStartupBoxSurvivesClear`.

### Decisions locked (recommended defaults — do not re-litigate silently)

- **Right-aligned wide block** (owner-selected). The info block's left column is `inner - infoW`; the
  widest info row sits flush against the right padding, shorter rows trail off toward it.
- **`context` fact only in the wide layout** (owner-selected). The stacked fallback is byte-for-byte
  today's output.
- **Switch threshold** = `inner >= logoW + startupWideMinGap + infoW`, with a new unexported const
  `startupWideMinGap = 4`. Below it, stacked. (An unusually long model name grows `infoW` and can
  force the fallback even on a wide terminal — acceptable, graceful degradation; document it.)
- **`context` is a trusted derived value** (it is `formatTokens` of an int, not config/CLI text), so
  it is **not** escape-stripped — like `Version`.
- **Top-aligned pairing**: logo line *i* sits beside info row *i*; render
  `max(len(logo), len(rows))` rows, blank-filling whichever side is shorter. No blank spacer line in
  the wide layout (the four logo lines pair directly with the four info rows).

---

## 1. Responsive start-up box — right-aligned wide layout + `context` fact — ✅ DONE (2026-07-23)

**What:**

*New fact on the view + seed.* In `internal/tui/transcript.go` add `Context string` to `startupView`
(`:90-95`, documented — "the formatted context-window size, e.g. `32k`; `""` when unknown; a trusted
derived value like `Version`"). In `addStartup` (`:138-142`) leave `Context` **unstripped** and note
in the doc comment that it is `formatTokens` of an int (trusted), alongside `Logo`/`Version`. In
`newModel` (`internal/tui/model.go:140-145`) seed `Context: formatTokens(opts.ContextWindow)`
(`formatTokens` is in the same package, `model.go:1318`). Adding a named field does not break the
existing `startupView{...}` literals (they keep their fields; `Context` zero-values).

*Two layouts in the renderer.* Rework `renderStartupBox` (`internal/tui/render.go:240-255`) into a
width chooser, extracting today's body unchanged as the fallback:

- `inner := width - th.startupBorder.GetHorizontalFrameSize()` — the usable content columns inside the
  frame (= `width - 4`).
- Split the logo (`strings.Split(v.Logo, "\n")`); `logoW` = widest `lipgloss.Width` over its lines.
- Build the wide info rows as a small `[]struct{ label, value string }` (preallocated, cap 4):
  `{"host", v.Host}`, `{"model", v.Model}`, `{"context", v.Context}`, `{"version", v.Version}`; drop
  any row whose `value == ""` (mirrors `nonEmpty`, so an unknown `context` omits its row). Over the
  kept rows compute `labelW` = widest label and `infoW` = widest `labelW + 2 + lipgloss.Width(value)`.
- **If** `inner >= logoW + startupWideMinGap + infoW` → **wide**; **else** → **stacked fallback**.

- *Wide* (`renderStartupWide`): right-align the block at left column `L := inner - infoW`. For each of
  `max(len(logo), len(rows))` rows build:
  `logoLine` padded with spaces to `logoW` **+** `strings.Repeat(" ", L-logoW)` **+** the info line,
  where the info line is `th.noteText.Render(label + pad-to-labelW) + "  " + value`. A row past the end
  of the logo uses `strings.Repeat(" ", logoW)`; a row past the end of the info rows contributes no
  info line. **No** blank spacer line. Join with `"\n"` and return
  `strings.Split(th.startupBorder.Width(width).Render(joined), "\n")` — lipgloss pads every line to the
  full width, so the widest info row lands flush against the right padding and shorter rows trail off
  toward it (the right-aligned look).
- *Stacked fallback* (`renderStartupStacked`): the **current** `renderStartupBox` body verbatim — logo
  lines, one `""` spacer, then the `startupRows` (`render.go:226`, host/model/version, **no** context),
  labels dim, values plain, through `th.startupBorder.Width(width).Render(...)`. Output is byte-for-byte
  today's, so narrow terminals are unchanged.

- Add the const near the renderer: `const startupWideMinGap = 4 // min columns between the logo and the
  right-aligned info block before the wide layout engages` — with a doc comment (godoc form).
- Keep `startupRows` for the stacked path; the wide path owns its own row list (it adds `context`).
- Give each new unexported symbol (`renderStartupWide`, `renderStartupStacked`, `startupWideMinGap`,
  `startupView.Context`) a doc comment starting with its name (Go house style), and keep the existing
  `renderStartupBox` comment accurate to its new chooser role.

**Tests** (package `internal/tui`; table-driven with `t.Run` where natural; `t.Helper()` on new
helpers — testing.go standards):

- Update `render_test.go` `TestRenderStartupBox` (`:651`) — add `Context: "32k"` to the fixture and
  re-point it at the **wide** layout (width 80 now selects it). Assert: (a) a distinctive logo
  fragment survives (`"████▄ ▄███▄"`); (b) **all four** facts + dim labels are present
  (`host`/`model`/`context`/`version` and their values, incl. `"32k"`); (c) **side-by-side proof** — a
  single physical line carries **both** a logo fragment **and** the `host` label (they share a row);
  (d) **right-aligned proof** — on the ANSI-stripped output, the line bearing the widest value ends
  (ignoring the border/padding) with that value, i.e. the block is flush right; (e) rounded corners
  `╭ ╮ ╰ ╯`; (f) **no** black-bg SGR (reuse the existing `Background(colBlack)` probe); (g) every line
  is exactly `width` columns and the top/bottom rows close on `╮`/`╯`.
- Add `render_test.go` `TestRenderStartupBoxStackedFallback` at a narrow width (e.g. **50** — inner 46,
  wide needs `36 + 4 + infoW` and fails, stacked logo still fits inner ≥ 36). Assert the **stacked**
  shape: `host`/`model`/`version` present; `context` label and its value (`"32k"`) **absent**; the logo
  fragment and the `host` label are on **separate** lines (stacked, not side-by-side); every line is
  exactly `width` columns; rounded corners present.
- Update `model_test.go` `TestNewModelSeedsStartupBox` (`:131`) — set `ContextWindow: 32768` on the
  test `Options` and assert the seeded `startupView.Context == "32k"` (== `formatTokens(opts
  .ContextWindow)`), alongside the existing host/model/version assertions. `TestStartupBoxSurvivesClear`
  needs no change.

**Acceptance:** `go build ./... && go vet ./... && go test ./...` green (`gofmt`-clean). Commit:
`feat(tui): responsive start-up box — right-aligned wide layout with context`.

---

## Explicitly NOT in this plan

- **No change to the narrow/stacked layout's output** — it is extracted verbatim as the fallback; the
  only behavioural change there is that it now engages only below the wide threshold.
- **No change to the footer, `formatTokens`, `Options`, or the version/host/model seams** — the plan
  reuses `Options.ContextWindow` and `formatTokens` as-is; nothing new is wired into the binary.
- **No new config keys, no persistence, no network.** The box reads only already-resolved `Options`.
- **No logo recolouring or border-glyph change** — the wide layout reuses the same `startupBorder`
  chrome and the logo's default foreground.
- **No value truncation in the wide layout** — a too-long value simply grows `infoW` and, if it no
  longer fits, drops the box to the stacked fallback (which the viewport soft-wraps if even that is
  too narrow, exactly as today).

## Critical files

- `internal/tui/transcript.go` — `startupView.Context` field; `addStartup` trust note
- `internal/tui/model.go` — `newModel` seed (`Context: formatTokens(opts.ContextWindow)`)
- `internal/tui/render.go` — `renderStartupBox` chooser, `renderStartupWide`, `renderStartupStacked`,
  `startupWideMinGap`
- `internal/tui/render_test.go` — updated `TestRenderStartupBox` (wide) + new
  `TestRenderStartupBoxStackedFallback`
- `internal/tui/model_test.go` — `TestNewModelSeedsStartupBox` context assertion
- (read-only references) `internal/tui/tui.go` `Options.ContextWindow`; `internal/tui/theme.go`
  `startupBorder`/`noteText`; `internal/tui/logo.txt`/`logo.go`

## Owner-run checklist (after implementation)

- [ ] Launch `apogee` in a **wide** terminal against the live endpoint: the start-up box shows the logo
  on the left and a right-aligned `host` / `model` / `context` / `version` block on the right, the box
  still spans the full content width, and the border glyphs match the prompt box with no black fill.
- [ ] The `context` row reads the model's context window (matches the footer's `ctx`, e.g. `32k`); when
  the endpoint reports no window (0), the `context` row is simply absent (no empty `context` line).
- [ ] **Narrow** the terminal (or a small pane): the box falls back to the stacked layout — logo, blank
  line, then `host` / `model` / `version` only (**no** `context` row) — exactly as before. Widen again:
  it flips back to the two-column layout. The switch is clean (no wrap/overflow at the boundary).
- [ ] Eyeball the switch threshold (`startupWideMinGap = 4`): the gap at the moment it engages looks
  comfortable, not cramped — bump the constant if it feels tight.
- [ ] `/clear` and resize still keep the box present and single (unchanged from today).
