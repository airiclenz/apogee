# /dev/null confinement fix — landlock + seatbelt device exemption

**Goal:** a confined subprocess can write to `/dev/null` again. Both POSIX confinement
backends deny-default file writes and re-grant them only beneath the box's writable
roots, so the shell idiom `2>/dev/null` fails with `cannot create /dev/null: Permission
denied` inside every confined tool call (observed live: apogee session
`20260813T113304Z-059d9f5a`, first terminal call, exit 2). Fix: each backend itself
exempts the single device file `/dev/null` from the write fence.

**Date:** 2026-08-13
**Status:** unexecuted
**sized for:** ~200k-context host

**Authoritative sources:**
- `docs/design/confinement-execution-contract.md` (§2.3 backend obligations, §5 capability honesty)
- ADR 0012 (confinement policy: workspace-write-only box semantics)
- Current backend code: `internal/platform/landlock_linux.go` (`applyLandlock`,
  `allowWriteBeneath`, `accessMaskForABI`), `internal/platform/seatbelt.go`
  (`seatbeltProfile`)

**Ratified design calls** (owner, 2026-08-13, via AskUserQuestion):
1. **Scope:** fix both backends (landlock + seatbelt) AND amend the
   confinement-execution-contract with the device-exemption rule.
2. **Device set:** `/dev/null` ONLY. Reads are already unfenced in both backends
   (landlock does not handle read; seatbelt denies only `file-write*`), so
   `/dev/zero`/`/dev/urandom` reads work today; no other device gets a write
   exemption. Extending the set is a future, evidence-driven change.
3. **Injection point:** inside each backend (`applyLandlock` / `seatbeltProfile`), not
   in `ConfinementBox` construction. The box stays pure policy (workspace + user
   paths), the exec fence's writable set (`internal/security/execsafety.go`) is
   untouched, and every box-construction site is covered automatically.

**Standing requirements:**
- skills: coding-standards
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Any device other than `/dev/null` (including `/dev/tty`, `/dev/zero`, `/dev/stdout`).
- The Windows confiner (integrity-label model; the `NUL` device is not label-fenced —
  no gap there).
- Config-driven device exemptions (`ConfineWritablePaths` seeding).
- Network policy, read fencing, or any other confinement semantics.
- Version bump (see closing note).

## 1. Landlock: allow /dev/null writes through the fence — ✅ DONE (2026-08-13)

NOTES (2026-08-13): live-kernel coverage landed in `internal/platform/landlock_linux_test.go` (new `TestLandlockAllowsDevNullThroughTheFence`) rather than `cmd/apogee/confinement_e2e_test.go`, which is unchanged — the existing landlock write-denial test is the cross-platform `confinetest.Probe` battery, and a `/dev/null` step inside that shared battery would also run against the Windows backend, which the plan puts out of scope.
NOTES (2026-08-13): the rule reuses the existing `allowWriteBeneath` helper instead of a second open path — it already does exactly the specified `O_PATH|O_CLOEXEC` open, ENOENT-skip, fail-closed-on-any-other-error; the path is spelled `os.DevNull`, a compile-time `"/dev/null"` under this file's linux build tag.
NOTES (2026-08-13): the new live test was confirmed to fail without the fix (`exit status 2`, the exact symptom in the plan's intro) and pass with it; landlock is enforceable on this host, so it ran for real rather than skipping.

**What:** In `internal/platform/landlock_linux.go`, after the writable-roots loop in
`applyLandlock`, add a path-beneath allow rule for the literal file `/dev/null`.
Landlock accepts a file (non-directory) `parent_fd`, but `landlock_add_rule` returns
EINVAL if the rule carries directory-only rights, so the rule must use a
file-applicable mask, NOT `accessMaskForABI`: a new pure function (suggested name
`deviceAccessMaskForABI(abi int) uint64`) returning
`LANDLOCK_ACCESS_FS_WRITE_FILE`, plus `LANDLOCK_ACCESS_FS_TRUNCATE` at ABI >= 3
(`> /dev/null` opens with O_TRUNC, which needs the TRUNCATE right once that ABI
handles it). Open `/dev/null` with `O_PATH|O_CLOEXEC`; on ENOENT skip the rule
(same tolerance as `allowWriteBeneath`), on any other open error fail the
confinement (fail closed, matching `allowWriteBeneath`). Document the exemption in
the file's header comment block (the box bounds where a child may WRITE; `/dev/null`
is a data sink whose writes are side-effect-free, exempted so POSIX shell idiom
survives the fence).

