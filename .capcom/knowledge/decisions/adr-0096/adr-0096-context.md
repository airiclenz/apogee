---
title: ADR 0096 context
---

apogee has one config file, `~/.apogee/config.yaml`, and every approval it remembers lives in process
memory: ask-before re-asks every command on every relaunch (bead `apogee-zlg`). Rival agents keep a
settings folder in the repo (`.claude/`, `.opencode/`, `.crush/`), commit it if they like, and put
a one-time "do you trust this folder?" prompt in front of it. The owner wants the repo as the home
for project settings — so they can be committed — and asked for the design on 2026-10-10 over
`apogee-089`, `apogee-zlg` and the terminal command-allowlist slice of `apogee-9d8`.

Two facts make the rivals' shape unsafe here as it stands:

- **The model writes the workspace.** In `allow-edits` and `auto` a tool edits workspace files without
  a look. A live grant file in the workspace is a file the model can grant itself through. File
  tools can be refused there, but shell commands cannot be fenced completely — the shell reading is a
  footgun-guard, not a boundary (ADR 0012) — so `python -c "open('.apogee/config.yaml','w')…"` gets past
  any refusal.
- **Folder trust is one yes for every later state of the folder.** A `git pull` that adds `allow: curl`
  after the user trusted the folder is never shown to anyone. ADR 0076 D10 already ratified the
  stronger shape — a repo entry that can act is *proposed* until adopted, adoption pins the entry,
  an edit re-proposes — but A2 pulled it from stage 2 because the pieces did not exist.
