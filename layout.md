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
  ⠉⠹ reading · main.go · 3s                                        16k 50% █████░░░░░
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

## What "width" means everywhere below

**Every width in this document is the width the terminal actually paints.** "How many columns
does this string occupy" has two answers and they disagree: the grapheme-cluster measure counts
`⚠️` (an emoji carrying VARIATION SELECTOR-16) as two cells, the older wcwidth measure counts it
as one, and which of the two a terminal paints in is a capability it either announces at start-up
(mode 2027, "Unicode core") or never mentions. Apogee measures in whichever one its own painter
is using — the TUI's single **width authority**, which starts at wcwidth and follows the painter
to the grapheme measure the moment the terminal says so
([ADR 0030](docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md),
`internal/tui/width.go`). So every rule below that says *width*, *column*, *wraps at* or *is
padded to* is stated in cells the terminal really shows — never in cells a library computed off
to one side.

**The prompt box is the one exception, on purpose.** The caret mirrors and the box's own height
mirror a third-party widget's internal wrap, and a mirror's oracle is the widget: they measure
the way that widget measures, whatever the painter chose. That is what makes the caret land under
the glyph the pointer is on.

---

## What "height" means: one row budget, and the transcript pays it

**Every pane above the input box takes its rows from the transcript.** The approval and ask
prompts, the `/sessions` browser, the `/model` | `/server` picker, the `/` and `@` dropdown, and
the staged-interjection band all sit in the frame between the session area and the bottom chrome,
and the session area is what shrinks to seat them. The frame is composed from ONE derivation of
how many rows are left over, so the rows the transcript is drawn on, the rows a mouse click may
address in it, and the rows an overlay paints are the same three answers to one question — a click
on a pane's border or on a session row selects nothing in the transcript, because there is no
transcript there.

**A short window shrinks the panes rather than pushing the box off-screen.** A pane never promises
rows the frame cannot hold: it spends what the window can spare above the input box, so a long
choice set on a half-height pane scrolls a smaller window around the selection, and on a window
too short for both, the pane keeps its rows and the session area goes to nothing. The input box
and the footer are never the ones that give way — they are the frame's floor. (The box's floor is
the box, not every row a draft grew it to: see the decision-surface rule below.)

**The budget is the FRAME's, not each surface's.** All of them are spending the same rows, so they
are allotted them together, once, before any of them is drawn — never each against the whole window
as though it were the only thing in it. A budget sized that way is exactly right one surface at a
time and wrong the moment two are open: a `/` menu typed while messages are queued asked for its
rows and the band asked for its six, and the frame that came out was four rows past the terminal's
last one, with the input box and the footer off the alternate screen. Which is a thing the human
does on purpose — the band is up because they are typing while the agent works, and the dropdown is
up because they are typing.

**So the surfaces give way in a fixed order, and the order is a claim about what the human is
doing.** The **session area** goes first and goes to nothing; then the **staged band**, which is a
reminder rather than a control, and whose count the status line is carrying anyway; then the
**`/` and `@` dropdown**, which a keystroke opened and a keystroke dismisses; then the **`/sessions`
browser and the picker**; and last the **approval or ask prompt**, which the run itself is blocked
on. The footer is not in the division at all, and the input box is in it only at its very end, and
only for the prompt (below). Two panes can want the same rows, and on a window that cannot seat
both, the one further down that order is not drawn.

**A decision surface is never invisible while its keys are live.** The approval and ask prompts are
the two, and being last in the give-way order is not enough on its own, because the **input box**
was outside the order entirely: a three-line draft grew the box by two rows, the frame had four to
give above it, and the prompt fell off the allocation with `a`/`d`/`s` still answering it — the
whole of what the frame then said about the decision being `approval needed` on the status line, no
tool name. So the box's **extra draft rows** join the order, at the very end of it: the box always
keeps one content row, and the rows a draft grew it past that give way to the prompt — and, past the
order entirely, to the frame itself (below). "The input box never gives way" is about the box, not
about every row a draft grew it to —
the box is on the frame at every window size, and what the human was typing is still in it, drawn
on as many rows as the frame can pay for. The dropdown, the browser and the picker never take the
box's rows: the browser and the picker own the keyboard while they are open, so no draft can be
growing beside them, and the dropdown is a completion of the very draft it would be shrinking.
Below twelve rows there is no pane at all (see the four-row floor below), which is the floor of the
pane arrangement rather than an exception to this rule; the frame has a lower floor of its own, and
it is the last thing this section states.

