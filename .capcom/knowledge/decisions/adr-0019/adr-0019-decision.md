---
title: ADR 0019 decision
---

**1. One tool; the host decides the mechanism.** The model calls
`present_document {path[, title]}` after writing a deliverable, and that is the whole of its
platform reasoning. The tool routes through a host-supplied **`domain.Presenter`** delegate —
the exact pattern of `ask_user`/`domain.Asker`: mode-**independent** (it never routes through the
Approval gate), `ReadOnly() == true` so it runs in **every** mode including Plan
(presenting writes nothing), **not** a safety gate, and **not** an `ExternalEffectTool` — the
user's own display is not a non-forkable remote the bench must stub, any more than the human
answering `ask_user` is. A **nil `Presenter` means the tool is not registered**, so a headless
host never offers the model an affordance it cannot honour.
ADR 0008's statelessness contract
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
  a local box with no *detected* desktop but never on a remote one. On a document the **model**
  named — which is every `present_document` call — this rung carries one further condition
  (amended 2026-09-22, §5's addendum below): it runs only when the file-only
  `present.command-on-model-documents` key is set, and otherwise is skipped for rung 0.

**3. The doc server is a capability-token allowlist, not a file server.** It serves **only**
explicitly presented files, each under a random token at `/d/<32-hex>/<basename>`: no directory
listing, 404 for everything else including prefix walks and `..`, content-type by extension, the
file **re-read from disk per GET** (so re-presenting after an edit shows fresh content), started
**lazily** on the first served presentation and closed on app shutdown. Its advertised address is
the server IP from `$SSH_CONNECTION`, else the `present.host` override, else an outbound-dial
probe for the local address (no packets need to arrive), else `127.0.0.1`; its port is
`present.port`, default **0** (ephemeral) — the URL is printed fresh per presentation, so a
stable port buys nothing.

*Addendum (2026-08-25, audit F-21) — the token's whole reach is the live transcript entry.* The
served URL carries a capability grant, so rung 0's entry is the only place it appears: the tool
result names the rung and the display path but never the URL, so no capability token is POSTed
upstream on the next Turn, and the session record drops a served entry's location on encode. The
server is lazily started and closed on shutdown anyway, so a persisted URL would be dead on every
resume — the token in it was the only thing a record could still keep.

**4. Fail visible, degrade to rung 0 — a presentation never fails the tool call.** An opener that
errors, a server that cannot bind, an undetectable desktop: none of these produce a tool error,
because rung 0 already happened host-side. The transcript entry says what happened ("no opener
available — path shown"), and the **tool result names the outcome** — `opened`, `served`, `shown`
— so the model can tell the user the truth ("opened in your browser" vs. "the path is shown in
the transcript") instead of asserting a success it cannot observe. Only a path that escapes the
workspace or is not an existing regular file is an error result.

**5. The opener runs host-side, outside tool confinement — and that is deliberate.**
ADR 0012 attaches
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

**Addendum (2026-09-22) — an execution-capable rung 3 runs only behind a file-only opt-in.**
§5's closing sentence — "nothing here runs a *model-chosen* command, unsupervised or otherwise" —
is **superseded**, together with the "the model never supplies a command" premise it rests on.
Both are true of the **command** and false of the **argument**: `{path}` is the path the model has
just written, so a `present.command` of `sh {path}` or `python {path}` does not *show* the model's
file, it *runs* it — on a rung that fires with no approval and no confinement box, in every mode
including Plan. The blast-radius bound §5 claims ("bounded by what can be presented") holds only
while the configured application renders rather than executes, and nothing checked that.

So: a `present_document` call reaches an **execution-capable** rung 3 — a non-empty
`present.command` on a local session — **only when the file-only
`present.command-on-model-documents` key is true**. Absent or false (the default), the ladder
**skips** rung 3 and degrades straight to rung 0 rather than to rung 1, because `present.command`
deliberately stands in for rung 1's mechanism (§2's rung 3) and quietly opening the document in
an application the user did not choose would be the wrong repair. The tool result then names the
key and the file that sets it, so the one person who can act on the degradation is told how. The
key is **file-only** — `/settings` will not write it — because a capability of this shape is
granted in the user's own config file, deliberately, and never from inside a session the model is
talking in. Rung 3's own argv[0] is fenced as well (2026-09-22): the template names a *program*,
so a bare `zed` is a PATH lookup at launch time, and a PATH entry the model may write is a program
the model chose — it is resolved absolutely and refused inside the workspace exactly as rung 1's
is. A user who never sets `present.command` sees no change at all: rung 1 is untouched by the key.

**Nothing else moves.** §1 stands unchanged — `present_document` is still `ReadOnly`, still
mode-**independent** and outside the Approval gate, still not an `ExternalEffectTool` — and the
opt-in adds no tool class and no row to `docs/design/confinement-execution-contract.md` §4: it is
a config fact the host reads, not a disposition. The **Presenter's fire-and-forget contract**
(`domain.Presenter`) stands: the withheld rung asks no Approver and no Asker, acquires no prompt
slot and blocks on no human. No ladder cell moves, and the amendments and note below are
untouched. The rest of §5 stands too — the opener still runs host-side, outside tool confinement,
and that is still deliberate.
