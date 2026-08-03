# Plan — The terminal window wears the session's name (`✭ <name>`)

**Date:** 2026-08-03
**Status:** WITHDRAWN (2026-08-03) — archived. Items 1, 2 and 4 were executed and item 3 is
withdrawn unrun: the owner ran the shipped build and no tab or window showed the name on any of his
three terminals, so the window title was withdrawn whole by
`docs/plans/2026-08-03 - 01 - session-name-on-the-top-rule-plan.md`. What survives from this plan
is `Model.sessionName` / `nameSession` (item 1) and the exported `title.StripEscapes` (item 2),
which the top-rule wave reuses; the OSC-2 seam and every doc claim about it are gone. Kept as the
record of why the terminal's own title bar is a closed route — see item 3's NOTES.
**Track:** one `ISSUES.md` bullet — *"I'd like to see the current session's name somewhere -
could the terminal window be named (trimmed to 30 characters maybe)"*. TUI presentation and
one exported helper in `internal/title`. No engine, protocol, persistence, or Mechanism
changes; the session record's stored title is untouched — this plan only *reads* it.

**Owner-ratified decisions (2026-08-03) — implement, do not re-litigate:**

1. The window title is **`✭ <name>`** — the star leads, always, one space, then the name.
2. The **name is clipped to 30 runes** (the star and its space are chrome on top of that).
3. A session that has said nothing yet is **`✭ apogee`** — the star still leads, so an apogee
   window is identifiable before it has a name.
4. No status/spinner decoration in the title (no `⏵`, no working marker). The title states
   *which session this window is*, and nothing that changes every frame.

**Why the window and not a frame row:** every row of the frame is already spoken for
(`layout.md`), and the session name is an identity fact, not a live one — it belongs on the
chrome the terminal owns rather than on a row the transcript would pay for.

**How it reaches the terminal:** Bubble Tea v2 carries it on the frame —
`tea.View.WindowTitle` (`charm.land/bubbletea/v2@v2.0.7/tea.go:141`), which the renderer emits
as `OSC 2` (`ESC ] 2 ; <title> BEL`) and, crucially, **only when the string changes**
(`cursed_renderer.go:372`). So `View` may set it on every frame at no cost, and no escape
sequence is ever written by apogee itself. On exit the renderer clears the title
(`cursed_renderer.go:189`) — it blanks rather than restores, which interactive shells re-stamp
at their next prompt.

**Terminal reach (verified 2026-08-03), for the README note in item 4:**

