package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The launcher-fronted config the `launch-profile:` cases start from — the shape ValidateServers
// asks for, since a pointer on an entry with no `llama-launcher:` key is refused rather than
// written. It carries a comment inside the entry and one after it, so a splice that ate either
// would show.
const launcherServersConfig = "server: rig\n" +
	"\n" +
	"# the launcher-fronted box\n" +
	"servers:\n" +
	"  - name: rig\n" +
	"    endpoint: http://192.168.64.1:1111\n" +
	"    llama-launcher: auto   # its own config decides the profiles\n" +
	"\n" +
	"# nothing below the list may move\n" +
	"ui:\n" +
	"  spinner: snake\n"

// The model a plain server's entry records once the user has picked it.
const pickedModel = "qwen3-30b-a3b"

func TestSaveServerEntrySetting_ReplacesAnExistingModelLine(t *testing.T) {
	before := readFixture(t, serversFixture)
	path := writeTestConfig(t, before)

	if err := SaveServerEntrySetting(path, "workbench", entryModelKey, pickedModel); err != nil {
		t.Fatalf("record the model on the entry: %v", err)
	}

	written := readTestConfig(t, path)
	compareGolden(t, "servers-edited.model-workbench.golden", written)
	if got := serverEntry(t, written, "workbench").Model; got != pickedModel {
		t.Errorf("model = %q, want %q", got, pickedModel)
	}
	b, a := SplitConfigLines([]byte(before)), SplitConfigLines([]byte(written))
	if len(a) != len(b) {
		t.Fatalf("a replacement changed the line count: %d -> %d", len(b), len(a))
	}
	for i := range b {
		if b[i] != a[i] && !strings.Contains(a[i], entryModelKey+":") {
			t.Errorf("line %d changed to something other than the model line: %q", i+1, a[i])
		}
	}
}

func TestSaveServerEntrySetting_InsertsTheModelIntoAnEntryThatLacksIt(t *testing.T) {
	before := readFixture(t, serversFixture)
	path := writeTestConfig(t, before)

	if err := SaveServerEntrySetting(path, "spare", entryModelKey, pickedModel); err != nil {
		t.Fatalf("record the model on the entry: %v", err)
	}

	written := readTestConfig(t, path)
	compareGolden(t, "servers-edited.model-spare.golden", written)
	at, inserted := splicedInsertion(t, before, written)
	if len(inserted) != 1 || inserted[0] != "      "+entryModelKey+": "+pickedModel {
		t.Fatalf("want one model line at the entry's own indentation on line %d, got %q", at, inserted)
	}
	if got := serverEntry(t, written, "spare").Model; got != pickedModel {
		t.Errorf("model = %q, want %q", got, pickedModel)
	}
	if got := serverEntry(t, written, "workbench").Model; got != "gpt-oss-20b" {
		t.Errorf("the neighbouring entry's model changed to %q", got)
	}
}

// The last entry has nothing after it to insert before, so the appended line goes at the end of the
// list — with the paragraphs below it still where the user put them.
func TestSaveServerEntrySetting_AppendsToTheLastEntry(t *testing.T) {
	before := readFixture(t, serversFixture)
	path := writeTestConfig(t, before)

	if err := SaveServerEntrySetting(path, "cloud", entryModelKey, pickedModel); err != nil {
		t.Fatalf("record the model on the last entry: %v", err)
	}

	written := readTestConfig(t, path)
	if _, inserted := splicedInsertion(t, before, written); len(inserted) != 1 {
		t.Fatalf("want one line inserted, got %q", inserted)
	}
	if got := serverEntry(t, written, "cloud").Model; got != pickedModel {
		t.Errorf("model = %q, want %q", got, pickedModel)
	}
}

func TestSaveServerEntrySetting_WritesTheLaunchProfilePointer(t *testing.T) {
	path := writeTestConfig(t, launcherServersConfig)

	if err := SaveServerEntrySetting(path, "rig", entryLaunchProfileKey, "gpt-oss-20b"); err != nil {
		t.Fatalf("record the launch profile on the entry: %v", err)
	}

	written := readTestConfig(t, path)
	if _, inserted := splicedInsertion(t, launcherServersConfig, written); len(inserted) != 1 {
		t.Fatalf("want one line inserted, got %q", inserted)
	}
	entry := serverEntry(t, written, "rig")
	if entry.LaunchProfile != "gpt-oss-20b" || entry.Model != "" {
		t.Errorf("want the pointer alone, got launch-profile %q model %q", entry.LaunchProfile, entry.Model)
	}

	// A second pick replaces the line the first one wrote.
	if err := SaveServerEntrySetting(path, "rig", entryLaunchProfileKey, "gemma-4"); err != nil {
		t.Fatalf("re-record the launch profile: %v", err)
	}
	rewritten := readTestConfig(t, path)
	if got := serverEntry(t, rewritten, "rig").LaunchProfile; got != "gemma-4" {
		t.Errorf("launch-profile = %q, want gemma-4", got)
	}
	if a, b := SplitConfigLines([]byte(written)), SplitConfigLines([]byte(rewritten)); len(a) != len(b) {
		t.Errorf("a replacement changed the line count: %d -> %d", len(a), len(b))
	}
	if !strings.Contains(rewritten, "llama-launcher: auto   # its own config decides the profiles") {
		t.Errorf("the entry's other lines did not survive\n%s", rewritten)
	}
}

