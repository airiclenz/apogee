---
title: The Windows CI job runs only part of the suite
status: open
type: task
priority: 3
created: 2026-11-27T18:35:35Z
---

The `test-windows` job in `.github/workflows/ci.yml` runs eight package trees plus two tagged test
sets, not the suite; its comment calls the rest "Linux-shaped". The Oct 12 test-suite review
found Windows "flakes" were mostly real bugs, because nothing exercised Windows before a push.
Untagged tests in packages outside those eight trees compile on Windows (`GOOS=windows go vet`)
but never run there.

Wanted: run every package whose untagged tests are portable on the Windows runner, and mark the
Linux-only tests with a build tag or a `runtime.GOOS` skip that names why, so the job's package
list is "everything except what is tagged", not a hand-kept allow list.

## Why

A Windows defect found by CI on the same push is cheaper than one found as a flake a week later.

## Acceptance

- The Windows job runs all packages by default; a package or test left out says why in its own source.
- The job stays within the CI time budget (record the before/after run time in the log).
- No new fixed deadline is added to make a Windows test pass.
