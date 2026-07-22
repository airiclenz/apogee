package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ----------------------------------------------------------------------------
// The behavioral-probe record (ADR 0021 §3 — the middle rung's on-disk form)
// ----------------------------------------------------------------------------

// ProbeRecordVersion is the record schema version SaveProbeRecord stamps and LoadProbeRecord
// accepts. A record at any OTHER version — newer from a later build, older from an earlier one
// — is SKIPPED with a warning naming the one-command fix, never repaired and never fatal:
// identity is a convenience layer above a safe floor (the Store's ErrStoreVersion posture, for
// the same reason). There is deliberately no migration path; re-running `apogee probe model`
// costs one command and re-earns the claim against THIS build's battery, which a rewritten old
// record could only pretend to.
//
// v2 (2026-07-22) replaced the record's `fingerprint` field — which used to hold a synthesised
// behavioural label — with `behavior`, the observed signature, once the identity became the
// advertised label itself (ADR 0021, Amendment 2026-07-22). v1 records are therefore skipped.
const ProbeRecordVersion = 2

// ProbeBatteryVersion is the capability battery this build understands. It is homed HERE
// rather than beside the battery itself because the resolver is the constant's real consumer:
// `internal/library` decides whether a stored record is comparable to what this build would
// produce, and it cannot import `internal/probe` to ask (that would invert the dependency —
// probe writes records, library reads them). `probe.BatteryVersion` mirrors it, so bumping the
// battery in one place retires every incomparable record.
const ProbeBatteryVersion = 1

const (
	// probeDirName is the probe records' own subdirectory of the apogee home. A directory of
	// individually deletable files (rather than one aggregate) is what makes ADR 0021 §4's
	// printed undo — "delete this file" — a real off-switch.
	probeDirName = "probe"

	// probeRecordNameBytes is how much of the key digest names a record file. Eight bytes is
	// ample for the handful of (endpoint, model) pairs one user probes.
	probeRecordNameBytes = 8
)

// ProbeRecord is one dated behavioral-identity claim: `apogee probe model` measured the model
// answering at Endpoint under the advertised label ModelLabel, at ProbedAt (ADR 0021 §3's key
// triple). Its EXISTENCE is the claim the resolver reads — it is what lifts ModelLabel from the
// metadata tier to ConfidenceMedium (ADR 0021, Amendment 2026-07-22) — so the record carries no
// separate identity string; the identity is the label it is filed under.
//
// Behavior is the observed signature (`probe:<battery>:<features>[:lp-<digest>]`) and it is the
// record's DISCRIMINATING half: a model swapped behind an unchanged label yields a different
// Behavior for the same key, which is exactly how the swap becomes detectable. Features and
// CapabilityTier are the same evidence in readable form, recorded so a surface can say WHAT was
// observed and when. Nothing reads them to make a decision: the tier carries no automatism of
// its own (ADR 0021 §2).
type ProbeRecord struct {
	Version        int       `json:"version"`
	BatteryVersion int       `json:"battery-version"`
	Endpoint       string    `json:"endpoint"`
	ModelLabel     string    `json:"model-label"`
	ProbedAt       time.Time `json:"probed-at"`
	Behavior       string    `json:"behavior"`
	Features       []string  `json:"features,omitempty"`
	CapabilityTier string    `json:"capability-tier,omitempty"`
}

// ProbeDir returns the probe records' directory under the apogee home. An empty home yields
// an empty path, which every function here treats as "no probe records are available" — the
// resolver then simply has no middle rung, rather than reaching for an ambient ~/.apogee
// (ADR 0001: the library never assumes a home).
func ProbeDir(home string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(home, probeDirName)
}

// ProbeRecordPath is the file a record for (endpoint, modelLabel) occupies under dir. The name
// is a digest of the key rather than the label itself: a model id is frequently a filesystem
// path or carries characters no portable filename may hold. The record's own JSON states the
// endpoint and label in plain text, so a directory listing stays diagnosable by reading a file.
func ProbeRecordPath(dir, endpoint, modelLabel string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, probeRecordKey(endpoint, modelLabel)+".json")
}

