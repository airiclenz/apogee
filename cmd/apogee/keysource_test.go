package main

// The key-source seams: what the composition root does when a `servers:` entry says WHERE its key
// comes from instead of carrying it. The resolution itself is internal/config's (KeyResolver, with
// its own suite); what is proved here is that every place a session reaches for a key goes through
// it — the startup flattening, the bind, the switch — and that a source which refuses fails the
// thing the user was doing rather than sending the request unauthenticated.
//
// The `api-key-cmd:` fixture is THIS test binary re-invoked behind a marker argument, the idiom
// internal/config's own resolver suite uses: a real process with a real argv and a real exit code,
// on every OS `go test` runs on and with no script to keep executable in testdata.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/session"
)

// keyFixtureMarker prefixes the one argument that turns a run of this test binary into a key
// command. It is a prefix rather than a bare flag so an ordinary run of the suite — whose arguments
// are cobra's and the testing package's — can never be mistaken for one.
const keyFixtureMarker = "apogee-key-fixture:"

// missingKeyProgram is a program name no machine has on its PATH: the smallest `api-key-cmd:` that
// is certain to fail, and it fails the way a mistyped or uninstalled credential tool does.
const missingKeyProgram = "apogee-no-such-key-program"

// TestAPIKeyCommandFixture is the fixture, not an assertion: invoked with the marker argument it
// prints the key after it and exits, and in an ordinary run of the suite it skips. Exiting inside
// the test is what keeps the framework's own "PASS" off the stdout the resolver reads as the key —
// safe here because the child is launched with only -test.run, never with the -test.paniconexit0
// `go test` hands the parent.
func TestAPIKeyCommandFixture(t *testing.T) {
	last := os.Args[len(os.Args)-1]
	if !strings.HasPrefix(last, keyFixtureMarker) {
		t.Skip("not the key-command fixture invocation")
	}
	fmt.Println(strings.TrimPrefix(last, keyFixtureMarker))
	os.Exit(0)
}

// keyCommandFor returns an `api-key-cmd:` line whose standard output is key. The program is quoted
// POSIX-style because that is how the resolver splits the line — inside single quotes a Windows
// path's backslashes are literal rather than escapes, so the one spelling works everywhere.
func keyCommandFor(t *testing.T, key string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return "'" + self + "' -test.run=^TestAPIKeyCommandFixture$ " + keyFixtureMarker + key
}

// A switch onto an entry whose key comes from a COMMAND carries what that command printed: the spec
// the engine is switched with, the Monitor the beats now go through, and the binding every
// out-of-band call reads are all built from the resolved token, not from the empty `api-key:` such
// an entry has.
func TestMoveResolvesACommandKeySource(t *testing.T) {
	t.Parallel()

	agent := &fakeSwitcher{}
	holder := newUpstreamHolder()
	holder.Bind("http://old.invalid:1111", "old-key", "old-model",
		heartbeat.NewMonitor("http://old.invalid:1111", "old-model", "old-key"))
	mover := sessionMover{
		agent:  agent,
		holder: holder,
		host:   &fakeStamper{},
		live:   newLiveSettings(config.Options{}, nil),
		keys:   config.NewKeyResolver(),
	}

	entry := config.ServerEntry{
		Name:      "workstation",
		Endpoint:  "http://192.168.64.1:1111",
		APIKeyCmd: keyCommandFor(t, "sk-from-the-keychain"),
	}
	if _, err := mover.move(entry); err != nil {
		t.Fatalf("move onto a command-source entry: %v", err)
	}

	if len(agent.specs) != 1 || agent.specs[0].APIKey != "sk-from-the-keychain" {
		t.Errorf("SwitchUpstream specs = %+v; want one carrying the key the command printed", agent.specs)
	}
	if got := holder.Binding().APIKey; got != "sk-from-the-keychain" {
		t.Errorf("the holder's binding carries %q; want the resolved key — every out-of-band call "+
			"against this server is built from it", got)
	}
}

// A key source that REFUSES refuses the switch, and refuses it before the engine is touched: the
// session stays on the server it was on, and the message names the entry the human just picked so
// they know which line of the file to fix (design call 4).
func TestMoveRefusesWhenTheKeySourceFails(t *testing.T) {
	t.Parallel()

	agent := &fakeSwitcher{}
	holder := newUpstreamHolder()
	holder.Bind("http://old.invalid:1111", "old-key", "old-model",
		heartbeat.NewMonitor("http://old.invalid:1111", "old-model", "old-key"))
	mover := sessionMover{
		agent:  agent,
		holder: holder,
		host:   &fakeStamper{},
		live:   newLiveSettings(config.Options{}, nil),
		keys:   config.NewKeyResolver(),
	}

	entry := config.ServerEntry{
		Name:      "workstation",
		Endpoint:  "http://192.168.64.1:1111",
		APIKeyCmd: missingKeyProgram,
	}
	_, err := mover.move(entry)
	if err == nil {
		t.Fatal("a move onto an entry whose key command cannot run: want the refusal, got none")
	}
	if !strings.Contains(err.Error(), "workstation") {
		t.Errorf("refusal = %q; want the entry's name in it", err)
	}
	if len(agent.specs) != 0 {
		t.Errorf("the engine was switched %d time(s) despite the refusal; the session must stay put",
			len(agent.specs))
	}
	if got := holder.Binding(); got.Endpoint != "http://old.invalid:1111" || got.APIKey != "old-key" {
		t.Errorf("the binding moved to %+v; a refused switch must leave it on the old server", got)
	}
}

