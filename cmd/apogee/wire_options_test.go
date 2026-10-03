package main

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
)

// The configured `currency:` label reaches the renderer through the root's options, so every priced
// amount the TUI prints names the currency the human chose (ADR 0093 decision 1) rather than one the
// renderer made up.
func TestRunRootWiresTheConfiguredCurrency(t *testing.T) {
	t.Parallel()

	opts := config.Options{
		Endpoint:     "http://127.0.0.1:1111",
		Model:        "fake",
		StartupEntry: config.ServerEntry{Endpoint: "http://127.0.0.1:1111", Model: "fake"},
		Mode:         "ask-before",
		Workspace:    t.TempDir(),
		ConfigDir:    t.TempDir(),
		Currency:     "EUR",
	}
	roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	w := newRootWiring(opts, apogee.ModeAskBefore, roots)
	t.Cleanup(w.close)
	if err := w.resolveConfig(); err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	if err := w.wireSession(context.Background()); err != nil {
		t.Fatalf("wireSession: %v", err)
	}

	if got, want := w.options().Currency, opts.Currency; got != want {
		t.Errorf("options().Currency = %q, want the configured %q", got, want)
	}
}
