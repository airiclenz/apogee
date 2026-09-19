package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestUnknownKeysWalksTheSchemaAsDeepAsItGoes pins what the walk reports, and — the half that
// matters as much — what it does not: a key under an any-typed action, the retired `sub-agents:`
// flag, the retired top-level `step-budget-notice:` switch, and a document with no root mapping
// all walk to nothing.
func TestUnknownKeysWalksTheSchemaAsDeepAsItGoes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		given string
		want  []keyAt
	}{
		{
			name:  "a misspelled top-level key names its line",
			given: "auto-compct: true\n",
			want:  []keyAt{{key: "auto-compct", line: 1}},
		},
		{
			name:  "a misspelled key under a block is spelled dotted",
			given: "mode: plan\nui:\n  spiner: dots\n",
			want:  []keyAt{{key: "ui.spiner", line: 3}},
		},
		{
			name:  "a servers entry is walked by index, and its known keys are not reported",
			given: "servers: [{name: a, plaintext-key-ok: true, foo: 1}]\n",
			want:  []keyAt{{key: "servers[0].foo", line: 1}},
		},
		{
			name:  "an mcp-servers entry is a struct slice the walk descends",
			given: "mcp-servers: [{name: a, transport: stdio, foo: 1}]\n",
			want:  []keyAt{{key: "mcp-servers[0].foo", line: 1}},
		},
		{
			name:  "a model profile is walked under its own name",
			given: "model-profiles:\n  fast:\n    thinking:\n      style: none\n    foo: 1\n",
			want:  []keyAt{{key: "model-profiles.fast.foo", line: 5}},
		},
		{
			name:  "the any-typed action keys of a reaction are not descended",
			given: "reactions: [{id: r, on: [x], run: {url: u, body: {deep: 1}}}]\n",
			want:  nil,
		},
		{
			name:  "the retired sub-agents flag is exempt on a servers entry",
			given: "servers: [{name: a, sub-agents: true}]\n",
			want:  nil,
		},
		{
			name:  "the exemption is the servers entry's alone",
			given: "sub-agents: true\n",
			want:  []keyAt{{key: "sub-agents", line: 1}},
		},
		{
			name:  "the retired step-budget-notice switch is exempt at the top level",
			given: "step-budget-notice: true\n",
			want:  nil,
		},
		{
			name:  "the step-budget-notice exemption is the top level's alone",
			given: "ui:\n  step-budget-notice: true\n",
			want:  []keyAt{{key: "ui.step-budget-notice", line: 2}},
		},
		{
			name:  "an empty file walks to nothing",
			given: "",
			want:  nil,
		},
		{
			name:  "a comment-only file walks to nothing",
			given: "# apogee configuration\n# nothing set yet\n",
			want:  nil,
		},
		{
			name:  "a document that does not parse is the decoder's error, not a set of keys",
			given: "mode: [\n",
			want:  nil,
		},
		{
			name:  "several unknown keys come back in file order",
			given: "auto-compct: true\nui:\n  spiner: dots\nservers:\n  - name: a\n    foo: 1\n",
			want: []keyAt{
				{key: "auto-compct", line: 1},
				{key: "ui.spiner", line: 3},
				{key: "servers[0].foo", line: 6},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := unknownKeys([]byte(tt.given))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("unknownKeys(%q) = %+v, want %+v", tt.given, got, tt.want)
			}
		})
	}
}

// TestParseConfigFileAnnouncesUnknownKeysThroughNotify pins the one line the loader prints per
// unknown key — path, dotted key, line — and that it is a notice: the file still loads.
func TestParseConfigFileAnnouncesUnknownKeysThroughNotify(t *testing.T) {
	t.Parallel()
	path := writeMigrationConfig(t, "auto-compct: true\nui:\n  spiner: dots\n")
	var notices []string
	fc, err := parseConfigFile(path, os.ReadFile, func(s string) { notices = append(notices, s) }, true)
	if err != nil {
		t.Fatalf("parseConfigFile refused a file whose only fault is two unknown keys: %v", err)
	}
	if fc.UI == nil {
		t.Error("the ui: block did not load beside its unknown key")
	}
	want := []string{
		fmt.Sprintf(unknownKeyNotice, path, "auto-compct", 1),
		fmt.Sprintf(unknownKeyNotice, path, "ui.spiner", 3),
	}
	if !reflect.DeepEqual(notices, want) {
		t.Errorf("notices = %q, want %q", notices, want)
	}
}

