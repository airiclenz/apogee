# Handoff — Linux closeout done: `make check` green under `-race`, live landlock verified

Date: 2026-07-23
Session type: execution. Ran the closeout Linux pass requested by the prior handoff
(`2026-07-23 - 00 - refocus-audit-...`). Committed + pushed as `eeb11bc`.

## What this session did

Ran the **closeout Linux pass** and it passed completely — closing **two** of the prior
handoff's open items in one run, the second unexpectedly.

### The pass — `make check` green, exit 0

Machine: the Ubuntu devbox this session ran on — kernel **7.0.0-28-generic**, **aarch64**.
Full `make check` passed with **cgo + the race detector enabled** (owner installed gcc 15.2
mid-session; the box shipped `CGO_ENABLED=0` and no C compiler, which had blocked `-race`):

- gofmt clean · `go vet ./...` · `go build ./...` · **`go test -race ./...`** all 24 packages ·
  ADR-0010 import invariant · all 6 cross-compile targets · `apogee --help` exit 0.
- This is the **first native run of the build-tagged Linux code paths** — before this they had
  only ever been cross-compiled from the Windows host. Closes the Linux execution gap for
  Phase 5, the Phase-5 review-fixes plan (item 13), and the Phase-5 second review-fixes plan.

### The surprise — live landlock enforcement (prior handoff item 2) is also closed

The prior handoff assumed this devbox had landlock **off** and asked for a separate
landlock-capable box (Ubuntu 26.04 rec). **That premise was wrong for this box.** Landlock is
**live** here: it's in `/sys/kernel/security/lsm`, and `apogee probe` reports
`backend: landlock (fs-write: available · network: available)`. So `confinetest.Probe` ran
**live** instead of self-skipping, and the landlock-tagged battery passed under `-race`:

- `write_outside_box_denied`, `write_under_user_profile_denied` — OS-denied ✓
- `inherits_domain_across_exec_denied` ✓ · `parent_unrestricted_after_confined_child` ✓
- `connect_denied_when_network_deny` ✓ · `connect_allowed_when_network_open` ✓

**No second box is needed for the landlock live-enforcement proof.** (Seatbelt tests correctly
skip — macOS-only; already confirmed on Mac hardware 2026-07-02.)

## Docs updated (in commit `eeb11bc`, docs-only + one rename — no code touched)

- **CHANGELOG.md** — "Linux landlock live enforcement" flipped from ⚠️ (kernel off) to ✅
  confirmed 2026-07-23 with the box/run details; macOS bullet's "Linux arm still open" trailer
  updated to closed.
- **TODO.md** — "Closeout Linux pass" leftover marked **✅ DONE** with run details.
- **Merge plan archived** — `git mv docs/plans/implementation-plan-apogee-merge.md
  docs/plans/archived/`. The active plans dir (`docs/plans/*.md`) is now **empty**. The two
  live links to it (`docs/design/technical-design.md`, ADR 0013) were repointed to the archived
  path; the plan's own internal `../` links left as-is, matching the frozen-snapshot convention
  of the other archived plans.

## Repo state

- `main` clean, **pushed** — `origin/main` at `eeb11bc`. No stashes, no other branches.
- A built `./apogee` binary sits in the worktree (from the probe/build steps); it's gitignored
  — `make clean` removes it. Not committed.
- GitHub: zero open issues, zero open PRs.

## Open work (what's left — none of it blocked on this pass anymore)

1. **Live checks newly practical from this box.** All three were previously blocked (no
   reachable endpoint AND, for the automated ones, no cgo for `-race`); this box now clears
   both — `apogee probe` shows the endpoint (`http://192.168.64.1:1111`, `gemma-4-e4b-it-qat`)
   **reachable**, landlock is live, and gcc 15.2 is installed. **These are three DIFFERENT
   things — do not conflate them (an earlier draft of this handoff did):**
   - **(a) Live-model file-edit eval — `make live-eval`** (`TestE2ELiveModel`, `internal/tui/
     live_test.go`). Automated. Drives the real model through a file-edit conversation: it
     streams, requests `write_file`, the write clears the approval rendezvous (auto-allowed as
     the headless stand-in for a human pressing "a"), a file lands in a temp workspace. This is
     the code's "open Phase-1 live eval." It does **NOT** exercise confinement/MCP/sub-agents.
     Fastest to knock out. Note `-count=1` is load-bearing; if you swap the server's loaded
     model, also set `APOGEE_LIVE_MODEL` or the result caches a stale PASS.
   - **(b) Live profile-seam smoke — `TestSmokeLiveProfileSeam`** (`internal/tui/
     smoke_live_test.go`): `APOGEE_LIVE_ENDPOINT=http://192.168.64.1:1111 go test -race
     -count=1 -run TestSmokeLiveProfileSeam -v ./internal/tui/`. Automated. Verifies the
     inline-think delimiter strip against a real model (gemma-4-e4b-it-qat emits
     `<|channel>…<channel|>`, not `<think>` — override via the marker env vars if needed).
   - **(c) Live Auto-confined *deliverable* run — the CHANGELOG "Known post-release
     verification" item. This is the still-open one, and it is MANUAL / owner-run — there is no
     automated test for it.** Launch an interactive Auto session against the endpoint:
     `./apogee --mode auto --endpoint http://192.168.64.1:1111` (endpoint is already in
     `~/.apogee/config.yaml`, so bare `./apogee --mode auto` also works; auto requires the
     filesystem confinement this box has). Then, in a real coding conversation, drive and
     **observe all three** behaviors the CHANGELOG names: (i) a shell command writing **outside
     the workspace** is OS-denied by landlock while an in-workspace write succeeds; (ii) an MCP
     tool still **raises Approval** (Auto fences fs-write but gates the unfenceable surface);
     (iii) a **sub-agent** is delegated and its nested work renders. Only after observing all
     three, flip the CHANGELOG bullet's Linux arm to ✅ (macOS arm is already owner-run there).
2. **OWNER CALL — prune the Windows disk-label walk or not** (~1 ms/object; a 5,051-object tree
   = 5.2 s label / 2.2 s revert on first confined command). Decide, don't code. Details in
   TODO.md and the 2026-07-22 handoff.
3. **Windows-only verification residue** — live Auto-confined run on Windows (if an endpoint is
   reachable there), below-minimum-Windows-build degradation notice, macOS cross-binary smoke.
   All in TODO.md §"Phase-5 verification leftovers".
4. Remaining ISSUES.md bugs, parity P1s (`/server`, session UI), design-session items, the
   >400-line Windows-file refactor — enumerated in `TODO.md` / `ISSUES.md`.

## Suggested skills

- `/archive-handoffs` — the two 2026-07-23 handoffs (this one and the `-00-` refocus audit) and
  the 2026-07-22 handoff are now archivable candidates; the merge plan is already archived.
- `/coding-standards` — mandatory for any new Go code (e.g. if item 1's live run surfaces a fix).
- `/refocus` — not needed soon; this doc plus `TODO.md`/`ISSUES.md` is the current state.
