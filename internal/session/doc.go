// Package session persists and reloads Agent snapshots — the same primitive the bench
// composes into forking and counterfactuals, which Apogee itself does not expose
// (ADR 0001). Snapshots are copyable values with a versioned schema (internal/domain).
//
// The Store writes snapshots to a directory (SessionsDir) under sortable timestamp
// filenames and owns the on-disk format, so callers never duplicate that knowledge.
// Decoding a snapshot back into a Session is domain.DecodeSession (the read path the
// binary's --resume uses directly).
package session
