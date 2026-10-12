---
title: ADR 0019 considered options
---

All of these were rejected; the ladder above is what survived.

- **A host back-channel** — reverse SSH tunnel, a host-side helper daemon, or path mapping over a
  shared mount as a *required* rung. **Rejected by the owner on security and dependency grounds**
  (2026-07-21) and out of scope permanently unless the owner reopens it: it turns a display
  affordance into a privileged channel into the user's machine. A `file://`-over-shared-mount
  **optional** rung was discussed and deliberately deferred, not adopted.
- **Shell out to the editor CLI** (`code <file>`, `zed <file>`) as the remote rung — rejected: Zed
  has no CLI on remotes at all, so the *primary* remote context cannot honour it, and an
  editor-specific command is a per-user preference, which is what rung 3 is for.
- **Separate tools per mechanism** (`open_document` + `serve_document`) letting the model pick —
  rejected: it puts platform detection back in a ~4B–35B model, which is the failure this tool
  exists to remove. One name, one meaning, host decides.
- **OSC 8 hyperlinks** — rejected: plain text is detected by more terminals than OSC 8 is rendered
  by, and a mangled escape sequence degrades worse than a path does. Revisit only with evidence.
- **Serving the whole workspace** from the doc server — rejected: a per-file capability token is
  the smaller, auditable grant; a workspace-rooted server is an exfiltration surface on a box the
  user may share.
- **Auto-opening on the remote box** (`xdg-open` inside a headless VM) — rejected: it fails or,
  worse, half-succeeds into a display nobody is looking at. Rung 1 is gated on a *detected*
  desktop for this reason.
