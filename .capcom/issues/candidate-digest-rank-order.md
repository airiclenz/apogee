---
title: The probe's candidate-token digest hashes rank order although it is documented as a set
status: open
type: bug
priority: 3
created: 2026-11-10T19:36:31Z
---

Source: docs/reviews/code-audit-2026-11-10.md ("The model fingerprint digest changes when near-tied tokens swap rank"). Verified VALID 2026-11-10 at a5c4f2db.

**Where:** `internal/probe/modelfingerprint.go` `candidateDigest` (:115-123); compared in `cmd/apogee/probemodel.go:252`.

**What:** `candidateDigest` and `BehaviorSignature` are documented as folding the candidate-token SET (the reason the signature survives sampling noise, ADR 0021 §6). The code hashes the tokens joined in `top_logprobs` order, most-likely first (`provider/wirejson.go` `topCandidates`). A rank swap between near-tied candidates changes the `lp-` digest; the probe compares signatures by exact equality and reports "the model behind this label changed since …". Temperature 0 does not prevent it (batched/GPU inference is not bit-deterministic). Tests check only the `lp-` prefix. Impact is limited to the `apogee probe model` report; identity does not use the digest.

**Why it matters:** the changed-model signal (ADR 0021 §3, amended 2026-09-16) exists to detect a swapped model behind an unchanged label; a false positive teaches the user to ignore it.

**Suggested fix:** sort a copy of the candidates before hashing. Note in the doc comment that a token entering/leaving the top-k still moves the digest. Add a test that two rank orders yield the same signature. Bump the battery version (or state a one-time note) so stored records are not reported as a changed model on the first probe after the change.

## Acceptance

- BehaviorSignature is identical for two batteries whose Candidates hold the same tokens in different orders, pinned by a test.
- The candidateDigest doc states exactly which variations move the digest.
- The first probe after upgrading does not falsely report a changed model.
