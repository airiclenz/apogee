---
id: commit-hygiene
displayName: Commit Hygiene
summary: Land one logical change per commit, with a message that says what changed and why, after the project's own checks have passed.
description: >-
  Use when committing, staging, splitting or rewording changes, writing a commit message,
  updating a changelog, or preparing a branch for review. Keeps one logical change per
  commit, spells the message in the conventional-commit shape the repository already uses,
  runs the project's own build, test and lint gates before the commit is made, and keeps
  version identifiers and release headings out of a commit nobody asked for.
triggers:
  - commit this
  - commit message
  - write a commit
  - git commit
  - stage these changes
  - split this commit
  - amend the commit
  - update the changelog
  - prepare a PR
  - conventional commits
  - squash these commits
---

# Commit Hygiene

A commit is a unit of explanation, not a save point. Someone reading `git log` a year
from now — often you — should be able to see what changed and why without opening the
diff, and to revert one thing without losing another.

## Read the repository's conventions first

Every rule below is a default that a project may override. Before your first commit in a
repository, look for what it already decided:

- The agent or contributor guide (`AGENTS.md`, `CLAUDE.md`, `CONTRIBUTING.md`) — branch
  policy, message format, required checks, forbidden trailers.
- `git log --oneline -20` — the real convention, whatever the docs say. Match the style
  you find: prefix scheme, capitalisation, scope names, ticket references.
- The changelog, if there is one, and the file that holds the version.

Where the repository is explicit, it wins over this skill. Where it is silent, the rest
of this file applies.

## One logical change per commit

- One commit does one thing: a fix, a feature, a rename, a dependency bump. If the
  message needs the word "and", you probably have two commits.
- A refactor and a behaviour change never share a commit. The refactor hides the change
  in a wall of moved lines, and neither can be reverted alone.
- Formatting churn goes in its own commit, so the next reader can skip it.
- Unrelated work that you noticed on the way is a separate commit, or a separate task.
- Split with `git add -p` when the working tree grew two changes at once. Do not bundle
  them because splitting is awkward.

## Stage deliberately

- Never stage the whole tree by reflex. Look at what is actually there:
  `git status --porcelain` for the list, `git diff --staged` for the content.
- Read your own staged diff before you commit. It is the cheapest review you will get,
  and it is where the stray debug print, the commented-out block and the local
  experiment turn up.
- Keep out: build output, editor and OS files, local configuration, anything generated.
  If it keeps reappearing, it belongs in `.gitignore`, not in the commit.
- Never commit a secret — a key, a token, a password, a private URL. A committed secret
  is leaked even after the next commit removes it: rotate it, do not just delete it.

## The message

Default shape, if the repository does not say otherwise — the conventional-commit form:

```
<type>(<scope>): <subject>

<body: why this change, and what it affects>
```

- Types: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `build`, `chore`. Scope is
  optional and names the area (`parser`, `auth`, `cli`) — use the scopes the log uses.
- Subject: imperative mood, lower case, no trailing period, about 50 characters and
  never past 72. "add retry to the payment client", not "added" or "adding".
- The subject says what changed. The body says WHY — the behaviour that was wrong, the
  constraint that forced the design, the thing you decided not to do. The diff already
  shows the what; nobody can recover the why.
- Wrap the body near 72 columns, and separate it from the subject with a blank line.
- Reference the issue or ticket where the project does (`(#1234)`, `Fixes #1234`).
- Do not add attribution or tool-advertisement trailers unless the project asks for
  them; some repositories strip or forbid them outright.

Avoid the empty messages: "fix bug", "update code", "changes", "wip". If the change is
genuinely too small to describe, it is too small to be its own commit.

## Run the project's checks before committing

The commit is the wrong place to discover a broken build.

- Run what the project runs: its test target, its formatter, its linter — a `make check`,
  an `npm test`, a `cargo test`, whatever the guide names. Do not invent a substitute.
- Run the tests that cover what you touched, and at least build the whole thing.
- If a check fails, fix it in this commit — not in a follow-up "fix the build" commit.
- If a pre-commit hook rejects the commit, read what it said. Never reach for
  `--no-verify` to get past it.
- A commit whose checks you did not run is a commit you cannot claim is green. Say so
  plainly rather than implying otherwise.

## Changelog and version discipline

- If the project keeps a changelog, add the entry in the same commit as the change,
  under its unreleased heading, in the voice the existing entries use.
- Write the entry for the user of the software, not for the reviewer of the diff: what
  they can now do, or what stopped going wrong.
- Internal refactors, test-only changes and typo fixes usually earn no entry. Do not pad
  the file.
- Never bump a version — a version file, a package manifest, a release heading, a tag —
  unless you were explicitly asked to. Propose the bump instead, and let the owner cut
  the release.

## Before you push

- Re-read `git log --oneline` for the commits you are about to push. The sequence should
  read as a story someone can follow.
- Rewrite history only while it is still yours: unpushed commits can be reordered,
  squashed or reworded freely; pushed ones are shared and stay as they are unless the
  project says otherwise.
- Push to the branch the project's policy names, and only when asked to push.
