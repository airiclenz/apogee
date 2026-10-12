---
title: ADR 0043 consequences
---

- Implemented by `../plans/2026-08-10 - 02 - structure-split-plan.md` (items 3–15), behaviour-
  preserving throughout, one commit per item, `make check` green per item.
- **Adding a file to a mapped package now costs one line of `doc.go`** — and forgetting it fails the
  package's own test rather than rotting quietly. Any split or rename inside a mapped package
  updates the map in the same commit.
- **Navigation questions get package-level answers again**: "where is the write queue" →
  `internal/tui/sessionsave.go`; "where is the MCP client built" → `cmd/apogee/wire_mcp.go`; "where
  does a config key live" → `internal/config`.
- `git blame` on the moved code needs `--follow` / `-C`; the moves are pure, so history is intact
  behind the rename.
- File naming inside a package follows the package's own majority — this repo's is compact, no
  underscores (`internal/tui`, most of `internal/mechanisms`); the plan's item 12 brings
  `internal/mechanisms` into line and cures its confusable `syntax.go` / `syntaxcheck.go` pair.
- More files and more qualified call sites (`config.X`) is the accepted cost. The alternative —
  fewer, larger files — is the state this record exists to end.
