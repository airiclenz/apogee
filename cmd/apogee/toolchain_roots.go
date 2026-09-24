package main

// The Go toolchain's read-only trees as library roots for the read tools.
//
// `terminal` already lets a `go build` read GOROOT and the module cache — a subprocess is fenced
// for WRITES, not reads — but the built-in read tools (read_file, grep, list_dir, find_files,
// copy_file's source) mount only the workspace, the scratch dir and the skill libraries, so a
// model asked to look at a standard-library file or a dependency's source was refused the very
// trees its build had just read (session-mining review 2026-09-14). The host probes the two
// paths once, off the boot path, and folds them onto the same live roots func the skill
// libraries ride (domain.Config.ReadMounts.Roots), so they appear on the orientation's
// `Read-only library roots:` line and every read tool accepts them.
//
// The probe is a HOST function, here and not in internal/tools: which trees a Driver mounts is
// the Driver's decision, and the seam the tools take stays a list of paths (ADR 0031).

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/subprocess"
)

// toolchainProbeTimeout bounds the one `go env` run: a toolchain that cannot answer two variables
// in this long is not one whose trees are worth waiting for, and the probe is silent either way.
const toolchainProbeTimeout = 5 * time.Second

// toolchainProbeArgs is the probe's argv after the program: the two variables, in the order the
// roots are announced — GOROOT first, the module cache second.
var toolchainProbeArgs = []string{"env", "GOROOT", "GOMODCACHE"}

// toolchainProbePins are the toolchain settings the probe runs with WHATEVER the host environment
// says, appended after the inherited keys so a duplicate spelling loses (os/exec resolves duplicates
// last-wins). They are the read-only three of internal/tools' goVetPins, for the same reasons:
//   - GOTOOLCHAIN=local — the probe runs in the temp root rather than the workspace precisely so
//     no go.mod steers it, but a GOTOOLCHAIN the operator exported could still make `go env`
//     download and execute a different toolchain before it answered (probed 2026-09-14: a go.mod
//     carrying a newer `go` line made `go env` attempt exactly that and fail).
//   - GOWORK=off — no go.work is consulted for two variables that are not module-scoped.
//   - GOFLAGS=-mod=readonly — nothing the probe does may edit a go.mod as a side effect.
//
// GOENV is deliberately NOT pinned off here, where the vet pins it off: the probe wants the
// toolchain's TRUE answer, and an operator's persisted `go env -w GOMODCACHE=…` is part of it.
var toolchainProbePins = []string{
	"GOTOOLCHAIN=local",
	"GOWORK=off",
	"GOFLAGS=-mod=readonly",
}

// toolchainProbeEnvKeys is the allowlist of host variables the probe inherits: PATH and HOME (the
// toolchain resolves its own programs, and both variables default beneath HOME), the four Go
// variables that decide the two answers themselves — an exported GOROOT, GOPATH or GOMODCACHE IS
// where `go build` under `terminal` reads from, and a GOENV names the file a persisted one sits in —
// and the temp and cache keys a confined run seeds (subprocess.ScratchEnvKeys), read from that one
// list so a key added there is passed through here too.
var toolchainProbeEnvKeys = append(
	[]string{"PATH", "HOME", "GOROOT", "GOPATH", "GOMODCACHE", "GOENV"},
	subprocess.ScratchEnvKeys()...,
)

// probeToolchainRoots runs `go env GOROOT GOMODCACHE` once and returns the trees a read tool may
// mount: each answer as its symlink-resolved real path, and only when it is an existing directory.
// Both conditions are the read fence's own — internal/tools' matchRoot skips a root that is not its
// own real path and rootUsable skips one that does not open — so a symlinked GOROOT, a symlinked
// `$HOME/go/pkg/mod` or a module cache no build has created yet is resolved or dropped HERE rather
// than announced on the orientation line and then refused.
//
// The probe runs in the temp root (os.TempDir as the process reads it) — never the workspace,
// whose go.mod could steer it, and never a caller's directory: the answer is a fact of the
// machine, and the probe runs on a goroutine that outlives whoever started it, so its cwd must be
// one whose life is the process's and that nobody reclaims (the first booter's apogee home was
// found removed before `go env` ran, 2026-09-15). A parent go.mod cannot steer it either: `go env
// GOROOT GOMODCACHE` is not module-scoped, and the pins above hold whatever the tree says. It runs
// with those pins over a PATH scoped out of workspace, and it is silent: no `go` on PATH, a
// failing or timed-out run, and an unusable answer all yield fewer roots, never an error. A tree
// the model cannot read is a smaller library, not a broken session.
//
// The program itself is resolved through the argv[0] fence every other exec site takes
// (security.ResolveProgram): the HOST's PATH — not the scoped one the child gets — is what names
// the `go` that runs at boot, and a PATH carrying an activated `.venv/bin` or any other directory
// inside the workspace would hand the model's writable tree a program spawned before the session
// starts. A `go` that resolves inside the workspace is refused, silently, like a missing one.
func probeToolchainRoots(ctx context.Context, workspace string) []string {
	goBinary, err := security.ResolveProgram(nil, "go", workspace, nil)
	if err != nil {
		return nil
	}
	result, err := subprocess.RunSubprocess(ctx, subprocess.SubprocessSpec{
		Argv:        append([]string{goBinary}, toolchainProbeArgs...),
		Dir:         os.TempDir(),
		Timeout:     toolchainProbeTimeout,
		Env:         toolchainProbeEnv(workspace),
		SplitStdout: true,
	})
	if err != nil || result.ExitCode != 0 || result.TimedOut {
		return nil
	}
	roots := make([]string, 0, len(toolchainProbeArgs)-1)
	for _, line := range strings.Split(result.Stdout, "\n") {
		if root, ok := usableToolchainRoot(strings.TrimSpace(line)); ok {
			roots = append(roots, root)
		}
	}
	return roots
}

