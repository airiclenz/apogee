# Handoff: code-structure refactor — split model.go and wire.go

Date: 2026-08-10. Written for a fresh session whose job is to turn the
2026-08-10 structure findings into a saved implementation plan via
`/implement-plan` (write mode first — the plan does not exist yet), then
execute it item-by-item on the owner's go.

## Mission

Cure the two file-level junk drawers the doc-landscape audit found — the
package layout itself is exemplary and must not be reshuffled:

1. `internal/tui/model.go` — 4,743 lines / 120 functions. The name says
   "the Model"; the content is the coordinator plus at least three
   extractable concern clusters.
2. `cmd/apogee/wire.go` — 2,932 lines / 102 functions, no file-top map,
   imports 15+ internal packages. Every "where is X constructed" answer is
   this haystack.
3. (Candidate, owner call at grill time) the `cmd/apogee` config cluster —
   `config.go`, `configwrite.go`, `configmigrate.go`, `configwatch.go`,
   `settingsrows.go`, `settingsedit.go`, roughly 7,000 lines in package
   main — a coherent concept (ADRs 0035/0037/0041) with no package of its
   own. `internal/config` would also shrink wire.go's gravitational pull.
   Must be checked against ADR 0010's "the binary owns file paths" stance
   before it becomes a plan item.

## Findings detail not recorded elsewhere

The audit report (`docs/reviews/2026-08-10 - 00 - doc-landscape-audit.md`,
§Flags 3–4) has the summary; the extractable-cluster specifics live only
here:

- model.go clusters identified: the session-save write queue
  (`snapshotPayload` / `scheduleSave` / `queueWrite` / `pumpWrites` /
  `saveComplete` / `foldRecordWrite`, ~lines 1977–2290 at audit time) →
  e.g. `sessionsave.go`; the approval-key handling cluster; the
  command-running/refusal cluster. The house pattern for the lift already
  exists in-package: `prompteditor.go` (input cluster) and `fold.go`
  (event fold) were extracted the same way. Keep the Model type as the
  coordinator; no package split — flat `internal/tui` stays.
- wire.go: split by subsystem seam into `wire_<seam>.go` files — the
  import list already names the seams (heartbeat, present, schedule, mcp,
  skills, launcher, …); give the cluster a file-top map comment.
- `internal/tui/doc.go` is a 594-line narration that names every file in
  the package — load-bearing for navigation but hand-maintained. Add a
  structural test that every non-test `.go` file in the package is named
  in doc.go (pattern to copy: `TestFoldEventCoversEveryEventVariant`).
  Any model.go split must update the doc.go map in the same commit.
- Minor, bundle if convenient: `internal/mechanisms` mixes snake_case and
  compact file naming and has a confusable syntax Mechanism/engine file
  pair; `internal/mechanisms/doc.go` is a 14-line stub for 27 files.
  (Landed 2026-08-10 in `d862d0d`: file names are compact throughout, the
  pair is now `internal/mechanisms/syntax.go` — the Mechanism — and
  `internal/mechanisms/syntaxengine.go` — the pure checker — and
  `internal/mechanisms/doc.go` carries a real file map.)
- Findability probes passed 5/5 at package level; the failures are one
  level down ("where is the session-save queue" → model.go; "where is X
  built" → wire.go). That is the disease being treated.

## Law and constraints

- **ADR 0011 / `internal/tui/doc.go`: the Bubble Tea Model is copied by
  value on every Update.** File splits are safe; anything that introduces
  a held-by-value no-copy type (e.g. `strings.Builder`) is not.
  `TestModelNoBuilderByValue` guards it.
- ADR 0010 (domain core, thin root facade) binds any `internal/config`
  move; ADR 0031's door-keeping invariants bind everything.
- The new yardstick is in the coding-standards skill (committed
  `da76213`, airiclenz/skills): coordinator types split by concern
  cluster; composition roots split by seam with a map comment; packages
  past ~10 files carry a test-enforced doc.go file map.
- Behaviour-preserving refactor: no rendering or wiring semantics change;
  `make check` green per item; one commit per plan item.
- Commit to `main` directly, no AI attribution trailers, never bump
  VERSION/CHANGELOG release headings unasked.

## Coordinate with in-flight work

- Plan `2026-08-10 - 00 - tool-surface-improvements-plan.md` was 4/10 done
  and mid-execution on 2026-08-10 (items 5–6 touching `internal/tools/git.go`
  and registries). Rebase mentally: check its state first; avoid file
  collisions (it does not touch model.go/wire.go, but registry/config
  files border the config cluster).
- Plan `2026-08-10 - 01 - doc-landscape-cleanup-plan.md` (unexecuted)
  item 10 adds `cmd/apogee/doc.go`. If it lands first, the wire.go split
  must update that map; if this refactor lands first, item 10's map must
  describe the new `wire_<seam>.go` files. Either order works — just
  reconcile whichever runs second.
- `docs/layout/tool-layout.md` carries an uncommitted owner design sketch
  (unimplemented rendering redesign). Unrelated to this refactor — do not
  touch, do not commit it incidentally.

## Suggested process

1. Read this doc, the audit report's §Flags, ADR 0010/0011, and skim
   `internal/tui/doc.go`'s map.
2. Grill the open design calls with the owner before writing the plan
   (AskUserQuestion; owner ratifies scope first — in/defer/deny on the
   config-cluster move and the mechanisms-naming bundle, then per-item
   calls: cluster boundaries, `wire_<seam>` seam list, doc.go-map test
   scope).
3. `/implement-plan` write mode → save the plan as
   `docs/plans/2026-08-1N - NN - structure-split-plan.md` (house format,
   ratified-calls header). The saved doc is the deliverable — stop there
   unless the owner says execute.
4. On "execute": `/implement-plan` the saved doc.

## Suggested skills

- `/implement-plan` — both phases: writing the plan doc (its Write mode
  encodes format, naming, and the hard stop), then executing it.
- `/coding-standards` — the three new structure rules are the acceptance
  yardstick for every item.
- `/grill-me` — for step 2 if the owner wants the design calls
  stress-tested rather than just asked.
