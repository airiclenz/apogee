---
title: "Per-server request-extra: body passthrough (pin OpenRouter provider.order / allow_fallbacks, prompt_cache_key / cache_control)"
status: done
type: feature
priority: 3
plan: 2026-09-23 - 00 - upstream-passthrough-and-measurement-plan
aliases: [apogee-glh]
created: 2026-09-20T14:54:23Z
closed: 2026-09-24T04:54:16Z
reason: "Resolved by plan 'upstream-passthrough-and-measurement' (archived at 0a3aba89): per-server request-extra RFC 7396 body passthrough on every Client built for an entry (items 2-4). Raced run of items 2-11 still pending on a host with ThreadSanitizer support."
---

Offered 2026-09-20 from the upstream-stall session (openrouter-ds4, provider Relace, every call 3-8 min): a user cannot pin an OpenRouter provider or send cache hints because ServerEntry (internal/config/config.go ~L1906) has no free-form request body field and apogee sends no cache hints (it only reads cached_tokens). Shape: a per-server 'request-extra:' map[string]any merged over the encoded body in each codec's encode (openaiCodec.encode / anthropicCodec.encode build a typed struct — needs marshal-then-overlay), carried by a provider.Option from the entry. Deliberately out of scope of the apogee-60x plan (2026-09-20 - 04); owner call 2026-09-20: file, keep parked.
