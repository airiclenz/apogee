package main

// The start-up key migration from the composition root's side (keymigrate.go): which entries are
// candidates, what a machine with no store is told instead, and — the part that touches a real
// credential — that the move writes, reads back and only then rewrites.
//
// The store is faked through the consumer-side interface, so the sequence can be exercised on any
// machine; what its ReadCmd hands back is a real command line (the keysource_test.go fixture), so
// the read-back verification runs the resolver's actual exec path rather than a stub of it.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/keystore"
	"github.com/airiclenz/apogee/internal/run"
)

// fakeStore is a secret store that records what it was asked to do AND what the config file looked
// like at that moment — which is how the write → verify → rewrite ORDER is proved rather than
// merely the fact that all three happened.
type fakeStore struct {
	t          *testing.T
	configPath string
	steps      []string
	stored     map[string]string
	writeErr   error
	// handBack overrides what the read-back command prints, for the case the store gives back
	// something other than what went in.
	handBack string
}

func (f *fakeStore) Name() string { return "Fake Store" }

func (f *fakeStore) Write(entry, key string) error {
	f.steps = append(f.steps, "write:"+f.configState())
	if f.writeErr != nil {
		return f.writeErr
	}
	if f.stored == nil {
		f.stored = map[string]string{}
	}
	f.stored[entry] = key
	return nil
}

func (f *fakeStore) ReadCmd(entry string) string {
	f.steps = append(f.steps, "read:"+f.configState())
	key := f.stored[entry]
	if f.handBack != "" {
		key = f.handBack
	}
	return keyCommandFor(f.t, key)
}

// configState says whether the entry in the config file still carries its literal key or has been
// pointed at a command — the one bit each step is stamped with.
func (f *fakeStore) configState() string {
	data, err := os.ReadFile(f.configPath)
	if err != nil {
		return "unreadable"
	}
	if strings.Contains(string(data), "api-key-cmd:") {
		return "command"
	}
	return "literal"
}

// plaintextConfig writes an apogee home whose one server carries its key in the file, and returns
// the home and the config path.
func plaintextConfig(t *testing.T, body string) (string, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return home, path
}

// onePlaintextServer is the file the migration is offered over: a commented entry with a literal
// key, so the rewrite has neighbouring lines to leave alone.
const onePlaintextServer = "# the box under the desk\nservers:\n" +
	"  - name: workstation\n    endpoint: http://127.0.0.1:1111\n" +
	"    api-key: sk-plain-as-day   # from the provider's console\n" +
	"    max-output-tokens: 4096\nserver: workstation\n"

// Only an entry whose key is IN the file is a candidate: a command or a variable source has nothing
// to move, and the marker is the human's own "never", which must not keep being reported.
func TestPlaintextKeyEntriesNamesOnlyTheKeysInTheFile(t *testing.T) {
	t.Parallel()

	servers := []config.ServerEntry{
		{Name: "workstation", APIKey: "sk-plain"},
		{Name: "by-command", APIKeyCmd: "pass show apogee/work"},
		{Name: "by-variable", APIKeyEnv: "APOGEE_WORK_KEY"},
		{Name: "acknowledged", APIKey: "sk-plain", PlaintextKeyOK: true},
		{Name: "keyless"},
		{Name: "  ", APIKey: "sk-plain"},
		{Name: "laptop", APIKey: "sk-also-plain"},
	}

	got := plaintextKeyEntries(servers)

	want := []string{"workstation", "laptop"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("candidates = %v; want %v — in the file's own order", got, want)
	}
}

// The notice a run with no offer to make gives names the entries and every alternative their owner
// can reach without apogee's help, it names the file this run actually read, and it carries the
// caller's own reason rather than a reason of its own — the two drivers do not share one.
func TestPlaintextKeyNoticeNamesTheEntriesAndTheAlternatives(t *testing.T) {
	t.Parallel()

	notice := plaintextKeyNotice("/somewhere/else/config.yaml", "this run cannot offer anything better",
		[]string{"workstation", "laptop"})

	for _, want := range []string{
		"workstation", "laptop", "/somewhere/else/config.yaml",
		"this run cannot offer anything better",
		"api-key-env", "api-key-cmd", "chmod 600", "plaintext-key-ok",
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice does not mention %q:\n%s", want, notice)
		}
	}
}

