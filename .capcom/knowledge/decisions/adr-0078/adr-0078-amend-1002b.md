---
title: "ADR 0078 amendment of 2026-10-02: the no-effort shape is per model"
---

Supersedes the first bullet of the amendment above ("The thinking mode follows the resolved
effort") for requests whose effort resolves to `off`, `none`, `minimal` or nothing, and its third
bullet ("The compaction summary never requests thinking on this wire") for the models below. A
named effort from `low` up is unchanged: `thinking: {"type": "adaptive"}` beside
`output_config.effort`.

`thinking: {"type": "disabled"}` is not a shape every current model accepts. Opus 5.5, Fable 5.x
and Mythos 5.x answer it with a 400 and have no off switch at all; Sonnet 5.5 answers it with a
400 but accepts `between_tools`. Because compaction summaries and the session-naming call both
force the off rung, every compaction on those models failed (bead
`apogee-anthropic-thinking-disabled-400`).

- **The no-effort shape comes from a prefix table on the model id.** The codec matches the
  request's model id, ignoring case, against a short table of id prefixes, and the longest match
  wins:

  | Model id starts with | Body for an effort below `low` |
  |---|---|
  | `claude-opus-5-5`, `claude-fable-5`, `claude-mythos-5` | no `thinking` key, and `output_config.effort: "low"` |
  | `claude-sonnet-5-5` | `thinking: {"type": "between_tools"}`, and no effort |
  | anything else | `thinking: {"type": "disabled"}`, and no effort, byte-identical to before |

  On the first row "no effort" means the least thinking the model allows, not none.
- **A new model needs a table row.** The table is a denylist in `internal/provider`, not a
  capability apogee discovers: the Messages API advertises no tell. A future model that also
  refuses `disabled` keeps getting `disabled`, and failing with a 400, until it has a row. An
  allowlist would have broken every model apogee does not know, including proxies that rename
  models.
- **Only `disabled` keeps the profile's sampling.** The `between_tools` shape and the
  no-key-plus-`low` shape both let the model think, so the body leaves out `temperature`, `top_p`
  and `top_k` exactly as it does for adaptive thinking.
- **The effort hint follows the requested effort, not the body.** The no-key-plus-`low` shape
  writes `output_config` for a request that named no effort. A fault on such a request is still
  reported as one that carried no effort, so it never gains the hint that blames an effort level.
- **Compaction and naming get the same mapping.** Both force the off rung, and the codec maps it
  per model like any other request. On Opus 5.5, Fable 5.x and Mythos 5.x the summariser therefore
  runs with adaptive thinking at effort `low`. The capped-summary fault says the summariser
  "asked for as little reasoning as this server allows" rather than "asked for no reasoning".
