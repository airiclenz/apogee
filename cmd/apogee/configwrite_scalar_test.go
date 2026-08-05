package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The hand-edited config every golden case starts from: comments, a commented example above each
// active setting, an active block with a trailing note on its one child, and a key whose example
// is present but commented out. What the goldens pin is that a write touches ONE line of it.
const editedConfigFixture = "settings-edited.yaml"

// readFixture reads a testdata config as a string.
func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read the fixture %s: %v", name, err)
	}
	return string(data)
}

// compareGolden compares got against testdata/<name>, writing the file when it is not there yet so
// a new case is reviewed as a diff rather than typed out by hand — and failing when it does, so a
// golden can never be created silently by a run that was meant to check one.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	want, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("write the new golden %s: %v", path, err)
		}
		t.Fatalf("%s did not exist; it has been written from this run's output — review it and re-run", path)
	case err != nil:
		t.Fatalf("read %s: %v", path, err)
	case got != string(want):
		t.Errorf("the spliced config does not match %s\n--- got ---\n%s--- want ---\n%s", path, got, want)
	}
}

// splicedInsertion reports the 1-based line the splice inserted at and the lines it inserted,
// failing when the difference between the two files is anything but one insertion — the property
// that matters more than the bytes: a comment-preserving writer adds lines and changes none.
func splicedInsertion(t *testing.T, before, after string) (int, []string) {
	t.Helper()
	b, a := splitConfigLines([]byte(before)), splitConfigLines([]byte(after))
	if len(a) <= len(b) {
		t.Fatalf("want an insertion, got %d lines from %d", len(a), len(b))
	}
	at := 0
	for at < len(b) && b[at] == a[at] {
		at++
	}
	added := len(a) - len(b)
	if !slices.Equal(b[at:], a[at+added:]) {
		t.Fatalf("the splice changed more than it inserted, from line %d:\n%s", at+1, after)
	}
	return at + 1, a[at : at+added]
}

// The one op each golden case runs on the fixture: a set, or a reset when reset is true.
type scalarOpCase struct {
	name   string
	path   string
	value  string
	reset  bool
	golden string
}

// The six shapes item 4 names, each against the same hand-edited file: replace an active top-level
// line, insert below a commented example, replace an active nested line (keeping its trailing
// note), insert into a block that is already open, delete a line, and delete a line whose block
// then has no children left. The last two cover the fallbacks: a key the file documents nowhere
// appends at the end, and a nested key whose block is absent and undocumented appends the block.
func TestSpliceScalarSettingGoldenOps(t *testing.T) {
	t.Parallel()
	for _, tt := range []scalarOpCase{
		{
			name: "an active line is rewritten in place",
			path: "endpoint", value: "http://other:2222",
			golden: "settings-edited.set-endpoint.golden",
		},
		{
			name: "a key with a commented example lands directly below it",
			path: "context-window", value: "32768",
			golden: "settings-edited.set-context-window.golden",
		},
		{
			name: "an active nested line keeps its indentation and its trailing note",
			path: "ui.spinner", value: "glitter",
			golden: "settings-edited.set-ui-spinner.golden",
		},
		{
			name: "a nested key joins the block that is already open",
			path: "present.port", value: "8080",
			golden: "settings-edited.set-present-port.golden",
		},
		{
			name: "a key the file documents nowhere is appended at the end",
			path: "cursor-shape", value: "underline",
			golden: "settings-edited.set-cursor-shape.golden",
		},
		{
			name: "a nested key whose block is absent and undocumented appends the block",
			path: "context-files.enable", value: "false",
			golden: "settings-edited.set-context-files-enable.golden",
		},
		{
			name: "a reset removes the key's line",
			path: "auto-title", reset: true,
			golden: "settings-edited.reset-auto-title.golden",
		},
		{
			name: "a reset of a block's last child removes the block line too",
			path: "ui.spinner", reset: true,
			golden: "settings-edited.reset-ui-spinner.golden",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := readFixture(t, editedConfigFixture)
			k := mustKey(tt.path)
			var updated []byte
			var err error
			if tt.reset {
				updated, err = deleteScalarSetting([]byte(input), k)
			} else {
				updated, err = setScalarSetting([]byte(input), k, tt.value)
			}
			if err != nil {
				t.Fatalf("splice %s: %v", tt.path, err)
			}
			if updated == nil {
				t.Fatalf("splice %s reported nothing to write", tt.path)
			}
			compareGolden(t, tt.golden, string(updated))
		})
	}
}

