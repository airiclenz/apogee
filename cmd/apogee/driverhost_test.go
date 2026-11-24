package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tui"
)

// teardownConfiner is a fenceable backend whose optional `Close() error` fails with err, so the
// teardown sentence the host's close owes is observable on every host — off Windows no real
// backend has a teardown at all.
type teardownConfiner struct {
	fakeConfiner

	err    error
	closed int
}

func (c *teardownConfiner) Close() error {
	c.closed++
	return c.err
}

// hostConfiner is the constructor a test hands the host: it builds c, the backend the test holds.
func hostConfiner(c apogee.Confiner) func() apogee.Confiner {
	return func() apogee.Confiner { return c }
}

// testDriverHost is a host over hermetic roots — a temporary apogee home and workspace — built
// without the sweeps, for the tests whose question is about one facility.
func testDriverHost(t *testing.T, opts config.Options, c apogee.Confiner) *driverHost {
	t.Helper()

	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	return newDriverHost(roots, opts, hostConfiner(c))
}

// A Driver started with no --workspace from a folder below the repository's top finds the Project
// root the config source names — the repository top holding .apogee/ — and records that source,
// while a Driver opened without one (the daemon) takes the apogee home's roots alone. It is serial:
// it moves the process's working directory.
func TestDriverHostOpenFindsTheProjectRootFromASubfolder(t *testing.T) {
	repo := t.TempDir()
	for _, dir := range []string{".git", ".apogee", filepath.Join("src", "pkg")} {
		if err := os.MkdirAll(filepath.Join(repo, dir), 0o700); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
	}
	sub := filepath.Join(repo, "src", "pkg")
	t.Chdir(sub)
	wantRoot, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	opts := config.Options{ConfigDir: t.TempDir()}

	t.Run("with a project the roots carry the source's Project root", func(t *testing.T) {
		h, err := openDriverHost(opts, true, hostConfiner(fenceableHost))
		if err != nil {
			t.Fatalf("openDriverHost: %v", err)
		}
		t.Cleanup(func() { h.close() })

		if h.roots.project != wantRoot {
			t.Errorf("roots.project = %q, want the repository top %q", h.roots.project, wantRoot)
		}
		if got := h.files.ProjectRoot(); got != h.roots.project {
			t.Errorf("files.ProjectRoot() = %q, want the roots' own %q", got, h.roots.project)
		}
		if gotWS, _ := filepath.EvalSymlinks(h.roots.workspace); gotWS != filepath.Join(wantRoot, "src", "pkg") {
			t.Errorf("roots.workspace = %q, want the working directory %q", h.roots.workspace, sub)
		}
	})

	t.Run("without a project the roots carry none", func(t *testing.T) {
		h, err := openDriverHost(opts, false, hostConfiner(fenceableHost))
		if err != nil {
			t.Fatalf("openDriverHost: %v", err)
		}
		t.Cleanup(func() { h.close() })

		if h.roots.project != "" {
			t.Errorf("roots.project = %q, want none — the daemon's Project root is each Firing's", h.roots.project)
		}
		if !h.files.IsZero() {
			t.Error("the host recorded a config source; a Driver opened without a project has none")
		}
	})
}

// Opening the host sweeps the scratch dirs earlier runs left behind and spares a fresh one: the
// sweep every Driver used to run for itself now runs once, here.
func TestDriverHostOpenSweepsStaleScratchDirs(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	roots, err := resolveRoots(configDir, "")
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	stale := filepath.Join(roots.scratch, "stale")
	fresh := filepath.Join(roots.scratch, "fresh")
	for _, dir := range []string{stale, fresh} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	aged := time.Now().Add(-scratchMaxAge - time.Hour)
	if err := os.Chtimes(stale, aged, aged); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	h, err := openDriverHost(config.Options{ConfigDir: configDir}, false, hostConfiner(fenceableHost))
	if err != nil {
		t.Fatalf("openDriverHost: %v", err)
	}
	t.Cleanup(func() { h.close() })

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a stale scratch dir survived the host's opening (stat err = %v)", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("a fresh scratch dir was swept: %v", err)
	}
}

