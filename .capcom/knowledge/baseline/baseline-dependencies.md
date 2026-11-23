---
title: "Before adding or updating a dependency: check the name, the version and the lockfile"
labels: [seed:baseline, seed-text:0.17.1:62335e391670]
---

- Check that the package exists under exactly that name in the registry or source this
  repository uses, and is the project you mean. A guessed or misspelled name can be a hostile
  package.
- Give a version, in the form the manifest already uses for its other dependencies. Not
  `latest`, a moving branch or an unversioned URL where a version can be named.
- Where the repository tracks a lockfile, commit it together with the manifest. Never delete
  or regenerate a whole lockfile to get past an error: update only the dependency concerned.
- Name the licence of a new dependency when you report the change. Ask the user when it has
  none, or is copyleft (GPL, AGPL) and this repository is not under that licence.
