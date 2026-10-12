---
title: "internal/agent: three request-capturing fakes (recordingResponder, capturingResponder, captureAllResponder) survive item 13 though the stubllm log now carries Sampling and Effort"
status: done
type: task
priority: 3
aliases: [apogee-ifx]
created: 2026-09-16T15:46:33Z
closed: 2026-09-16T16:12:46Z
reason: af9ffd19
---

Deferred out of plan 2026-09-16 - 01 (item 13, engine-tools-and-provider-deepening). The stubllm request log now records Request.Sampling and Request.Effort, but three pure Delta fakes remain whose only reason to exist is asserting those fields off provider.Request before the client shaped it: recordingResponder (internal/agent/harness_test.go:125, users agent_test.go:146 and :170 — effort dialect / thinking effort), capturingResponder (internal/agent/hookmutation_test.go:23, users budget_test.go:146 and :186 — Sampling), captureAllResponder (internal/agent/retryexchange_test.go:25, user rebind_test.go:368 — Sampling / reply ceiling). Item 14 did not retire them and no item 12–17 owned those files. Migrate the five users onto a stubllm Script read through the request log, then delete the three fakes. Their doc comments were reworded at closeout (commit 934b5db1) to say the log carries the fields; the fakes themselves are the open work.
