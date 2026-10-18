---
title: ADR 0104 decision
---

**A Project config may propose `mcp-servers:` and `reactions:` entries; each is inert until adopted.
Adoption pins a canonical form of the fields that decide what runs or where data goes, together with
the content of every project file the entry's command names.**

1. **Both keys become granting.** `mcp-servers:` and `reactions:` move from global-only to the
   granting class of ADR 0096 §6. A repo entry is proposed, adopted or rejected exactly as an Allow
   rule is — adopt / not now / reject, a rejection quiet until the entry changes, "not now" asked
   again at the next start. Their Adoption kinds are `mcp-server` and `reaction`, distinct from the
   Allow kind `mcp-servers`.
2. **What the pin covers.** For a reaction: `id` and its `run:`, `advise:` and `gate:` values.
   `on:`, `timeout`, `enabled` and `workspace:` are parameters (ADR 0076 decision 10) and change
   without asking. For an MCP server: the whole entry, since every field of it decides what runs,
   where it connects or what it sends.
3. **The pin is canonical.** The fingerprinted text is canonical JSON of the pinned fields (sorted
   keys, no insignificant whitespace), so reformatting, reordering keys or adding a comment never
   re-proposes; only a changed value does. Allow rules keep their exact-text pin.
4. **Project files the command names are pinned by content.** A repo entry may run a program inside
   the Project root (decision 6's program fence does not refuse it once adopted). Every argv element,
   the program included, that resolves to an existing regular file inside the Project root has its
   path and the SHA-256 of its content folded into the pin. A changed script re-proposes the entry,
   whoever changed it. A named file that has gone missing also re-proposes. A repo entry's relative
   argv and its working directory resolve against the Project root, and the pinned digests are
   re-checked before every spawn: a mismatch refuses that run with a notice and re-proposes the entry.
5. **The user's own entry wins.** A repo entry whose `name` (server) or `id` (reaction) matches a
   global one is dropped with a notice; it never overrides, and is never offered. This replaces
   decision 10's "later layers overriding by key" for these two keys.
6. **A repo MCP server starts minimal.** With `env-allowlist` unset, a repo stdio server gets the
   platform floor only (the `[]` behaviour); the repo lists every variable it needs, and those
   names are part of the pin and shown on the pane.
7. **A repo reaction stays in its repo.** The layer confines it: a repo entry exists only in
   sessions under that Project root, so an unset `workspace:` needs no filter. A spelled
   `workspace:` must sit inside the root (a relative one resolving against it); one outside refuses
   the entry with a notice. A refused repo entry — of either kind — is dropped alone; it never costs
   the rest of the Project config.
8. **An adopted repo `run:` is fenced.** It runs inside the workspace exec fence under the same
   permit as `advise:` and `gate:` when `confine-to-workspace` is on, and fails, as they do, on a host
   that cannot confine. With the fence off it runs as a global `run:` does.
9. **Otherwise adoption confers the global trust.** An adopted repo entry keeps the endpoint and
   webhook exemption from the resolved-IP floor and may use `headers-env` and `client-secret-env`;
   the pane names the endpoint and every variable it sends, and that is the gate.
10. **Only project rules cover a repo server.** A global `allow.mcp-servers` rule never covers a
    repo-defined server, even by name; a rule adopted in that project does, as does a session
    approval.
11. **The adoption pane shows what it asks.** For these kinds it gains a body listing the pinned
    fields verbatim — command and args, transport and endpoint, env and `env-allowlist` names,
    `headers-env` and `client-secret-env` names, the action argv or webhook URL — and on a re-ask
    names what changed (a field, or a file "changed since you adopted it"). No diff.
12. **Every Driver sees the same set.** Live reload reads the layered config. Headless and daemon
    Firings fire an adopted repo reaction, skip a proposed one with one notice, and never prompt;
    `apogee project adopt` answers from a terminal. `apogee mcp` lists repo servers with their
    state; `apogee mcp login` refuses one until it is adopted. `/settings` shows repo entries
    read-only, tagged `project` with their state, with adopt and reject; editing stays in the file.
    The scrub of `headers-env` names from tool subprocesses is recomputed on reload, so an entry
    adopted mid-session is covered without a restart.
