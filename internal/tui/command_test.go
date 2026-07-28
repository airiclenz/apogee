package tui

import (
	"reflect"
	"strings"
	"testing"
)

// ----------------------------------------------------------------------------
// The chat mini-language parser (command.go)
// ----------------------------------------------------------------------------

func TestParseInputCommands(t *testing.T) {
	cases := []struct {
		in   string
		verb string
	}{
		{"/clear", "clear"},
		{"/new", "new"}, // alias of /clear — recognised as its own verb, routed to the same logic
		{"/compact", "compact"},
		{"/continue", "continue"},
		{"/confine", "confine"},
		{"/version", "version"},
		{"/version now", "version"},    // surplus args ignored, like /clear
		{"  /clear  ", "clear"},        // surrounding whitespace is trimmed
		{"/clear extra args", "clear"}, // trailing args ignored (these commands take none)
	}
	for _, c := range cases {
		got := parseInput(c.in, nil)
		if got.kind != kindCommand || got.command != c.verb {
			t.Errorf("parseInput(%q) = {kind:%v cmd:%q}, want command %q", c.in, got.kind, got.command, c.verb)
		}
	}
}

// TestCommandTableDrivesParserAndMenu pins the one-registry guarantee: commandSpecs is the only
// list of "/" verbs, so a verb can never be offered by the dropdown while the parser has never
// heard of it (or vice versa). Exactly the non-menuOnly rows parse; every row is offered.
func TestCommandTableDrivesParserAndMenu(t *testing.T) {
	for _, spec := range commandSpecs {
		t.Run(spec.name, func(t *testing.T) {
			verb, _, ok := matchCommand("/" + spec.name)
			if ok == spec.menuOnly {
				t.Fatalf("matchCommand(%q) recognised = %v, want %v (menuOnly = %v)",
					"/"+spec.name, ok, !spec.menuOnly, spec.menuOnly)
			}
			if ok && verb != spec.name {
				t.Errorf("matchCommand(%q) verb = %q, want %q", "/"+spec.name, verb, spec.name)
			}
		})
	}

	// The parser's verb set survived the merge intact: the seven real commands, and /skill
	// offered by the menu alone.
	var parsed, menuOnly []string
	for _, spec := range commandSpecs {
		if spec.menuOnly {
			menuOnly = append(menuOnly, spec.name)
			continue
		}
		parsed = append(parsed, spec.name)
	}
	wantParsed := []string{"clear", "new", "sessions", "compact", "continue", "confine", "version", "skills"}
	if !reflect.DeepEqual(parsed, wantParsed) {
		t.Errorf("parser verbs = %v, want %v", parsed, wantParsed)
	}
	if !reflect.DeepEqual(menuOnly, []string{"skill"}) {
		t.Errorf("menu-only verbs = %v, want [skill]", menuOnly)
	}

	// The empty partial lists every row, in table order, labelled from the same table.
	var want []string
	for _, spec := range commandSpecs {
		want = append(want, spec.name)
	}
	var got []string
	for _, it := range commandSuggestions("") {
		got = append(got, it.value)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commandSuggestions(\"\") = %v, want every row in table order %v", got, want)
	}
	for i, it := range commandSuggestions("") {
		if wantLabel := "/" + commandSpecs[i].name + "  " + commandSpecs[i].summary; it.label != wantLabel {
			t.Errorf("row %d label = %q, want %q", i, it.label, wantLabel)
		}
	}
}

func TestParseInputUnknownSlashIsMessage(t *testing.T) {
	// An unrecognised /verb is NOT a command — it is sent to the agent verbatim, so a real
	// message that happens to start with "/" (a path, a typo) is never silently swallowed.
	for _, in := range []string{"/skill foo", "/unknown", "/usr/local/bin matters", "/"} {
		got := parseInput(in, nil)
		if got.kind != kindMessage {
			t.Errorf("parseInput(%q).kind = %v, want message", in, got.kind)
		}
	}
}

func TestParseInputMessageExtractsFileRefs(t *testing.T) {
	got := parseInput("look at @main.go and @internal/agent/loop.go please", nil)
	if got.kind != kindMessage {
		t.Fatalf("kind = %v, want message", got.kind)
	}
	if got.text != "look at @main.go and @internal/agent/loop.go please" {
		t.Errorf("text was rewritten: %q (the literal @tokens must stay so the model sees them)", got.text)
	}
	want := []string{"main.go", "internal/agent/loop.go"}
	if !reflect.DeepEqual(got.fileRefs, want) {
		t.Errorf("fileRefs = %v, want %v", got.fileRefs, want)
	}
}

