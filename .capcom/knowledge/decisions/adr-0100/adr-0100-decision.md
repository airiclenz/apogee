---
title: ADR 0100 decision
---

1. **One dotdir, `~/.apogee`, on every OS.** Config (`~/.apogee/config.yaml`), the skill
   library, sessions, scratch dirs and every other piece of global state live below it. The
   layout is the same on Linux, macOS and Windows.
2. **`--config` overrides the root.** It and the `APOGEE_CONFIG` environment variable (which
   `--config` beats) are the two ways to move the home, which tests and the e2e harness rely on.
3. **The Project config is a layer, not a second home.** `<Project root>/.apogee/config.yaml`
   (committable) layers over the global file, and its granting entries are live only by
   Adoption (ADR 0096).

   > **Amended 2026-10-17 (ADR 0103 D7).** A home named under `sessions.watch-homes` is a
   > read-only source of sessions to watch, never a second home. Nothing in it is swept, written,
   > held or configured from, and its sessions are never resumable.

4. **This is settled.** A design or review that proposes XDG or `os.UserConfigDir` re-opens
   this record and must supersede it explicitly.
