---
title: "internal/tools: TestConsoleClose_ReturnsTheUnreadTailAndTheExitCode flakes under shard load"
status: done
type: bug
priority: 2
aliases: [apogee-bng]
created: 2026-09-14T05:36:43Z
closed: 2026-09-14T05:44:50Z
reason: fixed in b52dcebe
---

During a make check (the sharded suite, scripts/test-shards.sh, several packages running beside each other under -race) internal/tools TestConsoleClose_ReturnsTheUnreadTailAndTheExitCode failed once with 'console_close_test.go:56: result = "exited with code 3", want the output nobody had read yet'; it passes 5/5 in isolation and the package had not been touched — a load-sensitive test.

Reproduction (2026-09-14): go test -race -count=20..60 -run TestConsoleClose_ReturnsTheUnreadTailAndTheExitCode ./internal/tools/ while go test -race -count=1 ./cmd/apogee/ runs beside it fails 0-2 of 20 (1 of 60 in the bite check), same message; seen once at count=20 in isolation too.

Root cause: a TEST race, not a product one. The test helper openUndrainedConsole (console_read_test.go) opened the Console with wait_ms=-1 believing that meant 'no collection at all', but consoleWait maps a negative wait to a ZERO window, and console_open's collectConsoleWindow(0) still does one Read(0) that drains whatever the program has already printed. Under -race and host load the parent goroutine is descheduled between pty start and that read while the child shell (a separate process) runs ahead and prints 'tail-bytes', so the open call consumed the very tail the close was supposed to return; close then reported only 'exited with code 3'. The product contract holds (Close returns everything unread AFTER the join with the reader: process.Close joins readerDone before the tail is taken).

Fix: openUndrainedConsole now prefixes the command with 'read release; ' so the shell blocks on its terminal until the open call has returned, then types the newline that releases it — nothing the command prints can predate the open, by construction. Same helper serves TestConsoleRead's dropped-bytes test. consoleWait's comment sharpened ('waits for nothing; still reports what is already buffered'). Bite check: old helper fails 1/60 under the load reproduction; fixed helper 0/60 + 0/20 under the same load.
