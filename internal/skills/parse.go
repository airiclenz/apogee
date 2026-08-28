package skills

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// maxSummaryLen caps a skill summary, mirroring the apogee-code oracle (summary.slice(0,200)):
// the summary is a one-line menu hint, not a second body, so a runaway description never
// crowds the merged "/" menu. The cap is the menu's alone — Skill.Description keeps the full
// text for the suggestion matcher, which must see a phrase wherever the author placed it.
const maxSummaryLen = 200

// maxTriggerLen caps ONE trigger phrase at 64 runes: a trigger is a fragment a prompt might
// contain ("review this diff"), not a sentence, and a phrase longer than a draft line can never be
// the thing that fits it — it would only widen the /skills row it prints on.
const maxTriggerLen = 64

// maxTriggers caps the LIST at 32 phrases: past that a skill has stopped declaring where it fits
// and started claiming every draft, which is exactly what a suggestion band must not let one
// SKILL.md do to the rest of the catalog.
const maxTriggers = 32

// frontmatterRe splits a SKILL.md into its YAML frontmatter (group 1) and body (group 2): an
// optional BOM, leading blank space, a "---" fence line, the frontmatter, a closing "---" fence
// line, then the rest. (?s) makes "." span newlines and the non-greedy first group stops at the
// first closing fence. \r? tolerates CRLF files. It widens the oracle's
// /^?---\n([\s\S]*?)\n---\n([\s\S]*)$/ by the leading-whitespace allowance.
//
// That allowance matters more than it looks: a file whose fence is preceded by one stray blank
// line does not merely lose its frontmatter, it falls through to parseFallback and loads a
// GARBAGE skill — displayName and summary both read "---", the fence itself. Tolerating the blank
// line is what keeps an invisible whitespace slip from putting nonsense in the menu. Only
// whitespace is skipped, so a plain-Markdown skill (no fence at the top) still takes the fallback
// path it is meant to.
var frontmatterRe = regexp.MustCompile(`(?s)\A\x{feff}?[ \t\r\n]*-{3}[ \t]*\r?\n(.*?)\r?\n-{3}[ \t]*\r?\n?(.*)\z`)

