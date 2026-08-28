---
name: security-audit
description: >
  Find and triage latent vulnerabilities in the workspace, ranked by whether
  they fire on a stock install, and produce a structured report. Sweeps
  eighteen check families — injection, secret exposure, access control, web
  surface, deserialization, memory safety, resource exhaustion, plus the
  decision-surface, trust-class, workspace-namespace and process-lifecycle
  families that agentic programs actually fail — selected from the codebase
  inventory and the program's archetype, then verifies every candidate against
  the project's written security posture before it reaches the report.
  Sub-agents do the heavy reading so orchestrator context stays bounded.
  Read-only: it never edits code, runs nothing, and reaches no network. Use when the user asks for a security audit,
  a vulnerability review, a threat assessment, or says "audit this for
  security", "find security holes", "check for vulnerabilities", "is this
  exploitable". Do NOT use for general code quality, bug hunting, or
  architectural review (use code-audit).
---

Fixture body — see testdata/library/README.md.
