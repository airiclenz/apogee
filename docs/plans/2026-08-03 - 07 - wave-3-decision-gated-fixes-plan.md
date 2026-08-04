# Plan — Wave 3: the decision-gated fixes (audit 2026-08-01)

**Date:** 2026-08-03
**Status:** not started
**Goal:** land the two audit findings the roadmap deliberately parked behind an owner decision —
`autofix`'s external-formatter spawn, which today runs outside every execution guard, and the
skills load order, where a repo-supplied skill silently outranks the user's own library. Both
decisions are now taken; this plan implements them.

**Owner decisions taken 2026-08-03 (do not re-open — implement as written):**

1. **`autofix`** — harden the seam **and** drop the two prettier rungs. The remaining external
   formatters (goimports, black, rustfmt) read *data* config files; only prettier `require()`s
   repo-authored JavaScript, which is the arbitrary-code-execution half of the finding.
2. **Skills** — the user's global library wins any cross-source ID collision, and every shadowed
   pair is recorded so `/skills` names it. Gating `.apogee/skills` behind `use-project-skills` was
   considered and **not** taken: a repo may still contribute skills, it just cannot silently
   replace one of the user's.

**Authoritative sources (precedence in this order):**

1. `docs/reviews/2026-08-01 - code-audit.md` — the two findings, their evidence and prescribed
   fixes: "High — `autofix` spawns an external formatter outside every execution guard" and
   "Medium — Repo-supplied skills outrank the user's global library and shadow it silently".
