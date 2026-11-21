---
title: "Repository state is one fact from gitexec (architecture review 2026-11-21 #3)"
status: parked
type: task
priority: 3
labels: [security]
created: 2026-11-21T12:27:13Z
---

Found by docs/reviews/architecture-review-2026-11-21.html, verified 2026-11-21 at 171d210f.

Problem: gitexec reports a refusal only as text. The capture path fakes a failed result carrying the sentence (internal/gitexec/gitexec.go ~316, again internal/tools/git.go:222, :262); the data path returns a plain `errors.New(refusal)` (gitexec.go ~461). An incomplete probe gets a sentinel (`ErrIncomplete`); a refusal gets none. Callers sort the outcomes themselves: cmd/apogee/gitstatus_host.go:184-187 runs two string predicates, gitactions_host.go:212 checks again. `GitRefusedError` lives in internal/tui/gitstatus.go:36 but is built in cmd/apogee. There are two "inside a work tree?" probes (internal/agent/treesnapshot.go:195, gitstatus_host.go:175); treesnapshot cannot tell refused from no repository. The program-naming config-key catalogue sits in internal/security/shellwrites.go:993, is compiled by gitexec (:545), and gitexec repeats the merge-driver pattern (:710). Co-edits: 122331e7, 56b67508, c707c071 (gitexec + status host + tui/gitstatus + tui/watch); c55891ee, 8e9f23cf, b13c5c80, c707c071 (gitexec + security).

Solution: gitexec returns the repository state at a root as one value — work tree (with top level), no repository, refused (the standing sentence), incomplete (ask again) — and owns the key catalogue with its benign values and which keys can be neutralised. Model-facing git tools keep receiving a refusal as a failed outcome (the Capture doc's "no signature for a future git tool to forget to handle"). gitstatus_host_test and tui/gitstatus_test stop scripting refusal sentences; treesnapshot gains a testable refused case.

Must keep: ADR 0010 (gitexec stays a leaf and cannot import tui, so the refused type moves down into gitexec, never up), ADR 0012 amendments 0906 and 1118(c), ADR 0108 amendment 1118, ADR 0049.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
