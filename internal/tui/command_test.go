package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/skills"
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
// heard of it (or vice versa). Every row parses, and every row is offered.
func TestCommandTableDrivesParserAndMenu(t *testing.T) {
	for _, spec := range commandSpecs {
		t.Run(spec.name, func(t *testing.T) {
			verb, _, ok := matchCommand("/" + spec.name)
			if !ok {
				t.Fatalf("matchCommand(%q) was not recognised; every registry row parses", "/"+spec.name)
			}
			if verb != spec.name {
				t.Errorf("matchCommand(%q) verb = %q, want %q", "/"+spec.name, verb, spec.name)
			}
		})
	}

	// The parser's verb set survived the merge intact: every row of the registry is a real command.
	var parsed []string
	for _, spec := range commandSpecs {
		parsed = append(parsed, spec.name)
	}
	wantParsed := []string{
		"clear", "color-scheme", "compact", "confine", "continue", "effort", "inspect", "model",
		"new", "rename", "schedule", "schedule-stop", "server", "sessions", "settings", "skills",
		"stop-server", "undo", "unload-model", "usage", "version"}
	if !reflect.DeepEqual(parsed, wantParsed) {
		t.Errorf("parser verbs = %v, want %v", parsed, wantParsed)
	}

	// The empty partial lists EVERY row, in table order, with its cells read off the same table — no
	// verb of the registry is withheld from the menu when the moment allows it (the one gated row,
	// /effort, is offered here because the model reports a dial). The row is CELLS, not one
	// concatenated label:
	// the verb, its summary and the (here empty, because idle) idle-only tag, each its own column.
	var want []string
	var wantCells []popupRow
	for _, spec := range commandSpecs {
		want = append(want, spec.name)
		wantCells = append(wantCells, popupRow{"/" + spec.name, spec.summary, ""})
	}
	var got []string
	var gotCells []popupRow
	for _, it := range commandSuggestions("", false, true) {
		got = append(got, it.value)
		gotCells = append(gotCells, it.cells)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commandSuggestions(\"\") = %v, want every offered row in table order %v", got, want)
	}
	if !reflect.DeepEqual(gotCells, wantCells) {
		t.Errorf("cells = %q, want them read off the table %q", gotCells, wantCells)
	}
}

// ----------------------------------------------------------------------------
// /effort — the thinking-effort verb (effort.go's popup and its accept)
// ----------------------------------------------------------------------------

// dialledEffort is a bound model that reports a dial and nothing else about it — the llama.cpp
// /props sighting, whose chat template proves the dial exists and names no vocabulary, which is what
// makes the picker fall back to the canonical four.
func dialledEffort() provider.EffortSupport {
	return provider.EffortSupport{Supported: true, Dialect: provider.EffortDialectKwargs}
}

// openEffortPicker drives "/effort" through the real key path against a model with the given dial and
// hands back the Model it left, so a test asserts on the pane a human would be looking at rather than
// on a picker some helper opened behind the verb's back.
func openEffortPicker(t *testing.T, eng *fakeEngine, support provider.EffortSupport) Model {
	t.Helper()
	m := newTestModelEng(t, eng, testOpts)
	m.hb.effort = support
	m.input.SetValue("/effort")
	m, cmd := stepCmd(t, m, keyEnter())
	if cmd != nil {
		t.Error("/effort returned a Cmd; it is synchronous and must not launch a worker")
	}
	if m.state != stateIdle {
		t.Errorf("state = %v, want idle (/effort must not launch a worker)", m.state)
	}
	return m
}

