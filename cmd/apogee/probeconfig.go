package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/projectroot"
	"github.com/airiclenz/apogee/internal/sanitize"
)

// maskedValue stands in for a Masked registry row's value: the row is rendered, its secret is not.
// No row carries Masked today (registry.go), so this is the guard that keeps a row that gains it
// from printing a secret through this report the day it does.
const maskedValue = "••••"

// probeConfigCommand builds `apogee probe config` — the fourth subject of `probe`, on the FREE side
// of its split (ADR 0021 §1): no agent, no model, no write. It reads config.yaml with the Project
// config of the current directory's Project root layered over it (ADR 0096 §6), the way a LIVE
// re-read of a saved file reads them (config.LoadLayeredOffer — the files the watcher re-reads)
// rather than the way startup does, so a file still written in a retired shape is reported as
// needing the migration a startup makes instead of being migrated here: the one migrating read is
// the startup pass, and a diagnostic that rewrote the file it was asked to describe would be the
// wrong kind of answer.
//
// It reports three things: every notice a startup would announce for the two files (an unknown
// key, a key that will be ignored, a global-only key the project file may not set, a malformed
// Host acknowledgement entry), each once; whether the global file awaits that migration; and — when
// it does not — the value every key of the registry resolves to from the two files alone, spelled
// the way the files spell it. A file a startup would REFUSE fails the command in startup's own
// sentence: past the reader, the report runs the startup resolution over the same files (no flags,
// no environment, nothing migrated — the pass the watcher's re-read runs), so a value a block
// validator rejects, or one only this machine can answer about (a `sessions:` share group), is
// refused here as it is at launch. Only a config that cannot name a startup server is tolerated:
// that is a session the TUI starts pre-bound, not a defect in the file. Flags, APOGEE_* variables
// and the host acknowledgement are deliberately NOT layered into the resolved values: `apogee
// probe` already reports the resolution a session runs with, and this verb's question is the
// narrower "what do the files say?". The one exception is APOGEE_CONFIG, because it names WHICH
// files: the apogee home is found on the precedence every other verb honours — --config over
// APOGEE_CONFIG over ~/.apogee — or a human whose home the variable sets would be shown a file
// their sessions never read.
func probeConfigCommand() *cobra.Command {
	var configDir string

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Report what the config files say: their notices, a pending migration, and every key's value",
		Long: "apogee probe config reads config.yaml, with the Project config of the current\n" +
			"directory's Project root (<root>/.apogee/config.yaml) layered over it, the way a live\n" +
			"re-read reads them, and reports what a startup would announce about them (an unknown\n" +
			"key it ignored, for instance), whether the global file is still written in a retired\n" +
			"shape that a startup would migrate, and otherwise the value every key resolves to from\n" +
			"the two files alone — the project file's value for a key it may set, the global file's\n" +
			"own value where it states one, the built-in default where neither does. A value a\n" +
			"startup would refuse fails the command with startup's own message.\n\n" +
			"It never writes: a file that needs the migration is reported, not rewritten. Flags and\n" +
			"the APOGEE_* environment are not layered in; `apogee probe` reports those. The one\n" +
			"exception is APOGEE_CONFIG, which names the apogee home to read when --config is not\n" +
			"given, as it does for every other command.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The env read is spelled here, as undo.go spells it, because this verb resolves no
			// session and so never reaches ApplyConfig, where every other command's is done.
			if configDir == "" {
				configDir = os.Getenv(config.EnvConfig)
			}
			report, err := configReport(configDir, probeProjectRoot(), os.ReadFile)
			if err != nil {
				return err
			}
			// The report is this command's PRODUCT, so it goes to real stdout, exactly as the
			// host report does (probe.go) — and escape-stripped on the way out, because it
			// quotes the file's own text (an unknown key's spelling, a refusal that echoes the
			// file's values) and a file is not a source this diagnostic may let repaint the
			// terminal.
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), sanitize.StripEscapes(report))
			return nil
		},
	}

	cmd.Flags().StringVar(&configDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")

	return cmd
}

// probeProjectRoot is the Project root the report layers: the current directory's, resolved by the
// same walk a session started here resolves it by. A directory that cannot be named is no project
// layer — the report then reads the global file alone, as a session with no Project root does.
func probeProjectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return projectroot.Resolve(cwd, userHome())
}

