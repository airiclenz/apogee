package scheme

import (
	"embed"
	"fmt"
	"reflect"
	"regexp"
	"sync"

	"gopkg.in/yaml.v3"
)

// DefaultName is the built-in scheme every fallback lands on: the palette apogee has
// always drawn with, apart from three roles retuned for legibility while the scheme
// system was being built (`code`, `tool-header` and `tool-marker`).
const DefaultName = "dark"

// builtinFS carries the shipped schemes. They live in the binary, never on disk —
// nothing installs them at boot and nothing fetches them from a network (ADR 0040).
//
//go:embed schemes/*.yaml
var builtinFS embed.FS

// Scheme is one color per semantic role — the whole vocabulary a scheme file speaks.
// Values are "#rrggbb" hex strings; the yaml tags are the file's keys, and this
// declaration is the single definition of both (roleKeys derives from it).
type Scheme struct {
	UserText    string `yaml:"user-text"`    // user-prompt text
	Chrome      string `yaml:"chrome"`       // user-block background + input/footer borders
	Divider     string `yaml:"divider"`      // top-edge divider hairline
	Surface     string `yaml:"surface"`      // input-box interior
	Muted       string `yaml:"muted"`        // status/footer/tool-detail dim
	MutedBright string `yaml:"muted-bright"` // the muted tone's open step: an EXPANDED tool block's text

	DiffAdd string `yaml:"diff-add"` // diff "+" lines
	DiffDel string `yaml:"diff-del"` // diff "-" lines
	// DiffAddBg and DiffDelBg are the background band a diff BODY line carries. The
	// added/removed signal rides that band so the text itself can wear the plain detail tone of
	// its block state, like every other detail line, and so the band can run the full width of
	// the row rather than stopping where the text does. They do not replace the foreground pair
	// above: `diff-add` / `diff-del` keep the markers, the stat summaries and every other use of
	// diff colour outside a diff body line. A scheme keeps both bands quiet and in the same hue
	// families as that pair, so the turquoise-vs-red pairing still survives red-green-weak vision.
	DiffAddBg string `yaml:"diff-add-bg"` // the band an added diff body line sits on
	DiffDelBg string `yaml:"diff-del-bg"` // the band a removed diff body line sits on
	Error     string `yaml:"error"`       // recovered-fault notices
	// Success is Error's counterpart at the other end of an outcome: the tone a marker wears
	// to say a thing came off. Its first consumer is the ✓ on a finished sub-agent
	// (docs/layout/tool-layout.md), but the role is named for the meaning rather than that
	// glyph, so anything else that ends well can speak in the same voice.
	Success string `yaml:"success"` // the ✓ and other "this came off" markers
	// Warning is the rung between `muted` and `error`: a condition apogee wants noticed but
	// has not called a fault. Its first consumer is the status line's quiet qualifier —
	// rendered before the activity clock, the honest report that no engine event has arrived
	// for a while — but the role is named for the meaning rather than that qualifier, so
	// anything else that qualifies rather than fails can speak in the same voice. A scheme
	// keeps it amber-ish: read past `muted`, short of the alarm `error` carries.
	Warning string `yaml:"warning"` // "worth noticing, not a fault" notices
	Code    string `yaml:"code"`    // inline `code` + fenced code blocks
	// ToolHeader is the tool label's own tone, kept apart from Code so a scheme can
	// pitch the block headers against the code it prints without moving both at once.
	ToolHeader string `yaml:"tool-header"` // tool-call block headers + the sub-agent rail

	ModePlan       string `yaml:"mode-plan"`        // autonomy mode: plan
	ModeAskBefore  string `yaml:"mode-ask-before"`  // autonomy mode: ask-before
	ModeAllowEdits string `yaml:"mode-allow-edits"` // autonomy mode: allow-edits
	ModeAuto       string `yaml:"mode-auto"`        // autonomy mode: auto

	Skill   string `yaml:"skill"`    // the prompt's inline /skill token accent
	FileRef string `yaml:"file-ref"` // the prompt's inline @file token accent

	PromptToggle string `yaml:"prompt-toggle"` // the collapsed prompt's see-more / see-less marker
	ToolMarker   string `yaml:"tool-marker"`   // a tool block's "+N more lines" marker + its outcome slot
	// ToolMarkerBright is ToolMarker's open step, the pair `muted` / `muted-bright` already
	// models: the same voice at a higher volume, so an opened tool block's marker tone reads a
	// step out of the collapsed ones around it. A stated role rather than a color computed at
	// render time, because every color on screen is a role a scheme can name (ADR 0040).
	//
	// Its consumer is the right-aligned outcome slot, which wears the marker role in both states
	// (docs/layout/tool-layout.md): the slot is apogee's reading of what a call came to rather than
	// a line the tool printed, so it speaks in the marker's voice and not the body's detail gray.
	ToolMarkerBright string `yaml:"tool-marker-bright"` // the marker role's EXPANDED-block step
	// ToolLeader is the dotted run carrying the eye from a tool row's target to the outcome slot
	// right-aligned at the row's edge (docs/layout/tool-layout.md). It ships seeded from `muted` —
	// the tone the ▶/▼ indicator beside it already wears — so the leader reads as the same chrome
	// until a scheme damps it further; splitting it off is what makes that possible at all.
	ToolLeader string `yaml:"tool-leader"` // the ⋯ leader between a tool row's target and its outcome

	Gauge     string `yaml:"gauge"`     // context-fill gauge bar
	Selection string `yaml:"selection"` // mouse drag-selection highlight background

	Spinner1 string `yaml:"spinner-1"` // status-line spinner blend stop 1
	Spinner2 string `yaml:"spinner-2"` // status-line spinner blend stop 2
	Spinner3 string `yaml:"spinner-3"` // status-line spinner blend stop 3
	Spinner4 string `yaml:"spinner-4"` // status-line spinner blend stop 4
}

