package domain

// ----------------------------------------------------------------------------
// Model fingerprint (CONTEXT "Library" — the Library keys observations on this)
// ----------------------------------------------------------------------------

// FingerprintConfidence tags how strongly a ModelFingerprint identifies the model behind
// the Upstream. The Library keys learned observations on a fingerprint and gates injection
// on this tier ("prefer not to inject under uncertainty", CONTEXT "Library"): a low-confidence
// identity is easily aliased — two different builds can advertise the same label — so an
// observation keyed there is the weakest evidence and an inject Mechanism may decline it.
type FingerprintConfidence int

const (
	// ConfidenceLow is a metadata label: the model id the Upstream advertises. It is the
	// always-available fallback, and the weakest tier because two distinct builds can share
	// one label (CONTEXT: "keyed on the model name" was the predecessor's gap).
	ConfidenceLow FingerprintConfidence = iota

	// ConfidenceMedium is a behavioral-probe identity: the same metadata Label as Low, promoted
	// because a live capability battery OBSERVED this model (native tool calls, structured
	// output, a multi-step chain, the candidate-token distribution where the server exposes it)
	// — a fuzzy feature match, never a hash of a response, which sampling alone would move. The
	// tier moves and the Label does not, deliberately: the Label is the key the Library and the
	// Validated-set match are filed under (ADR 0021, Amendment 2026-07-22). It is produced only
	// by a human running `apogee probe model`, which records a dated claim the resolver then
	// reads back (ADR 0021 §3); nothing auto-probes, so no startup path can manufacture this
	// tier as a side effect.
	ConfidenceMedium

	// ConfidenceHigh is a weights-hash: a digest derived from the reachable model file, so
	// two builds that share a label but differ in weights resolve to distinct fingerprints.
	ConfidenceHigh
)

// String renders the confidence tier for logging and diagnostics.
func (c FingerprintConfidence) String() string {
	switch c {
	case ConfidenceLow:
		return "low"
	case ConfidenceMedium:
		return "medium"
	case ConfidenceHigh:
		return "high"
	default:
		return "unknown"
	}
}

// ModelFingerprint is the confidence-tagged identity the Library keys observations on
// (CONTEXT "Library"). Label is the resolved identity string — a weights-hash digest, a
// probe signature, or the bare metadata label — and Confidence records which tier produced
// it, because injection is gated on confidence. A zero ModelFingerprint (empty Label) means
// the model could not be identified; the Library treats it as inert (nothing to key on).
type ModelFingerprint struct {
	Label      string
	Confidence FingerprintConfidence
}

// IsZero reports whether the fingerprint failed to identify the model (no Label). An inert
// Library (nothing to observe or inject against) is the zero-fingerprint case.
func (f ModelFingerprint) IsZero() bool { return f.Label == "" }

// FingerprintResolver resolves the model behind the Upstream to a confidence-tagged
// ModelFingerprint. It is the seam for the three identity tiers: a production resolver
// returns the best available — a weights-hash when the model file is reachable, else a stored
// behavioral-probe record for this endpoint and label, else the metadata label — and the loop
// never changes shape as rungs are added behind it (D8). Domain declares the seam;
// internal/library implements it (ADR 0010 — the dependency points at domain).
type FingerprintResolver interface {
	// Resolve returns the best-available fingerprint for modelID. A resolver that cannot
	// identify the model returns the zero ModelFingerprint rather than an error — an
	// unidentified model simply leaves the Library inert.
	Resolve(modelID string) ModelFingerprint
}
