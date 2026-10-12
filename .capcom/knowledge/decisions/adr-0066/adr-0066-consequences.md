---
title: ADR 0066 consequences
---

- `ServerEntry` loses `sub-agents:`; `ValidateServers` loses both the two-flags refusal and the
  posture-on-an-unflagged-entry refusal. `fileConfig` and `Options` grow `SubAgentsServer`, the
  settings registry grows a non-editable `KindServer` row, and the seeded `config.yaml` documents
  the key beside `server:`.
- The delegation wiring grows a retarget seam and takes the `servers:` list as a reader rather
  than a snapshot: a retarget resolves its name against the list as it stands then.
- The TUI gains `/sub-agents-server` — `/server`'s grammar, whose bare form opens a picker and
  which is allowed while the model works — and a second unasked-for start-up pane.
- CONTEXT.md's **Sub-agent server** term is no longer "the flagged entry" but "the entry the root
  key names", and its "at most one entry may carry the flag" sentence is gone.
- ADR 0045 is amended, not superseded: decisions 3-7 of that record are the mechanism this key
  steers, and its deferred items stay deferred. *Amended 2026-09-01 — its **Model-chosen routing**
  item is no longer deferred (ADR 0069);
  **Launcher actuation** still is.*
