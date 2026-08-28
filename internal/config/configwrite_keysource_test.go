package config

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The hand-edited config with a `servers:` list every key-source case starts from: a commented
// example block above the real list, list items indented deeper than the writer would have chosen,
// an aligned value with an end-of-line note on it, a keyless entry, and a last entry with nothing
// after it. What the goldens pin is that a key-source write touches ONE line of it.
const serversFixture = "servers-edited.yaml"

// The command a macOS migration persists for the fixture's first entry.
const keychainCmd = "security find-generic-password -s apogee -a workbench -w"

// serverEntry parses a written config the way apogee reads it and returns the named entry — the
// property that actually matters, as opposed to the bytes the goldens pin.
func serverEntry(t *testing.T, content, name string) ServerEntry {
	t.Helper()
	var fc fileConfig
	if err := yaml.Unmarshal([]byte(content), &fc); err != nil {
		t.Fatalf("the written config does not parse: %v\n%s", err, content)
	}
	for _, s := range fc.Servers {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("the written config has no %q entry\n%s", name, content)
	return ServerEntry{}
}

func TestSaveServerKeyCommand_ReplacesTheLiteralInPlace(t *testing.T) {
	path := writeTestConfig(t, readFixture(t, serversFixture))

	if err := SaveServerKeyCommand(path, "workbench", keychainCmd); err != nil {
		t.Fatalf("point the entry at a key command: %v", err)
	}

	written := readTestConfig(t, path)
	compareGolden(t, "servers-edited.key-cmd-workbench.golden", written)
	entry := serverEntry(t, written, "workbench")
	if entry.APIKey != "" || entry.APIKeyCmd != keychainCmd {
		t.Errorf("want the literal gone and the command in its place, got api-key %q api-key-cmd %q",
			entry.APIKey, entry.APIKeyCmd)
	}
}

func TestSaveServerKeyCommand_ReplacesOnTheLastEntry(t *testing.T) {
	path := writeTestConfig(t, readFixture(t, serversFixture))
	const cmd = "op read op://vault/cloud/credential"

	if err := SaveServerKeyCommand(path, "cloud", cmd); err != nil {
		t.Fatalf("point the last entry at a key command: %v", err)
	}

	written := readTestConfig(t, path)
	compareGolden(t, "servers-edited.key-cmd-cloud.golden", written)
	if got := serverEntry(t, written, "cloud").APIKeyCmd; got != cmd {
		t.Errorf("api-key-cmd = %q, want %q", got, cmd)
	}
	if got := serverEntry(t, written, "workbench").APIKey; got != "sk-desk-0001" {
		t.Errorf("the neighbouring entry's key changed to %q", got)
	}
}

func TestSaveServerKeyCommand_LeavesEveryOtherLineAlone(t *testing.T) {
	before := readFixture(t, serversFixture)
	path := writeTestConfig(t, before)

	if err := SaveServerKeyCommand(path, "workbench", keychainCmd); err != nil {
		t.Fatalf("point the entry at a key command: %v", err)
	}

	b, a := SplitConfigLines([]byte(before)), SplitConfigLines([]byte(readTestConfig(t, path)))
	if len(a) != len(b) {
		t.Fatalf("a replacement changed the line count: %d -> %d", len(b), len(a))
	}
	for i := range b {
		if b[i] == a[i] {
			continue
		}
		if !strings.Contains(a[i], apiKeyCmdKey+":") {
			t.Errorf("line %d changed to something other than the key-source line: %q", i+1, a[i])
		}
	}
}

func TestSaveServerKeyCommand_KeepsTheAlignmentAndTheEndOfLineNote(t *testing.T) {
	path := writeTestConfig(t, readFixture(t, serversFixture))

	if err := SaveServerKeyCommand(path, "workbench", keychainCmd); err != nil {
		t.Fatalf("point the entry at a key command: %v", err)
	}

	written := readTestConfig(t, path)
	const want = "      api-key-cmd:    " + keychainCmd + "    # the one the migration offers to move"
	if !strings.Contains(written, want) {
		t.Errorf("want the rewritten line to read\n%s\ngot\n%s", want, written)
	}
}

func TestSaveServerKeyCommand_QuotesACommandABareScalarWouldMisread(t *testing.T) {
	path := writeTestConfig(t, readFixture(t, serversFixture))
	const cmd = "keyfetch --scope apogee: workbench # primary"

	if err := SaveServerKeyCommand(path, "workbench", cmd); err != nil {
		t.Fatalf("point the entry at a key command: %v", err)
	}

	written := readTestConfig(t, path)
	if got := serverEntry(t, written, "workbench").APIKeyCmd; got != cmd {
		t.Errorf("the command did not survive the round trip: %q, want %q\n%s", got, cmd, written)
	}
}

func TestSaveServerKeyCommand_RewrittenFileRevalidates(t *testing.T) {
	path := writeTestConfig(t, readFixture(t, serversFixture))

	if err := SaveServerKeyCommand(path, "workbench", keychainCmd); err != nil {
		t.Fatalf("point the entry at a key command: %v", err)
	}

	written := readTestConfig(t, path)
	var fc fileConfig
	if err := yaml.Unmarshal([]byte(written), &fc); err != nil {
		t.Fatalf("the written config does not parse: %v\n%s", err, written)
	}
	if err := ValidateServers(fc.Servers); err != nil {
		t.Fatalf("the rewritten servers: list does not validate: %v\n%s", err, written)
	}
	if set := keySourceKeys(serverEntry(t, written, "workbench")); len(set) != 1 || set[0] != apiKeyCmdKey+":" {
		t.Errorf("want exactly the command source set, got %v", set)
	}
}

func TestSaveServerKeyCommand_ReSavingTheSameCommandWritesNothing(t *testing.T) {
	path := writeTestConfig(t, readFixture(t, serversFixture))
	if err := SaveServerKeyCommand(path, "workbench", keychainCmd); err != nil {
		t.Fatalf("point the entry at a key command: %v", err)
	}
	once := readTestConfig(t, path)

	if err := SaveServerKeyCommand(path, "workbench", keychainCmd); err != nil {
		t.Fatalf("re-save the same command: %v", err)
	}

	if twice := readTestConfig(t, path); twice != once {
		t.Errorf("a re-save rewrote the file\n--- once ---\n%s--- twice ---\n%s", once, twice)
	}
}

func TestSaveServerKeyCommand_Refusals(t *testing.T) {
	const flowList = "server: box\n" +
		"servers: [{name: box, endpoint: \"http://127.0.0.1:1111\", api-key: sk-1}]\n"
	const flowEntry = "server: box\nservers:\n" +
		"  - {name: box, endpoint: \"http://127.0.0.1:1111\", api-key: sk-1}\n"
	const wrappedKey = "server: box\nservers:\n  - name: box\n" +
		"    endpoint: http://127.0.0.1:1111\n    api-key: >\n      sk-wrapped\n"
	const noLiteral = "server: box\nservers:\n  - name: box\n    endpoint: http://127.0.0.1:1111\n"
	const noList = "mode: auto\n"

	cases := []struct {
		name    string
		config  string
		entry   string
		command string
		want    string
	}{
		{"unknown entry", readFixture(t, serversFixture), "nowhere", keychainCmd, "no servers: entry named"},
		{"flow-style list", flowList, "box", keychainCmd, "flow style ([...])"},
		{"flow-style entry", flowEntry, "box", keychainCmd, "flow style ({...})"},
		{"api-key spanning lines", wrappedKey, "box", keychainCmd, "multi-line block"},
		{"no literal to replace", noLiteral, "box", keychainCmd, "no api-key: line to replace"},
		{"no servers: list", noList, "box", keychainCmd, "no servers: entry named"},
		{"empty command", readFixture(t, serversFixture), "workbench", "   ", "the command is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTestConfig(t, tc.config)

			err := SaveServerKeyCommand(path, tc.entry, tc.command)

			if err == nil {
				t.Fatalf("want a refusal, got none:\n%s", readTestConfig(t, path))
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not say %q", err, tc.want)
			}
			if got := readTestConfig(t, path); got != tc.config {
				t.Errorf("a refused edit rewrote the file\n--- got ---\n%s--- want ---\n%s", got, tc.config)
			}
		})
	}
}

