# Plan: shared store with a foreign-owned home

**Goal:** A shared session store (`sessions.share-group`) saves and holds when its apogee home and `sessions/` already carry the share group and target modes, whoever owns them and even on a read-only mount. Only a directory that differs is chowned or chmodded, and a failure to fix one is reported as today.
**Date:** 2026-10-18
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 3834254e
**Closes:** share-group-foreign-home

**Regression check (2026-10-18, 3834254e):**
- 1: guard folded (the fix-failure test pre-sets the home so the chmod EPERM names `sessions/`; every "on every call" / "Every write sets" comment for the home and `sessions/` is rewritten)
- 2: guard folded (Acceptance greps the new wording in each file; the docs state that the running user must already own `sessions/` and the other directories apogee creates under a foreign-owned home)

**Sources:**
- `capcom show share-group-foreign-home`
- `internal/session/store.go` — `ensureDir`, `shareDir`, `sharedHomePerm`, `sharedDirPerm`
- `docs/manual/sessions.md` §Watching another user's sessions; `SECURITY.md` (the `sessions.share-group` bullet)

**Ratified design calls (writer, 2026-10-18 — no user-visible choice):**
- **Match test:** a directory matches when its group is the share gid and `Mode().Perm()` equals the target; special bits (setgid, sticky) are not compared and are left as set.
- **Each half on its own:** chown only when the group differs, chmod only when the permission bits differ; chmod still runs after a chown (a non-root chown may clear mode bits), re-checked by a fresh stat.
- **Test seam:** package-level function variables for the directory chown and chmod, overridden in tests; no new exported surface.

**Standing requirements:**
- skills: coding-standards
- Each `go test` names one package and a `-run` pattern, prefixed `GOMEMLIMIT=2GiB` (AGENTS.local.md; never a multi-package race run).

**Out of scope:** the v0.26.1 patch release the issue asks for (the owner cuts it with `release-apogee` after this plan); `setFileAccess` and the platform lock's file chown (files in `sessions/` are the process's own); Windows (a share group is refused there).

## 1. A shared store leaves an already-shared directory alone — ✅ DONE (2026-10-18)

NOTES (2026-10-18): retry fix — shareDir's chmod now passes perm plus the directory's existing setuid/setgid/sticky bits, so "special bits are left as set" holds when the permission bits differ too; added TestStoreShareGroup_SetgidDir_KeepsSetgid (a setgid sessions/ at dirPerm keeps setgid after Save), beyond the plan's two named tests.

**What:** Fix (defect `share-group-foreign-home`): `shareDir` chowns and chmods the apogee home and `sessions/` on every `save` and `hold`, so a home owned by another user (EPERM) or on a read-only mount (EROFS) fails every save, though it is already shared correctly.
**Regression guard.** `TestStoreShareGroup_DifferingDir_FixFailureIsReported` first pre-sets the home to `sharedHomePerm` and `shareTestGID` with the real `os.Chown`/`os.Chmod`, then swaps the chmod seam, and asserts the error names `dir` (`sessions/`), not the home. Every comment in `internal/session/store.go` saying the store sets the home's or `sessions/`'s mode/group on every write or call is rewritten — grep `on every call\|Every write sets\|explicitly` (`ensureDir`'s doc and `WithShareGroup`'s doc both carry it) — and `setFileAccess`'s "for shareDir's reason" cross-reference stays true (`shareDir` keeps the chown before the chmod).
**Goal:** `ensureDir` on a shared store whose home has the share gid and perm `sharedHomePerm`, and whose `sessions/` has the share gid and perm `sharedDirPerm`, issues no chown or chmod and returns nil; a directory whose group or perm differs is still fixed, and a failure to fix it is returned wrapped as `apogee: share sessions directory %q: …`.
**Approach (assumed at the header base):** in `internal/session/store.go`, `shareDir` `os.Stat`s path (stat error returned under the same wrap); reads its gid through a new unexported `fileGroup(info os.FileInfo) (gid int, ok bool)` split like `rename_other.go`/`rename_windows.go`: `dirgroup_other.go` (`//go:build !windows`, `info.Sys().(*syscall.Stat_t).Gid`) and `dirgroup_windows.go` (always `ok == false`, so the chown runs and fails as today); calls the chown seam only when the gid differs, then re-stats when it chowned, and calls the chmod seam only when `Perm()` differs from perm. The seams are unexported package vars (`chownDir = os.Chown`, `chmodDir = os.Chmod`) used only by `shareDir`. Update `ensureDir`'s and `shareDir`'s doc comments: the explicit setting now happens only when the directory differs. Every comment naming "on every call" for these directories is rewritten (grep `on every call` in `internal/session/`).
**Files:** internal/session/store.go, internal/session/dirgroup_other.go, internal/session/dirgroup_windows.go, internal/session/store_share_test.go
**Read first:** internal/session/store.go — shareDir, ensureDir, WithShareGroup, setFileAccess; internal/session/store_share_test.go — shareTestGID, sharedStoreLayout, writeAll; internal/session/rename_other.go — renameRecord (build-tag split pattern)
**Tests:** in `store_share_test.go`: `TestStoreShareGroup_AlreadyShared_IsNotChownedOrChmodded` — pre-set home to `sharedHomePerm` and `sessions/` to `sharedDirPerm`, both with `shareTestGID`, replace both seams with funcs that `t.Errorf` and return `syscall.EPERM`, then `writeAll` succeeds. `TestStoreShareGroup_DifferingDir_FixFailureIsReported` — home pre-set to `sharedHomePerm` and `shareTestGID` with the real `os.Chown`/`os.Chmod` before the seam is swapped, `sessions/` at `dirPerm`, chmod seam returns `syscall.EPERM`; `Save` fails with `errors.Is(err, syscall.EPERM)` and the message names `dir` (`sessions/`), not the home. Seams restored with `t.Cleanup`; these tests do not call `t.Parallel`. Existing share tests keep passing.
**Acceptance:** `go build ./internal/session/ && GOOS=windows go build ./internal/session/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestStoreShareGroup' ./internal/session/`
**Commit:** `fix(session): leave an already-shared home and sessions directory alone`

