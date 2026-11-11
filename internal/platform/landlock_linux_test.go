//go:build linux

package platform

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"

	"golang.org/x/sys/unix"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform/confinetest"
)

// TestMain intercepts the __confined-exec sentinel so the test binary itself acts as the
// in-child half of the re-exec wrapper (the standard TestHelperProcess idiom,
// confinement-execution-contract §6.1). When the harness's confined *exec.Cmd re-execs
// os.Args[0] (the test binary) with the sentinel, this dispatches to ApplyLandlockAndExec
// — exactly what cmd/apogee's main does for the product binary in P3.4.
func TestMain(m *testing.M) {
	if len(os.Args) >= 2 && os.Args[1] == confinedExecSentinel {
		os.Exit(runConfinedExecChild(os.Args[2:]))
	}
	os.Exit(m.Run())
}

// runConfinedExecChild mirrors the cmd/apogee sentinel dispatcher (P3.4): argv is
// [<encoded-box>, "--", <real argv...>]. It decodes the box, then hands off to
// ApplyLandlockAndExec, which confines this process and exec's the real argv.
func runConfinedExecChild(args []string) int {
	if len(args) < 2 || args[1] != "--" {
		fmt.Fprintln(os.Stderr, "confined-exec: malformed argv")
		return 2
	}
	box, err := DecodeConfinedBox(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := ApplyLandlockAndExec(box, args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0 // unreachable on success: ApplyLandlockAndExec exec's away.
}

// newTestConfiner returns a landlock confiner whose re-exec target is the test binary,
// so the harness's confined children land in TestMain's sentinel dispatcher above.
func newTestConfiner(t *testing.T) *landlockConfiner {
	t.Helper()
	c := NewLandlockConfiner()
	c.reexecPath = os.Args[0]
	return c
}

func TestLandlockProbe(t *testing.T) {
	// Not parallel: the confined children are real subprocesses of this binary.
	confinetest.Probe(t, newTestConfiner(t), Current(), FailFastPreamble(), newProbeDenialKiller, newProbeMergedDenialKiller)
}

// requireLandlockNetEnv, set to 1, makes TestLandlockProbeNetwork fail wherever it would skip:
// no landlock, an ABI below landlockABINetwork, or no bash for row #13's datagram. CI sets it
// on a runner known to carry ABI >= 4, so a silent skip there cannot pass for proof that the
// UDP arm ran (docs/manual/building.md).
const requireLandlockNetEnv = "APOGEE_REQUIRE_LANDLOCK_NET"

func TestLandlockProbeNetwork(t *testing.T) {
	c := newTestConfiner(t)
	required := os.Getenv(requireLandlockNetEnv) == "1"
	// Logged on every run, skipped or not, so a CI log says which kernel the arm met.
	t.Logf("landlock ABI %d (probe errno %v); the network arm needs ABI >= %d; %s=1: %v",
		c.abi, c.probeErrno, landlockABINetwork, requireLandlockNetEnv, required)
	if required {
		confinetest.ProbeNetworkRequired(t, c, Current())
		return
	}
	confinetest.ProbeNetwork(t, c, Current())
}

// The battery's row #12 branches on the backend's OWN disclosure, so on its own it cannot catch a
// backend that discloses the wrong thing consistently. This pins the disclosure to the kernel the
// tests actually run on: below ABI 3 landlock has no LANDLOCK_ACCESS_FS_TRUNCATE bit and must say
// so; at ABI 3 and above truncate is fenced and must not be named (C-06, live-reproduced
// 2026-08-25). Together the two make the truncate residual un-fakeable in either direction, the
// network half is pinned the same way from ABI 4 up, and the write-class AF_UNIX connect is
// pinned at every ABI that fences at all (ADR 0112).
func TestLandlockResidualsMatchHostABI(t *testing.T) {
	t.Parallel()
	c := NewLandlockConfiner()
	caps := c.Capabilities()
	if !caps.FSWrite {
		t.Skip("no landlock on this kernel; there is no fence to disclose a residual in")
	}
	wantResiduals := []string(nil)
	if c.abi < landlockABITruncate {
		wantResiduals = append(wantResiduals, "truncate(2)")
	}
	// The same un-fakeability, applied to the network half: a kernel that can deny TCP at all
	// must say that the deny box still passes UDP, and one that cannot deny the network must
	// not claim a gap in a fence it never offers.
	if c.abi >= landlockABINetwork {
		wantResiduals = append(wantResiduals, domain.ResidualUDPEgress)
	}
	// AF_UNIX connect is write-class and open at every ABI that fences at all (ADR 0112): a
	// host on ABI 1–3 that omits it hides the spawn-elsewhere escape from Auto's start notice.
	// It stays at ABI 9 and up too: RESOLVE_UNIX closes only the runtime dir and the container
	// sockets, and every other pathname socket and every abstract one stays reachable.
	wantResiduals = append(wantResiduals, domain.ResidualUnixEgress)
	if !slices.Equal(caps.Residuals, wantResiduals) {
		t.Errorf("host landlock ABI %d discloses Residuals = %v, want %v", c.abi, caps.Residuals, wantResiduals)
	}
}

func TestLandlockCapabilitiesHonest(t *testing.T) {
	t.Parallel()

	// The standing gap in landlock's network fence: the ABI has TCP bind and connect rights and
	// nothing more, so every kernel that can deny the network at all leaves UDP out — and the
	// write-class AF_UNIX connect, open at every ABI (ADR 0112), follows it.
	networkResiduals := []string{domain.ResidualUDPEgress, domain.ResidualUnixEgress}

	tests := []struct {
		name             string
		abi              int
		wantFSWrite      bool
		wantNetwork      bool
		wantAutoEligible bool
		wantAccess       uint64   // mask applyLandlock would request; asserted only when wantFSWrite
		wantResiduals    []string // accesses this ABI leaves unfenced: write-class while FSWrite is true, UDP egress while NetworkEgress is
		probeErrno       syscall.Errno
		wantUnavailable  string // the WHY behind FSWrite=false; "" on every fenceable row
	}{
		{"no_landlock", -1, false, false, false, 0, nil, unix.ENOSYS,
			"landlock unavailable (landlock_create_ruleset: function not implemented)"},
		// fs-only; AutoEligible on fs alone (ADR 0012). Its mask must stay at the ABI-1
		// baseline: asking for TRUNCATE (ABI 3) here was EINVAL on every create_ruleset — so
		// the access goes unfenced and is DISCLOSED instead (C-06, live-reproduced 2026-08-25).
		// AF_UNIX connect is open at every fencing ABI and write-class (ADR 0112), so it rides
		// beside truncate(2) here and on every row below.
		{"abi1_kernel_5_13", 1, true, false, true, baselineFSWriteAccess, []string{"truncate(2)", domain.ResidualUnixEgress}, 0, ""},
		// Debian 12 (6.1). REFER exists from here, and must be handled or the kernel denies
		// cross-directory rename/link outright — including inside the workspace. TRUNCATE still
		// does not, so the residual still stands.
		{"abi2_kernel_5_19", 2, true, false, true, baselineFSWriteAccess | unix.LANDLOCK_ACCESS_FS_REFER, []string{"truncate(2)", domain.ResidualUnixEgress}, 0, ""},
		// still fs-only, still Auto-eligible; TRUNCATE is finally requestable, so truncate(2)
		// leaves the list — and AF_UNIX connect, which no write right fences, stays on it.
		{"abi3_kernel_6_2", 3, true, false, true, fullFSWriteAccess, []string{domain.ResidualUnixEgress}, 0, ""},
		// Network egress is now enforceable — and the enforcement is TCP-only, so the same
		// honesty that discloses truncate(2) below ABI 3 discloses the UDP a deny box still
		// passes, ahead of the AF_UNIX token every fencing ABI carries. UDP is disjoint from
		// truncate(2) by construction: truncate(2) stops at ABI 2 and the network rights start
		// at ABI 4.
		{"abi4_kernel_6_7", 4, true, true, true, fullFSWriteAccess, networkResiduals, 0, ""},
		{"abi6_newer", 6, true, true, true, fullFSWriteAccess, networkResiduals, 0, ""},
		// RESOLVE_UNIX is handled from here and closes the user runtime dir and the container
		// sockets (ADR 0112) — but only those, so AF_UNIX connect stays disclosed. No abstract
		// scoping either: there is no ABI-6 scope row because the backend never scopes.
		{"abi9_resolve_unix", 9, true, true, true, fullFSWriteAccess | unix.LANDLOCK_ACCESS_FS_RESOLVE_UNIX, networkResiduals, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &landlockConfiner{abi: tt.abi, probeErrno: tt.probeErrno}
			caps := c.Capabilities()
			// The other half of capability honesty: a backend that cannot fence says WHY, and
			// one that can says nothing — a stale reason on a fenceable kernel would send the
			// user chasing a host fact that is not the problem.
			if caps.Unavailable != tt.wantUnavailable {
				t.Errorf("abi %d: Unavailable = %q, want %q", tt.abi, caps.Unavailable, tt.wantUnavailable)
			}
			if caps.FSWrite != tt.wantFSWrite {
				t.Errorf("abi %d: FSWrite = %v, want %v", tt.abi, caps.FSWrite, tt.wantFSWrite)
			}
			if caps.NetworkEgress != tt.wantNetwork {
				t.Errorf("abi %d: NetworkEgress = %v, want %v", tt.abi, caps.NetworkEgress, tt.wantNetwork)
			}
			// AutoEligible is FSWrite-only per ADR 0012 once P3.4 loosens it; until then the
			// domain predicate is FSWrite&&NetworkEgress. Assert the contract-mandated
			// property — fs-confinement alone makes the host confinement-usable — at the cap
			// level, independent of the (P3.4) AutoEligible predicate.
			if got := caps.FSWrite; got != tt.wantAutoEligible {
				t.Errorf("abi %d: fs-confinement availability = %v, want %v (Auto needs fs only, ADR 0012)", tt.abi, got, tt.wantAutoEligible)
			}
			// Capability honesty's second half (contract §5): a fence that leaves an access open
			// must SAY which one. Disclosing it is what keeps FSWrite=true honest at ABI 1–2 and
			// NetworkEgress=true honest from ABI 4 — and a backend that grows a residual without
			// disclosing it, or keeps disclosing one it has since closed, fails here.
			if !slices.Equal(caps.Residuals, tt.wantResiduals) {
				t.Errorf("abi %d: Residuals = %v, want %v (an unfenced access must be disclosed)",
					tt.abi, caps.Residuals, tt.wantResiduals)
			}
			// A residual is disclosure, never a gate: Auto still keys on FSWrite alone.
			if caps.AutoEligible() != tt.wantFSWrite {
				t.Errorf("abi %d: AutoEligible = %v, want %v (a residual must not change the Auto gate)",
					tt.abi, caps.AutoEligible(), tt.wantFSWrite)
			}
			if !tt.wantFSWrite {
				return // nothing is advertised, so no mask is ever handed to this kernel
			}
			// Capability honesty is a claim this kernel will ACCEPT the ruleset, not just that
			// landlock exists: advertising FSWrite while applyLandlock requests a right the ABI
			// does not know makes every confined call fail with EINVAL, so Auto becomes a mode
			// in which nothing runs. Exercising the derivation here is what makes the ABI-1 and
			// ABI-2 rows mean something below ABI 3.
			if got := accessMaskForABI(tt.abi); got != tt.wantAccess {
				t.Errorf("abi %d: accessMaskForABI = %#x, want %#x (advertising FSWrite obliges a mask this kernel accepts)", tt.abi, got, tt.wantAccess)
			}
		})
	}
}

// TestLandlockUnavailableReasonNamesTheErrno pins that the reason carries the kernel's
// own words for the failure, not a generic "unavailable": ENOSYS and EOPNOTSUPP point the
// user at different host facts (a kernel without landlock versus one booted with the LSM
// off), and the errno text is what tells them apart.
func TestLandlockUnavailableReasonNamesTheErrno(t *testing.T) {
	t.Parallel()

	c := &landlockConfiner{abi: -1, probeErrno: unix.EOPNOTSUPP}

	reason := c.unavailableReason()

	if !strings.Contains(reason, "operation not supported") {
		t.Errorf("unavailableReason() = %q; want the EOPNOTSUPP text \"operation not supported\"", reason)
	}
	if !strings.HasPrefix(reason, "landlock unavailable (landlock_create_ruleset: ") {
		t.Errorf("unavailableReason() = %q; want it to name the backend and the syscall that refused", reason)
	}
	if got := c.Capabilities().Unavailable; got != reason {
		t.Errorf("Capabilities().Unavailable = %q; want the same sentence unavailableReason() returns (%q)", got, reason)
	}
}

// baselineFSWriteAccess and fullFSWriteAccess are the two mask shapes the tests pin, spelled
// out from the unix constants rather than from landlockFSWriteAccessABI1 so a right silently
// dropped from (or added to) the production constant fails the table instead of moving with it.
const (
	baselineFSWriteAccess = uint64(unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE)

	fullFSWriteAccess = baselineFSWriteAccess |
		unix.LANDLOCK_ACCESS_FS_REFER |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE
)

func TestAccessMaskForABI(t *testing.T) {
	t.Parallel()

	// The exact mask per ABI. Both call sites (ruleset creation and every path-beneath rule)
	// derive from this function, and the kernel cross-checks them: a rule's allowed_access must
	// be a subset of the ruleset's handled_access_fs, so one shared derivation is the invariant.
	// wantTruncate is spelled per row rather than left implicit in the mask constants: it is the
	// bit Capabilities().Residuals answers for, so the mask and the disclosure are pinned against
	// the same table and cannot drift apart (C-06).
	tests := []struct {
		name         string
		abi          int
		want         uint64
		wantTruncate bool
	}{
		// Not a valid input (applyLandlock refuses below the floor); pinned so the clamp can
		// never become a mask that handles nothing — a ruleset fencing no write at all.
		{"below_floor_clamps_to_baseline", -1, baselineFSWriteAccess, false},
		{"abi1_kernel_5_13", 1, baselineFSWriteAccess, false},
		{"abi2_kernel_5_19", 2, baselineFSWriteAccess | unix.LANDLOCK_ACCESS_FS_REFER, false},
		{"abi3_kernel_6_2", 3, fullFSWriteAccess, true},
		{"abi4_kernel_6_7", 4, fullFSWriteAccess, true},
		{"abi6_newer", 6, fullFSWriteAccess, true},
		{"abi8_before_resolve_unix", 8, fullFSWriteAccess, true},
		{"abi9_resolve_unix", 9, fullFSWriteAccess | unix.LANDLOCK_ACCESS_FS_RESOLVE_UNIX, true},
		{"abi99_future", 99, fullFSWriteAccess | unix.LANDLOCK_ACCESS_FS_RESOLVE_UNIX, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := accessMaskForABI(tt.abi); got != tt.want {
				t.Errorf("accessMaskForABI(%d) = %#x, want %#x", tt.abi, got, tt.want)
			}
			if got := accessMaskForABI(tt.abi)&unix.LANDLOCK_ACCESS_FS_TRUNCATE != 0; got != tt.wantTruncate {
				t.Errorf("accessMaskForABI(%d) handles TRUNCATE = %v, want %v (below ABI %d it is unfenced and disclosed as a residual)",
					tt.abi, got, tt.wantTruncate, landlockABITruncate)
			}
		})
	}
}

