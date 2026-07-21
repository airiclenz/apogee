❯ The last prompt that the user sent is in white text. It's background color should be
  dark gray. Word wrap must apply everywhere, and it breaks short of the right edge:
  two columns stay free between the text and the scroll bar, three between the text
  and the window edge while no bar is painted. The user must be able to scroll up in
  the chat session to see the complete chat history. The last user prompt must stick to
  the top of the vivible session area (this is also implemented in apogee-code).

✦ The LLM's answer looks like this. There is an empty line between the users prompt
  and the agents response. Below there is the layout of a tool call.

✦ [Read File] Filename 

✦ [Read File] Filename 
  ┕ 0 - 100

✦ [Update File] Filename 
  ┕ +2 -2
    098 - a code line that has been removed
    099 - another code line that has been removed
    100 + a new code line
    101 + another new code line
   
✦ [Sub Agent] 3 Sub Agents
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
