---
title: ADR 0096 considered options
---

- **Rules only under `~/.apogee`, nothing in the repo.** Simplest and needs no pin, but nothing can
  be shared or committed, which is what the owner asked for.
- **A live repo file behind a one-time folder-trust prompt** (the rivals' shape). Rejected for the two
  facts above: the model can write the file, and trust never re-asks after a pull.
- **Exact-command replay** (persist today's session digest). Safest, but `go test -run TestY` would
  re-ask; it does not remove the annoyance the bead names.
- **Git remote URL as the project key.** The repo controls its own remote, so a hostile clone could
  claim another repo's adoptions.
