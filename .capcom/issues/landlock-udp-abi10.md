---
title: landlock ABI 10 can close UDP egress, so the 'no kernel closes connect(2) UDP' claims go stale
status: open
type: task
priority: 3
labels: [security]
created: 2026-11-11T13:13:42Z
---

Source: ADR 0112 (`.capcom/knowledge/decisions/adr-0112.md`), filed by item 4 of `docs/plans/2026-11-11 - 01 - fence-escape-and-open-defects-plan.md`; out of that plan's scope.

**What:** landlock ABI 10 adds UDP network rights (https://docs.kernel.org/userspace-api/landlock.html), so a network-deny box on an ABI ≥ 10 kernel can refuse a confined `connect(2)`/`sendto(2)` over UDP. apogee's landlock backend handles only `LANDLOCK_ACCESS_NET_BIND_TCP` and `LANDLOCK_ACCESS_NET_CONNECT_TCP`, and discloses `connect(2) UDP` (`domain.ResidualUDPEgress`) wherever `NetworkEgress` is true, as a gap "no kernel closes".

**Where the claim lives:** `internal/platform/landlock_linux.go` (the `Capabilities` comment: "a gap that no kernel closes"), `docs/manual/probe.md` ("egress no kernel closes"), and the drift tests that pin the residual tokens.

**Why it matters:** the disclosure is honest on ABI < 10 but becomes untrue there, and on ABI ≥ 10 the box could actually close UDP egress instead of disclosing it.

## Acceptance

- On a landlock ABI ≥ 10 host the backend either handles the UDP network rights in a network-deny box (and stops disclosing `connect(2) UDP` there) or keeps disclosing it with the claim reworded; no doc or comment says no kernel closes UDP egress.
- The drift tests that pin the disclosed residual tokens follow the change.
