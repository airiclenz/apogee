---
title: ADR 0109 considered options
---

- **Make the whole-set reconnect asynchronous** (the first draft plan). Every change would still
  re-dial and swap the whole set, so it needed masking of removed tools, generation counters,
  cancel-on-newer and a stash until idle, and a single broken server would still block the healthy
  ones. A per-server lifecycle removes those holes instead of patching them.
- **Per-server operations on the shared `mcp.Client`** (keyed sessions, `CloseServer`,
  `DialServer`). The client would have to become safe for concurrent use and its contract would
  change, for no gain: the engine never sees the client.
- **One `SwapTools` per finished server.** When several servers finish together that means several
  swaps, and each one breaks the cached prompt prefix again.
- **Let a removed server keep serving until idle.** A deny would not take effect before the next
  key, against the ratified call.
- **Drop a changed server whose re-dial fails.** A typo in its config would kill a working server.
- **Retry a failed new server automatically, with backoff.** More machinery and more noise for a
  case the next save, or a restart, already covers.
- **Re-dial every server on any MCP save.** It would churn healthy servers for nothing.
- **Make startup tolerate a failed non-OAuth server.** Out of scope: startup runs before the TUI and
  freezes nothing. It can be its own issue.
- **Make an adoption wait for its dials.** That brings back a wait on the user's keystroke.
- **On a deny, only cancel the in-flight dial.** That relies on every deny path finding every
  affected dial. Re-checking `Admit` at commit is the backstop.
