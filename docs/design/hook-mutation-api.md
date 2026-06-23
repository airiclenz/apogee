# Hook Mutation API — Design Draft (P0.1)

**Status:** ✅ **Applied to `apogee.go` 2026-06-23** under the §8 recommended defaults. This
is **P0.1** from the TDD §8 backlog — the missing piece of the public keystone: the
accessor/mutation surface for the opaque `Request`, `Response`, and `Conversation` types the
five hook interfaces operate on. The surface now type-checks (`go vet` + `go build` clean in a
throwaway module); bodies remain `panic` stubs until P0.2 stands up `go.mod` + `internal/`.

**Date:** 2026-06-23
**Method:** scoped from apogee-sim's *real* Transform/Injector/Intervention signatures
(not speculation, per TDD §6.2), via a three-slice survey of
`/workspace/repos/apogee-sim`:

| Slice | Surface surveyed | Grounds |
|---|---|---|
| A | proxy `RequestTransform` pipeline — library, decompose, toolfilter, cot, filehint, intent, grammar, compress | `Request` |
| B | history-inspecting/rewriting proxy mechs — cached_content_intercept, error_enrichment, read_loop, read_repeat, validate/syntax/autofix, codeinfo | `Response`, `ToolResult`, pairing |
| C | bench lab-only interventions — `internal/sim/intervention.go` (`correct_tool_result`, `truncate_history`) + fork/step state | `Conversation`, copyability |

All `file:line` references below are in `apogee-sim` unless prefixed `apogee.go:`.

---

## 1. Five findings that reshape the keystone

These came out of the survey and matter more than any individual signature.

1. **apogee-sim is request-only; the "post-*" hooks have no 1:1 port.** Every production
   mechanism is a `RequestTransform.Transform(req *ChatCompletionRequest, meta *RequestMeta)`
   (`internal/pipeline/pipeline.go:217`). The proxy never owned the loop, so *every* "act on
   the response / on a tool result" behaviour is implemented as **rewriting the next outgoing
   request's message history**. Consequence: the accessor design must be driven by *intent*
   (what each mechanism reads/writes), then mapped onto apogee's five hooks — not by porting
   the `RequestTransform` shape.

2. **Cross-turn history read access is needed at *every* hook, not just history-rewrite.**
   cached_content (`cached_content_intercept.go:79`), error_enrichment (≥2 same-file errors,
   `error_enrichment.go:125`), read_loop, read_repeat, and codeinfo all decide by *aggregating
   across turns*. So the primary mutable value a hook gets (`*Response`, `*ToolCall`,
   `*ToolResult`) is never enough — **each hook also needs a read-only window onto the
   conversation + tool menu + budget.** This is the single biggest addition to the sketch
   (see §3, `LoopView`).

3. **The dominant mutation is "inject a message into the stream at a role-safe position" —
   and it's reimplemented 3×.** library (`transform.go:64`), cot (`cot.go:125`), and decompose
   (`injectFocusDirective`/`injectStepHint`/…) each hand-roll "append to first system message,
   else prepend one", every one guarded by a literal **idempotency marker** substring-scan.
   filehint uses the shared `pipeline.InjectContext` (`pipeline.go:254`) which encodes the
   load-bearing **role-safety rule**: insert before the last user message, but if the
   conversation *ends in a tool result* append to the system prompt instead (strict Jinja
   templates reject `user`-after-`tool`). These two operations — *role-safe inject* and
   *append-to-system-with-marker* — must be **first-class API primitives**, not left to each
   mechanism.

4. **`compress` is not one mechanism in apogee — it splits across the architecture, which
   shrinks the mutation surface.** The TDD's four-way context split maps `compress` to:
   tool-result **capping** (a pre-request mechanism → "truncate one tool message's content in
   place"), generative **Compaction** (lives in `context/`, internal — *not* a hook, writes
   back via a wholesale replace the public API need not expose to mechanisms), and history
   **truncation** (`truncate_history` → a history-rewrite mechanism → "drop the middle").
   So the pre-request hook needs only *in-place content edit by index*, **not** a wholesale
   `ReplaceMessages` — that belongs to internal Compaction and to the `Conversation` surface.

5. **`RequestMeta` is three different things; only one is conversation data.** apogee-sim's
   `RequestMeta` (`pipeline.go:222`) bundles (a) budget/token math, (b) `SuppressedMechanisms`
   / `FiredCounts` self-regulation state, and (c) — via proxy globals, not even in meta —
   backend capabilities and the Library `Store`. In apogee these separate cleanly:
   (a) → the read-only `LoopView.Budget()`; (b) → **registry/loop-managed** (the loop simply
   doesn't call a suppressed mechanism; cross-mechanism coupling becomes an ordering/
   incompatibility declaration or a `view.Fired(id)` query) — *not* a map the mechanism reads;
   (c) → **construction-time dependency injection** (a Mechanism holds its `Store`/capability
   provider, given via `Config`), *not* a per-call argument. See §8 open decisions #2/#3.

