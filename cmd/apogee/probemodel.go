package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/validated"
)

// batteryRequestTimeout bounds ONE battery call. The battery is four short exchanges against a
// local server, so a call still running after a minute is a hung server rather than a slow
// model — and a probe that wedges is worse than a probe that reports a failure, because the
// failure is itself a finding the report can state.
const batteryRequestTimeout = 60 * time.Second

// errProbeModelNeedsEndpoint is the refusal when no endpoint is configured by any layer. The
// model half cannot degrade to a partial answer the way the host half can: with nothing to
// call there is no battery, and inventing a "model unreachable" fingerprint would be exactly
// the identity-from-absent-evidence this command must never mint.
var errProbeModelNeedsEndpoint = errors.New(
	"apogee probe model: no endpoint configured — set endpoint: in config.yaml, APOGEE_ENDPOINT, " +
		"or pass --endpoint (run `apogee probe` for the free host report, which needs none)")

// errProbeModelNeedsLabel is the refusal when neither --model nor the server names a model. The
// advertised label IS the identity the record claims (ADR 0021, Amendment 2026-07-22), so with
// no label there is nothing to key a claim on and the battery would spend tokens for a report
// that could record nothing.
var errProbeModelNeedsLabel = errors.New(
	"apogee probe model: the server advertises no active model — pass --model to name the model to probe")

