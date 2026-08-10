---
Status: accepted
---

# Files split by concern, and the config cluster gets a package

## Context

[ADR 0010](0010-package-layout-domain-core-and-thin-root-facade.md) settled the layout **above**
the file: a domain core, subsystem packages that never import root, and a thin root facade. It says
nothing about what happens **inside** a package, and for most of this repo nothing needed saying —
the 2026-08-10 doc-landscape audit (`../reviews/2026-08-10 - 00 - doc-landscape-audit.md`, §Flags 3–4)
found the package layout exemplary, with `internal/domain` mirroring [CONTEXT.md](../../CONTEXT.md)
file-by-file and 5/5 findability probes passing at package level.

Every probe that failed, failed one level down:

- **`internal/tui/model.go` — 4,743 lines, 120 functions.** The name says "the Model"; the content
  is the coordinator plus at least three uninvited concern clusters — the session-save write queue,
  approval key handling and prompt rendering, and command running/refusal. "Where is the session-save
  queue?" answers `model.go`, which is the same as no answer.
- **`cmd/apogee/wire.go` — 2,932 lines, 102 functions, no file-top map, 15+ internal imports.**
  Every "where is X constructed" answer is this one haystack.
- **The `cmd/apogee` config cluster — ~5,000 lines of `config.go`, `configwrite.go`,
  `configmigrate.go`, `configwatch.go`, `registry.go` and the `options` struct, plus ~800 lines of
  /settings projection.** A coherent concept with its own three ADRs
  ([0035](0035-the-settings-surface-persists-one-key-per-deliberate-edit.md),
  [0037](0037-every-settings-edit-applies-to-the-running-session.md),
  [0041](0041-the-config-file-is-watched.md)) living in `package main` because that is where it
  started, not because anything put it there.
- **`internal/tui/doc.go` — a 594-line narration that names every file in the package**, load-bearing
  for navigation, hand-maintained, and guarded by nothing. `internal/mechanisms/doc.go` shows the
  end state of that arrangement: a 14-line stub covering 27 files.

The yardstick these are measured against is not invented here. The coding-standards skill
(`airiclenz/skills`, commit `da76213`) gained three narrow rules the audit argued would have
prevented exactly these gaps: coordinator types split their methods across files by concern cluster;
composition roots split by subsystem seam and carry a map comment; a package past ~10 files
maintains a file map, test-enforced where the language allows.

The repo already knows how to do the first two — `internal/tui/prompteditor.go` (the input cluster)
and `internal/tui/fold.go` (the event fold) were lifted out of `model.go` exactly this way, and
`internal/tui/doc.go` is the map rule done by hand. What was missing was a decision that makes it
the rule rather than a habit that holds until the next busy week.

The owner ratified the four calls below on 2026-08-10; the mission brief is
`../handoffs/2026-08-10 - 00 - structure-refactor-model-wire-split.md` and the execution is
`../plans/2026-08-10 - 02 - structure-split-plan.md`.

## Decision

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

[ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md) binds the mechanics: the
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
answers the findability question directly. `settingsrows.go` and `settingsedit.go` do **not** move.
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
  [ADR 0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md) (the
  heartbeat observes, the rebind applies, the binary owns the move) and
  [ADR 0028](0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md) (the TUI
  owns *when*, the binary owns everything the switch touches). Those are statements about which
  layer decides, and the binary still decides: `package main` imports `internal/config` and calls
  it. Only file homes move — no behaviour, no ownership, no seam.
- **`internal/tui` stays flat.** No sub-packages. Splitting the renderer into sub-packages would
  force exported seams across the `Update`/fold path for no navigational gain the file map does not
  already give.
- **[ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)'s
  door-keeping invariants.** This is a structure-only decision: the engine stays wire-silent, the
  Approver stays wait-tolerant, no connector is added, and everything stays benchable.
- **Behaviour, rendering and wiring semantics.** Every split named here is a pure move; the tests
  that covered the code before cover it after.

## Considered options

- **Leave file-level structure to case-by-case judgment.** Rejected by evidence: judgment produced
  a 4,743-line `model.go` and a 2,932-line `wire.go` in a repo whose package layout is exemplary,
  and the findability probes fail exactly where the written rule was missing.
- **Sub-package `internal/tui` (e.g. `tui/approval`, `tui/session`).** Rejected: the `Model` is one
  coordinator that owns all of that state, so sub-packages would export seams purely to let the
  coordinator reach its own fields — churn against ADR 0011's thin-renderer shape, for a
  navigational win the concern-cluster files already deliver.
- **Move the whole settings cluster, `settingsrows.go` / `settingsedit.go` included, into
  `internal/config`.** Rejected: those two files are the /settings **display** projection, and their
  residency in the composition root is the ADR 0011 split working as designed. Moving them would put
  renderer-shaped output in a package that has no business knowing what a pane paints.
- **Generate the doc.go maps.** Rejected: the value of the map is the human-written half-line saying
  what a file is *for*; a generated list of names narrates nothing. The test pins coverage only, so
  the prose stays hand-written and cannot silently rot.
- **A hard line-count limit per file.** Rejected as the primary rule: size is the symptom that makes
  the drift visible, but the cure is a concern boundary. ~400 lines stays a smell threshold, not a
  gate.

## Consequences

- Implemented by `../plans/2026-08-10 - 02 - structure-split-plan.md` (items 3–15), behaviour-
  preserving throughout, one commit per item, `make check` green per item.
- **Adding a file to a mapped package now costs one line of `doc.go`** — and forgetting it fails the
  package's own test rather than rotting quietly. Any split or rename inside a mapped package
  updates the map in the same commit.
- **Navigation questions get package-level answers again**: "where is the write queue" →
  `internal/tui/sessionsave.go`; "where is the MCP client built" → `cmd/apogee/wire_mcp.go`; "where
  does a config key live" → `internal/config`.
- `git blame` on the moved code needs `--follow` / `-C`; the moves are pure, so history is intact
  behind the rename.
- File naming inside a package follows the package's own majority — this repo's is compact, no
  underscores (`internal/tui`, most of `internal/mechanisms`); the plan's item 12 brings
  `internal/mechanisms` into line and cures its confusable `syntax.go` / `syntaxcheck.go` pair.
- More files and more qualified call sites (`config.X`) is the accepted cost. The alternative —
  fewer, larger files — is the state this record exists to end.
