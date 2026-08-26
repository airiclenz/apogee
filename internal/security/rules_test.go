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
				t.Errorf("Inspect(probe) tier = %d, want %d — the project add shrank the shipped rule's coverage",
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
	// rule may be REMOVED). Both are on the floor in every mode; the TIERS differ by what a
	// write there does. `.git/hooks|config|modules` is delayed code execution outside every
	// confinement, so it hard-refuses with no per-call override. `~/.apogee` is the Tier-2
	// forced LOOK ADR 0049 §4 describes: the human is made to see the write, and their
	// informed yes runs it — curating the skill library and editing the config are the
	// operator's own ordinary steps.
	g := DefaultDangerousActionGuard()

	cases := []struct {
		name     string
		call     domain.ToolCall
		ruleID   string
		wantTier Tier
	}{
		{"write a pre-commit hook", writeCall(".git/hooks/pre-commit"), "write-git-control-plane", TierHardRefuse},
		{"write a hook in a nested repo", writeCall("vendor/dep/.git/hooks/post-checkout"), "write-git-control-plane", TierHardRefuse},
		{"rewrite the repo-local git config", writeCall("./.git/config"), "write-git-control-plane", TierHardRefuse},
		{"write a submodule's hook", writeCall(".git/modules/sub/hooks/pre-push"), "write-git-control-plane", TierHardRefuse},
		{"write a bare repo's config", writeCall("mirror.git/config"), "write-git-control-plane", TierHardRefuse},
		{"delete the hooks directory", terminalCall("rm -rf .git/hooks"), "write-git-control-plane", TierHardRefuse},
		{"chmod a hook executable", terminalCall("chmod +x .git/hooks/pre-commit"), "write-git-control-plane", TierHardRefuse},
		{"write the apogee config", writeCall("~/.apogee/config.yaml"), "write-apogee-control-plane", TierForceApproval},
		{"write the apogee library", writeCall("/home/alice/.apogee/library/probes.yaml"), "write-apogee-control-plane", TierForceApproval},
		{"write the apogee config on macOS", writeCall("/Users/alice/.apogee/config.yaml"), "write-apogee-control-plane", TierForceApproval},
		{"copy over the apogee config", terminalCall("cp evil.yaml $HOME/.apogee/config.yaml"), "write-apogee-control-plane", TierForceApproval},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			d := g.Inspect(tc.call, nil)

			if d.Tier != tc.wantTier {
				t.Fatalf("Inspect(%q) tier = %d, want %d (rule=%q)", tc.name, d.Tier, tc.wantTier, d.RuleID)
			}
			if d.RuleID != tc.ruleID {
				t.Errorf("Inspect(%q) rule = %q, want %q", tc.name, d.RuleID, tc.ruleID)
			}
		})
	}
}

// TestDefaultDangerousRules_ApogeeControlPlaneReadHintsTheSanctionedRoute pins the known
// false positive the rule's Hint exists for: the terminal declares no read-source keys, so
// a shell command that only READS from the home skill library still trips the write rule.
// The look stands (WritesOnly narrows by declared class, not by parsing shell text) — but
// the Decision must carry the Hint naming the dedicated tools, so a small model reroutes
// instead of looping on rewrites of the write half of its command. At Tier 2 that Hint is
// what the Approval prompt shows the human as its remedy and what a denied call hands back
// to the model (internal/agent).
func TestDefaultDangerousRules_ApogeeControlPlaneReadHintsTheSanctionedRoute(t *testing.T) {
	t.Parallel()
	g := DefaultDangerousActionGuard()

	call := terminalCall("cp /home/u/.apogee/skills/x/prompts/a.md /tmp/")
	d := g.Inspect(call, nil)

	if d.Tier != TierForceApproval {
		t.Fatalf("Inspect tier = %d, want TierForceApproval (rule=%q)", d.Tier, d.RuleID)
	}
	if d.RuleID != "write-apogee-control-plane" {
		t.Fatalf("Inspect rule = %q, want %q", d.RuleID, "write-apogee-control-plane")
	}
	if d.Hint == "" {
		t.Error("Decision.Hint is empty, want the rule's hint naming the sanctioned read route")
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
				t.Fatalf("Inspect(%q) wrongly triggered: tier=%d rule=%q reason=%q", tc.name, d.Tier, d.RuleID, d.Reason)
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
					t.Fatalf("Inspect(%q) wrongly triggered: tier=%d rule=%q reason=%q", tc.name, d.Tier, d.RuleID, d.Reason)
				}
				return
			}
			if d.Tier != TierHardRefuse {
				t.Fatalf("Inspect(%q) tier = %d, want TierHardRefuse (rule=%q)", tc.name, d.Tier, d.RuleID)
			}
			if d.RuleID != tc.wantRule {
				t.Errorf("Inspect(%q) rule = %q, want %q", tc.name, d.RuleID, tc.wantRule)
			}
		})
	}
}

