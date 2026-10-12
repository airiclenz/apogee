---
title: ADR 0089 context
---

ADR 0086 kept delegation blocking and rejected asynchronous delegation "for now, not denied". The
rejected shape was a spawn/message/stop/wait tool family around `sub_agent`: it would give small
models four more tools, and it would reach about ten subsystems.

ADR 0087 makes a Workflow an object that outlives a reply. It has a folder, receipts on disk, and
resume by hash. That removes most of the reasons asynchrony was hard. In the 2026-09-27 grill the
owner put background workflows in the first cut, launchable by the user and by the agent, and gave
the agent a full control tool.
