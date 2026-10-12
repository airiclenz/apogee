---
title: ADR 0042 context
---

Apogee is one binary. A user builds it from source or unpacks a release archive, runs it in a
repository, and expects an agent — not an install list. The machines it has to work on are not
uniform: a developer's laptop with a full toolchain, a bare container with neither `git` nor a
Python, a locked-down work machine where installing anything is a ticket. A coding agent that
refuses to start, or that silently loses half its tool surface with no explanation, is worse on
those hosts than one that says what it cannot do and keeps going.

The policy that answers this was decided at plan time and recorded in `technical-design.md` §3a — a
section of a component-narration document that is now archived
(`docs/design/archived/technical-design.md`). The code has been citing it by section number ever since: `internal/tools/git.go:21,76`,
`internal/tools/python_exec.go:34,52,89`, `internal/tools/run_tests.go:234,365`,
`internal/tools/grep.go:85`, and `internal/mechanisms/autofix.go`'s standing requirement #2, whose
`autofix_test.go` pins the "gracefully absent" path. Those citations name a live constraint on
every tool and Mechanism written since, so it is recorded here as a decision of its own rather than
archived along with the document that happened to hold it.
