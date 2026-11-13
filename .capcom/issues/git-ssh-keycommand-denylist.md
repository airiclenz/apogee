---
title: Repo-local gpg.ssh.defaultKeyCommand runs unconfined on sync
status: open
type: bug
priority: 2
plan: 2026-11-13 - 00 - urgent-defects-and-doc-truth-plan
created: 2026-11-13T09:11:48Z
---

The git command-config denylist (security.GitCommandConfigNameSource) does not match gpg.ssh.defaultKeyCommand. The git actions host's sync 'rebase @{u}' passes no --no-gpg-sign, so a planted repo-local commit.gpgsign=true + gpg.format=ssh + gpg.ssh.defaultKeyCommand=./x runs ./x unconfined. Found re-checking docs/reviews/code-audit-2026-11-10.md.

## Acceptance

- A repo setting gpg.ssh.defaultKeyCommand is refused by the git tool and the git actions host; a test proves the planted command never runs