// TestParseConfigFileNeverReportsAKeyTheMigrationOwns asserts the retired keys are consumed
// before the walk or refused with an error — never announced a second time as unknown. The
// quadruple and the `hooks:`/`mechanisms:`/`validated-sets:` blocks are folded or stripped by the
// startup migration; `llama-launcher:` and `model-profile:` are refused before it.
func TestParseConfigFileNeverReportsAKeyTheMigrationOwns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		given      string
		wantRefuse bool
	}{
		{
			name:  "the legacy quadruple is folded into servers:",
			given: "endpoint: http://box:1111\napi-key: k\nhost-alias: box\nmodel: m\nmode: plan\n",
		},
		{
			name: "a hooks: block is folded into reactions:",
			given: "servers:\n  - name: box\n    endpoint: http://box:1111\nserver: box\n" +
				"hooks:\n  - name: notify\n    events: [approval-waiting]\n    command: [\"notify-send\"]\n",
		},
		{
			name: "the mechanisms: key is stripped, top-level and per-server",
			given: "mechanisms:\n  tool_use_enforcer: false\nservers:\n  - name: box\n" +
				"    endpoint: http://box:1111\n    mechanisms:\n      validate: true\nserver: box\n",
		},
		{
			name: "the validated-sets: block is stripped",
			given: "servers:\n  - name: box\n    endpoint: http://box:1111\nserver: box\n" +
				"validated-sets:\n  enable: false\n",
		},
		{
			name:       "the retired llama-launcher: key is refused",
			given:      "servers:\n  - name: box\n    endpoint: http://box:1111\nserver: box\nllama-launcher: auto\n",
			wantRefuse: true,
		},
		{
			name:       "the retired model-profile: key is refused",
			given:      "servers:\n  - name: box\n    endpoint: http://box:1111\nserver: box\nmodel-profile:\n  tools: {}\n",
			wantRefuse: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeMigrationConfig(t, tt.given)
			var notices []string
			_, err := parseConfigFile(path, os.ReadFile, func(s string) { notices = append(notices, s) }, true)
			if tt.wantRefuse != (err != nil) {
				t.Fatalf("parseConfigFile: err = %v, want refusal %v", err, tt.wantRefuse)
			}
			for _, n := range notices {
				if strings.Contains(n, "unknown key") {
					t.Errorf("a key the migration owns was reported as unknown: %q", n)
				}
			}
		})
	}
}

// TestParseConfigFileIgnoresTheRetiredStepBudgetNotice pins the retired switch's whole contract at
// the loader, on the startup pass and the live re-read alike: a home seeded from the old starter
// template loads without error, without an unknown-key notice, without a value to land — the
// schema has no field for it — and without the file being rewritten, because the exemption
// (walkUnknownKeys) is read-only where the startup strips beside it back up and rewrite.
func TestParseConfigFileIgnoresTheRetiredStepBudgetNotice(t *testing.T) {
	t.Parallel()
	const given = "servers:\n  - name: box\n    endpoint: http://box:1111\nserver: box\n" +
		"context-fill-notice: false\nstep-budget-notice: true\n"
	for _, mayMigrate := range []bool{true, false} {
		t.Run(fmt.Sprintf("mayMigrate=%v", mayMigrate), func(t *testing.T) {
			t.Parallel()
			path := writeMigrationConfig(t, given)
			var notices []string
			fc, err := parseConfigFile(path, os.ReadFile, func(s string) { notices = append(notices, s) }, mayMigrate)
			if err != nil {
				t.Fatalf("parseConfigFile: %v", err)
			}
			if len(notices) != 0 {
				t.Errorf("parseConfigFile announced %q; want silence on the retired key", notices)
			}
			var opts Options
			if err := applyFile(&opts, fc); err != nil {
				t.Fatalf("applyFile: %v", err)
			}
			if opts.ContextFillNotice {
				t.Error("the neighbouring context-fill-notice: false was not read as false")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(after) != given {
				t.Errorf("the file was rewritten:\n%s\nwant it byte-identical to the input", after)
			}
			if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
				t.Errorf("the home holds %d entries after the read; want the one config, no backup", len(entries))
			}
		})
	}
}

// TestParseConfigFileStaysQuietOnAnEmptyOrCommentOnlyFile is the no-panic pledge at the loader:
// both shapes are accepted today, and the walk must not turn either into a start-up crash.
func TestParseConfigFileStaysQuietOnAnEmptyOrCommentOnlyFile(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{"empty": "", "comment-only": "# apogee\n"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			var notices []string
			if _, err := parseConfigFile(path, os.ReadFile, func(s string) { notices = append(notices, s) }, true); err != nil {
				t.Fatalf("parseConfigFile: %v", err)
			}
			if len(notices) != 0 {
				t.Errorf("notices = %q, want none", notices)
			}
		})
	}
}
