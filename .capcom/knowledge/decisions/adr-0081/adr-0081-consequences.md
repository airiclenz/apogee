---
title: ADR 0081 consequences
---

- **Raspberry Pi OS and ENOSYS containers become Auto-eligible** wherever `bwrap` is present and
  unprivileged user namespaces work. The degradation notice vanishes there; where it persists, it now
  says why.
- **Linux carries the same bounded ADR 0042 exception macOS does.** The claim that Linux confinement
  "never reaches outside the binary" is true of the first rung only; ADR 0042 §4 carries a dated note.
- **The `/dev/null` exemption is no longer exactly `/dev/null` on every backend.** Contract §2.3
  property 2 now names the namespace backend's device set as its own write-exempt set. Reads were
  never gated; nothing changes for them.
- **The denial signature is wider.** A write to a genuinely read-only mount inside any confined run is
  now a kill-on-denial match (ADR 0056 D2, amended).
- **`ConfinementCaps` has a fourth field**, `Unavailable`, filled by both Linux backends when they
  cannot fence. The field and its rendering are backend-agnostic — seatbelt and the Windows token
  backend still report a bare `false` and may fill it later without a contract change.
- **The escape battery gains a third Linux driver.** `TestNamespaceProbe` runs rows #1–#6, #11 and #12
  and `TestNamespaceProbeNetwork` rows #7/#8 on a bwrap host; the landlock driver skips there and the
  namespace driver skips where landlock is available, so a single host proves one backend.
- **A native namespace launcher stays open** as a follow-up; `--unshare-pid`, a per-host network
  allow list and a backend-forcing config key are out of scope and stay so.
