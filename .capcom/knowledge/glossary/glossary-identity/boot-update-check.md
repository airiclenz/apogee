---
title: Boot update check
---

**Boot update check**:
The interactive TUI's one request, on every boot, for the latest published release: a `HEAD` of
github.com's `releases/latest`, the tag read from the redirect. It runs asynchronously, never delays
boot, keeps no cache and carries nothing about the user, session, code or model; any failure means
no notice. When a newer release exists the startup box's version row names it and the upgrade
command for this binary's **Install channel** (`v0.24.11 → v0.25.0 · brew upgrade apogee`); apogee
never replaces itself in the background. On by default; `update-check: false` or a non-empty
`APOGEE_NO_UPDATE_CHECK` turns it off. No other **Driver** makes it — headless, the daemon, the
bench and `apogee probe` stay network-state-independent — and the code lives in `internal/update`,
imported only by `cmd/apogee`, so the engine stays wire-silent (ADR 0031). See
ADR 0097.
_Avoid_: "auto-update" (nothing is replaced without an explicit command), "telemetry" / "phone
home" (the request sends nothing about the user).
