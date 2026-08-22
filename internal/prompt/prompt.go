// Package prompt owns the system-prompt template language (ADR 0023): exactly four
// placeholders — {{workspace}}, {{datetime}}, {{mode}}, {{scratch}} — validated once at
// startup / construction and rendered fresh per request by the loop.
//
// The language is closed on purpose: the placeholder set is the whole vocabulary, the
// spelling is strict (a near-miss like "{{ workspace }}" is an unknown placeholder, not a
// silent literal), and an unknown placeholder is a startup error listing the known four
// rather than un-rendered braces shipped to the model. Validation and rendering are split
// because they happen at different times: cmd/apogee validates the configured template
// once while resolving config, internal/agent renders it per request from live inputs (the
// autonomy mode changes on a mode switch, the date changes at midnight).
//
// {{datetime}} deliberately renders the DATE only. A per-request timestamp would change
// the first system message every turn and bust the prefix KV cache local llama.cpp servers
// rely on; a date is stable within a day.
//
// The package imports stdlib only — not even internal/domain (Inputs.Mode is a plain
// string) — so both the host and the engine can use it with no import cycle.
package prompt

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The four placeholders the template language knows, in the order KnownList reports them.
// The spellings are the exact tokens matched and substituted: no whitespace tolerance, no
// case folding.
const (
	PlaceholderWorkspace = "{{workspace}}"
	PlaceholderDatetime  = "{{datetime}}"
	PlaceholderMode      = "{{mode}}"
	PlaceholderScratch   = "{{scratch}}"
)

// dateLayout renders {{datetime}} as a date with no time-of-day, so the rendered prompt is
// byte-stable within a day and the model server's prefix cache survives a turn.
const dateLayout = "2006-01-02"

// knownPlaceholders is the closed placeholder set, driving both the Validate check and the
// KnownList error text from one source.
var knownPlaceholders = []string{PlaceholderWorkspace, PlaceholderDatetime, PlaceholderMode, PlaceholderScratch}

// placeholderPattern matches any {{…}} token, known or not, so Validate can reject the
// unknown ones. The inner class excludes braces so a stray or unclosed brace is not folded
// into a neighbouring token; such text is not a placeholder and passes through verbatim.
var placeholderPattern = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// KnownList returns the known placeholders as one comma-joined string —
// "{{workspace}}, {{datetime}}, {{mode}}, {{scratch}}" — for error messages, so
// cmd/apogee and the engine name the same set in the same words.
func KnownList() string { return strings.Join(knownPlaceholders, ", ") }

// Inputs are the per-request render inputs. Mode is a plain string (the autonomy mode
// label) so this package imports no domain type, and Now is rendered date-only.
type Inputs struct {
	Workspace string    // Config.WorkspaceDir
	Mode      string    // string(agent.Mode()) — live, changes on a mode switch
	Now       time.Time // rendered DATE-ONLY (2006-01-02): KV-cache-stable within a day
	Scratch   string    // agent.ScratchDir() — per-session constant, so KV-cache-stable
}

// Validate rejects a template carrying an unknown {{…}} placeholder, naming the first
// offender and listing the known four. Spelling is strict, so "{{ workspace }}" and
// "{{WORKSPACE}}" are unknown; literal or unclosed braces are not placeholders and are
// accepted. An empty template validates: it configures no prompt.
func Validate(template string) error {
	for _, token := range placeholderPattern.FindAllString(template, -1) {
		if !slices.Contains(knownPlaceholders, token) {
			return fmt.Errorf("prompt: unknown placeholder %q; known placeholders: %s", token, KnownList())
		}
	}
	return nil
}

// Render substitutes every occurrence of the four placeholders. It assumes Validate has
// passed; an unknown placeholder — unreachable after validation — passes through verbatim
// rather than failing the request (the InstructionsFor defensive-degrade posture).
func Render(template string, in Inputs) string {
	rendered := strings.ReplaceAll(template, PlaceholderWorkspace, in.Workspace)
	rendered = strings.ReplaceAll(rendered, PlaceholderDatetime, in.Now.Format(dateLayout))
	rendered = strings.ReplaceAll(rendered, PlaceholderMode, in.Mode)
	return strings.ReplaceAll(rendered, PlaceholderScratch, in.Scratch)
}
