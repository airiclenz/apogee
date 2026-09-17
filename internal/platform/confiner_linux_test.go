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

	fenceableNamespace := func() *namespaceConfiner { return newNamespaceConfiner("/usr/bin/bwrap", "") }
	absentNamespace := func() *namespaceConfiner { return newNamespaceConfiner("", "bwrap not on PATH") }

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
			wantCaps:    domain.ConfinementCaps{FSWrite: true, NetworkEgress: true},
			wantLine:    "landlock (fs-write: available · network: available)",
		},
		// No landlock, bwrap works: the second rung is the backend, its caps untouched.
		{
			name:        "namespace_when_landlock_cannot_fence",
			landlock:    &landlockConfiner{abi: -1, probeErrno: unix.ENOSYS},
			newNS:       fenceableNamespace,
			wantBackend: "namespace",
			wantNSCalls: 1,
			wantCaps:    domain.ConfinementCaps{FSWrite: true, NetworkEgress: true},
			wantLine:    "namespace (fs-write: available · network: available)",
		},
		// Neither: the namespace backend is returned {false, false} carrying BOTH reasons,
		// so the probe / /confine line says what would have to change on this host.
		{
			name:        "neither_carries_both_reasons",
			landlock:    &landlockConfiner{abi: -1, probeErrno: unix.ENOSYS},
			newNS:       absentNamespace,
			wantBackend: "namespace",
			wantNSCalls: 1,
			wantCaps:    domain.ConfinementCaps{Unavailable: neitherHostReason},
			wantLine:    "namespace (fs-write: unavailable · network: unavailable · why: " + neitherHostReason + ")",
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
		want = domain.ConfinementCaps{
			Unavailable: landlock.unavailableReason() + "; " + namespace.unavailable,
		}
	}

	got := NewConfiner().Capabilities()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewConfiner().Capabilities() = %+v, want the first fenceable rung's %+v", got, want)
	}
}