2. `docs/handoffs/2026-08-01 - 01 - merged-findings-roadmap.md` §5 (the parked decisions) and §6
   (Wave 3's placement).
3. ADR 0012 and `docs/design/confinement-execution-contract.md` §2.2 / §2.4 / §4 — the execution
   contract the new permit must mirror. `internal/agent/resolution.go:342-347`
   (`resolveLadderAuto`) is the specific ladder row it copies.
4. `CONTEXT.md` → "Agent mode" (`:217-225`) — the four-rung ladder: **Plan** forbids command
   execution outright; **Ask-Before** and **Allow-Edits** gate a command behind an Approval, and a
   post-response hook has no way to open one.
5. Repo conventions: `AGENTS.md`, ADR 0010 (package layering — `internal/skills` and
   `internal/mechanisms` depend downward on `internal/domain`, never the root facade), ADR 0011
   and `internal/tui/doc.go` (the Bubble Tea `Model` is value-copied; never a `strings.Builder`
   by value).

Where any document above disagrees with an item's text, **the document wins** — implement what it
says and record the divergence as a dated NOTES line under the item.

**Standing requirements:**

- Invoke with forwarded skills: `coding-standards` (Go).
- `make check` green before every commit; it runs `-race`. One commit per item.
- **Never** bump `VERSION`, a CHANGELOG release heading, or any other version identifier. The
  closing note carries the suggestion; the owner decides.
- No live LLM endpoint is needed for any item here.
- Any authorized deviation from an item's text lands as a dated NOTES line under that item.

**Out of scope (deliberate):**

- The architecture candidates C1–C11 (roadmap §6, Wave 4+). In particular C1's shared Mechanism
  runner: this plan adds one ctx wrap in the post-response runner and changes no other hook
  plumbing.
- Duplicating `internal/tools`' process-group teardown (`newProcessTeardown`, unexported) into
  `internal/mechanisms`. `cmd.WaitDelay` is the audit's prescribed bound and is all item 2 adds;
  consolidating the two spawn sites onto one exec seam is later, separate work.
- Gating `.apogee/skills` behind `use-project-skills` (decision explicitly not taken).
- `internal/validated/shipped.json`. The gemma-4 validated set is keyed by Mechanism **ID**;
  removing a rung inside `autofix` changes no ID, so the file is not edited and the set's evidence
  is not re-opened here.
- Hook points other than post-response. No permit is installed for pre-request, pre-tool-exec or
  post-tool-result hooks; their continued absence keeps the "may not spawn" default, which is the
  intended posture.

---

## 1. A hook-time subprocess permit in `domain`, installed by the engine — ✅ DONE (2026-08-03)

**What:**

- `internal/domain/confinement.go` — add, beside the existing `Confinement` context helpers
  (`:76-107`):

  ```go
  type SubprocessPermit struct{ Confinement *Confinement }

  func WithSubprocessPermit(ctx context.Context, p SubprocessPermit) context.Context
  func SubprocessPermitFromContext(ctx context.Context) (SubprocessPermit, bool)
  ```

  Document the three-state contract on the type: **absence is the default and means this hook may
  not spawn a subprocess at all**; a present permit with a nil `Confinement` means "run unfenced"
  (the Auto + `confine-to-workspace: false` "I am the sandbox" opt-in, `resolution.go:343-347`); a
  present permit carrying one means "confine to that box first". Note explicitly why absence is
  refusal here while an absent `Confinement` handle in `ConfinementFromContext` means "run
  unconfined": dispatch has already resolved a verdict by the time a tool reads its handle, and no
  such resolution runs ahead of a hook.
- `internal/agent/dispatch.go` — add `func (a *Agent) hookExecutionCtx(ctx context.Context)
  context.Context` next to `resolutionInput` (`:110`) and `fsConfinementAvailable` (`:404`), so it
  sits with the other ladder-facing helpers. It mirrors `resolveLadderAuto`'s subprocess row:

  | Effective mode | `confine-to-workspace` | fs confinement caps | Installed |
  |---|---|---|---|
  | not Auto | — | — | nothing (Plan refuses; Ask-Before / Allow-Edits need an Approval a hook cannot open) |
  | Auto | off | — | permit, nil `Confinement` |
  | Auto | on | available | permit carrying `&domain.Confinement{Confiner: a.cfg.Confiner, Box: …}` |
  | Auto | on | unavailable | nothing (the ladder gates the subprocess surface here) |

  The mode is read through `a.effectiveMode()` (`:380`), never `a.Mode()`, so a sub-agent's
  parent-tightened mode binds. The box is built from the same three fields `resolutionInput` uses
  (`:121-125`): `a.cfg.WorkspaceDir`, `a.cfg.ConfineWritablePaths`, `a.cfg.ConfineNetworkAllow`.
- `internal/agent/hookrun.go` — in `runPostResponseHooks` (`:134`), wrap the ctx **once** before
  the dispatch loop so every post-response hook fires under the permit. Touch no other runner.
- `docs/design/confinement-execution-contract.md` — add a dated amendment section
  `## 10. Hook-time subprocess execution (amendment, 2026-08-03)` in the style of §9, recording
  the permit, the four-row table above, and why absence means refusal.
- `CHANGELOG.md` — one bullet under `## [Unreleased]`. No release heading, no `VERSION` change.

**Tests:**

- `internal/domain` — round-trip `WithSubprocessPermit` / `SubprocessPermitFromContext`, and
  `ok == false` on a bare `context.Background()`.
- `internal/agent` — a table test over all four rows above, using a fake post-response hook that
  captures the permit it observes: no permit in Plan / Ask-Before / Allow-Edits; permit with a nil
  `Confinement` in Auto + confine-off; permit carrying the expected box in Auto + confine-on +
  caps; no permit in Auto + confine-on + a Confiner reporting `FSWrite: false`. Reuse the
  package's existing agent constructors and fake Confiner; add a minimal caps-controllable fake in
  the test file if none exists.
- `internal/agent` — a sub-agent case: an agent whose own mode is Auto but whose `liveMode`
  (`agent.go:78`) reports a tighter parent mode gets **no** permit, proving the gate reads
  `effectiveMode()`.

**Acceptance:**

- `go test ./internal/domain/... ./internal/agent/... -race`
- `make check`
- `grep -n "SubprocessPermit" internal/agent/hookrun.go` — exactly one install site, in
  `runPostResponseHooks`.

**Commit:** `feat(agent): gate hook-time subprocess execution behind a permit`

---

## 2. `autofix` runs its formatters under the permit, and the prettier rungs go — ✅ DONE (2026-08-03)

NOTES (2026-08-03): the ladder entry's run signature is
`func(ctx context.Context, gate spawnGate, content string) (string, bool)`, not the item's
illustrative `(ctx, path, content)`. The permit's box and the per-Mechanism timeout are fire-time
values a construction-built closure cannot capture (a test sets `timeout` on the constructed
Mechanism, so capturing it at construction would ignore that), and `path` is unused by every
remaining rung once the filename-keyed formatter is gone. `spawnGate` carries all three
(`allowed` / `confinement` / `timeout`) and is read once per `PostResponse` as the item requires. A
permit whose `Confinement` carries a nil `Confiner` also skips the rung, on the same
"never fall back to unfenced" rule as a `Confine` error.

NOTES (2026-08-03): the "No permit → no spawn" test's second half — "while a broken Go file is
still repaired by the gofmt tail" — is not reachable as written: `go/format.Source` succeeds only
on content whose parse errors come from the missing package clause, and it never adds one, so the
in-process tail's output never reduces `checkSyntax`'s error count (probed against the real
parser). The requirement is met by two tests instead: `TestAutofixWithoutPermitNeverSpawns` proves
the marker is absent against the production ladder, and `TestAutofixWithoutPermitStillRunsInProcessRungs`
proves a non-external rung still runs unpermitted, using a hand-built ladder in the production
shape.

**Depends on item 1** (the permit type and the engine-side install).

**What:** all in `internal/mechanisms/autofix.go` unless stated.

- Delete the two prettier entries from `externalFormatters` (`:211-212`). With them gone,
  `formatterSpec.usesFilePath` (`:198`) and the `file.ts` / `file.js` argv substitution in
  `runExternalFormatter` (`:222-232`) are dead — delete both and drop the now-unused `path`
  parameter. **Keep** `sanitizePath` (`:188`): it still guards `attemptFix`'s entry.
- Replace the package-level `formatterTimeout` var (`:33`) with `const defaultFormatterTimeout =
  3 * time.Second` plus a `timeout time.Duration` field on `autofixMechanism`, seeded in
  `newAutofix` (`:71`). Tests set the field on a constructed Mechanism; nothing mutates a global.
- Thread the fire's ctx through the repair path: `PostResponse` (`:129`) → `attemptFix` (`:160`) →
  the ladder entry → `runExternalFormatter`, which derives `context.WithTimeout(ctx, m.timeout)`
  instead of `context.Background()` (`:234`), so a user cancel stops an in-flight formatter.
- **Permit gate:** read `domain.SubprocessPermitFromContext(ctx)` once per `PostResponse`. Absent
  → the external rungs are skipped entirely and only the in-process gofmt tail (`:107-113`) runs
  (it spawns nothing, so it stays available in every mode, Plan included). Present → the external
  rungs may run.
- **Confine:** when the permit carries a `*domain.Confinement`, call
  `conf.Confiner.Confine(runCtx, conf.Box, cmd)` before `cmd.Run()` — the same shape as
  `internal/tools/exec_common.go:136-140`. Any Confine error, `domain.ErrConfinementUnavailable`
  included, **skips that rung**; never fall back to running unconfined. A skipped rung degrades to
  "leave the payload as-is", which is the Mechanism's existing standing behaviour for an
  unavailable formatter.
- Set `cmd.WaitDelay = m.timeout` so a wrapper-shaped formatter (`prettier.cmd` → node,
  `black.exe` → python, any shell shim) that leaves a grandchild holding the inherited pipes
  cannot wedge `cmd.Run()` and freeze the single-goroutine loop.
- Keep "external vs in-process" decidable at fire time by carrying it on the ladder entry — e.g.
  `type repairer struct { external bool; run func(ctx context.Context, path, content string) (string, bool) }`
  — rather than re-deriving it from the language.
- `docs/design/mechanism-catalogue.md:120` — amend the `autofix` row's parenthetical so it names
  the current formatter set (goimports / black / rustfmt, plus the always-present in-process gofmt
  tail) and the permit gate.
- `CHANGELOG.md` — one `Fixed` bullet under `## [Unreleased]` naming both halves: the unguarded
  spawn and the prettier removal.

**Tests:** `internal/mechanisms/autofix_test.go`

- **No permit → no spawn.** With `Deps.LookPath` resolving a formatter to a helper that records a
  side effect (e.g. writes a marker file), a `PostResponse` on a broken payload with no permit in
  ctx leaves the marker absent — while a broken Go file is still repaired by the gofmt tail.
- **Confine is used.** Permit carrying a fake Confiner → `Confine` was called, with the permit's
  box, before the process ran.
- **Confine refusal is fatal to the rung.** A Confiner returning `domain.ErrConfinementUnavailable`
  → the rung is skipped, the process never runs, the payload comes back untouched.
- **Prettier is gone.** A syntax-broken `.ts` payload is returned unchanged even with a permit and
  a resolvable `prettier` on the injected PATH — behavioural, not an assertion over the table.
- **Cancel.** A ctx cancelled while a formatter is running makes `PostResponse` return promptly
  with the payload untouched.
- **Kill-path bound.** With `timeout` set short and a helper formatter that sleeps well past it,
  `PostResponse` returns within a generous multiple of the timeout. This is the test the global
  60s override made impossible.
- Delete the global-timeout override at `autofix_test.go:23` and adjust every test that leaned on
  it to set the per-Mechanism field instead.

**Acceptance:**

- `go test ./internal/mechanisms/... -race`
- `make check`
- `grep -ci prettier internal/mechanisms/autofix.go` → `0`
- `grep -n "context.Background" internal/mechanisms/autofix.go` → no match

**Commit:** `fix(mechanisms): confine autofix's formatter spawn and drop the prettier rungs`

---

## 3. ADR 0032 — the user's skill library outranks the workspace — ✅ DONE (2026-08-03)

NOTES (2026-08-03): the item's citation "ADR 0012 D6" has no referent — ADR 0012's Decision
section is unnumbered, and the tighten-only rule ("project config may only *add* (tighten)") sits
in its dangerous-action-guard paragraph. ADR 0032 therefore cites ADR 0012 and quotes that rule
rather than a decision number that does not exist. The ADR also carries a `## Considered options`
section beside the four required ones, matching the house shape of ADR 0031 (and 0027/0030/0012);
the rejected `use-project-skills` gating is recorded there.

Decision record only; no code changes in this item. It lands before item 4 so the code and doc
comments there can cite a document that exists.

**What:** write `docs/adr/0032-the-user-skill-library-outranks-the-workspace.md`, matching the
house ADR shape of `docs/adr/0031-*.md`: `---` / `Status: accepted` / `---` frontmatter, then
`# <title>`, `## Context`, `## Decision`, `## Consequences`.

- **Context** — the audit finding: `sourceDirs` returns the three dirs in increasing priority with
  `<workspace>/.apogee/skills` scanned unconditionally, and `Catalog.set` is a plain last-writer-wins
  map assignment, so a repo can ship `.apogee/skills/<id>/SKILL.md` carrying the DisplayName and
  Summary of a skill the user already has and an attacker-authored body. The user types `/<id>` and
  the repo's instructions are prepended to the turn with full agent authority — arbitrary command
  execution in Auto — with the picker showing one row and no notice. Record that the current order
  was adopted as apogee-code oracle parity (`internal/skills/load.go:56-59`) without weighing that
  consequence, and that it sits against the project's own repo-trust posture (ADR 0012 D6 — project
  config may only tighten).