func TestSaveServerEntrySetting_KeepsTheAlignmentAndTheEndOfLineNote(t *testing.T) {
	const config = "server: box\nservers:\n  - name: box\n" +
		"    endpoint: http://127.0.0.1:1111\n" +
		"    model:      gpt-oss-20b    # what I run most days\n"
	path := writeTestConfig(t, config)

	if err := SaveServerEntrySetting(path, "box", entryModelKey, pickedModel); err != nil {
		t.Fatalf("record the model on the entry: %v", err)
	}

	written := readTestConfig(t, path)
	const want = "    model:      " + pickedModel + "    # what I run most days"
	if !strings.Contains(written, want) {
		t.Errorf("want the rewritten line to read\n%s\ngot\n%s", want, written)
	}
}

func TestSaveServerEntrySetting_QuotesAValueABareScalarWouldMisread(t *testing.T) {
	path := writeTestConfig(t, readFixture(t, serversFixture))
	const id = "off"

	if err := SaveServerEntrySetting(path, "workbench", entryModelKey, id); err != nil {
		t.Fatalf("record the model on the entry: %v", err)
	}

	written := readTestConfig(t, path)
	if got := serverEntry(t, written, "workbench").Model; got != id {
		t.Errorf("the value did not survive the round trip: %q, want %q\n%s", got, id, written)
	}
}

func TestSaveServerEntrySetting_ReSettingTheSameValueWritesNothing(t *testing.T) {
	path := writeTestConfig(t, readFixture(t, serversFixture))
	if err := SaveServerEntrySetting(path, "workbench", entryModelKey, pickedModel); err != nil {
		t.Fatalf("record the model on the entry: %v", err)
	}
	once := readTestConfig(t, path)
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the written config: %v", err)
	}

	if err := SaveServerEntrySetting(path, "workbench", entryModelKey, pickedModel); err != nil {
		t.Fatalf("re-record the same model: %v", err)
	}

	if twice := readTestConfig(t, path); twice != once {
		t.Errorf("a re-set rewrote the file\n--- once ---\n%s--- twice ---\n%s", once, twice)
	}
	again, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the config again: %v", err)
	}
	if !again.ModTime().Equal(stat.ModTime()) {
		t.Errorf("a re-set touched the file: %v -> %v", stat.ModTime(), again.ModTime())
	}
}

func TestSaveServerEntrySetting_Refusals(t *testing.T) {
	const flowEntry = "server: box\nservers:\n" +
		"  - {name: box, endpoint: \"http://127.0.0.1:1111\"}\n"
	const plainEntry = "server: box\nservers:\n  - name: box\n    endpoint: http://127.0.0.1:1111\n"

	cases := []struct {
		name   string
		config string
		entry  string
		key    string
		value  string
		want   string
	}{
		{
			"unknown entry", readFixture(t, serversFixture), "nowhere", entryModelKey, pickedModel,
			`no servers: entry named "nowhere" — it configures "workbench", "spare" and "cloud"`,
		},
		{
			"key outside the allow-list", readFixture(t, serversFixture), "workbench", "endpoint",
			"http://elsewhere.test", "is not a servers: entry setting apogee writes",
		},
		{
			"empty value", readFixture(t, serversFixture), "workbench", entryModelKey, "   ",
			"there is no value to record",
		},
		{
			"launch-profile with no launcher to actuate it", plainEntry, "box", entryLaunchProfileKey,
			"gpt-oss-20b", "would not load",
		},
		{
			"flow-style entry", flowEntry, "box", entryModelKey, pickedModel, "flow style ({...})",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTestConfig(t, tc.config)

			err := SaveServerEntrySetting(path, tc.entry, tc.key, tc.value)

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

// An absent config is seeded from the embedded template the way every other setting write seeds it,
// so a write never leaves a bare fragment where a documented file belongs — and the seeded template
// names no server, so the edit itself then refuses with what the file does configure.
func TestSaveServerEntrySetting_SeedsAnAbsentConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	err := SaveServerEntrySetting(path, "workbench", entryModelKey, pickedModel)

	if err == nil || !strings.Contains(err.Error(), "no servers at all") {
		t.Fatalf("want a refusal naming an empty list, got %v", err)
	}
	if got := readTestConfig(t, path); got != string(defaultConfigYAML) {
		t.Errorf("the seeded config is not the embedded template\n%s", got)
	}
}

// The allow-list is checked before anything is read or written, so a key apogee does not write
// cannot even seed a config on its way to being refused.
func TestSaveServerEntrySetting_RefusesADisallowedKeyBeforeTouchingTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	if err := SaveServerEntrySetting(path, "workbench", "api-key", "sk-0001"); err == nil {
		t.Fatal("want a refusal for a key outside the allow-list")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the refused write touched the filesystem: %v", err)
	}
}

func TestSaveServerEntrySetting_RefusesWithoutAConfigPath(t *testing.T) {
	if err := SaveServerEntrySetting("", "box", entryModelKey, pickedModel); err == nil {
		t.Fatal("want a refusal when no config path is known")
	}
}
