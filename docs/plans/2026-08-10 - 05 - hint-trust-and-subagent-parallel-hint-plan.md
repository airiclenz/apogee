# Hint trust + sub_agent parallel-dispatch hint — implementation plan

**Goal:** close the two ISSUES.md entries logged 2026-08-10: (1) discovery silently
swaps an unadvertised model hint for `models[0]`, which blocks OpenRouter variant
slugs like `deepseek/deepseek-v4-pro:exacto`; (2) the `sub_agent` tool description
never tells the model it may emit several calls in one reply, leaving the ADR 0039
fan-out unexercised under models that don't batch tool calls on their own.

**Date:** 2026-08-10 · **Status:** not started · **Sized for:** ~200k-context host

**Authoritative sources**
- `ISSUES.md` — the two 2026-08-10 entries (this plan's mandate; each owning item
  deletes its entry).
- `internal/provider/discovery.go` — `toModelInfo` at :192, silent fallback
  `active := models[0]` at :214 (current-behavior ground truth).
- `internal/tools/sub_agent.go` — model-facing `description:` string at :23.
- ADR 0039 (delegations fan out concurrently, bounded by the server's
  parallel-agents cap; engine seam `internal/agent/agent.go:102`).
- Live evidence, 2026-08-10: OpenRouter accepts `deepseek/deepseek-v4-pro:exacto`
  on chat/completions but never lists variant slugs in `/v1/models`; session
  `20260810T191816Z-4b0f8f6c` shows every code-audit phase dispatched
  one-call-per-message.

