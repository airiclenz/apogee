---
title: ADR 0012 consequences
---

- **ADR 0004 is superseded.** Its network-default-deny, both-required, and no-escape-hatch decisions
  are reversed. What survives, restated here: the capability matrix, the blast-radius "never
  unsupervised *and* unbounded" invariant, and the per-tool teeth (MCP gates).
- **`ConfinementCaps.AutoEligible()` changes** to `FSWrite`-only; the `agent.New` Auto gate and
  `ErrAutoUnavailable` follow. Linux Auto's reach widens from kernel ≥6.7 to ≥5.13.

  > **Note 2026-10-12 (verified against code; plan `2026-10-12 - 00` item 3).** The construction
  > gate does not read `AutoEligible()`. Agent construction (`buildAgent`,
  > `internal/agent/construct.go`) refuses Auto only when **no Confiner** is injected at all, with
  > `ErrAutoUnavailable`; any non-nil Confiner admits Auto. A Confiner without `FSWrite` does not
  > refuse Auto: each subprocess call gates through Approval instead (`fsConfinementAvailable`,
  > `internal/agent/dispatch.go` — "confine if you can, gate if you can't"). `AutoEligible()` is read
  > only by `internal/probe`: the `apogee probe host` verdict, and `AutoUnattendedBlocked`, which
  > refuses **unattended** Auto — `apogee headless` and Schedule Firings — on a host that cannot fence.

- **`web-fetch` / `http-request` no longer gate in Auto** (url-safety filtered instead); **MCP gating
  is now conditional** on `confine-to-workspace` (gates when `true`, free when `false`).
- **`confine-to-workspace=false` is a documented footgun.** It is safe *only* via a VM; the
  dangerous-action guard is a mistake-net, not a security layer, and must never be described as making
  unconfined Auto "safe."
- **CONTEXT.md** Agent-mode / Confinement entries and the **Phase-3 plan** (§3 D1/D5, §1 exit #3,
  P3.4/P3.6/P3.8/P3.11) are updated to this model; the plan's §5 reopened block is resolved.
- The P3.1 design pass wrote the *implementation contract* on top of this settled *policy* —
  [`docs/design/confinement-execution-contract.md`](../../../../docs/design/confinement-execution-contract.md): the
  `Confine` signature (prepare-in-place over a `*exec.Cmd`; the closure form is deleted), the
  `workspaceScopedWriter` marker, the per-call disposition table, the capability-honesty rule, and the
  shared escape-probe harness P3.2/P3.3 build to. **Where that contract and this ADR's prose differ on a
  mechanism, that contract is authoritative on the *how*; this ADR remains authoritative on the *policy*.**
