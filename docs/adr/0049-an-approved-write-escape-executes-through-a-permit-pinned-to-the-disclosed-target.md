---
Status: accepted
---

# An approved write escape executes through a permit pinned to the disclosed target

## Context

The confinement execution contract's §4 ladder table has promised since P3.1 that a
workspace-scoped write whose target lies **outside** the workspace is *gated* in Ask-Before,
Allow-Edits and Auto — the human is asked, and "Allow" runs the write. Half of that promise
landed: dispatch classifies the target with `resolveTargetUnbounded`
(`internal/tools/workspace_scoped.go`), so the call reaches the Gate and the approval pane shows
the resolved path. The other half never did: every write tool executes through an `os.Root` fence
pinned at the workspace root (`safeWriteFile` → `security.SafeWriteFile`), which refuses the
escape whatever the verdict was. The human deliberates, approves, and receives an error result —
a gate whose "Allow" cannot succeed. The same fence also nullifies the **Auto ·
`confine=false`** cell, where the table says the write plainly *runs*: the VM opt-in whose whole
meaning is "the VM is the box" still carried a fifth, undocumented fence for native writes only.

The gap was flagged in the contract ("Realisation gap — half-landed"), tracked as the open
`ISSUES.md` defect "an *approved* out-of-workspace write still errors at Execute", and named an
owner call: land the reconciliation the contract promises, or ratify strict fencing as permanent
and amend the row. This record ratifies the calls the owner made on 2026-08-14 (grill session,
each branch resolved by AskUserQuestion). It supersedes nothing; it completes ADR 0012's ladder
semantics for the WS-write class.

## Decision

**The Gate's allow is executable: an approved out-of-workspace write actually writes, bounded to
exactly the resolved target the approval pane disclosed, carried there by a context permit.**
Concretely:

**1 — Approval is the bound (the subprocess precedent, applied to the safer case).** An
unconfinable subprocess that gates and gets "Allow" already runs *unconfined* — arbitrary blast
radius on a human's yes. Refusing the same trust to a native write whose exact resolved path the
human was shown held the more inspectable action to the stricter standard. ADR 0012's invariant —
a call runs ungated only if its blast radius is bounded — is satisfied the same way in both rows:
the Gate *is* the bound.

**2 — The channel is a write-escape permit on the context, pinned to the approved `Real`.** The
idiom is `domain.SubprocessPermit` (contract §10), applied to writes: when dispatch resolves a
WS-write-out Gate and the human allows, the run tail stamps the context with a permit carrying
the `writeTarget.Real` the pane disclosed. At the shared write funnel: no permit → today's
workspace fence, byte-for-byte (the never-worse floor for every existing call). Permit present →
the argument re-resolves; only an exact match with the permitted `Real` writes, through an
`os.Root` pinned at the target's **parent** directory with the final component written
non-following — the disclosure surface and the executor cannot part company. Any divergence is an
error result, no write. The alternatives were rejected deliberately: a statically widened fence
cannot honour an arbitrary approved target, and an Execute that trusts dispatch implicitly loses
defence-in-depth for every path that reaches it outside dispatch (bench, a future Driver — the
door ADR 0031 keeps open).

**3 — `box.WritablePaths` is in-fence for native writes (union semantics).** Classification and
the Execute fence both treat `WorkspaceRoot ∪ box.WritablePaths` as "in workspace": a declared
writable path runs ungated in Allow-Edits/Auto, and the fence pins its `os.Root` at the
containing root. The box already grants a confined *subprocess* those paths ungated; granting the
native tool less recreated the asymmetry decision 1 kills. The fence becomes one rule — the box
bounds writes — and the parked Windows box-local `%TEMP%` work lands onto a fence that already
understands it. (The field still has no writer; the semantics are latent but tested.)

**4 — Approval is final: no hard-deny set above the Gate.** The dangerous-action floor
(`~/.apogee`) keeps exactly its documented meaning — a Tier-2 *forced look*, never a boundary
(`internal/security/doc.go`) — and the human's informed yes then runs the write, `~/.apogee`
included. A hard-deny would protect the operator from their own informed answer, a concept this
threat model rejects (the operator is trusted), and would fence the legible path while an
approved `terminal` call could always take the opaque one. If approval fatigue ever proves real,
a hard-deny tier is a later additive tightening under the security-matrix's tighten-only law.