func TestSaveServerPlaintextKeyOK_AppendsTheMarkerToAMidListEntry(t *testing.T) {
	before := readFixture(t, serversFixture)
	path := writeTestConfig(t, before)

	if err := SaveServerPlaintextKeyOK(path, "workbench"); err != nil {
		t.Fatalf("mark the entry: %v", err)
	}

	written := readTestConfig(t, path)
	compareGolden(t, "servers-edited.marker-workbench.golden", written)
	at, inserted := splicedInsertion(t, before, written)
	if len(inserted) != 1 || strings.TrimSpace(inserted[0]) != plaintextKeyOKKey+": true" {
		t.Fatalf("want one marker line inserted at %d, got %q", at, inserted)
	}
	if !serverEntry(t, written, "workbench").PlaintextKeyOK {
		t.Error("the written entry does not read back as marked")
	}
}

func TestSaveServerPlaintextKeyOK_AppendsTheMarkerToTheLastEntry(t *testing.T) {
	before := readFixture(t, serversFixture)
	path := writeTestConfig(t, before)

	if err := SaveServerPlaintextKeyOK(path, "cloud"); err != nil {
		t.Fatalf("mark the last entry: %v", err)
	}

	written := readTestConfig(t, path)
	compareGolden(t, "servers-edited.marker-cloud.golden", written)
	if _, inserted := splicedInsertion(t, before, written); len(inserted) != 1 {
		t.Fatalf("want one line inserted, got %q", inserted)
	}
	if !serverEntry(t, written, "cloud").PlaintextKeyOK {
		t.Error("the written entry does not read back as marked")
	}
}