func TestExtractFileRefs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"none", "just a plain message", nil},
		{"at start", "@file.go here", []string{"file.go"}},
		{"after space", "see @a/b.go", []string{"a/b.go"}},
		{"multiple", "@x @y @z", []string{"x", "y", "z"}},
		{"dedup first-seen", "@x and @x again", []string{"x"}},
		{"email is not a ref", "mail me at foo@bar.com", nil},
		{"mid-word @ is not a ref", "user@host path", nil},
		{"trailing bare @ ignored", "ends with @", nil},
		{"path with dots", "@./internal/x.go", []string{"./internal/x.go"}},
		// Quoted refs — a path with spaces is unreachable without them (ISSUES [A]).
		{
			"quoted path with spaces",
			`@"docs/plans/2026-07-23 - 04 - version-build-number-plan.md"`,
			[]string{"docs/plans/2026-07-23 - 04 - version-build-number-plan.md"},
		},
		{"closing quote ends the token", `see @"a b.md", thanks`, []string{"a b.md"}},
		{"single quotes accepted", "see @'a b.md' now", []string{"a b.md"}},
		{"quoted without spaces", `@"main.go"`, []string{"main.go"}},
		{"dedup across forms", `@x and @"x"`, []string{"x"}},
		{"quoted then bare", `@"a b.md" and @main.go`, []string{"a b.md", "main.go"}},
		{"unterminated quote runs to end", `@"a b`, []string{"a b"}},
		{"unterminated quote stops at newline", "@\"a b\nnext @c.go line", []string{"a b", "c.go"}},
		{"unterminated quote right-trimmed", "@\"a b  \t\nnext", []string{"a b"}},
		{"empty quoted path ignored", `@"" here`, nil},
		{"quoted email is still not a ref", `mail me at foo@"bar baz.com"`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, refs := extractFileRefs(c.in)
			if !reflect.DeepEqual(refs, c.want) {
				t.Errorf("extractFileRefs(%q) = %v, want %v", c.in, refs, c.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Inline /skill tokens — the second half of the mini-language
// ----------------------------------------------------------------------------

// knownSkills builds the catalog predicate parseInput/extractSkillRefs resolve against, from a
// literal set of ids — the pure-layer stand-in for Model.knownSkillID.
func knownSkills(ids ...string) func(string) bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return func(id string) bool { return set[id] }
}

func TestExtractSkillRefs(t *testing.T) {
	known := knownSkills("grill-me", "code-audit", "clear")
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"none", "just a plain message", nil},
		{"at start", "/grill-me please", []string{"grill-me"}},
		{"after space", "now /code-audit this", []string{"code-audit"}},
		{"whole input", "/grill-me", []string{"grill-me"}},
		{"multiple", "/grill-me and /code-audit", []string{"grill-me", "code-audit"}},
		{"dedup first-seen", "/code-audit twice /code-audit", []string{"code-audit"}},
		{"unknown token ignored", "/code-adit please", nil},
		{"absolute path survives", "look in /usr/bin for it", nil},
		{"mid-word slash is not a token", "and/or /grill-me", []string{"grill-me"}},
		{"trailing punctuation is part of the token", "/grill-me, thanks", nil},
		{"newline is a boundary", "first line\n/code-audit", []string{"code-audit"}},
		{"tab is a boundary", "go\t/grill-me", []string{"grill-me"}},
		{"bare slash ignored", "/ alone", nil},
		{"nested path token not split", "/usr/grill-me", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractSkillRefs(c.in, known); !reflect.DeepEqual(got, c.want) {
				t.Errorf("extractSkillRefs(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}

	// A nil predicate means no catalog is wired: every token is prose.
	if got := extractSkillRefs("/grill-me now", nil); got != nil {
		t.Errorf("extractSkillRefs with a nil predicate = %v, want nil", got)
	}
}

// A message keeps its skill tokens IN the text (the @ref posture) and reports them as references,
// while the whole-input command rule still wins outright — a command verb SHADOWS a skill of the
// same id.
func TestParseInputSkillTokens(t *testing.T) {
	known := knownSkills("grill-me", "clear")

	got := parseInput("/grill-me check @main.go", known)
	if got.kind != kindMessage {
		t.Fatalf("kind = %v, want message", got.kind)
	}
	if want := "/grill-me check @main.go"; got.text != want {
		t.Errorf("text = %q, want %q (the /token stays in place)", got.text, want)
	}
	if want := []string{"grill-me"}; !reflect.DeepEqual(got.skillIDs, want) {
		t.Errorf("skillIDs = %v, want %v", got.skillIDs, want)
	}
	if want := []string{"main.go"}; !reflect.DeepEqual(got.fileRefs, want) {
		t.Errorf("fileRefs = %v, want %v", got.fileRefs, want)
	}

	// "clear" is both a command verb and (here) a skill id: the command wins, and no skill
	// reference is extracted from a line the parser never treats as a message.
	cmd := parseInput("/clear", known)
	if cmd.kind != kindCommand || cmd.command != "clear" {
		t.Fatalf("parseInput(/clear) = {kind:%v cmd:%q}, want the clear command", cmd.kind, cmd.command)
	}
	if len(cmd.skillIDs) != 0 {
		t.Errorf("a command carried skill refs: %v", cmd.skillIDs)
	}
	// The same id mid-message is an ordinary skill reference again.
	if got := parseInput("please /clear the mess", known); !reflect.DeepEqual(got.skillIDs, []string{"clear"}) {
		t.Errorf("mid-message /clear skillIDs = %v, want [clear]", got.skillIDs)
	}
}

// ----------------------------------------------------------------------------
// /confine — the one verb with arguments
// ----------------------------------------------------------------------------

func TestParseInputConfineGrammar(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want confineArgs
	}{
		{"bare is status", "/confine", confineArgs{action: confineStatus}},
		{"explicit status", "/confine status", confineArgs{action: confineStatus}},
		{"off is session-only", "/confine off", confineArgs{action: confineOff}},
		{"off saves the host", "/confine off --save", confineArgs{action: confineOff, save: true}},
		{"on re-confines", "/confine on", confineArgs{action: confineOn}},
		{"whitespace tolerated", "  /confine   off   --save  ", confineArgs{action: confineOff, save: true}},
		{"tab separated", "/confine\toff", confineArgs{action: confineOff}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseInput(c.in, nil)
			if got.kind != kindCommand || got.command != "confine" {
				t.Fatalf("parseInput(%q) = {kind:%v cmd:%q}, want the confine command", c.in, got.kind, got.command)
			}
			if got.err != nil {
				t.Fatalf("parseInput(%q).err = %v, want nil", c.in, got.err)
			}
			if got.confine != c.want {
				t.Errorf("parseInput(%q).confine = %+v, want %+v", c.in, got.confine, c.want)
			}
		})
	}
}

func TestParseInputConfineArgumentErrors(t *testing.T) {
	// Every bad-argument form stays a COMMAND carrying an error, so the router can report the
	// usage line: neither swallowed silently nor forwarded to the agent as a message.
	cases := []struct {
		name string
		in   string
	}{
		{"unknown subcommand", "/confine sideways"},
		{"unknown flag", "/confine off --force"},
		{"save without a subcommand", "/confine --save"},
		{"save on status", "/confine status --save"},
		{"save on on", "/confine on --save"},
		{"stray argument", "/confine off please"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseInput(c.in, nil)
			if got.kind != kindCommand || got.command != "confine" {
				t.Fatalf("parseInput(%q) = {kind:%v cmd:%q}, want the confine command", c.in, got.kind, got.command)
			}
			if got.err == nil {
				t.Fatalf("parseInput(%q).err = nil, want an argument error", c.in)
			}
			if !strings.Contains(got.err.Error(), confineUsage) {
				t.Errorf("parseInput(%q).err = %q, want it to carry %q", c.in, got.err, confineUsage)
			}
			if got.confine != (confineArgs{}) {
				t.Errorf("parseInput(%q).confine = %+v, want the zero value on an error", c.in, got.confine)
			}
		})
	}
}

func TestParseInputBlankIsEmptyMessage(t *testing.T) {
	got := parseInput("   ", nil)
	if got.kind != kindMessage || got.text != "" {
		t.Errorf("parseInput(blank) = {kind:%v text:%q}, want empty message", got.kind, got.text)
	}
}
