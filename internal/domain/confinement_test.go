package domain

// Tests for the hook-time SubprocessPermit context seam
// (docs/design/confinement-execution-contract.md §10). They pin the three-state contract the
// engine and the Mechanisms both key on: absent = "may not spawn", present+nil = "unfenced",
// present+box = "confine first".

import (
	"context"
	"testing"
)

// TestSubprocessPermitFromContext_BareContext_ReportsAbsent proves the DEFAULT is refusal: a
// context nobody granted a permit on yields ok == false, which every hook must read as "do not
// spawn a subprocess".
func TestSubprocessPermitFromContext_BareContext_ReportsAbsent(t *testing.T) {
	t.Parallel()

	permit, ok := SubprocessPermitFromContext(context.Background())

	if ok {
		t.Errorf("SubprocessPermitFromContext(Background()) ok = true, want false (absence is refusal)")
	}
	if permit.Confinement != nil {
		t.Errorf("absent permit carried a Confinement %+v, want nil", permit.Confinement)
	}
}

// TestSubprocessPermitRoundTrip covers both present states: an unfenced permit (nil Confinement)
// and one carrying the box the spawned command must be confined to.
func TestSubprocessPermitRoundTrip(t *testing.T) {
	t.Parallel()

	box := ConfinementBox{
		WorkspaceRoot: "/work/space",
		WritablePaths: []string{"/work/space/out"},
		NetworkAllow:  []string{"example.test"},
	}
	tests := []struct {
		name  string
		grant SubprocessPermit
	}{
		{name: "unfenced", grant: SubprocessPermit{}},
		{name: "confined", grant: SubprocessPermit{Confinement: &Confinement{Box: box}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := WithSubprocessPermit(context.Background(), tc.grant)

			got, ok := SubprocessPermitFromContext(ctx)
			if !ok {
				t.Fatalf("SubprocessPermitFromContext ok = false, want true after WithSubprocessPermit")
			}
			if tc.grant.Confinement == nil {
				if got.Confinement != nil {
					t.Fatalf("Confinement = %+v, want nil (unfenced permit)", got.Confinement)
				}
				return
			}
			if got.Confinement == nil {
				t.Fatalf("Confinement = nil, want the granted box %+v", box)
			}
			if got.Confinement.Box.WorkspaceRoot != box.WorkspaceRoot {
				t.Errorf("Box.WorkspaceRoot = %q, want %q", got.Confinement.Box.WorkspaceRoot, box.WorkspaceRoot)
			}
		})
	}
}

// TestSubprocessPermitAndConfinementAreDistinctKeys proves the two context seams do not alias:
// installing a tool-time Confinement grants no hook-time permit, and vice versa.
func TestSubprocessPermitAndConfinementAreDistinctKeys(t *testing.T) {
	t.Parallel()

	withConf := WithConfinement(context.Background(), Confinement{})
	withPermit := WithSubprocessPermit(context.Background(), SubprocessPermit{})

	if _, ok := SubprocessPermitFromContext(withConf); ok {
		t.Error("a Confinement handle granted a SubprocessPermit; the keys must be distinct")
	}
	if _, ok := ConfinementFromContext(withPermit); ok {
		t.Error("a SubprocessPermit surfaced as a Confinement handle; the keys must be distinct")
	}
}