**Ratified design calls** (owner via AskUserQuestion, 2026-08-10)
- **Hybrid hint resolution:** exact match first; else match the base slug before the
  first `:` (variant inherits the base entry's context window and display name);
  else trust the configured id as-is with context window unknown (Budget and
  auto-compaction then inactive, exactly as when discovery reports no window). In
  every non-empty-hint case the FULL configured id is what goes on the wire — the
  base-slug match informs window/display only. An empty advertised list with a
  non-empty hint trusts the hint (window unknown). `models[0]` fallback survives
  ONLY for an empty hint. A startup notice fires whenever resolution is not exact.
- **Sequencing gate:** the `sub_agent` description item must not run until the
  tool-display overhaul (`docs/plans/2026-08-10 - 04 - tool-display-overhaul-plan.md`)
  is archived — owner directive to avoid simultaneous work near `internal/tools`
  (items 1–2 touch only `internal/provider`/wiring and may run before it).
- **Startup notice is required**, ratified in the ISSUES entry text ("should at
  least print a startup notice").

**Standing requirements**
- skills: coding-standards
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope**
- Anything in the tool-display overhaul's territory (TUI tool presenters,
  `internal/tui`).
- Picker cosmetics for a bound-but-unadvertised model id (`internal/tui/picker.go:224-233`
  excludes by identity, so the actually-serving base model still lists as a
  switch target) — cosmetic, TUI territory, deliberately untouched while the
  overhaul is in flight.
- OpenRouter provider-routing request-body parameters (`provider:` object) or any
  other per-provider wire extension.
- Further Mechanisms nudging small models toward parallel dispatch (only the one
  description sentence ships here).
- VERSION / CHANGELOG release headings / tags (see closing note).

## 1. Hybrid hint resolution in toModelInfo

**What:** Implement the ratified hybrid resolution in
`internal/provider/discovery.go` `toModelInfo` (func at :192; today's silent
fallback at :214):

1. `hint == ""` → first advertised entry (unchanged).
2. Exact id match → that entry (unchanged).
3. Else, base-slug match: strip the hint at the first `:` and match that against
   advertised ids; on a hit, `ActiveModel` = the FULL hint, `ContextWindow` (and
   display name, where surfaced) from the matched base entry.
4. Else, trust: `ActiveModel` = the full hint, `ContextWindow` = 0 (unknown).
5. Empty advertised list with a non-empty hint → as (4), instead of today's
   early-return with no active model.

Add a resolution-grade field to `ModelInfo` (values covering: exact, base-slug,
trusted, first-advertised; naming/shape is implementer latitude under the
forwarded coding standards) so item 2 can decide whether to emit the notice
without re-deriving the match. No behavior change for callers beyond the new
field and the ratified resolution.

Binding notes (from the 2026-08-10 scout of the consumer chain):
- **Fixed point required:** `wire_verbs.go:44-50` restates the hint after every
  commit, and the heartbeat re-runs discovery each beat — the same hint against
  the same advertised list must always resolve to the same `ActiveModel`
  (guaranteed here because `ActiveModel` always carries the FULL hint), or the
  binding observer produces a rebind ping-pong every beat.
- **`setRuntimeContextWindow` (`discovery.go:167-173`)** syncs the
  `AvailableModels` entry matching `ActiveModel`; with a trusted/base-slug
  `ActiveModel` that scan no-ops. Confirm the top-level `ContextWindow` still
  takes the `/props` value in that case and cover it in a test; do not force the
  list sync.
- **Fail-loud is intended:** a genuinely wrong configured id now errors on each
  completion request instead of silently running `models[0]`; the item 2 notice
  is the explanation surface. Do not add a reachability probe here.
- **Keying consequence (document in the CHANGELOG bullet of item 2):** per-model
  config (`system-prompt-models`, validated-set identity, `probe model` records)
  keys on the resolved id, which is now the full configured id — users write
  per-model entries against the id they configured.

**Tests:** `internal/provider/discovery_test.go` has no `toModelInfo`-level
unit test or shared table — every case runs through `Discover` against the
`modelsServer` helper (`discovery_test.go:14-26`; hint case precedent at :83).
Add new `TestDiscover_*` functions (or subtests) the same way: exact match;
variant suffix inherits the base entry's window and keeps the full id active;
wholly unlisted hint trusted with window 0; empty advertised list with a hint;
empty hint falls back to first advertised; `/props` runtime window still lands
on a trusted `ActiveModel`. Each case asserts the resolution grade.

**Acceptance:** `go vet ./internal/provider/...` clean;
`go test -race -count=1 ./internal/provider/...` passes and includes the new
cases.

**Commit:** `fix(provider): trust an unadvertised model hint instead of swapping in models[0]`

## 2. Startup notice for non-exact hint resolution; close the ISSUES entry

Depends on item 1.

**What:** Emit ONE line when the resolution grade from item 1 is base-slug or
trusted (exact and first-advertised stay silent). The seam (2026-08-10 scout):
notices produced host-side in `rebindSpecFor` (`cmd/apogee/wire_settings.go:704-732`,
where the ADR 0016 validated-set one-liners already join the `notices` slice)
ride `tui.RebindResult.Notices` (`internal/tui/tui.go:770-774`) and land as
transcript notes at `internal/tui/model.go:1797-1805` — the same path as
`context window changed: unknown → 1M`. Plumbing the item-1 resolution grade
from `ModelInfo` to that seam (via the holder/monitor it already consults) is
implementer latitude; the notice text and firing condition are binding. No edits
inside `internal/tui` — the existing Notices plumbing already delivers the line. The line must name the configured id and the
window consequence; wording latitude within that, e.g.:

- base-slug: `model 'deepseek/deepseek-v4-pro:exacto' not advertised; using it as configured (context window from base 'deepseek/deepseek-v4-pro': 1M)`
- trusted: `model 'my-alias' not advertised; using it as configured (context window unknown — Budget and auto-compaction inactive)`

Also in this item: delete the discovery entry ("discovery silently swaps an
unadvertised model hint…") from `ISSUES.md`, and add a `CHANGELOG.md`
`[Unreleased]` bullet for the fix (bullet only — no release heading, no VERSION
change).

**Tests:** a unit test at the emitting seam asserting the note fires for
base-slug/trusted and stays silent for exact/first-advertised (shape follows how
the existing 'context window changed' note is tested; if that seam has no test
precedent, test the note-decision function directly).

**Acceptance:** `go build ./...` clean;
`go test -race -count=1 ./...` passes;
`! grep -q "silently swaps an unadvertised model hint" ISSUES.md`;
`grep -qi "unadvertised model hint\|not advertised" CHANGELOG.md`.

**Commit:** `feat(provider): startup notice when the configured model is not advertised`

## 3. Gate — tool-display overhaul archived

Depends on item 2. Verification-only gate item; it exists so item 4 cannot start
while the tool-display overhaul is still in flight (ratified sequencing call).

**What:** Verify the tool-display overhaul plan is finished and archived. No file
changes. If the check fails, the item is BLOCKED — the run stops here and this
plan is re-invoked later; items 1–2 stay landed.

**Tests:** none (verification-only).

**Acceptance:**
`test -f "docs/plans/archived/2026-08-10 - 04 - tool-display-overhaul-plan.md" && ! test -f "docs/plans/2026-08-10 - 04 - tool-display-overhaul-plan.md"`

**Commit:** none — nothing to commit; the verifier marks the item done on a
passing check alone.

## 4. sub_agent description: invite concurrent same-reply delegations; close the ISSUES entry

Depends on item 3.

**What:** In `internal/tools/sub_agent.go` (description string at :23), append
this sentence to the model-facing `description:` — binding text, verbatim:

> "You may call sub_agent several times in a single reply; sibling delegations
> run concurrently, so dispatch independent sub-tasks together in one reply
> rather than one per turn."

Schema, tool name, and everything else in the file unchanged. Update any test
asserting the old description text (grep for "Delegate a focused sub-task"
first). Also in this item: delete the sub_agent entry ("never tells the model it
MAY emit several calls…") from `ISSUES.md`, and add a `CHANGELOG.md`
`[Unreleased]` bullet.

**Tests:** a unit test in `internal/tools` asserting the description contains
"several times in a single reply" (regression guard for the sentence).

**Acceptance:** `go test -race -count=1 ./internal/tools/...` passes;
`grep -q "several times in a single reply" internal/tools/sub_agent.go`;
`! grep -q "never tells the model it MAY emit several calls" ISSUES.md`.

**Commit:** `feat(tools): sub_agent description invites concurrent same-reply delegations`

## Suggested version bump

No item changes VERSION. Per the house convention (micro-bump per shipped
feature), a micro bump is warranted after item 2 lands and another after item 4
(or one combined bump if both land in one sitting) — whether and when to bump is
the owner's call.
