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
// What deliberately did NOT come along: the /settings display projection (cmd/apogee's
// settingsrows.go / settingsedit.go). Those render and edit; they are the binary's thin-renderer
// seam (ADR 0011), and a package that owns the schema must not also own how one host draws it.
//
// Layering (ADR 0010): this package imports internal/domain and its siblings — never the root
// module path. The mode ladder it validates against is [domain.ParseMode]; the model profile it
// resolves is [domain.ModelProfile].
//
// The files, one line each.
//
// config.go is the core: the resolved [Settings], the on-disk schema (fileConfig), the
// per-source [Layer] and the precedence between them, the `servers:` entry a session starts on,
// and the apogee-home path resolution every other file here asks for. options.go is [Options] —
// the Driver's parsed invocation plus every value resolution writes back onto it, which is what
// ApplyConfig fills. registry.go is the declarative table describing every schema key exactly
// once — its type, default, env var, flag, validator and /settings visibility — guarded as a
// bijection with fileConfig, so a key added to the schema breaks the build gate until it is
// described. defaults.go is the starter config embedded from defaults/config.yaml and seeded on
// first run, plus the seed-if-absent write everything else reuses. configwrite.go is the textual
// splice writer that persists ONE key — or one host acknowledgement — into the user's file
// without moving a comment (ADR 0035). configmigrate.go is the one-time fold of the retired
// top-level upstream keys into `servers:` (ADR 0036 decision 9). configwatch.go is the
// one-goroutine poller that reports config.yaml changed, whoever changed it (ADR 0041).
//
// And doc.go this map.
package config
