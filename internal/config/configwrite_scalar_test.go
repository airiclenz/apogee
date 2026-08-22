package config

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

// The `validated-sets:` off-switch, in every state the block it lives in can be in: absent from the
// file, present as the template's commented example, and live — with the alias map already under it,
// or with the off-switch itself already written. It is the one key of that block a surface writes
// (its alias map stays an editor's job), and the alias map is what must survive the write: the two
// keys are one block, and a write that took the aliases with it would silently unbind a model.
func TestSpliceScalarSettingWritesTheValidatedSetsOffSwitch(t *testing.T) {
	t.Parallel()
	k := mustKey("validated-sets.enable")
	for _, tt := range []struct {
		name    string
		content string
		aliases int
	}{
		{name: "no block at all", content: readFixture(t, editedConfigFixture)},
		{name: "the template's commented example", content: string(defaultConfigYAML)},
		{
			name:    "a live block carrying the alias map",
			content: "mode: auto\nvalidated-sets:\n  alias:\n    my-gemma: gemma-4\n",
			aliases: 1,
		},
		{
			name:    "a live block whose off-switch is already written",
			content: "validated-sets:\n  enable: true\n  alias:\n    my-gemma: gemma-4\n",
			aliases: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			updated, err := setScalarSetting([]byte(tt.content), k, "false")
			if err != nil {
				t.Fatalf("splice %s: %v", k.Path, err)
			}
			if updated == nil {
				t.Fatalf("splice %s reported nothing to write", k.Path)
			}
			if got, ok, err := scalarAtPath(updated, k.Path); err != nil || !ok || got != "false" {
				t.Fatalf("after the write %s reads %q (ok=%v, err=%v), want %q", k.Path, got, ok, err, "false")
			}
			assertOnlyKeyChanged(t, tt.content, string(updated), k.Path)

			// The read-back that matters is the one the session does: the loader's own, off the file
			// as written, rather than a second parse of the text by the test.
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, updated, 0o600); err != nil {
				t.Fatalf("write the spliced config: %v", err)
			}
			file, err := LoadFileConfig(path, os.ReadFile, noNotify)
			if err != nil {
				t.Fatalf("load the spliced config: %v", err)
			}
			if file.ValidatedSetsEnable {
				t.Error("the loaded off-switch resolves to on, want the explicit false the write left")
			}
			if got := len(file.ValidatedSetsAlias); got != tt.aliases {
				t.Errorf("the write left %d aliases, want %d", got, tt.aliases)
			}
		})
	}
}

// The block-scalar writer, against the file it will actually meet: the seeded template, whose
// `system-prompt-text:` block is its ONE active key and the only multi-line value the schema has. The
// template is the hardest case the writer has — the prompt sits in the middle of twenty lines of
// documentation, with more documentation directly under it — so what is checked is that every one of
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
				err = SaveConfigSetting(path, tt.path, tt.value)
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
	if err := SaveConfigSetting(path, "ui.spinner", "glitter"); err != nil {
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
				err = SaveConfigSetting(path, tt.path, tt.value)
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
