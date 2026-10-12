---
title: Safety guardrails
---

**Safety guardrails**:
Apogee's production safety set: Agent modes, Approval, path-safety (TOCTOU-safe at use time via a
Go 1.26 `os.Root` pinned at the workspace root — `security.Fence.WriteFile`/`SafeReadFile`, so an
escaping symlink component swapped after the check is refused at write/read time), **url-safety**
(the network tools' `URLGuard` — scheme/host allow-deny plus a **default-on SSRF floor** that denies
loopback / private / IMDS / link-local **plus** RFC-6598 CGNAT `100.64/10`, the whole `0.0.0.0/8`,
TEST-NET / benchmark ranges, NAT64-embedded private/loopback under the `64:ff9b::/96` well-known
prefix (decoded, since RFC 6052 fixes it at `/96`), and — denied **wholesale**, since no decode of
them is sound or wanted — the RFC 8215 NAT64 local-use prefix `64:ff9b:1::/48` (which fixes no
translation length, so the embedded v4's bit offset is the operator's choice) plus the obsolete v6
transition / site-local ranges (6to4 `2002::/16`, IPv4-compatible `::a.b.c.d`, `fec0::/10`) — all
**by resolved IP**, re-checked at dial time so DNS-rebinding is closed; tighten-only, never
dissolvable by config —
and applied through the **one network funnel** every one of Apogee's own network tools reaches the
network through, so such a tool cannot reach the network unfiltered, and one that does not route
through it is not vouched for and gates; the network tools and the MCP transports honour the
process's `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` the way the LLM client already does, and the
order of judgement is unchanged by one: the guard judges the **destination** before the request
leaves, while the dial-time control pins the **proxy's own** resolved addresses, since the proxy
is what a proxied transport actually dials), tool-argument-guard (incl. the **Dangerous-action guard**
floor, the `http_request` header filter, a leading-`-` guard on git ref args, and **network
failure-message redaction** — every network tool's failure message names only the bare host, never
the key-bearing request URL), circuit-breaker, and a
**audit trail that is the `EventSink` itself** (one `domain.AuditEvent` per call decision, so the
trail is observable — a sub-agent's events reach the parent observer at `Depth>0`, not lost with the
discarded child).
The human-in-the-loop model — distinct from Confinement (OS-level) and from the bench's Sandbox.
_Avoid_: "the sandbox" (Apogee production is **not** sandboxed; "Sandbox" is a bench term
for the bench's `RealSandbox` that confines *unsupervised* sim runs — do not use it for
Apogee's production execution).