// Opening the store runs the snapshot sweep over it: a store past the age rule goes.
func TestDriverHostOpenStoreSweepsStaleSnapshotDirs(t *testing.T) {
	t.Parallel()

	h := testDriverHost(t, config.Options{}, fenceableHost)
	stale := snapshotDirAt(t, h.roots.snapshots, "stale", time.Now().Add(-scratchMaxAge-time.Hour))

	if _, err := h.openStore(nil, nil); err != nil {
		t.Fatalf("openStore: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a stale snapshot store survived openStore (stat err = %v)", err)
	}
}

// The ids the Driver names are spared by the sweeps — the record a --resume start resolved must
// not lose its undo history to the sweep that runs a moment before it opens — and keep is handed
// the very store openStore returns. A keep that fails is the start-up error.
func TestDriverHostOpenStoreKeepsWhatTheDriverNames(t *testing.T) {
	t.Parallel()

	t.Run("the named id survives the orphan rule", func(t *testing.T) {
		t.Parallel()

		h := testDriverHost(t, config.Options{}, fenceableHost)
		orphanAge := time.Now().Add(-snapshotOrphanMaxAge - time.Hour)
		kept := snapshotDirAt(t, h.roots.snapshots, "kept", orphanAge)
		orphan := snapshotDirAt(t, h.roots.snapshots, "orphan", orphanAge)

		var handed *session.Store
		store, err := h.openStore(nil, func(s *session.Store) ([]string, error) {
			handed = s
			return []string{"kept"}, nil
		})
		if err != nil {
			t.Fatalf("openStore: %v", err)
		}

		if handed != store {
			t.Error("keep was handed a store other than the one openStore returned")
		}
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("the store the Driver named was swept: %v", err)
		}
		if _, err := os.Stat(orphan); !os.IsNotExist(err) {
			t.Errorf("a record-less store the Driver did not name survived (stat err = %v)", err)
		}
	})

	t.Run("a failing keep is the start-up error", func(t *testing.T) {
		t.Parallel()

		h := testDriverHost(t, config.Options{}, fenceableHost)
		want := errors.New("no saved sessions for this workspace")

		store, err := h.openStore(nil, func(*session.Store) ([]string, error) { return nil, want })

		if !errors.Is(err, want) || store != nil {
			t.Errorf("openStore = (%v, %v), want (nil, %v)", store, err, want)
		}
	})
}

// With `sessions.share-group:` set over a group-readable config.yaml, the warning goes through the
// Driver's say as a share-group notice; over an owner-only one nothing is said.
func TestDriverHostOpenStoreSaysTheShareWarning(t *testing.T) {
	t.Parallel()

	open := func(t *testing.T, perm os.FileMode) (string, []tui.Notice) {
		t.Helper()
		home := shareTestHome(t, perm)
		roots, err := resolveRoots(home, t.TempDir())
		if err != nil {
			t.Fatalf("resolveRoots: %v", err)
		}
		opts := config.Options{Sessions: config.SessionSettings{ShareGroup: strconv.Itoa(os.Getegid())}}
		h := newDriverHost(roots, opts, hostConfiner(fenceableHost))

		var said []tui.Notice
		if _, err := h.openStore(func(n tui.Notice) { said = append(said, n) }, nil); err != nil {
			t.Fatalf("openStore: %v", err)
		}
		return home, said
	}

	t.Run("a group-readable config is warned of", func(t *testing.T) {
		t.Parallel()

		home, said := open(t, 0o640)

		if len(said) != 1 {
			t.Fatalf("said %d notices, want the share warning alone: %v", len(said), said)
		}
		if said[0].Kind != tui.NoticeShareGroupConfig {
			t.Errorf("notice kind = %v, want the share-group kind", said[0].Kind)
		}
		if path := filepath.Join(home, "config.yaml"); !strings.Contains(said[0].Text, path) {
			t.Errorf("warning = %q, want one naming %s", said[0].Text, path)
		}
	})

	t.Run("an owner-only config is not", func(t *testing.T) {
		t.Parallel()

		if _, said := open(t, 0o600); len(said) != 0 {
			t.Errorf("said %v, want nothing", said)
		}
	})
}

