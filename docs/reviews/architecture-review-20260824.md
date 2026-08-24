# Architecture review — apogee

Date: 2026-08-24.

Terminal coding agent, Go. The scope and criteria follow the *Improve Codebase Architecture* skill
(lines/terms from `LANGUAGE.md`: module, interface, implementation, depth, deep, shallow, seam,
adapter, leverage, locality). This is the plain-text sibling of
[`docs/reviews/architecture-review-20260824.html`](architecture-review-20260824.html) — the rendered
report (with diagrams) lives there; this file records the same findings as text.

The engine is well-shaped at the large scale — a single runner in `internal/run`, one-responder
provider seam, the ADR-0010 module order. The friction concentrates where a deep module was
*declared* but its logic stayed scattered across thin siblings. Six deepening candidates follow.

---

## 1. One write-funnel for safety + undo — **Strong**

**Files**

- `internal/tools/write_file.go`, `file_ops.go`, `delete_file.go`, `path_safety.go`, `workspace_scoped.go`
- `internal/security/safeio.go`, `writepermit.go`, `path_safety.go`
- `internal/undo/journal.go`
- `internal/agent/dispatch.go`

**Problem.** Adding one write tool composes six-plus helpers across three packages by hand, and the
undo capture is re-invoked at four separate mutation sites (copy, move, delete each call
`capturePreImage` + `commit` themselves). The "one funnel" guarantee ADR 0051 promises holds only
because the test discipline pins it — a new verb that omits the capture line silently falls out of
`/undo` coverage with no compile-time signal.

**Solution.** One deep `writeFunnel` module that executes a body function while marshalling
path-safety → permit → capture → OS write → journal atomically, and every writer of the family
lands on it.

**Wins**

- Locality: verdict, safety, capture concentrate in one module.
- A new write tool cannot silently lose `/undo` coverage.
- Deletion: six hand-composed helpers vanish, none reappear.
- Interface shrinks; implementation absorbs the funnel.

**Strength: Strong** · in-process

---

## 2. Collapse the 19-Mechanism boilerplate — **Strong**

**Files:** `internal/mechanisms/{grammar, decompose, toolfilter, toolresultcap, readloop, filehint,
cot, guideddecomposition, emptyresponse, validate, syntax, autofix, readrepeat, toolloop,
truncatehistory, errorenrich, cachedcontent}.go`, `catalogue.go`.

**Problem.** Every Mechanism repeats the same five-part template — init, register row, empty struct,
trivial constructor, hook closure — differing only in descriptor values and one hook method; only a
handful need injected dependencies, yet each file re-spells the register ceremony.

**Solution.** A shared `registerMechanism` in `catalogue.go` that fills the descriptor and
constructor by convention, dropping the ~19 near-empty `newZ` one-liners and the identical
post-response `ActionRetry{Inject}` delivery.

**Wins.**

- Deletion test: 19 constructors are pass-throughs — complexity vanishes nowhere.
- Locality: the Mechanism shape is owned once, not nineteen times.
- Catalogue and descriptor cannot drift.
- New Mechanism = one hook + one row, not five parts.

**Strength:** Strong · in-process

---

## 3. Make the registry the one config home — **Files**

**Files:** `internal/config/registry.go`, `config.go` (the `keyAccessors` table), `options.go`,
`defaults/`, `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_settings.go`.

**Problem.** Every config key is hand-described twice — once as a registry row, once as a parallel
`keyAccessors` closure for from-file/from-env/from-flag — plus a `fileConfig` field, an `Options`
field, a wire projection line, and a live-apply switch case. "Adding a key" is a five-table surgery,
and the registry's "single source of truth" is true only of the path, never of the resolution.

**Solution.** Fold the source metadata (flag, env, apply, projection) into the registry row and
generate the rest from it, so a new key lands as one row — with a coverage test that fails when a
row has no `Config` field or no apply case.

**Wins.**

- Leverage: one row pays back across every surface.
- Adding a key stops being a five-table restatement.
- A bijection guard pins behaviour, not just the path.

**Strength:** Strong · in-process

---

## 4. One composer for every unattended run — **Files**

**Files:** `cmd/apogee/headless.go`, `schedule.go`, `daemonfire.go` (each ~150-line Config block).

**Problem.** The same "compose an unattended run's `apogee.Config`" is written three times —
headless, the TUI-hosted Schedule, the daemon — drifting a hair per Driver on secret-internal state,
extra read roots, and response reserve.

**Solution.** One shared composer `firingConfig(binding, workspace, mode, delegates)` used by all
three Drivers — a single home for the unattached-run shape ADR-0031 already declares one shared
runner for.

**Wins.**

- Fix the unattended-run shape once, fixed in all three.
- A security/config change stops being a three-way edit.
- Locality: the run's posture concentrates in one module.

**Strength:** Strong · in-process

---

## 5. Type the diff tools' outcome — stop re-parsing prose — **Worth exploring**

**Files:** `internal/tui/diffbody.go`, `internal/tools/git.go`, `internal/tools/view_diff.go`,
`internal/domain/toolsummary.go`, `internal/tools/diff.go`.

**Problem.** `git_diff_range` never attaches a typed outcome at apply time, so the TUI re-walks the
rendered diff prose back into regions — restoring the ADR-0011 anti-pattern the edit tools solved
with typed regions — and the diff tag triple lives across three files, string-typed with no import.

**Solution.** Attach a typed `domain.DiffStat` to `git_diff_range` like the edit tools do, and export
the shared tag constants from the seam so one move cannot leave a set behind.

**ADR note.** Re-aligns with ADR-0011 (the view stays a thin renderer) — the re-parsing exists only
because the tool carries no typed summary, so this deepens rather than reopens the ADR.

**Wins.** The diff's facts live with the tool; the renderer stops re-deriving regions; the seam stays typed.

---

## 6. Stop persisting the TUI's render shape in the codec — **Worth exploring**

**Files:** `internal/tui/transcriptcodec.go`, `internal/tui/transcript.go`,
`internal/domain/toolsummary.go`.

**Problem.** The transcript codec re-encodes the TUI render shape — `EditRegions`/`RegionFiles` — into
the session record, directly contradicting `domain`'s "never persisted in the session record"
contract; it exists only to keep a diff's shape across a restart, which replay already carries.

**Solution.** Persist only the already-rendered stacked `Details` rows; the codec stops carrying a
presentation copy the engine owner explicitly disowned.

**ADR note.** Contradicts the domain's "EditRegions are never persisted" contract — worth fixing
because the persisted rows add a second home a schema change must find.

**Wins.** The session record stops holding presentation bytes; one fewer serialized region copy.

---

## Recommended first

The **write funnel** (candidate 1) — it is the daily cost of the whole safety axis. Today adding one
write tool is a six-helper, three-package assembly, and the `/undo` guarantee is a test-discipline
fiction, not a construction guarantee. One deep `writeFunnel` collapses the top three findings into
one module, passes the deletion test on every shallow copy helper, and raises the seam's test surface:
a new write verb gains path-safety, permit, and journal interplay by construction. The **config
consolidation** (candidate 3) has the broadest per-key leverage as the runner-up.