# Handoff — the window title shipped; VS Code's tab label is the open question

**Date:** 2026-08-03
**Status of the work:** the feature is LANDED on `main` (four commits, `1702d7b`…`bfc4837`,
unpushed). One research question is open and one manual verification pass is owed. Nothing is
broken; nothing is half-applied.

## What landed

The terminal window is now named after the session — `✭ <name>`, clipped to 30 runes, `✭ apogee`
before a session has said anything. Do not re-derive any of it from this file:

- **The plan, with per-item NOTES on every deviation:**
  `docs/plans/2026-08-03 - 00 - session-name-window-title-plan.md`. Items 1, 2, 4 are ✅ DONE, item 3
  is ⏳ OPEN.
- **The spec:** `layout.md` → "The terminal window's title" (shape, which name, what it deliberately
  omits, how it reaches the terminal, the per-terminal reach table).
- **The code:** `internal/tui/windowtitle.go` (the spelling), `Model.sessionName` + `nameSession`
  (`internal/tui/autotitle.go`), the two `v.WindowTitle` assignments in `Model.View`
  (`internal/tui/model.go`), and `title.StripEscapes` (`internal/title/title.go`, exported so the
  OSC payload answers to the same strip the naming pipeline does).
- **User-facing text:** `README.md` (Sessions section) and `CHANGELOG.md` under `[Unreleased]`.

`make check` is green. `VERSION` is deliberately untouched (a `v0.10.10` patch bump is suggested at
the foot of the plan, owner's call). The `ISSUES.md` bullet that asked for this is retired.

## The open question — and why it matters

The docs, `layout.md` and `README.md` all currently say: **in VS Code the OSC-set title only surfaces
once `"terminal.integrated.tabs.title": "${sequence}"` is set, because the tab label defaults to
`${process}`.**

**The owner reports that this is wrong in practice: Claude Code names their VS Code terminal tab
while that setting is still `${process}`.** An observation from the running editor outranks the
documentation, so the claim in our docs is now suspect and must not be defended — it must be
explained or corrected.

What was established before the research was stopped (do not redo this part):

- VS Code stores the two independently — `TitleEventSource.Process → _processName` (`${process}`)
  and `TitleEventSource.Sequence → _sequence` (`${sequence}`) — and a sequence title does **not**
  write `_processName`. Source: `src/vs/workbench/contrib/terminal/browser/terminalInstance.ts`,
  `_updateTitleProperties()`. The `TerminalLabelComputer` template-resolution logic — the half that
  would explain a fallback — was **not** read and is the obvious next stop.
- microsoft/vscode#291275, "Consider changing the default terminal tab title/description to include
  `${sequence}`", was opened by the terminal maintainer (@Tyriar) citing agentic CLIs, and closed as
  **not planned** (ref #294647). So the default was *not* changed, which deepens rather than settles
  the puzzle.
- Related, unread: anthropics/claude-code#56933 ("Allow disabling or customizing terminal tab title
  (currently locked to CC version)") and #18326 ("Propagate session name to terminal title via
  escape sequences"). #56933 implies Claude Code's title reliably reaches the VS Code tab.

### The leading hypothesis, and the cheap experiment that settles it

Apogee emits **OSC 2** (`ESC ] 2 ; title BEL`) because that is what Bubble Tea's
`tea.View.WindowTitle` compiles to (`ansi.SetWindowTitle`, hard-coded). Claude Code may be emitting
**OSC 0** — which sets the icon name *and* the window title — or **OSC 1** beside it. If VS Code's
tab label follows the icon name rather than the window title, that alone explains the whole
discrepancy.

Ask the owner to paste these into a VS Code integrated terminal (their settings unchanged) and
report which one moves the tab label:

```sh
printf '\033]2;OSC2-window-title\007'   # what apogee sends today
printf '\033]0;OSC0-both\007'           # window title + icon name
printf '\033]1;OSC1-icon-name\007'      # icon name alone
```

Thirty seconds of the owner's time beats any amount of source reading, and it also answers Zed, which
this handoff's docs claim shows the title in the toolbar but not the tab.

### If OSC 2 turns out to be the wrong sequence

Then apogee has a real change to make and it is **not** a one-liner: `tea.View.WindowTitle` is the
only supported route in Bubble Tea v2 and it is OSC 2 only. The options, in the order they should be
weighed (`AGENTS.md`: prefer the best long-term architecture over the lowest-churn patch):

1. Emit the extra sequence ourselves *beside* the framework's, from a `tea.Cmd` that writes to the
   tty — and reckon honestly with the fact that Bubble Tea diffs and resets `WindowTitle` on its
   own (`cursed_renderer.go:189, 372`), so a second writer must not fight it.
2. Keep OSC 2 and accept the VS Code tab as a documented gap (the current, already-shipped
   position).
3. Upstream a `View.IconName` / OSC 0 option to Bubble Tea.

Whatever is decided, `layout.md`'s reach table and `README.md`'s VS Code note are the two places that
must be corrected, and the plan's item 3 NOTES is where the finding belongs.

## The other thing owed: item 3, the manual pass

The executor cannot see a title bar, so the plan's item 3 is open. What *was* proved here: apogee
driven under a real pty emits exactly three OSC 2 sequences over a session's life —
`✭ apogee` at launch, `✭ <name>` after a `/rename`, and an empty one on exit (Bubble Tea blanks the
title on close rather than restoring it). So the emit path, the star, the 30-rune clip and the
change-only posture are all confirmed; only how each terminal *displays* the title is unverified.

The pty harness is at `scratchpad/ptycheck.py` in this session's temp directory — it is disposable,
and rewriting it is a five-minute job (`pty.fork`, `TERM=xterm-256color`, `APOGEE_CONFIG` pointed at
a throwaway dir whose `config.yaml` needs an `endpoint:` line or the binary exits before painting).

Still owed on the owner's own machines: Terminal.app, VS Code, Zed, Windows Terminal / `cmd.exe` —
and specifically whether the `✭` renders or shows tofu in the Windows console font. **If it is tofu,
the fallback glyph is the owner's call, not the executor's.**

## Conventions that bind the next session

- Commit straight to `main` (pre-production); commit and push only when the owner asks. Push has
  **not** happened yet — four commits are sitting local.
- No AI attribution trailers. Run `make check` before every commit.
- Never bump `VERSION` / the CHANGELOG release heading unasked — suggest it instead.
- The owner asked for sub-agents to be used where the work fans out; two ran in parallel for this
  wave (item 1's tests, item 2's code) and both reported cleanly.

## Suggested skills

- **`/implement-plan`** — if the experiment forces a code change, the plan file above is the
  resume state; add the item there and execute it item-by-item rather than patching ad hoc.
- **`/coding-standards`** — forward it to any executor touching `internal/tui` (the owner asked for
  it explicitly on this wave).
- **`/code-review`** — worth a pass over the four landed commits before the push, since the
  window-title seam is a security seam (untrusted text inside an OSC payload).
- Do **not** reach for `/refocus` or `/project-research`: everything needed is linked above.
