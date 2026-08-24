# Residuals sweep — plan

**Goal:** close the two open defects and the three closable parked residuals in `ISSUES.md` that
need no grill session: the `/v1` warning's quantifier, the hero tape's stray knob-2 sleep, the
untested `pdfDisplayPath` plural branch, the autofix exec-fence "live box" entry (closed as
by-design), and the Firing scratch-dir gap (every Firing gets its own scratch dir, on every
Driver).

**Date:** 2026-08-24
**Status:** ready
**sized for:** ~200k-context host

## Authoritative sources

- `ISSUES.md` § "Open defects" (the `/v1` quantifier, the knob-2 sleep) and the three parked
  entries "Hook exec fence and scheduled-Firing Configs carry the construction-time scratch seed",
  "`pdfDisplayPath`'s multi-page header is never exercised by a test", "The hero tape's knob 3 is
  a clock where a screen-state trigger is needed" (the last is OUT of scope — see below). **If this
  plan's description of a defect disagrees with `ISSUES.md`, `ISSUES.md` wins**; the ratified
  design calls below decide the REMEDY, which `ISSUES.md` does not.
- `docs/adr/0056-terminal-fail-fast-and-session-scratch.md` decision 3 — every session gets a
  scratch dir inside the confinement box under `~/.apogee/scratch/<session-id>/`, 0700, swept
  after 14 days; the model reaches it through `{{scratch}}`.
- `docs/adr/0033-*` (schedules/Firings) — a Firing constructs a FRESH Agent, runs Plan or Auto
  only, and saves exactly one record; `internal/run` stays runner-agnostic (the CALLER composes
  the Config).
- `docs/adr/0031-*` — benchable-all-the-way-up: every Driver (TUI, headless, daemon) reaches the
  same engine behaviour from the same Config values.
- `docs/design/confinement-execution-contract.md` §7 (the box) and §10 (hook permits).
- `CONTEXT.md` § "Scratch dir" (line ~551) — the domain wording items 6 must keep true.
- `AGENTS.md` — `ISSUES.md` holds OPEN items only; a closed item is REMOVED from it and recorded
  under `CHANGELOG.md` `[Unreleased]`. Each item below owns its own `ISSUES.md` removal.

## Ratified design calls

Decided by the repo owner (Airic Lenz) on 2026-08-24, in answer to the questions this plan raised
before it was saved:

