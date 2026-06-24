package security

import "testing"

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

func TestMergeDangerousRules_ProjectAddTightensInPlace(t *testing.T) {
	t.Parallel()
	// A same-ID add replaces (tightens) the earlier rule, but cannot remove it.
	base := []Rule{{ID: "shared", Pattern: "old", Tier: TierForceApproval, Reason: "old"}}
	projectAdd := []Rule{{ID: "shared", Pattern: "new", Tier: TierHardRefuse, Reason: "tightened"}}
	merged := MergeDangerousRules(base, nil, nil, projectAdd)
	got := ruleIDs(merged)
	if got["shared"].Tier != TierHardRefuse || got["shared"].Reason != "tightened" {
		t.Fatalf("same-ID project add did not replace-in-place: %+v", got["shared"])
	}
	if len(merged) != 1 {
		t.Fatalf("replace produced %d rules, want 1", len(merged))
	}
}

func TestMergeDangerousRules_ProjectCannotDissolveFloorByID(t *testing.T) {
	t.Parallel()
	// THE floor-preservation invariant: a project add must not be able to replace a
	// Tier-1 (TierHardRefuse) floor rule by reusing its ID with a looser tier and/or a
	// pattern that never matches. A same-ID project add at an equal-or-lower tier is
	// rejected outright, so the original floor rule survives intact.
	base := []Rule{
		{ID: "rm-rf-root", Pattern: `rm -rf /`, Tier: TierHardRefuse, Reason: "delete root"},
	}

	cases := []struct {
		name    string
		project Rule
	}{
		{
			// Loosen the tier (HardRefuse -> ForceApproval) AND neuter the pattern.
			name:    "lower tier",
			project: Rule{ID: "rm-rf-root", Pattern: `this-will-never-match`, Tier: TierForceApproval, Reason: "neutered"},
		},
		{
			// Same tier, but a pattern that never fires — equal tier is not strictly
			// stricter, so it must still be rejected (it could only loosen, never tighten).
			name:    "equal tier, neutered pattern",
			project: Rule{ID: "rm-rf-root", Pattern: `this-will-never-match`, Tier: TierHardRefuse, Reason: "neutered"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			merged := MergeDangerousRules(base, nil, nil, []Rule{tc.project})
			got := ruleIDs(merged)
			r, ok := got["rm-rf-root"]
			if !ok {
				t.Fatal("project add dissolved the Tier-1 floor rule entirely")
			}
			if r.Tier != TierHardRefuse {
				t.Errorf("floor rule tier = %v, want TierHardRefuse (project add loosened the floor)", r.Tier)
			}
			if r.Pattern != `rm -rf /` || r.Reason != "delete root" {
				t.Errorf("floor rule was replaced by the project add: %+v", r)
			}
			if len(merged) != 1 {
				t.Errorf("merged has %d rules, want 1 (the rejected project add must not be appended)", len(merged))
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
