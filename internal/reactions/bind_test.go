package reactions

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// A ProjectRunSlot is the late bind a Runner's executor holds before an Agent exists: nil answers
// nil, unbound refuses with the pinned text, bound runs the bound func, and a rebind replaces it.
func TestProjectRunSlot(t *testing.T) {
	t.Parallel()

	reaction := domain.Reaction{ID: "lint"}
	runAs := func(name string, got *string) domain.ProjectRun {
		return func(context.Context, domain.Reaction, string, []string) error {
			*got = name
			return nil
		}
	}

	t.Run("a nil slot answers nil", func(t *testing.T) {
		t.Parallel()
		var slot *ProjectRunSlot

		if slot.Runner() != nil {
			t.Error("a nil slot answered a runner; the executor must read it as no fenced runner")
		}
	})

	t.Run("an unbound slot refuses with the pinned text", func(t *testing.T) {
		t.Parallel()
		slot := &ProjectRunSlot{}

		err := slot.Runner()(context.Background(), reaction, "", nil)

		if !errors.Is(err, ErrProjectRunUnbound) {
			t.Fatalf("err = %v, want ErrProjectRunUnbound", err)
		}
		const want = "refusing to run a project reaction before its session's engine is built"
		if err.Error() != want {
			t.Errorf("err text = %q, want %q", err.Error(), want)
		}
	})

	t.Run("a bound slot runs the bound func", func(t *testing.T) {
		t.Parallel()
		slot := &ProjectRunSlot{}
		run := slot.Runner()
		var ran string

		slot.Bind(runAs("first", &ran))
		err := run(context.Background(), reaction, "", nil)

		if err != nil || ran != "first" {
			t.Errorf("ran %q with err %v, want the bound func to run cleanly", ran, err)
		}
	})

	t.Run("a rebind replaces the bound func", func(t *testing.T) {
		t.Parallel()
		slot := &ProjectRunSlot{}
		run := slot.Runner()
		var ran string
		slot.Bind(runAs("first", &ran))

		slot.Bind(runAs("second", &ran))
		err := run(context.Background(), reaction, "", nil)

		if err != nil || ran != "second" {
			t.Errorf("ran %q with err %v, want the rebound func", ran, err)
		}
	})
}

// An MCPSlot is the late bind a Runner's executor holds before the session dials its MCP set: nil
// answers nil, unbound refuses with the pinned text, bound resolves through the bound resolver,
// and a rebind replaces it.
func TestMCPSlot(t *testing.T) {
	t.Parallel()

	resolveAs := func(answer string) domain.MCPResolver {
		return func(string, string, bool) (domain.MCPCall, error) {
			return func(context.Context, json.RawMessage) (string, error) { return answer, nil }, nil
		}
	}
	callThrough := func(t *testing.T, resolve domain.MCPResolver) (string, error) {
		t.Helper()
		call, err := resolve("llm", "status", false)
		if err != nil {
			return "", err
		}
		return call(context.Background(), nil)
	}

	t.Run("a nil slot answers nil", func(t *testing.T) {
		t.Parallel()
		var slot *MCPSlot

		if slot.Resolver() != nil {
			t.Error("a nil slot answered a resolver; the executor must read it as not connected in this run")
		}
	})

	t.Run("an unbound slot refuses with the pinned text", func(t *testing.T) {
		t.Parallel()
		slot := &MCPSlot{}

		_, err := callThrough(t, slot.Resolver())

		if !errors.Is(err, ErrMCPResolverUnbound) {
			t.Fatalf("err = %v, want ErrMCPResolverUnbound", err)
		}
		const want = "MCP servers are not connected yet"
		if err.Error() != want {
			t.Errorf("err text = %q, want %q", err.Error(), want)
		}
	})

	t.Run("a bound slot resolves through the bound resolver", func(t *testing.T) {
		t.Parallel()
		slot := &MCPSlot{}
		resolve := slot.Resolver()

		slot.Bind(resolveAs("first"))
		got, err := callThrough(t, resolve)

		if err != nil || got != "first" {
			t.Errorf("answered %q with err %v, want the bound resolver's answer", got, err)
		}
	})

	t.Run("a rebind replaces the bound resolver", func(t *testing.T) {
		t.Parallel()
		slot := &MCPSlot{}
		resolve := slot.Resolver()
		slot.Bind(resolveAs("first"))

		slot.Bind(resolveAs("second"))
		got, err := callThrough(t, resolve)

		if err != nil || got != "second" {
			t.Errorf("answered %q with err %v, want the rebound resolver's answer", got, err)
		}
	})
}

