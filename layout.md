❯ The last prompt that the user sent is in white text. It's background color should be
  dark gray. Word wrap must apply everywhere, and it breaks short of the right edge:
  two columns stay free between the text and the scroll bar, three between the text
  and the window edge while no bar is painted. The user must be able to scroll up in
  the chat session to see the complete chat history. The session area follows the
  generated output: while the view sits at the bottom, every repaint keeps the tail
  visible as it streams. An answer shorter than the visible session area opens with its
  prompt stuck to the top of that area; once an answer outgrows the area the tail stays
  in view and the prompt it belongs to is overlaid on the top row as a sticky header.
  Scrolling up detaches from that: the view holds exactly where it was scrolled to and
  new output no longer moves it. Scrolling back down to the very bottom — or sending a
  prompt — resumes following (this is also implemented in apogee-code).

✦ The LLM's answer looks like this. There is exactly one empty line between the users
  prompt and the agents response — and exactly one between the answer and the next
  block, never two or three: the answer's own leading and trailing blank lines are
  trimmed off. Below there is the layout of a tool call.

✦ Read File
  ┕ main.go 1 - 154

✦ Read File
  ┝ README.md 1 - 154
  ┝ TODO.md   1 - 408
  ┕ ISSUES.md 1 - 8

✦ Run
  ┕ go test ./...
    ok      github.com/airiclenz/apogee/internal/tui   0.412s
    … +2 more lines

✦ View Diff
  ┕ main.go +2 -2
      a context line
    - a code line that has been removed
    - another code line that has been removed
    + a new code line
    + another new code line

✦ Sub Agent
  ┕ 3 Sub Agents
    Sub Agent 1: Agent Name (= brief one line summary)
    Sub Agent 2: Agent Name (= brief one line summary)
    Sub Agent 3: Agent Name (= brief one line summary)

✦ This is the last message from the LLM. There must always be one empty line between
  chat content and the bottom prompt/information section like displayed here.

▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
  ⠉⠹ reading · main.go · 3s                                      16k 50% ██████     ]
╭─────────────────────────────────────────────────────────────────────────────────────╮
│ Send a message… [Shift] + [Enter] creates a line break                              │
│ This text box can be multiline. The text edit area auto increases height to         │
│ accomodate the bigger message. Clicking into this field should position the cursor  │
│ at the clicked position. The background color of this box is black. The border      │
│ of this prompt box are dark gray.                                                   │
├─────────────────────────────────────────────────────────────────────────────────────┤
│ host-alias ✦ qwen3.6-27B-Q4_K_S.gguf ✦ 32k                               ask-before │
╰─────────────────────────────────────────────────────────────────────────────────────╯

---

## The rules behind the tool-call sketch

**The label.** A tool header is `✦ ` plus the tool's label, **and nothing else — never a
target**. That holds for every block alike: a grouped run, a lone call, a call still in flight,
and the stray-result `result` header. The target always leads the first branch line instead, so
a block does not visually reshape the moment a second call joins it. The label carries no
brackets and is rendered **bold in orange `#f0883e`** — the tone inline code and the auto-mode
marker already use. The styling is uniform too: a known friendly label ("Read File"), an unknown
tool's raw name, and `result` all look the same. The bare-name-means-unregistered signal was the
brackets' job and dies with them.

**What groups.** Consecutive tool calls at the same nesting depth carrying the same label fold
into one block. Any entry between them — narration, a note, an approval, an error — breaks the
run. Two different tools that share a label (a single and a multi find-and-replace are both
"Edit File") do group: the user groups by what they read, not by tool id.

**The outcome, in two halves.** What a finished call has to say is split in two, and everything
below follows from that split — never from counting lines. The **summary** is the single line that
rides the branch beside the target: a read's `1 - 154`, a diff's `+2 -2`, an `error: …`. The
**body** is what hangs beneath it: a command's output, a diff's own lines. A call may have either,
both, or — while it is still in flight — neither. Anything that fits on one line is a summary,
whatever produced it: a command whose whole output is one line rides the branch like a read does
(`┕ pwd /workspace/repos/apogee`), and only output that needs the `… +N more lines` remainder
becomes a body.

