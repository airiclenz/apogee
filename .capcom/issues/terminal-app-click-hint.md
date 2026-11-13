---
title: The 'cmd+click to open' hint is false in macOS Terminal.app
status: done
type: bug
priority: 3
plan: 2026-11-13 - 00 - urgent-defects-and-doc-truth-plan
created: 2026-11-12T14:31:49Z
closed: 2026-11-13T11:14:35Z
reason: resolved by 2026-11-13 - 00 - urgent-defects-and-doc-truth-plan (archived at 14fe8a72), item 7
---

Found 2026-11-12 during the `served-pdf-csp-sandbox` browser check, with apogee v0.35.0 in macOS Terminal.app (Darwin 27.2).

**What happens:** a presentation entry ends with `cmd+click to open` under the printed URL (`internal/tui/transcript.go` `presentedStatus`; the same hint appears in `internal/tui/startupbox.go`). In Terminal.app, cmd+click on the URL does nothing while apogee runs. The owner had to turn off **View → Allow Mouse Reporting** (⌘R) to select and copy the URL by hand.

**Likely cause (not verified):** apogee turns on mouse reporting, so Terminal.app hands the click to apogee instead of opening the link. Terminal.app may also need a different gesture than cmd+click for plain-text URLs. The `presentedStatus` comment names only Zed, VS Code, iTerm2, WezTerm and kitty as linkifying terminals; Terminal.app is not among them. `TERM_PROGRAM=Apple_Terminal` identifies it.

**Possible directions (design open):** word the hint by terminal (e.g. a copy hint under `Apple_Terminal`), emit the URL as an OSC 8 hyperlink where that helps, or let apogee itself open the URL on a click it receives through mouse reporting (Local sessions only — rung 1 rules apply).

## Acceptance

- In macOS Terminal.app, a presented document's URL and path are either openable by the gesture the hint names, or the hint does not promise a click there.
- The hint stays as it is in the terminals that do linkify plain text (Zed, VS Code, iTerm2, WezTerm, kitty).
- A test pins the hint per terminal (or the neutral wording), and docs/manual/configuration.md §Showing a finished document says what Terminal.app users get.
