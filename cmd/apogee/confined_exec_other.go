//go:build !linux

package main

// maybeDispatchConfinedExec is a no-op off Linux. Only the Linux landlock backend confines
// by re-invoking the apogee binary in the __confined-exec helper mode (it cannot apply
// landlock between fork and execve under CGO_ENABLED=0). macOS confines via sandbox-exec,
// which IS the wrapper process — the apogee binary is never re-invoked — and other OSes have
// no real Confiner yet (Windows is Phase 5), so there is no sentinel to intercept here
// (confinement-execution-contract §2.3 / §2.6).
func maybeDispatchConfinedExec() {}
