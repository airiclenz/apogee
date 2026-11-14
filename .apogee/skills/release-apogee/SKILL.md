---
id: release-apogee
displayName: Release (apogee)
summary: Cut and publish an apogee release end to end — notes, version, prepare, publish, smoke.
description: >
  Cuts an apogee release: checks the release gate with `go run ./cmd/release
  status` — pushing unpushed work on main and waiting for its CI run to go
  green when that is all the gate lacks — drafts the notes with release-notes-apogee, proposes the version and
  has the owner confirm it, makes the two local release commits with `prepare`,
  then — after a dry run and one explicit confirmation — runs `publish`, which
  pushes, waits for the CI-made tag, creates the GitHub release and updates
  Homebrew, Scoop and winget. Optional argument vX.Y.Z proposes the version.
  Use for "cut a release", "publish a release", "ship vX.Y.Z". Only drafting
  the notes is release-notes-apogee.
triggers:
  - cut a release
  - publish a release
  - ship a release
  - release apogee
---

# Release apogee — cut and publish a release

## Mission

Turn everything shipped since the last published release into a published
release: notes the owner approved, a version the owner confirmed, and every
channel converged. The deterministic work belongs to `go run ./cmd/release`
(ADR 0098); this skill does the judgment and the asking.

The optional argument `vX.Y.Z` (a bare `X.Y.Z` reads the same) is a version
**proposal** — the owner still confirms it in step 4.

## Boundaries

- **One irreversible step: `publish`.** Everything before it is local and
  undone with `git reset --keep HEAD~2`; everything after it is fixed
  forward, never undone.
- **No bypass.** A blocked gate stops the run, except the two entries step
  1a clears by pushing and waiting for green CI. You never tag, create a
  GitHub release, or commit to the tap or the bucket by hand — `publish`
  does all of it. The release tag is made by CI, never by you or `publish`;
  the one tag `publish` makes is the v1 placeholder (step 5). The
  one push you make yourself is step 1a's, of work already committed on
  main; the two `prepare` commits are pushed by `publish`.
- **Ask with the host's user-question tool** (`ask_user`, `AskUserQuestion`,
  or similar) at every *Ask* below, and wait for the answer. No answer, or a
  host without the tool, means stop — never assume consent.
- Needs an authenticated `gh` (`gh auth status`) and a Go toolchain; winget
  also needs `komac`. Shell snippets assume fail-fast hosts (`set -e`,
  `pipefail`): a command that may match nothing carries `|| true` — keep it.

## 1 — Status and resume check (one shell call)

```bash
T="$(git rev-parse --show-toplevel)"; cd "$T"
TMP="${TMPDIR:-${TEMP:-/tmp}}"; S="$TMP/release-apogee-status.json"
go run ./cmd/release status --json > "$S" || true
echo "TMP=$TMP"; echo "VERSION=$(cat VERSION)"; git log -2 --format=%s
```

Read `$S` yourself (it is small). Its keys: `version`, `highestTag`,
`gate.{ok,failures}`, `baseline.{tag,published,range,commits}`,
`steps[].{name,state,detail}`. A missing or unparsable file → stop and relay
the command's error (likely `gh` unauthenticated or no network).

**Resume.** When the two subjects read `chore(version): bump version to vX`
then `chore(release): cut vX`, vX is already prepared — whether or not it was
pushed. Say so, take vX as the confirmed version, and go to step 6: the notes
file `<TMP>/release-notes-apogee-vX.md` is reused when it exists (show it and
*Ask* approval as in step 3), else step 2 runs with the argument vX first.

**Gate.** `gate.ok` false and no resume → sort the `gate.failures` entries.
Two kinds are cleared here, by step 1a: `gate commit … is not on origin/main:
push it first` and `ci.yml has no successful run for …`. Any other entry (a
dirty tree, `origin/main has commits HEAD does not: pull first`, a check that
could not run) → **stop**: list every entry verbatim. Fixing those is the
owner's work; offer nothing that skips them. When every entry is one of the
two kinds, run step 1a. Once the gate is ok, print one ledger line: baseline
tag, publish date, commit count, highest tag.

## 1a — Push, then wait for green CI

