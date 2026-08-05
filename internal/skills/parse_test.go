package skills

import (
	"strings"
	"testing"
)

func TestParseSkillFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		dirName     string
		wantID      string
		wantDisplay string
		wantSummary string
		wantBodyHas string
	}{
		{
			name:        "canonical id/displayName/summary",
			content:     "---\nid: code-review\ndisplayName: Code Review\nsummary: Reviews a diff for bugs\n---\nDo a careful review.",
			dirName:     "ignored",
			wantID:      "code-review",
			wantDisplay: "Code Review",
			wantSummary: "Reviews a diff for bugs",
			wantBodyHas: "careful review",
		},
		{
			name:        "name/description aliases (agent-skills convention)",
			content:     "---\nname: lint\ndescription: Run the linter\n---\nlint the code",
			dirName:     "ignored",
			wantID:      "lint",
			wantDisplay: "Lint", // titleCase of the id, since no displayName
			wantSummary: "Run the linter",
			wantBodyHas: "lint the code",
		},
		{
			name:        "id derived from dirName, displayName title-cased",
			content:     "---\nsummary: A thing\n---\nbody here",
			dirName:     "my-cool-skill",
			wantID:      "my-cool-skill",
			wantDisplay: "My Cool Skill",
			wantSummary: "A thing",
			wantBodyHas: "body here",
		},
		{
			name:        "BOM and CRLF tolerated",
			content:     "\ufeff---\r\nid: x\r\nsummary: s\r\n---\r\nthe body",
			dirName:     "d",
			wantID:      "x",
			wantDisplay: "X",
			wantSummary: "s",
			wantBodyHas: "the body",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sk, err := parseSkill(tc.content, tc.dirName)
			if err != nil {
				t.Fatalf("parseSkill: %v", err)
			}
			if sk.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", sk.ID, tc.wantID)
			}
			if sk.DisplayName != tc.wantDisplay {
				t.Errorf("DisplayName = %q, want %q", sk.DisplayName, tc.wantDisplay)
			}
			if sk.Summary != tc.wantSummary {
				t.Errorf("Summary = %q, want %q", sk.Summary, tc.wantSummary)
			}
			if !strings.Contains(sk.Body, tc.wantBodyHas) {
				t.Errorf("Body = %q, want it to contain %q", sk.Body, tc.wantBodyHas)
			}
		})
	}
}

// Frontmatter that strict YAML rejects but a human reads without hesitation must still load.
// Each case here made the whole skill disappear before the lenient scan existed; the real-world
// one is the first — an unquoted description carrying ": ", which YAML reads as a nested mapping.
func TestParseSkillLenientFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantID      string
		wantSummary string
	}{
		{
			name:        `unquoted ": " inside a value`,
			content:     "---\nname: plan\ndescription: Writes plans in the house format: on request\n---\nbody",
			wantID:      "plan",
			wantSummary: "Writes plans in the house format: on request",
		},
		{
			name:        "tab-indented line",
			content:     "---\nname: plan\n\tdescription: Tabbed but obvious\n---\nbody",
			wantID:      "plan",
			wantSummary: "Tabbed but obvious",
		},
		{
			name:        "unbalanced quote",
			content:     "---\nname: plan\ndescription: \"Ran out of quote\n---\nbody",
			wantID:      "plan",
			wantSummary: "Ran out of quote",
		},
		{
			name:        "quotes stripped, inner colons kept",
			content:     "---\nname: plan\ndescription: \"execute: a path, or write: a goal\"\nbad: [\n---\nbody",
			wantID:      "plan",
			wantSummary: "execute: a path, or write: a goal",
		},
		{
			name:        "wrapped value folds into one summary",
			content:     "---\nname: plan\ndescription: First half: and\n  the second half\n---\nbody",
			wantID:      "plan",
			wantSummary: "First half: and the second half",
		},
		{
			name:        "key case is ignored",
			content:     "---\nname: plan\nDESCRIPTION: Shouty but readable: yes\n---\nbody",
			wantID:      "plan",
			wantSummary: "Shouty but readable: yes",
		},
		{
			name:        "block-scalar indicator does not leak into the value",
			content:     "---\nname: plan\ndescription: >-\n  folded: text\n---\nbody",
			wantID:      "plan",
			wantSummary: "folded: text",
		},
		{
			name:        "first declaration wins over a repeat",
			content:     "---\nname: plan\ndescription: The real one: here\ndescription: a later stray: no\n---\nbody",
			wantID:      "plan",
			wantSummary: "The real one: here",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sk, err := parseSkill(tc.content, "dir")
			if err != nil {
				t.Fatalf("parseSkill rejected recoverable frontmatter: %v", err)
			}
			if sk.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", sk.ID, tc.wantID)
			}
			if sk.Summary != tc.wantSummary {
				t.Errorf("Summary = %q, want %q", sk.Summary, tc.wantSummary)
			}
		})
	}
}

