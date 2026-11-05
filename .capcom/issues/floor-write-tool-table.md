---
title: "Floor guards read declared tool facts (architecture review 2026-10-27 #11)"
status: parked
type: task
priority: 4
created: 2026-11-05T19:56:21Z
---

Found by docs/reviews/archived/architecture-review-2026-10-27.html (#11), re-verified 2026-11-05 at f7ec72b6. The review rates it speculative. Which tools write a named file is still known in three places kept in step by hand: the workspaceScopedWriter marker in internal/tools, the wave4WriteTools name table in internal/floor/toolnames.go, and a mirror map in internal/agent/writedetection_test.go; toolCallPath in internal/floor/conversation.go guesses the path from several argument key spellings, so move_file's source is invisible. The review proposes stamping MutatesFile and the path-argument keys on domain.ToolDef where the menu is built, as ReadOnly already is, with the Floor guards preferring declared facts and keeping the sim spellings as a fallback for MCP and bench menus; internal/floor would still import only internal/domain. It hinges on ADR 0071 Decision 1 clause (c) and the apogee-sim parity pin: any change to a Floor guard's decision on apogee's or the sim's menu, tracking move_file's source included, needs bench evidence first.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