func TestAccessMaskForABIRightsTrackTheKernel(t *testing.T) {
	t.Parallel()

	// The properties behind the table, stated so a future right added to the mask cannot
	// regress them by moving the pinned constants too.
	for abi := landlockABIFSWrite; abi <= 10; abi++ {
		mask := accessMaskForABI(abi)

		// RESOLVE_UNIX is ABI 9: asked for earlier it is EINVAL; left out from there, a pathname
		// connect into the user runtime dir or a container socket goes unfenced (ADR 0112).
		if hasResolveUnix := mask&unix.LANDLOCK_ACCESS_FS_RESOLVE_UNIX != 0; hasResolveUnix != (abi >= landlockABIResolveUnix) {
			t.Errorf("abi %d: mask %#x handles RESOLVE_UNIX = %v, want %v (ABI %d and up)",
				abi, mask, hasResolveUnix, abi >= landlockABIResolveUnix, landlockABIResolveUnix)
		}

		// Never above the kernel: an unknown bit in handled_access_fs is EINVAL from
		// landlock_create_ruleset, and the child dies before exec.
		if abi < landlockABITruncate && mask&unix.LANDLOCK_ACCESS_FS_TRUNCATE != 0 {
			t.Errorf("abi %d: mask %#x requests TRUNCATE, which no kernel below ABI %d knows (EINVAL)", abi, mask, landlockABITruncate)
		}
		if abi < landlockABIRefer && mask&unix.LANDLOCK_ACCESS_FS_REFER != 0 {
			t.Errorf("abi %d: mask %#x requests REFER, which no kernel below ABI %d knows (EINVAL)", abi, mask, landlockABIRefer)
		}

		// Never below the kernel: REFER is denied by default even when UNHANDLED, so leaving it
		// out of a ruleset that could carry it forbids cross-directory rename/link (`git mv`)
		// even wholly inside the workspace.
		if abi >= landlockABIRefer && mask&unix.LANDLOCK_ACCESS_FS_REFER == 0 {
			t.Errorf("abi %d: mask %#x omits REFER; cross-directory rename/link is then denied inside the box too", abi, mask)
		}
		if abi >= landlockABITruncate && mask&unix.LANDLOCK_ACCESS_FS_TRUNCATE == 0 {
			t.Errorf("abi %d: mask %#x omits TRUNCATE; truncating a file outside the box would go unfenced", abi, mask)
		}

		// The write-class baseline is the floor at every ABI — the fence itself.
		if mask&baselineFSWriteAccess != baselineFSWriteAccess {
			t.Errorf("abi %d: mask %#x drops part of the ABI-1 write baseline %#x", abi, mask, baselineFSWriteAccess)
		}
		// Read and execute stay unhandled: the box bounds where a child WRITES (ADR 0012).
		for _, unhandled := range []struct {
			name string
			bit  uint64
		}{
			{"LANDLOCK_ACCESS_FS_READ_FILE", unix.LANDLOCK_ACCESS_FS_READ_FILE},
			{"LANDLOCK_ACCESS_FS_READ_DIR", unix.LANDLOCK_ACCESS_FS_READ_DIR},
			{"LANDLOCK_ACCESS_FS_EXECUTE", unix.LANDLOCK_ACCESS_FS_EXECUTE},
		} {
			if mask&unhandled.bit != 0 {
				t.Errorf("abi %d: mask %#x handles %s; the box is workspace-WRITE-only", abi, mask, unhandled.name)
			}
		}
	}
}

