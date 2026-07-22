package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// A reachable weight file resolves to the high-confidence weights-hash tier, and the label is
// content-derived: two files with different bytes get different fingerprints, identical bytes
// get the same one.
func TestResolveFingerprintWeightsHashTier(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modelA := filepath.Join(dir, "model-a.gguf")
	modelB := filepath.Join(dir, "model-b.gguf")
	modelACopy := filepath.Join(dir, "model-a-copy.gguf")
	writeFile(t, modelA, "the weights of model A")
	writeFile(t, modelB, "the weights of model B differ")
	writeFile(t, modelACopy, "the weights of model A")

	fpA := ResolveFingerprint(modelA)
	if fpA.Confidence != domain.ConfidenceHigh {
		t.Fatalf("reachable GGUF: confidence = %v; want high", fpA.Confidence)
	}
	if fpA.Label == "" || fpA.Label[:7] != "sha256:" {
		t.Errorf("weights-hash label = %q; want a sha256: digest", fpA.Label)
	}

	if fpB := ResolveFingerprint(modelB); fpB.Label == fpA.Label {
		t.Error("different weights should resolve to different fingerprints")
	}
	if fpCopy := ResolveFingerprint(modelACopy); fpCopy.Label != fpA.Label {
		t.Errorf("identical weights should resolve to the same fingerprint: %q vs %q", fpCopy.Label, fpA.Label)
	}
}

// A large weight file (past the head/tail sample window) still hashes, and a change confined to
// the middle of the file is caught because the file size is folded into the signature.
func TestResolveFingerprintLargeFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	big := make([]byte, 3*weightSampleBytes)
	for i := range big {
		big[i] = byte(i)
	}
	path := filepath.Join(dir, "big.safetensors")
	writeBytes(t, path, big)

	first := ResolveFingerprint(path)
	if first.Confidence != domain.ConfidenceHigh {
		t.Fatalf("large weight file: confidence = %v; want high", first.Confidence)
	}

	// Grow the file (changes the size, which the signature folds in) — the fingerprint moves.
	writeBytes(t, path, append(big, 0x01))
	if second := ResolveFingerprint(path); second.Label == first.Label {
		t.Error("a size change should move the weights-hash")
	}
}

// A model id that is not a reachable weight file falls back to the low-confidence metadata
// label — an unreachable path, a plain model name, and a non-weight extension all degrade.
func TestResolveFingerprintMetadataTier(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		modelID string
	}{
		{"plain model name", "qwen2.5-coder-7b"},
		{"unreachable gguf path", filepath.Join(t.TempDir(), "missing.gguf")},
		{"non-weight extension", filepath.Join(t.TempDir(), "notes.txt")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fp := ResolveFingerprint(tc.modelID)
			if fp.Confidence != domain.ConfidenceLow {
				t.Errorf("confidence = %v; want low", fp.Confidence)
			}
			if fp.Label != tc.modelID {
				t.Errorf("metadata label = %q; want the bare model id %q", fp.Label, tc.modelID)
			}
		})
	}
}

// An empty model id yields the zero fingerprint (an inert Library).
func TestResolveFingerprintEmpty(t *testing.T) {
	t.Parallel()
	if fp := ResolveFingerprint(""); !fp.IsZero() {
		t.Errorf("empty model id should yield the zero fingerprint; got %+v", fp)
	}
}

// The Resolver value satisfies the domain seam and delegates to ResolveFingerprintFrom. With no
// endpoint or probe directory injected it is the two-rung resolver it has always been.
func TestResolverSatisfiesSeam(t *testing.T) {
	t.Parallel()
	var r domain.FingerprintResolver = Resolver{}
	if got := r.Resolve("some-model"); got.Label != "some-model" || got.Confidence != domain.ConfidenceLow {
		t.Errorf("Resolver.Resolve = %+v; want the metadata-label fingerprint", got)
	}
}

