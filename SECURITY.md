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
  bind the web tools and MCP endpoints; redirects are never followed. An SSRF past them is in
  scope.
- **Secrets** — API keys are stripped from the environment of every tool subprocess and
  reaction command, and the variables your `api-key-env:` and `headers-env:` entries name
  are redacted from their output. A stdio MCP server is the exception — it inherits apogee's
  full environment unless its entry sets
  [`env-allowlist:`](docs/manual/configuration.md#external-mcp-servers--mcp-servers) (see
  [The upstream API key](docs/manual/configuration.md#the-upstream-api-key)). A key that
  shows up where it should not is in scope.
- **Prompt-injection escalation** — text a model reads (a file, a tool result, a fetched page,
  a skill) that gets apogee to skip a guard it would otherwise apply. Injection that merely
  makes the model do something the current mode already allows is not a vulnerability;
  injection that crosses a mode or fence boundary is.

Out of scope: a model behaving badly within its permitted mode, denial of service against
your own machine, findings that require a compromised `~/.apogee` or a hostile local user,
and third-party model servers or MCP servers themselves.

**The Windows confinement journal.** On Windows the box is a mandatory label on the disk, and
`~/.apogee/confinement` holds the journal that undoes it after an interrupted run. That
directory is a protected root: a box root that is, or contains, it is refused, so a confined
child can never write the record of its own labels. A revert trusts the journal only as far as
it can check it: each entry is bound to the file identity (volume serial and file index) of
the object it labelled, and a label is neither cleared nor restored on an object whose identity
no longer matches; a journal's owner counts as still running only while both its PID and its
process creation time match, so a recycled PID cannot keep a dead run's labels in place. A
same-user, Medium-integrity process that forges a journal is out of scope — it can forge
anything it can read, which is the hostile-local-user case above.

**A checkout's own git hooks are a trust decision, not something apogee fences.** This
repository ships git hooks under `.beads/hooks/`, and the documented hydration step —
`bd init` or `bd hooks install` — activates them by pointing git's `core.hooksPath` at that
checkout-controlled directory. From that moment your next `git commit`, `git checkout` or
`git push` runs shell out of the repository with your full user privileges. Those hooks are
git's to run and the repository's to write; nothing in apogee vets, pins or sandboxes them,
and no guarantee above applies to them. So hydrating a checkout means trusting its
`.beads/hooks/` exactly as you trust any other code you choose to run: read those files
before you hydrate a clone of a fork or of a repository you do not control, and do not
hydrate one you would not execute. This is out of scope as a vulnerability report against
apogee. (apogee's own git tool calls run git with `core.hooksPath=` blanked, so no
repository hook runs inside one — that fences the tools, not your shell, and not the
hydration decision.)

## Reporting

Please do not open a public issue for a vulnerability. Use GitHub's private reporting —
**Security → Report a vulnerability** on the repository — with the apogee version
(`apogee --version`), the platform, the autonomy mode, and a reproduction. You will get an
acknowledgement, a fix on `main` and a release; credit in the changelog if you want it.

The security-relevant design decisions are recorded under [`docs/adr/`](docs/adr/), and the
confinement contract in
[`docs/design/confinement-execution-contract.md`](docs/design/confinement-execution-contract.md).
