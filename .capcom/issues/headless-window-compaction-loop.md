---
title: Unpinned headless run compacts on a guessed window and loops
status: done
type: bug
priority: 2
plan: 2026-10-08 - 01 - firing-window-and-silent-drops-plan
aliases: [apogee-headless-window-compaction-loop]
created: 2026-10-08T12:00:02Z
closed: 2026-10-08T14:51:30Z
reason: "Resolved by plan '2026-10-08 - 01 - firing-window-and-silent-drops-plan' (items 1-3; archived at ec0827f2): unpinned Firing binds the observed window, honest unknown-window notice, saturated fold stops re-folding every Turn."
---

Seen 2026-10-08 during the apogee-l8s capture: apogee headless --endpoint <OpenRouter via proxy> --model ~deepseek/deepseek-v4-flash-latest --recipe audit internal/stubllm/. /v1/models advertised context_length 1048576 for the model, but an unattended Firing binds only a context-window: pin (cmd/apogee/wire_firing.go ~395-430, deliberate per its comment), so headless printed notice.WindowUnknown: 'context window unknown — automatic compaction and the Budget are inactive'. Compaction did run anyway, at depth 1 (the recipe's ground-truth child), against a conservative assumed bound: prompts stayed at about 14-15k tokens every turn, a compaction call fired almost every turn, and three times it failed with 'compaction could not bring the history under its allocation: the protected prefix (system prompt + first user message) and the compaction summary together exceed it; ... no context window is known for this model, so apogee is bounding what it sends by a conservative assumption'. The child kept losing context and re-orienting ('Let me read the key documents', 20+ times). It spent 66 turns and 50 minutes in stage 4 of about 20 before the run's timeout killed it. Two defects: (1) the announced behaviour ('automatic compaction inactive') contradicts the actual one (compaction on a guessed bound); (2) an unpinned headless or daemon run against a server that advertises its window is effectively unusable for long tasks. Decide whether a Firing should bind the advertised /v1/models window (the comment says aligning with sessions 'would mean changing what a Firing binds'), and whether the conservative bound should exist when the notice says compaction is off. Evidence was in the session scratchpad: headless.jsonl error events at seq 2384, 8917, 17758; session 20261008T101419Z-7f7a835a in ~/.apogee/sessions.
