---
title: TestFilePassRefusesThroughTheRows pins cursor-shape/sub-agents-choice refusals by prefix only
status: done
type: task
priority: 3
aliases: [apogee-ah5]
created: 2026-09-16T11:15:11Z
closed: 2026-09-16T21:07:12Z
reason: "test(config): the file pass pins every refusal row's full sentence"
---

Deferred out of plan 2026-09-16 - 00 (item 13). internal/config/config_test.go:2228 matches the startup refusal with strings.HasPrefix, so for the cursor-shape and sub-agents-choice cases (config_test.go:2216-2217) the domain tail of the sentence is not pinned at the file pass; only the delegate-timeout case pins its full sentence. Pin the two full sentences.
