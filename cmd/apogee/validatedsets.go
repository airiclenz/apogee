package main

import (
	"fmt"
	"sort"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/validated"
)

// setDecisionKind enumerates every answer the startup ladder can give about the Validated-set
// surface for one session. The zero value is setNoMatch — the inert answer — so a
// partially-built setDecision can never claim an apply.
type setDecisionKind int

const (
	// setNoMatch — an identity resolved but no entry carries its label: the model runs the
	// D1 floor, silently (an unknown model needs no banner).
	setNoMatch setDecisionKind = iota
	// setSurfaceOff — Bypass or `validated-sets: enable: false`: the surface is fully off
	// before any identity is even resolved (Bypass is the stronger explicit
	// mechanisms-off statement). setDecision.bypass says which switch.
	setSurfaceOff
	// setNoIdentity — no model id reached the ladder, so the fingerprint is zero and there
	// is nothing to key a match on (the unpinned-model session).
	setNoIdentity
	// setOffered — a direct label match at LOW confidence: automatism stays gated, the set
	// is offered, never auto-applied.
	setOffered
	// setSuppressed — an entry matched but the explicit `mechanisms:` block wins
	// (whole-set-or-nothing — a merge would be an unvalidated stack).
	setSuppressed
	// setSkipped — the matched entry fails validation against the live catalogue (unknown
	// ID after catalogue evolution, now-invalid stacking) and is skipped whole.
	setSkipped
	// setApplied — the entry applies this session.
	setApplied
)

// setDecision is the whole startup answer, carrying whatever a renderer needs to say it in
// its own voice: the startup notices on one side, `probe model`'s effect line on the other.
type setDecision struct {
	kind setDecisionKind
	// bypass distinguishes setSurfaceOff's two switches: true for Bypass, false for
	// `validated-sets: enable: false`.
	bypass bool
	// fp is the identity the ladder resolved. Zero for setSurfaceOff and setNoIdentity.
	fp domain.ModelFingerprint
	// match is validated.Match's decision; Entry (and ViaAlias/AliasFrom) are meaningful
	// for setOffered, setSuppressed, setSkipped and setApplied.
	match validated.Decision
	// aliasErr is the dangling alias — the user's own config referencing nothing. The kind
	// stays setNoMatch; startup raises it as its one loud error (the ADR 0015 removed-ID
	// posture) while the probe report stays silent about it.
	aliasErr error
	// skipErr is the catalogue's own reason for setSkipped, carried verbatim so both
	// surfaces print the same explanation of the same defect.
	skipErr error
	// loadNotices are the source-loading warnings (defective shipped bundle, unreadable or
	// malformed user entries, merge conflicts), unprefixed — each renderer adds its own
	// framing. Only collected once an identity resolved, matching the surface's silence
	// when it has nothing to decide.
	loadNotices []string
}

// startupSetDecision is THE identity-and-match ladder: what the next session start decides
// about the Validated-set surface for this (model, endpoint, config) triple. It exists once,
// used by both resolveValidatedSet (which enacts it) and `probe model`'s autoApplyKeys (which
// reports it), because the two used to be hand-maintained twins and diverged twice in one
// review cycle — parity by construction is the only durable fix (ADR 0021 §4: the probe
// report may never promise an effect startup will not deliver).
//
// The ladder, in order:
//   - Bypass or `validated-sets: enable: false` → the surface is fully off.
//   - The full identity ladder (ADR 0021 §3) via library.ResolveFingerprintFrom: the
//     weights-hash when the model id is a reachable file, else a behavioral record a previous
//     `apogee probe model` left for THIS endpoint and label, else the bare label. An empty
//     model id resolves nothing at all.
//   - An aliased match applies at ANY confidence (the human decision replaces the gate); a
//     direct label match applies at ≥ medium and is OFFERED at low.
//   - A non-empty explicit `mechanisms:` block means manual control: whatever matched, the
//     set is not applied.
//   - An applying entry is validated whole against the live catalogue; a defect skips it.
func startupSetDecision(opts options, userDir, probeDir string) setDecision {
	if opts.bypass || !opts.validatedSetsEnable {
		return setDecision{kind: setSurfaceOff, bypass: opts.bypass}
	}
	fp := library.ResolveFingerprintFrom(library.Sources{
		ModelID:  opts.model,
		Endpoint: opts.endpoint,
		ProbeDir: probeDir,
	})
	if fp.IsZero() {
		return setDecision{kind: setNoIdentity}
	}

	out := setDecision{fp: fp}
	shipped, shipErr := validated.Shipped()
	if shipErr != nil {
		// A defective embedded bundle is a build defect the pin test catches before
		// release; degrade soft like any other bad source rather than refusing to start.
		out.loadNotices = append(out.loadNotices, "skipping shipped validated sets: "+shipErr.Error())
	}
	user, warns := validated.LoadUserDir(userDir)
	entries, mergeWarns := validated.Merge(shipped, user)
	out.loadNotices = append(out.loadNotices, append(warns, mergeWarns...)...)

	match, err := validated.Match(fp.Label, fp.Confidence, opts.validatedSetsAlias, entries)
	if err != nil {
		out.aliasErr = err
		return out
	}
	out.match = match

	switch match.Kind {
	case validated.KindApplied, validated.KindOffered:
		if len(opts.mechanisms) > 0 {
			out.kind = setSuppressed
			return out
		}
	default:
		out.kind = setNoMatch
		return out
	}
	if match.Kind == validated.KindOffered {
		out.kind = setOffered
		return out
	}
	if verr := validated.Validate(match.Entry, mechanisms.Descriptors()); verr != nil {
		out.kind = setSkipped
		out.skipErr = verr
		return out
	}
	out.kind = setApplied
	return out
}

