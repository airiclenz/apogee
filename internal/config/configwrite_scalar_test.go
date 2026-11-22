package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/airiclenz/apogee/internal/domain"
)

// The hand-edited config every golden case starts from: comments, a commented example above each
// active setting, an active block with a trailing note on its one child, and a key whose example
// is present but commented out. What the goldens pin is that a write touches ONE line of it.
const editedConfigFixture = "settings-edited.yaml"

// resetScalarSetting is the reset half of setScalarSetting over bytes already in hand: the same
// splice-and-verify pair ResetConfigSetting hands the transaction (configedit.go), run without a
// file. Production has no such entry — a reset always addresses a path — so the two-line
// composition lives here, where the splice cases that read it are.
func resetScalarSetting(t *testing.T, data []byte, k Key) ([]byte, error) {
	t.Helper()
	splice, verify := scalarResetEdit(k)
	return verifiedEdit(data, splice, verify)
}

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
	b, a := SplitConfigLines([]byte(before)), SplitConfigLines([]byte(after))
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
			path: "server", value: "other",
			golden: "settings-edited.set-server.golden",
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
				updated, err = resetScalarSetting(t, []byte(input), k)
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
//
// A key the template ships ACTIVE is not set for the first time at all — the splice rewrites the
// line where it already stands, the shape settings-edited.set-ui-spinner.golden pins — so this
// table asks the template which of the two each path is in rather than hard-coding the answer.
// That is what keeps it honest when the template turns a key on or off.
func TestSpliceScalarSettingInsertsBelowTheTemplateExample(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		path  string
		value string
		want  []string
	}{
		{path: "server", value: "my-box", want: []string{"server: my-box"}},
		{path: "mode", value: "auto", want: []string{"mode: auto"}},
		{path: "editor", value: "code -w", want: []string{"editor: code -w"}},
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
			lines := SplitConfigLines([]byte(template))
			if value, active, err := scalarAtPath(defaultConfigYAML, tt.path); err != nil {
				t.Fatalf("read %s from the template: %v", tt.path, err)
			} else if active {
				if got := len(SplitConfigLines(updated)); got != len(lines) {
					t.Fatalf("the template sets %s (%s), so the write must rewrite that line: %d lines from %d",
						tt.path, value, got, len(lines))
				}
				if got, ok, err := scalarAtPath(updated, tt.path); err != nil || !ok || got != tt.value {
					t.Fatalf("after the rewrite, %s reads %q (set=%v, err=%v), want %q", tt.path, got, ok, err, tt.value)
				}
				return
			}
			at, added := splicedInsertion(t, template, string(updated))
			if !slices.Equal(added, tt.want) {
				t.Errorf("inserted %q, want %q", added, tt.want)
			}
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
// the file or refuse, and either shows up here. Every depth the registry holds is swept, the
// three-segment `ui.side-panel.show` and `ui.side-panel.width` among them.
func TestSpliceScalarSettingRoundTripsEveryEditableKey(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"the seeded template", editedConfigFixture} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := string(defaultConfigYAML)
			if name != "the seeded template" {
				input = readFixture(t, name)
			}
			for _, k := range KeyRegistry {
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

					reset, err := resetScalarSetting(t, result, k)
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

// A scalar written ABOVE a `reactions:` block, which is the shape that makes the verify step's
// job hard: the entries carry position-free values (`run:` is either a sequence or a mapping), so
// the write's own check that nothing but the one key moved — sameApartFrom, a reflect.DeepEqual
// over two parses — has to compare those values rather than the nodes they were read from. A
// yaml.Node in the schema would carry the line each entry was read at, and every insert above the
// block would look like an edit to it.
func TestSpliceScalarSettingWritesAboveAReactionsBlock(t *testing.T) {
	t.Parallel()

	const content = `mode: auto
reactions:
  - id: notify
    on: [approval-requested]
    run: ["notify-send", "waiting"]
  - id: bell
    on: [file-changed]
    run:
      url: https://hooks.example.com/apogee
      headers-env:
        Authorization: MY_WEBHOOK_TOKEN
`
	k := mustKey("editor")
	updated, err := setScalarSetting([]byte(content), k, "hx")
	if err != nil {
		t.Fatalf("splice %s: %v", k.Path, err)
	}
	if updated == nil {
		t.Fatalf("splice %s reported nothing to write", k.Path)
	}
	assertOnlyKeyChanged(t, content, string(updated), k.Path)

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatalf("write the spliced config: %v", err)
	}
	opts, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("load the spliced config: %v", err)
	}
	if len(opts.Reactions) != 2 {
		t.Fatalf("the write left %d reactions, want the 2 the block states: %+v",
			len(opts.Reactions), opts.Reactions)
	}
	if opts.Editor != "hx" {
		t.Errorf("editor reads %q after the write, want %q", opts.Editor, "hx")
	}
}

// The block-scalar writer, against the file it will actually meet: the seeded template, whose
// `system-prompt-text:` block is the only multi-line value the schema has. Since ADR 0064 the
// template states the key COMMENTED — a fresh install runs on the embedded default and this is the
// example showing where a prompt of your own would go — which leaves the writer the same job it
// always had, on the hardest file it has: the commented block sits in the middle of thirty lines of
// documentation, with more documentation directly under it, so what is checked is that every one of
// those lines survives a set, a re-set and a reset, and that what a reader takes back out of the file
// is what was written.
func TestSpliceTextBlockRewritesTheTemplatesPrompt(t *testing.T) {
	t.Parallel()
	const key = "system-prompt-text"
	k := mustKey(key)
	template := string(defaultConfigYAML)

	for _, tt := range []struct {
		name       string
		value      string
		wantHeader string
		wantLines  []string
	}{
		{
			name:       "an ordinary multi-line prompt",
			value:      "You are apogee.\nWork in {{workspace}}.\n",
			wantHeader: key + ": |",
			wantLines:  []string{"  You are apogee.", "  Work in {{workspace}}."},
		},
		{
			name:       "blank lines and trailing spaces survive",
			value:      "first line   \n\n\tindented by a tab\nlast   \n",
			wantHeader: key + ": |",
			wantLines:  []string{"  first line   ", "", "  \tindented by a tab", "  last   "},
		},
		{
			// A first line that opens with whitespace would be read as the block's own indentation and
			// vanish, so the header states the indentation explicitly instead.
			name:       "a prompt whose first line is indented keeps its indentation",
			value:      "  indented first line\nflush second line\n",
			wantHeader: key + ": |2",
			wantLines:  []string{"    indented first line", "  flush second line"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			updated, err := setScalarSetting([]byte(template), k, tt.value)
			if err != nil {
				t.Fatalf("set %s: %v", key, err)
			}
			if updated == nil {
				t.Fatalf("set %s reported nothing to write", key)
			}
			got := string(updated)
			assertCommentsSurvive(t, template, got)
			assertOnlyKeyChanged(t, template, got, key)
			if read, ok, err := scalarAtPath(updated, key); err != nil || !ok || read != tt.value {
				t.Errorf("the file reads back %q (set=%v, err=%v), want %q", read, ok, err, tt.value)
			}
			lines := SplitConfigLines(updated)
			at := slices.Index(lines, tt.wantHeader)
			if at < 0 {
				t.Fatalf("no %q header line in:\n%s", tt.wantHeader, got)
			}
			if body := lines[at+1 : at+1+len(tt.wantLines)]; !slices.Equal(body, tt.wantLines) {
				t.Errorf("the block reads %q, want %q", body, tt.wantLines)
			}

			// A re-set over the block this one wrote is the ordinary case after the first edit, and a
			// reset takes the WHOLE block away rather than the header line it starts with.
			again, err := setScalarSetting(updated, k, "A second prompt.\n")
			if err != nil {
				t.Fatalf("re-set %s: %v", key, err)
			}
			if read, ok, _ := scalarAtPath(again, key); !ok || read != "A second prompt.\n" {
				t.Errorf("after the re-set the file reads %q (set=%v), want the second prompt", read, ok)
			}
			assertCommentsSurvive(t, template, string(again))

			reset, err := resetScalarSetting(t, updated, k)
			if err != nil {
				t.Fatalf("reset %s: %v", key, err)
			}
			if read, ok, _ := scalarAtPath(reset, key); ok {
				t.Fatalf("after the reset, %s still reads %q", key, read)
			}
			assertCommentsSurvive(t, template, string(reset))
			assertOnlyKeyChanged(t, got, string(reset), key)
		})
	}
}

