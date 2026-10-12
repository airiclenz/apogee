---
title: ADR 0022 context
---

Snapshot/resume is an engine capability from the start:
ADR 0001 makes the
loop steppable and snapshottable, ADR 0007
pins the quiescent boundary a snapshot is valid at, and `domain.Session` is the versioned
`{Version, State}` envelope the engine round-trips. But the *product* had none of the surface
that turns "the engine can resume" into "the user has sessions":

- The store (`internal/session.Store`) was a **write-only, quit-only stub** — one `Save`
  method that wrote a fresh `<UTC-timestamp>.json` bare envelope, no IDs, no `List`/`Load`/
  `Delete`, no update-in-place. The TUI saved **only on a clean quit**, and `--resume <path>`
  read one file directly.
- On resume the engine remembered but **the view started empty** — nothing replayed the
  scrollback, so a resumed session showed a fresh box over a conversation the model still held
  in mind. A fresh-looking view lying about a remembering engine is the exact failure the
  `/clear`-reset work pinned as forbidden.
- A run that died mid-task lost everything since the last quit (i.e. everything), and there
  was no in-TUI way to start a new session or browse old ones — the standing
  `TODO.md` P1 "Session management UI" gap, and the successor the `/clear`-reset plan
  explicitly deferred to.

The oracle is `../apogee-code` (the retired VS Code extension): one `<id>.json` per session,
saved **after every completed turn** (never on quit — in-flight turns were lost), a **dual
representation** on disk (UI entries + provider messages) so resume repaints the scrollback, a
first-user-message title heuristic with inline rename, and a history list with click-to-resume
and delete. Its VS-Code-specific plumbing (webview messages, `globalState`) does not carry
over; the shape of *what persists and when* does.

This ADR records the design the owner ratified 2026-07-24. It **extends ADR 0001** (the
live-restore variant of the snapshot/resume feature) and **supersedes nothing**.
