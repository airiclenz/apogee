---
title: Move CI's Linux jobs from ubuntu-24.04 to ubuntu-26.04
status: open
type: task
priority: 4
created: 2026-10-13T18:14:13Z
---

CI's Linux jobs (`.github/workflows/ci.yml`, `.github/workflows/tag-on-version-bump.yml`) are pinned to `ubuntu-24.04` so GitHub's move of `ubuntu-latest` to Ubuntu 26.04 (by 2026-10-19) cannot change the runner under a push that changed nothing else. The pin is a holding position, not a destination: 24.04 images will eventually be retired, and the `check` job's landlock steps (`TestLandlockProbeNetwork` with `APOGEE_REQUIRE_LANDLOCK_NET=1`, the T-11 `probe` truncate-residual check) depend on the runner's kernel and landlock ABI, so the move should be proved by a run of its own.

## Acceptance

- Every `runs-on: ubuntu-24.04` names `ubuntu-26.04` (and the comments in `ci.yml` and `docs/design/test-drivers.md` T-11 row that name the image follow).
- A CI run on the new image is green, including both landlock steps; the run id is recorded on close.
- Lane count still fits the runner (`make test` sizes lanes off nproc).
