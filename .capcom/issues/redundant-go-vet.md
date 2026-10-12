---
title: go vet ./... in make check is redundant with golangci-lint
status: done
type: task
priority: 4
aliases: [apogee-ilh]
created: 2026-09-06T10:33:30Z
closed: 2026-09-11T10:28:50Z
reason: delivered by docs/plans/archived/2026-09-09 - 00 - deferred-findings-plan.md (17c105c1 090b0f63 480095e0 4cb5922e f3f9b321 94978659 5fe4fcc4 300336ff edb2bbe4 9ba9619e)
---

make check runs `go vet ./...` and then `make lint`, whose .golangci.yml sets `default: standard` — a set that includes govet, with all analyzers except fieldalignment and shadow enabled. The standalone vet is a subset of what the linter already runs.

Worth 0.4s, so this is tidiness rather than speed: it is filed so the redundancy is a decision on the record instead of a thing nobody noticed. The Windows-tagged `GOOS=windows go vet ./internal/platform/... ./internal/probe/...` step is NOT redundant and must stay — golangci-lint does not run under that build tag.

## Acceptance

Either the plain `go vet ./...` step is dropped with the overlap noted in the Makefile comment, or it is kept with a recorded reason.
