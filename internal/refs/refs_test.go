package refs

import (
	"reflect"
	"testing"
)

// knownSkills builds the catalog predicate SkillSpans resolves against, from a literal set of
// ids — the pure-layer stand-in for a Driver's live catalog probe.
func knownSkills(ids ...string) func(string) bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return func(id string) bool { return set[id] }
}

func TestFileRefs(t *testing.T) {
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
		// Quoted refs — a path with spaces is unreachable without them.
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
			t.Parallel()

			got := FileRefs(c.in)

			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("FileRefs(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestSkillRefs(t *testing.T) {
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
			t.Parallel()

			got := SkillRefs(c.in, known)

			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("SkillRefs(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// A nil predicate means no catalog is wired: every token is prose.
func TestSkillRefsNilPredicate(t *testing.T) {
	t.Parallel()

	if got := SkillRefs("/grill-me now", nil); got != nil {
		t.Errorf("SkillRefs with a nil predicate = %v, want nil", got)
	}
	if got := SkillSpans("/grill-me now", nil); got != nil {
		t.Errorf("SkillSpans with a nil predicate = %v, want nil", got)
	}
}

// The spans are the half Names throws away: they must cover the LITERAL token, quotes included,
// so a renderer painting [Start,End) paints exactly what the human typed.
func TestFileSpans(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Span
	}{
		{"none", "just a plain message", nil},
		{"bare token", "see @a/b.go now", []Span{{Start: 4, End: 11, Name: "a/b.go"}}},
		{"quotes are inside the span", `@"a b.md", thanks`, []Span{{Start: 0, End: 9, Name: "a b.md"}}},
		{"single quotes are inside the span", "@'a b.md' now", []Span{{Start: 0, End: 9, Name: "a b.md"}}},
		{
			"one span per occurrence, even when the names dedupe",
			`@x and @"x"`,
			[]Span{{Start: 0, End: 2, Name: "x"}, {Start: 7, End: 11, Name: "x"}},
		},
		{"skipped token still advances the scan", "@ @b.go", []Span{{Start: 2, End: 7, Name: "b.go"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := FileSpans(c.in)

			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("FileSpans(%q) = %+v, want %+v", c.in, got, c.want)
			}
			for _, sp := range got {
				if literal := c.in[sp.Start:sp.End]; literal[0] != '@' {
					t.Errorf("span %+v covers %q, want the literal @token", sp, literal)
				}
			}
		})
	}
}

func TestSkillSpans(t *testing.T) {
	known := knownSkills("grill-me", "code-audit")
	cases := []struct {
		name string
		in   string
		want []Span
	}{
		{"none", "look in /usr/bin for it", nil},
		{"at start", "/grill-me please", []Span{{Start: 0, End: 9, Name: "grill-me"}}},
		{
			"one span per occurrence, even when the names dedupe",
			"/grill-me and /grill-me",
			[]Span{{Start: 0, End: 9, Name: "grill-me"}, {Start: 14, End: 23, Name: "grill-me"}},
		},
		{"newline is a boundary", "first\n/code-audit", []Span{{Start: 6, End: 17, Name: "code-audit"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := SkillSpans(c.in, known)

			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("SkillSpans(%q) = %+v, want %+v", c.in, got, c.want)
			}
			for _, sp := range got {
				if literal := c.in[sp.Start:sp.End]; literal != "/"+sp.Name {
					t.Errorf("span %+v covers %q, want %q", sp, literal, "/"+sp.Name)
				}
			}
		})
	}
}

func TestNames(t *testing.T) {
	t.Parallel()

	if got := Names(nil); got != nil {
		t.Errorf("Names(nil) = %v, want nil", got)
	}

	spans := []Span{
		{Start: 0, End: 2, Name: "x"},
		{Start: 3, End: 7, Name: "y"},
		{Start: 8, End: 12, Name: "x"},
	}

	got := Names(spans)

	if want := []string{"x", "y"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Names(%+v) = %v, want %v (first-seen, de-duplicated)", spans, got, want)
	}
}

func TestScanToken(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		start    int
		wantPath string
		wantEnd  int
	}{
		{"bare token", "@a/b.go rest", 1, "a/b.go", 7},
		{"bare token to end", "@a/b.go", 1, "a/b.go", 7},
		{"quoted token", `@"a b.md", thanks`, 1, "a b.md", 9},
		{"single-quoted token", "@'a b.md' now", 1, "a b.md", 9},
		{"empty quoted token", `@"" here`, 1, "", 3},
		{"unterminated quote runs to end", `@"a b`, 1, "a b", 5},
		{"unterminated quote stops at newline", "@\"a b\nnext", 1, "a b", 5},
		{"unterminated quote right-trimmed", "@\"a b  \t\nnext", 1, "a b", 8},
		{"start past the end", "@", 1, "", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			path, end := ScanToken(c.in, c.start)

			if path != c.wantPath || end != c.wantEnd {
				t.Errorf("ScanToken(%q, %d) = (%q, %d), want (%q, %d)",
					c.in, c.start, path, end, c.wantPath, c.wantEnd)
			}
		})
	}
}

func TestIsSpace(t *testing.T) {
	t.Parallel()

	for _, b := range []byte{' ', '\t', '\n', '\r'} {
		if !IsSpace(b) {
			t.Errorf("IsSpace(%q) = false, want true", b)
		}
	}
	for _, b := range []byte{'a', '@', '/', '"', 0} {
		if IsSpace(b) {
			t.Errorf("IsSpace(%q) = true, want false", b)
		}
	}
}
