---
title: "Advise slot for non-tool Moments: the fenced tail user-role message (ADR 0076 D6 second form)"
status: open
type: feature
priority: 3
aliases: [apogee-4h5]
created: 2026-09-09T07:19:56Z
---

Stage 3 ships user advise: on post-tool-result and file-changed only, both landing as the trailer on the closing tool result. ADR 0076 D6's second slot — a fenced user-role message appended at the request tail for pre-request / post-response / history-rewrite advise — is unbuilt and unproven by the admission arm (which measures the trailer slot). Build it behind its own arm; the existing Outcome.Defer / Request.InjectContext path is NOT the slot (it folds into the system message after a tool result, the forgery adjacency D6 closes).
