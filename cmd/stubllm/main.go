// Command stubllm is the developer-facing half of internal/stubllm: it serves a scripted
// OpenAI-compatible upstream from a fixture file, and it records a new fixture from a real
// server (ADR 0062).
//
// It exists so a fixture is something you CAPTURE rather than something you write. Point
// `stubllm record` at a live llama.cpp or OpenRouter endpoint, run apogee through it, and the
// resulting YAML replays that conversation — the exact chunking, pacing, tool-call fragments
// and token accounting the server produced — into every later `go test` run, with no model
// loaded and no network reached.
//
//	stubllm serve  --script cmd/apogee/testdata/stubllm/smoke.yaml --listen 127.0.0.1:8080
//	stubllm record --upstream http://127.0.0.1:1111 --out smoke.yaml
//
// It is a dev tool, not a release asset: `make stubllm` builds it, and `make dist` does not
// ship it.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/stubllm"
)

// The exit codes. A bad command line is told apart from a run that started and failed, the way
// `apogee headless` distinguishes them, so a script driving this binary can react to each.
const (
	exitRunFailed = 1
	exitBadUsage  = 2
)

// defaultListen is where both subcommands listen when the caller names no address: a loopback
// port the kernel picks, printed on stdout. A stub upstream is never reachable off the machine
// by default — it answers whoever asks, without a key.
const defaultListen = "127.0.0.1:0"

// shutdownGrace is how long the recorder's listener is given to finish in-flight replies after
// the interrupt, before the fixture is written anyway.
const shutdownGrace = 2 * time.Second

func main() {
	if err := newRootCommand().ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "stubllm:", err)
		os.Exit(exitCodeFor(err))
	}
}

// newRootCommand builds the command tree. Errors are printed by main, not by Cobra, so a
// failure reads the same whichever subcommand produced it; usage still prints for a bad
// command line, because that is the one failure the usage text actually answers.
func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stubllm",
		Short: "Serve or record a scripted OpenAI-compatible upstream",
		Long: "stubllm serves apogee's test fixtures as a real HTTP upstream, and records new\n" +
			"ones from a real server. See docs/design/test-drivers.md.",
		SilenceErrors: true,
		Args:          cobra.NoArgs,
	}
	cmd.AddCommand(newServeCommand(), newRecordCommand())
	return cmd
}

// newServeCommand plays a fixture until the process is interrupted.
func newServeCommand() *cobra.Command {
	var scriptPath, listen string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Play a script as an OpenAI-compatible server until interrupted",
		Args:  cobra.NoArgs,
		RunE: runE(func(cmd *cobra.Command, _ []string) error {
			script, err := stubllm.Load(scriptPath)
			if err != nil {
				return err
			}

			ctx, stop := interruptible(cmd.Context())
			defer stop()

			server, err := stubllm.Serve(ctx, listen, script)
			if err != nil {
				return err
			}
			defer server.Close()

			announce(cmd, server.URL)
			<-ctx.Done()
			return nil
		}),
	}
	cmd.Flags().StringVar(&scriptPath, "script", "", "path of the script YAML to play (required)")
	cmd.Flags().StringVar(&listen, "listen", defaultListen, "address to listen on")
	must(cmd.MarkFlagRequired("script"))
	return cmd
}

// newRecordCommand proxies a real upstream and writes what it saw as a fixture.
func newRecordCommand() *cobra.Command {
	var upstream, listen, out string

	cmd := &cobra.Command{
		Use:   "record",
		Short: "Proxy a real upstream and write the traffic as a script fixture",
		Long: "Record proxies /v1/* to a real server and writes every completion it saw as a\n" +
			"script turn. Point apogee at the printed address (--endpoint), drive the run you\n" +
			"want to pin, then interrupt this process: the fixture is written on the way out.",
		Args: cobra.NoArgs,
		RunE: runE(func(cmd *cobra.Command, _ []string) error {
			recorder, err := stubllm.NewRecorder(upstream, out)
			if err != nil {
				return err
			}

			ctx, stop := interruptible(cmd.Context())
			defer stop()

			listener, err := net.Listen("tcp", listen)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", listen, err)
			}
			server := &http.Server{Handler: recorder, ReadHeaderTimeout: 10 * time.Second}
			go func() { _ = server.Serve(listener) }()

			announce(cmd, "http://"+listener.Addr().String())
			cmd.Printf("recording %s -> %s\n", upstream, out)

			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
			return recorder.Close()
		}),
	}
	cmd.Flags().StringVar(&upstream, "upstream", "", "base URL of the real server to record (required)")
	cmd.Flags().StringVar(&out, "out", "fixture.yaml", "path of the script YAML to write")
	cmd.Flags().StringVar(&listen, "listen", defaultListen, "address to listen on")
	must(cmd.MarkFlagRequired("upstream"))
	return cmd
}

// announce prints the bound address in the one line a shell script greps for. It is the only
// thing on stdout that a caller is meant to parse, which is why it is a fixed prefix and a URL.
func announce(cmd *cobra.Command, url string) {
	cmd.Printf("listening %s\n", url)
}

// interruptible returns ctx extended with the signals that mean "stop": a Ctrl-C ends both
// subcommands successfully, and for `record` it is the normal way the fixture gets written.
func interruptible(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}

// runError marks a failure that happened after the command line was accepted, so exitCodeFor
// can tell a mistyped invocation from a run that started and failed.
type runError struct{ err error }

func (e runError) Error() string { return e.err.Error() }
func (e runError) Unwrap() error { return e.err }

// runE adapts a subcommand's body: usage stops being printed the moment the command line has
// been accepted, and every failure from here on is marked as a run failure rather than a usage
// one. Cobra's own flag and argument errors never pass through here, which is what makes them
// distinguishable.
func runE(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		if err := run(cmd, args); err != nil {
			return runError{err: err}
		}
		return nil
	}
}

// exitCodeFor reports the status an error asks for: a run that started and failed exits 1, a
// command line that never started one exits 2 with its usage printed.
func exitCodeFor(err error) int {
	var failed runError
	if errors.As(err, &failed) {
		return exitRunFailed
	}
	return exitBadUsage
}

// must panics on a Cobra wiring error. The only errors these calls return are "no such flag",
// which is a typo in the lines above and cannot be produced at runtime.
func must(err error) {
	if err != nil {
		panic(err)
	}
}
