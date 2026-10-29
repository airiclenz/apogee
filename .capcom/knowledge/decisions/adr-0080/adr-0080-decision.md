---
title: ADR 0080 decision
---

1. **Seam: `git_commit` only, pre-execute, dispatch-side.** There is no `git_stage` tool, and
   the tool's own re-staging of tracked files cannot add secret bytes on its own; the commit
   call is the one place the payload becomes durable. Dispatch (`internal/agent/secretsguard.go`)
   runs the check between the text guard's `PreCheck` and `resolve()`, as one more precomputed
   guard fact in the D6 sense. It runs only when the text guard let the call proceed (a Tier-1
   refuse or a Tier-2 force from the call's own arguments stands unexamined — the guard is
   tighten-only) and never in Plan mode, where `resolve()` refuses every write leaf anyway and
   no git child is spawned.
2. **A shadow index, never the real one.** The check stages the call's `files` — spelled
   through the same `tools.CommitPathspecs` the tool itself uses, so a path the tool would
   refuse is a path the check never judges — into a *copy* of the index (`GIT_INDEX_FILE`),
   with the blobs `git add` writes landing in a private object directory
   (`GIT_OBJECT_DIRECTORY`) that reads the real store through
   `GIT_ALTERNATE_OBJECT_DIRECTORIES`. `git rev-parse --git-path index --git-path objects`
   locates both, so a linked worktree resolves to its own index; a repository that has never
   staged anything keeps its index *missing* rather than empty, which git reads as an empty
   index. A denied commit therefore leaves no secret blob in the real object store and the
   real index is untouched until the tool's own `git add`. The staged diff is read with
   `git diff --cached --diff-filter=ACMR --no-textconv --no-ext-diff --no-color`, and the
   staged names with `--name-only` under the same filter.
3. **Detection is a built-in floor, precision over recall.** The pure scanner
   (`security.SecretFindings`) reads only the *added* lines of the staged diff, attributing
   each to the file named by the nearest `+++ b/<path>` header, against five content classes:
   a PEM private-key block (`-----BEGIN … PRIVATE KEY-----`), an AWS access key id (`AKIA…`),
   a GitHub token (`ghp_`/`gho_`/`ghu_`/`ghs_`/`ghr_`/`github_pat_`), a Slack token
   (`xox[abpr]-…`) and a JWT. It flags a staged file by *name* alone when its basename
   matches `*.pem`, `*.key`, `*.p12`, `*.pfx`, `id_rsa*`, `id_ed25519*`, `id_ecdsa*`, `.env`
   or `.env.*` — except the committed templates `.env.example`, `.env.sample` and
   `.env.template`, and except any basename ending in `.pub`. Public keys and certificates are
   never flagged: they are meant to be published. The order of findings is pinned — path
   globs first in `files` order, then content classes in diff order, each `(path, class)`
   pair once — because the Hint prints them. No external scanner: the floor must cost nothing
   to install and nothing to run, and a call that takes seconds to resolve is a floor that
   gets switched off.
4. **Tier 2 in every mode; approval is the only escape.** A finding tightens the verdict to
   the dangerous-action guard's `TierForceApproval` under the rule id `commit-secrets` — the
   Approver is asked even on the auto rung, and as with every forced look the pane offers no
   "Always allow this session" row. There is no allow-list key in v1: the person saying yes
   *is* the allow-list, and the cost of a false positive is one look at a pane that names the
   file and the class. The audit trail records the Tier-2 decision under the same rule id.
5. **The finding travels in the Hint, not the Reason.** The pane's `Reason:` line keeps the
   pinned `forceApprovalReason` constant every Tier-2 member shares; the `Fix:` row carries
   `security.SecretsHint` — `staged secret material: <path> (<class>), … — unstage it, or
   approve to commit anyway`, at most three findings listed and a `+N more` tail — and a
   denied look answers the model with the same words, so the model learns which file to
   unstage rather than that "a guard fired".
6. **Silent skip on any git failure (ADR 0056 decision 4).** Each shadow run carries a 2 s
   timeout and executes in the workspace root; git absent, fenced or refused, not a
   repository, a timeout, an unexpected `rev-parse` shape — every failure skips the check for
   that call and the text guard's verdict stands. The pre-check must never fail, slow or
   block a commit the tool itself would have made; it is a floor, not a gate on git's health.

> *Amendment (2026-09-23, `adr-0080-amend-0923`):* decision 6 is **superseded** on two points.
> One `commitSecretsTimeout` of 30 s bounds the whole pre-check (all four shadow runs), not 2 s
> per run. A root that resolves no repository is still the silent skip; but once the repository has
> resolved, an expired budget or a git run failing mid-scan forces the Tier-2 look with the
> incomplete-scan hint instead of skipping.
