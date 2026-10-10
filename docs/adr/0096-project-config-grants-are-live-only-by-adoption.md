---
Status: accepted
Amends: ADR 0076 (decision D10 and amendment A2 — this ADR is the stage-2b grill A2 deferred to; it closes A2's gaps (a)–(d) for the keys it admits), ADR 0012 (the project layer may only tighten — except an **Allow rule**, which is a grant the user made and is live only by **Adoption**), ADR 0032 (the workspace skill anchor becomes the **Project root**)
---

# Project config grants are live only by adoption

## Context

apogee has one config file, `~/.apogee/config.yaml`, and every approval it remembers lives in process
memory: ask-before re-asks every command on every relaunch (bead `apogee-zlg`). Rival agents keep a
settings folder in the repo (`.claude/`, `.opencode/`, `.crush/`), commit it if they like, and put
a one-time "do you trust this folder?" prompt in front of it. The owner wants the repo as the home
for project settings — so they can be committed — and asked for the design on 2026-10-10 over
`apogee-089`, `apogee-zlg` and the terminal command-allowlist slice of `apogee-9d8`.

Two facts make the rivals' shape unsafe here as it stands:

- **The model writes the workspace.** In `allow-edits` and `auto` a tool edits workspace files without
  a look. A live grant file in the workspace is a file the model can grant itself through. File
  tools can be refused there, but shell commands cannot be fenced completely — the shell reading is a
  footgun-guard, not a boundary (ADR 0012) — so `python -c "open('.apogee/config.yaml','w')…"` gets past
  any refusal.
- **Folder trust is one yes for every later state of the folder.** A `git pull` that adds `allow: curl`
  after the user trusted the folder is never shown to anyone. ADR 0076 D10 already ratified the
  stronger shape — a repo entry that can act is *proposed* until adopted, adoption pins the entry,
  an edit re-proposes — but A2 pulled it from stage 2 because the pieces did not exist.

## Decision

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

## Considered options

- **Rules only under `~/.apogee`, nothing in the repo.** Simplest and needs no pin, but nothing can
  be shared or committed, which is what the owner asked for.
- **A live repo file behind a one-time folder-trust prompt** (the rivals' shape). Rejected for the two
  facts above: the model can write the file, and trust never re-asks after a pull.
- **Exact-command replay** (persist today's session digest). Safest, but `go test -run TestY` would
  re-ask; it does not remove the annoyance the bead names.
- **Git remote URL as the project key.** The repo controls its own remote, so a hostile clone could
  claim another repo's adoptions.

## Consequences

- Allowing `go test` or `make lint` runs whatever the repo's code does, and in `allow-edits` the model
  may edit that code. Every agent with an allowlist has this; the manual states it beside the feature.
- **Not in scope, filed separately:** MCP servers and Reactions in the project config (both execution
  keys D10 admits as adoption-gated); the sandbox loosens `NetworkAllow` and `confine-writable-paths`;
  the rest of the `apogee-9d8` tool × mode matrix.
- The AGENTS.md "single `~/.apogee` dotdir" decision stands for the **global** config home; the
  project layer is the stage-2b reopening A2 reserved.
