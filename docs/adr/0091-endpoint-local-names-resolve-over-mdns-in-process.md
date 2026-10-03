---
Status: accepted
---
> Note (2026-10-03): the Mechanism layer this ADR refers to was removed; ADR 0076 (Reactions) replaces it. Read the Mechanism-specific parts as historical.

# Endpoint `.local` names resolve over mDNS in-process

## Context

ADR 0042 decision 1 ships apogee as one CGO-free binary: every release build is `CGO_ENABLED=0`.
Without cgo, Go's resolver is its own pure-Go one. It reads `/etc/hosts` and asks the unicast DNS
servers in `/etc/resolv.conf`, and nothing else. It never goes through the system's NSS stack, so
it cannot reach the mDNS responder that `nss-mdns`/Avahi (or macOS's `mDNSResponder`) put behind
`.local` names.

A server named by its mDNS name — `endpoint: http://Apollo-II.local:1111` — therefore worked in a
source build that happened to link cgo and failed in every release archive. The heartbeat carried
the raw `lookup Apollo-II.local: no such host` behind "server offline", which reads as a dead box
when it is the name that failed (bd `apogee-mdns-local-unresolved`).

## Decision

**apogee resolves an LLM endpoint's `.local` name itself, with one mDNS query, after the system
resolver has failed.**

**D1 — In-process, no cgo, no helper program.** The release builds stay `CGO_ENABLED=0`, and apogee
does not shell out to `avahi-resolve` or `dns-sd`: `internal/mdns` sends one one-shot legacy
unicast query (RFC 6762 §5.1, §6.7) and reads the answer itself. Nothing new has to be installed,
which keeps ADR 0042 whole.

**D2 — Only `.local`, only after the system fails.** The fallback fires only for a host ending in
`.local` (case-insensitive, a trailing dot allowed), and only when the system resolver returned a
DNS error. Every other host, and every `.local` host the system resolves, is looked up exactly as
before.

**D3 — IPv4 multicast, both address families.** The query goes to `224.0.0.251:5353` only, and
waits at most one second (or less, when the caller's deadline is nearer). The A and AAAA answers in
the reply are both used; each address is dialled at the endpoint's port until one connects.

**D4 — One shared transport.** The fallback lives in the dialer of one package-level transport, a
clone of net/http's `DefaultTransport` that keeps its pool limits, proxy handling and connect
timeout. Every `provider.NewClient` client built without its own HTTP client shares it. Title and
judge calls build short-lived clients, and a private pool each would strand idle sockets.

**D5 — An unresolved name is reported as one.** When neither the system nor mDNS answers, the
offline refusal keeps its headline and names the failure:
`cannot send — server offline (http://Apollo-II.local:1111): host name Apollo-II.local did not
resolve — use the server's IP address or add it to /etc/hosts`. This applies to any host name that
does not resolve, `.local` or not; a cancelled or timed-out lookup keeps its raw text.

## Considered options

- **Build releases with cgo.** The system resolver would then answer `.local` as it does for every
  other program. It ends the single static cross-built binary that ADR 0042 rests on.
- **Shell out to `avahi-resolve` / `dns-sd`.** It reuses the system's responder, but it makes a
  working endpoint depend on a program being installed, which ADR 0042 rules out.
- **Only improve the message.** The user could then fix it with an IP or an `/etc/hosts` line, but a
  name that the rest of the machine resolves would still fail in apogee alone.

## Consequences

- `.local` endpoints work in release builds; `docs/manual/configuration.md` ("The servers you run
  models on") says so and explains the unresolved-host refusal.
- Only the provider transport falls back. The llama-launcher module's own HTTP clients, MCP
  transports, the web tools, webhooks and the url-safety guard's lookup still resolve through the
  system alone.
- IPv4 multicast only: a network that carries mDNS over IPv6 alone is not reached, and the fallback
  does no service discovery and keeps no cache — each failed system lookup costs up to one second.
- Nothing here is a Mechanism. It changes how a configured endpoint is reached, not what the model
  may do, so there is nothing for Bypass to switch off.
