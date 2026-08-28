package security

// ----------------------------------------------------------------------------
// The default dangerous-action ruleset + the config-merge semantics (ADR 0012)
// ----------------------------------------------------------------------------

// DefaultDangerousRules returns the built-in dangerous-action ruleset — the default-on
// floor (ADR 0012). Membership is *almost-never-legitimate* AND *catastrophic*
// (precision-over-recall): every rule here would, on a real coding host, be a small
// model's obvious mistake, not a normal step. The patterns are narrow on purpose — they
// are written against normalized (whitespace-collapsed, lower-cased, `\` folded to `/`)
// text and must NOT
// fire on legitimate near-misses like "rm -rf ./build" or "rm -rf node_modules". Those
// near-misses are RELATIVE targets: the recursive-delete rules below refuse every
// absolute one on purpose, project paths included.
//
// Everyday idiom is covered — end-of-options `--`, long flags, a quoted absolute target, an
// absolute shell path after the pipe; deliberate obfuscation (`eval`, variable expansion,
// `$'…'`, base64) is not, and that is the boundary `doc.go` states.
//
// Tiers (ADR 0012): TierHardRefuse has no per-call override; TierForceApproval forces
// the Approver even in Auto — a legitimate-but-risky idiom (`curl … | bash`, `sudo`), or a
// control plane whose write the human must LOOK at (`~/.apogee`, ADR 0049 §4): a speed-bump,
// not a block.

// homeAnchor matches the start of a user's home directory in the normalized text the guard
// inspects. The three write-* rules below share it byte-for-byte, so a home form is spelled
// once, here. It carries the macOS `/Users/<name>` beside the Linux `/home/<name>` because
// the desktop persona is macOS, and it is lower-case throughout because `normalize`
// (dangerous.go) lower-cases the inspected text. Windows needs only `%userprofile%` spelled
// out: `normalize` also folds `\` to `/`, so `C:\Users\alice` arrives as `c:/users/alice`
// and matches the `/users/<name>` branch already.
const homeAnchor = `(?:~|/home/[^/\s]+|/users/[^/\s]+|/root|\$home|%userprofile%)`

// deleteTargetAnchor matches the targets the two recursive-delete rules below refuse: an
// absolute path — POSIX `/…` or a Windows drive root `c:/…`, which is what `C:\…` folds to
// — a home in any of its spellings, and the `/*` glob. The bare `/` and `[a-z]:/` branches
// are the discriminating ones: they catch every absolute target on purpose, the project's
// own directory included, so the system-path enumeration that follows them is documentation
// of the worst cases rather than a filter. A relative target (./build, node_modules, src/)
// matches nothing here — that is the precision boundary.
const deleteTargetAnchor = `(?:/|[a-z]:/|~|\$home|%userprofile%|/\*|` +
	`/(?:etc|usr|bin|sbin|lib|boot|dev|var|sys|proc|root|home|users|opt)\b)`

// The recursive-delete rules' shared flag vocabulary. `rm`'s everyday spellings put the
// two flags in either order, split them apart, mix unrelated flags between them, end the
// options with `--` and quote the target — all of which a single fused `-rf` pattern misses
// (code audit C-10, 2026-08-26). Spelling the fragments once, here, keeps the two mirror
// rules below readable and keeps them in step with each other.
const (
	// rmFlag is ONE flag token, short (`-v`, `-rf`) or long (`--verbose`, `--one-file-system`),
	// or a bare `-` — getopt permutes the flags of `rm - -rf /etc`, so the lone dash is a flag
	// token like any other and the rules must see through it. It deliberately does not match a
	// bare `--`: end-of-options is rmEndOfOptions' job, and letting a flag token swallow it
	// would make the two indistinguishable — the `\s+` that follows every use of rmFlag is what
	// stops the bare-dash branch from eating a `--` one dash at a time.
	rmFlag = `(?:--?[a-z][a-z-]*|-)`
	// rmRecursive and rmForce are the two flags that make a delete catastrophic, each in its
	// short-cluster and long spelling. The short forms match any cluster CONTAINING the
	// letter (`-rv`, `-vrf`), which is how the flags actually arrive.
	rmRecursive = `(?:-[a-z]*r[a-z]*|--recursive)`
	rmForce     = `(?:-[a-z]*f[a-z]*|--force)`
	// rmEndOfOptions is the optional `--` separator that ends the flags; the target follows it.
	rmEndOfOptions = `(?:--\s+)?`
	// quoteOpen is the optional opening quote of a quoted target (`rm -rf "/etc"`). Only the
	// OPENING quote is matched: deleteTargetAnchor's branches end wherever the path ends, and
	// requiring a matching close would buy nothing a footgun-guard needs.
	quoteOpen = `["']?`
)

