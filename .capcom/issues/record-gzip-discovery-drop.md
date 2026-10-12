---
title: stubllm record drops a gzip-encoded discovery reply
status: done
type: bug
priority: 3
plan: 2026-10-08 - 01 - firing-window-and-silent-drops-plan
aliases: [apogee-record-gzip-discovery-drop]
created: 2026-10-08T12:00:16Z
closed: 2026-10-08T14:51:32Z
reason: "Resolved by plan '2026-10-08 - 01 - firing-window-and-silent-drops-plan' (item 6; archived at ec0827f2): recorder drops the client's Accept-Encoding so its transport decodes the reply; gzip upstream test added."
---

Seen 2026-10-08 recording OpenRouter (apogee -> stubllm record -> proxy -> https://openrouter.ai/api): the fixture got no discovery block, although /v1/models answered 200 with 467 models. The raw reply bytes start 0x1f 0x8b, i.e. gzip. Likely path: the client's Go transport adds Accept-Encoding: gzip implicitly; the recorder's httputil.ReverseProxy forwards that header, so the outbound transport does not decompress, and the upstream's gzip body reaches Recorder.capture as-is. fileProbe (internal/stubllm/record.go ~261-284) then fails json.Unmarshal on the compressed bytes and drops the reply silently ('A reply that does not decode is dropped rather than guessed at'). The client still decodes fine because its own transport decompresses. A streamed completion is affected the same way if an upstream ever gzips SSE (OpenRouter did not). Inferred from the bytes and the missing block, not yet confirmed in a test. Fix direction: strip Accept-Encoding on the outbound request in the recorder's Rewrite, or decode per Content-Encoding in capture; add a gzip upstream case to record_test.go.