// toolchainProbeEnv is the exact environment the probe runs with: the allowlisted host keys with
// PATH scoped out of workspace — the toolchain must not resolve its own programs out of the tree
// the model writes to — then the pins that decide how the toolchain behaves.
func toolchainProbeEnv(workspace string) []string {
	return append(
		platform.Current().ScopeEnv(workspace, toolchainProbeEnvKeys, os.LookupEnv),
		toolchainProbePins...,
	)
}

// usableToolchainRoot resolves one probed answer to the root the read tools mount, or reports that
// it cannot be one: not absolute (an unset variable prints an empty line), not there, or not a
// directory.
func usableToolchainRoot(answer string) (string, bool) {
	if !filepath.IsAbs(answer) {
		return "", false
	}
	root := security.EvalRealPath(answer)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return root, true
}

// toolchainLibrary holds the probed roots for the whole process: ONE probe, started off the boot
// path so no session pays the toolchain's start-up, feeding a live func that answers nothing until
// the probe has and the probed roots from then on. GOROOT and the module cache are facts of the
// machine, not of a session, which is why a daemon raising many Firings probes once and not per
// Firing, and why a session-raised Firing composes over the session's own answer.
type toolchainLibrary struct {
	once sync.Once
	done chan struct{}

	mu     sync.RWMutex
	probed []string
}

// newToolchainLibrary returns a library whose probe has not been started.
func newToolchainLibrary() *toolchainLibrary {
	return &toolchainLibrary{done: make(chan struct{})}
}

// start launches the probe in the background, once; every later call is a no-op, whatever
// workspace it names, because the answer does not depend on it — the first caller's is merely
// what the probe's PATH is scoped out of. Where the probe runs is not the caller's to name: it
// runs in the temp root (probeToolchainRoots), which outlives every caller, where a caller's own
// directory — a test's temporary home — may be gone before the goroutine execs.
func (l *toolchainLibrary) start(workspace string) {
	l.once.Do(func() {
		go func() {
			roots := probeToolchainRoots(context.Background(), workspace)
			l.mu.Lock()
			l.probed = roots
			l.mu.Unlock()
			close(l.done)
		}()
	})
}

// roots is the live answer: nil until the probe has finished, the probed roots after. It is the
// toolchain half of the composed read-roots func, so it is read per tool call and per orientation
// render, never frozen.
func (l *toolchainLibrary) roots() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return slices.Clone(l.probed)
}

// wait blocks until a started probe has answered. It exists for the tests that pin what the
// composed roots func carries: production reads the live answer and never waits on it. On a
// library nobody started it blocks forever, which is the caller's mistake, not a state to mask.
func (l *toolchainLibrary) wait() {
	<-l.done
}

// hostToolchain is the process's one library: the session boot and the Firing composer both
// start it (the second start is the no-op) and both compose their read roots over it.
var hostToolchain = newToolchainLibrary()

// composeReadRoots is the read-roots func a Config carries: the skill libraries first, in the
// provider's own order, then the toolchain roots — a stable order, so the orientation line and the
// mount are the same list, and both halves live, so a `use-project-skills` flip and the probe's
// answer each reach the next call without re-wiring.
func composeReadRoots(skillRoots, toolchainRoots func() []string) func() []string {
	return func() []string {
		skills := skillRoots()
		toolchain := toolchainRoots()
		roots := make([]string, 0, len(skills)+len(toolchain))
		return append(append(roots, skills...), toolchain...)
	}
}