> **Note (2026-08-26, security audit lead, §3.5).** The parenthetical above originally read
> "(`.git/`, `~/.apogee`)" and is narrowed to `~/.apogee`. The `.git/hooks|config|modules` rule
> stays **Tier-1 hard-refuse**: a write there is delayed code execution outside every
> confinement — the shell-rc class, not a control plane the operator edits by hand. Only
> `~/.apogee` is the Tier-2 forced look this decision describes. The code was reconciled to that
> reading on this date: `write-apogee-control-plane` shipped as `TierHardRefuse` and is now
> `TierForceApproval`, with the rule's Hint carried to the Approval prompt as its remedy and
> appended to a denied call's result.

**5 — The whole WS-write family honours the permit, uniformly.** The enforcement point is the
shared funnel, so `write_file`, `file_edit` (patch), `find_replace` (single + multi) and
`file_ops` (copy/move destination, delete) inherit it together — "an approved gate executes"
must not be false for four of five verbs the day it ships. An approved out-of-workspace
**delete** is the most destructive member and is deliberately included: its gate discloses the
same resolved path. One exception stays: `move_file`'s **source** is never classified or
disclosed at the Gate, so it keeps its unconditional in-workspace refusal — nothing escapes that
the pane didn't show.

**6 — The allow-for-session grain stays the argument digest.** No path-grain carve-out: an
"always allow this session" on an escape caches on tool + canonical-arguments digest like every
other gate, so changed content re-asks. A looser grain here would make the riskiest write class
the coarsest-cached one, and the content is part of what the human vouched for. A path grain is
a sighting-driven loosen to reconsider only if repeated-edit ergonomics bite.

**7 — `confine=false` mints the permit too.** The Auto · `confine=false` cell says *run*; the
run verdict never meets the Approver, so dispatch stamps the permit itself from its own
classification. One mechanism, two minters — an approval, or the mode whose contract is that the
VM is the box. The dangerous-action floor still overlays this mode unchanged.

## Consequences

- A gate is honest everywhere: "Allow" means the disclosed action happens — write, patch, copy,
  move, delete — and an error after a yes now signals a real fault, not a policy contradiction.
- The permit is engine-internal and wire-silent; drivers and the bench see only the verdicts they
  always saw (ADR 0031 intact). Sub-agents inherit the behaviour through the tree's Approver, and
  a Firing's fail-safe denier still refuses every Gate, so unattended escapes remain impossible.
- The fence's one rule — the box bounds writes, the Gate bounds escapes — is what the parked
  *configurable tool × mode security matrix* design builds on; its tighten-only law gains a
  concrete floor to tighten from.
- The realisation is tracked by `docs/plans/2026-08-14 - 01 - approved-escape-write-plan.md`;
  until it lands, contract §4's gap note stays flagged (decided, unbuilt) and the `ISSUES.md`
  defect stays open, marked planned.

## Amendment (2026-08-14) — the permit question is asked FIRST and on the RESOLVED path

Decision 2 above describes the permit as the branch a mutation takes once the fence has refused
it. The landed fix routes the other way round, and the ordering carries a case the record never
names. Both properties are stated by `internal/security/doc.go` (lines 103–104) and implemented in
`internal/security/writepermit.go`; this note brings the record level with them. Nothing decided
above changes — the permit is still exactly one path wide, and a call without one still meets
today's fence byte-for-byte.

**The match is resolved-based, and it is asked before the lexical branch.** `openMutationRoot` —
the one place every mutating primitive decides which root bounds it — first asks
`namesPermittedTarget`, which resolves the argument (`EvalRealPath` over the root-joined name) and
compares it to the permitted `Real`. Only when that question answers no does the call fall to the
in-workspace branch, and that branch stays **lexical**: `rootRelative`
(`internal/security/safeio.go:629`) judges the argument by `filepath` arithmetic against the
workspace root, exactly as it did before permits existed, with `os.Root` enforcing the
symlink-component half at use time. The permitted branch pins its root at the deepest *existing*
directory above the disclosed target rather than at a nominal parent, so a target whose parents do
not exist yet is ordinary rather than a refusal.

**Asking first is what makes the workspace-internal symlink case executable.** An argument spelled
*inside* the workspace can still resolve *outside* it — a disclosed link in the workspace pointing
out. That path is what dispatch resolved in order to classify the write and what the approval pane
showed in full, so the operator's yes names the outside target. Because the permit question comes
first and is asked of the resolved path, such an argument takes the permitted branch and answers
with that target's own ancestor; sending it down the lexical branch instead would refuse the one
call the human actually read, which is the gate failing to execute its own Allow. Every OTHER call
is untouched by the ordering: permits are minted only for disclosed escape targets, so an ordinary
in-workspace write can never meet the match.