func TestDeviceAccessMaskForABI(t *testing.T) {
	t.Parallel()

	// The file-applicable mask carried by a rule whose parent_fd is not a directory: the /dev/null
	// exemption and any writable root that is a regular file. It is derived separately from a
	// directory root's mask — see the subset/file-applicability properties pinned for both fd
	// types in TestDeviceAccessMaskStaysRuleApplicable below.
	tests := []struct {
		name string
		abi  int
		want uint64
	}{
		// Not a valid input (applyLandlock refuses below the floor); pinned so the clamp stays a
		// mask that still grants the write the exemption exists for.
		{"below_floor_clamps_to_write_file", -1, unix.LANDLOCK_ACCESS_FS_WRITE_FILE},
		{"abi1_kernel_5_13", 1, unix.LANDLOCK_ACCESS_FS_WRITE_FILE},
		{"abi2_kernel_5_19", 2, unix.LANDLOCK_ACCESS_FS_WRITE_FILE},
		// From ABI 3 the ruleset handles TRUNCATE, so `> /dev/null` (O_TRUNC) is denied unless
		// the exemption rule carries it too.
		{"abi3_kernel_6_2", 3, devNullAccessWithTruncate},
		{"abi4_kernel_6_7", 4, devNullAccessWithTruncate},
		{"abi6_newer", 6, devNullAccessWithTruncate},
		{"abi99_future", 99, devNullAccessWithTruncate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := deviceAccessMaskForABI(tt.abi); got != tt.want {
				t.Errorf("deviceAccessMaskForABI(%d) = %#x, want %#x", tt.abi, got, tt.want)
			}
		})
	}
}

