---
title: MCP login loopback callback accepts the first authorization response
status: done
type: bug
priority: 3
aliases: [apogee-mcp-callback-first-wins]
created: 2026-10-08T18:28:52Z
closed: 2026-10-08T20:02:27Z
reason: "Fixed in a34ac788: callback and pasted redirect accept only the login's state"
---

Deferred from the implement-plan run of docs/plans/2026-10-08 - 02 - mcp-oauth-plan.md (closeout).

cmd/apogee/mcp_login_fetch.go:325 (callbackHandler) and :372 (deliver) accept the first authorization response that reaches the loopback listener, so a forged local or cross-site GET /callback carrying a wrong state, or an error= parameter, ends a login early. Denial of service only: Login's state check (ErrStateMismatch) prevents any compromise.

Also: the missing-opener note path at cmd/apogee/mcp_login_fetch.go:245-247 ("No browser opener (...) on this machine") is untested in cmd/apogee/mcp_login_fetch_test.go.
