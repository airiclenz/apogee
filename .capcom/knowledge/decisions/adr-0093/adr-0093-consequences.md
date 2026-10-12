---
title: ADR 0093 consequences
---

- `internal/config` gains the root `currency:` key and the per-entry `price:` key. Both are
  documented in the configuration manual.
- `domain.Usage` and the session record's usage carry the integer amount and the priced and
  unpriced call counts. The record carries the currency label as well.
- The engine prices each call where it records usage, from the price on the agent's server
  binding.
- The amount appears on the `/usage` pane, the footer, the `/sessions` spend cell, and headless
  JSON and text output. Each shows the label and marks a partial amount.
- CONTEXT.md defines **Price** and **Spend (money)** beside **Context cost**.