**The box's extra rows give way to the FRAME as well, which is not a place in the order at all.**
The order above divides the rows the window can spare; the frame bound is about the window itself.
Every row a draft grows the box is a row taken out of the same window everything else is spending,
so past a point the box alone — no pane, no queue, nothing open — composes a frame past the
terminal's last line, and it did: a six-line message on a 12-row terminal put the footer two rows
off the screen, and a ten-line one did it to every terminal under eighteen rows. So the box is
capped at whatever leaves the frame inside the terminal, whether or not a prompt is up: five rows on
a 12-row terminal, one row on an 8-row one. The session area is what pays for those rows, exactly as
it pays for a pane's — it is first in the order and it goes to nothing.

**A capped box is a window onto the draft, and it says how much of it is out of sight.** The box
scrolls to keep the line the caret is on in view, and the draft ITSELF is never cut: it is what the
human typed, not prose apogee derived, and it is the one kind of content in the frame that cannot be
recovered from anywhere else. What the box is not drawing is counted on the box's own **top
border**, in the same `… (+N more lines)` marker every pane uses and on the same narrow ladder
(`… +3` where the full phrase does not fit). The border is the row the box always owns and that
carries nothing else — the box's title row in every way that counts — so the count costs the draft
no row. It differs from a pane's title row in one place only, at the bottom of the width ladder: a
pane sheds its NAME to keep its number, and this row has no name to shed, so under the width even
`… +3` needs, it stays a plain border rather than drawing a clipped count. `… +1` of `… +19` is not
a quieter statement of the fact; it is a false one.

**The frame's own floor is eight rows, and below it the frame is its floor and nothing more.** With
the session area gone the frame is one blank gap row, the `▔` hairline, the status line, the input
box (its top border and its one content row) and the footer's three — eight rows, every one of them
something the rules above forbid giving way. At exactly eight the frame is that chrome and it fits.
**Below eight it does not fit, and what it does there is deliberate:** it composes its floor and no
more. A long draft, a queued message or an open pane cannot make it grow past eight on a seven-row
terminal any more than on an eight-row one, so the overflow is bounded at one row per row the
terminal is short and never compounds. Clipping the frame to the window instead was considered and
rejected: it would make "the frame never exceeds the terminal" trivially true at every size, and the
next real overflow would then arrive silently instead of failing a test.

**A pane that gives way entirely leaves its fact on the status line.** That is the band's licence
for disappearing — the `N queued` readout carries the count — and the status line therefore owes
that readout the same width discipline a pane's title row owes its count (below): the left slot is
composed **to** the window rather than composed long and clipped to it, so a narrow window sheds
the activity phrase around the count and never the count off the end of the row.

**A surface still gets its irreducible rows before any other surface gets a comfortable one.** Each
open pane's four (below) come off the top of the budget, so a passive band can never squeeze out the
pane being acted on; the band then takes what it wants out of what is left, which is why it costs
the session area rather than the pane beside it; the session area keeps a three-row reserve out of
the remainder; and only the surplus past all three makes a pane taller than its floor, split evenly
when more than one is open.

**The smallest honest pane is four rows: its two borders, its title, and its key hint.** That is
the floor for every pane alike — a pane out of budget shows no rows AND no prose, rather than
keeping one row back for either. It still says what it is and how to act, which is the least a
pane can be and still be worth drawing; what it cannot do is claim a fifth row the frame has not
got. Under the eight rows of fixed chrome below the session area, that puts the shortest terminal
a pane can be drawn in at all at **twelve rows** — and at twelve the session area is already gone,
so the frame is exactly the pane and the chrome together.