// The seeded template is the file nearly every user actually has, and ADR 0035's placement call is
// about it: a key set for the first time lands under the paragraph that documents it. For a nested
// key that means under its block's whole commented example, since the block line has to be created
// with it.
func TestSpliceScalarSettingInsertsBelowTheTemplateExample(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		path  string
		value string
		want  []string
	}{
		{path: "endpoint", value: "http://box:1111", want: []string{"endpoint: http://box:1111"}},
		{path: "mode", value: "auto", want: []string{"mode: auto"}},
		{path: "api-key", value: "sk-token", want: []string{"api-key: sk-token"}},
		{path: "context-window", value: "32768", want: []string{"context-window: 32768"}},
		{path: "ui.spinner", value: "glitter", want: []string{"ui:", "  spinner: glitter"}},
		{path: "present.port", value: "8080", want: []string{"present:", "  port: 8080"}},
		{path: "context-files.enable", value: "false", want: []string{"context-files:", "  enable: false"}},
	} {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			template := string(defaultConfigYAML)
			updated, err := setScalarSetting(defaultConfigYAML, mustKey(tt.path), tt.value)
			if err != nil {
				t.Fatalf("splice %s into the template: %v", tt.path, err)
			}
			at, added := splicedInsertion(t, template, string(updated))
			if !slices.Equal(added, tt.want) {
				t.Errorf("inserted %q, want %q", added, tt.want)
			}
			lines := splitConfigLines([]byte(template))
			if above := lines[at-2]; !isCommentLine(above) {
				t.Fatalf("the setting landed under %q, which is not part of its commented example", above)
			}
			// A top-level key lands directly under its own commented example. A nested one lands
			// under the last line of its block's example, so the commented `# block:` line has to be
			// somewhere in the run of comments the setting now follows.
			key, _, nested := strings.Cut(tt.path, ".")
			if !nested {
				if indent, name, ok := commentedKey(lines[at-2]); !ok || indent != 0 || name != key {
					t.Errorf("the line above the setting is %q, want the commented %s:", lines[at-2], key)
				}
				return
			}
			run := at - 1
			for run > 1 && isCommentLine(lines[run-2]) {
				run--
			}
			documented := false
			for i := run; i < at; i++ {
				indent, name, ok := commentedKey(lines[i-1])
				documented = documented || (ok && indent == 0 && name == key)
			}
			if !documented {
				t.Errorf("the comment run above the setting (lines %d-%d) does not contain the commented %s:",
					run, at-1, key)
			}
		})
	}
}

// The registry sweep: every key the registry says a surface may edit must survive a set and a reset
// on both the seeded template and a hand-edited file — the result parsing, resolving the key to the
// value asked for, and differing from the input at that key ALONE. It is also what pins the
// template: a key whose commented example went missing or got duplicated would land at the end of
// the file or refuse, and either shows up here.
func TestSpliceScalarSettingRoundTripsEveryEditableKey(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"the seeded template", editedConfigFixture} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := string(defaultConfigYAML)
			if name != "the seeded template" {
				input = readFixture(t, name)
			}
			for _, k := range keyRegistry {
				if !k.Editable {
					continue
				}
				t.Run(k.Path, func(t *testing.T) {
					value := plausibleValue(t, k)
					set, err := setScalarSetting([]byte(input), k, value)
					if err != nil {
						t.Fatalf("set %s = %q: %v", k.Path, value, err)
					}
					result := []byte(input)
					if set != nil {
						result = set
					}
					if got, ok, err := scalarAtPath(result, k.Path); err != nil || !ok || got != value {
						t.Fatalf("after the set, %s reads %q (set=%v, err=%v), want %q", k.Path, got, ok, err, value)
					}
					assertOnlyKeyChanged(t, input, string(result), k.Path)

					reset, err := deleteScalarSetting(result, k)
					if err != nil {
						t.Fatalf("reset %s: %v", k.Path, err)
					}
					if reset == nil {
						t.Fatalf("reset %s reported nothing to remove", k.Path)
					}
					if got, ok, _ := scalarAtPath(reset, k.Path); ok {
						t.Fatalf("after the reset, %s still reads %q", k.Path, got)
					}
					assertOnlyKeyChanged(t, string(result), string(reset), k.Path)
				})
			}
		})
	}
}

