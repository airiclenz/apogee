package project

import (
	"errors"
	"os"

	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
)

// Reply is one of the adoption store's three record writes — (*adoption.Store).Adopt, Reject or
// Forget — the answer [Record] writes.
type Reply func(*adoption.Store, ...adoption.Entry) error

// HeldCheckError is an adoption whose held check could not re-read the Project config: nothing was
// recorded. Its text is the refusal a Driver shows as it stands.
type HeldCheckError struct {
	Err error
}

// Error is the refusal, naming the re-read's cause.
func (e *HeldCheckError) Error() string {
	return "apogee: the project config could not be re-read: " + e.Err.Error()
}

// Unwrap is the loader's error.
func (e *HeldCheckError) Unwrap() error { return e.Err }

// NextLoadError is an answer that was recorded, whose next snapshot could not be loaded — a global
// config that no longer resolves. The answer stands; the caller's snapshot is stale until a later
// load succeeds.
type NextLoadError struct {
	Err error
}

// Error says the answer is saved and names the load's cause.
func (e *NextLoadError) Error() string {
	return "apogee: the answer is saved, but the config could not be re-read after it: " + e.Err.Error()
}

// Unwrap is the loader's error.
func (e *NextLoadError) Unwrap() error { return e.Err }

// Answer records the user's answer to entries — proposals the Project config of root offered — in
// store, and returns the Snapshot loaded after the write (Load, its notices dropped) with the
// entries an adoption refused.
//
// A yes (adopt) answers the file as it stands, not as it was offered: the Project config is re-read
// alone, with no global config and so no adoption record (ADR 0096 §4), and only the entries it
// still holds — in force, proposed or rejected alike, pin for pin (adoption.Fingerprint) — are
// recorded; the rest come back as notHeld, in order, for the Driver to word. Reading no global
// keeps a global config that does not resolve from stopping the answer, and keeps a repo server a
// global one shadows held. A no (reject) records without a held check: refusing text the file no
// longer holds harms nothing.
//
// When nothing is recorded before the load — an error before the write, or a yes no entry is held
// for — next is the zero Snapshot and the caller's own snapshot stands; no record file is opened
// then. A no with no entries also records nothing (Record), but next is still loaded.
//
// Errors, told apart by what was recorded: a proposal that is not the Project config's
// (ProposalEntry; nothing recorded); a *HeldCheckError (nothing recorded); the store's errors,
// returned as they are (nothing recorded); and a *NextLoadError — the answer IS recorded, but the
// next snapshot could not be loaded.
func Answer(
	globalPath, root string,
	readFile func(string) ([]byte, error),
	store *adoption.Store,
	entries []domain.Proposal,
	adopt bool,
) (next Snapshot, notHeld []domain.Proposal, err error) {
	records, err := proposalEntries(entries)
	if err != nil {
		return Snapshot{}, nil, err
	}

	reply := Reply((*adoption.Store).Reject)
	if adopt {
		reply = (*adoption.Store).Adopt
		held, err := heldPrints(globalPath, root, readFile)
		if err != nil {
			return Snapshot{}, nil, &HeldCheckError{Err: err}
		}
		records, notHeld = keepHeld(entries, records, held)
		if len(records) == 0 {
			return Snapshot{}, notHeld, nil
		}
	}

	if err := Record(store, records, reply); err != nil {
		return Snapshot{}, notHeld, err
	}
	next, err = Load(globalPath, root, readFile, noNotice)
	if err != nil {
		return Snapshot{}, notHeld, &NextLoadError{Err: err}
	}
	return next, notHeld, nil
}

// Record writes reply for entries into store, with no held check: the answer to an entry apogee
// itself just wrote into the Project config, a rejection, or the Forget a removal leaves behind. It
// returns no snapshot — each caller takes the next one (Load) after its own file writes. No entries
// write nothing, so an empty answer never creates a record file.
//
// Errors: a refusal for a nil store, and the store's errors (nothing is recorded then).
func Record(store *adoption.Store, entries []adoption.Entry, reply Reply) error {
	if len(entries) == 0 {
		return nil
	}
	if store == nil {
		return errors.New("apogee: no adoption record is known for this project, so the answer was not recorded")
	}
	return reply(store, entries...)
}

// proposalEntries is the adoption entry of each proposal, in order (ProposalEntry).
//
// Errors: ProposalEntry's refusal for the first proposal that is not the Project config's.
func proposalEntries(proposals []domain.Proposal) ([]adoption.Entry, error) {
	records := make([]adoption.Entry, 0, len(proposals))
	for _, p := range proposals {
		e, err := ProposalEntry(p)
		if err != nil {
			return nil, err
		}
		records = append(records, e)
	}
	return records, nil
}

// keepHeld splits the parallel lists proposals and records: the records whose pin held holds, in
// order, and the proposals of the rest.
func keepHeld(
	proposals []domain.Proposal,
	records []adoption.Entry,
	held map[string]bool,
) (kept []adoption.Entry, notHeld []domain.Proposal) {
	kept = make([]adoption.Entry, 0, len(records))
	for i, e := range records {
		if held[adoption.Fingerprint(e)] {
			kept = append(kept, e)
			continue
		}
		notHeld = append(notHeld, proposals[i].Clone())
	}
	return kept, notHeld
}

// heldPrints is the pin (adoption.Fingerprint) of every entry the Project config of root holds now —
// Allow rules, MCP servers and reactions, in force, proposed or rejected alike — read from the
// project file alone, with the record off (no global path, so no workspaces dir). A Project config
// that is the global file at globalPath itself holds no entry, as the layered load treats it.
//
// Errors: a project layer the loader refuses.
func heldPrints(globalPath, root string, readFile func(string) ([]byte, error)) (map[string]bool, error) {
	path := config.ProjectFilePath(root)
	if path == "" || isSameFile(path, globalPath) {
		return nil, nil
	}
	resolved, offer, err := config.LoadLayeredOffer("", root, readFile, noNotice)
	if err != nil {
		return nil, err
	}
	rules := resolved.AllowRules.Rules
	held := make(map[string]bool, len(rules)+len(offer.Proposed)+len(offer.Rejected))
	for _, list := range [][]config.AllowRule{rules, offer.Proposed, offer.Rejected} {
		for _, r := range list {
			if r.Layer == config.SourceProject {
				held[adoption.Fingerprint(adoption.Entry{Kind: string(r.Kind), Text: r.Text})] = true
			}
		}
	}
	proposals := offer.Proposals
	for _, list := range [][]domain.Proposal{proposals.Adopted, proposals.Proposed, proposals.Rejected} {
		for _, p := range list {
			held[p.Fingerprint] = true
		}
	}
	return held, nil
}

// isSameFile reports whether the paths name one existing file; a path that cannot be stat'ed names
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

// noNotice drops a loader notice: an answer's loads say nothing — the watched re-read that follows
// the write says the files' notices.
func noNotice(string) {}
