---
Status: accepted
Amends: ADR 0012 (the dangerous-action guard gains its first content-derived Tier-2 member, `commit-secrets`)
---

# git_commit forces a look at staged secret material

## Context

A model that has just written a `.env`, dropped a PEM block into a fixture or pasted a token
into a config file will, a few calls later, run `git_commit` over it — and a commit is the
one write whose blast radius outlives the workspace: once pushed, a secret is compromised
whether or not the next commit removes it. Every other guard apogee ships judges a call's
**action text** — the tool, its target paths, its command lines — and never the payload a
write carries (`internal/security/doc.go`, ADR 0012); a commit's payload is exactly what
matters here, and none of the existing rules can see it.

Two obvious mechanisms are closed by decisions already on record:

- **A git hook cannot be the mechanism.** Every git apogee runs goes through
  `internal/gitexec` with `-c core.hooksPath=` (the hardening option that resolves every hook
  to a path no in-workspace write can create), so a pre-commit hook apogee installed would be
  blanked along with the attacker-authored ones it exists to blank. The Tier-1
  `write-git-control-plane` rule refuses any write under `.git/hooks` for the same reason — a
  hook is a program the repository names, and apogee does not execute those. A hook would
  also be a repository-local artefact where the guard is a property of the agent.
- **The verdict must be computed before anything executes.** `resolve()` is hermetically pure
  (`internal/agent/resolution.go`, D6): it does no I/O and reads only the facts dispatch
  precomputed. A secrets check that ran inside the tool, after `git add`, would have already
  written the secret blob into the real object store and staged it in the real index — the
  denied call would leave both behind.

The IDEAS.md entry ("Prevent secrets, certificates, public-keys … from being committed")
asked for a brainstorming grill; the owner ratified the calls below on 2026-09-17.

## Decision

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

## Consequences

- The dangerous-action guard now has one member whose evidence is not in the call but in
  what git would stage — the first **content-derived** rule. The "action text, never payload"
  contract of every other rule is unchanged; `commit-secrets` is the deliberate exception,
  scoped to the one call where the payload is the action. `CONTEXT.md` and the manual name
  it among the Tier-2 members.
- Part B adds nothing to the prompt (ADR 0031): the model meets the rule only through a
  denied call's result text, exactly as it meets every other guard.
- Every `git_commit` outside Plan mode now costs four short git children (rev-parse, add,
  two diffs) on a shadow index before it resolves — bounded by the timeout, side-effect free
  on the repository, and skipped the moment git is unavailable.
- The pattern and glob lists are code, not config. Extending them is a code change with a
  table-test row; a configurable rule set waits on the ADR 0012 merge seam no key calls
  today (apogee-089).
- A secret that matches no built-in class — a bespoke API key, a password in a YAML value —
  passes. That is the precision-over-recall trade every guard rule makes: the floor catches
  the obvious mistake, it is not a scanner.

## Alternatives rejected

- **gitleaks (or any external scanner) at the seam.** Far better recall, but an install
  dependency for a floor that ships default-on, a rule set apogee does not own, and a
  per-commit cost that would push the check off the floor into an opt-in — which for a
  mistake-guard is the same as off.
- **Scanning at write time (`write_file` / `file_edit` payloads).** Would catch the secret
  earlier, but every write is a legitimate place for one (a fixture, a scratch file, a
  gitignored `.env`) and only the commit makes it durable; write-time scanning would also
  breach the "never the payload" contract for every write tool rather than for one call.
  `terminal` `git commit` is likewise out: the shell view reads a command for what it writes,
  not for what git would stage.
- **A hard refuse (Tier 1).** A committed test fixture with a throwaway private key is a
  legitimate commit; refusing it outright with no way through would teach the operator to
  disable the guard. The Tier-2 look with the finding on the `Fix:` row is the speed-bump
  the two-tier design exists for.
- **A git pre-commit hook.** Closed by `core.hooksPath=` and by the control-plane rule, as
  the context records; a hook would also protect one repository rather than the agent.
- **An allow-list key in v1.** Deferred until a real false-positive pattern shows up; the
  approval prompt is the allow-list until then.

## Amendment (2026-09-23) — one budget, and an incomplete scan forces the look

Decision 6 and the Consequences sentence "bounded by the timeout, side-effect free on the
repository, and skipped the moment git is unavailable" describe the pre-check as it first
shipped. The shipped contract is now:

- **One budget, not one timeout per run.** A single `commitSecretsTimeout` of 30 s bounds the
  whole pre-check — one context covers all four shadow runs (rev-parse, add, two diffs) — where
  decision 6 gave each run its own 2 s. The box apogee is built for is a loaded local machine on
  which a cold git over a large index is slow rather than broken; a healthy repository answers in
  milliseconds and spends none of it.
- **Three outcomes, not two.** A workspace root that resolves no repository to scan — git
  absent, fenced or refused, not a repository, a path the tool itself would refuse — is still the
  silent skip, and the text guard's verdict stands. But once the repository has resolved, an
  expired budget or a git run that fails mid-scan no longer skips: the scan is incomplete, not
  clean, and the call is forced to the Tier-2 approval look with the incomplete-scan hint. A
  control that did not run is no longer indistinguishable from one that ran clean.
- **Still never a failure.** The pre-check never fails a commit the tool would have made; the
  worst it does is ask. "Must never slow or block" in decision 6 now reads: never beyond the one
  budget, and never without the human's say.

This narrows, rather than overrides, ADR 0056 decision 4: its silent-skip-on-any-failure rule no
longer governs the commit-secrets pre-check, and still governs the tree-mutation snapshot
(`treeSnapshotTimeout`), which is unchanged.
