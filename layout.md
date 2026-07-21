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

✦ Read File main.go
  ┕ 1 - 154

✦ Read File
  ┝ README.md 1 - 154
  ┝ TODO.md   1 - 408
  ┕ ISSUES.md 1 - 8

✦ Run go test ./...
  ┝ ok      github.com/airiclenz/apogee/internal/tui      0.412s
  ┕ … +2 more lines

✦ Update File main.go
  ┕ +2 -2
    098 - a code line that has been removed
    099 - another code line that has been removed
    100 + a new code line
    101 + another new code line

✦ Sub Agent 3 Sub Agents
  ┝ Sub Agent 1: Agent Name (= brief one line summary)
  ┝ Sub Agent 2: Agent Name (= brief one line summary)
  ┕ Sub Agent 3: Agent Name (= brief one line summary)

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

**The label.** A tool header is `✦ ` plus the tool's label plus, on a standalone call, its
target. The label carries no brackets and is rendered **bold in orange `#f0883e`** — the tone
inline code and the auto-mode marker already use. The styling is uniform: a known friendly
label ("Read File"), an unknown tool's raw name, and the stray-result `result` header all look
the same. The bare-name-means-unregistered signal was the brackets' job and dies with them.

**What groups.** Consecutive tool calls at the same nesting depth carrying the same label fold
into one block. Any entry between them — narration, a note, an approval, an error — breaks the
run. Two different tools that share a label (a single and a multi find-and-replace are both
"Edit File") do group: the user groups by what they read, not by tool id.

**What stays standalone.** A call is groupable when it has a target and at most one plain
detail line — which includes an `error: …` line, and an in-flight call whose result has not
landed yet. A call with several detail lines (the `Run` above, with its `… +N more lines`
remainder), one with diff detail (the `Update File` above), or one with no target at all
breaks the run and renders as its own block. A group of one is exactly today's single block,
target on the header line.

**The group's shape.** One header line carrying the label alone, then one branch line per
member — `┝`, and `┕` for the last. Each branch shows the member's target padded with spaces
to the widest target in the group, one space, then that member's single detail line, so the
detail column lines up. An in-flight member shows its padded target and nothing after it; the
whole block repaints when its result folds in. Anything too long soft-wraps under the branch
like any other detail line — nothing is clipped for alignment's sake.

**Blank lines.** Exactly one empty line between blocks, never more. Assistant text is trimmed
of its leading and trailing blank lines, and interior runs of two or more blank lines collapse
to one — except inside a fenced code block, where blank lines are code and stay verbatim.
