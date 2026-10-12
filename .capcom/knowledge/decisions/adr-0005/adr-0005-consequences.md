---
title: ADR 0005 consequences
---

- The Go sub-agent orchestrator **must** be constructed with the parent's mode, approval
  delegate, confiner, and guardrails threaded in — this is a required signature change from
  the TS source, not an optional one.
- Default sub-agent tool set = the parent's set; callers narrow it per task. (A default the
  catalogue/tooling work can revisit.)
- Sub-agent events nest into the parent's event stream so the TUI and bench observe them.
