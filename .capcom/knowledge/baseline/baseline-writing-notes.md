---
title: "Before adding or changing a Note: what belongs in Knowledge and how to write it"
labels: [seed:baseline, seed-text:0.17.1:884157cf8941]
---

Add a Note for what the next session cannot read from the code or the git history: a decision
and its reason, a constraint, a trap, a procedure that is easy to get wrong. Progress belongs
in an Issue's Log, not in a Note. A fact about one developer's own machine or preferences
does not belong in a Note.

- Search first with `capcom know search <word>...`, and update the Note that exists instead
  of adding a second one.
- One subject per Note, readable on its own. A long Note becomes a parent with child Notes.
- The title says when to read the Note ("Before cutting a release: ...") or states the rule.
- Add a Ref for each file, directory or symbol the Note depends on
  (`capcom know add --ref <path>`, `capcom know update --add-ref <path>`), so
  `capcom know verify` flags the Note when it goes.
- A Note that no longer holds is retired, not removed: `capcom know update <id> --retire`,
  with `--add-superseded-by <id>` when another Note replaces it.
