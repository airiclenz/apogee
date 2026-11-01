---
title: "A failed Terminal row hides its detail: no caret, and the model's error text never shown"
status: open
type: bug
priority: 2
created: 2026-11-01T10:19:20Z
---

A Terminal call that fails with empty output (seen: `capcom view x 2>/dev/null | head -60` → `error: exit 2`) renders a row with no expand caret, so the user cannot see why it failed. Its `exit 0` sibling in the same group has a caret.

Cause, traced 2026-11-01:

- The caret is dropped because the body is empty, not because the call failed. See `renderGroupMember` (`internal/tui/toolblock.go:290`: `tv.Details.len() > 0`) and the "nothing to reveal wears neither" rule (`internal/tui/theme.go:63`).
- The failure hook `subprocessFailure` (`internal/tui/toolregistry.go:813`) strips the `cwd:` line. `exitCodeMarker` (:761) then strips the whole `[exit code N — fail-fast: …]` marker. The body is `outputBody(remaining)` (:2001), which is nil for blank output.
- The model still receives the full text (`subprocessToolResult`, `internal/tools/terminal.go:404-445`: cwd, output, fail-fast note). The renderer holds it in `result.Content` but never shows it. Only the unhooked fallback `failureBody` (:2017) shows the full message.
- The same thing happens on success: a clean run with no output gets "(no output)", which "exit 0" then overwrites, so it also loses its caret.
- The spec (`docs/layout/tool-layout.md:80-89`) does not say what a hooked failure with an empty body should show.

## Acceptance

- A failed Terminal row always has a caret. Expanding it shows the output if there is any. If there is none, it shows the reason the model was given (the exit/fail-fast note), not an empty body.
- A clean run with no output either gets a caret that reveals "(no output)", or the spec says outright that it has none.
- `docs/layout/tool-layout.md` states what a hooked failure with an empty body shows, and a test pins it.
