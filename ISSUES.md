A: Activated / Active
P: Planned
X: Executed



- [ ] Currently I cannot see how much ot it's context a sub agent has used.

- [ ] Clicking on the header of a tool for collapsing/expanding is not very responsive (very often nothing happens at all) - this seems to be connected to 100% GPU usage when an agent is running. When no local session is in progress the expansion/collapse response is snappy.

- [ ] work-dir path needs to be displayed in the bottom instead of the context size (e.g. `/workspace/repos/apogee` for this project, the home dir shoulod be replaced with `~` so that `/Users/username/Repos/apogee` would be displayed as `~/Repos/apogee` instead). The context size should move to the context gauge and be diaplyed like this: `8K/98K 8%`

- [ ] I'd like to remove the frame from the lower part of the promt box as well. THe new layout is outlined in ./docs/design/prompt-box-layout.md

- [ ] pop-up lists like /sessions, /skills or @file are painted inconsistent. Some have an empty row between the bottom prompt/status section and some have not:
  - server: unwanted spacer row
  - session: unwanted spacer row

- [ ] keyboard path for collapse/expand: a block-cursor mode (↑/↓ move a highlighted block, enter toggles, esc leaves). Deliberately deferred from the collapse wave — layout.md "Collapsed and expanded blocks" keeps toggling mouse-only for now, on the same precedent that keeps transcript selection mouse-only.

- [ ] sent prompts with skills should not look like this: 
  ❯ /refocus on everything
    ✦ Refocus
  The skill(s) should be in-line with the text and simply color marked. I don't want additional skill tags like `✦ Refocus` in this example

- [ ] `/skill` wears the `— idle only` tag in the "/" menu while the model works, but picking it there works anyway — the tag is wrong. `commandSuggestions` (internal/tui/autocomplete.go) tags every row whose `commandSpec.whileRunning` is false, and `/skill` is declared `menuOnly: true` with no such flag (internal/tui/command.go); `acceptAutocomplete` then completes any `takesArgs`/`menuOnly` verb to `/skill ` and chains straight into the skill picker, never reaching the idle-only refusal (`refuseIdleOnlyCommand`). So the tag contradicts both the actual behaviour and README's own command table, which lists `/skill` as usable while the model works. Predates the popup-alignment work — the tag arrived with 72de7dd. The fix is a semantics call: either give `/skill` `whileRunning: true`, or stop tagging `menuOnly` verbs at all (which would also cover any future verb in that position).