---
name: code-audit
description: >
  High-signal code review producing an actionable markdown report: real bugs
  with realistic triggers, security holes, architectural drift, dead or
  duplicated systems, and missing tests on critical paths. Aggressively
  filters noise — no style nits, no spelling, no speculative findings — and
  adversarially verifies every significant finding before it reaches the
  report. Runs the project's own analysis tools (linters, vulnerability scan,
  race detector, coverage) first and feeds their output to every reviewer.
  Use when you want a senior-engineer-level audit or deep code review of a
  package, directory, branch diff, or whole repository. For small or
  context-constrained host models use /code-audit-sequential instead.
argument-hint: "Optional: scope (files, directory, branch/PR, or 'everything'); focus areas (bugs/security/tests/standards); add 'fresh' to discard a resumable run"
---

Fixture body — see testdata/library/README.md.
