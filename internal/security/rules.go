package security

// ----------------------------------------------------------------------------
// The default dangerous-action ruleset + the config-merge semantics (ADR 0012)
// ----------------------------------------------------------------------------

// DefaultDangerousRules returns the built-in dangerous-action ruleset — the default-on
// floor (ADR 0012). Membership is *almost-never-legitimate* AND *catastrophic*
// (precision-over-recall): every rule here would, on a real coding host, be a small
// model's obvious mistake, not a normal step. The patterns are narrow on purpose — they
// are written against normalized (whitespace-collapsed, lower-cased) text and must NOT
// fire on legitimate near-misses like "rm -rf ./build" or "rm -rf node_modules".
//
// Tiers (ADR 0012): TierHardRefuse has no per-call override; TierForceApproval forces
// the Approver even in Auto (a legitimate-but-risky idiom — a speed-bump, not a block).
func DefaultDangerousRules() []Rule {
	return []Rule{
		// --- Tier 1: hard-refuse ------------------------------------------------

		// `rm -rf /`, `rm -rf ~`, `rm -rf $HOME`, and root/home/system absolute
		// targets. The target alternation is the precision boundary: a relative or
		// ./ target (./build, node_modules, src/) never matches, so destructive
		// recursive deletes of project files stay allowed.
		{
			ID:     "rm-rf-root-home-system",
			Tier:   TierHardRefuse,
			Reason: "recursive force-delete of a root, home, or system path",
			Pattern: `\brm\s+(?:-[a-z]*\s+)*-?[a-z]*r[a-z]*f[a-z]*\s+` +
				`(?:/|~|\$home|/\*|/(?:etc|usr|bin|sbin|lib|boot|dev|var|sys|proc|root|home|opt)\b)`,
		},
		// `rm -fr` flag-order variant of the above (force then recurse).
		{
			ID:     "rm-fr-root-home-system",
			Tier:   TierHardRefuse,
			Reason: "recursive force-delete of a root, home, or system path",
			Pattern: `\brm\s+(?:-[a-z]*\s+)*-?[a-z]*f[a-z]*r[a-z]*\s+` +
				`(?:/|~|\$home|/\*|/(?:etc|usr|bin|sbin|lib|boot|dev|var|sys|proc|root|home|opt)\b)`,
		},
		// Classic shell fork bomb `:(){ :|:& };:` (whitespace-normalized).
		{
			ID:      "fork-bomb",
			Tier:    TierHardRefuse,
			Reason:  "fork bomb",
			Pattern: `:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`,
		},
		// Writes / deletes targeting the SSH key directory. WritesOnly: the pattern is a
		// bare path, so without the class the rule also fired on declared reads naming
		// it — which are the read fence's business, not this rule's.
		{
			ID:         "write-ssh-keys",
			Tier:       TierHardRefuse,
			Reason:     "write or delete under the SSH key directory (~/.ssh)",
			Pattern:    `(?:~|/home/[^/\s]+|/root|\$home)/\.ssh\b`,
			WritesOnly: true,
		},
		// Writes targeting credential / persistence files an autonomous mistake must
		// never touch: shell rc files, AWS/GCP/cloud creds, the crontab. Narrow to the
		// dotfile names so a project file named "config" is unaffected.
		{
			ID:     "write-credential-persistence",
			Tier:   TierHardRefuse,
			Reason: "write to a credential or shell-persistence file",
			Pattern: `(?:~|/home/[^/\s]+|/root|\$home)/` +
				`(?:\.bashrc|\.bash_profile|\.zshrc|\.profile|\.aws/credentials|\.config/gcloud|\.netrc|\.npmrc)\b`,
			WritesOnly: true,
		},
		// Writes / deletes reaching a repository's git control plane. `.git/hooks` and
		// `.git/config` are executed by the NEXT ordinary git command the operator runs — a
		// hook script, or the `core.hooksPath`, filter and textconv drivers a config names —
		// so a write here is delayed code execution outside any confinement, the same
		// persistence shape as the shell rc files above. `.git/modules` carries both files
		// again for every submodule. The boundary is the control plane, not the repository:
		// the pattern requires `.git/` followed by one of those three names, so the working
		// tree, `.gitignore`, `.gitattributes`, `.github/` and `.git/info/exclude` never
		// match, and neither does a `…/repo.git` clone URL. A bare repo's own
		// `<name>.git/config` does match, which is the same control plane by another path.
		// WritesOnly: reading `.git/config` (inspecting remotes) is ordinary in-workspace
		// work — the rule guards the write that plants delayed execution, not the read.
		{
			ID:         "write-git-control-plane",
			Tier:       TierHardRefuse,
			Reason:     "write or delete under a repository's git control plane (.git/hooks, .git/config)",
			Pattern:    `\.git/(?:hooks|config|modules)\b`,
			WritesOnly: true,
		},
		// Writes / deletes reaching apogee's own control plane. `~/.apogee` holds the global
		// config.yaml — the one source a dangerous-rule REMOVAL is honoured from
		// (MergeDangerousRules below) — plus the skill library and the session records, so a
		// write here can dissolve this floor for every later run. The boundary is the HOME
		// copy: a project's own `<workspace>/.apogee/skills` is workspace territory and never
		// matches, and a home relocated by `--config` / `APOGEE_CONFIG` is out of a text
		// pattern's reach (this is a footgun-guard, not a boundary — see doc.go). The anchor
		// spells the macOS `/Users/<name>` home alongside `/home/<name>` because the desktop
		// persona is macOS. WritesOnly is load-bearing here, not hygiene: the home skill
		// library lives under `~/.apogee/skills` and is a sanctioned extra READ root — every
		// skill run starts by listing its own skill directory and copy_file-ing resources
		// out of it, and without the class this rule hard-refused that first step.
		{
			ID:         "write-apogee-control-plane",
			Tier:       TierHardRefuse,
			Reason:     "write or delete under apogee's own control plane (~/.apogee)",
			Pattern:    `(?:~|/home/[^/\s]+|/users/[^/\s]+|/root|\$home)/\.apogee\b`,
			WritesOnly: true,
		},
		// Piping a remote download straight into a privileged disk-write (dd of=/dev/…)
		// or overwriting a block device — catastrophic and never a normal coding step.
		{
			ID:      "overwrite-block-device",
			Tier:    TierHardRefuse,
			Reason:  "raw write to a block device",
			Pattern: `\bdd\b[^|]*\bof=/dev/(?:sd|nvme|hd|mmcblk|disk)`,
		},

		// --- Tier 2: force-approval --------------------------------------------

		// `curl … | bash`, `wget … | sh`, `curl … | sudo bash` — the install-script
		// idiom. Legitimate often enough to be a speed-bump (force the Approver even in
		// Auto), not a hard block.
		{
			ID:      "remote-pipe-to-shell",
			Tier:    TierForceApproval,
			Reason:  "download piped directly into a shell (curl|bash-class)",
			Pattern: `\b(?:curl|wget|fetch)\b[^|]*\|\s*(?:sudo\s+)?(?:ba|z|d|fi)?sh\b`,
		},
		// `sudo` of an arbitrary command — a privilege escalation the human should see.
		{
			ID:      "sudo-escalation",
			Tier:    TierForceApproval,
			Reason:  "privilege escalation via sudo",
			Pattern: `\bsudo\s+\S`,
		},
	}
}

