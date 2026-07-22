//go:build !linux

package main

// maybeDispatchConfinedExec is a no-op off Linux. Only the Linux landlock backend confines
// by re-invoking the apogee binary in the __confined-exec helper mode (it cannot apply
// landlock between fork and execve under CGO_ENABLED=0). macOS confines via sandbox-exec,
// which IS the wrapper process — the apogee binary is never re-invoked — and Windows hands
// CreateProcessAsUser a restricted low-integrity token through SysProcAttr.Token, which
// expresses the restriction at process-creation time and so needs no helper, no sentinel and
// no argv rewrite either (ADR 0020 §1). Other OSes have no real Confiner at all, so there is
// no sentinel to intercept here (confinement-execution-contract §2.3 / §2.6 / §9.2).
func maybeDispatchConfinedExec() {}
