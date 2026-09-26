package tools

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// payloadSpellings are the argument-key spellings the dangerous-action guard treats as
// payload text (internal/security payloadKeys), reduced the way that guard reduces a key:
// domain.FoldArgumentKey, then `_` and `-` removed. A built-in tool whose schema carries a
// key with one of these spellings must declare that key with domain.ArgRolePayload itself,
// so the payload exclusion can rest on the tool's own declaration rather than on a key name
// any MCP tool might share.
var payloadSpellings = map[string]bool{
	"content":    true,
	"newcontent": true,
	"oldtext":    true,
	"newtext":    true,
	"pattern":    true,
	"message":    true,
	"query":      true,
	"question":   true,
	"choices":    true,
	"title":      true,
	"body":       true,
}

// argKeySpelling reduces key to the spelling payloadSpellings is keyed by.
func argKeySpelling(key string) string {
	return strings.NewReplacer("_", "", "-", "").Replace(domain.FoldArgumentKey(key))
}

// schemaProperties decodes the top-level property names of tool's JSON schema.
func schemaProperties(t *testing.T, tool domain.Tool) []string {
	t.Helper()
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatalf("%s: schema does not decode: %v", tool.Name(), err)
	}
	keys := make([]string, 0, len(schema.Properties))
	for key := range schema.Properties {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// TestBuiltinToolsDeclareEveryPayloadSpelledArgRole walks every tool this build carries —
// default-off and host-delegate tools included — and fails when a top-level schema key spelled
// like a payload key is not declared with domain.ArgRolePayload by the tool that carries it.
func TestBuiltinToolsDeclareEveryPayloadSpelledArgRole(t *testing.T) {
	checked := 0
	for _, tool := range builtinTools(t.TempDir(), HostTools{}) {
		declared := domain.ArgKeysWithRole(tool, domain.ArgRolePayload)
		for _, key := range schemaProperties(t, tool) {
			if !payloadSpellings[argKeySpelling(key)] {
				continue
			}
			checked++
			if !slices.Contains(declared, key) {
				t.Errorf("%s: schema key %q is spelled like a payload key but is not declared with domain.ArgRolePayload (declared payload keys: %v)",
					tool.Name(), key, declared)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no built-in schema key is spelled like a payload key — the walk checked nothing")
	}
}

// TestBuiltinToolArgRolesNameSchemaKeys fails when a built-in tool declares a role for a key
// its own schema does not carry: a misspelled declaration (`newcontent` for `newContent`)
// would silently narrow nothing.
func TestBuiltinToolArgRolesNameSchemaKeys(t *testing.T) {
	for _, tool := range builtinTools(t.TempDir(), HostTools{}) {
		declaring, ok := tool.(domain.ArgRoleTool)
		if !ok {
			continue
		}
		properties := schemaProperties(t, tool)
		for key, role := range declaring.ArgRoles() {
			if !slices.Contains(properties, key) {
				t.Errorf("%s: declares %q with role %q, but its schema has no such key (keys: %v)",
					tool.Name(), key, role, properties)
			}
		}
	}
}

// TestMultiFindReplaceDeclaresReplacementsArgRoleAsPayload pins the one declaration the
// top-level walk cannot see the reason for: multi_find_and_replace's `oldText`/`newText` sit
// inside the `replacements` array, so the top-level `replacements` key is the declaration that
// reaches them.
func TestMultiFindReplaceDeclaresReplacementsArgRoleAsPayload(t *testing.T) {
	got := domain.ArgKeysWithRole(NewMultiFindReplace(t.TempDir()), domain.ArgRolePayload)
	if want := []string{"replacements"}; !slices.Equal(got, want) {
		t.Fatalf("multi_find_and_replace payload keys = %v, want %v", got, want)
	}
}