// The move in full, in order: the secret reaches the store while the file still holds the literal,
// the read-back command is built while it still does, and only afterwards is the entry rewritten —
// into an ordinary `api-key-cmd:` line the config package validates like any other.
func TestMigrateKeyWritesVerifiesThenRewrites(t *testing.T) {
	_, path := plaintextConfig(t, onePlaintextServer)
	store := &fakeStore{t: t, configPath: path}

	if err := migrateKey(store, path, "workstation", "sk-plain-as-day", ""); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if len(store.steps) != 2 || store.steps[0] != "write:literal" || store.steps[1] != "read:literal" {
		t.Errorf("steps = %v; want the store written and read back before the file is touched", store.steps)
	}
	if got := store.stored["workstation"]; got != "sk-plain-as-day" {
		t.Errorf("the store holds %q; want the key that was in the file", got)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the rewritten config: %v", err)
	}
	body := string(data)
	// The literal LINE is what has to be gone. The fixture's read-back command happens to carry the
	// key as an argument (a real store's does not), so the key's text alone is not the assertion.
	if strings.Contains(body, "api-key: sk-plain-as-day") {
		t.Errorf("the literal key line is still in the file:\n%s", body)
	}
	if !strings.Contains(body, "api-key-cmd:") {
		t.Errorf("the entry was not pointed at the store:\n%s", body)
	}
	for _, keep := range []string{"# the box under the desk", "max-output-tokens: 4096",
		"# from the provider's console"} {
		if !strings.Contains(body, keep) {
			t.Errorf("the rewrite lost %q:\n%s", keep, body)
		}
	}

	layer, err := config.LoadFileConfig(path, os.ReadFile, func(string) {})
	if err != nil {
		t.Fatalf("the rewritten config does not load: %v", err)
	}
	if err := config.ValidateServers(layer.Servers); err != nil {
		t.Errorf("the rewritten config does not validate: %v", err)
	}
	if got := layer.Servers[0]; got.APIKey != "" || got.APIKeyCmd == "" {
		t.Errorf("the entry reads back as %+v; want exactly one source, the command", got)
	}
}

// A store that hands back something other than what went in aborts the migration BEFORE the file is
// touched: the entry keeps the literal key that still works, and the message says so.
func TestMigrateKeyAbortsBeforeRewriteOnAVerifyMismatch(t *testing.T) {
	_, path := plaintextConfig(t, onePlaintextServer)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the config: %v", err)
	}
	store := &fakeStore{t: t, configPath: path, handBack: "sk-something-else"}

	err = migrateKey(store, path, "workstation", "sk-plain-as-day", "")
	if err == nil {
		t.Fatal("a read-back mismatch was accepted; the config would point at a key nobody stored")
	}
	if !strings.Contains(err.Error(), "workstation") || !strings.Contains(err.Error(), "left alone") {
		t.Errorf("error = %v; want the entry named and the file said to be untouched", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the config again: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("the config was rewritten despite the mismatch:\n%s", after)
	}
}

// A store that refuses the write never gets as far as reading anything back, and never touches the
// file either.
func TestMigrateKeyStopsAtAFailedStoreWrite(t *testing.T) {
	_, path := plaintextConfig(t, onePlaintextServer)
	store := &fakeStore{t: t, configPath: path, writeErr: errors.New("the keychain is locked")}

	err := migrateKey(store, path, "workstation", "sk-plain-as-day", "")
	if err == nil || !strings.Contains(err.Error(), "the keychain is locked") {
		t.Fatalf("error = %v; want the store's own refusal", err)
	}
	if len(store.steps) != 1 {
		t.Errorf("steps = %v; want nothing after the failed write", store.steps)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "api-key: sk-plain-as-day") {
		t.Errorf("the file changed after a failed write:\n%s", data)
	}
}

