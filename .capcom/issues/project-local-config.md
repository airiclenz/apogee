---
title: Project-local config with a trust gate
status: done
type: feature
priority: 3
plan: 2026-10-10 - 00 - project-config-and-allow-rules-plan
aliases: [apogee-089]
created: 2026-09-07T10:48:55Z
closed: 2026-10-10T17:41:34Z
reason: "Resolved by plan 'project-config-and-allow-rules' (docs/plans/archived/2026-10-10 - 00 - project-config-and-allow-rules-plan.md, archived at 96463ed4): layered Project config, key classes, Adoption trust gate, Source split; residual gap tracked in apogee-settings-source-mark-stale."
---

Gap 9 of the 2026-09-06 contender gap assessment. DEFERRED by the owner 2026-09-07 — GRILL BEFORE ANY PLAN.

Today there is exactly one ~/.apogee/config.yaml and no project-local config; ADR 0073 (hooks) leans on 'global only' as part of its safety argument. Rivals ship .opencode/, .crush/, .claude/settings.json.

Global-only is a SECURITY DECISION today, not an oversight: a project-local config file is attacker-controlled content in any cloned repo. Anything here needs a trust gate on pi's project_trust model. This bead is a grill, not a plan. Effort M.

## Log

- 2026-09-08 beads: this bead is now the STAGE 2b grill of the Reaction core. ADR 0076 D10 ratified the doctrine (embedded -> ~/.apogee -> repo; parameters free, execution keys proposed-not-live; adoption pins the entry hash) but ADR 0076 amendment A2 pulled it out of stage 2 because three pieces do not exist and one rule is unsafe as written: (a) D10's key-class table would hand a repo layer confine-to-workspace, unconfined-hosts, tools.disabled and url-safety.deny-hosts, all GlobalOnly by ADR 0012, and names neither GlobalOnly nor the tighten-only shape already sitting unfed in security.MergeDangerousRules' projectAdd; (b) 'the repo config path is on the tool write deny list' describes machinery that does not exist - there is no path deny list and ~/.apogee is only TierForceApproval (ADR 0049 s4); (c) nothing in the tree hashes a config entry or models workspace trust; (d) internal/filewatch is single-file by construction (ADR 0041) and wire_settings.go re-reads a.configPath at five sites. Also owed: which file a /settings edit writes under layers, and the Source split (default|global|repo|env|flag) that SourceFile='' currently collapses.
