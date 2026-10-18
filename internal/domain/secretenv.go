package domain

import (
	"slices"
	"strings"
	"sync"
)

// SecretEnv is the credential scrub as a LIVE list: the environment variable names the execution
// tools drop from a subprocess's environment and the sync Reaction lane redacts out of an answer,
// read at each call rather than captured at construction. It is Config.SecretEnvVars in the form a
// running session can move — a Driver whose configuration changes under it (a Project config
// entry adopted mid-session that names a `headers-env:` token, ADR 0104 §12) widens it, and the
// next `terminal` child, the next sync `run:` and the next redaction read the wider list with no
// re-wiring.
//
// It only ever widens. A name a later re-read stops naming stays dropped for the rest of the
// session: a narrower scrub is the one direction that can hand a child a token, a retired
// Reaction generation may still be draining with the old entry's variables in hand, and the
// startup list carries names a re-read of the files alone cannot reproduce (the resolved key
// source of the entry the session started on). An extra name costs a child one variable it was
// never meant to rely on.
//
// The zero value and nil both hold no names. It is safe for concurrent use: Names hands back a
// slice the holder never writes into again (Widen replaces it whole).
type SecretEnv struct {
	mu    sync.RWMutex
	names []string
}

// NewSecretEnv is a holder seeded with names (copied), in their order.
func NewSecretEnv(names []string) *SecretEnv {
	return &SecretEnv{names: slices.Clone(names)}
}

// Names is the list in force now; nil for a nil or empty holder. Callers must not modify it.
func (s *SecretEnv) Names() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.names
}

// Widen adds each of names the holder does not hold yet, after the names it already holds and in
// the order given. Names are compared case-insensitively — the scrub's own comparison, one Windows
// variable in either spelling — and a blank one is skipped. It never removes a name (see the type).
// A nil holder ignores the call.
func (s *SecretEnv) Widen(names []string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	held := make(map[string]bool, len(s.names)+len(names))
	for _, name := range s.names {
		held[strings.ToUpper(name)] = true
	}
	next := slices.Clone(s.names)
	for _, name := range names {
		folded := strings.ToUpper(strings.TrimSpace(name))
		if folded == "" || held[folded] {
			continue
		}
		held[folded] = true
		next = append(next, strings.TrimSpace(name))
	}
	s.names = next
}

// SecretEnvNames is the credential scrub in force for c now: the live holder's list when
// c.SecretEnv is set, c.SecretEnvVars otherwise. The sync Reaction lane reads it at each spawn and
// each redaction, as the default execution tools read the holder at each call.
func (c *Config) SecretEnvNames() []string {
	if c.SecretEnv != nil {
		return c.SecretEnv.Names()
	}
	return c.SecretEnvVars
}
