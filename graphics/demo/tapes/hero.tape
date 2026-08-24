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
# 18s puts it just after the first `go test` returns red. Retune if the model's pace changes.
Sleep 18s

# Wording matters more than it looks: "also note the fix in the CHANGELOG" made the model
# read CHANGELOG.md and then forget to write it. The imperative form lands it first try.
Type "also add a CHANGELOG entry for the fix"
Sleep 500ms
Enter

# KNOB 3 — when the fix's edit card is opened. Tool blocks paint COLLAPSED, always (layout.md,
# "Collapsed and expanded blocks"), so the fix lands as one row — `Replace ↳ task.go … +1 -1 ·
# +N more lines ▶` — and the split diff (two panes, tinted add/del bands) paints only once
# something opens it. Nothing opens itself, so the tape does.
#
# The gesture is the keyboard block cursor: ⌥↑ enters it standing on the MOST RECENT block, ⏎
# opens what it stands on (blockcursor.go). VHS cannot send ⌥↑ — `Alt+up` types the literal
# text "up" — so the raw sequence goes in instead: Escape immediately followed by "[1;3A" at
# TypingSpeed 0ms, which reaches apogee as one CSI and is read as ⌥↑. The 0ms is load-bearing:
# split by a typing delay the ESC is read alone, which mid-run means "stop the run".
#
# The value is how long the fix takes to LAND, because the target is only right while the edit
# is the most recent block — once the next tool call starts, ⌥↑ stands on that one instead. A
# block opened mid-flight stays open, so the split diff holds for the rest of the clip. Failure
# looks like a stopped run, "[1;3A" sent as a message, or the wrong card opening.
#
# The PageDowns after the hold are not optional: a toggle keeps the toggled row where it is on
# screen (layout.md), which detaches the viewport from the tail — leave it detached and the
# CHANGELOG write, the green PASS and the /undo preview all paint below the fold. Paging back
# to the bottom re-attaches it; the extra presses are clamped there and cost nothing.
Sleep 12s
Set TypingSpeed 0ms
Escape
Type "[1;3A"
Set TypingSpeed 40ms
Sleep 700ms
Enter
Sleep 4s
PageDown 4
Sleep 500ms

# KNOB 2 — must outlast the run's tail. Overshooting is free (render.sh trims); undershooting
# truncates the green PASS and costs a whole take.
# It now counts from AFTER the knob-3 gesture, so it measures what is left of the tail — the
# CHANGELOG write and the closing green `go test` — not the whole run.
Sleep 45s
Sleep 5s

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
