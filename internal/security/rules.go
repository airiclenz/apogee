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
		// Writes / deletes targeting the SSH key directory.
		{
			ID:      "write-ssh-keys",
			Tier:    TierHardRefuse,
			Reason:  "write or delete under the SSH key directory (~/.ssh)",
			Pattern: `(?:~|/home/[^/\s]+|/root|\$home)/\.ssh\b`,
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
//   - A PROJECT add is TIGHTEN-ONLY. It may introduce a brand-new rule, but a same-ID
//     project add is accepted only if it is STRICTLY STRICTER than the rule it would
//     shadow (a higher Tier — e.g. promoting a TierForceApproval default to
//     TierHardRefuse). A same-ID project add at an equal-or-lower tier is REJECTED
//     (dropped), so a hostile or careless repo cannot replace-by-ID to dissolve or loosen
//     a Tier-1 floor rule (e.g. redefining "rm-rf-root-home-system" with a pattern that
//     never matches). This is the floor a project must not be able to lower.
//
// The merged slice is returned; pass it to NewDangerousActionGuard.
func MergeDangerousRules(base, globalAdd []Rule, globalRemove []string, projectAdd []Rule) []Rule {
	removed := make(map[string]bool, len(globalRemove))
	for _, id := range globalRemove {
		removed[id] = true
	}

	byID := make(map[string]int) // id -> index in out, for replace-on-duplicate
	out := make([]Rule, 0, len(base)+len(globalAdd)+len(projectAdd))

	// add merges rules from one source. honorRemove drops globally-removed IDs (base only);
	// tightenOnly governs same-ID collisions: when true (project adds), a same-ID rule
	// replaces an existing one ONLY if it is strictly stricter (higher Tier), and is
	// otherwise dropped — a project can never loosen or dissolve an existing rule. When
	// false (base/global), a same-ID rule replaces in place (the trusted-source path).
	add := func(rules []Rule, honorRemove, tightenOnly bool) {
		for _, r := range rules {
			if r.ID == "" {
				continue
			}
			if honorRemove && removed[r.ID] {
				continue
			}
			if idx, ok := byID[r.ID]; ok {
				if tightenOnly && r.Tier <= out[idx].Tier {
					// A project add may only TIGHTEN: reject a same-ID rule that is not
					// strictly stricter than the rule it would shadow, so it cannot
					// dissolve or loosen a floor rule by reusing its ID.
					continue
				}
				out[idx] = r // replace an earlier same-ID rule (tighten in place)
				continue
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