// configReport reads the config.yaml of the apogee home configDir, with the Project config of
// projectRoot layered over it, through the live reader and renders the report. A refusal of a
// retired shape is a FINDING the report states under `migration`, not a failure of the command.
// Every other refusal — the reader's (a malformed global file, a value a key refuses) or, once the
// reader has passed the files, the startup resolution's (startupRefusal) — is the command's own, in
// the sentence a startup prints. A project file the reader cannot read is announced and skipped, so
// it reaches the report under `notices`; a project value the reader accepts but a startup refuses
// fails the command like a global one. The notices of both passes are listed in arrival order, each
// once: the startup pass re-reads the same files, so it announces the reader's notices again.
func configReport(configDir, projectRoot string, readFile func(string) ([]byte, error)) (string, error) {
	var notices []string
	collect := func(msg string) {
		if !slices.Contains(notices, msg) {
			notices = append(notices, msg)
		}
	}
	o, offer, err := config.LoadLayeredOffer(config.FilePath(configDir), projectRoot, readFile, collect)
	if err != nil && !errors.Is(err, config.ErrRetiredShape) {
		return "", err
	}
	if err == nil {
		if refusal := startupRefusal(configDir, projectRoot, readFile, collect); refusal != nil {
			return "", refusal
		}
	}

	lines := []string{
		"apogee probe — config report",
		"  (nothing is written; the files are read the way a live reload reads them)",
		"",
		"notices",
	}
	lines = append(lines, indented(notices)...)
	lines = append(lines, "", "migration")
	if err != nil {
		// The refusal's own sentence, verbatim: the words a startup would print are the words
		// the reader should find here, and they already carry the paste-able replacement.
		lines = append(lines, indented(strings.Split(err.Error(), "\n"))...)
		lines = append(lines, "", "resolved",
			"  (start apogee once to migrate the file, then re-run)")
		return strings.Join(lines, "\n"), nil
	}
	lines = append(lines, "  (none)", "", "resolved")
	width := longestRegistryPath()
	for _, row := range config.KeyRegistry {
		lines = append(lines, configField(row.Path, resolvedValue(row, o, offer), width))
	}
	return strings.Join(lines, "\n"), nil
}

// startupRefusal runs the startup resolution (config.ApplyConfigOffer) over the two files the
// report reads and answers what it refuses: every block validator and the host-bound checks a
// launch would fail on, which the live reader never reaches. It is the pass the watcher's re-read
// runs (externalEdit.projection): no flag set and no environment, so it resolves from the files
// alone; LiveReread, so a retired shape is refused rather than migrated and nothing is written; the
// Project root pinned to the one the report layers, so both passes read the same project file. Its
// notices go to notify. A config that cannot name a startup server (config.StartupUndetermined) is
// no refusal here, as it is none to the TUI, which starts pre-bound on it.
func startupRefusal(configDir, projectRoot string, readFile func(string) ([]byte, error),
	notify func(string)) error {
	startup := config.Options{
		ConfigDir:         configDir,
		LiveReread:        true,
		ProjectRoot:       projectRoot,
		ProjectRootPinned: true,
	}
	_, err := config.ApplyConfigOffer(&startup, noFlagChanged, noEnvironment, readFile, notify)
	var undetermined *config.StartupUndetermined
	if errors.As(err, &undetermined) {
		return nil
	}
	return err
}

// resolvedValue is what the `resolved` section prints for one registry row: the value spelled
// the way the file spells it, the built-in default where the file states nothing, and a mask
// where the row says its value is not for rendering. offer is the Project config's offer the same
// read returned beside o, which a row that counts it (`allow`'s proposed rules) reads from
// (config.Key.ReadOf).
func resolvedValue(row config.Key, o config.Options, offer config.ProjectOffer) string {
	value := row.ReadOf(o, offer)
	if value == "" {
		value = row.Default
	}
	if row.Masked && value != "" {
		return maskedValue
	}
	return value
}

// indented renders a section's lines two spaces in, a blank line staying blank; a section with
// nothing to say reads `(none)` rather than nothing, so an empty section and a missing one never
// look alike.
func indented(lines []string) []string {
	if len(lines) == 0 {
		return []string{"  (none)"}
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, "  "+line)
	}
	return out
}

// configField is one `label: value` line in the host report's field layout (internal/probe),
// the label column widened to the registry's longest path so the values align down the section.
// A key holding nothing ends at its label rather than in the padding.
func configField(label, value string, width int) string {
	return strings.TrimRight(fmt.Sprintf("  %-*s %s", width+1, label+":", value), " ")
}

// longestRegistryPath is the width the `resolved` section's label column takes.
func longestRegistryPath() int {
	width := 0
	for _, row := range config.KeyRegistry {
		width = max(width, len(row.Path))
	}
	return width
}
