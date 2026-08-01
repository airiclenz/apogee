package security

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func ruleIDs(rules []Rule) map[string]Rule {
	m := make(map[string]Rule, len(rules))
	for _, r := range rules {
		m[r.ID] = r
	}
	return m
}

func TestMergeDangerousRules_GlobalMayAddAndRemove(t *testing.T) {
	t.Parallel()
	base := []Rule{
		{ID: "rm-root", Pattern: "rm -rf /", Tier: TierHardRefuse, Reason: "rm root"},
		{ID: "fork", Pattern: "forkbomb", Tier: TierHardRefuse, Reason: "fork"},
	}
	globalAdd := []Rule{{ID: "drop-db", Pattern: "drop database", Tier: TierHardRefuse, Reason: "drops db"}}
	globalRemove := []string{"fork"} // the user disables a default on their own machine

	merged := MergeDangerousRules(base, globalAdd, globalRemove, nil)
	got := ruleIDs(merged)

	if _, ok := got["fork"]; ok {
		t.Error("global remove did not drop the default 'fork' rule")
	}
	if _, ok := got["rm-root"]; !ok {
		t.Error("global remove wrongly dropped a non-removed default")
	}
	if _, ok := got["drop-db"]; !ok {
		t.Error("global add did not include the user's added rule")
	}
}

func TestMergeDangerousRules_ProjectMayOnlyAdd(t *testing.T) {
	t.Parallel()
	base := []Rule{{ID: "rm-root", Pattern: "rm -rf /", Tier: TierHardRefuse, Reason: "rm root"}}
	projectAdd := []Rule{{ID: "no-deploy", Pattern: "deploy prod", Tier: TierForceApproval, Reason: "deploy"}}

	merged := MergeDangerousRules(base, nil, nil, projectAdd)
	got := ruleIDs(merged)

	if _, ok := got["rm-root"]; !ok {
		t.Error("project merge wrongly dropped a default")
	}
	if _, ok := got["no-deploy"]; !ok {
		t.Error("project add did not include the project's added rule")
	}
}

func TestMergeDangerousRules_ProjectCannotRemoveDefault(t *testing.T) {
	t.Parallel()
	// A project config has NO remove list at all (the signature gives it none), so a
	// default can only ever be removed by the GLOBAL config. This asserts the floor: a
	// project's only lever is projectAdd; the default survives regardless.
	base := []Rule{{ID: "rm-root", Pattern: "rm -rf /", Tier: TierHardRefuse, Reason: "rm root"}}
	merged := MergeDangerousRules(base, nil, nil, []Rule{{ID: "x", Pattern: "x", Tier: TierForceApproval, Reason: "x"}})
	if _, ok := ruleIDs(merged)["rm-root"]; !ok {
		t.Fatal("the default floor must survive any project-level config")
	}
}

func TestMergeDangerousRules_ProjectAddTightensAlongside(t *testing.T) {
	t.Parallel()
	// A strictly-stricter same-ID project add is accepted, but it COEXISTS with the rule
	// it tightens rather than replacing it: the shipped Pattern must keep every match it
	// had, so a tier promotion can only add severity, never shrink coverage.
	base := []Rule{{ID: "shared", Pattern: "old", Tier: TierForceApproval, Reason: "old"}}
	projectAdd := []Rule{{ID: "shared", Pattern: "new", Tier: TierHardRefuse, Reason: "tightened"}}
	merged := MergeDangerousRules(base, nil, nil, projectAdd)

	if len(merged) != 2 {
		t.Fatalf("tighten produced %d rules, want 2 (both the shipped rule and the project add)", len(merged))
	}
	var shipped, tightened bool
	for _, r := range merged {
		if r.ID != "shared" {
			t.Fatalf("unexpected rule id %q in the merged set", r.ID)
		}
		switch r.Pattern {
		case "old":
			shipped = true
			if r.Tier != TierForceApproval || r.Reason != "old" {
				t.Errorf("the shipped rule was altered by the project add: %+v", r)
			}
		case "new":
			tightened = true
			if r.Tier != TierHardRefuse || r.Reason != "tightened" {
				t.Errorf("the project add was altered by the merge: %+v", r)
			}
		default:
			t.Errorf("unexpected rule pattern %q in the merged set", r.Pattern)
		}
	}
	if !shipped {
		t.Error("the shipped rule was dropped: a project add must never replace one")
	}
	if !tightened {
		t.Error("the strictly-stricter project add was not accepted")
	}

	// A second same-ID project add must clear the STRICTER of the two, so an equal-tier
	// follow-up is still rejected.
	again := MergeDangerousRules(base, nil, nil, []Rule{
		{ID: "shared", Pattern: "new", Tier: TierHardRefuse, Reason: "tightened"},
		{ID: "shared", Pattern: "newer", Tier: TierHardRefuse, Reason: "equal tier"},
	})
	if len(again) != 2 {
		t.Fatalf("an equal-tier follow-up project add was accepted: %d rules, want 2", len(again))
	}
}

