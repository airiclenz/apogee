---
title: "ADR 0071 decision 3: A row may retire on a ratified verdict; the gemma Validated set retires with it"
---

**A catalogued row may be retired on a ratified owner verdict, not only when it is inert by
construction.** This supersedes ADR 0016's
2026-08-29 "inert by construction" precondition for joining `RetiredIDs()`. Fourteen rows retire on
that licence — `stall_nudge`, `list_nudge`, `tool_use_directive`, `decompose`,
`guided_decomposition`, `filehint`, `read_loop`, `read_repeat`, `truncate_history`, `library`,
`toolfilter`, `error_enrichment`, `syntax`, `autofix` — and their source, tests and assets are
deleted, on the `grammar` precedent.

> **Amended 2026-09-27 (ADR 0087).** A Workflow is not `guided_decomposition` returning. That row told
> the model how to plan its own work and dispatched delegations it never asked for; a Workflow runs
> only a fan-out the model asked for in one `fan_out` call, or a Recipe a human wrote, and the engine
> plans nothing.

What the amendment *bought*, though, was a graceful-degradation rule: a user's Validated entry
naming a retired id sheds that id and still applies, rather than being skipped whole. That rule
survives verbatim and is now load-bearing, because a retired row here is no longer guaranteed
inert. It rests on a narrower claim than before: what a user's `~/.apogee/validated/*.json`
recorded was correct for the build that recorded it, and a curation change of ours must not
silently cost them their model's set.

**The shipped gemma Validated entry retires.** Its evidence is a leave-one-out campaign over a
fifteen-member stack, of which nine members no longer exist; a measured set is its members, so the
evidence is void without them and re-writing the entry over the survivors would publish a stack no
campaign ever measured. The shipped roster ends empty. The `validated` package, the user-directory
surface, `probe model` and the rebind path all stay — a user's own entry under a retired key still
resolves and still applies, shedding what retired.
