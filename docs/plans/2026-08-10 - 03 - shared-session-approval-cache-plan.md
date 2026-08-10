# Shared session allow-for-session cache — plan

- **Goal:** "allow for session" means the whole Session: one shared allow-cache for the
  entire agent tree, so sub-agents stop re-prompting for approvals the human already
  granted (ISSUES.md item "sub agents need repeated approval in auto mode"). Includes
  twin coalescing: a queued approval whose key was allowed while it waited auto-clears.
- **Date:** 2026-08-10
- **Status:** unexecuted
- **Sized for:** ~200k-context host
- **Authoritative sources:** repo @ `178fcb7`; `internal/agent/dispatch.go` `approve()`
  (~line 553) and its force-semantics comment (~line 580); `internal/agent/construct.go`
  `queuedApprover`/`queuedApprovals` (~lines 160–212); `internal/agent/subagent.go`
  `newChildAgent` doc (~lines 126–158); `internal/agent/agent.go:161` (`approved` field);
  `internal/agent/resolution.go` `gateCacheKey` (~line 423) and demote-fallback cacheKey
  (~line 487); `internal/domain/approval.go:33` (`ApprovalRequest`); ADR 0013 ("starts
  fresh … no approval-cache" sentence, ~line 60); ADR 0039 (siblings run concurrently).
- **Ratified design calls** (owner via AskUserQuestion, 2026-08-10, unless noted):
  1. Fix NORMAL gates only. Forced gates (Tier-2 speed-bumps, runtime demotes) keep
     skipping the cache in BOTH directions, by design.
  2. ONE shared session-scoped cache for the whole agent tree; every agent reads AND
     writes it; mutex-guarded (siblings run concurrently, ADR 0039). An allow anywhere
     clears prompts everywhere and survives the allowing sub-agent's death. In-memory
     only — never persisted.
  3. Twin coalescing IS in scope: a queued approval re-checks the cache after acquiring
     the prompt slot and auto-clears without prompting if its key was allowed meanwhile.
  4. Key transport: new `CacheKey string` field on `domain.ApprovalRequest`; empty means
     "this decision can never be remembered" (forced gates). Host-visible on purpose.
  5. An auto-cleared twin EMITS the normal ApprovalEvent (decision AllowForSession) — a
     visible transcript trace of why the second prompt never appeared. No sentinel.
  6. Cache home: the `queuedApprover` seam — already the one object shared by the whole
     tree via the `queuedApprovals` idempotence check. The seam owns all cache WRITES
     and the twin re-check; dispatch keeps a silent READ fast path (no event on a hit,
     matching today). (Plan author, 2026-08-10 — mechanical consequence of call 2.)
- **Standing requirements:** `skills: coding-standards`. Run `make check` before each
  commit. Any authorized deviation from item text lands as a dated NOTES line under the
  item. Never change VERSION or add a CHANGELOG release heading.
- **Out of scope:** forced-gate semantics (unchanged); TUI greying-out "allow for
  session" on forced gates (CacheKey makes it possible later — new ISSUES entry if
  wanted); persisting allows across session save/restore; `ask_user` questions (no
  allow-for-session concept); the other ISSUES.md items (keyboard collapse mode,
  out-of-workspace write reconciliation).

## 1. Session allow-cache in the approval queue seam, with twin coalescing

**What:**
- `internal/domain/approval.go`: add `CacheKey string` to `ApprovalRequest` (design
  call 4). Doc it: the allow-for-session identity of this request; empty = the decision
  cannot be remembered (a forced gate); engine-populated; hosts may ignore it.
- New file `internal/agent/approvalcache.go`: unexported `approvalCache` — `sync.Mutex`
  plus `map[string]bool`, methods `Allowed(key string) bool` and `Allow(key string)`
  (no-op on empty key), constructor or lazy init. Nil-receiver-safe: a nil
  `*approvalCache` answers false/no-ops, so unwrapped test rigs degrade to per-call
  prompting instead of panicking.
- `internal/agent/construct.go`: `queuedApprover` gains a `cache *approvalCache` field;
  `queuedApprovals` creates it when wrapping. The existing idempotence check is what
  keeps one cache per agent tree — a child's construction reuses the parent's wrapper.
  In `Approve`, after `slot.Acquire` succeeds: if `req.CacheKey != ""` and
  `cache.Allowed(req.CacheKey)`, return `ApprovalAllowForSession` WITHOUT calling the
  inner Approver — the twin path (design calls 3 and 5; the caller's normal
  ApprovalEvent emission is the visible trace). After the inner Approver returns
  `ApprovalAllowForSession` and `req.CacheKey != ""`, call `cache.Allow(req.CacheKey)` —
  the seam owns all writes (design call 6).
- This item does NOT touch dispatch: nothing populates `CacheKey` yet, so live behavior
  is unchanged until item 2 lands. Build must stay green.

**Tests** (extend `internal/agent` seam tests, e.g. beside `approvalqueue_test.go`):
- Twin coalesce: two concurrent `Approve` calls with the SAME `CacheKey`; the scripted
  inner approver answers the first with AllowForSession → the inner approver sees
  exactly ONE request; both callers get an allow verdict.