**What shrinking never buys is silence.** Prose a pane cannot seat is counted out in the
`… (+N more lines)` marker, and that marker outranks the prose itself: with one body row left it
IS the body, and with none left — the twelve-to-fifteen-row case, where the four-row floor is the
whole pane — it moves onto the **title row**, after the pane's name. So the approval prompt on a
half-height tmux pane reads `approve write_file?  … (+12 more lines)`: the tool the decision turns
on, and the fact that there is more to read than the window can show. A pane may lose the text; it
may not lose the reader's knowledge that there was text.

**The rows are counted the same way, when there is no window for them at all.** A row window the
pane did get scrolls around the selection, so the entries outside it are one keypress away and need
no marker. A window of **zero** rows is the other thing entirely — every choice or entry gone, and
the key hint still offering `↑↓ select` to pick between them — so those rows are counted onto the
title row too, in the **same** marker: `saved sessions  (all workspaces)  … (+8 more lines)`, and
the ask prompt at twelve rows counts its four answers there beside the question line it also
dropped. One marker states the whole of what the pane is holding back, because a title row too
narrow to seat one count has no room for two.

**Narrowness does not buy silence either.** A half-height pane is usually a half-width one too, and
the title row is composed **to** the pane's width rather than composed long and clipped to it — a
clipped row would drop the count off its end and put the silence straight back. The width is spent
in the order the row is read for: the pane's name, then the count, then the words around the count.
So the marker sheds its noun before it sheds its number (`approve write_file?  … +12` at 42 columns
and below), and past that the **name** is what gives way to an ellipsis (`approve writ…  … +12`),
never the number. On a pane too narrow for even a clipped name, the count is the whole row.

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

Inside a sub-agent run that separating row is not empty: it carries the `│` rail gutter, drawn as
deep as *both* neighbouring blocks reach, so the run's frame runs unbroken from its `⤷ sub-agent`
label to its last line. It is still exactly one row, and it is bare wherever the two blocks share
no rail — at a run's start and end, and between two sub-agent calls that follow one another, which
is what keeps them from reading as one run.

---

## Markdown tables in assistant text

**When a table is a table.** A pipe table in an answer renders as a table only once its delimiter
row has landed: a line carrying at least one unescaped `|`, immediately followed by a row built
only from `-`, `:`, spaces and pipes, with at least one `-` per cell and the same cell count as the
line above it. Leading and trailing pipes are optional, `\|` is a literal pipe inside a cell, and
the block ends at the first blank line or the first line with no pipe in it. A delimiter-shaped
line with no header above it is not a table and keeps whatever it renders as today.

**It is borderless.** No verticals, no outer frame, no corners — a table is columns of text and
nothing else, sitting in the same body column the rest of the answer sits in. Every cell is padded
to the widest cell in its column and columns are separated by exactly **two spaces**. Each cell is
rendered as inline markdown first, so `**bold**` and `` `code` `` inside a cell style the way they
do in a paragraph, and it is that *rendered* width — the painted width, measured by the
width authority like every width in this document — that sets the column, never the source width,
so markup characters and the bytes of a colour escape never push a column open. The **last** column
is padded like every other one, so every line of a table — header, rule and body rows alike — ends
in the same column and the block shows one straight right edge to whatever sits beside it. A row
that stopped at its last word instead would leave a wider gap to the scroll-bar gutter than the rule
above it does, which reads as the bar stepping inward beside the body.

**The header and its rule.** The header row's cells are bold, the same weight `**bold**` earns
anywhere else. The delimiter row renders as a **single unbroken run of `─`** spanning the whole
table: the two-space gutters are ruled along with the columns, so the line reads as one continuous
horizontal rule under the header rather than a dash per column interrupted at every column
division. It is exactly one line tall, wears the same muted colour as before, and — the gutters
being filled rather than blank — is exactly as wide as every other line of the block.

**Alignment is the delimiter row's word.** `:--` left, `--:` right, `:-:` centred, a plain `---`
left; every cell is padded on the side its column names, header cells included. A centred cell with
an odd remainder takes the extra space on its right. A row with fewer cells than the header is
padded out with empty ones and a row with more loses the excess: the column count is the header's
to set.

