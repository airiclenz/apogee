# The README hero clip: one task, red -> green, with a queued interjection mid-run, the fix's
# split diff opened on camera, and a closing /undo preview that reverts nothing.
# Recorded at real speed; trimmed and compressed afterwards by render.sh.
#
# This file is VHS mechanics only: what to type, what to wait on, and when. The WHY of every
# beat and every timing — the knobs, the measurements behind them, what a keeper take must show —
# lives in ../storyboards/hero.yaml, one entry per `# beat N` section below. Edit the storyboard
# first; touch a timing here only after its storyboard note says why.
#
# Geometry is 2× the shipped clip (2500×1360 at FontSize 30 for a 1250-wide GIF): VHS renders at
# device scale 1, and `demorig render` downscales, so a zoom in the storyboard has ≥2× to draw on.

Output hero.mp4

Set Shell bash
Set FontSize 30
Set FontFamily "Source Code Pro"
Set Width 2500
Set Height 1360
Set Padding 32
Set TypingSpeed 40ms
Set Theme "Catppuccin Mocha"

# Hidden: remap HOME to the demo home, enter the stage repo, point Go's caches inside the
# workspace. None of this is on camera.
Hide
Type "source ./env.sh"
Enter
Show

# beat 1 — the clip opens already inside apogee, footer shows auto
# The launch is recorded and cut in the render; the storyboard's first-paint anchor is the
# shell→TUI scene change. Its `align.first_prompt_at` is derived from this head — re-derive it
# when any Sleep here or the typing profile changes.
# Generated rhythm: gen.sh replaces this line with type.sh's per-character block at record time.
Sleep 500ms
Type "apogee --mode auto"
Sleep 500ms
Enter
Sleep 4s

# beat 2 — the prompt: one natural sentence, no flags
# Generated rhythm: gen.sh replaces this line with type.sh's per-character block at record time.
Type "the test suite is failing - find the bug, fix it, and prove the tests pass"
Sleep 800ms
Enter

# beat 3 — tool cards stream, the first go test comes back red
# Nothing is sent: the model works. The Sleep is KNOB 1 — when the interjection arrives; it has to
# land while tool cards are still streaming. Why 8s, and how a miss shows in the session JSON:
# storyboard beat 4.
Sleep 8s

# beat 4 — the interjection, typed while cards still stream
# The wording is load-bearing (storyboard beat 4).
# Generated rhythm: gen.sh replaces this line with type.sh's per-character block at record time.
Type "also add a CHANGELOG entry for the fix"
Sleep 500ms
Enter

# beat 5 — the fix lands: the split diff is opened on camera
# KNOB 3 — the gesture that opens the fix's edit card. Blocks paint collapsed and nothing opens
# itself, so the tape does, with the keyboard block cursor: ⌥↑ enters it standing on the MOST
# RECENT block, ⏎ opens what it stands on (blockcursor.go). Why the wait is a screen wait and not
# a Sleep, with the measurements: storyboard beat 5.
#
# VHS cannot send ⌥↑ — `Alt+up` types the literal text "up" — so the raw sequence goes in instead:
# Escape immediately followed by "[1;3A" at TypingSpeed 0ms, which reaches apogee as one CSI and
# is read as ⌥↑. The 0ms is load-bearing: split by a typing delay the ESC is read alone, which
# mid-run means "stop the run". The target is only right while the edit is the MOST RECENT block;
# a block opened mid-flight stays open, so the split diff holds for the rest of the clip.
#
# The PageDowns after the hold re-attach the viewport: a toggle keeps the toggled row where it is
# on screen (layout.md), which detaches the viewport from the tail, and left detached every later
# beat paints below the fold. The extra presses are clamped at the bottom and cost nothing.
#
# The Escape after the toggle takes the highlight bar back off the card: ⏎ OPENS the block but
# leaves the mode standing (blockcursor.go), and while the mode is active the block cursor claims
# esc ahead of handleKey's cancel (model.go, keyClaimOrder), so it drops the bar, leaves the card
# open and cannot stop the run. The sleep before it keeps it a lone ESC rather than the head of
# another CSI.
#
# `Wait+Screen`: `Wait`'s default scope is `Line`, which watches only the cursor's line; the card
# row is not the last line once the run moves on. `@40s`: a timeout is a loud failure (vhs exits
# non-zero), set past the worst observed run; vhs's default 15s sits inside the spread. Requires
# vhs 0.11.0 or newer, where `Wait[+Screen][@timeout]` landed.
#
# The pattern is this fix's diffstat spelled the way the TUI spells it — typographic minus U+2212,
# not an ASCII hyphen (diffCounts, internal/tui/toolview.go). A changed edit size means a changed
# pattern, here and in the storyboard's beat-5 expect; a stale one matches nothing and times out.
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

# beat 6 — the CHANGELOG entry lands and go test runs green
# KNOB 2 — must outlast the run's tail, counted from the knob-3 Escape above. Undershoot and
# `/undo` is queued as a message instead of executing; overshoot ships as dead air unless the
# render cuts it. Why 12s: storyboard beat 6.
Sleep 12s

# beat 7 — /undo previews the exchange's writes; Esc, nothing reverted
# Bare `/undo` previews and changes nothing; never type `/undo confirm`. What the preview is and
# why the storyboard anchors this beat off the tape's fixed tail: storyboard beat 7.
# Generated rhythm: gen.sh replaces this line with type.sh's per-character block at record time.
Type "/undo"
Sleep 500ms
Enter
Sleep 4s
Escape

# beat 8 — hold on the final frame
Sleep 2s
