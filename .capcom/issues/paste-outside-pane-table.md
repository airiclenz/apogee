---
title: "Paste walks the pane table, one draft edit door (architecture review 2026-10-14 #5)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#5), re-verified 2026-11-05 at f7ec72b6. Paste and clipboard replies are still the only gestures outside the pane table: foldPaste and foldWidgetMsg (internal/tui/prompteditor.go) are hand-ordered chains (settingsPaste, projectRuleMsg, listPaste, then the box), and the draft is still edited through scattered m.input.SetValue calls in ask.go, autocomplete.go, interject.go and recall.go, each owning its own after-edit duties. The review proposes a text answer on each pane row walked like keys, clicks and the wheel, and one draft edit operation that owns every after-edit duty (selection, recall, memo, overlay policy). Its symptoms were fixed by items 13 (/workflows claims a paste) and 14 (ctrl+v recomputes autocomplete) of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md; the shape enforces ADR 0053's 2026-10-14 amendment (paste is typing), with the /settings list keeping its own adapter.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