// devNullAccessWithTruncate is the ABI-3+ shape of the exemption mask, spelled from the unix
// constants rather than from the production function so the table pins a value instead of
// restating the code.
const devNullAccessWithTruncate = uint64(unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE)

func TestDeviceAccessMaskStaysRuleApplicable(t *testing.T) {
	t.Parallel()

	// The two properties the kernel enforces on every path-beneath rule, checked on the mask
	// allowWriteBeneath actually picks (ruleAccessMaskForABI) for both kinds of parent_fd.
	// Breaking either one does not merely lose the rule: landlock_add_rule answers EINVAL,
	// applyLandlock fails, and every confined call on the host dies before exec.
	for abi := landlockABIFSWrite; abi <= 10; abi++ {
		handled := accessMaskForABI(abi)

		// A directory root takes the whole handled mask but RESOLVE_UNIX, so a confined child can
		// create, remove and rename beneath it — and a root that contains a closed socket path
		// (a root at /run) cannot re-open it, since a rule inherits beneath its parent_fd.
		if dirMask := ruleAccessMaskForABI(abi, true); dirMask != handled&^unix.LANDLOCK_ACCESS_FS_RESOLVE_UNIX {
			t.Errorf("abi %d: directory rule mask %#x, want the handled mask less RESOLVE_UNIX %#x",
				abi, dirMask, handled&^unix.LANDLOCK_ACCESS_FS_RESOLVE_UNIX)
		}

		mask := ruleAccessMaskForABI(abi, false)
		if want := deviceAccessMaskForABI(abi); mask != want {
			t.Errorf("abi %d: file rule mask %#x, want the file-applicable mask %#x", abi, mask, want)
		}

		// A rule's allowed_access must be a subset of the ruleset's handled_access_fs.
		if mask&^handled != 0 {
			t.Errorf("abi %d: file rule mask %#x is not a subset of the handled mask %#x (landlock_add_rule EINVAL)", abi, mask, handled)
		}
		// A rule on a FILE parent_fd may carry only file-applicable rights.
		for _, dirOnly := range []struct {
			name string
			bit  uint64
		}{
			{"LANDLOCK_ACCESS_FS_MAKE_DIR", unix.LANDLOCK_ACCESS_FS_MAKE_DIR},
			{"LANDLOCK_ACCESS_FS_MAKE_REG", unix.LANDLOCK_ACCESS_FS_MAKE_REG},
			{"LANDLOCK_ACCESS_FS_MAKE_SYM", unix.LANDLOCK_ACCESS_FS_MAKE_SYM},
			{"LANDLOCK_ACCESS_FS_MAKE_SOCK", unix.LANDLOCK_ACCESS_FS_MAKE_SOCK},
			{"LANDLOCK_ACCESS_FS_MAKE_FIFO", unix.LANDLOCK_ACCESS_FS_MAKE_FIFO},
			{"LANDLOCK_ACCESS_FS_MAKE_BLOCK", unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK},
			{"LANDLOCK_ACCESS_FS_MAKE_CHAR", unix.LANDLOCK_ACCESS_FS_MAKE_CHAR},
			{"LANDLOCK_ACCESS_FS_REMOVE_DIR", unix.LANDLOCK_ACCESS_FS_REMOVE_DIR},
			{"LANDLOCK_ACCESS_FS_REMOVE_FILE", unix.LANDLOCK_ACCESS_FS_REMOVE_FILE},
			{"LANDLOCK_ACCESS_FS_REFER", unix.LANDLOCK_ACCESS_FS_REFER},
		} {
			if mask&dirOnly.bit != 0 {
				t.Errorf("abi %d: file rule mask %#x carries directory-only %s on a file parent_fd (landlock_add_rule EINVAL)", abi, mask, dirOnly.name)
			}
		}
		// And a file rule must still grant the write it exists for.
		if mask&unix.LANDLOCK_ACCESS_FS_WRITE_FILE == 0 {
			t.Errorf("abi %d: file rule mask %#x grants no WRITE_FILE; `2>/dev/null` and a file root stay denied inside the box", abi, mask)
		}
	}
}