**What stays standalone.** A call is groupable when it has a target, an empty body, and a plain
(non-diff) summary — which includes an `error: …` line, and an in-flight call whose result has not
landed yet. A call carrying a body (the `Run` above, with its `… +N more lines` remainder; the
`View Diff` above, with its diff beneath the `+2 -2`) or no target at all breaks the run and
renders as its own block. It renders in the *same shape* it would have had inside a group, though:
a block of one is byte-identical in shape to a block of many, which is the whole point of the
header carrying no target.

**The block's shape.** One header line carrying the label alone, then one branch line per call —
`┝`, and `┕` for the last. Two shapes, and they are the whole grammar:

- **A call with a target** — the branch is the target, and where the call has a summary, one space
  and that summary (`┕ main.go 1 - 154`, `┕ main.go +2 -2`); an in-flight call has no summary yet
  and shows the bare target, the whole block repainting when its result lands. Its body, if it has
  one, lays out beneath the branch, indented to the branch marker's own width — the `Run`'s output,
  the diff's lines under their `+2 -2`. Those are not `┝`/`┕` branches of their own; only calls are.
  A body of one line lays out exactly like a body of ten.
- **A call with no target** — the one shape with no target line: the header stands alone and the
  lines are themselves the `┝`/`┕` branches, the summary closing the list since it has no branch
  line to ride (an unregistered tool's pretty-printed arguments, then the `error: …` it earned; a
  stray `result`).

Within a block, every target is padded with spaces to the widest target so the summary column lines
up; a block of one pads to itself, which is no padding. Anything too long soft-wraps under its
marker like any other detail line — nothing is clipped for alignment's sake.

**Blank lines.** Exactly one empty line between blocks, never more. Assistant text is trimmed
of its leading and trailing blank lines, and interior runs of two or more blank lines collapse
to one — except inside a fenced code block, where blank lines are code and stay verbatim.

---

## The status line's spinner

**Which animation.** The two braille cells opening the running status line above are the `snake`
style, the default: six dots walking the outer ring of the 4×4 dot grid two cells form side by
side, one lap a second. `ui.spinner` in `config.yaml` selects the animation — `snake`, `glitter`
(a pair of cells re-rolled twenty times a second out of the braille block sorted by density,
under a six-second swell to a solid `⣿⣿` and back), or `classic`, the single-cell `⣾⣽⣻⢿⡿⣟⣯⣷`
rotation apogee shipped before and still supports. Only `classic` is one column wide; the other
two are two, which shifts the activity phrase one column right of the sketch's older single-cell
`⣻`. The phrase and the elapsed clock beside it are the same for every style.

**Which colour.** `ui.spinner-color` runs a soft ten-second loop through three palette tones
(periwinkle → turquoise → blue → back) over whichever style is selected; with it off, the glyph
keeps the terminal's own text colour on the status bar's black field. The two keys are
independent — every style renders both coloured and plain — so picking `classic` does not turn
the loop off, and `classic` with the loop off is exactly the status line apogee rendered before
the styles existed. The loop is quantised downstream by the terminal, so on a 256-colour
terminal it steps visibly and on a 16-colour one it collapses to a couple of tones; that is what
turning it off is for.

---

## The staged-interjection band

**What it shows, and when.** A message typed while the agent is working is *staged*, not sent: it
waits its turn and goes out at the next safe seam. Every staged message waiting to go out shows as
one row in a band sitting directly above the input box — the slot closest to the box, below the
status line. The band exists only while something is queued, in whichever state holds it: a live
queue draining as the agent runs, or a queue held over at idle after a stop. An empty queue paints
nothing at all — no rows, and no frame either.

**One group, framed.** The rows are one contiguous block, never interleaved with other chrome, and
the group is framed by one blank band row above and one below so it reads as its own object between
the status line and the box. The frame belongs to the band: it appears and disappears with it.

