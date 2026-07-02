# Plan — Collapse the dispatch decision into one Resolution verdict

**Date:** 2026-07-02
**Status:** READY — not started. Items are ordered; work them in sequence, one commit per item.
**Source:** architecture-review candidate #1 (`docs/architecture-review-20260629-110828.html`),
design settled in a `grill-with-docs` session on 2026-07-02. **All design calls are
pre-resolved** — the Design record below is authoritative; no item should need a
needs-design-call escalation. Two docs were already updated during the design session:
`CONTEXT.md` (new **Resolution** glossary entry) and ADR 0013 (the "guard tiers at the
recursion point" clarification).
**Track:** post-`v1.0.0` in-process refactor + one small ADR 0012 conformance fix. Items 1–2
are strictly behavior-preserving; item 3 is the single deliberate behavior change.
**Standing requirement:** `/coding-standards` (Go + testing variants) is mandatory for all new
code — invoke `implement-plan` with `coding-standards` forwarded. Apogee is pre-production:
commit direct to `main`, no PRs. `go test ./...`, `go test -race ./...`, `gofmt`, `go vet`
green are the gate on every item.

---

## Design record (the grilled decisions — do not re-derive)

The problem: `disposition.go` claims to be "the single place the autonomy ladder becomes
code," but **seven** verdict-changing rules live outside its table, spread across
`internal/agent/dispatch.go`:

1. Unknown tool → error result (`dispatch.go` `lookupTool` failure path)
2. Guard refuse — Tier-1 dangerous action / tripped breaker (`guards.PreExecute`)
3. The `sub_agent` recursion point, recognized between guard and ladder
4. Tier-2 force-approval upgrading a non-refuse disposition to a gate
5. Nil-Approver-while-gated → refuse (inside `approve()`)
6. Forced gates ignoring the allow-for-session cache (inside `approve()`)
7. Runtime `ErrConfinementUnavailable` → demote to a *forced* gate → re-run unconfined
   (`executeWithApprovalFallback`)

The decisions (D1–D8):

- **D1 — Term.** **Resolution** is the complete per-call verdict, computed in full before
  anything executes. "Disposition" is **retired from code**; in ADR/contract prose it survives
  only as the historical name of the post-guard ladder stage. See the `CONTEXT.md`
  **Resolution** entry.
- **D2 — Kinds.** `Run` · `Confine` · `Gate` · `Refuse` · `Delegate`. Unknown-tool, Plan-mode
  write, depth-bound, guard Tier-1, and nil-Approver-while-gated are all `Refuse` rows.
- **D3 — Guard tiers at the recursion point (ADR 0013 clarification, already committed to the
  ADR).** Tier-1 refuses the delegation call itself (fail fast). Tier-2 force-approval **never**
  gates a `Delegate` — nothing executes at delegation; the shared read-only floor re-fires on
  the child's actual dangerous call, which threads the parent's Approver. A rule of the table,
  pinned by a test row — not an accident of check ordering.
- **D4 — Runtime demote is a precomputed contingency.** Every `Confine` verdict carries one
  **bounded** fallback: a **forced** `Gate` (reason: confinement unavailable at run time) whose
  allow-continuation is *re-run unconfined*. Nil Approver ⇒ the fallback is `Refuse` instead.
  A fallback never carries another fallback. The executor follows the plan; it never decides.
- **D5 — Gate contract.** `Gate` carries `Force` (skip the allow-for-session cache), `Reason`
  (the human-facing why — today's `approvalReason()` mapping), and `CacheKey` (the
  allow-for-session cache key). Nil-Approver folds into `resolve()`: if the ladder says gate
  and no Approver is configured, the verdict is `Refuse("approval required but no Approver
  configured")` — `Gate` always means the Approver will actually be consulted. The
  **allow-for-session cache itself stays executor-side** (a memoized human answer, not verdict
  logic; a cache-hit is audit-distinct from a `Run`).
- **D6 — Shape.** A hermetically **pure** `resolve(in resolutionInput) Resolution` free
  function in a new `internal/agent/resolution.go`, all unexported (zero public-API/semver
  impact). Dispatch gathers facts; the one I/O-tainted fact — write-target-in-workspace
  (`EvalRealPath` touches disk) — is **precomputed** by dispatch and passed as a bool field.
  Box construction folds into the verdict (`Confine` carries the `domain.ConfinementBox`);
  `executeTool` stops building it.
- **D7 — Docs.** **No new ADR** (reversible, local). `docs/design/confinement-execution-contract.md`
  **§4 is amended in place** (keeping the section number — code comments cite "§4" rows),
  riding item 2's commit so the contract never describes a mechanism that doesn't exist.
- **D8 — Verification discipline.** Items 1–2 are behavior-preserving; the **existing tests are
  the oracle** and must pass **unmodified** except mechanical renames of the deleted `dispo*`
  identifiers (the three files that reference them: `internal/agent/dispatch_test.go`,
  `guardrails_test.go`, `setmode_test.go`). The new exhaustive table test lands *alongside*
  them; consolidation/deletion of redundant old tests is explicitly **out of scope** for this
  plan. Item 3 (`CacheKey` = server grain for MCP) is the only behavior change, with its own
  test.

Reference pointers (read before implementing): `internal/agent/dispatch.go`,
`internal/agent/disposition.go`, `internal/security/guard.go`, `internal/agent/subagent.go`
(depth bound at `runSubAgent`), `docs/design/confinement-execution-contract.md` §4,
ADR 0012, ADR 0013 (incl. the 2026-07-02 clarification), `CONTEXT.md` → **Resolution**.

---

## 1. Add the Resolution type, the pure resolver, and the exhaustive table test (additive — nothing rewired)

**What:** create `internal/agent/resolution.go` and `internal/agent/resolution_test.go`. Pure
addition: no existing file changes, no behavior changes. Dispatch keeps running the old path;
the new resolver is exercised only by its own test.

**The type (unexported):** a `resolution` struct with a `kind` enum (`resolveRun`,
`resolveConfine`, `resolveGate`, `resolveRefuse`, `resolveDelegate` — naming per
coding-standards; do NOT reuse the `dispo*` names) and fields per D4/D5:

- `reason string` — model-facing refusal text for `Refuse`; human-facing Approval prompt
  reason for `Gate`.
- `force bool` — `Gate` only: skip the allow-for-session cache (Tier-2 or runtime-demote).
- `cacheKey string` — `Gate` only: the allow-for-session key. **In this item it is always the
  tool name** (today's exact semantics; item 3 changes it for MCP).
- `box domain.ConfinementBox` — `Confine` only.
- `fallback *resolution` — `Confine` only: the precomputed runtime-demote contingency
  (D4). Structurally bounded: a fallback's own `fallback` is always nil.
- Audit metadata the executor needs to preserve today's observable behavior: carry the guard's
  `security.AuditDecision` + guard reason (or the whole `security.PreCheck`) on the resolution
  so the executor's `recordBlocked`/`recordExecuted` calls stay byte-identical. Note the
  current quirks that must survive: an unknown-tool refusal and a Plan-mode write refusal are
  **not** audit-recorded today; a guard refusal is; a deny-by-approver records with the
  guard's (pass-through) audit decision.

**The input (unexported):** `resolutionInput` with explicit facts — the effective mode
(`domain.Mode`), the `domain.ToolCall`, the resolved `domain.Tool` (nil ⇒ unknown tool), the
guard's `security.PreCheck`, `confineToWorkspace bool`, `fsConfineAvailable bool`,
`writeTargetInWorkspace bool` (precomputed — the resolver must do **no** I/O),
`atDepthBound bool` (true when `depth >= maxSubAgentDepth`), `approverPresent bool`, and the
prebuilt `domain.ConfinementBox`.

**The rule order inside `resolve()`** (this IS the design — implement exactly):

1. Guard `GuardRefuse` (Tier-1 / tripped breaker) → `Refuse` with the guard's reason + audit.
2. `sub_agent` call (`isSubAgentCall`) → `Delegate`; **Tier-2 force-approval is deliberately
   ignored here** (D3). But `atDepthBound` → `Refuse` with today's depth-limit message
   (`subagent.go` `runSubAgent`'s defensive text). The withheld-tool defence in
   `defaultSubAgentTools` is untouched — defence in depth keeps both layers.
3. Unknown tool (nil tool) → `Refuse(fmt.Sprintf("unknown tool %q", call.Tool))`, no audit.
4. The ladder table — `classifyTool` × mode × `confineToWorkspace` × caps, porting
   `dispose()`/`disposeAuto()` verbatim (move `toolClass` + `classifyTool` into this file).
   Then the leaf-verdict overlays, in order:
   - Tier-2 `GuardForceApproval` upgrades any non-`Refuse` leaf verdict to
     `Gate{force: true, reason: "dangerous-action guard forced approval"}`.
   - Any `Gate` with `!approverPresent` → `Refuse("approval required but no Approver
     configured")` (D5).
   - Gate reasons reproduce today's `approvalReason()` class mapping; `cacheKey` = tool name.
   - Every `Confine` carries `box` + `fallback` = `Gate{force: true, reason: "subprocess
     execution (confinement unavailable on this host)"}`; with `!approverPresent` the
     fallback is `Refuse("subprocess could not be confined and approval was not granted")`
     instead (D4/D5).

**The table test:** enumerate guard-outcome × mode (all four + unknown-mode default) × class
(all six) × caps × `confine-to-workspace` × approver-present, asserting kind/force/cacheKey/
reason/fallback shape. Pinned named rows beyond the matrix: Tier-2 on a `sub_agent` call →
`Delegate` (D3); `atDepthBound` `sub_agent` → `Refuse`; nil-Approver gate → `Refuse`; every
`Confine` row's fallback is forced and never nests; unknown tool → `Refuse` with no audit
decision. Use lightweight fake tools implementing the marker interfaces (see
`dispatch_test.go` for the existing fakes to mirror — do not modify that file in this item).

**Acceptance:** new file + test only; `git diff --stat` shows no existing file touched;
`go test ./... -race` green; gofmt/vet clean. Commit:
`refactor(agent): add the Resolution verdict type and pure resolver (unwired)`.

---

## 2. Rewire dispatch to execute Resolutions; retire disposition; amend contract §4 (behavior-preserving)

**What:** make `dispatch.go` a thin executor over `resolve()`, delete `disposition.go`, and
amend the contract — one commit.

**dispatch.go:** `resolveAndExecute` becomes: `lookupTool` (fact) → `guards.PreExecute`
(fact) → build `resolutionInput` (effective mode, precomputed `writeTargetInWorkspace`, caps,
depth bound, approver-present, box) → `resolve()` → `switch` on the verdict kind, executing
mechanically:

- `Refuse` → error result; audit/events exactly as today for each refusal source (use the
  carried audit metadata; unknown-tool and Plan-refuse stay un-audited — D8's byte-identical
  rule).
- `Delegate` → `runSubAgent` (its internal depth check at `subagent.go:57` becomes
  unreachable-but-kept or is removed in favor of the resolver's row — prefer removing it and
  citing the resolver row + the withheld-tool defence; ADR 0013's defence-in-depth is the
  tool-withholding plus the resolver row).
- `Gate` → `approve()` reworked to consume the verdict: `force` skips the cache, the cache map
  is keyed by `cacheKey` (still the tool name — no behavior change), `reason` feeds the
  `ApprovalRequest`. The nil-Approver branch inside `approve()` is deleted (the resolver
  already refused; add a defensive panic-free error path if reached).
- `Confine` → `executeTool` installs the verdict's `box` (delete `confinementBox()`); on
  `ErrConfinementUnavailable` execute the verdict's `fallback` mechanically (emit today's
  demote ErrorEvent, gate forced, on allow re-run unconfined with no Confinement handle) —
  delete `executeWithApprovalFallback` as a separate decision path; what remains is fallback
  *execution*.
- `Run` → `executeTool` with no handle.

**disposition.go:** delete the file. `toolClass`/`classifyTool` moved to `resolution.go`
(item 1); move the fact-gatherers — `effectiveMode`, `writeTargetInWorkspace`, `pathWithin`,
`fsConfinementAvailable` — into `dispatch.go` (they are dispatch's fact-gathering, not
verdict logic). Delete `approvalReason` (reasons live on the verdict).

**Existing tests (the oracle — D8):** `dispatch_test.go`, `guardrails_test.go`,
`setmode_test.go` must pass with **only mechanical edits**: references to the deleted
`dispo*` constants / `dispose` become the resolver equivalents. No assertion, fixture, or
scenario may change semantically. All other test files pass **untouched** — in particular the
ADR 0013 acceptance tests (sub-agent mode/subset/confine behavior) and the compact/TUI suites.

**Contract amendment (same commit — D7):** in
`docs/design/confinement-execution-contract.md`, retitle §4 to "The per-call **Resolution**
— the one verdict dispatch executes" and widen it: guard rows first (Tier-1 refuse; Tier-2
force on leaf verdicts only), the `Delegate` + depth-bound rows, the nil-Approver⇒Refuse
rule, a fallback column on the `subproc (caps sufficient)` row (forced gate → run unconfined;
Refuse when no Approver), and a `CacheKey` column (tool name; item 3 adds the MCP server
grain). Add a dated note: *"§4 previously described only the post-guard ladder stage (the
'per-call disposition'); the Resolution subsumes it — see CONTEXT.md → Resolution and the
2026-07-02 clarification in ADR 0013."* Keep the §4 section number (code comments cite it).
Update any `dispatch.go`/`resolution.go` comments citing "§4" wording that changed.
Add a CHANGELOG entry under Unreleased: dispatch decision collapsed into one Resolution
verdict (internal refactor, no behavior change).

