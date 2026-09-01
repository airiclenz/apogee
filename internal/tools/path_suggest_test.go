package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// rowBreakSibling is the fixture entry whose NAME carries a line break — a filename POSIX allows
// and Windows does not — and escapedRowBreakSuggestion is the spelling suggestSiblings answers it
// with: the break rendered as the two characters \ and n, so the name cannot forge a row of its own
// in a refusal the model reads. Both tests spell it from these constants so the fixture and the
// expectation can never drift apart.
const (
	rowBreakSibling           = "0025-row\nbreak.md"
	escapedRowBreakSuggestion = `docs/rows/0025-row\nbreak.md`
)

// seedSuggestTree creates the read-only tree the suggestSiblings cases share: a directory of
// seven files sharing one prefix, a mixed-case name, a directory and a file that differ only in
// case, a sibling that is a symlink out of the root, a parent that is a symlink out of it, and a
// sibling whose name carries a row break. The row-break entry lives in a directory of its own so
// the cap-at-five and sort expectations of the other cases are untouched by it.
func seedSuggestTree(t *testing.T, root, outside string) {
	t.Helper()

	dirs := []string{
		filepath.Join("docs", "adr"),
		filepath.Join("docs", "case"),
		filepath.Join("docs", "dirs", "Bundle"),
		filepath.Join("docs", "link"),
		filepath.Join("docs", "rows"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("setup mkdir %s: %v", dir, err)
		}
	}

	files := []string{
		filepath.Join("docs", "adr", "0025-alpha.md"),
		filepath.Join("docs", "adr", "0025-bravo.md"),
		filepath.Join("docs", "adr", "0025-charlie.md"),
		filepath.Join("docs", "adr", "0025-delta.md"),
		filepath.Join("docs", "adr", "0025-echo.md"),
		filepath.Join("docs", "adr", "0025-foxtrot.md"),
		filepath.Join("docs", "adr", "0025-golf.md"),
		filepath.Join("docs", "adr", "0026-unrelated.md"),
		filepath.Join("docs", "case", "ReadMe.md"),
		filepath.Join("docs", "dirs", "bundle.txt"),
		filepath.Join("docs", "link", "0025-plain.md"),
		filepath.Join("docs", "rows", rowBreakSibling),
		"top.txt",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("setup write %s: %v", name, err)
		}
	}

	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("setup write outside file: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "id_rsa"), filepath.Join(root, "docs", "link", "0025-escape.md")); err != nil {
		t.Fatalf("setup sibling symlink: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "docs", "elsewhere")); err != nil {
		t.Fatalf("setup parent symlink: %v", err)
	}
}

func TestSuggestSiblings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	seedSuggestTree(t, root, outside)

	cases := []struct {
		name          string
		rel           string
		given         string
		want          []string
		skipOnWindows bool
	}{
		{
			name:  "prefix hits are sorted and capped at five",
			rel:   filepath.Join("docs", "adr", "0025"),
			given: "docs/adr/0025",
			want: []string{
				"docs/adr/0025-alpha.md",
				"docs/adr/0025-bravo.md",
				"docs/adr/0025-charlie.md",
				"docs/adr/0025-delta.md",
				"docs/adr/0025-echo.md",
			},
		},
		{
			name:  "the prefix match is case-insensitive",
			rel:   filepath.Join("docs", "case", "readme"),
			given: "docs/case/readme",
			want:  []string{"docs/case/ReadMe.md"},
		},
		{
			name:  "a directory entry carries a trailing slash",
			rel:   filepath.Join("docs", "dirs", "bun"),
			given: "docs/dirs/bun",
			want:  []string{"docs/dirs/Bundle/", "docs/dirs/bundle.txt"},
		},
		{
			name:  "a sibling that is a symlink out of the root is listed by name",
			rel:   filepath.Join("docs", "link", "0025"),
			given: "docs/link/0025",
			want:  []string{"docs/link/0025-escape.md", "docs/link/0025-plain.md"},
		},
		{
			name:  "the given spelling of the parent is what the suggestions carry",
			rel:   filepath.Join("docs", "adr", "0026"),
			given: filepath.Join(root, "docs", "adr", "0026"),
			want:  []string{filepath.Join(root, "docs", "adr", "0026-unrelated.md")},
		},
		{
			name:  "no entry shares the prefix",
			rel:   filepath.Join("docs", "adr", "9999"),
			given: "docs/adr/9999",
			want:  nil,
		},
		{
			name:  "the parent itself is missing",
			rel:   filepath.Join("docs", "nowhere", "0025"),
			given: "docs/nowhere/0025",
			want:  nil,
		},
		{
			name:  "the parent is a file, not a directory",
			rel:   filepath.Join("top.txt", "0025"),
			given: "top.txt/0025",
			want:  nil,
		},
		{
			name:  "the parent is a symlink out of the root",
			rel:   filepath.Join("docs", "elsewhere", "id_rsa"),
			given: "docs/elsewhere/id_rsa",
			want:  nil,
		},
		{
			name:  "no name is missing",
			rel:   ".",
			given: ".",
			want:  nil,
		},
		{
			name:          "a sibling whose name carries a row break is escaped",
			rel:           filepath.Join("docs", "rows", "0025"),
			given:         "docs/rows/0025",
			want:          []string{escapedRowBreakSuggestion},
			skipOnWindows: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.skipOnWindows && runtime.GOOS == "windows" {
				t.Skip("the fixture entry's name holds a newline; Windows has no such files")
			}

			got := suggestSiblings(root, tc.rel, tc.given)

			if !slices.Equal(got, tc.want) {
				t.Errorf("suggestSiblings(root, %q, %q) = %q, want %q", tc.rel, tc.given, got, tc.want)
			}
		})
	}
}

func TestNotFoundMessage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		prefix      string
		given       string
		suggestions []string
		want        string
	}{
		{
			name:   "no suggestion leaves the former message unchanged",
			prefix: "path not found: ",
			given:  "docs/adr/0025",
			want:   "path not found: docs/adr/0025",
		},
		{
			name:        "one suggestion",
			prefix:      "path not found: ",
			given:       "docs/adr/0025",
			suggestions: []string{"docs/adr/0025-interjections.md"},
			want:        "path not found: docs/adr/0025 — did you mean: docs/adr/0025-interjections.md",
		},
		{
			name:        "several suggestions are semicolon-joined",
			prefix:      "directory not found: ",
			given:       "docs/adr",
			suggestions: []string{"docs/adr-drafts/", "docs/adrs/"},
			want:        "directory not found: docs/adr — did you mean: docs/adr-drafts/; docs/adrs/",
		},
		{
			// The suggestion is the one suggestSiblings answers for the row-break fixture entry,
			// so the assembled sentence is the one a model would really be handed.
			name:        "an escaped row break stays one row in the assembled sentence",
			prefix:      "file not found: ",
			given:       "docs/rows/0025",
			suggestions: []string{escapedRowBreakSuggestion},
			want:        "file not found: docs/rows/0025 — did you mean: " + escapedRowBreakSuggestion,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := notFoundMessage(tc.prefix, tc.given, tc.suggestions)

			if got != tc.want {
				t.Errorf("notFoundMessage(%q, %q, %q) = %q, want %q", tc.prefix, tc.given, tc.suggestions, got, tc.want)
			}
			// The refusal is one row of a one-message grammar: no case may assemble a sentence
			// that forges a second row, however the names inside it are spelled.
			if strings.ContainsAny(got, "\r\n") {
				t.Errorf("notFoundMessage(%q, %q, %q) = %q, want no raw row break", tc.prefix, tc.given, tc.suggestions, got)
			}
		})
	}
}
