---
title: ADR 0104 context
---

ADR 0076 decision 10 made the repo layer's execution keys — a Reaction's `run:` / `advise:` /
`gate:` — "proposed, not live" until adopted. ADR 0096 built the Project config and Adoption for the
one granting key it admitted, the Allow rule, and left `mcp-servers:` and `reactions:` global-only;
both its consequences and ADR 0076's 2026-10-10 amendment filed them as a follow-up (issue
`project-config-exec-keys`). This record is that follow-up, grilled on 2026-10-18.

What the code had when the grill opened:

- **Adoption pins a line of text.** `adoption.Entry{Kind, Text}` is fingerprinted as
  SHA-256(Kind + NUL + Text), Text being the rule exactly as the file spells it. A structured entry
  has no such line, and ADR 0076 decision 10 frees some of a reaction's fields from the pin.
- **Both kinds have a unique handle.** An MCP server's `name` (also its tool prefix and what an Allow
  rule matches) and a reaction's `id`.
- **Global entries are trusted as the user's own.** A stdio MCP server inherits apogee's whole
  environment when `env-allowlist` is unset ("the stdio command is chosen by the host in global
  config"); an MCP endpoint and a reaction webhook skip the resolved-IP floor because they are
  "config-file-only and never model-supplied"; `headers-env` sends any named variable as a header.
- **`run:` is unconfined** in every mode, fenced only at the program: a program that resolves inside
  the workspace is refused. `advise:` and `gate:` argv run inside the workspace exec fence when
  `confine-to-workspace` is on. An interpreter argv (`sh scripts/x.sh`) passes the program check, and
  a text pin would not notice the script changing.
- **Allow rules match a server by name only**, so a rule written for one server covers any server
  that later takes its name.
- **Live reload reads the global file only** (`reconnectMCP`, `reloadReactions`); headless and the
  daemon fire reactions but dial no MCP server.

A repo file can be written by the model (ADR 0096 §5 refuses the forms it sees; the pin is the
guarantee), so "the user chose it" holds for a repo entry only once it is adopted.
