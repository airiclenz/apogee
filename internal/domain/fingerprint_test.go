package domain

import "testing"

// The zero fingerprint reports IsZero; a labelled one does not.
func TestModelFingerprintIsZero(t *testing.T) {
	t.Parallel()
	if !(ModelFingerprint{}).IsZero() {
		t.Error("zero ModelFingerprint should report IsZero")
	}
	if (ModelFingerprint{Label: "sha256:abc", Confidence: ConfidenceHigh}).IsZero() {
		t.Error("a labelled ModelFingerprint should not report IsZero")
	}
}

// The confidence tiers render human-readable labels for logging, and the medium slot exists so
// the Phase-5 behavioral probe has a home without a format change.
func TestFingerprintConfidenceString(t *testing.T) {
	t.Parallel()
	cases := map[FingerprintConfidence]string{
		ConfidenceLow:    "low",
		ConfidenceMedium: "medium",
		ConfidenceHigh:   "high",
	}
	for tier, want := range cases {
		if got := tier.String(); got != want {
			t.Errorf("FingerprintConfidence(%d).String() = %q; want %q", tier, got, want)
		}
	}
}
