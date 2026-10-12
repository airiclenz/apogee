---
title: ADR 0026 context
---

ADR 0023 gave apogee a
system prompt: a template the user writes once in `~/.apogee/config.yaml` and apogee renders into
the first system message of every request. That prompt is **global** — it lives in the home
directory and travels with it — which is right for what it says: who apogee is, how you want to be
answered, which mode it is in.

It leaves the other half unsaid. Every repository has standing facts a coding agent needs before its
first useful move — where knowledge lives, what "done" means here, the conventions no file states
outright — and they belong to the **project**, not to the machine or to the person. The tooling
around apogee already settled on a convention for exactly this: a Markdown file at the repository
root, `AGENTS.md` (Claude Code's `CLAUDE.md`, the same shape), which agents fold into their standing
instructions. This repo keeps one. Without support for it, a user's only options are to paste the
project's conventions into a global prompt — where they follow apogee into every *other* project —
or to retype them into the first message of every session.

What makes this more than "read another file" is that the file is **not the user's own text**. A
global `system-prompt-file` is written by the person who configured apogee; a workspace `AGENTS.md`
is written by whoever wrote the repo, and apogee will meet ones it has never seen, in repos it was
handed a minute ago. Everything below follows from taking that seriously: the content can never be
allowed to fail apogee's startup, can never be interpreted, and can never move under a running
session.

This ADR records the design the owner ratified in the 2026-07-28 grill, implemented by
`docs/plans/2026-07-28 - 00 - workspace-context-files-plan.md`. It **supersedes nothing** and amends
ADR 0023's zero-byte anchor (§4 below, with a dated note in 0023 pointing here).
