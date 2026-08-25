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

// TestThinkingEffortValid pins the effort vocabulary the config loader gates on (ADR 0050, widened
// by ADR 0060): every level of the seven-name union passes — plus "none", the spelling the
// OpenRouter dialect gives the "off" rung — the ZERO value passes because absence is a legitimate
// configuration (the one that emits nothing and leaves the model's own template default alone), and
// everything else is refused, including a near-miss typo and a case variant (the wire mapping is
// verbatim, so "High" is not "high").
func TestThinkingEffortValid(t *testing.T) {
	cases := []struct {
		effort domain.ThinkingEffort
		want   bool
	}{
		{domain.EffortOff, true},
		{domain.EffortNone, true},
		{domain.EffortMinimal, true},
		{domain.EffortLow, true},
		{domain.EffortMedium, true},
		{domain.EffortHigh, true},
		{domain.EffortXHigh, true},
		{domain.EffortMax, true},
		{domain.ThinkingEffort(""), true}, // unset: the send-nothing anchor, not a defect
		{domain.ThinkingEffort("hihg"), false},
		{domain.ThinkingEffort("High"), false},
		{domain.ThinkingEffort("xhi"), false},
		{domain.ThinkingEffort("maximum"), false},
	}
	for _, tc := range cases {
		if got := tc.effort.Valid(); got != tc.want {
			t.Errorf("ThinkingEffort(%q).Valid() = %v, want %v", string(tc.effort), got, tc.want)
		}
	}

	// The constants spell exactly what goes on the wire — a rename that changed a level's text
	// would change the bytes the template sees without any test noticing.
	for _, tc := range []struct {
		effort domain.ThinkingEffort
		want   string
	}{
		{domain.EffortOff, "off"},
		{domain.EffortNone, "none"},
		{domain.EffortMinimal, "minimal"},
		{domain.EffortLow, "low"},
		{domain.EffortMedium, "medium"},
		{domain.EffortHigh, "high"},
		{domain.EffortXHigh, "xhigh"},
		{domain.EffortMax, "max"},
	} {
		if string(tc.effort) != tc.want {
			t.Errorf("effort constant = %q, want %q", string(tc.effort), tc.want)
		}
	}
}

// TestModelProfileZeroValueCarriesNoRosterDeltas pins the anchor the third axis must not move: a
// zero ModelProfile is still native tool calls with no inline thinking, and now also no roster
// deltas — so a host that configures no profile builds exactly the default menu, byte-identical
// to the set it built before the axis existed.
func TestModelProfileZeroValueCarriesNoRosterDeltas(t *testing.T) {
	t.Parallel()

	var profile domain.ModelProfile

	if profile.ToolCallFormat != "" || profile.Thinking.Style != "" {
		t.Errorf("zero ModelProfile = %+v, want the native/no-thinking anchor", profile)
	}
	if len(profile.Tools.Disabled) != 0 || len(profile.Tools.Enabled) != 0 {
		t.Errorf("zero ModelProfile.Tools = %+v, want no deltas in either direction", profile.Tools)
	}
}

// TestToolRosterDeltaSpellsBothDirectionsIndependently covers the axis itself: the two lists are
// separate words about separate tools — one subtracts, the other lifts — so neither direction can
// be read as the other. A delta with only one list spoken leaves the other silent, which is what
// lets a profile say "just this one extra tool" without restating the whole roster.
func TestToolRosterDeltaSpellsBothDirectionsIndependently(t *testing.T) {
	t.Parallel()

	delta := domain.ToolRosterDelta{Disabled: []string{"terminal"}, Enabled: []string{"web_search"}}

	if len(delta.Disabled) != 1 || delta.Disabled[0] != "terminal" {
		t.Errorf("ToolRosterDelta.Disabled = %v, want [terminal]", delta.Disabled)
	}
	if len(delta.Enabled) != 1 || delta.Enabled[0] != "web_search" {
		t.Errorf("ToolRosterDelta.Enabled = %v, want [web_search]", delta.Enabled)
	}

	subtractOnly := domain.ToolRosterDelta{Disabled: []string{"python_exec"}}

	if len(subtractOnly.Enabled) != 0 {
		t.Errorf("a disabled-only delta lifted %v, want nothing", subtractOnly.Enabled)
	}
}

// TestConfigCarriesBothGlobalRosterLists pins the global rung of the roster ladder on the
// construction surface: both directions are spelled on Config, they are independent lists, and a
// zero Config asks for neither — the default every embedder gets without saying anything.
func TestConfigCarriesBothGlobalRosterLists(t *testing.T) {
	t.Parallel()

	var zero domain.Config

	if len(zero.DisabledTools) != 0 || len(zero.EnabledTools) != 0 {
		t.Errorf("zero Config roster lists = %v / %v, want both empty",
			zero.DisabledTools, zero.EnabledTools)
	}

	cfg := domain.Config{
		DisabledTools: []string{"view_diff"},
		EnabledTools:  []string{"web_search"},
	}

	if len(cfg.DisabledTools) != 1 || cfg.DisabledTools[0] != "view_diff" {
		t.Errorf("Config.DisabledTools = %v, want [view_diff]", cfg.DisabledTools)
	}
	if len(cfg.EnabledTools) != 1 || cfg.EnabledTools[0] != "web_search" {
		t.Errorf("Config.EnabledTools = %v, want [web_search]", cfg.EnabledTools)
	}
}
