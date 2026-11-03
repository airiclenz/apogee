---
title: "Reaction host: the one library module that owns a Driver root's observe lane"
refs: [internal/reactions/host.go]
---

**Reaction host**:
One **Driver** root's observe lane, owned whole: the **Reaction** Runner over the stock executor,
built in one call so no Driver can arm the lane without its safety parts. It lives in
`internal/reactions` as `reactions.Host` (`reactions.NewHost`), beside the Runner and
`DefaultExecutor`, and the public facade re-exports it as `apogee.NewReactionHost` (with the
`ReactionHost`, `ReactionHostOptions` and `ReactionHostMCP` aliases), so an embedder gets the same
lane as the binary. Each Driver root builds its own, one per Firing and one per TUI session; the
observe lane stays Driver-side (ADR 0076 A8). It carries:
the url-safety guard every webhook posts through, over the same allow and deny lists the network
tools use; the `DefaultExecutor`'s workspace exec fence, which refuses a global `run:` program
inside the workspace; the project-run late bind the Agent fills once it is built; and one close
grace, `reactions.CloseGrace`, for both a root's close and a reload's drain.
It runs in one of two MCP modes. Under **`HostMCPNone`** (every Firing — headless, the daemon, a
TUI `/schedule`) it disarms the list's `mcp:` entries before arming, one notice per entry, and its
executor holds no resolver: this is where the one-notice-per-entry rule of `adr-0076-amend-1029` is
kept. `apogee probe context` settles its `mcp:` entries through the same
`reactions.DisarmUnconnectedMCP`, without a host. Under **`HostMCPLate`** (the TUI session) the executor
resolves through a late MCP bind the Driver fills with `BindMCP` once its MCP set is dialled, and an
`mcp:` firing before then is refused as not connected yet.
`Install` composes the lane into the root's `domain.Config`: it fills `Events`, `ObserveRunner`,
`Observe` and `BindProjectRun`, and `Report` and `PinMoved` when the options give them. It leaves
two things to the Driver: its own sink wrapping, laid over `Config.Events` after `Install`, and
`Config.MCP`, which only the Driver that dials may set — the engine reads a nil resolver as a
Driver that dials no server and an unbound one as a connection not made yet. The Driver arms the
sync lane from the host's settled list (`Reactions`, `Lanes`). See ADR 0073 (`adr-0073-amend-1102`).
_Avoid_: "hook runner" (the Runner is one part of the host), "reaction wiring" (names no owner).
