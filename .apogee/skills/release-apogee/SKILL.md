---
id: release-apogee
displayName: Release (apogee)
summary: Cut and publish an apogee release end to end — notes, version, prepare, publish, smoke.
description: >
  Cuts an apogee release: checks the release gate with `go run ./cmd/release
  status`, drafts the notes with release-notes-apogee, proposes the version and
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
- **No bypass.** A blocked gate stops the run. You never tag, push, create a
  GitHub release, or commit to the tap or the bucket by hand — `publish`
  does all of it, and the tag is made by CI, never by you or `publish`.
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

**Gate.** `gate.ok` false and no resume → **stop**: list every
`gate.failures` entry verbatim. Fixing them (a dirty tree, an unpushed or
diverged HEAD, a red or missing `ci.yml` run) is the owner's work; offer
nothing that skips them. Otherwise print one ledger line: baseline tag,
publish date, commit count, highest tag.

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

From `highestTag` `v0.M.P` (the retracted `v1.x` tags never count):

- **minor** `v0.(M+1).0` when the notes carry a Breaking section or the
  inventory is feature-heavy (new features are the bulk of the rows);
- otherwise **patch** `v0.M.(P+1)` — also the fallback when the inventory
  gives no clear answer.

An argument version replaces the proposal. *Ask* with the proposal first and
the other size second, plus a free-text choice; the owner's answer is final.
A version not strictly above `highestTag` → say `prepare` will refuse it and
*Ask* again.

## 5 — Probe the channels and prepare

```bash
go run ./cmd/release status vX.Y.Z --json || true
go run ./cmd/release prepare vX.Y.Z
git log -2 --format=%s
```

From the status JSON, name every `setup-pending` step (`publish` skips it
and still succeeds; a later re-run converges it once the owner's one-time
setup — e.g. the first `komac new` — is done) and every `unknown` one.
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
*abort*. Anything but *publish* is abort.

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

- Never tag, push, release or update a channel by hand; `publish` alone does.
- Never bypass the gate, nor switch versions to get past a failed `publish`.
- The owner confirms notes, version and publish — each via the question tool.
