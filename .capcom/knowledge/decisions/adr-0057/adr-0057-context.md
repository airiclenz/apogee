---
title: ADR 0057 context
---

The 2026-08-16 tool-surface poll round ratified the identity framing — apogee is built for
smaller local models while working even better with bigger ones — and promoted per-profile tool
rosters as the mechanism serving both classes: the default roster stays tuned for the models
that need the help, feedback from larger models lands in a profile tier instead of re-tuning the
default, and a new tool can ship default-off/profile-enabled instead of costing every small
model a tool-list slot (`docs/design/tool-surface-findings.md`). What existed was one global
`tools.disabled:` list applied to the default tool set, and the per-model
Model profile (ADR 0044) with its two wire-shape
axes. A grill on 2026-08-23 settled the shape; this record is that ratification.
