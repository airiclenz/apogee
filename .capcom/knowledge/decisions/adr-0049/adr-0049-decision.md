---
title: ADR 0049 decision
---

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

> **Note (2026-10-02, verified against code):** the parenthetical above stopped holding on
> 2026-08-22. Since ADR 0056 D3, `box.WritablePaths` has a production writer:
> `Config.ConfinementBox` (`internal/domain/confinement.go`) folds the session `ScratchDir` into
> it, and the shipped composition root sets that dir (`rootWiring.wireSession` in
> `cmd/apogee/wire_live.go`, `firingConfig` in `cmd/apogee/wire_firing.go`). So this decision's
> union semantics are live for the scratch dir, as ADR 0056 and ADR 0012's 2026-09-14 amendment
> rely on. The other source of the field still has no writer. `Config.ConfineWritablePaths`
> (`internal/domain/config.go`) is set by no production code, and `internal/config` declares no
> key that fills it, so a per-project writable path remains latent.

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