// keyLineRe matches one "key: value" line for the lenient scan: a key, a colon, then the
// remainder of the line taken verbatim as the value. Indentation is allowed before the key —
// a tab-indented "\tdescription:" is exactly the kind of slip the scan exists to recover, and
// refusing indented keys would read it as prose belonging to the line above. The value group is
// optional so a bare "key:" (whatever follows belongs to it) still registers. Requiring
// whitespace after the colon keeps "foo:bar" a plain value, as YAML has it.
var keyLineRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_.-]*)[ \t]*:(?:[ \t]+(.*))?$`)

// blockScalarRe matches a bare YAML block-scalar indicator ("|", ">-", "|+2"). It introduces a
// value on the following lines rather than being one, so the scan drops it and lets those lines
// fold in as the value instead of prefixing the summary with punctuation.
var blockScalarRe = regexp.MustCompile(`^[|>][+-]?\d*$`)

// recognisedKeys is the set of frontmatter keys the lenient scan will adopt, lowercased. Only
// these are read: an unmodelled key ("argument-hint", "metadata", "allowed-tools") closes the
// current field instead of contributing to it, so its own indented block cannot be mistaken for
// continuation text belonging to the skill's description.
var recognisedKeys = map[string]bool{
	"id":          true,
	"name":        true,
	"displayname": true,
	"summary":     true,
	"description": true,
	"triggers":    true,
}

// frontmatter is the recognised YAML frontmatter keys, including the apogee-code/agent-skills
// aliases: id|name for the identifier, displayName for the menu label, summary|description
// for the menu hint, and apogee's own optional triggers for the suggestion matcher. An unknown key
// is ignored (yaml.v3 does not error on extras).
type frontmatter struct {
	ID          string        `yaml:"id"`
	Name        string        `yaml:"name"`
	DisplayName string        `yaml:"displayName"`
	Summary     string        `yaml:"summary"`
	Description string        `yaml:"description"`
	Triggers    triggersField `yaml:"triggers"`
}

// hasNamingField reports whether a scanned block yielded any field a skill could actually be built
// from. Triggers are deliberately NOT among them: they are optional garnish on the suggestion
// matcher and can never make a skill loadable on their own, so a block that recovered nothing else
// is still better reported through the strict parser's error, which names the offending line.
func (f frontmatter) hasNamingField() bool {
	return firstNonEmpty(f.ID, f.Name, f.DisplayName, f.Summary, f.Description) != ""
}

// triggersField is the frontmatter's optional "triggers:" value. It exists because authors write
// one intent in two shapes — a YAML sequence of phrases, or a single comma-separated scalar — and
// a plain []string accepts only the first, turning the second into a type error that costs the
// whole strict parse.
type triggersField []string

// UnmarshalYAML decodes either accepted shape: a sequence contributes each of its scalar entries,
// a scalar is split on commas. Normalisation (casing, whitespace, the caps) is deliberately left
// to normalizeTriggers, so this path and the lenient scan land on the same list.
//
// Any OTHER node kind — a mapping, an alias — yields no error and an empty list. triggers: is the
// one field nothing depends on: it feeds the suggestion matcher and nothing else, never the model
// and never the skill's identity. Returning an error here would fail the strict unmarshal and drop
// the whole block onto the lenient scan, so one mistyped optional field would cost the skill its
// quoting, its block scalars and its comments — a skill is never sunk, nor degraded, over its
// triggers.
func (t *triggersField) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		phrases := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind == yaml.ScalarNode {
				phrases = append(phrases, item.Value)
			}
		}
		*t = phrases
	case yaml.ScalarNode:
		*t = splitTriggers(nil, node.Value)
	}
	return nil
}

// splitTriggers resolves a recovered triggers: field into phrases. Items the author already
// separated — the entries of a YAML block sequence, which the lenient scan hands over one per
// line — are taken as they stand, so a phrase carrying a comma survives whole. Only a single
// scalar value is cut on commas, the shape an author who wrote the field on one line intends. An
// empty value yields no phrases rather than one blank.
func splitTriggers(items []string, value string) []string {
	if len(items) > 0 {
		return items
	}
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, ",")
}

// normalizeTriggers turns the raw phrases either path recovered into the list a Skill carries:
// trimmed, lowercased, internal whitespace collapsed, empties dropped, duplicates removed (first
// declaration wins, as the lenient scan has it), each phrase clipped to maxTriggerLen runes and
// the list to maxTriggers entries.
//
// Casing and spacing are settled HERE rather than at match time because a trigger is compared
// against a draft on every keystroke: folding an authored phrase once at load is the difference
// between a matcher that reads a normalised corpus and one that re-normalises it per frame. The
// clip runs before the dedupe so two phrases that survive it identically collapse to one.
func normalizeTriggers(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	phrases := make([]string, 0, min(len(raw), maxTriggers))
	seen := make(map[string]bool, len(raw))
	for _, phrase := range raw {
		phrase = strings.ToLower(strings.Join(strings.Fields(phrase), " "))
		if r := []rune(phrase); len(r) > maxTriggerLen {
			phrase = string(r[:maxTriggerLen])
		}
		if phrase == "" || seen[phrase] {
			continue
		}
		seen[phrase] = true
		phrases = append(phrases, phrase)
		if len(phrases) == maxTriggers {
			break
		}
	}
	return phrases
}

// parseSkill turns one SKILL.md's content into a Skill, deriving the ID from dirName when the
// frontmatter omits it. It has two paths: frontmatter (the canonical case, itself strict-then-
// lenient — see parseFrontmatterFields) and a no-frontmatter fallback (the oracle's leniency —
// id from the folder, displayName/summary sniffed from the first lines). Either path must yield
// a non-empty id, displayName, AND summary, else the skill is rejected so the caller skips it
// with a soft error. Load sets Dir; parseSkill leaves it "".
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
	fm, err := parseFrontmatterFields(fmText)
	if err != nil {
		return Skill{}, err
	}
	id := firstNonEmpty(fm.ID, fm.Name, dirName)
	summary := firstNonEmpty(fm.Summary, fm.Description)
	return validate(Skill{
		ID:          strings.TrimSpace(id),
		DisplayName: strings.TrimSpace(firstNonEmpty(fm.DisplayName, titleCase(id))),
		Summary:     clampSummary(summary),
		Description: strings.TrimSpace(summary),
		Body:        strings.TrimSpace(body),
		Triggers:    normalizeTriggers(fm.Triggers),
	})
}

// parseFrontmatterFields reads the recognised keys out of a frontmatter block, strictly first
// and leniently second.
//
// Strict YAML is the canonical path and runs unchanged, so a well-formed block keeps exactly its
// YAML meaning — quoting, block scalars, comments, the lot. The lenient scan runs ONLY when
// yaml.v3 rejects the block, and reads it as what SKILL.md frontmatter almost always is in
// practice: a flat run of "key: value" lines. That recovers the everyday authoring slips a human
// would not even call ambiguous — an unquoted value containing ": ", a tab indent, an unbalanced
// quote — each of which a strict parser can only answer by making the entire skill disappear.
//
// The trade is deliberate. These files are shared with tools whose parsers are more forgiving, so
// a skill another tool lists must not silently vanish here; apogee would rather load a skill whose
// frontmatter is sloppy than withhold one whose intent is plain. Nothing is lost when the block IS
// valid — the strict result is used untouched, and only a hard YAML failure reaches the scan.
//
// When even the scan finds no recognised key, the original YAML error is returned rather than the
// scan's silence: it names the actual line and fault, which is the more useful thing to print.
func parseFrontmatterFields(text string) (frontmatter, error) {
	var strict frontmatter
	strictErr := yaml.Unmarshal([]byte(text), &strict)
	if strictErr == nil {
		return strict, nil
	}
	if lenient, ok := scanFrontmatterFields(text); ok {
		return lenient, nil
	}
	return frontmatter{}, fmt.Errorf("malformed YAML frontmatter: %w", strictErr)
}

// scanFrontmatterFields is the lenient reader: it walks the block line by line, taking each
// recognised "key: value" as a field and folding every following unkeyed line into it (the shape
// a wrapped value takes). A folded line that is a block-sequence entry ("- phrase") is recorded as
// an ITEM of the open key as well, so a list-shaped triggers: yields the phrases the author wrote
// rather than the one run-on value folding them together makes. A line that is wholly a comment is
// dropped, but a "#" INSIDE a value stays literal here — the strict parser already had its say,
// and on this path the author's text is more trustworthy than YAML's comment rule. Keys are
// lowercased, so displayName and displayname both land.
//
// Two rules keep a recovery from inventing meaning. FIRST DECLARATION WINS, so a repeated key
// cannot overwrite the one the author led with. And any key outside recognisedKeys CLOSES the
// open field rather than extending it, which is what stops a nested block ("metadata:" and its
// indented children) from being folded into the description above it.
//
// ok reports whether any NAMING key came back with a value (hasNamingField); false means there is
// nothing here worth preferring over the strict parser's error.
func scanFrontmatterFields(text string) (frontmatter, bool) {
	values := map[string]string{}
	items := map[string][]string{}
	openKey := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := keyLineRe.FindStringSubmatch(line)
		if m == nil { // not a key line — continuation text for whatever field is open
			if openKey == "" {
				continue
			}
			if item, isItem := sequenceItem(line); isItem {
				items[openKey] = append(items[openKey], item)
			}
			values[openKey] = strings.TrimSpace(values[openKey] + " " + line)
			continue
		}
		key := strings.ToLower(m[1])
		if _, declared := values[key]; declared || !recognisedKeys[key] {
			openKey = "" // a repeat, or a key we do not model: end the run rather than absorb it
			continue
		}
		values[key] = stripBlockScalar(unquoteValue(strings.TrimSpace(m[2])))
		openKey = key
	}
	fm := frontmatter{
		ID:          values["id"],
		Name:        values["name"],
		DisplayName: values["displayname"],
		Summary:     values["summary"],
		Description: values["description"],
		Triggers:    splitTriggers(items["triggers"], values["triggers"]),
	}
	return fm, fm.hasNamingField()
}

// sequenceItem reports whether a continuation line is a YAML block-sequence entry ("- phrase"),
// returning the entry's own text. The scan needs the items apart from the folded value because a
// list is the ordinary shape of triggers:, and folding one glues every phrase of it into a single
// run-on trigger no draft can ever match. The entry is unquoted like any other scanned value, so a
// phrase the author quoted for YAML's benefit keeps its text and loses its quotes; an entry with
// nothing after the dash is no phrase at all and is left to the fold.
func sequenceItem(line string) (string, bool) {
	rest, isEntry := strings.CutPrefix(line, "-")
	if !isEntry || (rest != "" && rest[0] != ' ' && rest[0] != '\t') {
		return "", false
	}
	item := unquoteValue(strings.TrimSpace(rest))
	return item, item != ""
}

// stripBlockScalar drops a value that is only a block-scalar indicator, so the lines folded in
// after it become the value rather than trailing a stray "|" or ">-".
func stripBlockScalar(v string) string {
	if blockScalarRe.MatchString(v) {
		return ""
	}
	return v
}

// unquoteValue strips one layer of surrounding quotes, so a value the author quoted for YAML's
// benefit ("execute: a path") does not carry its quotes through the lenient path, which otherwise
// takes the line verbatim.
//
// A LONE opening quote is stripped too — that unclosed quote is the very fault that sent the
// block down this path, and leaving it would put a stray character in the menu. The strip is
// gated on the quote being the only one in the value, so a value that merely begins with a quoted
// phrase (`"hello" world`) keeps every character it has.
func unquoteValue(s string) string {
	if len(s) < 2 {
		return s
	}
	for _, quote := range []byte{'"', '\''} {
		if s[0] != quote {
			continue
		}
		if s[len(s)-1] == quote {
			return s[1 : len(s)-1]
		}
		if strings.Count(s, string(quote)) == 1 {
			return s[1:]
		}
	}
	return s
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
		Description: summary,
		Body:        strings.TrimSpace(content),
	})
}

// validate rejects a skill missing any load-bearing field — without an id it cannot be
// attached, without a displayName/summary it cannot be shown, without a body there is nothing
// to inject — so the loader skips it rather than surfacing a half-blank, contentless entry in
// the menu. The body check is what turns an empty/whitespace SKILL.md into a skip: the
// fallback would otherwise name it after its folder and load a skill with nothing to say.
//
// It then rejects an id that is not a single token (badIDRune), which is a REFUSAL rather than a
// completeness check: both fields a skill is named by come from an untrusted SKILL.md — or from a
// directory name in an untrusted repo — and an id carrying interior whitespace is a command line,
// not a name.
func validate(s Skill) (Skill, error) {
	if s.ID == "" || s.DisplayName == "" || s.Summary == "" || s.Body == "" {
		return Skill{}, fmt.Errorf("skill is missing a required field (id=%q displayName=%q summary=%q body-empty=%v)",
			s.ID, s.DisplayName, s.Summary, s.Body == "")
	}
	if r, bad := badIDRune(s.ID); bad {
		return Skill{}, fmt.Errorf("skill id %q contains U+%04X: an id is ONE token — no whitespace, no control characters", s.ID, r)
	}
	return s, nil
}

// badIDRune reports the first rune of an id that may not be in one, and whether there was such a
// rune. A skill id is invoked as a "/id" token in the chat line, and the command parser cuts that
// line at its first space or tab and looks only the leading piece up in its verb registry — so an
// id like "confine off --save" is a NAME to this package and a COMMAND LINE to the parser. That
// mismatch is the whole reason for the check: a repo-supplied SKILL.md must not be able to write a
// command into the human's composer under the guise of naming itself.
//
// Control characters are refused for the neighbouring reason: they take no display cell, so an id
// carrying one shows in the menu as something shorter than what it is, and a newline splits a
// rendered row outright. Both classes are refused at the earliest place they can be — the loader
// skips the skill and records the refusal, so the id never enters the catalog at all.
func badIDRune(id string) (rune, bool) {
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return r, true
		}
	}
	return 0, false
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
