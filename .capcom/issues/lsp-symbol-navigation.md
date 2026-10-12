---
title: "LSP / symbol navigation: named find_definition and find_references tools, LSP backend for diagnostics"
status: open
type: feature
priority: 3
aliases: [apogee-3fs]
created: 2026-09-07T10:48:54Z
---

Gap 6 of the 2026-09-06 contender gap assessment. DEFERRED by the owner 2026-09-07 (no design work, no plan yet).

Today there is no LSP and no symbol navigation; 'diagnostics' is go vet plus a detected linter. opencode auto-installs 30+ language servers and feeds diagnostics to the model; Crush uses gopls.

Fit, if it is ever taken up: do NOT auto-inject diagnostics — that is pre-failure steering and ADR 0073 denied lint-fed-to-model. Do add NAMED tools find_definition / find_references (tree-sitter or LSP backend) and an LSP backend under diagnostics. Ships default-off with a bench arm alongside the apogee-304.* arms (ADR 0057 D3 precedent). Effort L.
