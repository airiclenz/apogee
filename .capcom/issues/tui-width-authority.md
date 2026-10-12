---
title: The TUI width authority — the standing rules its work left behind
status: done
type: decision
priority: 4
labels: [standing-rule]
aliases: [apogee-gng]
created: 2026-09-03T18:40:51Z
closed: 2026-09-03T18:42:21Z
reason: "Standing rule, not work: recorded so ADR 0030's exceptions are not re-filed. Migrated from ISSUES.md 2026-09-03."
---

**Status:** standing rules from the *width authority* plan (`2026-07-31 - 03`, under `docs/plans/`,
archived on completion) —
[ADR 0030](docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md). The residue
itself is closed; see `CHANGELOG.md` for what landed. What stays here binds future work, which is
why it is not in the changelog alone.

**The rules:** the one `lipgloss.Style.Width` still in the package — the prompt box framing a
widget that wraps in GraphemeWidth itself — is ADR 0030 §6's widget-mirror exception, so do not
re-file it, and never put a `Width` style on a *bordered* surface (§5). `inputContentRows`' taller
counts need no clamping change: `promptEditor.rows` and `layout()` already clamp
(`TestPromptEditorRowsClampsTheWidgetCount`).