1. **Every Firing gets its OWN scratch dir** (items 5–6): the Driver mints the record id up front
   (`session.NewID`, the way an interactive session's id is minted when it begins), creates
   `~/.apogee/scratch/<id>/`, sets `Config.ScratchDir` to it, and passes the id through a new
   `run.Spec.RecordID` so the saved record and the scratch dir share one name and the existing
   14-day sweep reclaims it. Applies to all three Drivers — the in-session `/schedule` Firing
   (today: carries the interactive session's BOOT-TIME seed, stale after `/clear` or a session
   switch), the daemon Firing and `apogee headless` (today: no scratch dir at all). Rejected:
   sharing the interactive session's live dir; running Firings scratchless.
2. **The autofix exec fence closes as BY-DESIGN, no behaviour change** (item 4). Formatter paths
   are resolved from `PATH` exactly once, at construction, before the model has written a byte;
   a scratch dir that moves later is a fresh `~/.apogee/scratch/<id>/` that cannot contain an
   already-resolved path, so a live box could never change the outcome. Rejected: a live box
   accessor on `mechanisms.Deps` with a per-fire re-check (a Deps contract change guarding a case
   that cannot occur).
3. **Knob 2 keeps its documented 12s; the uncommented `Sleep 5s` is deleted** (item 2). 12s is
   ~4× the measured ~3s tail. Rejected: folding both into one 17s sleep.

Write-time calls by the plan author (mechanical, no user-visible difference):

4. `run.Spec.RecordID` empty ⇒ `run.Once` keeps minting the id at completion exactly as today, so
   the bench and every existing caller are untouched; only the three Drivers set it.
5. `pdfDisplayPath` is pinned by a direct unit test over the counts 0, 1 and 2 (item 3) — no
   multi-page binary fixture is committed.

## Standing requirements

- `skills: coding-standards` (forwarded by Execute mode by default).
- Any authorised deviation from an item's text lands as a dated `NOTES:` line under that item —
  never a silent change.
- Per-item Acceptance is targeted; `make check` runs once, at the closeout.
- No version identifier changes in any item (see the closing note).

## Out of scope

- Knob 3 of the hero tape (`ISSUES.md` "The hero tape's knob 3 is a clock…") — needs a
  screen-state trigger mechanism in the recording rig, not a value; its entry stays as written.
- "A delegation that never ran shows no prompt when expanded" and the `Request.InjectContext`
  placement — both grill-first by their own entries.
- Any re-recording of the hero clip: the banked take stands; items 2 only corrects the tape source.
- A live box accessor on `mechanisms.Deps` (rejected under design call 2).
- Retention/pruning of scratch dirs beyond the existing 14-day sweep.

---

## 1. Narrow the `/v1` warning's quantifier to the requests it is true of — ✅ DONE (2026-08-24)

NOTES (2026-08-24): the item's acceptance grep `grep -n 'every request' internal/config/defaults/config.yaml` no longer matches the api-key line it predicts — that line spells "EVERY request" in capitals, so the case-sensitive grep never matched it. Its substantive half holds: no match anywhere in the `endpoint` block (the surviving matches are lines 212, 224 and 502, unrelated prompt/context prose).

NOTES (2026-08-24): ISSUES.md is edited by all three items of this batched dispatch in one working tree, so file-level staging of item 1's commit necessarily carries items 2 and 3's ISSUES.md removals with it.

**What:** the seeded template says an `endpoint` spelled with a `/v1` suffix sends "every
request" to `/api/v1/v1/…`. That holds for the two paths the client builds from the base —
`/v1/chat/completions` and `/v1/models` — but not for the capability probe, which is
`propsPath = "/props"` (`internal/provider/client.go:20`) and carries no `/v1`. Reword the
comment at `internal/config/defaults/config.yaml:46-47` so the quantifier matches the code —
e.g. "Spelled with the suffix, the chat and model-list requests land on `/api/v1/v1/…` and 404s."
— keeping the conclusion (a suffixed base breaks the requests that matter) and the OpenRouter
example. The demo README repeats the same sentence at `graphics/demo/README.md:58`; fix it the
same way. `CHANGELOG.md:119` is history and stays as written. Remove the "The `endpoint` key's
`/v1` warning overstates 'every request'" entry from `ISSUES.md` § "Open defects" and record the
close under `CHANGELOG.md` `[Unreleased]` › Fixed (one short paragraph; the sidecar carries the
text for the verifier).

**Files:** `internal/config/defaults/config.yaml`, `graphics/demo/README.md`, `ISSUES.md`,
`CHANGELOG.md`

**Tests:** none new — the template is prose. `go test ./internal/config/` must still pass (the
package's tests parse the seeded template).

**Acceptance:**
- `go test ./internal/config/` → ok
- `grep -n 'every request' internal/config/defaults/config.yaml` → matches only the `api-key`
  line (the bearer-token sentence), never the `endpoint` block
- `grep -n 'every request' graphics/demo/README.md` → no match on the `/v1` sentence
- `grep -c 'overstates' ISSUES.md` → `0`

**Commit:** `docs(config): scope the /v1 endpoint warning to the requests it breaks`

---

## 2. Delete knob 2's stray `Sleep 5s` from the hero tape — ✅ DONE (2026-08-24)

NOTES (2026-08-24): removing the entry also took the `---` separator that had divided it from the `/v1` entry item 1 removed, which leaves `## Open defects` as an empty section heading; the heading is kept because the file's own Conventions block names it as one of the two standing sections.

NOTES (2026-08-24): `graphics/demo/README.md` needed no change, as the item says. `ISSUES.md` cited `README:112` as teaching the same 12s figure, but the README carries no `12s` (or `17s`) anywhere — line 112 states the overshoot reasoning without a number — so the plan's reading is the correct one and nothing there went stale.

NOTES (2026-08-24): ISSUES.md is edited by all three items of this batched dispatch in one working tree; item 1's commit may already carry this item's ISSUES.md removal.

**What:** `graphics/demo/tapes/hero.tape:116` is knob 2 (`Sleep 12s`) and line 117 is an
uncommented `Sleep 5s` that makes the effective wait 17s while the knob comment (lines 108–115)
reasons entirely about 12s. Per design call 3, delete line 117 and nothing else — the comment
already states the reasoning for 12s and stays byte-identical; `graphics/demo/README.md` names
no number for knob 2 and needs no change. Remove the "The hero tape's knob-2 wait is 17s…" entry
from `ISSUES.md` § "Open defects" and record the close under `CHANGELOG.md` `[Unreleased]` ›
Fixed (two sentences).

**Files:** `graphics/demo/tapes/hero.tape`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** none — a VHS tape has no test surface in this repo; the tape is not re-rendered by
this item.

**Acceptance:**
- `awk 'NR==116' graphics/demo/tapes/hero.tape` → `Sleep 12s`
- `awk 'NR==117' graphics/demo/tapes/hero.tape` → a blank line (the stray `Sleep 5s` is gone)
- `grep -c '^Sleep 5s' graphics/demo/tapes/hero.tape` → `0`
- `git diff --stat HEAD -- graphics/demo/tapes/hero.tape` → exactly 1 deletion, 0 insertions
- `grep -c 'knob-2 wait' ISSUES.md` → `0`

**Commit:** `chore(demo): drop the stray sleep that made knob 2 seventeen seconds`

---

## 3. Pin `pdfDisplayPath`'s plural header with a direct unit test — ✅ DONE (2026-08-24)

NOTES (2026-08-24): ISSUES.md is edited by all three items of this batched dispatch in one working tree; an earlier item's commit may already carry this item's ISSUES.md removal.

**What:** `pdfDisplayPath` (`internal/tools/read_file.go:149-153`) branches on the page count —
`"<path> (PDF, 1 page)"` singular, `"<path> (PDF, N pages)"` plural — and only the singular
branch is asserted today (`read_file_test.go:688`, through `Execute` on the one-page fixture).
Per design call 5, add a table-driven test `TestPDFDisplayPath` in
`internal/tools/read_file_test.go` covering the counts 0, 1 and 2 (`"x.pdf (PDF, 0 pages)"`,
`"x.pdf (PDF, 1 page)"`, `"x.pdf (PDF, 2 pages)"`); the 0 case pins that a zero-page count reads
as plural, which is what the function does today. No production change; no new fixture. Remove
the "`pdfDisplayPath`'s multi-page header is never exercised by a test" entry from `ISSUES.md`
§ "Parked / deferred work" and record the close under `CHANGELOG.md` `[Unreleased]` › Fixed (one
sentence under a "tests" wording).

**Files:** `internal/tools/read_file_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** the new `TestPDFDisplayPath` (three cases, `t.Parallel()` like its neighbours).

**Acceptance:**
- `go test ./internal/tools/ -run 'TestPDFDisplayPath|TestReadFile_Execute_ReadsAPDF' -v` →
  all PASS, `TestPDFDisplayPath` reports 3 subtests
- `go vet ./internal/tools/` → clean
- `grep -c 'pdfDisplayPath' ISSUES.md` → `0`

**Commit:** `test(tools): pin pdfDisplayPath's singular and plural headers`

---

## 4. Close the autofix exec-fence entry as by-design, with the reasoning pinned — ✅ DONE (2026-08-24)

NOTES (2026-08-24): the fence test already existed as `TestAutofixRefusesAFormatterInsideTheWritableBox` (workspace-root arm), so per the item's instruction it was EXTENDED — a `WritablePaths` arm plus a shared `plant` helper for the two planted formatters — rather than duplicated as a new test.
NOTES (2026-08-24): the CHANGELOG entry text is delivered in this sidecar instead of written into `CHANGELOG.md` (the item's Files list names that file); the run protocol makes the verifier the single writer of the shared documents.

**What:** `ISSUES.md` § "Parked / deferred work" › "Hook exec fence and scheduled-Firing Configs
carry the construction-time scratch seed" names two surfaces. This item closes the FIRST (the
hook exec fence); items 5–6 close the second (the Firing). Per design call 2 there is no
behaviour change. Do three things:

1. **State the reasoning where the value is derived and where it is declared.** Extend the
   comment above `deps.WritableBox = cfg.ConfinementBox()` in `deriveDeps`
   (`internal/agent/construct.go:344-353`) and the `WritableBox` field doc in `mechanisms.Deps`
   (`internal/mechanisms/catalogue.go:56-64`) with one paragraph each: the box is the
   CONSTRUCTION-TIME one deliberately — autofix resolves its formatters from `PATH` exactly once,
   at construction (`newAutofix`, `internal/mechanisms/autofix.go:116-146`), before the model has
   written anything, and never re-resolves; the only box field that moves later is the session
   scratch dir (`Agent.SetScratchDir`), and a moved-to scratch dir is a freshly created
   `~/.apogee/scratch/<id>/` that cannot contain an already-resolved path — so a live box would
   measure the same paths against a fence that cannot include them. Name that this is why the
   tools' per-call `Agent.confinementBox()` (`internal/agent/dispatch.go:422-432`) follows the
   live dir while this one does not: the tools resolve and spawn PER CALL, autofix resolves ONCE.
2. **Pin it with a test** in `internal/mechanisms/autofix_test.go`: build autofix through `Build`
   with a `Deps.LookPath` that resolves a formatter INSIDE `Deps.WritableBox`'s `WritablePaths`
   (a temp dir named as an extra writable path) → that rung is absent from the ladder; and with
   the same formatter OUTSIDE the box → the rung is present. If an equivalent assertion already
   exists in that file, extend it rather than duplicating it, and say so in a NOTES line.
3. **Register bookkeeping.** Edit the `ISSUES.md` entry so it names ONLY the Firing half
   (retitle it "Scheduled-Firing Configs carry the construction-time scratch seed", drop the
   hook-fence clause and its `construct.go:351` citation, keep the Firing evidence) — item 6
   removes the entry outright. Record the by-design close under `CHANGELOG.md` `[Unreleased]` ›
   Fixed with the reasoning above in two or three sentences.

Standards that shape this item: comments explain WHY, not what; the test names the rule it pins
in its doc comment; no production code changes.

**Files:** `internal/agent/construct.go`, `internal/mechanisms/catalogue.go`,
`internal/mechanisms/autofix_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** the new (or extended) fence test in `autofix_test.go`.

**Acceptance:**
- `go test ./internal/mechanisms/ -run 'Autofix' -v` → PASS, and the output names a test whose
  name contains `WritableBox` or `Fence`
- `go build ./...` → ok
- `git diff HEAD --stat -- internal/agent/construct.go internal/mechanisms/catalogue.go` →
  comment-only changes (`go build` unchanged; `git diff HEAD -- <file> | grep '^[+-]' | grep -v
  '^[+-]\s*//' | grep -v '^[+-]\{3\}'` → empty for both files)
- `grep -c 'Hook exec fence' ISSUES.md` → `0`; `grep -c 'construction-time scratch seed' ISSUES.md`
  → `1`

**Commit:** `docs(mechanisms): pin why the autofix exec fence is the construction-time box`

---

## 5. `run.Spec.RecordID` — a caller may name the record before the Firing runs — ✅ DONE (2026-08-24)

**What:** the engine half of design call 1. Add to `run.Spec` (`internal/run/run.go`, the `Spec`
struct at ~line 23) a field:

```go
// RecordID is the id the saved record is filed under, minted by the CALLER before the
// Firing starts so anything the caller keys on that id — the Firing's scratch dir — exists
// under the same name from the first tool call. Empty ⇒ Once mints one at completion, as
// it always has; the bench and every caller that keys nothing on the id leave it empty.
RecordID string
```

In `Once`, at the record construction (`run.go:243-246`), use `spec.RecordID` when non-empty and
`session.NewID(finishedAt)` otherwise (design call 4: the empty path is byte-for-byte today's).
`Result.SessionID` keeps reporting the id actually used. Validate nothing about the id's shape
here: `session.Store.Save` already refuses an id that is not a valid stem (`store.go:61`), and
that refusal surfaces through the existing "save the firing's record" error. Update the package
doc (`internal/run/doc.go`) with one sentence on `RecordID`.

**Files:** `internal/run/run.go`, `internal/run/doc.go`, `internal/run/run_test.go`

**Tests:** in `run_test.go`, using the existing harness (`harness_test.go`): (a) a Spec with
`RecordID` set and a Store → `Result.SessionID` equals it and `store.Load(id)` returns the record;
(b) a Spec with `RecordID` empty → `Result.SessionID` is non-empty and minted (the existing
persisted-firing test already covers this — extend it with the assertion that the id is not the
one from (a) rather than writing a new test); (c) a `RecordID` that is not a valid stem
(e.g. `"../escape"`) with a Store → `Once` returns the "save the firing's record" error and the
run's own Result is still returned.

**Acceptance:**
- `go test ./internal/run/ -run 'RecordID|Persist' -v` → PASS for the new and extended cases
- `go test ./internal/run/` → ok (the live test skips without `APOGEE_LIVE_ENDPOINT`)
- `go vet ./internal/run/` → clean

**Commit:** `feat(run): let the caller name a Firing's record id before the run`

---

## 6. Every Driver gives its Firing an own scratch dir named after the record — ✅ DONE (2026-08-24)

NOTES (2026-08-24): the daemon half is spelled as `configFor` returning the entry's scratch ROOT beside the Config (the plan left that shape to the implementer); `fire` mints the dir there, so the Config is still composed once and `ScratchDir` is assigned exactly once per Driver file.
NOTES (2026-08-24): the shared assertion (`assertFiringScratchDir`) and the `firingScratch` unit test live in `cmd/apogee/wire_test.go` — named by the item's Tests paragraph, though its Files list omits the file.

Depends on item 5.

**What:** the Driver half of design call 1. Add one helper beside `ensureScratchDir` in
`cmd/apogee/wire.go` (~line 422):

```go
// firingScratch mints the record id a Firing will be saved under and creates that id's
// scratch dir under root — the pair every Driver hands run.Once as Spec.RecordID and
// Config.ScratchDir, so the record and the dir share one name and gcScratchDirs reclaims
// the dir on the sessions' own schedule. dir is "" when root is "" or creation failed,
// exactly as ensureScratchDir answers; the id is minted regardless.
func firingScratch(root string, now time.Time) (id, dir string)
```

Then wire it at the three Firing composition sites, each setting `cfg.ScratchDir = dir` on the
Config it composes and `RecordID: id` on the `run.Spec` it passes to `runOnce`:

- `cmd/apogee/schedule.go` — `scheduleWiring.fire` (~line 80–140): `w.roots.scratch` is the
  root. This REPLACES the boot-time seed `w.base.ScratchDir` carried today (the defect).
- `cmd/apogee/daemonfire.go` — `daemonWiring.fire` (~line 160–172): the per-entry `roots`
  resolved in `configFor` (~line 221) carry `scratch`; return the scratch root from `configFor`
  (or resolve it once in `fire` beside it — the implementer's call, but the Config must be
  composed ONCE) and pass it to `firingScratch`. Also run `gcScratchDirs(roots.scratch,
  time.Now())` once where the daemon wiring is built (`daemonfire.go` ~line 90–105, where the
  store is created) — a daemon is a long-lived process that never passes the TUI boot at
  `wire.go:84`.
- `cmd/apogee/headless.go` — the `runOnce` call at ~line 470: `roots.scratch`; run
  `gcScratchDirs(roots.scratch, time.Now())` once at headless start, right after `resolveRoots`,
  for the same reason.

The clock: `time.Now()` at each site (a Firing's id is a wall-clock stamp like a session's; the
tests pin the id by reading it back off the Spec, not by freezing the clock). Nothing in
`internal/run` or `internal/agent` changes in this item.

Docs: amend `CONTEXT.md` § "Scratch dir" (~line 551) with one sentence — a Firing gets its own dir
named after its record, on every Driver. Remove the (already retitled, item 4) "Scheduled-Firing
Configs carry the construction-time scratch seed" entry from `ISSUES.md` and record the close
under `CHANGELOG.md` `[Unreleased]` › Fixed: name all three Drivers and what each did before
(the seed / nothing), and that the dir shares the record's name so the 14-day sweep covers it.

Standards that shape this item: one helper, three call sites, no per-Driver copies of the
mint-and-create pair; the Config is composed once per Firing and handed over whole.

**Files:** `cmd/apogee/wire.go`, `cmd/apogee/schedule.go`, `cmd/apogee/daemonfire.go`,
`cmd/apogee/headless.go`, `cmd/apogee/schedule_test.go`, `cmd/apogee/daemonfire_test.go`,
`cmd/apogee/headless_test.go`, `CONTEXT.md`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** each Driver's existing test that captures the composed `run.Spec` through the
`runOnce` seam gains assertions: `Spec.RecordID` is non-empty; `Spec.Config.ScratchDir ==
filepath.Join(<that test's scratch root>, Spec.RecordID)`; the dir exists with mode `0700`
(skip the mode check on Windows). For `schedule_test.go` add the negative: the Firing's
`ScratchDir` is NOT the session's seed even when the seed is set. A unit test for
`firingScratch` in `wire_test.go` (or the file that tests `ensureScratchDir`) covers the
empty-root case (id minted, dir `""`).

**Acceptance:**
- `go test ./cmd/apogee/ -run 'Firing|Headless|Schedule|Scratch' -v` → PASS, including the new
  assertions
- `go test ./cmd/apogee/` → ok
- `go vet ./cmd/apogee/` → clean
- `grep -n 'ScratchDir' cmd/apogee/schedule.go cmd/apogee/daemonfire.go cmd/apogee/headless.go`
  → one assignment per file
- `grep -c 'construction-time scratch seed' ISSUES.md` → `0`

**Commit:** `fix(cmd): give every Firing its own scratch dir named after its record`

---

## Suggested version bump

`patch` (v0.16.6 → v0.16.7): items 5–6 change observable behaviour on every Driver (a Firing's
`{{scratch}}` is now a real per-Firing dir; `run.Spec` gains an additive field, a minor-class
API change under ADR 0001's additive rule but no public release is cut on it), the rest is
docs and tests. No item bumps anything — the owner decides.
