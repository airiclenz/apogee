---
title: A repo-local merge driver silently drops the footer git segment and refuses every read-only git tool
status: done
type: bug
priority: 2
labels: [security]
plan: 2026-11-18 - 00 - git-refusal-silent-footer-plan
created: 2026-11-18T08:58:29Z
closed: 2026-11-18T10:45:13Z
reason: "Resolved by plan '2026-11-18 - 00 - git-refusal-silent-footer-plan' (archived at b6aa679e): repo-local merge drivers neutralised per call, merge-capable argv refused, footer shows 'git refused' with a one-time note, posture recorded in adr-0012-amend-1118 and adr-0108-amend-1118."
---

A repository whose own `.git/config` carries any command-valued key loses every git tool AND the footer's git segment, and the footer loss is silent. Verified against the code at v0.35.4 (7c8f5aa1).

### Observed

Repo `Event-Horizon` (unrelated to apogee) has capcom's merge driver registered repo-locally:

```
[merge "capcom-log"]
    name = capcom Log merge
    driver = capcom merge-driver %O %A %B %P %S %X %Y
```

with `.gitattributes` mapping `.capcom/issues/**/*.md` to `merge=capcom-log`. Every git tool (`git_status`, `git_log`, `git_show`, …) returns:

> git refused: this repository's own config names a program git would execute (merge.capcom-log.driver). Repo-local command-valued keys are refused for every git tool; the operator's global git config is untouched and still applies.

The footer shows no branch, `Δ`, `↓` or `↑` and no explanation. Moving the key to the global config restores both (the probe judges `--local`/`--worktree` only), but any tooling that re-registers repo config re-breaks it.

### Where it happens

- **The blast radius.** `security.GitCommandConfigNameSource` (`internal/security/shellwrites.go:992`) includes `merge\..*\.driver`; gitexec's `CommandConfigName` anchors it, and `probeCommandConfig` runs in front of every captured/queried call (`refuseCapture`, `queryDiagnosed` in `internal/gitexec/gitexec.go`) whatever the verb. The refusal is per repository, not per verb.
- **The silence.** The footer's host (`cmd/apogee/gitstatus_host.go`) reads `rev-parse --is-inside-work-tree` through `CaptureEnvTo` first; `insideWorkTree` maps a refused, non-incomplete exit to `inside=false, err=nil`, which `Model.foldGitStatus` (`internal/tui/gitstatus.go`) treats as "not a work tree" and clears the segment. Documented on `gitStatusHost.Status` and pinned by `cmd/apogee/gitstatus_host_test.go:318` (a repo-local `core.sshCommand` → "not inside"). The refusal's own sentence reaches no user surface outside a model's tool result.

### What is wrong (the refusal itself is by design)

1. **Silent.** The user sees no git segment and nothing tying that to the cause.
2. **Disproportionate.** `merge.<x>.driver` runs only during a conflicted merge, rebase or cherry-pick, yet it disables pure reads (`status`, `log`, `show`, `rev-parse`) that cannot run it. Any repo with a legitimate merge driver (capcom, git-lfs, linguist setups) loses the git tools and the footer. Precedent for verb-awareness already exists: `mergetool.<tool>.path` and `difftool.<tool>.path` stay out of the denylist because git runs them only on a typed verb (ADR 0049 amend-1114), and the remote/url/protocol/submodule keys are neutralised per verb instead of refused (`gitTransportConfig`, `--upload-pack=git-upload-pack`).

No ADR records the per-repository whole-class refusal; it lives in gitexec comments ("since 2026-08-14 … since 2026-08-26 for the whole class").

### Design options to decide between

- **(a) Verb-aware posture.** Classify each key by the verbs that can execute it (a merge driver: merge, rebase, cherry-pick, pull, revert, `am -3`, stash apply/pop) and refuse only those verbs in a repo that carries it.
- **(b) `-c` neutralisation for read-only verbs.** Override the offending key per call (prior art: `-c protocol.ext.allow=never` + `GIT_ALLOW_PROTOCOL`). Note git has no empty-value "off" for a merge driver; the override must name a harmless one (e.g. `-c merge.<x>.driver=false`?) and must be proved not to execute, for every key the probe names.
- **(c) Status quo** with the refusal surfaced to the user.

Whichever is chosen, the footer must say why it shows nothing.

Related, closed: `footer-git-status`, `git-remote-program-keys`, `guard-refuses-git-reads`, `gitexec-stale-config-memo`.

## Why

A legitimate repo-local merge driver (capcom's own, git-lfs, linguist) costs the user every git tool and the footer git status with no explanation; the user cannot connect the missing segment to its cause, and read-only verbs are refused for a program they can never run.

## Acceptance

- When gitexec refuses the workspace for a repo-local command-valued key, the footer shows a git marker or notice naming the offending key(s) (the same names CommandConfigRefusal lists) instead of dropping the segment; a repo that is genuinely no work tree still shows nothing
- The choice between (a) verb-aware posture, (b) -c neutralisation for read-only verbs, and (c) status quo with a surfaced explanation is recorded as an ADR (or an ADR 0049 amendment), including which keys each verb can execute
- Tests with a planted merge.<x>.driver whose program writes a marker file: the footer's host and the read-only git tools (status, log, show, diff) behave as the decision says, and the marker file never appears on any verb apogee runs
- cmd/apogee/gitstatus_host_test.go's refused-repository tests (the core.sshCommand case at :318) updated to the new contract; the gitStatusHost.Status and GitStatusHost.Status doc comments say what a refused repository returns
- docs/manual (configuration or the footer section) states the refusal, the marker, and that moving the key to the global git config is the workaround
