package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/reactions"
)

// writeHooksConfig writes one config file holding the given body and returns its path.
func writeHooksConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// A `hooks:` block resolves entry for entry onto the user-origin observe Reactions a root's Runner
// fires: origin and class stamped, the events parsed, the one action the entry spells carried across
// whole as the handler that runs it, an absent `timeout:` defaulted to the ratified 30s and a
// spelled one parsed, and `workspace:` reduced to the comparable spelling with its leading `~`
// expanded. The home directory is moved for the case rather than read, so the `~` rule is asserted
// against a path this test owns.
func TestLoadFileConfigResolvesTheHooksBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := writeHooksConfig(t, `
hooks:
  - name: notify
    events: [exchange-finished, error]
    command: ["notify-send", "apogee finished"]
    workspace: ~/work
  - name: bell
    events: [approval-waiting]
    webhook: https://hooks.example.com/apogee
    headers:
      X-Source: apogee
    headers-env:
      Authorization: APOGEE_HOOK_TOKEN
    timeout: 250ms
`)

	opts, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}

	if len(opts.Hooks) != 2 {
		t.Fatalf("resolved %d hooks; want 2: %+v", len(opts.Hooks), opts.Hooks)
	}

	notify := opts.Hooks[0]
	wantWorkspace, err := reactions.ResolveWorkspace(filepath.Join(home, "work"))
	if err != nil {
		t.Fatalf("resolve the expected workspace: %v", err)
	}
	if notify.ID != "notify" {
		t.Errorf("notify id = %q; want the entry's own `name:`", notify.ID)
	}
	if notify.Origin != domain.OriginUser || notify.Class != domain.ClassObserve {
		t.Errorf("notify origin/class = %q/%q; want %q/%q — a `hooks:` entry is the user's observe row",
			notify.Origin, notify.Class, domain.OriginUser, domain.ClassObserve)
	}
	if got, want := notify.On, []reactions.Event{reactions.ExchangeFinished, reactions.Error}; !eventsEqual(got, want) {
		t.Errorf("notify events = %v; want %v", got, want)
	}
	argv, ok := notify.Handler.(domain.ArgvHandler)
	if !ok {
		t.Fatalf("notify handler = %T; want a domain.ArgvHandler for a `command:` entry", notify.Handler)
	}
	if got, want := strings.Join(argv.Argv, " "), "notify-send apogee finished"; got != want {
		t.Errorf("notify command = %q; want %q", got, want)
	}
	if notify.Timeout != defaultHookTimeout {
		t.Errorf("notify timeout = %v; want the %v default an absent `timeout:` takes", notify.Timeout,
			defaultHookTimeout)
	}
	if notify.Workspace != wantWorkspace {
		t.Errorf("notify workspace = %q; want %q — the leading ~ is expanded before the filter is compared",
			notify.Workspace, wantWorkspace)
	}

	bell := opts.Hooks[1]
	if bell.Timeout != 250*time.Millisecond {
		t.Errorf("bell timeout = %v; want 250ms — a spelled duration replaces the default", bell.Timeout)
	}
	webhook, ok := bell.Handler.(domain.WebhookHandler)
	if !ok {
		t.Fatalf("bell handler = %T; want a domain.WebhookHandler for a `webhook:` entry", bell.Handler)
	}
	if webhook.URL != "https://hooks.example.com/apogee" {
		t.Errorf("bell webhook = %q; want the URL the file spells", webhook.URL)
	}
	if got := webhook.Headers["X-Source"]; got != "apogee" {
		t.Errorf("bell literal header = %q; want apogee", got)
	}
	if got := webhook.HeadersEnv["Authorization"]; got != "APOGEE_HOOK_TOKEN" {
		t.Errorf("bell headers-env value = %q; want the variable NAME the header is read from", got)
	}
	if bell.Workspace != "" {
		t.Errorf("bell workspace = %q; want empty — an unspelled filter is active everywhere", bell.Workspace)
	}
}

// eventsEqual compares two event lists element by element.
func eventsEqual(got, want []reactions.Event) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// A `hooks:` block that cannot be run is refused at LOAD, naming the entry — a Hook that silently
// never fires is the one failure a user could not diagnose, since nothing a Hook does is visible in
// the conversation. Every rule the entry shape carries has a row here, and each asserts the entry's
// own name reaches the message.
func TestLoadFileConfigRefusesAnUnrunnableHook(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
		// key is the prefix the refusal carries: the config layer's own `hook "…"` for a rule it
		// owns, the reactions package's `reaction "…"` for one the Runner's Validate owns.
		key string
	}{
		{
			name: "duplicate names",
			body: `
hooks:
  - name: notify
    events: [error]
    command: ["true"]
  - name: notify
    events: [error]
    command: ["false"]
`,
			want: "must be unique",
			key:  `reaction "notify"`,
		},
		{
			name: "unknown event",
			body: `
hooks:
  - name: notify
    events: [exchange-started]
    command: ["true"]
`,
			want: "unknown hook event",
			key:  `hook "notify"`,
		},
		{
			name: "both actions",
			body: `
hooks:
  - name: notify
    events: [error]
    command: ["true"]
    webhook: https://hooks.example.com/apogee
`,
			want: "exactly one",
			key:  `hook "notify"`,
		},
		{
			name: "no action",
			body: `
hooks:
  - name: notify
    events: [error]
`,
			want: "exactly one",
			key:  `hook "notify"`,
		},
		{
			name: "headers on a command",
			body: `
hooks:
  - name: notify
    events: [error]
    command: ["true"]
    headers:
      X-Source: apogee
`,
			want: "belong to a `webhook:` entry",
			key:  `hook "notify"`,
		},
		{
			name: "webhook is not http",
			body: `
hooks:
  - name: notify
    events: [error]
    webhook: ftp://files.example.com/drop
`,
			want: "absolute http:// or https:// URL",
			key:  `reaction "notify"`,
		},
		{
			name: "timeout is not a duration",
			body: `
hooks:
  - name: notify
    events: [error]
    command: ["true"]
    timeout: soon
`,
			want: "is not a duration",
			key:  `hook "notify"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := LoadFileConfig(writeHooksConfig(t, tc.body), os.ReadFile, noNotify)

			if err == nil {
				t.Fatalf("LoadFileConfig accepted %s; want a refusal", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal = %q; want it to explain %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Errorf("refusal = %q; want %s so the user knows which line to fix", err, tc.key)
			}
		})
	}
}

// An empty or absent block resolves to no Hooks and no error: the feature is dormant by default,
// exactly as `mcp-servers:` is.
func TestLoadFileConfigWithoutHooksResolvesNone(t *testing.T) {
	t.Parallel()

	opts, err := LoadFileConfig(writeHooksConfig(t, "mode: auto\n"), os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	if opts.Hooks != nil {
		t.Errorf("resolved hooks = %+v; want none for a file that states no block", opts.Hooks)
	}
}

// The names a root keeps the `terminal` tool away from: every variable a webhook header is read
// from, each once, in a stable order — the values come off a map, so an unsorted answer would
// reorder between runs and make every caller's own output unstable.
func TestHookEnvNamesDeduplicatesAndSorts(t *testing.T) {
	t.Parallel()

	opts := Options{Hooks: []domain.Reaction{
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

	got := HookEnvNames(opts)

	want := []string{"TOKEN_A", "TOKEN_B"}
	if len(got) != len(want) {
		t.Fatalf("HookEnvNames = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("HookEnvNames = %v; want %v", got, want)
		}
	}
}
