---
title: "MCP OAuth binding is one value (architecture review 2026-10-14 #17)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#17), re-verified 2026-11-05 at f7ec72b6. `apogee mcp login` and connect still each assemble the same OAuth facts: loginMCPServer and mcpClientSecret (cmd/apogee/mcp_cmd.go) through the exported mcp.EndpointClient, and newOAuthHandler (internal/mcp/transport.go) reading client-secret-env and vetting the endpoint itself, with different error sentences. The review proposes that internal/mcp assemble one OAuth binding (name, vetted endpoint, store, clients, client id, secret) that both LoginConfig and HandlerConfig embed, so the secret is read in one place and EndpointClient's export goes away. Its symptom (a refresh after a client-id change sending the stored client id with the new secret) was fixed by item 6 of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md (a client-id mismatch is a missing login, mcpauth clientMatches); the review notes this departs from ADR 0095 D1's name + endpoint token key and would want a dated amendment.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
