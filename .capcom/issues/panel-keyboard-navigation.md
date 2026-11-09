---
title: Navigation mode spans the transcript and the side panel (←/→ switch region)
status: open
type: feature
priority: 2
created: 2026-11-08T21:33:39Z
---

The side panel is mouse-only (ADR 0110 decision 3, now superseded). ADR 0111 decisions 1–5
design its keyboard reach: the block cursor becomes one navigation mode over two regions,
the transcript and the panel. `alt+↑`/`alt+↓` enter it (landing in the panel when the
transcript has no stop), `→`/`←` switch region, and `↑`/`↓`, `⏎`, `esc` and `^x` mean the
same in both. The panel's stops are its click targets (`panelTargetAt`), and `⏎` acts
through `followPanelTarget`, so key and pointer cannot drift; `⏎` on a section header
toggles its pin, and `^x` on a sub-agent row stops that run. The panel cursor anchors to
row identity, and each region remembers its row while the mode lasts.

Today `←`/`→` in block-cursor mode are unclaimed and move the prompt caret unseen
(`internal/tui/blockcursor.go`); this work claims them.

Design: [ADR 0111](.capcom/knowledge/decisions/adr-0111.md); peer survey
[docs/design/side-panel-findings.md](docs/design/side-panel-findings.md). Covers the
IDEAS.md item "Complete key-based navigation — including side-ways arrow keys".

## Why

Every peer with a side surface reaches it by keyboard; apogee's panel is the one surface the keyboard cannot reach.

## Acceptance

- From the prompt, `alt+↑` then `→` puts the cursor on the panel's first stop; `←` returns to the transcript row the cursor left.
- `⏎` on each panel row kind does exactly what a click on it does (one driven test per kind), and `⏎` on a header toggles its pin.
- `^x` on a panel sub-agent row stops that run.
- A row appearing or vanishing above the panel cursor does not move it off its row; when its own row vanishes it lands on the nearest stop in the same section.
- `←`/`→` in navigation mode no longer move the prompt caret.
- `esc` leaves navigation mode from either region; the key legend and `docs/manual/commands.md` name the new keys.
