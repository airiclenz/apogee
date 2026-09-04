# The README hero clip: one task, red -> green, with a queued interjection mid-run, the fix's
# split diff opened on camera, and a closing /undo preview that reverts nothing.
# Recorded at real speed; trimmed and compressed afterwards by render.sh.

Output hero.mp4
Output hero.gif

Set Shell bash
Set FontSize 15
Set FontFamily "Source Code Pro"
Set Width 1250
Set Height 680
Set Padding 16
Set TypingSpeed 40ms
Set Theme "Catppuccin Mocha"

# Hidden: remap HOME to the demo home, enter the stage repo, point Go's caches inside the
# workspace. None of this is on camera.
Hide
Type "source ./env.sh"
Enter
Show

Sleep 500ms
Type "apogee --mode auto"
Sleep 500ms
Enter
Sleep 4s

Type "the test suite is failing - find the bug, fix it, and prove the tests pass"
Sleep 800ms
Enter

# KNOB 1 — when the interjection arrives. It has to land while tool cards are still
# streaming, which is what makes it visibly *queued* rather than a normal next turn.
# 8s against OpenRouter deepseek-v4-flash: the exchange runs ~29s end to end, so submitting at
# ~10s (8s + the 1.5s of typing + 0.5s) lands mid-stream with margin on both sides. The old 18s
# was tuned for the local model and now fires AFTER the exchange has closed — the tell is the
# session JSON, where the entry reads `user` instead of `interjected` and `/undo` then covers
# only the CHANGELOG write. Retune if the model's pace changes.
Sleep 8s

# Wording matters more than it looks: "also note the fix in the CHANGELOG" made the model
# read CHANGELOG.md and then forget to write it. The imperative form lands it first try.
Type "also add a CHANGELOG entry for the fix"
Sleep 500ms
Enter

