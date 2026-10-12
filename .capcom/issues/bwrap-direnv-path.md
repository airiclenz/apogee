---
title: bwrap resolves on a PATH a workspace's direnv can reach
status: done
type: bug
priority: 3
aliases: [apogee-bwrap-direnv-path]
created: 2026-10-02T08:51:27Z
closed: 2026-10-02T22:20:35Z
reason: "Resolved by plan 2026-10-02 - 02 - refocus-findings-plan, archived at 4fa71d90 (item 6: bwrap only from fixed system directories, fails closed)"
---

The namespace confiner (internal/platform/namespace_linux.go NewNamespaceConfiner) resolves bwrap with a bare exec.LookPath once per process, at construction, before any session workspace exists — the one program not resolved through security.ResolveProgram (confinement-execution-contract.md amendment 2026-10-02; plan 2026-10-02 - 00 item 4). When apogee is started from a shell whose PATH a workspace manages (direnv's .envrc PATH_add, a project .venv/bin activated by the shell), a bwrap planted in that workspace directory is the launcher every confined command then runs through. The fence cannot apply at construction because there is no root yet. Options: re-check the resolved bwrapPath against the session workspace with security.RefuseExecFromWritablePath when the box is built (Confine), refusing the namespace backend for that session; or resolve bwrap from a fixed system path list. Needs an owner call on which, and on whether a refused bwrap degrades to unconfined-with-warning or blocks.