// probeModelCommand builds `apogee probe model` — the capability battery and the behavioral
// fingerprint (ADR 0021 §3). It is the expensive, explicit half of `probe`: it spends real
// tokens on a live Upstream and, unless --no-save, records a Medium-confidence identity that
// promotes any matching Validated set from "offered" to "auto-applied" (ADR 0016 §5). Both
// costs are stated before the first call and again in the report, and the record's path is
// printed so deleting it is a supported undo.
//
// It never runs as a side effect of anything. `apogee probe` reports the host and stops, even
// with a perfectly reachable endpoint sitting in the config — the whole point of the split.
func probeModelCommand() *cobra.Command {
	var opts options
	var noSave bool

	cmd := &cobra.Command{
		Use:   "model",
		Short: "Run the capability battery and record the model's behavioral fingerprint",
		Long: "apogee probe model asks the configured model to do three things — emit a native\n" +
			"tool call, return a JSON object, and carry a tool result into a second call — and\n" +
			"reports what it observed, an ordinal capability tier, and the model-profile knobs\n" +
			"the findings suggest (printed for you to paste; config.yaml is never touched).\n\n" +
			"It costs live model calls. It also WRITES: the behavioral fingerprint it derives is\n" +
			"recorded under the apogee home at medium confidence, which is what promotes a\n" +
			"matching Validated set from offered to auto-applied on later runs (ADR 0016 §5).\n" +
			"Probing does NOT rename your model: the identity stays the advertised label, so\n" +
			"validated-set entries, aliases and Library observations keyed on it keep matching —\n" +
			"only the confidence rises, from low to medium.\n\n" +
			"Pass --no-save to run the full battery and write nothing; the record's path is\n" +
			"printed either way, so deleting that file undoes it.\n\n" +
			"Note (2026-07-22): probe records written by an earlier build use a record format\n" +
			"this version no longer reads. They are skipped with a warning and there is no\n" +
			"migration — re-run `apogee probe model` once per model to record them again.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The same resolution a session performs (flag > env > file > default) — so the
			// model this battery measures is the model a session on this host would talk to.
			// It reads only; the host half's no-seeding rule holds here too.
			if err := applyConfig(&opts, cmd.Flags().Changed, os.Getenv, os.ReadFile, func(msg string) { cmd.PrintErrln(msg) }); err != nil {
				return err
			}
			roots, err := resolveRoots(opts.configDir, opts.workspace)
			if err != nil {
				return err
			}
			if opts.endpoint == "" {
				return errProbeModelNeedsEndpoint
			}

			// Said BEFORE the first call, per ADR 0021 §4: a command that spends tokens and
			// switches automatism on announces both in advance, not in its epilogue.
			cmd.PrintErrln(probe.ModelPreamble(noSave))

			// The advertised label the record is keyed on: the pinned --model when there is
			// one, else what the server says its active model is — because that label is what
			// a later OFFLINE session has in hand when it resolves identity.
			label := opts.model
			if label == "" {
				info, derr := provider.NewClient(opts.endpoint, "").Discover(cmd.Context())
				if derr != nil {
					return derr
				}
				label = info.ActiveModel
			}
			if label == "" {
				// The identity IS the label (ADR 0021, Amendment 2026-07-22), so a server that
				// names no active model leaves nothing to record a claim about. Refusing here
				// beats running the battery and then reporting an identity-less result the
				// reader would have to decode.
				return errProbeModelNeedsLabel
			}

			client := provider.NewClient(opts.endpoint, label, provider.WithRequestTimeout(batteryRequestTimeout))
			result := probe.GatherModel(cmd.Context(), probe.ModelInputs{
				Endpoint: opts.endpoint,
				Model:    label,
				Chat: func(ctx context.Context, req provider.Request) (provider.RawResponse, error) {
					return client.Respond(ctx, req)
				},
			})
			result.Save = recordProbeFingerprint(result, roots, opts, !noSave)

			cmd.Println(result.Report())
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.endpoint, "endpoint", "", "OpenAI-compatible LLM server URL to probe")
	flags.StringVar(&opts.model, "model", "",
		"model to probe (default: ask the server for its active model)")
	flags.StringVar(&opts.workspace, "workspace", "",
		"workspace root (default: current directory)")
	flags.StringVar(&opts.configDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")
	flags.BoolVar(&noSave, "no-save", false,
		"run the full battery and print the report, but record no fingerprint (ADR 0021's off-switch)")

	return cmd
}

// recordProbeFingerprint performs the one write `probe model` makes, and gathers everything the
// report must say about it: where the record lives, whether an earlier record for the same
// endpoint + label disagreed (a model swapped behind an unchanged label), and which Validated
// sets this identity promotes to auto-apply.
//
// It lives HERE rather than in internal/probe because the consequence is composed of things the
// composition root owns — the apogee home's roots and the merged Validated-set entries — and
// because keeping the write at wire time is the same placement discipline ADR 0016's
// realisation set for the Validated-set decision. A zero fingerprint (an incomplete battery)
// writes nothing at all: an identity may not be minted from evidence with a hole in it.
func recordProbeFingerprint(m probe.Model, roots stateRoots, opts options, save bool) probe.SaveOutcome {
	out := probe.SaveOutcome{Requested: save}
	if m.Fingerprint.IsZero() {
		return out
	}

	out.Path = library.ProbeRecordPath(roots.probe, m.Endpoint, m.Model)
	out.AutoApply, out.Promoted, out.Suppressed = autoApplyKeys(m, opts, roots.validated)

	// A previous record for the same key whose behavioral SIGNATURE differs is the ADR 0021 §3
	// signal: the label did not change, the model behind it did. The identity cannot carry that
	// signal — it is the label, which by construction did not move — which is exactly why the
	// signature is recorded beside it. A defective previous record is simply not a comparison
	// (LoadProbeRecord already reports why), so it is silently not one here.
	if prev, _, ok := library.LoadProbeRecord(roots.probe, m.Endpoint, m.Model); ok && prev.Behavior != m.Behavior {
		out.Changed = prev.ProbedAt.Format(time.RFC3339)
	}

	if !save {
		return out
	}
	path, err := library.SaveProbeRecord(roots.probe, library.ProbeRecord{
		Endpoint:       m.Endpoint,
		ModelLabel:     m.Model,
		ProbedAt:       m.ProbedAt,
		Behavior:       m.Behavior,
		Features:       m.Battery.Features(),
		CapabilityTier: string(m.Tier),
	})
	if err != nil {
		out.Failure = err.Error()
		return out
	}
	out.Path = path
	out.Written = true
	return out
}

// autoApplyKeys names the Validated-set entries that AUTO-APPLY for this model once the record
// exists, and whether the record is what MADE them apply — the consequence ADR 0021 §4 requires
// `probe model` to state at the moment it happens, rather than leaving the user to notice a
// changed session next time.
//
// It asks the same question resolveValidatedSet asks at startup, twice: once at the confidence
// the record will carry and once at the confidence this model resolves to WITHOUT it. The
// difference between the two answers is the promotion, and computing it rather than assuming it
// is what keeps the report's effect line true on a machine where an alias was already applying
// the set. A LOADING defect stays silent here: this is a courtesy line in a report, and a broken
// user file is already loud at the startup path that owns it. A catalogue defect in the entry
// that would otherwise apply is named instead of dropped, because that one changes the answer.
func autoApplyKeys(m probe.Model, opts options, validatedDir string) (keys []string, promoted bool, suppressed string) {
	// The session-level off-switches resolveValidatedSet checks first. They hold whatever the
	// record says, so the report must name them rather than promising an effect startup will
	// not deliver.
	switch {
	case opts.bypass:
		return nil, false, "Bypass suppresses the Validated-set surface entirely, so no set applies as this session is configured"
	case !opts.validatedSetsEnable:
		return nil, false, "`validated-sets: enable: false` turns the surface off, so no set applies as this session is configured"
	}

	shipped, _ := validated.Shipped()
	user, _ := validated.LoadUserDir(validatedDir)
	entries, _ := validated.Merge(shipped, user)

	withRecord, err := validated.Match(m.Fingerprint.Label, m.Fingerprint.Confidence, opts.validatedSetsAlias, entries)
	if err != nil || withRecord.Kind != validated.KindApplied {
		return nil, false, ""
	}
	if len(opts.mechanisms) > 0 {
		return nil, false, fmt.Sprintf("Validated set %s matches, but your explicit mechanisms: config takes "+
			"precedence (whole-set-or-nothing), so it is not applied", withRecord.Entry.Key)
	}

	// The catalogue check startup runs as its last rung (resolveValidatedSet's Validate call).
	// Asking it HERE too is what keeps this report and the next session start from disagreeing
	// about the same entry: a set this build cannot assemble — an unknown ID after the catalogue
	// evolved, a now-invalid stacking — is skipped whole at startup, so claiming it auto-applies
	// would promise an effect that never arrives. Named rather than silent, and carrying the
	// catalogue's own reason — the text startup's skip notice prints — so the two surfaces read
	// as one answer about one entry.
	if verr := validated.Validate(withRecord.Entry, mechanisms.Descriptors()); verr != nil {
		return nil, false, fmt.Sprintf("the next session start skips validated-set entry %q: %v; it is not applied",
			withRecord.Entry.Key, verr)
	}

	// The counterfactual: the same label at the tier it would resolve to with no record stored.
	// An alias applies at ANY confidence (the human decision replaced the gate), so a set matched
	// that way was already applying and this probe promoted nothing. Match's only error is the
	// dangling alias, which is confidence-independent and already returned above.
	without, _ := validated.Match(m.Fingerprint.Label, domain.ConfidenceLow, opts.validatedSetsAlias, entries)
	return []string{withRecord.Entry.Key}, without.Kind != validated.KindApplied, ""
}
