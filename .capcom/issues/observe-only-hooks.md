---
title: "Hooks: observe-only Driver-side reactions to engine events (ADR 0073)"
status: done
type: feature
priority: 2
plan: 2026-09-06 - 01 - hooks-plan
aliases: [apogee-20w]
created: 2026-09-06T13:17:07Z
closed: 2026-09-06T19:16:59Z
reason: Implemented by docs/plans/2026-09-06 - 01 - hooks-plan.md (all 11 items); changelog entries under [Unreleased].
---

Grilled 2026-09-06; design settled in ADR 0073 and CONTEXT.md (Hook, Hook event). Deliver: internal/hooks runner (EventSink decorator, per-hook ordered bounded queue, Driver reporter), two additive event variants (TurnEvent; ApprovalEvent requested+decided with phase), five Hook events (exchange-finished, turn-finished, file-changed, approval-waiting, error), global hooks: config row (argv command / webhook, workspace filter, timeout, headers env-ref, live-reload), wiring at TUI bridge sink + run.Spec.Config.Events for headless/daemon, facade re-export, /settings row, manual page, stubllm-driven tests. Denied (do not re-open): plugins, veto/pre-* hooks, lint-feedback to model, repo-local hook files. Next step: /implement-plan Write mode produces docs/plans/ plan owning this bead.