- **Decision** — the global library (`<apogee home>/skills`) wins any **cross-source** ID collision;
  the workspace sources keep their relative order among themselves (the bare `skills/` dir still
  outranks `.apogee/skills`); **every** collision, cross-source or same-source, is recorded on the
  catalog so `/skills` can name both the winner and the loser. Discovery still scans all three dirs
  — `.apogee/skills` is **not** newly gated behind `use-project-skills`.
- **Consequences** — a deliberate, documented deviation from oracle parity (a skill written for
  either tool still loads; only collision resolution differs). A repo can still contribute new
  skill ids; it can no longer silently replace one of the user's. ADR 0002's open-extension-point
  posture is unchanged. This supersedes no ADR — the prior order lived only in a code comment.

**Tests:** none — this item is a decision record.

**Acceptance:**

- `ls docs/adr/0032-the-user-skill-library-outranks-the-workspace.md`
- The file opens with the `Status: accepted` frontmatter and carries all four sections.
- `make check` (nothing else in the tree changed).

**Commit:** `docs(adr): ADR 0032 — the user's skill library outranks the workspace`

---

## 4. The loader honours the new precedence and records every shadowed skill — ✅ DONE (2026-08-04)

NOTES (2026-08-03): recording the loser "through `addSkip`" as the item specifies has one
consequence the item does not state — `Catalog.skipError` joins every recorded skip, so `Load` now
returns a non-nil soft error on a pure collision, where nothing failed to load. This is the literal
reading and it is left as-is (both callers already drop that error: `NewProvider` discards it and
`wire.go:487` discards `Reload`'s), rather than teaching `skipError` to filter on cause. The renamed
`TestLoadHomeOverridesWorkspaceOnIDCollision` therefore drops the original's `t.Fatalf` on the soft
error; its "one skill, not both" assertion is kept as required. `Load`'s doc comment now says a
record is not necessarily a failure.

**Depends on item 3** (cites ADR 0032 from code comments and `CONTEXT.md`).

**What:**

- `internal/skills/load.go` — `sourceDirs` (`:60-72`) returns
  `[<workspace>/.apogee/skills, <workspace>/skills (still gated by UseProjectSkills), <home>/skills]`:
  home **last**, so the existing last-writer-wins walk gives the global library priority while the
  intra-workspace order is unchanged. Rewrite the function comment — it currently sells the order
  as oracle parity — to state the ADR 0032 rule and cite the ADR.
- `internal/skills/skill.go` — add the typed cause the report keys on:

  ```go
  type ShadowedError struct{ By string } // By = the winning SKILL.md's absolute path
  ```

  with an `Error() string` reading like `shadowed by the skill of the same id at <By>`. Document
  that this is not a load failure: the file parsed fine and simply lost a collision.
- `internal/skills/catalog.go` — `set` (`:36`) takes the loaded file's absolute path and keeps an
  unexported `pathByID map[string]string` beside `byID`. On a collision it records
  `SkipError{Path: <loser's SKILL.md>, Err: ShadowedError{By: <winner's path>}}` through `addSkip`
  before replacing, then updates both maps. This also closes the same-source case — two folders in
  one dir with colliding ids — which today loses one silently, against `doc.go:23-25`'s own "soft
  must not mean silent" contract. Update `newCatalog` (`:29`) for the new map.
- `internal/skills/load.go` — `loadSkillFile` (`:119-140`) passes `abs` into `set`.
- `internal/skills/doc.go:21-25` — rewrite the Layering paragraph to the new rule, citing ADR 0032.
- `CONTEXT.md` — the **Skill** entry (~`:571`): "the later source winning a name clash" becomes the
  new rule, with the ADR 0032 link alongside the existing ADR 0027 link.
- `CHANGELOG.md` — one bullet under `## [Unreleased]`.

**Tests:** `internal/skills/load_test.go`, `internal/skills/catalog_test.go`

- Invert and rename `TestLoadWorkspaceOverridesHomeOnIDCollision` (`load_test.go:47`): the **home**
  skill must now win. Keep its "one skill, not both" assertion.
- The loser is in `Skipped()` with a `ShadowedError` (reached via `errors.As`) naming the winner's
  absolute path.
- The bare `skills/` dir still beats `.apogee/skills` on a collision, and that loser is recorded
  too — the intra-workspace order is deliberately unchanged.
- Two skill folders with the same id inside **one** source dir: one wins, the other is recorded;
  nothing is lost silently.
- `TestLoadProjectSkillsGating` (`:66`) and `TestLoadCleanScanHasNoSkips` (`:174`) still pass
  unchanged — gating is untouched and a collision-free scan records nothing.

**Acceptance:**

- `go test ./internal/skills/... -race`
- `make check`
- `grep -n "Home" internal/skills/load.go` shows the home dir appended **last** in `sourceDirs`.

**Commit:** `fix(skills): the global library wins an id collision and shadowing is recorded`

---

## 5. `/skills` names a shadowed skill as shadowed, not broken — ✅ DONE (2026-08-04)

NOTES (2026-08-04): the partition splits `skippedSkillLines` into two renderers rather than
branching inside it, so the old name is gone: `failedSkillLines` (the unchanged failure section) and
`shadowedSkillLines`, fed by `partitionSkips` over the shared `shadowedBy` predicate — one place
answers "which kind of skip is this?", so the split and the rendering cannot disagree.
`loadedSkillLines` gained an empty-list guard because `skillCatalogNote` now joins its three
sections through one loop, which would otherwise print a `0 skills available:` header.
`layout.md` was checked as the item requires and left untouched: it names `/skills` only as a verb
that reports in place (`:879`) and specifies nothing about the note's shape.

**Depends on item 4** (`ShadowedError` and the recorded pairs).

**What:** `internal/tui/skills.go`

- `skippedSkillLines` (`:133-145`) heads every skip with `N skills found but not loaded` — true of
  a malformed file, misleading for a shadowed one, which loaded fine and simply lost. Partition the
  `[]skills.SkipError` on `errors.As` against `skills.ShadowedError` and render two sections: the
  load failures under the existing heading, and the shadowed pairs under their own — e.g.
  `N skills shadowed by another of the same id:` — with the losing file on one indented line and
  the winner's path on the next, so the user can see which copy is live.
- `skillCatalogNote` (`:64-79`) joins the sections; keep the blank-line separation rule so a note
  carrying all three sections still reads as sections rather than a wall.
- `emptyCatalogLines` (`:89-104`) lists the three dirs in "the layered order `sourceDirs` walks" —
  item 4 changed that order. Re-order the lines to match and keep the annotations true: the global
  library is now the one that **wins** a clash, and the `(only when use-project-skills is on)` note
  stays on the bare `skills/` line.
- ADR 0011 applies: this item assembles `[]string` slices only — never introduce a
  `strings.Builder` held by value anywhere the TUI `Model` can reach.
- `layout.md` — check whether it specifies the `/skills` note's shape; amend that section if it
  does, otherwise leave the file untouched.
- `CHANGELOG.md` — one bullet under `## [Unreleased]`.

**Tests:** `internal/tui/skill_test.go`

- A catalog with one malformed skill **and** one shadowed pair renders both sections, each with its
  own heading, and the shadowed entry names the winner's path.
- A catalog whose only skips are shadowed pairs claims nothing "could not load".
- The existing clean-catalog and empty-catalog assertions still pass, with the empty-catalog dir
  list in the new order.

**Acceptance:**

- `go test ./internal/tui/... -race`
- `make check`

**Commit:** `feat(tui): the /skills report distinguishes shadowed skills from broken ones`

---

## Closing notes

**Suggested version bump (owner's call — no item touches `VERSION`).** The plan carries a security
fix on a Mechanism that ships in the gemma-4 validated set, plus a user-visible change to skill
precedence and to the `/skills` report. A **minor** bump (`v0.10.14` → `v0.11.0`) is the defensible
level once all five items land; a patch bump is arguable if the precedence change is read as a fix
rather than a behaviour change. Recommend deciding at archive time, not per item.

**Roadmap bookkeeping.** When this plan is archived, Wave 3 of
`docs/handoffs/2026-08-01 - 01 - merged-findings-roadmap.md` §6 is complete. What remains in that
roadmap is Wave 4+ — the G1–G6 architecture grills (C1–C11) — and the owner-run proofs in §7
(landlock ABI-1/2 on a real kernel, the Windows hard-link pass). Neither is touched here.