func TestSaveServerPlaintextKeyOK_RewritesAnEntryThatSpellsItFalse(t *testing.T) {
	const config = "server: box\nservers:\n  - name: box\n" +
		"    endpoint: http://127.0.0.1:1111\n    api-key: sk-1\n" +
		"    plaintext-key-ok: false   # asked me once already\n"
	path := writeTestConfig(t, config)

	if err := SaveServerPlaintextKeyOK(path, "box"); err != nil {
		t.Fatalf("mark the entry: %v", err)
	}

	written := readTestConfig(t, path)
	if !strings.Contains(written, "plaintext-key-ok: true   # asked me once already") {
		t.Errorf("want the value rewritten and the note kept, got\n%s", written)
	}
	if !serverEntry(t, written, "box").PlaintextKeyOK {
		t.Error("the written entry does not read back as marked")
	}
}

func TestSaveServerPlaintextKeyOK_ReMarkingWritesNothing(t *testing.T) {
	path := writeTestConfig(t, readFixture(t, serversFixture))
	if err := SaveServerPlaintextKeyOK(path, "workbench"); err != nil {
		t.Fatalf("mark the entry: %v", err)
	}
	once := readTestConfig(t, path)

	if err := SaveServerPlaintextKeyOK(path, "workbench"); err != nil {
		t.Fatalf("re-mark the entry: %v", err)
	}

	if twice := readTestConfig(t, path); twice != once {
		t.Errorf("a re-mark rewrote the file\n--- once ---\n%s--- twice ---\n%s", once, twice)
	}
}

// The backstop: the entry's last value is a block scalar the node tree measures to its header line
// only, so the marker's insertion point lands INSIDE it. Nothing in the splice detects that — the
// verification does, by reading the result back and finding a config that is not the one asked for.
func TestSaveServerPlaintextKeyOK_RefusesWhenTheMarkerWouldLandInsideABlockValue(t *testing.T) {
	const config = "server: box\nservers:\n  - name: box\n    api-key: sk-1\n" +
		"    endpoint: >\n      http://127.0.0.1:1111\n"
	path := writeTestConfig(t, config)

	err := SaveServerPlaintextKeyOK(path, "box")

	if err == nil {
		t.Fatalf("want a refusal, got none:\n%s", readTestConfig(t, path))
	}
	if got := readTestConfig(t, path); got != config {
		t.Errorf("a refused edit rewrote the file\n--- got ---\n%s--- want ---\n%s", got, config)
	}
}