**Painted edge to edge.** Every row of the band — the content rows and the two blank framing rows
alike — is faint text on a black field spanning the **full window width**. A row is clipped
ANSI-aware to the window with a `…` tail and then padded back out to that same width, so the black
runs to both edges and the terminal's own background never shows through past the text. The input
box directly beneath has the same black interior, so band and box read as one joined block of
chrome — the status line's posture, one row up.

**A row.** `  ⧖ message`: two spaces of indent (`bodyIndent` — the same body column the status
line's text and the transcript's own body sit in, so the ⧖ lines up with the spinner column above),
the ⧖ staged-interjection marker, one space, then the message flattened to a single line (runs of
whitespace collapse to one space) as a preview. The message itself is untouched; this is only how a
waiting row is shown.

**Order and cap.** Rows are in delivery order, oldest first — so the row nearest the input box is
the newest, the one Backspace takes back. At most three content rows show at once (`maxQueuedRows`);
the strip steals its height from the transcript viewport, so an unbounded queue would squeeze the
conversation off the screen. Past the cap the **newest** rows are the ones kept and a
`  … N more queued` marker rides at the top of the group, inside the frame and indented and painted
like every other row, so the count says nothing was dropped. The worst case on screen is six rows:
the cap, the marker, and the two framing rows.

**What the band is not.** Once a staged message is delivered it leaves the band and appears in the
transcript as its own ⧖ block, which keeps the transcript's own look. The status line's `N queued`
readout is separate too, and reads the same count from the same queue.

---

## The prompt box's mini-language

**One dropdown for `/`.** Typing a `/` token opens ONE suggestion pane above the box — the same
bordered, titled pane the `/sessions` browser and the approval prompt use, in the slot that
shrinks the transcript to make room. It is titled `commands and skills` and it lists both:
commands first, prefix-matched, each with its one-line summary; then skills, matched on id and
display name, each row led by the `✦` skill glyph so the two kinds never read alike. `@` opens the
same pane over workspace files. At most eight rows show; the hint line under them reads
`↑/↓ select · ⏎/tab accept · esc dismiss`.

**It follows the caret, not the end of the line.** The token being completed is the word the caret
stands in or immediately after — so a draft already in the box does not shut the menu out, and
going back to fix a misspelled skill id mid-message offers exactly the same menu the end of the
buffer does. Accepting splices over that token alone and puts the caret just after the splice;
everything on either side is untouched.

**Accepting a command RUNS it.** The `/verb` is cut out of the draft and the command fires; the
rest of what was typed stays in the box with the caret where it belongs. The two verbs that need
what follows them are the exception and complete instead: `/confine` (which takes arguments — and
arguments are only ever read from a whole-line invocation) and `/skill`, which chains into the
picker over the catalog. Accepting a skill row writes that skill's own `/id ` token into the text.

**The box never goes dead while the model works.** Every region stays open. A command that needs a
quiescent engine is not hidden — its row renders with a trailing `— idle only` tag in the pane's
faint unselected style, and accepting it anyway prints the note and leaves the draft exactly as it
was. The verbs that only report (`/version`, `/skills`, `/confine` with no arguments) run there
and then.

**Tokens light up when they resolve.** Inside the box a `/token` is painted in the skill violet
only when it names a skill in the catalog, and an `@path` in the reference blue only when the path
is in the workspace listing. Everything else stays plain prompt text, so the colour is a live
verdict rather than decoration: a typo simply never lights. Both accents are drawn on the box's
own black, so the field still reads as one band, and a token wrapped across rows is painted on
every row it spans. A drag-selection drawn over a token wins — selection is painted last.

**What is not here any more.** There is no strip of attached-skill chips above the box. A skill is
its `/token` in the text now, so the message says what it invokes without a second surface
repeating it. The transcript's sent user block still carries its violet `✦ name` chips — that is
the record of a send, not the state of the editor.
