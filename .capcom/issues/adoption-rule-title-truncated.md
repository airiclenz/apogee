---
title: The Adoption pane can hide the dangerous tail of a project Allow rule
status: open
type: task
priority: 3
labels: [security, tui]
created: 2026-11-27T21:27:15Z
---

`internal/tui/adoption.go:158-162,192-197` (rendered via `popup.go:533-536, 1730-1752,
1911-1919`): the pane shows the whole Allow rule only in a one-row title truncated to pane width,
with no body.

A hostile repo can lead an Allow rule with a long benign-looking prefix so the dangerous tail is cut
off before the user adopts it. The untruncated rule appears in the transcript notice above the
pane, which mitigates it.

Fix: render the full rule wrapped in the pane body, or refuse one-click adoption for a rule longer
than the pane width. Check `docs/layout/user-questions-layout.md` for the pane's spec.

Source: `docs/reviews/code-audit-2026-11-27.md` (High lowered to Medium, independently verified).

## Acceptance

- A rule longer than the pane is shown in full before it can be adopted; a driven test (`internal/tuitest`) pins it at a narrow width.
