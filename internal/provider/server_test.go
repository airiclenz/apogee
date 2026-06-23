package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"sync/atomic"
	"testing"
	"time"
)

// TestHelperBlock is not a real test: re-executed as a child process by the spawn tests
// (the canonical os/exec helper-process pattern), it stands in for a launched Upstream —
// it blocks until interrupted/killed so ServerManager.Stop has a live process to reap.
func TestHelperBlock(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	os.Exit(0)
}

func helperSpec(timeout time.Duration) LaunchSpec {
	return LaunchSpec{
		Command:        os.Args[0],
		Args:           []string{"-test.run=TestHelperBlock"},
		Env:            []string{"GO_WANT_HELPER_PROCESS=1"},
		StartupTimeout: timeout,
	}
}

func TestServerManager_Reachable(t *testing.T) {
	t.Parallel()

	t.Run("true when healthy", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		defer srv.Close()
		if !NewServerManager(srv.URL).Reachable(context.Background()) {
			t.Error("Reachable = false, want true for a 200 server")
		}
	})

	t.Run("false on 5xx", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		if NewServerManager(srv.URL).Reachable(context.Background()) {
			t.Error("Reachable = true, want false for a 500 server")
		}
	})

	t.Run("false when down", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close() // nothing is listening now
		if NewServerManager(url).Reachable(context.Background()) {
			t.Error("Reachable = true, want false for a closed server")
		}
	})
}

func TestServerManager_StartAdoptsRunningServer(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	mgr := NewServerManager(srv.URL)
	// Command is intentionally bogus: if Start tried to spawn it the call would fail, so a
	// nil return proves the already-running server was adopted without spawning.
	err := mgr.Start(context.Background(), LaunchSpec{Command: "/nonexistent/apogee-upstream-xyz"})
	if err != nil {
		t.Fatalf("Start adopting a running server: %v", err)
	}
	if mgr.cmd != nil {
		t.Error("Start spawned a process though the server was already up")
	}
	if err := mgr.Stop(context.Background()); err != nil {
		t.Errorf("Stop after adopt: %v", err)
	}
}

func TestServerManager_StartSpawnsThenStops(t *testing.T) {
	t.Parallel()

	var healthy atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	mgr := NewServerManager(srv.URL)
	mgr.pollInterval = 10 * time.Millisecond

	// The "spawned server" becomes healthy shortly after launch.
	go func() {
		time.Sleep(50 * time.Millisecond)
		healthy.Store(true)
	}()

	if err := mgr.Start(context.Background(), helperSpec(5*time.Second)); err != nil {
		t.Fatalf("Start spawning helper: %v", err)
	}
	if mgr.cmd == nil {
		t.Fatal("Start did not record a spawned process")
	}

	if err := mgr.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if mgr.cmd != nil {
		t.Error("Stop did not clear the spawned process")
	}
}

func TestServerManager_StartTimesOutAndReaps(t *testing.T) {
	t.Parallel()

	// Server never becomes healthy, so the spawned process must be reaped on timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	mgr := NewServerManager(srv.URL)
	mgr.pollInterval = 10 * time.Millisecond

	err := mgr.Start(context.Background(), helperSpec(60*time.Millisecond))
	if err == nil {
		t.Fatal("Start succeeded though the server never became healthy")
	}
	if mgr.cmd != nil {
		t.Error("Start did not reap the process after the startup timeout")
	}
}
