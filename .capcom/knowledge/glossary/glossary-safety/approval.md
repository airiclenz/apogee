---
title: "Approval: the human-in-the-loop gate on a single tool call"
---

**Approval**:
The human-in-the-loop gate on a single tool call — the primary safety guarantee in
Ask-Before mode. Delivered through a delegate the host (TUI, embedder) supplies.
An *allow for this session* grant is keyed on the call's canonical, key-folded arguments, so one
executed call is one remembered decision however the model spelled it; a call whose argument keys
COLLIDE under that fold — two spellings of one parameter — is refused before it is resolved, since
no single reading of it would describe what the tool would run.
