package main

import (
	"fmt"
	"sort"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/validated"
)

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
// The decision ladder, in order:
//   - Bypass or `validated-sets: enable: false` → the surface is fully off, notices
//     included (Bypass is the stronger explicit mechanisms-off statement).
//   - An aliased match applies at ANY confidence (the human decision replaces the
//     gate); a direct label match applies at ≥ medium and is OFFERED at low.
//   - A non-empty explicit `mechanisms:` block means manual control: whatever matched,
//     the set is not applied and one notice says so (whole-set-or-nothing — a merge
//     would be an unvalidated stack).
//   - An applying entry is validated whole against the live catalogue first; a defect
//     (unknown ID after catalogue evolution, now-invalid stacking) skips the entry.
func resolveValidatedSet(opts options, userDir string) (set []apogee.MechanismID, notices []string, err error) {
	if opts.bypass || !opts.validatedSetsEnable {
		return nil, nil, nil
	}
	fp := library.ResolveFingerprint(opts.model)
	if fp.IsZero() {
		return nil, nil, nil
	}

	shipped, shipErr := validated.Shipped()
	if shipErr != nil {
		// A defective embedded bundle is a build defect the pin test catches before
		// release; degrade soft like any other bad source rather than refusing to start.
		notices = append(notices, "apogee: skipping shipped validated sets: "+shipErr.Error())
	}
	user, warns := validated.LoadUserDir(userDir)
	entries, mergeWarns := validated.Merge(shipped, user)
	for _, w := range append(warns, mergeWarns...) {
		notices = append(notices, "apogee: "+w)
	}

	decision, err := validated.Match(fp.Label, fp.Confidence, opts.validatedSetsAlias, entries)
	if err != nil {
		return nil, notices, err // the dangling alias — loud by design
	}

	switch decision.Kind {
	case validated.KindApplied, validated.KindOffered:
		if len(opts.mechanisms) > 0 {
			notices = append(notices, suppressedNotice(decision.Entry))
			return nil, notices, nil
		}
	default:
		return nil, notices, nil // no match: the D1 floor needs no banner
	}

	if decision.Kind == validated.KindOffered {
		notices = append(notices, offerNotice(decision.Entry, fp.Label))
		return nil, notices, nil
	}

	if verr := validated.Validate(decision.Entry, mechanisms.Descriptors()); verr != nil {
		notices = append(notices, fmt.Sprintf("apogee: skipping validated-set entry %q: %v", decision.Entry.Key, verr))
		return nil, notices, nil
	}

	set = append([]apogee.MechanismID(nil), decision.Entry.Set...)
	sort.Slice(set, func(i, j int) bool { return set[i] < set[j] })
	notices = append(notices, appliedNotice(decision))
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
