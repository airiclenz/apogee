---
title: ADR 0098 decision
---

1. **The release process is committed to the repo and needs nothing outside it.** A clone, the Go
   toolchain, `git`, `gh` and (for winget) `komac` are enough to cut a release from Linux, macOS
   or Windows.

2. **Agents do the judgment work through two committed, host-agnostic skills.**
   - `release-notes-apogee` is read-only. It builds the evidence inventory of what shipped since
     the last published release and writes the release notes from it. It is useful on its own
     ("what shipped?").
   - `release-apogee` is the cut. It runs that same notes pipeline by reading
     `release-notes-apogee`'s files in place, proposes the version, gets the owner's confirmation,
     and hands publishing to code.

   Both skills are canonical under `<Project root>/.apogee/skills/`, so apogee loads them as
   project skills. Claude Code reaches each one through a one-file pointer,
   `.claude/skills/<id>/SKILL.md`, which tells the host to read the canonical file and follow it.
   The pointer is a file, not a symlink, because a Windows checkout does not reliably keep
   symlinks. The skills name only generic host capabilities ("the host's sub-agent tool"), so
   either host can run them. They are repo-maintenance skills: they are **not** embedded in the
   binary (`internal/skills/shipped/`), because people who install apogee never release it. Their
   ids carry `-apogee` so they never collide with the same-named skills in a personal library,
   which would otherwise win the collision
   (ADR 0032).

3. **Code does the deterministic work through a Go maintainer program, `cmd/release`.** It runs
   as `go run ./cmd/release <verb>`. Like `cmd/stubllm`, it is never a release asset. Its verbs:
   - `status [vX.Y.Z]` is read-only. It runs the preflight and prints, for the given version,
     which publish steps are done and which are pending.
   - `prepare vX.Y.Z` makes two **local** commits: the changelog rollup, then the `VERSION`
     bump. Nothing is pushed, so an aborted cut is undone with a reset.
   - `publish vX.Y.Z --notes-file <path>` is the only verb that changes anything outside the
     machine. It pushes, waits for the CI tag, builds the archives, creates the GitHub release
     with `--verify-tag`, updates the tap formula, publishes Scoop and winget, then smoke-tests.
     `--dry-run` prints the plan instead.

   The Go standard library (`archive/tar`, `archive/zip`, `crypto/sha256`) replaces `zip`, `tar`
   and `sha256sum`. `make dist` becomes a thin wrapper around a `dist` verb, and `make
   release-status` and `make release-smoke` wrap theirs. The three bash scripts are deleted, along
   with `make release-scoop` and `make release-winget`, because `publish` covers both. GitHub is reached through the `gh` CLI, which already holds the owner's
   auth and runs on every OS. The notes reach the code as a file outside the repo, through
   `--notes-file`.

4. **`publish` converges.** Before each step it asks the remote whether that step is already
   done, and skips it if so: the tag exists; the release exists with all seven assets; the
   formula already pins the version; the bucket manifest is already at the version; a winget pull
   request for the version is already open or merged. A half-finished release is resumed by
   running `publish` again. There is no local state file, because the remote is the truth, and a
   second run cannot duplicate a commit or a pull request. `status` reports these same probes.

5. **The gate is green CI on the gated commit, not a local `make check`.** The gate commit G is
   `HEAD` with its trailing first-parent commits that touch only `CHANGELOG.md` and/or `VERSION`
   peeled off; the peel stops at any commit whose parent count is not 1, so a merge is never
   peeled. `status` refuses to clear a cut unless the tree is clean, G is an ancestor of or equal
   to `origin/main`, `origin/main` is an ancestor of or equal to `HEAD`, and `ci.yml` has a
   successful run for G or for any peeled commit that `origin/main` contains. The peeled commits
   count because a push runs CI on its head alone: a feature pushed with its micro-bump leaves
   the green run on the bump, not on the feature. CI runs the full check on a clean machine,
   Windows leg included, and gives the same answer whichever OS cuts the release. The two
   `prepare` commits, like every micro-bump the peel walks past, touch only `CHANGELOG.md` and
   `VERSION`, so the commit that was gated is, in effect, the commit that ships. The gate has no
   bypass: red CI, flaky or not, blocks the release until it is fixed.

6. **Package manifests are rendered whole from templates in this repo.** The Homebrew formula
   in `airiclenz/homebrew-tap` and the Scoop manifest in `airiclenz/scoop-bucket` are generated
   output. `cmd/release` renders each one from a template kept beside it, filling in the version
   and the per-platform hashes from `SHA256SUMS`, and the rendered file says it is generated. The
   formula's description, caveats and test therefore change in the same commit as the features
   they describe. Hand edits in the tap or bucket are overwritten at the next release. Both repos
   take a direct commit (`apogee X.Y.Z`) and a push, with no pull request.

7. **Releases are cut on the owner's machine; CI cuts are deliberately not yet.** `publish`
   never prompts. Every input is a flag or an environment variable, stdin is never read, and a
   failure exits non-zero after printing the `status` table. A later workflow could therefore run
   it unchanged. What stands in the way is outside the program: the CI tag is made with
   `GITHUB_TOKEN` and cannot trigger another workflow, the tap, bucket and winget pushes need a
   PAT, and the notes come from an agent. Code signing (`apogee-3p1`) is the likeliest reason to
   revisit this.

8. **A channel whose one-time setup is pending is skipped, not failed.** `status` reports the
   pending setup. `publish` names the skip and still succeeds. Once the setup lands (for winget,
   once the first `komac new` pull request has merged), running `publish` again converges the
   missing channel.

9. **The owner's personal `/cut-release` and `/release-notes` stand aside.** When they find a
   workspace that ships its own release skill, they stop and name it. That guard lives in the
   personal skill library, not in this repo.
