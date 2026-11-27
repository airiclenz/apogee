---
title: The mDNS lookup gives up after one second with no retransmit
status: open
type: task
priority: 4
labels: [slow-box]
created: 2026-11-27T21:27:15Z
---

`internal/mdns/mdns.go:25`: `lookupTimeout = time.Second` caps a single multicast query and reports
`errNoAnswer`; RFC 6762 recommends repeating the query.

On a power-saving Wi-Fi link or against a loaded responder, a healthy `.local` model server is
reported offline.

Fix: resend at about 250 ms and 750 ms inside a longer window of a few seconds, bounded by ctx.

Source: `docs/reviews/code-audit-2026-11-27.md` (Medium, independently verified).

## Acceptance

- A responder that answers only the second or third query is found; a test with a scripted responder pins it.
