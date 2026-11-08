# Side panel findings — other coding agents vs. apogee

Researched 2026-11-08 (session of that date; handoff `docs/handoffs/2026-11-08 - 00 - side panel usability research and next steps.md`), then **verified the same day** claim by claim by five independent sub-agents: one against apogee's own code and docs, four against each peer's primary docs and source. Purpose: ground the planned side-panel usability work in what peer agents ship. Versions read: opencode v2.2.6 (`anomalyco/opencode`, branch `dev`), Crush `main` at `8f1f02951cabc30b82e497fd916ca434bad1e6f4`, Hermes Agent `main` (2026-11-08), pi `main`, the Claude Code docs and Warp docs as served 2026-11-08. The apogee half is summarised in the table only; the shipped truth is `layout.md` §"The side panel", `docs/layout/side-panel-layout.md`, ADR 0110 (`capcom know show "ADR 0110" --with-children`) and the glossary Note `side-panel`.

The verification pass corrected the first cut substantially. The biggest corrections: apogee already has sub-agent stop/steer (through the Run view) and per-sub-agent spend (`/usage`), and its footer already carries git and model. Several peers' panels are extensible (opencode, Hermes, Claude Code), not opencode's alone. Crush focuses its sidebar with `l`/`→`, not `tab`. opencode's v2 sidebar has its own native sections and is not keyboard-focusable.

## Comparison at a glance

"Panel" means the docked side surface. A cell that names another surface (footer, dialog, overlay) credits that surface, not the panel.

