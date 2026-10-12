---
title: dangerous-action guard refuses read-only shell commands that name .git/hooks or .git/config
status: done
type: bug
priority: 2
aliases: [apogee-2ay]
created: 2026-09-14T11:10:47Z
closed: 2026-09-15T11:49:42Z
reason: Fixed by plan 2026-09-14 - 02 item 10 (write-git-control-plane reads a verb-aware write view of the shell text; internal/security/shellwrites.go)
---

The write-git-control-plane rule (internal/security/rules.go:143, pattern `\.git/(?:hooks|config|modules)\b`, WritesOnly) refuses ANY `terminal` command whose text mentions those paths, because a tool that declares no read-source keys is fully inspected (internal/security/dangerous.go:159). Reads are caught with the writes.

Observed 2026-09-14 in the /code-audit run (session 20260914T103923Z-50e9123c), two lens sub-agents refused with "write or delete under a repository's git control plane":

  ls -la .git/hooks/ 2>/dev/null | head -30; cmp -s .beads/hooks/commit-msg .git/hooks/commit-msg && echo BYTE-IDENTICAL || echo DIFFERENT
  cmp -s /workspace/repos/apogee/.git/hooks/commit-msg /workspace/repos/apogee/.beads/hooks/commit-msg && echo IDENTICAL || echo DIFFERENT; ls -l /workspace/repos/apogee/.git/hooks/commit-msg

Both are pure reads, and apogee's own AGENTS.md invites exactly this check ("`.git/hooks/commit-msg` is a deliberate dormant fallback — byte-identical to `.beads/hooks/commit-msg`"). The rule's own comment says it guards "the write that plants delayed execution, not the read", but for shell that intent is unimplemented: WritesOnly only narrows the view for tools that declare read sources.

Why it matters: an auditing or refocusing agent that inspects hooks is stopped by a hard refuse (no approval route), and the refusal reason misdescribes what it did. The floor invariant (nothing apogee puts in front of a model may make it worse than the bare loop) is what a false hard refuse chips at.

What to do: give the write-shaped view a notion of verb for shell text — e.g. only match a control-plane path when the command also carries a write/delete/redirect verb (`>`, `>>`, `tee`, `cp`/`mv`/`install` with the path as destination, `rm`, `chmod`, `git config --local/--global set`, `sed -i`, heredoc), or conversely skip the rule when every command in the pipeline is from a read-only set (`ls`, `cat`, `cmp`, `diff`, `head`, `tail`, `stat`, `file`, `grep`, `wc`, `md5sum`, `sha256sum`). Keep the hard refuse for anything ambiguous. Same question applies to the ssh-key and credential-file WritesOnly rules on shell text. Add guard tests for both refused commands above (must pass) and for `cp x .git/hooks/pre-commit`, `echo … > .git/config` (must still refuse).
