package config

import (
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// The descriptor's row set: every row whose value is one scalar carries a field — every bool,
// count, share, duration, name and word — except the four that resolve with something no field
// holds: `context-files.enable` (derived from the resolved list), `sub-agents-server` (whose Read
// shows a word for the empty value) and the two system-prompt keys (one carrier with their block).
// A field row has its file projection derived, and needs no hand-written accessor at all.
func TestFieldCarriedByEveryScalarRow(t *testing.T) {
	t.Parallel()

	fieldless := map[string]bool{
		"context-files.enable": true, "sub-agents-server": true,
		"system-prompt-text": true, "system-prompt-file": true,
	}
	for _, row := range KeyRegistry {
		scalar := row.Kind != KindStructured && row.Kind != KindStringList
		wantField := scalar && !fieldless[row.Path]
		if (row.field != nil) != wantField {
			t.Errorf("registry row %q (kind %q) carries a field = %v, want %v", row.Path, row.Kind,
				row.field != nil, wantField)
		}
		if row.field != nil && (row.fromFile == nil || row.Read == nil || row.Set == nil) {
			t.Errorf("registry row %q carries a field but bindRows derived no Read, Set or fromFile", row.Path)
		}
	}
	for _, k := range handWrittenAccessors {
		if k.row.field != nil {
			t.Errorf("hand-written accessor %q restates plumbing its field already derives", k.row.Path)
		}
	}
}

// The init guard over the two tables: a field row with a hand-written entry of any kind, a row with
// neither, and a hand-written entry naming no row all panic — while a field row with a variable and
// a flag (`bypass`) gets its env and flag passes derived from the field.
func TestFieldAccessorsOverRefuseTwoFileProjections(t *testing.T) {
	t.Parallel()

	stateless := func(*Options, fileConfig) error { return nil }
	tests := []struct {
		name      string
		rows      []Key
		written   []keyAccessor
		wantPanic string
	}{
		{
			name:      "field row with a hand-written fromFile",
			rows:      []Key{mustKey("auto-compact")},
			written:   []keyAccessor{{row: mustKey("auto-compact"), fromFile: stateless}},
			wantPanic: "derives its file, env and flag projections from its field",
		},
		{
			name: "field row with hand-written env and flag plumbing",
			rows: []Key{mustKey("bypass")},
			written: []keyAccessor{{
				row:      mustKey("bypass"),
				fromFlag: func(o *Options, flags Options) { o.Bypass = flags.Bypass },
			}},
			wantPanic: "derives its file, env and flag projections from its field",
		},
		{
			name:      "row with neither a field nor a fromFile",
			rows:      []Key{mustKey("servers")},
			wantPanic: "neither a field nor a hand-written fromFile",
		},
		{
			name:      "hand-written entry naming no row",
			rows:      []Key{mustKey("auto-compact")},
			written:   []keyAccessor{{row: mustKey("servers"), fromFile: stateless}},
			wantPanic: "servers",
		},
		{
			name: "field row with a variable and a flag",
			rows: []Key{mustKey("bypass")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var built []keyAccessor
			recovered := func() (r any) {
				defer func() { r = recover() }()
				built = accessorsOver(tt.rows, tt.written)
				return nil
			}()
			if tt.wantPanic == "" {
				if recovered != nil {
					t.Fatalf("accessorsOver panicked: %v", recovered)
				}
				if len(built) != 1 || built[0].fromFile == nil || built[0].fromEnv == nil || built[0].fromFlag == nil {
					t.Fatalf("accessorsOver = %+v, want one entry with the derived fromFile, fromEnv and "+
						"fromFlag", built)
				}
				return
			}
			msg, _ := recovered.(string)
			if !strings.Contains(msg, tt.wantPanic) {
				t.Errorf("accessorsOver panic = %v, want one naming %q", recovered, tt.wantPanic)
			}
		})
	}
}

// A bool of a block lands its file value as a typed copy, never through the row's Set: a file whose
// `ui:` block names a spinner the block validator refuses, or whose `present:` block carries a port
// out of range, still loads — those refusals are ResolveOptions', as they were before the field.
func TestFieldBlockBoolFileValueIsATypedCopy(t *testing.T) {
	t.Parallel()

	file := "ui:\n  spinner: twirl\n  spinner-color: false\npresent:\n  port: 70000\n  auto-open: false\n" +
		"  command-on-model-documents: true\n"
	o, err := LoadFileConfig("config.yaml", func(string) ([]byte, error) { return []byte(file), nil }, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig refused a file it loaded before the field: %v", err)
	}
	if o.UI.SpinnerColor || string(o.UI.Spinner) != "twirl" {
		t.Errorf("ui = %+v, want spinner-color false beside the spinner as written", o.UI)
	}
	if o.Present.AutoOpen || !o.Present.CommandOnModelDocuments || o.Present.Port != 70000 {
		t.Errorf("present = %+v, want auto-open false and command-on-model-documents true beside the "+
			"port as written", o.Present)
	}
}

// A `present:` bool's Set still re-runs the block's validator over the edited copy (landIn): with
// the block's port out of range, the edit is refused and the block is left as it was.
func TestFieldPresentBoolSetKeepsTheBlockValidator(t *testing.T) {
	t.Parallel()

	for path, value := range map[string]string{
		"present.auto-open": "false", "present.command-on-model-documents": "true",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			var o Options
			mustApplyFile(t, &o, fileConfig{})
			o.Present.Port = 70000
			before := o.Present
			if err := mustKey(path).Set(value, &o); err == nil {
				t.Errorf("Set(%q) with present.port 70000 was accepted; the block validator must refuse it", value)
			}
			if o.Present != before {
				t.Errorf("a refused Set moved the block: %+v, want %+v", o.Present, before)
			}
		})
	}
}

