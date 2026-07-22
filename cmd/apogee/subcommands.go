package main

import "github.com/spf13/cobra"

// subcommands returns the subcommands the shipped binary registers on the root, in the
// order they should appear under `apogee --help`. main — the composition root — is the
// only production caller; tests build their own sets and hand them to newRootCommand.
//
// `probe` is the first and, today, the only entry: the host report (ADR 0021's free half)
// plus its `host` child. `apogee headless` stays deferred (CONTEXT.md). Registering a child
// is what makes a Commands section appear under `apogee --help` — the one permitted output
// delta of the Phase-5 subcommand work.
//
// Bare `apogee` is unaffected. The root keeps its own RunE (the TUI launch) and
// `Args: cobra.NoArgs`: Cobra matches argv[1] against the children first and only falls
// through to the root's argument validation when nothing matches, so an unknown word still
// fails with the same `unknown command` error it does today.
func subcommands() []*cobra.Command { return []*cobra.Command{newProbeCommand()} }
