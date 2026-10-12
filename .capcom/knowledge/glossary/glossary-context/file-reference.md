---
title: "File reference (`@file`): a workspace file the user names with an `@path` token"
---

**File reference (`@file`)**:
A workspace file the user names with an `@path` token in their message. The loop resolves
each reference at the start of the Turn — reading it within the workspace fence
(`security.SafeOpen`, `os.Root`-pinned) and injecting its content into the user message
as that request's *file context* — and reports-and-skips a missing or escaping ref. The token
is **bare** (`@path`, a run of non-whitespace) or **quoted** (`@"path with spaces"` — `'` is
accepted too, only `"` is ever produced), where the closing quote ends the token and an
unterminated one runs to the end of that line; there are no escape sequences. Parsing
the token belongs to `internal/refs` — a Driver-neutral grammar every Driver reads, not a TUI-private
one; resolution is the agent's. A **Firing**'s prompt is read on that same grammar, so a headless
or scheduled run resolves its references exactly as a session does — except that a missing or
escaping one is skipped *without* the notice, a Firing having no event sink to carry it.
It is the same inline grammar a **Skill**
`/token` uses, and the prompt box accents both on the same rule: a token lights up exactly when it
resolves — the path is in the workspace listing, the id is in the catalog — so a typo visibly
fails to light instead of failing at submit (ADR 0027). A reference is judged by its BYTES, not
its name: content opening `%PDF-` is injected as the document's *extracted text* — the same
extraction `read_file` performs (`internal/doctext`), `[Page N]` marker lines and all — under the
annotation `(PDF, N pages; extracted text, read-only)`, and a document holding no extractable text
(a scan) is reported-and-skipped like any other unresolvable ref, while a text file someone named
`notes.pdf` still injects its text under the plain header. Each block also carries a **structural
floor** (ADR 0006, no config key, never disabled under Bypass): `resolveFileRefs` clamps a
reference's content — before its header is added — against the Budget's
History allocation *split across every reference of that one message* — @file blocks and attached
**Skill** bodies alike, since the two kinds land in one message and spend one allocation — so
anything past that share is elided to the same head/tail-plus-marker shape a capped tool result
gets and no reference can hand the emergency fold a most-recent message it cannot shed. That
share is itself capped at an **absolute 32k tokens** per reference (`fileRefMaxTokens`, a
constant, no key): on a million-token window the share alone would admit a megabyte file whole.
A clip is told to the user as a **note** (`domain.RefClippedEvent` — `@<name> clipped to 32k
tokens — read_file ranges for the rest`, or naming the share when a small window's share bound
instead), never as the missing-reference error: the message went ahead.
_Avoid_: "attachment", "upload" (a reference is read live from the workspace, not stored).
