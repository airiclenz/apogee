package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/sanitize"
)

// maskedValue stands in for a Masked registry row's value: the row is rendered, its secret is not.
// No row carries Masked today (registry.go), so this is the guard that keeps a row that gains it
// from printing a secret through this report the day it does.
const maskedValue = "••••"

// probeConfigCommand builds `apogee probe config` — the fourth subject of `probe`, on the FREE side
// of its split (ADR 0021 §1): no agent, no model, no write. It reads config.yaml the way a LIVE
// reload reads it (config.LoadFileConfig — the projection every `/settings` apply comes through)
// rather than the way startup does, so a file still written in a retired shape is reported as
// needing the migration a startup makes instead of being migrated here: the one migrating read is
// the startup pass, and a diagnostic that rewrote the file it was asked to describe would be the
// wrong kind of answer.
//
// It reports three things: every notice the reader collects (an unknown key, a key that will be
// ignored), whether the file awaits that migration, and — when it does not — the value every key
// of the registry resolves to from the file alone, spelled the way the file spells it. Flags,
// APOGEE_* variables and the host acknowledgement are deliberately NOT layered in: `apogee probe`
// already reports the resolution a session runs with, and this verb's question is the narrower
// "what does the file say?".
func probeConfigCommand() *cobra.Command {
	var configDir string

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Report what config.yaml says: its notices, a pending migration, and every key's value",
		Long: "apogee probe config reads config.yaml the way a live reload reads it and reports what\n" +
			"the reader noticed (an unknown key it ignored, for instance), whether the file is still\n" +
			"written in a retired shape that a startup would migrate, and otherwise the value every\n" +
			"key resolves to from the file alone — the file's own value where it states one, the\n" +
			"built-in default where it does not.\n\n" +
			"It never writes: a file that needs the migration is reported, not rewritten. Flags and\n" +
			"the APOGEE_* environment are not layered in; `apogee probe` reports those.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := configReport(config.FilePath(configDir), os.ReadFile)
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

// configReport reads the config file at path through the live reader and renders the report.
// A refusal of a retired shape is a FINDING the report states under `migration`, not a failure
// of the command; every other error the reader returns — a malformed file, a value a key
// refuses — is the command's own, in the reader's sentence.
func configReport(path string, readFile func(string) ([]byte, error)) (string, error) {
	var notices []string
	o, err := config.LoadFileConfig(path, readFile, func(msg string) { notices = append(notices, msg) })
	if err != nil && !errors.Is(err, config.ErrRetiredShape) {
		return "", err
	}

	lines := []string{
		"apogee probe — config report",
		"  (nothing is written; the file is read the way a live reload reads it)",
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
		lines = append(lines, configField(row.Path, resolvedValue(row, o), width))
	}
	return strings.Join(lines, "\n"), nil
}

// resolvedValue is what the `resolved` section prints for one registry row: the value spelled
// the way the file spells it, the built-in default where the file states nothing, and a mask
// where the row says its value is not for rendering.
func resolvedValue(row config.Key, o config.Options) string {
	value := row.Read(o)
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
