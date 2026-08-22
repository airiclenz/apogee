package domain

// Tests for the two permit context seams. The hook-time SubprocessPermit
// (docs/design/confinement-execution-contract.md §10) pins the three-state contract the engine and
// the Mechanisms both key on: absent = "may not spawn", present+nil = "unfenced", present+box =
// "confine first". The write-time WriteEscapePermit (ADR 0049) pins the two-state one the shared
// write funnel keys on: absent = "the workspace fence alone governs", present = "this one resolved
// target, and only it".

import (
	"context"
	"reflect"
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

// TestWriteEscapePermitFrom walks the write-escape seam's whole contract: a granted target rides a
// context intact, a bare context reports absent, and a permit with no target is never present —
// including when it shadows a granted one, so an inner scope cannot inherit an escape.
func TestWriteEscapePermitFrom(t *testing.T) {
	t.Parallel()

	granted := WithWriteEscapePermit(context.Background(), WriteEscapePermit{Real: "/out/side/notes.md"})

	tests := []struct {
		name     string
		ctx      context.Context
		wantOK   bool
		wantReal string
	}{
		{name: "granted target round-trips", ctx: granted, wantOK: true, wantReal: "/out/side/notes.md"},
		{name: "bare context is absent", ctx: context.Background(), wantOK: false},
		{
			name:   "empty Real is never present",
			ctx:    WithWriteEscapePermit(context.Background(), WriteEscapePermit{}),
			wantOK: false,
		},
		{
			name:   "empty Real revokes an outer grant",
			ctx:    WithWriteEscapePermit(granted, WriteEscapePermit{}),
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			permit, ok := WriteEscapePermitFrom(tc.ctx)

			if ok != tc.wantOK {
				t.Fatalf("WriteEscapePermitFrom ok = %v, want %v", ok, tc.wantOK)
			}
			if permit.Real != tc.wantReal {
				t.Errorf("Real = %q, want %q", permit.Real, tc.wantReal)
			}
		})
	}
}

// TestWriteEscapePermitIsADistinctKey proves the write-escape seam does not alias the two
// confinement seams: neither a hook-time SubprocessPermit nor a tool-time Confinement handle
// grants an out-of-workspace write, and a write-escape permit grants neither of them.
func TestWriteEscapePermitIsADistinctKey(t *testing.T) {
	t.Parallel()

	withEscape := WithWriteEscapePermit(context.Background(), WriteEscapePermit{Real: "/out/side"})

	if _, ok := WriteEscapePermitFrom(WithSubprocessPermit(context.Background(), SubprocessPermit{})); ok {
		t.Error("a SubprocessPermit granted a write escape; the keys must be distinct")
	}
	if _, ok := WriteEscapePermitFrom(WithConfinement(context.Background(), Confinement{})); ok {
		t.Error("a Confinement handle granted a write escape; the keys must be distinct")
	}
	if _, ok := SubprocessPermitFromContext(withEscape); ok {
		t.Error("a write-escape permit surfaced as a SubprocessPermit; the keys must be distinct")
	}
	if _, ok := ConfinementFromContext(withEscape); ok {
		t.Error("a write-escape permit surfaced as a Confinement handle; the keys must be distinct")
	}
}

// TestConfigConfinementBox pins the mapping the constructor exists to own: every box field comes
// from its Config counterpart, none is dropped and none is crossed with another. Hand-assembling
// the box at each call site is what let a site forget NetworkAllow and open a silent confinement
// hole, so this guard is what makes the one constructor trustworthy as the single source.
func TestConfigConfinementBox(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
		want ConfinementBox
	}{
		{
			name: "every confine field reaches the box",
			cfg: Config{
				WorkspaceDir:         "/work/space",
				ConfineWritablePaths: []string{"/work/space/out", "/tmp/build"},
				ConfineNetworkAllow:  []string{"example.test:443"},
			},
			want: ConfinementBox{
				WorkspaceRoot: "/work/space",
				WritablePaths: []string{"/work/space/out", "/tmp/build"},
				NetworkAllow:  []string{"example.test:443"},
			},
		},
		{
			name: "an unconfigured Config names no fence",
			cfg:  Config{},
			want: ConfinementBox{},
		},
		{
			// The session scratch dir (workspace-clobber hardening, 2026-08-22): a set
			// ScratchDir joins WritablePaths — appended after the host's own paths — so a
			// confined subprocess may write there and nowhere else new.
			name: "a set ScratchDir joins WritablePaths",
			cfg: Config{
				WorkspaceDir:         "/work/space",
				ConfineWritablePaths: []string{"/tmp/build"},
				ScratchDir:           "/home/u/.apogee/scratch/2026-08-22-abcd",
			},
			want: ConfinementBox{
				WorkspaceRoot: "/work/space",
				WritablePaths: []string{"/tmp/build", "/home/u/.apogee/scratch/2026-08-22-abcd"},
			},
		},
		{
			name: "a ScratchDir with no other writable paths still reaches the box",
			cfg: Config{
				WorkspaceDir: "/work/space",
				ScratchDir:   "/home/u/.apogee/scratch/2026-08-22-abcd",
			},
			want: ConfinementBox{
				WorkspaceRoot: "/work/space",
				WritablePaths: []string{"/home/u/.apogee/scratch/2026-08-22-abcd"},
			},
		},
		{
			// An empty ScratchDir adds NOTHING — the box is byte-identical to one built
			// before the field existed, including WritablePaths staying nil when unset.
			name: "an empty ScratchDir is omitted",
			cfg: Config{
				WorkspaceDir: "/work/space",
			},
			want: ConfinementBox{
				WorkspaceRoot: "/work/space",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			box := tc.cfg.ConfinementBox()

			if !reflect.DeepEqual(box, tc.want) {
				t.Errorf("ConfinementBox() = %+v, want %+v", box, tc.want)
			}
		})
	}
}

// TestConfigConfinementBoxNeverMutatesTheConfiguredPaths proves folding a ScratchDir in cannot
// scribble on the host's own ConfineWritablePaths: the append lands in a fresh slice, so a slice
// the host still holds (and every later box built from it) keeps exactly the paths it configured.
func TestConfigConfinementBoxNeverMutatesTheConfiguredPaths(t *testing.T) {
	t.Parallel()

	configured := make([]string, 1, 2) // spare capacity, so an in-place append WOULD land in it
	configured[0] = "/tmp/build"
	cfg := Config{
		WorkspaceDir:         "/work/space",
		ConfineWritablePaths: configured,
		ScratchDir:           "/home/u/.apogee/scratch/id",
	}

	_ = cfg.ConfinementBox()

	if got := configured[:cap(configured)][1]; got == cfg.ScratchDir {
		t.Errorf("ConfinementBox() appended into the configured slice's backing array (found %q)", got)
	}
	if !reflect.DeepEqual(configured, []string{"/tmp/build"}) {
		t.Errorf("ConfineWritablePaths mutated to %v, want [/tmp/build]", configured)
	}
}
