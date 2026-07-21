❯ The last prompt that the user sent is in white text. It's background color should be
  dark gray. Word wrap must apply everywhere, and it breaks short of the right edge:
  two columns stay free between the text and the scroll bar, three between the text
  and the window edge while no bar is painted. The user must be able to scroll up in
  the chat session to see the complete chat history. The last user prompt must stick to
  the top of the vivible session area (this is also implemented in apogee-code).

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

  ⣻ reading · main.go · 3s                                       16k 50% ██████     ]
╭─────────────────────────────────────────────────────────────────────────────────────╮
│ Send a message… [Shift] + [Enter] creates a line break                              │
│ This text box can be multiline. The text edit area auto increases height to         │
│ accomodate the bigger message. Clicking into this field should position the cursor  │
│ at the clicked position. The background color of this box is black. The border      │
│ of this prompt box are dark gray.                                                   │
├━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━──────────────────━━━━━━━━━━━━━━━━━━━━━━┤
│ host-alias ✦ qwen3.6-27B-Q4_K_S.gguf ✦ 32k                               ask-before │
╰━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━──────────────────━━━━━━━━━━━━━━━━━━━╯

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
