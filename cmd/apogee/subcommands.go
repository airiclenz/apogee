package main

import "github.com/spf13/cobra"

// subcommands returns the subcommands the shipped binary registers on the root, in the
// order they should appear under `apogee --help`. main — the composition root — is the
// only production caller; tests build their own sets and hand them to newRootCommand.
//
// It is the registration seam, and it is deliberately EMPTY today: `apogee probe` lands
// with the Phase-5 probe items and `apogee headless` stays deferred (CONTEXT.md). While
// the slice is empty the root has no children at all, so Cobra adds neither its `help`
// nor its `completion` command and `apogee --help` stays byte-identical to the pre-seam
// binary — the first subcommand to land here is what makes a Commands section appear
// (the only permitted output delta).
//
// Bare `apogee` is unaffected either way. The root keeps its own RunE (the TUI launch)
// and `Args: cobra.NoArgs`: Cobra matches argv[1] against the children first and only
// falls through to the root's argument validation when nothing matches, so an unknown
// word still fails with the same `unknown command` error it does today.
func subcommands() []*cobra.Command { return nil }