// The env pass lands a variable through the row's bound Set — refused in the row's sentence behind
// the variable's lead — and the flag pass copies the parsed flag unjudged: `--mode fast` resolves to
// the word as typed, for the Driver's own parse to refuse after resolution, as it always has.
func TestFieldEnvLandsThroughSetFlagCopiesUnjudged(t *testing.T) {
	t.Parallel()

	var o Options
	mustApplyFile(t, &o, fileConfig{})
	err := applyEnv(&o, func(name string) string {
		if name == EnvMode {
			return "fast"
		}
		return ""
	})
	if err == nil || !strings.HasPrefix(err.Error(), `apogee: invalid APOGEE_MODE "fast": `) {
		t.Errorf("applyEnv(APOGEE_MODE=fast) = %v, want the refusal behind the variable's lead", err)
	}

	applyFlags(&o, Options{Mode: "fast", StartupServer: "gpu"}, func(name string) bool {
		return name == "mode" || name == "server"
	})
	if o.Mode != "fast" || o.StartupServer != "gpu" {
		t.Errorf("flag pass = mode %q, server %q; want both copied as parsed", o.Mode, o.StartupServer)
	}
}

// A file mode outside the ladder is a typed copy, not a refusal: under `--mode plan` or
// APOGEE_MODE=plan the run still resolves, to the source above the file.
func TestFieldFileModeUnderAnOverrideResolves(t *testing.T) {
	t.Parallel()

	readFile := func(string) ([]byte, error) { return []byte("mode: fast\n"), nil }
	tests := []struct {
		name    string
		flags   Options
		changed []string
		env     map[string]string
	}{
		{name: "flag", flags: Options{Mode: "plan"}, changed: []string{"mode"}},
		{name: "env", env: map[string]string{EnvMode: "plan"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := tt.flags
			changed := func(name string) bool { return slices.Contains(tt.changed, name) }
			getenv := func(name string) string { return tt.env[name] }
			if _, err := ResolveOptions(&opts, changed, getenv, readFile, noNotify, testHostID); err != nil {
				t.Fatalf("ResolveOptions over `mode: fast` with a %s override: %v", tt.name, err)
			}
			if opts.Mode != "plan" {
				t.Errorf("mode = %q, want the %s's plan", opts.Mode, tt.name)
			}
		})
	}
}

