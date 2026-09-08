package reactions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestEventsIsTheWholeVocabularyInOrder pins the eleven events and their documented order — the
// order a config template lists and an error message names them in. The six standalone notices
// come first, in the order this package has always listed them, then the five seam-closing
// notices in loop order.
func TestEventsIsTheWholeVocabularyInOrder(t *testing.T) {
	t.Parallel()

	got := Events()

	want := []Event{
		"exchange-finished", "turn-finished", "file-changed", "approval-waiting",
		"approval-decided", "error", "pre-request-finished", "post-response-finished",
		"pre-tool-exec-finished", "post-tool-result-finished", "history-rewrite-finished",
	}
	if len(got) != len(want) {
		t.Fatalf("Events() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Events()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestEventsReturnsACopy proves a caller cannot rewrite the vocabulary through the slice it got.
func TestEventsReturnsACopy(t *testing.T) {
	t.Parallel()

	Events()[0] = "tampered"

	if Events()[0] != ExchangeFinished {
		t.Errorf("Events()[0] = %q after a caller wrote to a previous result, want %q", Events()[0], ExchangeFinished)
	}
}

// TestParseEvent accepts every spelling in the vocabulary and refuses anything else with a
// message that lists the allowed spellings — a misspelt event is the likeliest config mistake.
func TestParseEvent(t *testing.T) {
	t.Parallel()

	for _, want := range Events() {
		t.Run(string(want), func(t *testing.T) {
			t.Parallel()

			got, err := ParseEvent(string(want))

			if err != nil {
				t.Fatalf("ParseEvent(%q) returned %v, want no error", want, err)
			}
			if got != want {
				t.Errorf("ParseEvent(%q) = %q, want %q", want, got, want)
			}
		})
	}

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()

		_, err := ParseEvent("turn-started")

		if err == nil {
			t.Fatal("ParseEvent(\"turn-started\") returned no error, want one")
		}
		if !strings.Contains(err.Error(), "turn-finished") {
			t.Errorf("ParseEvent error = %q, want it to list the allowed events", err)
		}
	})
}

// validHook is the smallest entry that passes, for a table to vary one field of.
func validHook() Hook {
	return Hook{
		Name:    "notify",
		Events:  []Event{TurnFinished},
		Command: []string{"say", "done"},
		Timeout: 30 * time.Second,
	}
}

// TestHookValidateAcceptsTheValidShapes covers the six shapes a `hooks:` entry may take: a bare
// command, a command scoped to a workspace, a command on several events, a bare webhook, a
// webhook with literal headers, and a webhook with env-referenced headers.
func TestHookValidateAcceptsTheValidShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		hook Hook
	}{
		{"command", validHook()},
		{"command scoped to a workspace", func() Hook {
			h := validHook()
			h.Workspace = "/work/repo"
			return h
		}()},
		{"command on several events", func() Hook {
			h := validHook()
			h.Events = []Event{TurnFinished, ExchangeFinished, FileChanged, ApprovalWaiting, Error}
			return h
		}()},
		{"webhook", Hook{
			Name:    "post",
			Events:  []Event{Error},
			Webhook: "https://example.test/hook",
			Timeout: time.Second,
		}},
		{"webhook with literal headers", Hook{
			Name:    "post",
			Events:  []Event{Error},
			Webhook: "http://example.test/hook",
			Headers: map[string]string{"X-Origin": "apogee"},
			Timeout: time.Second,
		}},
		{"webhook with env headers", Hook{
			Name:       "post",
			Events:     []Event{Error},
			Webhook:    "https://example.test/hook",
			HeadersEnv: map[string]string{"Authorization": "APOGEE_HOOK_TOKEN"},
			Timeout:    time.Second,
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if err := c.hook.Validate(); err != nil {
				t.Errorf("Validate() = %v, want no error for a valid %s entry", err, c.name)
			}
		})
	}
}

