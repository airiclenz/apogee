package scheme

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeScheme drops a file into a user schemes directory, creating the directory.
func writeScheme(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %q: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %q: %v", name, err)
	}
}

func TestDiscoverWithoutUserDirListsBuiltins(t *testing.T) {
	t.Parallel()
	got := Discover(filepath.Join(t.TempDir(), "never-created"))
	if !slices.Equal(got, builtinNames()) {
		t.Fatalf("Discover of a missing directory = %v, want the built-ins %v", got, builtinNames())
	}
	if !slices.Contains(got, DefaultName) {
		t.Errorf("Discover = %v, want it to offer the default %q", got, DefaultName)
	}
}

func TestDiscoverMergesUserFilesSortedAndDeduped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeScheme(t, dir, DefaultName+fileExt, "") // shadows a built-in: one name, not two
	writeScheme(t, dir, "zebra.yaml", "")
	writeScheme(t, dir, "aardvark.yaml", "")
	writeScheme(t, dir, "notes.txt", "") // not a scheme file
	writeScheme(t, dir, ".yaml", "")     // no name left once the extension is stripped
	if err := os.Mkdir(filepath.Join(dir, "nested.yaml"), 0o700); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	want := append([]string{"aardvark", "zebra"}, builtinNames()...)
	slices.Sort(want)

	got := Discover(dir)
	if !slices.Equal(got, want) {
		t.Fatalf("Discover = %v, want %v", got, want)
	}
	if !slices.IsSorted(got) {
		t.Errorf("Discover = %v, want it sorted", got)
	}
}

func TestResolvePrefersUserFileOverBuiltin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const wantError = "#010203"
	writeScheme(t, dir, DefaultName+fileExt, "error: \""+wantError+"\"\n")

	got, warnings := Resolve(DefaultName, dir)
	if len(warnings) != 0 {
		t.Fatalf("a valid user scheme warned: %v", warnings)
	}
	if got.Error != wantError {
		t.Errorf("error = %q, want the shadowing user file's %q", got.Error, wantError)
	}
	if got.Gauge != Default().Gauge {
		t.Errorf("gauge = %q, want the default %q — a partial shadow still inherits", got.Gauge, Default().Gauge)
	}
}

func TestResolveFallsBackToBuiltin(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{filepath.Join(t.TempDir(), "never-created"), ""} {
		got, warnings := Resolve(DefaultName, dir)
		if len(warnings) != 0 {
			t.Errorf("userDir %q: built-in resolve warned: %v", dir, warnings)
		}
		if got != Default() {
			t.Errorf("userDir %q: got %+v, want the built-in %q", dir, got, DefaultName)
		}
	}
}

func TestResolveUnknownNameWarnsAndUsesDefault(t *testing.T) {
	t.Parallel()
	got, warnings := Resolve("chartreuse", t.TempDir())
	if got != Default() {
		t.Errorf("got %+v, want the default scheme", got)
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
	}
	line := warnings[0].String()
	if !strings.Contains(line, "chartreuse") || !strings.Contains(line, DefaultName) {
		t.Errorf("warning reads %q, want it to name the unknown scheme and the fallback", line)
	}
}

func TestResolveUnreadableFileWarnsAndUsesDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A directory standing where the scheme file belongs fails the read on every OS,
	// unlike chmod 000, which a test running as root would sail straight through.
	if err := os.Mkdir(filepath.Join(dir, DefaultName+fileExt), 0o700); err != nil {
		t.Fatalf("create blocking dir: %v", err)
	}

	got, warnings := Resolve(DefaultName, dir)
	if got != Default() {
		t.Errorf("got %+v, want the default scheme", got)
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
	}
	if line := warnings[0].String(); !strings.Contains(line, DefaultName+fileExt) {
		t.Errorf("warning reads %q, want it to name the file that would not open", line)
	}
}

func TestResolvePassesParseWarningsThrough(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeScheme(t, dir, "mine.yaml", "error: \"#zz0000\"\n")

	got, warnings := Resolve("mine", dir)
	if got.Error != Default().Error {
		t.Errorf("error = %q, want the default %q kept", got.Error, Default().Error)
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
	}
	if w := warnings[0]; w.File != "mine.yaml" || w.Key != "error" {
		t.Errorf("got %+v, want the user file and key named", w)
	}
}

func TestExportWritesEmbeddedBytesVerbatim(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "schemes") // absent on purpose: Export creates it

	path, err := Export(DefaultName, dir)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if want := filepath.Join(dir, DefaultName+fileExt); path != want {
		t.Errorf("Export returned %q, want %q", path, want)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the export: %v", err)
	}
	want, ok := builtinBytes(DefaultName)
	if !ok {
		t.Fatalf("embedded scheme %q is missing", DefaultName)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("exported %d bytes, want the embedded %d verbatim — comments are the point", len(got), len(want))
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat the created directory: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory mode %o, want 700", perm)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the export: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode %o, want 600", perm)
	}

	// The whole point of an export is that it loads back as the scheme it copied.
	loaded, warnings := Resolve(DefaultName, dir)
	if len(warnings) != 0 {
		t.Errorf("the exported file warned on load: %v", warnings)
	}
	if loaded != Default() {
		t.Errorf("exported scheme loaded as %+v, want %+v", loaded, Default())
	}
}

func TestExportRefusesToOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path, err := Export(DefaultName, dir)
	if err != nil {
		t.Fatalf("first Export: %v", err)
	}
	const edited = "error: \"#010203\"\n"
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatalf("simulate a user edit: %v", err)
	}

	if _, err := Export(DefaultName, dir); err == nil {
		t.Fatal("second Export succeeded, want a refusal")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("refusal reads %q, want it to say the file already exists", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != edited {
		t.Error("the refused export still clobbered the user's edits")
	}
}

func TestExportRejectsNamesThatAreNotBuiltin(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"chartreuse", "", "../escape", "sub/dark"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Nested and absent, so "did Export touch the filesystem at all" is one stat.
			dir := filepath.Join(t.TempDir(), "schemes")
			path, err := Export(name, dir)
			if err == nil {
				t.Fatalf("Export(%q) wrote %q, want an error", name, path)
			}
			if !strings.Contains(err.Error(), DefaultName) {
				t.Errorf("error reads %q, want it to list the built-ins", err)
			}
			if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
				t.Errorf("a rejected export created %q", dir)
			}
		})
	}
}