// The verb ASKS rather than reads: the level-word grammar is deleted (ADR 0060 D7), so /effort opens
// the popup and — until a row is taken — has moved nothing and said nothing.
func TestEffortCommandOpensThePicker(t *testing.T) {
	eng := &fakeEngine{effortProfile: domain.EffortLow}
	m := openEffortPicker(t, eng, dialledEffort())

	if !m.picker.open || m.picker.kind != pickerEffort {
		t.Fatalf("picker = {open:%v kind:%v}, want the effort picker up", m.picker.open, m.picker.kind)
	}
	if got := eng.effortsSet(); len(got) != 0 {
		t.Errorf("SetEffortOverride calls = %v, want none until a row is accepted", got)
	}
	if got := lastNote(m); got != "" {
		t.Errorf("note = %q, want none — the pane is the answer", got)
	}
}

// A model with no dial keeps the verb ROUTABLE and answers it with one note: the menu withholds the
// row (ADR 0060 D5), but a hand-typed line still lands, and a verb that resolved and then did nothing
// would read as a broken command rather than as an absent dial.
func TestEffortWithoutADialAnswersWithOneNote(t *testing.T) {
	eng := &fakeEngine{effortProfile: domain.EffortLow}
	m := openEffortPicker(t, eng, provider.EffortSupport{})

	if m.picker.open {
		t.Errorf("picker kind %v opened over a model with no dial", m.picker.kind)
	}
	if got := lastNote(m); got != noEffortDialNote {
		t.Errorf("note = %q, want %q", got, noEffortDialNote)
	}
	if got := eng.effortsSet(); len(got) != 0 {
		t.Errorf("SetEffortOverride calls = %v, want none — there is no dial to move", got)
	}
}

// TestEffortPickerAcceptDrivesTheEngineDoor is the run half, over the rows a human walks to: a level
// row reaches Engine.SetEffortOverride as itself, the appended "auto" row reaches it as the zero
// value that CLEARS the override, and every accept closes the pane on the binding resolution — both
// layers named, because a level means one thing as an override and another as a profile setting.
func TestEffortPickerAcceptDrivesTheEngineDoor(t *testing.T) {
	reported := provider.EffortSupport{
		Supported: true,
		Dialect:   provider.EffortDialectReasoning,
		Efforts:   []string{"none", "low", "high"},
		Default:   "low",
	}
	cases := []struct {
		name     string
		support  provider.EffortSupport
		profile  domain.ThinkingEffort
		override domain.ThinkingEffort // what the session already carried before the pick
		down     int                   // ↓ presses before ⏎
		wantSet  []domain.ThinkingEffort
		wantNote string
	}{
		{
			name:     "the highlighted first row is the first level offered",
			support:  dialledEffort(),
			wantSet:  []domain.ThinkingEffort{domain.EffortOff},
			wantNote: "thinking effort: off (session override: off; profile: —)",
		},
		{
			name:     "a level layers above the profile",
			support:  dialledEffort(),
			profile:  domain.EffortLow,
			down:     3, // off, low, medium, HIGH
			wantSet:  []domain.ThinkingEffort{domain.EffortHigh},
			wantNote: "thinking effort: high (session override: high; profile: low)",
		},
		{
			name:     "the auto row clears the override with the zero value",
			support:  dialledEffort(),
			profile:  domain.EffortLow,
			override: domain.EffortHigh,
			down:     4, // past the canonical four, onto "auto"
			wantSet:  []domain.ThinkingEffort{""},
			wantNote: "thinking effort: low (session override: —; profile: low)",
		},
		{
			name:     "a reported set is picked in the server's own words",
			support:  reported,
			down:     2, // none, low, HIGH
			wantSet:  []domain.ThinkingEffort{domain.EffortHigh},
			wantNote: "thinking effort: high (session override: high; profile: —)",
		},
		{
			name:     "auto sits under a reported set too",
			support:  reported,
			override: domain.EffortHigh,
			down:     3, // past the three reported levels
			wantSet:  []domain.ThinkingEffort{""},
			wantNote: "thinking effort: the model's own default (session override: —; profile: —)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eng := &fakeEngine{effortProfile: c.profile, effortOverride: c.override}
			m := openEffortPicker(t, eng, c.support)
			for range c.down {
				m = step(t, m, keyDown())
			}

			m, cmd := stepCmd(t, m, keyEnter())

			if cmd != nil {
				t.Error("the accept returned a Cmd; it moves session state and launches no worker")
			}
			if m.picker.open {
				t.Error("the accept left the picker open")
			}
			if got := eng.effortsSet(); !slices.Equal(got, c.wantSet) {
				t.Errorf("SetEffortOverride calls = %v, want %v", got, c.wantSet)
			}
			if got := lastNote(m); got != c.wantNote {
				t.Errorf("note = %q, want %q", got, c.wantNote)
			}
		})
	}
}

