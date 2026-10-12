---
title: Driver
---

**Driver**:
A program that embeds the **Embeddable agent** and drives the loop through its public
API. Four *product* Drivers ship — the TUI, `apogee daemon`, `apogee headless` and the bench — and
the in-process and PTY test drivers in `internal/tuitest` are Drivers too
(ADR 0062).
The engine must stay sufficient for *any* Driver: nothing model-visible or
safety-relevant may exist only in one Driver's surface, and every inbound wire surface (HTTP API,
webhook listener) is composed by a Driver — the engine itself is wire-silent and composes none. The
engine does dial out: the **Upstream**, the network tools (`http_request`, `web_fetch`,
`web_search`) and, beside them, one Reaction wire of its own — a sync-lane advise or gate
**Reaction** spelled as a webhook POSTs its seam document through the url-safety guard
(`(*Agent).runSyncWebhook`; ADR 0076's 2026-09-19 amendment). An observe webhook is posted by the
Driver's Reaction runner, not by the engine. See
ADR 0031.
_Avoid_: "embedder" (names the linking, not the responsibility of pacing the loop),
"frontend" / "client" (a Driver owns the loop's pace and state roots, not just a view).
