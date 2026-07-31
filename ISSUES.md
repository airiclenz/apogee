A: Activated / Active
P: Planned
X: Executed

- [ ] When a new promt has been sent off but the model did not generate any response yet, I cannot scroll up in the session chat. 

- [ ] work-dir path needs to be displayed

- [ ] we need a possibility to klick on tool calls to see al the details /e.g. "+22 more line" extend to show the lines. This should be a global module that can be re-used for all tools including sub-agents (e.g. all below and including `⤷ sub-agent` is collapsed). It should also by defualt display the tool name, a summary about the tool (like now) and maybe even an in-progress indicator (the very left bullet point as a blinking star). The summary will be more complex for complex tools like sub-agents which could include many tool calls in itself (cascading). This will need deeper grilling / planning.  

- [ ] sent prompts with skills should not look like this: 
  ❯ /refocus on everything
    ✦ Refocus
  The skill(s) should be in-line with the text and simply color marked. I don't want additional skill tags like `✦ Refocus` in this example

- [ ] when resuming a session, the message "resumed: session-name" should not end up saved in the session history. Also, resumes session should not receive the systme prompt again.

- [ ] `/skill` wears the `— idle only` tag in the "/" menu while the model works, but picking it there works anyway — the tag is wrong. `commandSuggestions` (internal/tui/autocomplete.go) tags every row whose `commandSpec.whileRunning` is false, and `/skill` is declared `menuOnly: true` with no such flag (internal/tui/command.go); `acceptAutocomplete` then completes any `takesArgs`/`menuOnly` verb to `/skill ` and chains straight into the skill picker, never reaching the idle-only refusal (`refuseIdleOnlyCommand`). So the tag contradicts both the actual behaviour and README's own command table, which lists `/skill` as usable while the model works. Predates the popup-alignment work — the tag arrived with 72de7dd. The fix is a semantics call: either give `/skill` `whileRunning: true`, or stop tagging `menuOnly` verbs at all (which would also cover any future verb in that position).