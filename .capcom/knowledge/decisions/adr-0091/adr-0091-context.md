---
title: ADR 0091 context
---

ADR 0042 decision 1 ships apogee as one CGO-free binary: every release build is `CGO_ENABLED=0`.
Without cgo, Go's resolver is its own pure-Go one. It reads `/etc/hosts` and asks the unicast DNS
servers in `/etc/resolv.conf`, and nothing else. It never goes through the system's NSS stack, so
it cannot reach the mDNS responder that `nss-mdns`/Avahi (or macOS's `mDNSResponder`) put behind
`.local` names.

A server named by its mDNS name — `endpoint: http://Apollo-II.local:1111` — therefore worked in a
source build that happened to link cgo and failed in every release archive. The heartbeat carried
the raw `lookup Apollo-II.local: no such host` behind "server offline", which reads as a dead box
when it is the name that failed (bd `apogee-mdns-local-unresolved`).
