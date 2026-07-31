A: Activated / Active
P: Planned
X: Executed

- [ ] When a new promt has been sent off but the model did not generate any response yet, I cannot scroll up in the session chat. 

- [ ] items in the pop-up module are currently terribly alligned - e.g. skills and their description should be displayed as properly separated columns so that 2nd and 3rd tier items per line are aligned vertically.

- [ ] markdown tables emitted by the model are not properly rendered as a table in the apogee chat.

- [ ] work-dir path needs to be displayed

- [ ] the vertical sub-agent indicator line on the very left of the chat should be continuous. Currently each new action (new tool, new response, ...) are separated with an empty line. The sub-agent indicator line should be displauyed even in these spacer lines. A new sub agent tool call directly after a prior sub-agent tool call should NOT be visually connected to the prior call.
I'd also like to have the sub agent line in orange (same color as tool headers)

- [ ] we need a possibility to klick on tool calls to see al the details /e.g. "+22 more line" extend to show the lines. This should be a global module that can be re-used for all tools including sub-agents (e.g. all below and including `⤷ sub-agent` is collapsed). It should also by defualt display the tool name, a summary about the tool (like now) and maybe even an in-progress indicator (the very left bullet point as a blinking star). The summary will be more complex for complex tools like sub-agents which could include many tool calls in itself (cascading). This will need deeper grilling / planning.  