**Acceptance:** `go test ./... -race` green; `git diff` on `*_test.go` shows identifier
renames only; `disposition.go` gone; `dispatch.go` contains no ladder/guard-tier/demote
*decision* logic (grep: no mode-switch, no `GuardForceApproval` branching outside
fact-gathering/verdict-execution); contract §4 amended; CHANGELOG updated. Commit:
`refactor(dispatch): collapse the dispatch decision into one Resolution verdict`.

---

## 3. Cache MCP allow-for-session at server grain (ADR 0012 conformance — the one behavior change)

**What:** ADR 0012 promises MCP "allow for this session" caches at **server** grain
("approving one `github` tool allows `github.*` for the Session"), but the cache has always
been keyed by qualified tool name (`github__search` only). Close the gap via the `CacheKey`
seam built in items 1–2.

**How:**

- `internal/mcp/tool.go`: export the server identity on `serverTool` — a `ServerAlias()
  string` method returning the alias the tool was qualified with (empty for the degenerate
  unnamed-server case — one unnamed server is still one grain).
- `internal/agent/resolution.go`: for a `classMCP` gate, set
  `cacheKey = "mcp-server:" + alias`, obtaining the alias via an optional-interface assertion
  (`interface{ ServerAlias() string }`) so `internal/agent` does not import `internal/mcp`.
  A `classMCP` tool *not* implementing the interface falls back to `cacheKey` = tool name —
  today's tighter grain, a safe (tighten-only) degradation. The `"mcp-server:"` prefix makes
  the grain collision-proof against ordinary tool names.
- **Do not** change `force` semantics: a forced gate still ignores the cache entirely.

**Tests:** in `internal/agent` (mirroring the existing fake-tool idiom): approving
`github__search` with allow-for-session pre-clears `github__create_issue`; does **not**
pre-clear `jira__search`; a forced gate on a cached server still prompts; a resolver table
row pins the `mcp-server:` cacheKey shape (including the empty-alias case). Update the item-2
contract §4 `CacheKey` column note and add a CHANGELOG entry (fix: MCP allow-for-session now
server-grain per ADR 0012).

**Acceptance:** new tests green; `go test ./... -race` green; no other behavior change
(the item-1/2 table rows for non-MCP classes unchanged). Commit:
`fix(approval): cache MCP allow-for-session at server grain (ADR 0012)`.

---

## Explicitly NOT in this plan

- Consolidating/deleting old dispatch tests made redundant by the resolution table test
  (later cleanup, never in the commit that changes the code — D8).
- The deferred tool×mode disposition matrix (`TODO.md`), review candidates #2–#4, and any
  Mechanism/Phase-4 work.
- Any public-API change: everything stays unexported in `internal/agent`; no semver event.