func TestLandlockAllowsDevNullThroughTheFence(t *testing.T) {
	// Not parallel: the confined children are real subprocesses of this binary.
	c := newTestConfiner(t)
	if !c.Capabilities().FSWrite {
		t.Skip("landlock unavailable on this host (FSWrite==false); skipping the live /dev/null exemption probe")
	}

	ws := t.TempDir()
	outside := t.TempDir()
	box := domain.ConfinementBox{WorkspaceRoot: ws}

	t.Run("dev_null_write_succeeds", func(t *testing.T) {
		// Both halves of the idiom that the fence broke: the stderr redirect every confined
		// terminal call uses, and a truncating `>` redirect (O_TRUNC, hence the TRUNCATE right).
		const line = `: 2>/dev/null && echo x > /dev/null`
		if err := runConfinedShellLine(t, c, box, line); err != nil {
			t.Fatalf("confined `sh -c %q` failed, want success (/dev/null is exempt from the fence): %v", line, err)
		}
	})

	t.Run("out_of_box_write_still_denied", func(t *testing.T) {
		// The exemption is exactly one device, not a hole in the fence: an ordinary out-of-box
		// write must still be OS-denied.
		target := filepath.Join(outside, "escape.txt")
		if err := runConfinedShellLine(t, c, box, "echo x > "+Current().Quote(target)); err == nil {
			t.Fatalf("confined write to %q succeeded, want OS denial (the exemption must not widen the fence)", target)
		}
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat %q = %v, want not-exist: the out-of-box write was not blocked", target, err)
		}
	})
}

func TestLandlockFileRootIsWritableInPlace(t *testing.T) {
	// Not parallel: the confined children are real subprocesses of this binary.
	c := newTestConfiner(t)
	if !c.Capabilities().FSWrite {
		t.Skip("landlock unavailable on this host (FSWrite==false); skipping the live file-root probe")
	}

	ws := t.TempDir()
	dir := t.TempDir()
	fileRoot := filepath.Join(dir, "root.txt")
	if err := os.WriteFile(fileRoot, []byte("before\n"), 0o600); err != nil {
		t.Fatalf("seed file root: %v", err)
	}
	box := domain.ConfinementBox{WorkspaceRoot: ws, WritablePaths: []string{fileRoot}}
	q := Current().Quote

	t.Run("file_root_write_succeeds", func(t *testing.T) {
		// A truncating `>` redirect: WRITE_FILE plus TRUNCATE on the file's own rule. Before the
		// fix the rule carried directory-only rights and landlock_add_rule's EINVAL failed the
		// whole confinement.
		if err := runConfinedShellLine(t, c, box, "echo after > "+q(fileRoot)); err != nil {
			t.Fatalf("confined write to the file root %q failed, want success: %v", fileRoot, err)
		}
		got, err := os.ReadFile(fileRoot)
		if err != nil {
			t.Fatalf("read file root: %v", err)
		}
		if string(got) != "after\n" {
			t.Fatalf("file root holds %q, want %q", got, "after\n")
		}
	})

	t.Run("sibling_write_denied", func(t *testing.T) {
		// The rule covers the file alone, never its directory: a sibling stays outside the box.
		sibling := filepath.Join(dir, "sibling.txt")
		if err := runConfinedShellLine(t, c, box, "echo x > "+q(sibling)); err == nil {
			t.Fatalf("confined write to sibling %q succeeded, want OS denial (a file root must not open its directory)", sibling)
		}
		if _, err := os.Stat(sibling); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat %q = %v, want not-exist: the sibling write was not blocked", sibling, err)
		}
	})

	t.Run("file_root_unlink_denied", func(t *testing.T) {
		// Unlink is a right on the parent directory, which a file root does not grant.
		if err := runConfinedShellLine(t, c, box, "rm -f "+q(fileRoot)); err == nil {
			t.Fatalf("confined rm of the file root %q succeeded, want OS denial (a file root grants in-place write only)", fileRoot)
		}
		if _, err := os.Stat(fileRoot); err != nil {
			t.Fatalf("stat %q = %v, want the file root still present", fileRoot, err)
		}
	})
}

