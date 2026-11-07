---
title: ADR 0109 consequences
---

- **ADR 0037 is superseded in part, for MCP servers only** (`adr-0037-amend-1107`):
  - decision 1's "same keystroke" and "no second moment";
  - decision 7's mid-session validate-then-commit over the whole set (its startup sentence stands);
  - decision 8's "exactly three" row annotations, which gain `reconnecting MCP…`.
  Decision 2's single registry-mutation path stands.
- **`mcp-swap-midturn-race` lands first**, as a fix of its own. This design builds on a `SwapTools`
  that a mid-Turn save can no longer race.
- `liveMCP` grows per-server entries, generations, pending closes and a teardown join. Close and
  discard never run on the Update loop. `internal/mcp` keeps its one-client-per-set shape; each
  entry simply holds a one-server set.
- A dial's result reaches Update as a message, which needs a watch route and a line in
  `internal/tui/doc.go`'s file map. The idle-point commit is a named engine-holds question
  (ADR 0011). Nothing called from Update waits on a goroutine blocked in `p.Send`.
- `applyMovedKinds` stops turning an MCP note into an `AdoptRules` error.
- The reactions `mcp:` resolver reads the live entries. A closed entry answers
  `mcp server "X" is not connected` at once.
- Tests that pin today's synchronous behaviour change, among them
  `TestReconnectSkipsARepoServerWhosePinMoved`,
  `TestWatchedBrokenProjectConfigDropsAdoptedServerUntilItParses` and the url-safety row tests
  (`TestApplySettingURLSafetyRowCarriesTheMCPLabelOnce`, `…ReportsAFailedReconnectInTheNote`,
  `…DropsAnMCPServerTheNewListDenies`).
- Prose to follow when the code ships:
  - `docs/manual/configuration.md` (live apply and `url-safety`);
  - `docs/design/mcp-client.md`;
  - the settings row annotations in `docs/layout/settings-screen-layout.md`;
  - the glossary's Busy indicator (a new wait word) and MCP client notes.
- A view of each server's live state (`/mcp`) is not part of this record. It is filed as its own
  issue.
