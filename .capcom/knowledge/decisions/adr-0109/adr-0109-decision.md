---
title: ADR 0109 decision
---

**Each MCP server has its own connection lifecycle. A settings save dials, re-dials or closes only
the servers it changes, off the Update loop, and the tool registry takes in the finished dials at
the next idle point.**

1. **One connection per server.** `liveMCP` (`cmd/apogee`) holds a map from server name to entry.
   Each entry owns its own one-server `mcp.Client`, dialled through the same `ConnectWith` path as
   today, so `Admit`, the repo-server pin check and the OAuth handler are reused unchanged. The
   engine still receives only the tool registry built from the live entries, and ADR 0031's
   wire-silent engine stands.
2. **A save touches only what it changes.** The new `mcp-servers` set is compared with the live one
   server by server, by an equality over the resolved `ServerConfig` (endpoint, command, args, env,
   headers and `headers-env` names, auth, pin). An added server is dialled, a removed one is closed,
   a changed one is re-dialled, and an unchanged one is left alone: no re-dial, no tool churn.
3. **Dials run off the Update loop and start at once**, mid-Turn included. A dial has no fixed
   deadline: a hung dial blocks no later edit, because a newer request cancels it (decision 6).
4. **Finished dials join at the next idle point, in one rebuild.** Every dial that has finished by an
   idle point goes into one registry rebuild in config order, committed by one `SwapTools`. ADR 0037
   decision 2's single registry-mutation path stands, and the tool order stays stable. At that
   commit each finished dial is checked again with `mcp.Admit` under the live `url-safety` lists, and
   one that is now denied is closed instead of installed.
5. **A removal or a deny takes effect on the keystroke.** The server's entry is marked closed at
   once, so a call to any of its tools is refused with `mcp server "X" is not connected` before the
   next key, mid-Turn included. Its in-flight dial, if any, is cancelled. Its process closes off the
   Update loop, and its tools leave the menu at the next idle rebuild.
6. **A newer request for a server supersedes the older one.** Each entry carries a generation. A new
   request for server X bumps X's generation and cancels X's in-flight dial. A result whose
   generation is stale is closed off the loop and writes no note: a superseded dial's
   `context canceled` is silent. Other servers' dials are untouched.
7. **A failed re-dial keeps the old session.** If a changed server's dial fails, its old session
   keeps serving and its tools stay. The same holds for a repo server whose pin moved
   (`PinMovedError`, which still cues the config re-read) and for an OAuth server that needs a login
   (`LoginRequiredError`). Mid-session nothing prompts (ADR 0095 decision 4). The note carries
   today's cause text. A newly added server whose first dial fails stays absent. A failed server is
   retried by a save that touches it, by an `$EDITOR` round trip on the `mcp-servers` row, even one that
   leaves the key unchanged (it re-dials every server whose last dial failed, keeping ADR 0037
   decision 1's "re-committing the row retries the apply"; with nothing failed, an unchanged round
   trip stays a no-op), or by a restart. A watched save, an adoption or a `url-safety` edit that leaves a
   failed server untouched never re-dials it, and there is no automatic retry.

   > **Amended 2026-11-07 (`docs/plans/2026-11-07 - 01 - url-safety-mcp-deny-gaps-plan.md`, item
   > 1).** A deny that closes a failed server's kept connection — its last dial failed, so the old
   > session went on serving — counts as touching that server, whether the deny comes from a
   > `url-safety` edit or from a request under lists that close the session's endpoint. One dial to
   > its configured endpoint is queued on the keystroke, mid-Turn included, when the verdict its last
   > request recorded and the deny's lists both admit that endpoint, no dial for it is queued or
   > running, and no successful result is held (a held failed one is superseded, so no `previous
   > connection kept` note outlives the closed session). The dial is re-admitted under the live lists
   > at commit. A server whose configured endpoint the lists deny as well is retired and dialled
   > nowhere, and a failed server whose session no deny closes still stays un-dialled.
8. **The outcome arrives per server, after the keystroke.**
   - Each server's outcome is one ephemeral transcript note under the `mcp` label: `mcp <server> reconnected` (or
     `mcp <server> connected` for a new server), `mcp reconnect failed: <server>: … — previous connection kept`, or
     `mcp connect failed: <server>: …` for a new server. A resume does not replay it.

     > **Amended 2026-11-07 (`docs/plans/2026-11-07 - 00 - mcp-reconnect-off-loop-plan.md`, items 4
     > and 11).** There is a fifth note. A finished dial that decision 4's re-admit denies at commit
     > is closed and noted `mcp server <server> disconnected — its endpoint is denied`: a policy
     > decision the human made, worded as the url-safety row words it, never as a failed dial.
   - The `mcp-servers` row shows `reconnecting MCP…` while any dial it started runs. When they
     finish, and the settings surface is still open, the row shows the outcome: `saved`, or
     `saved — live apply failed: <server>: …`.
   - On a `url-safety` row, the part that happens at once stays on the row:
     `mcp server X disconnected — its endpoint is denied`. The dial of a newly admitted server
     reports through its transcript note, like any other dial.
   - Adopting or rejecting a Project config entry returns at once and reports nothing about MCP. The
     dials it starts report through their notes, and the pin-skip note moves there too.
9. **One busy word.** While any dial runs, the busy indicator shows `reconnecting MCP…`, whatever
   the number of servers. It ranks just below `connecting…` and above a pop-up's loading row.
10. **Quit leaves nothing behind.** Teardown cancels every dial in flight, then closes every live
    server and every pending close in parallel, and waits for all of them, each bounded by its own
    close ladder. Nothing is installed after teardown begins.
11. **Startup is unchanged.** The connect at launch stays all-or-nothing and fatal (ADR 0037
    decision 7's last sentence, ADR 0095 decision 7).
12. **Scope.** This governs every Driver that applies settings live; today that is the TUI. Headless
    builds no `rootWiring` and never reconnects mid-run.
