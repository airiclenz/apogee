# beads → capcom migration plan

**Goal:** make capcom (`.capcom/`, git-native markdown files) apogee's issue register of record and remove beads (`bd`, its Dolt DB, remote sync and git hooks) from the repository, losing no issue, alias, dependency or attribution guard.
**Date:** 2026-10-12
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** b15fa780

**Sources:**
- `../capcom/docs/migrating-from-beads.md` (the migration recipe and field mapping — authoritative for every conversion rule)
- `../capcom/docs/format.md` (issue file, config and merge-driver format)
- `../capcom/docs/handoffs/2026-10-11 - 01 - apogee-beads-migration-feasibility.md` (local; cut-over surface and data facts)
- Trial converter of 2026-10-12, kept locally at `docs/skill-runs/implement-plan/2026-10-12_-_00_-_beads-to-capcom-migration-plan/trial/` (`convert.py`, `verify.py`; passed `capcom check`, `fmt --check`, 312/41/15 counts, all aliases)

**Ratified design calls** (owner, 2026-10-12):
- **commit-msg hook:** moves to a tracked `.githooks/commit-msg`; each clone sets `core.hooksPath=.githooks`.
- **Slugs:** the executor picks every new id from title and description, no owner gate; a bad one is renamed later with `capcom mv` (old id stays an alias).
- **Archive:** the final `bd export`, `interactions.jsonl` and a copy of `.beads/` go to `../apogee-beads-archive/`, outside the repo, local only.
- **Merge driver:** enabled — `capcom init --merge-driver`; each clone runs the two printed `git config` lines. *Amended at the regression check (2026-10-12): the owner's call is kept, its scope is fixed to the operator's global git config (`git config --global merge.capcom-log.name …` / `merge.capcom-log.driver …`), never repo-local — apogee's gitexec refuses every git tool when the repo's own config holds a `merge.*.driver` key (`internal/gitexec` CommandConfigRefusal, `TestGit_RefusesRepoLocalCommandConfig`).*
- **Old ids in prose:** left as written everywhere (Go comments, docs, CHANGELOG); every beads id survives as a capcom alias and resolves.

**Standing requirements:**
- skills: coding-standards
- **Owner precondition before item 2:** bd writes are frozen on every machine (Windows included) and each ran a final `bd sync`.
- This box is a Raspberry Pi 5: never run `go test -race` over a whole heavy package or pattern; one test at a time with `GOMEMLIMIT=2GiB`; never two test runs at once. Pass this to every sub-agent.
- Never run `bd init`, `bd import` or any bd write. capcom requires `--id` on every `capcom create`.

**Out of scope:**
- Migrating `apogee-sim`'s register (separate `asim-` register, own Dolt remote).
- Archiving or deleting the `airiclenz/apogee-beads` GitHub repo; uninstalling `bd`.
- Rewriting old beads ids in Go comments, test strings (`internal/security/*`), fixtures (`internal/*/testdata/closingshape/`) or historical docs (CHANGELOG, `docs/adr/`, `docs/reviews/`, `docs/plans/archived/`).
- capcom-side defects filed 2026-10-12 (`create-alias-unparseable`, `kv-warning-order`, `beads-guide-trial-gaps`).
- Other clones' one-time setup (they follow the AGENTS.md bullet item 1 writes).

