//go:build !windows

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// rigPortKey is the rig.env key naming the port apogee's configured server listens on — the
// port a take's cassette proxy or replayer is bound to on 127.0.0.1. The rig's work dir itself
// is resolved by [workDir].
const rigPortKey = "PORT"

// firstPaintTimeout bounds how long a take waits for apogee's first paint after launch.
const firstPaintTimeout = 30 * time.Second

// serverShutdownTimeout bounds how long the model source is given to finish its in-flight
// replies once the take has ended.
const serverShutdownTimeout = 5 * time.Second

// newRecordCommand records a take by replaying the storyboard's cassette into apogee.
func newRecordCommand() *cobra.Command {
	var work string
	cmd := &cobra.Command{
		Use:   "record <storyboard.yaml> [--work <dir>]",
		Short: "Record a take of a clip, replaying its cassette as the model",
		Long: "record resets the rig's stage, serves the storyboard's cassette on the port rig.env\n" +
			"names, runs apogee under the rig's env.sh in a pty at the storyboard's frame, performs\n" +
			"every beat, writes <work>/<clip>.take, then checks the take and exits with the check's\n" +
			"status. The take starts at apogee's first paint.",
		Args: cobra.ExactArgs(1),
		RunE: runE(func(cmd *cobra.Command, args []string) error {
			board, err := Load(args[0])
			if err != nil {
				return err
			}
			// The cassette is read before anything touches the rig, so a missing one costs no reset.
			cassette, err := stubllm.LoadCassette(board.Cassette)
			if err != nil {
				return fmt.Errorf("cassette %s: %w", board.Cassette, err)
			}
			rig, err := openRig(work)
			if err != nil {
				return err
			}
			source := modelSource{handler: stubllm.NewReplayer(cassette, 1)}
			return recordClip(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), board, rig, source)
		}),
	}
	addWorkFlag(cmd, &work)
	return cmd
}

// newCaptureCommand records a take with a live model behind a recording proxy, and saves what
// the model answered as the storyboard's cassette.
func newCaptureCommand() *cobra.Command {
	var work, upstream, keyEnv string
	cmd := &cobra.Command{
		Use:   "capture <storyboard.yaml> --upstream <url> [--key-env <VAR>] [--work <dir>]",
		Short: "Record a take against a live model, capturing its replies into the cassette",
		Long: "capture is record with a live model: the port rig.env names serves a recording proxy\n" +
			"to --upstream, authenticating with the key in --key-env (none when it is empty), and\n" +
			"once every beat has run the captured exchanges are saved as the storyboard's cassette.\n" +
			"The take and the check follow as for record.",
		Args: cobra.ExactArgs(1),
		RunE: runE(func(cmd *cobra.Command, args []string) error {
			board, err := Load(args[0])
			if err != nil {
				return err
			}
			if err := checkCassetteDir(board.Cassette); err != nil {
				return err
			}
			recorder, err := stubllm.NewCassetteRecorder(upstream, keyEnv)
			if err != nil {
				return err
			}
			rig, err := openRig(work)
			if err != nil {
				return err
			}
			source := modelSource{
				handler: recorder,
				finish: func() error {
					if err := recorder.Cassette().Save(board.Cassette); err != nil {
						return fmt.Errorf("save the cassette %s: %w", board.Cassette, err)
					}
					_, err := fmt.Fprintf(cmd.ErrOrStderr(), "cassette: %s\n", board.Cassette)
					return err
				},
			}
			return recordClip(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), board, rig, source)
		}),
	}
	addWorkFlag(cmd, &work)
	cmd.Flags().StringVar(&upstream, "upstream", "", "the live model server the proxy forwards to (required)")
	cmd.Flags().StringVar(&keyEnv, "key-env", "",
		"the environment variable holding the upstream's API key; empty sends none")
	if err := cmd.MarkFlagRequired("upstream"); err != nil {
		panic(err) // the flag is declared on the line above
	}
	return cmd
}

// addWorkFlag declares the --work flag both recording commands share.
func addWorkFlag(cmd *cobra.Command, work *string) {
	cmd.Flags().StringVar(work, "work", "",
		"the rig's work dir (default $"+workDirEnv+", else ~/"+defaultWorkDir+")")
}

