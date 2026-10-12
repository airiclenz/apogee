---
title: writeTarget.note() discloses the workspace's own symlink on every write (macOS)
status: done
type: bug
priority: 3
labels: [macos, tools]
aliases: [apogee-qm8]
created: 2026-09-18T16:26:42Z
closed: 2026-09-19T16:00:12Z
reason: "Implemented by docs/plans/archived/2026-09-19 - 00 - deferred-follow-ons-plan.md (items ✅ DONE 2026-09-19; commits 9f1b6a7f 0a01b4ce 104d0c93 4e2d1413 and the if9 item); CHANGELOG [Unreleased] carries the entries. Closed late: bd had no database on this host at closeout."
---

writeTarget.note() (internal/tools/write_target.go:179) returns " → resolves to <Real>" whenever Real != Named, verbatim. Named is root-joined from the argument with nothing followed; Real is security.EvalRealPath of that (resolveTargetUnbounded, internal/tools/workspace_scoped.go:207). The root itself is never resolved — resolveRoots (cmd/apogee/wire.go:428) only makes it absolute — so a workspace reached through ANY symlink puts the note on EVERY write, not just the one that went somewhere else.

On macOS that is the common case, not the edge: `cd /tmp/x && apogee` keeps PWD as /tmp/x while the real path is /private/tmp/x, so every write_file/edit result the model reads grows " → resolves to /private/tmp/x/…". Cosmetic — the write lands where it should and the fence judges Real — but it defeats the note's own contract ("nothing on the common path", "naming where the call really landed when that is NOT the path the argument named") and dilutes the one disclosure that matters, the per-file symlink.

Surfaced 2026-09-18 while making the suite green on macOS: TestWriteTargetNote had to take its root by real path (commit c468a53b) to test what it claims.

Fix sketch: compare Real against the argument resolved under the ROOT's real path — i.e. note only when EvalRealPath(named) != EvalRealPath(root)+rel — or resolve the root once at scope construction (writeScopeOf) so Named already carries the real root. Either way the approval pane and the note must keep agreeing (the "compute it BEFORE the write" rule at note()); check TestWriteTargetNote's symlinked-final-name case still discloses.
