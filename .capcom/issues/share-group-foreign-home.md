---
title: A shared store fails every save when the apogee home belongs to another user
status: done
type: bug
priority: 2
plan: 2026-10-18 - 01 - share-group-foreign-home-plan.md
created: 2026-10-18T08:01:08Z
closed: 2026-10-18T08:42:35Z
reason: Fixed in 42f8e803/e2ab9443, shipped in v0.26.1 (https://github.com/airiclenz/apogee/releases/tag/v0.26.1)
---

With `sessions.share-group` set, `Store.ensureDir` (`internal/session/store.go`) calls `shareDir` on every `save` and `hold`. That runs `os.Chown(-1, gid)` and `os.Chmod` on the apogee home (`sharedHomePerm`, 0710) and on `sessions/` (`sharedDirPerm`, 0750) every time. It does so even when the group and mode are already right. An unprivileged process gets EPERM from chown/chmod on a directory it does not own, and EROFS on a read-only mount, so every record save and every hold fails.

This rules out a hardened layout that watch mode should support. Seqcom (the autonomous agent in `seqcom-infra`) runs headless as user `seqcom`. Its apogee home `/srv/seqcom/.apogee` is `root:seqcom 0755` and mounted read-only by the systemd unit, so the agent cannot swap its `config.yaml` symlink. Only `sessions/`, `snapshots/`, `scratch/` and `workspaces/` are writable. Root can set the home to `root:apogee-watch 0710` once, but apogee still tries to chown and chmod it on every save. Found by reading the code at `abed5ec8`. v0.26.0 (`89bbaabb`) has the same code, and it has not been run in that layout yet.

Wanted: `shareDir` leaves alone a directory whose group already is the share group and whose mode already is the target. It only chowns and chmods when something differs. When a change is needed and fails, the error stays as it is now. A home that is already shared correctly then works whoever owns it.

Blocks Seqcom's `apogee-watch` (in `seqcom-infra`), which waits on a patch release with this fix.

## Acceptance

- A shared store whose home and `sessions/` already have the share group and the target modes saves and holds without calling chown or chmod on them. A test covers this, e.g. through a seam for the two calls, or by checking that a directory the test cannot chown is accepted when it already matches.
- A directory whose group or mode differs is still fixed as today, and a failure to fix it is still reported (existing `store_share_test.go` cases keep passing).
- `docs/manual/sessions.md` ("Watching another user's sessions") and `SECURITY.md` mention that a home owned by another user (e.g. root) works when it is set up with the share group and 0710 in advance.
- `CHANGELOG.md` under `[Unreleased]`, and a patch release (v0.26.1).

## Log

- 2026-10-18 Airic Lenz: Plan '2026-10-18 - 01 - share-group-foreign-home-plan' (archived at 29804b44) landed the shareDir fix with tests, the sessions.md/SECURITY.md wording and the [Unreleased] CHANGELOG entry; only the v0.26.1 patch release remains (release-apogee).