// The middle rung: a stored behavioral record for this endpoint + advertised label resolves the
// model at ConfidenceMedium — the tier that makes a Validated set auto-apply (ADR 0016 §5) and
// the reason `apogee probe model` persists at all. The LABEL is untouched: the record promotes
// the identity the model already had, it does not mint a second spelling of it (ADR 0021,
// Amendment 2026-07-22). That is what keeps entry keys, aliases and Library observations
// matching across the promotion — a relabelling here would demote every one of them.
func TestResolveFingerprintBehavioralTier(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "probe")
	src := Sources{ModelID: "qwen2.5-coder-7b", Endpoint: "http://127.0.0.1:8080", ProbeDir: dir}
	if _, err := SaveProbeRecord(dir, ProbeRecord{
		Endpoint:   src.Endpoint,
		ModelLabel: src.ModelID,
		ProbedAt:   time.Now(),
		Behavior:   "probe:1:tools+json+chain",
	}); err != nil {
		t.Fatalf("save probe record: %v", err)
	}

	before := ResolveFingerprintFrom(Sources{ModelID: src.ModelID, Endpoint: src.Endpoint})
	fp := ResolveFingerprintFrom(src)
	if fp.Confidence != domain.ConfidenceMedium {
		t.Fatalf("confidence = %v; want medium from the stored probe record", fp.Confidence)
	}
	if fp.Label != before.Label || fp.Label != src.ModelID {
		t.Errorf("label = %q; want the un-probed label %q unchanged — the probe raises the tier only",
			fp.Label, before.Label)
	}
}

// The rungs are ordered best-evidence-first: a reachable weights file identifies the bytes
// themselves, so it wins over a behavioral claim recorded for the same model.
func TestResolveFingerprintWeightsHashBeatsProbeRecord(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	dir := filepath.Join(home, "probe")
	weights := filepath.Join(home, "model.gguf")
	writeFile(t, weights, "the weights")
	if _, err := SaveProbeRecord(dir, ProbeRecord{
		Endpoint:   "http://127.0.0.1:8080",
		ModelLabel: weights,
		ProbedAt:   time.Now(),
		Behavior:   "probe:1:tools",
	}); err != nil {
		t.Fatalf("save probe record: %v", err)
	}

	fp := ResolveFingerprintFrom(Sources{ModelID: weights, Endpoint: "http://127.0.0.1:8080", ProbeDir: dir})
	if fp.Confidence != domain.ConfidenceHigh {
		t.Errorf("confidence = %v; want the weights-hash tier to win the ladder", fp.Confidence)
	}
}

// A record from a different endpoint is not this model's identity: "the model at :8080" is the
// thing that was measured, so the same label at another server falls through to the label tier.
func TestResolveFingerprintIgnoresAnotherEndpointsRecord(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "probe")
	if _, err := SaveProbeRecord(dir, ProbeRecord{
		Endpoint:   "http://127.0.0.1:8080",
		ModelLabel: "shared-label",
		ProbedAt:   time.Now(),
		Behavior:   "probe:1:tools",
	}); err != nil {
		t.Fatalf("save probe record: %v", err)
	}

	fp := ResolveFingerprintFrom(Sources{ModelID: "shared-label", Endpoint: "http://127.0.0.1:9999", ProbeDir: dir})
	if fp.Confidence != domain.ConfidenceLow || fp.Label != "shared-label" {
		t.Errorf("resolved %+v; want the metadata tier for an endpoint that was never probed", fp)
	}
}

// A defective record never blocks or corrupts resolution: the ladder skips it and continues to
// the always-available label tier (ADR 0021 §3's soft-degrade posture).
func TestResolveFingerprintSkipsDefectiveRecord(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "probe")
	src := Sources{ModelID: "some-model", Endpoint: "http://127.0.0.1:8080", ProbeDir: dir}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := ProbeRecordPath(dir, src.Endpoint, src.ModelID)
	if err := os.WriteFile(path, []byte("{not a record"), filePerm); err != nil {
		t.Fatalf("write: %v", err)
	}

	fp := ResolveFingerprintFrom(src)
	if fp.Confidence != domain.ConfidenceLow || fp.Label != "some-model" {
		t.Errorf("resolved %+v; want a soft fall-through to the metadata tier", fp)
	}
}

// With no probe directory injected there is simply no middle rung — the resolver never reaches
// for an ambient ~/.apogee (ADR 0001).
func TestResolveFingerprintWithoutProbeDir(t *testing.T) {
	t.Parallel()
	fp := ResolveFingerprintFrom(Sources{ModelID: "some-model", Endpoint: "http://127.0.0.1:8080"})
	if fp.Confidence != domain.ConfidenceLow {
		t.Errorf("confidence = %v; want low with no probe records available", fp.Confidence)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	writeBytes(t, path, []byte(content))
}

func writeBytes(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
