package provider

import (
	"reflect"
	"testing"
)

// TestMergePatchApply pins the RFC 7396 semantics apogee's request-extra rides on: objects
// merge member by member, arrays and scalars replace, null deletes, a new member is appended
// after the target's own, and an untouched member keeps its bytes and its place.
func TestMergePatchApply(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		target string
		patch  string
		want   string
	}{
		{
			name:   "deep merge keeps sibling members",
			target: `{"model":"m","reasoning":{"effort":"high"}}`,
			patch:  `{"reasoning":{"exclude":true}}`,
			want:   `{"model":"m","reasoning":{"effort":"high","exclude":true}}`,
		},
		{
			name:   "array replaces rather than merges",
			target: `{"stop":["a","b"],"n":1}`,
			patch:  `{"stop":["c"]}`,
			want:   `{"stop":["c"],"n":1}`,
		},
		{
			name:   "null deletes a member",
			target: `{"model":"m","top_k":40,"stream":false}`,
			patch:  `{"top_k":null}`,
			want:   `{"model":"m","stream":false}`,
		},
		{
			name:   "null for an absent member is a no-op",
			target: `{"model":"m"}`,
			patch:  `{"seed":null}`,
			want:   `{"model":"m"}`,
		},
		{
			name:   "new members append in patch order",
			target: `{"model":"m"}`,
			patch:  `{"provider":{"order":["x"]},"models":["a","b"]}`,
			want:   `{"model":"m","provider":{"order":["x"]},"models":["a","b"]}`,
		},
		{
			name:   "object over a scalar merges into an empty object and drops nested nulls",
			target: `{"provider":"x"}`,
			patch:  `{"provider":{"order":["y"],"gone":null}}`,
			want:   `{"provider":{"order":["y"]}}`,
		},
		{
			name:   "scalar replaces an object",
			target: `{"reasoning":{"effort":"high"}}`,
			patch:  `{"reasoning":false}`,
			want:   `{"reasoning":false}`,
		},
		{
			name:   "untouched subtree keeps its exact bytes",
			target: `{"tools":[{"parameters":{"z":1,"a":"<"}}],"model":"m"}`,
			patch:  `{"provider":{"sort":"throughput"}}`,
			want:   `{"tools":[{"parameters":{"z":1,"a":"<"}}],"model":"m","provider":{"sort":"throughput"}}`,
		},
		{
			name:   "non-canonical patch whitespace is compacted",
			target: `{}`,
			patch:  "{ \"a\" : [ 1 , 2 ] }",
			want:   `{"a":[1,2]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			patch, err := parseMergePatch(tc.patch)
			if err != nil {
				t.Fatalf("parseMergePatch: %v", err)
			}

			got, err := patch.apply([]byte(tc.target))

			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("apply = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestMergePatchIsReadOnlyAcrossMerges pins that apply never writes into the decoded patch: a
// Client shares one across every encode, parallel ones included.
func TestMergePatchIsReadOnlyAcrossMerges(t *testing.T) {
	t.Parallel()
	const raw = `{"provider":{"order":["x"],"drop":null},"reasoning":{"exclude":true}}`
	patch, err := parseMergePatch(raw)
	if err != nil {
		t.Fatalf("parseMergePatch: %v", err)
	}
	pristine, _ := parseMergePatch(raw)

	for _, target := range []string{`{"provider":{"drop":1,"keep":2}}`, `{"reasoning":{"effort":"low"}}`} {
		if _, err := patch.apply([]byte(target)); err != nil {
			t.Fatalf("apply(%s): %v", target, err)
		}
	}

	if !reflect.DeepEqual(patch, pristine) {
		t.Errorf("patch changed across two merges: %+v, want %+v", patch, pristine)
	}
}

// TestParseMergePatch pins which request-extra strings install a patch: "" and {} install
// none, and anything but one JSON object is refused.
func TestParseMergePatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		wantNil bool
		wantErr bool
	}{
		{name: "empty string", raw: "", wantNil: true},
		{name: "empty object", raw: "{}", wantNil: true},
		{name: "object", raw: `{"a":1}`},
		{name: "array", raw: `[1]`, wantErr: true},
		{name: "scalar", raw: `5`, wantErr: true},
		{name: "malformed", raw: `{"a":`, wantErr: true},
		{name: "trailing data", raw: `{"a":1} {}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			patch, err := parseMergePatch(tc.raw)

			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if !tc.wantErr && (patch == nil) != tc.wantNil {
				t.Errorf("patch = %+v, want nil %v", patch, tc.wantNil)
			}
		})
	}
}
