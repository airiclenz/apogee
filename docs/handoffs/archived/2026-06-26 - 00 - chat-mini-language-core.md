# Handoff — Chat input mini-language (core slice)

**Date:** 2026-06-26
**Status:** approved design, implementing
**Track:** post-`v1.0.0` **feature-parity** (apogee-code → Go TUI). This is the *first*
entry on that track. It belongs to **no** phase plan — `docs/plans/` covers phases 0–3
(shipped in `v1.0.0`). It is purely **additive**: a new TUI parse/route layer plus the
completion of a half-scaffolded agent feature (`@file` resolution). No freeze break.

## Why

apogee-code (the original VS Code extension, and the behavioral oracle — see its shipped
webview `media/chat.js`, array `Ws`, not its stale TDD) gives the chat box a mini-language:
`/`-commands, `@file` references, and an autocomplete overlay. The Go TUI sends the raw
input string straight through (`internal/tui/model.go` `submit()` →
`domain.UserInput{Text: text}`), so none of that exists. `TODO.md` tracks this as the
**[P0]** "Chat input mini-language" item.

## Scope (this slice)

Builds the **core** of the mini-language:

- a pure parser + router between the input box and the agent;
- an autocomplete overlay covering **both** `/`-commands and `@`-file names;
- commands `/clear`, `/continue`, and a **stubbed** `/compact`;
- a real **agent-side `@file` resolver**.

### Decisions locked with the owner (2026-06-26)

1. **`/compact` is stubbed.** The TODO assumed it wires to "the existing generative
   Compaction reducer in `internal/context`". That reducer **does not exist** —
   `internal/context/` is an empty Phase-0 scaffold (`doc.go` only). What exists is the
   plumbing: a `CompactionEnabled` config flag, the `HistoryRewriter` interface
   (`domain/mechanism.go:60`), the `runHistoryRewriteHooks` fire path, and
   `Conversation.Replace` for write-back — but no summarizer. So `/compact` routes through
   the parser and reports "compaction not yet implemented" via `Agent.Compact()`, a seam the
   follow-up reducer slice fills in.
2. **Autocomplete covers `/`-commands and `@`-files** (file names via a bounded, capped walk
   of the workspace root).
3. **`@file` resolves agent-side**, not in the TUI — replacing the `loop.go`
   `noteUnresolvedFileRefs` error-emit. Rationale (best long-term, per the owner's
   architecture preference): reuses the agent's existing workspace pin + security
   primitives, round-trips through snapshot/resume, and is where future context-budgeting
   will live.

### Out of scope (follow-ups)

- **`/compact` real reducer** — build `internal/context` generative summarization, wire into
  the `Agent.Compact()` seam created here.
- **`/skill`** — needs a new `internal/skills` package (the separate [P0] item). The
  `UserInput.SkillIDs` field is pre-wired now so that slice doesn't bump the session format.
- **`/server`** — needs a swappable provider seam (today `upstream` is immutable
  post-construction).
- **`@`-file-listing cache** — the overlay walks on demand; a cap bounds cost; caching later.

## Design

A pure parser classifies each raw input line:

```
parsedInput{ kind, command, text, fileRefs }
```

- **Command** iff the trimmed line's first whitespace token ∈ `{/clear, /compact, /continue}`.
- Anything else (incl. unknown `/foo`) is a **message**, sent to the agent as-is — we never
  swallow a legit message that happens to start with `/`. The overlay steers users to the
  known set.
- An `@`-ref is `@` at start-of-line or after whitespace, then non-space path chars (so
  `foo@bar.com` is *not* a ref). The literal `@token` stays in `text`; the path goes into
  `fileRefs`.

**Routing (replaces the body of `submit()`):**

| input | action |
|---|---|
| message | `startExchange(UserInput{Text, FileRefs})` (as today) |
| `/continue` | `startExchange(UserInput{Text: "Please continue"})` |
| `/clear` | `eng.ClearContext()` + `transcript.addNote("context cleared")`, stay idle |
| `/compact` | `eng.Compact(ctx)` → note the not-implemented result, stay idle |

## Implementation map (file → change)

**Agent core:**
- `internal/tui/tui.go:20` `Engine` — add `ClearContext() error`, `Compact(context.Context) error`.
- `internal/agent/agent.go` (by `Snapshot`, :148) — `ClearContext()` resets
  `a.conv = domain.NewConversation(nil)`, keeps `turnIndex/inExchange/pendingInput/approved/mode`,
  guards `inExchange`. `Compact(ctx)` returns sentinel `domain.ErrCompactionNotImplemented`.
- `internal/agent/loop.go:161-166,410` — replace `noteUnresolvedFileRefs` with
  `resolveFileRefs`: per ref `security.SafeReadFile(a.cfg.WorkspaceDir, ref)` (TOCTOU-safe,
  `os.Root`-pinned — `internal/security/safeio.go`), replicate `read_file.go` guards (10 MB
  `maxFileReadBytes`, reject dir/missing, uniform `ErrPathEscape` message), inject content
  before the user text, emit `ErrorEvent` + skip on a bad ref (turn still proceeds).
- `internal/domain/config.go:138` `UserInput` — add `SkillIDs []string` (`omitempty`,
  reserved for `/skill`); add `ErrCompactionNotImplemented`.

**TUI parser + overlay:**
- `internal/tui/command.go` (new) + `command_test.go` (new) — pure `parseInput`.
- `internal/tui/model.go` — route in `submit()`; add `autocomplete` state; recompute on idle
  typing; intercept nav/accept/dismiss keys; `renderAutocomplete()` inserted above the input
  with the approval-overlay viewport-shrink pattern (`View()` ~ :542-578). Bounded
  `workspaceFiles(prefix)` walk of `m.opts.Workspace`.
- Test fakes implementing `Engine` in `internal/tui/*_test.go` gain the two methods.

## Verification

- `go test ./...`, `go build ./...`; `TestModelNoBuilderByValue` still green.
- Live TUI (`http://192.168.64.1:1111`): `/` lists commands; `@` lists files; tab splices a
  path; a message with `@<file>` is read by the model; missing ref → error line, turn
  proceeds; `/clear` clears model memory but keeps scrollback; `/compact` notes
  not-implemented; `/continue` resumes.
