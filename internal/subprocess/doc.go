// Package subprocess is the one-shot subprocess core: the single place a program apogee runs on
// somebody else's behalf is spawned, fenced, capped and reaped. It is what internal/tools' execution
// tools funnel through and what internal/gitexec builds its hardened git runner on, so a spawner
// added later inherits the whole contract instead of hand-rolling an exec.Command that has none.
//
// One call, one process (ADR 0008 — no persistent shell or REPL). [RunSubprocess] takes a
// [SubprocessSpec], honours the §2.4 confinement-and-teardown contract
// (docs/design/confinement-execution-contract.md) and hands back a [SubprocessResult]: the captured
// output, the exit status, and the flags a caller renders a refusal or a denial from. The contract's
// three halves are all here — the process-tree teardown that reaps what the call leaves behind, the
// confinement handoff that fails CLOSED when a box was required and could not be established, and
// the live kill-on-denial watch a confined run's output is scanned by.
//
// Output is bounded by default: [MaxSubprocessOutputBytes] caps what one call can accumulate, so a
// runaway command cannot exhaust memory or flood a context window. [RunSubprocessTo] is the one
// exception, and it is deliberate — a caller splicing a child's stdout into a file or a stream takes
// it uncapped through an io.Writer of its own, with stderr still capped and every other guarantee
// unchanged.
//
// The package is a LEAF: it imports internal/domain and internal/platform for the confinement values
// and the per-OS spawn facilities, and nothing else in the tree — never internal/tools, never
// internal/agent (ADR 0010).
//
// # The files, one line each
//
// subprocess.go is the whole core — the ceilings and the default timeout, the spec and result
// shapes, the capped output buffer, the teardown seam, and the run functions themselves.
// cmdline_unix.go is a no-op because execve takes a real argv; cmdline_other.go hands Windows the
// raw command line verbatim through SysProcAttr.CmdLine, bypassing the argv joining cmd.exe cannot
// read.
package subprocess
