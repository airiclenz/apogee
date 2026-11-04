---
title: web-search-endpoint is never refused at startup, despite the manual
status: open
type: bug
priority: 2
plan: 2026-11-04 - 00 - config-refusal-parity-plan.md
created: 2026-11-04T20:46:42Z
---

Seqcom docs-a5-v0310 (apogee v0.31.0). The row is a `stringField`, so `validateSearchEndpoint` runs only at /settings, and it passes anything `url.Parse` survives. `x y z`, `://`, `http://[` start fine and fail at call time with url-safety errors. docs/manual/configuration.md says unparseable text is refused at startup.

## Why

A config the manual says is refused loads and fails later, out of sight.

## Acceptance

- A value with no http(s) host after the https:// heal is refused at load (startup, live re-read, probe config), naming the key
- Blank text means unset; off/none/disabled still disable the tool
