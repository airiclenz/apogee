package library

import (
	"os"
	"path/filepath"
	"testing"

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

// The Resolver value satisfies the domain seam and delegates to ResolveFingerprint.
func TestResolverSatisfiesSeam(t *testing.T) {
	t.Parallel()
	var r domain.FingerprintResolver = Resolver{}
	if got := r.Resolve("some-model"); got.Label != "some-model" || got.Confidence != domain.ConfidenceLow {
		t.Errorf("Resolver.Resolve = %+v; want the metadata-label fingerprint", got)
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