- Distinct keys → both prompt (two inner requests).
- Empty `CacheKey` → never cached: repeated calls all reach the inner approver, and an
  AllowForSession answer writes nothing.
- Plain `ApprovalAllow` writes nothing (a later same-key call still prompts).

**Acceptance:**
- `go test ./internal/agent/ -count=1` passes.
- `go test -race ./internal/agent/ -count=1` passes.
- `make check` passes.

**Commit:** `feat(agent): session-scoped allow-for-session cache in the approval queue seam`

## 2. Route dispatch through the shared cache; delete the per-Agent map

Depends on item 1.

**What:**
- `internal/agent/approvalcache.go` (or `construct.go`): helper
  `sessionAllows(ap domain.Approver) *approvalCache` — type-asserts `*queuedApprover`
  and returns its cache; nil Approver or any other type → nil (safe via the
  nil-receiver methods from item 1).
- `internal/agent/dispatch.go` `approve()`:
  - Fast path becomes `if !force && sessionAllows(a.cfg.Approver).Allowed(cacheKey)` —
    still silent (no ApprovalEvent on a hit), matching today's cached fast path.
  - Populate `areq.CacheKey`: `cacheKey` when `!force`, `""` when `force` — this single
    mapping is what keeps design call 1 true (forced gates never read NOR seed the
    cache, since the seam keys everything off `CacheKey`). The `resolution` structs are
    NOT touched — the demote fallback may keep carrying a cacheKey with `force` set
    (resolution.go ~487); the force→empty mapping lives here only.
  - Delete the local AllowForSession write block — the seam writes now (design call 6).
    Preserve the WHY prose of the ~line 580 comment (a forced allow-for-session behaves
    as a plain allow) wherever it reads naturally now.
- `internal/agent/agent.go`: delete the `approved` field (line 161) and any lazy init.
  Scouted: its only readers/writers are the four `dispatch.go` lines — nothing persists
  or restores it.

**Tests** (drive real Agent trees with a recording approver, patterned on
`approvalqueue_test.go` / `delegationname_test.go` fixtures):
- Parent answers a gate "allow for session"; a sub-agent then hits a gate with the same
  cache key → the recording approver sees NO second request; the child's call runs.
- A sub-agent's "allow for session" clears the same key for the PARENT and for a LATER
  sibling (write-back survives the child).
- A forced gate prompts EVERY time and never seeds the cache (an allow-for-session on
  it does not pre-clear a later ordinary gate under the same key) — keep/adapt any
  existing test pinning this.
- Full existing suite passes unmodified in intent (adapt tests that reached into
  `a.approved` directly, if any exist).

**Acceptance:**
- `go test ./internal/agent/ -count=1` passes.
- `go test -race ./... -count=1` passes.
- `make check` passes.

**Commit:** `fix(agent): sub-agents share the session allow-for-session approval cache`

## 3. Amend the docs that pinned the old boundary; close the ISSUES entry

Depends on item 2.

**What:**
- `internal/agent/subagent.go` `newChildAgent` doc (~lines 139–140): the child is no
  longer "NOT given … approval cache" — reword: conversation and pending input stay
  withheld (ADR 0008 statelessness boundary); the allow-for-session cache is
  session-scoped and reaches the child through the shared approver seam.
- ADR 0013 (~line 60, the "starts fresh — … no parent
  conversation/pending-input/approval-cache" sentence): amend the sentence and add a
  dated note — `Amended 2026-08-10:` the allow-for-session cache is now session-scoped,
  shared tree-wide through the approver queue seam (see plan
  `2026-08-10 - 03`); conversation/pending-input isolation unchanged. Scouted: ADR 0008
  itself and CONTEXT.md have no allow-for-session/approval-cache mentions — re-grep both
  (`grep -rni "allow.for.session\|approval cache" docs/adr/0008* CONTEXT.md`) and update
  only if the grep hits.
- `ISSUES.md`: remove the "sub agents need repeated approval in auto mode" line.
- `CHANGELOG.md` under `[Unreleased]`: Fixed — "allow for session" now covers the whole
  agent tree; sub-agents no longer re-prompt, and a queued duplicate approval
  auto-clears once its key is allowed. No release heading, no VERSION change.

**Tests:** none (docs only).

**Acceptance:**
- `grep -c "approval" internal/agent/subagent.go` — the reworded comment present; the
  literal "NOT given" claim about the approval cache gone
  (`! grep -n "approval cache" internal/agent/subagent.go | grep -i "not given"`).
- `grep -n "Amended 2026-08-10" "docs/adr/0013-the-sub-agent-orchestrator-is-the-recursion-point-with-isolated-live-guard-state.md"` hits.
- `! grep -n "repeated approval in auto mode" ISSUES.md` (line removed).
- `grep -n "allow for session" CHANGELOG.md` hits under `[Unreleased]`.
- `make check` passes.

**Commit:** `docs(agent): session-scoped approval cache — amend ADR 0013, close ISSUES entry`

## Suggested version bump

Micro bump (user-visible fix) once shipped — owner's call, not performed by this plan.
