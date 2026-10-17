# Security policy

apogee runs a model with a shell, a file system and the network at its disposal, so its
security surface is the thing the project is built around. This page says what counts as a
vulnerability, which versions get fixes, and how to report one privately.

## Supported versions

apogee is pre-production `0.x`. Fixes land on `main` and ship in the next release; only the
latest release on the [releases page](https://github.com/airiclenz/apogee/releases/latest)
is supported. There are no backports.

## What counts

Anything that lets a model, a tool result, a skill, an MCP server or a web page do what the
documented posture says it cannot. The guarantees apogee makes, in the order they matter:

- **Auto-mode confinement** — an unsupervised run cannot write outside the workspace and its
  scratch directory (Linux landlock, or user + mount namespaces via `bwrap` where the kernel
  has no landlock; macOS seatbelt; a restricted Windows token). An escape
  from the fence, on any platform, is the highest-severity report there is. See
  [Auto mode's blast radius](docs/manual/configuration.md#auto-modes-blast-radius).
- **The dangerous-action guard** — the refused and prompted command shapes documented under
  [the dangerous-action guard](docs/manual/configuration.md#the-dangerous-action-guard). A
  spelling that slips past a Tier-1 refusal or a Tier-2 prompt is in scope.
- **Approval scoping** — an approval covers the call you approved, resolved to the path the
  prompt showed you, and nothing else. A call that runs under someone else's approval is in
  scope.
- **Network fencing** — the `url-safety:` allow and deny lists and the private-range floor
  bind the web tools and MCP endpoints; redirects are never followed. A webhook reaction
  (`run:`, `advise:` and `gate:` alike) obeys the same allow and deny lists and never follows a
  redirect, but — like an MCP endpoint, an address you wrote yourself — skips the private-range
  floor, so a loopback or LAN endpoint still works; a running session's own webhooks pick up a
  `url-safety:` edit at its next start, while a `/schedule` firing raised after the edit already
  uses the new lists. An SSRF past them is in scope.
- **Secrets** — API keys are stripped from the environment of every tool subprocess and
  every `advise:` or `gate:` reaction command, and the variables your `api-key-env:` and
  `headers-env:` entries name are redacted from their output. Two exceptions inherit
  apogee's full environment: a stdio MCP server, unless its entry sets
  [`env-allowlist:`](docs/manual/configuration.md#external-mcp-servers--mcp-servers), and a
  `run:` reaction command — your own notifier, whose output never reaches the model (see
  [The upstream API key](docs/manual/configuration.md#the-upstream-api-key) and
  [reactions.md](docs/manual/reactions.md)). A key that shows up where it should not is in
  scope.
- **Prompt-injection escalation** — text a model reads (a file, a tool result, a fetched page,
  a skill) that gets apogee to skip a guard it would otherwise apply. Injection that merely
  makes the model do something the current mode already allows is not a vulnerability;
  injection that crosses a mode or fence boundary is.

Out of scope: a model behaving badly within its permitted mode, denial of service against
your own machine, findings that require a compromised `~/.apogee` or a hostile local user,
and third-party model servers or MCP servers themselves. The one exception to the hostile local
user is the session sharing below: once `sessions.share-group` or `sessions.watch-homes` is set,
the other user on the far side of it is inside the threat model for those files.

**The Windows confinement journal.** On Windows the box is a mandatory label on the disk, and
`~/.apogee/confinement` holds the journal that undoes it after an interrupted run. That
directory is a protected root: a box root that is, or contains, it is refused, so a confined
child can never write the record of its own labels. A revert trusts the journal only as far as
it can check it: each entry is bound to the file identity (volume serial and file index) of
the object it labelled, and a label is neither cleared nor restored on an object whose identity
no longer matches; a journal's owner counts as still running only while both its PID and its
process creation time match, so a recycled PID cannot keep a dead run's labels in place; a
record written before creation times were journalled carries none, and for it the PID check
alone decides. A
same-user, Medium-integrity process that forges a journal is out of scope — it can forge
anything it can read, which is the hostile-local-user case above.

**Session records and Event journals, and watching them.** A session record holds what the
session saw, unscrubbed: every tool argument and result, so every file the agent read and every
command's output. While a session runs, the Event journal beside it
(`~/.apogee/sessions/<id>.events.jsonl`, which [watch mode](docs/manual/sessions.md#watching-a-live-session)
follows) holds the same and more — the streamed answer, the model's thinking and each prompt —
and is just as unscrubbed. It is not the Windows confinement journal above. Both are
owner-private by default (`0700` directories, `0600` files), and the Event journal is removed when
its session is released. What a watch costs the owner is nothing: the owner writes the journal
whether or not anyone reads it, never learns of a reader, and nothing flows back from one to it.

- **`sessions.share-group` deliberately admits that group's members as readers.** With it set,
  the apogee home becomes `0710` — traversable by the group, not listable — `sessions/` becomes
  `0750`, and every record, Event journal and `.lock` file in it `0640`, owned by the group.
  A member reads everything those sessions saw, secrets that passed through a tool result
  included, so name a group of people you would show your terminal to. Because the home is now
  traversable, any file under it whose own mode grants the group is reachable too: apogee warns
  at startup when `config.yaml` is group-readable, since it may hold a plaintext `api-key:`.
  Read access to a `.lock` file is also enough to take a lock on it, so a member can keep the
  owner from holding one of its sessions — a denial of service the group is trusted not to
  commit. Scratch, snapshots and the rest of the home stay owner-private. That a member can
  read the shared files is the design, not a vulnerability; a member reaching anything beyond
  them, or writing to the owner's home, is in scope. Removing the key re-privatises records
  and journals on their next write but leaves the directories and existing `.lock` files as
  they were (the [sessions page](docs/manual/sessions.md#watching-another-users-sessions) says
  how to close them at once).
- **A watched home's bytes are untrusted.** A home named under `sessions.watch-homes` belongs to
  another user, who controls every byte of its records and journals. apogee only reads it,
  through a read-only store: it never creates, writes, sweeps, truncates or holds anything
  there — asking whether a session is live is a momentary shared lock that the owner's hold
  waits out — never resumes a session from it, and reads no config or skill from it; what it paints
  passes through the same escape stripping as any other untrusted text. A crafted record or
  journal in a watched home that makes the watcher's apogee write, run, or reach the terminal
  with an escape sequence is in scope. Both keys are global-only, so a repository's own config
  can never choose whose sessions you read or widen who reads yours.

**A checkout's own git hooks are a trust decision, not something apogee fences.** This
repository ships git hooks under `.githooks/`, and the documented activation step —
`git config core.hooksPath .githooks` — activates them by pointing git's `core.hooksPath` at
that checkout-controlled directory. From that moment your next `git commit`, `git checkout` or
`git push` runs shell out of the repository with your full user privileges. Those hooks are
git's to run and the repository's to write; nothing in apogee vets, pins or sandboxes them,
and no guarantee above applies to them. So activating a checkout's hooks means trusting its
`.githooks/` exactly as you trust any other code you choose to run: read those files
before you activate them in a clone of a fork or of a repository you do not control, and do
not activate them in one you would not execute. This is out of scope as a vulnerability
report against apogee. (apogee's own git tool calls run git with `core.hooksPath=` blanked, so
no repository hook runs inside one — that fences the tools, not your shell, and not the
activation decision.)

## Reporting

Please do not open a public issue for a vulnerability. Use GitHub's private reporting —
**Security → Report a vulnerability** on the repository — with the apogee version
(`apogee --version`), the platform, the autonomy mode, and a reproduction. You will get an
acknowledgement, a fix on `main` and a release; credit in the changelog if you want it.

The security-relevant design decisions are recorded as [decision records](.capcom/knowledge/decisions.md), and the
confinement contract in
[`docs/design/confinement-execution-contract.md`](docs/design/confinement-execution-contract.md).
