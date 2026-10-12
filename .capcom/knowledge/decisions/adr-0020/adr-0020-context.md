---
title: ADR 0020 context
---

Windows is the last Phase-0 stub in the confinement story. `internal/platform/confiner_other.go`
hands it `denyConfiner`, so `--mode auto` on Windows reports `{FSWrite:false,
NetworkEgress:false}`, every subprocess call takes the Approval path, and the degradation
notice fires on every session. That is *correct* under
ADR 0012
("confine if you can, gate if you can't"), not a bug — and it is exactly what Phase 5 exists to
end. This ADR is the design session the merge plan's risk table asked for: which facility, what
it may honestly claim, what it costs, and where the floor is.

**The two shipped backends fence by path policy. Windows has no facility of that shape.**
Landlock takes a ruleset of path-beneath allow rules and applies it to the subject; seatbelt
takes a profile with `allow file-write*` under the box's roots. Both are *handed the box* and
enforce it, and **neither touches the user's disk**. Windows' mandatory access control (MIC)
fences by **identity**: a process token carries an integrity level, a securable object may carry
a mandatory label in its SACL, and the kernel's mandatory check runs *before* the DACL. Nothing
in that model takes "these paths are writable" as an argument. Everything below follows from
that one asymmetry.

**What the pinned toolchain actually offers** (verified against `go 1.26` and
`golang.org/x/sys v0.45.0`, both pinned in `go.mod`):

- `syscall.SysProcAttr` on Windows has a `Token syscall.Token` field — `os/exec` starts the
  child through `CreateProcessAsUser` with a caller-supplied token. It is the **only**
  confinement hook `os/exec` exposes on Windows.
- `x/sys/windows` has the identity half in full: `OpenProcessToken` / `DuplicateTokenEx`,
  `SetTokenInformation` + `TokenIntegrityLevel` + `Tokenmandatorylabel` + `SIDAndAttributes`,
  `CreateWellKnownSid(WinLowLabelSid)`, `GetNamedSecurityInfo` / `SetNamedSecurityInfo` +
  `LABEL_SECURITY_INFORMATION` + `SE_FILE_OBJECT`, `SecurityDescriptorFromString` (SDDL) and
  `RtlGetNtVersionNumbers`. It does **not** bind `CreateRestrictedToken` — one `advapi32`
  `LazyProc` to declare, the same shape the landlock backend already takes with raw
  `unix.Syscall`.
- It has **no AppContainer surface at all** (only the `KF_FLAG_*_APPCONTAINER` /
  `CLSCTX_APPCONTAINER` / `ERROR_*_APPCONTAINER` constants), and `SysProcAttr` has no field for
  a `PROC_THREAD_ATTRIBUTE_LIST`, so `PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES` — the only
  way to *launch into* an AppContainer — is unreachable through `os/exec`.
