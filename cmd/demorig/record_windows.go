//go:build windows

package main

import "github.com/spf13/cobra"

// newRecordCommand keeps the command line of the unix build and refuses to run: recording needs
// a unix pty ([errNoPTY]).
func newRecordCommand() *cobra.Command {
	var work string
	cmd := &cobra.Command{
		Use:   "record <storyboard.yaml> [--work <dir>]",
		Short: "Record a take of a clip, replaying its cassette as the model (unix only)",
		Args:  cobra.ExactArgs(1),
		RunE:  runE(func(*cobra.Command, []string) error { return errNoPTY }),
	}
	cmd.Flags().StringVar(&work, "work", "", "the rig's work dir")
	return cmd
}

// newCaptureCommand keeps the command line of the unix build and refuses to run: recording needs
// a unix pty ([errNoPTY]).
func newCaptureCommand() *cobra.Command {
	var work, upstream, keyEnv string
	cmd := &cobra.Command{
		Use:   "capture <storyboard.yaml> --upstream <url> [--key-env <VAR>] [--work <dir>]",
		Short: "Record a take against a live model, capturing its replies into the cassette (unix only)",
		Args:  cobra.ExactArgs(1),
		RunE:  runE(func(*cobra.Command, []string) error { return errNoPTY }),
	}
	cmd.Flags().StringVar(&work, "work", "", "the rig's work dir")
	cmd.Flags().StringVar(&upstream, "upstream", "", "the live model server the proxy forwards to (required)")
	cmd.Flags().StringVar(&keyEnv, "key-env", "", "the environment variable holding the upstream's API key")
	if err := cmd.MarkFlagRequired("upstream"); err != nil {
		panic(err) // the flag is declared on the line above
	}
	return cmd
}