**Files:** `internal/platform/landlock_linux.go`,
`internal/platform/landlock_linux_test.go`, `cmd/apogee/confinement_e2e_test.go`

**Tests:**
- Hermetic: unit-test the new mask function per ABI (ABI 1/2 → WRITE_FILE only;
  ABI 3+ → WRITE_FILE|TRUNCATE), alongside the existing `accessMaskForABI` tests.
- Live-kernel (skips where landlock is unavailable, following the existing pattern in
  `landlock_linux_test.go` / the confinetest harness): a confined child running
  `sh -c ': 2>/dev/null && echo x > /dev/null'` succeeds, while an out-of-box write
  (e.g. to a sibling temp dir) is still denied — add to the existing e2e coverage in
  `cmd/apogee/confinement_e2e_test.go` or the confinetest harness, whichever the
  existing landlock write-denial test lives in.

**Acceptance:**
- `go build ./...`
- `go test ./internal/platform/ ./internal/platform/confinetest/`
- `go test ./cmd/apogee/ -run 'Confinement'`

**Commit:** `fix(confine): allow /dev/null writes through the landlock fence`

## 2. Seatbelt: allow /dev/null writes through the fence — ✅ DONE (2026-08-13)

NOTES (2026-08-13): No darwin live coverage added — the item conditions it on a live write-denial
test already existing in `seatbelt_darwin_test.go`, and that file holds no local write test: it
delegates to the shared `confinetest.Probe` battery, so covering `/dev/null` there would mean new
shared harness machinery, which the item forbids.

**What:** In `internal/platform/seatbelt.go`, `seatbeltProfile` emits an unconditional
`(allow file-write* (literal "/dev/null"))` clause after the `(deny file-write*)`
line (alongside the subpath re-grants; emit it even when the box has no writable
roots). No canonicalization needed — `/dev/null` is not a symlink on macOS. Update
`seatbeltProfile`'s doc comment to state the exemption and its rationale (mirror
item 1's wording).

**Files:** `internal/platform/seatbelt.go`, `internal/platform/seatbelt_test.go`

**Tests:**
- Hermetic (runs on any OS — `seatbeltProfile` is a pure function): the profile
  string contains the `/dev/null` allow clause, for a box with roots and for an
  empty box; existing deny/allow tests still pass.
- Darwin-only live coverage follows the existing pattern in
  `seatbelt_darwin_test.go` ONLY if a live write-denial test already exists there;
  do not build new darwin harness machinery for this item.

**Acceptance:**
- `go build ./...`
- `go test ./internal/platform/ -run Seatbelt`

**Commit:** `fix(confine): allow /dev/null writes through the seatbelt fence`

## 3. Contract amendment: record the device exemption

**What:** Amend `docs/design/confinement-execution-contract.md`: in the section
stating the backends' write-fence obligations (§2.3 or the box-semantics section —
place it where the workspace-write-only semantics are defined), add the
device-exemption rule: both POSIX backends allow writes to the literal `/dev/null`
in addition to the box's writable roots; rationale (POSIX shell redirection idiom
breaks inside the fence otherwise; `/dev/null` is a side-effect-free sink); the
exemption is backend-level — it is NOT part of `ConfinementBox` and does not appear
in the exec fence's writable set; the set is exactly `/dev/null` (extending it is a
contract change). Depends on items 1 and 2 (documents landed behavior).

**Files:** `docs/design/confinement-execution-contract.md`

**Tests:** none (doc-only).

**Acceptance:**
- `grep -n '/dev/null' 'docs/design/confinement-execution-contract.md'` shows the
  new rule in the write-fence/box-semantics section.

**Commit:** `docs(design): record the /dev/null device exemption in the confinement contract`

---

**Suggested version bump:** patch (bug fix in shipped confinement behavior) — the
owner decides; no version identifier is changed by this plan.
