package main

// The drift gate over internal/config's settings registry. Nothing cross-checked the registry
// against docs/manual/configuration.md before this file, which is how ten keys drifted out of the
// manual at once. It is the third of the repo's hand-maintained-list gates and copies the two that
// came before it — internal/tools' TestManualListsEveryKnownToolName and this package's
// TestManualListsEveryEnvironmentOverride — in path constant, failure-message shape and the plain
// "read the file, grep it" reading.
//
// The path constant (manualConfigPath) and the quoting helper (firstSentence) are the env gate's,
// in this same package: they are reused here, never redeclared.

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
)

// settingsDocumentedElsewhere is the allowlist, and it is deliberately narrow: it is ONLY for a
// registry key the manual documents on a page other than configuration.md. Each entry carries a
// one-line reason naming that page, so the list stays auditable rather than becoming a silencer —
// a key that is simply undocumented gets a sentence in configuration.md instead of a row here.
//
// Empty today: every key in config.KeyRegistry is named on the configuration page.
var settingsDocumentedElsewhere = map[string]string{}

// fencedBlock captures the body of one fenced code block — the config examples the manual shows a
// key inside when the prose around them names the block rather than the key.
var fencedBlock = regexp.MustCompile("(?s)```[^\n]*\n(.*?)```")

// TestManualDocumentsEverySettingsKey pins the settings reference to the registry that IS the
// schema: every Path in config.KeyRegistry is documented on docs/manual/configuration.md, and a key
// added to the registry (which the bijection guard already ties to fileConfig) fails here until the
// manual gains it too.
func TestManualDocumentsEverySettingsKey(t *testing.T) {
	t.Parallel()

	manual := readManualConfig(t)

	if len(config.KeyRegistry) == 0 {
		t.Fatal("config.KeyRegistry is empty; the scan has stopped working")
	}

	var missing []string
	for _, key := range config.KeyRegistry {
		documented := manualDocumentsSetting(manual, key.Path)
		if reason, allowed := settingsDocumentedElsewhere[key.Path]; allowed {
			if reason == "" {
				t.Errorf("the allowlist exempts %s with no reason; every entry must name the page that documents it", key.Path)
			}
			if documented {
				t.Errorf("the allowlist exempts %s as %q, but %s documents it after all; drop the entry",
					key.Path, reason, manualConfigPath)
			}
			continue
		}
		if !documented {
			missing = append(missing, key.Path)
		}
	}

	if len(missing) > 0 {
		t.Errorf("%s documents no %s; the settings reference has fallen behind config.KeyRegistry. "+
			"Document each key beside its own block, or — only when another manual page documents it — "+
			"add it to settingsDocumentedElsewhere with the reason naming that page. The page begins: %s",
			manualConfigPath, strings.Join(missing, ", "), firstSentence(manual.body))
	}
}

// TestManualDocumentsEverySettingsKeyRejectsAnUndocumentedKey is the gate's negative case: the
// predicate says no to a key the manual has never heard of. It feeds FABRICATED paths to the real
// predicate over the real page rather than mutating the registry, so the gate itself is proved
// without the gate's subject moving.
func TestManualDocumentsEverySettingsKeyRejectsAnUndocumentedKey(t *testing.T) {
	t.Parallel()

	manual := readManualConfig(t)

	for _, path := range []string{
		"no-such-setting",
		"no-such-block.no-such-key",
		"ui.no-such-key",
	} {
		if manualDocumentsSetting(manual, path) {
			t.Errorf("the predicate calls %q documented; no such key is on %s, so the gate cannot fail",
				path, manualConfigPath)
		}
		if _, allowed := settingsDocumentedElsewhere[path]; allowed {
			t.Errorf("the allowlist carries the fabricated path %q", path)
		}
	}
}

// TestManualDocumentsEverySettingsKeyRejectsALeafBorrowedFromAnotherBlock pins the scope of the
// leaf arm. Every path here is fabricated, and every one of them has its leaf back-ticked SOMEWHERE
// on the real page — under a different block. That is the collision the loose arm used to wave
// through (`context-files.enable` passing off `validated-sets:`' own `enable:`), so the case is a
// gate on the scoping and not merely another undocumented-key case: the test first asserts the leaf
// really is on the page, then that the predicate still says no.
func TestManualDocumentsEverySettingsKeyRejectsALeafBorrowedFromAnotherBlock(t *testing.T) {
	t.Parallel()

	manual := readManualConfig(t)

	for _, borrowed := range []struct {
		path string
		from string
	}{
		{path: "context-files.alias", from: "validated-sets:"},
		{path: "validated-sets.names", from: "context-files:"},
		{path: "ui.max-age", from: "sessions:"},
		{path: "sessions.enable", from: "validated-sets: and context-files:"},
	} {
		_, leaf, _ := strings.Cut(borrowed.path, ".")
		if !backTickedKey(manual.body, leaf) {
			t.Errorf("%s no longer spells `%s` anywhere, so %q pins nothing; pick a leaf the page "+
				"still documents under another block", manualConfigPath, leaf, borrowed.path)
			continue
		}
		if manualDocumentsSetting(manual, borrowed.path) {
			t.Errorf("the predicate calls %q documented; its leaf `%s` is documented under %s, not "+
				"under its own block, so the leaf arm is reading outside the block's section",
				borrowed.path, leaf, borrowed.from)
		}
	}
}

// manualPage is docs/manual/configuration.md read once — the whole body, the fenced blocks pulled
// out of it (the predicate's third arm looks only inside those), and the body cut into its `##`
// sections (the second arm looks only inside the section that documents the key's own block).
type manualPage struct {
	body     string
	fences   []string
	sections []string
}

