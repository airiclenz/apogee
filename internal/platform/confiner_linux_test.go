//go:build linux

package platform

import (
	"reflect"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/probe"
)

// The neither-host wording every surface carries once both rungs have said no: the
// namespace backend is named, and the reason cell joins landlock's errno to bwrap's absence.
const neitherHostReason = "landlock unavailable (landlock_create_ruleset: function not implemented); bwrap not on PATH"

func TestSelectLinuxConfiner(t *testing.T) {
	t.Parallel()

	fenceableNamespace := func() *namespaceConfiner { return newNamespaceConfiner("/usr/bin/bwrap", "", "") }
	absentNamespace := func() *namespaceConfiner {
		return newNamespaceConfiner("", "bwrap not on PATH", domain.CauseBackendAbsent)
	}

	tests := []struct {
		name        string
		landlock    *landlockConfiner
		newNS       func() *namespaceConfiner
		wantBackend string
		wantNSCalls int
		wantCaps    domain.ConfinementCaps
		wantLine    string
	}{
		// Both rungs work: landlock wins (kernel-enforced, no external binary) and the
		// namespace probe — a real fork of bwrap — is never paid for.
		{
			name:        "landlock_wins_and_namespace_is_not_constructed",
			landlock:    &landlockConfiner{abi: 4},
			newNS:       fenceableNamespace,
			wantBackend: "landlock",
			wantNSCalls: 0,
			// ABI 4 can deny TCP, and TCP is all landlock's network rights cover, so the
			// selected backend's caps carry the two egress classes a deny box still passes —
			// and the line the user reads names them.
			wantCaps: domain.ConfinementCaps{
				FSWrite:       true,
				NetworkEgress: true,
				Residuals:     []string{domain.ResidualUDPEgress, domain.ResidualUnixEgress},
			},
			wantLine: "landlock (fs-write: available · network: available · unfenced: connect(2) UDP, connect(2) AF_UNIX)",
		},
		// No landlock, bwrap works: the second rung is the backend, its caps untouched — and
		// those caps carry the one egress class `--unshare-net` cannot reach, a pathname
		// AF_UNIX socket the read-only root binds into the box, so the line names it too.
		{
			name:        "namespace_when_landlock_cannot_fence",
			landlock:    &landlockConfiner{abi: -1, probeErrno: unix.ENOSYS},
			newNS:       fenceableNamespace,
			wantBackend: "namespace",
			wantNSCalls: 1,
			wantCaps: domain.ConfinementCaps{
				FSWrite:       true,
				NetworkEgress: true,
				Residuals:     []string{domain.ResidualUnixEgress},
			},
			wantLine: "namespace (fs-write: available · network: available · unfenced: connect(2) AF_UNIX)",
		},
		// Neither: the namespace backend is returned {false, false} carrying BOTH reasons,
		// so the probe / /confine line says what would have to change on this host.
		{
			name:        "neither_carries_both_reasons",
			landlock:    &landlockConfiner{abi: -1, probeErrno: unix.ENOSYS},
			newNS:       absentNamespace,
			wantBackend: "namespace",
			wantNSCalls: 1,
			// The sentence joins both rungs; the CAUSE is the namespace rung's own, because
			// that is the backend the selector returned. A zero here would leave a caller
			// unable to tell an unfenceable host from a fenceable one.
			wantCaps: domain.ConfinementCaps{Unavailable: neitherHostReason, Cause: domain.CauseBackendAbsent},
			wantLine: "namespace (fs-write: unavailable · network: unavailable · why: " + neitherHostReason + ")",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			counting := func() *namespaceConfiner {
				calls++
				return tt.newNS()
			}

			got := selectLinuxConfiner(tt.landlock, counting)

			if calls != tt.wantNSCalls {
				t.Errorf("namespace constructor called %d times, want %d", calls, tt.wantNSCalls)
			}
			if name := probe.BackendName(got); name != tt.wantBackend {
				t.Errorf("BackendName = %q, want %q (%T)", name, tt.wantBackend, got)
			}
			if tt.wantBackend == "landlock" && got != domain.Confiner(tt.landlock) {
				t.Errorf("selector returned %T rather than the constructed landlock backend", got)
			}
			if caps := got.Capabilities(); !reflect.DeepEqual(caps, tt.wantCaps) {
				t.Errorf("Capabilities() = %+v, want %+v", caps, tt.wantCaps)
			}
			if line := probe.CapabilityLine(probe.BackendName(got), got.Capabilities()); line != tt.wantLine {
				t.Errorf("CapabilityLine = %q, want %q", line, tt.wantLine)
			}
		})
	}
}

func TestNewConfinerOnThisHost(t *testing.T) {
	// Not parallel: NewNamespaceConfiner launches bwrap for real. The selector must agree
	// with its own rungs on the real machine — landlock's caps where landlock fences, the
	// namespace backend's where only bwrap does, and both reasons joined where neither does.
	landlock := NewLandlockConfiner()
	namespace := NewNamespaceConfiner()
	want := landlock.Capabilities()
	switch {
	case want.FSWrite:
	case namespace.Capabilities().FSWrite:
		want = namespace.Capabilities()
	default:
		// Both rungs said no: the prose joins them, and the typed cause is the namespace
		// rung's own — whatever this host's bwrap did (absent, refused, or too slow to
		// answer) — never landlock's and never the zero value.
		want = domain.ConfinementCaps{
			Unavailable: landlock.unavailableReason() + "; " + namespace.unavailable,
			Cause:       namespace.cause,
		}
	}

	got := NewConfiner().Capabilities()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewConfiner().Capabilities() = %+v, want the first fenceable rung's %+v", got, want)
	}
}
