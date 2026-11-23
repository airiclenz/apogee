---
title: The side panel repaints every section from the whole write log every frame
status: open
type: task
priority: 3
labels: [perf, tui]
plan: 2026-11-23 - 07 - tui-perf-frame-and-local-fixes-plan.md
created: 2026-11-23T19:29:39Z
---

## Problem

Scenario: idle, many tool blocks, streaming — any frame while the side panel is shown
(`ui.side-panel.show: auto`, the default, shows it on any wide-enough window). `View`
(`internal/tui/model.go:3731`) has no early return; when the panel has a paint width it builds
`newPanelFrame` (`model.go:3802` → `internal/tui/panelframe.go:185`) every frame:

- `panelSectionsIn` (`internal/tui/sidepanel.go:622`) re-parses the `ui.side-panel.sections`
  setting (`domain.ParseSidePanelSections`) on every call;
- for the changed-files section, `panelSectionIn` → `changedFileRows` (`sidepanel.go:327`) →
  `State.ChangedFiles` (`internal/sidepanel/sidepanel.go:602-633`) makes two passes over every
  write the session ever landed (`s.writes`, append-only and never trimmed,
  `internal/sidepanel/sidepanel.go:299/306`), building two maps;
- `paintPanelRows` (`sidepanel.go:~716`) paints every row of every shown section
  (`panelFileRowIn`, `sidepanel.go:988`: lipgloss render, `ws.shorten`, `trimPathLeft`
  re-measuring per path component, `truncateToWidth`) before `panelLayout` (`sidepanel.go:524`)
  keeps only the rows that fit the column.

No memo, generation key or dirty flag sits on this path (the `panelSectionPaints` counter only
pins one paint per `View`). A mouse hit-test builds the frame again (`panelHitAt`,
`panelframe.go:263`). Each frame is O(session writes + all section rows) where O(visible rows)
would do.

Not measured: the audit's profile did not exercise the panel with live content. The multiplier
is session state (landed writes, changed files, tasks, sub-agent runs — typically tens to
hundreds), not transcript size, which is why the verifier moved it from High to Medium.

## Why

It is paid on every spinner tick (10–20 fps) and every token flush for as long as the panel is
shown, growing with every edit the session makes — CPU taken from the local model on a
Pi 5–class host.

## Suggested fix

Suggestion from the audit, output identical: memo the painted sections behind a pointer keyed on
the panel state's identity (`m.panel` is replaced copy-on-write on every write, so compare the
pointer), the content width, the fold/opened lists, the spinner frame (sub-agent rows only) and
the viewed run; and/or have `sidepanel.State` maintain the per-path rows incrementally instead
of re-deriving them from the whole write log. Paint only the body rows `panelLayout` keeps.

## Acceptance

- [ ] A benchmark in `internal/tui` with ~300 edit calls over ~150 distinct paths and the panel
      shown at 160 columns records `View` alone with the panel on vs off, before and after.
- [ ] A `View` whose panel state did not change performs no `ChangedFiles` walk and no section
      repaint — pinned by `panelSectionPaints` or a call counter.
- [ ] The panel's rows still update when a write lands, shown by a test driving the TUI through
      `internal/tuitest` + `internal/stubllm` (never an `httptest` upstream closure); rendered
      output is byte-identical.
- [ ] No tight deadline or fixed clock added (slow-box rule).
- [ ] `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` passes.

## Source

TUI performance audit 2026-11-23, verified claim 5 (filed High, verified as Medium). Report:
`docs/reviews/tui-perf-audit-2026-11-23.md`.
