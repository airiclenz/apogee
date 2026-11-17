---
title: Windows Console output loses line breaks ConPTY paints with cursor moves
status: open
type: bug
priority: 3
created: 2026-11-17T10:04:29Z
---

On Windows, a Console's output loses line breaks that ConPTY paints with cursor movement instead of CR/LF. Seen 2026-11-17 on Windows 10.0.26300 (v0.35.3, owner-run `windows-conpty-console-check`): a fresh `cmd` Console read back as

```
Microsoft Windows [Version 10.0.26300.9711]
(c) Microsoft Corporation. All rights reserved.C:\Users\airic\Repos\apogee>
```

where cmd prints a blank line between the banner and the prompt. Both the TUI's Console Read pane and the text the model receives from `console_read` / `console_send` show the merged line.

**Likely cause (not yet reproduced):** ConPTY renders its screen buffer and often moves to a new row with a cursor-position sequence (`ESC[<r>;<c>H`, or `ESC[<n>B` and similar) rather than `\r\n`. `stripEscapes` (`internal/console/ansi.go:23`) drops every CSI sequence and emits nothing for it, so a row change becomes no separator at all. POSIX ptys pass the program's own newlines through, so the defect is likely Windows-only. Confirm by capturing the raw ConPTY bytes for a fresh `cmd` before designing the fix.

**Why it matters:** the Console family's promise is that the model reads "what a human would SEE on the screen" (`ansi.go`). Merged lines corrupt that for every multi-line Windows output: prompts glue onto the previous output, and a model parsing output line by line misreads it.

## Acceptance

- A Windows Console's read of a fresh `cmd` shows the banner, the blank line and the prompt on their own lines, in the pane and in the model-facing text.
- A test (fed recorded ConPTY bytes, so it runs on every OS) pins that cursor moves that change row read as line breaks, and that colour and same-row sequences still strip to nothing.
- No change to POSIX Console output.

## Log

- 2026-11-17 Airic Lenz: Second sample, 2026-11-17, same host: 'dir' in a fenced Console read back 'Volume Serial Number is CE0D-BB1C Directory of C:\Users\airic\Repos\apogee2026-11-17  10:41    <DIR>          .' — two newline+blank-line pairs lost. Every single newline between dir rows survived, and so did the blank line before the final prompt, which came after the output scrolled. Refined hypothesis: while ConPTY fills the first screen it skips empty rows with a cursor move (ESC[<r>;<c>H / ESC[<n>B) instead of CRLFs; once the buffer scrolls it emits real newlines. The fix must turn a row-advancing cursor move into the right number of newlines (blank rows included), not just one. The recorded-bytes test should cover both a first-screen and a scrolled output.
