---
title: Footer shows git branch, uncommitted files and ahead/behind counts
status: done
type: feature
priority: 3
plan: 2026-11-03 - 00 - footer-git-status-plan.md
created: 2026-11-03T08:11:32Z
closed: 2026-11-03T11:40:10Z
reason: Resolved by plan '2026-11-03 - 00 - footer-git-status-plan' (all 8 items), archived at 2649bdb1.
---

The footer's upstream slot states `host ✦ model ✦ effort ✦ workdir` (layout.md, "The footer's upstream slot"). It says nothing about the git state of the workdir. Add one segment after the workdir: the branch and three compact counts.

```
workstation ✦ qwen3.6-27B-Q4_K_S.gguf ✦ high ✦ ~/Repos/apogee ✦ main 5Δ 0↓ 4↑
```

- `Δ`: uncommitted files: modified, staged and untracked, with gitignored files left out. Each untracked file counts on its own, so a new folder counts every file in it.
- `↓`: commits on the upstream that the local branch does not have (incoming).
- `↑`: local commits not yet on the upstream (outgoing).
- Zero counts render muted and non-zero counts in the footer's normal text tone. `↓`/`↑` are left out when the branch has no upstream. The whole segment is left out outside a git workspace.

### Prior art

A prototype exists, it refreshes every second but never runs git on that path. A detached background job keeps a per-repo cache up to date. It runs `git status --porcelain=v2 --branch --untracked-files=all` at most every 5 s, and `git fetch --quiet --no-tags` at most every 5 min, so incoming commits can appear. It sets `GIT_OPTIONAL_LOCKS=0`, so it never takes `index.lock` from a foreground git. It also sets `GIT_TERMINAL_PROMPT=0` and `GCM_INTERACTIVE=never`, plus SSH `BatchMode=yes` when no `core.sshCommand` is set, so a fetch can never prompt. A single `git status --porcelain=v2 --branch` call gives both the file count and the `# branch.ab +A -B` ahead/behind line.

### Constraints to design against

- **Slow box** (AGENTS.md): no fixed deadline may fail a healthy run. The refresh waits on git off the render path, keeps the last good counts while it runs, and only backs off.
- **Driver boundary** (ADR 0031): the engine stays wire-silent. Polling git is the TUI Driver's business, not the engine's.
- **Background fetch touches the network and writes remote-tracking refs.** Decide whether it ships on, sits behind a config key, or follows the confinement/mode rung. The local `Δ`/`↑` counts and the `↓` against the last-fetched upstream need no network.
- The footer is a priority fit (see `status-line-priority-fit`). Decide where this segment sits in it, likely given up after the spend and before the workdir.

## Acceptance

- In a git workspace with an upstream, the footer shows `<branch> NΔ N↓ N↑` after the workdir, and the counts match `git status --porcelain=v2 --branch --untracked-files=all`
- Outside a git workspace the segment and its separator are gone; with no upstream only `<branch> NΔ` shows; on a detached HEAD the counts show without a branch name
- No git process runs on the render path: a frame never waits on git, and a slow or hung git or fetch on a slow box leaves the last counts in place without blocking the UI or failing anything
- Commits pulled in by the periodic fetch show in `↓` within one fetch interval, and the fetch never prompts for credentials
- On a narrow row the segment gives way by its place in the footer's priority fit, and layout.md's "The footer's upstream slot" describes it
