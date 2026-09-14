# Refocus doc corrections (Reaction core) — plan

**Goal:** Apply the nine documentation corrections the 2026-09-13 `/refocus` run found where the docs drifted from the shipped Reaction core, plus two stale code comments it turned up. Doc and comment edits only — no behaviour changes.
**Date:** 2026-09-13
**Status:** unexecuted
**Sized for:** ~200k-context host
**Base commit:** 62a51ac5

**Sources:**
- `docs/skill-runs/refocus/2026-09-13/corrections.md` (the nine proposed fixes with their code evidence — the authoritative wording; every CURRENT string was re-verified at the base commit)
- `docs/skill-runs/refocus/2026-09-13/briefing.md` ("Does the documentation tell the truth?")
- `internal/domain/reaction.go:438-599`, `internal/agent/reactions.go:57,293-302,597-599,758-789`, `internal/agent/floorguards.go:26-29`, `internal/agent/fillnotice.go:24`, `internal/reactions/runner.go:261`, `cmd/apogee/wire_engine.go:500`, `internal/tools/console_*.go` (`domain.DefaultOffTool` assertions)
- `docs/adr/0076-*` A8, `docs/adr/0077-*`

**Ratified design calls (owner, 2026-09-13):**
- **`Runner.Replace`:** stays exported; the docs are reworded to say `SetReactions` is its only caller.
- **`reaction_fired` actions:** `fired`, `cap`, `salvage`, `notice` are documented, not narrowed out of the code.
- **`internal/floor` freeze:** a comment-only edit to `toolnames.go` is allowed; the freeze binds behaviour, not prose.

