package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// fakeGoRecord is what the fake `go` of these tests writes per invocation: the directory it ran
// in and the three pinned variables as it saw them, one record per line. Each installFakeGo bakes
// its own log path into its own script, so a fake's log carries only the probes run through THAT
// fake — every record in it is the test's own, whatever the process-wide library did meanwhile.
type fakeGoRecord struct {
	dir         string
	toolchain   string
	work        string
	flags       string
	homeVisible bool
}

// installFakeGo puts a `go` on PATH — a shell script, so these tests are POSIX-only — that logs
// every invocation and prints the answers given, one per line, with the exit status given. Only
// that directory is on PATH afterwards, so the real toolchain is out of reach for the duration.
// The fake lives in its own t.TempDir, outside any workspace a test names: the ordinary case,
// where the argv[0] fence lets the probe through.
func installFakeGo(t *testing.T, answers []string, exit int) (log string) {
	t.Helper()

	return installFakeGoIn(t, t.TempDir(), answers, exit)
}

// installFakeGoIn is installFakeGo with the directory chosen by the caller — the inside-workspace
// case plants the fake beneath the workspace the probe is fenced against. dir must exist.
func installFakeGoIn(t *testing.T, dir string, answers []string, exit int) (log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake go is a shell script")
	}

	log = filepath.Join(dir, "go.log")
	var body strings.Builder
	body.WriteString("#!/bin/sh\n")
	body.WriteString("printf '%s\\t%s\\t%s\\t%s\\t%s\\n' \"$PWD\" \"$GOTOOLCHAIN\" \"$GOWORK\" \"$GOFLAGS\" \"${HOME:+set}\" >> ")
	body.WriteString(shellQuote(log))
	body.WriteString("\n")
	for _, answer := range answers {
		body.WriteString("printf '%s\\n' ")
		body.WriteString(shellQuote(answer))
		body.WriteString("\n")
	}
	body.WriteString("exit " + strconv.Itoa(exit) + "\n")
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(body.String()), 0o700); err != nil {
		t.Fatalf("write the fake go: %v", err)
	}
	t.Setenv("PATH", dir)
	return log
}

// shellQuote wraps s in single quotes for the fake script, escaping any single quote inside.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// fakeGoRecords reads the fake's log back as records.
func fakeGoRecords(t *testing.T, log string) []fakeGoRecord {
	t.Helper()

	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the fake go was never run (no log): %v", err)
	}
	var records []fakeGoRecord
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("malformed fake go record %q", line)
		}
		records = append(records, fakeGoRecord{
			dir: fields[0], toolchain: fields[1], work: fields[2], flags: fields[3],
			homeVisible: fields[4] == "set",
		})
	}
	return records
}

// TestProbeToolchainRootsMountsTheTwoAnswersAsRealDirectories: a `go` that answers two existing
// directories yields both, in that order, as their real paths — a GOROOT reached through a symlink
// is announced as the directory it resolves to, which is the only spelling the read fence mounts.
func TestProbeToolchainRootsMountsTheTwoAnswersAsRealDirectories(t *testing.T) {
	tmp := t.TempDir()
	goroot := filepath.Join(tmp, "go-real")
	modcache := filepath.Join(tmp, "mod")
	for _, dir := range []string{goroot, modcache} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	gorootLink := filepath.Join(tmp, "go")
	if err := os.Symlink(goroot, gorootLink); err != nil {
		t.Fatalf("symlink the fake GOROOT: %v", err)
	}
	installFakeGo(t, []string{gorootLink, modcache}, 0)

	got := probeToolchainRoots(context.Background(), t.TempDir())

	want := []string{realPath(t, goroot), realPath(t, modcache)}
	if !slices.Equal(got, want) {
		t.Errorf("probeToolchainRoots() = %v; want the two answers as real paths %v", got, want)
	}
}

