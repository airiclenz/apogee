---
title: Live Auto-confined deliverable run on Windows
status: done
type: task
priority: 3
labels: [owner-run, windows]
aliases: [apogee-2uh.1]
created: 2026-09-03T18:40:50Z
closed: 2026-11-17T09:52:58Z
reason: "Owner live run on Windows (v0.35.3): a write outside the repo, both through a file tool and through a shell command, was refused under confinement."
---

- **Live Auto-confined deliverable run on Windows** — the ADR 0020 backend is proven natively
  (escape battery + the real `Terminal` tool under `platform.NewConfiner()`); an end-to-end
  deliverable session under Auto is what remains, if an LLM endpoint is reachable from that
  machine.


## Owner-run steps (on Windows, about 30–45 min plus set-up)

1. Install Go at `go.mod`'s version, clone, `go build ./cmd/apogee`. Make sure a model
   endpoint is reachable from the machine (`docs/manual/configuration.md`).
2. In a scratch project, start apogee, `/mode auto`, and ask for a small deliverable: create a
   file, then run a build or test command.
3. Confirm it completes under confinement with no approval prompt for in-project work, and that
   a write outside the project is refused.

**Close when:** the session finishes the deliverable and the outside write is refused.
