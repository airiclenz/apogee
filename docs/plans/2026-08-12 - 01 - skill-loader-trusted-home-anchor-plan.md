# Trusted home anchor for the skill loader — implementation plan

- **Goal:** restore the global skill library behind an operator-authored symlink. The
  hostile-bytes hardening (item 11, commit `304cee5`) contained every skill-source anchor
  against its base uniformly, so `~/.apogee/skills` linked at a folder outside the apogee home
  — the owner's actual setup, `~/.apogee/skills → /workspace/repos/skills` — stopped loading
  and `/skills` reports "skill source dir … was not scanned". The workspace anchors keep the
  containment (that half is the real attack: a cloned repo shipping `.apogee/skills` as a
  symlink); the home anchor is operator territory and follows the operator's symlink again.
- **Date:** 2026-08-12 · **Status:** not started
- **Sized for:** ~200k-context host; one item, independently committable
- **Skills:** `coding-standards`
- **Authoritative sources** (an item that disagrees with these follows these):
  - `internal/skills/load.go` at `0b9d799` — `skillAnchor` / `sourceAnchors` / `openAnchor` /
    `loadDir` and their doc comments; the walk caps land here and are NOT touched.
  - `docs/plans/private/2026-08-11 - 06 - hostile-bytes-hardening-plan.md`, item 11 — the change
    being partially walked back, whose NOTES pinned this exact cost ("a `~/.apogee/skills`
    linked at a dotfiles folder now stops loading"); and item 14 — the dangerous-action floor
    now gates model writes to `~/.apogee`, which is what makes the home anchor trustworthy.
  - `internal/skills/doc.go` — "soft must not mean silent"; skip records stay.
  - ADR 0032 — the home library is the user's own and wins collisions; same trust posture.
- **Verified against:** HEAD `0b9d799`. Every `file:line` below was read at that commit.

## Ratified design calls

Owner, 2026-08-12, via AskUserQuestion:

1. **Trust the home anchor.** The home source dir opens via plain `os.OpenRoot(a.dir())` —
   follows the operator's symlink in every component, pins the fence at the RESOLVED library,
   so a symlink below the library that leaves it is still refused and both walk caps stay.
   Workspace anchors keep the two-step base→rel containment exactly as `304cee5` landed it.
   Rejected alternatives: a `skills.dir` config key (more surface, breaks existing symlinked
   setups until each owner edits config); reversing the symlink on this machine (fixes one
   machine, keeps refusing a common dotfiles pattern).
2. **Scope: all three riders in.** Amend the `304cee5` CHANGELOG entry, add a supersession
   NOTES line to hostile-bytes item 11, and pin by test that a symlink BELOW the resolved home
   library still cannot escape it.

## Out of scope

- The workspace anchors' containment (`.apogee/skills`, bare `skills/`) — stands unchanged.
- Any config key for the library location — rejected at ratification.
- Symlinked skill FOLDERS inside a source dir: `fs.WalkDir` never descended into symlinked
  dirs, before or after `304cee5`; this plan does not change that.
- The walk bounds (`maxSkillDirs`, `maxSkillDirDepth`) and the skip-recording contract.

## 1. Follow the operator's symlink on the home library anchor — ✅ DONE (2026-08-12)

NOTES (2026-08-12): CHANGELOG deviation from the run's "verifier is the sole CHANGELOG writer" rule — this item AMENDS the existing `304cee5` `[Unreleased]` bullet in place (the plan's item text and its Files line require exactly that, and explicitly forbid a new entry, since the uniform rule never shipped in a release). An amendment is not expressible as sidecar entry text without producing a duplicate bullet, so the edit is already applied in `CHANGELOG.md` and the sidecar's `CHANGELOG` heading is deliberately omitted — nothing further for the verifier to apply.
NOTES (2026-08-12): the supersession NOTES line was added to item 11 of `docs/plans/private/2026-08-11 - 06 - hostile-bytes-hardening-plan.md`; that doc is git-excluded and is deliberately NOT in FILES above — it must never be staged or committed.
NOTES (2026-08-12): manual acceptance check recorded — on this machine (`~/.apogee/skills → /workspace/repos/skills`) `skills.Load(Sources{Home: "~/.apogee"})` now loads 22 skills including `security-audit`, with zero skips and no soft error (before the change the same call recorded "skill source dir … was not scanned"). `Skill.Dir` still stamps through the symlink path (`/root/.apogee/skills/security-audit`), so the `/skills` source disclosure keeps classifying library skills as the library with no TUI change. Driven by a throwaway `go run` program under the repo, removed immediately after.