// runConfinedShellLine runs line through the platform shell, confined to box by c, and returns
// the run error. It mirrors confinetest's runConfined for probes that are landlock-specific and
// so cannot join the cross-platform battery — /dev/null is a POSIX device with no Windows
// counterpart the battery could assert on.
func runConfinedShellLine(t *testing.T, c *landlockConfiner, box domain.ConfinementBox, line string) error {
	t.Helper()
	ctx := context.Background()
	argv := Current().Command(line)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if err := c.Confine(ctx, box, cmd); err != nil {
		t.Fatalf("Confine(%v): %v", argv, err)
	}
	return cmd.Run()
}

func TestLandlockConfineRewritesCmd(t *testing.T) {
	t.Parallel()

	c := &landlockConfiner{abi: 4, reexecPath: "/path/to/apogee"}
	box := domain.ConfinementBox{WorkspaceRoot: "/ws"}
	cmd := exec.Command("/bin/echo", "hello", "world")

	if err := c.Confine(context.Background(), box, cmd); err != nil {
		t.Fatalf("Confine: %v", err)
	}

	if cmd.Path != "/path/to/apogee" {
		t.Errorf("cmd.Path = %q, want re-exec self %q", cmd.Path, "/path/to/apogee")
	}
	if len(cmd.Args) < 5 {
		t.Fatalf("cmd.Args = %v, too short", cmd.Args)
	}
	if cmd.Args[0] != "/path/to/apogee" {
		t.Errorf("cmd.Args[0] = %q, want self", cmd.Args[0])
	}
	if cmd.Args[1] != confinedExecSentinel {
		t.Errorf("cmd.Args[1] = %q, want sentinel %q", cmd.Args[1], confinedExecSentinel)
	}
	// Args[2] is the encoded box; it must round-trip.
	gotBox, err := DecodeConfinedBox(cmd.Args[2])
	if err != nil {
		t.Fatalf("decode box arg: %v", err)
	}
	if gotBox.WorkspaceRoot != "/ws" {
		t.Errorf("decoded box WorkspaceRoot = %q, want /ws", gotBox.WorkspaceRoot)
	}
	if cmd.Args[3] != "--" {
		t.Errorf("cmd.Args[3] = %q, want separator --", cmd.Args[3])
	}
	gotOrig := strings.Join(cmd.Args[4:], " ")
	if gotOrig != "/bin/echo hello world" {
		t.Errorf("original argv = %q, want %q", gotOrig, "/bin/echo hello world")
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Error("Confine did not set SysProcAttr.Setpgid for process-group teardown")
	}
}

func TestLandlockConfineForwardsResolvedPath(t *testing.T) {
	t.Parallel()

	// Regression (P3.4 review): the confined argv must carry the RESOLVED program
	// path (cmd.Path), not the bare cmd.Args[0]. The in-child half re-execs via
	// syscall.Exec, which does NO PATH lookup, so a bare "echo" would ENOENT.
	c := &landlockConfiner{abi: 4, reexecPath: "/path/to/apogee"}
	cmd := exec.Command("echo", "hi") // bare name; Go resolves cmd.Path via LookPath
	resolved := cmd.Path
	if resolved == "" || !strings.HasPrefix(resolved, "/") {
		t.Skipf("echo did not resolve to an absolute path (%q); cannot exercise regression", resolved)
	}
	if err := c.Confine(context.Background(), domain.ConfinementBox{WorkspaceRoot: "/ws"}, cmd); err != nil {
		t.Fatalf("Confine: %v", err)
	}
	// Args layout: [self, sentinel, box, "--", <program>, <args...>].
	if len(cmd.Args) < 6 {
		t.Fatalf("cmd.Args = %v, too short", cmd.Args)
	}
	if got := cmd.Args[4]; got != resolved {
		t.Errorf("confined program = %q, want resolved path %q (not bare name)", got, resolved)
	}
}

