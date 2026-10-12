# publish checks archives on a release without SHA256SUMS — plan

**Goal:** when `publish` resumes a GitHub release that exists but carries no `SHA256SUMS`, every archive already uploaded is checked against the rebuild before anything is uploaded, so a rebuild never mixes with archives it does not match.
**Date:** 2026-10-12
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 921c0235
**Closes:** publish-unsummed-archives

**Sources:**
- `.capcom/issues/publish-unsummed-archives.md`
- `capcom know show "ADR 0098"` — the repo owns its release pipeline (§4 publish converges from remote state)
- `docs/plans/archived/2026-10-11 - 00 - release-pipeline-in-repo-plan.md` — item 6 (the deferral)

**Ratified design calls (owner, 2026-10-12):**
- **Mechanism:** read each uploaded asset's GitHub `digest` (`sha256:<hex>`) from `gh api repos/airiclenz/apogee/releases/tags/vX.Y.Z`; an asset whose digest is absent or empty is downloaded into the run's build directory and hashed locally. `gh release view --json` (gh 2.46) does not carry the digest.
- **Scope:** only the no-`SHA256SUMS` resume path gains the check; the existing `checkPublishedSums` path, used when `SHA256SUMS` is uploaded, is unchanged.

**Standing requirements:**
- skills: coding-standards
- This box: run tests only narrowed to one package, `GOMEMLIMIT=2GiB`, never two runs at once.

**Out of scope:**
- Byte-checking archives when `SHA256SUMS` is present.
- `status` probes and the `smoke` verb.
- Any change to `docs/manual/building.md` (it does not describe the resume check).

**Regression check (2026-10-12, 921c0235):**
- 1: guard folded (writer decision) — a draft release's 404 from the tags endpoint, or an archive absent from its assets, takes the `gh release download` fallback; hash with `fileSHA256`.

## 1. publish checks uploaded archives against the rebuild when the release has no SHA256SUMS

**What:** Fixes the gap deferred from the release-pipeline plan's item 6: `convergeRelease` uploads its own `SHA256SUMS` beside archives it never compared.
**Goal:** a `publish` run that finds the release existing without `SHA256SUMS` compares each already-uploaded archive's SHA-256 (GitHub digest, else a downloaded copy hashed locally) with the rebuilt `SHA256SUMS` line for it; any mismatch fails the run before any `gh release upload`, naming each mismatched archive and `gh release delete-asset vX.Y.Z <name>`; a full match uploads only the missing assets, `SHA256SUMS` included.
**Approach (assumed at the header base):** In `cmd/release/publish.go`, `convergeRelease` gains an `else` branch beside its `slices.Contains(uploaded, checksumsFile)` check calling a new `checkUploadedArchives(ctx, run, outDir, uploaded)`. It runs `gh api repos/<apogeeRepo>/releases/tags/<tag>` through `run.commands` with `nonInteractiveEnv`, parses `assets[].name` / `assets[].digest`, and for each name in `releaseAssetNames` other than `checksumsFile` that is in `uploaded`: strips the `sha256:` prefix and compares, lower-cased, with `checksumFor(rebuilt, name)`; when the digest is absent, empty or not `sha256:`, runs `gh release download <tag> --pattern <name> --dir <run.buildDir>/published` and hashes the file with `fileSHA256` (`cmd/release/dist.go`). The mismatch error mirrors `checkPublishedSums`'s wording but names only archives (there is no `SHA256SUMS` to delete). Update `convergeRelease`'s doc comment, which today says a release without `SHA256SUMS` "has nothing to check against". Downloads never land in the checkout.
**Regression guard.** A failed `gh release create` leaves a draft release, which `gh release view` finds but `gh api repos/airiclenz/apogee/releases/tags/vX.Y.Z` answers 404 — so a 404 from the tags endpoint, and any uploaded archive absent from its assets, is treated as "no digest" and takes the `gh release download --pattern <name> --dir <run.buildDir>/published` fallback (which finds drafts); the run never fails on that 404 alone. Hash downloaded files with the existing `fileSHA256` (cmd/release/dist.go), not a new crypto/sha256 helper. Add a test row where the api answers `ghNotFound` (cmd/release/probes_test.go) that downloads, matches, and uploads only SHA256SUMS.
**Files:** cmd/release/publish.go, cmd/release/publish_test.go
**Read first:** cmd/release/publish.go — convergeRelease, checkPublishedSums; cmd/release/probes.go — releaseAssetNames;
cmd/release/render.go — checksumFor; cmd/release/dist.go — fileSHA256; cmd/release/publish_test.go — publishFake.respond, TestPublishPartialReleaseUploadsOnlyTheMissingAssets;
cmd/release/probes_test.go — ghNotFound
**Tests:** extend `publishFake` (`cmd/release/publish_test.go`) to answer `gh api …/releases/tags/<tag>` with per-asset digests and `gh release download --pattern <archive> --dir` by writing bytes. Cases: the existing `"SHA256SUMS missing"` row of `TestPublishPartialReleaseUploadsOnlyTheMissingAssets` now expects the digest read and still uploads only `SHA256SUMS`; a digest mismatch on one archive fails before any upload and names it plus `gh release delete-asset v0.24.11`, and does not name `SHA256SUMS` as an asset to delete; an asset with no digest is downloaded (argv pinned, `--dir` outside the checkout) and a matching download passes; the api answering `ghNotFound` (a draft release) downloads every uploaded archive, matches, and uploads only `SHA256SUMS`; the `"two archives missing"` row (SUMS present) issues no `gh api …/releases/tags` call.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/release/`; `go vet ./cmd/release/`; `go run ./cmd/release publish v0.24.11 --notes-file README.md --dry-run` exits as before and `git status --porcelain` is unchanged.
**Commit:** `fix(release): publish checks uploaded archives when a release has no SHA256SUMS`
