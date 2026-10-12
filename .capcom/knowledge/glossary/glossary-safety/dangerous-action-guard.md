---
title: "Dangerous-action guard: the footgun guard refusing a small model's catastrophic mistakes"
---

**Dangerous-action guard**:
A **footgun-guard — *not* a security boundary** — that refuses a small model's obvious
catastrophic *mistakes* before execution, in **every** mode independent of Confinement (ADR 0012;
lives in `internal/security`, P3.6). Two tiers: **hard-refuse** (`rm -rf` of any absolute path,
the project's own directory included — only a relative target passes; fork bombs; writes to
`~/.ssh`/credential/persistence files or to a repository's git control plane (`.git/hooks`,
`.git/config`, `.git/modules`); a raw `dd` write to a block device — no per-call override) and
**force-approval** (`curl | bash`-class — sometimes a legit installer — `sudo` of any command, a write under
`~/.apogee`, apogee's own control plane, which the operator legitimately curates by hand
(ADR 0049 §4) — except the session's own scratch dir, which the box already declares writable
(ADR 0049 amendment 2026-08-28), and `commit-secrets` — a `git_commit` whose shadow-staged diff or
staged file names carry secret material (ADR 0080): a speed-bump that forces the Approver even in Auto, carrying the rule's way out to
the prompt and to a denied call's result; a forced look on a call Auto would have confined stays
confined once allowed — approval decides *whether*, confinement *where*). Rules match the call's **action text** — the tool, its target paths, its
command lines and code — and never the **payload** a call carries (a file body, a replacement string,
a search pattern, a commit message), so writing or grepping a document that merely *quotes* `~/.ssh`
is not an action. The payload exemption is **per tool, never by key name**: only an argument the
calling tool itself declares with the payload role (`domain.ArgRolePayload`), and only at the top
level of its arguments, is skipped — an MCP tool declares none, so every argument it is handed,
nested values included, is inspected in full (ADR 0012 amendment 2026-09-26). `commit-secrets` is the one **content-derived** member: its evidence is what git
would stage, precomputed by dispatch into a shadow index before the pure resolution runs (D6), never
the call's arguments. It is **tighten-only** and trivially bypassable by anything determined,
so it **never** makes `confine-to-workspace=false` "safe" — only the VM does. Default-on, and
user-configurable through the **`dangerous-rules:`** key (ADR 0096 §6): `add:` entries (`id`,
`pattern`, `tier: ask|refuse`, `reason`) and `remove:` IDs. The add/remove split — a global config
may add *or* remove rules by ID (it is the user's machine), a **Project config** may only *add*, and
only stricter — is the rule of the ADR 0012 merge seam (`security.MergeDangerousRules`), which
`dangerous-rules:` feeds.
_Avoid_: "malicious-action filter", "blacklist", "denylist" (all imply an adversary boundary it is
not — it guards against mistakes, not attackers).