// plausibleValue is a valid value for a key, different enough from the defaults to be visible in
// the file: the first of an enum's vocabulary, and a fixed literal for everything else.
func plausibleValue(t *testing.T, k configKey) string {
	t.Helper()
	switch k.Kind {
	case kindBool:
		return "false"
	case kindInt:
		return "4096"
	case kindEnum:
		if len(k.EnumValues) == 0 {
			t.Fatalf("%s is an enum with no vocabulary", k.Path)
		}
		return k.EnumValues[0]
	case kindString:
		return "apogee-test-value"
	}
	t.Fatalf("%s has no writable kind (%s)", k.Path, k.Kind)
	return ""
}

// assertOnlyKeyChanged is the guarantee the whole writer exists for, checked from the outside: two
// configs that parse the same apart from one key.
func assertOnlyKeyChanged(t *testing.T, before, after, path string) {
	t.Helper()
	var b, a fileConfig
	if err := yaml.Unmarshal([]byte(before), &b); err != nil {
		t.Fatalf("the input does not parse: %v", err)
	}
	if err := yaml.Unmarshal([]byte(after), &a); err != nil {
		t.Fatalf("the edited file does not parse: %v", err)
	}
	if !sameApartFrom(b, a, path) {
		t.Errorf("the edit changed more than %s:\n%s", path, after)
	}
}

