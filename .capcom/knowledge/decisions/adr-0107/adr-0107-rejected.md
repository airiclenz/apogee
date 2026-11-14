---
title: ADR 0107 considered options
---

- **v1.9.0 as the first stable release.** It outranks v1.8.0 and carries its own list, so it
  needs no placeholder, and the rehearsal showed it resolving normally. But the first stable
  release would then not be called v1.0.0, and the number would point at a v1.0–v1.8 history
  that users never saw and that is not part of the stability promise. The placeholder costs one
  tag and keeps the number that says what the release is.
- **A `/v2` module path.** Starting the stable line at `github.com/airiclenz/apogee/v2` escapes
  the burned v1.x history entirely, but it changes the import path of every importer — the bench
  (`apogee-sim`) imports apogee as a Go module (ADR 0001) — and the first stable release would
  be called v2.0.0, a second major version that never had a first. The v1 line would stay
  unusable for good.
- **A vanity import path.** A new module path on a domain of the project's own (a `go-import`
  meta tag) gets a clean version history, but it also changes every import path, ties the
  module's resolvability to a domain and a page that must be kept up forever, and leaves the
  GitHub path, with the same burned history, in every proxy and checksum database anyway.
