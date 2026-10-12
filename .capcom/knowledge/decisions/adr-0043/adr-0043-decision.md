---
title: ADR 0043 decision
---

**1. A coordinator file splits by concern cluster, and keeps only coordination.**

"One primary type per file" is a floor, not a ceiling: it never excuses a coordinator file that
absorbs every new method written for its type. When a central model, controller or app object grows
a cluster of related behaviour — one with its own state machine, its own vocabulary, its own tests —
that cluster moves to its own file in the **same package**, named for the concern
(`sessionsave.go`, `approval.go`, `commandrun.go`), and the coordinator file keeps the type, its
fields, and the dispatch that routes into the clusters.

For `internal/tui/model.go` this lifts exactly three clusters — the session-save write queue,
approval handling (keys and prompt rendering both), and command running/refusal. The `Model` struct
and its fields stay put: the fields a cluster owns (`writeBusy`, `pendingWrites`, `pending`,
`approvalSel`, …) are the coordinator's state, and splitting them across files would buy nothing and
cost the one-place reading of what a `Model` is.

ADR 0011 binds the mechanics: the
Model is copied by value on every `Update`, so a same-package file move is always safe and anything
that introduces a held-by-value no-copy type is not. `TestModelNoBuilderByValue` stays the guard.

**2. A composition root splits by subsystem seam, and says so at the top of the file.**

Wiring files grow by accretion because every new subsystem needs one more constructor, and no
individual addition is ever the one that broke it. Once a composition root outgrows readability it
splits into `wire_<seam>.go` files — one per subsystem seam, each opening with a short comment
saying which seam it wires — and the root file keeps the entry point (`runRoot`), the few top-level
helpers, and a **file-top map comment naming every seam file with a one-line description**. For
`cmd/apogee` the seams are settings, tools, MCP, presentation, session/recall hosts, engine, and
server binding.

**3. The config cluster becomes `internal/config`; the /settings display projection stays in the
binary.**

`config.go`, `configwrite.go`, `configmigrate.go`, `configwatch.go`, the key registry
(`registry.go`) and the `options` struct move to `internal/config` as a package with a name that
answers the findability question directly. (Amended 2026-09-30: `configwatch.go` did not land in
`internal/config`. The stat-poll watcher is its own package, `internal/filewatch`, which knows only
that a file changed; parsing, the diff and the apply stay in `cmd/apogee`.) `settingsrows.go` and `settingsedit.go` do **not** move.
Their file headers already state the reason and it survives the move unchanged: the schema, the
precedence that decided which source won, the config file's own spelling of a value, the masking of
a secret, and the `$EDITOR` round trip are the **binary's** knowledge, and the renderer that draws
the pane holds none of it (ADR 0011's thin renderer). What crosses that seam is a list of rows a
pane can paint — a projection built in the composition root — and nothing else.

The move lands **bridge-then-drop**: a temporary `configbridge.go` of type aliases keeps every
staying file compiling unchanged while the files relocate in reviewable steps, and the last step
deletes it and qualifies the call sites. The bridge is scaffolding, not a layer; it does not outlive
the move.

**4. A package past ~10 non-test files carries a doc.go file map, enforced by a test.**

The map names **every** non-test file in the package with a half-line role. Ten counts. A shared
helper (`internal/docmap`) reads the package directory, globs its non-test `.go` files and fails
naming any file the directory's `doc.go` does not mention; each qualifying package opts in with a
one-line `docmap_test.go`. A missing `doc.go` in a qualifying package is a failure, not a skip.

At ratification the rule binds `internal/tui` (53 non-test files), `internal/tools` (34),
`internal/mechanisms` (27), `cmd/apogee` (26), `internal/domain` (22), `internal/platform` (18),
`internal/agent` (18), the new `internal/config`, `internal/processing` (11), `internal/security`
(10) and `internal/probe` (10). Packages below the threshold pay nothing.

### What does not change

- **ADR 0010's layering.** `internal/config` imports `internal/domain` and siblings and **never**
  the root module; the root facade stays thin and holds no engine logic. Nothing about the moved
  code becomes public API.
- **The "binary owns it" behaviour stances** of
  ADR 0024 (the
  heartbeat observes, the rebind applies, the binary owns the move) and
  ADR 0028 (the TUI
  owns *when*, the binary owns everything the switch touches). Those are statements about which
  layer decides, and the binary still decides: `package main` imports `internal/config` and calls
  it. Only file homes move — no behaviour, no ownership, no seam.
- **`internal/tui` stays flat.** No sub-packages. Splitting the renderer into sub-packages would
  force exported seams across the `Update`/fold path for no navigational gain the file map does not
  already give.
- **ADR 0031's
  door-keeping invariants.** This is a structure-only decision: the engine stays wire-silent, the
  Approver stays wait-tolerant, no connector is added, and everything stays benchable.
- **Behaviour, rendering and wiring semantics.** Every split named here is a pure move; the tests
  that covered the code before cover it after.