// checkCassetteDir refuses a capture whose cassette could not be saved: finding that out after
// a whole take has run against a live model would waste the take.
func checkCassetteDir(cassette string) error {
	info, err := os.Stat(filepath.Dir(cassette))
	if err != nil {
		return fmt.Errorf("cassette %s: %w", cassette, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cassette %s: %s is not a directory", cassette, filepath.Dir(cassette))
	}
	return nil
}

// modelSource answers apogee's model requests during a take: the handler is served on the rig's
// port, and finish, when set, runs once a take's beats have all passed.
type modelSource struct {
	handler http.Handler
	finish  func() error
}

// rig is the demo rig setup.sh built: a work dir holding env.sh, reset.sh and rig.env, the
// isolated home apogee runs in, and the stage repo inside it.
type rig struct {
	work string
	port int
}

// openRig resolves the work dir — the flag, else APOGEE_DEMO_WORK, else ~/.cache/apogee-demo —
// and checks it holds a built rig with a port to serve the model on.
func openRig(flag string) (rig, error) {
	work, err := workDir(flag)
	if err != nil {
		return rig{}, err
	}
	r := rig{work: work}
	for _, script := range []string{r.envScript(), r.resetScript()} {
		if _, err := os.Stat(script); err != nil {
			return rig{}, fmt.Errorf("no rig at %s — run graphics/demo/setup.sh first: %w", work, err)
		}
	}
	port, err := readRigPort(filepath.Join(work, "rig.env"))
	if err != nil {
		return rig{}, err
	}
	r.port = port
	return r, nil
}

func (r rig) envScript() string   { return filepath.Join(r.work, "env.sh") }
func (r rig) resetScript() string { return filepath.Join(r.work, "reset.sh") }
func (r rig) home() string        { return filepath.Join(r.work, "home") }
func (r rig) stage() string       { return filepath.Join(r.home(), "Repos", "taskman") }
func (r rig) sessionsDir() string { return filepath.Join(r.home(), ".apogee", "sessions") }
func (r rig) addr() string        { return net.JoinHostPort("127.0.0.1", strconv.Itoa(r.port)) }

// takePath is where a clip's take is written.
func (r rig) takePath(clip string) string { return takeFile(r.work, clip) }

// readRigPort reads the PORT line of rig.env, the shell-sourced KEY=VALUE file setup.sh writes.
func readRigPort(path string) (int, error) {
	f, err := os.Open(path) //nolint:gosec // the rig's own file, under the operator's work dir
	if err != nil {
		return 0, fmt.Errorf("rig.env: %w — run graphics/demo/setup.sh first", err)
	}
	defer f.Close() //nolint:errcheck // read-only
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, found := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !found || strings.TrimSpace(key) != rigPortKey {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return 0, fmt.Errorf("%s: %s=%q is not a port", path, rigPortKey, value)
		}
		return port, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	return 0, fmt.Errorf("%s: no %s line — re-run graphics/demo/setup.sh", path, rigPortKey)
}

// recordClip runs one take end to end: the take itself, then the session it saved, the take
// file carrying that session's path, the model source's finish, and last the check, whose
// status is the command's. A take whose beats failed is still written — it is what shows why —
// but is neither finished nor checked.
func recordClip(ctx context.Context, stdout, stderr io.Writer, board *Storyboard, r rig, source modelSource) error {
	if _, err := fmt.Fprintf(stderr, "recording %s …\n", board.Clip); err != nil {
		return err
	}
	take, beatsErr := runTake(ctx, board, r, source.handler, stderr)
	if take == nil {
		return beatsErr
	}
	session, sessionErr := newestSession(r.sessionsDir())
	if sessionErr == nil {
		take.Session = session
	}
	if err := SaveTake(r.takePath(board.Clip), take); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stderr, "take: %s\n", r.takePath(board.Clip)); err != nil {
		return err
	}
	if beatsErr != nil {
		return beatsErr
	}
	if sessionErr != nil {
		return sessionErr
	}
	if source.finish != nil {
		if err := source.finish(); err != nil {
			return err
		}
	}
	return judgeRecording(ctx, stdout, board, take, r.stage())
}

