---
title: ADR 0100 considered options
---

- **XDG base directories on Linux** (`$XDG_CONFIG_HOME`, `$XDG_STATE_HOME`, …). Rejected. They
  split one tool's state over several roots and differ from the macOS and Windows layouts, so
  docs, support answers and tests would each need a per-OS table.
- **`os.UserConfigDir` per OS.** Rejected for the same reason: a different home on every OS,
  and on macOS a path with a space that every shell example has to quote.
