---
title: present.command (rung-3 opener) runs a model-chosen program unapproved and unconfined in every mode (audit 2026-09-20 High)
status: done
type: bug
priority: 1
aliases: [apogee-2we]
created: 2026-09-21T20:21:40Z
closed: 2026-09-22T19:13:05Z
reason: Closed
---

Source: docs/reviews/code-audit-2026-09-20.md, High 'the rung-3 opener (present.command) runs a model-chosen program unapproved and unconfined in every mode'.
Where: cmd/apogee/wire_present.go:58-65, internal/present/opener.go:194-198,387-412, internal/presenter.go:141-152.
What: with a Local session and auto-open on, present_document is ReadOnly, so rung 3's Opener.Open(req.Path) runs in every mode, Plan included, with no gate. argv[0] is PATH-resolved at exec with no writable-path fence (rung 1 goes through security.ResolveProgram), and {path} is model-chosen into the workspace: write a script under confinement, present_document it, 'sh {path}' runs it as the user. Below Critical only because present.command is a user-set, file-only global override.
Audit fix: resolve and fence rung 3's argv[0] through security.ResolveProgram as rung 1 does; gate the opener half of present_document through the disposition when an execution-capable rung is wired (classify as an external effect, or run the opener inside the session's Confiner box). ADR 0019 §5's 'user's own configuration' rationale predates the model-triggerable tool — revisit it in the design.
