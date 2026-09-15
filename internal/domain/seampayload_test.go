package domain_test

// The one payload document every fired out-of-process Reaction reads (seampayload.go), pinned at
// its two sync Moments; the observe lane's goldens live with the code that fills them, in
// internal/reactions' payload_test.go.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestSeamPayloadJSONOmitsWhatTheMomentDoesNotCarry pins the document's shape at each of the two
// sync Moments: a gate at pre-tool-exec carries the pending call's arguments and no result, while a
// post-tool-result advise carries the result — including `is_error` on a successful one, which is
// always written so a script need not tell absent from false.
func TestSeamPayloadJSONOmitsWhatTheMomentDoesNotCarry(t *testing.T) {
	t.Parallel()

	gate, err := json.Marshal(domain.SeamPayload{
		Event:     domain.MomentPreToolExec,
		Reaction:  "watcher",
		Tool:      "terminal",
		Arguments: json.RawMessage(`{"command":"ls"}`),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(gate); !strings.Contains(got, `"arguments":{"command":"ls"}`) || strings.Contains(got, `"result"`) {
		t.Errorf("gate document = %s, want the arguments and no result member", got)
	}
	if strings.Contains(string(gate), `"path"`) {
		t.Errorf("gate document = %s, want no path member on a Moment that carries none", gate)
	}

	advise, err := json.Marshal(domain.SeamPayload{
		Event:    domain.MomentPostToolResult,
		Reaction: "watcher",
		Result:   &domain.SeamResult{Content: "done"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `"result":{"content":"done","is_error":false}`; !strings.Contains(string(advise), want) {
		t.Errorf("advise document = %s, want it to carry %s", advise, want)
	}
}

// TestSeamPayloadEnv pins the headline variables a one-line script reads instead of parsing
// stdin: their names, their fixed order, the Schedule pair a Firing adds, and the rule that a fact
// the payload does not carry is OMITTED rather than set empty — a variable inherited from the user's own environment must not be
// blanked by a reaction with nothing to put there.
func TestSeamPayloadEnv(t *testing.T) {
	t.Parallel()

	full := domain.SeamPayload{
		Event:     domain.MomentFileChanged,
		Reaction:  "watcher",
		Workspace: "/work/space",
		Path:      "/work/space/main.go",
	}
	want := []string{
		"APOGEE_REACTION_EVENT=file-changed",
		"APOGEE_REACTION_NAME=watcher",
		"APOGEE_REACTION_WORKSPACE=/work/space",
		"APOGEE_REACTION_PATH=/work/space/main.go",
	}
	if got := full.Env(); !reflect.DeepEqual(got, want) {
		t.Errorf("Env() = %q, want %q", got, want)
	}

	pathless := domain.SeamPayload{Event: domain.MomentPreToolExec, Reaction: "watcher"}
	if got := pathless.Env(); !reflect.DeepEqual(got, []string{
		"APOGEE_REACTION_EVENT=pre-tool-exec",
		"APOGEE_REACTION_NAME=watcher",
	}) {
		t.Errorf("Env() = %q, want the two facts it carries and nothing blanked", got)
	}

	scheduled := domain.SeamPayload{
		Event:    domain.MomentTurnFinished,
		Reaction: "notify",
		Schedule: &domain.ScheduleRef{ID: "nightly", Name: "Nightly docs sweep"},
	}
	if got := scheduled.Env(); !reflect.DeepEqual(got, []string{
		"APOGEE_REACTION_EVENT=turn-finished",
		"APOGEE_REACTION_NAME=notify",
		"APOGEE_REACTION_SCHEDULE_ID=nightly",
		"APOGEE_REACTION_SCHEDULE_NAME=Nightly docs sweep",
	}) {
		t.Errorf("Env() = %q, want the Schedule id and name after the four headline facts", got)
	}
}
