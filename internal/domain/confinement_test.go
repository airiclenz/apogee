package domain

// Tests for the two permit context seams. The reaction-time SubprocessPermit
// (docs/design/confinement-execution-contract.md §10) pins the three-state contract the engine and
// the Reactions both key on: absent = "may not spawn", present+nil = "unfenced", present+box =
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

// TestWithoutConfinementHidesTheHandle pins the seam the dispatch's bookkeeping git rides: a
// handle an outer scope installed is invisible below WithoutConfinement, a context that never
// carried one is unchanged, and cancellation still flows from the parent — the stripped context
// is the same chain, not a detached one.
func TestWithoutConfinementHidesTheHandle(t *testing.T) {
	t.Parallel()

	box := ConfinementBox{WorkspaceRoot: "/work/space"}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()

	confined := WithConfinement(parent, Confinement{Box: box})
	stripped := WithoutConfinement(confined)

	if _, ok := ConfinementFromContext(confined); !ok {
		t.Fatal("the installed handle was not visible before stripping")
	}
	if conf, ok := ConfinementFromContext(stripped); ok {
		t.Errorf("ConfinementFromContext(stripped) = %+v, ok = true; want ok = false", conf)
	}
	if _, ok := ConfinementFromContext(WithoutConfinement(parent)); ok {
		t.Error("stripping a context that never carried a handle made one appear")
	}
	if stripped.Done() != parent.Done() {
		t.Error("the stripped context's Done() is not the parent's; cancellation must still flow")
	}

	cancel()

	if stripped.Err() == nil {
		t.Error("the parent's cancellation did not reach the stripped context")
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
			// confined subprocess may write there and nowhere else new; it also rides on the
			// box's own ScratchDir so the spawn can seed the toolchain caches beneath it.
			name: "a set ScratchDir joins WritablePaths",
			cfg: Config{
				WorkspaceDir:         "/work/space",
				ConfineWritablePaths: []string{"/tmp/build"},
				ScratchDir:           "/home/u/.apogee/scratch/2026-08-22-abcd",
			},
			want: ConfinementBox{
				WorkspaceRoot: "/work/space",
				WritablePaths: []string{"/tmp/build", "/home/u/.apogee/scratch/2026-08-22-abcd"},
				ScratchDir:    "/home/u/.apogee/scratch/2026-08-22-abcd",
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
				ScratchDir:    "/home/u/.apogee/scratch/2026-08-22-abcd",
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

// A NETWORK-egress residual must not become a back-door Auto gate. ADR 0012 put Auto behind
// filesystem-write confinement alone, and the residual set is disclosure: a landlock host at ABI
// 4+ discloses the UDP and pathname-AF_UNIX egress its deny box cannot fence, and that host is
// exactly as Auto-eligible as it was before it started saying so. Pinned at the domain level so
// the property holds for any backend that grows a network residual, not just the one that has it.
func TestNetworkOnlyResidualsLeaveCapsAutoEligible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		caps      ConfinementCaps
		wantAuto  bool
		wantCount int
	}{
		{
			name:      "network_only_residuals_on_a_fencing_host",
			caps:      ConfinementCaps{FSWrite: true, NetworkEgress: true, Residuals: []string{ResidualUDPEgress, ResidualUnixEgress}},
			wantAuto:  true,
			wantCount: 2,
		},
		{
			name:      "write_and_network_residuals_together",
			caps:      ConfinementCaps{FSWrite: true, NetworkEgress: true, Residuals: []string{"truncate(2)", ResidualUDPEgress, ResidualUnixEgress}},
			wantAuto:  true,
			wantCount: 3,
		},
		{
			name:      "network_residuals_never_rescue_a_host_that_cannot_fence_writes",
			caps:      ConfinementCaps{NetworkEgress: true, Residuals: []string{ResidualUDPEgress, ResidualUnixEgress}},
			wantAuto:  false,
			wantCount: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotAuto := tt.caps.AutoEligible()

			if gotAuto != tt.wantAuto {
				t.Errorf("AutoEligible() = %v, want %v (Auto reads FSWrite alone; a residual only gets said)", gotAuto, tt.wantAuto)
			}
			if len(tt.caps.Residuals) != tt.wantCount {
				t.Errorf("Residuals = %v, want %d tokens disclosed", tt.caps.Residuals, tt.wantCount)
			}
		})
	}
}

// The two network tokens are the ONE spelling internal/platform discloses and internal/probe
// filters on; a silent re-wording here would leave the filter matching nothing and put a network
// residual back into the auto-mode banner it must never reach.
func TestConfinementNetworkResidualTokensAreNamedBySyscall(t *testing.T) {
	t.Parallel()

	if ResidualUDPEgress != "connect(2) UDP" {
		t.Errorf("ResidualUDPEgress = %q, want %q", ResidualUDPEgress, "connect(2) UDP")
	}
	if ResidualUnixEgress != "connect(2) AF_UNIX" {
		t.Errorf("ResidualUnixEgress = %q, want %q", ResidualUnixEgress, "connect(2) AF_UNIX")
	}
	if ResidualUDPEgress == ResidualUnixEgress {
		t.Error("the two network residual tokens are identical; they name two different egress classes")
	}
}

// Cause is the typed half of a confiner's incapacity, and two properties make it readable at
// all. A caps value that FENCES carries none, so a caller branching on it is never handed a
// stale token on a host where the fence is real; and no enumerated cause is itself the empty
// token, which is how "fences" says it has nothing to name. The second is what the first rests
// on — collapse them and every backend's disclosure becomes ambiguous at once — and neither is
// visible from any single backend, so both are pinned here rather than in internal/platform.
func TestFenceableCapsCarryNoCause(t *testing.T) {
	t.Parallel()

	fenceable := []struct {
		name string
		caps ConfinementCaps
	}{
		{"fs only", ConfinementCaps{FSWrite: true}},
		{"fs and network", ConfinementCaps{FSWrite: true, NetworkEgress: true}},
		{"fencing with a disclosed residual", ConfinementCaps{FSWrite: true, Residuals: []string{"truncate(2)"}}},
	}
	for _, tt := range fenceable {
		if tt.caps.Cause != "" {
			t.Errorf("%s: Cause = %q, want \"\" — a backend that fences has no incapacity to name",
				tt.name, tt.caps.Cause)
		}
		if !tt.caps.AutoEligible() {
			t.Errorf("%s: AutoEligible() = false, want true — the case is meant to be a fencing host", tt.name)
		}
	}

	causes := []ConfinementCause{CauseBackendAbsent, CauseProbeTimedOut, CauseLaunchRefused}
	for i, cause := range causes {
		if cause == "" {
			t.Errorf("cause %d is the empty token, which is how a fencing host says it has none", i)
		}
		for _, other := range causes[i+1:] {
			if cause == other {
				t.Errorf("causes %q and %q are the same value; a caller cannot tell a timed-out "+
					"probe from an absent backend then", cause, other)
			}
		}
	}
}