// mcpEntry is a user reaction whose handler calls a tool on an MCP server.
func mcpEntry(id string, class domain.Class) domain.Reaction {
	return domain.Reaction{
		ID:      id,
		Origin:  domain.OriginUser,
		Class:   class,
		On:      []domain.Moment{domain.MomentSessionStarted},
		Handler: domain.MCPHandler{Server: "llm", Tool: "status"},
		Timeout: time.Second,
	}
}

// commandEntry is a user reaction whose handler runs an argv command.
func commandEntry(id string) domain.Reaction {
	return domain.Reaction{
		ID:      id,
		Origin:  domain.OriginUser,
		Class:   domain.ClassObserve,
		On:      []domain.Moment{domain.MomentExchangeFinished},
		Handler: domain.ArgvHandler{Argv: []string{"true"}},
		Timeout: time.Second,
	}
}

// reactionIDs names a list's entries by id and class, in order.
func reactionIDs(list []domain.Reaction) []string {
	ids := make([]string, 0, len(list))
	for _, r := range list {
		ids = append(ids, r.ID+"/"+string(r.Class))
	}
	return ids
}

// DisarmUnconnectedMCP drops advise and observe mcp: entries, keeps gate ones and every other
// entry in order, and says one pinned notice per entry, deduplicated by id and text.
func TestDisarmUnconnectedMCP(t *testing.T) {
	t.Parallel()

	const (
		inertStatus = `reaction "status": its mcp: handler is inert here — MCP servers are not connected in this run`
		inertRecord = `reaction "record": its mcp: handler is inert here — MCP servers are not connected in this run`
		inertBoth   = `reaction "both": its mcp: handler is inert here — MCP servers are not connected in this run`
		refusedGate = `reaction "guard": its mcp: gate cannot run here — MCP servers are not connected in this ` +
			`run, so every call it gates is refused`
	)
	tests := []struct {
		name     string
		list     []domain.Reaction
		wantKept []domain.Reaction
		wantSaid []string
	}{
		{
			name: "advise and observe dropped, gate and others kept in order",
			list: []domain.Reaction{
				commandEntry("first"),
				mcpEntry("status", domain.ClassAdvise),
				mcpEntry("guard", domain.ClassGate),
				mcpEntry("record", domain.ClassObserve),
				commandEntry("last"),
			},
			wantKept: []domain.Reaction{
				commandEntry("first"),
				mcpEntry("guard", domain.ClassGate),
				commandEntry("last"),
			},
			wantSaid: []string{inertStatus, refusedGate, inertRecord},
		},
		{
			name: "a run: and advise: pair under one id says once",
			list: []domain.Reaction{
				mcpEntry("both", domain.ClassObserve),
				mcpEntry("both", domain.ClassAdvise),
			},
			wantKept: []domain.Reaction{},
			wantSaid: []string{inertBoth},
		},
		{
			name:     "a list with no mcp: entries comes back unchanged",
			list:     []domain.Reaction{commandEntry("first"), commandEntry("last")},
			wantKept: []domain.Reaction{commandEntry("first"), commandEntry("last")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			original := reactionIDs(tt.list)
			var said []string

			kept := DisarmUnconnectedMCP(tt.list, func(line string) { said = append(said, line) })

			if got, want := reactionIDs(kept), reactionIDs(tt.wantKept); !slices.Equal(got, want) {
				t.Errorf("kept %q, want %q", got, want)
			}
			if !slices.Equal(said, tt.wantSaid) {
				t.Errorf("said %q, want %q", said, tt.wantSaid)
			}
			if got := reactionIDs(tt.list); !slices.Equal(got, original) {
				t.Errorf("the input list was modified: now %q, was %q", got, original)
			}
		})
	}
}

// A nil report drops the notices but not the filtering.
func TestDisarmUnconnectedMCPNilReport(t *testing.T) {
	t.Parallel()
	list := []domain.Reaction{mcpEntry("status", domain.ClassAdvise), commandEntry("log")}

	kept := DisarmUnconnectedMCP(list, nil)

	if got, want := reactionIDs(kept), []string{"log/observe"}; !slices.Equal(got, want) {
		t.Errorf("kept %q, want %q", got, want)
	}
}
