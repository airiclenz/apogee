package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// writeReactionsConfig writes one config file holding the given body and returns its path.
func writeReactionsConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// A `reactions:` block resolves entry for entry onto the user-origin observe Reactions a root's
// Runner fires: origin and class stamped, the `on:` list read as Moments, the one action `run:`
// spells turned into the handler that runs it — a sequence into an argv command, a mapping into a
// webhook — an absent `timeout:` defaulted to the ratified 30s and a spelled one parsed,
// `workspace:` reduced to the comparable spelling with its leading `~` expanded, and a parked entry
// dropped rather than armed. The home directory is moved for the case rather than read, so the `~`
// rule is asserted against a path this test owns.
func TestLoadFileConfigResolvesTheReactionsBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := writeReactionsConfig(t, `
reactions:
  - id: notify
    on: [exchange-finished, error]
    run: ["notify-send", "apogee finished"]
    workspace: ~/work
  - id: bell
    on: [approval-requested]
    run:
      url: https://hooks.example.com/apogee
      headers:
        X-Source: apogee
      headers-env:
        Authorization: MY_WEBHOOK_TOKEN
    timeout: 250ms
  - id: parked
    on: [turn-finished]
    run: ["true"]
    enabled: false
`)

	opts, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}

	if len(opts.Reactions) != 2 {
		t.Fatalf("resolved %d reactions; want the 2 armed entries (the parked one is dropped): %+v",
			len(opts.Reactions), opts.Reactions)
	}

	notify := opts.Reactions[0]
	if notify.ID != "notify" {
		t.Errorf("first id = %q, want %q", notify.ID, "notify")
	}
	if notify.Origin != domain.OriginUser || notify.Class != domain.ClassObserve {
		t.Errorf("first entry is %s/%s, want %s/%s",
			notify.Origin, notify.Class, domain.OriginUser, domain.ClassObserve)
	}
	wantOn := []domain.Moment{domain.MomentExchangeFinished, domain.MomentError}
	if len(notify.On) != len(wantOn) {
		t.Fatalf("first entry fires on %v, want %v", notify.On, wantOn)
	}
	for i := range wantOn {
		if notify.On[i] != wantOn[i] {
			t.Fatalf("first entry fires on %v, want %v", notify.On, wantOn)
		}
	}
	argv, ok := notify.Handler.(domain.ArgvHandler)
	if !ok {
		t.Fatalf("first handler = %T; want a domain.ArgvHandler for a `run:` sequence", notify.Handler)
	}
	if len(argv.Argv) != 2 || argv.Argv[0] != "notify-send" || argv.Argv[1] != "apogee finished" {
		t.Errorf("first argv = %v, want the two elements the sequence spells", argv.Argv)
	}
	if notify.Timeout != defaultReactionTimeout {
		t.Errorf("first timeout = %v, want the %v default an entry spelling none takes",
			notify.Timeout, defaultReactionTimeout)
	}
	if notify.Workspace == "" || !strings.HasSuffix(notify.Workspace, "work") {
		t.Errorf("first workspace = %q, want the expanded ~/work under %q", notify.Workspace, home)
	}

	bell := opts.Reactions[1]
	webhook, ok := bell.Handler.(domain.WebhookHandler)
	if !ok {
		t.Fatalf("second handler = %T; want a domain.WebhookHandler for a `run:` mapping", bell.Handler)
	}
	if webhook.URL != "https://hooks.example.com/apogee" {
		t.Errorf("webhook url = %q, want the one the mapping spells", webhook.URL)
	}
	if webhook.Headers["X-Source"] != "apogee" {
		t.Errorf("webhook headers = %v, want the literal X-Source the mapping spells", webhook.Headers)
	}
	if webhook.HeadersEnv["Authorization"] != "MY_WEBHOOK_TOKEN" {
		t.Errorf("webhook headers-env = %v, want the variable NAME the mapping spells", webhook.HeadersEnv)
	}
	if bell.Timeout != 250*time.Millisecond {
		t.Errorf("second timeout = %v, want the 250ms the entry spells", bell.Timeout)
	}
	if bell.Workspace != "" {
		t.Errorf("second workspace = %q, want the unset filter for an entry naming none", bell.Workspace)
	}
}

// An empty or absent block resolves to no Reactions and no error: the lane is dormant by default,
// exactly as `mcp-servers:` is. A block whose every entry is parked resolves the same way, so a
// file that keeps its definitions without arming them is indistinguishable from one that has none.
func TestLoadFileConfigWithoutArmedReactionsResolvesNone(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		body string
	}{
		{name: "no block at all", body: "mode: auto\n"},
		{name: "every entry parked", body: "reactions:\n  - id: parked\n    on: [error]\n    run: [\"true\"]\n    enabled: false\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts, err := LoadFileConfig(writeReactionsConfig(t, tt.body), os.ReadFile, noNotify)
			if err != nil {
				t.Fatalf("LoadFileConfig: %v", err)
			}
			if opts.Reactions != nil {
				t.Errorf("resolved reactions = %+v; want none", opts.Reactions)
			}
		})
	}
}