// wiringFor is a rootWiring with just the two things the key-migration decision reads: the resolved
// options and the config root.
func wiringFor(home string, servers []config.ServerEntry) *rootWiring {
	return &rootWiring{opts: config.Options{Servers: servers}, roots: stateRoots{config: home}}
}

// With a store that can complete the move, the offer is recorded for the renderer and nothing is
// said on stderr — the question is the pane, not a sentence nobody can answer.
func TestPrepareKeyMigrationRaisesTheOfferWithAStore(t *testing.T) {
	t.Parallel()

	w := wiringFor(t.TempDir(), []config.ServerEntry{
		{Name: "workstation", APIKey: "sk-plain"},
		{Name: "by-command", APIKeyCmd: "pass show apogee/work"},
	})
	store := &fakeStore{t: t}
	var notices bytes.Buffer

	w.prepareKeyMigration(func(string) (secretStore, error) { return store, nil }, &notices)

	if w.keyOffer.StoreName != store.Name() {
		t.Errorf("offer store = %q; want the store's own name", w.keyOffer.StoreName)
	}
	if got := w.keyOffer.Entries; len(got) != 1 || got[0] != "workstation" {
		t.Errorf("offer entries = %v; want the one entry with a key in the file", got)
	}
	if w.secrets == nil {
		t.Error("the store was not kept; the answer seams would be unwired")
	}
	if notices.Len() != 0 {
		t.Errorf("a machine that CAN be offered the move was told to do it by hand: %q", notices.String())
	}
	if w.keyMigrator() == nil || w.plaintextKeyKeeper() == nil {
		t.Error("the answer seams are nil on a run that raised the offer")
	}
}

// Without one, nothing is asked and the notice is the whole answer — an offer apogee cannot
// complete is worse than no offer.
func TestPrepareKeyMigrationNoticesWithoutAStore(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	w := wiringFor(home, []config.ServerEntry{{Name: "workstation", APIKey: "sk-plain"}})
	var notices bytes.Buffer

	w.prepareKeyMigration(func(string) (secretStore, error) { return nil, keystore.ErrNoStore }, &notices)

	if w.keyOffer.StoreName != "" || len(w.keyOffer.Entries) != 0 {
		t.Errorf("offer = %+v; want none where the move cannot be completed", w.keyOffer)
	}
	if w.keyMigrator() != nil || w.plaintextKeyKeeper() != nil {
		t.Error("the answer seams are wired on a run with no store to answer into")
	}
	if got := notices.String(); !strings.Contains(got, "workstation") ||
		!strings.Contains(got, filepath.Join(home, "config.yaml")) {
		t.Errorf("notice = %q; want the entry and the file named", got)
	}
	// This path — and only this path — has actually asked the machine, so it is the only one allowed
	// to say the machine has nothing to offer.
	if got := notices.String(); !strings.Contains(got, "has no secret store apogee can move it into") {
		t.Errorf("notice = %q; want the probe-failed reason", got)
	}
}

// A probe that FAILED rather than finding nothing puts its own sentence in the notice — the case
// that produces one is a store tool the exec fence refused, and telling that operator "this machine
// has no secret store" would report a PATH entry pointing into their workspace as a fact about their
// platform. The probe is measured against the run's RESOLVED workspace root — the fence root a
// startup launch has, there being no confinement box yet — and not the `--workspace` option, which
// may be empty or relative and is no root to compare an absolute program path against.
func TestPrepareKeyMigrationCarriesAProbeRefusalIntoTheNotice(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	w := wiringFor(home, []config.ServerEntry{{Name: "workstation", APIKey: "sk-plain"}})
	w.roots.workspace = t.TempDir()
	w.opts.Workspace = "." // the raw option, deliberately not what the fence is measured against
	refusal := errors.New("keystore: refusing to run the secret store tool /ws/bin/secret-tool")
	var fenced string
	var notices bytes.Buffer

	w.prepareKeyMigration(func(root string) (secretStore, error) { fenced = root; return nil, refusal }, &notices)

	if got := notices.String(); !strings.Contains(got, refusal.Error()) {
		t.Errorf("notice = %q; want the probe's own sentence, not the absent-store one", got)
	}
	if got := notices.String(); strings.Contains(got, reasonNoStore) {
		t.Errorf("notice = %q; a refused store tool was reported as a machine with no store", got)
	}
	if fenced != w.roots.workspace {
		t.Errorf("probe fenced against %q; want the run's resolved workspace root %q", fenced, w.roots.workspace)
	}
}

