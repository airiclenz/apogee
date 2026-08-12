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

## Amendment (2026-07-26) — rung 1's launch is bounded by an extension allow-list

**Why now.** §5 above bounds the opener's blast radius **by what may be presented** — a path
resolved inside the workspace root, confirmed to be a regular file, with no command from the model.
The 2026-07-26 url-safety audit (finding H-2) showed that bound is one step short of what §5
claims. Path-safety decides *which* file is handed over, but the model chooses the file's **name**,
and on every desktop it is the **extension** that chooses the program: `open report.command`,
`cmd /c start "" report.bat` and `xdg-open report.desktop` do not *show* those files, they **run**
them, with the user's full privileges and outside any confinement box. In Auto the model can write
such a file in-workspace (a `workspaceScopedWriter` auto-runs) and then present it; in Plan a
checked-in `build.bat` in a hostile repo is enough. That is a model-chosen command by the back
door, which is precisely what §5 says this ADR never does — so the sentence in §5 is kept and the
code is made to deserve it.

**(a) Rung 1 hands the OS handler only what an OS handler renders.** The launch is now bounded by
an **extension allow-list**, `present.OpenerRenderable` (`internal/present`): documents, images and
text — the formats whose default handler **displays** the file rather than executing it. An
extension outside the set produces **no argv at all**; `Open` reports the existing `ErrNoOpener`, so
the ladder degrades to **rung 0** — the transcript entry with the path — exactly as it already does
for a session with no desktop. Nothing new is refused to the *user*: the path is still presented,
and they open it themselves if they meant to.

