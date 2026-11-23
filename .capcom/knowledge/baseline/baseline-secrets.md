---
title: "Before committing or writing an Issue, Log entry or Note: no secrets in what git tracks"
labels: [seed:baseline, seed-text:0.17.1:92fb654fbd39]
---

Never write a secret in plain text into a file git tracks: a password, API key, token,
private key, or a connection string that holds one. Issue bodies, Log entries and Notes are
committed too, and stay in history.

- Read secrets from the environment, a file git ignores, or the secret store the repository
  uses. Commit a template with placeholder values instead.
- `git add -A` and `git add .` also stage untracked files such as `.env`. Read
  `git diff --staged` before each commit, including test fixtures, logs and pasted command
  output.
- Do not print a secret to look at it (`cat .env`, `env`): what you print leaves the machine
  with the session.
- When you quote output in an Issue or Note, replace the secret with `<redacted>`.
- A secret in a commit that is not pushed: remove it from the commit before pushing.
- A secret that was pushed is leaked. Tell the user so it is rotated; removing it from
  history is not enough. Do not rewrite pushed history yourself to hide it.

Do not copy personal data of real people (customer records, production rows, addresses from
logs) into fixtures, examples, Issues or Notes: use made-up data.
