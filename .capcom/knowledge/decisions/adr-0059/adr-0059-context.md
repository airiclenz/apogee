---
title: ADR 0059 context
---

The 2026-08-16 tool-surface poll round's single top pick from Qwen3.8-27B was a persistent
terminal — attach to a running dev server, drive a REPL, send keystrokes to an interactive
program — recorded in `docs/design/tool-surface-findings.md` as needing a grill because it
collides with two settled contracts. ADR 0008
makes tools **stateless across Turns** so the bench can fork a run at the quiescent boundary,
and names `terminal`/`python_exec` one-shot by contract. The confinement execution contract
(§2.4) makes every subprocess run a container the call tears down on every exit path. A
persistent process is exactly what both forbid. The first round's `concurrent terminal`
denial does not cover it — that was parallelism (ADR 0039); this is interactivity. A grill on
2026-08-25 settled the shape; this record is that ratification.
