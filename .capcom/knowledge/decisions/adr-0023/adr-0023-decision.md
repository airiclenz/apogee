---
title: ADR 0023 decision
---

**1. The configuration surface is file-only: ~~three top-level keys~~.** *(Superseded 2026-08-31 by
ADR 0064 §2 and this record's own 2026-08-31
amendment below: **four**. A fourth top-level key, `use-default-prompt:`, joins the three named
here — a bool, default `true`, gating the embedded-default rung of that record's resolution ladder;
an explicit `use-default-prompt: false` is the "send no prompt at all" that deleting these keys
used to spell.)* `system-prompt-text:` (the
template inline), `system-prompt-file:` (a file holding it), and `system-prompt-models:` (a map of
model name → an entry reusing the *same two key spellings*, not a bare `text:`/`file:`). There is
no flag and no environment variable, following the newer-key convention (`present:`, `mechanisms:`,
`model-profile:`): a system prompt is a standing statement about how you work, not a per-invocation
switch. `system-prompt-file` expands a leading `~` and resolves a relative path against the
**apogee home** — the directory `config.yaml` itself lives in — and never against the workspace: the
key lives in a global file that travels with the home, so a workspace-relative base would break one
config across every project it is used in.

**2. A matching per-model entry REPLACES the global prompt; a non-matching entry is inert.**
Matching is exact string equality against the **resolved** model name — the label discovery or
`--model` settled on, the same one the Validated sets key on
(ADR 0016) *(Amended 2026-09-30:
Validated sets are deleted, ADR 0076
A9; the matching rule stands on its own)* — so selection can
only happen in the composition root *after* model resolution, and `resolveSystemPrompt` is called
from `runRoot` for that reason. Replacement is **whole-entry**: an entry setting only
`system-prompt-file` does not inherit the global `system-prompt-text`, because a prompt is one
document and half of one is not a prompt. An entry naming a model this run is not is never selected
and its file is **never read** — the `unconfined-hosts` posture
(ADR 0012's 2026-07-21
amendment): it describes a configuration this machine is not in, so one config can carry a prompt
per model across every machine it travels to.

**3. Validation splits on what is machine-independent.** Setting **both** spellings at one level, or
a `system-prompt-models` entry setting **neither**, is a defect in the file itself — a contradiction
and an indentation slip respectively — so both are checked at **every** level while config resolves,
where the message can name the exact key. Whether a file **reads** and whether a template's
placeholders are **known** are properties of the **selected** source only, checked in
`resolveSystemPrompt`: refusing to start over a non-matching entry's file, which may exist only on
another machine, would make the travelling config of §2 unusable everywhere else. Defense in depth:
`newAgent` re-validates `Config.SystemPrompt` (the `processing.ParserFor` construction-gate
precedent), so a Go embedder's typo fails construction loudly instead of shipping raw braces to the
model; the host-side check fires first for config users and names the config key.

**4. The template language is exactly four placeholders, strictly spelled, and closed.**
`{{workspace}}` (the absolute workspace path), `{{datetime}}`, `{{mode}}` (the autonomy mode label),
`{{scratch}}` (the session scratch dir, joined with
ADR 0056 §3).
Nothing else, and the set is **closed by decision**, not pending expansion — every placeholder is a
promise the engine must keep rendering. Three properties are load-bearing:

- **`{{datetime}}` renders the DATE only** (`2006-01-02`). A per-request timestamp would change the
  first system message every turn and throw away the prefix KV cache a local llama.cpp server
  relies on; a date is stable within a day, which is the resolution the model actually needs.
- **Spelling is strict**: `{{ workspace }}`, `{{WORKSPACE}}` and `{{foo}}` are all *unknown
  placeholders* and all are a **startup error listing the known four** — never silent literals
  shipped to the model, because raw braces in a system prompt are exactly the kind of defect a
  small model copies rather than questions. Stray or unclosed braces are not placeholders and pass
  through verbatim.
