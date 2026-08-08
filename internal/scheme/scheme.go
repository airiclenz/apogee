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
	Error   string `yaml:"error"`    // recovered-fault notices
	Code    string `yaml:"code"`     // inline `code` + fenced code blocks
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
	ToolMarker   string `yaml:"tool-marker"`   // a tool block's "+N more lines" remainder marker

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
// TestEmbeddedDarkMatchesPinnedPalette fails long before a user could meet it.
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
