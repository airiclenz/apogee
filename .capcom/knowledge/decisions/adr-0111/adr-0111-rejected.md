---
title: ADR 0111 rejected
---

- **A separate panel focus key** (for example `ctrl+o`) with its own key set. It would add a second
  mode to learn beside the block cursor, and spend a free chord the one-mode design does not need.
- **Tree semantics for `←`/`→`** (fold and unfold in the transcript, panel entry on another key).
  The panel sits to the right; spatial region switching is what Crush does and what the IDEAS item
  asked for. Folding stays on `⏎` and going up a run level on `esc`.
- **`←`/`→` by position** (unfold first, then enter the panel). Less predictable for no gain.
- **A dedicated key into the panel.** `alt+↑` then `→` reaches it in two keys, and a fresh session
  lands there directly.
- **Body rows only as stops, pinning by `/panel pin`.** It would break click/key parity on the
  header's pin glyph.
- **`esc` back to the transcript region.** One `esc` meaning across regions is easier to hold.
- **A Crush-style compact header row as the narrow fallback.** A one-line summary gives a glance but
  no way to act on a row; the overlay reuses the panel whole.
- **No narrow fallback.** A narrow terminal would keep losing the panel's glance and its keyboard
  reach together.
- **An MCP Event at the idle-point commit.** State would lag a dial that ends mid-Turn, against the
  issue's acceptance that the view reflects a dial that finishes while it is open.
- **TUI-only MCP state from the existing `mcpOutcome` seam.** It breaks ADR 0110 decision 4
  (sections fold from Events only) and leaves headless, the daemon and watch mode blind.
- **`/mcp` as a picker with actions.** It would move reconnect out of settings for no new
  capability; a report pane is enough for the issue.
- **MCP section pinned by default, or not appended to a custom list.** Pinned would spend rows on
  healthy servers; not appending would hide the section from users who once reordered theirs.
- **An unread mark for runs that finished out of view.** It needs a new "seen" state per run, and
  terminal notifications already cover the case.
- **Spend replacing the fill column, or a second detail line per sub-agent row.** The first trades
  one fact for another on a 12-column budget; the second doubles the section's height.
