---
title: ADR 0111 decision
---

1. **One navigation mode, two regions.** The block cursor becomes navigation mode, over the
   transcript and the side panel. It is entered as today, `alt+↑` (last stop) or `alt+↓` (first
   stop); when the transcript has no stop but the panel has, entry lands in the panel. No other key
   enters it: the audit of free keys that ADR 0110 decision 8 waited for finds none needed.
2. **`←`/`→` switch region.** `→` moves the cursor into the panel, `←` back to the transcript. Each
   region remembers its row while the mode lasts; with none yet, the cursor lands on the stop
   nearest the row it left. `←`/`→` are claimed in navigation mode, so they no longer move the
   prompt caret.
3. **The same keys in both regions.** `↑`/`↓` move within the region, clamped, no wrap. `⏎` acts.
   `esc` leaves navigation mode from either region (`←` is the way back to the transcript). `^x` on
   a sub-agent row stops that run, as on a transcript delegation row. Printable keys end the mode
   and fall through to the prompt, as today.
4. **Panel stops are its click targets.** The panel cursor walks the targets `panelTargetAt`
   resolves, and `⏎` calls `followPanelTarget`, the click's own path, so key and pointer cannot
   drift (the rule `blockcursor.go` already keeps for the transcript). A section header is a stop
   whose `⏎` toggles its pin, as the pin glyph's click does. Rows with no target (`+N more`, a file
   row with no CallID) are not stops.
5. **The panel cursor anchors to identity.** It holds a row's identity (run, entry or section), not
   a line index, so rows appearing or vanishing do not move it. When its row goes, it moves to the
   nearest stop in the same section, else to the panel's first stop.
6. **A mark that is not colour.** Both cursors, transcript and panel, mark their row with a `▌` in
   the left gutter as well as the selection tone (ADR 0052 decision 4). This fixes the transcript
   cursor's colour-only highlight too.
7. **Narrow-window overlay.** When the panel is not drawn because the window is under its gate,
   `→` in navigation mode opens it as an overlay over the transcript's right edge, at the panel's
   width, clamped to the window. The overlay lives exactly while navigation mode is in the panel
   region: `←`, `esc`, or a click outside it closes it. Bare `/panel` opens it too, in navigation
   mode with the cursor on the panel's first stop, alongside its report. Clicks inside the overlay act as
   on the docked panel. The overlay never re-wraps the transcript, so ADR 0110 decision 6 holds; it
   cannot be resized, since the separator belongs to the docked panel. Under `off`, `→` does nothing.
8. **An MCP lifecycle Event.** The engine's Event set gains a per-server MCP Event, journaled:
   `dialling`, `connected` (with the tool count), `failed` (with the cause text) and `disconnected`
   (with the reason). It is emitted when a dial starts and when it finishes, not at the idle-point
   commit; the tools still join the menu at the next idle point (ADR 0109). Because it rides the
   Event stream and the journal, every Driver, watch mode and a resumed session see the same state
   (ADR 0031). How the dial's owner in `cmd/apogee` reaches the stream is the plan's to settle.
9. **`/mcp`, a read-only report pane.** `/mcp` lists every configured server with transport,
   source, live state, tool count and the last failure's cause, folded from that Event. It is a
   report pane like `/usage` (non-modal, scrollable, `esc` closes) and updates while open.
   Reconnecting stays on the settings `mcp-servers` row.
10. **A fifth panel section, `mcp`.** Rows read `name · state · N tools`. It is last in the default
    order and `auto`: shown while any server is dialling, failed or disconnected, hidden while all are
    connected. A user's `ui.side-panel.sections` list that does not name it gains it at the end, in
    `auto`. A click or `⏎` on the section opens `/mcp`.
11. **`waiting` on a sub-agent row.** While a run has an approval requested and not yet decided,
    showing or queued, its row's phase word reads `waiting` with its own glyph. The fold opens the
    gate on `ApprovalEvent` requested and closes it on decided, on an ErrorEvent for that gate, or
    when the run or Turn finishes. No "unread" state is kept; terminal notifications already cover a
    finished Turn.
12. **Declined.** Tokens or spend on sub-agent rows (no room at the default width; `/usage` and Run
    view's usage section carry them). A docked session switcher (`/sessions` stays a dialog, as in
    Crush and pi). A footer navigation region: every footer segment has a slash-command twin (`/git`,
    `/model`, `/usage`), and the separator has `/panel width`. Third-party panel sections and LSP
    status are out of this record's scope, not declined: the first bears on ADR 0031 and needs its
    own design, and apogee has no LSP.
