// Command stubllm is the developer-facing half of internal/stubllm: it serves a scripted
// OpenAI-compatible upstream from a fixture file, it records a new fixture from a real
// server (ADR 0062), and it replays or captures a cassette — a live model's raw replies, keyed
// by conversation.
//
// It exists so a fixture is something you CAPTURE rather than something you write. Point
// `stubllm record` at a live llama.cpp or OpenRouter endpoint, run apogee through it, and the
// resulting YAML replays that conversation — the exact chunking, pacing, tool-call fragments
// and token accounting the server produced — into every later `go test` run, with no model
// loaded and no network reached.
//
//	stubllm serve  --script cmd/apogee/testdata/stubllm/smoke.yaml --listen 127.0.0.1:8080
//	stubllm record --upstream http://127.0.0.1:1111 --out smoke.yaml
//	stubllm replay --cassette graphics/demo/storyboards/hero.cassette --listen 127.0.0.1:18181
//	stubllm capture --upstream https://openrouter.ai/api --key-env OPENROUTER_API_KEY --out hero.cassette
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
	"path/filepath"
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

// defaultListen is where every subcommand listens when the caller names no address: a loopback
// port the kernel picks, printed on stdout. A stub upstream is never reachable off the machine
// by default — it answers whoever asks, without a key.
const defaultListen = "127.0.0.1:0"

// shutdownGrace is how long a listener is given to finish in-flight replies after the interrupt,
// before a recorder writes its fixture or cassette anyway.
const shutdownGrace = 2 * time.Second

// capturedPace replays a cassette's chunks at the offsets they arrived at: the captured speed.
const capturedPace = 1

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
			"ones from a real server; it also replays and captures cassettes. See\n" +
			"docs/design/test-drivers.md.",
		SilenceErrors: true,
		Args:          cobra.NoArgs,
	}
	cmd.AddCommand(newServeCommand(), newRecordCommand(), newReplayCommand(), newCaptureCommand())
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
			"want to pin, then interrupt this process: the fixture is written on the way out.\n" +
			"A streamed reply that carried events it could not decode is recorded without them,\n" +
			"and a warning naming the turn is printed on stderr.",
		Args: cobra.NoArgs,
		RunE: runE(func(cmd *cobra.Command, _ []string) error {
			recorder, err := stubllm.NewRecorder(upstream, out)
			if err != nil {
				return err
			}
			// What the fixture silently lost — a stream event that would not decode — is said on
			// stderr when it is written, beside the fixture rather than in it.
			recorder.WarnTo(cmd.ErrOrStderr())

			ctx, stop := interruptible(cmd.Context())
			defer stop()

			server, err := startServer(cmd, listen, recorder)
			if err != nil {
				return err
			}
			cmd.Printf("recording %s -> %s\n", upstream, out)

			<-ctx.Done()
			shutdown(server)
			return recorder.Close()
		}),
	}
	cmd.Flags().StringVar(&upstream, "upstream", "", "base URL of the real server to record (required)")
	cmd.Flags().StringVar(&out, "out", "fixture.yaml", "path of the script YAML to write")
	cmd.Flags().StringVar(&listen, "listen", defaultListen, "address to listen on")
	must(cmd.MarkFlagRequired("upstream"))
	return cmd
}

// newReplayCommand serves a cassette at its captured pace until the process is interrupted.
func newReplayCommand() *cobra.Command {
	var cassettePath, listen string

	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Serve a cassette's captured replies until interrupted",
		Long: "Replay answers each completion request with the exchange the cassette filed under\n" +
			"the request's conversation key, chunk by chunk at the captured pace, and the\n" +
			"discovery probes with their stored replies. A request the cassette holds no\n" +
			"exchange for is answered with HTTP 500 naming its key.",
		Args: cobra.NoArgs,
		RunE: runE(func(cmd *cobra.Command, _ []string) error {
			cassette, err := stubllm.LoadCassette(cassettePath)
			if err != nil {
				return err
			}

			ctx, stop := interruptible(cmd.Context())
			defer stop()

			server, err := startServer(cmd, listen, stubllm.NewReplayer(cassette, capturedPace))
			if err != nil {
				return err
			}
			<-ctx.Done()
			shutdown(server)
			return nil
		}),
	}
	cmd.Flags().StringVar(&cassettePath, "cassette", "", "path of the cassette to serve (required)")
	cmd.Flags().StringVar(&listen, "listen", defaultListen, "address to listen on")
	must(cmd.MarkFlagRequired("cassette"))
	return cmd
}

// newCaptureCommand proxies a live model and writes what it answered as a cassette.
func newCaptureCommand() *cobra.Command {
	var upstream, keyEnv, out, listen string

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Proxy a live model and write its replies as a cassette",
		Long: "Capture proxies /v1/* (and llama.cpp's /props) to a live model, authenticating with\n" +
			"the key held in --key-env, and records every completion's raw reply under its\n" +
			"conversation key. Point apogee at the printed address, drive the session you want\n" +
			"to pin, then interrupt this process: the cassette is written on the way out.",
		Args: cobra.NoArgs,
		RunE: runE(func(cmd *cobra.Command, _ []string) error {
			if err := checkCassetteDir(out); err != nil {
				return err
			}
			recorder, err := stubllm.NewCassetteRecorder(upstream, keyEnv)
			if err != nil {
				return err
			}

			ctx, stop := interruptible(cmd.Context())
			defer stop()

			server, err := startServer(cmd, listen, recorder)
			if err != nil {
				return err
			}
			cmd.Printf("capturing %s -> %s\n", upstream, out)

			<-ctx.Done()
			shutdown(server)
			if err := recorder.Cassette().Save(out); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "cassette: %s\n", out)
			return err
		}),
	}
	cmd.Flags().StringVar(&upstream, "upstream", "", "base URL of the live model server (required)")
	cmd.Flags().StringVar(&keyEnv, "key-env", "",
		"the environment variable holding the upstream's API key; empty sends none")
	cmd.Flags().StringVar(&out, "out", "", "path of the cassette to write (required)")
	cmd.Flags().StringVar(&listen, "listen", defaultListen, "address to listen on")
	must(cmd.MarkFlagRequired("upstream"))
	must(cmd.MarkFlagRequired("out"))
	return cmd
}

// checkCassetteDir refuses a capture whose cassette could not be saved: finding that out after a
// whole session has run against a live model would waste the session.
func checkCassetteDir(cassette string) error {
	dir := filepath.Dir(cassette)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cassette %s: %w", cassette, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cassette %s: %s is not a directory", cassette, dir)
	}
	return nil
}

// startServer listens on listen, serves handler there in the background, and announces the
// bound address once the listener is up.
func startServer(cmd *cobra.Command, listen string, handler http.Handler) (*http.Server, error) {
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", listen, err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()

	announce(cmd, "http://"+listener.Addr().String())
	return server, nil
}

// shutdown stops server, giving the replies still in flight shutdownGrace to finish. A reply
// that outlives the grace is cut: the interrupt has already been asked for.
func shutdown(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	_ = server.Shutdown(ctx)
}

// announce prints the bound address in the one line a shell script greps for. It is the only
// thing on stdout that a caller is meant to parse, which is why it is a fixed prefix and a URL.
func announce(cmd *cobra.Command, url string) {
	cmd.Printf("listening %s\n", url)
}

// interruptible returns ctx extended with the signals that mean "stop": a Ctrl-C ends every
// subcommand successfully, and for `record` and `capture` it is the normal way the fixture or
// cassette gets written.
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
