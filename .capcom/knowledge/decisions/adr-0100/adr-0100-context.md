---
title: ADR 0100 context
---

Phase 2 (June 2026) resolved apogee's state roots — config, the skill library, sessions — in
the composition root (`cmd/apogee`, `roots.config/library/sessions` →
`domain.Config.{ConfigDir,LibraryDir,SessionsDir}`). Each OS has its own convention for where
such state goes: XDG directories on Linux, `~/Library/Application Support` on macOS,
`%AppData%` on Windows, which Go exposes as `os.UserConfigDir`. Splitting state that way puts
apogee's files in a different place on every machine, and on Linux spreads them over several
directories.

The owner settled the question during phase 2 (P2.5, 2026-06-23), and AGENTS.md carried the
ruling as a standing convention from 2026-07-28 until this record took it over (2026-10-14).
