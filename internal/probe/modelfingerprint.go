package probe

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// CapabilityTier is the ordinal summary of what a model can be ASKED to do, derived from the
// battery's outcomes. It is a REPORTED SIGNAL ONLY (ADR 0021 §2): nothing in the system reads
// it, no prompt adapts to it, and no Mechanism is gated on it. Adaptive prompt complexity —
// the transform that would consume it — is a recorded TODO.md follow-on precisely because a
// model-facing transform owes the catalogue a bench campaign (ADR 0009), and manufacturing a
// row we cannot yet fill would trade a real design for a placeholder.
type CapabilityTier string

const (
	// TierFull — every capability observed: native tool calls, structured output, and a
	// multi-step chain.
	TierFull CapabilityTier = "full"
	// TierStructured — two of the three: the model handles structure but not the whole loop.
	TierStructured CapabilityTier = "structured"
	// TierBasic — one capability observed.
	TierBasic CapabilityTier = "basic"
	// TierTextOnly — none observed: the model answers in prose and nothing else.
	TierTextOnly CapabilityTier = "text-only"
)

// Tier summarises the battery as an ordinal tier. It counts observed capabilities rather than
// privileging one, because the tier is a summary for a human reading a report — the individual
// findings, which the report states in full, are what any actual decision would be made on.
func Tier(b Battery) CapabilityTier {
	switch len(b.Features()) {
	case 3:
		return TierFull
	case 2:
		return TierStructured
	case 1:
		return TierBasic
	default:
		return TierTextOnly
	}
}

// Fingerprint is the behavioral identity a completed battery earns for the model advertising
// `advertised` at the probed endpoint: THE ADVERTISED LABEL AT ConfidenceMedium. The battery
// raises the TIER of an identity the system already has; it does not mint a second, differently
// spelled one (ADR 0021, Amendment 2026-07-22).
//
// That is the load-bearing decision, so it is worth stating why. The label is a KEY: Validated
// -set entries are filed under it (ADR 0016 §3 — `gemma-4-e4b-it-qat`), users paste it into a
// `validated-sets: alias:` entry, and the Library files its observations under it. A label that
// also encoded the observed feature set would be a key that MOVES whenever the battery version,
// the feature vector, or the server's willingness to expose logprobs moves — so the very act of
// probing would orphan every entry, alias and observation for that model, demoting it instead of
// promoting it. Keeping one key across the Low and Medium rungs means `apogee probe model` does
// exactly the one thing ADR 0021 §4 promises: it flips a matching Validated set from OFFERED to
// APPLIED, and changes nothing else.
//
// The behavioural evidence is not discarded — it becomes BehaviorSignature, recorded beside the
// identity in the probe record. It is the thing compared across probes (a swapped model behind
// an unchanged label), which is where discrimination is actually needed: identity resolution is
// pure and offline, so it can only ever read back what a probe wrote, and a signature baked into
// the key bought no extra safety there.
//
// An incomplete battery yields the zero fingerprint — an identity may not be minted from a run
// whose evidence has a hole in it, and least of all a tier that switches automatism on.
func Fingerprint(advertised string, b Battery) domain.ModelFingerprint {
	if !b.Complete() || advertised == "" {
		return domain.ModelFingerprint{}
	}
	return domain.ModelFingerprint{Label: advertised, Confidence: domain.ConfidenceMedium}
}

// BehaviorSignature renders what the battery OBSERVED as a compact, comparable string:
//
//	probe:<battery>:<features>[:lp-<digest>]
//
// and each part earns its place. The battery version, because a signature from a different suite
// is not comparable to one from this build. The feature set, because that is the behaviour
// actually measured — and it is a SET, so temperature, sampling noise, or a re-worded reply
// leave it untouched where a response hash would move (ADR 0021 §6: a fuzzy feature match, never
// a response hash). The candidate-token digest when the server exposed a distribution, because
// two models with the same capability set still disagree about what token could come next.
//
// It carries no advertised label: the record it lives in is keyed on endpoint + label already,
// so repeating that here would only make two probes of the SAME model compare unequal whenever
// the label was pinned differently. An incomplete battery signs nothing.
func BehaviorSignature(b Battery) string {
	if !b.Complete() {
		return ""
	}
	parts := []string{"probe", strconv.Itoa(b.Version), featureSet(b)}
	if digest := candidateDigest(b.Candidates); digest != "" {
		parts = append(parts, "lp-"+digest)
	}
	return strings.Join(parts, ":")
}

// featureSet renders the observed capabilities as the signature's feature component, "none"
// when the model demonstrated nothing (a real and reportable observation — a text-only model).
func featureSet(b Battery) string {
	features := b.Features()
	if len(features) == 0 {
		return "none"
	}
	return strings.Join(features, "+")
}

// candidateDigest folds the candidate-token SET into a short digest. The tokens'
// probabilities are deliberately excluded: they drift with temperature and server build, while
// which tokens were in contention is the stable shape of the distribution. No candidates (the
// server exposed no logprobs) yields "", and the signature simply carries no distribution
// component — the feature match still stands, one notch less discriminating.
func candidateDigest(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(candidates, "\x00")))
	return hex.EncodeToString(sum[:4])
}

// SuggestProfile turns the battery into the model-profile knobs a user might want (CONTEXT:
// Model profile). It is a SUGGESTION and travels as one: ProfileYAML renders it for copying and
// config.yaml is never touched (ADR 0021 §5 — the config file is the user's own document, a
// probe produces evidence, and turning evidence into a preference is the user's move).
func SuggestProfile(b Battery) domain.ModelProfile {
	p := domain.ModelProfile{ToolCallFormat: domain.FormatNative}
	if !b.Observed(CapNativeToolCall) {
		// No structured call arrived, so the loop would have to recover calls from visible
		// content. Markdown-fenced is the suggestion because it is the format small models
		// reach for unprompted; custom-regex needs a pattern only the user can supply.
		p.ToolCallFormat = domain.FormatMarkdownFenced
	}
	switch b.Thinking.Style {
	case "harmony":
		p.Thinking = domain.ThinkingProfile{Style: domain.ThinkingHarmony}
	case "delimited":
		p.Thinking = domain.ThinkingProfile{
			Style: domain.ThinkingDelimited,
			Start: b.Thinking.Start,
			End:   b.Thinking.End,
		}
	default:
		p.Thinking = domain.ThinkingProfile{Style: domain.ThinkingNone}
	}
	return p
}

// ProfileYAML renders a suggested profile as the paste-ready `model-profile:` block, indented
// to sit under the top level of ~/.apogee/config.yaml. The keys are the on-disk schema's
// (cmd/apogee's modelProfileConfig), not the Go field names, because the whole value of this
// output is that it can be pasted without translation.
func ProfileYAML(p domain.ModelProfile) string {
	lines := []string{
		"  model-profile:",
		"    tool-call-format: " + string(p.ToolCallFormat),
		"    thinking:",
		"      style: " + string(p.Thinking.Style),
	}
	if p.Thinking.Start != "" || p.Thinking.End != "" {
		lines = append(lines,
			"      start: "+quoteYAML(p.Thinking.Start),
			"      end: "+quoteYAML(p.Thinking.End))
	}
	return strings.Join(lines, "\n")
}

// quoteYAML double-quotes a delimiter token so YAML's own metacharacters (a leading <, a #, a
// colon) cannot change the pasted block's meaning.
func quoteYAML(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}