// An empty prompt is not how the key is cleared: a block with nothing in it would be a set of nothing,
// where the reset that removes the block is the deliberate act (validateSystemPromptText).
func TestSpliceTextBlockRefusesAnEmptyPrompt(t *testing.T) {
	t.Parallel()
	if _, err := setScalarSetting(defaultConfigYAML, mustKey("system-prompt-text"), "\n\n"); err == nil {
		t.Fatal("an empty prompt was written")
	}
}

// assertCommentsSurvive checks that every comment line of the input is still in the output, in the same
// order — the promise the whole writer exists for, and the one thing a multi-line splice can break that
// a parsed comparison would never see: the file's documentation is not in fileConfig.
func assertCommentsSurvive(t *testing.T, before, after string) {
	t.Helper()
	comments := func(text string) []string {
		var out []string
		for _, line := range SplitConfigLines([]byte(text)) {
			if isCommentLine(line) {
				out = append(out, line)
			}
		}
		return out
	}
	b, a := comments(before), comments(after)
	if !slices.Equal(b, a) {
		t.Errorf("the splice changed the file's comments: %d lines before, %d after\n%s", len(b), len(a), after)
	}
}

// plausibleValue is a valid value for a key, different enough from the defaults to be visible in
// the file: the first of an enum's vocabulary, and a fixed literal for everything else. A list's
// literal is spelled the way the file spells one, which is also what the sweep reads back — a value
// written and re-read has to come back the same string or the writer's round-trip means nothing.
func plausibleValue(t *testing.T, k Key) string {
	t.Helper()
	switch k.Kind {
	case KindBool:
		return "false"
	case KindInt:
		return "4096"
	case KindFloat:
		// Inside the one share the range accepts, and in the shortest spelling that reads back as
		// itself — the canonical form renderSettingValue writes, so the sweep's read-back compares
		// against the value as offered.
		return "0.35"
	case KindEnum:
		if len(k.EnumValues) == 0 {
			t.Fatalf("%s is an enum with no vocabulary", k.Path)
		}
		return k.EnumValues[0]
	case KindString, KindServer, KindScheme:
		return "apogee-test-value"
	case KindStringList:
		return "[apogee-test.md, docs/apogee-test.md]"
	case KindText:
		// Written the way a reader takes it back out of a clip-chomped block: several lines, a blank
		// one among them, and exactly one trailing newline — the canonical spelling renderSettingValue
		// normalizes to, so the sweep's read-back comparison is against the value as offered.
		return "apogee test prompt, first line\n\nand a third line after a blank one\n"
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

// The list kind end to end, in both states the file it is written into can be in. On the file the
// users actually have — the seeded template, which ships `context-files:` active with its `names:`
// list on ONE line — a write REWRITES that line rather than folding the list into a block sequence,
// and a second write rewrites it again rather than adding another. Created from nothing, the list
// is still one line, under the block line created with it, and the reset takes both back out.
func TestSpliceScalarSettingWritesAListOnOneLine(t *testing.T) {
	t.Parallel()
	k := mustKey("context-files.names")
	template := string(defaultConfigYAML)

	rewritten, err := setScalarSetting(defaultConfigYAML, k, "NOTES.md, docs/HOWTO.md")
	if err != nil {
		t.Fatalf("rewrite the template's list: %v", err)
	}
	if rewritten == nil {
		t.Fatal("the write reported nothing to do")
	}
	if before, after := len(SplitConfigLines(defaultConfigYAML)), len(SplitConfigLines(rewritten)); before != after {
		t.Fatalf("the rewrite changed the line count from %d to %d — a list this writer touches stays on one line",
			before, after)
	}
	if got, ok, err := scalarAtPath(rewritten, k.Path); err != nil || !ok || got != "[NOTES.md, docs/HOWTO.md]" {
		t.Fatalf("the rewritten list reads %q (set=%v, err=%v)", got, ok, err)
	}
	assertOnlyKeyChanged(t, template, string(rewritten), k.Path)

	// A second edit is a REWRITE of that same one line: the flow sequence already there is a value
	// this writer may replace (valueFitsOneLine).
	updated, err := setScalarSetting(rewritten, k, "[AGENTS.md]")
	if err != nil {
		t.Fatalf("rewrite the list again: %v", err)
	}
	if before, after := len(SplitConfigLines(rewritten)), len(SplitConfigLines(updated)); before != after {
		t.Errorf("the second rewrite changed the line count from %d to %d", before, after)
	}
	if got, ok, err := scalarAtPath(updated, k.Path); err != nil || !ok || got != "[AGENTS.md]" {
		t.Fatalf("the rewritten list reads %q (set=%v, err=%v)", got, ok, err)
	}
	assertOnlyKeyChanged(t, string(rewritten), string(updated), k.Path)

	// Written into a file that has no such block, the list is one line too — the block line and the
	// list line, and nothing else — and the reset takes the pair back out whole, the `names:` line
	// and the `context-files:` key it was the only child of.
	const bare = "mode: auto\n"
	created, err := setScalarSetting([]byte(bare), k, "NOTES.md, docs/HOWTO.md")
	if err != nil {
		t.Fatalf("create the block: %v", err)
	}
	at, inserted := splicedInsertion(t, bare, string(created))
	want := []string{"", "context-files:", "  names: [NOTES.md, docs/HOWTO.md]"}
	if !slices.Equal(inserted, want) {
		t.Fatalf("inserted %q at line %d, want %q — one line for the block and one for the list", inserted, at, want)
	}
	reset, err := resetScalarSetting(t, created, k)
	if err != nil {
		t.Fatalf("reset the list: %v", err)
	}
	if got, ok, _ := scalarAtPath(reset, k.Path); ok {
		t.Errorf("after the reset the list still reads %q", got)
	}
	if slices.Contains(SplitConfigLines(reset), "context-files:") {
		t.Errorf("the reset left the block line standing with nothing under it:\n%s", reset)
	}
	assertOnlyKeyChanged(t, string(created), string(reset), k.Path)
}

// A file shape the line arithmetic would have to guess at is refused, loudly, with nothing written:
// the writer's whole posture is that a config it cannot read confidently is one the user edits by
// hand. Each case runs through SaveConfigSetting, so the file on disk is checked too.
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
			content: "{server: box}\n",
			path:    "server", value: "other",
			wantMsg: "flow style",
		},
		{
			name:    "a second document apogee would never read",
			content: "server: box\n---\nserver: other\n",
			path:    "server", value: "third",
			wantMsg: "more than one YAML document",
		},
		{
			name:    "a top level that is not a mapping of settings",
			content: "- server\n- mode\n",
			path:    "mode", value: "auto",
			wantMsg: "not a mapping of settings",
		},
		{
			name:    "a flow-style ancestor two levels up has no line to edit",
			content: "ui: {side-panel: {show: auto}}\n",
			path:    "ui.side-panel.show", value: "off",
			wantMsg: "its ui: block is written in flow style",
		},
		{
			name:    "a flow-style ancestor one level up has no line to edit",
			content: "ui:\n  side-panel: {show: auto}\n",
			path:    "ui.side-panel.show", value: "off",
			wantMsg: "its side-panel: block is written in flow style",
		},
		{
			name:    "a reset refuses a flow-style ancestor at depth three",
			content: "ui:\n  side-panel: {width: 48}\n",
			path:    "ui.side-panel.width", reset: true,
			wantMsg: "its side-panel: block is written in flow style",
		},
		{
			name:    "a nested block key holding something other than a block",
			content: "ui:\n  side-panel: wide\n",
			path:    "ui.side-panel.show", value: "off",
			wantMsg: "its side-panel: is not a block of settings",
		},
		{
			name:    "a block key holding something other than a block",
			content: "ui: solid\n",
			path:    "ui.spinner", value: "glitter",
			wantMsg: "not a block of settings",
		},
		{
			name:    "one key commented out in two places names no one place",
			content: "# server: one\n# server: two\nmode: auto\n",
			path:    "server", value: "box",
			wantMsg: "in two places",
		},
		{
			name:    "a value written as a multi-line block cannot be rewritten as one line",
			content: "server: |\n  box\n",
			path:    "server", value: "other",
			wantMsg: "multi-line block",
		},
		{
			name:    "a value that does not sit on its key's line",
			content: "server:\n  box\n",
			path:    "server", value: "other",
			wantMsg: "same line",
		},
		{
			name:    "a scalar key holding a list",
			content: "server:\n  - box\n",
			path:    "server", value: "other",
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
		// The footer's structured keys are edited in the file: neither Editable nor gesture-written
		// (Key.Gesture), so the writer admits neither.
		{
			name:    "the footer's rows are edited in the file",
			content: "mode: auto\n",
			path:    "ui.footer.rows", value: "[[host, model]]",
			wantMsg: "ui.footer.rows is not a simple value; edit it in config.yaml",
		},
		{
			name:    "the footer's colours are edited in the file",
			content: "mode: auto\n",
			path:    "ui.footer.colors", value: "{model: success}",
			wantMsg: "ui.footer.colors is not a simple value; edit it in config.yaml",
		},
		// A gesture writes the side panel's two lists, and no surface resets either: the refusal is
		// the one the writer made before a gesture could write them.
		{
			name:    "the side panel's sections are not reset",
			content: "ui:\n  side-panel:\n    sections: [mcp: pinned]\n",
			path:    "ui.side-panel.sections", reset: true,
			wantMsg: "apogee: ui.side-panel.sections is not a simple value; edit it in config.yaml",
		},
		{
			name:    "the side panel's folded sections are not reset",
			content: "ui:\n  side-panel:\n    folded: [usage]\n",
			path:    "ui.side-panel.folded", reset: true,
			wantMsg: "apogee: ui.side-panel.folded is not a simple value; edit it in config.yaml",
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
			path:    "servers-list", value: "box",
			wantMsg: "not a setting apogee knows",
		},
		// The rows' validate hooks, on the write path: a value the kind accepts but the KEY does not
		// (registry.go). Every one of them is refused before the file is opened, so the assertion
		// below — the file is byte-for-byte what it was — is what "validate before writing" means.
		{
			name:    "a search endpoint no URL parse accepts",
			content: "mode: auto\n",
			path:    "web-search-endpoint", value: "%zz",
			wantMsg: "not a URL",
		},
		{
			name:    "a negative context window is a window nothing fits in",
			content: "mode: auto\n",
			path:    "context-window", value: "-1",
			wantMsg: "0 or more",
		},
		{
			name:    "a port outside the range the document server could bind",
			content: "mode: auto\n",
			path:    "present.port", value: "99999",
			wantMsg: "0-65535",
		},
		{
			name:    "a context-file name that climbs out of the workspace",
			content: "mode: auto\n",
			path:    "context-files.names", value: "[../secrets.md]",
			wantMsg: "climbs out of the workspace",
		},
		{
			name:    "a prompt file cleared by writing nothing, which is what the reset is for",
			content: "mode: auto\n",
			path:    "system-prompt-file", value: "",
			wantMsg: "name a file to read the prompt from",
		},
		// The list kind's own two shapes: what a `names:` line may hold, and what it may not. Both are
		// the user's layout rather than a defect, so both are refused rather than rewritten.
		{
			name:    "a list key holding a single value",
			content: "context-files:\n  names: AGENTS.md\n",
			path:    "context-files.names", value: "[NOTES.md]",
			wantMsg: "not a list",
		},
		{
			name:    "a list written one item per line is a layout this writer will not fold",
			content: "context-files:\n  names:\n    - AGENTS.md\n    - CLAUDE.md\n",
			path:    "context-files.names", value: "[NOTES.md]",
			wantMsg: "one item per line",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTestConfig(t, tt.content)
			var err error
			if tt.reset {
				err = ResetConfigSetting(path, tt.path)
			} else {
				_, err = SaveConfigSetting(path, tt.path, tt.value)
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

// The end-to-end write into a config that was not there: the seeded template's own text survives
// it, and what the write left behind is the one key it was given and nothing else. That the seeding
// and the mode-preserving atomic write happen at all is the transaction's property
// (configedit_test.go); what is asked here is that THIS writer's edit reaches a seeded file whole.
// Which shape the splice took — an insertion below the commented example, or a rewrite of a line
// the template ships active — is the splice's own business, pinned by the placement tests above;
// what a seeded file must show either way is every line of the template's documentation intact.
func TestConfigWriteSettingSeedsAnAbsentConfig(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := SaveConfigSetting(path, "ui.spinner", "glitter"); err != nil {
		t.Fatalf("save into an absent config: %v", err)
	}
	written := readTestConfig(t, path)
	if got, ok, err := scalarAtPath([]byte(written), "ui.spinner"); err != nil || !ok || got != "glitter" {
		t.Errorf("the seeded config reads ui.spinner = %q (set=%v, err=%v), want \"glitter\":\n%s", got, ok, err, written)
	}
	if !strings.Contains(written, "# apogee configuration") {
		t.Error("the seeded template's documentation did not survive the write")
	}
	if got, want := commentLines(written), commentLines(string(defaultConfigYAML)); !slices.Equal(got, want) {
		t.Errorf("the write changed the template's documentation: %d comment lines, want %d", len(got), len(want))
	}
	assertOnlyKeyChanged(t, string(defaultConfigYAML), written, "ui.spinner")
}

// commentLines is the documentation of a config file: every whole-line comment in it, in order. A
// write splices one setting's value, so whatever it does to that setting's own line, this comes
// back equal.
func commentLines(data string) []string {
	var out []string
	for _, line := range SplitConfigLines([]byte(data)) {
		if isCommentLine(line) {
			out = append(out, line)
		}
	}
	return out
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
		{name: "a reset of a key the file does not set", path: "server", reset: true},
		{name: "a reset of a nested key whose block is absent", path: "ui.spinner", reset: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTestConfig(t, content)
			var err error
			if tt.reset {
				err = ResetConfigSetting(path, tt.path)
			} else {
				_, err = SaveConfigSetting(path, tt.path, tt.value)
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
				updated, err = resetScalarSetting(t, []byte(tt.content), k)
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

// A three-segment key, into each shape its chain can be in: no `ui:` at all, `ui:` without
// `side-panel:`, a `side-panel:` with nothing under it, and a populated one. The missing blocks are
// created around the key at the column the present block's children use, each created level one
// indentation step under the last, and every comment and neighbour
// stays byte-for-byte where it was. A reset that empties the chain takes each emptied block with it,
// innermost first, and stops at the first block that still holds something.
func TestSpliceScalarSettingThreeDeepShapes(t *testing.T) {
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
			name:    "no ui block lands the whole nest below its commented example",
			content: "mode: auto # the mode\n\n# ui:\n#   side-panel:\n#     show: auto\n\n# a closing note\nserver: box\n",
			path:    "ui.side-panel.show", value: "auto",
			want: "mode: auto # the mode\n\n# ui:\n#   side-panel:\n#     show: auto\n" +
				"ui:\n  side-panel:\n    show: auto\n\n# a closing note\nserver: box\n",
		},
		{
			name:    "no ui block and no example appends the whole nest at the end",
			content: "mode: auto # the mode\n",
			path:    "ui.side-panel.width", value: "48",
			want: "mode: auto # the mode\n\nui:\n  side-panel:\n    width: 48\n",
		},
		{
			name:    "a ui block without side-panel gets the block and the key after its children",
			content: "ui:\n    # the spinner\n    spinner: snake # mine\nmode: auto\n",
			path:    "ui.side-panel.show", value: "auto",
			want: "ui:\n    # the spinner\n    spinner: snake # mine\n    side-panel:\n      show: auto\nmode: auto\n",
		},
		{
			name:    "a ui block with nothing under it gets the nest as its first child",
			content: "ui: # later\nmode: auto\n",
			path:    "ui.side-panel.width", value: "48",
			want: "ui: # later\n  side-panel:\n    width: 48\nmode: auto\n",
		},
		{
			name:    "a side-panel block with nothing under it gets its first child",
			content: "ui:\n  spinner: snake\n  side-panel: # the panel\nmode: auto\n",
			path:    "ui.side-panel.show", value: "auto",
			want: "ui:\n  spinner: snake\n  side-panel: # the panel\n    show: auto\nmode: auto\n",
		},
		{
			name:    "a populated side-panel block gets the key after its children",
			content: "ui:\n  side-panel:\n      # the sections\n      sections: [usage: pinned]\n  spinner: snake\n",
			path:    "ui.side-panel.width", value: "48",
			want: "ui:\n  side-panel:\n      # the sections\n      sections: [usage: pinned]\n      width: 48\n  spinner: snake\n",
		},
		{
			name:    "a set key is rewritten in place, its note kept",
			content: "ui:\n  side-panel:\n    show:   auto   # wide windows\n",
			path:    "ui.side-panel.show", value: "off",
			want: "ui:\n  side-panel:\n    show:   \"off\"   # wide windows\n",
		},
		{
			name:    "a reset of the last key takes side-panel and then ui with it",
			content: "mode: auto\n# the panel\nui:\n  side-panel:\n    width: 48\nserver: box\n",
			path:    "ui.side-panel.width", reset: true,
			want: "mode: auto\n# the panel\nserver: box\n",
		},
		{
			name:    "a reset of the last key takes side-panel but leaves a ui that holds more",
			content: "ui:\n  side-panel:\n    show: auto\n  spinner: snake\n",
			path:    "ui.side-panel.show", reset: true,
			want: "ui:\n  spinner: snake\n",
		},
		{
			name:    "a reset leaves a side-panel that still has a key",
			content: "ui:\n  side-panel:\n    show: auto\n    width: 48\n",
			path:    "ui.side-panel.show", reset: true,
			want: "ui:\n  side-panel:\n    width: 48\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			k := mustKey(tt.path)
			var updated []byte
			var err error
			if tt.reset {
				updated, err = resetScalarSetting(t, []byte(tt.content), k)
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
			content: "server: box   # my box\n",
			path:    "server", value: "other",
			want: "server: other   # my box\n",
		},
		{
			name:    "so does the note on a key with no value yet",
			content: "server:  # not set yet\nmode: auto\n",
			path:    "server", value: "box",
			want: "server: box  # not set yet\nmode: auto\n",
		},
		{
			name:    "hand-made alignment survives a rewrite",
			content: "server:    box\n",
			path:    "server", value: "other",
			want: "server:    other\n",
		},
		{
			name:    "an indented commented example is not an anchor",
			content: "servers:\n  - name: box\n    # model: qwen\nmode: auto\n",
			path:    "server", value: "other",
			want: "servers:\n  - name: box\n    # model: qwen\nmode: auto\n\nserver: other\n",
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
		{path: "server", value: "123", want: `server: "123"`},
		{path: "present.command", value: "zed {path}", want: "  command: zed {path}"},
		{path: "context-window", value: "32768", want: "context-window: 32768"},
		{path: "auto-compact", value: "false", want: "auto-compact: false"},
	} {
		t.Run(tt.path+"="+tt.value, func(t *testing.T) {
			t.Parallel()
			updated, err := setScalarSetting([]byte("mode: auto\n"), mustKey(tt.path), tt.value)
			if err != nil {
				t.Fatalf("splice %s = %q: %v", tt.path, tt.value, err)
			}
			if !slices.Contains(SplitConfigLines(updated), tt.want) {
				t.Errorf("spliced:\n%s\nwant a line %q", updated, tt.want)
			}
			if got, ok, err := scalarAtPath(updated, tt.path); err != nil || !ok || got != strings.TrimSpace(tt.value) {
				t.Errorf("%s reads back as %q (set=%v, err=%v), want %q", tt.path, got, ok, err, strings.TrimSpace(tt.value))
			}
		})
	}
}

// The `sub-agents-server:` key has a writer of its own because its registry row is deliberately not
// editable — the settings pane shows it read-only so ⏎ on a KindServer row cannot move the SESSION
// (registry.go) — and the settings-surface writer refuses a non-editable key by design. What the
// verb that records it needs is every other property of that writer: the key lands under its own
// commented example in a seeded file, the template's documentation survives, and nothing else moves.
func TestConfigWriteSubAgentsServerWritesTheNonEditableKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")

	if _, err := SaveConfigSetting(path, "sub-agents-server", "my-box"); err == nil {
		t.Error("SaveConfigSetting wrote a non-editable key; the settings surface must refuse it")
	}
	if err := SaveSubAgentsServer(path, "my-box"); err != nil {
		t.Fatalf("SaveSubAgentsServer: %v", err)
	}

	written := readTestConfig(t, path)
	if got, ok, err := scalarAtPath([]byte(written), "sub-agents-server"); err != nil || !ok || got != "my-box" {
		t.Errorf("the config reads sub-agents-server = %q (set=%v, err=%v), want \"my-box\":\n%s", got, ok, err, written)
	}
	if got, want := commentLines(written), commentLines(string(defaultConfigYAML)); !slices.Equal(got, want) {
		t.Errorf("the write changed the template's documentation: %d comment lines, want %d", len(got), len(want))
	}
	assertOnlyKeyChanged(t, string(defaultConfigYAML), written, "sub-agents-server")

	// A re-pick moves the same line rather than adding a second one.
	if err := SaveSubAgentsServer(path, "other-box"); err != nil {
		t.Fatalf("SaveSubAgentsServer over an existing line: %v", err)
	}
	again := readTestConfig(t, path)
	if got, ok, _ := scalarAtPath([]byte(again), "sub-agents-server"); !ok || got != "other-box" {
		t.Errorf("the re-pick reads sub-agents-server = %q, want \"other-box\":\n%s", got, again)
	}
	if n := strings.Count(again, "\nsub-agents-server:"); n != 1 {
		t.Errorf("the file carries %d active sub-agents-server lines, want exactly 1:\n%s", n, again)
	}
}

// Neither splice may disturb a `servers:` entry's `description:` — the free text ADR 0069 relays to
// the model so it can pick a delegation's seat. Both writers address a TOP-LEVEL key, so the entries
// are untouched by construction; this pins that, because a description silently dropped by the write
// that records the delegation target would be a sentence the model stops being told.
func TestConfigWriteSubAgentsServerSplicesLeaveEntryDescriptionsAlone(t *testing.T) {
	t.Parallel()

	const fixture = `# apogee config

sub-agents-server: other-box

servers:
  - name: my-box
    endpoint: http://localhost:1111/v1
    description: fast local 27B — search and edits
  - name: other-box
    endpoint: http://otherbox:1111/v1
    description: the big remote model
`
	path := writeTestConfig(t, fixture)

	if err := SaveSubAgentsServer(path, "my-box"); err != nil {
		t.Fatalf("SaveSubAgentsServer: %v", err)
	}
	assertDescriptionsIntact(t, readTestConfig(t, path), "the record")

	if err := ResetSubAgentsServer(path); err != nil {
		t.Fatalf("ResetSubAgentsServer: %v", err)
	}
	assertDescriptionsIntact(t, readTestConfig(t, path), "the clear")
}

// assertDescriptionsIntact fails unless the two entries of the fixture above still carry the
// descriptions they were written with, read back through the decoder rather than off the text: what
// matters is the sentence a reader gets, not the line it happens to sit on.
func assertDescriptionsIntact(t *testing.T, written, what string) {
	t.Helper()
	var fc fileConfig
	if err := yaml.Unmarshal([]byte(written), &fc); err != nil {
		t.Fatalf("%s left a file that does not parse: %v\n%s", what, err, written)
	}
	if len(fc.Servers) != 2 {
		t.Fatalf("%s left %d entries, want 2:\n%s", what, len(fc.Servers), written)
	}
	if got, want := fc.Servers[0].Description, "fast local 27B — search and edits"; got != want {
		t.Errorf("%s changed my-box's description to %q, want %q", what, got, want)
	}
	if got, want := fc.Servers[1].Description, "the big remote model"; got != want {
		t.Errorf("%s changed other-box's description to %q, want %q", what, got, want)
	}
}

// The `auto` pick the `/sub-agents-server` picker offers is an opt-OUT: it CLEARS the key rather
// than recording a name, so the next session picks its delegation target the way one that was never
// told picks one. The clear has a function of its own for the write's reason — the settings-surface
// reset admits Editable keys only, and this row is deliberately not one — and it must be the same
// careful splice: the key's line goes, and the file's comments and its `servers:` entries stay
// exactly where the user left them.
func TestConfigWriteSubAgentsServerClearsTheNonEditableKey(t *testing.T) {
	t.Parallel()

	const fixture = `# apogee config

# Where sub-agents run.
sub-agents-server: my-box

servers:
  # the workstation on the desk
  - name: my-box
    endpoint: http://localhost:1111/v1
  - name: other-box
    endpoint: http://otherbox:1111/v1

ui:
  spinner: dots
`
	path := writeTestConfig(t, fixture)

	if err := ResetConfigSetting(path, subAgentsServerPath); err == nil {
		t.Error("ResetConfigSetting cleared a non-editable key; the settings surface must refuse it")
	}
	if err := ResetSubAgentsServer(path); err != nil {
		t.Fatalf("ResetSubAgentsServer: %v", err)
	}

	cleared := readTestConfig(t, path)
	if got, ok, err := scalarAtPath([]byte(cleared), subAgentsServerPath); err != nil || ok {
		t.Errorf("the config still reads sub-agents-server = %q (set=%v, err=%v):\n%s", got, ok, err, cleared)
	}
	if got, want := commentLines(cleared), commentLines(fixture); !slices.Equal(got, want) {
		t.Errorf("the clear changed the file's comments:\n%v\nwant:\n%v", got, want)
	}
	var fc fileConfig
	if err := yaml.Unmarshal([]byte(cleared), &fc); err != nil {
		t.Fatalf("the cleared file does not parse: %v\n%s", err, cleared)
	}
	if len(fc.Servers) != 2 || fc.Servers[0].Name != "my-box" || fc.Servers[1].Name != "other-box" {
		t.Errorf("the clear disturbed the servers block: %+v\n%s", fc.Servers, cleared)
	}
	assertOnlyKeyChanged(t, fixture, cleared, subAgentsServerPath)

	// A file that never set the key is already at its default: nothing to remove, and no error.
	if err := ResetSubAgentsServer(path); err != nil {
		t.Fatalf("ResetSubAgentsServer on a file that does not set the key: %v", err)
	}
	if again := readTestConfig(t, path); again != cleared {
		t.Errorf("the second clear rewrote the file:\n%s\nwant:\n%s", again, cleared)
	}
}

// gestureSetOnDisk writes value as key through the setting writer's own internals — the row's render
// (renderSettingValue) and the splice-and-gate pair (scalarSetEdit) under the whole transaction
// (edit) — into a config holding content, and returns the file as written. It is the whole-value
// write a gesture-written key admits; which value a gesture hands the public writer is not asked here.
func gestureSetOnDisk(t *testing.T, content, key, value string) (string, error) {
	t.Helper()
	path := writeTestConfig(t, content)
	splice, verify, err := scalarSetEdit(mustKey(key), value)
	if err != nil {
		return "", err
	}
	if err := edit(path, splice, verify); err != nil {
		return "", err
	}
	return readTestConfig(t, path), nil
}

// The side panel's two lists are gesture-written structured keys: the writer admits them
// (writableKey), renders each value through the key's own parser and speller into one canonical flow
// line, splices that line wherever the scalar splice puts any key — rewriting one already there,
// keeping its note, or creating the blocks a bare file lacks — and verifies the result by the key's
// read-back hook, since a dotted-path read sees a list of maps as nothing. What lands is what the
// next start resolves: the row's own file projection reads it back as the canonical text.
func TestSpliceScalarSettingWritesGestureKeys(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, content, key, value string
		want                      string // the canonical text the key's line carries
		wantLine                  string // the whole line it lands on
	}{
		{
			name:    "sections into the template's side-panel block",
			content: string(defaultConfigYAML),
			key:     "ui.side-panel.sections", value: "[tasks: pinned, usage: hidden]",
			want:     "[tasks: pinned, usage: hidden, sub-agents: auto, changed-files: auto, mcp: auto]",
			wantLine: "    sections: [tasks: pinned, usage: hidden, sub-agents: auto, changed-files: auto, mcp: auto]",
		},
		{
			name:    "sections over a line already there keep its note",
			content: "ui:\n  side-panel:\n    sections: [mcp: pinned]  # mine\n",
			key:     "ui.side-panel.sections", value: "[mcp: hidden]",
			want:     "[mcp: hidden, sub-agents: auto, tasks: auto, changed-files: auto, usage: auto]",
			wantLine: "    sections: [mcp: hidden, sub-agents: auto, tasks: auto, changed-files: auto, usage: auto]  # mine",
		},
		{
			name:    "a whole sections list lands beside the block's other key, every section named",
			content: "ui:\n  side-panel:\n    show: auto\n",
			key:     "ui.side-panel.sections", value: "[usage: pinned]",
			want:     "[usage: pinned, sub-agents: auto, tasks: auto, changed-files: auto, mcp: auto]",
			wantLine: "    sections: [usage: pinned, sub-agents: auto, tasks: auto, changed-files: auto, mcp: auto]",
		},
		{
			name:    "a whole folded list lands beside the block's other key, a repeated name collapsed",
			content: "ui:\n  side-panel:\n    show: auto\n",
			key:     "ui.side-panel.folded", value: "[mcp, tasks, mcp]",
			want:     "[tasks, mcp]",
			wantLine: "    folded: [tasks, mcp]",
		},
		{
			name:    "folded into a bare file creates its blocks, in canonical order",
			content: "mode: auto\n",
			key:     "ui.side-panel.folded", value: "[mcp, usage, mcp]",
			want:     "[usage, mcp]",
			wantLine: "    folded: [usage, mcp]",
		},
		{
			name:    "folded to nothing is the empty list",
			content: "ui:\n  side-panel:\n    folded: [usage]\n",
			key:     "ui.side-panel.folded", value: "[]",
			want:     "[]",
			wantLine: "    folded: []",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			k := mustKey(tt.key)
			if _, err := writableKey(tt.key); err != nil {
				t.Fatalf("the writer refuses the gesture-written key: %v", err)
			}
			text, want, err := renderSettingValue(k, tt.value)
			if err != nil || text != tt.want || want != tt.want {
				t.Fatalf("render %q = (%q, %q, %v), want %q for both", tt.value, text, want, err, tt.want)
			}

			written, err := gestureSetOnDisk(t, tt.content, tt.key, tt.value)
			if err != nil {
				t.Fatalf("write %s: %v", tt.key, err)
			}
			if !slices.Contains(SplitConfigLines([]byte(written)), tt.wantLine) {
				t.Errorf("no line reads %q:\n%s", tt.wantLine, written)
			}
			assertOnlyKeyChanged(t, tt.content, written, tt.key)
			assertCommentsSurvive(t, tt.content, written)

			var fc fileConfig
			if err := yaml.Unmarshal([]byte(written), &fc); err != nil {
				t.Fatalf("the written file does not parse: %v", err)
			}
			if got, ok := k.readBack(fc); !ok || got != tt.want {
				t.Errorf("read back %q (set=%v), want %q", got, ok, tt.want)
			}
			var o Options
			if err := k.fromFile(&o, fc); err != nil || k.Read(o) != tt.want {
				t.Errorf("the next start resolves %q (err=%v), want %q", k.Read(o), err, tt.want)
			}

			// A re-set of what the file now says is a confirmation: nothing to write.
			splice, verify, err := scalarSetEdit(k, tt.value)
			if err != nil {
				t.Fatalf("state the re-set: %v", err)
			}
			if again, err := verifiedEdit([]byte(written), splice, verify); err != nil || again != nil {
				t.Errorf("a re-set rewrote the file (err=%v):\n%s", err, again)
			}
		})
	}
}

// What a gesture-written key will not take: a value its own parser refuses — refused before the file
// is opened, in the load's own sentence and led once — and a list the file writes one entry per line,
// which the writer will not fold into one (valueFitsOneLine).
func TestSpliceScalarSettingRefusesGestureKeyShapes(t *testing.T) {
	t.Parallel()
	k := mustKey("ui.side-panel.sections")
	err := validateSettingValue(k, "[nope: pinned]")
	switch {
	case err == nil:
		t.Fatal("a section that is no section was admitted")
	case !strings.HasPrefix(err.Error(), "apogee: invalid ui.side-panel.sections"):
		t.Errorf("error = %v, want the load's own sentence", err)
	case strings.Contains(err.Error(), "apogee: apogee:"):
		t.Errorf("error = %v, led twice", err)
	}

	if err := validateSettingValue(mustKey("ui.side-panel.folded"), "[lsp]"); err == nil ||
		!strings.Contains(err.Error(), `unknown section "lsp"`) {
		t.Errorf("a folded list naming no section: err = %v, want a refusal naming it", err)
	}

	const blockList = "ui:\n  side-panel:\n    sections:\n      - mcp: pinned\n"
	if _, err := gestureSetOnDisk(t, blockList, k.Path, "[mcp: hidden]"); err == nil ||
		!strings.Contains(err.Error(), "one entry per line") {
		t.Errorf("a list written one entry per line: err = %v, want a refusal naming the layout", err)
	}
}

// A pin, unpin or hide is one section's change, merged into the file's own list under the edit lock:
// the entries the file writes keep their order, their modes and the line's note, a partial list stays
// partial, and a section the list does not name brings in the whole list as the session resolves it
// — so the sections keep the order they show in. A file with no list gets the default with the one
// mode changed, in whatever of the blocks the file already has. landed is the list the file then
// resolves to, every section named.
func TestSaveConfigSettingMergesASectionModeDelta(t *testing.T) {
	t.Parallel()
	const resolvedDefault = "[sub-agents: auto, tasks: auto, changed-files: auto, usage: hidden, mcp: auto]"
	for _, tt := range []struct {
		name, content, delta string
		want                 string // the whole file after the write
		wantLanded           string
	}{
		{
			name: "a named section's mode changes in place, comments, order and other keys kept",
			content: "# my config\nmodel: qwen\nui:\n  spinner: glitter    # the sparkly one\n  side-panel:\n" +
				"    show: on\n    # the order I like\n" +
				"    sections: [tasks: pinned, usage: auto, sub-agents: hidden]  # mine\n  notify: bell\n",
			delta: "usage: hidden",
			want: "# my config\nmodel: qwen\nui:\n  spinner: glitter    # the sparkly one\n  side-panel:\n" +
				"    show: on\n    # the order I like\n" +
				"    sections: [tasks: pinned, usage: hidden, sub-agents: hidden]  # mine\n  notify: bell\n",
			wantLanded: "[tasks: pinned, usage: hidden, sub-agents: hidden, changed-files: auto, mcp: auto]",
		},
		{
			name:       "an unnamed section brings in the resolved list, its display order kept",
			content:    "ui:\n  side-panel:\n    sections: [tasks: pinned]\n",
			delta:      "usage: hidden",
			want:       "ui:\n  side-panel:\n    sections: [tasks: pinned, sub-agents: auto, changed-files: auto, usage: hidden, mcp: auto]\n",
			wantLanded: "[tasks: pinned, sub-agents: auto, changed-files: auto, usage: hidden, mcp: auto]",
		},
		{
			name:       "a reordered list keeps its order",
			content:    "ui:\n  side-panel:\n    sections: [mcp: auto, usage: pinned, tasks: auto, sub-agents: auto, changed-files: auto]\n",
			delta:      "tasks: hidden",
			want:       "ui:\n  side-panel:\n    sections: [mcp: auto, usage: pinned, tasks: hidden, sub-agents: auto, changed-files: auto]\n",
			wantLanded: "[mcp: auto, usage: pinned, tasks: hidden, sub-agents: auto, changed-files: auto]",
		},
		{
			name:       "no ui block",
			content:    "model: qwen\n",
			delta:      "usage: hidden",
			want:       "model: qwen\n\nui:\n  side-panel:\n    sections: " + resolvedDefault + "\n",
			wantLanded: resolvedDefault,
		},
		{
			name:       "a ui block with keys",
			content:    "ui:\n    spinner: snake\nmodel: qwen\n",
			delta:      "usage: hidden",
			want:       "ui:\n    spinner: snake\n    side-panel:\n      sections: " + resolvedDefault + "\nmodel: qwen\n",
			wantLanded: resolvedDefault,
		},
		{
			name:       "an empty ui block",
			content:    "ui:\nmodel: qwen\n",
			delta:      "usage: hidden",
			want:       "ui:\n  side-panel:\n    sections: " + resolvedDefault + "\nmodel: qwen\n",
			wantLanded: resolvedDefault,
		},
		{
			name:       "an empty side-panel block",
			content:    "ui:\n  side-panel:\n  spinner: snake\n",
			delta:      " usage :  hidden ",
			want:       "ui:\n  side-panel:\n    sections: " + resolvedDefault + "\n  spinner: snake\n",
			wantLanded: resolvedDefault,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTestConfig(t, tt.content)

			landed, err := SaveConfigSetting(path, "ui.side-panel.sections", tt.delta)

			if err != nil {
				t.Fatalf("SaveConfigSetting: %v", err)
			}
			if got := readTestConfig(t, path); got != tt.want {
				t.Errorf("config =\n%s\nwant\n%s", got, tt.want)
			}
			if landed != tt.wantLanded {
				t.Errorf("landed = %q, want %q", landed, tt.wantLanded)
			}
		})
	}
}

// A config with no list — here the seeded template itself, written for an absent file — gets the
// default list with the one mode changed, inserted as one line of the `side-panel:` block it joins.
func TestSaveConfigSettingSectionDeltaCreatesTheMissingListFromTheDefault(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	template, err := ReadConfigForWrite(path)
	if err != nil {
		t.Fatalf("seed the template: %v", err)
	}

	if _, err := SaveConfigSetting(path, "ui.side-panel.sections", "tasks: pinned"); err != nil {
		t.Fatalf("SaveConfigSetting: %v", err)
	}

	at, added := splicedInsertion(t, string(template), readTestConfig(t, path))
	want := "    sections: [sub-agents: auto, tasks: pinned, changed-files: auto, usage: auto, mcp: auto]"
	if len(added) != 1 || added[0] != want {
		t.Fatalf("inserted %q at line %d, want the one line %q", added, at, want)
	}
	if above := SplitConfigLines(template)[at-2]; !strings.HasPrefix(strings.TrimSpace(above), "show:") {
		t.Errorf("the list landed under %q, want it in the side-panel block after show:", above)
	}
}

// A delta the file already says is a confirmation: the file is not rewritten — for a mode the list
// already gives the section, and for a fold the list already holds — and landed is still the list
// the file resolves to.
func TestSaveConfigSettingMergeReSetWritesNothing(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, content, key, delta, wantLanded string
	}{
		{
			name:    "a section's mode the list already gives",
			content: "ui:\n  side-panel:\n    sections: [tasks: pinned, usage: auto]\n",
			key:     "ui.side-panel.sections", delta: "tasks: pinned",
			wantLanded: "[tasks: pinned, usage: auto, sub-agents: auto, changed-files: auto, mcp: auto]",
		},
		{
			name:    "a fold the list already holds",
			content: "ui:\n  side-panel:\n    folded: [usage, mcp]\n",
			key:     "ui.side-panel.folded", delta: "mcp: folded",
			wantLanded: "[usage, mcp]",
		},
		{
			name:    "an open over a list that folds nothing",
			content: "ui:\n  side-panel:\n    folded: []\n",
			key:     "ui.side-panel.folded", delta: "mcp: open",
			wantLanded: "",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTestConfig(t, tt.content)
			stale := time.Unix(1, 0)
			if err := os.Chtimes(path, stale, stale); err != nil {
				t.Fatalf("age the file: %v", err)
			}

			landed, err := SaveConfigSetting(path, tt.key, tt.delta)

			if err != nil {
				t.Fatalf("SaveConfigSetting: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if !info.ModTime().Equal(stale) || readTestConfig(t, path) != tt.content {
				t.Error("a re-set of what the file already says rewrote the file")
			}
			if landed != tt.wantLanded {
				t.Errorf("landed = %q, want %q", landed, tt.wantLanded)
			}
		})
	}
}

// A fold toggle is one section's change too: the file's other folded sections stay folded, the line
// lands in its canonical spelling beside the block's other keys, comments and other keys carried
// over, landed is the list the file now holds, and a reload reads that list back. Opening the last
// folded section leaves `[]` in the file, which reloads — and lands — as the zero value.
func TestSaveConfigSettingMergesAFoldDelta(t *testing.T) {
	t.Parallel()
	path := writeTestConfig(t, "# my config\n"+
		"ui:\n"+
		"  side-panel:\n"+
		"    show: on   # wide screen\n"+
		"    sections: [tasks: pinned]\n"+
		"  notify: bell\n")

	if _, err := SaveConfigSetting(path, "ui.side-panel.folded", "mcp: folded"); err != nil {
		t.Fatalf("fold mcp: %v", err)
	}
	landed, err := SaveConfigSetting(path, "ui.side-panel.folded", "usage: folded")

	if err != nil {
		t.Fatalf("fold usage: %v", err)
	}
	if want := "[usage, mcp]"; landed != want {
		t.Errorf("landed = %q, want %q", landed, want)
	}
	want := "# my config\n" +
		"ui:\n" +
		"  side-panel:\n" +
		"    show: on   # wide screen\n" +
		"    sections: [tasks: pinned]\n" +
		"    folded: [usage, mcp]\n" +
		"  notify: bell\n"
	if got := readTestConfig(t, path); got != want {
		t.Errorf("config =\n%s\nwant\n%s", got, want)
	}
	opts, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if string(opts.UI.SidePanelFolded) != landed {
		t.Errorf("folded reads back %q, want the landed %q", opts.UI.SidePanelFolded, landed)
	}

	for _, delta := range []string{"mcp: open", "usage: open"} {
		if landed, err = SaveConfigSetting(path, "ui.side-panel.folded", delta); err != nil {
			t.Fatalf("%s: %v", delta, err)
		}
	}

	if landed != "" {
		t.Errorf("landed = %q after opening every section, want the zero value", landed)
	}
	if got := readTestConfig(t, path); !strings.Contains(got, "    folded: []\n") {
		t.Errorf("config does not carry the empty list:\n%s", got)
	}
	if opts, err = LoadFileConfig(path, os.ReadFile, noNotify); err != nil || opts.UI.SidePanelFolded != "" {
		t.Errorf("reload = %q, %v; want the zero value", opts.UI.SidePanelFolded, err)
	}
}

// What a merge-hook key will not take, refused with the file left as it was: a value that is not one
// section's delta — a whole bracketed list among them, refused naming the delta's form — a section or
// mode this build does not know, a list the file writes one entry per line, a block in flow style,
// and a list in the file that no longer parses.
func TestSaveConfigSettingMergeRefusals(t *testing.T) {
	t.Parallel()
	const sections, folded = "ui.side-panel.sections", "ui.side-panel.folded"
	for _, tt := range []struct {
		name, content, key, delta, reason string
	}{
		{
			name:    "a whole sections list",
			content: "model: qwen\n", key: sections, delta: "[usage: pinned]",
			reason: `ui.side-panel.sections is written one section at a time, as "<section>: <mode>"`,
		},
		{
			name:    "two sections at once",
			content: "model: qwen\n", key: sections, delta: "usage: pinned, tasks: hidden",
			reason: `as "<section>: <mode>"`,
		},
		{
			name:    "a section with no mode",
			content: "model: qwen\n", key: sections, delta: "usage",
			reason: `as "<section>: <mode>"`,
		},
		{
			name:    "a whole folded list",
			content: "model: qwen\n", key: folded, delta: "[usage, mcp]",
			reason: `ui.side-panel.folded is written one section at a time, as "<section>: folded" or "<section>: open"`,
		},
		{
			name:    "a fold state that is neither folded nor open",
			content: "model: qwen\n", key: folded, delta: "usage: shut",
			reason: `as "<section>: folded" or "<section>: open"`,
		},
		{
			name:    "a fold delta naming no section",
			content: "model: qwen\n", key: folded, delta: ": folded",
			reason: `as "<section>: folded" or "<section>: open"`,
		},
		{
			name:    "an unknown section",
			content: "model: qwen\n", key: sections, delta: "lsp: hidden",
			reason: "unknown section",
		},
		{
			name:    "an unknown mode",
			content: "model: qwen\n", key: sections, delta: "tasks: folded",
			reason: "unknown mode",
		},
		{
			name:    "an unknown section to fold",
			content: "model: qwen\n", key: folded, delta: "lsp: folded",
			reason: "unknown section",
		},
		{
			name:    "a list one entry per line",
			content: "ui:\n  side-panel:\n    sections:\n      - tasks: pinned\n", key: sections, delta: "tasks: hidden",
			reason: "one entry per line",
		},
		{
			name:    "a side-panel block in flow style",
			content: "ui:\n  side-panel: {show: auto}\n", key: sections, delta: "tasks: hidden",
			reason: "flow style",
		},
		{
			name:    "a sections list in the file that no longer parses",
			content: "ui:\n  side-panel:\n    sections: [tasks: pinned, tasks: auto]\n", key: sections, delta: "usage: hidden",
			reason: "named twice",
		},
		{
			name:    "a folded list in the file that no longer parses",
			content: "ui:\n  side-panel:\n    folded: [lsp]\n", key: folded, delta: "usage: folded",
			reason: "unknown section",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTestConfig(t, tt.content)

			landed, err := SaveConfigSetting(path, tt.key, tt.delta)

			if err == nil || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("err = %v, want a refusal naming %q", err, tt.reason)
			}
			if landed != "" {
				t.Errorf("a refused write landed %q", landed)
			}
			if got := readTestConfig(t, path); got != tt.content {
				t.Errorf("a refused write changed the file:\n%s", got)
			}
		})
	}
}

