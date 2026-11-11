---
title: A non-directory writable root makes landlock_add_rule fail and takes down every confined call
status: open
type: bug
priority: 3
plan: 2026-11-11 - 00 - pdf-parser-fork-and-defects-plan
created: 2026-11-10T19:37:31Z
---

Source: docs/reviews/code-audit-2026-11-10.md (Medium, "A file in `confine-writable-paths` breaks every confined call on the landlock backend"). Verified PARTLY (real but latent) 2026-11-10 at a5c4f2db.

Corrected: no `confine-writable-paths` config key exists (internal/domain/config.go:167-173; ADR 0012 amendment 0914 note), so no shipped build hits this today — shipped boxes list only the session scratch directory. The trigger is an embedder (or a future key) setting `domain.Config.ConfineWritablePaths` to a regular file, which the ADR 0031 north star makes worth fixing.

**Where:** `internal/platform/landlock_linux.go:368-376` (writable-roots loop in `applyLandlock`) and `allowWriteBeneath` (`:418-446`).

**What:** every writable root gets `accessMaskForABI(abi)`, which carries directory-only rights (MAKE_*, REMOVE_*, REFER). For a non-directory `parent_fd`, `landlock_add_rule` returns EINVAL, so `applyLandlock` fails and every confined call fails. bwrap (`--bind`) and seatbelt (`subpath`) accept the same box. The /dev/null exemption already handles this with `deviceAccessMaskForABI`, and `allowWriteBeneath`'s doc claims a file root works.

**Suggested fix:** `fstat` the O_PATH fd; when not a directory, intersect the mask with the file-applicable rights (the `deviceAccessMaskForABI` set: WRITE_FILE, plus TRUNCATE at ABI ≥ 3). Or refuse a non-directory root with a clear error at box construction. All three backends should give the same answer.

## Acceptance

- A box whose WritablePaths holds a regular file confines successfully on landlock with writes to that file allowed and elsewhere denied, or all three backends refuse such a box the same way with a clear error.
- A test in internal/platform/landlock_linux_test.go covers a file root (skipping where landlock is absent), and the allowWriteBeneath comment matches the behaviour.
