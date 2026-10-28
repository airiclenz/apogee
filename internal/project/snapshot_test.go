package project

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
)

// snapshotProjectYAML is a Project config proposing every kind the pane asks about: two MCP servers,
// two reactions and two terminal Allow rules.
const snapshotProjectYAML = "mcp-servers:\n" +
	"  - name: srv-a\n    transport: stdio\n    command: srv-a\n" +
	"  - name: srv-b\n    transport: stdio\n    command: srv-b\n" +
	"reactions:\n" +
	"  - id: rx-a\n    on: [file-changed]\n    run: [echo, a]\n" +
	"  - id: rx-b\n    on: [file-changed]\n    run: [echo, b]\n" +
	"allow:\n  terminal: [go test, go build]\n"

// noNotify drops the loader's notices.
func noNotify(string) {}

// loadFixture writes a global config and snapshotProjectYAML side by side under one temp dir,
// adopts the first entry of each kind (srv-a, rx-a, `go test`), and loads the Snapshot they resolve
// to.
func loadFixture(t *testing.T) Snapshot {
	t.Helper()

	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	root := filepath.Join(dir, "project")
	projectPath := config.ProjectFilePath(root)
	for _, d := range []string{home, filepath.Dir(projectPath)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("create %s: %v", d, err)
		}
	}
	globalPath := filepath.Join(home, "config.yaml")
	for path, body := range map[string]string{globalPath: "mode: plan\n", projectPath: snapshotProjectYAML} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	unanswered, err := Load(globalPath, root, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	store, err := adoption.New(config.WorkspacesDir(home), root)
	if err != nil {
		t.Fatalf("adoption.New: %v", err)
	}
	for _, p := range unanswered.Entries() {
		if p.Handle == "srv-a" || p.Handle == "rx-a" || p.Text == "go test" {
			entry, err := ProposalEntry(p)
			if err != nil {
				t.Fatalf("ProposalEntry %+v: %v", p, err)
			}
			if err := store.Adopt(entry); err != nil {
				t.Fatalf("Adopt %+v: %v", entry, err)
			}
		}
	}

	snapshot, err := Load(globalPath, root, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return snapshot
}

// The entries are the proposals the offer still asks about, in the pane's order: the project Allow
// rules first, each carrying the fingerprint its answer is recorded under, then the servers and
// reactions — and an adopted entry of any kind is not among them.
func TestSnapshotEntriesAreTheOfferInThePanesOrder(t *testing.T) {
	t.Parallel()
	snapshot := loadFixture(t)

	entries := snapshot.Entries()

	want := []struct {
		kind  domain.ProposalKind
		label string
	}{
		{domain.ProposalKind(domain.AllowRuleTerminal), "go build"},
		{domain.ProposalMCPServer, "srv-b"},
		{domain.ProposalReaction, "rx-b"},
	}
	if len(entries) != len(want) {
		t.Fatalf("Entries = %+v; want %d entries", entries, len(want))
	}
	for i, w := range want {
		label := entries[i].Handle
		if label == "" {
			label = entries[i].Text
		}
		if entries[i].Kind != w.kind || label != w.label {
			t.Errorf("Entries[%d] = %s %q; want %s %q", i, entries[i].Kind, label, w.kind, w.label)
		}
		if entries[i].Fingerprint == "" {
			t.Errorf("Entries[%d] (%s) carries no fingerprint", i, label)
		}
	}
	rulePrint := adoption.Fingerprint(adoption.Entry{Kind: string(config.AllowTerminal), Text: "go build"})
	if entries[0].Fingerprint != rulePrint {
		t.Errorf("the rule's fingerprint = %q; want its recorded pin %q", entries[0].Fingerprint, rulePrint)
	}
	if adopted := snapshot.Offer().Proposals.Adopted; len(adopted) != 2 {
		t.Errorf("Offer().Proposals.Adopted = %+v; want srv-a and rx-a", adopted)
	}
}

// Every accessor hands out a copy: writing through one leaves the snapshot, and the next copy,
// unchanged.
func TestSnapshotAccessorsShareNoBackingArray(t *testing.T) {
	t.Parallel()
	snapshot := loadFixture(t)

	options := snapshot.Options()
	options.AllowRules.Rules[0].Text = "edited"
	offer := snapshot.Offer()
	offer.Proposals.Adopted[0].Handle = "edited"
	offer.Proposed[0].Text = "edited"
	entries := snapshot.Entries()
	entries[0].Text = "edited"

	if got := snapshot.Options().AllowRules.Rules[0].Text; got == "edited" {
		t.Error("Options() shares the effective Allow rules with the snapshot")
	}
	if got := snapshot.Offer(); got.Proposals.Adopted[0].Handle == "edited" || got.Proposed[0].Text == "edited" {
		t.Error("Offer() shares a list with the snapshot")
	}
	if got := snapshot.Entries(); got[0].Text == "edited" {
		t.Error("Entries() shares its list with the snapshot")
	}
	if &snapshot.Options().Proposals.Adopted[0] == &snapshot.options.Proposals.Adopted[0] ||
		&snapshot.Offer().Proposals.Proposed[0] == &snapshot.offer.Proposals.Proposed[0] ||
		&snapshot.Entries()[0] == &snapshot.entries[0] {
		t.Error("an accessor hands out the snapshot's own backing array")
	}
}

// A Snapshot built from a set already in hand is the one Load read it from: Diff finds nothing
// moved between them, and it shares no backing array with the values it was built from.
func TestSnapshotOfMatchesTheLoadItWasBuiltFrom(t *testing.T) {
	t.Parallel()
	loaded := loadFixture(t)
	options, offer := loaded.Options(), loaded.Offer()

	built := SnapshotOf(options, offer)
	options.AllowRules.Rules[0].Text = "edited"
	offer.Proposals.Adopted[0].Fingerprint = "edited"

	if got := Diff(loaded, built); got != (Moved{}) {
		t.Errorf("Diff(loaded, SnapshotOf(loaded)) = %+v; want nothing moved", got)
	}
	if got, want := built.Entries(), loaded.Entries(); !reflect.DeepEqual(got, want) {
		t.Errorf("SnapshotOf entries = %+v; want the load's %+v", got, want)
	}
}

// adoptedProposal is an adopted entry of kind whose pin is print.
func adoptedProposal(kind domain.ProposalKind, print string) domain.Proposal {
	return domain.Proposal{Kind: kind, Handle: print, Text: print, Fingerprint: print}
}

// snapshotOf is a Snapshot whose effective Allow rules are rules and whose adopted entries are
// adopted, and that both proposes and rejects inert — entries never in force, which Diff must not
// count.
func snapshotOf(rules []config.AllowRule, adopted []domain.Proposal, inert ...domain.Proposal) Snapshot {
	options := config.Options{AllowRules: config.AllowRules{Rules: rules}}
	options.Proposals = config.ProjectProposals{Adopted: adopted, Proposed: inert, Rejected: inert}
	return Snapshot{
		options: options,
		offer:   config.ProjectOffer{Proposals: options.Proposals.Clone()},
	}
}

// Diff moves a kind exactly when its in-force set moved: an adoption or the rejection of an adopted
// entry moves its kind alone; an entry proposed or rejected without ever being in force, and an
// unchanged set, move nothing.
func TestSnapshotDiff(t *testing.T) {
	t.Parallel()

	globalRule := config.AllowRule{Kind: config.AllowTerminal, Text: "make", Layer: config.SourceGlobal}
	projectRule := config.AllowRule{Kind: config.AllowTerminal, Text: "go test", Layer: config.SourceProject}
	server := adoptedProposal(domain.ProposalMCPServer, "srv")
	reaction := adoptedProposal(domain.ProposalReaction, "rx")
	base := snapshotOf([]config.AllowRule{globalRule}, nil)

	tests := []struct {
		name          string
		before, after Snapshot
		want          Moved
	}{
		{"unchanged", base, snapshotOf([]config.AllowRule{globalRule}, nil), Moved{}},
		{"allow adopted", base, snapshotOf([]config.AllowRule{globalRule, projectRule}, nil), Moved{Allow: true}},
		{"allow rejected", snapshotOf([]config.AllowRule{globalRule, projectRule}, nil), base, Moved{Allow: true}},
		{"global rule removed", base, snapshotOf(nil, nil), Moved{Allow: true}},
		{"server adopted", base, snapshotOf([]config.AllowRule{globalRule}, []domain.Proposal{server}),
			Moved{Servers: true}},
		{"server rejected", snapshotOf([]config.AllowRule{globalRule}, []domain.Proposal{server}), base,
			Moved{Servers: true}},
		{"server rejected while proposed", base, snapshotOf([]config.AllowRule{globalRule}, nil, server), Moved{}},
		{"server re-adopted under a new pin",
			snapshotOf(nil, []domain.Proposal{server}),
			snapshotOf(nil, []domain.Proposal{{Kind: domain.ProposalMCPServer, Handle: "srv", Text: "srv v2",
				Fingerprint: "srv v2"}}),
			Moved{Servers: true}},
		{"reaction adopted", base, snapshotOf([]config.AllowRule{globalRule}, []domain.Proposal{reaction}),
			Moved{Reactions: true}},
		{"reaction rejected", snapshotOf([]config.AllowRule{globalRule}, []domain.Proposal{reaction}), base,
			Moved{Reactions: true}},
		{"reaction rejected while proposed", base, snapshotOf([]config.AllowRule{globalRule}, nil, reaction),
			Moved{}},
		{"adopted order aside",
			snapshotOf(nil, []domain.Proposal{server, adoptedProposal(domain.ProposalMCPServer, "srv-2")}),
			snapshotOf(nil, []domain.Proposal{adoptedProposal(domain.ProposalMCPServer, "srv-2"), server}),
			Moved{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := Diff(tc.before, tc.after); got != tc.want {
				t.Errorf("Diff = %+v; want %+v", got, tc.want)
			}
		})
	}
}
