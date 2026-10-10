package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/adoption"
)

// The `allow:` key — Allow rules (ADR 0096 §2, §4; CONTEXT.md "Allow rule", "Adoption")
//
// An Allow rule is a persisted yes that answers an ordinary Approval: a word prefix for `terminal`,
// a whole server for an MCP server. The key is the one granting-class key, and its two layers are
// trusted differently. A GLOBAL rule is the user's own, written by hand in `~/.apogee/config.yaml`,
// and live as written. A PROJECT rule sits in a file that may be committed, pulled or written
// behind apogee's back, so it is carried apart from the layered merge (layer.go) and is live only
// while the user's adoption record (internal/adoption) pins its exact text; every other project
// rule is reported as proposed or rejected, and grants nothing.

// AllowKind is the granting key an Allow rule sits under — the tool family it answers for.
type AllowKind string

const (
	// AllowTerminal rules are word prefixes of a `terminal` command line.
	AllowTerminal AllowKind = "terminal"
	// AllowMCPServers rules each name one MCP server whose every tool call they answer.
	AllowMCPServers AllowKind = "mcp-servers"
)

// workspacesDirName is the folder under the apogee home that keeps each Project root's adoption
// record (ADR 0096 §4).
const workspacesDirName = "workspaces"

// adoptionUnreadNotice is the line a project file whose rules cannot be classified costs: they all
// stay proposed — inert — and the session says why.
const adoptionUnreadNotice = "apogee: project config %s: cannot read the adoption record: %v; " +
	"its allow rules stay proposed"

// AllowRule is one Allow rule: the key it sits under, its text exactly as the file spells it — the
// text an adoption pins — and the layer it came from, SourceGlobal or SourceProject.
type AllowRule struct {
	Kind  AllowKind
	Text  string
	Layer Source
}

// AllowRules is the `allow:` key resolved. Rules is the effective set — every global rule, then
// every adopted project rule, each in file order — and is the only list that grants anything.
// Proposed holds the project rules no answer is recorded for, and Rejected the ones the user turned
// down; both are inert and are reported so a surface can offer or list them. The zero value grants
// nothing.
type AllowRules struct {
	Rules    []AllowRule
	Proposed []AllowRule
	Rejected []AllowRule
}

// allowConfig is the on-disk `allow:` block: the terminal word prefixes and the MCP server names
// that run without asking at an ordinary gate.
type allowConfig struct {
	Terminal   []string `yaml:"terminal"`
	MCPServers []string `yaml:"mcp-servers"`
}

// workspacesDirOf is the adoption records' folder beside the global config file at globalPath, or
// "" when no global path is known.
func workspacesDirOf(globalPath string) string {
	if globalPath == "" {
		return ""
	}
	return WorkspacesDir(filepath.Dir(globalPath))
}

// WorkspacesDir is where the apogee home configDir keeps each Project root's adoption record, or ""
// for an unknown home — the one spelling of `~/.apogee/workspaces` the adoption store and the
// Project config writer are both handed.
func WorkspacesDir(configDir string) string {
	if configDir == "" {
		return ""
	}
	return filepath.Join(configDir, workspacesDirName)
}

// fileAllowRules projects `allow:` — the file's rules, tagged as the global layer's. A blank rule
// refuses the start naming it: an empty prefix would match every command, which no one writes on
// purpose. The Project config's rules are not read here: the layered load carries them apart
// (projectAllowRules), and a project file reaches this pass only to be checked alone.
func fileAllowRules(o *Options, fc fileConfig) error {
	o.AllowRules = AllowRules{}
	if fc.Allow == nil {
		return nil
	}
	rules, err := fc.Allow.rules(SourceGlobal)
	if err != nil {
		return err
	}
	o.AllowRules.Rules = rules
	return nil
}

// rules converts the block's entries into rules tagged with layer, terminal rules first, each list
// in file order.
//
// Errors: an error naming the list and the entry for a rule that is empty or only whitespace.
func (ac *allowConfig) rules(layer Source) ([]AllowRule, error) {
	out := make([]AllowRule, 0, len(ac.Terminal)+len(ac.MCPServers))
	for _, list := range []struct {
		kind    AllowKind
		entries []string
	}{{AllowTerminal, ac.Terminal}, {AllowMCPServers, ac.MCPServers}} {
		for i, text := range list.entries {
			if strings.TrimSpace(text) == "" {
				return nil, fmt.Errorf("apogee: allow.%s entry %d is empty — a rule names what it allows",
					list.kind, i+1)
			}
			out = append(out, AllowRule{Kind: list.kind, Text: text, Layer: layer})
		}
	}
	return out, nil
}

// projectAllowRules is a Project config's rules sorted by the user's recorded answers.
type projectAllowRules struct {
	adopted  []AllowRule
	proposed []AllowRule
	rejected []AllowRule
}

// allowEntries is the adoption entries rules spell — the kind and the exact text an answer pins.
func allowEntries(rules []AllowRule) []adoption.Entry {
	if len(rules) == 0 {
		return nil
	}
	entries := make([]adoption.Entry, len(rules))
	for i, r := range rules {
		entries[i] = adoption.Entry{Kind: string(r.Kind), Text: r.Text}
	}
	return entries
}

// classifyProjectRules sorts the project file's rules (at path, under projectRoot) by the adoption
// record kept under workspacesDir. A record that cannot be read leaves every rule proposed — inert
// — with one notice: a rule that cannot be shown to be adopted is not.
func classifyProjectRules(entries []adoption.Entry, path, workspacesDir, projectRoot string,
	notify func(string)) projectAllowRules {
	if len(entries) == 0 {
		return projectAllowRules{}
	}
	store, err := adoption.New(workspacesDir, projectRoot)
	var sorted adoption.Classification
	if err == nil {
		sorted, err = store.Classify(entries)
	}
	if err != nil {
		notify(fmt.Sprintf(adoptionUnreadNotice, path, err))
		return projectAllowRules{proposed: projectRulesOf(entries)}
	}
	return projectAllowRules{
		adopted:  projectRulesOf(sorted.Adopted),
		proposed: projectRulesOf(sorted.Proposed),
		rejected: projectRulesOf(sorted.Rejected),
	}
}

// projectRulesOf turns classified adoption entries back into project-layer rules.
func projectRulesOf(entries []adoption.Entry) []AllowRule {
	if len(entries) == 0 {
		return nil
	}
	out := make([]AllowRule, len(entries))
	for i, e := range entries {
		out[i] = AllowRule{Kind: AllowKind(e.Kind), Text: e.Text, Layer: SourceProject}
	}
	return out
}

// effective is the resolved `allow:` key: the global rules the file pass read, then the adopted
// project rules, with the rest of the project's rules reported beside them.
func (p projectAllowRules) effective(global []AllowRule) AllowRules {
	rules := make([]AllowRule, 0, len(global)+len(p.adopted))
	rules = append(append(rules, global...), p.adopted...)
	if len(rules) == 0 {
		rules = nil
	}
	return AllowRules{Rules: rules, Proposed: p.proposed, Rejected: p.rejected}
}

// allowRulesSummary summarizes `allow:` by what it grants and what waits on an answer — "3 rules,
// 1 proposed"; empty when there are no rules at all.
func allowRulesSummary(set AllowRules) string {
	parts := make([]string, 0, 2)
	if live := countSummary(len(set.Rules), "rule"); live != "" {
		parts = append(parts, live)
	}
	if proposed := len(set.Proposed); proposed > 0 {
		parts = append(parts, fmt.Sprintf("%d proposed", proposed))
	}
	return strings.Join(parts, ", ")
}
