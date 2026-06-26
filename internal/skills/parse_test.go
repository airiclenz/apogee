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
