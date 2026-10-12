---
title: session-snapshot ingestion restores untrusted history as committed conversation (audit 2026-09-20 Medium)
status: done
type: bug
priority: 2
aliases: [apogee-mre]
created: 2026-09-21T20:21:43Z
closed: 2026-09-23T06:06:59Z
reason: "Plan 2026-09-22 - 00 items 12-13: a restored snapshot's roles, counts and sizes are checked before it is applied, and it cannot forge the engine's own structure."
---

Source: docs/reviews/code-audit-2026-09-20.md, Medium 'session-snapshot ingestion restores untrusted history as committed conversation'.
Where: internal/agent/state.go:157 (restoreState/decodeState), both ingestion paths (Resume / RestoreSession).
What: the version check and task-list caps cover shape only; dropLeadingSystem strips only a leading run of System messages. A crafted role:assistant message reaches the model as genuine history with no fence, and a crafted pendingInput reaches it as the user's own next input. A hostile repo ships a crafted session export; the user restores it; the model acts on injected instructions with the session's live tools and mode. Medium because every tool call still passes the guards/mode/Approver — the leak is into the model's context.
Audit fix: treat the payload as hostile on both paths — validate each role, refuse or sanitise tool-result/tool-call content carrying fence lines or engine-note markers, bound message count and per-message size, refuse unattributable pendingInput; surface snapshot provenance before applying history.