- **There is no `{{tools_block}}`.** The tool menu stays engine-owned and auto-appended (§6), so the
  user's template can never place it wrongly, duplicate it, or omit it for a model that needs it.

The language lives in a **new `internal/prompt` package**, stdlib-only — it imports not even
`internal/domain` (`Inputs.Mode` is a plain string) — so the host (`cmd/apogee`, validating at
startup) and the engine (`internal/agent`, rendering per request) share one implementation with no
import cycle and one error wording.

**5. `Config.SystemPrompt` carries the TEMPLATE; the Agent supplies the render inputs.** The field
is the validated template, never a rendered string, because three of the four inputs are **live**:
the autonomy mode changes on a Shift+Tab, the date changes at midnight in a long-running session,
and the session scratch dir moves at a session boundary. All four inputs already sit on the Agent —
`cfg.WorkspaceDir`, the lock-guarded `Mode()`, the lock-guarded `ScratchDir()`, and a new injectable
`now func() time.Time` (the `sessionHost.now` shape) — so rendering is a private method, not a
public render-provider seam with exactly one implementation.

**6. Seeding is at `buildRequest`, position 0 — one system message, in one fixed order.** The
rendered prompt is prepended to the message projection before `domain.NewRequest` is built, which
buys every required property at once:

- `AppendToSystem` finds the prompt at index 0 and appends **after** it; the wire seam's
  `injectSystemInstructions` appends the tool block after **that**. The wire therefore carries
  **one** system message reading **prompt → mechanism directives → tool block** — the merge shape
  `TestPromptSeam_AppendsToSeededSystemMessage` already pinned, now the default path.
  *(Amended 2026-09-16: `injectSystemInstructions` is retired; both merges are the one pure
  `domain.MergeSystem`, which `toProviderRequest` applies to the Request's `State()` copy ahead of
  the wire projection — the shape and the order are unchanged.)*
- The projection is **per-request**, so the prompt is re-rendered on every request (`armRequest`,
  and `refold` after an emergency fold —
  ADR 0018) and
  **never enters history or the snapshot**. Nothing about resume, forking or session records changes
  (ADR 0022): `cfg` is not
  serialized, so a session resumed under a changed prompt gets the new one.
- `""` seeds **nothing** — no empty system message, no whitespace — so the native no-prompt anchor
  stays byte-identical and every pre-existing test is untouched.
- `loopView()` stays **unseeded**: it is "the conversation so far" for tool-stage hooks, and the
  profile tool block is likewise absent from it. Seeding is a request-projection concern, not a view
  one.

**7. Sub-agents inherit the prompt; the internal request paths keep their own.** `newChildAgent`
copies `cfg` wholesale, so a child renders the same template against **its own** mode and workspace
with no carve-out — which is right: a sub-agent is the same assistant on a smaller task
(ADR 0005). The compaction summariser
(`internal/context`) and the probe battery (`internal/probe`) build their **own** requests and never
call `buildRequest`, so the user's prompt cannot reach them **by construction** — and must not: a
summariser told "you are a coding agent, use tools" writes a worse summary
(ADR 0021
draws the same line for the battery).

**8. The shipped template goes one key ACTIVE, and that ends its "changes nothing" invariant.**
`internal/config/defaults/config.yaml` carries an uncommented `system-prompt-text:` — a ~5-line persona
and context frame — so a **fresh install** gets a system prompt, while an **existing** seeded config
(which `seedConfig` never overwrites) has no such key and keeps today's promptless behaviour byte
for byte. There is deliberately **no compiled-in fallback**: the default lives in the file the user
can read and edit, deleting the key is how you turn it off, and an upgrade therefore changes nothing
for anyone already running. The template's old invariant — every line commented, so it parses to an
empty `layer{}` — is amended rather than abandoned: the test now asserts that the system prompt is
the **only** key it sets and that every other key still parses to nothing.
