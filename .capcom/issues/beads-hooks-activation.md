---
title: .beads/hooks activation runs repo-shipped shell as live git hooks (audit 2026-09-20 Critical)
status: done
type: bug
priority: 1
aliases: [apogee-242]
created: 2026-09-21T20:21:38Z
closed: 2026-09-23T06:06:56Z
reason: "Documented, not coded (owner call 2026-09-22): plan 2026-09-22 - 00 item 14 — SECURITY.md and AGENTS.md state what hook hydration trusts."
---

Source: docs/reviews/code-audit-2026-09-20.md, Critical 'repo-shipped .beads/hooks/* become live git hooks with the user's full privileges'.
Where: .beads/hooks/commit-msg, post-checkout, pre-push, prepare-commit-msg — activated by bd init / bd hooks install pointing core.hooksPath at .beads/hooks (the documented hydration recipe in AGENTS.md).
What: committed, executable, repo-controlled shell; bd's marker handling preserves commit-msg (marker-free) and prepare-commit-msg's unmarked tail verbatim, so after hydration the next git commit/checkout/push runs the repo's shell as the user.
Audit fix: bd hooks install should only activate hooks it can vouch for (hash-pin the marked files, refuse or flag unmarked content), or the repo must not ship live hooks.
Repo-policy call for the owner (handoff 2026-09-21 step 4): this repo IS the owner's own — the hooks are the attribution stripper and the bd-init guard the owner wrote. Candidate outcomes: (a) won't-fix + document in SECURITY.md/AGENTS.md that hydration trusts the checkout's hooks and a clone of an untrusted fork must inspect .beads/hooks before bd hooks install; (b) upstream a hash-pin to bd. Not an apogee code change either way.