// readManualConfig reads the settings reference. The repo layout is fixed and `go test` runs in the
// package directory, so a missing page is a failure rather than a reason to skip.
func readManualConfig(t *testing.T) manualPage {
	t.Helper()

	body, err := os.ReadFile(manualConfigPath)
	if err != nil {
		t.Fatalf("read %s: %v", manualConfigPath, err)
	}

	page := manualPage{body: string(body)}
	for _, m := range fencedBlock.FindAllStringSubmatch(page.body, -1) {
		page.fences = append(page.fences, m[1])
	}
	page.sections = splitManualSections(page.body)
	return page
}

// splitManualSections cuts the page at its `##` headings: one section per top-level heading, each
// running to the next one, with whatever precedes the first heading as the opening section. A `###`
// subsection stays inside the `##` section that holds it — the manual documents a block under one
// top-level heading and elaborates it in subsections, so that is the unit "this block's section"
// means. Fenced blocks are tracked while scanning so a `##` line inside a code example could never
// cut a section in two; the page has none today, and this keeps that from becoming load-bearing.
func splitManualSections(body string) []string {
	lines := strings.Split(body, "\n")

	var sections []string
	start, inFence := 0, false
	for i, line := range lines {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence || i == start || !strings.HasPrefix(line, "## ") {
			continue
		}
		sections = append(sections, strings.Join(lines[start:i], "\n"))
		start = i
	}
	return append(sections, strings.Join(lines[start:], "\n"))
}

// manualDocumentsSetting is the gate's predicate, and it accepts the spellings the page actually
// uses rather than only the registry's dotted Path:
//
//   - the dotted path, back-ticked anywhere on the page — `ui.stall-after`;
//   - the leaf segment, back-ticked inside the section that documents the key's OWN block —
//     `max-age`, which is how the manual names a key while it is already talking about the block
//     that holds it;
//   - the leaf spelled `<leaf>:` inside a fenced config example beneath its own block heading —
//     `present:` with `auto-open:` indented under it, or a top-level key at column 0.
//
// Both back-ticked arms accept an optional trailing colon inside the back-ticks: `mcp-servers:` is
// the manual's house spelling and `mcp-servers` never occurs, so without the colon a dozen
// correctly documented keys would fail every arm.
//
// The leaf arm is SCOPED to the parent block's own section, which is what stops two blocks that
// share a leaf name from vouching for each other: `context-files.enable` counts because the
// `context-files:` paragraph spells `enable:` itself, never because `validated-sets:` spells its
// own one screen away. The scope follows the page's own structure rather than a table this file
// would have to maintain — a section documents a block when it names the block back-ticked (`ui:`)
// or shows it as a block line in one of its fenced examples (`present:`) — so a block documented
// across two sections is vouched for by either, and a leaf outside both is not documented at all.
//
// A top-level key is its own leaf and the whole page is its section, so only the first and third
// arms apply to it.
func manualDocumentsSetting(manual manualPage, path string) bool {
	if backTickedKey(manual.body, path) {
		return true
	}
	if parent, leaf, nested := strings.Cut(path, "."); nested && leafKeyInItsOwnSection(manual, parent, leaf) {
		return true
	}
	return settingSpelledInFence(manual.fences, path)
}

// leafKeyInItsOwnSection is the second arm: the back-ticked leaf counts only where it sits in a
// section that is talking about the block that holds it.
func leafKeyInItsOwnSection(manual manualPage, parent, leaf string) bool {
	block := blockLine(parent)
	for _, section := range manual.sections {
		if sectionDocumentsBlock(section, parent, block) && backTickedKey(section, leaf) {
			return true
		}
	}
	return false
}

// sectionDocumentsBlock says whether one section is the place the manual documents `parent:` — it
// either names the block in prose or shows it as a block line in one of its own examples.
func sectionDocumentsBlock(section, parent string, block *regexp.Regexp) bool {
	if backTickedKey(section, parent) {
		return true
	}
	for _, m := range fencedBlock.FindAllStringSubmatch(section, -1) {
		if block.MatchString(m[1]) {
			return true
		}
	}
	return false
}

// blockLine matches a block's own line inside a fenced example — `present:` with its keys indented
// under it. It is the same shape settingSpelledInFence uses for a nested key's parent, spelled once
// here and shared, so the two places that ask "is this fence showing that block?" cannot drift.
func blockLine(parent string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(parent) + `:[ \t]*$`)
}

// backTickedKey is the spelling both back-ticked arms look for, in whatever text they are given
// — the whole page for the dotted path, one section for the leaf: `<name>` or `<name>:`.
func backTickedKey(body, name string) bool {
	return strings.Contains(body, "`"+name+"`") || strings.Contains(body, "`"+name+":`")
}

// settingSpelledInFence is the third arm. A nested key needs its parent block's own line in the
// same fence, with the leaf indented under it; a top-level key is the fence's own column-0 line.
func settingSpelledInFence(fences []string, path string) bool {
	parent, leaf, nested := strings.Cut(path, ".")
	if !nested {
		atRoot := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(path) + `:`)
		for _, fence := range fences {
			if atRoot.MatchString(fence) {
				return true
			}
		}
		return false
	}

	block := blockLine(parent)
	under := regexp.MustCompile(`(?m)^[ \t]+` + regexp.QuoteMeta(leaf) + `:`)
	for _, fence := range fences {
		if block.MatchString(fence) && under.MatchString(fence) {
			return true
		}
	}
	return false
}
