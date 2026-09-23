# Plan — copy reaches the clipboard inside tmux

**Goal:** a drag-select copy in the TUI lands on the outer terminal's clipboard when apogee runs
inside tmux on its default `set-clipboard external`, and a driven test pins the OSC 52 bytes apogee
writes so the copy path cannot silently die again.
**Date:** 2026-09-23
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** a4874a81
**Closes:** apogee-tmux-copy-dropped

**Regression check (2026-09-23, a4874a81):**
- 1: guard folded — the new tests reach the Acceptance filter; the channel-prose grep also catches channel counts.
- 2: guard folded — the real system-clipboard route is neutralised; the recorder is wired before the first launch.

**Sources:**
- bead `apogee-tmux-copy-dropped` (diagnosis and the tmux probe)
- `internal/tui/clipboard.go`, `internal/tui/mouse.go` (`copyFlash`), `internal/tui/doc.go` (copy prose)
- tmux 3.5a `input.c` `input_osc_52`: an application's OSC 52 is honoured only under `set-clipboard on`; `load-buffer -w` is tmux's own write, honoured under `external` too
- `docs/design/test-drivers.md` (driving the TUI in `go test`)

**Ratified design calls:**
- **Mechanism:** inside tmux (`TMUX` non-empty) `copyFlash` also pipes the text to `tmux load-buffer -w -`; OSC 52 (`tea.SetClipboard`) and the system-clipboard write stay. Owner, 2026-09-23.
- **No warning / no DCS passthrough:** no user-facing tmux notice and no `\ePtmux;` wrapping. Owner, 2026-09-23.
- **Regression test:** a `cmd/apogee` driven test asserts the OSC 52 bytes on the program's output for the prompt and for a transcript row after `--continue`. Owner, 2026-09-23.

**Standing requirements:**
- skills: coding-standards
- Any authorised deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Detecting or rewriting the user's tmux options; screen/zellij routes.
- The system-clipboard (atotto) route's behaviour.

## 1. The copy hands the text to tmux when apogee runs inside it

**What:** fix for `apogee-tmux-copy-dropped` (a defect, not a regression): inside tmux on its
default `set-clipboard external`, tmux drops the OSC 52 apogee writes, so "copied N chars" flashes
and nothing pastes.

**Regression guard.** Every new test is named so Acceptance's `-run` selects it (`TestTmuxClipboard…`),
and the Acceptance filter also gains `|Tmux`. The channel-prose rule's grep also matches channel
counts — `BOTH channels\|two channels\|two clipboard` (mouse.go:835 `copyFlash`, clipboard.go:15,
mouse_test.go:308 `fireBatch`) — and each hit that counts the copy channels is updated to include tmux.

**Goal:** when `TMUX` is non-empty, every copy `copyFlash` makes (prompt, transcript, /settings
field) also runs `tmux load-buffer -w -` with the copied text on stdin; with `TMUX` empty or unset
no tmux process is started; the OSC 52 and system-clipboard routes are unchanged.

**Approach (assumed at the header base):** in `internal/tui/clipboard.go` add a second
package-level seam beside `writeSystemClipboard` (e.g. `writeTmuxClipboard func(string) error`),
whose real value reads `TMUX` via `os.Getenv` at call time, returns nil without spawning when it is
empty, and otherwise runs `exec.CommandContext` of exactly `tmux load-buffer -w -` under a 2-second
timeout with the text as stdin. Wrap it in a Cmd shaped like `systemClipboardCmd` (best-effort,
error swallowed, yields nil) and add it to `copyFlash`'s `tea.Batch`. Keep the gate testable
without touching the process env: the real seam is a thin composition over a helper that takes
`getenv` and a runner, and unit tests drive that helper. `recordSystemClipboard` (mouse_test.go)
also substitutes the tmux seam so no existing copy test can spawn a real `tmux` on a developer's box
running the suite inside tmux. Update the prose that names the copy channels — rule: every comment
or doc passage stating which channels a copy uses (grep `internal/tui` and `docs/manual` for
`OSC52\|OSC 52\|system clipboard\|clipboard program`) — to name the tmux route, and add one sentence
to the manual where drag-select is described (`docs/manual/commands.md`) saying a copy inside tmux
also goes through `tmux load-buffer -w`, and that `set-clipboard off` blocks it.

