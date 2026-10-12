---
title: "Advise lane: the file-changed narrowing lives only in adviseArgv, so a Go-handler advise reaction on file-changed would fire unnarrowed"
status: done
type: bug
priority: 3
labels: [reactions, stage-3]
aliases: [apogee-0n6]
created: 2026-09-12T20:07:01Z
closed: 2026-09-12T20:45:27Z
reason: fixed in 7c5e213f
---

Deferred out of the stage-3 run (plan 2026-09-09 - 01, item 6). `subscribes` (internal/agent/reactions.go:330) widens `file-changed` to `post-tool-result` for EVERY class-advise reaction, but the narrowing to successful workspace writes lives only in the argv path — `adviseArgv` (internal/agent/reactions.go:493, the `writtenPath` check at ~:544). A future Go-handler advise reaction listing `on: [file-changed]` would therefore fire on every tool call unnarrowed. Latent: no such reaction exists today (the only advise reactions are user argv entries). Fix is to move the narrowing beside the widening (or guard/validate that a Go-handler advise reaction cannot list file-changed) and pin it with a test.
