package main

// Checklist T-23's read-it-fresh half, automated: can somebody who has never seen this repository
// get from a downloaded archive to a reply from a model using ONLY README.md and docs/manual/?
//
// The reader is the judge model, driving a clean container through a `run` tool. That is what makes
// the claim testable at all — the item exists because a doc gap is invisible to whoever wrote the
// doc, and a scripted check can only ever verify the steps its author already knew about. The model
// gets the docs and a shell and nothing else: no repository, no source, no code to fall back on.
// Its report of what did not work as written is the artifact, and T-23's own oracles settle it.
//
// It never runs unattended. Both gates must be open — `docker` on PATH and a judge endpoint
// configured — and it is outside the suite's wall-clock budget by design (docs/design/test-drivers.md).

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/judge"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
)

const (
	// The container the newcomer works in: a plain Debian userland with nothing Go-shaped in it,
	// so a step that silently needs a toolchain fails here instead of passing on a dev box.
	newcomerImage = "debian:stable-slim"
	// The whole exercise, end to end. A local judge model driving twenty shell steps is slow.
	newcomerBudget = 15 * time.Minute
	// One shell command inside the container.
	newcomerStepBudget = 60 * time.Second
	// The tool-use loop's ceiling. A reader who has not installed apogee in twenty steps has
	// found the finding this test exists for.
	newcomerMaxSteps = 20
	// How much of one command's output the model is shown. Enough for a report, bounded so a
	// `find /` does not eat the context window.
	newcomerOutputCap = 4000
)

// TestNewcomerFollowsTheDocs is the judged half of T-23: the docs, a container, a shell, and a
// reader who is not allowed to look at the code.
func TestNewcomerFollowsTheDocs(t *testing.T) {
	if !judge.Enabled() {
		judge.Skip(t)
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker is not on PATH; the newcomer container needs it (this test is local-only)")
	}
	if runtime.GOOS != "linux" {
		// The stub upstream binds on the docker bridge gateway, and that address is an
		// interface of the host on Linux only; elsewhere the daemon lives in a VM the host
		// cannot bind into, and the exercise would fail on plumbing.
		t.Skipf("the newcomer stub binds on the docker bridge gateway, which the host can bind on linux only (this is %s)", runtime.GOOS)
	}

	ctx, cancel := context.WithTimeout(context.Background(), newcomerBudget)
	defer cancel()

	gateway := newcomerBridgeGateway(t, ctx, docker)
	stub, err := stubllm.Serve(ctx, gateway+":0", loadScript(t, "newcomer"))
	if err != nil {
		t.Skipf("could not bind the newcomer stub on %s:0, the docker bridge gateway "+
			"(rootless docker, or a remote DOCKER_HOST, where it is not a host interface): %v", gateway, err)
	}
	t.Cleanup(stub.Close)

	kit := newcomerKit(t)
	container := startNewcomerContainer(t, ctx, docker, kit)

	transcript, report := driveNewcomer(t, ctx, docker, container, stub.URL, stub.Model)

	// The oracles are T-23's own, verbatim, narrowed by Extra to the half a container can observe.
	judge.Require(t, ctx, judge.Rubric{
		Item:  "T-23",
		Claim: "a newcomer reaches a working session using only README Install + Quick start, with no step that had to be corrected to work",
		PassWhen: "a newcomer reaches a working session using only README Install + Quick start, every one " +
			"of the eight `APOGEE_*` variables behaves as documented (including the two parse errors), the " +
			"trace flags work while staying out of `--help`, and the `url-safety:` prose in both the manual " +
			"and the seeded template describes live, MCP-covering lists.",
		FailsIf: "any command must be corrected to work; a documented variable, flag, config key or tool " +
			"name does not exist (or one that exists is undocumented); a variable's precedence differs from " +
			"the four-layer rule; a bad `APOGEE_MODE`/`APOGEE_BYPASS` value is silently defaulted; the seeded " +
			"template still says the url-safety lists are startup-only or MCP-exempt; or the docs name a " +
			"version, model id or release-asset URL that no longer exists.",
		Extra: []string{
			"Judge ONLY the install-and-first-session half of the oracle — the part the transcript shows. " +
				"The environment-variable, trace-flag and url-safety halves are covered by other tests and " +
				"are not evidence here; their absence from the transcript is not a fail.",
			"The Homebrew and OpenRouter steps of this item are not observable in this container (no " +
				"published release, no API key) and were deliberately skipped; that is not a fail.",
			"The reader had ONLY README.md and docs/manual/ — no repository, no source, no network beyond " +
				"the local model server. A step that needed something outside the docs IS a fail.",
			"The container has no interactive terminal, so where the docs describe an interactive session " +
				"the reader was told to use the documented non-interactive equivalent. Reaching a reply " +
				"that way counts as a working session.",
			"A command the reader ran wrongly and then corrected on its own is NOT a fail. A command the " +
				"DOCS state, run as written, that does not work IS a fail.",
		},
	},
		judge.Artifact{
			Name: "the newcomer's own report of what did not work as written",
			Kind: judge.KindStdout,
			Text: report,
		},
		judge.Artifact{
			Name: "the shell transcript: every command the newcomer ran and what it printed",
			Kind: judge.KindTranscript,
			Text: transcript,
		},
	)
}