// runTake resets the stage, serves the model source on the rig's port, launches apogee under
// env.sh in a pty at the storyboard's frame, waits for its first paint and performs every beat.
// It returns the take whenever apogee was launched, together with the first thing that went
// wrong; a nil take means nothing was recorded.
func runTake(ctx context.Context, board *Storyboard, r rig, handler http.Handler, logs io.Writer) (*Take, error) {
	reset := exec.CommandContext(ctx, r.resetScript()) //nolint:gosec // the rig's own script
	reset.Env = append(os.Environ(), workDirEnv+"="+r.work)
	reset.Stdout, reset.Stderr = logs, logs
	if err := reset.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w", r.resetScript(), err)
	}

	listener, err := net.Listen("tcp", r.addr())
	if err != nil {
		return nil, fmt.Errorf("serve the model on %s: %w", r.addr(), err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = server.Serve(listener) // ends with ErrServerClosed once the take is over
	}()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		defer cancel()
		if server.Shutdown(shutdown) != nil {
			_ = server.Close()
		}
		<-served
	}()

	launch := "source " + shellQuote(r.envScript()) + " >/dev/null && exec apogee"
	term, err := StartTerminal("bash", []string{"-c", launch}, TermOptions{
		Cols: board.Frame.Cols, Rows: board.Frame.Rows, FPS: board.Frame.FPS,
		Env: apogeeEnv(os.Environ()), Dir: r.work, FromFirstPaint: true,
	})
	if err != nil {
		return nil, err
	}
	runErr := performBeats(ctx, term, board.Beats)
	return term.Close(), runErr
}

// ambientApogeeEnv are the APOGEE_* variables that would steer the apogee a take launches away
// from the rig: another config file or workspace, another server, endpoint or model than the
// one the cassette was captured from, another starting mode, bypass. The rig's config.yaml is
// the whole of what a take runs on, so none of them reaches it.
var ambientApogeeEnv = []string{
	config.EnvConfig, config.EnvServer, config.EnvEndpoint, config.EnvModel, config.EnvMode,
	config.EnvBypass, config.EnvWorkspace,
}

// apogeeEnv is environ less every variable in ambientApogeeEnv.
func apogeeEnv(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if !slices.Contains(ambientApogeeEnv, name) {
			kept = append(kept, entry)
		}
	}
	return kept
}

// performBeats waits for the program's first paint, then runs the beats.
func performBeats(ctx context.Context, term *Terminal, beats []Beat) error {
	timeout := time.NewTimer(firstPaintTimeout)
	defer timeout.Stop()
	select {
	case <-term.Painted():
	case <-term.Done():
		return fmt.Errorf("apogee exited (status %d) before its first paint", term.ExitCode())
	case <-timeout.C:
		return fmt.Errorf("apogee did not paint within %s of its launch", firstPaintTimeout)
	case <-ctx.Done():
		return ctx.Err()
	}
	return NewEngine(term).Run(ctx, beats)
}

// shellQuote spells s as one single-quoted shell word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// newestSession is the most recently written session JSON in dir. reset.sh wipes the sessions
// dir before a take, so the newest one there is the take's.
func newestSession(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return "", err
	}
	var newest string
	var newestAt time.Time
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if newest == "" || info.ModTime().After(newestAt) {
			newest, newestAt = path, info.ModTime()
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no session saved under %s — the take cannot be checked", dir)
	}
	return newest, nil
}

// errChecksFailed is what a recording whose check found a FAIL row returns.
var errChecksFailed = errors.New("expect(s) failed")

// judgeRecording checks the take — the session it saved and the screens it recorded — against
// the storyboard's expects and the stage, printing the check table to w and failing when any row
// fails.
func judgeRecording(ctx context.Context, w io.Writer, board *Storyboard, take *Take, stage string) error {
	rows, err := checkTake(ctx, board, take, stage)
	if err != nil {
		return err
	}
	if err := writeCheckTable(w, rows); err != nil {
		return err
	}
	if failed := countFailed(rows); failed > 0 {
		return fmt.Errorf("%d %w", failed, errChecksFailed)
	}
	return nil
}
