---
title: sub_agent tools argument shape refused on first call
status: done
type: bug
priority: 2
labels: [tool-surface]
plan: 2026-11-10 - 01 - panel-and-tool-shape-fixes-plan
created: 2026-11-10T09:47:30Z
closed: 2026-11-10T12:51:54Z
reason: "Resolved by plan '2026-11-10 - 01 - panel-and-tool-shape-fixes-plan' items 2-3 (archived at f53b8f0d): sub_agent, fan_out and workflow stages decode [\"read-only\"] and a string-encoded array; unknown names/keywords still refused naming both shapes."
---

The `sub_agent` `tools` argument is a union (`"type": ["string", "array"]`): the string
`"read-only"` or an array of tool names (`internal/tools/sub_agent.go`, `subAgentSchemaTemplate`
and `SubAgentRoster.UnmarshalJSON`). Models get the shape wrong on the first try, and each miss
costs a whole parent Turn before the retry.

Evidence from `~/.apogee/sessions/` (all `z-ai/glm-5.3-flash`): in the three November sessions
that set `tools`, **every first call that set it was refused (6 of 6)**; the retries all ran.

| Session | Sent | Refusal |
|---|---|---|
| `20261108T180314Z-ab4bf4f2` (×2), `20261110T075755Z-e4e0975d` (×2) | `"tools": ["read-only"]` | `sub_agent tools: unknown tool read-only — name tools from your own menu` (`internal/agent/subagent.go`, `requestedChildTools`) |
| `20261109T183741Z-eb8a62ea` (×2) | `"tools": "[\"read_file\", \"list_dir\"]"` (JSON array encoded as a string) | `tools: unknown keyword "[\"read_file\"…]" — use "read-only" or an array of tool names` |
| `20260922T155525Z-b3185938` (×2, model unrecorded) | same string-encoded array | same |

Two failure shapes:

1. **Keyword wrapped in an array** — `["read-only"]`. The meaning is unambiguous (no tool is named
   `read-only`), yet the refusal tells the model the keyword is an unknown *tool* and to "name
   tools from your own menu", which steers it away from the keyword it meant. In the 2026-11-10
   session the model then listed tools by hand instead of using the read-only set.
2. **Array encoded as a JSON string** — the string branch of the union sees a keyword it does not
   know. The refusal even names the right shape, but a model that emitted this once tends to.

Fix options (pick in design):

- **Decode both shapes as what they mean** (no model-facing change, so it cannot regress Bypass):
  an array whose only element is `"read-only"` → the read-only set; a string that parses as a JSON
  array of strings → that array. Keep refusing a genuinely unknown keyword or tool name.
- **And/or reshape the schema** so it is not a union, which smaller models and grammar-constrained
  servers handle poorly — e.g. array-only with `read-only` as a recognised item, or a separate
  boolean. This changes what the model sees, so it needs bench evidence (AGENTS.md hard invariant).
- At minimum, the `["read-only"]` refusal should say to pass `"read-only"` as the whole value,
  not as a tool name.

Check whether `fan_out` / workflow spawns (`internal/agent/workflowspawn.go`, which also uses
`SubAgentRoster`) carry the same union and take the same fix.

## Why

Each refusal costs a full parent Turn on exactly the small models apogee is built for; 6 of 6 first tries failed for glm-5.3-flash in November.

## Acceptance

- `"tools": ["read-only"]` runs the delegation with the read-only set (unit test on SubAgentRoster / requestedChildTools).
- A string holding a JSON array of tool names runs the delegation with that array, or the chosen schema reshape makes that shape impossible; either is pinned by a test.
- An unknown keyword or tool name is still refused, with a message that names the accepted shapes.
- The fan_out / workflow spawn path that shares SubAgentRoster behaves the same.
