---
title: ADR 0020 considered options
---

- **AppContainer.** The better fence in the long run — a per-app SID, capability-based, with the
  network itself a capability, so `NetworkEgress` could one day be honest. Rejected for v1 on
  three counts. *Behaviour:* it denies network by default and, worse, **blocks loopback** unless
  `CheckNetIsolation LoopbackExempt` is run **as administrator** — a real regression for an agent
  whose routine work is starting a local server and curling it. *Plumbing:* `x/sys/windows`
  v0.45.0 has no AppContainer surface and Go's `SysProcAttr` has no hook for
  `PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES`, so it needs hand-rolled `mksyscall` bindings
  **plus** a resident wrapper parent process calling `CreateProcess` with a `STARTUPINFOEX` —
  i.e. exactly the helper process the token design deletes. *And it does not even buy back the
  disk mutation:* an AppContainer still needs DACL ACEs for its package SID on the box's paths.
  **Recorded as the post-v1 tightening**, with the loopback/network problem named as what must be
  solved first, before the bindings are worth writing.
- **Job-Object-only.** A Job Object bounds resources, UI access and process count, and is how the
  whole tree gets killed (item 7) — but `JOB_OBJECT_LIMIT_*` has no path or write restriction
  whatsoever. It can never make `FSWrite` true. Rejected as a *confinement* facility; retained,
  unchanged, as the **teardown** mechanism it actually is.
- **A restricted token with restricting SIDs, Chromium-style, and no integrity label.** Rejected
  for v1: it fences through a double DACL check, so *reads* of system DLLs break unless the
  restricting SID set is tuned per program, and the allow half still has to be written onto the
  box's DACLs — the same disk mutation, more of it, and more brittle.
- **A helper process mirroring the Linux 42-liner.** Rejected: there is nothing for it to do. No
  "restrict myself" API exists, and `SysProcAttr.Token` already expresses the restriction at
  process-creation time. It would add a sentinel, an argv encoding, a second process and a second
  failure mode, for zero additional enforcement.
- **Reporting `NetworkEgress: true` because the box's default is network-open anyway.** Rejected:
  capabilities describe what the backend **can enforce**, not what today's box happens to ask for
  (contract §5). Claiming true would leave a non-empty `NetworkAllow` silently unenforced — the
  precise defect `networkDenyDecision` fails closed on.
- **Not labelling at all — run Low and accept a read-only workspace.** Rejected: a confined agent
  that cannot write to its workspace is not confined, it is broken. `FSWrite` would have to be
  reported false and Windows would stay gated, which is the status quo Phase 5 exists to end.
- **Labelling but never restoring, for speed.** Rejected: it would permanently and invisibly
  lower the integrity of the user's project tree. A tool that mutates ACLs and does not clean up
  after itself has not earned `--mode auto`.
