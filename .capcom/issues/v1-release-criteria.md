---
title: The v1.0.0 release criteria and stability promise
status: open
type: task
priority: 3
created: 2026-11-14T10:40:46Z
---

What must be true before v1.0.0 is cut, and the stability promise that v1.0.0 makes. ADR 0107
([`adr-0107`](.capcom/knowledge/decisions/adr-0107.md)) settles only the number: v1.0.0 is
releasable through a self-retracting placeholder tag, once a 0.x bridge release has shipped the
list-based update check (issue `v1-placeholder-retraction`). It deliberately left these two
questions open.

**The v1 criteria** are the parked work the owner has named as blocking a stable release:

- `windows-auto-temp-caches` — Windows Auto: box-local `%TEMP%` and toolchain caches.
- `phase5-owner-verification` — the owner-run Phase-5 verification passes.
- `unsigned-release-binaries` — the published binaries are code-signed.

**The stability promise** is what v1.0.0 freezes and how it may grow. ADR 0001's "v1.0.0 cut"
amendment of 2026-06-25 named a frozen public surface (the root `apogee` package, its host
delegates, the `Mode` ladder, the tool registry, the `Event` variants); that promise was
overtaken by the 2026-07-23 restart at v0.x, and the surface has moved since. A new ADR states
the promise for the real v1.0.0: which packages and types are covered, what is additive (a
minor bump) and what is a break, and how the Go module, the binary's CLI and the config keys
are each versioned.

Cutting the bridge release and v1.0.0 stays the owner's act; this issue holds the criteria, not
the cut.

## Why

v1.0.0 promises stability to every Driver and importer. ADR 0107 only made the number releasable, so the criteria and the promise need one place, readable before the cut.

## Acceptance

- `windows-auto-temp-caches` is closed, or the owner has recorded here that it does not block v1.0.0.
- `phase5-owner-verification` is closed, or the owner has recorded here that it does not block v1.0.0.
- `unsigned-release-binaries` is closed, or the owner has recorded here that it does not block v1.0.0.
- A stability-promise ADR is accepted in the `decisions` tree, naming the covered surface and what counts as a minor bump versus a break.
- `v1-placeholder-retraction` is closed and a 0.x bridge release carrying the list-based update check is published.
