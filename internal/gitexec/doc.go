// Package gitexec is the hardened git runner: the single place apogee launches the system git,
// whoever asked for it. The git TOOLS the model calls, the tracked-file mutation floor's own
// bookkeeping git and the session-owned snapshot store all come through here, so none of them
// can spawn a git that misses a hardening measure the others carry.
//
// git is a convenience dependency (§3a) — detected on PATH, degrading gracefully when absent,
// never required — and it is also the most attackable program apogee runs, because a repository
// tells git which OTHER programs to execute. The package answers both halves:
//
//   - [Host.Resolve] and [Host.Program] resolve git on PATH and fence what they found
//     through internal/security's exec fence, so a git the model could have written into the
//     workspace never becomes argv[0]. [UnavailableMessage] is the graceful sentence for a host
//     with no git.
//   - Every invocation carries the global hardening options (an emptied core.hooksPath, a
//     disabled core.fsmonitor) ahead of its subcommand and GIT_CONFIG_NOSYSTEM in an allowlisted,
//     PATH-scoped environment ([SafeEnv]). [DiffHardeningArgs] closes the read-path diff drivers
//     the inspection tools would otherwise execute.
//   - A repository whose OWN config names a program git would execute — a credential helper, a
//     filter driver, an sshCommand — gets no git call at all ([CommandConfigName],
//     [CommandConfigRefusal]); the operator's global config is on the other side of that trust
//     boundary and still applies. The probe behind the refusal is memoised per
//     (git binary, root, environment) and re-probed when the config's files change — the scope
//     files, HEAD and every include they name are fingerprinted and re-stat-ed on each call. A
//     root no repository reaches is never memoised, so a mid-session `git init` is seen. A probe
//     that could not complete — a call that timed out, wedged its drain or overran the output
//     cap — refuses the call too ([CommandConfigIncompleteRefusal]) and is never memoised. The
//     pattern's source string is internal/security's
//     ([security.GitCommandConfigNameSource]), which the shell write view widens with
//     core.hooksPath, core.fsmonitor, interactive.diffFilter and the include paths to name
//     .git/config for a `git config` line that sets such a key.
//
// Seven entry points, one funnel. [Host.Capture] returns the captured outcome — exit code and
// output — for a caller rendering what git printed to the model. [Host.CaptureEnv] is Capture
// with a caller's env and [CaptureOptions] — NoTerminal starts git with no controlling terminal,
// for a run apogee starts on its own that must never prompt — and [Host.CaptureEnvTo] is
// CaptureEnv with stdout streamed uncapped to the caller's writer, for an answer parsed at any
// size. [Host.Run] and [Host.Query] return the child's stdout as DATA, with the diagnostics left
// out of the payload and every failure flattened to one error. [Host.RunTo] is Run with the
// payload streamed uncapped to the caller's writer, for output a truncation would corrupt.
// [Host.RunDiagnosed] is Run that also hands back the child's stderr on a zero exit, for a
// command whose warning is the only sign its answer is partial. Every one but Capture takes an
// env the caller appends — GIT_DIR, GIT_WORK_TREE, GIT_INDEX_FILE, or a captured run's own
// variables — which redirects the run to an object database of apogee's own without weakening
// anything the hardening put there, and the probe behind the refusal runs under that same env.
// [IsIncompleteRefusal] tells a probe that could not finish from a repository that is refused, and
// a data entry point's error matches [ErrIncomplete] when git gave no complete answer (a timeout,
// a wedged drain, a stdout that overran the output cap, that unfinished probe), so a caller never
// caches a "no" it was not given: it asks again after a timeout, a wedge or an unfinished probe,
// and streams through [Host.RunTo] a payload that overran the cap, which is the same on every retry.
//
// Every entry point is a method of a [Host] value — the PATH lookup, the two subprocess
// launchers and the per-OS environment rules one run goes through — and a caller runs git
// through the Host it was handed: the git tools through their execHost's, the engine's tree
// snapshotter and secrets guard through the one the Agent builds them with, the snapshot store
// through the one it was opened with. There is no package-level lookup or launcher to swap; the one
// package-level wrapper over [OS] left is [SafeEnv]. A nil field is the real OS's
// facility, so the zero Host ([OS]) runs the system git; a fake Host scripts git's outcomes with
// no git installed, and its answers still pass the exec fence, the hardening and the
// command-config refusal.
//
// The package is a LEAF over internal/subprocess: it imports internal/domain, internal/platform,
// internal/security and internal/subprocess, and nothing else in the tree — never internal/tools,
// never internal/agent (ADR 0010). Everything tool-shaped — the tool structs, the ref guards, the
// result rendering — stays in internal/tools/git.go, which reaches this package through thin
// wrappers; the porcelain-v2 status parsing lives in internal/gitstatus, a pure parser that runs
// no git and so imports neither this package nor internal/subprocess.
//
// # The files, one line each
//
// gitexec.go is the whole runner — the Host value and the resolution seam, the allowlist and
// hardening values, the spec builder, the entry points, and the repo-local command-config probe
// and refusal.
package gitexec
