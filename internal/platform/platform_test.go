package platform

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func TestDenyConfinerCapabilities(t *testing.T) {
	t.Parallel()

	caps := denyConfiner{}.Capabilities()
	tests := []struct {
		name string
		got  bool
		want bool
	}{
		{"FSWrite", caps.FSWrite, false},
		{"NetworkEgress", caps.NetworkEgress, false},
		{"AutoEligible", caps.AutoEligible(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Errorf("denyConfiner caps %s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestDenyConfinerConfineRunsFnUnchanged(t *testing.T) {
	t.Parallel()

	ran := false
	sentinel := errors.New("from fn")
	err := denyConfiner{}.Confine(context.Background(), domain.ConfinementBox{}, func(context.Context) error {
		ran = true
		return sentinel
	})
	if !ran {
		t.Fatal("Confine did not run fn")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("Confine returned %v, want fn's error %v", err, sentinel)
	}
}

func TestNewDenyConfinerNotAutoEligible(t *testing.T) {
	t.Parallel()

	if NewDenyConfiner().Capabilities().AutoEligible() {
		t.Error("NewDenyConfiner() reports AutoEligible() == true, want false")
	}
}

func TestCurrentHost(t *testing.T) {
	t.Parallel()

	h := Current()
	if h == nil {
		t.Fatal("Current() returned nil")
	}

	const line = "echo hi"
	argv := h.Command(line)
	if len(argv) != 3 {
		t.Fatalf("Command(%q) = %v, want 3 elements", line, argv)
	}
	if argv[2] != line {
		t.Errorf("Command(%q) last arg = %q, want %q", line, argv[2], line)
	}

	wantShell, wantExt := "sh", ""
	if runtime.GOOS == "windows" {
		wantShell, wantExt = "cmd", ".exe"
	}
	if argv[0] != wantShell {
		t.Errorf("Command(%q)[0] = %q, want %q", line, argv[0], wantShell)
	}
	if got := h.ExecExt(); got != wantExt {
		t.Errorf("ExecExt() = %q, want %q", got, wantExt)
	}
}