// A refused delta never seeds the template: it is judged before the file is opened, so an absent
// config stays absent.
func TestSaveConfigSettingRefusedDeltaSeedsNothing(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ key, delta string }{
		{"ui.side-panel.sections", "lsp: pinned"},
		{"ui.side-panel.sections", "[tasks: pinned]"},
		{"ui.side-panel.folded", "lsp: folded"},
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if _, err := SaveConfigSetting(path, tt.key, tt.delta); err == nil {
			t.Fatalf("%s %q was accepted", tt.key, tt.delta)
		}
		if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
			t.Errorf("%s %q: a refused write created the config (stat: %v)", tt.key, tt.delta, statErr)
		}
	}
}

// landed for a scalar key is the value as the file now spells it — trimmed and canonical — not the
// keystrokes the caller handed over.
func TestSaveConfigSettingAnswersTheScalarAsTheFileSpellsIt(t *testing.T) {
	t.Parallel()
	path := writeTestConfig(t, "mode: auto\n")
	for _, tt := range []struct{ key, value, want string }{
		{"ui.spinner", "  glitter ", "glitter"},
		{"context-files.names", "AGENTS.md, CLAUDE.md,", "[AGENTS.md, CLAUDE.md]"},
		{"auto-compact", "TRUE", "true"},
	} {
		landed, err := SaveConfigSetting(path, tt.key, tt.value)
		if err != nil || landed != tt.want {
			t.Errorf("SaveConfigSetting(%s, %q) = %q, %v; want %q", tt.key, tt.value, landed, err, tt.want)
		}
	}
}