**Regression check (2026-09-13, 62a51ac5):** two reviewers over the tree at the base commit; SAFE items are not listed.
- 1: guard folded — the `go test -run 'TestContext|TestDocs'` clause is dropped from Acceptance (writer's decision: no Go test reads `CONTEXT.md`).
- 2: guard folded — Acceptance recast to bite (CURRENT strings absent, PROPOSED fragments present; writer's decision); the pin citation corrected to `fillnotice_test.go:482` (`TestContextFillNoticeIsAbsentWhenOffAndSkippedUnderBypass`).
- 3: guard folded — the rule "every doc that records `Runner.Replace` as retired or collapsed" plus its grep; `docs/design/reaction-core-greenfield.md:275` joins Files.
- 4: guard folded — the `go test -run 'TestE2ENewcomer'` clause is dropped from Acceptance (writer's decision: no Go test reads either index row).
- 6: guard folded — the interface is `domain.DefaultOffTool` (`internal/domain/tools.go:158`); `tools.DefaultOffTool` does not exist and the rewritten comment spells it right.
Re-check round (2026-09-13, 62a51ac5), same reviewers over the amended plan:
- 2: guard recast — `CONTEXT.md` wraps at ~100 columns, so Acceptance is rewritten as a paragraph-joined check (`tr '\n' ' '` then fixed-string `grep -qF` per CURRENT/PROPOSED fragment); the sweep grep stays; the implementer need not keep any fragment on one line.
- 6: guard extended — the stale `tools.DefaultOffTool` spelling is fixed everywhere in `internal/config` comments (`config.go:2326`, `options.go:352`); `internal/config/options.go` joins Files and `! grep -rq "tools\.DefaultOffTool" internal/config/` joins Acceptance.

**Standing requirements:**
- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Beads: none owned — findings come from the refocus run, not the register. Closeout writes one `CHANGELOG.md` `[Unreleased]` entry from the sidecars.
- Every item is docs/comments only: no `VERSION`, no CHANGELOG release heading, no behaviour change; `make check` is the closeout's job.

**Out of scope:** `docs/manual/headless.md` (its kinds table and `reaction_fired` row belong to `docs/plans/2026-09-13 - 00 - headless-ci-hygiene-plan.md`); unexporting `Runner.Replace`; narrowing the `reaction_fired` vocabulary; the doc-vs-doc disagreements the briefing lists as unsettled (ADR 0077 D5 vs stage-3 plan, CHANGELOG layering, historical `internal/hooks/...` paths in ADR 0075, the confinement contract's "hook" vocabulary); the undocumented gate-hint carry-over (`gate.go:298-299`); the Reaction residue beads.

## 1. CONTEXT.md — Reaction entry, `ReactionFiredEvent` actions, `Generation` swap sentence

**What:** Three edits in `CONTEXT.md`, each replacing the CURRENT string with the PROPOSED string from `corrections.md` items 1, 5 and 2 verbatim:
- lines 1181-1182 (`Reaction` entry): `Validate` no longer claims to refuse "an id another reaction already took"; duplicates are refused one level up by `Generation.Validate` per lane (same id allowed across the observe and sync lanes) and by the agent's arming step (`internal/agent/reactions.go:758-789`).
- lines 1191-1194 (`ReactionFiredEvent` action list): add `fired` (an armed reaction acted at pre-request, pre-tool-exec or history-rewrite — `internal/agent/reactions.go:57,597-599`), `cap` and `salvage` (the two Floor guards that book their own labels — `internal/agent/floorguards.go:26-29`) and `notice` (the context-fill notice — `internal/agent/fillnotice.go:24`).
- lines 1211-1212 (`Generation` sentence): the swap replaced `SetBypass` and `SetFloor`; the runner keeps `Replace` as the observe half's swap primitive, called only from the Driver's `SetReactions` (`cmd/apogee/wire_engine.go:500`).
Line numbers are the base commit's; locate by the CURRENT string, not the number. Every action word written must be the literal string the code books — copy each from the cited constant, never from memory.
**Regression guard.** Drop the `go test -count=1 -run 'TestContext|TestDocs' ./cmd/apogee/` clause from Acceptance — no Go test reads CONTEXT.md, the grep clauses are the whole check.
**Files:** `CONTEXT.md`
**Tests:** none in Go (prose only). Guard by grep in Acceptance.
**Acceptance:** `! grep -n "an id another reaction already took" CONTEXT.md && grep -c "Generation.Validate" CONTEXT.md && grep -n "salvage" CONTEXT.md | grep -q "cap" && ! grep -n "and the runner's own \`Replace\`" CONTEXT.md && grep -q "called only from the Driver's \`SetReactions\`" CONTEXT.md`
**Commit:** `docs(context): Reaction.Validate, reaction_fired actions and Runner.Replace wording match the shipped core`

## 2. CONTEXT.md — the three Bypass sentences name the context-fill notice

**What:** Replace the three CURRENT Bypass strings (`corrections.md` item 4: lines 10-11, 771-775, 1164-1165) with their PROPOSED strings verbatim: Bypass also switches off the engine's one advise builtin, the context-fill notice (ADR 0077); the seven Floor guards, observe and gate stay on. Authority: `internal/agent/reactions.go:293-302` (`bypassSkips` applied to builtins of class advise), pinned by `internal/agent/fillnotice_test.go:482` (`TestContextFillNoticeIsAbsentWhenOffAndSkippedUnderBypass`, the "Bypass on" case at :489). Rule for the sweep: every sentence in `CONTEXT.md` that says Bypass leaves the engine's builtins on or intact, or that Bypass switches off user/bench advise "and nothing else", is rewritten to name the notice — find them with `grep -n -i "bypass" CONTEXT.md | grep -i "builtin\|nothing else\|intact"`; `CONTEXT.md:1266` already reads correctly and is the model for the wording.
**Regression guard.** Acceptance must bite, and `CONTEXT.md` wraps at ~100 columns, so line-wise greps on multi-word fragments are unreliable (the `structure intact` CURRENT clause already spans two lines at the base commit) — Acceptance is a paragraph-joined check: `J="$(tr '\n' ' ' < CONTEXT.md | tr -s ' ')"` then, against `$J`, the three CURRENT strings absent (`builtins, **observe** and **gate** on)`, `leaving the engine's own builtins and the agent's structure intact`, `of user and bench-armed origin and nothing else`) and the three PROPOSED fragments present (`the engine's own advise builtin — the context-fill notice — with them`, `the engine's one advise builtin, the **Context-fill notice**`, `plus the engine-origin context-fill notice (the one advise builtin)`), each via `printf '%s' "$J" | grep -qF -- '<fragment>'` (fixed-string, so the `**` and `(` need no escaping); the sweep grep from What is kept. The implementer need not keep any fragment on one line — the joined check tolerates wrapping. The pin for the notice being off under Bypass is `internal/agent/fillnotice_test.go:482` (the "Bypass on" case at :489), not the rollback-resume test at :403-418 nor `TestE2EAdviseIsOffUnderBypass` (which pins user advise off); `internal/agent/reactions.go:296-301` stays the code authority.
**Files:** `CONTEXT.md`
**Tests:** none in Go (prose only).
**Acceptance:** `J="$(tr '\n' ' ' < CONTEXT.md | tr -s ' ')" && ! printf '%s' "$J" | grep -qF -- 'builtins, **observe** and **gate** on)' && ! printf '%s' "$J" | grep -qF -- "leaving the engine's own builtins and the agent's structure intact" && ! printf '%s' "$J" | grep -qF -- 'of user and bench-armed origin and nothing else' && printf '%s' "$J" | grep -qF -- "the engine's own advise builtin — the context-fill notice — with them" && printf '%s' "$J" | grep -qF -- "the engine's one advise builtin, the **Context-fill notice**" && printf '%s' "$J" | grep -qF -- 'plus the engine-origin context-fill notice (the one advise builtin)' && test "$(grep -n -i 'bypass' CONTEXT.md | grep -i 'builtin\|nothing else\|intact' | grep -v -i 'context-fill\|advise builtin' | wc -l)" -eq 0`
**Commit:** `docs(context): the three Bypass sentences say the context-fill notice is off under Bypass too`

## 3. ADR 0076 A8 — `SetReactions` is `Runner.Replace`'s only caller

**What:** In `docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md` A8 (lines 305-306) replace the CURRENT string with the PROPOSED string from `corrections.md` item 3 verbatim: `SetReactions(gen)` retires `SetBypass` and `SetFloor` and becomes the only caller of `Runner.Replace` (kept as the observe lane's swap primitive). Edit the amendment text in place — no separate amendment note; the ratified call is that the docs were wrong, not the decision.
**Regression guard.** The item is a rule, not a closed list of one site: every doc that records `Runner.Replace` as retired or collapsed is reworded the same way — find them with `grep -rn "Runner.Replace" docs CONTEXT.md` (hits at base: ADR 0076:306, `docs/design/reaction-core-greenfield.md:34` [historical inventory, leave] and greenfield:275 [Status: Decided, not archived — its §9.1 row still lists `Runner.Replace` beside `SetBypass`/`SetFloor` as collapsed into "one generation swap — delivered", which the ADR would then contradict]). Rewrite the greenfield:275 row so `Runner.Replace` reads as kept (the observe lane's swap primitive, called only by `SetReactions`) while `SetBypass`/`SetFloor` stay delivered as collapsed; archived plans are historical and stay.
**Files:** `docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md`, `docs/design/reaction-core-greenfield.md`
**Tests:** none in Go (prose only).
**Acceptance:** `grep -q "becomes the only caller of \`Runner.Replace\`" "docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md" && ! grep -q "\`Runner.Replace\` together" "docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md"`
**Commit:** `docs(adr): 0076 A8 — SetReactions is Runner.Replace's only caller, not its retirement`

## 4. Manual index rows describe all three `reactions:` entry kinds

**What:** Replace the two Reactions index rows with the PROPOSED strings from `corrections.md` items 6 and 7 verbatim: `README.md:252` (repo front door) and `docs/manual/README.md:12` (manual index) both name `run:`, `advise:` and `gate:`, the Moments (the manual row says "sixteen Moments", not "eleven notices" — `internal/domain/reaction.go:68-88`), and migrating from `hooks:`. Keep each row a single table line.
**Regression guard.** Drop the `go test -count=1 -run 'TestE2ENewcomer' ./cmd/apogee/` clause from Acceptance — no Go test reads either index row; the grep clauses are the whole check.
**Files:** `README.md`, `docs/manual/README.md`
**Tests:** none in Go (prose only).
**Acceptance:** `grep -q "advise:" README.md && grep -n "Reactions" README.md | grep -q "gate:" && grep -n "Reactions" docs/manual/README.md | grep -q "sixteen Moments" && ! grep -q "eleven notices" docs/manual/README.md`
**Commit:** `docs(readme): the Reactions index rows cover run:, advise: and gate:`

## 5. Archived stage-3 and context-fill plans read `Status: executed`

**What:** Flip the header status on the two archived plans (`corrections.md` items 8-9): `docs/plans/archived/2026-09-09 - 01 - reaction-core-stage-3-plan.md` line 5 `**Status:** unexecuted` → `**Status:** executed (16/16, archived 2026-09-12)`; `docs/plans/archived/2026-09-09 - 02 - context-fill-notice-plan.md` line 5 `**Status:** unexecuted` → `**Status:** executed (8/8, archived 2026-09-13)`. Nothing else in either file changes.
**Files:** `docs/plans/archived/2026-09-09 - 01 - reaction-core-stage-3-plan.md`, `docs/plans/archived/2026-09-09 - 02 - context-fill-notice-plan.md`
**Tests:** none.
**Acceptance:** `! grep -l "Status:\*\* unexecuted" "docs/plans/archived/2026-09-09 - 01 - reaction-core-stage-3-plan.md" "docs/plans/archived/2026-09-09 - 02 - context-fill-notice-plan.md" && git diff --numstat HEAD -- docs/plans/archived/ | awk '{ if ($1 != 1 || $2 != 1) exit 1 }'`
**Commit:** `chore(plans): archived stage-3 and context-fill plans carry Status: executed`

## 6. `toolsConfig.Enabled` comment — the Console four ship default-off

**What:** In `internal/config/config.go` the `Enabled` field comment (base lines 2325-2328) says "Absent/empty ⇒ nothing is added back, which is today's whole menu since no tool ships default-off". The Console four (`console_open`, `console_send`, `console_read`, `console_close`) assert `domain.DefaultOffTool` (`internal/tools/console_*.go`) and are documented as default-off in `docs/manual/configuration.md:125-129`. Rewrite the sentence: absent/empty ⇒ nothing is added back, so a default-off tool (today the Console four, `domain.DefaultOffTool`) stays off until named here. Comment only; no code, no test change.
**Regression guard.** Spell it `domain.DefaultOffTool` in the rewritten sentence — the interface lives at `internal/domain/tools.go:158` and every Console assertion uses `domain.`; `tools.DefaultOffTool` is not a symbol. Rule: the stale spelling `tools.DefaultOffTool` is fixed everywhere it appears in `internal/config` comments, found with `grep -rn "tools\.DefaultOffTool" internal/config/` (at the base commit: `config.go:2326` and `options.go:352`); Acceptance checks the sweep left none.
**Files:** `internal/config/config.go`, `internal/config/options.go`
**Tests:** none new; the existing `TestApplyConfigToolsEnabled` stays green.
**Acceptance:** `! grep -q "since no tool ships default-off" internal/config/config.go && ! grep -rq "tools\.DefaultOffTool" internal/config/ && gofmt -l internal/config/ | wc -l | grep -q '^0$' && go vet ./internal/config/ && go test -race -count=1 -run 'TestApplyConfigTools' ./internal/config/`
**Commit:** `docs(config): toolsConfig.Enabled comment names the default-off Console four`

## 7. `internal/floor/toolnames.go` comment no longer says content-repair rows "stay lab Mechanisms"

**What:** `internal/floor/toolnames.go:53` reads "only the content-repair rows (which stay lab Mechanisms) need the narrower question" — the Mechanism layer was deleted (ADR 0071/0076). Rewrite the parenthetical so the sentence stands without the layer: the narrower "does this call carry a full file body" question belonged to the retired content-repair rows and no Floor guard asks it. Comment only (ratified call: the `internal/floor` freeze binds behaviour, not prose). Rule for the sweep, this file only: every comment in `internal/floor/toolnames.go` that names a Mechanism as a live layer — `grep -n -i "mechanism" internal/floor/toolnames.go`; hits in other `internal/floor` files are out of scope (they are historical references the retirement wave exempted).
**Files:** `internal/floor/toolnames.go`
**Tests:** none new.
**Acceptance:** `! grep -q "stay lab Mechanisms" internal/floor/toolnames.go && gofmt -l internal/floor/ | wc -l | grep -q '^0$' && go test -race -count=1 ./internal/floor/ && git diff --numstat HEAD -- internal/floor/ | awk '{ if ($1 > 3 || $2 > 3) exit 1 }'`
**Commit:** `docs(floor): toolnames.go comment stops naming the retired Mechanism layer`
