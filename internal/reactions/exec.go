package reactions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// DefaultExecutor is the Executor every Driver installs: it runs an entry's `run:` argv, POSTs its
// `run: url:` webhook or calls its `run: mcp:` tool, picking by whichever handler the entry carries. A domain.Reaction holds
// exactly one, so the choice is a fact of the entry rather than a preference expressed here.
//
// workspaceRoot is the exec fence, and only the command half uses it: a program that lives inside
// the workspace is one the model could have written, and running it would turn a file write into
// arbitrary code execution on the user's machine (ADR 0073 §6). Pass the run's workspace root —
// the same value the Runner is given — and pass "" only where there is no workspace at all, which
// leaves the fence to security.ResolveProgram's own defaults.
//
// guard is the webhook half's: every `run: url:` post goes through it (webhook.Post), so a host the
// url-safety lists close is refused before any dial and a redirect is never followed. Pass the
// guard the root builds from its url-safety allow/deny lists — security.NewURLGuard over the same
// two fields the network tools are guarded by.
//
// project is the command half's runner for an adopted repo reaction (domain.Reaction.Project):
// its `run:` runs inside the workspace exec fence under the Agent's own permit rather than on the
// user's unsandboxed posture (ADR 0104 §8), so the root hands in the Agent's runner — late-bound,
// since the Runner is built before the Agent. nil refuses every repo `run:`.
//
// resolve is the MCP half's: it turns a `run: mcp:` entry's server and tool into the call
// (adr-0076-amend-1029). A Driver that dials no MCP server passes nil, and every such entry then
// fails as not connected — a backstop, since those Drivers drop the entries before arming. The
// root hands in a late-bound resolver where the Runner is built before the servers connect.
//
// The returned Executor is safe for concurrent use across every Reaction at the root, which the
// Executor contract requires.
func DefaultExecutor(
	workspaceRoot string,
	guard security.URLGuard,
	project domain.ProjectRun,
	resolve domain.MCPResolver,
) Executor {
	return defaultExecutor{
		command: commandExecutor{workspaceRoot: workspaceRoot, project: project},
		webhook: webhookSender{guard: guard},
		mcp:     mcpCaller{resolve: resolve},
	}
}

// defaultExecutor is the three real executors behind DefaultExecutor, dispatching on the entry's
// handler.
type defaultExecutor struct {
	command commandExecutor
	webhook webhookSender
	mcp     mcpCaller
}

// Run hands the firing to whichever half the entry's handler names.
func (e defaultExecutor) Run(ctx context.Context, r domain.Reaction, p domain.SeamPayload) error {
	switch r.Handler.(type) {
	case domain.ArgvHandler:
		return e.command.Run(ctx, r, p)
	case domain.WebhookHandler:
		return e.webhook.Run(ctx, r, p)
	case domain.MCPHandler:
		return e.mcp.Run(ctx, r, p)
	}
	return errors.New("neither a command nor a webhook to run")
}

// mcpCaller calls an observe entry's MCP tool through the resolver the root injected, and discards
// the answer: nothing an observe Reaction says may reach the model, the conversation or the Session
// record. The user authored the call, so it never reaches the Approver; nothing is spawned, so no
// permit is taken. It keeps no per-run state, so it is safe for concurrent use.
type mcpCaller struct {
	// resolve turns the entry's server and tool into the call; nil means no MCP server is dialled
	// in this run, and every entry fails as not connected.
	resolve domain.MCPResolver
}

// errMCPNotConnected is an MCP entry's failure in a run whose Driver injected no resolver. The
// Runner's repeat suppression reports it once per entry, not on every firing.
var errMCPNotConnected = errors.New("MCP servers are not connected in this run")

// errNoProjectPin refuses an adopted repo entry that carries no adoption pin: with nothing to check
// it against, the call could not be told apart from one the repo never had adopted.
var errNoProjectPin = errors.New("refusing to run a project reaction that carries no adoption pin")

// Run resolves the entry's tool and calls it with the entry's static args plus, under its
// payload-arg, the firing's document as a JSON object — the same document a command reads on stdin.
// An adopted repo entry passes its pin first and is resolved as a project entry, so the resolver
// refuses a server its own Project config does not declare. ctx is already bounded by the entry's
// Timeout; a call that outlives it is reported as `timed out after <timeout>`, like a command's.
func (c mcpCaller) Run(ctx context.Context, r domain.Reaction, p domain.SeamPayload) error {
	handler, ok := r.Handler.(domain.MCPHandler)
	if !ok {
		return errors.New("no MCP tool to call")
	}
	if c.resolve == nil {
		return errMCPNotConnected
	}
	if r.Project {
		if err := admitProjectPin(r); err != nil {
			return err
		}
	}
	call, err := c.resolve(handler.Server, handler.Tool, r.Project)
	if err == nil && call == nil {
		err = errors.New("mcp: the resolver returned no call")
	}
	if err != nil {
		return err
	}

	body, err := encodePayload(p)
	if err != nil {
		return err
	}
	args, err := mcpArguments(handler, body)
	if err != nil {
		return err
	}
	if _, err := call(ctx, args); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("timed out after %s", r.Timeout)
		}
		return err
	}
	return nil
}

// admitProjectPin is a repo entry's spawn-time check (ADR 0104 §4), run before its tool is resolved
// as it is before a repo command is spawned: a nil pin, a moved project file or a pin that could not
// be checked refuses the call.
func admitProjectPin(r domain.Reaction) error {
	if r.Pin == nil {
		return errNoProjectPin
	}
	if _, err := r.Pin.Admit(); err != nil {
		return fmt.Errorf("refusing to run: %w", err)
	}
	return nil
}

// mcpArguments is the JSON object an MCP entry's tool is called with: the handler's static Args
// and, when it names a PayloadArg, the payload document under that name. Validate refuses a
// PayloadArg that Args already holds, so the payload never overwrites a static argument.
func mcpArguments(handler domain.MCPHandler, document []byte) (json.RawMessage, error) {
	args := make(map[string]any, len(handler.Args)+1)
	maps.Copy(args, handler.Args)
	if handler.PayloadArg != "" {
		args[handler.PayloadArg] = json.RawMessage(document)
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("could not encode the mcp args: %w", err)
	}
	return encoded, nil
}

// encodePayload renders the firing as the JSON document every half sends — on stdin for a command,
// as the POST body for a webhook, under the payload-arg for an MCP tool. It is one function so they
// can never drift: a script that learns to read the document from a command Reaction reads the
// identical document from a webhook or a tool.
func encodePayload(p domain.SeamPayload) ([]byte, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("could not encode the payload: %w", err)
	}
	return body, nil
}
