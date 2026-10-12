---
title: ADR 0026 considered options
---

- **Concatenating the content into `Config.SystemPrompt` at the composition root** — rejected, and it
  is the option most of this ADR exists to refuse. It would run a stranger's file through
  `prompt.Validate`, so a repo whose `AGENTS.md` mentions `{{foo}}` would stop apogee from starting;
  it would also freeze the content at process start, losing §5's per-boundary re-read, and would make
  the prompt template and the discovered data one indistinguishable string in the Budget, the
  snapshot path and every error message.
- **A priority chain (first name found wins)** — rejected with §2. Adding a fallback name would then
  silently disable the primary one in every repo carrying both, and there is no reading of a
  configured *list* under which that is the obvious behaviour.
- **Walking up parent directories, git-style** — rejected with §1: the same repo would say different
  things depending on where it was cloned, and a file above the workspace is outside the fence every
  other read in apogee respects.
- **A global `~/.apogee/AGENTS.md`** — rejected: `system-prompt-text` / `system-prompt-file` already
  *are* the standing text that follows the user, and a second spelling of the same thing would only
  raise the question of how the two merge.
- **Re-reading per request, or watching the files** — rejected with §5: it would invalidate the
  server's prefix cache mid-session and leave a conversation reasoned partly under text that is no
  longer present. "Takes effect on your next `/new`" is both cheaper and easier to predict.
- **Truncating or capping oversize content to its Budget share** — rejected with §8. The one thing
  worse than a project's conventions costing tokens is *half* of them reaching the model, with the
  cut falling wherever the estimator happened to land. The human is told; the human decides.
- **A configurable size limit (a new constant or key)** — rejected: the Budget already allocates a
  system-prompt share, and a second, unrelated number would be one more thing to tune wrongly.
- **Failing startup on an unreadable context file** — rejected with §7: it makes a repo able to stop
  apogee from starting, which discovery must never do.
- **A `--context-file` flag / `APOGEE_CONTEXT_FILES`** — rejected with the other newer keys
  (ADR 0023 §1): the precedence ladder is for per-invocation switches.
- **Modelling it as a Mechanism (so `--bypass` would disable it)** — rejected. It is standing
  configuration, like the system prompt, not a self-regulating loop behaviour
  (ADR 0006); a bypass run must still know what
  project it is in, or the two arms of an A/B are not comparable on anything but the Mechanisms.
- **An engine-side notice/note event carrying the report** — rejected: `internal/domain/events.go`
  has no note event, session notes are TUI-owned (`transcript.addNote`), and the host already owns
  the three boundaries the notice attaches to. A read-only report method keeps the new surface to one
  method and no new event variant.
