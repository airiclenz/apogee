package domain

import (
	"context"
	"testing"
)

// TestSubAgentNameContextRoundTrip pins the ask path's second identity carrier: the engine installs
// the delegation's short name on the call's context and the ask_user tool reads it straight back
// out, because that tool builds its own AskRequest one interface boundary away from the Agent that
// knows the name. An absent value reads as "" — the same emptiness an unnamed delegation installs,
// so a caller needs no second signal to know it must fall back to the task.
func TestSubAgentNameContextRoundTrip(t *testing.T) {
	t.Parallel()

	if got := SubAgentNameFromContext(context.Background()); got != "" {
		t.Errorf("SubAgentNameFromContext on a bare context = %q, want none", got)
	}

	ctx := WithSubAgentName(context.Background(), "repo-scout")
	if got := SubAgentNameFromContext(ctx); got != "repo-scout" {
		t.Errorf("SubAgentNameFromContext = %q, want the installed name", got)
	}

	// A nested delegation overwrites its parent's value with its own — including with the empty
	// name of an UNNAMED child, which must report its own namelessness rather than inherit a name
	// that is not its.
	if got := SubAgentNameFromContext(WithSubAgentName(ctx, "")); got != "" {
		t.Errorf("an unnamed child read %q off the context, want none", got)
	}

	// The two carriers are independent keys: naming a child never disturbs its task, and vice versa.
	both := WithSubAgentTask(ctx, "audit the config loader")
	if name, task := SubAgentNameFromContext(both), SubAgentTaskFromContext(both); name != "repo-scout" || task != "audit the config loader" {
		t.Errorf("name/task = %q/%q, want them carried side by side", name, task)
	}
}