**Files:** internal/tui/clipboard.go; internal/tui/mouse.go; internal/tui/doc.go; internal/tui/mouse_test.go; internal/tui/clipboard_test.go; docs/manual/commands.md
**Read first:** internal/tui/mouse.go — copyFlash; internal/tui/clipboard.go — writeSystemClipboard, systemClipboardCmd;
internal/tui/mouse_test.go — recordSystemClipboard, fireBatch, TestDragCopyAlsoWritesTheSystemClipboard;
internal/tui/seams_guard_test.go — TestNoParallelTestSwapsAPackageSeam, packageSeams

**Tests:** (every new test named `TestTmuxClipboard…` so the Acceptance filter runs it)
- helper with `getenv` returning "" → runner never called; with `/tmp/tmux-1000/default,1,0` → runner called once with argv `tmux load-buffer -w -` and stdin equal to the text (multi-byte text included).
- a copy (prompt drag) through `copyFlash` hands the exact selection to the tmux seam (serial test, recorder helper).
- a runner error is swallowed: the Cmd returns nil and the flash still shows.

**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `go test -race -count=1 -run 'Clipboard|Copies|Copy|Seam|Tmux' ./internal/tui/`

**Commit:** `fix(tui): a copy inside tmux also goes through tmux load-buffer -w, which tmux forwards on its default set-clipboard`

## 2. A driven test pins the OSC 52 bytes a copy writes

**What:** test-only guard for the copy path: the existing copy tests assert only a non-nil Cmd and
the flash, so nothing checks the bytes that reach the terminal. Depends on item 1.

**Regression guard.** The driven copy runs the real atotto route (cmd/apogee cannot reach `writeSystemClipboard`): `t.Setenv` `DISPLAY` and
`WAYLAND_DISPLAY` to "" (cf. `presentRemote`, e2e_present_test.go:204); on darwin point `PATH` at an empty dir (atotto execs `pbcopy`) or skip;
on windows skip (atotto writes via user32). The recorder is supplied BEFORE the first `start` (launch helper / `startSession` parameter; `start`
hands `tui.Build` `io.MultiWriter(drv.Output(), rec)`), since `start` waits for the first frame, and is mutex-guarded for `-race`.

**Approach (assumed at the header base):** in `cmd/apogee`, a new e2e test drives a real launch
through the existing harness (`launchTUI`/`e2eSession.start`, `tuitest.Driver`), recording every
byte the program writes by teeing the writer `start` hands `tui.Build` (add an optional recorder to
`e2eSession`, off by default so no other test changes). Drive SGR mouse input (`\x1b[<0;x;yM`,
`\x1b[<32;x2;yM`, `\x1b[<0;x2;ym`) over typed prompt text, assert the recording contains
`"\x1b]52;c;" + base64(selection) + "\x07"` and the "copied N chars" flash; then
`RelaunchWith("--continue")` and drag over a restored transcript row, asserting the same shape for
that row's text. The test is serial and sets `TMUX` to "" with `t.Setenv` so the item-1 route never
spawns the developer's real tmux; it names the tmux case as out of its reach (a
multiplexer between apogee and the terminal is invisible to an in-process test) in its doc comment.

**Files:** cmd/apogee/e2e_copy_test.go; cmd/apogee/e2e_support_test.go
**Read first:** cmd/apogee/e2e_support_test.go — e2eSession.start, startSession, RelaunchWith; internal/tuitest/driver.go — Driver.Output, onlcrWriter;
internal/tuitest/keys.go — Click, Release; cmd/apogee/e2e_present_test.go — presentRemote

**Tests:** the new test itself; it must fail if `copyFlash` drops `tea.SetClipboard` (check by
temporarily removing it locally, not committed). It clears `DISPLAY` / `WAYLAND_DISPLAY`, neutralises or skips
darwin and skips windows, and its recording holds the first launch's bytes (recorder wired before the first `start`).

**Acceptance:**
- `go vet ./cmd/apogee/`
- `go test -race -count=1 -run 'Copy' ./cmd/apogee/`

**Commit:** `test(cmd/apogee): a driven drag pins the OSC 52 bytes for the prompt and a resumed transcript`