// newcomerKit stages exactly what the reader is given: the two documents, and one release archive
// packed the way `make dist` packs it (a versioned directory holding the binary, LICENSE and
// README.md, tarred and gzipped under the same name). One target rather than six — the reader only
// installs the one their machine runs, and five extra cross-builds buy nothing here.
func newcomerKit(t *testing.T) string {
	t.Helper()

	version := strings.TrimSpace(readRepoFile(t, "../../VERSION"))
	bare := strings.TrimPrefix(version, "v")
	name := fmt.Sprintf("apogee_%s_%s_%s", bare, runtime.GOOS, runtime.GOARCH)

	kit := t.TempDir()
	stage := filepath.Join(kit, name)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatalf("stage the archive: %v", err)
	}

	build := exec.Command("go", "build", "-trimpath", "-o", filepath.Join(stage, "apogee"), ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the binary for the archive: %v\n%s", err, out)
	}
	for _, f := range []string{"LICENSE", "README.md"} {
		copyRepoFile(t, "../../"+f, filepath.Join(stage, f))
	}

	tarball := exec.Command("tar", "-czf", filepath.Join(kit, name+".tar.gz"), "-C", kit, name)
	if out, err := tarball.CombinedOutput(); err != nil {
		t.Fatalf("pack the archive: %v\n%s", err, out)
	}
	if err := os.RemoveAll(stage); err != nil {
		t.Fatalf("clear the staging dir: %v", err)
	}

	// The docs, and only the docs.
	copyRepoFile(t, "../../README.md", filepath.Join(kit, "README.md"))
	copyTree(t, "../../docs/manual", filepath.Join(kit, "docs", "manual"))
	return kit
}

// startNewcomerContainer runs the image with the kit mounted read-only and nothing to do, and
// returns its id. It joins docker's default bridge network rather than the host's namespace: the
// stub upstream on the bridge gateway and the internet are reachable, nothing else on this machine
// is. Processes and memory are capped and the container cannot gain privileges; it still runs as
// root, because the reader's `apt` needs to. It is removed however the test ends.
func startNewcomerContainer(t *testing.T, ctx context.Context, docker, kit string) string {
	t.Helper()

	run := exec.CommandContext(ctx, docker, "run", "--detach", "--network", "bridge",
		"--pids-limit", "512", "--memory", "2g", "--security-opt", "no-new-privileges",
		"--volume", kit+":/kit:ro", newcomerImage, "sleep", "3600")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Skipf("could not start the %s container (is the docker daemon running?): %v\n%s",
			newcomerImage, err, out)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() {
		rm := exec.Command(docker, "rm", "--force", id)
		if out, err := rm.CombinedOutput(); err != nil {
			t.Logf("removing the newcomer container: %v\n%s", err, out)
		}
	})
	return id
}

// newcomerBridgeGateway is the address the container reaches this machine on: the gateway of
// docker's default bridge network, where the stub upstream binds. An answer that is empty or is not
// an IP is a soft gate like the other three — without it there is no address to hand the reader.
func newcomerBridgeGateway(t *testing.T, ctx context.Context, docker string) string {
	t.Helper()

	inspect := exec.CommandContext(ctx, docker, "network", "inspect", "bridge",
		"--format", "{{(index .IPAM.Config 0).Gateway}}")
	out, err := inspect.CombinedOutput()
	if err != nil {
		t.Skipf("could not inspect the docker bridge network (is the docker daemon running?): %v\n%s", err, out)
	}
	gateway := strings.TrimSpace(string(out))
	if net.ParseIP(gateway) == nil {
		t.Skipf("the docker bridge network named no usable gateway address (got %q); "+
			"the newcomer stub has nowhere the container can reach it", gateway)
	}
	return gateway
}

