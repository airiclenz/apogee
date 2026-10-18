package main

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/tui"
)

// ----------------------------------------------------------------------------
// The Allow-rule and adoption acts of tui.ConfigHost (ADR 0096 §4, ADR 0104 §11)
// ----------------------------------------------------------------------------
//
// Four answers the human gives about a project rule — two of them, adopt and reject, about every
// kind of entry the Project config proposes (a Proposal: an Allow rule, a repo MCP server or a repo
// reaction) — each one write to the Project config or to
// its adoption record (or both) — and the one act that reaches a global rule, its removal from the
// global config (the `/settings` *Allow rules* section's) — followed by the same settling: the external-edit baseline is
// re-taken, because apogee's own write is not an edit the watcher should report back (ADR 0041
// decision 8), and the effective Allow rules are re-resolved from the files and installed on the
// session — the live-settings holder and the engine, whose rule set the whole agent tree shares, so
// a sub-agent already running reads the new rule at its next ordinary gate.
//
// Beside the four answers is the one read the adoption pane asks after a watched change
// (ProposedRules): the entries the Project config proposes now, resolved exactly as the settling
// resolves the rules in force. An adopted server or reaction is put into force at once, through the
// same door a watched re-read applies the layered set by (applyAdoptedKinds): apogee's own write
// re-takes the baseline, so no watcher report would carry it. And beside that is the live refresh: a watched change to any of the
// three files the rules resolve from — the global config, the Project config, the adoption record —
// that moved them is installed the same way (followReloadedAllowRules), so a hand-edited `allow:`
// or an `apogee project adopt` in another terminal applies without a restart.
//
// A recorded yes to text no file holds would answer the first teammate's commit that happened to
// spell it, so none is written. The order of the two writes keeps apogee's own acts from leaving
// one: an adoption is recorded only after the file holds the rule, and dropped before the file
// loses it, so a failure part-way leaves a rule PROPOSED — inert. An adoption of a rule the file
// already held is re-checked against the file at the moment it is answered (projectRulesHeld): an
// edit that took the rule out since it was offered refuses the yes, and nothing is recorded for it.
// The yes is pinned to the exact text it answered (adoption.Fingerprint) — a structured entry's
// canonical pin, files and all — so an edit that lands after the re-check proposes its entry afresh
// rather than inheriting the answer.

// AddProjectRule is the "Always in this project…" answer: the rule into the Project config, its
// exact text adopted, the effective rules settled.
func (h configHost) AddProjectRule(kind apogee.AllowRuleKind, text string) error {
	return h.w.addProjectRule(kind, text)
}

// AdoptRules records the human's adoption of the Project config's proposals, and settles: an
// adopted server or reaction is put into force at once. An entry the Project config no longer
// holds as it was offered is refused by name and nothing is recorded for it; the rest are adopted.
func (h configHost) AdoptRules(proposals []apogee.Proposal) error {
	return h.w.adoptProjectRules(proposals)
}

// RejectRules records the human's rejection of the Project config's proposals, and settles: a rule
// that was in force leaves the effective set.
func (h configHost) RejectRules(proposals []apogee.Proposal) error {
	return h.w.answerProjectRules(proposals, (*adoption.Store).Reject)
}

// RemoveRule takes a rule out of the file that holds it — a project rule out of the Project config,
// its recorded answer first; a global rule out of the global config — and settles.
func (h configHost) RemoveRule(rule apogee.AllowRule) error {
	if rule.Layer == apogee.AllowRuleGlobal {
		return h.w.removeGlobalRule(rule)
	}
	return h.w.removeProjectRule(rule)
}