func TestDefaultDangerousRules_HomeAnchoredRulesMatchTheWindowsHome(t *testing.T) {
	t.Parallel()
	// The Windows home reaches the same rules by two routes: `normalize` (dangerous.go)
	// folds `\` to `/`, so `C:\Users\alice` arrives as `c:/users/alice` and matches the
	// `/users/<name>` branch the macOS block above pins, and `%userprofile%` — the one
	// home form the fold cannot produce — is spelled out in the anchors. Precision holds
	// as it does elsewhere: an ordinary Windows path that is not a home-anchored target,
	// and ordinary text that merely carries a backslash, stay normal coding steps
	// (wantRule "").
	g := DefaultDangerousActionGuard()

	cases := []struct {
		name     string
		call     domain.ToolCall
		wantRule string
		// wantTier is the matched rule's own tier — Tier 2 for apogee's control plane (a
		// forced look, ADR 0049 §4), Tier 1 for the rest. Ignored when wantRule is "".
		wantTier Tier
	}{
		{"write an SSH key on Windows", writeCall(`C:\Users\alice\.ssh\authorized_keys`), "write-ssh-keys", TierHardRefuse},
		{"write an npmrc under the profile variable", writeCall(`%USERPROFILE%\.npmrc`), "write-credential-persistence", TierHardRefuse},
		{"write the apogee config on Windows", writeCall(`C:\Users\alice\.apogee\config.yaml`), "write-apogee-control-plane", TierForceApproval},
		{"recursively delete a Windows home", terminalCall(`rm -rf C:\Users\alice`), "rm-rf-root-home-system", TierHardRefuse},
		{"recursively delete the profile variable", terminalCall(`rm -rf %USERPROFILE%`), "rm-rf-root-home-system", TierHardRefuse},
		{"recursively delete a Windows home, flag order", terminalCall(`rm -fr C:\Users\alice`), "rm-fr-root-home-system", TierHardRefuse},
		{"write a project file on a Windows drive", writeCall(`C:\code\app\main.go`), "", TierNone},
		{"write a relative path that merely contains users", writeCall(`docs\users\guide.md`), "", TierNone},
		{"delete a project directory relatively on Windows", terminalCall(`rm -rf build\out`), "", TierNone},
		{"an ordinary backslash escape in a command", terminalCall(`printf 'a\tb\n' > out.txt`), "", TierNone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			d := g.Inspect(tc.call, nil)

			if tc.wantRule == "" {
				if d.Triggered() {
					t.Fatalf("Inspect(%q) wrongly triggered: tier=%d rule=%q reason=%q", tc.name, d.Tier, d.RuleID, d.Reason)
				}
				return
			}
			if d.Tier != tc.wantTier {
				t.Fatalf("Inspect(%q) tier = %d, want %d (rule=%q)", tc.name, d.Tier, tc.wantTier, d.RuleID)
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
