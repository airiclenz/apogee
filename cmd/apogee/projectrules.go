package main

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/project"
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
// global config (the `/settings` *Allow rules* section's) — followed by the same settling: the
// external-edit baseline is re-taken, because apogee's own write is not an edit the watcher should
// report back (ADR 0041 decision 8), and the next Snapshot of the files (project.Load, or the one project.Answer returns)
// is put in force against the one the session holds (installSnapshot): the live-settings holder
// takes its rules and proposals whole, and only the kinds project.Diff reports moved go further —
// the effective rules onto the engine, whose rule set the whole agent tree shares, so a sub-agent
// already running reads the new rule at its next ordinary gate; a moved server or reaction through
// the same doors a watched re-read applies the layered set by, since apogee's own write re-takes
// the baseline and no watcher report would carry it.
//
// Beside the four answers is the one read the adoption pane asks after a watched change
// (ProposedRules): the entries the Project config proposes now, resolved exactly as the settling
// resolves the rules in force. And beside that is the live refresh: a watched change to any of the
// three files the rules resolve from — the global config, the Project config, the adoption record —
// that moved them is installed the same way (followReloadedAllowRules), so a hand-edited `allow:`
// or an `apogee project adopt` in another terminal applies without a restart.
//
// A recorded yes to text no file holds would answer the first teammate's commit that happened to
// spell it, so none is written. The order of the two writes keeps apogee's own acts from leaving
// one: an adoption is recorded only after the file holds the rule, and dropped before the file
// loses it, so a failure part-way leaves a rule PROPOSED — inert. An adoption of a rule the file
// already held is re-checked against the file at the moment it is answered (project.Answer's held
// check): an edit that took the rule out since it was offered refuses the yes, and nothing is
// recorded for it. The yes is pinned to the exact text it answered (adoption.Fingerprint) — a
// structured entry's canonical pin, files and all — so an edit that lands after the re-check
// proposes its entry afresh rather than inheriting the answer.

// AddProjectRule is the "Always in this project…" answer: the rule into the Project config, its
// exact text adopted, the effective rules settled.
func (h configHost) AddProjectRule(kind apogee.AllowRuleKind, text string) error {
	return h.w.addProjectRule(kind, text)
}

// AdoptRules records the human's adoption of the Project config's proposals, and settles: an
// adopted server or reaction is put into force at once. An entry the Project config no longer
// holds as it was offered is refused by name and nothing is recorded for it; the rest are adopted.
func (h configHost) AdoptRules(proposals []apogee.Proposal) error {
	return h.w.answerProjectRules(proposals, true)
}

