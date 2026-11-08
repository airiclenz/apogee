---
title: Side panel changed-files key does not resolve a symlinked workspace root
status: done
type: bug
priority: 2
plan: 2026-11-08 - 01 - side-panel-followups-plan
created: 2026-11-08T13:24:46Z
closed: 2026-11-08T15:27:33Z
reason: "Resolved by plan '2026-11-08 - 01 - side-panel-followups-plan' item 2 (archived at 24c79fdb): panelWriteKey keys by real path against the workspace root's real path, so live and resumed writes fold to one row under a symlinked root; tests cover it."
---

The side panel folds each changed file under one key, live and on resume alike (`panelWriteKey`, internal/tui/sidepanel.go:912). It joins and cleans the path against `m.transcript.ws.root` and takes `filepath.Rel` (internal/tui/sidepanel.go:913-924), but never resolves symlinks. When the configured workspace root is a symlink, a live write and the resume-replayed write of the same file can produce different keys (one relative, one absolute, or two spellings), so the file shows on two rows.

Deferred out of the side-panel run (plan `2026-11-08 - 00 - tui-side-panel-plan`).

## Acceptance

- A file written live and the same file replayed on resume fold to one changed-files row when the workspace root is reached through a symlink
- A test covers a symlinked workspace root for panelWriteKey