// A file shape the line arithmetic would have to guess at is refused, loudly, with nothing written:
// the writer's whole posture is that a config it cannot read confidently is one the user edits by
// hand. Each case runs through saveConfigSetting, so the file on disk is checked too.
func TestConfigWriteSettingRefusals(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		content string
		path    string
		value   string
		reset   bool
		wantMsg string
	}{
		{
			name:    "a flow-style block has no line to edit",
			content: "ui: {spinner: snake}\n",
			path:    "ui.spinner", value: "glitter",
			wantMsg: "flow style",
		},
		{
			name:    "a flow-style top level has no line to edit either",
			content: "{endpoint: http://box:1111}\n",
			path:    "endpoint", value: "http://other:2222",
			wantMsg: "flow style",
		},
		{
			name:    "a second document apogee would never read",
			content: "endpoint: http://box:1111\n---\nendpoint: http://other:2222\n",
			path:    "endpoint", value: "http://third:3333",
			wantMsg: "more than one YAML document",
		},
		{
			name:    "a top level that is not a mapping of settings",
			content: "- endpoint\n- mode\n",
			path:    "mode", value: "auto",
			wantMsg: "not a mapping of settings",
		},
		{
			name:    "a block key holding something other than a block",
			content: "ui: solid\n",
			path:    "ui.spinner", value: "glitter",
			wantMsg: "not a block of settings",
		},
		{
			name:    "one key commented out in two places names no one place",
			content: "# endpoint: http://one:1111\n# endpoint: http://two:2222\nmode: auto\n",
			path:    "endpoint", value: "http://box:1111",
			wantMsg: "in two places",
		},
		{
			name:    "a value written as a multi-line block cannot be rewritten as one line",
			content: "endpoint: |\n  http://box:1111\n",
			path:    "endpoint", value: "http://other:2222",
			wantMsg: "multi-line block",
		},
		{
			name:    "a value that does not sit on its key's line",
			content: "endpoint:\n  http://box:1111\n",
			path:    "endpoint", value: "http://other:2222",
			wantMsg: "same line",
		},
		{
			name:    "a scalar key holding a list",
			content: "endpoint:\n  - http://box:1111\n",
			path:    "endpoint", value: "http://other:2222",
			wantMsg: "not a single value",
		},
		{
			name:    "a reset refuses the same shapes a set does",
			content: "ui: {spinner: snake}\n",
			path:    "ui.spinner", reset: true,
			wantMsg: "flow style",
		},
		{
			name:    "a bool takes true or false",
			content: "mode: auto\n",
			path:    "auto-compact", value: "maybe",
			wantMsg: "true or false",
		},
		{
			name:    "an int takes a whole number",
			content: "mode: auto\n",
			path:    "context-window", value: "lots",
			wantMsg: "whole number",
		},
		{
			name:    "an enum takes one of its values",
			content: "mode: auto\n",
			path:    "cursor-shape", value: "sideways",
			wantMsg: "one of block, underline, bar",
		},
		{
			name:    "a structured block is not written from the settings surface",
			content: "mode: auto\n",
			path:    "servers", value: "nope",
			wantMsg: "not a simple value",
		},
		{
			name:    "the confinement acknowledgement stays with /confine",
			content: "mode: auto\n",
			path:    "confine-to-workspace", value: "false",
			wantMsg: "/confine",
		},
		{
			name:    "a key that is not in the schema at all",
			content: "mode: auto\n",
			path:    "endpoints", value: "http://box:1111",
			wantMsg: "not a setting apogee knows",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTestConfig(t, tt.content)
			var err error
			if tt.reset {
				err = resetConfigSetting(path, tt.path)
			} else {
				err = saveConfigSetting(path, tt.path, tt.value)
			}
			if err == nil {
				t.Fatalf("want an error, got a written file:\n%s", readTestConfig(t, path))
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %v, want it to mention %q", err, tt.wantMsg)
			}
			if got := readTestConfig(t, path); got != tt.content {
				t.Errorf("a refused write changed the file:\n%s", got)
			}
		})
	}
}

// The end-to-end write: an absent config is seeded from the template first (so an edit never leaves
// a bare fragment where a documented file belongs), the template's own text survives the write, and
// the file's mode is carried over — a config may hold an api-key, so a rewrite must not widen it.
func TestConfigWriteSettingSeedsAnAbsentConfig(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := saveConfigSetting(path, "ui.spinner", "glitter"); err != nil {
		t.Fatalf("save into an absent config: %v", err)
	}
	written := readTestConfig(t, path)
	if !strings.Contains(written, "\nui:\n  spinner: glitter\n") {
		t.Errorf("the seeded config does not carry the setting:\n%s", written)
	}
	if !strings.Contains(written, "# apogee configuration") {
		t.Error("the seeded template's documentation did not survive the write")
	}
	if _, added := splicedInsertion(t, string(defaultConfigYAML), written); !slices.Equal(added, []string{"ui:", "  spinner: glitter"}) {
		t.Errorf("the write added %q to the template", added)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the written config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the seeded config's mode is %v, want 0600", perm)
	}
}

// Writing what the file already says, and resetting a key it does not set, are both confirmations
// rather than edits: nothing is written, so a settings screen that re-commits an unchanged row
// cannot churn the user's file.
func TestConfigWriteSettingWritesNothingWhenTheFileAlreadyAgrees(t *testing.T) {
	t.Parallel()
	const content = "mode: auto\nauto-compact: true\n"
	for _, tt := range []struct {
		name  string
		path  string
		value string
		reset bool
	}{
		{name: "a set to the value already there", path: "mode", value: "auto"},
		{name: "a set of a bool already there", path: "auto-compact", value: "true"},
		{name: "a reset of a key the file does not set", path: "endpoint", reset: true},
		{name: "a reset of a nested key whose block is absent", path: "ui.spinner", reset: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTestConfig(t, content)
			var err error
			if tt.reset {
				err = resetConfigSetting(path, tt.path)
			} else {
				err = saveConfigSetting(path, tt.path, tt.value)
			}
			if err != nil {
				t.Fatalf("write %s: %v", tt.path, err)
			}
			if got := readTestConfig(t, path); got != content {
				t.Errorf("the file was rewritten:\n%s", got)
			}
		})
	}
}