// roleKeys lists every role's YAML key in declaration order and fieldIndex maps each
// back to the Scheme field carrying it. Both are read off the struct tags rather than
// restated, so adding a role to Scheme is the only edit a new role needs.
var roleKeys, fieldIndex = buildRoles()

func buildRoles() ([]string, map[string]int) {
	t := reflect.TypeOf(Scheme{})
	keys := make([]string, 0, t.NumField())
	index := make(map[string]int, t.NumField())
	for i := range t.NumField() {
		key := t.Field(i).Tag.Get("yaml")
		keys = append(keys, key)
		index[key] = i
	}
	return keys, index
}

// hexColor is the only value shape a role accepts: "#rrggbb". Anything else is a
// warning, not a failure.
var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// setRole writes value into the field the key names. The caller has already confirmed
// the key is known.
func (s *Scheme) setRole(key, value string) {
	reflect.ValueOf(s).Elem().Field(fieldIndex[key]).SetString(value)
}

// Warning is one thing a scheme file got wrong. Key is empty when the whole file is at
// fault rather than a single role.
type Warning struct {
	File   string
	Key    string
	Reason string
}

// String renders the warning as the one line a user sees, e.g.
// `color-scheme "dark.yaml": key "error": bad hex "#zz0000" — using default`.
func (w Warning) String() string {
	if w.Key == "" {
		return fmt.Sprintf("color-scheme %q: %s", w.File, w.Reason)
	}
	return fmt.Sprintf("color-scheme %q: key %q: %s", w.File, w.Key, w.Reason)
}

// defaultScheme parses the embedded default once. Its warnings are deliberately
// dropped: a defect in a file compiled into the binary is a build defect, and
// TestEmbeddedDarkIsTheCompleteDefault fails long before a user could meet it.
var defaultScheme = sync.OnceValue(func() Scheme {
	data, ok := builtinBytes(DefaultName)
	if !ok {
		return Scheme{}
	}
	s, _ := parseInto(Scheme{}, DefaultName+".yaml", data)
	return s
})

// Default returns the embedded "dark" scheme — the palette every unset role falls back
// to and the scheme apogee runs with when no other one resolves.
func Default() Scheme { return defaultScheme() }

// builtinBytes returns the embedded YAML of a shipped scheme, verbatim (comments
// included — an exported copy is meant to be edited).
func builtinBytes(name string) ([]byte, bool) {
	data, err := builtinFS.ReadFile("schemes/" + name + ".yaml")
	if err != nil {
		return nil, false
	}
	return data, true
}

// Parse decodes a scheme file over the defaults. It never fails: an unknown key is
// skipped, a value that is not "#rrggbb" keeps its default, a file that will not decode
// at all yields the default scheme — each with a Warning naming file and key. A key the
// file simply omits is silent, because partial files are the intended way to write one.
// name is the label warnings carry, normally the file's base name.
func Parse(name string, data []byte) (Scheme, []Warning) {
	return parseInto(Default(), name, data)
}

// parseInto is Parse with an explicit fallback, so Default can build itself from the
// embedded YAML without recursing through Parse.
func parseInto(base Scheme, name string, data []byte) (Scheme, []Warning) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return base, []Warning{{File: name, Reason: fmt.Sprintf("unreadable YAML (%v) — using the default scheme", err)}}
	}
	if len(doc.Content) == 0 {
		return base, nil // an empty file omits every role, and omission is silent
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return base, []Warning{{File: name, Reason: "not a mapping of color roles — using the default scheme"}}
	}

	out := base
	var warnings []Warning
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		_, known := fieldIndex[key.Value]
		switch {
		case !known:
			warnings = append(warnings, Warning{File: name, Key: key.Value, Reason: "unknown color role — ignored"})
		case value.Kind != yaml.ScalarNode:
			warnings = append(warnings, Warning{File: name, Key: key.Value, Reason: "expected a #rrggbb color — using default"})
		case !hexColor.MatchString(value.Value):
			warnings = append(warnings, Warning{File: name, Key: key.Value, Reason: fmt.Sprintf("bad hex %q — using default", value.Value)})
		default:
			out.setRole(key.Value, value.Value)
		}
	}
	return out, warnings
}