// `ui.side-panel.show` lands through the setting writer like any nested enum — rewritten in place
// with its note kept, or inserted beside the block's list where the file does not set it — and a
// mode it cannot hold is refused before the file is opened, so an absent config stays absent.
func TestSaveConfigSettingWritesSidePanelShow(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, content, want string
	}{
		{
			name:    "rewritten in place",
			content: "ui:\n  side-panel:\n    show: auto           # by width\n",
			want:    "ui:\n  side-panel:\n    show: \"off\"           # by width\n",
		},
		{
			name:    "inserted beside the list",
			content: "ui:\n  side-panel:\n    sections: [tasks: pinned]\n",
			want:    "ui:\n  side-panel:\n    sections: [tasks: pinned]\n    show: \"off\"\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTestConfig(t, tt.content)

			if _, err := SaveConfigSetting(path, "ui.side-panel.show", "off"); err != nil {
				t.Fatalf("SaveConfigSetting: %v", err)
			}

			if got := readTestConfig(t, path); got != tt.want {
				t.Errorf("config =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := SaveConfigSetting(path, "ui.side-panel.show", "sometimes"); err == nil {
		t.Fatal("an unknown show mode was accepted")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("a refused write created the config (stat: %v)", statErr)
	}
}

// A reset of `ui.side-panel.show` removes its line, so the key reads back at its default. Comments,
// order and the other keys stay as found; a block the removal empties goes with it, and `ui:` too
// when the block was all it held.
func TestResetConfigSettingRemovesSidePanelShow(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, content, want string
	}{
		{
			name: "beside the list",
			content: "# mine\nui:\n  spinner: glitter\n  side-panel:\n    show: \"off\"   # by hand\n" +
				"    # the order I like\n    sections: [tasks: pinned]\n  notify: bell\n",
			want: "# mine\nui:\n  spinner: glitter\n  side-panel:\n" +
				"    # the order I like\n    sections: [tasks: pinned]\n  notify: bell\n",
		},
		{
			name:    "the block's only key",
			content: "ui:\n  spinner: glitter\n  side-panel:\n    show: on\ncurrency: EUR\n",
			want:    "ui:\n  spinner: glitter\ncurrency: EUR\n",
		},
		{
			name:    "ui's only block",
			content: "currency: EUR\nui:\n  side-panel:\n    show: on\n",
			want:    "currency: EUR\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTestConfig(t, tt.content)

			if err := ResetConfigSetting(path, "ui.side-panel.show"); err != nil {
				t.Fatalf("ResetConfigSetting: %v", err)
			}

			if got := readTestConfig(t, path); got != tt.want {
				t.Errorf("config =\n%s\nwant\n%s", got, tt.want)
			}
			opts, err := LoadFileConfig(path, os.ReadFile, noNotify)
			if err != nil {
				t.Fatalf("LoadFileConfig: %v", err)
			}
			if opts.UI.SidePanelShow != domain.DefaultSidePanelShow {
				t.Errorf("show reads back %q, want the default %q", opts.UI.SidePanelShow, domain.DefaultSidePanelShow)
			}
		})
	}
}

