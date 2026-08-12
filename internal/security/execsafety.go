package security

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ErrExecFromWritablePath is the sentinel for a program apogee refused to execute because
// argv[0] resolved inside a path the model can write. Callers match it (errors.Is) to tell a
// refusal apart from a program that is merely absent — the two need different words on the
// result, because "not available" sends the operator looking for a missing install while this
// one sends them at their own PATH.
var ErrExecFromWritablePath = errors.New("security: the program resolves inside a model-writable path")

// RefuseExecFromWritablePath refuses an argv[0] that resolves inside the writable confinement
// box: the workspace root, plus every box.WritablePaths entry when a box is given.
//
// It closes the plant-then-exec chain. A confined call may write anywhere inside the box by
// design, so it can author (or overwrite, keeping the 0755 bit of what it replaced) an
// executable there; a later call that resolves its program on PATH — every exec site does —
// then runs those bytes, and the run that executes them may be an UNCONFINED one, outside the
// box the first call was fenced by. Confinement is not a mitigation for this: the box bounds
// writes, not what a program does once it is the program. So the refusal belongs at the
// resolution site of every exec, and it is the only place the two halves can be told apart.
//
// The fence rule is ResolveInRoot's, deliberately: what the model may WRITE and what apogee
// refuses to EXECUTE are the same boundary measured twice, so they cannot drift apart. argv0 is
// resolved through symlinks first (a /usr/local/bin name pointing into the workspace is the
// same attack wearing a hat), and a relative argv0 — what exec.LookPath returns when PATH
// carries a relative entry, resolved by the child against a working directory that is usually
// the workspace itself — is made absolute against the process's own directory rather than
// waved through.
//
// An empty fence set (no root, no box) refuses nothing: a caller that cannot name a workspace
// has no policy to apply, and inventing one would refuse every program on the host.
func RefuseExecFromWritablePath(argv0, root string, box *domain.ConfinementBox) error {
	program := strings.TrimSpace(argv0)
	if program == "" {
		return nil
	}
	if !filepath.IsAbs(program) {
		abs, err := filepath.Abs(program)
		if err != nil {
			// A relative argv[0] whose absolute form cannot be derived is refused rather than
			// run: it resolves against the child's working directory, which for every tool
			// here is inside the workspace.
			return fmt.Errorf("%w: %s does not resolve to an absolute program path", ErrExecFromWritablePath, program)
		}
		program = abs
	}

	for _, fence := range execFences(root, box) {
		resolved, err := ResolveInRoot(program, fence)
		if err != nil {
			continue // outside this fence (ErrPathEscape) — the safe answer
		}
		return fmt.Errorf("%w: %s resolves inside %s", ErrExecFromWritablePath, resolved, EvalRealPath(fence))
	}
	return nil
}

// execFences is the set of directories an executable may not resolve inside: the workspace
// root plus the box's extra writable paths (its own WorkspaceRoot included — a box built for a
// different root than the caller's is a wiring bug, not a licence to exec from it). Empty
// entries are dropped, because ResolveInRoot would measure an empty root against the process's
// working directory and refuse the whole host.
func execFences(root string, box *domain.ConfinementBox) []string {
	fences := make([]string, 0, 4)
	add := func(dir string) {
		if strings.TrimSpace(dir) != "" {
			fences = append(fences, dir)
		}
	}
	add(root)
	if box != nil {
		add(box.WorkspaceRoot)
		for _, dir := range box.WritablePaths {
			add(dir)
		}
	}
	return fences
}