// ProposedRules is what the Project config proposes now — its Allow rules, MCP servers and
// reactions no answer is recorded for — for the adoption pane to offer after a watched change. It
// writes nothing.
func (h configHost) ProposedRules() ([]apogee.Proposal, error) {
	return h.w.proposedRules()
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

// answerProjectRules records answer for proposals — every one the Project config's — and settles.
//
// Errors: a refusal naming a proposal that is not one, the store's errors, and the settling's.
func (w *rootWiring) answerProjectRules(proposals []apogee.Proposal, answer adoptionAnswer) error {
	entries, err := proposalEntries(proposals)
	if err != nil {
		return err
	}
	if err := w.recordAnswer(entries, answer); err != nil {
		return err
	}
	return w.settleAllowRules()
}

// adoptProjectRules adopts the proposals — every one the Project config's — it holds at this
// moment, pin for pin, and settles: the rules in force re-resolved, and an adopted server or
// reaction put into force (applyAdoptedKinds). Each entry it no longer holds as offered is refused
// by name, and nothing is recorded for it; the rest are adopted all the same.
//
// Errors: a refusal naming a proposal that is not one (nothing is recorded then), a Project config
// that cannot be re-read (nothing is recorded then), one refusal per entry the file no longer holds
// as offered, the store's errors, the settling's, and a server or reaction the session could not
// move onto — the adoption is recorded then, and the next watched re-read or start applies it.
func (w *rootWiring) adoptProjectRules(proposals []apogee.Proposal) error {
	entries, err := proposalEntries(proposals)
	if err != nil {
		return err
	}
	held, err := projectRulesHeld(w.configPath(), w.roots.project)
	if err != nil {
		return err
	}
	entries, refused := keepHeldEntries(entries, held)
	if len(entries) == 0 {
		return refused
	}
	if err := w.recordAnswer(entries, (*adoption.Store).Adopt); err != nil {
		return errors.Join(refused, err)
	}
	if err := w.settleAllowRules(); err != nil {
		return errors.Join(refused, err)
	}
	return errors.Join(refused, w.applyAdoptedKinds(entries))
}

// applyAdoptedKinds puts the adopted servers and reactions among entries into force at once,
// through the doors a watched re-read of the layered set uses (settingsApplier.reconnectMCP and
// reloadReactions, ADR 0104 §12): the servers re-dialled from the layered set, the reactions
// swapped onto their lanes. An Allow rule needs neither — the settling already installed it — and a
// wiring that holds no live MCP set or engine (a Driver that composes none) has nothing to move.
//
// Errors: the reconnect's and the reload's, joined.
func (w *rootWiring) applyAdoptedKinds(entries []adoption.Entry) error {
	applier := w.settingsApplier()
	var errs []error
	if hasEntryKind(entries, config.MCPServerAdoptionKind) && w.mcpSet != nil && w.toolSet != nil {
		errs = append(errs, applier.reconnectMCP())
	}
	if hasEntryKind(entries, config.ReactionAdoptionKind) && w.engine != nil && w.live != nil {
		errs = append(errs, applier.reloadReactions())
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("apogee: the adoption is saved, but this session could not apply it: %w", err)
	}
	return nil
}

// hasEntryKind reports whether any of entries is of the Adoption kind kind.
func hasEntryKind(entries []adoption.Entry, kind string) bool {
	return slices.ContainsFunc(entries, func(e adoption.Entry) bool { return e.Kind == kind })
}

// projectRulesHeld is the fingerprint (adoption.Fingerprint) of every entry the Project config of
// projectRoot holds now — Allow rules, MCP servers and reactions, in force, proposed or rejected
// alike — read from the file the moment it is asked. Only the project file is read: a global config that does not resolve must not stop an
// answer about a project rule, and no global rule is one. A Project config that is the global file
// at globalPath itself holds no project rule, as the layered load treats it.
//
// Errors: a project layer that resolves to options the loader refuses.
func projectRulesHeld(globalPath, projectRoot string) (map[string]bool, error) {
	path := config.ProjectFilePath(projectRoot)
	if path == "" || isSameFile(path, globalPath) {
		return nil, nil
	}
	resolved, err := config.LoadLayeredConfig("", projectRoot, os.ReadFile, func(string) {})
	if err != nil {
		return nil, fmt.Errorf("apogee: the project config could not be re-read: %w", err)
	}
	set := resolved.AllowRules
	held := make(map[string]bool, len(set.Rules)+len(set.Proposed)+len(set.Rejected))
	for _, list := range [][]config.AllowRule{set.Rules, set.Proposed, set.Rejected} {
		for _, r := range list {
			if r.Layer == config.SourceProject {
				held[adoption.Fingerprint(adoption.Entry{Kind: string(r.Kind), Text: r.Text})] = true
			}
		}
	}
	proposals := resolved.Proposals
	for _, list := range [][]apogee.Proposal{proposals.Adopted, proposals.Proposed, proposals.Rejected} {
		for _, p := range list {
			held[p.Fingerprint] = true
		}
	}
	return held, nil
}

// isSameFile reports whether the paths name one existing file; a path that cannot be read names
// none.
func isSameFile(path, other string) bool {
	if other == "" {
		return false
	}
	a, err := os.Stat(path)
	if err != nil {
		return false
	}
	b, err := os.Stat(other)
	return err == nil && os.SameFile(a, b)
}

// keepHeldEntries is entries cut to those whose fingerprint held holds, in order, with one refusal
// joined per entry cut — nil when none was.
func keepHeldEntries(entries []adoption.Entry, held map[string]bool) ([]adoption.Entry, error) {
	kept := make([]adoption.Entry, 0, len(entries))
	var refused []error
	for _, e := range entries {
		if held[adoption.Fingerprint(e)] {
			kept = append(kept, e)
			continue
		}
		refused = append(refused, notHeldError(e))
	}
	return kept, errors.Join(refused...)
}

// notHeldError is the refusal of an adoption whose exact text the Project config no longer holds —
// for a server or reaction, an entry an edit changed or took out since it was offered.
func notHeldError(e adoption.Entry) error {
	if e.Handle != "" {
		return fmt.Errorf("apogee: the project config no longer holds the %s entry `%s` as it was offered, "+
			"so it was not adopted", e.Kind, e.Handle)
	}
	return fmt.Errorf("apogee: the project config no longer holds the %s rule `%s`, so it was not adopted",
		e.Kind, e.Text)
}

// removeProjectRule forgets the rule's answer, then takes it out of the Project config, and
// settles.
//
// Errors: a refusal for a rule that is not a project rule, the store's errors (the file is then
// untouched), the writer's refusals (the rule is then in the file but proposed, and the error says
// so), and the settling's.
func (w *rootWiring) removeProjectRule(rule apogee.AllowRule) error {
	if rule.Layer != apogee.AllowRuleProject {
		return fmt.Errorf("apogee: the %s is not a project rule; it is not in the project config", rule)
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

// removeGlobalRule takes a global rule out of the global config through that file's own splice
// writer, and settles. A global rule has no adoption record, so the file is the one write.
//
// Errors: a refusal for a rule of a kind the `allow:` key lacks, the writer's refusals (the file is
// then untouched), and the settling's.
func (w *rootWiring) removeGlobalRule(rule apogee.AllowRule) error {
	kind, err := configAllowKind(rule.Kind)
	if err != nil {
		return err
	}
	if err := config.RemoveGlobalAllowRule(w.configPath(), kind, rule.Text); err != nil {
		return err
	}
	return w.settleAllowRules()
}

// settingKeyAllow is the `allow:` key's registry path — the name a re-read reports the rules moved by.
const settingKeyAllow = "allow"

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
	if err := w.installAllowRules(); err != nil {
		return fmt.Errorf("apogee: the answer is saved, but the rules in force could not be refreshed: %w", err)
	}
	return nil
}

// followReloadedAllowRules is the live refresh behind a watched re-read (configHost.ReloadConfig):
// when the re-read found the `allow:` key moved — a hand edit to either config file, or an answer
// recorded outside this session — the effective rules are re-resolved and installed exactly as an
// act's settling installs them. It is keyed on the re-read rather than on the row's apply because
// a rule set that empties summarizes as an empty value, which the pane journals without applying.
// A resolution that fails leaves the rules in force as they were and says so as a notice beside the
// re-read's own.
func (w *rootWiring) followReloadedAllowRules(reload tui.ConfigReload) tui.ConfigReload {
	hasMoved := slices.ContainsFunc(reload.Applied, func(a tui.AppliedSetting) bool { return a.Path == settingKeyAllow })
	if !hasMoved {
		return reload
	}
	if err := w.installAllowRules(); err != nil {
		reload.Notices = append(reload.Notices, fmt.Sprintf(
			"apogee: the allow rules changed on disk, but the rules in force could not be refreshed: %v", err))
	}
	return reload
}

// installAllowRules re-resolves the effective Allow rules from the two files and the adoption
// record — the resolution a relaunch would make — and installs them on the live-settings holder and
// the engine.
//
// Errors: a global config that no longer resolves; nothing is installed then.
func (w *rootWiring) installAllowRules() error {
	resolved, err := w.layeredOptions()
	if err != nil {
		return err
	}
	if w.live != nil {
		w.live.update(func(o *config.Options) { o.AllowRules = resolved.AllowRules })
	}
	if w.engine != nil {
		w.engine.SetAllowRules(allowRulesFromOptions(resolved))
	}
	return nil
}

// proposedRules re-resolves the two files and the adoption record and reports what comes out
// proposed (proposalsOf).
//
// Errors: a global config that no longer resolves.
func (w *rootWiring) proposedRules() ([]apogee.Proposal, error) {
	resolved, err := w.layeredOptions()
	if err != nil {
		return nil, fmt.Errorf("apogee: the proposed entries could not be read: %w", err)
	}
	return proposalsOf(resolved), nil
}

// proposalsOf is everything the resolved options hold proposed, in the order the adoption pane
// asks about them: the project Allow rules first, each with the fingerprint its answer is recorded
// under, then the repo MCP servers and reactions.
func proposalsOf(opts config.Options) []apogee.Proposal {
	rules := engineAllowRules(opts.AllowRules.Proposed)
	proposals := make([]apogee.Proposal, 0, len(rules)+len(opts.Proposals.Proposed))
	for _, r := range rules {
		p := r.Proposal()
		if e, err := projectRuleEntry(r); err == nil {
			p.Fingerprint = adoption.Fingerprint(e)
		}
		proposals = append(proposals, p)
	}
	return append(proposals, opts.Proposals.Proposed...)
}

// layeredOptions is the resolution a relaunch would make of the two config files and the adoption
// record — what the settling installs and the proposed-rules read reports. The loader's notices are
// not repeated here: the watched re-read says them (ConfigHost.ReloadConfig).
func (w *rootWiring) layeredOptions() (config.Options, error) {
	return loadLayeredOptions(w.configPath(), w.roots.project)
}

// loadLayeredOptions is the global config at configPath with the Project config of projectRoot
// layered over it, sorted by that root's adoption record — the one live reading every re-read of
// the layered set makes (the Allow-rule settling, and the live reload of the MCP servers and
// reactions, settingsApplier.layeredConfig). It never migrates, and it drops the loader's notices:
// the watched re-read says them (ConfigHost.ReloadConfig). A Project config that does not parse is
// skipped by the loader, so the answer is then the global file alone.
func loadLayeredOptions(configPath, projectRoot string) (config.Options, error) {
	return config.LoadLayeredConfig(configPath, projectRoot, os.ReadFile, func(string) {})
}

// workspacesDir is where this run keeps each Project root's adoption record and the Project config
// writer's lock: `~/.apogee/workspaces`.
func (w *rootWiring) workspacesDir() string { return config.WorkspacesDir(w.roots.config) }

// proposalEntries is the adoption entries the proposals pin, in order.
//
// Errors: proposalEntry's, for the first proposal that has one.
func proposalEntries(proposals []apogee.Proposal) ([]adoption.Entry, error) {
	entries := make([]adoption.Entry, 0, len(proposals))
	for _, p := range proposals {
		e, err := proposalEntry(p)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// proposalEntry is the adoption entry a proposal pins: a repo server or reaction by its kind,
// handle and canonical text — the handle is what the store records the adopted text under, which
// a later change is named against — and an Allow rule as the project rule it is (projectRuleEntry).
//
// Errors: a refusal for a server or reaction with no handle or no pin, and projectRuleEntry's.
func proposalEntry(p apogee.Proposal) (adoption.Entry, error) {
	if p.Kind == apogee.ProposalMCPServer || p.Kind == apogee.ProposalReaction {
		if p.Handle == "" || p.Text == "" {
			return adoption.Entry{}, fmt.Errorf("apogee: the %s entry has no name or no pin; only an entry "+
				"the project config proposes is adopted", p.Kind)
		}
		return adoption.Entry{Kind: string(p.Kind), Handle: p.Handle, Text: p.Text}, nil
	}
	return projectRuleEntry(apogee.AllowRule{
		Kind:  apogee.AllowRuleKind(p.Kind),
		Text:  p.Text,
		Layer: apogee.AllowRuleProject,
	})
}

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