| Feature | apogee | opencode (v2) | pi | Crush | Hermes (TUI) | Claude Code (VS Code ext.) | Warp |
|---|---|---|---|---|---|---|---|
| Docked side panel | ✔ built in, right; shown in `auto` from 137 cols | ✔ right sidebar, 42 cols; auto above 120 cols | ✖ (footer + widgets + overlays) | ✔ right sidebar, fixed 32 cols | ✖ built in (bottom dock + overlays); user widgets can take side rails | ✔ panel in either sidebar or an editor tab; CLI none | ✔ desktop (Conversation Panel, vertical tabs, mailbox) |
| Narrow-terminal fallback | not drawn; bare `/panel` says why | panel becomes a dimmed right overlay | n/a | compact header + `ctrl+d` drop-down below 120×30 | n/a | n/a (VS Code layout) | n/a |
| Session list & switching | dialog (`/sessions`: resume, rename, delete, watch live) | native session tabs (horizontal, or a vertical left strip) + `/sessions`; in-sidebar list via plugin | dialog (`/resume`) + `/tree` | dialog (`ctrl+s`: filter, rename, delete) | overlay (`ctrl+x`, `/sessions`) | ✔ Activity Bar list (groups, filters) + history dialog | ✔ Conversation Panel; CLI `/conversations` |
| Sub-agent live progress | ✔ section (phase, elapsed, context fill) | composer Subagents tab, not the sidebar | ✖ (no built-in sub-agents) | ✔ section while dispatched; rows retire 5 min after finishing | ✔ live-work dock | ✔ agent map (prompt-box agent count, `/tasks`) | ✔ CLI "Agents:" tab bar; app pill bar |
| Stop / steer a sub-agent | ✔ one click → Run view: live transcript, `^x` stop, message the sub-agent | stop (composer Subagents tab) | ✖ | ✖ | ✔ steer, stop, pause, live transcript | stop + read-only transcript (no steer) | ✔ CLI: kill a child, prompt a local child |
| Task list / todos | ✔ section | ✔ Todo section | widget (extension) | pills panel above the editor (`ctrl+t`) | ✔ todo panel + `/goal` row | to-do list (kept visible in Focus view) | ✔ chip/popup |
| Files changed, per-file +N −M | ✔ `↳` marks a sub-agent's write; click → that write | ✖ in v2 sidebar (v1 had it); separate diff viewer | ✖ | ✔ Modified Files section | files read/written per agent in `/agents` (no counts) | per-edit diffs with Accept/Reject; no list | ✔ vertical-tab diff stats, CLI diff chip, code-review panel |
| Context / token / cost | ✔ section (`auto` from 50% fill, or pinned); ctx gauge + spend always in status line/footer | ✔ Context section (tokens, %, $) + Limit | ✔ footer + `/session` | ✔ model block (%, tokens, $) | ✔ status line + `/usage` | ✔ prompt-box context indicator + cache clock; `/usage` | ✔ statusline, `/cost` |
| Per-sub-agent token/cost | ✔ `/usage` row per sub-agent; panel in Run view shows that run's own; panel rows show fill % only | plugin (TokenMeter) | ✖ | ✖ | ✔ tokens/cost per subtree | tokens per sub-agent (agent map); `/usage` share by type | ✖ (not documented) |
| Git | footer (branch + counts, click → `/git`); not in panel | ✔ sidebar footer `cwd:branch` | ✔ footer | ✔ cwd + branch | ✔ status line | ✖ (CLI: custom status line) | ✔ statusline, vertical tabs |
| Model / provider | footer (host, model, effort); not in panel | ✖ in sidebar; `/models` dialog | ✔ footer (model, thinking, provider) | ✔ model, provider, reasoning | ✔ status line + `/model` | ✔ prompt-box model + effort button | ✔ statusline |
| LSP / MCP / skills status | ✖ (MCP section a named candidate, ADR 0110 d.2; waits on an MCP Event) | ✔ MCP section (per-server state, click → dialog) | `/mcp` overlay | ✔ LSP (diagnostic counts), MCP (tool counts), Skills sections | `/connectors`; banner MCP/skills sections | `/mcp` dialog; `/usage` attribution | MCP view, not in a sidebar |
| Resize the panel | ✔ drag, `/panel width`, double-click resets | ✖ fixed (toggle; `session.sidebar: auto\|hide`) | n/a | ✖ fixed (toggle) | dock expand/collapse | moved by drag; width is VS Code's | vertical tabs drag-resize; others toggle |
| Keyboard access to the panel | ✖ no focus or navigation (v1); `/panel` commands only | ✖ toggle only; not focusable | ✔ (overlays) | ✔ `l`/`→` focus when content overflows; scroll keys | ✔ | ✔ `Cmd+Esc`, palette, screen-reader navigation | ✔ |
| Third-party sections | ✖ (4 built-in sections) | ✔ `sidebar.content`/`sidebar.footer` + `session.panel` slots | ✔ widgets, footer, overlays | ✖ | ✔ widget SDK (dock rows, side rails) | ✔ mods (pane beside the conversation) | ✖ |

## Where apogee is ahead

- **Click-to-jump wiring.** A changed file opens its latest write's tool block, unfolded and scrolled to, opening the sub-agent's run first when a sub-agent wrote it. A task opens its task card; a sub-agent opens its Run view; usage opens `/usage`; the pin glyph toggles pinned/auto and persists. Crush's sidebar takes clicks only to focus it for scrolling; opencode's native rows open the MCP dialog or collapse a section, and its sessions plugin switches sessions.
- **Run view scoping.** While a Run view is open, the panel's tasks, changed files and usage are that run's own, and the viewed sub-agent is marked `▸`. No peer narrows its panel to one sub-agent.
- **Sub-agent attribution on files.** `↳` marks a file a sub-agent wrote (it does not name which one; Run view narrows the list instead). Crush and opencode v1 also show per-file `+N −M`, so the counts themselves are parity, not a lead.
- **Drag-resize with a stability promise.** Drag, `/panel width`, double-click reset to 36, persisted to `ui.side-panel.width`. The transcript re-wraps only on a deliberate act (drag, width command, threshold crossing), never on live content changes. Both terminal peers' sidebars have a fixed width (Crush 32, opencode 42); only Warp's desktop vertical tabs drag-resize.
- **Per-section modes and overflow folding.** Each section is pinned, `auto` (shown only while live: sub-agents linger 30 s, usage from 50% fill) or hidden, set in config or by the pin glyph. Under pressure, sections fold to header-with-summary rows (`◆ Tasks 2/5`) and `+N more`, so the panel never scrolls. opencode also hides sections until they have content, and Crush gates only its Sub-agents section.
- **A Driver-neutral fold.** The panel's content is a fold over engine Events (ADR 0110 d.4), so watch mode replays it from the Event journal and any Driver could reuse it. No peer documents an equivalent.

