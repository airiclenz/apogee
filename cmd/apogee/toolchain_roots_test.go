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
// in and the three pinned variables as it saw them, one record per line, so a test can find the
// record of ITS probe by the home it named even if the process-wide library probed concurrently
// through the same PATH.
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
func installFakeGo(t *testing.T, answers []string, exit int) (log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake go is a shell script")
	}

	dir := t.TempDir()
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

	got := probeToolchainRoots(context.Background(), t.TempDir(), t.TempDir())

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

	got := probeToolchainRoots(context.Background(), t.TempDir(), t.TempDir())

	if want := []string{realPath(t, goroot)}; !slices.Equal(got, want) {
		t.Errorf("probeToolchainRoots() = %v; want only the existing directory %v", got, want)
	}
}

// TestProbeToolchainRootsRunsInTheHomeWithThePins: the probe runs in the apogee home — never the
// workspace, whose go.mod would steer `go env` into a toolchain download — and with the three
// pins set whatever the process environment says, while HOME still reaches it (GOMODCACHE defaults
// beneath it).
func TestProbeToolchainRootsRunsInTheHomeWithThePins(t *testing.T) {
	home := realPath(t, t.TempDir())
	workspace := t.TempDir()
	t.Setenv("GOTOOLCHAIN", "go1.99.0+auto")
	t.Setenv("GOWORK", filepath.Join(workspace, "go.work"))
	t.Setenv("GOFLAGS", "-mod=mod")
	log := installFakeGo(t, []string{home}, 0)

	probeToolchainRoots(context.Background(), home, workspace)

	var mine []fakeGoRecord
	for _, rec := range fakeGoRecords(t, log) {
		if rec.dir == home {
			mine = append(mine, rec)
		}
	}
	if len(mine) != 1 {
		t.Fatalf("the fake go ran %d time(s) in the home %s; want exactly the probe's one", len(mine), home)
	}
	rec := mine[0]
	if rec.toolchain != "local" || rec.work != "off" || rec.flags != "-mod=readonly" {
		t.Errorf("the probe ran with GOTOOLCHAIN=%q GOWORK=%q GOFLAGS=%q; want local / off / -mod=readonly "+
			"whatever the process exported", rec.toolchain, rec.work, rec.flags)
	}
	if !rec.homeVisible {
		t.Error("the probe ran without HOME; the module cache's default location could not be answered")
	}
}

// TestProbeToolchainRootsYieldsNothingWithoutGo: an empty PATH means no toolchain, and no roots.
func TestProbeToolchainRootsYieldsNothingWithoutGo(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if got := probeToolchainRoots(context.Background(), t.TempDir(), t.TempDir()); got != nil {
		t.Errorf("probeToolchainRoots() = %v with no go on PATH; want nil", got)
	}
}

// TestProbeToolchainRootsYieldsNothingWhenTheProbeFails: a `go` that exits non-zero — the
// toolchain-download failure the pins exist to prevent, or any other — yields no roots and no
// error surfaces anywhere: the session starts with a smaller library, not a complaint.
func TestProbeToolchainRootsYieldsNothingWhenTheProbeFails(t *testing.T) {
	goroot := t.TempDir()
	installFakeGo(t, []string{goroot, "go: downloading go1.99.0 (linux/amd64)"}, 1)

	if got := probeToolchainRoots(context.Background(), t.TempDir(), t.TempDir()); got != nil {
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
	home := realPath(t, t.TempDir())
	lib.start(home, t.TempDir())
	lib.start(t.TempDir(), t.TempDir())
	lib.wait()

	if got, want := lib.roots(), []string{realPath(t, goroot)}; !slices.Equal(got, want) {
		t.Errorf("roots() = %v after the probe; want %v", got, want)
	}
	var runs int
	for _, rec := range fakeGoRecords(t, log) {
		if rec.dir == home {
			runs++
		}
	}
	if runs != 1 {
		t.Errorf("the fake go ran %d time(s) for the library; want once, the second start being a no-op", runs)
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