Only on a blocked gate whose entries are all of step 1's two kinds, and never
on a resume: the two `prepare` commits are pushed by `publish` alone. No *Ask*
— the owner asked for the release, and this pushes only work already
committed on main.

```bash
test "$(git rev-parse --abbrev-ref HEAD)" = main || { echo "NOT_ON_MAIN"; exit 1; }
git log --oneline origin/main..HEAD
git push origin main
echo "HEAD=$(git rev-parse HEAD)"
```

`NOT_ON_MAIN` → stop and say so. Otherwise name the commits pushed (none
when HEAD was already on origin/main and only its CI was missing), then run
step 1's block again first. The gate peels trailing commits whose every
path `ci.yml`'s push filter skips, so a push of prose alone (issues, plans)
can clear it on the green run already below it: when the gate is now ok,
carry on from step 1's ledger line and skip the wait. Otherwise find the
`ci.yml` run for HEAD and wait for it to finish — GitHub can take a while to
list a new run, so the loop waits up to 10 minutes for it to appear, then
the watch runs as long as CI does. Give the shell call a long timeout, or
run it in the background and wait for it:

```bash
H="HEAD_VALUE"; ID=""
for i in $(seq 1 40); do
  ID="$(gh run list --workflow ci.yml --commit "$H" --limit 1 --json databaseId --jq '.[0].databaseId // empty' || true)"
  [ -n "$ID" ] && break
  sleep 15
done
echo "RUN=${ID:-none}"
[ -z "$ID" ] || { gh run watch "$ID" --exit-status --interval 30 > /dev/null && echo "CI=green" || echo "CI=red"; }
```

- `RUN=none` → **stop**. No CI run exists for HEAD. The usual cause is a
  push that touched only paths `ci.yml` filters out (`.capcom/**`,
  `docs/plans/**`, `CHANGELOG.md`, …): CI never runs on it, and the gate
  peels such commits, so a gate still blocked after the re-run means the
  commit below them has no green run either. Say that, and list the gate's
  failures verbatim.
- `CI=red` → **stop**. Relay the failed jobs with
  `gh run view <RUN> --log-failed | tail -n 60 || true`. Red CI blocks the
  release, flaky or not; never re-run a job to get past it.
- `CI=green` → run step 1's block again. The gate must now be ok; any entry
  still listed → **stop** and list them verbatim (never a second push).

## 2 — Draft the notes

Read `.apogee/skills/release-notes-apogee/SKILL.md` in place and follow it
exactly as its coordinator, with **no** version argument (the version is
chosen from its inventory). Its output is `NOTES_SRC` =
`<TMP>/release-notes-apogee-<baseline tag>..HEAD.md`; its summary states the
implied bump. A stop in that skill stops this one.

## 3 — Approve the notes

The notes are already shown in full. *Ask*: approve, request changes, or
abort. On changes, edit `NOTES_SRC` exactly as asked, show the changed lines,
and *Ask* again. Abort → stop; nothing has been written to the repository.

## 4 — Propose and confirm the version

