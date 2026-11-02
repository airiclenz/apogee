---
title: "ADR 0105 amendment of 2026-11-02 (c): the TUI follows a fallover's launcher and settles its profile at the Turn boundary"
---

Decided with the Server fallback follow-ups (docs/plans/2026-11-02 - 01 - fallback-followups-plan.md, items 5 and 6). It records what the TUI does around the engine's mid-Turn rehome (decision 5) of the session seat, amending decision 5.

Decision 5 rehomes the seat and binds the fallback's model through a `RebindSpec`, but in the TUI two things around that rehome are the host's, not the engine's, and the rehome alone left them behind.

**The launcher follows the fallback entry.** A session-seat fallover installs the `llama-launcher:` config the fallback's entry names, exactly as `/server` onto that entry would: off when the fallback carries no `llama-launcher:` key though the primary did, on against the fallback's own config when it carries one. A follow the host refuses leaves the launcher where the session still is.

**The fallback profile settles at the Turn's end boundary.** The rebind a `/server` move ends in is run for the fallback too: the fallback profile's `tools:` roster is applied, an effort override the fallback model does not offer is cleared (ADR 0060 D8), and the profile and hint notices show; the transcript gets no second model-change note, since the fallback note already named the model. It runs at the boundary rather than with the rehome because the tool set's swap door (`SwapTools`) refuses mid-Exchange, so it is stashed as the TUI's pending rebind and applied when the engine is released — at once where nothing holds the engine, as a background workflow's switch under an idle session finds it. The rebind carries the window the fallback's beat observed, never the pinned one the engine was handed, because the binary records a rebind's window as the server's own observation, and a pin recorded there would bind the pin after a later `/settings` clear.