**Files:** internal/skills/load.go, internal/skills/load_test.go, CHANGELOG.md,
`docs/plans/private/2026-08-11 - 06 - hostile-bytes-hardening-plan.md` (untracked/private —
the NOTES edit rides along and is never staged or committed)

**What:** `skillAnchor` (`internal/skills/load.go:77`) gains a `trusted bool`, set true only on
the home anchor in `sourceAnchors` (`load.go:98`). `openAnchor` (`load.go:210`) branches on it:
a trusted anchor returns `os.OpenRoot(a.dir())` directly — the pre-`304cee5` open, which
resolves every component including an operator-authored symlink and pins the `os.Root` fence at
the resolved library, so the walk below it stays contained against the TARGET and both walk
caps apply unchanged — while an untrusted anchor keeps the existing two-step base→rel
containment. `Skill.Dir` stamping is untouched (`loadDir` joins from `a.dir()`, the symlink
path), so the `/` menu and `/skills` source disclosure (`internal/tui/skills.go:152`,
hostile-bytes item 10) keeps classifying library skills as the library with no TUI change.

Restate the two doc comments that claim the uniform rule: `loadDir`'s fence paragraph
(`load.go:124-130`) and `openAnchor`'s (`load.go:200-209`). The stated rationale is the trust
split, not a symlink opinion: the workspace anchors are repo-authored bytes and stay contained;
the apogee home is the operator's control plane — the dangerous-action floor gates model writes
to `~/.apogee` (hostile-bytes item 14) — so a symlink found there was placed by the operator
and naming it is exactly how a dotfiles-managed library is wired.

Amend the `304cee5` bullet under CHANGELOG `[Unreleased]` ("A cloned repo can no longer
relocate the skill loader's fence…"): drop "— or at the apogee home for the global library —"
from the pinning sentence and state the split — workspace anchors are contained against the
workspace root; the home library anchor follows the operator's symlink and the fence pins at
the resolved library. No new entry: the uniform rule never shipped in a release, so the
unreleased bullet is corrected in place.

Add one NOTES line to hostile-bytes plan item 11 (private doc, untracked): the uniform-fence
consequence its first NOTES paragraph pinned was walked back for the HOME anchor on 2026-08-12
(owner ratification, this plan); the workspace half stands.

**Tests:** in `internal/skills/load_test.go`:
- Remove the "home library skills/ is the symlink" subcase from `TestLoadAnchorSymlinkRefused`
  (`load_test.go:337`; the subcase and its comment sit near line 379) — the two workspace
  subcases stay.
- New `TestLoadHomeLibraryAnchorSymlinkFollowed`: `<home>/skills` is an ABSOLUTE symlink to an
  outside dir holding a skill; `Load(Sources{Home: home})` loads it with no error and no skips.
- New `TestLoadHomeLibraryEscapeBelowResolvedTargetRefused`: same symlinked home library, plus a
  symlink INSIDE the target pointing at a second outside dir holding a skill; the escapee does
  not load — the fence pinned at the resolved library holds (mirror
  `TestLoadSymlinkEscapeRefused`, `load_test.go:307`).
- `TestLoadAnchorSymlinkInsideBaseFollowed` (`load_test.go:409`) stays green unchanged.
- Windows: skip under `runtime.GOOS == "windows"` like the neighbouring symlink tests.

**Acceptance:**
- `go test -race ./internal/skills/...` passes.
- Manual check recorded in NOTES: on this machine (`~/.apogee/skills → /workspace/repos/skills`),
  `/skills` lists `security-audit` from the library and shows no "was not scanned" skip.

**commit:** `fix(skills): follow the operator's symlink on the home library anchor`

## Verification (whole plan)

- `make check` once at closeout, per the repo convention.
- No version bump: this corrects behavior that never shipped in a release (`v0.12.0` predates
  `304cee5`), and the CHANGELOG amendment folds into the existing `[Unreleased]` bullet.
