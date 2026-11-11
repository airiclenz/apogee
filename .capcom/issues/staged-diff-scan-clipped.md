---
title: The commit secrets scan reads only the first 256 KiB of the staged diff and passes
status: done
type: bug
priority: 2
labels: [security]
created: 2026-11-10T19:34:27Z
closed: 2026-11-11T06:36:33Z
reason: resolved by audit-defects plan (2026-11-10 - 02 - audit-defects-plan), archived at e5e5bb54
---

Source: docs/reviews/code-audit-2026-11-10.md (Medium, "The commit secrets guard can scan a clipped diff and pass"). Verified 2026-11-10 at a5c4f2db.

**Where:** internal/gitexec/gitexec.go `queryDiagnosed` (behind `Host.Query`, `Run`, `RunDiagnosed`); caller internal/agent/secretsguard.go `scanStagedSecrets` → `runShadowGit`.

**What:** `Query` captures git's stdout in a CappedBuffer limited to `subprocess.MaxSubprocessOutputBytes` (256 KiB). `queryDiagnosed` turns a timeout or a wedged drain into `ErrIncomplete` but ignores `res.Truncated`, so a clipped stdout comes back as a complete, successful answer ending in the truncation marker. `scanStagedSecrets` runs `git diff --cached` (and the `--name-only -z` listing) through that path and scans whatever came back.

**Why it matters:** a staged diff over 256 KiB (a lockfile or generated file sorted ahead of a file holding a key) means the secret after the cut is never scanned; the pre-commit check reports clean instead of forcing the "scan could not finish" Tier-2 look. Since `secret-scan-silent-skip` the check's contract is that a scan that cannot finish is reported, never passed as clean. `probeGit` in the same file and the footer's fetch (cmd/apogee/gitstatus_host.go) already treat `Truncated` as incomplete.

**Suggested fix:** in `queryDiagnosed`, map `res.Truncated` to an `incompleteError` ("git <sub>: output exceeded <cap> bytes"), as `probeGit` does. Better for the diff itself: stream it uncapped (the `RunTo` / writer path) into the scanner so a large commit is actually scanned rather than always forced to a prompt. Check the other `Run`/`Query` callers (treesnapshot, snapshot store) still behave once a truncated answer becomes an error.

## Acceptance

- A staged diff over 256 KiB whose secret lies past the first 256 KiB is either found or forces the incomplete-scan Tier-2 look; it is never reported clean.
- Query/Run never return a truncated stdout as a successful answer: a test with a fake launcher that sets Truncated gets an error matching ErrIncomplete.
- The same holds for the --name-only -z listing.
