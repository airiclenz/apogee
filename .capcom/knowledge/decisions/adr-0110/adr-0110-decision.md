---
title: ADR 0110 decision
---

**The side panel is a fixed column on the right of the transcript slot that keeps live session
state in view. Its content moves inside a column that itself changes only on a deliberate event,
and every section folds from engine Events.**

1. **Purpose: a glance, not a workspace.** The panel keeps in view the session state you would
   otherwise open a popup for or scroll back to find. The detail views (the task-list card, `/usage`,
   Run view) stay and the panel replaces none of them, so a narrow terminal loses the glance and
   nothing else.
2. **v1 sections:** sub-agents, the task list, files changed this session, and usage. MCP live state
   (`mcp-live-status-view`) and advice are later sections, each needing its Event first (decision 4).

   > **Amended 2026-11-08 (ADR 0111).** The MCP section is designed: a fifth section `mcp`, last
   > and `auto`, folded from a new journaled MCP lifecycle Event (ADR 0111 decisions 8–10).

3. **Read-only in v1.** A click on a section's body opens its detail view: Run view for a sub-agent
   row, the task-list card, `/usage`. A pin glyph in each section's header toggles that section's
   pin. The panel takes no keyboard focus. Focus and ←/→ navigation belong to the key-navigation
   work, which this panel unblocks but does not do.

   > **Amended 2026-11-08 (ADR 0111).** Superseded: the panel is the second region of navigation
   > mode (`alt+↑`/`alt+↓` enter, `→`/`←` switch region, `⏎` acts through the click's path,
   > ADR 0111 decisions 1–5).

4. **Events only.** Every section renders from engine Events, folded by a Driver-neutral fold that
   any Driver can reuse. A fact the engine does not yet emit gets an additive Event (or an additive
   field) before its section ships. For v1 that is a typed task-list Event, a per-session
   changed-files fold over `WriteTarget` and Edit regions at every depth, a start time on the
   sub-agent phase Event for elapsed time, and a context-cost Event the engine emits at each
   Turn's end with the standing rows (system content, tool menu). History is the latest context
   estimate minus those rows. This is stricter than ADR 0031 requires,
   deliberately: watch mode and a resumed session rebuild the panel from the Event journal, and a
   future Driver can show it.

   > **Amended 2026-11-11 (`adr-0110-amend-1111`).** The changed-files rows are folded from Events
   > as before, a successful `delete_file` folding as a removal; the TUI then hides a listed file a
   > disk re-check finds gone, a filter over the fold rather than a fold of Events.

5. **Placement: right side, transcript-slot height.** The column runs beside the transcript slot,
   over every row above the session-name rule. The session-name rule, status line, prompt box and
   footer keep the full width. Panes that sit in the transcript slot (`/settings`, approvals, report panes, Run view)
   take the narrower slot while the panel shows.
6. **A stable column.** The column's width and presence change only when the terminal crosses the
   width gate, or on the user's toggle. Sections grow, shrink, appear and hide inside it, and a panel
   with nothing live shows a quiet empty state instead of closing, so live content never re-wraps
   the transcript.
7. **Width and gate.** The column is 36 columns wide. In `auto` it shows while the transcript keeps
   at least 100 columns, enough for a side-by-side split diff. In `on`, a hard floor still hides it
   whenever the transcript would drop under 80 columns. The numbers are tunable in the layout spec;
   the floor rule is not.
8. **Visibility setting.** `ui.side-panel.show` is `auto | on | off`, default `auto`. The `/panel`
   command sets it (`/panel on|off|auto`) and edits sections (`/panel pin|unpin|hide <section>`). A
   key binding waits for the key-navigation work, after an audit of free keys.

   > **Amended 2026-11-08 (ADR 0111).** Superseded: the audit found no key needed. The panel is
   > reached through navigation mode, and on a window under the gate `→` or bare `/panel` opens
   > it as an overlay (ADR 0111 decisions 1 and 7).

9. **Section modes.** Each section is `pinned` (always shown), `auto` (shown while live) or `hidden`
   (never shown), in the ordered list `ui.side-panel.sections`, shaped like `ui.footer.rows`; the order is the display order.
   A pin or hide made from the panel or `/panel` is written back to the setting, the way
   `ui.task-list-open` is. Defaults: all four `auto`, ordered sub-agents, task list, changed files,
   usage.
10. **Live rules for auto sections.** Each section owns a predicate over its folded state:
    - sub-agents: while any run is running, plus a short linger after the last one ends;
    - task list: while it has an unfinished task;
    - changed files: once the session has written a file;
    - usage: once context fill passes a threshold (about 50%).
    The exact linger and threshold go in the layout spec.

    > **Amended 2026-11-11 (`adr-0110-amend-1111`).** Changed files is live while at least one row
    > remains, after deleted files are removed and files gone from disk are hidden.

11. **Overflow folds, never scrolls.** When the shown sections need more rows than the slot has,
    pinned sections get rows first, then auto sections in list order. A squeezed section folds to a
    one-line summary (`Tasks 3/7`). The panel does not scroll in v1.

    > **Amended 2026-11-09 (`adr-0110-amend-1109`).** A squeezed header wears `▶`, and opening one
    > puts it in a session-only opened tier that takes rows after pinned sections and before auto ones.

12. **Run view scoping.** While Run view shows a run, the panel follows it: that run's task list,
    usage and writes, matching the status-line gauge, which already retargets. The sub-agents section
    still lists every run.
13. **Section content.**
    - Sub-agent row: the run's name, phase, elapsed time and context fill.
    - Changed-file row: the path trimmed from the left, +/- line counts where the Tool summary's
      Edit regions carry them, and a marker when a sub-agent wrote it. Only apogee's file tools
      count; a write through `terminal` is not seen, and the manual says so.

      > **Amended 2026-11-11 (`adr-0110-amend-1111`).** A successful `delete_file` removes the
      > row instead of counting as a change, and a listed file gone from disk is hidden until it
      > exists again; a write through `terminal` is still not seen.

    - Usage: the context-fill bar and what fills it (system, tools, history), tokens in/out, cache
      hit % and spend.
14. **Replay.** Watch mode shows the panel by folding the Event journal. A resumed session has no
    journal (it is removed when the session is released), so it rebuilds the panel from the
    session record's stored entries, which keep the task list, write targets with Edit regions,
    and usage. Its sub-agents section starts empty, since those runs are over. (Owner, 2026-11-08,
    during plan writing.)
