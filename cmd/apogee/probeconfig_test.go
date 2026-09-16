package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runProbeConfig executes `apogee probe config` against a hermetic apogee home and returns
// everything it printed on both streams. It passes only --config: the verb declares no
// --workspace, because nothing it reports depends on one.
func runProbeConfig(t *testing.T, configHome string) string {
	t.Helper()
	cmd := newProbeCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"config", "--config", configHome})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("probe config: %v\n%s", err, out.String())
	}
	return out.String()
}

// seedConfigHome seeds a home with the given config.yaml and returns the home and the file.
func seedConfigHome(t *testing.T, contents string) (home, path string) {
	t.Helper()
	home = t.TempDir()
	path = filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return home, path
}

// The report carries what the live reader noticed — an unknown key, spelled as the startup
// notice spells it — and every registry key at the value the file resolves it to, without a
// pending migration. The `servers:` entry's api-key never reaches the output: the registry's
// servers row is a count, and no row renders a secret.
func TestProbeConfigReportsUnknownKeysAndResolvedValues(t *testing.T) {
	t.Parallel()
	home, path := seedConfigHome(t, "servers:\n"+
		"  - name: probe-target\n"+
		"    endpoint: http://127.0.0.1:1\n"+
		"    api-key: sk-probe-secret-value\n"+
		"server: probe-target\n"+
		"bogus: 1\n"+
		"ui:\n"+
		"  stall-after: 45s\n")

	report := runProbeConfig(t, home)

	for _, want := range []string{
		"apogee probe — config report",
		"  (nothing is written; the file is read the way a live reload reads it)",
		`apogee: config ` + path + `: unknown key "bogus" at line 6 is ignored`,
		"migration\n  (none)",
		"ui.stall-after:",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}
	if !strings.Contains(sectionOf(report, "notices"), `unknown key "bogus"`) {
		t.Errorf("the unknown-key notice is not under `notices`:\n%s", report)
	}
	resolved := sectionOf(report, "resolved")
	for _, want := range []string{"ui.stall-after:", "45s", "servers:", "1 server", "server:", "probe-target"} {
		if !strings.Contains(resolved, want) {
			t.Errorf("resolved section lacks %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "sk-probe-secret-value") {
		t.Errorf("the entry's api-key reached the report:\n%s", report)
	}
}

// A file still written in the retired top-level quadruple is REPORTED as awaiting the startup
// migration — the refusal's own sentence under `migration`, the resolved section replaced by the
// pointer at the startup that migrates it — and the file is left byte-for-byte as it was: the
// verb reads the way a live reload reads, and a live reload never writes.
func TestProbeConfigRefusesToMigrateALegacyFile(t *testing.T) {
	t.Parallel()
	legacy := "endpoint: http://127.0.0.1:1\n" +
		"api-key: sk-legacy-secret\n" +
		"host-alias: legacy-box\n" +
		"model: legacy-model\n"
	home, path := seedConfigHome(t, legacy)

	report := runProbeConfig(t, home)

	migration := sectionOf(report, "migration")
	if !strings.Contains(migration, "apogee: "+path+" still uses the retired top-level "+
		"endpoint:/api-key:/host-alias:/model: keys — the servers: list is now the single "+
		"definition of the servers you run models on.") {
		t.Errorf("the refusal sentence is not under `migration`:\n%s", report)
	}
	if !strings.Contains(sectionOf(report, "resolved"),
		"(start apogee once to migrate the file, then re-run)") {
		t.Errorf("the resolved section does not point at the startup migration:\n%s", report)
	}
	if strings.Contains(sectionOf(report, "resolved"), "ui.stall-after") {
		t.Errorf("a file awaiting migration was resolved anyway:\n%s", report)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config.yaml after the run: %v", err)
	}
	if string(after) != legacy {
		t.Errorf("probe config rewrote the file:\n%s", after)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 1 {
		t.Errorf("probe config wrote beside the file: %v", entries)
	}
}

// A file the reader refuses for any OTHER reason — here a malformed one — fails the command in
// the reader's own sentence rather than being reported as a migration.
func TestProbeConfigFailsOnAMalformedFile(t *testing.T) {
	t.Parallel()
	home, _ := seedConfigHome(t, "servers: [\n")
	cmd := newProbeCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"config", "--config", home})

	err := cmd.ExecuteContext(context.Background())

	if err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("err = %v, want the reader's parse refusal", err)
	}
	if strings.Contains(out.String(), "migration") {
		t.Errorf("a malformed file was reported as a migration:\n%s", out.String())
	}
}

// sectionOf returns the lines of the report under the given section heading, up to the next
// unindented line.
func sectionOf(report, heading string) string {
	lines := strings.Split(report, "\n")
	var section []string
	inSection := false
	for _, line := range lines {
		if line == heading {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") {
			break
		}
		section = append(section, line)
	}
	return strings.Join(section, "\n")
}
