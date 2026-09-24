package config

import (
	"strings"
	"testing"
)

// The descriptor's row set: every KindBool row carries a field except `context-files.enable`,
// whose value derives from the resolved list and owns no field — and a field row has its file
// projection derived, so resolution reads it without a hand-written fromFile.
func TestFieldCarriedByEveryBoolRow(t *testing.T) {
	t.Parallel()

	for _, row := range KeyRegistry {
		wantField := row.Kind == KindBool && row.Path != "context-files.enable"
		if (row.field != nil) != wantField {
			t.Errorf("registry row %q (kind %q) carries a field = %v, want %v", row.Path, row.Kind,
				row.field != nil, wantField)
		}
		if row.field != nil && (row.fromFile == nil || row.Read == nil || row.Set == nil) {
			t.Errorf("registry row %q carries a field but bindRows derived no Read, Set or fromFile", row.Path)
		}
	}
	for _, k := range handWrittenAccessors {
		if k.row.field != nil && k.fromFile != nil {
			t.Errorf("hand-written accessor %q carries a fromFile its field already derives", k.row.Path)
		}
	}
}

// The init guard over the two tables: a field row whose hand-written entry carries a fromFile, a
// row with neither, and a hand-written entry naming no row all panic — while a field row that
// keeps hand-written env and flag plumbing (`bypass`, until those derive too) does not.
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
			wantPanic: "derives its file value from its field",
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
			name: "field row keeping its env and flag plumbing",
			rows: []Key{mustKey("bypass")},
			written: []keyAccessor{{
				row:      mustKey("bypass"),
				fromEnv:  setThroughRow("bypass"),
				fromFlag: func(o *Options, flags Options) { o.Bypass = flags.Bypass },
			}},
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
					t.Fatalf("accessorsOver = %+v, want one entry with the derived fromFile and the "+
						"hand-written fromEnv and fromFlag", built)
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
