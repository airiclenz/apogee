package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// sampleRecord is a well-formed record for the endpoint/label pair the tests key on.
func sampleRecord() ProbeRecord {
	return ProbeRecord{
		Endpoint:       "http://127.0.0.1:8080",
		ModelLabel:     "qwen2.5-coder-7b",
		ProbedAt:       time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC),
		Behavior:       "probe:1:tools+json+chain",
		Features:       []string{"tools", "json", "chain"},
		CapabilityTier: "full",
	}
}

// A saved record round-trips, and Save stamps the versions itself so a caller cannot record an
// unversioned (and therefore un-retirable) claim.
func TestSaveAndLoadProbeRecord(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "probe")
	rec := sampleRecord()

	path, err := SaveProbeRecord(dir, rec)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if path != ProbeRecordPath(dir, rec.Endpoint, rec.ModelLabel) {
		t.Errorf("saved to %q; want the keyed path %q", path, ProbeRecordPath(dir, rec.Endpoint, rec.ModelLabel))
	}

	got, warning, ok := LoadProbeRecord(dir, rec.Endpoint, rec.ModelLabel)
	if !ok || warning != "" {
		t.Fatalf("load: ok=%v warning=%q; want a clean read", ok, warning)
	}
	if got.Version != ProbeRecordVersion || got.BatteryVersion != ProbeBatteryVersion {
		t.Errorf("versions = %d/%d; want %d/%d stamped by Save",
			got.Version, got.BatteryVersion, ProbeRecordVersion, ProbeBatteryVersion)
	}
	if got.Behavior != rec.Behavior || got.ModelLabel != rec.ModelLabel || !got.ProbedAt.Equal(rec.ProbedAt) {
		t.Errorf("round-trip lost the claim: %+v", got)
	}
}

// Two different (endpoint, label) pairs occupy different files, so probing a second server does
// not overwrite the first server's claim — and each file stays individually deletable, which is
// the printed undo.
func TestProbeRecordPathIsKeyedOnEndpointAndLabel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	same := ProbeRecordPath(dir, "http://a:8080", "m")
	if ProbeRecordPath(dir, "http://a:8080", "m") != same {
		t.Error("the same key must resolve to the same file")
	}
	for _, other := range [][2]string{{"http://b:8080", "m"}, {"http://a:8080", "other"}} {
		if ProbeRecordPath(dir, other[0], other[1]) == same {
			t.Errorf("(%s, %s) collided with the first key", other[0], other[1])
		}
	}
	if ProbeRecordPath("", "http://a", "m") != "" {
		t.Error("no directory means no record path — the resolver must not invent a home")
	}
}

// The record is owner-private on disk (0o700 directory, 0o600 file), the posture the Library
// store and internal/session already take for a private per-model record.
func TestProbeRecordIsOwnerPrivate(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not meaningful on Windows")
	}
	dir := filepath.Join(t.TempDir(), "probe")
	path, err := SaveProbeRecord(dir, sampleRecord())
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory mode = %o; want 0700", perm)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o; want 0600", perm)
	}
}

// An absent record is the ordinary state of a fresh install: not found, and NOT a warning —
// warning on every un-probed model would train the user to ignore the channel that matters.
func TestLoadProbeRecordAbsentIsSilent(t *testing.T) {
	t.Parallel()
	_, warning, ok := LoadProbeRecord(t.TempDir(), "http://127.0.0.1:8080", "never-probed")
	if ok || warning != "" {
		t.Errorf("absent record: ok=%v warning=%q; want a silent miss", ok, warning)
	}
}

// Every defect degrades SOFT: skipped, with one line naming why, and never an error the caller
// must handle. Identity is a convenience layer above a safe floor (ADR 0021 §3).
func TestLoadProbeRecordSoftDegradesOnDefect(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*ProbeRecord)
		raw     string
		wantsIn string
	}{
		{name: "malformed json", raw: "{not json", wantsIn: "malformed probe record"},
		{
			name:    "newer schema",
			mutate:  func(r *ProbeRecord) { r.Version = ProbeRecordVersion + 1 },
			wantsIn: "newer than this build understands",
		},
		{
			name:    "stale battery",
			mutate:  func(r *ProbeRecord) { r.BatteryVersion = ProbeBatteryVersion + 1 },
			wantsIn: "re-run `apogee probe model`",
		},
		{
			name:    "older schema",
			mutate:  func(r *ProbeRecord) { r.Version = ProbeRecordVersion - 1 },
			wantsIn: "re-run `apogee probe model` to record it again",
		},
		{
			name:    "no model label",
			mutate:  func(r *ProbeRecord) { r.ModelLabel = "" },
			wantsIn: "names no model",
		},
		{
			name:    "no behavioral signature",
			mutate:  func(r *ProbeRecord) { r.Behavior = "" },
			wantsIn: "records no behavioral signature",
		},
		{
			name:    "undated",
			mutate:  func(r *ProbeRecord) { r.ProbedAt = time.Time{} },
			wantsIn: "not a dated claim",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), "probe")
			rec := sampleRecord()
			rec.Version, rec.BatteryVersion = ProbeRecordVersion, ProbeBatteryVersion
			if tc.mutate != nil {
				tc.mutate(&rec)
			}

			data := []byte(tc.raw)
			if tc.raw == "" {
				var err error
				if data, err = json.Marshal(rec); err != nil {
					t.Fatalf("encode: %v", err)
				}
			}
			if err := os.MkdirAll(dir, dirPerm); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			path := ProbeRecordPath(dir, sampleRecord().Endpoint, sampleRecord().ModelLabel)
			if err := os.WriteFile(path, data, filePerm); err != nil {
				t.Fatalf("write: %v", err)
			}

			_, warning, ok := LoadProbeRecord(dir, sampleRecord().Endpoint, sampleRecord().ModelLabel)
			if ok {
				t.Fatal("a defective record must not be usable")
			}
			if !strings.Contains(warning, tc.wantsIn) {
				t.Errorf("warning = %q; want it to name %q", warning, tc.wantsIn)
			}
		})
	}
}
