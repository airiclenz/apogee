---
title: ADR 0104 does not record the env display and env-file pins
status: open
type: task
priority: 3
created: 2026-10-18T18:44:05Z
---

ADR 0104 decision 11 (`.capcom/knowledge/decisions/adr-0104/adr-0104-decision.md:48-51`) says the adoption pane shows "env and `env-allowlist` names". During the run the user decided otherwise: the pane shows each `env:` entry in full (`KEY=VALUE`), and an in-root file an `env:` value names is pinned by content like an argv element. Decision 4 (`adr-0104-decision.md:21-27`) still names only argv elements as pinned files. What shipped (`internal/config/execpin.go` `fileCandidates`/`envValueCandidates`, `internal/tui/adoptionbody.go`, `docs/layout/user-questions-layout.md`) and the manual follow the decision; the record does not.

Deferred out of plan `2026-10-18 - 00 - project-config-exec-keys-plan`.

## Acceptance

- ADR 0104 decisions 4 and 11 (or an amendment Note) record that `env:` entries are shown in full on the pane and that an in-root file an `env:` value names is pinned by its SHA-256.
- `capcom check` passes.
