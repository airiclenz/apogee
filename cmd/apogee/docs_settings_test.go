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

// manualPage is docs/manual/configuration.md read once — the whole body, and the fenced blocks
// pulled out of it, since the third arm of the predicate looks only inside those.
type manualPage struct {
	body   string
	fences []string
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
	return page
}

// manualDocumentsSetting is the gate's predicate, and it accepts the spellings the page actually
// uses rather than only the registry's dotted Path:
//
//   - the dotted path, back-ticked — `ui.stall-after`;
//   - the leaf segment, back-ticked — `max-age`, which is how the manual names a key while it is
//     already talking about the block that holds it;
//   - the leaf spelled `<leaf>:` inside a fenced config example beneath its own block heading —
//     `present:` with `auto-open:` indented under it, or a top-level key at column 0.
//
// Both back-ticked arms accept an optional trailing colon inside the back-ticks: `mcp-servers:` is
// the manual's house spelling and `mcp-servers` never occurs, so without the colon a dozen
// correctly documented keys would fail every arm.
//
// The leaf arm is deliberately loose — it does not check WHICH block's paragraph the back-ticked
// leaf sits in, so two blocks sharing a leaf name vouch for each other. That is the price of
// reading the page the way it is written; the strictness that matters is the first direction, that
// no registry key goes unmentioned.
func manualDocumentsSetting(manual manualPage, path string) bool {
	leaf := path
	if _, after, nested := strings.Cut(path, "."); nested {
		leaf = after
	}
	return backTickedKey(manual.body, path) ||
		backTickedKey(manual.body, leaf) ||
		settingSpelledInFence(manual.fences, path)
}

// backTickedKey is the first two arms: `<name>` or `<name>:`.
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

	block := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(parent) + `:[ \t]*$`)
	under := regexp.MustCompile(`(?m)^[ \t]+` + regexp.QuoteMeta(leaf) + `:`)
	for _, fence := range fences {
		if block.MatchString(fence) && under.MatchString(fence) {
			return true
		}
	}
	return false
}
