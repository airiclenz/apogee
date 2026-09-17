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
  are redacted from their output. A key that shows up where it should not is in scope.
- **Prompt-injection escalation** — text a model reads (a file, a tool result, a fetched page,
  a skill) that gets apogee to skip a guard it would otherwise apply. Injection that merely
  makes the model do something the current mode already allows is not a vulnerability;
  injection that crosses a mode or fence boundary is.

Out of scope: a model behaving badly within its permitted mode, denial of service against
your own machine, findings that require a compromised `~/.apogee` or a hostile local user,
and third-party model servers or MCP servers themselves.

## Reporting

Please do not open a public issue for a vulnerability. Use GitHub's private reporting —
**Security → Report a vulnerability** on the repository — with the apogee version
(`apogee --version`), the platform, the autonomy mode, and a reproduction. You will get an
acknowledgement, a fix on `main` and a release; credit in the changelog if you want it.

The security-relevant design decisions are recorded under [`docs/adr/`](docs/adr/), and the
confinement contract in
[`docs/design/confinement-execution-contract.md`](docs/design/confinement-execution-contract.md).
