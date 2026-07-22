package probe

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ModelInputs are what the composition root has resolved before the battery can run: which
// Upstream to measure, what it advertises itself as (the pinned --model, else the discovered
// active one — the label a later OFFLINE session will have in hand), and the seam to call it
// through. Now is injectable so the record's date — the thing that makes it a dated claim
// rather than a permanent truth — is deterministic under test.
type ModelInputs struct {
	Endpoint string
	Model    string
	Chat     Chat
	Now      func() time.Time
}

// Model is the finished model report: the run, its ordinal summary, the profile it suggests,
// the behavioral identity it derived, and what became of the record. Like Host it is a value,
// so a test asserts on findings rather than parsing prose.
type Model struct {
	Endpoint    string
	Model       string
	ProbedAt    time.Time
	Battery     Battery
	Tier        CapabilityTier
	Profile     domain.ModelProfile
	Fingerprint domain.ModelFingerprint

	// Behavior is the observed signature behind the fingerprint (see BehaviorSignature). It is
	// evidence, not identity: it is recorded beside the claim and compared across probes so a
	// model swapped behind an unchanged label is detectable, and it is never a match key.
	Behavior string

	// Save is filled in by the caller AFTER GatherModel returns, because what a record costs
	// — which Validated sets it promotes, whether one already existed — is knowledge the
	// composition root holds (it owns the validated-set entries and the apogee home), not the
	// battery's. GatherModel never writes; the caller decides and records the outcome here.
	Save SaveOutcome
}

// SaveOutcome is what happened to the fingerprint record, and it exists because ADR 0021 §4
// makes the write the reason `probe model` is an ACT rather than a report: writing a Medium
// fingerprint promotes a model from "a Validated set is offered" to "a Validated set is
// applied" (ADR 0016 §5). Every field here is something the report must say out loud.
type SaveOutcome struct {
	// Requested is false under --no-save: the battery ran in full and nothing was written.
	Requested bool
	// Path is where the record lives, or would have lived. It is printed either way, because
	// deleting that file is the supported undo.
	Path string
	// Written reports that the record actually reached disk.
	Written bool
	// Failure carries a write error's message. A failed write is soft — the report still
	// stands, only the identity was not recorded.
	Failure string
	// Changed is the date of a PREVIOUS record for this same endpoint + advertised label whose
	// behavioral signature differed: the model behind the label changed since then. Empty when
	// there was no previous record, or when it agreed.
	Changed string
	// AutoApply names the Validated-set entries that auto-apply for this model once the record
	// exists.
	AutoApply []string
	// Promoted distinguishes the two ways AutoApply can be non-empty: true when the record is
	// what made those sets apply (they were merely OFFERED before — the ADR 0021 §4 promotion),
	// false when the user's own alias already applied them and the record only makes the match
	// direct. Saying "it was previously only offered" in the second case would be a false claim
	// about the user's machine.
	Promoted bool
	// Suppressed names a session-level off-switch (Bypass, validated-sets: enable: false, an
	// explicit mechanisms: block) that holds whatever this record says. It exists so the report
	// never announces an effect the next startup will decline to deliver.
	Suppressed string
}

// GatherModel runs the model half of `apogee probe`: the live capability battery, then the
// pure derivations over it — the tier, the suggested profile, and the behavioral fingerprint.
// It spends real tokens and it WRITES NOTHING: persistence is the caller's explicit act, which
// is what lets `--no-save` be a genuine off-switch rather than a rollback.
func GatherModel(ctx context.Context, in ModelInputs) Model {
	now := in.Now
	if now == nil {
		now = time.Now
	}
	battery := RunBattery(ctx, in.Chat)
	return Model{
		Endpoint:    in.Endpoint,
		Model:       in.Model,
		ProbedAt:    now().UTC(),
		Battery:     battery,
		Tier:        Tier(battery),
		Profile:     SuggestProfile(battery),
		Fingerprint: Fingerprint(in.Model, battery),
		Behavior:    BehaviorSignature(battery),
	}
}

// ModelPreamble is the line `apogee probe model` prints BEFORE it calls anything, so the cost
// and the consequence are stated in advance rather than discovered in the output (ADR 0021 §4).
// noSave flips it to the read-only reading of the same act.
func ModelPreamble(noSave bool) string {
	if noSave {
		return "apogee probe model: calling the model live (--no-save: nothing will be written)."
	}
	return "apogee probe model: calling the model live, then recording its behavioral fingerprint. " +
		"Re-run with --no-save to probe without writing."
}

// Report renders the model report for a terminal. Pure — GatherModel did the observing and the
// caller filled in Save — so every line is table-testable without a live endpoint.
//
// The order is the order the questions get asked: what was measured, what it did, what that
// makes it, what you might want to configure, and finally what this command just changed about
// your machine. The consequence goes LAST because it is the part the reader must not miss.
func (m Model) Report() string {
	lines := []string{
		"apogee probe — model battery",
		fmt.Sprintf("  (live model calls; capability battery v%d)", m.Battery.Version),
		"",
		"upstream",
		field("endpoint", orUnknown(m.Endpoint)),
		field("model", orUnknown(m.Model)),
		field("probed at", m.ProbedAt.Format(time.RFC3339)),
		"",
		"capabilities",
	}
	lines = append(lines, m.findingLines()...)
	lines = append(lines,
		field("logprobs", m.candidateLine()),
		"",
		field("tier", string(m.Tier)+" — a reported signal only; nothing in apogee adapts to it (ADR 0021 §2)"),
		"",
		"behavioral fingerprint")
	lines = append(lines, m.fingerprintLines()...)
	lines = append(lines,
		"",
		"suggested model profile — paste into ~/.apogee/config.yaml (nothing is written for you):",
		ProfileYAML(m.Profile))

	report := strings.Join(lines, "\n")
	if record := m.recordSection(); record != "" {
		report += "\n\n" + record
	}
	return report
}

