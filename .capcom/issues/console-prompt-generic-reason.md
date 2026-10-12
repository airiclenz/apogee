---
title: Console send approval prompt says confinement is unavailable on a host that can confine
status: done
type: bug
priority: 3
plan: 2026-10-04 - 00 - console-vision-fork-followups-plan
aliases: [apogee-console-prompt-generic-reason]
created: 2026-10-04T09:01:31Z
closed: 2026-10-04T11:42:54Z
reason: item 1 resolved by plan 'console-vision-fork-followups' (docs/plans/archived/2026-10-04 - 00 - console-vision-fork-followups-plan.md), archived at e1d81cb2
---

A POSIX console_send to a Console opened unconfined, while a confinement box is active, is gated (apogee-zwvj). The refusal/deny detail reaches the model via withConfineFailureDetail (internal/agent/dispatch.go), but the approval prompt the user sees is confineFallback's generic text (internal/agent/resolution.go): reason 'subprocess execution (confinement unavailable on this host)' and remedy '/confine off ...'. On Linux/macOS the host can confine; the real fix is to close and reopen the Console fenced. resolve() builds the fallback before the tool runs, so the tool's reason has to travel in a typed error (unwrapping to ErrConfinementUnavailable) and replace the gate text in executeConfineFallback. Windows console_send and every real host failure keep the generic wording.