// /effort is safe while a worker works, unlike /confine beside it in the table — the pane opens over
// a running Exchange and its accept moves the dial — because the override is read when the NEXT
// request is built: a level set mid-Turn changes nothing about the one in flight, and mid-Turn is
// exactly when a human notices the model is thinking too hard.
func TestEffortRunsWhileTheWorkerWorks(t *testing.T) {
	eng := &fakeEngine{effortProfile: domain.EffortLow}
	m := newTestModelEng(t, eng, testOpts)
	m.hb.effort = dialledEffort()
	m.input.SetValue("open the exchange")
	m, _ = stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}

	m.input.SetValue("/effort")
	m = step(t, m, keyEnter())
	if !m.picker.open || m.picker.kind != pickerEffort {
		t.Fatalf("picker = {open:%v kind:%v}, want the effort picker up mid-Exchange", m.picker.open, m.picker.kind)
	}

	m = step(t, m, keyDown()) // off → low
	m = step(t, m, keyDown()) // low → medium
	m = step(t, m, keyDown()) // medium → high
	m = step(t, m, keyEnter())

	if got := eng.effortsSet(); !slices.Equal(got, []domain.ThinkingEffort{domain.EffortHigh}) {
		t.Errorf("SetEffortOverride calls = %v, want [high] — the pick lands mid-Exchange too", got)
	}
	if got := lastNote(m); got != "thinking effort: high (session override: high; profile: low)" {
		t.Errorf("note = %q, want the resolution stated mid-run", got)
	}
	if m.state != stateRunning {
		t.Errorf("state = %v, want the Exchange still running (the verb touches no worker)", m.state)
	}
}