// The scalar keys of a block are typed copies at the load: a bad spinner, an out-of-range port, a
// negative count, an unparseable age, an unknown scheme and a bad search endpoint all LOAD, and the
// block refusals stay ResolveOptions'. The checked-text keys still refuse at the load.
func TestFieldLoadFileConfigKeepsItsRefusals(t *testing.T) {
	t.Parallel()

	load := func(file string) (Options, error) {
		return LoadFileConfig("config.yaml", func(string) ([]byte, error) { return []byte(file), nil }, noNotify)
	}
	for _, file := range []string{
		"ui:\n  spinner: twirl\n",
		"present:\n  port: 70000\n",
		"sessions:\n  max-age: forever\n",
		"sessions:\n  max-count: -1\n",
		"ui:\n  color-scheme: no-such-scheme\n",
		"ui:\n  tools-fold-over: -2\n",
		"web-search-endpoint: not a url\n",
		"mode: fast\n",
	} {
		if _, err := load(file); err != nil {
			t.Errorf("LoadFileConfig(%q) refused a file it loaded before the field: %v", file, err)
		}
	}
	for _, file := range []string{
		"ui:\n  spinner: twirl\n",
		"present:\n  port: 70000\n",
		"sessions:\n  max-age: forever\n",
	} {
		opts := Options{}
		_, err := ResolveOptions(&opts, func(string) bool { return false }, func(string) string { return "" },
			func(string) ([]byte, error) { return []byte(file), nil }, noNotify, testHostID)
		if err == nil {
			t.Errorf("ResolveOptions(%q) accepted a block its validator refuses", file)
		}
	}
	for _, file := range []string{
		"delegate-timeout: soon\n",
		"stream-idle-timeout: soon\n",
		"re-stream-budget: -1\n",
		"sub-agents-choice: anyone\n",
		"cursor-shape: triangle\n",
		"ui:\n  stall-after: soon\n",
	} {
		if _, err := load(file); err == nil {
			t.Errorf("LoadFileConfig(%q) loaded a value its row refuses at the file pass", file)
		}
	}
}

// With every scalar of a block landing on its own, an empty file still resolves each block to the
// defaults its own type declares — the value the block mappers used to build from nothing.
func TestFieldEmptyFileResolvesBlockDefaults(t *testing.T) {
	t.Parallel()

	var o Options
	mustApplyFile(t, &o, fileConfig{})
	if o.UI != domain.DefaultUIPrefs() {
		t.Errorf("ui = %+v, want domain.DefaultUIPrefs() %+v", o.UI, domain.DefaultUIPrefs())
	}
	if want := (PresentSettings{AutoOpen: true}); o.Present != want {
		t.Errorf("present = %+v, want %+v", o.Present, want)
	}
	if want := defaultSessionSettings(); o.Sessions != want {
		t.Errorf("sessions = %+v, want %+v", o.Sessions, want)
	}
	if o.CursorShape != "" {
		t.Errorf("cursor-shape = %q, want the empty name that asks for the renderer's default", o.CursorShape)
	}
}

// A re-read clears what the last one carried: an unparseable `sessions.max-age` from one pass does
// not survive a pass over a file that no longer states it.
func TestFieldSessionsMaxAgeResetsOnARepass(t *testing.T) {
	t.Parallel()

	bad := "forever"
	var o Options
	mustApplyFile(t, &o, fileConfig{Sessions: &sessionsConfig{MaxAge: &bad}})
	if o.Sessions.Validate() == nil {
		t.Fatalf("sessions after `max-age: forever` = %+v, want the text carried for Validate", o.Sessions)
	}
	mustApplyFile(t, &o, fileConfig{})
	if err := o.Sessions.Validate(); err != nil {
		t.Errorf("a re-pass over an empty file kept the old text: %v", err)
	}
}