func TestLandlockConfineRejectsEmptyArgv(t *testing.T) {
	t.Parallel()

	// Confine must refuse a cmd with no argv rather than produce a malformed re-exec
	// command line — the deterministic guard before any self-resolution.
	c := &landlockConfiner{abi: 4}
	cmd := &exec.Cmd{} // no Args
	err := c.Confine(context.Background(), domain.ConfinementBox{}, cmd)
	if err == nil {
		t.Fatal("Confine with empty argv returned nil, want error")
	}
	// The message is the shared POSIX one (confine_posix.go's errNoArgv), and it must survive
	// the guard being duplicated here to keep it ahead of the self-executable resolution:
	// pinned on both backends so neither drifts after the argv wrap moved out of them.
	if got, want := err.Error(), "apogee: confine: cmd has no argv"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestApplyLandlockAndExecRejectsEmptyArgv(t *testing.T) {
	t.Parallel()

	// The in-child half must refuse an empty argv before touching landlock or exec — there
	// is nothing to exec, so it returns an error rather than producing a malformed exec.
	// This guard fires regardless of the host kernel (no landlock call is reached), so the
	// test is hermetic on this dev host where landlock is off.
	if err := ApplyLandlockAndExec(domain.ConfinementBox{}, nil); err == nil {
		t.Fatal("ApplyLandlockAndExec(nil argv) returned nil, want error")
	}
	if err := ApplyLandlockAndExec(domain.ConfinementBox{}, []string{}); err == nil {
		t.Fatal("ApplyLandlockAndExec(empty argv) returned nil, want error")
	}
}

func TestNetworkDenyDecision(t *testing.T) {
	t.Parallel()

	denyBox := domain.ConfinementBox{WorkspaceRoot: "/ws", NetworkAllow: []string{"example.com:443"}}
	openBox := domain.ConfinementBox{WorkspaceRoot: "/ws"}

	tests := []struct {
		name          string
		box           domain.ConfinementBox
		abi           int
		wantHandleNet bool
		wantErr       bool // expect ErrConfinementUnavailable (fail-closed)
	}{
		// Network open (empty NetworkAllow): never restricts, never fails, any ABI.
		{"open_abi1", openBox, 1, false, false},
		{"open_abi4", openBox, 4, false, false},
		// Network deny + enforceable (ABI >= 4): restrict TCP connect.
		{"deny_abi4", denyBox, 4, true, false},
		{"deny_abi6", denyBox, 6, true, false},
		// Network deny but NOT enforceable (ABI < 4): FAIL CLOSED rather than running open.
		{"deny_abi1_failclosed", denyBox, 1, false, true},
		{"deny_abi3_failclosed", denyBox, 3, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handleNet, err := networkDenyDecision(tt.box, tt.abi)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("abi %d, deny box: err = nil, want fail-closed (network-deny unenforceable must not run open)", tt.abi)
				}
				if !errors.Is(err, domain.ErrConfinementUnavailable) {
					t.Errorf("abi %d: err = %v, want ErrConfinementUnavailable so dispatch gates the call", tt.abi, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("abi %d: unexpected err: %v", tt.abi, err)
			}
			if handleNet != tt.wantHandleNet {
				t.Errorf("abi %d: handleNet = %v, want %v", tt.abi, handleNet, tt.wantHandleNet)
			}
		})
	}
}

func TestEncodeDecodeBoxRoundTrip(t *testing.T) {
	t.Parallel()

	box := domain.ConfinementBox{
		WorkspaceRoot: "/ws",
		WritablePaths: []string{"/tmp/cache", "/tmp/build"},
		NetworkAllow:  []string{"example.com:443"},
	}
	enc, err := encodeBox(box)
	if err != nil {
		t.Fatalf("encodeBox: %v", err)
	}
	got, err := DecodeConfinedBox(enc)
	if err != nil {
		t.Fatalf("DecodeConfinedBox: %v", err)
	}
	if got.WorkspaceRoot != box.WorkspaceRoot ||
		len(got.WritablePaths) != len(box.WritablePaths) ||
		len(got.NetworkAllow) != len(box.NetworkAllow) {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, box)
	}
}

func TestConfinedExecSentinelAccessor(t *testing.T) {
	t.Parallel()
	if ConfinedExecSentinel() != confinedExecSentinel {
		t.Errorf("ConfinedExecSentinel() = %q, want %q", ConfinedExecSentinel(), confinedExecSentinel)
	}
}

