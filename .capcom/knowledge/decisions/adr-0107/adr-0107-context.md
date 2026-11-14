---
title: ADR 0107 context
---

apogee once ran an accidental pre-production v1.x series: v1.0.0 was tagged on 2026-06-25 (ADR 0001,
its "v1.0.0 cut" amendment), the project restarted at v0.x on 2026-07-23, and the v1 tags were
deleted. ADR 0001's consequences note of 2026-09-30 records the fix of the day: go.mod carries
`retract [v1.0.0, v1.8.0]`, so `@latest` resolves to the current 0.x release.

Deleting a tag does not delete what the Go module proxy and checksum database once served.
sum.golang.org lookups on 2026-11-04 found v1.1.0, v1.7.0 and v1.8.0 recorded; v1.0.0, v1.0.1,
v1.2.0–v1.6.0, v1.7.1, v1.8.1 and v1.9.0 are not. A recorded number can never ship different
content: the checksum database pins it.

Go reads retractions from the go.mod of a module's highest version. That is v1.8.0, and its
go.mod — immutable on the proxy — retracts the whole `[v1.0.0, v1.8.0]` range, itself included.
So any release inside that range is retracted the moment it is published, whatever the repo's
own go.mod says, and the range in the repo's go.mod made the same promise locally: the release
tool, the CI tagging script and the update check all refused the whole range (commit 8febcc90).
v1.0.0, the number the project wants for its first stable release, was unreleasable.

An offline rehearsal on 2026-11-05 (`GOPROXY=file://` over the real tag history) settled the
mechanism. A release inside the range on its own stays retracted. A higher tag that carries a
list naming only the burned versions, itself included, makes Go read that list instead: with
that tag on v1.0.0's commit, `@latest` is v1.0.0. A placeholder that does not retract itself
becomes `@latest` itself. Later releases (v1.2.0, v1.9.0) resolve normally. Designed in plan
`2026-11-05 - 00 - v1-placeholder-retraction-plan` (issue `v1-placeholder-retraction`).
