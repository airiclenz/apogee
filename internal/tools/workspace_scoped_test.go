package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestMarkerAccessors_NonMarkerTool proves the marker accessors report a non-marker tool
// as NOT a workspace-scoped writer: IsWorkspaceScopedWriter is false and
// WorkspaceWriteTarget yields ("", false). read_file is read-only and structurally does
// not satisfy the unexported workspaceScopedWriter marker, so dispatch must not treat it
// as an Apogee-own path-safety-bounded write.
func TestMarkerAccessors_NonMarkerTool(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ro := NewReadFile(root) // read-only; carries no marker

	if IsWorkspaceScopedWriter(ro) {
		t.Error("IsWorkspaceScopedWriter(read_file) = true, want false (read-only tool is not a workspace-scoped writer)")
	}

	call := domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"file.txt"}`)}
	abs, ok := WorkspaceWriteTarget(ro, call)
	if ok || abs != "" {
		t.Errorf("WorkspaceWriteTarget(read_file) = (%q, %v), want (\"\", false) for a non-marker tool", abs, ok)
	}
}

// TestMarkerAccessors_MarkerTool is the positive contrast: write_file DOES carry the
// marker, so IsWorkspaceScopedWriter is true and WorkspaceWriteTarget resolves the call's
// target path. This guards against a regression where the accessors stop recognising a
// genuine marker carrier (which would wrongly route an in-workspace write through gating).
// Its hand-written {"path":…} call is the deliberate exception to the rule the tests below
// follow: it is the one place that pins pathArgWriteTarget's decode key to the spelling
// pathArgKey documents, so that constant cannot silently drift away from the fence.
func TestMarkerAccessors_MarkerTool(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	w := NewWriteFile(root) // carries the workspaceScopedWriter marker

	if !IsWorkspaceScopedWriter(w) {
		t.Fatal("IsWorkspaceScopedWriter(write_file) = false, want true (write_file is an Apogee-own workspace-scoped writer)")
	}

	call := domain.ToolCall{ID: "c1", Tool: "write_file", Arguments: []byte(`{"path":"file.txt","content":"x"}`)}
	abs, ok := WorkspaceWriteTarget(w, call)
	if !ok || abs == "" {
		t.Errorf("WorkspaceWriteTarget(write_file) = (%q, %v), want a resolved in-workspace target", abs, ok)
	}
}

// pathArgKey is the json name pathArgWriteTarget decodes — the argument spelling the
// blast-radius fence can see for a writer that names ONE file. TestMarkerAccessors_MarkerTool's
// hand-written call pins it to the fence's real behaviour; the tests below pin every such write
// tool's own surface to it.
const pathArgKey = "path"

// destinationArgKey is pathArgKey's twin for the two-path writers, whose write lands at the
// destination — the key destinationArgWriteTarget decodes.
const destinationArgKey = "destination"

// writeTargetProbe pairs a workspace-scoped writer with a zero value of the args struct
// its OWN Execute decodes, and with the json key its workspaceWriteTarget resolves the write
// through. Every call these tests feed the fence is marshalled from that
// struct, so the json key that reaches the fence is whatever tag the tool itself
// declares — never a literal written in this file. That is what makes the shared decode
// guarded rather than merely asserted: rename one writer's json:"path" tag and its calls
// stop naming a key the fence reads, which fails here instead of silently classifying an
// out-of-workspace write as in-bounds.
type writeTargetProbe struct {
	tool domain.Tool
	args any
	// targetKey is the argument the tool's own workspaceWriteTarget decodes: pathArgKey for the
	// writers that name one file, destinationArgKey for the copy/move pair (their source is
	// fenced at operation time, never classified — destinationArgWriteTarget says why).
	targetKey string
}

// writeTargetProbes returns one probe per workspace-scoped writer, each scoped to root.
// TestWriteTargetProbesCoverEveryWriter keeps the list complete.
func writeTargetProbes(root string) []writeTargetProbe {
	return []writeTargetProbe{
		{tool: NewWriteFile(root), args: writeFileArgs{}, targetKey: pathArgKey},
		{tool: NewSingleFindReplace(root), args: singleFindReplaceArgs{}, targetKey: pathArgKey},
		{tool: NewMultiFindReplace(root), args: multiFindReplaceArgs{}, targetKey: pathArgKey},
		{tool: NewEditExistingFile(root), args: fileEditArgs{}, targetKey: pathArgKey},
		{tool: NewCopyFile(root), args: fileOpsArgs{}, targetKey: destinationArgKey},
		{tool: NewMoveFile(root), args: fileOpsArgs{}, targetKey: destinationArgKey},
	}
}

// TestWriteTargetProbesCoverEveryWriter keeps the rename guard exhaustive: a new
// workspaceScopedWriter registered in the default tool set without a probe here would
// escape both tests below, and the fence going blind on it would surface only in
// production as an unclassified write.
func TestWriteTargetProbesCoverEveryWriter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	var carriers []string
	for _, tool := range DefaultTools(root) {
		if IsWorkspaceScopedWriter(tool) {
			carriers = append(carriers, tool.Name())
		}
	}
	var probed []string
	for _, p := range writeTargetProbes(root) {
		probed = append(probed, p.tool.Name())
	}
	slices.Sort(carriers)
	slices.Sort(probed)

	if !slices.Equal(carriers, probed) {
		t.Errorf("marker carriers in the default tool set = %v, probed here = %v; every workspace-scoped writer needs a probe or its path argument is unguarded",
			carriers, probed)
	}
}

// TestWriteToolsDeclarePathArgument pins both halves of every writer's target argument to the
// key that writer's fence body decodes: the schema the MODEL is told to fill, and the args struct
// EXECUTE decodes. The fence reads one minimal one-field struct per writer, so a
// writer that renames the argument on either side alone stays internally plausible while
// the fence silently stops seeing its write target — this fails first instead.
func TestWriteToolsDeclarePathArgument(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, p := range writeTargetProbes(root) {
		t.Run(p.tool.Name(), func(t *testing.T) {
			t.Parallel()

			var schema struct {
				Required   []string                  `json:"required"`
				Properties map[string]map[string]any `json:"properties"`
			}
			if err := json.Unmarshal(p.tool.Schema(), &schema); err != nil {
				t.Fatalf("schema is not valid JSON: %v", err)
			}

			property, ok := schema.Properties[p.targetKey]
			if !ok {
				t.Fatalf("schema declares properties %v, none named %q — the model would name this tool's write target something the fence never reads",
					propertyNames(schema.Properties), p.targetKey)
			}
			if property["type"] != "string" {
				t.Errorf("schema %q type = %v, want string (the fence decodes it as one)", p.targetKey, property["type"])
			}
			if !slices.Contains(schema.Required, p.targetKey) {
				t.Errorf("schema required = %v, want it to include %q — an optional target is a write the fence cannot classify",
					schema.Required, p.targetKey)
			}

			// The struct Execute decodes must tag its target field with the same key...
			argsType := reflect.TypeOf(p.args)
			pathField(t, argsType, p.targetKey)

			// ...and name exactly the arguments the schema does, so neither surface can be
			// renamed without the other.
			tags, properties := argJSONNames(argsType), propertyNames(schema.Properties)
			if !slices.Equal(tags, properties) {
				t.Errorf("%s json tags = %v, schema properties = %v; the decoded and the model-facing argument names must agree",
					argsType.Name(), tags, properties)
			}
		})
	}
}

// TestWriteTargetsAgreeOnPath is what makes the shared decode safe. Every
// workspace-scoped writer resolves its target through one of two shared bodies
// (pathArgWriteTarget, destinationArgWriteTarget), each decoding a minimal one-field struct
// rather than the tool's own args type — sound only while every write tool spells its target
// argument the way its body reads it. Each call below is marshalled
// from the tool's OWN args struct, so either side drifting (a renamed tag, a fence that
// decodes some other key) leaves the call naming a key the other cannot read and this fails
// first, instead of dispatch mis-classifying an out-of-workspace write as in-bounds. The
// negative rows pin the other half of the contract: nothing inspectable must yield
// ("", false), never a bare root.
func TestWriteTargetsAgreeOnPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	target := filepath.Join(root, "sub", "f.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// The oracle is the stdlib's own symlink resolution rather than the resolver under
	// test, so a temp dir reached through a symlinked parent (macOS /tmp) compares equal
	// for the right reason.
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	for _, p := range writeTargetProbes(root) {
		cases := []struct {
			name    string
			args    json.RawMessage
			wantAbs string
			wantOK  bool
		}{
			{
				name:    "target resolves against the root",
				args:    callArgs(t, p.args, p.targetKey, filepath.Join("sub", "f.txt")),
				wantAbs: want,
				wantOK:  true,
			},
			{name: "empty target", args: callArgs(t, p.args, p.targetKey, ""), wantAbs: "", wantOK: false},
			{name: "undecodable arguments", args: json.RawMessage("{"), wantAbs: "", wantOK: false},
		}

		for _, tc := range cases {
			t.Run(p.tool.Name()+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				call := domain.ToolCall{ID: "c1", Tool: p.tool.Name(), Arguments: tc.args}
				abs, ok := WorkspaceWriteTarget(p.tool, call)
				if abs != tc.wantAbs || ok != tc.wantOK {
					t.Errorf("WorkspaceWriteTarget(%s, %s) = (%q, %v), want (%q, %v)",
						p.tool.Name(), tc.args, abs, ok, tc.wantAbs, tc.wantOK)
				}
			})
		}
	}
}

// callArgs marshals a call payload from the tool's own args struct with its target field (the
// one tagged key) set to path, so the json key the fence receives is the one that struct
// declares. Sibling arguments stay zero — classification asks only where the write would land.
func callArgs(t *testing.T, args any, key, path string) json.RawMessage {
	t.Helper()

	argsType := reflect.TypeOf(args)
	value := reflect.New(argsType).Elem()
	value.FieldByIndex(pathField(t, argsType, key).Index).SetString(path)

	raw, err := json.Marshal(value.Interface())
	if err != nil {
		t.Fatalf("marshal %s: %v", argsType.Name(), err)
	}
	return raw
}

// pathField returns the field of a writer's args struct tagged with key — the only
// field that writer's fence body can see. A missing or non-string field fails the test, because
// that is precisely the drift the fence cannot survive: the tool would keep writing while
// dispatch resolved no target and treated every one of its writes as in-bounds.
func pathField(t *testing.T, argsType reflect.Type, key string) reflect.StructField {
	t.Helper()

	for i := 0; i < argsType.NumField(); i++ {
		if field := argsType.Field(i); jsonName(field) == key && field.Type.Kind() == reflect.String {
			return field
		}
	}
	t.Fatalf("%s declares json arguments %v — no string field tagged %q, which is the only key its write-target body decodes",
		argsType.Name(), argJSONNames(argsType), key)
	return reflect.StructField{}
}

// argJSONNames returns the sorted json object keys an args struct marshals to.
func argJSONNames(argsType reflect.Type) []string {
	names := make([]string, 0, argsType.NumField())
	for i := 0; i < argsType.NumField(); i++ {
		names = append(names, jsonName(argsType.Field(i)))
	}
	slices.Sort(names)
	return names
}

// jsonName returns the json object key field marshals to: its tag name, or the Go field
// name when untagged, with options like ",omitempty" stripped.
func jsonName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	if name == "" {
		return field.Name
	}
	return name
}

// propertyNames returns the sorted property names a tool's declared schema names.
func propertyNames(properties map[string]map[string]any) []string {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