**(b) It is an allow-list, and it is wider than rung 2's.** The deny side is unbounded and
OS-specific (Windows' whole `PATHEXT` plus `.hta`/`.scr`/`.msi`/`.reg`/`.lnk`, macOS'
`.command`/`.terminal`/`.app`/`.scpt`, Linux' `.desktop`), so a list of what must never run is a
list somebody is always one entry behind on. Rung 1's set is **its own** and deliberately **wider**
than rung 2's four browser-renderable extensions (`.html`, `.htm`, `.svg`, `.pdf`), which stay where
they are in `internal/tui`: an OS handler is exactly what shows the `.docx`, `.png` or `.md` a
browser would only download, and opening a deliverable in the application that knows it is rung 1's
whole value. Rung 2's set is a **subset** of rung 1's by construction, pinned by a test.

**(c) The bound stops at rung 3.** A `present.command` template names **one** application, so the
extension selects nothing there — the user's configured opener shows whatever it is given, and
narrowing it to a curated list would refuse the source files and odd formats they configured it for.
§5's reasoning stands unchanged on that rung: `present.command` is the **user's own** configuration,
with the same standing as their shell.

**(d) Nothing else about the ladder moves.** Rung 0 is unconditional as before, rung 2's set and the
doc server's extension-agnostic serving are untouched, the tool result still names the outcome
(`shown` for a refused extension, never an error — §4's degrade), and `present_document` remains
mode-**independent** and outside the Approval gate. This amendment adds no tool class and no row to
`docs/design/confinement-execution-contract.md` §4; the bound is a property of the mechanism, not a
new disposition.

## Amendment (2026-07-26) — rung 1's Windows launch refuses a name cmd.exe would parse

**Why now.** The amendment above bounds which *program* the extension selects; on Windows the launch
itself still travels through a **shell**. Rung 1's opener there is `cmd /c start "" <path>`: Go
joins that argv into one command line, quoting an argument only when it holds a space or a quote
(`syscall.EscapeArg`), and cmd.exe then **re-parses** the joined line — where `&`, `|`, `^`, `<`,
`>` and `%` are syntax. A model-written file named `report&calc&.html` in a space-free workspace
path therefore reads back as three commands, and the middle one runs — with the user's full
privileges, outside any confinement box, under an extension (`.html`) squarely inside the
allow-list, because the injection rides the rest of the **name**. Raised by the url-safety plan's
item 2 verifier (2026-07-26, item 17); the Windows opener ships unexercised, so this closes the
hole before it is live.

**(a) The bound.** On Windows — and only there — rung 1 refuses a path carrying any character
cmd.exe can read as its own grammar: the operators `&` `|` `^` `<` `>`; the expansions `%` (live
even inside double quotes) and `!` (live when a machine-wide registry key enables delayed
expansion); the quote (Go escapes an embedded `"` as `\"`, which cmd's parser does not honour, so
the two disagree about where the quoted region ends); the token delimiters `;` `,` `=` (an unquoted
path holding one splits into **two** `start` arguments, and `start` resolves its first argument
like a command name); and ASCII control characters. A refused path builds **no argv at all** and
reports the existing `ErrNoOpener`, so the ladder degrades to rung 0 exactly as it does for a
refused extension — the path is still presented, the tool result still reads `shown`, never an
error (§4's degrade, unchanged).

**(b) A space is not refused, and neither are parentheses.** Go double-quotes an argument holding a
space, and (a)'s set is exactly the set that stays live inside — or breaks out of — those quotes,
which is why refusing it suffices. Parentheses are literal to cmd mid-argument once (a)'s set is
gone, and `report(1).html` is a name real deliverables have.

**(c) The other rungs and the other desktops are untouched.** `open` and `xdg-open` receive the
path as one execve argument with no shell in between, so macOS and Linux need no name bound. Rung 3
stays unbounded (the previous amendment's (c) — a `present.command` names one application and is
launched without cmd.exe), rung 2 serves over HTTP and launches nothing, and the extension
allow-list stands unchanged beside this bound: the extension decides what the handler would *do*
with the file; the name bound decides whether cmd.exe would even hand the handler the file the
model named.

## Amendment (2026-07-26) — the allow-list refuses the pre-2007 office formats, and `.csv` is ruled in

**Why now.** Raised by the url-safety plan's item 2 verifier as residual risk (carried forward as
that plan's item 20): the first amendment's rule — the formats whose default handler **displays**
the file rather than executing it — and the shipped set disagreed. The set admitted `.doc`, `.xls`
and `.ppt`, the pre-2007 binary office formats, whose single container carries macros the handler
offers to run on one *Enable Content* click. The distinction the rule draws is `.docx` vs `.docm` —
OOXML split the macro-free formats from the macro-carrying ones precisely so the extension states
which one it is — and the legacy trio, which has **no macro-free variant**, sits on the `.docm`
side of that line. The available moves were to narrow the set or to soften the rule into "macros
are not auto-run, one click away, an accepted risk"; the **set moves and the rule stands**, because
the rule as stated is the defensible one and the cost of narrowing is close to nil.

**(a) `.doc`, `.xls` and `.ppt` are out.** A deliverable in a pre-2007 format degrades exactly as
any refused extension does (the first amendment's (a)): no argv at all, `ErrNoOpener`, rung 0 — the
path still presented, the tool result still `shown`, never an error. The user-facing cost is
negligible: a coding agent's office deliverables arrive in the post-2007 formats, which stay in the
set.

**(b) `.csv` stays in, ruled explicitly rather than waved through.** A CSV is plain text with no
container for code — there is nothing in the file its handler can be asked to *run*, which is the
rule's own line. The residual surface (spreadsheet formula / DDE injection) exists only when the
default handler happens to be a spreadsheet, and even there nothing reaches the OS without the user
clicking through that application's own security prompts — DDE launch has shipped default-off or
prompted in both Excel and LibreOffice since the 2017 mitigations. Set against that, `.csv` is a
format a coding agent's deliverables genuinely arrive in, which `.doc` and `.ppt` are not.

**(c) Nothing else moves.** Rung 2's four browser extensions were never in the removed trio, so the
subset invariant holds unchanged; rung 3 stays unbounded (the first amendment's (c)); the Windows
name bound (the second amendment) is untouched.

## Amendment (2026-08-12) — the allow-list refuses active content, and rung 2 carries a policy

**Why now.** Raised by the external security audit of 2026-08-11 and ratified by the owner
(hostile-bytes hardening plan, design call 3). The first amendment's rule is "the formats whose
default handler **displays** the file rather than executing it", and the shipped set admitted
`.html`, `.htm`, `.xhtml` and `.svg` — whose default handler is a browser, which does not so much
display a page as **run** it. The preconditions are all stock: `present_document` is `ReadOnly`, so
it auto-runs in **every** mode including Plan; `present.auto-open` defaults true; and the document
need not have been written by the model, since one that arrived in the clone is enough. Script in
that page then reaches loopback, RFC1918 and `169.254.169.254` from the **browser's** network
position, with none of the `URLGuard` filtering that is the stated justification for a network tool
auto-running at all. This is the same move as the third amendment and further along the same line:
a macro needs one *Enable Content* click, a `<script>` needs none.

The bound is real but not unlimited, and the triage records the limit rather than overstating the
finding: `presentationRungs` wires the opener only on a **Local** session with `auto-open` set, and
`Opener.argv` additionally requires `HasDesktop`, so any `SSH_*` variable in the environment or a
headless container means no opener was ever built. The persona this defends is the local desktop
one, which is apogee's primary persona.

**(a) The four active-content extensions are out of rung 1.** They degrade exactly as any refused
extension does (the first amendment's (a)): no argv at all, `ErrNoOpener`, rung 0 — the path still
presented, the tool result still `shown`, never an error. Because `climb`'s two branches are
exclusive (a Local session degrades to the baseline rather than falling through to rung 2), the
user-visible consequence is that a **local** `present_document report.html` launches no browser.

**(b) Rung 2 keeps them, and gains the policy that bounds them.** The rung that shows active
content must be the rung that can police it, and only a served response can carry a header: every
document the doc server answers now carries `Content-Security-Policy: default-src 'none'; img-src
'self' data:; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'; frame-ancestors
'none'; sandbox`, plus `X-Content-Type-Options: nosniff`. `default-src 'none'` is the load-bearing
directive — it refuses script, `fetch`, XHR and every subresource load, which is the mechanism the
audit named. The bare `sandbox` is not redundant beside it: CSP has no directive for `<meta
http-equiv="refresh">`, and withholding `allow-top-navigation` is what closes that half. `img-src`
and `style-src` are the narrow re-openings that keep a self-contained report readable. The policy's
**directives** are what a test asserts, not the header's presence — a permissive policy would
satisfy a presence check while closing nothing.

**(c) The subset invariant is deliberately INVERTED on this axis.** The first amendment's (b) and
the third's (c) both state that rung 2's set is a subset of rung 1's, pinned by a test. Three of
rung 2's four extensions have now left rung 1, so the two sets **cross** on `.pdf` rather than
nest, and the test states the crossing in both directions instead. The original reasoning — a
ladder that answers differently depending on where it runs is suspect — is answered rather than
abandoned: the two rungs differ here **because** their ability to bound the document differs, which
is the ladder working as designed and not drift. Rung 1 stays the wider set for every inert format;
rung 3 stays unbounded (the first amendment's (c)); the Windows name bound (the second amendment)
is untouched.
