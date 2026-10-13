---
title: TestE2EDefaultWireStaysChatCompletions fails about 1 in 3 on windows/arm64
status: done
type: bug
priority: 2
created: 2026-10-12T21:51:20Z
closed: 2026-10-13T17:36:02Z
reason: "Confirmed fixed on the owner's windows/arm64 box: 10 consecutive green runs (go test -count=10 -failfast -run '^TestE2EDefaultWireStaysChatCompletions$' ./cmd/apogee/, 2026-10-13). Fix: 1afb2250, the Windows sharing-violation retry on the session record rename (open-bugs-sweep plan item 4)."
---

`TestE2EDefaultWireStaysChatCompletions` (`cmd/apogee/e2e_wire_anthropic_test.go`) fails about 1 in 3 runs on the owner's windows/arm64 box. It drives the loop through `driveWireLoop` with no wire named and asserts every request reached `/v1/chat/completions` and none `/v1/messages`. No CI runner is windows/arm64, so the failure has never been captured in a log the census could read, and its cause — load, timing, or a real defect on that platform — is unknown.

**Acceptance:** the failure is reproduced and its output captured on windows/arm64, the cause named, and the test runs green repeatedly there (judge by N consecutive green runs, not by a fix landing).

Source: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md` §4, "These were left open and never filed or done"; lever 11 of §8.


## Progress (2026-10-13)

Advanced by `docs/plans/archived/2026-10-13 - 00 - open-bugs-sweep-plan.md`; left open for the owner's confirmation run.

- **Likely cause, fixed (item 4):** the test's `savedTranscript` poll opens the session record while the app's `atomicWrite` renames over it; on Windows a reader without FILE_SHARE_DELETE makes that rename fail with a sharing violation or access-denied. `internal/session/rename.go` now retries the rename on those two errors for ~2 s (`rename_windows.go` wires the predicate; `rename_other.go` is plain `os.Rename`).
- **Diagnostics (item 5):** a failing `driveWireLoop` now logs every stub request, probe, unmatched and stray request (`stubllm` `Strays()`), the live frame, the app output, the session store's file names, `GOOS/GOARCH` and `testtime.Scale()` — so a further failure explains itself.

**Next:** run `TestE2EDefaultWireStaysChatCompletions` repeatedly on windows/arm64; close on N consecutive greens, or read the dump from the next failure.
