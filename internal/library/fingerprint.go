package library

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Model-fingerprint resolution (the three-tier seam, CONTEXT "Library")
// ----------------------------------------------------------------------------

// weightFileExts is the set of model-weight file extensions ResolveFingerprint hashes when the
// model id is a reachable path. A local server often reports its active model as a filesystem
// path (e.g. /models/qwen2.5-coder-7b.gguf); this whitelist mirrors internal/tui/model.go so
// the resolver and the footer agree on what counts as a weight file.
var weightFileExts = map[string]bool{
	".gguf":        true,
	".ggml":        true,
	".bin":         true,
	".safetensors": true,
}

// weightSampleBytes is how much of a weight file's head and tail the signature folds in. A full
// digest of a multi-gigabyte GGUF at every startup would be a heavy, pointless cost; a hash of
// the file size plus its head and tail distinguishes different weights (GGUF header + tensor
// tail differ across models and quantizations) while staying fast. This is a deliberate v1
// choice: the tier is "weights-hash (high)" — a content signature of the weights — not a
// full-file digest.
const weightSampleBytes = 1 << 20 // 1 MiB

// ResolveFingerprint returns the best-available fingerprint for modelID. When modelID is a
// reachable weight file it hashes the weights (ConfidenceHigh); otherwise it falls back to the
// bare metadata label (ConfidenceLow). The behavioral-probe tier (ConfidenceMedium) is Phase 5
// and is not produced here (D8). An empty modelID yields the zero fingerprint (inert Library).
func ResolveFingerprint(modelID string) domain.ModelFingerprint {
	if modelID == "" {
		return domain.ModelFingerprint{}
	}
	if sig, ok := weightsSignature(modelID); ok {
		return domain.ModelFingerprint{Label: "sha256:" + sig, Confidence: domain.ConfidenceHigh}
	}
	return domain.ModelFingerprint{Label: modelID, Confidence: domain.ConfidenceLow}
}

// Resolver is the production FingerprintResolver: it satisfies the domain seam by delegating to
// ResolveFingerprint. A Phase-5 behavioral probe would be a separate resolver behind the same
// interface, so no consumer changes when it lands.
type Resolver struct{}

// Resolve returns the best-available fingerprint for modelID (see ResolveFingerprint).
func (Resolver) Resolve(modelID string) domain.ModelFingerprint { return ResolveFingerprint(modelID) }

// Compile-time proof the resolver satisfies the domain seam (ADR 0010 — library implements the
// interface domain declares).
var _ domain.FingerprintResolver = Resolver{}

// weightsSignature computes the weights-hash for a reachable weight file: a SHA-256 over the
// file size plus its head and tail (or the whole file when it is small). It returns ("", false)
// when modelID is not a weight-file path or the file is unreachable — the caller then falls
// back to the metadata-label tier. Nothing is written; only the model file is read.
func weightsSignature(modelID string) (string, bool) {
	if !weightFileExts[strings.ToLower(filepath.Ext(modelID))] {
		return "", false
	}
	info, err := os.Stat(modelID)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}

	file, err := os.Open(modelID)
	if err != nil {
		return "", false
	}
	defer file.Close()

	hash := sha256.New()
	var sizeHeader [8]byte
	binary.LittleEndian.PutUint64(sizeHeader[:], uint64(info.Size()))
	hash.Write(sizeHeader[:]) // fold in the length so head/tail collisions still diverge

	size := info.Size()
	if size <= 2*weightSampleBytes {
		if _, err := io.Copy(hash, file); err != nil {
			return "", false
		}
		return hex.EncodeToString(hash.Sum(nil)), true
	}

	if _, err := io.CopyN(hash, file, weightSampleBytes); err != nil {
		return "", false
	}
	if _, err := file.Seek(-weightSampleBytes, io.SeekEnd); err != nil {
		return "", false
	}
	if _, err := io.CopyN(hash, file, weightSampleBytes); err != nil {
		return "", false
	}
	return hex.EncodeToString(hash.Sum(nil)), true
}
