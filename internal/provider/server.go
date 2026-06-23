package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	healthCheckTimeout    = 2 * time.Second
	defaultStartupTimeout = 30 * time.Second
	healthPollInterval    = 500 * time.Millisecond
	gracefulShutdown      = 5 * time.Second
)

// LaunchSpec describes how to start a local Upstream process. Env entries (KEY=VALUE) are
// appended to the inherited environment; StartupTimeout 0 ⇒ the default.
type LaunchSpec struct {
	Command        string
	Args           []string
	Env            []string
	Dir            string
	StartupTimeout time.Duration
}

// ServerManager detects and optionally launches a local OpenAI-compatible Upstream,
// waiting until it answers a health probe before reporting it ready and terminating any
// process it started on Stop. It is the embeddable-core slice of the TS oracle's
// server-process manager: PID-file persistence and cross-restart orphan adoption are a
// VS Code lifecycle concern and are deliberately omitted — the bench owns server
// lifecycle and only needs detect / launch-and-wait / stop. Not safe for concurrent use.
type ServerManager struct {
	baseURL      string
	healthPath   string
	httpClient   *http.Client
	pollInterval time.Duration // health-poll cadence while waiting for a spawned server
	cmd          *exec.Cmd     // non-nil only for a process this manager spawned (not an adopted one)
}

// ServerOption configures a ServerManager.
type ServerOption func(*ServerManager)

// WithHealthPath overrides the health-probe path (default "/v1/models").
func WithHealthPath(path string) ServerOption {
	return func(m *ServerManager) { m.healthPath = path }
}

// WithServerHTTPClient injects the *http.Client used for health probes.
func WithServerHTTPClient(h *http.Client) ServerOption {
	return func(m *ServerManager) { m.httpClient = h }
}

// NewServerManager builds a manager for the Upstream at baseURL.
func NewServerManager(baseURL string, opts ...ServerOption) *ServerManager {
	m := &ServerManager{
		baseURL:      strings.TrimRight(baseURL, "/"),
		healthPath:   modelsPath,
		httpClient:   &http.Client{},
		pollInterval: healthPollInterval,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Reachable reports whether the Upstream answers a health probe within a short timeout.
// Any transport error or non-2xx status reads as unreachable.
func (m *ServerManager) Reachable(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+m.healthPath, nil)
	if err != nil {
		return false
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body) // drain for connection reuse
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// Start ensures the Upstream is up. If it already answers, Start adopts it without
// spawning (and Stop will leave it running). Otherwise it launches spec.Command and polls
// until the server is healthy or the startup timeout elapses; on timeout it reaps the
// process and returns an error.
func (m *ServerManager) Start(ctx context.Context, spec LaunchSpec) error {
	if spec.Command == "" {
		return errors.New("apogee: ServerManager.Start: spec.Command is required")
	}
	if m.Reachable(ctx) {
		return nil // already up — adopt without spawning
	}

	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.Dir = spec.Dir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("apogee: start upstream: %w", err)
	}
	m.cmd = cmd

	timeout := spec.StartupTimeout
	if timeout == 0 {
		timeout = defaultStartupTimeout
	}
	if m.waitHealthy(ctx, timeout) {
		return nil
	}

	_ = m.Stop(context.Background()) // reap the process that never became healthy
	return fmt.Errorf("apogee: upstream did not become healthy within %s", timeout)
}

// Stop terminates a process this manager spawned: a best-effort graceful interrupt, then
// a hard kill if it has not exited within the grace period. An adopted (already-running)
// Upstream is left untouched. Safe to call when nothing was spawned.
func (m *ServerManager) Stop(ctx context.Context) error {
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	proc := m.cmd.Process

	exited := make(chan struct{})
	go func() {
		_ = m.cmd.Wait()
		close(exited)
	}()

	_ = proc.Signal(os.Interrupt) // best-effort graceful stop (unsupported on some platforms)

	timer := time.NewTimer(gracefulShutdown)
	defer timer.Stop()
	select {
	case <-exited:
		m.cmd = nil
		return nil
	case <-timer.C:
	case <-ctx.Done():
	}

	_ = proc.Kill()
	<-exited
	m.cmd = nil
	return nil
}

// waitHealthy polls Reachable until the Upstream answers or the timeout/context elapses.
func (m *ServerManager) waitHealthy(ctx context.Context, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if m.Reachable(ctx) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		timer := time.NewTimer(m.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}
