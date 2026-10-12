---
title: TestE2EDefaultWireStaysChatCompletions fails about 1 in 3 on windows/arm64
status: open
type: bug
priority: 2
created: 2026-10-12T21:51:20Z
---

`TestE2EDefaultWireStaysChatCompletions` (`cmd/apogee/e2e_wire_anthropic_test.go`) fails about 1 in 3 runs on the owner's windows/arm64 box. It drives the loop through `driveWireLoop` with no wire named and asserts every request reached `/v1/chat/completions` and none `/v1/messages`. No CI runner is windows/arm64, so the failure has never been captured in a log the census could read, and its cause — load, timing, or a real defect on that platform — is unknown.

**Acceptance:** the failure is reproduced and its output captured on windows/arm64, the cause named, and the test runs green repeatedly there (judge by N consecutive green runs, not by a fix landing).

Source: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md` §4, "These were left open and never filed or done"; lever 11 of §8.
