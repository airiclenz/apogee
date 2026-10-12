---
title: ADR 0091 decision
---

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