---

## 2. Shared substrate

The types every hook surface is built from. `Message` is the read-only snapshot handed out;
mutation is always by **index against the owning container** (so the loop keeps ownership of
the backing storage and can preserve `Extra` round-tripping and copyability).

```go
type Role string
const (
    RoleSystem    Role = "system"
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
)

// Message is a read-only snapshot of one conversation message handed to hooks.
// Hooks never hold the loop's backing storage; they read Messages and mutate by index.
type Message struct {
    Role       Role
    Content    string
    ToolCalls  []ToolCall      // RoleAssistant only (reuses apogee.go:341 ToolCall)
    ToolCallID string          // RoleTool only — links the result to its originating ToolCall.ID
}

// Extra exposes preserved unknown wire fields (reasoning_content, tool_choice, thinking…).
// Round-trip preservation of these is load-bearing for fork/resume (finding §1.C, pipeline.go:59).
func (m Message) Extra(key string) (json.RawMessage, bool)

type ToolDef struct {            // one entry of the tool menu the model sees
    Name        string
    Description string
    Schema      json.RawMessage  // JSON-schema of arguments
}

type Budget struct {             // the RequestMeta budget fields (pipeline.go:222), read-only
    ContextLimit  int
    Used          int            // estimated tokens used so far
    CharsPerToken float64
}
```

### 2.1 `LoopView` — the read-only window every hook gets (finding §1.2)

```go
// LoopView is the read-only context passed to every hook in addition to its own mutable
// value. It is the home of all cross-turn reads the survey found mechanisms need.
type LoopView interface {
    Conversation() ConversationView
    Tools() []ToolDef            // the current menu (validate checks names against it; bridge.go)
    Budget() Budget
    Turn() int                   // turn index (codeinfo "fire once per session" → guard on this + a marker)
    Fired(mechanismID string) int // self-regulation query (replaces meta.FiredCounts coupling, §1.5)
}

// ConversationView is read-only history with the pairing helpers the survey showed are
// needed everywhere (forward + backward ToolCallID resolution — slice B §C).
type ConversationView interface {
    Len() int
    At(i int) Message
    Range(fn func(i int, m Message) bool)
    LastUser() (msg Message, index int, ok bool)        // intent.LastUserMessage, used by 5 mechs
    CallByID(id string) (call ToolCall, index int, ok bool)   // tool result -> originating call (name/args)
    ResultFor(callID string) (msg Message, index int, ok bool) // call -> its tool result
}
```

`CallByID`/`ResultFor` encode the pairing logic reimplemented in every history mechanism
(`findToolResultError` `error_enrichment.go:149`, `isToolResultError` `read_loop_detector.go:139`,
`findToolResult` `codeinfo/transform.go:112`). The tool *name* and *arguments* live only on
the originating `ToolCall`, never on the tool-result message — so resolving result→call is
mandatory for any error-handling mechanism (slice B §C).

---

## 3. `Request` — pre-request hook

`PreRequestHook.PreRequest(ctx, req *Request)` (`apogee.go:402`). The outgoing request *is*
the conversation-as-it-will-be-sent, so reads go through `req.View()`; mutations are the
characterised set from slice A.

```go
type Request struct{ /* opaque: messages, tool menu, params, extras */ }

// --- reads ---
func (r *Request) Model() string                                  // library keys Store.Query on it
func (r *Request) View() LoopView                                 // conversation + tools + budget (§2.1)
func (r *Request) Extra(key string) (json.RawMessage, bool)       // grammar checks for response_format

// --- mutations (each grounded in §7) ---

// AppendToSystem appends text to the first system message (creating one if absent), but is a
// no-op if marker already occurs in that message. Returns whether it injected. Replaces the
// 3× hand-rolled append-to-system loops (library/cot/decompose). (finding §1.3)
func (r *Request) AppendToSystem(marker, text string) (injected bool)

// InjectContext inserts a user message at the role-safe position: before the last user
// message, or — if the conversation ends in a tool result — appended to the system prompt
// (pipeline.InjectContext, pipeline.go:254). Used by filehint; the natural home for the
// inject-a-hint mechanisms (error_enrichment/read_loop/read_repeat) wherever they land.
func (r *Request) InjectContext(text string)

// SetMessageContent edits one message's content in place by index: tool-result capping
// (compress capToolResults, compress.go:458) and decompose's history-collapse of older user
// messages (decompose.go:212). No wholesale replace at this hook — that is internal
// Compaction's job (finding §1.4).
func (r *Request) SetMessageContent(index int, content string)

// SetTools replaces/reorders the tool menu (toolfilter, toolfilter.go:70).
func (r *Request) SetTools(tools []ToolDef)

// SetExtra sets an unknown request field, lazily allocating (grammar sets response_format,
// proxy.go:657 — the only writer of the Extra map).
func (r *Request) SetExtra(key string, v json.RawMessage)

// SetSampling sets temperature/max_tokens. FORWARD-LOOKING: no surveyed mechanism mutates
// these; included for completeness, low priority.
func (r *Request) SetSampling(p SamplingParams)
```