// TestLandlockResolveUnixPlan drives the RESOLVE_UNIX planner over a fake root filesystem: every
// pathname socket stays reachable through a sibling rule except the closed paths (the user runtime
// dir, the container sockets), whose ancestors never get a rule of their own, and the re-opened
// agent sockets get one on the socket file or the agent's directory (ADR 0112).
func TestLandlockResolveUnixPlan(t *testing.T) {
	t.Parallel()

	socket := &fstest.MapFile{Mode: fs.ModeSocket}
	dir := &fstest.MapFile{Mode: fs.ModeDir | 0o755}
	hostFS := fstest.MapFS{
		"bin":                             {Mode: fs.ModeSymlink, Data: []byte("usr/bin")},
		"home/u/proj/go.mod":              {},
		"run/dbus/system_bus_socket":      socket,
		"run/docker.sock":                 socket,
		"run/foo.pid":                     {},
		"run/podman/podman.sock":          socket,
		"run/podman/containers":           dir,
		"run/snapd.socket":                socket,
		"run/user/1000/bus":               socket,
		"run/user/1000/ssh.sock":          socket,
		"run/user/1000/gnupg/S.gpg-agent": socket,
		"run/user/1001/bus":               socket,
		"tmp/rt/bus":                      socket,
		"tmp/tmux-1000/default":           socket,
		"tmp/x.sock":                      socket,
		"var/run":                         {Mode: fs.ModeSymlink, Data: []byte("../run")},
	}
	resolve := func(p string, isFile bool) landlockRule {
		return landlockRule{Path: p, Access: unix.LANDLOCK_ACCESS_FS_RESOLVE_UNIX, IsFile: isFile}
	}

	tests := []struct {
		name    string
		closure domain.UnixSocketClosure
		want    []landlockRule
	}{
		{
			// Nothing to close: one rule on `/`, so handling the right denies nothing.
			name: "nothing_closed_grants_root",
			want: []landlockRule{resolve("/", false)},
		},
		{
			// /run/user/1000 closed beside a granted /run/user/1001; podman.sock closed beside a
			// granted /run/podman/containers; docker.sock closed beside a granted socket file in
			// /run. /run, /run/user and /run/podman are ancestors and get no rule; the symlinks
			// (/bin, /var/run) and the regular file (/run/foo.pid) get none either. The agent
			// sockets beneath the closed runtime dir are re-opened: ssh-agent on its socket file,
			// gpg-agent on its directory.
			name: "runtime_dir_and_container_sockets_closed",
			closure: domain.UnixSocketClosure{
				Closed:   []string{"/run/user/1000", "/run/docker.sock", "/run/podman/podman.sock"},
				Reopened: []string{"/run/user/1000/ssh.sock", "/run/user/1000/gnupg"},
			},
			want: []landlockRule{
				resolve("/home", false),
				resolve("/run/dbus", false),
				resolve("/run/podman/containers", false),
				resolve("/run/snapd.socket", true),
				resolve("/run/user/1001", false),
				resolve("/tmp", false),
				resolve("/var", false),
				resolve("/run/user/1000/ssh.sock", true),
				resolve("/run/user/1000/gnupg", false),
			},
		},
		{
			// A runtime dir under /tmp: the planner descends into /tmp, not /run, so tmux's
			// socket and a bare socket file in /tmp stay reachable and /run is granted whole.
			name:    "runtime_dir_under_tmp",
			closure: domain.UnixSocketClosure{Closed: []string{"/tmp/rt"}},
			want: []landlockRule{
				resolve("/home", false),
				resolve("/run", false),
				resolve("/tmp/tmux-1000", false),
				resolve("/tmp/x.sock", true),
				resolve("/var", false),
			},
		},
		{
			// A re-opened path that is not beneath a closed one is already granted by its
			// sibling rule; one that is missing, or that would contain a closed path, gets none.
			name: "reopened_outside_missing_or_containing_closed",
			closure: domain.UnixSocketClosure{
				Closed:   []string{"/run/user/1000"},
				Reopened: []string{"/home/u/.gnupg", "/run/user/1000/gone.sock", "/run/user"},
			},
			want: []landlockRule{
				resolve("/home", false),
				resolve("/run/dbus", false),
				resolve("/run/docker.sock", true),
				resolve("/run/podman", false),
				resolve("/run/snapd.socket", true),
				resolve("/run/user/1001", false),
				resolve("/tmp", false),
				resolve("/var", false),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := planResolveUnixRules(tt.closure, hostFS)

			if !slices.Equal(got, tt.want) {
				t.Errorf("planResolveUnixRules =\n  %v\nwant\n  %v", got, tt.want)
			}
			for _, rule := range got {
				if rule.Access != unix.LANDLOCK_ACCESS_FS_RESOLVE_UNIX {
					t.Errorf("rule %q allows %#x, want exactly RESOLVE_UNIX (never a write right)", rule.Path, rule.Access)
				}
				if slices.Contains(tt.closure.Reopened, rule.Path) {
					continue
				}
				for _, closed := range tt.closure.Closed {
					if rule.Path == closed || isBeneath(rule.Path, closed) || isBeneath(closed, rule.Path) {
						t.Errorf("rule %q re-opens closed path %q (the path, an ancestor, or beneath it)", rule.Path, closed)
					}
				}
			}
		})
	}
}

// TestLandlockConfineCarriesUnixSocketClosure pins that the socket closure is planned in the parent
// at Confine time and survives encodeBox/DecodeConfinedBox into the child, which must never re-read
// it from its own environment (its TMPDIR is the scratch dir). Below ABI 9 nothing is planned.
func TestLandlockConfineCarriesUnixSocketClosure(t *testing.T) {
	runtimeDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatalf("chmod runtime dir: %v", err)
	}
	agentSock := filepath.Join(runtimeDir, "agent.sock")
	if err := os.WriteFile(agentSock, nil, 0o600); err != nil {
		t.Fatalf("create agent socket stand-in: %v", err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("SSH_AUTH_SOCK", agentSock)

	for _, tt := range []struct {
		name       string
		abi        int
		wantClosed bool
	}{
		{"abi9_plans_and_carries", 9, true},
		{"abi8_plans_nothing", 8, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &landlockConfiner{abi: tt.abi, reexecPath: "/path/to/apogee"}
			cmd := exec.Command("/bin/true")

			if err := c.Confine(context.Background(), domain.ConfinementBox{WorkspaceRoot: "/ws"}, cmd); err != nil {
				t.Fatalf("Confine: %v", err)
			}
			got, err := DecodeConfinedBox(cmd.Args[2])
			if err != nil {
				t.Fatalf("decode box arg: %v", err)
			}

			if !tt.wantClosed {
				if len(got.UnixSockets.Closed) != 0 || len(got.UnixSockets.Reopened) != 0 {
					t.Errorf("abi %d: UnixSockets = %+v, want none planned below ABI %d", tt.abi, got.UnixSockets, landlockABIResolveUnix)
				}
				return
			}
			if !slices.Contains(got.UnixSockets.Closed, runtimeDir) {
				t.Errorf("decoded Closed = %v, want it to hold the runtime dir %q", got.UnixSockets.Closed, runtimeDir)
			}
			if !slices.Contains(got.UnixSockets.Reopened, agentSock) {
				t.Errorf("decoded Reopened = %v, want it to hold the agent socket %q", got.UnixSockets.Reopened, agentSock)
			}
		})
	}
}
