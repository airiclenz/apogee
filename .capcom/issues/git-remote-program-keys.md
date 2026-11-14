---
title: Repo-local remote/url/submodule git keys run a program unconfined
status: open
type: bug
priority: 2
labels: [security]
created: 2026-11-14T10:11:00Z
---

From docs/reviews/code-audit-2026-11-10.md (Medium, "Git command-config denylist omits keys that make git run a program"); the only finding of that audit with no issue. The repo-local denylist `security.GitCommandConfigNameSource` (internal/security/shellwrites.go:992), enforced by internal/gitexec/gitexec.go, does not match `remote.<n>.uploadpack` / `receivepack` / `vcs`, `url.<x>.insteadOf` (an `ext::` rewrite), `protocol.<x>.allow`, or `submodule.<n>.update=!cmd`. `hardeningOptions` blanks only `core.hooksPath` and `core.fsmonitor`. Sibling of git-ssh-keycommand-denylist (closed 2026-11-13).

## Why

A planted `.git/config` entry (hostile checkout or prompt-injected model) is executed unconfined and ungated by apogee's own fetch, pull and push.

## Acceptance

- The git tool and the git actions host refuse a repo-local `remote.<n>.uploadpack`, `remote.<n>.receivepack`, `remote.<n>.vcs`, `url.<x>.insteadOf`, `protocol.<x>.allow` and `submodule.<n>.update`, or neutralise them with `-c protocol.ext.allow=never` plus `GIT_ALLOW_PROTOCOL`; a test proves a planted command never runs
- The shell write view (`git config <key> ...`) treats the same keys as git-control-plane writes; ADR 0049 amended if its key list changes
