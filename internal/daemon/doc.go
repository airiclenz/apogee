// Package daemon owns the declarative file behind `apogee daemon`: what
// `~/.apogee/daemon/schedules.yaml` may say, what every entry in it means, and which of those
// entries this host can actually run (ADR 0034, ADR 0055).
//
// It is a package rather than a cluster of files in the binary for the reason internal/config is
// one (ADR 0043): everything here is a fact about the FILE, not about the process that reads it.
// The daemon subcommand is its first caller, but a `daemon check` on someone's laptop, a test, or
// a future surface that lints the file before writing it all need the same answer to "is this a
// file the daemon would adopt, and what does it say".
//
// # Pure by construction
//
// Nothing here starts a clock, opens a socket, reads config.yaml or asks the host what it can do.
// The facts validation cannot derive from the file — which `servers:` entries exist, and what this
// host can fence ([HostConfinement]) — arrive as [Host], injected by the caller that already holds
// them. The Auto-eligibility VERDICT is not among them: this package asks
// [probe.AutoUnattendedBlocked] for it with those facts, so a Firing cannot be refused on a
// different ladder than a headless run (ADR 0033 decision 3). That keeps every rule testable
// without a config file, a launcher or a kernel, and it keeps the file's meaning in one place while
// the hosts that supply those facts vary. The one exception is deliberate: a `workspace:` is checked against the real filesystem,
// because a Firing that runs somewhere that does not exist is the defect this file most wants to
// catch before the daemon adopts it, not hours later in a saved record.
//
// # All defects, never the first
//
// [Load] reports EVERY defect it finds, joined into one error, on internal/config's
// ValidateServers reasoning: a defect in a hand-edited file outlives the day it was written, and
// a validator that stops at the first one turns a five-minute fix into five rounds of edit-and-
// rerun. The daemon's reload rule is all-or-nothing for the same reason (ADR 0034): one bad entry
// rejects the whole file with its reasons logged, and the previously adopted set keeps running.
//
// # Reload by name
//
// An edited file is adopted by DIFFING it against the running set, matching entries by their
// `name:` (ADR 0034): an entry whose spec is untouched is left strictly alone, so its cycle keeps
// its phase and the schedule fires when it was always going to. Only what actually changed is
// stopped and started. That is why the name is required and unique — it is the identity a reload
// recognises a schedule by across an edit, and a stop-everything-and-re-add reload would re-phase
// every neighbour of the one line someone fixed.
//
// The files, one line each.
//
// file.go is the file model ([File], [Entry], [Trigger], [Action]), the strict YAML parse, and
// the validation that applies the schema's defaults and names every defect.
//
// diff.go is the reload: [Diff], which decides by name what an edit changes; [Apply], which enacts
// that decision on a [Scheduler] and keeps the daemon's name→id map; and [Entry.Spec], the mapping
// onto the scheduler library's half of an entry.
package daemon
