# Release pipeline in the repo — plan

**Goal:** Move apogee's release process into the repo per ADR 0098: a Go maintainer program `cmd/release` (`status`, `prepare`, `publish`, `dist`, `smoke`) does the deterministic work on Linux, macOS and Windows, and two committed host-agnostic skills (`release-notes-apogee`, `release-apogee`) do the judgment work. The bash release scripts and `make release-scoop`/`release-winget` retire.
**Date:** 2026-10-11
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 6fee4616

**Sources:**
- `docs/adr/0098-the-repo-owns-its-release-pipeline.md` (authority; wins over any item on a conflict)
- `docs/adr/0097-the-tui-checks-for-a-newer-release-and-names-the-upgrade-command.md` §6 (as amended 2026-10-11)
- `docs/handoffs/2026-10-11 - 00 - release-automation-in-repo.md` (skill-design decisions, verified facts)
- Personal reference engines: `~/.claude/skills/release-notes/`, `~/.claude/skills/cut-release/`

**Ratified design calls** (owner, 2026-10-11 grill unless noted):
- **Split:** ADR 0098 §1–8 as written, except §5 as amended below.
- **CI gate (owner, 2026-10-11, plan write):** gate commit G = HEAD with its trailing first-parent commits that touch only `CHANGELOG.md` and/or `VERSION` peeled off. Cleared when the tree is clean, G is an ancestor of or equal to `origin/main`, `origin/main` is an ancestor of or equal to HEAD, and `ci.yml` has a successful run for G or for any peeled commit that `origin/main` contains (pushes run CI only on the push head, so a feature+micro-bump push leaves the green run on the bump) — widened at the 2026-10-11 regression check. No bypass.
- **Dist source (owner, 2026-10-11, plan write):** `publish` builds archives in a fresh local `git clone` of the repo checked out at the tag, in a temp dir — never in the current checkout.
- **docs-truth probe (owner, 2026-10-11, plan write):** dropped; not ported.
- **Skill ids:** exactly `release-notes-apogee` and `release-apogee` (the personal skills' delegate guard matches them).
- **Notes pipeline:** recon and plans scouts dropped; baseline is `status --json`; blind scouts changelog/commits/surface kept; merge → verify (contested only) → notes → trace-check kept; notes format unchanged; ~600–800 lines total.
- **Version proposal:** minor if the inventory has Breaking or is feature-heavy, else patch; fallback next patch above the highest `v0.*` tag; owner confirms; `prepare` refuses a version not strictly above the highest `v0.*` tag (retracted `v1.x` ignored).
- **Commit subjects:** rollup `chore(release): cut vX.Y.Z`; bump `chore(version): bump version to vX.Y.Z`; tap/bucket `apogee X.Y.Z`.
- **Release title:** the tag (`vX.Y.Z`), matching published releases.

**Standing requirements:**
- skills: coding-standards
- Pi 5 box: tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run … <one package>`; never race `./...`, `./internal/tui/...` or `./cmd/apogee/...`; never two test runs at once. Brief every sub-agent with this.
- `cmd/release` tests never reach the network or real `git`/`gh`/`komac`: every external command goes through the one runner seam (item 1).
- Deviations from item text land as a dated NOTES line under the item.

**Out of scope:**
- CI-cut releases (ADR 0098 §7); code signing (`apogee-3p1`); the first `komac new` (owner, manual).
- Fixing red CI (`apogee-ci-e2e-timeout-flakes`) — it blocks the first cut, not this plan.
- The personal skills' delegate guard (done in `/home/airic/repos/skills`).
- Embedding the release skills in the binary; any change to apogee's tool counts.

**Regression check (2026-10-11, 6fee4616):**
- 2: recast — CI condition widened to a green run on G or any peeled commit `origin/main` contains; `status` version optional / bare X.Y.Z; supersedes ADR 0098 §5's run-for-that-SHA rule (item 14 amends it).
- 4: guard folded — refuse when `VERSION` already reads vX.Y.Z; acceptance builds the binary in the repo before running it in the clone.
- 5: guard folded — `.gitattributes` pins `cmd/release/templates/**` to LF.
- 6: recast — `--dry-run` reports a blocked gate / VERSION mismatch and exits 1 but still prints probes and the plan; guard folded — fetch the tag before `rev-parse`.
- 7: guard folded — `SHA256SUMS` downloaded to stdout or the run's temp dir; dry-run acceptance takes item 6's form.
- 8: recast — version argument optional / bare X.Y.Z; guard folded — unreadable manifest is SKIP, host os/arch are fields.
- 9: recast — wrappers pass `$(VERSION)` verbatim; ci.yml comment reworded; acceptance grep replaced with a `git grep` expecting no output.
- 10: guard folded — Phase 1 ignores `status`'s exit code; read-only statement carves out `status`'s fetch and names the `gh` need; first `.apogee/` effect stated.
- 13: guard folded — `check-ignore --no-index`; load test uses `Sources{ProjectRoot}` alone.
- 14: recast — Windows vet/test trees rule and the widened CI rule in ADR 0098 §5; guard folded — `git grep` acceptance and a wider building.md sweep.
- 2 (re-check): guard folded — peel lists paths with the two-tree `git diff-tree -r --name-only <c>^1 <c>` and stops at any commit whose parent count is not 1.
- 6 (re-check): recast — gate and VERSION-at-HEAD == vX refusal apply only while the tag step is pending; once the remote annotated tag exists, release and channel steps converge from remote state (as ADR 0098 §4 and §8 require).
- 7 (re-check): channel steps inherit item 6's tag-done convergence (What line; "tag done, VERSION moved on, dirty tree → winget still converges" test).
- 14 (re-check): guard folded — the Windows-trees `git grep` also matches the bare `internal/auditsplit` of building.md's make-check row.

## 1. `cmd/release` skeleton, runner seam and the `dist` verb — ✅ DONE (2026-10-11)

NOTES (2026-10-11): each archive also carries the top-level `<stem>/` directory entry (mode 0755), as `tar -czf`/`zip -qr` in `make dist` wrote it; LICENSE and README.md are packed straight from `--src` rather than copied into the staging dir first; staging lives in an OS temp dir removed on exit.

NOTES (2026-10-11): added beyond the item text — a guard refusing an `--out` that is or contains `--src` (it would be emptied), and a failing `git rev-list`/`git log` is a run failure (exit 1) rather than an omitted build number; an empty count still omits it as the item says.

NOTES (2026-10-11): the root command declares no `Args` (unlike cmd/stubllm's `cobra.NoArgs`), so an unknown verb exits 2 instead of printing help and exiting 0.

**What:**
**Goal:** `go run ./cmd/release dist [--src DIR] [--out DIR]` writes the six release archives and `SHA256SUMS` with the exact names, layout and ldflags `make dist` produces, using only the Go stdlib for packing and hashing, byte-identical across two runs over the same tree.
**Approach (assumed at the header base):** New package `main` in `cmd/release/` on the `cmd/stubllm` pattern: cobra root (`newRootCommand`), `SilenceErrors`, `exitRunFailed=1`/`exitBadUsage=2` via a `runError` wrapper, dev-tool doc comment ("never a release asset"). One deep runner seam — `type runner interface{ run(ctx, dir string, env []string, name string, args ...string) (string, error) }` — with an `execRunner` (os/exec, stdout returned, stderr in the error) and a recording fake in tests; every later verb takes it. `dist`: reads `<src>/VERSION` (trim, drop `v` → bare); for each of linux/darwin/windows × amd64/arm64 runs `go build -trimpath -ldflags "<L>" -o <stage>/apogee_<bare>_<os>_<arch>/<apogee|apogee.exe> ./cmd/apogee` with `CGO_ENABLED=0 GOOS GOARCH`, where `<L>` = `-X github.com/airiclenz/apogee.buildCount=<git rev-list --count HEAD>` (omitted if empty) + ` -X github.com/airiclenz/apogee.distBuild=release-archive`; copies `LICENSE`, `README.md` into that top dir; packs windows as `.zip`, others `.tar.gz` (`archive/zip`, `archive/tar`+`compress/gzip`); entries forward-slash, sorted, mode 0755 for the binary / 0644 otherwise, uid/gid 0, mtime = HEAD commit time, gzip header without name/mtime; writes `SHA256SUMS` as `<hex>  <name>` lines sorted by name; removes staging dirs. Default `--out` is `<src>/dist`, emptied first.
**Files:** cmd/release/main.go, cmd/release/runner.go, cmd/release/dist.go, cmd/release/main_test.go, cmd/release/dist_test.go
**Read first:** Makefile — dist; internal/update/stage.go — extractTarGzEntry, extractZipEntry, checksumFor (entry must be `<stem>/<bin>`, TypeReg / regular zip mode); cmd/stubllm/main.go — newRootCommand, runError; version.go — buildCount, distBuild
**Tests:** fake runner records six `go build` argv/env with the exact ldflags string (and the empty-count case); a fake build that writes a stub binary yields archives whose entries are `apogee_<bare>_<os>_<arch>/{apogee[.exe],LICENSE,README.md}`; `SHA256SUMS` format and order; two runs produce identical bytes; usage error exits 2.
**Acceptance:**
- `go vet ./cmd/release/ && GOOS=windows go vet ./cmd/release/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/release/`
- `d=$(mktemp -d) && go run ./cmd/release dist --out "$d" && ls "$d" | wc -l` prints 7, and `grep -ac release-archive` finds the stamp in the extracted linux_arm64 binary.
**Commit:** `feat(release): add cmd/release with a stdlib dist verb`

## 2. `status`: version helpers, the CI gate and the baseline — ✅ DONE (2026-10-11)

NOTES (2026-10-11): consequential edit — cmd/release/main.go: made necessary by the new status verb (registered in newRootCommand; its usage line added to the package doc)

NOTES (2026-10-11): status-level tests (baseline, JSON keys pinned, version argument and VERSION default, exit status, text table) live in a new cmd/release/status_test.go rather than being folded into gate_test.go/version_test.go; the gate's own tests stay in gate_test.go and the version helpers' in version_test.go

NOTES (2026-10-11): internal/update.Newer is not reused: its parsed version type and retraction bounds are unexported and it refuses a bare X.Y.Z, so version.go keeps its own parser (same tag pattern, `v` optional) and the same [v1.0.0, v1.8.0] bounds; prepare's "strictly above the highest tag" check can still call update.Newer

NOTES (2026-10-11): a commit that changes no path at all is not peeled (the gate stops at it), reading "touch only CHANGELOG.md and/or VERSION" as touching at least one of them

NOTES (2026-10-11): `git merge-base --is-ancestor` is read by exit status through an ExitCode() interface on the runner's error (exec.ExitError carries it), so exit 1 means "no" and anything else is a check that could not run; runner.go is unchanged

NOTES (2026-10-11): status prints its report before failing, so a blocked gate or a check that could not run (gh missing, fetch failed) still shows what was found; a check that could not run inside the gate is named as a gate failure, and a baseline or tag-list error is returned after printing

Depends on item 1.

**What:** Recast at the regression check (2026-10-11).
**Goal:** `go run ./cmd/release status [vX.Y.Z] [--json]` reports the gate (per the ratified CI-gate call, with each failing condition named), the baseline (latest *published* GitHub release tag, its publish date, and the commit range `<tag>..HEAD` with its count), and the highest `v0.*` tag; exit 0 when the gate clears, 1 when it is blocked or a probe errored, 2 on usage.
**Approach (assumed at the header base):** `version.go`: parse `vMAJOR.MINOR.PATCH`, compare (reuse `internal/update.Newer` where it fits), highest `v0.*` from `git tag --list 'v*'` ignoring the retracted `[v1.0.0, v1.8.0]` range. `gate.go`: `git fetch --quiet origin main --tags`; peel HEAD's trailing first-parent commits whose `git diff-tree --name-only` set ⊆ {CHANGELOG.md, VERSION}; ancestry via `git merge-base --is-ancestor`; clean tree via `git status --porcelain`; CI via `gh run list --workflow ci.yml --commit <G> --status success --json databaseId`. Baseline via `gh release view --json tagName,publishedAt` (latest) — never the newest tag. JSON shape (stable, consumed by the skills): `{"version","highestTag","gate":{"ok","commit","failures":[]},"baseline":{"tag","published","range","commits"},"steps":[]}`; `steps` is filled by item 3. Text output is a short table.
**Regression guard.** The CI condition of the gate clears when ci.yml has a successful run for G or for any peeled commit that origin/main contains (pushes run CI only on the push head, so a feature+micro-bump push leaves the green run on the bump); add a test "green run on the peeled bump, none on G". The version argument of status is optional (default: the VERSION file's value) and a bare X.Y.Z is accepted with `v` prepended. Supersedes ADR 0098 §5 ("ci.yml has a successful run for that SHA", lines 77–79); item 14's §5 edit carries the widened rule.
The peel lists a commit's paths with the two-tree form `git diff-tree -r --name-only <c>^1 <c>` (the single-commit form prints the commit id as its first line, and nothing for a merge), and stops at any commit whose parent count is not 1 (`git rev-list --parents -n1 <c>`); this replaces the Approach's `git diff-tree --name-only`. Fake-runner diff-tree fixtures copy real `git diff-tree` output.
**Files:** cmd/release/version.go, cmd/release/gate.go, cmd/release/status.go, cmd/release/version_test.go, cmd/release/gate_test.go
**Read first:** internal/update/version.go — Newer, isRetracted; .github/scripts/version-bump.sh — bumps loop (--first-parent, every VERSION change tagged); .github/workflows/ci.yml — on.push/pull_request; .github/workflows/tag-on-version-bump.yml — "Create the annotated tags"; docs/adr/0098-the-repo-owns-its-release-pipeline.md — §3 status, §5 gate; cmd/release/runner.go — runner (item 1)
**Tests:** version order incl. `v1.8.0` ignored; peel stops at the first commit touching another path; merge commit at HEAD is never peeled; a diff-tree fixture copied from real output (bump commit → `VERSION` only) peels; each gate failure (dirty, G not pushed, origin/main ahead of HEAD, no green run) named; green run on the peeled bump, none on G → cleared; no argument → the `VERSION` value, bare `X.Y.Z` → `vX.Y.Z`; baseline from latest release while newer bare tags exist; JSON keys pinned.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/release/`
- `go run ./cmd/release status --json` prints parseable JSON (`| python3 -m json.tool`) with baseline tag equal to `gh release view --json tagName -q .tagName`.
**Commit:** `feat(release): status reports the CI gate and the release baseline`

## 3. `status`: per-channel step probes

Depends on item 2.

**What:**
**Goal:** `status vX.Y.Z` (and, without an argument, the `VERSION` value when it is above the baseline) lists one row per publish step — `tag`, `release`, `homebrew`, `scoop`, `winget`, `smoke` — each `done`, `pending`, `setup-pending` or `unknown` with a detail; the same probe functions are what `publish` (items 6–7) uses to skip done work.
**Approach (assumed at the header base):** `probes.go`, all read-only via the runner: tag — `git ls-remote --tags origin refs/tags/vX` plus `gh api repos/airiclenz/apogee/git/ref/tags/vX --jq .object.type` == `tag` (a lightweight tag is a failure, not done); release — `gh release view vX --json assets`, done only with all seven assets (six archives + `SHA256SUMS`); homebrew — `gh api repos/airiclenz/homebrew-tap/contents/Formula/apogee.rb`, done when its `version "X.Y.Z"` matches; scoop — repo missing → `setup-pending`, `bucket/apogee.json` missing or other version → pending (an empty repo is pending, not setup-pending); winget — `gh api repos/microsoft/winget-pkgs/contents/manifests/a/AiricLenz/Apogee` 404 → `setup-pending`; done when `manifests/a/AiricLenz/Apogee/X.Y.Z` exists (merged, "published") or `gh pr list --repo microsoft/winget-pkgs --state open --search "AiricLenz.Apogee X.Y.Z in:title"` finds a PR ("submitted", detail names it); smoke — always `pending` in status (it is an action). A probe that cannot run (gh missing, auth) is `unknown`, never `done`.
**Files:** cmd/release/probes.go, cmd/release/probes_test.go, cmd/release/status.go
**Read first:** scripts/release-scoop.sh — BUCKET_REPO default airiclenz/scoop-bucket, MANIFEST_PATH bucket/apogee.json; scripts/release-winget.sh — PACKAGE_ID AiricLenz.Apogee; docs/adr/0098-the-repo-owns-its-release-pipeline.md — §4 converge probes, §8 setup-pending; cmd/release/status.go — steps (item 2 JSON); .github/workflows/tag-on-version-bump.yml — createTag (annotated tags)
**Tests:** each probe's done/pending/setup-pending/unknown branch over canned fake-runner output, incl. lightweight tag, release with 6 of 7 assets, empty bucket repo, winget PR open vs merged vs absent.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/release/`; `go run ./cmd/release status v0.24.11` shows tag/release/homebrew `done`, scoop and winget not `done`.
**Commit:** `feat(release): status probes every publish step`

## 4. `prepare` verb

Depends on item 2.

**What:**
**Goal:** `go run ./cmd/release prepare vX.Y.Z` makes exactly two local commits — `chore(release): cut vX.Y.Z` touching only `CHANGELOG.md` (a `## [X.Y.Z] — YYYY-MM-DD` heading, local date, em dash, no `v`, inserted directly under `## [Unreleased]` with one blank line on each side, so every Unreleased bullet now sits under it), then `chore(version): bump version to vX.Y.Z` touching only `VERSION` (`vX.Y.Z\n`) — and pushes nothing.
**Approach (assumed at the header base):** Refuse (exit 1, nothing written) when: the version does not parse; it is not strictly above the highest `v0.*` tag (item 2 helper); the tree is dirty; `## [Unreleased]` is missing or has no bullet before the next `## [` heading; a `## [X.Y.Z]` heading already exists. Stage by path (`git add CHANGELOG.md`, then `git add VERSION`), never `-A`. No link-reference line is added (0.x releases carry none).
**Regression guard.** Also refuse (exit 1, nothing written) when `VERSION` already reads vX.Y.Z — an unpushed micro-bump — naming it and suggesting the next patch. The acceptance builds in the repo first (`go build -o "$tmp/release" ./cmd/release`) and runs `"$tmp/release" prepare v0.99.0` inside the clone: `go run <repo>/cmd/release` there fails as outside the clone's main module.
**Files:** cmd/release/prepare.go, cmd/release/prepare_test.go
**Read first:** CHANGELOG.md — `## [Unreleased]`, `## [0.24.11] — 2026-10-08` heading (5b3ed35c is the exact rollup shape); .github/scripts/version-bump.sh — read_version, bumps (tags only commits that change VERSION); cmd/release/version.go — highest v0.* tag helper (item 2); internal/update/version.go — Newer (false on a retracted latest)
**Tests:** rollup over a fixture CHANGELOG (bytes before/after); each refusal, incl. a fixture whose `VERSION` equals the argument; fake runner sees two commits with the exact subjects and staged paths; always a fresh bump commit.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/release/`; `go build -o "$tmp/release" ./cmd/release` in the repo, then in a scratch `git clone` of it `"$tmp/release" prepare v0.99.0` yields `git log -2 --name-only` showing the two subjects with `CHANGELOG.md` and `VERSION` alone.
**Commit:** `feat(release): prepare makes the changelog rollup and version bump commits`

## 5. Homebrew formula and Scoop manifest templates

Depends on item 1.

**What:**
**Goal:** `cmd/release` renders the whole Homebrew formula and Scoop manifest for a version from embedded templates and a `SHA256SUMS` body; both outputs say they are generated by `cmd/release` in `airiclenz/apogee` and are overwritten at each release.
**Approach (assumed at the header base):** `cmd/release/templates/apogee.rb.tmpl` reproduces the live formula (desc, homepage, version, license, `on_macos`/`on_linux` × `on_arm`/`on_intel` url+sha256, `def install`, `caveats`, `test`) with a leading generated-comment and its "one `make dist`" comment reworded to `cmd/release dist`; `cmd/release/templates/scoop.json.tmpl` reproduces `scripts/release-scoop.sh`'s manifest (4-space indent, `64bit`/`arm64`, `extract_dir`, `bin: apogee.exe`, `checkver: github`, `autoupdate` with `$baseurl/SHA256SUMS`) plus a `"##"` generated note. `render.go`: `//go:embed`, `text/template`, hash lookup accepting `hash  name` and `hash *name`, lowercased, 64-hex required, missing archive → error. Rendered JSON must parse.
**Regression guard.** `.gitattributes` gains `cmd/release/templates/** text eol=lf` with the house comment: the templates are go:embed'd, so a core.autocrlf=true checkout (windows-latest) bakes CRLF into them — the 0.24.11 formula byte-equality test goes red once item 9 adds `./cmd/release/...` to test-windows, and a Windows-host publish would push a CRLF formula.
**Files:** cmd/release/templates/apogee.rb.tmpl, cmd/release/templates/scoop.json.tmpl, cmd/release/render.go, cmd/release/render_test.go, cmd/release/testdata/apogee-0.24.11.rb, .gitattributes
**Read first:** scripts/release-scoop.sh — write_manifest, arch_block, autoupdate_block; release_scripts_test.go — TestReleaseScoopDryRun; airiclenz/homebrew-tap Formula/apogee.rb (live, via gh api contents) — "one `make dist`" comment; .gitattributes — embed LF pins, `**/testdata/**`; .github/workflows/ci.yml — test-windows
**Tests:** formula for 0.24.11 with the live hashes equals `testdata/apogee-0.24.11.rb` (the live formula plus the generated comment and reworded line); every assertion of `release_scripts_test.go`'s `TestReleaseScoopDryRun` ported to the rendered manifest; bad/missing hash errors.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/release/`
**Commit:** `feat(release): render the Homebrew formula and Scoop manifest from templates`

## 6. `publish`: gate, push, tag, archives and the GitHub release

Depends on items 1, 3, 4.

**What:** Recast at the regression check (2026-10-11).
**Goal:** `go run ./cmd/release publish vX.Y.Z --notes-file F [--dry-run]` converges the `tag` and `release` steps: never prompts or reads stdin; refuses unless the gate clears and `VERSION` at HEAD equals vX.Y.Z; skips each step its probe reports done; on failure exits non-zero after printing the status table; `--dry-run` runs gate and probes and prints what each pending step would do, changing nothing.
**Approach (assumed at the header base):** `publish.go` orchestrates steps in order (tag, release; items 7–8 append homebrew, scoop, winget, smoke). Tag step: `git push origin HEAD:main` when `origin/main` is behind HEAD, then poll `git ls-remote --tags origin refs/tags/vX` (`--tag-timeout`, default 10m, 10s interval) until present; then require it annotated and `git rev-parse vX^{commit}` == the bump commit; never create a tag. Release step: `git fetch --tags origin`; `git clone --quiet <toplevel> <tmp>` + checkout `vX`; item 1's `dist` with `--src <tmp>`; if no release, `gh release create vX --verify-tag --title vX --notes-file F --latest <7 files>`; if it exists with assets missing, download its `SHA256SUMS` when present and require the rebuilt sums equal it (mismatch → fail naming `gh release delete-asset`), then `gh release upload` only the missing ones. `--notes-file` must be a readable non-empty file. Temp dirs removed on exit.
**Regression guard.** Under --dry-run a blocked gate or VERSION ≠ vX is reported and the run exits 1, but probes and the per-step plan are still printed. After the `ls-remote` poll succeeds the tag step fetches the tag (`git fetch origin refs/tags/vX:refs/tags/vX`) before `git rev-parse vX^{commit}` — on a first run the workflow creates it after the gate's fetch, so the local ref is absent.
The gate and the VERSION-at-HEAD == vX refusal apply only while the tag step is pending (it is the only step that pushes HEAD). Once the remote annotated tag exists, publish converges the release step (built from a fresh clone at the tag) and every channel step from remote state alone, whatever HEAD, VERSION or the working tree hold — this is what ADR 0098 §4 and §8 require for resuming a release after later micro-bumps (e.g. winget converging days later). Add a test "tag done, VERSION moved on, dirty tree → winget still converges". Item 7's channel steps inherit this.
**Files:** cmd/release/publish.go, cmd/release/publish_test.go
**Read first:** docs/adr/0098-the-repo-owns-its-release-pipeline.md — §3 publish, §4 converges, §8 setup-pending; .github/workflows/tag-on-version-bump.yml — "Create the annotated tags" (createTag/createRef); scripts/release-smoke.sh — pre-check A (ls-remote + gh api .object.type); scripts/release-winget.sh — VERSION default block; Makefile — dist, DIST_LDFLAGS
**Tests:** fake runner: full run order; second run with all probes done issues no mutating command; dry-run issues none; blocked gate (tag pending); tag done, VERSION moved on, dirty tree → the release step still converges, no gate or VERSION refusal; dry-run with a blocked gate exits 1 yet prints every probe row and planned action; first-run tag fetched before `rev-parse`; tag timeout; lightweight tag refused; partial release uploads only missing assets; sums mismatch fails; argv of `gh release create` pinned incl. `--verify-tag`.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/release/`; `go run ./cmd/release publish v0.24.11 --notes-file README.md --dry-run` output shows tag and release `done` (exit status not checked) and `git status --porcelain` is unchanged.
**Commit:** `feat(release): publish converges the tag and the GitHub release`

## 7. `publish`: Homebrew tap, Scoop bucket and winget

Depends on items 5, 6.

**What:** The channel steps inherit item 6's regression guard: once the remote annotated tag exists they converge from remote state alone, with no gate or VERSION-at-HEAD refusal.
**Goal:** `publish` also converges `homebrew`, `scoop` and `winget`: a `setup-pending` channel is skipped and named and the run still succeeds; the tap and bucket get a direct commit `apogee X.Y.Z` and push only when the rendered file differs; winget is submitted with komac and reported as "submitted", never "published".
**Approach (assumed at the header base):** Hashes come from the release's `SHA256SUMS` (`gh release download vX -p SHA256SUMS`). Tap: `gh repo clone airiclenz/homebrew-tap <tmp>`, write `Formula/apogee.rb` (item 5), `git add` that path, no staged diff → done, else commit + `git push origin HEAD`. Bucket: same on `airiclenz/scoop-bucket` writing `bucket/apogee.json`; an empty repo pushes with `git push origin HEAD:main`. Winget: `komac update AiricLenz.Apogee --version <bare> --urls <windows_amd64.zip url> <windows_arm64.zip url> --submit`, token only via env (`GITHUB_TOKEN`, else `gh auth token`), never argv or output; komac missing → step fails with install hint (cargo/brew/winget). Dry-run prints the komac argv with the token redacted.
**Regression guard.** `SHA256SUMS` is downloaded with `gh release download vX -p SHA256SUMS -O -` (stdout) or `--dir` under the run's temp dir — never into the checkout, whose clean-tree gate the next `status`/`publish` checks.
**Files:** cmd/release/channels.go, cmd/release/channels_test.go, cmd/release/publish.go
**Read first:** scripts/release-winget.sh — komac_cmd, token lookup (GITHUB_TOKEN, gh auth token), komac-missing die; scripts/release-scoop.sh — clone/add/diff --cached/commit/push tail; release_scripts_test.go — TestReleaseWingetDryRun, TestReleaseWingetRequiresKomac; docs/adr/0098-the-repo-owns-its-release-pipeline.md — §8 setup-pending skip
**Tests:** port `TestReleaseWingetDryRun` (token never printed) and `TestReleaseWingetRequiresKomac`; tap no-diff run commits nothing; empty bucket push argv; setup-pending scoop/winget skip with success exit; komac never run when the PR probe says submitted; the `SHA256SUMS` download argv writes outside the checkout; tag done, VERSION moved on, dirty tree → winget still converges.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/release/`; `go run ./cmd/release publish v0.24.11 --notes-file README.md --dry-run` output (exit status not checked) names the scoop and winget lines.
**Commit:** `feat(release): publish converges the tap, the Scoop bucket and winget`

## 8. `smoke` verb, run last by `publish`

Depends on items 1, 6, 7.

**What:** Recast at the regression check (2026-10-11).
**Goal:** `go run ./cmd/release smoke [vX.Y.Z] [--dist-dir D]` (the version argument is optional — default: the VERSION file's value — and a bare X.Y.Z is accepted with `v` prepended) runs the checks of `scripts/release-smoke.sh` in Go and exits 1 if any fails; a check whose tool or input is missing prints `SKIP:` and never counts as a pass in the summary; `publish` runs it as its final step and passes its build dir.
**Approach (assumed at the header base):** In order: annotated tag on the remote (item 3 probe); with `--dist-dir`, its `SHA256SUMS` equals the published one (replaces the script's local `make dist` check); every one of the six assets downloads (`gh release download`) and matches `SHA256SUMS` (`crypto/sha256`); each extracted binary's `debug/buildinfo` `vcs.revision` equals `vX^{commit}` (missing → fail, `vcs.modified=true` → warn); the host's archive (`runtime.GOOS/GOARCH`, `.zip` or `.tar.gz`) runs `--version` containing vX; Homebrew `brew update && brew upgrade apogee` + version check, only when apogee is brew-installed; Scoop manifest via `gh api …/contents/bucket/apogee.json`: version and both arch hashes (lowercased, `sha256:` stripped) equal `SHA256SUMS`, a mismatch fails only when vX is the latest release. Summary line counts pass/fail/skip.
**Regression guard.** An unreadable Scoop manifest (404, empty repo, gh error) prints `SKIP:` as `release-smoke.sh` does — `airiclenz/scoop-bucket` is empty today; only a readable manifest at another version is a mismatch. The host os/arch are fields defaulting to `runtime.GOOS`/`runtime.GOARCH`, so a test selects windows/amd64 on a Linux host.
**Files:** cmd/release/smoke.go, cmd/release/smoke_test.go, cmd/release/publish.go
**Read first:** scripts/release-smoke.sh — binary_revision, binary_modified, pre-check A, host-archive block, latest_release, manifest_hash; Makefile — release-smoke; .github/workflows/tag-on-version-bump.yml — annotated tag creation
**Tests:** each check pass/fail/skip over fixtures (tiny archives built in-test, a Go test binary for buildinfo is not required — fake the buildinfo reader behind a func var); zip host archive path (host fields set to windows/amd64); unreadable manifest → SKIP; no argument → the `VERSION` value, bare `X.Y.Z` → `vX.Y.Z`; skip never counted as pass; exit code.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/release/`; `go run ./cmd/release smoke v0.24.11` passes the tag, asset and buildinfo checks.
**Commit:** `feat(release): port the release smoke test to cmd/release`

## 9. Retire the bash release path; wire Make and CI

Depends on items 1–8.

**What:** Recast at the regression check (2026-10-11).
**Goal:** `make dist`, `make release-status` and `make release-smoke` are one-line wrappers over `go run ./cmd/release` (`dist`; `status $(VERSION)`; `smoke $(VERSION)`); `make release-scoop`, `make release-winget` and `scripts/release-{scoop,winget,smoke}.sh` no longer exist; CI's `test-windows` leg and `make check`'s Windows vet line include `./cmd/release/...`; `.apogee/skills/** text eol=lf` is pinned.
**Approach (assumed at the header base):** Makefile: replace the `dist` recipe and the three release targets (keep `##` help lines, reworded); drop `CROSS_TARGETS`/`DIST_*`/`SHA256` vars only if nothing else uses them (`cross` uses `CROSS_TARGETS`). Delete `release_scripts_test.go` (its assertions were ported in items 5 and 7). `.gitattributes`: drop the `scripts/*.sh` line only if no `scripts/*.sh` remains, add the `.apogee/skills/**` pin with a comment. `ci.yml`: append `./cmd/release/...` to the `test-windows` `run:` line with a one-line reason, following commit `560768b3`.
**Regression guard.** Wrappers pass `$(VERSION)` through verbatim (empty means the verb's own default, per items 2 and 8). Reword ci.yml's comment that says "These four trees" to match the new list. The building.md lines listing the Windows vet/test trees are owned by item 14, not item 9.
**Files:** Makefile, release_scripts_test.go, scripts/release-scoop.sh, scripts/release-winget.sh, scripts/release-smoke.sh, .gitattributes, .github/workflows/ci.yml
**Read first:** Makefile — dist, check (GOOS=windows go vet line), DIST_DIR/DIST_VERSION/DIST_LDFLAGS/SHA256 (dist-only), CROSS_TARGETS (cross keeps it); release_scripts_test.go — requireExecutableInIndex (no outside users); .github/workflows/ci.yml — test-windows comment ("These four trees") and run line;
.gitattributes — scripts/*.sh block (check-pins.sh, test-shards.sh remain)
**Tests:** none new (wrappers); existing `cmd/release` suite.
**Acceptance:** `make -n dist release-status release-smoke` shows only `go run ./cmd/release …`; `make help | grep -c release-scoop` prints 0; `make actionlint`; `go vet .`; `git grep -n 'release-\(scoop\|winget\|smoke\)\.sh' -- ':!CHANGELOG.md' ':!**/archived/**' ':!docs/plans/**' ':!.beads/**'` prints nothing.
**Commit:** `build(release): make targets wrap cmd/release; retire the bash release scripts`

## 10. `release-notes-apogee`: coordinator and shared formats

**What:**
**Goal:** `.apogee/skills/release-notes-apogee/SKILL.md` is a host-agnostic, read-only coordinator that builds the evidence inventory since the last published release and writes the release notes, plus its shared files `prompts/_shared.md`, `prompts/_apogee.md`, `prompts/_inventory-format.md`, `prompts/_notes-format.md`.
**Approach (assumed at the header base):** Port and trim `~/.claude/skills/release-notes/SKILL.md` and its `_*.md`. Frontmatter: `id`, `displayName`, `summary`, `description`, `triggers` (apogee's format, `internal/skills/parse.go`). Sub-agents read prompts in place at `.apogee/skills/release-notes-apogee/prompts/<X>.md` (no SKILL_DIR search, no staging copy); dispatch names "whatever agent-spawning tool the host provides (`sub_agent`, `Agent`, `Task`, or similar)". Phase 1 = one shell call `go run ./cmd/release status --json` → `baseline.md` in the run dir `docs/skill-runs/release-notes-apogee/<date>/` (stop when baseline is missing); Phase 2 = scouts changelog/commits/surface in one batch; 3 merge; 4 verify contested rows; 5 notes; 6 trace-check (one re-render on GAPS). Optional argument `vX.Y.Z` names the output `<OS temp dir>/release-notes-apogee-vX.Y.Z.md` (else `…-<baseline>..HEAD.md`); the coordinator reads receipts only, modifies nothing outside the run dir and the output file, states the implied bump as a fact. `_apogee.md` holds recon's facts: CHANGELOG path and heading format, commit conventions, the user-surfaces table (README, `docs/manual/`, `CONTEXT.md`, `layout.md`, command/flag/config/tool registries, emitted strings), docs dir. `_inventory-format.md` drops the P- row; `_notes-format.md` unchanged in substance. Creating the repo's first `.apogee/` makes projectroot.Resolve answer the repo root for sessions started in subfolders of this repo — intended by ADR 0098 §2; no code change.
**Regression guard.** Phase 1 keeps `status --json`'s stdout whatever its exit code (`… > baseline.md; true` or equivalent) and stops only when `baseline.tag` is empty or the JSON does not parse — the gate is release-apogee's concern, not the notes skill's. The read-only statement carves out the one `git fetch` `status` makes (it moves `origin/main` and adds tags) and names the `gh` auth the baseline needs.
**Files:** .apogee/skills/release-notes-apogee/SKILL.md, .apogee/skills/release-notes-apogee/prompts/_shared.md, .apogee/skills/release-notes-apogee/prompts/_apogee.md, .apogee/skills/release-notes-apogee/prompts/_inventory-format.md, .apogee/skills/release-notes-apogee/prompts/_notes-format.md
**Read first:** internal/skills/parse.go — frontmatter, validate; internal/skills/load.go — sourceAnchors, projectSkillsBase; internal/projectroot/projectroot.go — Resolve; ~/.claude/skills/release-notes/SKILL.md — Step 0a/0b; .gitignore — /docs/skill-runs/
**Tests:** item 13's load test covers parsing.
**Acceptance:** `wc -l` of these five ≤ 380; `grep -rniE 'claude/skills|subagent_type|recon|plans\.md|P-[0-9]' .apogee/skills/release-notes-apogee` prints nothing.
**Commit:** `feat(skills): add the release-notes-apogee coordinator`

## 11. `release-notes-apogee`: the three blind scouts

Depends on item 10.

**What:**
**Goal:** `prompts/changelog-scout.md`, `prompts/commits-scout.md` and `prompts/surface-scout.md` exist, each blind to the other two, reading `baseline.md` and `_apogee.md` for the range and facts, writing CL-/C-/S- rows in `_inventory-format.md`'s shape and a ≤6-line receipt per `_shared.md`.
**Approach (assumed at the header base):** Port and trim the personal `changelog`, `commits` and `surface` prompts; drop every MAP/recon input; changelog reads only `CHANGELOG.md`'s `[Unreleased]` and any release headings above the baseline, runs no git; surface diffs `_apogee.md`'s surface paths `<baseline>..HEAD` and is the only evidence scout.
**Files:** .apogee/skills/release-notes-apogee/prompts/changelog-scout.md, .apogee/skills/release-notes-apogee/prompts/commits-scout.md, .apogee/skills/release-notes-apogee/prompts/surface-scout.md
**Read first:** ~/.claude/skills/release-notes/prompts/changelog.md — MAP inputs, Unreleased steps; ~/.claude/skills/release-notes/prompts/commits.md — MAP conventions; ~/.claude/skills/release-notes/prompts/surface.md — User surfaces table, Emitted strings; CHANGELOG.md — `## [Unreleased]`, `## [0.24.11] — 2026-10-08` heading shape
**Tests:** none (prose).
**Acceptance:** combined `wc -l` ≤ 220; `grep -niE 'MAP|recon' ` over the three prints nothing.
**Commit:** `feat(skills): add the release-notes-apogee scouts`

## 12. `release-notes-apogee`: merge, verify, notes, trace-check

Depends on item 11.

**What:**
**Goal:** `prompts/merge.md`, `prompts/verify.md`, `prompts/notes.md` and `prompts/trace-check.md` complete the pipeline: merge → R-rows tagged CONFIRMED/CLAIMED/UNCLAIMED/CONFLICT with a `CONTESTED:` receipt line; verify one contested row → CONFIRMED|DROP|REWRITE; notes → `notes.md` + `trace.md` in the notes format; trace-check → two-way bullet↔row check plus the leak grep, `RESULT=PASS|GAPS n`.
**Approach (assumed at the header base):** Port and trim the personal prompts; drop every PLANS/P- input. Total skill size after this item 600–800 lines.
**Files:** .apogee/skills/release-notes-apogee/prompts/merge.md, .apogee/skills/release-notes-apogee/prompts/verify.md, .apogee/skills/release-notes-apogee/prompts/notes.md, .apogee/skills/release-notes-apogee/prompts/trace-check.md
**Read first:** ~/.claude/skills/release-notes/prompts/merge.md — PLANS input; ~/.claude/skills/release-notes/prompts/verify.md — scout outputs list (PLANS); ~/.claude/skills/release-notes/prompts/trace-check.md — leak grep id list (C-nn/P-nn/S-nn); ~/.claude/skills/release-notes/prompts/_inventory-format.md — P-nn row; ~/.claude/skills/release-notes/prompts/notes.md
**Tests:** none (prose).
**Acceptance:** `cat .apogee/skills/release-notes-apogee/{SKILL.md,prompts/*.md} | wc -l` between 600 and 800; `grep -rn 'P-\|PLANS' .apogee/skills/release-notes-apogee` prints nothing.
**Commit:** `feat(skills): complete the release-notes-apogee pipeline`

## 13. `release-apogee` skill, Claude Code pointers and a load test

Depends on items 2, 4, 6, 12.

**What:**
**Goal:** `.apogee/skills/release-apogee/SKILL.md` cuts a release end to end; `.claude/skills/release-apogee/SKILL.md` and `.claude/skills/release-notes-apogee/SKILL.md` are committed one-file pointers; a Go test loads `.apogee/skills` as project skills and pins the ids `release-apogee` and `release-notes-apogee`.
**Approach (assumed at the header base):** release-apogee flow: run `go run ./cmd/release status --json`, stop naming the gate failures (no bypass) and report `setup-pending` channels; run the notes pipeline by reading `.apogee/skills/release-notes-apogee/SKILL.md` in place; show the notes and get approval; propose the version per the ratified rule and confirm with the host's user-question tool; `prepare vX.Y.Z`; copy notes to `<OS temp dir>/release-notes-apogee-vX.Y.Z.md`; `publish … --dry-run`, show it, confirm the one irreversible step, `publish`; on failure relay the table and re-run `publish` after the fix; abort before publish = `git reset --keep HEAD~2`; winget "submitted" ≠ published; never tag by hand. Pointer body: read `.apogee/skills/<id>/SKILL.md` and follow it exactly; frontmatter `name: <id>`, `description:`. `.claude/.gitignore` gains `!skills/` and `!skills/**`. Test in `internal/skills/` loads `Sources{ProjectRoot: <repo root>}` (with whatever flag enables project skills) and asserts exactly those two ids, non-empty bodies.
**Regression guard.** The test calls `skills.Load(skills.Sources{ProjectRoot: <abs repo root>})` with no Home, Workspace or flags — `.apogee/skills` is not flag-gated; `UseProjectSkills` gates only `<Workspace>/skills/` — and asserts exactly the two ids, non-empty Body and an empty `cat.Skipped()`. The ignore check is `git check-ignore -q --no-index …` (exit 1 expected): without `--no-index` any tracked path exits 1.
**Files:** .apogee/skills/release-apogee/SKILL.md, .claude/skills/release-apogee/SKILL.md, .claude/skills/release-notes-apogee/SKILL.md, .claude/.gitignore, internal/skills/repo_skills_test.go
**Read first:** internal/skills/load.go — Sources, Load, sourceAnchors, projectSkillsBase; internal/skills/parse.go — validate; internal/tui/skill_test.go — projectroot.Resolve + skills.Load pattern; .claude/.gitignore; ~/.claude/skills/cut-release/SKILL.md — delegate guard (`release-` not `release-notes-`)
**Tests:** `TestRepoReleaseSkillsLoad` (exactly the two ids, non-empty Body, empty `cat.Skipped()`).
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run TestRepoReleaseSkillsLoad ./internal/skills/`; `git check-ignore -q --no-index .claude/skills/release-apogee/SKILL.md` exits 1; `wc -l .apogee/skills/release-apogee/SKILL.md` ≤ 180.
**Commit:** `feat(skills): add the release-apogee skill and Claude Code pointers`

## 14. Rewrite the release runbook and amend ADR 0098 §5

Depends on items 9, 13.

**What:** Recast at the regression check (2026-10-11).
**Goal:** `docs/manual/building.md` §Releasing describes the shipped pipeline (run the `release-apogee` skill, or by hand `status` → `prepare` → `publish --notes-file` → re-run to converge; the CI gate; the one-time Windows setup kept, its make targets replaced by `publish`); its make table and build-requirements lines name no retired target or `zip`/`tar`/`sed`/`sha256sum` requirement; ADR 0098 §5 states the ratified peel rule and the widened CI rule (item 2); no non-archived doc names a retired target or script.
**Approach (assumed at the header base):** Rule: every non-historical mention of `release-scoop`, `release-winget`, `scripts/release-` or the POSIX dist tools is rewritten — find them with `grep -rnE 'release-(scoop|winget)|scripts/release-|sha256sum' --include='*.md' . | grep -vE 'CHANGELOG|archived|handoffs|docs/reviews'` (known at write time: `docs/manual/building.md`, `docs/design/test-drivers.md`). Accepted ADRs other than 0098 are history and stay. ADR 0098 §5 is edited in place (ratified the same day).
**Regression guard.** Add the rule "every site listing the Windows vet/test trees gains ./cmd/release/..." found with `` git grep -n 'internal/auditsplit`\|internal/auditsplit/\.\.\.\|four trees' -- ':!CHANGELOG.md' ':!**/archived/**' `` (known at write time: ci.yml:175/196, Makefile:349, building.md:35 make-check row and :235-236 "vets the four trees" text). The ADR 0098 §5 edit states the widened CI rule from item 2's decision. The retired-name rule runs as `git grep -nE 'release-(scoop|winget)|scripts/release-|sha256sum' -- '*.md' ':!CHANGELOG.md' ':!**/archived/**' ':!docs/handoffs/**' ':!docs/reviews/**' ':!docs/plans/**'`; building.md is swept with `` git grep -nE 'release-(scoop|winget)|scripts/release-|sha256sum|`zip`|BUCKET_(URL|REPO)|DRY_RUN|separate manual|four trees' docs/manual/building.md `` — the Versions-and-tags paragraph and the make-check row included.
**Files:** docs/manual/building.md, docs/design/test-drivers.md, docs/adr/0098-the-repo-owns-its-release-pipeline.md
**Read first:** docs/manual/building.md — make table (dist, check, release-smoke rows), Versions and tags paragraph, Releasing (steps 3–4, One-time setup), make check/Windows-tagged paragraph, build-requirements paragraph; docs/adr/0098-the-repo-owns-its-release-pipeline.md — Decision §5; docs/design/test-drivers.md — T-21 row, release smoke row (no retired name at base: nothing to change)
**Tests:** none (docs).
**Acceptance:** the retired-name `git grep` in the guard prints only `docs/adr/` lines; `grep -n 'peel' docs/adr/0098-the-repo-owns-its-release-pipeline.md` finds §5.
**Commit:** `docs(release): rewrite the runbook for cmd/release and the release skills`
