package library

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
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

// Sources are everything the resolver may consult, so the full ladder is one call with one
// set of inputs. ModelID alone yields the two rungs that need nothing else; Endpoint and
// ProbeDir add the middle one, because a behavioral record is keyed on WHICH server was
// measured as well as what it called itself (ADR 0021 §3). An empty ProbeDir simply removes
// that rung — the resolver never reaches for an ambient ~/.apogee (ADR 0001).
type Sources struct {
	ModelID  string
	Endpoint string
	ProbeDir string
}

// ResolveFingerprint returns the best-available fingerprint for modelID using only what the
// model id itself can supply: the weights-hash when it is a reachable weight file
// (ConfidenceHigh), else the bare metadata label (ConfidenceLow). It is the ladder MINUS its
// middle rung — callers that know the endpoint and the apogee home use ResolveFingerprintFrom,
// which can also reach a stored behavioral-probe record (ConfidenceMedium).
func ResolveFingerprint(modelID string) domain.ModelFingerprint {
	return ResolveFingerprintFrom(Sources{ModelID: modelID})
}

// ResolveFingerprintFrom returns the best-available fingerprint over the FULL three-tier
// ladder the seam reserves (CONTEXT "Library"; ADR 0021 §3):
//
//	High   — a weights-hash, when the model id is a reachable weight file.
//	Medium — the SAME metadata label as Low, promoted because a previous `apogee probe model`
//	         recorded a behavioral claim for this endpoint + label and that record is still
//	         comparable to what this build's battery would produce.
//	Low    — the bare metadata label, always available.
//
// The order is best-evidence-first and it is not a preference: a reachable weights file
// identifies the bytes themselves, which a behavioral claim can only approximate.
//
// Note what the middle rung does and does not change. It changes the CONFIDENCE only; the label
// is byte-identical to Low's (ADR 0021, Amendment 2026-07-22). That is deliberate: the label is
// the key Validated-set entries, user aliases and Library observations are all filed under, so a
// probe that re-spelled it would orphan every one of them — demoting the model at the moment the
// user asked to promote it. Probing therefore does exactly one thing to this function's output:
// it lifts Low to Medium, which is precisely the threshold ADR 0016 §5 auto-applies at.
//
// A defective stored record (unreadable, malformed, wrong schema version, battery-stale) is
// skipped with a one-line stderr warning and the ladder simply continues to Low — the same
// soft-degrade the Library store takes for a corrupt store, because auto-applying evidence is a
// convenience layer above a safe floor. An empty ModelID yields the zero fingerprint (inert
// Library).
func ResolveFingerprintFrom(src Sources) domain.ModelFingerprint {
	if src.ModelID == "" {
		return domain.ModelFingerprint{}
	}
	if sig, ok := weightsSignature(src.ModelID); ok {
		return domain.ModelFingerprint{Label: "sha256:" + sig, Confidence: domain.ConfidenceHigh}
	}
	if label, ok := probedLabel(src); ok {
		return domain.ModelFingerprint{Label: label, Confidence: domain.ConfidenceMedium}
	}
	return domain.ModelFingerprint{Label: src.ModelID, Confidence: domain.ConfidenceLow}
}

// probedLabel reports whether a usable behavioral record exists for this endpoint + advertised
// label, returning the label the record itself names (identical to src.ModelID by construction —
// the record is keyed on it — but read from the record so the resolved identity is always the
// one the file claims). A defective record's warning goes to stderr exactly as the Library store
// surfaces its own soft failures: a resolver that returned the warning instead would push a
// decision onto every caller, including the engine's construction path, which has nowhere to put
// it.
func probedLabel(src Sources) (string, bool) {
	rec, warning, ok := LoadProbeRecord(src.ProbeDir, src.Endpoint, src.ModelID)
	if warning != "" {
		fmt.Fprintln(os.Stderr, warning)
	}
	if !ok {
		return "", false
	}
	return rec.ModelLabel, true
}

// Resolver is the production FingerprintResolver: it satisfies the domain seam by delegating to
// ResolveFingerprintFrom. Endpoint and ProbeDir are the host's injected context (empty ⇒ the
// behavioral rung is unavailable and the resolver is the weights-hash/metadata pair it has
// always been), so a consumer holding only the seam still gets the best ladder its wiring
// allows.
type Resolver struct {
	Endpoint string
	ProbeDir string
}

// Resolve returns the best-available fingerprint for modelID (see ResolveFingerprintFrom).
func (r Resolver) Resolve(modelID string) domain.ModelFingerprint {
	return ResolveFingerprintFrom(Sources{ModelID: modelID, Endpoint: r.Endpoint, ProbeDir: r.ProbeDir})
}

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
