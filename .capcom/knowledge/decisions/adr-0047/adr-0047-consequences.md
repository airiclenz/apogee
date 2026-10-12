---
title: ADR 0047 consequences
---

- **A key source is a documented config surface, and the seeded template teaches it.** The
  `servers:` comment block carries all three sources as commented examples, the exactly-one rule, and
  the GUI-prompt note an interactive backend needs — a template test pins that.
- **A new seam that needs a server's key calls the run's resolver.** Reading `entry.APIKey` directly
  is now a bug that presents as "my command-source server has no key": the composition root holds one
  `KeyResolver` and every seam — bind, switch, delegation target, heartbeat, `probe`, `probe model`,
  `headless` — goes through it.
- **`probe`'s `APIKeyConfigured` means "a key source is configured"**, not "a literal key is
  present"; its doc comment says so.
- **A first use can now block on a human.** A `/server` switch onto a keychain-backed entry may put a
  GUI unlock in front of the user for up to a minute. That cost is paid once per entry per session
  (point 3), and it is why the offer's product is a *command* the user recognises rather than a
  hidden lookup.
- **The store is the user's.** Apogee upserts one item per migrated entry and never deletes one:
  removing or renaming a `servers:` entry leaves its store item alone, because a secret the user may
  have started depending on elsewhere is not apogee's to garbage-collect.
- **Realisations.** `ServerEntry`'s three key fields and `ValidateServers`' refusals
  (`internal/config/config.go`); the resolver (`internal/config/keyresolve.go`); the entry-scoped
  rewrite and the marker (`internal/config/configwrite.go`); the store probe, write and `ReadCmd`
  (`internal/keystore/`, one concern one package per
  ADR 0043); the move itself — write,
  verify, rewrite — assembled in the composition root (`cmd/apogee/keymigrate.go`), with the offer
  pane in `internal/tui/keymigration.go` and the storeless/headless notice beside the confinement
  warnings on stderr.
