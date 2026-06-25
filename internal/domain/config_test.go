package domain_test

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestNextMode walks the autonomy privilege ladder: each rung advances to the next, Auto wraps
// back to Plan, and an unknown/empty mode starts the cycle at Plan (never stuck off-ladder).
func TestNextMode(t *testing.T) {
	cases := []struct {
		cur  domain.Mode
		want domain.Mode
	}{
		{domain.ModePlan, domain.ModeAskBefore},
		{domain.ModeAskBefore, domain.ModeAllowEdits},
		{domain.ModeAllowEdits, domain.ModeAuto},
		{domain.ModeAuto, domain.ModePlan}, // wrap-around
		{domain.Mode(""), domain.ModePlan},
		{domain.Mode("bogus"), domain.ModePlan},
	}
	for _, tc := range cases {
		if got := domain.NextMode(tc.cur); got != tc.want {
			t.Errorf("NextMode(%q) = %q, want %q", tc.cur, got, tc.want)
		}
	}

	// Four advances from any rung return to it — a closed 4-cycle.
	m := domain.ModePlan
	for i := 0; i < 4; i++ {
		m = domain.NextMode(m)
	}
	if m != domain.ModePlan {
		t.Errorf("four NextMode steps from Plan landed on %q, want a full wrap back to Plan", m)
	}
}