// Every refusal the `reactions:` block can earn, pinned by its sentence. A user meets these at
// startup with nothing else to go on, so the wording is the contract: each one names the entry it
// is about and says what to write instead.
func TestLoadFileConfigRefusesMalformedReactions(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "run: is a bare string",
			body: "reactions:\n  - id: notify\n    on: [error]\n    run: notify-send\n",
			want: `reaction "notify": run: is an argv list or a webhook mapping {url:, headers:, headers-env:}`,
		},
		{
			name: "run: is absent",
			body: "reactions:\n  - id: notify\n    on: [error]\n",
			want: `reaction "notify": run: is an argv list or a webhook mapping {url:, headers:, headers-env:}`,
		},
		{
			name: "the webhook mapping carries a key it does not have",
			body: "reactions:\n  - id: notify\n    on: [error]\n    run:\n      url: https://example.com/\n      header-env:\n        A: B\n",
			want: `reaction "notify": run: is an argv list or a webhook mapping {url:, headers:, headers-env:}`,
		},
		{
			name: "advise: is spelled",
			body: "reactions:\n  - id: coach\n    on: [post-response]\n    advise: [\"say-something\"]\n",
			want: `reaction "coach": advise: is not yet shipped (ADR 0076 stage 3)`,
		},
		{
			name: "gate: is spelled",
			body: "reactions:\n  - id: warden\n    on: [pre-tool-exec]\n    gate: [\"decide\"]\n",
			want: `reaction "warden": gate: is not yet shipped (ADR 0076 stage 3)`,
		},
		{
			name: "the id is a Floor guard's key",
			body: "reactions:\n  - id: tool-use-enforcer\n    on: [error]\n    run: [\"true\"]\n",
			want: `reaction "tool-use-enforcer": that is the Floor guard tool-use-enforcer: — set the top-level key, not a reactions: entry`,
		},
		{
			name: "on: names a seam",
			body: "reactions:\n  - id: shaper\n    on: [pre-request]\n    run: [\"true\"]\n",
			want: `invalid reaction "shaper": run: reacts to notices; "pre-request" is a seam`,
		},
		{
			name: "on: names no moment at all",
			body: "reactions:\n  - id: notify\n    on: [turn-done]\n    run: [\"true\"]\n",
			want: `reaction "notify": unknown reaction event "turn-done"`,
		},
		{
			name: "the entry has no id",
			body: "reactions:\n  - on: [error]\n    run: [\"true\"]\n",
			want: "reactions: an entry has no id: every reaction needs an `id:` to be reported by",
		},
		{
			name: "timeout: is not a duration",
			body: "reactions:\n  - id: notify\n    on: [error]\n    run: [\"true\"]\n    timeout: soon\n",
			want: "reaction \"notify\": timeout: \"soon\" is not a duration — write it as `30s` or `2m`",
		},
		{
			name: "an id is spelled twice",
			body: "reactions:\n  - id: notify\n    on: [error]\n    run: [\"true\"]\n  - id: notify\n    on: [turn-finished]\n    run: [\"true\"]\n",
			want: `reaction "notify": a second entry has this name`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadFileConfig(writeReactionsConfig(t, tt.body), os.ReadFile, noNotify)
			if err == nil {
				t.Fatalf("LoadFileConfig accepted the block; want the refusal %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("refusal = %q; want it to carry %q", err, tt.want)
			}
		})
	}
}

// The ids an entry may not take are the seven Floor-guard config keys, and the list is a literal —
// so this is what keeps it honest: every name in it is a real registry key, and a guard key renamed
// out from under it fails here rather than silently letting a `reactions:` entry take the name.
func TestFloorGuardKeysAreRegistryKeys(t *testing.T) {
	t.Parallel()

	if len(floorGuardKeys) != 7 {
		t.Errorf("floorGuardKeys names %d keys, want the seven Floor guards", len(floorGuardKeys))
	}
	for _, key := range floorGuardKeys {
		if _, ok := LookupKey(key); !ok {
			t.Errorf("floorGuardKeys names %q, which is no registry key", key)
		}
	}
}

// The names a root keeps the `terminal` tool away from: every variable a webhook header is read
// from, each once, in a stable order — the values come off a map, so an unsorted answer would
// reorder between runs and make every caller's own output unstable.
func TestReactionEnvNamesDeduplicatesAndSorts(t *testing.T) {
	t.Parallel()

	opts := Options{Reactions: []domain.Reaction{
		{ID: "bell", Handler: domain.WebhookHandler{
			HeadersEnv: map[string]string{"Authorization": "TOKEN_B", "X-Trace": "TOKEN_A"},
		}},
		{ID: "page", Handler: domain.WebhookHandler{
			HeadersEnv: map[string]string{"Authorization": "TOKEN_B"},
		}},
		{ID: "quiet", Handler: domain.WebhookHandler{
			HeadersEnv: map[string]string{"X-Blank": "  "},
		}},
		{ID: "run", Handler: domain.ArgvHandler{Argv: []string{"true"}}},
	}}

	got := ReactionEnvNames(opts)

	want := []string{"TOKEN_A", "TOKEN_B"}
	if len(got) != len(want) {
		t.Fatalf("ReactionEnvNames = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ReactionEnvNames = %v; want %v", got, want)
		}
	}
}