// findingLines render one line per capability probe: the verdict, then the evidence. A probe
// that never completed says so distinctly from one that completed and observed nothing — "we
// could not ask" and "the model cannot" are different facts about the world.
func (m Model) findingLines() []string {
	out := make([]string, 0, len(m.Battery.Findings))
	for _, f := range m.Battery.Findings {
		verdict := "no  — " + f.Detail
		switch {
		case f.Failure != "":
			verdict = "??  — probe failed: " + f.Failure
		case f.Observed:
			verdict = "yes — " + f.Detail
		}
		out = append(out, field(string(f.Capability), verdict))
	}
	return out
}

// candidateLine states whether the server exposed a candidate-token distribution — an
// observation about the SERVER, not the model, and the difference between a fingerprint that
// can tell two similar models apart and one that cannot.
func (m Model) candidateLine() string {
	if n := len(m.Battery.Candidates); n > 0 {
		return fmt.Sprintf("exposed — %d candidate tokens folded into the fingerprint", n)
	}
	return "not exposed — the fingerprint rests on the feature match alone"
}

// fingerprintLines state the identity the battery earned and the evidence behind it, or why
// neither was derived. An incomplete battery yields no fingerprint at all: a tier minted from
// evidence with a hole in it would switch automatism on for a model that was never successfully
// asked what it can do.
//
// The label line says out loud that the identity is UNCHANGED — the same string the model was
// already known by — because a reader who expected a new opaque identity should learn here that
// their aliases and Library observations keep matching (ADR 0021, Amendment 2026-07-22).
func (m Model) fingerprintLines() []string {
	if m.Fingerprint.IsZero() {
		return []string{
			field("label", "none — the battery did not complete, so no identity was derived"),
			field("behavior", "not signed — an incomplete run is not an observation"),
			field("confidence", "n/a (identity resolves as it did before: weights-hash if reachable, else the model label)"),
		}
	}
	return []string{
		field("label", m.Fingerprint.Label+" — unchanged; the probe raises its confidence, it does not rename it"),
		field("behavior", m.Behavior+" — the observed signature, compared against the next probe"),
		field("confidence", m.Fingerprint.Confidence.String()+" — a dated behavioral claim, not a permanent truth"),
	}
}

// recordSection is the consequence block: what was written, where, what it now enables, and how
// to undo it. It is the part of the output ADR 0021 §4 makes binding — a command that switches
// automatism on must read like one.
func (m Model) recordSection() string {
	if m.Fingerprint.IsZero() {
		return "record\n" + field("written", "no — an incomplete battery derives no identity to record")
	}

	lines := []string{"record", field("path", orUnknown(m.Save.Path))}
	switch {
	case !m.Save.Requested:
		lines = append(lines, field("written", "NO — --no-save was given; the battery ran and nothing was recorded"))
	case m.Save.Failure != "":
		lines = append(lines, field("written", "no — the write failed: "+m.Save.Failure))
	case m.Save.Written:
		lines = append(lines, field("written", "yes — delete the file above to undo, or re-run with --no-save"))
	default:
		lines = append(lines, field("written", "no"))
	}

	if m.Save.Changed != "" {
		lines = append(lines, field("changed",
			"the model behind this label changed since "+m.Save.Changed+" — the previous record's behavioral signature differed"))
	}
	lines = append(lines, field("effect", m.effectLine()))
	return strings.Join(lines, "\n")
}

// effectLine names the automatism this record switches on. Under ADR 0016 §5 a model at low
// confidence gets an OFFER and the same model at medium gets the set APPLIED, so running this
// command is the act that makes the promotion — and when a matching set exists, the report
// names it rather than leaving the user to discover it next session.
//
// Every branch here is a claim about the reader's machine, so each is narrowed until it is true
// of it: a set already applying through the user's own alias was not promoted by this record,
// and "none matches today" is stated against the key that would have to match, not in the
// abstract.
func (m Model) effectLine() string {
	if !m.Save.Requested || !m.Save.Written {
		return "none — with no record stored, this model's identity stays at the label tier (low confidence)"
	}
	if m.Save.Suppressed != "" {
		return "this model now resolves at medium confidence, but " + m.Save.Suppressed + "."
	}
	if len(m.Save.AutoApply) == 0 {
		return "this model now resolves at medium confidence, so a Validated set keyed " +
			m.Fingerprint.Label + " would AUTO-APPLY (ADR 0016 §5). No entry carries that key today."
	}
	if !m.Save.Promoted {
		return "Validated set " + strings.Join(m.Save.AutoApply, ", ") +
			" was already applying through your validated-sets alias; the record makes the match direct " +
			"(medium confidence), and nothing else about this session changes."
	}
	return "Validated set " + strings.Join(m.Save.AutoApply, ", ") +
		" now AUTO-APPLIES for this model (ADR 0016 §5) — it was previously only offered."
}