---

## 4. `Response` — post-response hook

`PostResponseHook.PostResponse(ctx, resp *Response) (PostResponseDecision, error)`
(`apogee.go:407`). The just-produced assistant turn. The *single-turn-expressible*
mechanisms — validate, syntax, autofix (slice B §E) — live here cleanly; the history-needing
ones read `resp.View()`.

```go
type Response struct{ /* opaque: text, parsed tool calls, finish reason, thinking */ }

func (r *Response) Text() string                       // empty-response / tool-use-enforcer off-ramps
func (r *Response) ToolCalls() []ToolCall              // validate/syntax read name + raw-string args
func (r *Response) FinishReason() FinishReason         // exposed for parity; apogee-sim never used it
func (r *Response) Thinking() (text string, ok bool)   // harmony/thinking channel (NEW — not in apogee-sim)
func (r *Response) View() LoopView                     // read_repeat needs history (slice B §E)

// --- mutation (corresponds to PostResponseDecision ActionIntercept) ---
// SetToolCallArguments rewrites one tool call's arguments in place — autofix writing back
// formatted file content (response_validator.go:126, the only response mutation in apogee-sim).
func (r *Response) SetToolCallArguments(index int, args json.RawMessage)
func (r *Response) SetText(s string)                   // rare; intercept the assistant text
```

### 4.1 `PostResponseDecision` — pin the `ActionDefer` payload

`PostResponseDecision` (`apogee.go:430`) already enumerates Retry / Intercept / Defer. The
survey grounds what each *carries*:

- **`ActionRetry`** — re-call Upstream now. This is validate/syntax's `retryWithCorrection`
  (`response_validator.go:366`): append the bad response + a correction message, re-request.
- **`ActionDefer`** — the **feed-forward** pattern: in streaming mode the response can't be
  retried in place, so the correction is *stored* (`StoreCorrection`, `session_state.go:176`)
  and injected (role-safe) into the **next** request (`injectCorrectionIfNeeded`,
  `response_validator.go:19`). So `ActionDefer` carries an injection payload:

```go
type PostResponseDecision struct {
    Action PostResponseAction
    // Inject is the role-safe text injected into the NEXT request when Action == ActionDefer.
    Inject string
}
```

