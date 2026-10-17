---
title: ADR 0096 decision
---

**A project keeps its settings in `<Project root>/.apogee/config.yaml`, which may be committed. Any
entry in it that grants something is inert until the user adopts it; adoption is recorded outside
the repo, pinned to that entry's exact content, so the pin — not a write refusal — is the guarantee.**

1. **Project root.** The nearest folder holding a `.apogee/`, walking up from the workspace to — never
   past — its git top-level; the workspace itself outside a repo or when none is found. Never `$HOME`
   (its `~/.apogee` is the global config). The workspace `.apogee/skills` anchor uses the same root.
2. **Allow rule.** A persisted yes that answers an ordinary Approval. For `terminal`: a word prefix
   (`go test`) that **every** simple command of the line must match, split by the existing shell
   reader. Any file redirect, `$(…)`, backtick, heredoc, unparseable line, wrapper (`sudo`, `env`,
   `timeout`, …) or `NAME=value` prefix makes the line ask; `cd` into a folder inside the workspace
   counts as matched, any other `cd` asks. No wildcards. Never applies on Windows `cmd` (no parser).
   For MCP: a whole server. File edits get no rules — `allow-edits` is that choice. A **project** rule
   lives in the project config and is the approval pane's default; a **global** rule lives in
   `~/.apogee/config.yaml`, is written by hand and is live as written (it is the user's own machine).
3. **Where a rule answers.** The ordinary gate of ask-before and allow-edits, and Auto's gate for a
   call it cannot confine. Never a refusal (Plan, Tier 1), never a forced look (Tier 2 — dangerous
   actions, `~/.apogee` writes), and a Reaction's `ask` or `deny` still wins. The engine applies rules,
   so every Driver gets the same answer (ADR 0031) — though today no unattended Driver reaches an
   ordinary gate (headless and the daemon run only Plan or a fenced Auto, and dial no MCP server),
   so rules fire only in the TUI until one does. **Every call a rule lets through emits
   an audit event and a visible line naming the rule and its layer** — the answer to D2's "second,
   unaudited autonomy ladder": the ladder is audited, and only the user's own answers build it.
4. **Adoption.** apogee writes the project config only on the user's answer — the approval pane's
   "Always in this project…", `/settings`, the adoption pane — and on each write records the
   SHA-256 of every granting entry in `~/.apogee/workspaces/<sha256 of the Project root path>.yaml`.
   At load, a granting entry whose fingerprint is not recorded is *proposed*: inert, and offered at
   startup (or at the next turn boundary when the file changes mid-session) as adopt / not now /
   reject. A rejection is pinned too and stays quiet until the entry changes. Unattended Drivers leave
   proposals inert and say nothing (a rule cannot fire there); `apogee project adopt` adopts from a
   terminal. The daemon reads no Project config at all: it resolves settings once from its own start
   folder and every firing reuses them, so one repo's project layer would leak into another's
   firings — per-firing layering is a follow-up.
   A fresh clone, or the same repo at another path, asks once.

   > *Amendment (2026-10-17, `adr-0096-amend-1017`):* the daemon now layers each Firing's own
   > workspace's Project config, read fresh at that Firing, over a copy of its startup Options; the
   > startup resolve stays global-only and read once. The daemon sentence above is superseded on that
   > point alone.

5. **Write refusal.** File tools are refused — Tier 1, every mode, no look — on writing, moving or
   deleting `<Project root>/.apogee/config.yaml`, and on deleting, moving or replacing `.apogee/`
   itself. The shell reader refuses the forms it can see. `.apogee/skills/` stays workspace territory.
   This is the footgun layer; the pin holds even where it is evaded.
6. **Key classes.** Every config key carries one; the default is **global-only**, so a new key is
   safe until someone marks it otherwise.
   - *Granting — adoption-gated:* Allow rules.
   - *Tighten-only — live:* `dangerous-rules:` additions, `tools.disabled` additions,
     `url-safety.deny-hosts` additions. The global layer may add **or remove** dangerous rules by ID
     (`security.MergeDangerousRules` is finally fed); the project may only add, and only stricter.
   - *Project parameters — live:* keys that describe the project and widen nothing (context-file
     names, `use-project-skills`, workflow limits — the registry marks each).
   - *Global-only — ignored from a project, with a notice:* everything else, including `mode`,
     Bypass, `confine-to-workspace`, `unconfined-hosts`, servers and API keys, `editor`,
     `system-prompt-file`.
7. **Precedence and `/settings`.** default < global < project < env < flag; tighten-only keys union
   instead. Each `/settings` row shows its source (`default | global | project | env | flag`). An
   edit to a project-capable key asks *global or this project*; global-only keys write global as
   today. A new *Allow rules* section lists every rule with its layer and state (adopted, proposed,
   rejected) and removes or adopts them.