| Terminal | Result |
| --- | --- |
| macOS Terminal.app, iTerm2, WezTerm, Kitty, Ghostty, Alacritty | window/tab title, no configuration |
| Windows Terminal | tab title, unless the profile sets `suppressApplicationTitle` or a fixed `tabTitle` |
| `cmd.exe` on legacy conhost | works — Bubble Tea enables `ENABLE_VIRTUAL_TERMINAL_PROCESSING` on the output handle itself (`tty_windows.go:49`), so it does not depend on the `HKCU\Console` registry default |
| VS Code | xterm.js parses it, but the tab label defaults to `${process}`; the user must set `"terminal.integrated.tabs.title": "${sequence}"` |
| Zed | parsed (Alacritty backend) and shown in the terminal **toolbar/breadcrumb**, not the tab label — [zed#19996](https://github.com/zed-industries/zed/issues/19996) is the open request; nothing apogee can do from its side |
| tmux / GNU screen | sets the *pane* title; it reaches the outer window only with `set -g set-titles on` |

**The security seam this plan must close:** the title text is untrusted twice over — it can be
a model's reply (`internal/title`) *and* a record's `Meta.Title` read back off disk, which
nothing sanitizes on the way in (`internal/tui/sessions.go:349`). A `BEL` inside an OSC payload
**terminates the sequence**, so a stored title carrying one would inject whatever follows it
straight into the terminal. `internal/tui`'s own `stripEscapes` (`transcript.go:582`) drops
only `ESC` and is therefore *not* sufficient here; `internal/title`'s unexported `stripEscapes`
(`title.go:361`) drops whole escape sequences **and** every non-whitespace control character,
which is exactly the guarantee this seam needs. Item 2 exports that one rather than growing a
second definition of "a title carries no control characters".

**ADR:** none. This is presentation; `layout.md` is its spec home (item 4). ADR 0011 (the
`Model` is value-copied — the new field is a plain `string`, which is safe) still binds. ADR
0030 (`m.th.measure`) does **not** apply: the title never enters the cell buffer, so it is
clipped in **runes**, the posture `clipRunes` already carries.

**Standing requirements:** forward skill `coding-standards` when executing
(`/implement-plan <this file> with skills: coding-standards`). Run `make check` before every
commit. Changelog bullets go under `## [Unreleased]` — never touch `VERSION` or a release
heading.

**Out of scope:** every other `ISSUES.md` bullet; the *icon*/tab title (`OSC 1`) and title
push/pop (`XTPUSHTITLE`/`XTPOPTITLE`) — one `OSC 2` is what the terminals above agree on;
showing the name inside the frame as well; a config flag to turn the title off (add one only
if the owner asks — the title is inert on every terminal that ignores it); any version bump.

File:line anchors below are as of `217b2e1` (2026-08-03) and may drift a few lines.

## 1. The active session's name becomes Model state — ✅ DONE (2026-08-03, `23be036`)

NOTES (2026-08-03): three deviations, all deliberate.

- **`nameSession` does NOT sanitize.** The plan put `title.StripEscapes` at the writer; it landed at
  the READER instead (`formatWindowTitle`, item 2), which is the seam where the guarantee is
  load-bearing — the OSC payload — and the only place a name is consumed. This also let item 1 stand
  alone without item 2's export (verified: the commit builds and tests green with item 2 stashed).
  `Model.sessionName` therefore holds untrusted text, which its field comment says outright.
- **`flushPendingTitle`'s flush branch does not "re-affirm" the name** — `applyTitle` already named
  the session when the stash was made, and nothing can change `sessionName` between the stash and
  its flush without also replacing the stash. Re-affirming would have been dead code.
- **Its DROP branch, however, needed a write the plan did not foresee.** Verification turned up a
  real divergence: `titleTouched` is session-global but set by a browser rename of ANY row, so a
  human renaming some *other* stored session while a generated title waits for an id trips the
  never-clobber rule without naming this one. The stash is dropped, the record keeps its heuristic
  title — and the window was left wearing the dropped name, which nothing on disk carried. The drop
  branch now gives the name back up when `sessionName == pendingTitle`, and only then, so a name the
  human chose for THIS session still stands. Pinned by `TestSessionNameGivesUpADroppedAutoTitle`
  (Model side) and `TestWindowTitleGivesUpADroppedAutoTitle` (window side).

**What:**

Today the `Model` never holds the live session name. The title is derived at *write* time
(`snapshotPayload`, `internal/tui/model.go:1577`) and pushed at the store, and `SessionHost.Save`
fixes it at create and ignores it thereafter — so every later name goes out through
`applyTitle` and is then forgotten by the renderer. Give it one home and one writer.

- New field on the `Model`, in the existing naming block (`internal/tui/model.go:75-87`, beside
  `autoTitleFired` / `titleTouched` / `pendingTitle` / `pendingSource`):

  ```go
  // sessionName is the ACTIVE session's name as the human last saw it decided — the title an
  // automatic or manual naming call resolved, or the one a resumed record arrived with. It is
  // display state, not the record's truth: the store owns the stored title, and this is only
  // what the frame says the window is (windowTitle). Empty means "not named yet", which the
  // window title answers from the heuristic instead. A plain string, so it rides the
  // value-copied Model (ADR 0011).
  sessionName string
  ```

- One writer, `nameSession(name string)`, next to `applyTitle` in `internal/tui/autotitle.go`.
  It is the *only* place the field is assigned, and it sanitizes at that seam
  (`title.StripEscapes` + whitespace collapse — item 2 exports it) so no caller has to remember
  to. An all-control or empty name leaves the field untouched rather than blanking the window.

- The feed points, all of which already exist:
  - `applyTitle` (`internal/tui/autotitle.go:166`) — the choke point for *both* naming routes
    (automatic `foldAutoTitle`, manual `/rename` in either form) and for the stash branch, so
    a name shows the moment it resolves even before a record id exists to rename.
  - `flushPendingTitle` (`internal/tui/autotitle.go:194`) — when it **drops** an automatic
    stash because a human named the session meanwhile, the field must not keep the dropped
    name; the human's name already went through `applyTitle`, so the drop branch needs no
    write, but the flush branch must re-affirm the flushed name.
  - `replayResumed` (`internal/tui/model.go:307`) — the `--resume`/`--continue` start: name the
    window from `r.Title`, beside the `autoTitleFired = true` latch.
  - `resumeLoaded` (`internal/tui/sessions.go:375-379`) — the `/sessions` restore: name it from
    `msg.rec.Meta.Title`, beside the same latch. Only on the success path — a failed
    `RestoreSession` returns before it, and the window must keep naming the conversation that
    is still live.
  - the browser's inline rename (`internal/tui/sessions.go:265-275`) — `r` renames *any* row,
    so it feeds the field **only when the row is the active session**
    (`id == m.sessions.ActiveID()`); renaming a stored session from the browser must not
    rename the window.
  - `startNewSession` (`internal/tui/model.go:1186-1192`) — reset to `""` with the three fields
    already reset there: a fresh record has no name, and the window says `✭ apogee` again.

**Tests:** in `internal/tui/autotitle_test.go` (the naming machinery's home) —
`TestSessionNameFollowsAppliedTitle` (automatic and manual both land on the field, including
the stash branch before an id exists), `TestSessionNameSurvivesDroppedAutoTitle` (a stash
dropped by the never-clobber rule leaves the human's name standing),
`TestSessionNameResetByClear`. In `internal/tui/sessions_test.go` —
`TestSessionNameFollowsResume` (both resume paths; a failed restore leaves it untouched) and
`TestBrowserRenameOfInactiveRowLeavesSessionName`. Every existing naming test must stay green
unmodified: this item adds state, it changes no decision.

**Acceptance:** the name a session is known by is readable from the `Model` at any moment,
from every route that can set one, and no stored title changes. `make check` green.

**Commit:** `feat(tui): the model tracks the active session's name`

## 2. `windowTitle` — the spelling, and the sanitizer it needs — ✅ DONE (2026-08-03, `4f401e6`)

NOTES (2026-08-03): `internal/title`'s `stripEscapes` had ONE in-package call site, not the three
the plan guessed, and `title_test.go` needed no change. The file-head block in `windowtitle.go` is a
section banner rather than a package comment — `package tui`'s doc comment lives in `doc.go`, and a
second one would be a duplicate-package-doc smell. `windowTitle` gates its heuristic branch on
`m.transcript.firstUserText() != ""` rather than on `sessionTitle`'s return, because `sessionTitle`
answers a dated `Session <date>` for a silent session and the window must say `✭ apogee` there; a
session that HAS spoken keeps whatever `sessionTitle` makes of it, dated fallback included, so the
window and the browser agree. `TestViewCarriesWindowTitle` reads `m.View().WindowTitle` in both the
`!m.ready` placeholder and the laid-out frame, as specified.

**What:**

- Export the strong sanitizer from `internal/title`: rename the unexported `stripEscapes`
  (`internal/title/title.go:361`) to **`StripEscapes`**, keeping its doc comment and updating
  its three in-package call sites (`Sanitize`, `title.go:291`, and any other). Extend the
  comment with the reason it is now exported: it is the one definition of "a title carries no
  escape sequence and no control character", and the window-title seam — where a stray `BEL`
  would *terminate the OSC payload* — is its second consumer.

- New `internal/tui/windowtitle.go` holding the spelling as pure functions, with a package
  comment stating the OSC-2 contract, the trust posture, and decisions 1-4 above:

  ```go
  // windowTitleRunes bounds the NAME the window title carries. The star and its space are
  // chrome on top of it (owner decision, 2026-08-03). Runes, not cells: the title never
  // enters the cell buffer, so ADR 0030's measure rule does not reach it.
  const windowTitleRunes = 30

  const windowTitleMark = "✭ "        // the star leads, always
  const windowTitleUnnamed = "apogee" // what a session that has said nothing is called
  ```

  - `windowTitle() string` on the `Model`: `m.sessionName` when it is set; otherwise the
    heuristic the first `Save` stamps — `sessionTitle(m.transcript.firstUserText())`
    (`model.go:1902`) — so a window is named from the human's opening request the instant it is
    sent, hours before any naming call answers; otherwise `windowTitleUnnamed`. The heuristic
    branch is deliberately re-derived rather than cached: it walks to the first user entry and
    stops, and a cached copy is one more thing `/clear` could leave stale.
  - `formatWindowTitle(name string) string`: sanitize (`title.StripEscapes`), collapse
    whitespace runs to single spaces (`strings.Fields` + `Join`), trim, fall back to
    `windowTitleUnnamed` when nothing survives, clip to `windowTitleRunes` with the package's
    existing `clipRunes` (`toolpresent.go:770`), and prefix `windowTitleMark`.

- Wire it into the frame: in `View` (`internal/tui/model.go:2738`), beside `v.AltScreen`,
  `v.MouseMode` and `v.Cursor` (~`model.go:2787-2790`), add `v.WindowTitle = m.windowTitle()`.
  The `!m.ready` early return (`model.go:2739`) gets it too — a window that is still laying out
  is already this session's window.

**Tests:** `internal/tui/windowtitle_test.go` — a table over `formatWindowTitle`: a plain name;
a name exactly at and one past 30 runes (clipped with `…`, never mid-rune); a CJK name (30
*runes*, not bytes); a name carrying `\x1b]2;pwned\x07`, a bare `BEL`, a bare `ESC`, a tab and a
newline (all stripped or collapsed — the assertion is that the output contains no rune for
which `unicode.IsControl` holds, the `transcript_test.go:439` posture); an empty and an
all-control name (falls back to `✭ apogee`); and that every case starts with `✭ `. Then
`TestWindowTitleFollowsSession` on the `Model`: unnamed → `✭ apogee`; after a first prompt →
the heuristic; after a naming call → the generated name; after `/clear` → `✭ apogee` again.
`TestViewCarriesWindowTitle` reads `m.View().WindowTitle` directly (the `cursor_test.go:39`
precedent) in both the `!m.ready` and the laid-out frame.

**Acceptance:** the window title is a pure function of the session's name; no control character
can reach the OSC payload from a model reply, a pasted `/rename`, or a record read off disk;
the name is clipped at 30 runes and always preceded by `✭ `. `make check` green.

**Commit:** `feat(tui): the terminal window wears the session's name`

## 3. Manual verification on the owner's terminals — ❌ WITHDRAWN (2026-08-03)

NOTES (2026-08-03, WITHDRAWN — the manual pass is moot): the owner ran the shipped build and no tab
or window showed the name on any of the three terminals he uses — Terminal.app read
`apogee apogee - 80x24`, VS Code read `apogee`, Zed read `airic - zsh`. The window title is
therefore withdrawn whole by
`docs/plans/2026-08-03 - 01 - session-name-on-the-top-rule-plan.md` item 1, which deletes
`internal/tui/windowtitle.go` and every claim that documented it; the session's name moves to the
`▔` top rule, a row apogee paints and measures itself. There is nothing left for this item to
verify. The research below is kept as the record of WHY the route was abandoned rather than
repaired — it does not need re-deriving.

NOTES (2026-08-03): the executor cannot see a title bar, so this item stays open. What COULD be
verified here was: apogee driven under a real pty (`pty.fork`, `TERM=xterm-256color`) emits exactly
three `OSC 2` sequences over a session's life — `ESC ] 2 ; ✭ apogee BEL` at launch,
`ESC ] 2 ; ✭ the window title wave BEL` after a `/rename`, and an empty one on exit (the renderer's
blank-on-close, as documented). So the emit path, the star, the name, and the change-only posture
are proven; what remains is only how each terminal chooses to DISPLAY the title, which is the list
below.

NOTES (2026-08-03, the VS Code row — RESOLVED from source, no experiment needed): the owner's
observation that Claude Code names their VS Code tab with `tabs.title` still `${process}` is real,
and the reason is not the escape sequence. VS Code **1.117** (PR microsoft/vscode#304528, merged
2026-04-13) detects a fixed set of agent CLIs by the pty's foreground **process name** —
`generalShellTypeMap` in `src/vs/platform/terminal/node/terminalProcess.ts` maps `claude`, `codex`,
`commandcode`, `copilot`, `gemini` — and `TerminalLabelComputer.refreshLabel`
(`terminalInstance.ts:2739-2741`) then swaps the user's template for `${sequence}` for exactly
those, gated by `terminal.integrated.tabs.allowAgentCliTitle` (default **true**). The global
default stayed `${process}` — the earlier attempt to change it for everyone (PR #294647, the one
issue #291275 pointed at) was merged 2026-02-11 and **reverted six days later** (`edb22b0d`), which
is why that issue reads as not-planned. So the shipped docs were right for apogee and wrong only in
implying the rule is universal; both `layout.md` and `README.md` now say why.

The OSC-0 hypothesis in the handoff is **refuted**, so the "if OSC 2 is the wrong sequence" branch
is closed and no code changes: xterm.js registers `OSC 0` → `setTitle` + `setIconName`, `OSC 2` →
`setTitle` (`InputHandler.ts:286-290`), VS Code listens only to `xterm.raw.onTitleChange`
(`terminalInstance.ts:1526`) and stores it as `_sequence` (`:2158`). `OSC 0` would therefore land
exactly where `OSC 2` already lands, and `OSC 1` reaches nothing. Bubble Tea's hard-coded `OSC 2`
stands. The only route off the setting is getting `apogee` into VS Code's table upstream — parked
in `TODO.md`, owner's call.

**What:** no code. Run apogee under each terminal the issue names and confirm what the table
above predicts, then record the outcome as NOTES on this item (this is the one item whose
result cannot be asserted by a test — `make check` cannot see a title bar):

- macOS Terminal.app — window title reads `✭ <name>`, and changes when `/rename` lands.
- VS Code integrated terminal — with `"terminal.integrated.tabs.title": "${sequence}"` set, the
  tab reads it; without it the tab keeps `${process}` (expected, not a bug).
- Zed — the terminal toolbar/breadcrumb reads it (`terminal.toolbar` shown); the tab does not.
- Windows `cmd.exe` / Windows Terminal — the title reads it, and the `✭` renders rather than
  showing tofu. **If the star does not render on the owner's Windows font**, say so in the
  NOTES and stop: the fallback glyph is the owner's call, not the executor's.

**Tests:** none (manual).

**Acceptance:** every row of the table above is confirmed or corrected in this item's NOTES,
and item 4's README note quotes the corrected version.

**Commit:** none of its own — fold any correction into item 4's commit.

## 4. The docs say what the window says — ✅ DONE (2026-08-03)

NOTES (2026-08-03): written from the researched reach table, since item 3 stays open — if the owner's
own pass corrects a row, `layout.md` and `README.md` are the two places to amend.

**What:**

- `layout.md`: a new `## The terminal window's title` section after `## The footer's upstream
  slot` (~line 504) — the frame's chrome ends at the terminal's, so it reads in place. It
  states: the title is `✭ <name>`, the name clipped to 30 runes, the star always leading; the
  unnamed session is `✭ apogee`; the title carries no live state by decision, because it is an
  identity, not a gauge; and the reach table (as corrected by item 3), because a reader
  wondering why their VS Code tab still says `zsh` must find the answer where the title is
  specified.
- `README.md`: a short note in the sessions/TUI area — the window is named after the session,
  and the one line of VS Code configuration that surfaces it. Keep it to the two facts a user
  can act on; `layout.md` holds the rest.
- `internal/tui/doc.go`: one line in the frame's inventory noting that the frame also names the
  window (`windowtitle.go`), so the next reader of `View` knows where that assignment comes
  from.
- `ISSUES.md`: retire the bullet at line 8 (delete it — the issue is closed by this plan).
- `CHANGELOG.md` under `## [Unreleased]` → `### Added`: the window title, in the house voice —
  what it says, the 30-rune clip, the star, and the VS Code/Zed caveat in one clause.

**Tests:** none beyond `make check` (docs only).

**Acceptance:** `layout.md` specifies the title, `README.md` tells a user how to see it in VS
Code, the `ISSUES.md` bullet is gone, and `CHANGELOG.md` records it under `[Unreleased]`.
`VERSION` untouched.

**Commit:** `docs(tui): specify the terminal window's title`

**VERSION-SUGGESTION:** this wave adds a user-visible feature — a patch bump (`v0.10.10`) would
be reasonable when the owner wants one. Do not bump it as part of this plan.
