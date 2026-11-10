---
title: Harmony stripper leaks gpt-oss tool-call messages whose header carries <|constrain|>
status: open
type: bug
priority: 3
created: 2026-11-10T19:36:31Z
---

Source: docs/reviews/code-audit-2026-11-10.md ("gpt-oss tool-call headers are not recognised as Harmony"). Verified VALID 2026-11-10 at a5c4f2db.

**Where:** `internal/processing/harmony.go`, the `harmonyMessage` and `harmonyOpen` patterns (~:30-38), used by `StripHarmony` and `IsHarmonyThinking` through `harmonyStripper` (parserfor.go) — the shipped default for every `gpt-oss` model (internal/profiles/shipped.go:31-36).

**What:** both patterns accept only `[^<]*` between the channel name and `<|message|>`. gpt-oss writes tool calls as `<|channel|>commentary to=functions.x <|constrain|>json<|message|>{…}<|call|>`; the `<|constrain|>` token stops the match, so `StripHarmony` returns raw control tokens and JSON arguments as visible text and `IsHarmonyThinking` reports false while that message streams. No test covers `to=` or `<|constrain|>`.

**Why it matters:** applies when a gpt-oss model sits behind a server that leaves Harmony unparsed: the user sees raw `<|channel|>…<|call|>` text and it lands in the assistant content of history. (apogee has no text parser for Harmony tool calls, so the call does not run either way — a separate gap.)

**Suggested fix:** accept the constrain token in both patterns, e.g. `(?:[^<]|<\|constrain\|>)*`. Add StripHarmony and IsHarmonyThinking vectors for a `to=functions.x <|constrain|>json` header, terminated by `<|call|>` and unterminated.

## Acceptance

- StripHarmony on a full analysis plus commentary to=functions.x <|constrain|>json<|message|>{…}<|call|> response leaves no control tokens or call JSON in Visible.
- IsHarmonyThinking is true for the same message while it is still unterminated.
- Existing harmony vectors still pass.
