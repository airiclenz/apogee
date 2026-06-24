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
