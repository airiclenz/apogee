---
title: Console tool tests fail TempDir cleanup on Windows
status: done
type: bug
priority: 3
aliases: [apogee-windows-console-tempdir-cleanup]
created: 2026-10-04T21:26:03Z
closed: 2026-10-07T08:14:58Z
reason: "Fixed in e4261665: consoleTestCtx claims t.TempDir before registering registry.CloseAll, so the TempDir parent's RemoveAll runs after the Consoles exit (LIFO); fixes all callers. Console tests green on windows/arm64."
---

On Windows, three internal/tools console tests fail with a t.TempDir RemoveAll 'file in use' cleanup error, though their assertions pass: TestConsoleSend_UnconfinedConsoleIsDemotedWhereAConsoleCannotBeConfined, TestConsoleOpen_ScrubsCredentials and TestConsoleOpen_StampsTheEngineMintedOwnerKey. Cause: each test registers its TempDir after the registry's CloseAll cleanup, and cleanups run LIFO, so the dir is removed while cmd.exe still runs inside it. Pre-existing - fails identically at a7695e00, before the windows-console-confinement plan. Fix: create the TempDir before the console registry/test context (as console_windows_test.go and console_open_windows_test.go already do). Found during the 2026-10-04 windows-console-confinement plan run (archived at d5012578).