func DefaultDangerousRules() []Rule {
	return []Rule{
		// --- Tier 1: hard-refuse ------------------------------------------------

		// `rm -rf /`, `rm -rf ~`, `rm -rf $HOME`, `rm -rf C:\Users\<name>` — and every
		// other ABSOLUTE target (deleteTargetAnchor above carries the alternation and the
		// reasoning behind its branches). The precision boundary is relative-vs-absolute,
		// not which absolute path: a relative or ./ target (./build, node_modules, src/)
		// never matches, so destructive recursive deletes of project files stay allowed as
		// long as they are named relatively, which is how a coding agent names them.
		// Refusing every absolute target, the project's own directory included, is the
		// deliberate choice — an absolute recursive force-delete is a small model's mistake
		// often enough, and cheap enough to re-issue relatively, that the hard refuse is
		// the right answer.
		{
			ID:     "rm-rf-root-home-system",
			Tier:   TierHardRefuse,
			Reason: "recursive force-delete of a root, home, or system path",
			Pattern: `\brm\s+(?:` + rmFlag + `\s+)*(?:-[a-z]*r[a-z]*f[a-z]*|` +
				rmRecursive + `\s+(?:` + rmFlag + `\s+)*` + rmForce + `)\s+(?:` +
				rmFlag + `\s+)*` + rmEndOfOptions + quoteOpen + deleteTargetAnchor,
		},
		// `rm -fr` flag-order variant of the above (force then recurse) — and its split
		// spellings, `rm -f -r` and `rm --force --recursive`.
		{
			ID:     "rm-fr-root-home-system",
			Tier:   TierHardRefuse,
			Reason: "recursive force-delete of a root, home, or system path",
			Pattern: `\brm\s+(?:` + rmFlag + `\s+)*(?:-[a-z]*f[a-z]*r[a-z]*|` +
				rmForce + `\s+(?:` + rmFlag + `\s+)*` + rmRecursive + `)\s+(?:` +
				rmFlag + `\s+)*` + rmEndOfOptions + quoteOpen + deleteTargetAnchor,
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
			Pattern:    homeAnchor + `/\.ssh\b`,
			WritesOnly: true,
		},
		// Writes targeting credential / persistence files an autonomous mistake must
		// never touch: shell rc files, AWS/GCP/cloud creds, the crontab. Narrow to the
		// dotfile names so a project file named "config" is unaffected.
		{
			ID:     "write-credential-persistence",
			Tier:   TierHardRefuse,
			Reason: "write to a credential or shell-persistence file",
			Pattern: homeAnchor + `/` +
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
		// Writes / deletes reaching apogee's own control plane — a Tier-2 forced LOOK, the one
		// force-approval rule written among the hard refuses above, because its subject is the
		// control plane next door to `.git/` and the tier belongs to the rule rather than to the
		// block it is spelled in (NewDangerousActionGuard sorts by tier, so ordering here is
		// documentation, not precedence). ADR 0049 §4 states the floor: `~/.apogee` is a forced
		// look, never a boundary — the human is made to SEE the write, and their informed yes
		// runs it, `~/.apogee` included. What earns the softer tier is what a write there does:
		// `.git/hooks|config` above is delayed code execution outside every confinement (the
		// shell-rc class), while `~/.apogee` holds the global config.yaml — the one source a
		// dangerous-rule REMOVAL is honoured from (MergeDangerousRules below) — plus the skill
		// library and the session records, so a write here can dissolve this floor for every
		// later run: catastrophic as a model's mistake, ordinary as the operator's own step
		// (curating the skill library, editing a scheme), which is the shape a look answers and
		// a refusal does not. The boundary is the HOME
		// copy: a project's own `<workspace>/.apogee/skills` is workspace territory and never
		// matches, and a home relocated by `--config` / `APOGEE_CONFIG` is out of a text
		// pattern's reach (this is a footgun-guard, not a boundary — see doc.go). The home
		// spellings it accepts are homeAnchor's, above. WritesOnly is load-bearing here, not
		// hygiene: the home skill
		// library lives under `~/.apogee/skills` and is a sanctioned extra READ root — every
		// skill run starts by listing its own skill directory and copy_file-ing resources
		// out of it, and without the class this rule refused that first step outright. The Hint
		// exists because WritesOnly only helps tools that DECLARE read-source keys — the
		// terminal declares none, so a shell command that merely reads from the home skill
		// library still trips this write rule, and the write-flavoured Reason alone sends a
		// small model looping on rewrites of the write half. The hint names the sanctioned
		// route instead — and at Tier 2 it now rides the Approval prompt as its remedy line and
		// the deny result's tail (internal/agent's resolution.go / dispatch.go), which is where
		// a small model actually reads it. One dir under `~/.apogee` never reaches this rule at
		// all: the session's OWN scratch dir under `~/.apogee/scratch/`, whose spellings dispatch
		// passes as a per-call exemption and maskExempt removes from the text before any rule runs
		// (ADR 0049 amendment, 2026-08-28) — the confinement box already declares that dir
		// writable (ADR 0056), so a look there answers nothing while prompting on every command
		// the model routes through its sanctioned scratch space. Other sessions' scratch dirs, and
		// every other control-plane path, keep the look.
		{
			ID:     "write-apogee-control-plane",
			Tier:   TierForceApproval,
			Reason: "write or delete under apogee's own control plane (~/.apogee)",
			Hint: "a terminal command naming ~/.apogee needs approval, even for a read; " +
				"list, read or copy from there with the dedicated tools instead (list_dir, " +
				"read_file, grep, find_files, or copy_file's source argument)",
			Pattern:    homeAnchor + `/\.apogee\b`,
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

		// `curl … | bash`, `wget … | sh`, `curl … | sudo bash`, `curl … | /bin/bash` — the
		// install-script idiom. Legitimate often enough to be a speed-bump (force the
		// Approver even in Auto), not a hard block. The optional absolute directory before
		// the shell name is there because the idiom is written with a path as often as
		// without one; the trailing `\b` is what keeps `shellcheck` out.
		{
			ID:      "remote-pipe-to-shell",
			Tier:    TierForceApproval,
			Reason:  "download piped directly into a shell (curl|bash-class)",
			Pattern: `\b(?:curl|wget|fetch)\b[^|]*\|\s*(?:sudo\s+)?(?:/[a-z0-9_./-]*/)?(?:ba|z|d|fi|k|a)?sh\b`,
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
