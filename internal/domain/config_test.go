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

// TestTighterMode proves the sub-agent tighten-only helper (ADR 0013): the more restrictive of
// two modes (lower on the Plan < Ask-Before < Allow-Edits < Auto ladder) wins, the result is
// symmetric, tightening below the spawn mode takes effect while loosening above it never does,
// and an off-ladder mode ranks with Ask-Before so a stray value can neither loosen nor
// over-tighten the result.
func TestTighterMode(t *testing.T) {
	cases := []struct {
		a, b, want domain.Mode
	}{
		{domain.ModeAuto, domain.ModePlan, domain.ModePlan}, // parent tightens Auto→Plan below the child
		{domain.ModePlan, domain.ModeAuto, domain.ModePlan}, // symmetric: order does not matter
		{domain.ModeAllowEdits, domain.ModeAskBefore, domain.ModeAskBefore},
		{domain.ModeAuto, domain.ModeAllowEdits, domain.ModeAllowEdits},
		{domain.ModePlan, domain.ModePlan, domain.ModePlan},       // equal ⇒ itself
		{domain.ModeAuto, domain.ModeAuto, domain.ModeAuto},       // a parent loosening back to Auto stays Auto only when the child is Auto too
		{domain.Mode(""), domain.ModeAuto, domain.Mode("")},       // off-ladder ranks as Ask-Before ⇒ tighter than Auto
		{domain.ModePlan, domain.Mode("bogus"), domain.ModePlan},  // Plan is tighter than an off-ladder (Ask-Before-ranked) mode
		{domain.ModeAllowEdits, domain.Mode(""), domain.Mode("")}, // off-ladder (Ask-Before rank) is tighter than Allow-Edits
	}
	for _, tc := range cases {
		if got := domain.TighterMode(tc.a, tc.b); got != tc.want {
			t.Errorf("TighterMode(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}