// TestProbeToolchainRootsDropsAnAnswerThatIsNotADirectory: an absent module cache (no build has
// created it yet), an answer that is a file, and an empty line (an unset variable) are dropped, and
// the roots that remain keep their order — the model is never told about a tree it cannot open.
func TestProbeToolchainRootsDropsAnAnswerThatIsNotADirectory(t *testing.T) {
	tmp := t.TempDir()
	goroot := filepath.Join(tmp, "go")
	if err := os.MkdirAll(goroot, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	installFakeGo(t, []string{filepath.Join(tmp, "absent"), "", file, goroot}, 0)

	got := probeToolchainRoots(context.Background(), t.TempDir())

	if want := []string{realPath(t, goroot)}; !slices.Equal(got, want) {
		t.Errorf("probeToolchainRoots() = %v; want only the existing directory %v", got, want)
	}
}

// TestProbeToolchainRootsRunsInTheTempRootWithThePins: the probe runs in the temp root — never
// the workspace, whose go.mod would steer `go env` into a toolchain download, and never a
// directory the caller minted and may reclaim — and with the three pins set whatever the process
// environment says, while HOME still reaches it (GOMODCACHE defaults beneath it). The fake's `$PWD`
// is the kernel's resolved cwd, so a symlinked temp root (macOS /var) is compared through realPath.
func TestProbeToolchainRootsRunsInTheTempRootWithThePins(t *testing.T) {
	answer := realPath(t, t.TempDir())
	workspace := t.TempDir()
	t.Setenv("GOTOOLCHAIN", "go1.99.0+auto")
	t.Setenv("GOWORK", filepath.Join(workspace, "go.work"))
	t.Setenv("GOFLAGS", "-mod=mod")
	log := installFakeGo(t, []string{answer}, 0)

	probeToolchainRoots(context.Background(), workspace)

	records := fakeGoRecords(t, log)
	if len(records) != 1 {
		t.Fatalf("the fake go ran %d time(s); want exactly the probe's one", len(records))
	}
	rec := records[0]
	if want := realPath(t, os.TempDir()); rec.dir != want {
		t.Errorf("the probe ran in %s; want the temp root %s — neither the workspace nor a caller's dir", rec.dir, want)
	}
	if rec.toolchain != "local" || rec.work != "off" || rec.flags != "-mod=readonly" {
		t.Errorf("the probe ran with GOTOOLCHAIN=%q GOWORK=%q GOFLAGS=%q; want local / off / -mod=readonly "+
			"whatever the process exported", rec.toolchain, rec.work, rec.flags)
	}
	if !rec.homeVisible {
		t.Error("the probe ran without HOME; the module cache's default location could not be answered")
	}
}

// TestProbeToolchainRootsRefusesAGoInsideTheWorkspace: a `go` that PATH resolves inside the
// workspace — an activated `.venv/bin`, the shape of the defect — is refused by the argv[0] fence
// like every other exec site's program: nil roots, and the fake is never spawned (no log is
// written), because the host's own PATH is what names the program and the tree the model writes
// to may not supply one that runs at boot.
func TestProbeToolchainRootsRefusesAGoInsideTheWorkspace(t *testing.T) {
	workspace := t.TempDir()
	bin := filepath.Join(workspace, ".venv", "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatalf("mkdir the workspace's bin: %v", err)
	}
	log := installFakeGoIn(t, bin, []string{t.TempDir()}, 0)

	got := probeToolchainRoots(context.Background(), workspace)

	if got != nil {
		t.Errorf("probeToolchainRoots() = %v with go inside the workspace; want nil", got)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Errorf("the fake go inside the workspace was spawned (log stat: %v); want it refused before it ran", err)
	}
}

// TestProbeToolchainRootsYieldsNothingWithoutGo: an empty PATH means no toolchain, and no roots.
func TestProbeToolchainRootsYieldsNothingWithoutGo(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if got := probeToolchainRoots(context.Background(), t.TempDir()); got != nil {
		t.Errorf("probeToolchainRoots() = %v with no go on PATH; want nil", got)
	}
}

// TestProbeToolchainRootsYieldsNothingWhenTheProbeFails: a `go` that exits non-zero — the
// toolchain-download failure the pins exist to prevent, or any other — yields no roots and no
// error surfaces anywhere: the session starts with a smaller library, not a complaint.
func TestProbeToolchainRootsYieldsNothingWhenTheProbeFails(t *testing.T) {
	goroot := t.TempDir()
	installFakeGo(t, []string{goroot, "go: downloading go1.99.0 (linux/amd64)"}, 1)

	if got := probeToolchainRoots(context.Background(), t.TempDir()); got != nil {
		t.Errorf("probeToolchainRoots() = %v from a failing probe; want nil", got)
	}
}

// TestToolchainLibraryProbesOnceAndAnswersLive: the library answers nothing before its probe has
// finished and the probed roots after; a second start is a no-op, so the fake runs exactly once
// however many Drivers compose over the library.
func TestToolchainLibraryProbesOnceAndAnswersLive(t *testing.T) {
	goroot := t.TempDir()
	log := installFakeGo(t, []string{goroot}, 0)
	lib := newToolchainLibrary()

	if got := lib.roots(); got != nil {
		t.Fatalf("a library nobody started answers %v; want nil", got)
	}
	lib.start(t.TempDir())
	lib.start(t.TempDir())
	lib.wait()

	if got, want := lib.roots(), []string{realPath(t, goroot)}; !slices.Equal(got, want) {
		t.Errorf("roots() = %v after the probe; want %v", got, want)
	}
	if runs := len(fakeGoRecords(t, log)); runs != 1 {
		t.Errorf("the fake go ran %d time(s) for the library; want once, the second start being a no-op", runs)
	}
}

// TestToolchainLibraryOutlivesTheCallerThatStartedIt: the probe's cwd is not the caller's to
// reclaim. A library started with a workspace that is removed the moment start returns — the
// shape of a unit test whose t.TempDir home went away before the goroutine execed `go env` — still
// probes once and answers the directory the fake named, because the probe runs in the temp root
// and not in anything the caller owns.
func TestToolchainLibraryOutlivesTheCallerThatStartedIt(t *testing.T) {
	goroot := t.TempDir()
	log := installFakeGo(t, []string{goroot}, 0)
	lib := newToolchainLibrary()

	workspace, err := os.MkdirTemp("", "apogee-booter-*")
	if err != nil {
		t.Fatalf("mint the booter's workspace: %v", err)
	}
	lib.start(workspace)
	if err := os.RemoveAll(workspace); err != nil {
		t.Fatalf("reclaim the booter's workspace: %v", err)
	}
	lib.wait()

	if got, want := lib.roots(), []string{realPath(t, goroot)}; !slices.Equal(got, want) {
		t.Errorf("roots() = %v after the caller's directory went away; want %v", got, want)
	}
	if runs := len(fakeGoRecords(t, log)); runs != 1 {
		t.Errorf("the fake go ran %d time(s); want once", runs)
	}
}

// TestComposeReadRootsListsSkillsThenToolchain: the composed func is the skill roots followed by
// the toolchain roots, both read live, and the same list on every call.
func TestComposeReadRootsListsSkillsThenToolchain(t *testing.T) {
	t.Parallel()

	skills := []string{"/lib/skills", "/ws/.apogee/skills"}
	toolchain := []string{"/usr/local/go", "/home/op/go/pkg/mod"}
	roots := composeReadRoots(
		func() []string { return skills },
		func() []string { return toolchain },
	)

	want := []string{"/lib/skills", "/ws/.apogee/skills", "/usr/local/go", "/home/op/go/pkg/mod"}
	if got := roots(); !slices.Equal(got, want) {
		t.Errorf("composed roots = %v; want skills then toolchain %v", got, want)
	}
	toolchain = nil
	if got := roots(); !slices.Equal(got, skills) {
		t.Errorf("composed roots = %v after the toolchain half emptied; want the skills alone %v", got, skills)
	}
}

// realPath is the symlink-resolved spelling of p — what a probed root is announced as, and what a
// t.TempDir under a symlinked temp root (macOS /tmp) has to be compared through.
func realPath(t *testing.T, p string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("resolve %s: %v", p, err)
	}
	return resolved
}