// driveNewcomer runs the tool-use loop and returns the shell transcript beside the model's closing
// report. The loop ends when the model answers without calling the tool, or at the step ceiling —
// which is itself reported, because a reader who ran out of steps did not reach a session.
func driveNewcomer(t *testing.T, ctx context.Context, docker, container, upstream, model string) (transcript, report string) {
	t.Helper()

	client, judgeModel, err := judge.Client(ctx)
	if err != nil {
		t.Fatalf("build the judge client: %v", err)
	}
	defer client.Close()
	t.Logf("newcomer driven by %s", judgeModel)

	runTool := provider.ToolSpec{
		Name: "run",
		Description: "Run one shell command inside the sandbox and get its combined output back. " +
			"The command runs as root in a Debian container with /kit read-only, on a private " +
			"network: the model server and the internet are reachable, nothing else on this machine is.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"command":` +
			`{"type":"string","description":"the shell command to run"}},"required":["command"]}`),
	}

	messages := []provider.Message{
		{Role: "system", Content: newcomerSystemPrompt},
		{Role: "user", Content: newcomerTask(upstream, model)},
	}

	var log strings.Builder
	zero := 0.0
	for step := 1; step <= newcomerMaxSteps; step++ {
		resp, err := client.Respond(ctx, provider.Request{
			Model:    judgeModel,
			Messages: messages,
			Tools:    []provider.ToolSpec{runTool},
			Sampling: provider.Sampling{Temperature: &zero},
		})
		if err != nil {
			t.Fatalf("newcomer step %d: %v", step, err)
		}
		if len(resp.ToolCalls) == 0 {
			return log.String(), resp.Content
		}

		messages = append(messages, provider.Message{
			Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls,
		})
		for _, call := range resp.ToolCalls {
			command := newcomerCommandOf(call)
			output := newcomerExec(ctx, docker, container, command)
			fmt.Fprintf(&log, "$ %s\n%s\n", command, output)
			messages = append(messages, provider.Message{
				Role: "tool", ToolCallID: call.ID, Content: output,
			})
		}
	}

	fmt.Fprintf(&log, "\n[the reader hit the %d-step ceiling without finishing]\n", newcomerMaxSteps)
	return log.String(), fmt.Sprintf(
		"I did not reach a working session within %d steps.", newcomerMaxSteps)
}

// newcomerCommandOf reads the command off one tool call, tolerating a model that answered with a
// bare string instead of the object the schema asked for.
func newcomerCommandOf(call provider.ToolCall) string {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err == nil && args.Command != "" {
		return args.Command
	}
	return strings.TrimSpace(call.Function.Arguments)
}

// newcomerExec runs one command in the container and returns its combined output, bounded in both
// time and size. A failure is output too: what the reader saw is the evidence, and a command that
// did not work is the whole point of the exercise.
func newcomerExec(ctx context.Context, docker, container, command string) string {
	if strings.TrimSpace(command) == "" {
		return "(no command)"
	}
	stepCtx, cancel := context.WithTimeout(ctx, newcomerStepBudget)
	defer cancel()

	cmd := exec.CommandContext(stepCtx, docker, "exec", "--workdir", "/root", container, "sh", "-lc", command)
	out, err := cmd.CombinedOutput()
	text := string(out)
	if len(text) > newcomerOutputCap {
		text = text[:newcomerOutputCap] + "\n…(output truncated)"
	}
	if err != nil {
		text += fmt.Sprintf("\n(exit: %v)", err)
	}
	if strings.TrimSpace(text) == "" {
		text = "(no output)"
	}
	return text
}

// newcomerSystemPrompt casts the model as the reader, and fences it off from everything a reader
// would not have.
const newcomerSystemPrompt = `You are a software developer who has never seen this program before. You are sitting at a fresh Debian machine with a shell, and you are trying to install a program called apogee and get one reply out of it.

You have exactly two sources of truth: the file /kit/README.md and the directory /kit/docs/manual/. Read them with the run tool. Do NOT guess, do NOT use knowledge you already have about this program, and do NOT look anywhere else — there is no source code on this machine and no internet.

Work in single shell commands through the run tool. Read a document first, then do what it says, literally, one step at a time.

When you are done — either you got a reply from the model, or you are stuck — stop calling the tool and answer in prose with a report titled "What did not work as written". List every command or instruction you took from the documents that did not work exactly as the document stated it, with the document's wording and what actually happened. If everything worked as written, say so explicitly.`

// newcomerTask is the user turn: the machine's facts and the goal, with no hint about how apogee
// is installed or started — that is what the documents are for.
func newcomerTask(upstream, model string) string {
	return fmt.Sprintf(`This machine has:

- /kit/README.md and /kit/docs/manual/ — the only documentation that exists.
- /kit/apogee_*.tar.gz — a release archive already downloaded for this machine's platform. Wherever the documents tell you to download an archive, use this local file instead; that substitution is expected and is not a finding.
- An OpenAI-compatible model server already running at %s, serving the model %q. No API key is needed.
- No Homebrew, no Go toolchain, and NO interactive terminal: this shell cannot run a full-screen program. Where the documents describe an interactive session, use the documented non-interactive way to send one prompt instead. If the documents describe no such way, that itself is a finding — report it.

Goal: install apogee from the archive and get one reply back from the model server, using only those two documents. Then write your report.`, upstream, model)
}

// readRepoFile reads a file the repo layout fixes in place.
func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

// copyRepoFile copies one repository file into the kit.
func copyRepoFile(t *testing.T, from, to string) {
	t.Helper()
	body, err := os.ReadFile(from)
	if err != nil {
		t.Fatalf("read %s: %v", from, err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(to), err)
	}
	if err := os.WriteFile(to, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", to, err)
	}
}

// copyTree copies a documentation directory into the kit, files only — the manual has no
// subdirectories today, and one that appears silently should be noticed rather than flattened.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatalf("read %s: %v", from, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			copyTree(t, filepath.Join(from, e.Name()), filepath.Join(to, e.Name()))
			continue
		}
		copyRepoFile(t, filepath.Join(from, e.Name()), filepath.Join(to, e.Name()))
	}
}
