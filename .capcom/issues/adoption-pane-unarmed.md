---
title: Adoption pane opens unasked with adopt preselected and no arming delay
status: done
type: bug
priority: 1
plan: 2026-10-11 - 01 - p1-p2-bugs-plan
aliases: [apogee-adoption-pane-unarmed]
created: 2026-10-11T18:09:30Z
closed: 2026-10-11T21:53:32Z
reason: "Resolved by plan '2026-10-11 - 01 - p1-p2-bugs-plan' (items 1, 3: adoption pane arms before it answers and opens on not now; driven test and docs), archived at 3fb76cff"
---

The adoption pane (pickerAdoption, internal/tui/adoption.go:85 and :177) opens unprompted, at start-up and mid-session at the first idle fold after a watched config save, with the 'adopt' row highlighted. Unlike the approval pane, which opens with its decision keys dead until armed (approvalArmed, internal/tui/approval.go:194-248, approvalArmBackstop), the shared picker has no arming delay. A keypress already in flight — e.g. an ⏎ sending a draft just as the pane appears — lands on 'adopt' and makes a Project config granting entry (an Allow rule) live, so the matching action then runs without asking. That defeats ADR 0096's premise that a granting entry is live only by deliberate Adoption.

Found by reading the code during the 2026-10-11 docs review (layout spec for the adoption pane, layout.md); not reproduced in a driven test yet.

Fix direction: give the adoption pane the same arm-before-answer rule as the approval pane (swallow keys until armed), and/or preselect 'not now' instead of 'adopt'.

Acceptance: a driven test where ⏎ arrives in the same frame the pane opens leaves the rule unadopted; layout.md's adoption-pane paragraph states the arming rule and the default row.