// The nested shapes a file can be in when a block key is present but empty, or present with a
// sibling: the child joins the block it belongs to rather than the documentation above it, and a
// reset that empties a block takes the block line only when nothing else is left in it.
func TestSpliceScalarSettingNestedBlockShapes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		content string
		path    string
		value   string
		reset   bool
		want    string
	}{
		{
			name:    "a block key with nothing under it gets its first child",
			content: "ui:\nmode: auto\n",
			path:    "ui.spinner", value: "glitter",
			want: "ui:\n  spinner: glitter\nmode: auto\n",
		},
		{
			name:    "a child joins its block at the indentation its siblings use",
			content: "ui:\n    spinner: snake\nmode: auto\n",
			path:    "ui.show-scrollbar", value: "false",
			want: "ui:\n    spinner: snake\n    show-scrollbar: false\nmode: auto\n",
		},
		{
			name:    "a reset leaves a block that still has a child",
			content: "ui:\n  spinner: snake\n  show-scrollbar: false\n",
			path:    "ui.spinner", reset: true,
			want: "ui:\n  show-scrollbar: false\n",
		},
		{
			name:    "a reset of the last child takes the block line with it",
			content: "mode: auto\nui:\n  spinner: snake\n",
			path:    "ui.spinner", reset: true,
			want: "mode: auto\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			k := mustKey(tt.path)
			var updated []byte
			var err error
			if tt.reset {
				updated, err = deleteScalarSetting([]byte(tt.content), k)
			} else {
				updated, err = setScalarSetting([]byte(tt.content), k, tt.value)
			}
			if err != nil {
				t.Fatalf("splice %s: %v", tt.path, err)
			}
			if string(updated) != tt.want {
				t.Errorf("spliced:\n%q\nwant:\n%q", updated, tt.want)
			}
		})
	}
}

// What a rewritten line keeps of itself: the user's alignment, and their end-of-line note — which
// on a bare `key:` the parser hangs off the key rather than the absent value. And what is NOT an
// insertion anchor: a commented example that is itself indented, which sits inside somebody else's
// block, where an active top-level setting must never land.
func TestSpliceScalarSettingKeepsTheLinesOwnNotes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		content string
		path    string
		value   string
		want    string
	}{
		{
			name:    "a trailing note survives a rewrite",
			content: "endpoint: http://box:1111   # my box\n",
			path:    "endpoint", value: "http://other:2222",
			want: "endpoint: http://other:2222   # my box\n",
		},
		{
			name:    "so does the note on a key with no value yet",
			content: "endpoint:  # not set yet\nmode: auto\n",
			path:    "endpoint", value: "http://box:1111",
			want: "endpoint: http://box:1111  # not set yet\nmode: auto\n",
		},
		{
			name:    "hand-made alignment survives a rewrite",
			content: "endpoint:    http://box:1111\n",
			path:    "endpoint", value: "http://other:2222",
			want: "endpoint:    http://other:2222\n",
		},
		{
			name:    "an indented commented example is not an anchor",
			content: "servers:\n  - name: box\n    # endpoint: http://box:1111\nmode: auto\n",
			path:    "endpoint", value: "http://other:2222",
			want: "servers:\n  - name: box\n    # endpoint: http://box:1111\nmode: auto\n\nendpoint: http://other:2222\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			updated, err := setScalarSetting([]byte(tt.content), mustKey(tt.path), tt.value)
			if err != nil {
				t.Fatalf("splice %s: %v", tt.path, err)
			}
			if string(updated) != tt.want {
				t.Errorf("spliced:\n%q\nwant:\n%q", updated, tt.want)
			}
		})
	}
}

