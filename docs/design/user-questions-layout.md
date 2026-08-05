> **Status:** the owner's pinned mockup for the two decision surfaces — implemented by
> `docs/plans/2026-08-04 - 03 - user-questions-menu-layout-plan.md`, whose ratified design calls
> amend it where the two differ; `layout.md` carries the rules that came out of it.

Arrow keys (up and down) navigate.
Enter selects the item and sends the answer.
If shortcuts are given, the directly select the corresponding item and send the answer.
Please refer to the menu system UI-laypout in llama-launcher for reference.


# User Approval:

╭────────────────────────── Approve terminal? ───────────────────────────╮
│ Reason: subprocess execution (confinement unavailable on this  host)   │
│ Command:                                                               │
│   cd /workspace/repos/apogee && git status                             │
│                                                                        │
│ ❯ Allow                     [a]                                        │
│ · Always allow this session [s]                                        │
│ · Deny                      [d]                                        │
│ · Cancel                    [esc]                                      │
╰────────────────────────────────────────────────────────────────────────╯


# Multi option question:

╭────────────────────────────────────────────────────────────────────────╮
│ How to continue with the implementation of the feature "The best.      │
│ Feature in the world"?                                                 │ 
│                                                                        │
│ ❯ [1] Just do it all in one shot and commit once.                      │
│                                                                        │
│ · [2] Commit each piece as you go and run make check after every       │
│   commit.                                                              │
│                                                                        │
│ · [3] Implement the config redesign first, commit it, then do the TUI  │
│   part in a separate commit. Run make check after each. The config     │
│   change is the riskier part — get it stable first.                    │
│                                                                        │
╰────────────────────────────────────────────────────────────────────────╯


# Multi-select question:

> Pinned 2026-08-05 by `docs/plans/2026-08-05 - 00 - ask-user-multi-select-plan.md`, whose ratified
> design calls amend this file for questions the model marks `multi_select`: `␣` toggles the
> highlighted row, `⏎` sends every checked row (the highlighted one alone when none is checked), and
> the marker glyphs are `[x]` / `[ ]`. The pointer and dim rows are the menu style already drawn
> above; the gap between the marker and the label is the pop-up module's own two-space column
> gutter, where the sketch below draws one.

╭──────────────────────────────────────────────╮
│ Which findings should I fix?                 │
│                                              │
│ ❯ [x] Fix the nil-check in submitAnswer      │
│                                              │
│ · [ ] Add the missing layout() call          │
│                                              │
│ · [x] Guard the empty-choices path           │
│                                              │
│  ↑↓ select · ␣ toggle · ⏎ send · …           │
╰──────────────────────────────────────────────╯