// firstCommandToken is the parser's cut, and the menu's guard borrows it — so what it cuts on is a
// contract, not an implementation detail. A space or a tab ends the verb; a NEWLINE does not, which
// is what keeps a multi-line message whose first line reads "/clear" a message rather than a
// command.
func TestFirstCommandTokenIsTheParsersCut(t *testing.T) {
	cases := map[string]string{
		"/clear":              "/clear",
		"/confine off --save": "/confine",
		"/schedule\t1m auto":  "/schedule",
		"/clear\nmore":        "/clear\nmore", // a newline is not a boundary: the line stays a message
		" leading":            "",
		"":                    "",
	}
	for in, want := range cases {
		if got := firstCommandToken(in); got != want {
			t.Errorf("firstCommandToken(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNoSkillIDCanOutParseABuiltinVerb pins the agreement the shadow guard exists for, across the
// WHOLE registry rather than one verb: for every command apogee ships, a catalog id that opens with
// that verb and carries arguments — the shape a hostile repo would write into `.apogee/skills`,
// which loads unconditionally and re-scans mid-session — is read by the parser as that command, and
// is therefore withheld from the merged menu. The two halves are asserted together on purpose: the
// first is the premise (accepting the row WOULD run the verb) and the second is the guard, so a
// future change that moves one without the other fails here instead of silently reopening the hole.
//
// The ids used here can no longer enter a catalog at all — skills.validate refuses an id that is
// not one token — but the guard is the second layer, and a defence in depth is only worth having if
// it is tested at its own level.
func TestNoSkillIDCanOutParseABuiltinVerb(t *testing.T) {
	for _, spec := range commandSpecs {
		t.Run(spec.name, func(t *testing.T) {
			id := spec.name + " off --save"

			verb, rest, ok := matchCommand("/" + id)
			if !ok || verb != spec.name {
				t.Fatalf("matchCommand(%q) = (%q, ok=%v), want the verb %q — the premise of the guard",
					"/"+id, verb, ok, spec.name)
			}
			if rest != "off --save" {
				t.Errorf("matchCommand(%q) tail = %q, want the arguments the verb would receive", "/"+id, rest)
			}

			m := Model{opts: Options{Skills: fakeSkillCatalog{skills: []skills.Skill{
				{ID: id, DisplayName: spec.name, Summary: "looks like a skill"},
			}}}}
			for _, it := range m.slashSuggestions("", "") {
				if it.skill {
					t.Errorf("the merged menu offered %q, which the parser reads as /%s", it.value, spec.name)
				}
			}
		})
	}
}

// TestCommandSpecsReadAlphabetically pins the dropdown's order at its source. The table IS the
// display order — commandSuggestions renders it as it stands, no render-time sort — so a verb added
// in the wrong place would quietly un-sort the menu; this fails loudly instead.
func TestCommandSpecsReadAlphabetically(t *testing.T) {
	names := make([]string, 0, len(commandSpecs))
	for _, spec := range commandSpecs {
		names = append(names, spec.name)
	}

	if !slices.IsSorted(names) {
		t.Errorf("commandSpecs order = %v, want alphabetical", names)
	}
}

// Drift guard on the recall carve-out: exactly the session-reset pair and the pure-UI panes
// (/settings, /usage, /inspect) are withheld from the walk. The flag is easy to copy onto a neighbouring row
// and impossible to notice once there — a verb that quietly stopped being recallable would look like
// recall losing lines — so the set is pinned by name rather than by count.
func TestOnlyResetAndPureUIVerbsAreNotRecallable(t *testing.T) {
	var got []string
	for _, spec := range commandSpecs {
		if spec.noRecall {
			got = append(got, spec.name)
		}
	}

	if want := []string{"clear", "inspect", "new", "settings", "usage"}; !reflect.DeepEqual(got, want) {
		t.Errorf("noRecall verbs = %v, want exactly %v — every other sent line stays recallable", got, want)
	}
}

// Drift guard on the Exchange gate: exactly /continue and /compact open an Exchange with the model,
// so exactly those two answer to an unreachable server the way a typed message does (runCommand
// reads the flag through parsedInput.opensExchange). It replaced a name comparison written in
// commandrun.go, where forgetting a verb failed nothing a human could see — the verb simply ran
// into a dead upstream — so the set is pinned by name here rather than by count.
func TestOnlyTheCannedTurnAndCompactionOpenAnExchange(t *testing.T) {
	var got []string
	for _, spec := range commandSpecs {
		if spec.opensExchange {
			got = append(got, spec.name)
		}
	}

	if want := []string{"compact", "continue"}; !reflect.DeepEqual(got, want) {
		t.Errorf("opensExchange verbs = %v, want exactly %v — every other verb is purely local", got, want)
	}
}

// Drift guard on the server set and on the latch that reads it. actuationBlocked used to carry a
// six-name list of its own, pinned by nothing: a verb added to the namespace and forgotten there
// ran straight into a server mid-restart and said nothing about it. Both halves are pinned — the
// flag's own set, and the six verbs the latch refuses because of the two flags together — because
// the second is the one a reader of the latch actually asks about.
func TestTheActuationLatchRefusesExactlyTheServerAndExchangeVerbs(t *testing.T) {
	var touching []string
	for _, spec := range commandSpecs {
		if spec.touchesServer {
			touching = append(touching, spec.name)
		}
	}
	if want := []string{"model", "server", "stop-server", "unload-model"}; !reflect.DeepEqual(touching, want) {
		t.Errorf("touchesServer verbs = %v, want exactly %v", touching, want)
	}

	var blocked []string
	for _, spec := range commandSpecs {
		if actuationBlocked(spec.name) {
			blocked = append(blocked, spec.name)
		}
	}
	want := []string{"compact", "continue", "model", "server", "stop-server", "unload-model"}
	if !reflect.DeepEqual(blocked, want) {
		t.Errorf("latch-blocked verbs = %v, want exactly %v — everything else stays live", blocked, want)
	}

	if actuationBlocked("no-such-verb") {
		t.Error("the latch blocked a name the table does not carry, want it live: the table is the authority")
	}
}

// Structural guard on the grammar hook. A verb whose arguments are richer than a token list
// declares its own parse on its own row (commandSpec.parseArgs) instead of in a second switch keyed
// by name, and three things follow. A hook only ever sees arguments the parser hands over, so a
// hook on a row that does not take arguments is dead code. Every grammar here words its BARE form
// as a report rather than an error, which is what lets verbArgsOf answer the zero value for a line
// the hook never ran for. And the set carrying one is named: it is exactly the switch that was
// deleted, so a further grammar is a deliberate edit at this line rather than a silent one.
func TestOnlyTheGrammarVerbsCarryAParseArgsHook(t *testing.T) {
	var got []string
	for _, spec := range commandSpecs {
		if spec.parseArgs == nil {
			continue
		}
		got = append(got, spec.name)

		if !spec.takesArgs {
			t.Errorf("/%s carries a parseArgs hook but not takesArgs — the parser would never call it", spec.name)
		}
		if _, err := spec.parseArgs(nil); err != nil {
			t.Errorf("/%s bare = %v, want no error — a bare grammar verb reports, it does not refuse", spec.name, err)
		}
	}

	if want := []string{"color-scheme", "confine", "skills", "undo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("verbs with a grammar of their own = %v, want exactly %v", got, want)
	}
}

// The two verbs that act on the session's server are ordinary rows: named for what they act on,
// /stop-server and /unload-model carry no ambiguity to guard against, so the dropdown offers them on
// the bare "/" and on every prefix of their own names — a verb the human cannot discover is a verb
// they will not find. The busy menu offers them too, tagged (the row below pins the tag itself).
func TestServerVerbsAreOffered(t *testing.T) {
	for _, tc := range []struct{ partial, want string }{
		{"", "stop-server"},
		{"", "unload-model"},
		{"s", "stop-server"},
		{"st", "stop-server"},
		{"stop", "stop-server"},
		{"stop-server", "stop-server"},
		{"u", "unload-model"},
		{"un", "unload-model"},
		{"unload", "unload-model"},
		{"unload-model", "unload-model"},
	} {
		for _, busy := range []bool{false, true} {
			var got []string
			for _, it := range commandSuggestions(tc.partial, busy, true) {
				got = append(got, it.value)
			}
			if !containsString(got, tc.want) {
				t.Errorf("commandSuggestions(%q, busy=%v) = %v, want /%s offered",
					tc.partial, busy, got, tc.want)
			}
		}
	}
}

// The merged "/" dropdown the human actually sees offers them as well — one registry feeds it, so a
// row the table declares reaches the menu whole.
func TestSlashMenuOffersTheServerVerbs(t *testing.T) {
	m := newTestModel(t)
	for _, tc := range []struct{ typed, want string }{
		{"/", "stop-server"},
		{"/", "unload-model"},
		{"/s", "stop-server"},
		{"/st", "stop-server"},
		{"/u", "unload-model"},
		{"/un", "unload-model"},
	} {
		m.input.SetValue(tc.typed)
		ac := m.computeAutocomplete(m.caretByteOffset())
		var got []string
		for _, it := range ac.items {
			got = append(got, it.value)
		}
		if !containsString(got, tc.want) {
			t.Errorf("typing %q offers %v, want /%s among them", tc.typed, got, tc.want)
		}
	}
}

// While a worker works the menu still offers every verb — hiding half the namespace mid-run is the
// ISSUES #12 symptom — but the rows that cannot run there say so. The tag follows commandSpecs'
// whileRunning column exactly, so a future verb flipping that flag needs no second edit here.
func TestCommandSuggestionsTagIdleOnlyRowsWhileBusy(t *testing.T) {
	rows := commandSuggestions("", true, true)
	if len(rows) != len(commandSpecs) {
		t.Fatalf("rows = %d, want every one of the %d verbs listed while busy", len(rows), len(commandSpecs))
	}
	for i, it := range rows {
		spec := commandSpecs[i]
		tagged := containsString(it.cells, idleOnlyTag)
		if tagged == spec.whileRunning {
			t.Errorf("row %q cells = %q; tagged = %v, want %v (whileRunning = %v)",
				spec.name, it.cells, tagged, !spec.whileRunning, spec.whileRunning)
		}
	}
}

// A model with no thinking-effort dial is not offered a way to set one: the menu DROPS /effort's row
// rather than greying it out, because absence is the whole signal (ADR 0060 D5). The gate follows
// commandSpecs' gatedByEffort column, so every ungated verb stays offered either way, and it reaches
// only as far as the dropdown — the registry and the parser are untouched below.
func TestCommandSuggestionsHideEffortWithoutADial(t *testing.T) {
	t.Parallel()

	for _, partial := range []string{"", "e", "eff", "effort"} {
		for _, supported := range []bool{false, true} {
			var got []string
			for _, it := range commandSuggestions(partial, false, supported) {
				got = append(got, it.value)
			}
			if offered := containsString(got, "effort"); offered != supported {
				t.Errorf("commandSuggestions(%q, effortSupported=%v) = %v; /effort offered = %v, want %v",
					partial, supported, got, offered, supported)
			}
		}
	}

	// The gate withholds the gated row and nothing else: a bare partial still lists every other verb
	// of the registry when the dial is absent.
	var offered []string
	for _, it := range commandSuggestions("", false, false) {
		offered = append(offered, it.value)
	}
	for _, spec := range commandSpecs {
		if spec.gatedByEffort {
			continue
		}
		if !containsString(offered, spec.name) {
			t.Errorf("/%s was withheld from the dial-less menu %v, want every ungated verb offered",
				spec.name, offered)
		}
	}
}

// The merged "/" menu the human actually sees reads the gate off the landed beat, so the row appears
// and disappears with the bound model rather than with a setting.
func TestSlashMenuHidesEffortWithoutADial(t *testing.T) {
	t.Parallel()

	for _, supported := range []bool{false, true} {
		m := newTestModel(t)
		m.hb.effort = provider.EffortSupport{Supported: supported, Dialect: provider.EffortDialectKwargs}

		var got []string
		for _, it := range m.slashSuggestions("eff", "") {
			got = append(got, it.value)
		}
		if offered := containsString(got, "effort"); offered != supported {
			t.Errorf("slashSuggestions(\"eff\") with supported=%v = %v; /effort offered = %v, want %v",
				supported, got, offered, supported)
		}
	}
}

// Hiding the row never un-routes the verb: a hand-typed /effort still parses as the command whatever
// the bound model reports, because parseInput reads the registry and knows nothing of the menu's
// gate. That is what lets the typed verb answer with its own note instead of being read as a message.
func TestTypedEffortStillParsesWithoutADial(t *testing.T) {
	t.Parallel()

	got := parseInput("/effort", nil)
	if got.kind != kindCommand || got.command != "effort" {
		t.Fatalf("parseInput(%q) = {kind:%v cmd:%q}, want the /effort command however the menu is gated",
			"/effort", got.kind, got.command)
	}
}

// safeWhileRunning is asked about the parsed LINE, not the verb: /confine reports under one form
// and mutates Auto's blast radius under the other, and only the report is boundary-free.
func TestSafeWhileRunningReadsTheLine(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"/version", true},
		{"/skills", true},
		{"/confine", true},
		{"/confine status", true},
		{"/confine bogus", true}, // a usage-line report, not a mutation (parseConfine's error path)
		{"/confine off", false},
		{"/confine off --save", false},
		{"/confine on", false},
		{"/clear", false},
		{"/new", false},
		{"/sessions", false},
		{"/rename", false}, // the bare form issues a completion of its own
		{"/rename my own name", false},
		{"/compact", false},
		{"/continue", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			parsed := parseInput(c.in, nil)
			if parsed.kind != kindCommand {
				t.Fatalf("parseInput(%q).kind = %v, want a command", c.in, parsed.kind)
			}
			if got := parsed.safeWhileRunning(); got != c.want {
				t.Errorf("safeWhileRunning(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestParseInputUnknownSlashIsMessage(t *testing.T) {
	// An unrecognised /verb is NOT a command — a message that merely CONTAINS a "/" word (a path,
	// a typo, an unparsed "/skill foo") is sent to the agent verbatim, never silently swallowed.
	// The sole-token case is the typo guard's, and has its own test below.
	for _, in := range []string{"/skill foo", "/unknown and more", "/usr/local/bin matters", "/"} {
		got := parseInput(in, nil)
		if got.kind != kindMessage {
			t.Errorf("parseInput(%q).kind = %v, want message", in, got.kind)
		}
	}
}

// ----------------------------------------------------------------------------
// The sole-token typo guard
// ----------------------------------------------------------------------------

// An input that is NOTHING BUT one "/word" naming no verb and no skill is its own kind: the router
// refuses it with a note instead of sending it, which is what stops a typo'd "/code-adit" — or, in
// the issue that spawned this, a "/skills" the build did not yet have — from reaching the model as
// prose. Everything else keeps its old classification exactly.
func TestParseInputSoleUnknownSlash(t *testing.T) {
	known := knownSkills("grill-me", "clear")
	cases := []struct {
		name string
		in   string
		want inputKind
	}{
		{"typo'd skill id", "/code-adit", kindUnknownSlash},
		{"typo'd command verb", "/comapct", kindUnknownSlash},
		{"whitespace around it still counts", "  /code-adit  ", kindUnknownSlash},
		{"the retired picker verb is an unknown word like any other", "/skill", kindUnknownSlash},
		{"a lone path names nothing either", "/usr/local/bin", kindUnknownSlash},
		{"a real command is a command", "/clear", kindCommand},
		{"a known skill token is a message", "/grill-me", kindMessage},
		{"more than the one token is a message", "/code-adit the parser", kindMessage},
		{"a second line makes it a message", "/code-adit\nplease", kindMessage},
		{"a bare slash is a message", "/", kindMessage},
		{"ordinary prose is a message", "fix /code-adit please", kindMessage},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseInput(c.in, known)
			if got.kind != c.want {
				t.Fatalf("parseInput(%q).kind = %v, want %v", c.in, got.kind, c.want)
			}
			if got.kind == kindUnknownSlash && got.text != strings.TrimSpace(c.in) {
				t.Errorf("guarded text = %q, want the token as typed %q", got.text, strings.TrimSpace(c.in))
			}
		})
	}

	// With no catalog wired nothing resolves, so the guard still only claims the sole-token form.
	if got := parseInput("/grill-me", nil); got.kind != kindUnknownSlash {
		t.Errorf("parseInput(/grill-me, nil).kind = %v, want the guard (no catalog ⇒ nothing resolves)", got.kind)
	}
}

// The note names the word that failed to resolve — a typo is fixed by seeing it. The retired
// /skill picker verb has no special case left: a sole "/skill" earns the same generic refusal as
// any other word naming nothing.
func TestUnknownSlashNote(t *testing.T) {
	for _, token := range []string{"/code-adit", "/skill"} {
		got := unknownSlashNote(token)
		if !strings.Contains(got, token) || !strings.Contains(got, "unknown command or skill") {
			t.Errorf("unknownSlashNote(%s) = %q, want it to name the token as unknown", token, got)
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

// ----------------------------------------------------------------------------
// Inline "/id" skill tokens — the second half of the mini-language
// ----------------------------------------------------------------------------

// knownSkills builds the catalog predicate parseInput resolves inline "/token"s against, from a
// literal set of ids — the pure-layer stand-in for Model.knownSkillID.
func knownSkills(ids ...string) func(string) bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return func(id string) bool { return set[id] }
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
			if confine := verbArgsOf[confineArgs](got); confine != c.want {
				t.Errorf("parseInput(%q).confine = %+v, want %+v", c.in, confine, c.want)
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
			if confine := verbArgsOf[confineArgs](got); confine != (confineArgs{}) {
				t.Errorf("parseInput(%q).confine = %+v, want the zero value on an error", c.in, confine)
			}
		})
	}
}

// The /color-scheme grammar has three accepting branches and one refusing one, and the split
// between the last two is the whole of what the parser decides: one token is a NAME (any name — a
// scheme this build cannot find is a forgiving load, not a parse error), two tokens are only ever
// "export <name>".
func TestParseInputColorSchemeArguments(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want colorSchemeArgs
	}{
		{"bare lists", "/color-scheme", colorSchemeArgs{action: colorSchemeList}},
		{"a name switches", "/color-scheme light", colorSchemeArgs{action: colorSchemeSwitch, name: "light"}},
		{"an unknown name still parses", "/color-scheme solarized",
			colorSchemeArgs{action: colorSchemeSwitch, name: "solarized"}},
		{"export takes a name", "/color-scheme export dark",
			colorSchemeArgs{action: colorSchemeExport, name: "dark"}},
		{"whitespace tolerated", "  /color-scheme   export   dark  ",
			colorSchemeArgs{action: colorSchemeExport, name: "dark"}},
		{"tab separated", "/color-scheme\tlight", colorSchemeArgs{action: colorSchemeSwitch, name: "light"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseInput(c.in, nil)
			if got.kind != kindCommand || got.command != "color-scheme" {
				t.Fatalf("parseInput(%q) = {kind:%v cmd:%q}, want the color-scheme command", c.in, got.kind, got.command)
			}
			if got.err != nil {
				t.Fatalf("parseInput(%q).err = %v, want nil", c.in, got.err)
			}
			if scheme := verbArgsOf[colorSchemeArgs](got); scheme != c.want {
				t.Errorf("parseInput(%q).colorScheme = %+v, want %+v", c.in, scheme, c.want)
			}
		})
	}
}

func TestParseInputColorSchemeArgumentErrors(t *testing.T) {
	// The refusing branch, and the reason it exists: a line the parser cannot read must never be
	// guessed into a switch, because a wrong guess repaints the screen and writes the config.
	cases := []struct {
		name string
		in   string
	}{
		{"export with no name", "/color-scheme export"},
		{"export with two names", "/color-scheme export dark light"},
		{"a name with a stray token", "/color-scheme light please"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseInput(c.in, nil)
			if got.kind != kindCommand || got.command != "color-scheme" {
				t.Fatalf("parseInput(%q) = {kind:%v cmd:%q}, want the color-scheme command", c.in, got.kind, got.command)
			}
			if got.err == nil {
				t.Fatalf("parseInput(%q).err = nil, want an argument error", c.in)
			}
			if !strings.Contains(got.err.Error(), colorSchemeUsage) {
				t.Errorf("parseInput(%q).err = %q, want it to carry %q", c.in, got.err, colorSchemeUsage)
			}
			if scheme := verbArgsOf[colorSchemeArgs](got); scheme != (colorSchemeArgs{}) {
				t.Errorf("parseInput(%q).colorScheme = %+v, want the zero value on an error", c.in, scheme)
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
