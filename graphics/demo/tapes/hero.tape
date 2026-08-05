# The README hero clip: one task, red -> green, with a queued interjection mid-run.
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

# KNOB 2 — must outlast the run's tail. Overshooting is free (render.sh trims); undershooting
# truncates the green PASS and costs a whole take.
Sleep 45s
Sleep 5s
