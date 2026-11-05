---
title: "Prompt box background is not uniform: rows and cells paint different shades"
status: open
type: bug
priority: 2
labels: [tui]
created: 2026-11-05T15:08:02Z
---

The area around the prompt box does not paint one uniform background. With a black colour scheme (screenshot 2026-11-05 16.02.44), the rows above the prompt box and the footer rows show bands of a slightly lighter shade next to true black. The footer's second row (`~/repos/apogee ✦ main 0Δ 0↓ 0↑`) paints the lighter band only as wide as its text, and the first row's right end (after `ask before`) shows a short band of the other shade too. So some cells take the theme's background and others take the terminal default (or an unset/other style), per row and per span.

Specs: `layout.md`, `docs/layout/prompt-box-layout.md`. Code to start from: `internal/tui/footerfit.go` (footer painter), `internal/tui/editorgeometry.go` (prompt box).

## Why

A patchy background reads as a rendering bug and makes the bottom of the screen look broken on dark themes.

## Acceptance

- Every cell of the prompt box, the rows around it and both footer rows paints the same background colour under the active theme, to the full terminal width
- A frame-judging test (docs/design/test-drivers.md) pins the uniform background across those rows, including padding past the end of a row's text
