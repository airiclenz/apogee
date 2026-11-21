package skills

import (
	"errors"
	"reflect"
	"testing"
)

// TestFailedLinesAndShadowedLines pins the skip report's wording byte for byte: the singular and
// plural heads, the two- and four-space indents, discovery order within each half, the partition
// on ShadowedError (each half ignores the other's skips), and a field's line breaks folded to one
// row. The TUI's /skills report and the unattended Drivers both render through these two
// functions, so a change here is a change to every one of them.
func TestFailedLinesAndShadowedLines(t *testing.T) {
	t.Parallel()

	broken := SkipError{Path: "/lib/broken/SKILL.md", Err: errors.New("malformed YAML frontmatter")}
	missing := SkipError{Path: "/ws/.apogee/skills/missing/SKILL.md", Err: errors.New("missing required field: summary")}
	shadowed := SkipError{Path: "/ws/.apogee/skills/dup/SKILL.md", Err: ShadowedError{By: "/lib/dup/SKILL.md"}}
	shadowedToo := SkipError{Path: "/ws/.apogee/skills/twin/SKILL.md", Err: ShadowedError{By: "/lib/twin/SKILL.md"}}

	tests := []struct {
		name         string
		skips        []SkipError
		wantFailed   []string
		wantShadowed []string
	}{
		{
			name: "no skips renders neither half",
		},
		{
			name:  "one failed skip takes the singular head",
			skips: []SkipError{broken},
			wantFailed: []string{
				"1 skill found but not loaded:",
				"  broken — malformed YAML frontmatter",
				"    /lib/broken/SKILL.md",
			},
		},
		{
			name:  "many failed skips take the plural head in discovery order",
			skips: []SkipError{missing, broken},
			wantFailed: []string{
				"2 skills found but not loaded:",
				"  missing — missing required field: summary",
				"    /ws/.apogee/skills/missing/SKILL.md",
				"  broken — malformed YAML frontmatter",
				"    /lib/broken/SKILL.md",
			},
		},
		{
			name:  "one shadowed skip names both copies",
			skips: []SkipError{shadowed},
			wantShadowed: []string{
				"1 skill shadowed by another of the same id:",
				"  dup — /ws/.apogee/skills/dup/SKILL.md",
				"    the live copy is /lib/dup/SKILL.md",
			},
		},
		{
			name:  "a mix splits on the cause and each half keeps its own order",
			skips: []SkipError{shadowed, broken, shadowedToo, missing},
			wantFailed: []string{
				"2 skills found but not loaded:",
				"  broken — malformed YAML frontmatter",
				"    /lib/broken/SKILL.md",
				"  missing — missing required field: summary",
				"    /ws/.apogee/skills/missing/SKILL.md",
			},
			wantShadowed: []string{
				"2 skills shadowed by another of the same id:",
				"  dup — /ws/.apogee/skills/dup/SKILL.md",
				"    the live copy is /lib/dup/SKILL.md",
				"  twin — /ws/.apogee/skills/twin/SKILL.md",
				"    the live copy is /lib/twin/SKILL.md",
			},
		},
		{
			name: "a field with embedded line breaks stays one row",
			skips: []SkipError{
				{Path: "/lib/evil\nname/SKILL.md", Err: errors.New("yaml: line 2:\n\tmapping values\rare not allowed")},
				{Path: "/ws/a\nb/SKILL.md", Err: ShadowedError{By: "/lib/a\nb/SKILL.md"}},
			},
			wantFailed: []string{
				"1 skill found but not loaded:",
				"  evil name — yaml: line 2:  mapping values are not allowed",
				"    /lib/evil name/SKILL.md",
			},
			wantShadowed: []string{
				"1 skill shadowed by another of the same id:",
				"  a b — /ws/a b/SKILL.md",
				"    the live copy is /lib/a b/SKILL.md",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FailedLines(tt.skips); !reflect.DeepEqual(got, tt.wantFailed) {
				t.Errorf("FailedLines =\n%q\nwant\n%q", got, tt.wantFailed)
			}
			if got := ShadowedLines(tt.skips); !reflect.DeepEqual(got, tt.wantShadowed) {
				t.Errorf("ShadowedLines =\n%q\nwant\n%q", got, tt.wantShadowed)
			}
		})
	}
}