// TestHookValidateRefusesEachRule checks every rule of the entry shape, and that the message
// names the entry so a user with several Hooks is told which line to fix.
func TestHookValidateRefusesEachRule(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		hook     Hook
		wantText string
		unnamed  bool
	}{
		{name: "no name", hook: func() Hook {
			h := validHook()
			h.Name = "  "
			return h
		}(), wantText: "no name", unnamed: true},
		{name: "no events", hook: func() Hook {
			h := validHook()
			h.Events = nil
			return h
		}(), wantText: "no events"},
		{name: "unknown event", hook: func() Hook {
			h := validHook()
			h.Events = []Event{"turn-started"}
			return h
		}(), wantText: "unknown hook event"},
		{name: "both actions", hook: func() Hook {
			h := validHook()
			h.Webhook = "https://example.test/hook"
			return h
		}(), wantText: "exactly one"},
		{name: "neither action", hook: func() Hook {
			h := validHook()
			h.Command = nil
			return h
		}(), wantText: "exactly one"},
		{name: "blank argv[0]", hook: func() Hook {
			h := validHook()
			h.Command = []string{"   ", "done"}
			return h
		}(), wantText: "must not be blank"},
		{name: "headers on a command", hook: func() Hook {
			h := validHook()
			h.Headers = map[string]string{"X-Origin": "apogee"}
			return h
		}(), wantText: "belong to a `webhook:` entry"},
		{name: "headers-env on a command", hook: func() Hook {
			h := validHook()
			h.HeadersEnv = map[string]string{"Authorization": "TOKEN"}
			return h
		}(), wantText: "belong to a `webhook:` entry"},
		{name: "relative webhook", hook: Hook{
			Name: "post", Events: []Event{Error}, Webhook: "example.test/hook", Timeout: time.Second,
		}, wantText: "absolute http:// or https:// URL"},
		{name: "non-http webhook", hook: Hook{
			Name: "post", Events: []Event{Error}, Webhook: "ftp://example.test/hook", Timeout: time.Second,
		}, wantText: "absolute http:// or https:// URL"},
		{name: "hostless webhook", hook: Hook{
			Name: "post", Events: []Event{Error}, Webhook: "https:///hook", Timeout: time.Second,
		}, wantText: "names no host"},
		{name: "blank headers-env value", hook: Hook{
			Name: "post", Events: []Event{Error}, Webhook: "https://example.test/hook",
			HeadersEnv: map[string]string{"Authorization": " "}, Timeout: time.Second,
		}, wantText: "maps to no environment variable name"},
		{name: "zero timeout", hook: func() Hook {
			h := validHook()
			h.Timeout = 0
			return h
		}(), wantText: "not a positive duration"},
		{name: "negative timeout", hook: func() Hook {
			h := validHook()
			h.Timeout = -time.Second
			return h
		}(), wantText: "not a positive duration"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			err := c.hook.Validate()

			if err == nil {
				t.Fatalf("Validate() returned no error for %s, want one", c.name)
			}
			if !strings.Contains(err.Error(), c.wantText) {
				t.Errorf("Validate() = %q, want it to contain %q", err, c.wantText)
			}
			if !c.unnamed && !strings.Contains(err.Error(), `hook "`+c.hook.Name+`"`) {
				t.Errorf("Validate() = %q, want it to name the entry %q", err, c.hook.Name)
			}
		})
	}
}

// TestValidateAllRefusesDuplicateNames — the name is the identity a failure notice, the de-dup
// record and the payload's "hook" field all key on, so two entries may not share one.
func TestValidateAllRefusesDuplicateNames(t *testing.T) {
	t.Parallel()

	first, second := validHook(), validHook()
	second.Events = []Event{Error}

	err := ValidateAll([]Hook{first, second})

	if err == nil {
		t.Fatal("ValidateAll returned no error for two entries named \"notify\", want one")
	}
	if !strings.Contains(err.Error(), "unique") {
		t.Errorf("ValidateAll = %q, want it to say names must be unique", err)
	}
}

// TestValidateAllAcceptsDistinctEntriesAndAnEmptyList — no Hooks configured is the ordinary case.
func TestValidateAllAcceptsDistinctEntriesAndAnEmptyList(t *testing.T) {
	t.Parallel()

	second := validHook()
	second.Name = "post"

	if err := ValidateAll([]Hook{validHook(), second}); err != nil {
		t.Errorf("ValidateAll(two distinct entries) = %v, want no error", err)
	}
	if err := ValidateAll(nil); err != nil {
		t.Errorf("ValidateAll(nil) = %v, want no error", err)
	}
}

// TestValidateAllReportsAMalformedEntry — the whole-list check runs each entry's own rules.
func TestValidateAllReportsAMalformedEntry(t *testing.T) {
	t.Parallel()

	broken := validHook()
	broken.Name = "broken"
	broken.Command = nil

	err := ValidateAll([]Hook{validHook(), broken})

	if err == nil || !strings.Contains(err.Error(), `hook "broken"`) {
		t.Errorf("ValidateAll = %v, want the failure to name the broken entry", err)
	}
}

// TestSubscribedEventsIsTheUnion — the set the matcher is built over.
func TestSubscribedEventsIsTheUnion(t *testing.T) {
	t.Parallel()

	first, second := validHook(), validHook()
	second.Name = "post"
	second.Events = []Event{TurnFinished, Error}

	got := SubscribedEvents([]Hook{first, second})

	want := map[Event]bool{TurnFinished: true, Error: true}
	if len(got) != len(want) {
		t.Fatalf("SubscribedEvents = %v, want %v", got, want)
	}
	for e := range want {
		if !got[e] {
			t.Errorf("SubscribedEvents is missing %q", e)
		}
	}
	if len(SubscribedEvents(nil)) != 0 {
		t.Errorf("SubscribedEvents(nil) = %v, want an empty set", SubscribedEvents(nil))
	}
}