## Gaps worth considering for the usability work

Re-ranked after verification, most common elsewhere first.

1. **Keyboard access.** Crush (focus with `l`/`→` when the content overflows; `h`/`←` back; ↑↓/jk/g/G scroll), Hermes, Warp and the Claude Code extension all reach their side surface by keyboard; apogee v1 has only `/panel` commands (a documented v1 limit: ADR 0110 d.3, d.8). opencode's sidebar is not focusable either. Because apogee's panel folds instead of scrolling, focus here would mean row navigation and activation, not scrolling. Every surface a click reaches (Run view, `/usage`, task cards) is already reachable from the transcript's block cursor.
2. **Third-party sections.** Now the second most common pattern: opencode slots (`sidebar.content`, `sidebar.footer`, `session.panel`), Hermes's widget SDK (`$HERMES_HOME/tui-widgets/*.mjs`, side rails), Claude Code mods, pi widgets. This is the biggest architectural step, and it bears on ADR 0031 (no first-party connectors, Driver-neutral engine). ADR 0110 d.2 already accepts that the panel grows sections.
3. **MCP / LSP status in the panel.** Crush (LSP diagnostic counts, MCP tool counts, Skills) and opencode (per-server MCP state, click → dialog) both carry it natively. apogee's `mcp-live-status-view` issue asks for a view such as `/mcp`; ADR 0110 d.2 names an MCP section as a candidate once MCP state has an Event.
4. **Narrow-terminal fallback.** opencode turns the sidebar into a right-hand overlay below 120 columns; Crush moves its content into a one-line header plus a `ctrl+d` drop-down below 120×30. apogee draws nothing below its threshold (117 cols for `on`, 137 for `auto`), and bare `/panel` explains why.
5. **A docked session switcher.** opencode's native vertical session tabs (drag-resizable, with unread and status marks), Warp's Conversation Panel and Claude Code's Activity Bar list keep sessions docked. Crush and pi keep switching in a dialog, as apogee does (`/sessions`).
6. **Needs-attention signalling.** Claude Code's tab dots (blue: permission pending; orange: finished while hidden), Warp's per-tab state icons and notification mailbox, and opencode's session-tab unread marks. apogee's sub-agent rows show phase but have no "waiting on you" mark.
7. **Row-level detail and actions** (minor). Sub-agent rows could show tokens/spend (today only in `/usage` and in the Run view's usage section), and stop could live on the row (today one click away in the Run view).

No longer gaps: git and model rows (the default footer, directly under the panel, shows host, model, effort, workdir, branch with counts and spend); sub-agent stop/steer (Run view); per-sub-agent spend (`/usage`).

## Full per-tool findings

### opencode (anomalyco/opencode, formerly sst/opencode) — v2.2.6, verified from source

- Repo: `sst/opencode` redirects to https://github.com/anomalyco/opencode (default branch `dev`). Current docs live on https://opencode.ai/docs/ (keybinds: https://opencode.ai/docs/cli/keybinds). docs.opencode.ai serves the legacy v1 pages. v1.x and v2.x are different TUIs.
- Sidebar: right side, 42 columns. `<leader>b` (leader defaults to `ctrl+x`; v2 id `session.sidebar.toggle`, v1 `sidebar_toggle`). `session.sidebar: auto|hide` shows it automatically above 120 columns; narrower, it opens as a dimmed right-hand overlay. It is hidden in child (sub-agent) sessions. Source: `packages/tui/src/component/session-frame.tsx`, `packages/tui/src/routes/session/sidebar.tsx`.
- v2.2.6 sections (`packages/tui/src/feature-plugins/sidebar/`): session title; **Context** (tokens, % used, $ spent); **Limit** ($ left on a workspace limit, hidden when none applies); **MCP** (per server: coloured dot and Connected / Connecting / Error / Disabled / Sign in; click opens the MCP dialog); **Todo** ("N remaining"); footer with `cwd:branch` plus a "Getting started" card until a provider is connected. v1.19.5 also had **LSP** and **Modified Files** (per-file `+N -M`) and showed the version in the footer.
- Behaviour: each section shows only while it has content; a list with more than two items gets a mouse-collapsible header, and the collapsed state is remembered.
- Interactions: not a focus target (focus is forced back to the session; `session-frame.tsx`). Collapsing, MCP details and location actions are mouse clicks; scrolling is by mouse wheel. Keyboard access is the toggle only. Fixed width.
- Plugin slots (https://opencode.ai/docs/build/plugins/cli, §Slots): `sidebar.content` and `sidebar.footer` with placement `prepend | append | before | after | replace`, plus `session.panel` for plugin-owned docked panels ("the host owns sizing, focus, and full-screen presentation"). v1 used `sidebar_content` / `sidebar_title` / `sidebar_footer`, documented only in the in-repo `specs/tui-plugins.md`.
- Community plugins:
  - https://github.com/Mangome/opencode-sessions-sidebar — the 10 most recent sessions, click to switch, `•` marks the active one, a braille spinner while generating, collapsible header, `/sessions-count`. Needs OpenCode 2.x (1.x users stay on 0.1.x). Its README, not the plugin API, is the source of the "native look" wording.
  - https://github.com/njbraun/opencode-plugin-session-token-summary — one combined total over root and sub-agent sessions, not a per-agent breakdown.
  - https://github.com/jonasotoaguilar/opencode-tokenmeter — TokenMeter: live tokens, cost, and per-agent groups (`↳ agent (N tasks)`) in the v1 `sidebar_content` slot.
- Other native surfaces: **session tabs**, horizontal by default or a vertical left strip with `tabs.layout: "vertical"` (Ctrl+Tab, Ctrl+1…0, `<leader>1-9`; unread and status marks; the strip drag-resizes from 5 to 72 columns and remembers its width). A **composer Subagents tab** (Running/idle; Enter jumps; `ctrl+d` interrupts), alongside shells and terminals. A drag-resizable **terminal pane** on the right (`<leader>left` / `<leader>right`). A **diff viewer** with a file tree, scope choice (All / Committed / Uncommitted / Last turn) and mark-reviewed (`m`). Dialogs: `/sessions` (`<leader>l`), `/models` (`<leader>m`), command palette (Ctrl+P); F2 cycles recent models, Ctrl+O opens recent sessions and projects.

### pi (earendil-works/pi, formerly badlogic/pi-mono)

- No docked side panel: a single-column transcript plus editor, footer, widgets and overlays. Sources: `packages/coding-agent/docs/` (extensions.md, tui.md, sessions.md, keybindings.md, usage.md) on https://github.com/earendil-works/pi. No doc says pi avoids a sidebar on purpose; the README says pi "skips features like sub-agents and plan mode".
- Footer (`src/modes/interactive/components/footer.ts`): `cwd (branch) • session name`; tokens `↑in ↓out`, cache `R`/`W`/`CH%`, `$cost` (marked "(sub)" on a subscription); context `N%/window`; `model • thinking level`, with `(provider)` when more than one provider is configured; a line of extension status texts (`ctx.ui.setStatus`). An extension can replace the footer (`ctx.ui.setFooter`) or header (`setHeader`).
- Widgets: `ctx.ui.setWidget(id, lines | (tui, theme) => Component)` above the editor, or `placement: "belowEditor"`. The plan-mode example extension uses one (`setWidget("plan-todos")` plus a `📋 N/M` footer status); there is also a `todo.ts` example. The docs present widgets as display content and route interactive UI through `custom()`.
- Overlays: `ctx.ui.custom(..., { overlay: true, overlayOptions: { anchor: "top-right", width: "50%", margin: 2 } })`. This is marked "(Experimental)". An overlay can stay visible after releasing input (`handle.unfocus`) and can be shown or hidden by terminal size, which makes it the nearest pi gets to a docked pane.
- Pickers: `/resume` (search, rename Ctrl+R, sort Ctrl+S, path toggle Ctrl+P, delete Ctrl+D or Ctrl+Backspace on an empty query, named-only filter Ctrl+N); `/tree` session-tree navigator with filters; model selector Ctrl+L, model cycling Ctrl+P, thinking-level cycling Shift+Tab; `/mcp` overlay (status, sign-in, reconnect, enable/disable); `/hotkeys`. `/session` reports file, ID, message count, tokens and cost.
- Input: keyboard-driven with user-editable `~/.pi/agent/keybindings.json`; fullscreen mode routes mouse events to components.

### Crush (charmbracelet/crush) — verified from source at `8f1f0295`

Paths are under `internal/ui/model/` unless stated otherwise.

- **Sidebar**: permanent right-hand panel, fixed `sidebarWidth := 32` (`ui.go:4564`), hidden when no session is active (the landing view then shows cwd + branch, the model block and LSP/MCP/Skills in the main area: `landing.go`).
- **Compact mode**: `ctrl+b` calls `toggleCompactMode`, the same switch as `tui.compact_mode`, and saves it to the global config. It works only with an active session at 120×30 or larger. Below 120 columns or 30 rows Crush goes compact on its own. In compact mode the sidebar's content moves rather than disappears:
  - a one-line header shows branch, cwd, running sub-agent count, LSP error count, context %, and a `ctrl+d` hint (`header.go:134-206`);
  - `ctrl+d` (compact mode only) drops down a details panel with title, model block, Files/LSP/MCP/Skills/Subagents columns and the version.
- **Sections, top to bottom** (`sidebar.go:79-128`):
  1. Logo, fixed and not scrolled; a small variant under 30 rows.
  2. Session title, at most 2 lines.
  3. Branch line, then cwd.
  4. Model block: model "via provider", reasoning/thinking setting, `NN% (tokens) $cost` with a warning icon above 80%, Hypercredits on Hyper. It does not show the window size.
  5. Subagents, only once dispatched: spinner, elapsed time, label; finished rows retire after 5 minutes.
  6. Modified Files: per-file `+N -M`, non-zero only, newest first (`session.go:194-247`).
  7. LSPs, with per-server error/warning/hint/info counts.
  8. MCPs, with per-server tool/prompt/resource counts and error text.
  9. Skills.

  Sections 6–9 always render, showing "None" when empty.
- **Interactions**: focus with `l`/`→` from chat, or a click, only when the content overflows (`ui.go:3638-3641`, `2055`). `tab` only switches editor ↔ chat and never enters the sidebar. Focused, ↑↓/jk scroll 4 lines, g/Home and G/End jump, `h`/`←` returns to chat, and the wheel scrolls it. Virtual scrolling with an auto-hiding scrollbar that stays while focused; focus returns to chat once the content fits. Otherwise read-only: no item click, rename, switch, drag or resize anywhere in `ui.go`.
- **Sessions dialog** (`ctrl+s`; `internal/ui/dialog/sessions.go`):
  - type to filter; ↑↓ (or ctrl+p/ctrl+n) navigate, wrapping; mouse wheel scrolls;
  - enter, tab or ctrl+y chooses; double-click within 400 ms opens, a single click selects;
  - `ctrl+r` renames; `ctrl+x` deletes, confirmed with y/enter, and a busy session refuses with "Agent is busy".
- **Other keys** (`keys.go`): `ctrl+n` new session (idle agent only), `ctrl+l`/`ctrl+m` model picker, `ctrl+t`/`ctrl+space` toggle the pills panel above the editor (todos and the prompt queue; help text "toggle tasks"), `ctrl+p` command palette, `ctrl+g` full help.
- **Docs**: the repo's `docs/` holds only `config/` and `hooks/`; there is no root CHANGELOG. The README names `tui.compact_mode` (in a Nix example) and the session picker but never describes the sidebar; `docs/config/README.md` documents `option ui compact true`.
- **Pattern**: an info sidebar with focus-to-scroll, a compact fallback, and a separate modal sessions picker.

### Hermes Agent (Nous Research)

- A Python agent with two front-ends. The **TUI** (`hermes --tui`) is TypeScript/React on a forked Ink (`@hermes/ink`, Node 20+, `ui-tui/`) over the Python gateway. The **Classic CLI**, still the shipped default, uses prompt_toolkit. Docs: https://hermes-agent.nousresearch.com/docs/user-guide/tui; source: https://github.com/NousResearch/hermes-agent.
- No built-in sidebar. A **widget SDK** (`ui-tui/src/sdk/`; user widgets in `$HERMES_HOME/tui-widgets/*.mjs`, hot-reloaded, `/widgets-reload`) places "ambient" widgets in dock-top/dock-bottom rows or in left/right **side rails** that reserve a column (default 44) beside the transcript, which reflows around them. It also supports modal widgets.
- **Live-work dock**: a strip above the composer showing sub-agents and a Processes block, keeping the composer draft. Ctrl+T expands it to the full-height `/agents` roster (alias `/tasks`). Per worker: Enter/`t` live transcript, `d` details, `e` steer, `x` stop, `p` pause, `X` kill the subtree. In the roster: `s` sorts, `f` filters, `[`/`]` step through history, j/k/g/G navigate, `q`/Esc close. Ctrl+R (or F7) toggles the dock between preview and a one-line summary.
- `/agents` rolls up tokens and cost per subtree (`lib/subagentTree.ts`), plus per-agent lists of files read and written (no `+N −M`).
- **Status line**: agent state, cwd (replaced by the session title at the right edge), live git branch, per-prompt elapsed time, auto-compression count, background-task count, YOLO badge. In source (`appChrome.tsx`) it also shows model, context fill bar, cost, voice, live-session count, sub-agent count, cache hit, latency and tokens/s; segments drop out as the terminal narrows.
- A standing `/goal` row above the dock (`⊙ goal · 3/20 turns · 2 subgoals`, plus gates; parked ⏳ and paused ⏸ states show a reason). Alongside it sit a collapsible todo panel and a loop bar.
- **Live session switcher**: Ctrl+X, `/sessions`, `/switch`, `/sessions new`, or a click on "N live sessions". ↑↓ or mouse select, Enter switches, Ctrl+D closes, Ctrl+N starts blank, Ctrl+R refreshes, Tab picks a model for a `+new` row. Resumable history rows can be deleted with `d`. Ctrl+X deletes a highlighted queued message instead of opening the switcher.
- **Overlays**: `/model` (grouped by provider, cost hints; also Ctrl+O), `/usage`, `/agents`, `/details` (tool-call detail globally or per section), `/help`, `/connectors` (accounts: rename, add, reconnect, remove), `/skin`, `/reload`, `/mouse`.
- Also: a gateway-durable queued-messages panel (`queued (N)`, seen by every attached client); a startup banner with four sections (Tools open; Skills, System Prompt and MCP Servers collapsed); Esc-Esc transcript rewind (Enter edits and truncates, `f` forks a session from just before).
- **Hermes Desktop** is first-party (https://hermes-agent.nousresearch.com/docs/user-guide/desktop): a chat-first window with a left sidebar, a Projects sidebar of grouped sessions, and dockable panes. `nesquena/hermes-webui` is a separate third-party web UI.

### Claude Code (VS Code extension; CLI noted where it differs)

Source: https://code.claude.com/docs/en/vs-code unless noted; features below are extension-only unless marked.

- **Panel placement**: move it by dragging its tab or title bar to the secondary (right) sidebar, the primary (left) sidebar or the editor area; Claude remembers the location. `preferredLocation` picks where it opens: `sidebar` (right sidebar) or `panel` (the default, an editor tab). Width follows VS Code's own view sizing. There is also a `lockEditorGroups` setting.
- **Sessions list in the Activity Bar** (Spark icon): named collapsible groups (v2.1.229 or later; per workspace folder), filters Active / Needs input / Working / Completed / Open / Closed (v2.1.271 or later; "Open" includes a session running in a terminal), and an "Account & usage" header with the active session's totals.
- **Session history** (button at the top of the panel): searchable, with Local and Web tabs (the Web tab limited to the open repo when it is a GitHub repository), rename and archive on hover. `archiveInactiveSessions` defaults to 14 days (1, 2 or 7; 0 turns it off) and skips open, unread and grouped sessions.
- **Agent map**: opened by clicking the agent count ("2 agents", with a status dot) at the bottom of the prompt box, or `/tasks` (v2.1.277 or later). It shows a sub-agent tree with status, elapsed time and token count. A click shows a sub-agent's prompt and tool calls, opens its read-only transcript, or stops it. Background shells and monitors are listed below, each with live output and a stop control. There is no steering. Live progress rows also sit under the tool call that started a sub-agent, with "Run in background" (v2.1.287 or later). The CLI's `/tasks` is a list of background work, not a tree.
- **`/usage`** (CLI and extension; claude.ai plan sign-ins only): account and plan bars, a percentage share by skill / sub-agent type / plugin / MCP server, a Day/Week toggle (CLI `d`/`w`), and fix suggestions (CLI `f`; the extension's "Fix …" button needs v2.1.305 or later). Other sign-ins see only the session's own cost and tokens.
- **Prompt box**: context indicator, prompt-cache clock, a permission-mode indicator, and a model + effort button that opens the picker.
- Also:
  - side-by-side diffs with per-change Accept/Reject in Manual mode (none on a diff with more than 100 changes);
  - plan mode as a Markdown document with inline comments;
  - `/bookmarks` panel (v2.1.286 or later);
  - `/btw` answers in a panel beside the chat (in the CLI, not a side panel);
  - Focus view (`Ctrl+Option+F` / `Ctrl+Alt+F`): hides tool calls, results and thinking, and keeps the latest to-do list visible;
  - checkpoints on hover (fork, rewind code, or both).
- **Tabs and windows**: parallel tabs and windows, each with its own history. Tab dots: blue = permission pending, orange = finished while hidden.
- **Keyboard**: Cmd+Esc / Ctrl+Esc toggles editor ↔ Claude; Cmd+Shift+Esc opens a new conversation tab; Cmd+Shift+T reopens a closed one. Screen-reader navigation: turn headings, Tab into the transcript, "Claude Code: Focus last message".
- **Mods** (https://code.claude.com/docs/en/plugins/mods/overview; CLI, Desktop and the extension from v2.1.299): a plugin can draw a pane beside the conversation or above the prompt, a toast, a status line, or its own rendering of a message or tool call. The panel labels such content "From the X plugin". The CLI's built-in `/diff` pane is itself a mod.
- **CLI surfaces**:
  - `statusLine` (https://code.claude.com/docs/en/statusline): a user script receives JSON with model, workspace, git worktree, cost, context window, prompt cache and rate limits;
  - `claude agents` (https://code.claude.com/docs/en/agent-view): a full-screen list of background sessions grouped Pinned / Needs input / Ready for review / Working / Completed (Ctrl+S groups by directory). `Space` peeks, `Enter`/`→` attaches, `←` detaches, `Ctrl+X` stops. This is the closest terminal analogue to a session panel.

### Warp

Desktop app (https://docs.warp.dev, agent pages collected in https://docs.warp.dev/_llms-txt/agents.txt):

- **Conversation Panel** (left): Active and Past sections (title, age, working directory), click to switch (a past one opens in a new pane), a search field and "New conversation". Toggle with Ctrl+Shift+H. ⌘Y opens a conversation selector; on Linux/Windows the docs disagree between Ctrl+Y and Ctrl+Shift+Y.
- **Vertical tabs**: a resizable left panel showing each pane's agent status, branch, diff stats, PR link and a hover details sidecar; configured under `[appearance.vertical_tabs]`. This is where third-party CLI agents show up, together with the agent toolbelt and Remote Control.
- **Agent Management Panel** (https://docs.warp.dev/platform/managing-cloud-agents/): interactive Warp Agent conversations and cloud runs, Personal/All tabs, filters by source, creation day, creator and status. Rows show source, status, and duration (cloud runs only); local orchestration children are not separate rows.
- **Notification mailbox** (bell; a sidebar panel): All tabs / Unread / Errors (hidden when empty), mark all read, click to jump. Keys: ↑↓, Enter, Shift+Tab cycles filters, Esc. Per-tab state icons: working / blocked / completed / errored, plus an attention badge.
- Also:
  - a task list chip (bottom-right when input is pinned to the bottom, otherwise along the right) opening a collapsible popup;
  - a Code Review panel (⌘⇧+ / Ctrl+Shift++, or `/open-code-review`) with inline comments and batched send;
  - plans in the Warp Drive side panel, plus a plan panel toggle (⌘⌥P / Ctrl+Alt+P);
  - an orchestration pill bar above the agent view.
- **Third-party CLI agents** never appear in the Conversation or Agent Management panels. Recognised: Claude Code, Codex, OpenCode, Amp, Auggie, Copilot CLI, Cursor CLI, Gemini CLI, Droid, Pi, Goose, Antigravity, Hermes (the "also recognized" tier, no notifications yet), Mistral Vibe, Grok Build, Kiro CLI. Crush is not mentioned anywhere in the docs.

Warp Agent CLI (`warp`, bare terminal):

- No side panel; slash-command panels instead.
- `/statusline`: auto-approve toggle, model, cwd, git branch status (with ahead/behind), git diff status, GitHub PR, credit usage, context-window usage, to-do chips, Vim-mode indicator, optional date/time/voice.
- An "Agents:" tab bar during orchestration. Shift+↑ focuses it; Tab/→ and Shift+Tab/← cycle; Shift+←/→ jump to the first/last child; ↓ returns to input; Esc returns to the orchestrator. Ctrl+C kills the selected child (once from the tab bar, twice while viewing a child). Local children also take follow-up prompts and approvals.
- `/conversations` or `←` on empty input opens a conversation menu filtered by title.
- `/cost` toggles credit-usage details for the latest response.
- `/status` opens a read-only panel: version, conversation name and ID, cwd, organisation, email.

## Cross-cutting observations

1. **The bare-terminal agents split two ways.**
   - Docked sidebar: opencode and Crush.
   - Footer or status line plus overlays: pi, Hermes, Warp CLI and the Claude Code CLI. Hermes and Claude Code let user widgets or mods dock panes anyway, and pi's experimental non-focused overlay comes close to one.
   - Session switching stays out of the sidebar in both terminal sidebars: Crush uses a modal dialog, and opencode uses its own session-tabs strip (a separate docked surface) with an in-sidebar list only via plugin.
2. **Sessions and token/cost are the most common side-surface contents.** Sub-agent trees with per-branch rollups appear in Hermes `/agents` (tokens and cost per subtree) and Claude Code's agent map (tokens per sub-agent, no cost).
3. **Extensibility is the norm, not the exception.** opencode (documented slots), Hermes (widget SDK with side rails), Claude Code (mods) and pi (widgets, footer, overlays) all let third parties draw into the UI. Crush, Warp and apogee have fixed section sets.
4. **The two terminal sidebars both have a narrow-terminal fallback** (opencode overlay, Crush compact header + drop-down), and both have a fixed width. apogee instead hides the panel and makes the width user-adjustable.
5. **Keyboard reach varies.** Crush focuses its sidebar only when it overflows (focus exists to scroll); opencode never focuses its sidebar; the overlay-based agents are keyboard-first by construction.
6. **Oddity:** Warp recognises Pi and Hermes as third-party CLI agents but never mentions Crush.
