---
title: MCP servers and Reactions in the Project config, adoption-gated
status: open
type: feature
priority: 3
aliases: [apogee-project-config-exec-keys]
created: 2026-10-10T09:03:43Z
---

ADR 0096 out-of-scope: mcp-servers and reactions are execution keys ADR 0076 D10 admits from a repo layer only as proposed-not-live entries pinned by Adoption. Extend the 0096 granting class and the adoption pane to them.

## Log

- 2026-10-17 Airic Lenz: 2026-10-17 plan-scoping: left out of the quit-keys/stage-kinds/daemon-layer plan; needs a grill + ADR 0096/0076 amendment first. Open calls: (a) what text Adoption pins for a structured entry (YAML slice vs re-encoded node vs canonical JSON); (b) pin the whole entry or only execution keys (does editing on:/timeout: re-propose?); (c) merge with global entries — append, override by name/id, or refuse a collision; (d) adoption kinds must differ from the Allow kind 'mcp-servers' (e.g. mcp-server / reaction); (e) adoption pane needs a body showing argv/transport/endpoint/env/headers-env names verbatim — layout mockup; (f) fence for repo reactions (run: argv unconfined today) and workspace: default; (g) secret leakage — repo webhook headers-env naming API keys, repo stdio server inheriting the env when env-allowlist is nil, MCP endpoint's url-safety exemption; (h) allow rules over repo servers; (i) apogee mcp login/listing over proposed servers; (j) live reload — reconnectMCP/reloadReactions read the global file only (wire_settings.go); (k) /settings summaries and the Allow rules section; (l) headless firing adopted repo reactions. Seams: config.KeyClass rows mcp-servers/reactions, layer.go projectWalk.key, allow.go projectAllowRules, adoption.Entry/Fingerprint, tui/adoption.go typed on domain.AllowRule. ~7-9 items once decided.
