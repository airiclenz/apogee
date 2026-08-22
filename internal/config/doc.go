// Package config owns apogee's on-disk configuration: what a config key may contain, how the
// four sources compose into one resolved value, and how a single key is written back without
// disturbing the file the user hand-edits.
//
// It is a package rather than a cluster of files in the binary because everything here is a fact
// about the CONFIG, not about any one Driver (CONTEXT: Driver): the schema, the precedence rule
// (flag > env > file > default), the key registry the /settings surface reads, the splice writer,
// the one-time legacy fold, and the poller that reports the file changed. A bench harness or a
// daemon resolving the same file has the same needs as the TUI binary, and ADR 0043 moved the
// cluster here so none of them has to reach into cmd/apogee for it.
//
// What deliberately did NOT come along: how a Driver DRAWS the settings surface (cmd/apogee's
// settingsrows.go / settingsedit.go) — the sections rows are grouped under, the mask over a secret,
// the edit affordances, the pane's own vocabulary of kinds. That is the binary's thin-renderer seam
// (ADR 0011), and a package that owns the schema must not also own how one host draws it. What a key
// currently HOLDS, spelled the way the file spells it, is the schema's own knowledge and comes off
// the registry row ([Key.Read] and its two siblings), so no host restates the schema to render it.
//
// Layering (ADR 0010): this package imports internal/domain and its siblings — never the root
// module path. The mode ladder it validates against is [domain.ParseMode]; the model profile it
// resolves is [domain.ModelProfile].
//
// The files, one line each.
//
// config.go is the core: the on-disk schema (fileConfig), the precedence that resolves it onto
// [Options] ([ResolveOptions], one accessor per key), the `servers:` entry a session starts on,
// and the apogee-home path resolution every other file here asks for. options.go is [Options] —
// the Driver's parsed invocation plus every value resolution writes onto it, which is what
// ApplyConfig fills. registry.go is the declarative table describing every schema key exactly
// once — its type, default, env var, flag, validator, /settings visibility, and the projections
// that read its value back out of a resolved [Options] — guarded as a bijection with fileConfig,
// so a key added to the schema breaks the build gate until it is described. defaults.go is the starter config embedded from defaults/config.yaml and seeded on
// first run, plus the seed-if-absent write everything else reuses. configsplice.go is the line and
// node machinery every write into config.yaml shares — read, parse for positions, cut and rejoin
// the text, verify the result against the original, replace the file atomically — which is what
// keeps ONE key writable without moving a comment (ADR 0035). configedit.go is the one transaction
// those pieces are run in — seed and read the file, splice, re-parse, verify, replace it
// atomically — together with the per-container shape predicates a writer's verify step is written
// from. configwrite.go is the
// acknowledgement writer that records a host `/confine off --save` names, and the per-entry writer
// that remembers a choice on a single `servers:` entry. configwrite_scalar.go sets or resets one
// /settings key, addressed by its registry path, and configwrite_scalarsplice.go is that writer's
// splice machinery — where the key stands in the parsed document, how a text key's block is
// rendered, and where a key the file does not set yet is inserted.
// configwrite_keysource.go points one `servers:`
// entry at a key command, or marks the entry as keeping the plaintext key it already carries
// (ADR 0047). configwrite_mechanism.go writes one catalogued Mechanism's line into the
// `mechanisms:` block — the one writer addressed by catalogue id rather than by registry path,
// since that block's children are the Mechanism catalogue's and not the schema's (ADR 0016).
// keyresolve.go turns a `servers:` entry's KEY SOURCE — a
// literal key, a command whose output is the key, or the name of an environment variable — into the
// token a seam sends, running it at first use and caching the answer for the session.
// configmigrate.go is the one-time fold of the retired
// top-level upstream keys into `servers:` (ADR 0036 decision 9). The one-goroutine poller that
// reports config.yaml changed, whoever changed it (ADR 0041), is deliberately NOT here: it knows
// nothing about YAML or this schema, and it has a second caller in the daemon's schedules watch, so
// it lives in internal/filewatch.
//
// And doc.go this map.
package config