This also resolves an apogee-sim gap (slice C §D #3): the deferred correction lived in
*transient* proxy `SessionState`, dropped on fork. For apogee, a deferred correction must
survive snapshot/resume → it belongs **in `Conversation`** (§6, open decision #4).

---

## 5. `ToolCall` / `ToolResult` — pre-tool-exec & post-tool-result

Both are **already concrete mutable structs** in the sketch (`apogee.go:341`, `apogee.go:348`),
so mutation is direct field assignment. The survey adds two things:

- **pre-tool-exec** (`PreToolExec(ctx, call *ToolCall)`, `apogee.go:412`): cached_content's
  relocation target. Deciding "this read is redundant" requires scanning history for a prior
  successful read of the same path (`cached_content_intercept.go:79`) → **needs `LoopView`**.
- **post-tool-result** (`PostToolResult(ctx, result *ToolResult)`, `apogee.go:418`):
  `correct_tool_result` simply replaces content (`intervention.go:140`) — `result.Content = x`
  already works. But error-handling mechanisms treat read-errors vs write-errors differently
  and need the **originating tool name + args**, which live on the call, not the result
  (slice B §C) → the hook must receive the **originating `ToolCall`** and a `LoopView`.

```go
// Proposed signature changes (add LoopView everywhere; add the originating call to post-tool-result):
PreToolExec(ctx context.Context, call *ToolCall, view LoopView) error
PostToolResult(ctx context.Context, call ToolCall, result *ToolResult, view LoopView) error
```

`ToolResult.IsError bool` (`apogee.go:352`) stays — and is *better* than apogee-sim, which had
to pattern-match error strings (`isToolResultError` etc.) precisely because the proxy only saw
text. In apogee the tool reports `IsError` authoritatively. Error *classification*
(syntax/import/missing-file — `classifyError` `error_enrichment.go:31`) stays
mechanism-internal, not a field on the type.

---

## 6. `Conversation` — history-rewrite hook

`HistoryRewriter.HistoryRewrite(ctx, conv *Conversation)` (`apogee.go:424`). Grounded by the
two lab-only interventions (`internal/sim/intervention.go`) plus the fork/step requirements
(slice C). The intervention signature there is
`ApplyIntervention(iv, messages []Message, tools []Tool) ([]Message, []Tool)` — it works on a
flat message slice and returns a copy; apogee wraps that slice as the opaque `Conversation`.

```go
type Conversation struct{ /* opaque, copyable, fully serializable, NO live handles */ }

// --- reads ---
func (c *Conversation) Len() int
func (c *Conversation) At(i int) Message
func (c *Conversation) Range(fn func(i int, m Message) bool)

// --- boundary helpers truncate_history needs (intervention.go:156–173) ---
func (c *Conversation) PrefixEnd() int            // end of leading system msgs + first user msg
func (c *Conversation) AssistantBoundaries() []int // cut points keeping tool results adjacent to their call

// --- mutations ---
func (c *Conversation) SetMessageContent(i int, content string) // correct_tool_result at history level
func (c *Conversation) DropRange(start, end int)                // truncate_history: drop the middle
func (c *Conversation) Insert(i int, m Message)                 // the static "gap note" user message
func (c *Conversation) Replace(msgs []Message)                  // generative Compaction writes summaries back here
```

**Reality check on the sketch's comment** (`apogee.go:534` "turns, summaries, deferred
actions"): apogee-sim has **no summaries** (truncate is *drop-the-middle + optional static
gap note*, not summarization — slice C §D #1) and **no turn abstraction** (flat
`[]Message`; "turns" are reconstructed by scanning assistant boundaries — §D #2). So:

- **Summaries** are not a separate structure — they are ordinary messages produced by
  generative Compaction (`context/`) and written back via `Replace`. No `GetSummaries()`.
- **Deferred actions** do *not* exist in apogee-sim's forkable state (only as transient proxy
  state, dropped on fork — §D #3). apogee *should* add them to `Conversation` so `ActionDefer`
  (§4.1) survives snapshot/resume. → open decision #4.

**Copyability** (slice C §C): apogee-sim forks by *serialize-to-disk-then-reload*
(`fork.go:39`), so the value is independent by construction. This validates ADR 0001's
"cleanly copyable value with no live handles." Requirements the design inherits: (a) fully
JSON round-trippable including per-message `Extra` (`pipeline.go:70–152`); (b) zero embedded
live handles — anything session-scoped that could couple branches (apogee-sim re-keys
`SessionID` on fork, `fork.go:120`) must reset on copy.

---

## 7. Operation → mechanism traceability

Every method above earns its place from a real mechanism. Condensed:

| API operation | Mechanisms that need it | Evidence |
|---|---|---|
| iterate messages (role+content) | library, decompose, compress, filehint | transform.go:33, decompose.go:193 |
| `LastUser()` | library, decompose, toolfilter, cot, filehint | intent.go:67 |
| `Budget()` (token math) | library, compress | transform.go:31, compress.go:62 |
| assistant `ToolCalls[].Name` scan | toolfilter, cot, decompose, compress, filehint | toolfilter.go:79, cot.go:160 |
| tool-call `Arguments` → path | cot, compress, codeinfo | cot.go:185, codeinfo/transform.go:78 |
| `Tools()` menu (name/desc/schema) | toolfilter, cot, grammar | toolfilter.go:154, grammar.go:14 |
| `CallByID` / `ResultFor` pairing | cached_content, error_enrichment, read_loop, read_repeat, codeinfo | error_enrichment.go:149 |
| `AppendToSystem(marker,…)` | library, cot, decompose | transform.go:64, cot.go:125 |
| `InjectContext` (role-safe) | filehint, codeinfo, error_enrichment, read_loop, read_repeat | pipeline.go:254 |
| `SetTools` | toolfilter | toolfilter.go:70 |
| `SetExtra` | grammar | proxy.go:657 |
| `SetMessageContent(i)` (pre-request) | compress (tool-cap), decompose (collapse) | compress.go:458, decompose.go:212 |
| `Response.SetToolCallArguments` | autofix | response_validator.go:126 |
| `Response.ToolCalls()` read | validate, syntax, read_repeat | validate.go:71 |
| `ActionRetry` / `ActionDefer.Inject` | validate/syntax retry, streaming feed-forward | response_validator.go:366, session_state.go:176 |
| `ToolResult.Content =` (replace) | correct_tool_result | intervention.go:140 |
| post-tool-result needs originating call | error_enrichment (read vs write error) | slice B §C |
| `Conversation.DropRange` + `Insert` | truncate_history | intervention.go:178 |
| `Conversation.PrefixEnd`/`AssistantBoundaries` | truncate_history | intervention.go:156 |
| `Conversation.Replace` | generative Compaction (context/) | compress.go:93 (analogue) |

---

## 8. Open decisions (need a call before editing `apogee.go`)

1. **Give every hook read access to loop state?** *Recommended yes — **APPLIED**, refined for
   consistency:* the survey is unambiguous that non-history hooks need cross-turn reads
   (finding §1.2). Resolution shipped: `Request` and `Response` expose `View() LoopView`
   (the request/response *is* the working value, so the view rides on it); `PreToolExec` and
   `PostToolResult` take an explicit `LoopView` argument (their primary value is too small to
   carry it); `HistoryRewriter` reads/writes `*Conversation` directly (it *is* the history).
   So `PreRequest`/`PostResponse` signatures are unchanged; only the two tool-stage hooks
   gained a parameter (plus the originating `ToolCall` on `PostToolResult`).

2. **Self-regulation state: registry-managed, not a meta map.** *Recommend:* suppression is
   the loop's job (don't call a suppressed mechanism); the decompose↔read-loop coupling
   (`meta.FiredCounts`, `decompose.go:124`) becomes a `LoopView.Fired(id)` query +/or an
   ordering/incompatibility declaration in the `MechanismDescriptor`. No `SuppressedMechanisms`
   map handed to mechanisms.

3. **Mechanism dependencies via construction, not per-call.** *Recommend:* the Library
   `Store` and backend-capability provider (grammar's `SupportsNativeToolCalls`, `proxy.go:629`)
   are injected when the Mechanism is built (via `Config`), held on the Mechanism — not passed
   through the hook. Keeps the hook signature about *conversation state*, not plumbing.

4. **`Conversation` carries a deferred-corrections queue?** *Recommend yes* — so `ActionDefer`
   (§4.1) survives snapshot/resume, closing the apogee-sim gap where it lived in transient
   session state dropped on fork (slice C §D #3). Summaries are *not* a separate structure
   (they are messages; §6).

5. **`Message.Content` is string-only for v1?** *Recommend yes.* apogee-sim already flattens
   OpenAI multi-modal array-content to a string (`pipeline.go:84`); a small-model coding agent
   doesn't need parts. Preserve any unknown structure in `Extra`. Revisit if a vision model
   target appears.

6. **Mutation is index-addressed, never raw-slice.** *Recommend:* hooks get `Message` value
   snapshots + `SetMessageContent(i,…)` / `Insert` / `DropRange` / `Replace`; the loop keeps
   the backing slice. This supports the in-place editors (compress, decompose) *and* the
   wholesale rewriters (Compaction, truncate) without leaking storage or breaking `Extra`
   round-tripping.

7. **`grammar` and `filehint` aren't `RequestTransform`s in apogee-sim** (proxy-level hooks,
   slice A §D #1). Not an API-shape issue — both still take a request and mutate it, so the
   `Request` surface covers them — but a flag for the Phase-4 catalogue-mapping session that
   the hook-point assignment isn't always the obvious one.

---

## 9. Proposed concrete edits to `apogee.go` (once decisions land)

1. Add the §2 substrate: `Role`, `Message`, `ToolDef`, `Budget`, `LoopView`,
   `ConversationView`.
2. Replace the empty bodies of `Request`/`Response`/`Conversation` (`apogee.go:525–538`) with
   the §3/§4/§6 method sets (still opaque structs; methods only).
3. Add a `LoopView` argument to `PreToolExec` and `PostToolResult` (plus the originating
   `ToolCall` to the latter); `Request`/`Response` expose `View()` instead (decision #1).
4. Replace `PostResponseDecision.Payload` with a typed `Inject string` (§4.1).
5. Note in the `Conversation` doc comment that summaries are messages and that it carries a
   deferred Response Action (decision #4).

This keeps the keystone **opaque + additively versioned** (ADR 0001) while giving hooks the
exact operation set the apogee-sim corpus proves they need.

> **Applied 2026-06-23.** All five edits landed in `apogee.go`; `go vet` and `go build` pass
> in a throwaway module (stub bodies). Items #1 and #2 differ slightly from the original
> bullets as noted in §8 #1.