From `highestTag` `vX.M.P` (the burned versions v1.1.0, v1.7.0, v1.8.0 and
v1.8.1 — `internal/update`'s `RetractedVersions` — never count):

- **minor** `vX.(M+1).0` when the notes carry a Breaking section or the
  inventory is feature-heavy (new features are the bulk of the rows);
- otherwise **patch** `vX.M.(P+1)` — also the fallback when the inventory
  gives no clear answer.

**Never propose a burned version.** A proposal that is burned skips past it:
a minor bump takes the next minor whose `.0` is not burned (v1.0.x → v1.2.0,
v1.6.x → v1.9.0), a patch the next unburned patch. Never propose a major bump
yourself: v1.0.0 is the owner's choice. When the owner picks it, name the
**Cutting v1.0.0** runbook in `docs/manual/building.md` §Releasing — the 0.x
bridge release published first, `git fetch --prune --prune-tags` before
`prepare`, and `publish`'s `placeholder` step — and *Ask* again to confirm. A
`highestTag` of v1.x while no v1.x release has been published means local
tags of the deleted v1.x series: stop and ask the owner to run
`git fetch --prune --prune-tags`.

An argument version replaces the proposal. *Ask* with the proposal first and
the other size second, plus a free-text choice; the owner's answer is final.
A version not strictly above `highestTag`, or a burned one → say `prepare`
will refuse it (and CI would not tag it) and *Ask* again.

## 5 — Probe the channels and prepare

```bash
go run ./cmd/release status vX.Y.Z --json || true
go run ./cmd/release prepare vX.Y.Z
git log -2 --format=%s
```

From the status JSON, name every `setup-pending` step (`publish` skips it
and still succeeds; a later re-run converges it once the owner's one-time
setup — e.g. the first `komac new` — is done) and every `unknown` one. The
`placeholder` step, between `release` and `homebrew`, is `pending` only for a
release from v1.0.0 up to the placeholder (v1.8.1) while origin lacks the
placeholder tag — v1.0.0 in practice — and reads `done` with
`not needed: …` for every other version. When it is pending, say so:
`publish` will tag the placeholder on the release's commit, so
`go install …@latest` resolves to the release.
`prepare` makes exactly two **local** commits and pushes nothing; a refusal
writes nothing — relay it and stop.

## 6 — Write the release notes file (one shell call)

The notes' title still reads `unreleased`; the published copy names the
version. Substitute `NOTES_SRC_VALUE`, `TMP_VALUE` and the version:

```bash
NF="TMP_VALUE/release-notes-apogee-vX.Y.Z.md"
{ printf '# apogee — %s — %s\n' "vX.Y.Z" "$(date +%F)"; tail -n +2 "NOTES_SRC_VALUE"; } > "$NF.tmp" &&
mv "$NF.tmp" "$NF"; head -n 3 "$NF"; echo "NF=$NF"
```

Skip it on a resume that reuses `NF`; the temp file guards `NOTES_SRC` = `NF`.

## 7 — Dry run, then the one confirmation

```bash
go run ./cmd/release publish vX.Y.Z --notes-file "NF_VALUE" --dry-run
```

Show its output in full. Exit 1 means the gate is blocked or VERSION at HEAD
does not read vX.Y.Z while the tag is pending — relay the reasons and stop
(see *Abort*). Otherwise *Ask* exactly once: "Publish vX.Y.Z? This pushes
main, waits for CI to tag it, creates the GitHub release and updates
Homebrew, Scoop and winget. It cannot be undone." — options *publish* and
*abort*. When the dry run shows the `placeholder` step pending, the question
also names it: "…, creates the GitHub release, tags the placeholder vP on its
commit and updates Homebrew, Scoop and winget. …", with vP the placeholder the
dry run names. Anything but *publish* is abort.

## 8 — Publish

```bash
go run ./cmd/release publish vX.Y.Z --notes-file "NF_VALUE"
```

It never prompts and can wait up to `--tag-timeout` (10m) for the CI-made
tag: give the shell call a long timeout, or run it in the background and wait
for it. It prints a step table and exits 1 when a step fails.

**On failure:** relay the table verbatim and name the failed step and its
detail. Once the cause is fixed (by the owner, or by you when the fix is
outside the release — never a hand-made tag, release or channel commit),
re-run the **same** command: `publish` converges, skipping every step its
probe reports done. A re-run needs no new confirmation for the same version.

## Abort

Before `publish` ran: when `git log -2 --format=%s` shows the two `prepare`
commits for vX.Y.Z, undo them with `git reset --keep HEAD~2` and confirm
`VERSION` reads the old version. Never reset anything else. After `publish`
has pushed, nothing is undone: a broken release is fixed with a new version.

## Report

End with the final step table and, in plain words:

- the version, the GitHub release URL and the notes file path;
- each channel's outcome — **winget "submitted" is not published**: the
  pull request to winget-pkgs merges later, outside this run;
- every `setup-pending` channel, and that re-running `publish vX.Y.Z` after
  its setup converges it;
- the smoke result `publish` ran last; a SKIP is not a pass — say which.

## Hard rules

- Never tag, release or update a channel by hand; `publish` alone does.
  Push only in step 1a, only main's committed work, and only once.
- Never bypass the gate, nor switch versions to get past a failed `publish`.
  Red CI stops the run; only green CI lets step 1a continue.
- The owner confirms notes, version and publish — each via the question tool.
