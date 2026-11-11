---
title: apogee update cuts off a slow but progressing archive download at 2 minutes
status: done
type: bug
priority: 2
labels: [slow-box]
created: 2026-11-10T19:35:12Z
closed: 2026-11-11T06:36:34Z
reason: resolved by audit-defects plan (2026-11-10 - 02 - audit-defects-plan), archived at e5e5bb54
---

Source: docs/reviews/code-audit-2026-11-10.md ("`apogee update` fails on a slow but progressing download"). Verified 2026-11-10 at a5c4f2db.

**Where:** `internal/update/stage.go:29` (`downloadTimeout = 2 * time.Minute`), `newDownloadClient` at `:143-148`, `download` at `:155-177`.

**What:** the asset client sets `http.Client{Timeout: downloadTimeout}`, which in Go bounds the whole exchange including reading the body, so a transfer that is still moving is aborted at 120 s. The v0.34.0 linux/arm64 archive is ~17.2 MB, so any link slower than ~143 KB/s cannot finish, and a retry fails the same way.

**Why it matters:** apogee runs on throttled laptops and SBCs over slow or metered links. The comment at `:27-28` ("still ends a stalled transfer") shows the intent was to catch a stall, but a total timeout cannot tell a stall from a slow transfer.

**Suggested fix:** drop `Client.Timeout`. Bound the stall: `Transport.ResponseHeaderTimeout` (plus dial/TLS timeouts) and a body reader that resets an idle deadline after each successful read. Keep a much larger total backstop if any, and the existing `maxArchiveBytes`/`maxChecksumsBytes` ceilings and checksum check.

## Acceptance

- A test server that trickles a body slowly but steadily past the old 2-minute total (scaled through a test knob) completes the download.
- A server that stops sending mid-body fails within the idle bound with an error that names the stall.
- The size ceilings and SHA256SUMS mismatch refusal are unchanged.