// The recorder follows `server-stats:`: on, it holds the store over the home's stats file; off, it
// holds nothing and the file is never created.
func TestDriverHostStatsFollowsTheServerStatsKey(t *testing.T) {
	t.Parallel()

	t.Run("on", func(t *testing.T) {
		t.Parallel()

		h := testDriverHost(t, config.Options{ServerStats: true}, fenceableHost)

		if h.stats.current() == nil {
			t.Error("server-stats: on opened no store")
		}
		if want := serverStatsPath(h.roots.config); h.stats.path != want {
			t.Errorf("stats path = %q, want %q", h.stats.path, want)
		}
	})

	t.Run("off", func(t *testing.T) {
		t.Parallel()

		h := testDriverHost(t, config.Options{ServerStats: false}, fenceableHost)

		if h.stats.current() != nil {
			t.Error("server-stats: off opened a store")
		}
		if _, err := os.Stat(serverStatsPath(h.roots.config)); !os.IsNotExist(err) {
			t.Errorf("stat the stats file = %v; server-stats: off must neither write nor create it", err)
		}
	})
}

// close stops the recorder, tears the confiner down once and returns the sentence a failed
// teardown owes; a clean teardown, a backend with none and a nil host each owe nothing.
func TestDriverHostCloseStopsStatsAndTearsTheConfinerDown(t *testing.T) {
	t.Parallel()

	t.Run("a failed teardown returns its sentence", func(t *testing.T) {
		t.Parallel()

		failure := errors.New("label revert failed")
		c := &teardownConfiner{fakeConfiner: fakeConfiner{caps: apogee.ConfinementCaps{FSWrite: true}}, err: failure}
		h := testDriverHost(t, config.Options{ServerStats: true}, c)

		got := h.close()

		if want := platform.ConfinementTeardownNotice(failure); got != want || got == "" {
			t.Errorf("close() = %q, want the teardown sentence %q", got, want)
		}
		if c.closed != 1 {
			t.Errorf("the confiner was torn down %d times, want once", c.closed)
		}
		if h.stats.current() != nil {
			t.Error("the stats recorder still holds a store after close")
		}
	})

	t.Run("a clean teardown owes nothing", func(t *testing.T) {
		t.Parallel()

		c := &teardownConfiner{fakeConfiner: fakeConfiner{caps: apogee.ConfinementCaps{FSWrite: true}}}
		h := testDriverHost(t, config.Options{}, c)

		if got := h.close(); got != "" || c.closed != 1 {
			t.Errorf("close() = %q after %d teardowns, want \"\" after one", got, c.closed)
		}
	})

	t.Run("a backend without a teardown owes nothing", func(t *testing.T) {
		t.Parallel()

		if got := testDriverHost(t, config.Options{}, fenceableHost).close(); got != "" {
			t.Errorf("close() = %q, want \"\"", got)
		}
	})

	t.Run("a nil host owes nothing", func(t *testing.T) {
		t.Parallel()

		var h *driverHost
		if got := h.close(); got != "" {
			t.Errorf("close() on a nil host = %q, want \"\"", got)
		}
	})
}