// SaveProbeRecord writes rec under dir, owner-private (0o700 directory, 0o600 file — the
// posture the Library store and internal/session already take for a private per-model record),
// and returns the path written. It stamps the schema and battery versions itself, so a caller
// cannot record an unversioned claim. A write failure is returned rather than swallowed: this
// is the ONE write `apogee probe model` performs and its whole point, so a user who asked for
// it must hear that it did not happen.
func SaveProbeRecord(dir string, rec ProbeRecord) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("apogee: no probe record directory resolved")
	}
	rec.Version = ProbeRecordVersion
	rec.BatteryVersion = ProbeBatteryVersion

	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", fmt.Errorf("apogee: create probe record directory %q: %w", dir, err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return "", fmt.Errorf("apogee: encode probe record: %w", err)
	}
	path := ProbeRecordPath(dir, rec.Endpoint, rec.ModelLabel)
	if err := os.WriteFile(path, data, filePerm); err != nil {
		return "", fmt.Errorf("apogee: write probe record %q: %w", path, err)
	}
	return path, nil
}

// LoadProbeRecord reads the record for (endpoint, modelLabel) under dir. It returns the record
// and ok=true only for a record this build can actually compare against; every defect —
// unreadable, malformed, written by a newer build, produced by a different battery, or missing
// the fields that make it a claim — yields ok=false plus a one-line warning for the caller to
// surface (ADR 0021 §3: soft on every defect, never a blocked startup). A record that is simply
// ABSENT is not a defect: ok=false with no warning, which is the state of every fresh install.
func LoadProbeRecord(dir, endpoint, modelLabel string) (rec ProbeRecord, warning string, ok bool) {
	path := ProbeRecordPath(dir, endpoint, modelLabel)
	if path == "" {
		return ProbeRecord{}, "", false
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ProbeRecord{}, "", false // never probed here — the ordinary case, silently
	}
	if err != nil {
		return ProbeRecord{}, fmt.Sprintf("apogee: skipping probe record %q: %v", path, err), false
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return ProbeRecord{}, fmt.Sprintf("apogee: skipping malformed probe record %q: %v", path, err), false
	}
	if w := probeRecordDefect(rec); w != "" {
		return ProbeRecord{}, fmt.Sprintf("apogee: skipping probe record %q: %s", path, w), false
	}
	return rec, "", true
}

// probeRecordDefect classifies a decoded record against what THIS build can compare, returning
// the defect's one-line wording or "" when the record is usable. It is pure so the whole
// soft-degrade ladder is table-testable without a filesystem.
func probeRecordDefect(rec ProbeRecord) string {
	switch {
	case rec.Version > ProbeRecordVersion:
		return fmt.Sprintf("it is schema version %d, newer than this build understands (%d)", rec.Version, ProbeRecordVersion)
	case rec.Version < ProbeRecordVersion:
		return fmt.Sprintf("it is schema version %d, which this build (v%d) no longer reads — re-run `apogee probe model` to record it again",
			rec.Version, ProbeRecordVersion)
	case rec.BatteryVersion != ProbeBatteryVersion:
		return fmt.Sprintf("it was produced by capability battery v%d, not this build's v%d — re-run `apogee probe model`",
			rec.BatteryVersion, ProbeBatteryVersion)
	case rec.ModelLabel == "":
		return "it names no model, so there is no identity to resolve"
	case rec.Behavior == "":
		return "it records no behavioral signature, so nothing was actually observed"
	case rec.ProbedAt.IsZero():
		return "it is undated, so it is not a dated claim"
	default:
		return ""
	}
}

// probeRecordKey digests the (endpoint, model label) pair the record is keyed on. The NUL
// separator keeps the two fields from running together, so an endpoint ending in a label's
// prefix cannot collide with a different split of the same bytes.
func probeRecordKey(endpoint, modelLabel string) string {
	sum := sha256.Sum256([]byte(endpoint + "\x00" + modelLabel))
	return hex.EncodeToString(sum[:probeRecordNameBytes])
}
