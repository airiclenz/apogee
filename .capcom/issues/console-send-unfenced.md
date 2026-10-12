---
title: POSIX console_send to an unconfined Console runs unfenced after /mode auto
status: done
type: bug
priority: 3
labels: [security]
plan: 2026-10-03 - 00 - table-stakes-and-image-input-plan
aliases: [apogee-zwvj]
created: 2026-10-03T18:11:28Z
closed: 2026-10-03T23:28:48Z
reason: "Resolved by refocus-followups-plan (docs/plans/archived/2026-10-03 - 01 - refocus-followups-plan.md, db1550b0) item 2: POSIX console_send to an unconfined Console under a box now demotes like Windows"
---

Pre-existing gap recorded by plan 'docs/plans/2026-10-03 - 00 - table-stakes-and-image-input-plan.md' item 16 (ADR 0059 §2 and §4, Bounds amended 2026-10-03). On POSIX, a Console opened unconfined in an ask-before mode (Ask-Before / Approval) and then sent to after a switch to /mode auto runs console_send without a fence or a prompt: the fence is a property of the process set at console_open, and the per-send resolve() admits the send in Auto because it assumes the open-time fence binds it. Item 16 closed this for Windows only (each send to an unconfined Console under a confinement box reports ErrConfinementUnavailable and is demoted to Approval); POSIX was left unchanged. Decide whether a send under Auto to a Console that was not opened Confined should gate like the Windows path.