// RejectRules records the human's rejection of the Project config's proposals, and settles: a rule,
// server or reaction that was in force leaves the session.
func (h configHost) RejectRules(proposals []apogee.Proposal) error {
	return h.w.answerProjectRules(proposals, false)
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
// nothing and is adopted all the same. The adoption is recorded with no held check (project.Record):
// apogee itself just wrote the text into the file.
//
// Errors: the writer's refusals (config.AddProjectAllowRule), a store that cannot be opened or
// written — the rule is then in the file but proposed, the error says so, and the settling still
// runs, since the file moved — and the settling's.
func (w *rootWiring) addProjectRule(kind apogee.AllowRuleKind, text string) error {
	ck, err := project.AllowKind(kind)
	if err != nil {
		return err
	}
	old := w.inForce()
	path, err := config.AddProjectAllowRule(w.roots.project, w.workspacesDir(), ck, text)
	if err != nil {
		return err
	}
	if err := w.recordAnswer([]adoption.Entry{{Kind: string(ck), Text: text}}, (*adoption.Store).Adopt); err != nil {
		// The file moved even though the record did not, so the baseline must follow it.
		return errors.Join(fmt.Errorf(
			"apogee: %s holds the rule, but its adoption was not recorded, so it stays proposed: %w", path, err),
			w.settleAllowRules(old))
	}
	return w.settleAllowRules(old)
}

// answerProjectRules records the human's answer to proposals — every one the Project config's — in
// one project.Answer, and settles against the snapshot it returns. A yes (adopt) answers the file as
// it stands: each entry it no longer holds as offered is refused by name and nothing is recorded for
// it, the rest are adopted all the same. A no (reject) is recorded with no held check.
//
// Errors: a refusal naming a proposal that is not one, a store that cannot be opened, a Project
// config that cannot be re-read for the held check (nothing is recorded then), one refusal per
// entry the file no longer holds as offered, the store's errors, a next snapshot that cannot be
// loaded — the answer is recorded then, and nothing is installed — and installSnapshot's: a server
// or reaction the session could not move onto, the answer recorded, and the next watched re-read
// or start applies it.
func (w *rootWiring) answerProjectRules(proposals []apogee.Proposal, adopt bool) error {
	store, err := w.adoptionStore()
	if err != nil {
		return err
	}
	old := w.inForce()
	next, notHeld, err := project.Answer(w.configPath(), w.roots.project, w.fileReader(), store, proposals, adopt)
	refused := notHeldRefusals(notHeld)
	var stale *project.NextLoadError
	switch {
	case errors.As(err, &stale):
		w.refreshBaseline()
		return errors.Join(refused, staleRulesError(stale.Err))
	case err != nil:
		return errors.Join(refused, err)
	case len(notHeld) == len(proposals):
		// Nothing was recorded, so nothing moved and the snapshot in force stands.
		return refused
	}
	w.refreshBaseline()
	return errors.Join(refused, w.installSnapshot(old, next))
}

// notHeldRefusals is one refusal per proposal the Project config no longer holds as it was offered
// (notHeldError), joined in order — nil when there is none.
func notHeldRefusals(notHeld []apogee.Proposal) error {
	refused := make([]error, 0, len(notHeld))
	for _, p := range notHeld {
		refused = append(refused, notHeldError(p))
	}
	return errors.Join(refused...)
}

// notHeldError is the refusal of an adoption whose exact text the Project config no longer holds —
// for a server or reaction, an entry an edit changed or took out since it was offered.
func notHeldError(p apogee.Proposal) error {
	if p.Kind == apogee.ProposalMCPServer || p.Kind == apogee.ProposalReaction {
		return fmt.Errorf("apogee: the project config no longer holds the %s entry `%s` as it was offered, "+
			"so it was not adopted", p.Kind, p.Handle)
	}
	return fmt.Errorf("apogee: the project config no longer holds the %s rule `%s`, so it was not adopted",
		p.Kind, p.Text)
}

// removeProjectRule forgets the rule's answer, then takes it out of the Project config, and
// settles — on the file-failure path too, since the record moved.
//
// Errors: a refusal for a rule that is not a project rule, the store's errors (the file is then
// untouched), the writer's refusals (the rule is then in the file but proposed, and the error says
// so), and the settling's.
func (w *rootWiring) removeProjectRule(rule apogee.AllowRule) error {
	if rule.Layer != apogee.AllowRuleProject {
		return fmt.Errorf("apogee: the %s is not a project rule; it is not in the project config", rule)
	}
	entry, err := project.RuleEntry(rule)
	if err != nil {
		return err
	}
	old := w.inForce()
	if err := w.recordAnswer([]adoption.Entry{entry}, (*adoption.Store).Forget); err != nil {
		return err
	}
	if _, err := config.RemoveProjectAllowRule(w.roots.project, w.workspacesDir(), config.AllowKind(entry.Kind),
		entry.Text); err != nil {
		// The record moved even though the file did not, so the rules in force must follow it.
		return errors.Join(fmt.Errorf("apogee: the rule's adoption was dropped, so it no longer applies, "+
			"but it is still in the project config: %w", err), w.settleAllowRules(old))
	}
	return w.settleAllowRules(old)
}

// removeGlobalRule takes a global rule out of the global config through that file's own splice
// writer, and settles. A global rule has no adoption record, so the file is the one write and
// nothing is recorded.
//
// Errors: a refusal for a rule of a kind the `allow:` key lacks, the writer's refusals (the file is
// then untouched), and the settling's.
func (w *rootWiring) removeGlobalRule(rule apogee.AllowRule) error {
	kind, err := project.AllowKind(rule.Kind)
	if err != nil {
		return err
	}
	old := w.inForce()
	if err := config.RemoveGlobalAllowRule(w.configPath(), kind, rule.Text); err != nil {
		return err
	}
	return w.settleAllowRules(old)
}

// settingKeyAllow is the `allow:` key's registry path — the name a re-read reports the rules moved by.
const settingKeyAllow = "allow"

// adoptionStore opens this Project root's adoption record.
//
// Errors: adoption.New's — no record directory, or a Project root that cannot be resolved.
func (w *rootWiring) adoptionStore() (*adoption.Store, error) {
	return adoption.New(w.workspacesDir(), w.roots.project)
}

// recordAnswer opens this Project root's adoption record and writes reply for entries into it, with
// no held check (project.Record).
func (w *rootWiring) recordAnswer(entries []adoption.Entry, reply project.Reply) error {
	store, err := w.adoptionStore()
	if err != nil {
		return err
	}
	return project.Record(store, entries, reply)
}

// settleAllowRules is the settling every file write ends with: the external-edit baseline re-taken,
// then the next Snapshot loaded from the two files and the adoption record — the resolution a
// relaunch would make — and put in force against old, the snapshot in force before the write
// (installSnapshot).
//
// Errors: a global config that no longer resolves; nothing is installed then, and the rules in
// force stay as they were. installSnapshot's otherwise.
func (w *rootWiring) settleAllowRules(old project.Snapshot) error {
	w.refreshBaseline()
	next, err := project.Load(w.configPath(), w.roots.project, w.fileReader(), func(string) {})
	if err != nil {
		return staleRulesError(err)
	}
	return w.installSnapshot(old, next)
}

// staleRulesError is the refusal of a settling whose next snapshot could not be loaded: the answer
// or the file write stands, but the rules in force are as they were.
func staleRulesError(err error) error {
	return fmt.Errorf("apogee: the answer is saved, but the rules in force could not be refreshed: %w", err)
}

// refreshBaseline re-takes the external-edit baseline after apogee's own write, which is not an edit
// the watcher should report back (ADR 0041 decision 8). A wiring with no baseline has none to take.
func (w *rootWiring) refreshBaseline() {
	if w.externalEdits != nil {
		w.externalEdits.refresh()
	}
}

// inForce is the session's in-force state as a Snapshot — the live-settings holder's options with
// the offer of the snapshot it holds beside them (project.SnapshotOf) — the side an act's settling
// diffs the next snapshot against. It reads no file. A wiring with no holder holds the empty
// Snapshot, so every kind the next one holds reads as moved.
func (w *rootWiring) inForce() project.Snapshot {
	if w.live == nil {
		return project.Snapshot{}
	}
	return project.SnapshotOf(w.live.options(), w.live.offer())
}

// installSnapshot puts next in force over old. The live-settings holder takes next whole — its
// effective Allow rules onto the options, the snapshot itself for its offer, the inert rules and
// the repo proposals (liveSettings.holdInForce) — whatever moved: the `/settings` rows list from it. Then only what project.Diff reports moved goes further: the
// effective rules onto the engine, and a moved server or reaction set through the doors a watched
// re-read applies the layered set by (applyMovedKinds), each fed next's options and reading no
// file.
//
// Errors: applyMovedKinds'.
func (w *rootWiring) installSnapshot(old, next project.Snapshot) error {
	moved := project.Diff(old, next)
	opts := next.Options()
	if w.live != nil {
		w.live.holdInForce(next)
	}
	if moved.Allow && w.engine != nil {
		w.engine.SetAllowRules(allowRulesFromOptions(opts))
	}
	return w.applyMovedKinds(moved, opts)
}

// applyMovedKinds puts the servers and reactions of layered into force when moved says their set
// moved, through the doors a watched re-read of the layered set uses (settingsApplier.reconnectMCPFrom
// and reloadReactionsFrom, ADR 0104 §12): the servers re-dialled, the reactions swapped onto their
// lanes. A wiring that holds no live MCP set or engine (a Driver that composes none) has nothing to
// move.
//
// Errors: the reconnect's and the reload's, joined — and a repo server the reconnect skipped for a
// moved pin, worded as the reconnect's note: the rest of the set landed, but that server is not in
// force, and the answer's row is the one surface a mid-session adoption reports on.
func (w *rootWiring) applyMovedKinds(moved project.Moved, layered config.Options) error {
	applier := w.settingsApplier()
	var errs []error
	if moved.Servers && w.mcpSet != nil && w.toolSet != nil {
		note, err := applier.reconnectMCPFrom(layered)
		errs = append(errs, err)
		if note != "" {
			errs = append(errs, errors.New(note))
		}
	}
	if moved.Reactions && w.engine != nil && w.live != nil {
		errs = append(errs, applier.reloadReactionsFrom(layered))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("apogee: the adoption is saved, but this session could not apply it: %w", err)
	}
	return nil
}

// fileReader is what an act's loads read every file through (project.Load, project.Answer): the
// wiring's readFile seam, or the disk when none is set.
func (w *rootWiring) fileReader() func(string) ([]byte, error) {
	if w.readFile != nil {
		return w.readFile
	}
	return os.ReadFile
}

// followReloadedAllowRules is the live refresh behind a watched re-read (configHost.ReloadConfig):
// when the re-read found the `allow:` key moved — a hand edit to either config file, or an answer
// recorded outside this session — the effective rules are re-resolved and installed exactly as an
// act's settling installs them. It is keyed on the re-read rather than on the row's apply because
// a rule set that empties summarizes as an empty value, which the pane journals without applying.
// A resolution that fails leaves the rules in force as they were and says so as a notice beside the
// re-read's own.
//
// The repo entries' proposals are refreshed on EVERY re-read, moved key or not: an entry the file
// newly proposes, or an answer recorded in another terminal, moves no registry row's value — a
// proposed server is no server in force — yet the `/settings` rows listing them must follow it.
func (w *rootWiring) followReloadedAllowRules(reload tui.ConfigReload) tui.ConfigReload {
	hasMoved := slices.ContainsFunc(reload.Applied, func(a tui.AppliedSetting) bool { return a.Path == settingKeyAllow })
	if !hasMoved {
		return w.followReloadedProposals(reload)
	}
	if err := w.installAllowRules(); err != nil {
		reload.Notices = append(reload.Notices, fmt.Sprintf(
			"apogee: the allow rules changed on disk, but the rules in force could not be refreshed: %v", err))
	}
	return reload
}

// followReloadedProposals is that refresh for the repo entries alone, on a re-read that moved no
// Allow rule: the next snapshot loaded and held for its offer (holdProposals). A load that fails
// leaves them as they were and says so beside the re-read's own notices.
func (w *rootWiring) followReloadedProposals(reload tui.ConfigReload) tui.ConfigReload {
	next, err := w.loadSnapshot()
	if err != nil {
		reload.Notices = append(reload.Notices, fmt.Sprintf(
			"apogee: the project config changed on disk, but its listed entries could not be refreshed: %v", err))
		return reload
	}
	w.holdProposals(next)
	return reload
}

// installAllowRules loads the next snapshot of the two files and the adoption record — the
// resolution a relaunch would make — and installs its effective Allow rules on the live-settings
// holder and the engine, with the snapshot itself held beside them for its offer
// (installAllowRulesFrom).
//
// Errors: a global config that no longer resolves; nothing is installed then.
func (w *rootWiring) installAllowRules() error {
	next, err := w.loadSnapshot()
	if err != nil {
		return err
	}
	w.installAllowRulesFrom(next)
	return nil
}

// installAllowRulesFrom installs next's effective Allow rules on the live-settings holder and the
// engine, the holder taking next whole for its offer (liveSettings.holdInForce) — installAllowRules
// without the read, for a caller that already holds the snapshot. It reads no file.
func (w *rootWiring) installAllowRulesFrom(next project.Snapshot) {
	if w.live != nil {
		w.live.holdInForce(next)
	}
	if w.engine != nil {
		w.engine.SetAllowRules(allowRulesFromOptions(next.Options()))
	}
}

// holdProposals holds next on the live-settings holder for its offer — the repo MCP servers and
// reactions, adopted, proposed, rejected, which the `/settings` repo rows list from
// (liveSettings.proposals) — leaving the effective rules in force as they are. A wiring with no
// holder has nowhere to put them.
func (w *rootWiring) holdProposals(next project.Snapshot) {
	if w.live != nil {
		w.live.holdOffer(next)
	}
}

// proposedRules loads the next snapshot of the two files and the adoption record and reports the
// entries it asks the user to answer (project.Snapshot.Entries).
//
// Errors: a global config that no longer resolves.
func (w *rootWiring) proposedRules() ([]apogee.Proposal, error) {
	next, err := w.loadSnapshot()
	if err != nil {
		return nil, fmt.Errorf("apogee: the proposed entries could not be read: %w", err)
	}
	return next.Entries(), nil
}

// loadSnapshot is the next snapshot of the two config files and the adoption record — the
// resolution a relaunch would make (project.Load), read through the wiring's file reader — that a
// watched refresh holds and the proposed-entries read reports. The loader's notices are not
// repeated here: the watched re-read says them (ConfigHost.ReloadConfig).
func (w *rootWiring) loadSnapshot() (project.Snapshot, error) {
	return project.Load(w.configPath(), w.roots.project, w.fileReader(), func(string) {})
}

// bootSnapshot is the Snapshot of the resolution this run made at launch — w.opts with the offer
// read beside it (offerOfOptions) — reading no file: what the adoption pane offers at start-up and
// what the `/settings` rows, which project the boot options, count the proposed rules from.
func (w *rootWiring) bootSnapshot() project.Snapshot {
	return project.SnapshotOf(w.opts, offerOfOptions(w.opts))
}

// offerOfOptions is the Project config's offer resolved options carry — their Proposals and their
// inert Allow lists — as the config.ProjectOffer the same resolution returned beside them.
func offerOfOptions(opts config.Options) config.ProjectOffer {
	return config.ProjectOffer{
		Proposals: opts.Proposals,
		Proposed:  opts.AllowRules.Proposed,
		Rejected:  opts.AllowRules.Rejected,
	}.Clone()
}

// layeredOptions is the resolution a relaunch would make of the two config files and the adoption
// record, as the options alone — loadSnapshot without the offer, which a watched refresh and the
// proposed-entries read load instead. The loader's notices are not repeated here: the watched
// re-read says them (ConfigHost.ReloadConfig).
func (w *rootWiring) layeredOptions() (config.Options, error) {
	return loadLayeredOptions(w.configPath(), w.roots.project)
}

// loadLayeredOptions is the global config at configPath with the Project config of projectRoot
// layered over it, sorted by that root's adoption record — the one live reading every re-read of
// the layered set makes (the watched Allow-rule refresh, and the live reload of the MCP servers and
// reactions, settingsApplier.layeredConfig). It never migrates, and it drops the loader's notices:
// the watched re-read says them (ConfigHost.ReloadConfig). A Project config that does not parse is
// skipped by the loader, so the answer is then the global file alone.
func loadLayeredOptions(configPath, projectRoot string) (config.Options, error) {
	return config.LoadLayeredConfig(configPath, projectRoot, os.ReadFile, func(string) {})
}

// workspacesDir is where this run keeps each Project root's adoption record and the Project config
// writer's lock: `~/.apogee/workspaces`.
func (w *rootWiring) workspacesDir() string { return config.WorkspacesDir(w.roots.config) }

// proposalEntry is the adoption entry a proposal pins (project.ProposalEntry).
func proposalEntry(p apogee.Proposal) (adoption.Entry, error) { return project.ProposalEntry(p) }