// A config with no plaintext key asks nothing, says nothing, and does not even probe: the Linux
// probe is a subprocess on the start-up path, and a run with nothing to offer has no use for it.
func TestPrepareKeyMigrationSkipsTheProbeWithNothingToOffer(t *testing.T) {
	t.Parallel()

	w := wiringFor(t.TempDir(), []config.ServerEntry{
		{Name: "by-command", APIKeyCmd: "pass show apogee/work"},
		{Name: "acknowledged", APIKey: "sk-plain", PlaintextKeyOK: true},
	})
	var notices bytes.Buffer
	probed := false

	w.prepareKeyMigration(func(string) (secretStore, error) { probed = true; return nil, keystore.ErrNoStore }, &notices)

	if probed {
		t.Error("the secret store was probed for a config with nothing to migrate")
	}
	if notices.Len() != 0 {
		t.Errorf("a config with nothing to migrate was told about it: %q", notices.String())
	}
}

// The migrator seam resolves the key off the entry it was asked about, and refuses a name that no
// longer carries one rather than storing an empty secret.
func TestKeyMigratorRefusesAnEntryWithNoPlaintextKey(t *testing.T) {
	t.Parallel()

	w := wiringFor(t.TempDir(), []config.ServerEntry{{Name: "by-command", APIKeyCmd: "pass show x"}})
	w.secrets = &fakeStore{t: t}

	_, err := w.keyMigrator()("by-command")
	if err == nil || !strings.Contains(err.Error(), "by-command") {
		t.Fatalf("error = %v; want a refusal naming the entry", err)
	}
}

// A headless run is TOLD about a plaintext key and never asked about one: the notice reaches
// stderr, where it cannot contaminate the answer, and the run proceeds exactly as it would have.
func TestHeadlessNoticesPlaintextKeysAndNeverPrompts(t *testing.T) {
	stub := &stubRunner{res: run.Result{FinalText: "ok", Turns: 1}}
	prev := runOnce
	runOnce = stub.once
	t.Cleanup(func() { runOnce = prev })
	t.Setenv(config.EnvMode, "")

	home, path := plaintextConfig(t, onePlaintextServer)

	cmd := newHeadlessCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--config", home, "--workspace", t.TempDir(), "--no-save", "hi"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("headless: %v\n%s", err, errOut.String())
	}

	if got := errOut.String(); !strings.Contains(got, "workstation") ||
		!strings.Contains(got, "api-key-env") {
		t.Errorf("stderr = %q; want the plaintext-key notice", got)
	}
	// The reason has to be the true one for THIS driver: a headless run never probes, so it cannot
	// claim the machine has no store — it may well have one, and the human would go looking for a
	// missing keyring instead of reading on.
	if got := errOut.String(); !strings.Contains(got, "headless runs never prompt") {
		t.Errorf("stderr = %q; want the headless reason for the notice", got)
	}
	if got := errOut.String(); strings.Contains(got, "has no secret store") {
		t.Errorf("stderr = %q; the headless notice claims a machine it never probed has no store", got)
	}
	if strings.Contains(out.String(), "workstation") {
		t.Errorf("the notice contaminated the answer: %q", out.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the config: %v", err)
	}
	if !strings.Contains(string(data), "api-key: sk-plain-as-day") {
		t.Errorf("an unattended run edited the config it was only meant to report on:\n%s", data)
	}
}