// Values whose YAML spelling has to be decided rather than copied: a bare `off` would parse as a
// bool, an empty string as null, and a bare `123` as a number, so each is quoted — and the check
// that they are is that the value reads back as the string it went in as.
func TestSpliceScalarSettingQuotesValuesThatNeedIt(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		path  string
		value string
		want  string
	}{
		{path: "web-search-endpoint", value: "off", want: `web-search-endpoint: "off"`},
		{path: "present.host", value: "", want: `  host: ""`},
		{path: "host-alias", value: "123", want: `host-alias: "123"`},
		{path: "present.command", value: "zed {path}", want: "  command: zed {path}"},
		{path: "endpoint", value: "  http://box:1111  ", want: "endpoint: http://box:1111"},
		{path: "context-window", value: "32768", want: "context-window: 32768"},
		{path: "auto-compact", value: "false", want: "auto-compact: false"},
	} {
		t.Run(tt.path+"="+tt.value, func(t *testing.T) {
			t.Parallel()
			updated, err := setScalarSetting([]byte("mode: auto\n"), mustKey(tt.path), tt.value)
			if err != nil {
				t.Fatalf("splice %s = %q: %v", tt.path, tt.value, err)
			}
			if !slices.Contains(splitConfigLines(updated), tt.want) {
				t.Errorf("spliced:\n%s\nwant a line %q", updated, tt.want)
			}
			if got, ok, err := scalarAtPath(updated, tt.path); err != nil || !ok || got != strings.TrimSpace(tt.value) {
				t.Errorf("%s reads back as %q (set=%v, err=%v), want %q", tt.path, got, ok, err, strings.TrimSpace(tt.value))
			}
		})
	}
}

// sameApartFrom is the verification every splice passes, so what it does and does not forgive is
// worth pinning on its own: a block created for one key compares equal to the absent block it
// replaced, while a change to any other setting — including one in the same block — does not.
func TestConfigWriteSameApartFrom(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		before string
		after  string
		path   string
		want   bool
	}{
		{
			name:   "the target key alone",
			before: "mode: auto\nendpoint: http://box:1111\n",
			after:  "mode: plan\nendpoint: http://box:1111\n",
			path:   "mode", want: true,
		},
		{
			name:   "a block created for the target key",
			before: "mode: auto\n",
			after:  "mode: auto\nui:\n  spinner: glitter\n",
			path:   "ui.spinner", want: true,
		},
		{
			name:   "a block emptied by resetting the target key",
			before: "mode: auto\nui:\n  spinner: glitter\n",
			after:  "mode: auto\n",
			path:   "ui.spinner", want: true,
		},
		{
			name:   "a neighbour in the same block",
			before: "ui:\n  spinner: snake\n  show-scrollbar: true\n",
			after:  "ui:\n  spinner: glitter\n",
			path:   "ui.spinner", want: false,
		},
		{
			name:   "a neighbour elsewhere in the file",
			before: "mode: auto\nendpoint: http://box:1111\n",
			after:  "mode: plan\nendpoint: http://other:2222\n",
			path:   "mode", want: false,
		},
		{
			name:   "a path the schema does not have cannot be verified",
			before: "mode: auto\n",
			after:  "mode: auto\n",
			path:   "ui.sparkle", want: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var b, a fileConfig
			if err := yaml.Unmarshal([]byte(tt.before), &b); err != nil {
				t.Fatalf("parse before: %v", err)
			}
			if err := yaml.Unmarshal([]byte(tt.after), &a); err != nil {
				t.Fatalf("parse after: %v", err)
			}
			if got := sameApartFrom(b, a, tt.path); got != tt.want {
				t.Errorf("sameApartFrom(..., %q) = %v, want %v", tt.path, got, tt.want)
			}
			// The comparison must not consume what it compares: a second call sees the same answer,
			// which is what proves it copied the blocks it reached into rather than blanking them.
			if got := sameApartFrom(b, a, tt.path); got != tt.want {
				t.Errorf("a second sameApartFrom(..., %q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
