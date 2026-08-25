# Sessions

Every conversation is a session, saved continuously: after each completed turn the
session is written to `~/.apogee/sessions/` (asynchronously, best-effort), so a
crash or `kill -9` costs at most the turn in flight. A turn that hands work to a
sub-agent is saved as that work runs — a **progress save** fires when the
delegation is issued and each time the sub-agent finishes a tool — so a long
delegation is on disk while it happens, not only once its turn ends. A saved
session stores the engine's conversation **and** the TUI scrollback, so
resuming repaints the transcript you actually saw — tool cards included — and
relights the context gauge, instead of opening an empty view over a model that
still remembers.

- `apogee --continue` resumes this workspace's most recent session; `--resume`
  takes a session id (from `/sessions`) or a file path.
- `/sessions` opens the in-TUI browser (newest first): typing filters the list,
  `⏎` resumes, `^r` renames inline, `^d` deletes after a confirm, `^a` toggles
  between this workspace and all workspaces. The verbs are chords precisely so
  the letters are free to type with — every selector pop-up filters as you type.
  A new session names itself: on its first prompt apogee asks the
  model, in a single call off to the side of the conversation, for a short title
  (`auto-title:`, a file-only key, on by default). With that off — or when the
  call fails or answers with nothing usable — the title falls back to the first
  user message, or to a dated `Session <date>` when that message is empty or
  opens a code fence. A bare `/rename` later re-reads the session — your opening
  request plus the most recent ones — and names it for what it has become, so
  one that moved on to another task gets named for where it ended up.
- A run of a `/schedule` saves its own session, so it browses like every other:
  the browser tags it `⟳ <schedule>` beside its title, so a run reads as one of a
  series rather than as a session nobody remembers starting. Ordering, resume,
  rename and delete treat it exactly like a session you held yourself.
- `/clear` (or `/new`) closes the current session into history and starts a fresh
  one — neither deletes; discarding is an explicit `^d` in the browser.
- A session killed mid-task resumes to the last completed turn and says so;
  `/continue` then picks the unfinished work back up, while sending a new message
  instead discards it and continues fresh. A delegation that was still running when
  the session was written comes back marked **interrupted**, with a note saying the
  sub-agent's unfinished work was not kept: `/continue` re-runs the step that
  started it, and a new message discards it.
- The session's **name is written on the top rule**, the hairline above the status
  line — `▔▔▔▔ the name ▔▔▔▔` — so a screen full of panes says which conversation
  each one is. It shows whatever named the session, from `/rename` or from the automatic
  naming call, and a session with no name yet shows a plain unbroken rule. Nothing
  needs configuring in your terminal for it: it is a row apogee paints itself.

Autonomy mode, tool approvals, confinement, and MCP connections are deliberately
**not** part of a saved session — they are re-established or re-confirmed on
resume, so yesterday's approvals never silently apply to today's run.

