A: Activated / Active
P: Planned
X: Executed

- [ ] I'd like a full screen menu for all available settings when running the slash command /settings. This needs grilling.

- [ ] not all tool calls follow the collapsable display pattern. Generally, I want to not see more than 3 to 4 lines of a tool call and the tool call content needs to be properly formatted (orientation, font weight, color highlights), aligned and readable. All tools must follow the same general formatting rules and be collapsable. This means for example that file-write-tools needs to be able to display the changed lines, and so on...
The tool header needs to indicate if a tool in the session chat is expanded or collapse (e.g. `Run Python ▾` for expanded and `Run Python ▸` for collapsed - feel free to find better symbols). This will probably need grilling.

- [ ] Collapsable tool calls print too much of the tool's content if the first line is very long. apogee just wraps the line and prints however many lines it needs. I'd like to limit the apogee tool printout - when collapsed - to 3 lines. Important information like the amount of tool calls for a sub agent need to stay visible (maybe print them in a separated extra line?)

- [ ] `/new` should not be recorded as a recallable prompt

- [ ] `/skill` is not needed anymore - remove it.

- [ ] Currently I cannot see how much of it's context a sub agent has used.

- [ ] keyboard path for collapse/expand: a block-cursor mode (↑/↓ move a highlighted block, enter toggles, esc leaves). Deliberately deferred from the collapse wave — layout.md "Collapsed and expanded blocks" keeps toggling mouse-only for now, on the same precedent that keeps transcript selection mouse-only.

- [ ] `clipRunes` (internal/tui/toolpresent.go:770) spends its budget in RUNES while the paint spends CELLS, so a cap of N runes does not bound the N cells it promises. The status line is where that bites: `toolPhrase` (internal/tui/activity.go:161) clips a tool target to `statusTargetRunes` = 32 (internal/tui/activity.go:121), and that cap exists so the left slot cannot push the context gauge off an 80-column status row. A double-width target — a CJK path, an emoji in a filename — is 32 runes but up to 64 cells; `statusLeft` then truncates the over-wide phrase against the whole window, and the gauge goes off the row: exactly the outcome the cap exists to prevent. The TAB half of this is already FIXED and should not be re-raised — 35f4245 made this caller cell-honest for tabs by expanding the target before the cap counts it (`expandTabs`), completing the tab sweep run through the other measuring sites by 1015b9b, 92786ca, 86ae843, 6cd6b3b and 5a479a1. What survives is the width mismatch itself: `statusTargetRunes` is still spent in runes. For scale, the probe on the tab case measured 27 cells at the clip against 91 cells painted on the row.
SUSPECTED, UNPROBED: the same rune-vs-cell mismatch should also sit at `clipRunes`' other caller, `clipDetail` / `detailClipRunes` = 160 (internal/tui/toolpresent.go:764), where a detail line of double-width text would clip at 160 runes and paint up to 320 cells. The transcript soft-wraps rather than dropping a neighbour, so a reader would expect extra wrapped rows rather than a lost element — but nobody has run that probe, so this half is inference from the shared helper, not a confirmed defect.

- [ ] Two comments left by 35f4245 disagree about the same probed number. `toolPhrase` (internal/tui/activity.go:157) says a target clipped to 32 runes "painted up to 139 cells"; that same commit's test comments (internal/tui/paint_test.go:897 and :913) say 91, which is the figure the probe actually measured and the one the rune-vs-cell entry above cites. The 139 is the stale one. Documentation accuracy only — the code is right either way — but a reader trusting the 139 would mis-size any future budget work on that path.

- [ ] `/skill` wears the `— idle only` tag in the "/" menu while the model works, but picking it there works anyway — the tag is wrong. `commandSuggestions` (internal/tui/autocomplete.go) tags every row whose `commandSpec.whileRunning` is false, and `/skill` is declared `menuOnly: true` with no such flag (internal/tui/command.go); `acceptAutocomplete` then completes any `takesArgs`/`menuOnly` verb to `/skill ` and chains straight into the skill picker, never reaching the idle-only refusal (`refuseIdleOnlyCommand`). So the tag contradicts both the actual behaviour and README's own command table, which lists `/skill` as usable while the model works. Predates the popup-alignment work — the tag arrived with 72de7dd. The fix is a semantics call: either give `/skill` `whileRunning: true`, or stop tagging `menuOnly` verbs at all (which would also cover any future verb in that position).
