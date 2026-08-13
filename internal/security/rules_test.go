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
			d := NewDangerousActionGuard(merged).Inspect(tc.probe, nil)
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

func TestDefaultDangerousRules_ControlPlanesAreOnTheFloor(t *testing.T) {
	t.Parallel()
	// The two control planes a coding host hands the model by default: the repository's
	// own `.git/` (whose hooks and config the next git command executes, outside any
	// confinement) and apogee's `~/.apogee` (whose config.yaml is the one place a floor
	// rule may be REMOVED). Both are tier 1: no per-call override, in every mode.
	g := DefaultDangerousActionGuard()

	cases := []struct {
		name   string
		call   domain.ToolCall
		ruleID string
	}{
		{"write a pre-commit hook", writeCall(".git/hooks/pre-commit"), "write-git-control-plane"},
		{"write a hook in a nested repo", writeCall("vendor/dep/.git/hooks/post-checkout"), "write-git-control-plane"},
		{"rewrite the repo-local git config", writeCall("./.git/config"), "write-git-control-plane"},
		{"write a submodule's hook", writeCall(".git/modules/sub/hooks/pre-push"), "write-git-control-plane"},
		{"write a bare repo's config", writeCall("mirror.git/config"), "write-git-control-plane"},
		{"delete the hooks directory", terminalCall("rm -rf .git/hooks"), "write-git-control-plane"},
		{"chmod a hook executable", terminalCall("chmod +x .git/hooks/pre-commit"), "write-git-control-plane"},
		{"write the apogee config", writeCall("~/.apogee/config.yaml"), "write-apogee-control-plane"},
		{"write the apogee library", writeCall("/home/alice/.apogee/library/probes.yaml"), "write-apogee-control-plane"},
		{"write the apogee config on macOS", writeCall("/Users/alice/.apogee/config.yaml"), "write-apogee-control-plane"},
		{"copy over the apogee config", terminalCall("cp evil.yaml $HOME/.apogee/config.yaml"), "write-apogee-control-plane"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			d := g.Inspect(tc.call, nil)

			if d.Tier != TierHardRefuse {
				t.Fatalf("Inspect(%q) tier = %v, want TierHardRefuse (rule=%q)", tc.name, d.Tier, d.RuleID)
			}
			if d.RuleID != tc.ruleID {
				t.Errorf("Inspect(%q) rule = %q, want %q", tc.name, d.RuleID, tc.ruleID)
			}
		})
	}
}

func TestDefaultDangerousRules_ControlPlaneNearMissesNotBlocked(t *testing.T) {
	t.Parallel()
	// Precision-over-recall (ADR 0012): the two control-plane rules stop at the control
	// plane. Everything here is a normal coding step — repo metadata that is not the
	// control plane, a clone URL ending in `.git`, and a project's own `.apogee/skills`,
	// which is workspace territory rather than the home config.
	g := DefaultDangerousActionGuard()

	cases := []struct {
		name string
		call domain.ToolCall
	}{
		{"write .gitignore", writeCall(".gitignore")},
		{"write .gitattributes", writeCall(".gitattributes")},
		{"write a GitHub workflow", writeCall(".github/workflows/ci.yml")},
		{"write .git/info/exclude", writeCall(".git/info/exclude")},
		{"clone a repo whose URL ends in .git", terminalCall("git clone https://example.com/x/y.git")},
		{"prune .git from a find", terminalCall("find . -path ./.git -prune -o -name '*.go' -print")},
		{"read the git log", terminalCall("git log --oneline -5")},
		{"write a project skill", writeCall(".apogee/skills/review/SKILL.md")},
		{"write a project skill under the workspace", writeCall("./.apogee/skills/review/SKILL.md")},
		{"write a doc about the apogee config", writeCall("docs/apogee-config.md")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			d := g.Inspect(tc.call, nil)

			if d.Triggered() {
				t.Fatalf("Inspect(%q) wrongly triggered: tier=%v rule=%q reason=%q", tc.name, d.Tier, d.RuleID, d.Reason)
			}
		})
	}
}

func TestDefaultDangerousRules_HomeAnchoredRulesMatchTheMacOSHome(t *testing.T) {
	t.Parallel()
	// The desktop persona is macOS, where a home is `/Users/<name>` rather than
	// `/home/<name>` — so the home-anchored rules spell both. `normalize` lower-cases the
	// inspectable text (dangerous.go), which is why the patterns carry `/users/`.
	// Precision-over-recall still holds: a macOS home path that is not an SSH key or one
	// of the named credential / persistence files is a normal coding step (wantRule "").
	g := DefaultDangerousActionGuard()

	cases := []struct {
		name     string
		call     domain.ToolCall
		wantRule string
	}{
		{"write an SSH key on macOS", writeCall("/Users/alice/.ssh/id_rsa"), "write-ssh-keys"},
		{"write AWS credentials on macOS", writeCall("/Users/alice/.aws/credentials"), "write-credential-persistence"},
		{"write a zsh rc on macOS", writeCall("/Users/alice/.zshrc"), "write-credential-persistence"},
		{"delete an SSH key on macOS", terminalCall("rm -f /Users/alice/.ssh/id_ed25519"), "write-ssh-keys"},
		{"recursively delete a macOS home", terminalCall("rm -rf /Users/alice"), "rm-rf-root-home-system"},
		{"recursively delete a macOS home, flag order", terminalCall("rm -fr /Users/alice"), "rm-fr-root-home-system"},
		{"write a project file in a macOS home", writeCall("/Users/alice/code/app/main.go"), ""},
		{"write the AWS config, not its credentials", writeCall("/Users/alice/.aws/config"), ""},
		{"write a file whose name merely starts with .ssh", writeCall("/Users/alice/.sshconfig.bak"), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			d := g.Inspect(tc.call, nil)

			if tc.wantRule == "" {
				if d.Triggered() {
					t.Fatalf("Inspect(%q) wrongly triggered: tier=%v rule=%q reason=%q", tc.name, d.Tier, d.RuleID, d.Reason)
				}
				return
			}
			if d.Tier != TierHardRefuse {
				t.Fatalf("Inspect(%q) tier = %v, want TierHardRefuse (rule=%q)", tc.name, d.Tier, d.RuleID)
			}
			if d.RuleID != tc.wantRule {
				t.Errorf("Inspect(%q) rule = %q, want %q", tc.name, d.RuleID, tc.wantRule)
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
