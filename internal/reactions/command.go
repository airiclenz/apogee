package reactions

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/userexec"
)

// A Reaction's `run:` argv runs under internal/userexec's posture — the one `api-key-cmd:` runs
// under too: no shell, fenced at the program (a Reaction is the USER's configuration rather than
// anything the model chose, so it runs unsandboxed; what it may not do is execute a file the model
// could have written — ADR 0073 §6), no terminal, bounded. What this file adds is the Reaction's own
// shape of that posture: stdin is the payload JSON and nothing else; stdout is discarded, because
// nothing a Reaction prints may reach the model, the conversation or the Session record; stderr is
// kept only to quote back in the failure line the Driver reports; and the APOGEE_REACTION_* facts
// are appended to the inherited environment last, so they win over an inherited variable of the
// same name.

// saidSeparator joins what the command said onto the failure line's own words: `exit 3: no such
// recipient`.
const saidSeparator = ": "

// commandExecutor runs an entry's `run:` argv. It holds only the workspace root, because that
// is the whole fence: everything else about one run comes from the entry and the firing.
//
// It is safe for concurrent use — it keeps no per-run state — which the Executor contract requires,
// since one executor serves every Reaction and each Reaction has a worker of its own.
type commandExecutor struct {
	workspaceRoot string
}

// Run executes the entry's argv with the payload on stdin, and reports what went wrong in the
// words the Driver puts in front of the user. The Runner prefixes the entry's name and event, so
// the message here says only what happened: `exit 3: …`, `timed out after 30s`, or the refusal that
// stopped the program from being run at all.
//
// The context is already bounded by the entry's Timeout by the time it arrives, and is cancelled
// when the Runner is closing; both end the child, and only the deadline is reported — a
// cancellation is apogee's own shutdown and the Runner drops it.
func (c commandExecutor) Run(ctx context.Context, r domain.Reaction, p domain.SeamPayload) error {
	handler, ok := r.Handler.(domain.ArgvHandler)
	if !ok || len(handler.Argv) == 0 {
		return errors.New("no command to run")
	}
	body, err := encodePayload(p)
	if err != nil {
		return err
	}

	result, err := userexec.Run(ctx, handler.Argv, userexec.Options{
		WorkspaceRoot: c.workspaceRoot,
		Stdin:         bytes.NewReader(body),
		Env:           p.Env(),
	})
	said := ""
	if result.StderrTail != "" {
		said = saidSeparator + result.StderrTail
	}
	switch {
	case err != nil:
		return fmt.Errorf("%w%s", err, said)
	case result.TimedOut:
		return fmt.Errorf("timed out after %s%s", r.Timeout, said)
	case result.ExitCode != 0:
		return fmt.Errorf("exit %d%s", result.ExitCode, said)
	}
	return nil
}