**The width cap is absolute.** No rendered line ever exceeds the width the block was given, a table
no more than anything else — and it is the **painted** width that is capped, so the cap holds on a
line carrying emoji or CJK on any terminal, not merely in the measure the layout computed with. The
only thing that may cross it is a single grapheme wider than the whole limit, which no break can
divide; it takes a line to itself. Where the natural column widths plus their gutters do not fit,
the widest column is shrunk one cell at a time — the leftmost of them where two are equally wide,
so the outcome is the same every repaint — until the table fits, and a cell too wide for the column
it lands in is cut with a `…` tail. If the table cannot fit even with every column down to a single
cell, it is not drawn as a table at all: the block falls back to the plain paragraphs it would have
been before, which is always readable and never overflows.

**One row is one line.** No cell wraps; a row is exactly one physical line however much it carries.
Overflow is the `…` above, never a second row.

**A partial table is plain text.** While a table streams in, everything before the delimiter row is
ordinary paragraphs — the contract every other half-typed construct keeps — and the block snaps
into a table on the row that completes it. Columns go on measuring themselves against the rows that
have arrived, so a table may widen a column as it streams; that reflow is expected and costs
nothing but a repaint.

**Before and after.** What the model emits:

```
| Tool | Calls | Notes |
|:--|--:|:-:|
| Read File | 12 | fast |
| Run | 3 | `go test ./...` |
```

and what the transcript draws for it at 34 columns of body width — `Tool` left, `Calls` right,
`Notes` centred, the header bold, `go test ./...` styled as inline code with its backticks gone:

```
Tool       Calls      Notes
───────────────────────────────
Read File     12      fast
Run            3  go test ./...
```

Two of those four lines carry trailing blanks out to the rule's last column — the padding of the
centred `Notes` column, which print cannot show. All four are 31 columns wide.

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

## The status line's right slot

**Where it ends.** The right end of the status line is one slot that its occupants take in turn —
the context-usage gauge (`16k 50% █████░░░░░` in the sketch above, where `░` draws the empty half
of the ten-cell track: on screen those cells are a painted dark-gray field carrying no glyph of
their own), the key hint that stands in for it (`esc stop` while a turn runs, `enter dismiss`
after an error, the primed-`ctrl+c` line), and the mouse-copy flash. Whichever one is showing, it
ends **two columns short of the window edge** (`bodyIndent`) — the mirror of the two columns the
left slot leads with, and the same column the footer's mode marker below it ends in, so the
gauge's last track cell in the sketch sits directly above the last character of `ask-before`. The
margin belongs to the slot, not to any one occupant: the gauge and every hint end in that column,
and nothing in the slot ever touches the edge. The black field runs past it to the edge
regardless — the row is one unbroken band, as it is on the left. In a window too narrow to hold
both slots the right one is dropped whole rather than squeezed, two columns sooner than it used
to be.

**And what the left slot sheds.** The left slot carries the state's own words — the running phrase
and its clock, `approval needed`, `answer needed`, `error` — and after them the `N queued` count of
what is waiting to go out. It is composed **to** the window's width, in the order it is read for,
exactly as a pane's title row is: the **count** is the last thing it gives up and the **phrase** is
what is trimmed around it (`⣾ read… · 5 queued` at 20 columns), because on the short windows where
the band has been dropped that count is the only thing the whole frame says about the queue. Below
two columns of room the phrase goes whole, separator and all, rather than reading as an ellipsis.

---

## The footer's upstream slot

