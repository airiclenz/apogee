A: Activated / Active
P: Planned
X: Executed

- [ ] Pressing backspace/del on selected text in the prompt editor should delete it.

- [ ] Currently I cannot see how much of it's context a sub agent has used.

- [X] Clicking on the header of a tool for collapsing/expanding is not very responsive (very often nothing happens at all) - this seems to be connected to 100% GPU usage when an agent is running. When no local session is in progress the expansion/collapse response is snappy.

- [ ] keyboard path for collapse/expand: a block-cursor mode (↑/↓ move a highlighted block, enter toggles, esc leaves). Deliberately deferred from the collapse wave — layout.md "Collapsed and expanded blocks" keeps toggling mouse-only for now, on the same precedent that keeps transcript selection mouse-only.

- [ ] sent prompts with skills should not look like this: 
  ❯ /refocus on everything
    ✦ Refocus
  The skill(s) should be in-line with the text and simply color marked. I don't want additional skill tags like `✦ Refocus` in this example

- [ ] `/skill` wears the `— idle only` tag in the "/" menu while the model works, but picking it there works anyway — the tag is wrong. `commandSuggestions` (internal/tui/autocomplete.go) tags every row whose `commandSpec.whileRunning` is false, and `/skill` is declared `menuOnly: true` with no such flag (internal/tui/command.go); `acceptAutocomplete` then completes any `takesArgs`/`menuOnly` verb to `/skill ` and chains straight into the skill picker, never reaching the idle-only refusal (`refuseIdleOnlyCommand`). So the tag contradicts both the actual behaviour and README's own command table, which lists `/skill` as usable while the model works. Predates the popup-alignment work — the tag arrived with 72de7dd. The fix is a semantics call: either give `/skill` `whileRunning: true`, or stop tagging `menuOnly` verbs at all (which would also cover any future verb in that position).
