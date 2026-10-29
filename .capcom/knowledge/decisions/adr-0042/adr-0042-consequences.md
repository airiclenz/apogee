---
title: ADR 0042 consequences
---

- **A tool that wants an external program owes three things**: a resolution seam that is a package
  var or injected dep (so a test can fake it), a named degraded result the model can act on, and a
  test of the absent path — `autofix_test.go`'s "gracefully absent" case and
  `TestRunTestsMissingRunnerProgramDegradesGracefully` are the shape. *(Amended 2026-10-29:
  `autofix_test.go` went with the autofix Mechanism in `eaa340de` (2026-09-03); the standing
  examples are `TestRunTestsMissingRunnerProgramDegradesGracefully`
  (`internal/tools/run_tests_test.go`) and `TestGit_GracefulWhenAbsent`
  (`internal/tools/git_test.go`).)*

  > **Note 2026-09-30 (architecture review 2026-09-30 #4).** For git, `go vet` and MCP stdio the
  > seam is now always the injected dep, never a package-level variable: `internal/gitexec` has no
  > package-level lookup or launcher left, and every git run goes through a `gitexec.Host` its
  > caller passes — the git tools' `execHost` (as `execHost.git()`), the Host the Agent builds its
  > tree snapshotter and secrets guard with (`gitexec.OS()` in production), the one the snapshot
  > store was opened with. `go vet` (diagnostics) launches through the tool's `execHost`, and an
  > MCP stdio server is env-scoped and torn down through the `mcp.Host` the composition root passes
  > to `ConnectWith`. A test fakes the field of the host it hands in. The rule itself is unchanged.

- **The policy costs capability, deliberately.** An `io/fs` walk is slower than ripgrep on a large
  tree, and in-process `go/format.Source` fixes less than `goimports`. Both are accepted: the fast
  path is still taken when the program is there, and the floor is a working agent everywhere.
  *(Amended 2026-10-29: only the search half of this trade remains, and it has no fast path — `grep`
  and `find_files` never detect ripgrep (decision 3). The `go/format.Source`-behind-`goimports` rung
  retired with autofix in `eaa340de` (2026-09-03); no non-test code imports `go/format` today.)*
- **`probe`'s host report is where "what is present here" is answered**
  (ADR 0021),
  not a start-up check — the free half already names the host's capabilities on demand.
- **The code's `§3a` citations now have a standing home.** They keep their wording (they cite the
  document that decided it, which is preserved under `docs/design/archived/`), but this ADR is the
  record a reader should land on, and new code cites this ADR rather than a section number.
- **A new direct module dependency is an ADR-shaped decision.** Not every one needs its own record,
  but the burden is on the addition: what it buys that the standard library does not, and what it
  costs the cross-build.