// TestResolveWorkspaceFollowsSymlinks — the filter and the root are compared through this one
// resolution, so a workspace reached through a link must reduce to the same string either way.
func TestResolveWorkspaceFollowsSymlinks(t *testing.T) {
	t.Parallel()

	realDir := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("create the workspace: %v", err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("this host cannot create a symlink: %v", err)
	}

	viaLink, err := ResolveWorkspace(link)
	direct, directErr := ResolveWorkspace(realDir)

	if err != nil || directErr != nil {
		t.Fatalf("ResolveWorkspace returned %v / %v, want no error", err, directErr)
	}
	want, symErr := filepath.EvalSymlinks(realDir)
	if symErr != nil {
		t.Fatalf("EvalSymlinks(%q): %v", realDir, symErr)
	}
	if viaLink != want || direct != want {
		t.Errorf("ResolveWorkspace(link) = %q and ResolveWorkspace(real) = %q, want both %q", viaLink, direct, want)
	}
}

// TestResolveWorkspaceEmptyIsTheUnsetFilter — an entry that scopes itself to nothing is active
// everywhere, and callers get that without special-casing the empty string first.
func TestResolveWorkspaceEmptyIsTheUnsetFilter(t *testing.T) {
	t.Parallel()

	got, err := ResolveWorkspace("")

	if err != nil || got != "" {
		t.Errorf("ResolveWorkspace(\"\") = (%q, %v), want (\"\", nil)", got, err)
	}
}

// TestResolveWorkspaceExpandsALeadingTilde — a config may name the path the way a shell would.
func TestResolveWorkspaceExpandsALeadingTilde(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("this host has no home directory: %v", err)
	}

	got, err := ResolveWorkspace("~/projects/apogee")

	if err != nil {
		t.Fatalf("ResolveWorkspace = %v, want no error", err)
	}
	want, _ := ResolveWorkspace(filepath.Join(home, "projects", "apogee"))
	if got != want {
		t.Errorf("ResolveWorkspace(\"~/projects/apogee\") = %q, want %q", got, want)
	}
}

// TestResolveWorkspaceLeavesANonLeadingTildeAlone — `~` is a legal filename character.
func TestResolveWorkspaceLeavesANonLeadingTildeAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	odd := filepath.Join(dir, "backup~")

	got, err := ResolveWorkspace(odd)

	if err != nil {
		t.Fatalf("ResolveWorkspace = %v, want no error", err)
	}
	if !strings.HasSuffix(got, "backup~") {
		t.Errorf("ResolveWorkspace(%q) = %q, want the trailing ~ preserved", odd, got)
	}
}

// TestEventValuesArePinnedLiterals pins each Event constant to its exact spelling. The values
// are a stable contract — they are what a user writes under `events:`, what reaches a fired
// command as APOGEE_HOOK_EVENT, and now also the notice Moments of the Reaction core — so
// re-homing the type behind an alias must not move a single byte.
func TestEventValuesArePinnedLiterals(t *testing.T) {
	t.Parallel()

	pins := []struct {
		got  Event
		want string
	}{
		{ExchangeFinished, "exchange-finished"},
		{TurnFinished, "turn-finished"},
		{FileChanged, "file-changed"},
		{ApprovalWaiting, "approval-waiting"},
		{Error, "error"},
	}
	for _, pin := range pins {
		if string(pin.got) != pin.want {
			t.Errorf("event constant = %q, want %q", string(pin.got), pin.want)
		}
	}
}

// TestEventsAreTheNoticeMoments proves the two vocabularies are one set in one order, not two
// that happen to agree: Event is an alias for domain.Moment, so a notice added on either side
// without the other would show up here.
func TestEventsAreTheNoticeMoments(t *testing.T) {
	t.Parallel()

	got, want := Events(), domain.Notices()

	if len(got) != len(want) {
		t.Fatalf("Events() = %v, want the notice Moments %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Events()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestParseEventErrorTextIsByteIdentical holds the refusal message to the byte. A misspelt event
// name is the likeliest mistake in a `hooks:` block, so the sentence that lists the vocabulary is
// user-facing text: it changes only when the vocabulary itself does, and then deliberately.
func TestParseEventErrorTextIsByteIdentical(t *testing.T) {
	t.Parallel()

	_, err := ParseEvent("turn-started")

	if err == nil {
		t.Fatal("ParseEvent(\"turn-started\") returned no error, want one")
	}
	const want = `unknown hook event "turn-started" — the events are ` +
		`exchange-finished, turn-finished, file-changed, approval-waiting, approval-decided, ` +
		`error, pre-request-finished, post-response-finished, pre-tool-exec-finished, ` +
		`post-tool-result-finished, history-rewrite-finished`
	if err.Error() != want {
		t.Errorf("ParseEvent error =\n%q\nwant\n%q", err.Error(), want)
	}
}
