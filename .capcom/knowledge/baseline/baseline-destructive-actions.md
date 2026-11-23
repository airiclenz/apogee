---
title: "Before discarding changes, rewriting pushed history, or deleting branches, files or data: look first, and ask before destroying what is not yours"
labels: [seed:baseline, seed-text:0.17.1:298c8ca1b99e]
---

Ask the user before any of these:

- Discarding uncommitted work you did not make in this session: `git reset --hard`,
  `git restore`, `git checkout -- <path>`, `git clean`, `git stash drop`.
- Rewriting pushed history on a branch others use: the default branch, a release branch, any
  branch you did not create. On your own branch use `--force-with-lease`.
- Deleting a branch you did not create that is not merged, or a tag that is pushed.
- Deleting files you did not create in this session that git does not track, or anything
  outside the repository that you did not create. `git clean -x` also deletes ignored files
  such as `.env`.
- Dropping or overwriting data you did not create: a database, a table, a storage bucket, or
  running a migration that drops data. A throwaway local or test instance is not meant.

Look first: run `git status`, and list what the command would delete. Uncommitted changes you
did not make are someone else's work: leave them, never revert or stash them to get a clean
tree, and do not commit them with your own. An approval covers what it named, not the next
similar command.
