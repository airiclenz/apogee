---
name: test-checklist
description: >
  Compiles a release test checklist for everything implemented since the last
  officially cut release (GitHub release, Homebrew formula, package registry,
  tag, CHANGELOG), plus work in progress cross-checked against implementation
  plans. Runs the automatable part itself — full suite, per-item targeted
  tests, throwaway probes, localhost browser checks — and writes numbered
  step-by-step instructions for everything that needs a human. Saves
  docs/test-checklists/DATE - NN - NAME.md; a 'record' mode ticks off
  manual results. Use when the user asks "what do I need to test", "testing
  checklist", "test guideline", "what changed since the last release and how
  do I test it", or wants a test plan before cutting a release. Do NOT use for
  judging code quality (use code-audit). For small or context-constrained
  host models use /test-checklist-sequential instead.
argument-hint: "Optional: 'since TAG'; 'name: SLUG'; 'no-run' (compile only, execute nothing); 'fresh' (discard a resumable run); 'record: T-03 pass, T-05 fail: NOTE' (tick off manual results)"
---

Fixture body — see testdata/library/README.md.