// A reset of a side-panel key the file does not set is already at its default: the file is not
// rewritten. The two gesture-written lists, which no surface resets, are refused with the file
// untouched.
func TestResetConfigSettingSidePanelNoOpAndRefusals(t *testing.T) {
	t.Parallel()
	const body = "ui:\n  side-panel:\n    sections: [tasks: pinned]\n"
	path := writeTestConfig(t, body)
	stale := time.Unix(1, 0)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatalf("age the file: %v", err)
	}

	if err := ResetConfigSetting(path, "ui.side-panel.show"); err != nil {
		t.Fatalf("ResetConfigSetting of an unset key: %v", err)
	}
	for _, key := range []string{"ui.side-panel.sections", "ui.side-panel.folded"} {
		if err := ResetConfigSetting(path, key); err == nil {
			t.Errorf("ResetConfigSetting(%q) succeeded; want a refusal", key)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.ModTime().Equal(stale) || readTestConfig(t, path) != body {
		t.Error("a no-op or refused reset rewrote the file")
	}
}

// `ui.side-panel.width` lands beside the block's other keys and resets back off the file, `show` and
// `sections` kept as found both ways; a width below 24 is refused before the file is opened, and the
// file is left as it was.
func TestSaveConfigSettingWritesAndResetsSidePanelWidth(t *testing.T) {
	t.Parallel()
	const body = "ui:\n  side-panel:\n    show: on             # by hand\n" +
		"    sections: [tasks: pinned]\n"
	path := writeTestConfig(t, body)

	if _, err := SaveConfigSetting(path, "ui.side-panel.width", "23"); err == nil {
		t.Error("SaveConfigSetting(width 23) succeeded; want a refusal")
	}
	if got := readTestConfig(t, path); got != body {
		t.Fatalf("a refused width rewrote the file:\n%s", got)
	}
	if _, err := SaveConfigSetting(path, "ui.side-panel.width", "48"); err != nil {
		t.Fatalf("SaveConfigSetting(width 48): %v", err)
	}
	if got, want := readTestConfig(t, path), body+"    width: 48\n"; got != want {
		t.Errorf("config after save =\n%s\nwant\n%s", got, want)
	}
	if _, err := SaveConfigSetting(path, "ui.side-panel.width", "40"); err != nil {
		t.Fatalf("SaveConfigSetting(width 40): %v", err)
	}
	if got, want := readTestConfig(t, path), body+"    width: 40\n"; got != want {
		t.Errorf("config after a rewrite in place =\n%s\nwant\n%s", got, want)
	}
	opts, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	if opts.UI.SidePanelWidth != 40 || opts.UI.SidePanelShow != domain.SidePanelShowOn {
		t.Errorf("read back width %d show %q; want 40 and on", opts.UI.SidePanelWidth, opts.UI.SidePanelShow)
	}

	if err := ResetConfigSetting(path, "ui.side-panel.width"); err != nil {
		t.Fatalf("ResetConfigSetting(width): %v", err)
	}

	if got := readTestConfig(t, path); got != body {
		t.Errorf("config after reset =\n%s\nwant\n%s", got, body)
	}
	opts, err = LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	if opts.UI.SidePanelWidth != 0 {
		t.Errorf("width reads back %d after reset, want 0, the unset width", opts.UI.SidePanelWidth)
	}
}
