---
title: ADR 0044 considered and rejected
---

- **Keep the global block, just document it better.** Does nothing about the churn, and nothing at
  all for the human who does not yet know the tag shape.
- **Gate the shipped table on fingerprint confidence** (the Validated-set rule): kills the
  out-of-box case — see decision 2.
- **Per-axis merge of a user entry over a shipped one**: needs an 'inherit' spelling; the same
  ambiguity replace-whole exists to avoid (decision 4).
- **A separate `disable-shipped-profiles:` off-switch**: a user entry already expresses it, and a
  second key would need its own precedence story.
- **A back-compat shim that reads the old key as a `*` pattern**: two spellings for one thing,
  forever, in a pre-production tool.
- **JSON/embedded-bundle machinery for the table** (the Validated-set bundle shape): three entries
  of declarative Go data need no loader.