## 2. The manual and SECURITY.md say a pre-shared foreign home works — ✅ DONE (2026-10-18)

NOTES (2026-10-18): no CHANGELOG entry — the user-facing change is item 1's fix; this item documents it. Wording says "group and mode" (not "exact mode") so it stays true of item 1 keeping setuid/setgid/sticky bits; docs/manual/configuration.md needed no change (it states no directory modes).
NOTES (2026-10-18): retry per verifier FIX — the list of directories the running user must already own is no longer exhaustive ("every other directory apogee creates there that the run uses") and names `daemon/` for `apogee daemon` in both files.

**What:** Document the layout item 1 enables: a home owned by another user (e.g. root) or mounted read-only works with `share-group` when it is set up with the share group and `0710` (and `sessions/` with the group and `0750`, owned by the running user) in advance; apogee then changes neither.
**Regression guard.** The new wording says the running user must already own `sessions/` — and the other directories apogee creates under the home (`scratch/`, `snapshots/`, `workspaces/`) — before the home is handed to another owner; a root-owned `0710` home otherwise refuses their creation (EACCES). The Acceptance greps the new phrase in each file separately (the `0710` grep already matches at base, and `-run 'Drift|Doc' ./cmd/apogee/` reads neither file).
**Goal:** `docs/manual/sessions.md` §Watching another user's sessions (step 2's paragraph) and the `sessions.share-group` bullet in `SECURITY.md` each state that apogee sets the home's and `sessions/`'s group and mode only when they differ, and that a home owned by another user works when pre-set to the share group and `0710`.
**Approach (assumed at the header base):** one or two sentences in each, after the sentence listing the `0710`/`0750`/`0640` modes; e.g. `sudo chown root:apogee-watch /srv/svc/.apogee && sudo chmod 0710 /srv/svc/.apogee` as the example. Plain language, the shipped vocabulary.
**Files:** docs/manual/sessions.md, SECURITY.md
**Read first:** docs/manual/sessions.md — §Watching another user's sessions step 2; SECURITY.md — `sessions.share-group` bullet; docs/manual/configuration.md — sessions.share-group paragraph (stays consistent); internal/session/store.go — ensureDir, WithShareGroup
**Tests:** none (docs); doc drift tests unaffected.
**Acceptance:** `grep -n "only when they differ" docs/manual/sessions.md && grep -n "only when they differ" SECURITY.md && GOMEMLIMIT=2GiB go test -count=1 -run 'Drift|Doc' ./cmd/apogee/`
**Commit:** `docs(sessions): note that a pre-shared home owned by another user works`