// resolveValidatedSet is the Validated-set runtime surface's whole decision for this
// session (ADR 0016 + its 2026-07-19 realisation), placed HERE at product wire time so
// the engine never auto-enables anything — ADR 0015's "EnableMechanisms is the one
// enable path" stands, and a bench arm can never be contaminated by a matching set.
//
// It returns the enable set to fold into Config.EnableMechanisms (nil when nothing
// applies), the per-session stderr notices to print, and an error ONLY for the one loud
// case: a dangling alias, which is the user's own config referencing nothing (the ADR
// 0015 removed-ID posture). Every data defect is soft — a skip notice, never a blocked
// startup — because auto-enable is a convenience layer above a safe floor.
//
// The decision itself is startupSetDecision's; this function only renders it in the
// startup voice and enacts the one applying case.
func resolveValidatedSet(opts options, userDir, probeDir string) (set []apogee.MechanismID, notices []string, err error) {
	d := startupSetDecision(opts, userDir, probeDir)
	for _, w := range d.loadNotices {
		notices = append(notices, "apogee: "+w)
	}
	if d.aliasErr != nil {
		return nil, notices, d.aliasErr // the dangling alias — loud by design
	}

	switch d.kind {
	case setSuppressed:
		notices = append(notices, suppressedNotice(d.match.Entry))
	case setOffered:
		notices = append(notices, offerNotice(d.match.Entry, d.fp.Label))
	case setSkipped:
		notices = append(notices, fmt.Sprintf("apogee: skipping validated-set entry %q: %v", d.match.Entry.Key, d.skipErr))
	case setApplied:
		set = append([]apogee.MechanismID(nil), d.match.Entry.Set...)
		sort.Slice(set, func(i, j int) bool { return set[i] < set[j] })
		notices = append(notices, appliedNotice(d.match))
	default:
		// setSurfaceOff, setNoIdentity, setNoMatch: the D1 floor needs no banner.
	}
	return set, notices, nil
}

// appliedNotice is the per-session line for an applying set (ADR 0016 §5's "visible
// per-session notice"): it names the entry, the mechanism count, the licensing
// campaign, the source (shipped / user-local), and the off-switch. Pure so the wording
// is table-testable (the contextWindowNotice pattern).
func appliedNotice(d validated.Decision) string {
	via := ""
	if d.ViaAlias {
		via = fmt.Sprintf(" via alias %s -> %s", d.AliasFrom, d.Entry.Key)
	}
	return fmt.Sprintf("apogee: Validated set for %s applied%s — %d mechanisms on (campaign %s; %s). "+
		"Turn off with validated-sets: enable: false.",
		d.Entry.Key, via, len(d.Entry.Set), d.Entry.Evidence.Campaign, d.Entry.Source)
}

// offerNotice is the low-confidence offer (ADR 0016 realisation: below medium the
// surface offers, never auto-applies): the model identity is a name-only match, so
// applying is an explicit human decision — the notice carries the exact alias YAML to
// paste, §3's own mechanism.
func offerNotice(e validated.Entry, label string) string {
	return fmt.Sprintf("apogee: a Validated set exists for %q but the model identity is name-only "+
		"(low confidence). To apply it, add to ~/.apogee/config.yaml:\n"+
		"  validated-sets:\n    alias:\n      %q: %q", e.Key, label, e.Key)
}

// suppressedNotice is the manual-control line: a non-empty explicit `mechanisms:` block
// wins over any match (whole-set-or-nothing — the set applies verbatim or not at all,
// never merged into hand-picked Mechanisms).
func suppressedNotice(e validated.Entry) string {
	return fmt.Sprintf("apogee: a Validated set matches %s but your explicit mechanisms: config "+
		"takes precedence; set not applied.", e.Key)
}