// posture hands each Driver every sentence the backend's posture owes for one mode, as the probe
// words it, and judges none of them: each cell fills exactly its own field.
func TestDriverHostPostureCells(t *testing.T) {
	t.Parallel()

	residualCaps := apogee.ConfinementCaps{FSWrite: true, Residuals: []string{"truncate(2)"}}
	tests := []struct {
		name           string
		caps           apogee.ConfinementCaps
		mode           apogee.Mode
		confine        bool
		wantUnconfined bool
		wantDegraded   bool
		wantResidual   bool
		wantPrewarm    bool
	}{
		{name: "auto unconfined warns", caps: apogee.ConfinementCaps{FSWrite: true}, mode: domain.ModeAuto,
			confine: false, wantUnconfined: true},
		{name: "auto on a host that cannot fence is degraded", caps: apogee.ConfinementCaps{}, mode: domain.ModeAuto,
			confine: true, wantDegraded: true},
		{name: "auto on a fence with a residual discloses it and pre-warms", caps: residualCaps,
			mode: domain.ModeAuto, confine: true, wantResidual: true, wantPrewarm: true},
		{name: "auto on a whole fence only pre-warms", caps: apogee.ConfinementCaps{FSWrite: true},
			mode: domain.ModeAuto, confine: true, wantPrewarm: true},
		{name: "plan owes nothing", caps: apogee.ConfinementCaps{}, mode: domain.ModePlan, confine: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &fakeConfiner{caps: tt.caps}
			h := testDriverHost(t, config.Options{}, c)

			p := h.posture(tt.mode, tt.confine)

			if p.backend != probe.BackendName(c) {
				t.Errorf("backend = %q, want %q", p.backend, probe.BackendName(c))
			}
			if !reflect.DeepEqual(p.caps, tt.caps) {
				t.Errorf("caps = %+v, want %+v", p.caps, tt.caps)
			}
			if (p.unconfined == unconfinedAutoWarning) != tt.wantUnconfined || (p.unconfined != "") != tt.wantUnconfined {
				t.Errorf("unconfined = %q, want set: %v", p.unconfined, tt.wantUnconfined)
			}
			wantDegraded := probe.DegradedNotice(p.backend, tt.caps, tt.mode, tt.confine)
			if p.degraded != wantDegraded || (p.degraded != "") != tt.wantDegraded {
				t.Errorf("degraded = %q, want %q (set: %v)", p.degraded, wantDegraded, tt.wantDegraded)
			}
			wantResidual := probe.ResidualNotice(p.backend, tt.caps, tt.mode, tt.confine)
			if p.residual != wantResidual || (p.residual != "") != tt.wantResidual {
				t.Errorf("residual = %q, want %q (set: %v)", p.residual, wantResidual, tt.wantResidual)
			}
			if p.prewarm != tt.wantPrewarm {
				t.Errorf("prewarm = %v, want %v", p.prewarm, tt.wantPrewarm)
			}
		})
	}
}

// prewarm reaches the label walk through the prewarmLabelWalk seam, with the host's own confiner,
// the workspace it is handed and the Driver's writer. Serial: it swaps a package seam.
func TestDriverHostPrewarmGoesThroughTheSeam(t *testing.T) {
	type call struct {
		confiner  apogee.Confiner
		workspace string
		out       io.Writer
	}
	var calls []call
	prev := prewarmLabelWalk
	prewarmLabelWalk = func(c apogee.Confiner, workspace string, out io.Writer) {
		calls = append(calls, call{confiner: c, workspace: workspace, out: out})
	}
	t.Cleanup(func() { prewarmLabelWalk = prev })

	h := testDriverHost(t, config.Options{}, fenceableHost)
	var out bytes.Buffer

	h.prewarm(h.roots.workspace, &out)

	if len(calls) != 1 {
		t.Fatalf("the pre-warm ran %d times, want once", len(calls))
	}
	if calls[0].confiner != apogee.Confiner(fenceableHost) {
		t.Errorf("the pre-warm walked for %#v, not the host's confiner", calls[0].confiner)
	}
	if calls[0].workspace != h.roots.workspace {
		t.Errorf("workspace = %q, want %q", calls[0].workspace, h.roots.workspace)
	}
	if calls[0].out != &out {
		t.Error("the pre-warm was handed a writer other than the Driver's")
	}
}
