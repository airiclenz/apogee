---
Status: accepted
---

# Documents are presented, not opened: one tool, a host-side presentation ladder

## Context

A Skill that produces a report — an architecture review, a research summary, a migration plan —
ends with a file on disk the user never sees. `write_file` renders as a one-line
`Write File <path> +N bytes` card in the transcript and nothing more, so the deliverable an
Exchange just spent its whole Budget producing is, from the user's seat, invisible. The model's
remaining moves are to paste the document back into chat (which defeats writing it to a file) or
to guess at a shell command that might display it — and platform guessing is exactly what a
~4B–35B model is worst at. Apogee's premise is that small models need single, dumb, explicitly
named affordances; "show this document to the user" is one such affordance and does not exist.

The obvious implementation — shell out to an editor or `xdg-open` — is wrong the moment Apogee is
not running on the user's desktop, which for this project is a *normal* case, not an edge. These
facts were established live in the owner's devbox (2026-07-21) and are the ground this decision
stands on:

- Apogee's primary remote context is a **Zed SSH-remoted terminal** (`TERM_PROGRAM=zed`,
  `SSH_CONNECTION` set). Zed ships **no `zed` CLI on remotes** (zed-industries/zed discussions
  #32214, #33601 — confirmed still true June 2026), so the VS Code-style `code <file>` shell-out
  has no analogue there.
- Zed's terminal makes **file paths in output cmd+clickable** (opening in the editor through the
  remote server) and **URLs cmd+clickable** (opening the *host's* default browser). Both work on
  **plain text** — no OSC 8 needed, and plain text is the more portable form (iTerm2, WezTerm,
  kitty and VS Code's terminal all detect it too).
- An HTTP server bound in the devbox on `0.0.0.0` **is reachable from the host** at the
  server-side IP carried in `$SSH_CONNECTION`, proven with `python3 -m http.server` and a host
  browser.
- **Host-side policy can still block that URL invisibly**: macOS Local Network permission
  (Sequoia+) made Chrome fail with a generic "site can't be reached" while Safari worked on the
  same URL. Every mechanism above the baseline must therefore **fail visible**, and the baseline
  must always be printed.
- The owner **rejected any host back-channel** (reverse SSH, a host-side helper daemon, path
  mapping over a shared mount as a required rung) on security and dependency grounds.

## Decision

**1. One tool; the host decides the mechanism.** The model calls
`present_document {path[, title]}` after writing a deliverable, and that is the whole of its
platform reasoning. The tool routes through a host-supplied **`domain.Presenter`** delegate —
the exact pattern of `ask_user`/`domain.Asker`: mode-**independent** (it never routes through the
Approval gate), `ReadOnly() == true` so it runs in **every** mode including Plan
(presenting writes nothing), **not** a safety gate, and **not** an `ExternalEffectTool` — the
user's own display is not a non-forkable remote the bench must stub, any more than the human
answering `ask_user` is. A **nil `Presenter` means the tool is not registered**, so a headless
host never offers the model an affordance it cannot honour.
[ADR 0008](0008-stateless-tools-and-non-forkable-external-effects.md)'s statelessness contract
holds unchanged: the tool holds a **delegate reference, never a live handle**, and the doc server
below is owned by the **host** process with a lifetime tied to the app, not to a Turn — so
nothing live crosses the quiescent boundary and a resumed or forked run inherits no dangling
listener.

**2. The presentation ladder** — host-side, evaluated per call; the highest applicable rung runs
*in addition to* rung 0, never instead of it:

- **Rung 0 — baseline, always.** A prominent presentation entry in the transcript carrying the
  workspace-relative path as plain text on its own line. It is **never skipped**, even when a
  higher rung succeeds, because it is the rung that is never wrong: cmd+clickable in Zed / VS
  Code / iTerm2 / WezTerm / kitty, copyable everywhere else.
- **Rung 1 — local desktop ⇒ auto-open.** When the session is **local** (no `SSH_CONNECTION` /
  `SSH_TTY` / `SSH_CLIENT` in the environment) **and** a desktop exists (darwin and windows:
  always; linux: `DISPLAY` or `WAYLAND_DISPLAY` set), the host invokes the OS opener — `open`,
  `cmd /c start "" <path>`, `xdg-open`. HTML lands in the default browser, everything else in its
  OS-associated app. This is the headline behaviour on a user's own machine.
- **Rung 2 — remote + browser-renderable ⇒ serve and print the URL.** For `.html`, `.htm`,
  `.svg`, `.pdf` the host registers the file with an embedded **doc server** and adds the URL to
  the presentation entry, again as plain text on its own line. The user's cmd+click opens it in
  the *host's* browser — the reachability the devbox probe established, with no back-channel.
- **Rung 3 — config override.** `present.command` (a template containing `{path}`) **replaces**
  rung 1's OS opener when set, on every OS, for users who want one specific application. It
  replaces rung 1's *mechanism*, not rung 1's gate on **locality** (clarified 2026-07-21): the
  ladder asks only whether the session is local, because `present.command` says which application
  shows a document, not which machine the user is sitting at. Whether this machine has anything to
  open into is the **opener's own** answer (its `ErrNoOpener`), and a configured command **stands
  in for that desktop test** — an OS with no built-in opener is precisely the case the override
  exists for. So the desktop check lives in exactly one place, and a set `present.command` opens on
  a local box with no *detected* desktop but never on a remote one.

**3. The doc server is a capability-token allowlist, not a file server.** It serves **only**
explicitly presented files, each under a random token at `/d/<32-hex>/<basename>`: no directory
listing, 404 for everything else including prefix walks and `..`, content-type by extension, the
file **re-read from disk per GET** (so re-presenting after an edit shows fresh content), started
**lazily** on the first served presentation and closed on app shutdown. Its advertised address is
the server IP from `$SSH_CONNECTION`, else the `present.host` override, else an outbound-dial
probe for the local address (no packets need to arrive), else `127.0.0.1`; its port is
`present.port`, default **0** (ephemeral) — the URL is printed fresh per presentation, so a
stable port buys nothing.

**4. Fail visible, degrade to rung 0 — a presentation never fails the tool call.** An opener that
errors, a server that cannot bind, an undetectable desktop: none of these produce a tool error,
because rung 0 already happened host-side. The transcript entry says what happened ("no opener
available — path shown"), and the **tool result names the outcome** — `opened`, `served`, `shown`
— so the model can tell the user the truth ("opened in your browser" vs. "the path is shown in
the transcript") instead of asserting a success it cannot observe. Only a path that escapes the
workspace or is not an existing regular file is an error result.

**5. The opener runs host-side, outside tool confinement — and that is deliberate.**
[ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md) attaches
confinement to **blast radius** at the **subprocess granularity** the model's tools reach through
dispatch. The opener is not one of those: it is the **host's** act, in the host process, on the
user's own desktop session, launching the user's own browser or editor — the same category as the
TUI drawing on the terminal, not the category a workspace fence exists to bound. Confining it
would be nonsense (a browser fenced to the workspace cannot run) and would not buy safety,
because the blast radius here is bounded **by what can be presented, not by fencing the opener**:
the path is resolved inside the workspace root and must be an existing regular file before any
rung runs, the model never supplies a command, and `present.command` is the **user's own**
configuration — global config, the same standing as their shell. ADR 0012's invariant is
untouched: nothing here runs a *model-chosen* command, unsupervised or otherwise.

## Considered options

All of these were rejected; the ladder above is what survived.

- **A host back-channel** — reverse SSH tunnel, a host-side helper daemon, or path mapping over a
  shared mount as a *required* rung. **Rejected by the owner on security and dependency grounds**
  (2026-07-21) and out of scope permanently unless the owner reopens it: it turns a display
  affordance into a privileged channel into the user's machine. A `file://`-over-shared-mount
  **optional** rung was discussed and deliberately deferred, not adopted.
- **Shell out to the editor CLI** (`code <file>`, `zed <file>`) as the remote rung — rejected: Zed
  has no CLI on remotes at all, so the *primary* remote context cannot honour it, and an
  editor-specific command is a per-user preference, which is what rung 3 is for.
- **Separate tools per mechanism** (`open_document` + `serve_document`) letting the model pick —
  rejected: it puts platform detection back in a ~4B–35B model, which is the failure this tool
  exists to remove. One name, one meaning, host decides.
- **OSC 8 hyperlinks** — rejected: plain text is detected by more terminals than OSC 8 is rendered
  by, and a mangled escape sequence degrades worse than a path does. Revisit only with evidence.
- **Serving the whole workspace** from the doc server — rejected: a per-file capability token is
  the smaller, auditable grant; a workspace-rooted server is an exfiltration surface on a box the
  user may share.
- **Auto-opening on the remote box** (`xdg-open` inside a headless VM) — rejected: it fails or,
  worse, half-succeeds into a display nobody is looking at. Rung 1 is gated on a *detected*
  desktop for this reason.

## Consequences

- **A new host delegate joins `Approver`/`Asker`/`Confiner` on `domain.Config`.** `Presenter` is
  additive public surface (nil ⇒ the tool is unregistered), so the bench and any headless
  embedder are unaffected by construction, and the change is a **minor** bump.
- **A new `internal/present` package** carries the host-side mechanisms (locality/desktop
  detection, the OS opener, the doc server) as injectable-seam code the TUI wires and any embedder
  may reuse. It imports the standard library plus `shlex` — the POSIX splitter the `terminal` tool
  already uses — so the one place a user's command line is parsed (`present.command`) behaves
  identically wherever it appears; it adds **no new dependency**. Under
  [ADR 0010](0010-package-layout-domain-core-and-thin-root-facade.md) it depends on
  `internal/domain` downward only, never the root facade.
- **The transcript grows a first-class presentation entry**, visually distinct from tool cards —
  a deliverable is not plumbing. Path and URL are rendered as **plain text on their own lines**,
  never wrapped in markup and not split mid-token if avoidable, because terminal linkification is
  the whole mechanism.
- **A file-only `present:` config block** (`auto-open`, `command`, `port`, `host`) tunes rungs 1–3.
  `auto-open: false` disables rung 1 and **never** rung 0. The shipped template documents the
  macOS Local Network gotcha as the first thing to check when a served URL is unreachable.
- **Skills stay user-authored** ([ADR 0002](0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)):
  nothing here edits a builtin skill. The guidance that a report-producing skill should end with
  `present_document` is documentation, recorded in `TODO.md`.
- **CONTEXT.md gains the vocabulary** — *Present / Presentation*, *Presenter*, *presentation
  ladder*, *doc server* — worded to match this ADR.
- **Deferred, kept additive:** a markdown→HTML rendering rung (the doc server stays
  extension-agnostic so it can be added without reshaping the ladder), and port-forward
  integration (Zed `port_forwards`, VS Code auto-forwarding) — the direct-IP URL covers the
  primary topology.
- **The Windows opener ships unexercised** until the merge plan's Phase 5 (Windows shell/path
  backend) provides a real Windows harness — the same posture as `internal/platform`'s Windows
  stub, and stated rather than hidden. (2026-07-22: Phase 5 shipped; the live opener check is
  folded into the owner-run smoke passes.)