# KNOB 3 — when the fix's edit card is opened. Tool blocks paint COLLAPSED, always (layout.md,
# "Collapsed and expanded blocks"), so the fix lands as one row — `Replace ↳ task.go … +1 −1 ·
# +N more lines ▶` — and the split diff (two panes, tinted add/del bands) paints only once
# something opens it. Nothing opens itself, so the tape does.
#
# The gesture is the keyboard block cursor: ⌥↑ enters it standing on the MOST RECENT block, ⏎
# opens what it stands on (blockcursor.go). VHS cannot send ⌥↑ — `Alt+up` types the literal
# text "up" — so the raw sequence goes in instead: Escape immediately followed by "[1;3A" at
# TypingSpeed 0ms, which reaches apogee as one CSI and is read as ⌥↑. The 0ms is load-bearing:
# split by a typing delay the ESC is read alone, which mid-run means "stop the run".
#
# The target is only right while the edit is the MOST RECENT block — once the next tool call
# starts, ⌥↑ stands on that one instead. A block opened mid-flight stays open, so the split diff
# holds for the rest of the clip. Failure looks like a stopped run, "[1;3A" sent as a message,
# or the wrong card opening.
#
# The PageDowns after the hold are not optional: a toggle keeps the toggled row where it is on
# screen (layout.md), which detaches the viewport from the tail — leave it detached and the
# CHANGELOG write, the green PASS and the /undo preview all paint below the fold. Paging back
# to the bottom re-attaches it; the extra presses are clamped there and cost nothing.
#
# The Escape after the toggle is what takes the highlight bar back off the card. ⏎ OPENS the
# block but leaves the mode standing — only esc or a printable key ends it (blockcursor.go), and
# PgDn is explicitly one of the keys that "leaves the mode standing" — so without this the bar
# stays painted across the opened card's header row in the theme's SELECTION tone for the rest of
# the clip, and the storyboard's showcase card wears a full-width band that reads as a selection
# nobody made. Esc drops the bar and leaves the card open, because a block opened mid-flight
# stays open. It cannot stop the run the way a bare ESC would: while the mode is active the block
# cursor claims esc ahead of handleKey's cancel (model.go, keyClaimOrder — the block cursor
# claims its keys "ahead of the switches in handleKey"), and ⏎ did not end the mode. The sleep
# before it is what keeps it a lone ESC rather than the head of another CSI.
#
# WHAT IT WAITS ON. This used to be a fixed ten-second `Sleep` from the interjection's Enter, and
# a clock cannot track this beat. The window is the gap between the fix's Replace card painting
# and the queued interjection being DELIVERED; delivery happens at the very next tool boundary, so
# the gap is UNDER A SECOND, while against OpenRouter deepseek-v4-flash the run varies ~19s to
# ~29s end to end and slides that sub-second window by ~8s. Measured hit rate: about one take in
# seven. Both misses cost the take and one is worse: too LATE opens the CHANGELOG card instead
# (`+3 −0 ▼` while task.go stays `+1 −1 · +8 more lines ▶`, and beat 5 has no split diff), while
# too EARLY (measured at 3s) leaves the block cursor nothing settled to stand on, the leading ESC
# of the CSI is read alone, and the run is CANCELLED — the session JSON ends `note: cancelled`
# with the stage tree clean. So the tape waits on the SCREEN instead: `Wait+Screen` blocks until
# the fix card's own diffstat is painted, which is exactly what tells that card from the
# CHANGELOG one.
#
# WHY +Screen. `Wait`'s default scope is `Line`, which watches only the line the cursor is on;
# the card row is not the last line on screen once the run moves on, so a `Line` wait would never
# see the diffstat.
#
# WHY 40s. A `Wait` timeout is a LOUD failure — vhs exits non-zero and the take dies — where the
# old miss was silent, shipping a clip with the wrong card open. The budget is set well past the
# ~29s worst case so it never fires on a slow run; vhs's own default, 15s, sits inside the
# observed spread. Requires vhs 0.11.0 or newer, which is where `Wait[+Screen][@timeout]` landed.
#
# A CHANGED EDIT SIZE MEANS A CHANGED PATTERN. `\+1 −1` is this fix's diffstat spelled the way
# the TUI spells it — with the table's typographic minus U+2212, not an ASCII hyphen (diffCounts,
# internal/tui/toolview.go). Reseed the bug, or let the model fix it differently, and the pattern
# must be re-derived from the new card; a stale count or an ASCII `-` matches nothing on screen
# and every take then times out.
Wait+Screen@40s /\+1 −1/
Set TypingSpeed 0ms
Escape
Type "[1;3A"
Set TypingSpeed 40ms
Sleep 700ms
Enter
Sleep 800ms
Escape
Sleep 4s
PageDown 4
Sleep 500ms

# KNOB 2 — must outlast the run's tail. Undershooting is what costs a take: `/undo` typed while
# the run is still going is QUEUED as a message instead of executing, and beat 7 is gone.
# It counts from AFTER the knob-3 gesture (its Escape, not its Enter), so it measures what is
# left of the tail — the CHANGELOG write and the closing green `go test` — not the whole run.
# 12s against OpenRouter deepseek-v4-flash: that tail measures ~3s, so this overshoots ~4x.
# Overshoot is NOT free the way the old comment claimed — render.sh only trims the HEAD (-ss),
# so every idle second here ships as dead air in the middle of the clip. The old 45s left ~27s
# of it. Give it margin for a slow take, not an unbounded cushion.
Sleep 12s

# The closing beat. Bare `/undo` PREVIEWS and changes nothing: the preview is a plain
# transcript note listing every path the exchange wrote — task.go and CHANGELOG.md together,
# because an interjection joins the Exchange it steered instead of opening a new undo group
# (ADR 0025, ADR 0051). Nothing here is modal, so there is nothing to dismiss; the Escape is
# the "walked away, kept the fix" gesture, and the proof is `git -C <stage> diff --stat` still
# non-empty after the take rather than anything on the frame. Never type `/undo confirm` — it
# would unfix the bug on camera.
Type "/undo"
Sleep 500ms
Enter
Sleep 4s
Escape
Sleep 2s
