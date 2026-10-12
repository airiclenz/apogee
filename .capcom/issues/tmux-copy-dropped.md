---
title: Copy never reaches the clipboard when apogee runs inside tmux
status: done
type: bug
priority: 1
plan: 2026-09-23 - 00 - tmux-copy-route-plan
aliases: [apogee-tmux-copy-dropped]
created: 2026-09-23T16:45:16Z
closed: 2026-09-23T17:18:52Z
reason: "Resolved by plan 2026-09-23 - 00 - tmux-copy-route-plan (archived a17ae6e2): copy inside tmux also pipes to tmux load-buffer -w; driven test pins OSC 52 bytes"
---

Drag-select copy flashes 'copied N chars' but nothing pastes when apogee runs inside tmux on its default set-clipboard external (e.g. VS Code remote terminal profile running tmux over SSH). tmux 3.5a input_osc_52 drops an application's OSC 52 unless set-clipboard is on; the atotto system-clipboard fallback writes the remote host's clipboard (no DISPLAY). Verified 2026-09-23: a program's \e]52 is dropped under 'external', while 'tmux load-buffer -w -' reaches the attached client as OSC 52. Fix: when TMUX is set, copyFlash also hands the text to 'tmux load-buffer -w -'. Tests only assert a non-nil Cmd + flash; nothing asserts the OSC 52 bytes on the wire. Workaround: tmux set -g set-clipboard on.
