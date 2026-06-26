package skills

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// maxSummaryLen caps a skill summary, mirroring the apogee-code oracle (summary.slice(0,200)):
// the summary is a one-line picker hint, not a second body, so a runaway description never
// crowds the /skill dropdown.
const maxSummaryLen = 200

// frontmatterRe splits a SKILL.md into its YAML frontmatter (group 1) and body (group 2): an
// optional BOM, a "---" fence line, the frontmatter, a closing "---" fence line, then the rest.
// (?s) makes "." span newlines and the non-greedy first group stops at the first closing fence.
// \r? tolerates CRLF files. It mirrors the oracle's /^?---\n([\s\S]*?)\n---\n([\s\S]*)$/.
var frontmatterRe = regexp.MustCompile(`(?s)\A\x{feff}?-{3}[ \t]*\r?\n(.*?)\r?\n-{3}[ \t]*\r?\n?(.*)\z`)

// frontmatter is the recognised YAML frontmatter keys, including the apogee-code/agent-skills
// aliases: id|name for the identifier, displayName for the picker label, summary|description
// for the picker hint. An unknown key is ignored (yaml.v3 does not error on extras).
type frontmatter struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	DisplayName string `yaml:"displayName"`
	Summary     string `yaml:"summary"`
	Description string `yaml:"description"`
}

// parseSkill turns one SKILL.md's content into a Skill, deriving the ID from dirName when the
// frontmatter omits it. It has two paths: frontmatter (the canonical case) and a no-frontmatter
// fallback (the oracle's leniency — id from the folder, displayName/summary sniffed from the
// first lines). Either path must yield a non-empty id, displayName, AND summary, else the skill
// is rejected so the caller skips it with a soft error. Load sets Dir; parseSkill leaves it "".
func parseSkill(content, dirName string) (Skill, error) {
	if m := frontmatterRe.FindStringSubmatch(content); m != nil {
		return parseWithFrontmatter(m[1], m[2], dirName)
	}
	return parseFallback(content, dirName)
}

// parseWithFrontmatter builds a Skill from the parsed frontmatter and trimmed body, applying
// the oracle's alias/derivation rules: id = id||name||dirName, displayName = displayName||
// titleCase(id), summary = summary||description.
func parseWithFrontmatter(fmText, body, dirName string) (Skill, error) {
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		return Skill{}, fmt.Errorf("malformed YAML frontmatter: %w", err)
	}
	id := firstNonEmpty(fm.ID, fm.Name, dirName)
	return validate(Skill{
		ID:          strings.TrimSpace(id),
		DisplayName: strings.TrimSpace(firstNonEmpty(fm.DisplayName, titleCase(id))),
		Summary:     clampSummary(firstNonEmpty(fm.Summary, fm.Description)),
		Body:        strings.TrimSpace(body),
	})
}

// parseFallback handles a SKILL.md with no frontmatter (the oracle's fallback): id = dirName,
// displayName = the first non-empty line with its heading marker stripped, summary = the first
// non-empty non-heading line, body = the whole file. It lets a plain-Markdown skill load.
func parseFallback(content, dirName string) (Skill, error) {
	var displayName, summary string
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if displayName == "" {
			displayName = strings.TrimSpace(strings.TrimLeft(t, "#"))
		}
		if summary == "" && !strings.HasPrefix(t, "#") {
			summary = t
		}
		if displayName != "" && summary != "" {
			break
		}
	}
	if displayName == "" {
		displayName = dirName
	}
	if summary == "" {
		summary = displayName // a heading-only skill summarises itself by its title
	}
	return validate(Skill{
		ID:          strings.TrimSpace(dirName),
		DisplayName: strings.TrimSpace(displayName),
		Summary:     clampSummary(summary),
		Body:        strings.TrimSpace(content),
	})
}

// validate rejects a skill missing any load-bearing field — without an id it cannot be
// attached, without a displayName/summary it cannot be shown, without a body there is nothing
// to inject — so the loader skips it rather than surfacing a half-blank, contentless entry in
// the picker. The body check is what turns an empty/whitespace SKILL.md into a skip: the
// fallback would otherwise name it after its folder and load a skill with nothing to say.
func validate(s Skill) (Skill, error) {
	if s.ID == "" || s.DisplayName == "" || s.Summary == "" || s.Body == "" {
		return Skill{}, fmt.Errorf("skill is missing a required field (id=%q displayName=%q summary=%q body-empty=%v)",
			s.ID, s.DisplayName, s.Summary, s.Body == "")
	}
	return s, nil
}

// firstNonEmpty returns the first argument that is non-empty after trimming, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// titleCase turns a kebab-case id into a spaced, capitalised label ("code-review" → "Code
// Review"), the oracle's displayName fallback when none is declared.
func titleCase(s string) string {
	parts := strings.Split(strings.TrimSpace(s), "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}

// clampSummary trims and caps a summary at maxSummaryLen runes (rune-safe so a multibyte cut
// never splits a character).
func clampSummary(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxSummaryLen {
		return string(r[:maxSummaryLen])
	}
	return s
}