// The lenient scan must never touch a block strict YAML accepts, or valid files would quietly
// change meaning — a block scalar stays folded, and a real trailing comment stays stripped.
func TestParseSkillValidYAMLKeepsYAMLSemantics(t *testing.T) {
	folded, err := parseSkill("---\nname: plan\ndescription: >-\n  one line\n  continued\n---\nbody", "dir")
	if err != nil {
		t.Fatalf("parseSkill: %v", err)
	}
	if folded.Summary != "one line continued" {
		t.Errorf("Summary = %q, want the folded block scalar", folded.Summary)
	}

	commented, err := parseSkill("---\nname: plan\ndescription: real value # a note\n---\nbody", "dir")
	if err != nil {
		t.Fatalf("parseSkill: %v", err)
	}
	if commented.Summary != "real value" {
		t.Errorf("Summary = %q, want the YAML comment stripped", commented.Summary)
	}
}

// An indented key belongs to whatever encloses it, so the lenient scan must not hoist it into the
// skill's own fields — otherwise a nested "description:" would hijack the merged menu's summary.
func TestParseSkillLenientIgnoresNestedKeys(t *testing.T) {
	sk, err := parseSkill("---\nname: plan\ndescription: The real one: here\nmetadata:\n  description: internal note\n---\nbody", "dir")
	if err != nil {
		t.Fatalf("parseSkill: %v", err)
	}
	if sk.Summary != "The real one: here" {
		t.Errorf("Summary = %q, want the top-level description", sk.Summary)
	}
}

// Leniency has a floor: a block that is neither valid YAML nor carries a recognised key is still
// a skip, and the reported reason must be the YAML diagnosis (the line and the fault), not the
// scan's silence — that string is what /skills prints for the human to act on.
func TestParseSkillUnrecoverableFrontmatterKeepsYAMLError(t *testing.T) {
	_, err := parseSkill("---\nnope: \"unbalanced\n---\nbody", "dir")
	if err == nil {
		t.Fatal("expected a rejection for frontmatter with no recognised key")
	}
	if !strings.Contains(err.Error(), "yaml:") {
		t.Errorf("error = %q, want it to carry the YAML diagnosis", err)
	}
}

// A stray blank line above the fence used to cost the file its frontmatter AND load a garbage
// skill named after the fence itself ("---" as both display name and summary).
func TestParseSkillToleratesBlankLinesBeforeFence(t *testing.T) {
	sk, err := parseSkill("\n\n---\nname: plan\ndescription: Still frontmatter\n---\n\n# Plan\n\nbody", "dir")
	if err != nil {
		t.Fatalf("parseSkill: %v", err)
	}
	if sk.ID != "plan" || sk.Summary != "Still frontmatter" {
		t.Errorf("ID/Summary = %q/%q, want the frontmatter to be recognised", sk.ID, sk.Summary)
	}
	if strings.Contains(sk.DisplayName, "-") {
		t.Errorf("DisplayName = %q, want the fence not to leak into it", sk.DisplayName)
	}
}

func TestParseSkillNoFrontmatterFallback(t *testing.T) {
	content := "# My Skill\nDoes a useful thing.\nmore detail"
	sk, err := parseSkill(content, "my-skill-dir")
	if err != nil {
		t.Fatalf("parseSkill: %v", err)
	}
	if sk.ID != "my-skill-dir" {
		t.Errorf("ID = %q, want the dir name", sk.ID)
	}
	if sk.DisplayName != "My Skill" { // first line, heading marker stripped
		t.Errorf("DisplayName = %q, want %q", sk.DisplayName, "My Skill")
	}
	if sk.Summary != "Does a useful thing." { // first non-heading line
		t.Errorf("Summary = %q, want %q", sk.Summary, "Does a useful thing.")
	}
	if !strings.Contains(sk.Body, "# My Skill") {
		t.Errorf("fallback Body should be the whole file, got %q", sk.Body)
	}
}

func TestParseSkillSummaryClampedTo200(t *testing.T) {
	long := strings.Repeat("a", 500)
	sk, err := parseSkill("---\nid: x\nsummary: "+long+"\n---\nbody", "d")
	if err != nil {
		t.Fatalf("parseSkill: %v", err)
	}
	if len([]rune(sk.Summary)) != maxSummaryLen {
		t.Errorf("summary length = %d, want clamped to %d", len([]rune(sk.Summary)), maxSummaryLen)
	}
}

func TestParseSkillRejectsIncomplete(t *testing.T) {
	tests := []struct {
		name    string
		content string
		dirName string
	}{
		{"frontmatter without summary", "---\nid: x\ndisplayName: X\n---\nbody", "d"},
		{"empty file", "", "d"},
		{"only whitespace", "   \n  \n", "d"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseSkill(tc.content, tc.dirName); err == nil {
				t.Error("expected a rejection error for an incomplete skill, got nil")
			}
		})
	}
}

func TestTitleCase(t *testing.T) {
	cases := map[string]string{
		"code-review": "Code Review",
		"lint":        "Lint",
		"a-b-c":       "A B C",
		"":            "",
	}
	for in, want := range cases {
		if got := titleCase(in); got != want {
			t.Errorf("titleCase(%q) = %q, want %q", in, got, want)
		}
	}
}