func TestMergeDangerousRules_ProjectCannotDissolveFloorByID(t *testing.T) {
	t.Parallel()
	// THE floor-preservation invariant: no same-ID project add — whatever tier it claims —
	// may take a shipped rule's Pattern out of the merged set. A lower or equal tier is
	// rejected outright; a strictly higher tier is accepted but coexists, so in every case
	// the shipped rule survives byte-for-byte and still fires on the text it always caught.
	// The last case is the dissolve-by-promotion attack: promoting a Tier-2 default to
	// TierHardRefuse while swapping in a pattern that never matches used to discard the
	// shipped pattern, so `sudo …` stopped matching anything at all.
	rmRoot := Rule{ID: "rm-rf-root", Pattern: `rm -rf /`, Tier: TierHardRefuse, Reason: "delete root"}
	sudo := Rule{ID: "sudo-escalation", Pattern: `\bsudo\s+\S`, Tier: TierForceApproval, Reason: "privilege escalation"}

	cases := []struct {
		name      string
		shipped   Rule
		project   Rule
		wantCount int             // rules in the merged set
		probe     domain.ToolCall // a call the shipped rule must still catch
		wantTier  Tier            // the tier Inspect must report for probe
	}{
		{
			// Loosen the tier (HardRefuse -> ForceApproval) AND neuter the pattern.
			name:      "lower tier",
			shipped:   rmRoot,
			project:   Rule{ID: "rm-rf-root", Pattern: `this-will-never-match`, Tier: TierForceApproval, Reason: "neutered"},
			wantCount: 1,
			probe:     terminalCall("rm -rf /"),
			wantTier:  TierHardRefuse,
		},
		{
			// Same tier, but a pattern that never fires — equal tier is not strictly
			// stricter, so it must still be rejected (it could only loosen, never tighten).
			name:      "equal tier, neutered pattern",
			shipped:   rmRoot,
			project:   Rule{ID: "rm-rf-root", Pattern: `this-will-never-match`, Tier: TierHardRefuse, Reason: "neutered"},
			wantCount: 1,
			probe:     terminalCall("rm -rf /"),
			wantTier:  TierHardRefuse,
		},
		{
			// Tier promotion (ForceApproval -> HardRefuse) with a neutered pattern. The
			// add is accepted — it is strictly stricter — but it may not carry the shipped
			// pattern away with it, so `sudo …` still forces the Approver.
			name:      "tier promotion, neutered pattern",
			shipped:   sudo,
			project:   Rule{ID: "sudo-escalation", Pattern: `zzz-never-fires`, Tier: TierHardRefuse, Reason: "neutered"},
			wantCount: 2,
			probe:     terminalCall("sudo apt install curl"),
			wantTier:  TierForceApproval,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			merged := MergeDangerousRules([]Rule{tc.shipped}, nil, nil, []Rule{tc.project})

			var found bool
			for _, r := range merged {
				if r.Pattern == tc.shipped.Pattern {
					found = true
					if r != tc.shipped {
						t.Errorf("the shipped rule was altered by the project add: %+v", r)
					}
				}
			}
			if !found {
				t.Fatalf("the project add dissolved the shipped rule's pattern %q", tc.shipped.Pattern)
			}
			if len(merged) != tc.wantCount {
				t.Errorf("merged has %d rules, want %d: %+v", len(merged), tc.wantCount, merged)
			}

			// End-to-end: the guard built from the merged set still catches the call.
			d := NewDangerousActionGuard(merged).Inspect(tc.probe)
			if d.Tier != tc.wantTier {
				t.Errorf("Inspect(probe) tier = %v, want %v — the project add shrank the shipped rule's coverage",
					d.Tier, tc.wantTier)
			}
			if d.RuleID != tc.shipped.ID {
				t.Errorf("Inspect(probe) rule = %q, want %q", d.RuleID, tc.shipped.ID)
			}
		})
	}
}

func TestMergeDangerousRules_DefaultRulesetMergesCleanly(t *testing.T) {
	t.Parallel()
	// The real default ruleset round-trips through a no-op merge unchanged in count.
	def := DefaultDangerousRules()
	merged := MergeDangerousRules(def, nil, nil, nil)
	if len(merged) != len(def) {
		t.Fatalf("no-op merge changed rule count: %d vs %d", len(merged), len(def))
	}
	// And it compiles into a working guard.
	g := NewDangerousActionGuard(merged)
	if len(g.Rules()) == 0 {
		t.Fatal("default ruleset produced an empty guard")
	}
}