**What it carries.** The footer's content row states the upstream on the left — `host ✦ model ✦
window`, the sketch's `host-alias ✦ qwen3.6-27B-Q4_K_S.gguf ✦ 32k` — and the autonomy mode on the
right. The host falls back to the endpoint's own host when no alias is configured, and the window
is dropped when nothing has named one.

**The words that stand in for a model.** The model and its window are replaced **together**
whenever there is no binding to report — a context window is not a fact about a model nobody has
named yet — and only ever by one word:

- `connecting…` while a wired heartbeat has not bound a model: the seconds between the first paint
  and the first landed beat, and again after a `/server` switch until the new server answers.
- `loading <profile>…` while a profile load is in flight. It **outranks** `connecting…`, and
  it replaces a model that is still bound, because the launcher is in the middle of invalidating
  that binding and the profile being waited on is the more specific truth. `/unload-model` and
  `/stop-server` show `unload-model…` and `stop-server…` the same way — neither has a profile to
  name, so the slot spells the verb the human typed back at them.

The slot holds one word at a time and says nothing else: an actuation's own progress steps are
transcript notes, not chrome. The `✦ offline` marker is separate again — it is *appended* to the
left slot in the error tone once the offline crossing is made, beside whatever the slot is showing.

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

**The cap is the band's taste; the frame's row budget is the answer to it** (the section on height
above). The band is one of the surfaces sharing the rows above the input box, and it gives way
before any pane does, so a short window — or an open pane beside it — cuts it below three rows and
sometimes to none. What the budget drops is counted in **the same** `… N more queued` marker the cap
overflows into, one wording for one fact, and the marker outranks the rows it describes: a band with
one row to spend spends it on the count, not on one of five. Below three rows there is no honest
band left — its frame, one row and a marker do not fit — so it is not drawn at all, and the status
line's `N queued` readout is what carries the count, as it does in every frame the band appears in.

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
same pane over workspace files. At most eight rows show — and fewer than eight when the window
cannot spare eight, because the dropdown answers to the same row budget every pane does (the
section above): a short terminal scrolls a smaller window around the selected row, and one with no
rows to give counts the whole menu onto the title row rather than opening an empty pane under a
hint still offering `↑/↓ select`. The hint line under the rows reads
`↑/↓ select · ⏎/tab accept · esc dismiss`. The command rows read **alphabetically**, so the menu
can be scanned without knowing the table behind it, and every verb the parser knows is in it —
`/stop-server` and `/unload-model` included. Those two act on the session's own server and say so
in their names, and a verb the human cannot discover is a verb they will not find.

**Its rows are columns, not sentences.** A dropdown row is not one concatenated string. The name
and its one-line summary render as **vertically aligned columns**, each padded to the widest cell in
it, so every summary in the pane starts at the same screen column however long the verbs and skill
names beside them run — and in the merged `/` menu a skill's description is aligned against the
command summaries above it, so the two kinds read as one table rather than two lists stacked. The
busy-state `— idle only` tag is a column of its own after the summary, and it costs the pane
nothing while the engine is idle, because a column no row fills collapses away. `@`'s file rows
have one field and so no columns to align: they render exactly as they always did. The full rule
is the **Column contract** under "One overlay for 'which one?'" below, which governs every pop-up
pane alike — this dropdown, the pickers, and the `/sessions` browser.

**It follows the caret, not the end of the line.** The token being completed is the word the caret
stands in or immediately after — so a draft already in the box does not shut the menu out, and
going back to fix a misspelled skill id mid-message offers exactly the same menu the end of the
buffer does. Accepting splices over that token alone and puts the caret just after the splice;
everything on either side is untouched.

**Accepting a command RUNS it.** The `/verb` is cut out of the draft and the command fires; the
rest of what was typed stays in the box with the caret where it belongs. The verbs that need what
follows them are the exception and complete instead: the three that take arguments — `/confine`,
`/model` and `/server` (and arguments are only ever read from a whole-line invocation) —
plus `/skill`, which chains into the picker over the catalog. Accepting a skill row writes that skill's
own `/id ` token into the text.

**One overlay for "which one?".** `/model` and `/server` with nothing after them open a
picker: the same bordered pane as the `/sessions` browser, one row per choice, one highlight, `↑/↓
select · ⏎ switch · esc close` under it, at most eight rows with a window scrolling around the
selection. It is modal — while it is open every key belongs to it. `/server` lists the servers
`config.yaml` names plus the one this session started on, in three columns — `name`, `— endpoint`,
`· current` — and the row the session is on is the one that fills the third, faintly; picking it
says so instead of switching. `/model` has
two offerings and lists whichever one this host can answer from: with llama-launcher configured,
the Launch profiles its config defines, in the launcher's own order, in five columns — `name`,
`— backend`, `· 32k`, `(:8080)`, `· running` — where the port shows only for a profile that does
not live where this session is pointed and `· running` marks one that is live right now; without
it, what the server currently advertises, in two columns — `model`, `— 32k` — refreshed in place
if a heartbeat lands underneath it. Either way what
the session is ALREADY on is not among the rows — there is no `· current` mark to pick, because
there is no row that would switch nothing, which is what makes the hint's `⏎ switch` true of every
row. Given an argument (`/model <name>`, `/server <name>`) the verb acts straight away and no pane
opens at all. When there is nothing to
pick — no monitor, an unreachable server, nothing advertised yet, nothing but the model already
bound, no `servers:` block, no launcher config where one was named, no profiles in it, only the
profile already loaded — the answer is one honest line in the transcript and no empty pane.

**The Column contract.** Every one of those grammars is a row of **cells**, and the pop-up module —
not the code that produced the row — owns the alignment, alongside its marker, highlight, windowing
and truncation. A column is as wide as its widest cell measured in painted display cells (the width
authority again, so CJK and emoji count for what they occupy on screen, not for their runes), and it
is measured over **all** of the pane's rows rather than the eight in the window, so the columns never
shift under the eye while the selection scrolls. Adjacent columns are separated by a **two-space**
gutter — the same minimum gap a markdown table keeps. Each separator glyph leads the cell it
introduces rather than trailing the one before it — `— backend`, `· 32k`, `(:8080)` — so the `—`, the
`·` and the `(` line up down the pane as well as the words after them. Every pop-up kind has a
**fixed schema**: a tier a row does not state is an *empty cell*, which pads like any other, so an
unstated context window or a nameless backend cannot slide the tiers after it sideways; and a column
**no** row in the pane fills collapses away entirely, costing it neither width nor gutter. The
composed line is right-trimmed and then goes through the pane's ordinary pipeline — the two-cell
selection marker in front, the highlight bar across it, truncation to the inner width with a trailing
`…`. That truncation is **whole-row**, never column by column, so a narrow terminal loses the
rightmost tiers rather than scrambling the alignment of the ones still on screen. A row with a single
cell has no columns to align and renders exactly as it did before columns existed: `@`'s file
suggestions, an armed rename buffer in the `/sessions` browser, and the ask and approval prompts.

**`/model`'s launcher accept is the one that does not finish on the spot.** Picking a Launch profile
takes the actuation latch and hands the pane's decision to a blocking launcher verb: the overlay
closes, the footer's model slot says `loading <profile>…`, and the launcher's steps arrive as
transcript notes until the beat completes the move. While that latch is held, the paths that would
open an Exchange (a send, `/continue`, `/compact`) and the four switching verbs (`/model`,
`/server`, `/unload-model`, `/stop-server`) are each refused with one line instead of acting; Esc
does not cancel an actuation, because the launcher's own cancel is `/stop-server` once the verb
returns.

**The box never goes dead while the model works.** Every region stays open. A command that needs a
quiescent engine is not hidden — its row fills the menu's `— idle only` column in the pane's faint
unselected style, and accepting it anyway prints the note and leaves the draft exactly as it was.
The tag belongs to the moment rather than to the verb: while the engine is idle no row fills that
cell, so the column collapses and the menu reads exactly as it does when nothing can be gated. The
verbs that only report (`/version`, `/skills`, `/confine` with no arguments) run there and then.

**Tokens light up when they resolve.** Inside the box a `/token` is painted in the skill violet
only when it names a skill in the catalog, and an `@path` in the reference blue only when the path
is in the workspace listing. Everything else stays plain prompt text, so the colour is a live
verdict rather than decoration: a typo simply never lights. Both accents are drawn on the box's
own black, so the field still reads as one band, and a token wrapped across rows is painted on
every row it spans. A drag-selection drawn over a token wins — selection is painted last.

**What is not here any more.** There is no strip of attached-skill chips above the box. A skill is
its `/token` in the text now, so the message says what it invokes without a second surface
repeating it. The transcript's sent user block still carries its violet `✦ name` chips — and so
does a delivered `⧖` interjection block, which is the same record — because that is the record of
a send, not the state of the editor.
