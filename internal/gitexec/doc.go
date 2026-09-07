// Package gitexec is the hardened git runner: the single place apogee launches the system git,
// whoever asked for it. The git TOOLS the model calls, the tracked-file mutation floor's own
// bookkeeping git and the session-owned snapshot store all come through here, so none of them
// can spawn a git that misses a hardening measure the others carry.
//
// git is a convenience dependency (§3a) — detected on PATH, degrading gracefully when absent,
// never required — and it is also the most attackable program apogee runs, because a repository
// tells git which OTHER programs to execute. The package answers both halves:
//
//   - [Resolve] and [Program] resolve git on PATH and fence what they found through
//     internal/security's exec fence, so a git the model could have written into the workspace
//     never becomes argv[0]. [UnavailableMessage] is the graceful sentence for a host with no git.
//   - Every invocation carries the global hardening options (an emptied core.hooksPath, a
//     disabled core.fsmonitor) ahead of its subcommand and GIT_CONFIG_NOSYSTEM in an allowlisted,
//     PATH-scoped environment ([SafeEnv]). [DiffHardeningArgs] closes the read-path diff drivers
//     the inspection tools would otherwise execute.
//   - A repository whose OWN config names a program git would execute — a credential helper, a
//     filter driver, an sshCommand — gets no git call at all ([CommandConfigName],
//     [CommandConfigRefusal]); the operator's global config is on the other side of that trust
//     boundary and still applies. The probe behind the refusal is memoised per
//     (git binary, root, environment).
//
// Four entry points, one funnel. [Capture] returns the captured outcome — exit code and output —
// for a caller rendering what git printed to the model. [Run] and [Query] return the child's
// stdout as DATA, with the diagnostics left out of the payload and every failure flattened to one
// error. [RunTo] is Run with the payload streamed uncapped to the caller's writer, for output a
// truncation would corrupt. All four take an env the caller appends — GIT_DIR, GIT_WORK_TREE,
// GIT_INDEX_FILE — which redirects the run to an object database of apogee's own without
// weakening anything the hardening put there.
//
// The package is a LEAF over internal/subprocess: it imports internal/domain, internal/platform,
// internal/security and internal/subprocess, and nothing else in the tree — never internal/tools,
// never internal/agent (ADR 0010). Everything tool-shaped — the tool structs, the ref guards, the
// porcelain-v2 parsing, the result rendering — stays in internal/tools/git.go, which reaches this
// package through thin wrappers.
//
// # The files, one line each
//
// gitexec.go is the whole runner — the resolution seam, the allowlist and hardening values, the
// spec builder, the four entry points, and the repo-local command-config probe and refusal.
package gitexec
