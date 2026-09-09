package domain_test

// The sync lane's stdin document (seampayload.go). The test package is EXTERNAL because the tag
// parity below reads internal/reactions' own payload, and that package imports internal/domain —
// an in-package test would be an import cycle.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/reactions"
)

// TestSeamPayloadSharesTheObservePayloadsKeys pins the promise that makes the two lanes one
// vocabulary: every field the sync document shares with the observe payload carries the same JSON
// key and the same Go type, so a user's script reads `event`, `turn` or `path` off a gate firing
// exactly as it reads them off an observe firing. Renaming one side alone fails here.
func TestSeamPayloadSharesTheObservePayloadsKeys(t *testing.T) {
	t.Parallel()

	shared := []string{"Event", "Reaction", "Time", "Workspace", "Depth", "Turn", "CallID", "Tool", "Path"}

	seam := reflect.TypeOf(domain.SeamPayload{})
	observe := reflect.TypeOf(reactions.Payload{})

	for _, name := range shared {
		seamField, ok := seam.FieldByName(name)
		if !ok {
			t.Errorf("domain.SeamPayload has no field %s", name)
			continue
		}
		observeField, ok := observe.FieldByName(name)
		if !ok {
			t.Errorf("reactions.Payload has no field %s", name)
			continue
		}
		if got, want := seamField.Tag.Get("json"), observeField.Tag.Get("json"); got != want {
			t.Errorf("%s json tag = %q on the sync document, %q on the observe payload", name, got, want)
		}
		if got, want := seamField.Type, observeField.Type; got != want {
			t.Errorf("%s type = %v on the sync document, %v on the observe payload", name, got, want)
		}
	}
}

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

// TestSeamPayloadEnv pins the four headline variables a one-line script reads instead of parsing
// stdin: their names, their fixed order, and the rule that a fact the payload does not carry is
// OMITTED rather than set empty — a variable inherited from the user's own environment must not be
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
}

// TestSeamPayloadEnvNamesMatchTheObserveLane pins that the two lanes export the SAME four variable
// names. They are declared twice — internal/reactions imports internal/domain, so the dependency
// cannot point back — and this is what keeps the copies honest.
func TestSeamPayloadEnvNamesMatchTheObserveLane(t *testing.T) {
	t.Parallel()

	pairs := [][2]string{
		{domain.EnvReactionEvent, reactions.EnvEvent},
		{domain.EnvReactionName, reactions.EnvName},
		{domain.EnvReactionWorkspace, reactions.EnvWorkspace},
		{domain.EnvReactionPath, reactions.EnvPath},
	}
	for _, pair := range pairs {
		if pair[0] != pair[1] {
			t.Errorf("sync lane exports %q where the observe lane exports %q", pair[0], pair[1])
		}
	}
}
