package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// DefaultExecutor is the Executor every Driver installs: it runs a Hook's `command:` argv or POSTs
// its `webhook:` URL, picking by whichever the entry set. Validate has already guaranteed exactly
// one is set, so the choice is a fact of the entry rather than a preference expressed here.
//
// workspaceRoot is the exec fence, and only the command half uses it: a program that lives inside
// the workspace is one the model could have written, and running it would turn a file write into
// arbitrary code execution on the user's machine (ADR 0073 §6). Pass the run's workspace root —
// the same value the Runner is given — and pass "" only where there is no workspace at all, which
// leaves the fence to security.ResolveProgram's own defaults.
//
// The returned Executor is safe for concurrent use across every Hook at the root, which the
// Executor contract requires.
func DefaultExecutor(workspaceRoot string) Executor {
	return defaultExecutor{
		command: commandExecutor{workspaceRoot: workspaceRoot},
	}
}

// defaultExecutor is the pair of real executors behind DefaultExecutor, dispatching on the Hook.
type defaultExecutor struct {
	command commandExecutor
	webhook webhookSender
}

// Run hands the firing to whichever half the Hook configured.
func (e defaultExecutor) Run(ctx context.Context, h Hook, p Payload) error {
	switch {
	case len(h.Command) > 0:
		return e.command.Run(ctx, h, p)
	case h.Webhook != "":
		return e.webhook.Run(ctx, h, p)
	}
	return errors.New("neither a command nor a webhook to run")
}

// encodePayload renders the firing as the JSON document both halves send — on stdin for a command,
// as the POST body for a webhook. It is one function so the two can never drift: a script that
// learns to read the document from a command Hook reads the identical document from a webhook.
func encodePayload(p Payload) ([]byte, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("could not encode the payload: %w", err)
	}
	return body, nil
}
