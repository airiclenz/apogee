---
title: ADR 0027 context
---

The prompt box had grown two slash worlds that did not know about each other, and the owner hit
the seam from three directions in one issue (`ISSUES.md`): *"slash commands like `/skills` do not
work when typing a message to be scheduled and they also don't work if I want to use a slash
command after I already typed something in the prompt editor. I also would like to see skills and
files in-line in the prompt text. Skills should also directly be callable with `/` (without the
need to call the list with `/skills`)."*

Every symptom traced to a different piece of the same design:

- **The `/` menu was whole-value and idle-only.** `computeAutocomplete` opened the command region
  only when the entire input was one `/partial` with no whitespace anywhere, and only at
  `stateIdle`. A draft already in the box therefore had no command namespace at all, and neither
  did the message being composed while the model worked — which is the state the human spends the
  most time in.
- **Skills were STATE beside the message, not text in it.** `/skill <partial>` popped a chip onto
  `pendingSkills`, a parallel attachment list rendered as a violet strip above the box, carried to
  submit, popped by Backspace, dropped by `reset`, carried by `/continue` and dropped by
  `/compact`. Nothing about the message the model would read said a skill was attached, a skill
  could not be named directly, and `stageInterjection` silently discarded the attachments
  (`interject.go` parsed with `parsed, _ :=` and built a `UserInput` carrying no `SkillIDs`) — so a
  skill staged while the model worked was quietly lost.
- **`/skills` did not exist**, and a `/word` naming nothing was sent to the model verbatim. The
  literal reproduction in the issue is a human typing `/skills` and watching it arrive as prose.
- **`runCommand` reset the editor unconditionally**, so invoking a command could only ever mean
  destroying the draft.
- Two registries described the same verbs — `knownCommands` (parser) and `commandMenu` (dropdown,
  with summaries and the menu-only `skill`) — and drift between them was structural.

Mid-string completion had been **deliberately deferred** in `TODO.md` on the grounds that the
trailing-token rule was "cursor-position-free, robust". That rationale is what this ADR reverses.

The design below is the 2026-07-28 grill session's rulings, implemented item-by-item by
`docs/plans/2026-07-28 - 03 - slash-skills-inline-plan.md`. It supersedes nothing and amends
ADR 0025 decision 10 in one clause.