// The bind is the other arrival on a server, so it resolves the same way: the Agent is constructed
// with the token the entry's command printed, and a source that refuses leaves the session unbound
// rather than constructed against a server it cannot authenticate to.
func TestBindResolvesACommandKeySource(t *testing.T) {
	t.Parallel()

	engine := newLateEngine(apogee.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })

	var handed apogee.Config
	binder := serverBinder{
		cfg:    validCfg(t),
		engine: engine,
		holder: newUpstreamHolder(),
		caps:   newParallelAgentsCap(engine),
		keys:   config.NewKeyResolver(),
		build: func(cfg apogee.Config, resumed *session.Record) (*apogee.Agent, error) {
			handed = cfg
			return buildAgent(cfg, resumed)
		},
	}

	entry := config.ServerEntry{
		Name:      "workstation",
		Endpoint:  "http://127.0.0.1:1111",
		APIKeyCmd: keyCommandFor(t, "sk-bound-by-the-command"),
	}
	if err := binder.bind(entry); err != nil {
		t.Fatalf("bind onto a command-source entry: %v", err)
	}
	if handed.APIKey != "sk-bound-by-the-command" {
		t.Errorf("the Agent was constructed with APIKey %q; want the key the entry's command printed",
			handed.APIKey)
	}
	if got := binder.holder.Binding().APIKey; got != "sk-bound-by-the-command" {
		t.Errorf("the holder's binding carries %q; want the resolved key", got)
	}
}

// A bind whose key source refuses constructs nothing: the engine holder is still unbound, so the
// session can be bound properly once the human fixes the entry.
func TestBindRefusesWhenTheKeySourceFails(t *testing.T) {
	t.Parallel()

	engine := newLateEngine(apogee.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })

	built := false
	binder := serverBinder{
		cfg:    validCfg(t),
		engine: engine,
		holder: newUpstreamHolder(),
		caps:   newParallelAgentsCap(engine),
		keys:   config.NewKeyResolver(),
		build: func(cfg apogee.Config, resumed *session.Record) (*apogee.Agent, error) {
			built = true
			return buildAgent(cfg, resumed)
		},
	}

	err := binder.bind(config.ServerEntry{
		Name:      "workstation",
		Endpoint:  "http://127.0.0.1:1111",
		APIKeyCmd: missingKeyProgram,
	})
	if err == nil {
		t.Fatal("a bind onto an entry whose key command cannot run: want the refusal, got none")
	}
	if !strings.Contains(err.Error(), "workstation") {
		t.Errorf("refusal = %q; want the entry's name in it", err)
	}
	if built {
		t.Error("the Agent was constructed despite the refusal; a session must not start against a " +
			"server whose key could not be produced")
	}
	if binder.holder.Binding().Endpoint != "" {
		t.Error("the holder was bound despite the refusal")
	}
}

// ADR 0036 decision 6 stands over the new sources: APOGEE_API_KEY overlays the STARTUP entry, and an
// overlaid entry is a literal-source entry for that run — the command the file names is not run at
// all. The two claims are proved together because they are one path: resolution flattens the entry's
// source onto the options, and the resolver reads the overlay off the same triple.
func TestStartupOverlayBeatsACommandKeySource(t *testing.T) {
	home := t.TempDir()
	command := keyCommandFor(t, "sk-from-the-keychain")
	body := "servers:\n  - name: workstation\n    endpoint: http://127.0.0.1:1111\n" +
		"    api-key-cmd: " + strconv.Quote(command) + "\nserver: workstation\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	// Without the override, the entry's command is what answers — the flattening carried the source
	// onto the options at all, which is what the startup seams re-assemble the entry from.
	plain := config.Options{ConfigDir: home}
	if err := config.ApplyConfig(&plain, func(string) bool { return false },
		func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if plain.APIKeyCmd != command {
		t.Fatalf("opts.APIKeyCmd = %q; want the startup entry's own api-key-cmd", plain.APIKeyCmd)
	}
	key, err := config.NewKeyResolver().Resolve(startupEntry(plain))
	if err != nil {
		t.Fatalf("resolve the startup entry: %v", err)
	}
	if key != "sk-from-the-keychain" {
		t.Errorf("the startup key resolved to %q; want what the entry's command printed", key)
	}

	// With it, the environment's key wins and the command is never asked. The source is still
	// carried — the file is not rewritten by an override — so only the resolution order can explain
	// the answer.
	overlaid := config.Options{ConfigDir: home}
	getenv := func(name string) string {
		if name == config.EnvAPIKey {
			return "sk-from-the-environment"
		}
		return ""
	}
	if err := config.ApplyConfig(&overlaid, func(string) bool { return false },
		getenv, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig with APOGEE_API_KEY: %v", err)
	}
	if overlaid.APIKeyCmd != command {
		t.Errorf("the overlay erased api-key-cmd (%q); it must overlay the value, not the file",
			overlaid.APIKeyCmd)
	}
	overlaidKey, err := config.NewKeyResolver().Resolve(startupEntry(overlaid))
	if err != nil {
		t.Fatalf("resolve the overlaid startup entry: %v", err)
	}
	if overlaidKey != "sk-from-the-environment" {
		t.Errorf("the overlaid startup key resolved to %q; want APOGEE_API_KEY's own value", overlaidKey)
	}
}