func TestSaveServerPlaintextKeyOK_RefusesAnUnknownEntry(t *testing.T) {
	config := readFixture(t, serversFixture)
	path := writeTestConfig(t, config)

	err := SaveServerPlaintextKeyOK(path, "nowhere")

	if err == nil || !strings.Contains(err.Error(), "no servers: entry named") {
		t.Fatalf("want a not-found refusal, got %v", err)
	}
	if got := readTestConfig(t, path); got != config {
		t.Errorf("a refused edit rewrote the file\n%s", got)
	}
}

// An entry edit is the file's SECOND reader, and it locates the entry by a name its caller holds
// from the first one — already canonical. So a padded `name:` on disk has to be matched trimmed, in
// the parsed list and in the node tree alike, or an entry that works at every other layer would be
// unwritable. The file keeps the padding: only the match is canonical.
func TestSaveServerPlaintextKeyOK_MatchesAPaddedEntryNameCanonically(t *testing.T) {
	const config = "server: box\nservers:\n  - name: \" box \"\n" +
		"    endpoint: http://127.0.0.1:1111\n    api-key: sk-1\n"

	t.Run("the padded entry is found by its canonical name", func(t *testing.T) {
		path := writeTestConfig(t, config)

		if err := SaveServerPlaintextKeyOK(path, "box"); err != nil {
			t.Fatalf("mark the entry whose name is written padded: %v", err)
		}

		written := readTestConfig(t, path)
		if !strings.Contains(written, "- name: \" box \"") {
			t.Errorf("the edit rewrote the entry's own name: scalar\n%s", written)
		}
		fc, err := parseConfigFile(path, os.ReadFile, noNotify)
		if err != nil {
			t.Fatalf("the written config does not load: %v\n%s", err, written)
		}
		if len(fc.Servers) != 1 || fc.Servers[0].Name != "box" || !fc.Servers[0].PlaintextKeyOK {
			t.Errorf("want the one entry loading as box and marked, got %+v\n%s", fc.Servers, written)
		}
	})

	t.Run("a name the file does not carry is still refused", func(t *testing.T) {
		path := writeTestConfig(t, config)

		err := SaveServerPlaintextKeyOK(path, "nowhere")

		if err == nil || !strings.Contains(err.Error(), "no servers: entry named") {
			t.Fatalf("want a not-found refusal, got %v", err)
		}
		if got := readTestConfig(t, path); got != config {
			t.Errorf("a refused edit rewrote the file\n%s", got)
		}
	})
}

// verifyEntryEdit is the gate under every entry writer, not only the two key-source ones, so its
// refusal takes the noun from its caller: a model or launch-profile write that missed its entry must
// not tell the reader "the key source" did not land. Driven directly, because the refusal is the
// backstop for a splice that landed somewhere unexpected — no reachable fixture produces it.
func TestVerifyEntryEditNamesWhatTheEditFailedToPlace(t *testing.T) {
	const config = "server: box\nservers:\n  - name: box\n    api-key: sk-1\n"
	var before fileConfig
	if err := yaml.Unmarshal([]byte(config), &before); err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	// The "edit" is the unchanged file, so the result cannot hold the entry the caller asked for.
	want := before.Servers[0]
	want.Model = "qwen3-30b"

	for _, noun := range []string{"the key source", "the model"} {
		t.Run(noun, func(t *testing.T) {
			err := verifyEntryEdit(before, before, 0, want, noun)

			if err == nil {
				t.Fatal("want a refusal when the edit did not land on the entry")
			}
			msg := fmt.Sprintf("the edit did not put %s on the %q entry", noun, want.Name)
			if got := err.Error(); !strings.Contains(got, msg) {
				t.Errorf("want a refusal containing %q, got %q", msg, got)
			}
		})
	}
}

func TestSaveServerKeyCommand_RefusesWithoutAConfigPath(t *testing.T) {
	if err := SaveServerKeyCommand("", "box", keychainCmd); err == nil {
		t.Fatal("want a refusal when no config path is known")
	}
	if err := SaveServerPlaintextKeyOK("", "box"); err == nil {
		t.Fatal("want a refusal when no config path is known")
	}
}
