---
title: Side panel for live session state on wide terminals
status: done
type: feature
priority: 2
plan: 2026-11-08 - 00 - tui-side-panel-plan
created: 2026-11-07T21:03:21Z
closed: 2026-11-08T13:35:20Z
reason: "Resolved by 2026-11-08 - 00 - tui-side-panel-plan (archived 65efc682): all 13 items shipped; follow-ups panel-linger-idle-repaint and panel-write-key-symlink filed separately."
---

The TUI has one column: the transcript, with session state scattered across popups (`/usage`,
`/advice`), bands above the prompt, the status line and transcript notes that scroll away. A side
panel would give that state a permanent place on a wide terminal. Owner's idea (IDEAS.md, moved here
2026-11-07): "Showing information like task-lists, changed files, ... Auto-display when apogee's
width grows above a certain size."

Design is open; this issue gathers the starting position for a grill.

## Purpose first

Without a purpose there is no reason for the panel. The grill's first question is what it is for.
Candidates:

- the task list
- files changed or edited this session
- context fill and token usage (today `/usage` and the status line)
- running sub-agents and their progress (IDEAS "sub-agent gauge", "sub-agent control")
- each MCP server's live state (`mcp-live-status-view` wants "a view"; a panel section may answer it)
- staged interjections, scheduled Firings, Reactions/advice fired this session

## Owner's starting position (to grill, not settled)

- **Several sections, user-chosen:** the user decides which sections the panel may show and which
  never.
- **Dynamic by default:** the panel shows what matters most at that moment in the session (sub-agents
  while they run, the task list while one is active, …), and hides what has gone quiet.
- **Pinning:** the user can pin a section so it is always shown; the rest stays dynamic.
- **Width-gated:** visible only when the terminal is wide enough, unless that causes problems not yet
  seen (resize while open, a toggle override, what a shrinking terminal does to state).

## Constraints the design must meet

- The panel's facts come from engine Events, so every Driver (ADR 0031) could show them; the panel
  reads no TUI-only state the engine cannot see.
- Every width is measured and every row squared by the one width authority (ADR 0030); a horizontal
  panel join needs a new authority-measured join, not `JoinHorizontal`. The transcript's width
  shrinks, which touches wrapping, tool blocks, split diffs and the scroll-bar column (`layout.md`).
- The `Model` is copied by value (ADR 0011) and pinned under 32 KiB (`TestModelFitsTheStack`): panel
  state goes behind a pointer.
- A slow box (Pi, slow local model) must not pay a redraw cost it notices.
- Terminal text selection across rows breaks with a second column; copy must keep working.
- It unblocks the IDEAS item "Complete key-based navigation - including side-ways arrow keys".

Next: a pre-grill context read (`layout.md`, ADRs 0011/0030/0031, the glossary terms in play), then
`/grilling`, then an ADR and a plan in `docs/plans/`.

## Why

Session state today is scattered across popups, bands and transcript notes that scroll away; a wide terminal has room to keep it in view.

## Acceptance

- The grill has settled the panel's purpose and its section set, recorded in an ADR
- Display rules (width gate, dynamic sections, pinning, user choice) are specified in layout.md
- An implementation plan under docs/plans/ names this issue

## Log

- 2026-11-07 Airic Lenz: Grill round 1 (2026-11-07) settled: purpose = keep live session state in view, popups stay the detail views; v1 sections = task list, changed files this session, sub-agents, context/usage; read-only glance, a click opens the section's detail view, no keyboard focus in v1; right side, transcript-slot height (prompt/status/footer keep full width); fixed column — only content inside moves, column changes only on width gate or toggle, empty state instead of closing; ui.side-panel.show auto|on|off (default auto) + toggle, hard minimum always wins; every section renders from engine Events only (typed task-list Event and per-session changed-files collector first).
- 2026-11-07 Airic Lenz: Grill rounds 2-3 (2026-11-07) settled: sections have modes pinned|auto|hidden in an ordered list (like ui.footer.rows), pin/unpin persists; auto liveness per section — task list while an unfinished task exists, sub-agents while any runs (+ short linger), changed files once a file is written, usage past a context-fill threshold (~50%); panel follows the viewed run in Run view (sub-agents section still lists all); changed files = file-tool write targets at all depths (terminal writes not shown, documented); width fixed 36 cols, auto gate transcript >=100 cols, hard floor transcript >=80 cols; overflow: pinned first then list order, squeezed section folds to a one-line summary, no panel scrolling in v1; defaults all auto, order sub-agents, task list, changed files, usage, empty state when none live; pin glyph in section header toggles pin, body click opens detail view, /panel command (on|off|auto, pin|unpin|hide <section>).
- 2026-11-07 Airic Lenz: Grill round 4 (2026-11-07) settled: /panel command only in v1, key binding chosen later with the ←/→ navigation item; watch mode shows the panel and a resumed session rebuilds it from the journal; usage section = context-fill breakdown (system/tools/history), tokens in/out, cache hit %, spend; sub-agent rows = name, phase, elapsed, context fill; changed-file rows = left-trimmed path, +/- counts (if the write-target Event can carry them cheaply), sub-agent marker. Open facts for the plan: does ContextEstimateEvent carry a fill breakdown; can WriteTarget carry line counts.
