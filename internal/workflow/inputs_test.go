package workflow

import (
	"reflect"
	"strings"
	"testing"
)

// auditInputs are the declarations the cases below bind against: a required scope, then an
// optional focus with no default.
var auditInputs = []InputDecl{
	{Name: "scope", Required: true, Description: "the paths to audit"},
	{Name: "focus", Description: "what to look for"},
}

func TestBindInputsBindsTheUsersText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		decls       []InputDecl
		text        string
		wantValues  map[string]string
		wantMissing []string
	}{
		{
			name:       "positional tokens bind in declared order",
			decls:      auditInputs,
			text:       "internal/ security",
			wantValues: map[string]string{"scope": "internal/", "focus": "security"},
		},
		{
			name:       "keyed tokens bind by name in any order",
			decls:      auditInputs,
			text:       "focus=security scope=internal/",
			wantValues: map[string]string{"scope": "internal/", "focus": "security"},
		},
		{
			name:       "a positional token fills the first input no key took",
			decls:      auditInputs,
			text:       "scope=internal/ security",
			wantValues: map[string]string{"scope": "internal/", "focus": "security"},
		},
		{
			name:       "a keyed token after a positional one still binds by name",
			decls:      auditInputs,
			text:       "internal/ focus=security",
			wantValues: map[string]string{"scope": "internal/", "focus": "security"},
		},
		{
			name:       "quoted tokens keep their whitespace",
			decls:      auditInputs,
			text:       `"internal/my dir" focus='error handling'`,
			wantValues: map[string]string{"scope": "internal/my dir", "focus": "error handling"},
		},
		{
			name:       "a quoted equals sign is a positional value",
			decls:      auditInputs,
			text:       `"a=b"`,
			wantValues: map[string]string{"scope": "a=b", "focus": ""},
		},
		{
			name:       "a word that is not a name before = is a positional value",
			decls:      auditInputs,
			text:       "https://example.test/?q=1",
			wantValues: map[string]string{"scope": "https://example.test/?q=1", "focus": ""},
		},
		{
			name:       "an unbound optional input binds the empty string",
			decls:      auditInputs,
			text:       "internal/",
			wantValues: map[string]string{"scope": "internal/", "focus": ""},
		},
		{
			name:        "a required input with no value is missing",
			decls:       auditInputs,
			text:        "focus=security",
			wantValues:  map[string]string{"focus": "security"},
			wantMissing: []string{"scope"},
		},
		{
			name:        "empty text leaves every required input missing",
			decls:       auditInputs,
			text:        "  \t ",
			wantValues:  map[string]string{"focus": ""},
			wantMissing: []string{"scope"},
		},
		{
			name: "a declared default fills an unbound input",
			decls: []InputDecl{
				{Name: "scope", Required: true},
				{Name: "focus", Default: "bugs"},
			},
			text:       "internal/",
			wantValues: map[string]string{"scope": "internal/", "focus": "bugs"},
		},
		{
			name:       "a required input with a default is not missing",
			decls:      []InputDecl{{Name: "scope", Required: true, Default: "."}},
			text:       "",
			wantValues: map[string]string{"scope": "."},
		},
		{
			name:       "an empty positional value takes its slot and falls to the default",
			decls:      []InputDecl{{Name: "scope", Default: "."}, {Name: "focus"}},
			text:       `"" security`,
			wantValues: map[string]string{"scope": ".", "focus": "security"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			values, missing, err := BindInputs(tc.decls, tc.text)

			if err != nil {
				t.Fatalf("BindInputs(%q) error: %v", tc.text, err)
			}
			if !reflect.DeepEqual(values, tc.wantValues) {
				t.Errorf("values = %v, want %v", values, tc.wantValues)
			}
			if !reflect.DeepEqual(missing, tc.wantMissing) {
				t.Errorf("missing = %v, want %v", missing, tc.wantMissing)
			}
		})
	}
}

func TestBindInputsRefusesWithAnErrorNamingTheProblem(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		decls []InputDecl
		text  string
		want  []string
	}{
		{
			name:  "unknown key",
			decls: auditInputs,
			text:  "scope=internal/ depth=3",
			want:  []string{`unknown input "depth"`, "scope, focus"},
		},
		{
			name:  "surplus tokens",
			decls: auditInputs,
			text:  `internal/ security extra "one more"`,
			want:  []string{`surplus value "extra"`, `surplus value "\"one more\""`},
		},
		{
			name:  "a key given twice",
			decls: auditInputs,
			text:  "scope=a scope=b",
			want:  []string{`input "scope" is given twice`},
		},
		{
			name:  "every problem at once",
			decls: auditInputs,
			text:  "depth=3 a b c",
			want:  []string{`unknown input "depth"`, `surplus value "c"`},
		},
		{
			name:  "unterminated quote",
			decls: auditInputs,
			text:  `internal/ "error handling`,
			want:  []string{"unterminated", `"error handling`},
		},
		{
			name:  "any token when nothing is declared",
			decls: nil,
			text:  "internal/",
			want:  []string{`surplus value "internal/"`, "the inputs are: none"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			values, missing, err := BindInputs(tc.decls, tc.text)

			if err == nil {
				t.Fatalf("BindInputs(%q) = %v, %v; want an error", tc.text, values, missing)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
			if values != nil || missing != nil {
				t.Errorf("on an error values = %v, missing = %v; want both nil", values, missing)
			}
		})
	}
}
