package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
)

// ----------------------------------------------------------------------------
// The Allow-rule acts of tui.ConfigHost (ADR 0096 §4)
// ----------------------------------------------------------------------------
//
// Four answers the human gives about a project rule, each one write to the Project config or to
// its adoption record (or both) followed by the same settling: the external-edit baseline is
// re-taken, because apogee's own write is not an edit the watcher should report back (ADR 0041
// decision 8), and the effective Allow rules are re-resolved from the files and installed on the
// session — the live-settings holder and the engine, whose rule set the whole agent tree shares, so
// a sub-agent already running reads the new rule at its next ordinary gate.
//
// The order of the two writes is always the safe one. An adoption is recorded only after the file
// holds the rule, and dropped before the file loses it, so a failure part-way leaves a rule
// PROPOSED — inert — and never a recorded yes to text no file holds: such a yes would answer the
// first teammate's commit that happened to spell it.

// AddProjectRule is the "Always in this project…" answer: the rule into the Project config, its
// exact text adopted, the effective rules settled.
func (h configHost) AddProjectRule(kind apogee.AllowRuleKind, text string) error {
	return h.w.addProjectRule(kind, text)
}

// AdoptRules records the human's adoption of project rules the file already holds, and settles.
func (h configHost) AdoptRules(rules []apogee.AllowRule) error {
	return h.w.answerProjectRules(rules, (*adoption.Store).Adopt)
}

// RejectRules records the human's rejection of project rules, and settles: a rule that was in force
// leaves the effective set.
func (h configHost) RejectRules(rules []apogee.AllowRule) error {
	return h.w.answerProjectRules(rules, (*adoption.Store).Reject)
}

// RemoveRule takes a project rule out of the Project config, its recorded answer first, and
// settles. A global rule is refused: it is the human's own line in the global config.
func (h configHost) RemoveRule(rule apogee.AllowRule) error {
	return h.w.removeProjectRule(rule)
}

// addProjectRule writes the rule, adopts it, and settles. A rule the list already holds writes
// nothing and is adopted all the same.
//
// Errors: the writer's refusals (config.AddProjectAllowRule), a store that cannot be opened or
// written — the rule is then in the file but proposed, and the error says so — and a settling that
// cannot re-read the config files.
func (w *rootWiring) addProjectRule(kind apogee.AllowRuleKind, text string) error {
	ck, err := configAllowKind(kind)
	if err != nil {
		return err
	}
	path, err := config.AddProjectAllowRule(w.roots.project, w.workspacesDir(), ck, text)
	if err != nil {
		return err
	}
	if err := w.recordAnswer([]adoption.Entry{{Kind: string(ck), Text: text}}, (*adoption.Store).Adopt); err != nil {
		// The file moved even though the record did not, so the baseline must follow it.
		return errors.Join(fmt.Errorf(
			"apogee: %s holds the rule, but its adoption was not recorded, so it stays proposed: %w", path, err),
			w.settleAllowRules())
	}
	return w.settleAllowRules()
}

// answerProjectRules records answer for rules — every one a project rule — and settles.
//
// Errors: a refusal naming a rule that is not a project rule, the store's errors, and the
// settling's.
func (w *rootWiring) answerProjectRules(rules []apogee.AllowRule, answer adoptionAnswer) error {
	entries := make([]adoption.Entry, 0, len(rules))
	for _, r := range rules {
		e, err := projectRuleEntry(r)
		if err != nil {
			return err
		}
		entries = append(entries, e)
	}
	if err := w.recordAnswer(entries, answer); err != nil {
		return err
	}
	return w.settleAllowRules()
}

// removeProjectRule forgets the rule's answer, then takes it out of the Project config, and
// settles.
//
// Errors: a refusal for a global rule, the store's errors (the file is then untouched), the
// writer's refusals (the rule is then in the file but proposed, and the error says so), and the
// settling's.
func (w *rootWiring) removeProjectRule(rule apogee.AllowRule) error {
	if rule.Layer != apogee.AllowRuleProject {
		return fmt.Errorf("apogee: the %s is in your global config, %s; remove it there by hand",
			rule, w.configPath())
	}
	entry, err := projectRuleEntry(rule)
	if err != nil {
		return err
	}
	if err := w.recordAnswer([]adoption.Entry{entry}, (*adoption.Store).Forget); err != nil {
		return err
	}
	if _, err := config.RemoveProjectAllowRule(w.roots.project, w.workspacesDir(), config.AllowKind(entry.Kind),
		entry.Text); err != nil {
		// The record moved even though the file did not, so the rules in force must follow it.
		return errors.Join(fmt.Errorf("apogee: the rule's adoption was dropped, so it no longer applies, "+
			"but it is still in the project config: %w", err), w.settleAllowRules())
	}
	return w.settleAllowRules()
}

// adoptionAnswer is one of the adoption store's three record writes — Adopt, Reject or Forget.
type adoptionAnswer func(*adoption.Store, ...adoption.Entry) error

// recordAnswer opens this Project root's adoption record and writes answer for entries into it.
func (w *rootWiring) recordAnswer(entries []adoption.Entry, answer adoptionAnswer) error {
	store, err := adoption.New(w.workspacesDir(), w.roots.project)
	if err != nil {
		return err
	}
	return answer(store, entries...)
}

// settleAllowRules is the settling every act ends with: the external-edit baseline re-taken, then
// the effective Allow rules re-resolved from the two files and the adoption record — the resolution
// a relaunch would make — and installed on the live-settings holder and the engine.
//
// Errors: a global config that no longer resolves; nothing is installed then, and the rules in
// force stay as they were.
func (w *rootWiring) settleAllowRules() error {
	if w.externalEdits != nil {
		w.externalEdits.refresh()
	}
	resolved, err := config.LoadLayeredConfig(w.configPath(), w.roots.project, os.ReadFile, func(string) {})
	if err != nil {
		return fmt.Errorf("apogee: the answer is saved, but the rules in force could not be refreshed: %w", err)
	}
	if w.live != nil {
		w.live.update(func(o *config.Options) { o.AllowRules = resolved.AllowRules })
	}
	if w.engine != nil {
		w.engine.SetAllowRules(allowRulesFromOptions(resolved))
	}
	return nil
}

// workspacesDir is where this run keeps each Project root's adoption record and the Project config
// writer's lock: `~/.apogee/workspaces`.
func (w *rootWiring) workspacesDir() string { return config.WorkspacesDir(w.roots.config) }

// projectRuleEntry is the adoption entry a project rule pins.
//
// Errors: a refusal for a rule that is not a project rule, or of a kind the `allow:` key lacks.
func projectRuleEntry(rule apogee.AllowRule) (adoption.Entry, error) {
	if rule.Layer != apogee.AllowRuleProject {
		return adoption.Entry{}, fmt.Errorf("apogee: the %s is not a project rule; only those are adopted", rule)
	}
	kind, err := configAllowKind(rule.Kind)
	if err != nil {
		return adoption.Entry{}, err
	}
	return adoption.Entry{Kind: string(kind), Text: rule.Text}, nil
}

// configAllowKind is the `allow:` list an engine rule kind is written under.
func configAllowKind(kind apogee.AllowRuleKind) (config.AllowKind, error) {
	switch kind {
	case apogee.AllowRuleTerminal:
		return config.AllowTerminal, nil
	case apogee.AllowRuleMCPServer:
		return config.AllowMCPServers, nil
	}
	return "", errors.New("apogee: an allow rule is a terminal rule or an MCP-server rule; this one is neither")
}
