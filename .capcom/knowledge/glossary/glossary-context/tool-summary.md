---
title: "Tool summary: the structured, host-facing half of a tool's outcome"
---

**Tool summary**:
The **structured half** of a tool's outcome, carried beside the prose half on the same
tool result: `Content` is the prose half and is written for the **model** (its wording is
free to change); a `ToolSummary` is written for a **host** — the TUI's tool card today, a
headless or bench renderer later — and carries the facts the tool already computed for its
own header (a read span, a match count, a diffstat) **as data** rather than as a sentence a
reader has to parse back out. It is a **sealed sum** in `domain`, exactly like an Event: the
marker method is unexported, so an embedder can *read* every variant and *add* none.
**Optional by construction**, so tools stay an open extension point
(ADR 0002) — a
tool that emits none renders from prose exactly as before, and only the **eleven** built-ins
whose outcome the view used to re-derive carry one (`read_file`, `list_dir`,
`grep`, `view_diff`, `web_search`, `git_status`, and — as **Edit regions** — the four writing tools
`write_file`, `edit_existing_file`, `single_find_and_replace`, `multi_find_and_replace`) — plus
`sub_agent`, whose **DelegationOutcome** (since 2026-10-02) states how the delegated run ended (the
engine bound it stopped at, a human stop, no report, the count of steered messages) on every result
for a run the engine started, and on none for a refusal — `read_file`'s
carries the locate facts too, the substring asked for and the absolute line numbers it fell
on. The model-facing complement is the **syntax trailer**: the four writing tools append their
in-process syntax verdict on the bytes they just wrote to the prose half of the same success result
(`internal/tools/syntaxtrailer.go` over `internal/syntaxcheck` — the one syntax engine, whose
Go-only entry `syntaxcheck.CheckGo` also gives the `diagnostics` tool its syntax verdict; the two
share the parser and keep their own renderings), so the model reads what its edit
did to the file while the host's card still shows only the tool's own first line — the write always
lands, and the trailer is feedback, never a refusal.
A summary is **never sent to the model** — it is display data a host consumes, and the
summary *value* never reaches the session record. Since 2026-10-17 it has one wire form, a mirror
owned by `internal/eventjson` on **Event lines** and the **Event journal**, decoded only for live
**Watch mode** and never on a record replay path (ADR 0103 D9, amending ADR 0052). What the
transcript codec may mirror into the session record are the **facts** a variant carries, which is what the neutral codec in `internal/session` does for the
**Edit regions** below
(ADR 0052 §5);
the rendered view the codec stores keeps its presenter verdicts on the wire, a standing denial
(ADR 0083 §1).
A Reaction that rewrites `Content` at the `post-tool-result` Moment does not invalidate it: a
summary records what the tool *did*, not what the text *says*.
_Avoid_: "tool metadata", "tool result type" (the result already has a type; this is its
structured outcome).
