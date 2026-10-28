package project

import (
	"errors"
	"fmt"
	"slices"

	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
)

// Snapshot is one resolution of the layered config: the options the global config and the Project
// config resolve to under the adoption record, the Project config's offer from the same read, and
// the entries that offer asks the user to answer. It is immutable — every accessor returns a copy
// that shares no backing array with the snapshot — so one can be held across goroutines and
// compared with the next ([Diff]). The zero value is the empty resolution: no options, no offer.
type Snapshot struct {
	options config.Options
	offer   config.ProjectOffer
	entries []domain.Proposal
}

// Load reads one Snapshot: the global config at globalPath with the Project config of root layered
// over it, sorted by root's adoption record (config.LoadLayeredOffer). readFile reads every file and
// notify hears the loader's notices; an empty root is no project layer.
//
// Errors: the loader's, unwrapped — a global config that does not resolve. A Project config that
// does not parse is a notice, and the global file alone answers.
func Load(globalPath, root string, readFile func(string) ([]byte, error), notify func(string)) (Snapshot, error) {
	options, offer, err := config.LoadLayeredOffer(globalPath, root, readFile, notify)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{options: options, offer: offer, entries: EntriesOf(offer)}, nil
}

// Options is the resolved options, deep-copied (config.Options.Clone).
func (s Snapshot) Options() config.Options { return s.options.Clone() }

// Offer is what the Project config offers the user to answer, deep-copied
// (config.ProjectOffer.Clone).
func (s Snapshot) Offer() config.ProjectOffer { return s.offer.Clone() }

// Entries is the answerable proposals — every entry the offer holds proposed — in the order the
// adoption pane asks about them (EntriesOf), each deep-copied.
func (s Snapshot) Entries() []domain.Proposal { return cloneProposals(s.entries) }

// Moved names the kinds of entry whose in-force set differs between two snapshots ([Diff]): the
// effective Allow rules, the adopted repo MCP servers and the adopted repo reactions. A kind that
// did not move needs nothing applied.
type Moved struct {
	Allow     bool
	Servers   bool
	Reactions bool
}

// Diff reports which kinds moved from before to after. Allow compares the whole effective rule set (every
// global rule, then every adopted project rule), so a global edit moves it as an adoption does;
// Servers and Reactions compare the adopted entries of their kind by fingerprint, as sets. An entry
// proposed or rejected without ever being adopted moves nothing: it was never in force.
func Diff(before, after Snapshot) Moved {
	return Moved{
		Allow:     !slices.Equal(before.options.AllowRules.Rules, after.options.AllowRules.Rules),
		Servers:   adoptedMoved(before, after, domain.ProposalMCPServer),
		Reactions: adoptedMoved(before, after, domain.ProposalReaction),
	}
}

// adoptedMoved reports whether the fingerprints of the adopted entries of kind differ between before
// and after, order aside.
func adoptedMoved(before, after Snapshot, kind domain.ProposalKind) bool {
	return !slices.Equal(adoptedFingerprints(before, kind), adoptedFingerprints(after, kind))
}

// adoptedFingerprints is the sorted fingerprints of s's adopted entries of kind.
func adoptedFingerprints(s Snapshot, kind domain.ProposalKind) []string {
	var prints []string
	for _, p := range s.offer.Proposals.Adopted {
		if p.Kind == kind {
			prints = append(prints, p.Fingerprint)
		}
	}
	slices.Sort(prints)
	return prints
}

// EntriesOf is everything offer holds proposed, in the order the adoption pane asks about them: the
// project Allow rules first, each with the fingerprint its answer is recorded under, then the repo
// MCP servers and reactions. Each entry is a copy that shares no backing array with offer.
func EntriesOf(offer config.ProjectOffer) []domain.Proposal {
	rules := EngineAllowRules(offer.Proposed)
	proposals := make([]domain.Proposal, 0, len(rules)+len(offer.Proposals.Proposed))
	for _, r := range rules {
		p := r.Proposal()
		if e, err := RuleEntry(r); err == nil {
			p.Fingerprint = adoption.Fingerprint(e)
		}
		proposals = append(proposals, p)
	}
	for _, p := range offer.Proposals.Proposed {
		proposals = append(proposals, p.Clone())
	}
	return proposals
}

// cloneProposals is list with every element deep-copied (domain.Proposal.Clone). A nil list stays
// nil.
func cloneProposals(list []domain.Proposal) []domain.Proposal {
	if list == nil {
		return nil
	}
	out := make([]domain.Proposal, len(list))
	for i, p := range list {
		out[i] = p.Clone()
	}
	return out
}

// EngineAllowRules is rules in the engine's spelling, each tagged with the layer it came from — the
// spelling a surface lists them in and the adoption pane offers them in. Nil for no rules.
func EngineAllowRules(rules []config.AllowRule) []domain.AllowRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]domain.AllowRule, len(rules))
	for i, r := range rules {
		kind := domain.AllowRuleTerminal
		if r.Kind == config.AllowMCPServers {
			kind = domain.AllowRuleMCPServer
		}
		layer := domain.AllowRuleGlobal
		if r.Layer == config.SourceProject {
			layer = domain.AllowRuleProject
		}
		out[i] = domain.AllowRule{Kind: kind, Text: r.Text, Layer: layer}
	}
	return out
}

// ProposalEntry is the adoption entry a proposal pins: a repo server or reaction by its kind,
// handle and canonical text — the handle is what the store records the adopted text under, which
// a later change is named against — and an Allow rule as the project rule it is (RuleEntry).
//
// Errors: a refusal for a server or reaction with no handle or no pin, and RuleEntry's.
func ProposalEntry(p domain.Proposal) (adoption.Entry, error) {
	if p.Kind == domain.ProposalMCPServer || p.Kind == domain.ProposalReaction {
		if p.Handle == "" || p.Text == "" {
			return adoption.Entry{}, fmt.Errorf("apogee: the %s entry has no name or no pin; only an entry "+
				"the project config proposes is adopted", p.Kind)
		}
		return adoption.Entry{Kind: string(p.Kind), Handle: p.Handle, Text: p.Text}, nil
	}
	return RuleEntry(domain.AllowRule{
		Kind:  domain.AllowRuleKind(p.Kind),
		Text:  p.Text,
		Layer: domain.AllowRuleProject,
	})
}

// RuleEntry is the adoption entry a project rule pins.
//
// Errors: a refusal for a rule that is not a project rule, or of a kind the `allow:` key lacks.
func RuleEntry(rule domain.AllowRule) (adoption.Entry, error) {
	if rule.Layer != domain.AllowRuleProject {
		return adoption.Entry{}, fmt.Errorf("apogee: the %s is not a project rule; only those are adopted", rule)
	}
	kind, err := AllowKind(rule.Kind)
	if err != nil {
		return adoption.Entry{}, err
	}
	return adoption.Entry{Kind: string(kind), Text: rule.Text}, nil
}

// AllowKind is the `allow:` list an engine rule kind is written under.
//
// Errors: a refusal for a kind that is neither a terminal rule nor an MCP-server rule.
func AllowKind(kind domain.AllowRuleKind) (config.AllowKind, error) {
	switch kind {
	case domain.AllowRuleTerminal:
		return config.AllowTerminal, nil
	case domain.AllowRuleMCPServer:
		return config.AllowMCPServers, nil
	}
	return "", errors.New("apogee: an allow rule is a terminal rule or an MCP-server rule; this one is neither")
}
