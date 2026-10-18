---
title: A project file inside an argv element is not pinned
status: open
type: bug
priority: 2
created: 2026-10-18T18:44:05Z
---

An adopted repo entry's argv element is pinned whole (ADR 0104 decision 4; `internal/config/execpin.go:172` `fileCandidates`): only an element that is itself an in-root file path gets a content digest. An `env:` value goes through `envValueCandidates` (`internal/config/execpin.go:189`), which also tries each word, the part after a word's `=`, and each path-list element. So `args: ["--require=./hook.js"]` on a repo MCP server (or the same in a reaction's `run:`) does not pin `hook.js`, and the file can change after adoption without re-proposing the entry, while the same text in an `env:` value does pin it.

Deferred out of plan `2026-10-18 - 00 - project-config-exec-keys-plan`.

## Acceptance

- A decision is recorded (ADR 0104 amendment) on whether argv elements get the same in-root file search as `env:` values.
- If they do: an argv element such as `--require=./hook.js` pins `hook.js`, its change re-proposes the entry and refuses the launch/spawn, and a test in `internal/config/execpin_test.go` pins it; the manual's pin description (`docs/manual/configuration.md`, "Every project file the entry names") follows.