// MergeDangerousRules applies the config-merge semantics ADR 0012 fixes:
//
//   - base is the built-in default ruleset (the floor).
//   - globalAdd / globalRemove come from the user's global config
//     (~/.apogee/config.yaml) — it is the user's machine, so the global config may BOTH
//     add rules AND remove default rules (a footgun-guard the owner may relax).
//   - projectAdd comes from a project config — a project may ONLY add rules (tighten),
//     never remove, so a hostile or careless repo cannot dissolve the floor.
//
// Removal is by rule ID; an unknown remove-ID is ignored.
//
// Same-ID semantics differ by source, and this is the security-load-bearing distinction:
//
//   - A GLOBAL add with an existing ID REPLACES the earlier rule outright — it is the
//     user's own machine, so the global config is fully trusted to redefine a rule (it
//     may already remove one).
//   - A PROJECT add is TIGHTEN-ONLY and NEVER replaces. It may introduce a brand-new
//     rule, and a same-ID project add is accepted only if it is STRICTLY STRICTER than
//     the rule it would shadow (a higher Tier — e.g. promoting a TierForceApproval
//     default to TierHardRefuse); such an add then COEXISTS with the rule it tightens
//     instead of overwriting it. A same-ID project add at an equal-or-lower tier is
//     REJECTED (dropped). Together these close both halves of the replace-by-ID attack: a
//     repo can neither loosen a rule by reusing its ID at a lower tier, nor dissolve one
//     by reusing its ID at a HIGHER tier while swapping in a pattern that never fires
//     (redefining "sudo-escalation" as TierHardRefuse over "zzz-never-fires" used to
//     discard the shipped pattern and stop matching sudo altogether). Coexistence is
//     safe because Inspect reports the STRICTEST matching rule: the shipped pattern keeps
//     every match it had, and a call the project's pattern also matches is reported at
//     the project's higher tier. This is the floor a project must not be able to lower.
//
// The merged slice is returned; pass it to NewDangerousActionGuard. It may therefore hold
// more than one rule with a given ID — only ever a project tighten sitting beside the rule
// it tightened, never a base or global duplicate.
func MergeDangerousRules(base, globalAdd []Rule, globalRemove []string, projectAdd []Rule) []Rule {
	removed := make(map[string]bool, len(globalRemove))
	for _, id := range globalRemove {
		removed[id] = true
	}

	// byID maps an id to the index in out of the STRICTEST rule seen so far under it — the
	// replace target for a trusted source, and the bar a project add must clear.
	byID := make(map[string]int)
	out := make([]Rule, 0, len(base)+len(globalAdd)+len(projectAdd))

	// add merges rules from one source. honorRemove drops globally-removed IDs (base only);
	// tightenOnly governs same-ID collisions: when true (project adds), a same-ID rule is
	// APPENDED beside the rule it shadows ONLY if it is strictly stricter (higher Tier), and
	// is otherwise dropped — a project can never loosen, replace or dissolve an existing
	// rule, only add severity on top of one. When false (base/global), a same-ID rule
	// replaces in place (the trusted-source path).
	add := func(rules []Rule, honorRemove, tightenOnly bool) {
		for _, r := range rules {
			if r.ID == "" {
				continue
			}
			if honorRemove && removed[r.ID] {
				continue
			}
			if idx, ok := byID[r.ID]; ok {
				if !tightenOnly {
					out[idx] = r // a trusted source redefines the rule in place
					continue
				}
				if r.Tier <= out[idx].Tier {
					// A project add may only TIGHTEN: reject a same-ID rule that is not
					// strictly stricter than the rule it would shadow, so it cannot
					// loosen a floor rule by reusing its ID.
					continue
				}
				// Strictly stricter: KEEP BOTH. Overwriting here would let a project
				// discard the shipped Pattern under cover of a tier promotion — the
				// dissolve-by-promotion hole. Inspect reports the strictest matching rule,
				// so coexistence adds the project's severity without shrinking the shipped
				// rule's coverage. The id now resolves to the stricter of the two, so a
				// further same-ID project add must clear THAT bar.
			}
			byID[r.ID] = len(out)
			out = append(out, r)
		}
	}

	// base honours global removals; global additions and project additions do not
	// (a project add can never be "removed" by the global remove-list, and a global
	// add the user just wrote should not be cancelled by their own remove-list).
	// Project additions are tighten-only (the last argument) — the floor-preservation
	// invariant a hostile repo must not be able to break.
	add(base, true, false)
	add(globalAdd, false, false)
	add(projectAdd, false, true)
	return out
}
