---
title: apogee project adopt prints a repo's Allow rule text to the terminal unescaped
status: done
type: bug
priority: 2
labels: [security]
created: 2026-11-10T19:34:35Z
closed: 2026-11-11T06:36:33Z
reason: resolved by audit-defects plan (2026-11-10 - 02 - audit-defects-plan), archived at e5e5bb54
---

Source: docs/reviews/code-audit-2026-11-10.md (Medium, "`apogee project adopt` prints repo-supplied rule text raw into the terminal"). Verified 2026-11-10 at a5c4f2db.

**Where:** cmd/apogee/project_cmd.go `printProjectQuestion` (Allow-rule branch) and `heldName` (Handle == "" branch).

**What:** an Allow rule's `proposal.Text` comes from the repository's `.apogee/config.yaml` and is written to the terminal byte for byte. The adoption pane and `tui.AdoptionEntryName` spell the same entry through `flatText` (escape-stripped, line breaks folded), and the undo listing strips too. Config load (internal/config/allow.go `rules()`) refuses only empty entries, so a rule may carry `\r`, ESC and other control characters. The already-sanitised `AdoptionQuestion` title is computed and then ignored on this branch.

**Why it matters:** a hostile repo can write a rule whose escapes or carriage return make the question show a harmless rule (say `git status`) while `y` pins the real bytes, a much broader Allow rule. `apogee project adopt` exists to be that trust gate.

**Suggested fix:** build the Allow-rule question from the sanitised `tui.AdoptionQuestion` title or `tui.AdoptionEntryName`, and make `heldName` use `AdoptionEntryName` for every kind. As defence in depth, reject control characters (C0, DEL, bidi controls) in Allow rule text at load, at least for the Project layer.

## Acceptance

- A Project-config Allow rule containing \r or an ESC sequence is never written raw to the terminal by apogee project adopt, in the question or in the 'no longer holds' refusal.
- The CLI question names an Allow rule with the same spelling as the adoption pane.
- A test feeds a rule with control characters through printProjectQuestion and heldName and asserts no C0 or ESC byte reaches the output, or that load refuses the rule.