**Regression check (2026-10-12, b15fa780):**
- 1: guard folded (writer's decision on the repoint sentence; Acceptance grep scoped to what the item owns)
- 2: guard folded (scratch repo is a clone of the base; verify.py exits non-zero and asserts the named counts)
- 3: recast (merge driver in global git config; one `/.beads/` ignore line kept; `.capcom/**` LF-pinned)
- 4: guard folded (merge-driver clone instruction says `--global`; historical-line exception widened; subordination rewrite bound to capcom's block wording)
- 5: guard folded (Tests run `TestDesignDoc` in stubllm, one package per command)
- 3 (second round): guard folded (kept ignore line widened to `/.beads*` so the repo-root `.beads.gate.lock` stays ignored; the lock is removed and `git status --porcelain` is empty for the release gate)

## 1. commit-msg hook moves to a tracked .githooks/ — ✅ DONE (2026-10-12)

NOTES (2026-10-12): `core.hooksPath` is set in this checkout's local git config only (not a tracked change); bd's own hooks (pre-commit, post-merge, pre-push, post-checkout, prepare-commit-msg, post-rewrite) no longer run here, as the plan expects under the freeze.
NOTES (2026-10-12): the old AGENTS.md bullet called the hook an "attribution stripper"; the hook refuses (exit 1) and strips nothing, so the rewritten bullet says "refuses". The `.git/hooks/commit-msg` dormant-fallback sentence was kept, repointed to `.githooks/commit-msg`.
NOTES (2026-10-12): the AGENTS.md Dolt-sync bullet still says the git half of the sync "goes live with `bd hooks install`" and names `.beads/hooks/bd-sync-soft`; that bullet is item 4's (and the hooks item 3's), left untouched here.

**What:**
**Goal:** `.githooks/commit-msg` is tracked, byte-identical to `.beads/hooks/commit-msg` at the header base, LF-pinned in `.gitattributes`; this checkout's `core.hooksPath` is `.githooks`; `SECURITY.md` and the `AGENTS.md` git-hooks bullet describe `.githooks/` as the hook directory and how a clone activates it (`git config core.hooksPath .githooks`), with the trust warning kept.
**Approach (assumed at the header base):**
- `cp .beads/hooks/commit-msg .githooks/commit-msg` (keep the exec bit); add `.githooks/** text eol=lf` beside the `.beads/hooks/**` line in `.gitattributes` (item 3 removes the beads line).
- `git config --local core.hooksPath .githooks`. From here on bd's own hooks no longer run; that is expected under the freeze.
- `SECURITY.md` §"What counts" hook paragraph: replace `.beads/hooks/`, `bd init` / `bd hooks install` activation with `.githooks/` and the `git config` command; keep the "running it is a trust decision" substance.
- `AGENTS.md` "Git hooks live in `.beads/hooks/`" bullet: rewrite to `.githooks/` holding only `commit-msg` (the attribution backstop, never the rule); drop the bd-managed hooks, the `bd init` guard, the staged-file-sweep history and the `post-rewrite` text. Add one sentence: a clone activates it with `git config core.hooksPath .githooks`; a clone still using the old beads hook directory repoints to .githooks.
**Regression guard.** AGENTS.md's repoint sentence is worded without the literal path — "a clone still using the old beads hook directory repoints to .githooks" — so item 1's `grep '\.beads/hooks'` Acceptance holds, and item 4's historical-line exception keeps that sentence. The spoken-id (AGENTS.md:34) and Dolt-sync (AGENTS.md:37) bullets are item 4's, so this item's grep covers `SECURITY.md` and the "Git hooks live in" bullet only.
**Files:** .githooks/commit-msg, .gitattributes, SECURITY.md, AGENTS.md
**Read first:** .beads/hooks/commit-msg — refuse-on-attribution grep (exit 1, no stripping); .gitattributes — `.beads/hooks/** text eol=lf` block; SECURITY.md — "A checkout's own git hooks are a trust decision" paragraph;
AGENTS.md — "Git hooks live in" bullet, spoken-id bullet, Dolt-sync bullet; internal/gitexec/gitexec.go — hardeningOptions (core.hooksPath= blanked, so apogee's git tools ignore the move)
**Tests:** `printf 'x: y\n\nCo-Authored-By: Claude <a@b>\n' > /tmp/m && sh .githooks/commit-msg /tmp/m; cat /tmp/m` shows the trailer stripped (or the commit refused, matching the hook's own behaviour at base).
**Acceptance:**
- `cmp .githooks/commit-msg .beads/hooks/commit-msg && test -x .githooks/commit-msg`
- `git config --local --get core.hooksPath` prints `.githooks`
- `git check-attr eol .githooks/commit-msg` prints `lf`
- `grep -n '\.beads/hooks' SECURITY.md` prints nothing; the "Git hooks live in" bullet (`grep -n 'Git hooks live' AGENTS.md`) names `.githooks/`, not `.beads/hooks`
- The item's own commit passes through `.githooks/commit-msg` (no attribution trailer in `git log -1`).
**Commit:** `chore(hooks): move the commit-msg hook to a tracked .githooks/`

## 2. Archive beads and build the converter with its id table — ✅ DONE (2026-10-12)

NOTES (2026-10-12): `bd sync` not run — per the owner's DECISION the final sync was already done on every machine and only read-only bd commands were allowed; `bd diff main origin/main` printed "No changes between main and origin/main", and `bd memories` reports none stored, so no Knowledge Notes to carry.
NOTES (2026-10-12): archive written outside the repo at ../apogee-beads-archive/ (issues-final.jsonl, byte-identical to .beads/issues.jsonl; interactions.jsonl; beads-dir/, a `cp -a` of .beads/ that `diff -r` matches).
NOTES (2026-10-12): idtable.json is hand-chosen, not the trial's mechanical title truncation: 80 spoken ids keep their `apogee-` remainder; 232 others (hash ids, dotted children, and seven spoken ids of five words — e.g. apogee-usage-tap-by-call-id → usage-tap-call-id) get slugs picked from title and description.
NOTES (2026-10-12): verify.py runs capcom itself (`verify.py <export> <repo-root> [idtable]`) instead of taking four pre-made JSON files as the trial did, and also checks every alias resolves via `capcom show`, titles verbatim, and that each related link is stored on one side only (`capcom show` lists it on both).
NOTES (2026-10-12): dry run in a `git clone --no-local` scratch of 5f4acaf5: `capcom init --merge-driver` (merge-driver git config lines not run), types epic/decision added to config by hand, convert.py, `capcom check` 0, `capcom fmt`, `capcom fmt --check` 0, `capcom reindex`, list --all 312 / list 41 / ready 15, verify.py exit 0.

**What:**
**Goal:** `../apogee-beads-archive/` holds `issues-final.jsonl` (a fresh `bd export`, byte-identical to the committed `.beads/issues.jsonl`), `interactions.jsonl` and a full copy of `.beads/`; `scripts/beads-to-capcom/convert.py` and `idtable.json` are committed; `idtable.json` maps every one of the 312 beads ids to a unique capcom slug; a dry run of the converter into a scratch git repo passes `capcom check`, `capcom fmt --check` and `verify.py`.
**Approach (assumed at the header base):**
- Depends on item 1 and the owner precondition. Run `bd sync` once (read the warning; on a halt stop and report) and `bd diff main origin/main` (must be empty), then the read-only `bd export -o ../apogee-beads-archive/issues-final.jsonl`; `cmp` it with `.beads/issues.jsonl`; `cp -a .beads ../apogee-beads-archive/beads-dir`.
- Port the trial `convert.py` / `verify.py` (see Sources) into `scripts/beads-to-capcom/`; if the trial copy is gone, write them from the capcom guide. Run with `python3 -I`. The converter reads ids from `idtable.json`, never derives them.
- `idtable.json`: a spoken id whose `apogee-` remainder is a valid slug keeps it; every other id gets a slug chosen from its title and description — kebab, 2–4 words of `a-z0-9`, ≤ 40 chars, naming the defect or work, never the fix, never hash-derived, unique.
- Conversion rules beyond the guide (binding): an undated lead in `notes` is dated by `created_at`; only a date at the start of a line opens a dated entry, and the date plus one following colon is dropped from the text; a non-plan `spec_id` becomes `- <created_at date> beads: spec: <path>`; a non-empty `close_reason` is kept verbatim; `closed` maps to `done` (no `dropped` — no apogee reason says duplicate, abandoned or rejected); `related` is written into one side's `related:` key; config adds types `epic`, `decision`.
**Regression guard.** The scratch repo is a clone of the base commit (`git clone --no-local . <scratch>`) — `capcom check` stats every `plan:` under `docs/plans/` and `docs/plans/archived/` (`isPlanFound`), so an empty `git init` repo reports missing-plan for all 139 planned issues. The ported `verify.py` exits non-zero on any mismatch and asserts aliases 312 1:1, deps 5, related 2, nesting 60, planned 139, open 41, ready 15 (the trial copy only prints PASS/FAIL and always exits 0).
**Files:** scripts/beads-to-capcom/convert.py, scripts/beads-to-capcom/verify.py, scripts/beads-to-capcom/idtable.json
**Read first:** docs/skill-runs/.../trial/convert.py — choose_ids, split_notes, main (derives ids today; port must read idtable.json); docs/skill-runs/.../trial/verify.py — open-set, alias, deps checks;
.beads/issues.jsonl — 312 rows, statuses closed/open only, types incl. epic/decision; ../capcom/internal/check/check.go — isPlanFound, CodeMissingPlan; ../capcom/docs/migrating-from-beads.md — Steps 1–6, Field mapping, Plans
**Tests:** dry run in a scratch clone of the base (`git clone --no-local . <scratch>`): `capcom init --merge-driver`, converter, `capcom check`, `capcom fmt`, `capcom fmt --check`, `capcom reindex`, `python3 -I verify.py`.
**Acceptance:**
- `cmp ../apogee-beads-archive/issues-final.jsonl .beads/issues.jsonl && test -s ../apogee-beads-archive/interactions.jsonl`
- `python3 -I -c 'import json;t=json.load(open("scripts/beads-to-capcom/idtable.json"));assert len(t)==312==len(set(t.values()))'`
- In the scratch clone: `capcom check` exit 0; `capcom fmt --check` exit 0; `capcom list --all --json` has 312 entries, `capcom list --json` 41, `capcom ready --json` 15; `verify.py` exit 0 (aliases 312 1:1, deps 5, related 2, nesting 60, planned 139, open 41, ready 15 — each asserted, non-zero exit on any mismatch).
**Commit:** `chore(issues): add the beads-to-capcom converter and id table`

## 3. Cut over: .capcom/ in, .beads/ out, in one commit — ✅ DONE (2026-10-12)

NOTES (2026-10-12): capcom-log merge driver wired with `git config --global merge.capcom-log.name "capcom Log merge"` and `git config --global merge.capcom-log.driver "capcom merge-driver %O %A %B %P %S %X %Y"`; `git config --local --get-regexp '^merge\.'` prints nothing. `beads.role` unset from the local config; `.beads/` and `.beads.gate.lock` removed from disk (archived in item 2).
NOTES (2026-10-12): `.claude/settings.json` and `.codex/hooks.json` held only bd hook groups, so each was reset to an empty `hooks` object before `capcom init --claude-hooks --codex-hooks` wrote capcom's SessionStart and PreCompact groups; `.codex/config.toml` kept.
NOTES (2026-10-12): `.gitattributes` gains `.capcom/** text eol=lf` under a short comment, beside the `merge=capcom-log` line `capcom init --merge-driver` appended; the "Git hooks are POSIX sh scripts" comment stays, now covering `.githooks/**` alone. AGENTS.md: one stray blank line left by deleting the bd blocks was collapsed so a single blank precedes the capcom block (`capcom agents --check` still current).
NOTES (2026-10-12): `.capcom/knowledge/` is empty and so untracked; `capcom check` and `capcom prime` were confirmed to pass with the directory absent, as a fresh clone will have it. Converter run: 312 issues, nested 60, deps 5, related 2; `capcom check`, `capcom fmt`, `capcom fmt --check` (0 files), `capcom reindex`, `verify.py` all checks passed — run before `scripts/beads-to-capcom/` was deleted.
NOTES (2026-10-12): remaining `.beads/` mentions outside this item's files are owned elsewhere — `.apogee/skills/release-notes-apogee/prompts/commits-scout.md` (item 6), AGENTS.md convention bullets (item 4), Go test strings and closingshape fixtures (plan out of scope).

**What:**
Recast at the regression check (2026-10-12).
**Goal:** one commit adds the converted `.capcom/` store (312 issues, `capcom check` clean, canonical form) and removes every tracked file under `.beads/` and `.agents/skills/beads/`, the converter directory, both bd-managed `AGENTS.md` blocks (replaced by the capcom block), every `bd` hook group in `.claude/settings.json` and `.codex/hooks.json` (replaced by capcom's), the `.gitignore` beads block (one `/.beads*` line stays) and the `.gitattributes` `.beads/hooks/**` line; the `capcom-log` merge driver is wired in the operator's global git config; no commit holds both trackers or neither.
**Approach (assumed at the header base):**
- Depends on item 2. `capcom init --merge-driver` (run its two printed `git config` lines with `--global`, never repo-local), add types `epic`, `decision` to `.capcom/config.yaml`, run `scripts/beads-to-capcom/convert.py` against `../apogee-beads-archive/issues-final.jsonl`, then `capcom check`, `capcom fmt`, `capcom reindex`, `verify.py`.
- Delete by hand the `SessionStart` (`bd-sync-soft`) and `PreToolUse` (`bd-create-needs-id`) groups in `.claude/settings.json` and all four `bd codex-hook` groups in `.codex/hooks.json`; then `capcom init --claude-hooks --codex-hooks`. Keep `.codex/config.toml` (`hooks = true` now serves capcom).
- `AGENTS.md`: delete the `BEGIN/END BEADS INTEGRATION` and `BEGIN/END BEADS CODEX SETUP` blocks; run `capcom agents`. Convention bullets are item 4's.
- `.gitignore`: replace the "Beads / Dolt files" block with one `/.beads*` line under a one-line comment (a former beads clone's local Dolt state; archive then delete).
- `git rm -r .beads .agents/skills/beads scripts/beads-to-capcom`; `rm -rf .beads` (archived in item 2); `rm -f .beads.gate.lock`; `git config --local --unset beads.role`.
**Regression guard.** The capcom-log merge driver is defined in the operator's GLOBAL git config (`git config --global merge.capcom-log.name …` / `merge.capcom-log.driver …`, the two lines `capcom init --merge-driver` prints, with --global), never repo-local — apogee's gitexec refuses every git tool when the repo's own config holds a merge.*.driver key (internal/gitexec CommandConfigRefusal, TestGit_RefusesRepoLocalCommandConfig); item 3's Acceptance adds `git config --local --get-regexp '^merge\.'` prints nothing. Keep one `/.beads*` line in root .gitignore with a one-line comment (a former beads clone's local Dolt state; archive then delete) and exclude that line from item 3's beads grep — `/.beads*`, not `/.beads/`, so the repo-root `.beads.gate.lock` (and `.beads-credential-key` on other clones) stays ignored; else it shows as `??` and the release gate's clean-tree check (cmd/release/gate.go checkGate, prepare.go checkPrepare) fails. Remove the local lock with `rm -f .beads.gate.lock`; Acceptance adds `git status --porcelain` prints nothing. Add `.capcom/** text eol=lf` to .gitattributes beside capcom's merge=capcom-log line. This supersedes the header's ratified merge-driver scope ("each clone runs the two printed `git config` lines"), amended there.
**Files:** .capcom/, .beads/, .agents/skills/beads/, scripts/beads-to-capcom/, AGENTS.md, .claude/settings.json, .codex/hooks.json, .gitignore, .gitattributes
**Read first:** .gitignore — "Beads / Dolt files" block (`.beads.gate.lock`, `.beads-credential-key`); cmd/release/gate.go — checkGate (`git status --porcelain` clean gate);
../capcom/internal/cli/init.go — mergeDriverAttribute, mergeDriverConfig; internal/gitexec/gitexec.go — CommandConfigName; internal/tools/git_test.go — TestGit_RefusesRepoLocalCommandConfig;
.claude/settings.json — SessionStart bd-sync-soft, PreToolUse bd-create-needs-id; .codex/hooks.json — four bd codex-hook groups
**Tests:** `capcom check`; `capcom fmt --check`; `capcom agents --check`; `capcom prime` exits 0 with no `warning:` line.
**Acceptance:**
- `capcom check && capcom fmt --check && capcom agents --check`
- `capcom list --all --json` 312 entries; `capcom list --json` 41; `capcom ready --json` 15; `capcom show apogee-t57.10 --kv` resolves
- `git ls-files .beads .agents/skills/beads scripts/beads-to-capcom` prints nothing; `test ! -e .beads`
- `grep -nE '\bbd\b|beads' .claude/settings.json .codex/hooks.json .gitignore .gitattributes | grep -vE '^\.gitignore:[0-9]+:(/\.beads\*$|# )'` prints nothing (the kept `/.beads*` line and its comment excepted); `grep -c 'capcom prime' .claude/settings.json .codex/hooks.json` ≥ 1 each
- `git check-attr merge .capcom/issues/x.md` prints `capcom-log`; `git check-attr eol .capcom/issues/x.md` prints `lf`
- `git config --local --get-regexp '^merge\.'` prints nothing
- `test ! -e .beads.gate.lock`; `git status --porcelain` prints nothing after the commit
**Commit:** `chore(issues): move the issue register from beads to capcom`

## 4. AGENTS.md, README and CONTEXT speak capcom — ✅ DONE (2026-10-12)

NOTES (2026-10-12): AGENTS.md gains one new bullet beside the register bullet for the merge-driver clone instruction (both `git config --global` lines, with the repo-local refusal named via `internal/gitexec` `CommandConfigRefusal`), rather than folding it into an existing bullet.
NOTES (2026-10-12): remaining grep hits are migration facts only — AGENTS.md register bullet (`ISSUES.md` → beads 2026-09-03, beads → capcom 2026-10-12), git-hooks bullet (item 1's repoint sentence), spoken-id bullet (migrated beads ids kept as aliases). The old `apogee-net-deny-egress` example was dropped from the alias list: it was never an issue id and does not resolve; the new-slug example is the real `net-deny-egress-gap`.
NOTES (2026-10-12): CONTEXT.md keeps `apogee-fsx` as the plan says; it resolves as an alias of `adaptive-prompt-complexity`.

**What:**
**Goal:** no live sentence in `AGENTS.md`, `README.md` or `CONTEXT.md` names `bd`, beads or a "bead" as apogee's register or its workflow; each states the capcom equivalent.
**Approach (assumed at the header base):**
- Depends on item 3. Rule: every line matching `grep -nE '\bbd\b|beads?\b|\.beads' AGENTS.md README.md CONTEXT.md` is rewritten, except a historical fact (the `ISSUES.md` migration date) which may name beads in the past tense.
- `AGENTS.md` bindings: the register bullet points at `.capcom/` and `capcom ready` / `show` / `create --id`, keeping "closing an issue writes no changelog entry, so do both"; the spoken-id bullet becomes "every issue id is a chosen slug (`capcom create --id <slug>`), two to four kebab words naming the defect or work; rename with `capcom mv`, which keeps the old id as an alias; the 312 beads ids live on as aliases"; the planned-label bullet becomes "a plan owning an issue sets `capcom update <id> --plan '<plan basename>'`; `capcom ready --planned` / `--unplanned` are the two queries"; delete the `issues.jsonl`-lag and Dolt-remote sync bullets; "deferred bead" / "belongs in a bead" become "issue"; the "What beads owns" bullet becomes the same subordination rule for the capcom block. Add: each clone runs the two `capcom-log` merge-driver `git config --global` lines once.
- `README.md` "issue register (`bd`)" and `CONTEXT.md` "(`bd`, `apogee-fsx`)": name capcom; keep the id.
**Regression guard.** AGENTS.md's clone instruction for the merge driver says `git config --global` with the two capcom-log lines, never repo-local. A line may name beads where it states a migration fact (ids kept as aliases, the `ISSUES.md` date, a clone repointing off the old hook directory — item 1's sentence). The "What beads owns" rewrite quotes capcom's "track work there and nowhere else" (`../capcom/internal/agents/block.go` blockIntro) as the wording this section overrides for plans/ADRs/handoffs/CHANGELOG, and drops the "block itself states it is subordinate" clause.
**Files:** AGENTS.md, README.md, CONTEXT.md
**Read first:** AGENTS.md — "Issue register" bullet, "What beads owns", "Every new bead gets a spoken id", "A bead a plan has designed", issues.jsonl-lag and Dolt-sync bullets, docs/handoffs bullet, "Regressions are never deferred";
../capcom/internal/agents/block.go — blockIntro; README.md — "## Status"; CONTEXT.md — "**Capability tier**"; internal/tools/manual_drift_test.go — TestReadmeStatesTheToolCounts
**Tests:** `capcom agents --check` (the managed block untouched).
**Acceptance:**
- `grep -nE '\bbd\b|\bbeads?\b|\.beads' AGENTS.md README.md CONTEXT.md` prints only historical or migration-fact lines (reviewer reads each)
- `capcom agents --check && capcom check`
**Commit:** `docs: describe capcom as the issue register`

## 5. Manual and design docs name capcom — ✅ DONE (2026-10-12)

NOTES (2026-10-12): register named as `(capcom, \`apogee-…\`)`, matching item 4's CONTEXT.md wording; "bead"/"Open bead" became "issue"/"Open issue". All eight ids cited (apogee-304, -n6z, -m3p, -575, -089, -1d8, -tbs, -8wy) resolve via `capcom show` as aliases.

**What:**
**Goal:** `docs/manual/` and the live `docs/design/` docs name capcom wherever they name apogee's register; ids are kept.
**Approach (assumed at the header base):**
- Depends on item 3. Rule: every match of `grep -rnE '\(`bd`|\bbead\b|Open bead|\bbeads\b' docs/manual docs/design --exclude-dir=archived`; known at base: `docs/manual/configuration.md` (`bd`, `apogee-m3p`), `docs/design/confinement-execution-contract.md`, `test-drivers.md`, `tool-surface-findings.md`, `reaction-core-greenfield.md` ("Open bead", "bead `apogee-…`").
- Do not add any `APOGEE_*` variable to configuration.md's "Environment overrides" section (`TestManualListsEveryEnvironmentOverride` checks both directions).
**Regression guard.** The stubllm tests that read `docs/design/test-drivers.md` are `TestDesignDoc*` (no `TestStubLLM*` exists), and the two packages run as two sequential commands, never one `go test` line.
**Files:** docs/manual/configuration.md, docs/design/confinement-execution-contract.md, docs/design/test-drivers.md, docs/design/tool-surface-findings.md, docs/design/reaction-core-greenfield.md
**Read first:** docs/manual/configuration.md — Windows-fence limit paragraph (`apogee-m3p`); cmd/apogee/docs_env_test.go — TestManualListsEveryEnvironmentOverride; internal/stubllm/script_test.go — designDocExample, TestDesignDocExampleScriptParses, TestDesignDocCapturesExampleParses;
docs/design/reaction-core-greenfield.md — header "Open bead", stage-2b amendments, /settings row table; docs/design/test-drivers.md — irreducible limit cells (`apogee-n6z`); docs/design/tool-surface-findings.md — intro (`apogee-304`); docs/design/confinement-execution-contract.md — L5 amendment (`apogee-8wy`)
**Tests:** `GOMEMLIMIT=2GiB go test -count=1 -run TestManual ./cmd/apogee/`, then `GOMEMLIMIT=2GiB go test -count=1 -run TestDesignDoc ./internal/stubllm/` (sequential, never together).
**Acceptance:**
- the rule's grep prints nothing
- the Tests command passes
**Commit:** `docs: name capcom as the issue register in the manual and design docs`

## 6. release-notes-apogee prompts read capcom ids — ✅ DONE (2026-10-12)

NOTES (2026-10-12): the slug example is the live issue `adoption-pane-unarmed` (in `_apogee.md` and `trace-check.md`); trace-check's leak grep is unchanged — a bare capcom slug has no fixed shape a regex can catch, so the prose tells the checker to read for one instead.

**What:**
**Goal:** the `release-notes-apogee` skill classifies capcom commits and ids: ticket ids are capcom slugs or old beads ids (aliases); `chore(issues)` and the historical `chore(beads)` are both no-op commit types; scouts skip `.capcom/` (and historical `.beads/`).
**Approach (assumed at the header base):**
- Rule: every match of `grep -rnE '\bbeads?\b|\bbd\b|\.beads' .apogee/skills/release-notes-apogee/`; known at base: `prompts/_apogee.md`, `commits-scout.md` (two lines), `trace-check.md`, `_inventory-format.md`, `_notes-format.md`, `changelog-scout.md`, `merge.md`.
- "bead id" → "issue id"; keep `apogee-qi3`-shaped examples (still valid aliases) and add one slug example.
**Files:** .apogee/skills/release-notes-apogee/prompts/{_apogee.md,commits-scout.md,trace-check.md,_inventory-format.md,_notes-format.md,changelog-scout.md,merge.md}
**Read first:** .apogee/skills/release-notes-apogee/prompts/_apogee.md — "Commit conventions"; .apogee/skills/release-notes-apogee/prompts/commits-scout.md — step 3 skip list, Bookkeeping; .apogee/skills/release-notes-apogee/prompts/trace-check.md — id classes;
.apogee/skills/release-notes-apogee/prompts/_inventory-format.md — Refs; .apogee/skills/release-notes-apogee/prompts/changelog-scout.md — Refs; .apogee/skills/release-notes-apogee/prompts/_notes-format.md;
.apogee/skills/release-notes-apogee/prompts/merge.md; internal/skills/repo_skills_test.go — release skill ids
**Tests:** none beyond the grep (prose-only).
**Acceptance:**
- the rule's grep prints only lines naming `chore(beads)` or `.beads/` as historical
**Commit:** `chore(skills): teach release-notes-apogee capcom issue ids`
