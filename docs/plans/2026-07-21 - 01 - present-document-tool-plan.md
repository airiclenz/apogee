# Implementation plan — `present_document`: a tool that shows a finished document to the user

**Date:** 2026-07-21. **Status: PLAN — not started.** Execute with `/implement-plan` in a fresh
session: one sub-agent per numbered work item below, verifier before commit, mark items done in
this file. This plan does not supersede `2026-07-21 - 00` (the release-cut handoff) — the two are
independent; the owner sequences them. Reconciled against
`docs/plans/implementation-plan-apogee-merge.md`: no overlap — its open Phase 5 (Windows
shell/path backend) is untouched; the Windows opener rung here (item 4) ships unexercised until
Phase 5 gives it a real Windows harness, the same posture as `internal/platform`'s Windows stub.

## Why

Skills that produce a report (e.g. an architecture review) end with a file on disk the user never
sees: `write_file` renders as a one-line `Write File <path> +N bytes` card and nothing more. Small
models especially need a single, dumb, explicitly named affordance — "show this document to the
user" — instead of platform reasoning. The owner wants: **automatic opening when apogee runs on the
user's desktop**, and the most robust, system-independent behaviour possible when it runs remotely
(SSH remoting, devbox VMs, containers).

## Empirical grounding (verified 2026-07-21, in the owner's devbox)

These facts were established live in the session that produced this plan; do not re-derive them.

- Apogee's primary remote context is a **Zed SSH-remoted terminal** (`TERM_PROGRAM=zed`,
  `SSH_CONNECTION=192.168.64.1 … 192.168.64.2 22`). Zed has **no `zed` CLI on remotes**
  (zed-industries/zed discussions #32214, #33601 — confirmed still true June 2026), so a
  VS Code-style `code <file>` shell-out is not available.
- Zed's terminal makes **file paths in output cmd+clickable** (opens in the editor via the remote
  server, with line numbers) and **URLs cmd+clickable** (opens the *host's* default browser). Both
  work on plain text — no OSC 8 needed, and plain text is the more portable form.
- An HTTP server bound in the devbox on `0.0.0.0` **is reachable from the host** at the address in
  `$SSH_CONNECTION` (server-side IP, `192.168.64.2`): proven with `python3 -m http.server 8934` and
  a host browser.
- **Host-side policy can still block the URL rung invisibly**: macOS Local Network permission
  (Sequoia+) made Chrome fail with a generic "site can't be reached" while Safari worked. Lesson:
  every mechanism above the baseline must *fail visible*, and the baseline (plain path/URL text in
  the transcript) must always be printed.
- The owner **explicitly rejected any host back-channel** (reverse SSH, host-side helper daemon,
  path mapping over shared mounts as a required rung): security holes and dependencies. Out of
  scope permanently unless the owner reopens it.

## Settled design (do not re-litigate in work items)

**One tool, host decides.** The model calls `present_document {path[, title]}` after writing a
deliverable. The tool routes through a host-supplied `domain.Presenter` delegate — the exact
pattern of `ask_user`/`domain.Asker` (mode-independent, `ReadOnly()`, runs in Plan, NOT a safety
gate, NOT an `ExternalEffectTool`, nil delegate ⇒ tool not registered). The *host* picks the
mechanism; the model never reasons about platforms.

**The presenter ladder** (host-side, per call):

0. **Baseline, always:** a prominent presentation line in the transcript carrying the
   workspace-relative path (cmd+clickable in Zed/VS Code/iTerm/WezTerm/kitty; copyable anywhere).
   This rung is never skipped, even when a higher rung succeeds — it is the rung that is never
   wrong.
1. **Local desktop → auto-open** (the owner's headline want): when the session is local (no
   `SSH_CONNECTION`/`SSH_TTY`/`SSH_CLIENT` in the environment) and a desktop exists (darwin:
   always; windows: always; linux: `DISPLAY` or `WAYLAND_DISPLAY` set), invoke the OS opener —
   `open <path>` / `cmd /c start "" <path>` / `xdg-open <path>`. HTML lands in the default
   browser, markdown in the OS-associated app.
2. **Remote + browser-renderable file → serve and print the URL:** extensions `.html`, `.htm`,
   `.svg`, `.pdf` are registered with an embedded lazy doc server and the URL is added to the
   presentation line. The user's cmd+click opens the host browser.
3. **Config override:** `present.command` (a template with `{path}`) replaces rung 1's opener when
   set — for users who want a specific app.

**Doc server (rung 2) security posture:** serves ONLY explicitly presented files, each under a
random capability token (`/d/<32-hex>/<basename>`), no directory listing, 404 for everything else,
re-reads the file from disk per GET (re-presenting after edits shows fresh content), content-type
by extension, starts lazily on the first served presentation, closes on app shutdown. Advertised
address: server IP from `$SSH_CONNECTION` → `present.host` config override → outbound-dial
fallback (`net.Dial` to a public IP, read the local addr, no packets need to arrive). Port:
`present.port`, default 0 (ephemeral) — the URL is printed fresh per presentation, so a stable
port buys nothing.

**Fail visible, degrade to baseline.** Opener failure, server failure, or an undetectable desktop
never errors the tool call: the outcome degrades to rung 0 and the transcript line says what
happened (e.g. "no opener available — path shown"). The model's tool result names the outcome so
it can tell the user truthfully ("opened in your browser" vs. "the path is shown in the
transcript").

**Rejected alternatives** (record in the ADR): host back-channel (owner, security); `zed` CLI
(doesn't exist remotely); OSC 8 hyperlinks (less portable than plain text detection); serving the
whole workspace (capability-token allowlist instead); auto-opening on the *remote* box
(`xdg-open` in a headless VM is noise).

## Work items

Each item is one sub-agent's task: read the named files first, implement, test, `go vet` + run the
package tests, then mark the item `[x]` here. Follow existing idiom religiously — this codebase's
comment density and doc.go conventions are load-bearing (see `internal/tools/ask_user.go` and
`internal/tui/asker.go` as the pattern to rhyme with). ADR 0010: `internal/*` may depend only on
`internal/domain` downward, never the root facade.

- [x] **1. ADR 0019 + CONTEXT.md vocabulary.** Write
  `docs/adr/0019-documents-are-presented-not-opened.md` in the house ADR style (read 0012 and 0008
  for form): context (the skills-deliverable gap, the Zed/remote findings above), decision (one
  tool + host presenter ladder as specified in "Settled design"), consequences, rejected
  alternatives (list above, including the owner's security rejection of host back-channels). Add
  the new domain words to `CONTEXT.md`'s vocabulary: *Present / Presentation* (the act of
  surfacing a finished document to the user), *Presenter* (the host delegate), *presentation
  ladder* (the mechanism ladder, rung 0 always), *doc server* (the capability-token file server).
  Acceptance: ADR cross-references 0008 (delegate statelessness) and 0012 (why the opener runs
  host-side, outside tool confinement); CONTEXT.md entries match the ADR's wording.
  NOTES (2026-07-21): the rejected-alternatives list is carried under the house-style heading
  `## Considered options` (every other ADR uses that name, and the item requires house ADR
  style), and it gains one option the plan did not list — separate per-mechanism tools
  (`open_document`/`serve_document`) — rejected for the same reason the settled design gives for
  one tool. CONTEXT.md's four terms live in a new `### Deliverables and presentation` section
  placed after `### Context and history` (the plan did not fix a location).

- [x] **2. Domain seam: `Presenter`.** New `internal/domain/present.go` mirroring `ask.go` (P3.11
  pattern — read it first): `Presenter` interface with
  `Present(ctx context.Context, req PresentRequest) (PresentOutcome, error)`; `PresentRequest`
  struct: `Path` (absolute), `DisplayPath` (workspace-relative, for display), `Title` (optional,
  may be empty). `PresentOutcome` struct: `Method` (a `PresentMethod` string-typed enum:
  `PresentOpened`, `PresentServed`, `PresentShown`) and `Location` (the URL for served, the
  display path otherwise). Structs, not bare strings, for the same D7 freeze-safety reason
  `AskRequest` documents. Add `Presenter Presenter` to `domain.Config` next to `Asker`
  (`internal/domain/config.go:42`) with a matching "nil ⇒ present_document is not registered"
  comment. Acceptance: doc comments state mode-independence, fail-safe-under-ctx-cancel, and the
  nil-omission contract; `go vet ./internal/domain` clean.
  NOTES (2026-07-21): the item names only `internal/domain`, but the seam is also re-exported
  from the root facade (`apogee.go`, a `Presentation` block beside the existing `Ask-user`
  one: `Presenter`/`PresentRequest`/`PresentOutcome`/`PresentMethod` + the three constants).
  Without it no out-of-module embedder can implement the interface (the argument types would
  be unnameable), which would contradict ADR 0019's "Presenter is additive public surface"
  consequence and item 9's CHANGELOG line; ADR 0010's facade-re-exports-the-public-types rule
  makes it the same edit `Asker` got. Also: `Presenter` sits directly after `Asker` in the
  `Config` delegate block (the item said "next to `Asker`"), which regofmts that block's
  comment alignment.

- [x] **3. `internal/present` — locality + advertise address (`detect.go`).** New package
  `internal/present` (host-side presentation mechanisms; consumed by the TUI, importable by any
  embedder; depends on stdlib only). `doc.go` explains the package charter and cites ADR 0019.
  `detect.go`: `Locality(env func(string) string) Kind` returning `Local`/`Remote` — Remote iff
  any of `SSH_CONNECTION`/`SSH_TTY`/`SSH_CLIENT` is non-empty; `HasDesktop(goos string, env)` —
  darwin/windows true, linux true iff `DISPLAY` or `WAYLAND_DISPLAY` set, else false;
  `AdvertiseHost(env, override string) string` — override if set, else the third field of
  `SSH_CONNECTION` (its server IP; bracket IPv6 for URL use), else the outbound-dial trick
  (`net.Dial("udp", "203.0.113.1:9")`, read `LocalAddr`, close — no packets sent), else
  `127.0.0.1`. All take env/goos injected for table tests. Acceptance: table tests cover the
  devbox case (`SSH_CONNECTION=192.168.64.1 50072 192.168.64.2 22` → `192.168.64.2`), IPv6
  bracketing, override precedence, empty-env fallback.
  NOTES (2026-07-21): `AdvertiseHost` consults **`SSH_CONNECTION` before the `override`**, not
  override-first as this bullet's literal text says — "Settled design" governs (it, ADR 0019 and
  CONTEXT.md all fix the order `$SSH_CONNECTION` → `present.host` → dial → `127.0.0.1`), and the
  deviation is called out in the function's doc comment so nobody "fixes" it later. Three
  additions the bullet did not spell out: the outbound-dial probe is factored behind an
  unexported `advertiseHost(env, override, dial)` seam so the precedence chain is table-testable
  off any routing table (the exported signature is exactly as specified); URL bracketing is
  applied to *every* link of the chain (override and probe result too), not only the
  `SSH_CONNECTION` field, since all three feed the same URL authority; and a malformed
  `SSH_CONNECTION` (fewer than 3 fields, or a third field that is not a numeric IP — a zoned
  link-local included) falls through to the next link rather than advertising garbage. A nil
  `env` reads as an empty environment in all three exported functions.

- [x] **4. `internal/present` — the OS opener (`opener.go`).** `Opener` value constructed with an
  injected runner (`func(name string, args ...string) error` — the seam tests fake) plus goos/env;
  `Open(path string) error` builds: darwin `open <path>`; windows `cmd /c start "" <path>`; linux
  `xdg-open <path>`; anything else or no desktop → a sentinel `ErrNoOpener`. `CommandOverride`
  field: when the config template (e.g. `zed {path}`) is set, split it (reuse
  `github.com/google/shlex`, already a dependency — see `internal/tools/terminal.go`), substitute
  `{path}`, run that instead on every OS. The opener runs detached from the TUI's terminal
  (stdout/stderr discarded) — it must never scribble on the Bubble Tea screen. Acceptance: tests
  assert the exact argv per OS, the override path (including `{path}` substitution and quoting),
  `ErrNoOpener` on headless linux, and that a runner error surfaces (for fail-visible handling
  upstream).
  NOTES (2026-07-21): four points the bullet did not spell out. (a) `CommandOverride` is
  consulted BEFORE any platform detection, so it replaces the OS opener on every OS *and*
  stands in for the desktop check — otherwise "run that instead on every OS" would be untrue
  exactly where it matters (an OS with no built-in opener, which is the case the override
  exists for); the ladder still gates rung 1 on Local + `auto-open` (item 7/8). (b) A template
  that never mentions `{path}` gets the path APPENDED (git's `core.editor` convention), because
  an opener that launched but opened nothing is a success the user cannot see. (c) An
  unparseable or program-less template is a configuration error naming `present.command`, NOT
  `ErrNoOpener` — the sentinel means "nothing to open into" and must stay distinguishable.
  (d) The production runner (`launchDetached`, the nil-`Run` default) starts the command with
  its standard streams on the null device, waits up to a 2s grace for an exit status, and
  returns nil once the command outlives it: waiting is what makes a failed launch visible
  (every opener in the table exits in milliseconds), and the bound is what stops a
  user-configured foreground command from holding the presenting Turn open. The child is
  reaped either way. A blank path is rejected rather than handed to the opener. Also: item 3's
  `doc.go` line "imports the standard library only" is now false (this item mandates shlex), so
  it was corrected in place.

- [x] **5. `internal/present` — the doc server (`server.go`).** `DocServer` with `Serve(path
  string) (url string, err error)`: lazily starts one `net/http` server on first call
  (`net.Listen("tcp", ":<port>")`, port 0 default), registers the file under a fresh
  `crypto/rand` 16-byte hex token, returns
  `http://<advertiseHost>:<boundPort>/d/<token>/<basename>`. Handler: exact-match token →
  re-read file from disk per GET, `Content-Type` from extension (`mime.TypeByExtension`,
  fallback `text/html` for `.html`), everything else (including `/d/<token>/` prefix walks and
  `..`) → 404 with no body listing; no logging of tokens. `Close()` shuts the server down
  (wired to app shutdown in item 8). Concurrency-safe token map (mutex). Acceptance: httptest-
  style tests over a real listener — 200 + correct content-type for a registered file, fresh
  content after the file is rewritten, 404 for wrong token/other paths/traversal attempts, two
  Serve calls share one listener, Close idempotent.
  NOTES (2026-07-21): five points the bullet did not spell out. (a) The advertised address is a
  caller-supplied `Host` field (what `AdvertiseHost` returned, already URL-authority form) rather
  than computed inside the server: detection lives in `detect.go`, the server's bind address
  cannot answer "where can the USER reach this box", and item 8's wire already computes it. Empty
  advertises `127.0.0.1`; `hostForURL` is re-applied (it is idempotent) so a raw IPv6 literal
  still composes a legal URL. `Port` is the matching field. (b) `Serve` stats the path and
  refuses a blank / missing / non-regular one instead of printing a URL that would 404 in front
  of the user — the fail-visible rule (ADR 0019 §4) wants that failure at Serve, where the
  caller can still degrade to rung 0. (c) After `Close`, `Serve` returns an error rather than
  starting a second listener: the server's lifetime is the app's (ADR 0019 §1), so a late
  presentation degrades instead of resurrecting a port past shutdown. (d) The handler answers
  only GET/HEAD (any other method gets the same bare 404, since a 405 would confirm the token is
  real), serves through `http.ServeContent` with the Content-Type set from the extension first
  (so range and conditional requests work for PDFs while the type stays a function of the
  extension alone, never sniffing), and the `http.Server`'s `ErrorLog` is discarded — net/http
  logs request paths, which here contain tokens, and stderr is the Bubble Tea screen. A bare
  `HandlerFunc` is used rather than `ServeMux` precisely because the mux would clean paths and
  301 the tidy form, echoing a token in a `Location` header. (e) `Serve`'s results are unnamed
  (`(string, error)`) — the bullet's `url string` would shadow the `net/url` package the URL is
  composed with. Test note: the successful configured-port bind is asserted through the
  collision path (a port the test itself holds, so only a server that binds `s.Port` can fail on
  it) — binding a just-released ephemeral port races other parallel tests for it.

- [x] **6. The tool: `internal/tools/present_document.go`.** Mirror `ask_user.go` exactly in
  shape (spec var, args struct, delegate-holding tool, nil-delegate defensive error, `ReadOnly()
  → true`, `var _ domain.ReadOnlyTool` assertion). Spec — name `present_document`; description
  written for small models: `"Show a finished document to the user. Call this after writing a
  report or other deliverable file the user should read; it opens or links the document for
  them."`; schema: required `path` (workspace-relative or absolute), optional `title`. Execute:
  resolve via `resolveInRoot` (see `open_file.go`), require an existing regular file, build the
  workspace-relative `DisplayPath`, call the Presenter; map the outcome to result text the model
  can relay truthfully — `opened` → "Presented <display>: opened on the user's machine."; `served`
  → "Presented <display>: shown in the transcript with a link (<url>)."; `shown` → "Presented
  <display>: the path is shown in the transcript for the user to open."; Presenter error → the
  degraded-but-shown wording, not an IsError (rung 0 happened host-side regardless). Path
  escape/missing file → IsError result. Register in `DefaultToolsWithHost`
  (`internal/tools/registry.go`) guarded by `host.Presenter != nil`, add the field to
  `HostTools`, and update the registry doc comments + any tool-count assertions in
  `registry_test.go`. Acceptance: tests with a fake Presenter cover all three outcomes, nil
  delegate, missing file, directory, path escape, and registry omission-when-nil.
  NOTES (2026-07-21): six points the bullet did not spell out. (a) "Presenter error → the
  degraded-but-shown wording" is narrowed by ADR 0007 exactly as `ask_user.go` narrows it —
  a Presenter error raised because the ctx was CANCELLED is re-raised as a Go error so the
  loop rolls the Turn back; every other Presenter error degrades to the shown wording as
  specified. (b) The wording switch treats an unknown `PresentMethod` (the enum is open,
  ADR 0019) and a `served` outcome with an empty `Location` as the baseline: those are the
  only claims such an outcome can truthfully carry. (c) `DisplayPath` is measured against the
  SYMLINK-RESOLVED root (`security.EvalRealPath`), because `resolveInRoot` returns a real path
  and a plain `filepath.Rel` against the configured root answers with `../..` wherever the root
  is reached through a symlink (macOS `/tmp`). (d) `present_document` is appended AFTER
  `ask_user` (the two host-delegate tools close the menu, independently gated), so it is last
  when both delegates are set — `registry_test.go` asserts both positions. (e)
  `internal/agent/loop.go`'s `hostTools(cfg)` now threads `cfg.Presenter` into `HostTools`, the
  one-line counterpart of `Asker: cfg.Asker`: item 8's read list is `cmd/apogee/*` only, so
  without it `domain.Config.Presenter` would be dead for every embedder that lets the engine
  build the default registry. (f) `internal/tools/doc.go` gains a present_document paragraph —
  the package-doc convention the work-items preamble calls load-bearing. CHANGELOG/README are
  untouched here: item 9 owns the docs sweep.

- [x] **7. TUI: `uiPresenter`, the presentation transcript entry, and the tool card.** Read
  `internal/tui/asker.go`, `messages.go`, `transcript.go`, `toolpresent.go`, and `doc.go` (ADR
  0011: value-copied Model — no `strings.Builder` by value, no self-pointers) first. New
  `internal/tui/presenter.go`: `uiPresenter` holds the `*programRef` plus the configured
  mechanisms (an `*present.Opener`, `*present.DocServer`, locality/desktop facts — reference
  types/pointers only). `Present()` runs the ladder ON THE WORKER goroutine (it is called inside
  a Step, like Ask, but needs no human rendezvous): pick rung → attempt → build
  `PresentOutcome` → `prog.send(presentedMsg{…})` → return. It must respect ctx (bail promptly
  when cancelled) and never block on the UI. `presentedMsg` carries display path, title, method,
  and location; the Update loop appends a dedicated transcript entry — visually distinct from
  tool cards (this is a deliverable, not plumbing): title line when given, then the
  workspace-relative path as plain text on its own line, then the URL as plain text on its own
  line when served, then a short status suffix ("opened in your browser" / "cmd+click to open").
  Plain text for path and URL — terminal emulators do the linkification; never wrap them in
  markup or split them across wrapped lines mid-token if avoidable. Add the `toolpresent.go`
  registry entry (label "Present", verb "presenting", target `path`). Acceptance: presenter unit
  tests with faked mechanisms cover ladder selection (local+desktop → opened; remote+html →
  served; remote+md → shown; opener failure → degrades to shown with the failure noted);
  transcript rendering tests (including narrow-width wrap behaviour around the URL);
  `TestModelNoBuilderByValue` still green.
  NOTES (2026-07-21): eight points, the first of which is the open question this item was asked
  to settle. (a) **A set `present.command` DOES bypass the desktop gate and does NOT bypass the
  locality gate.** The ladder consults locality and nothing else; whether this machine has
  anything to open into is `Opener.Open`'s own answer (`ErrNoOpener`), exactly as item 4 decided
  — so the desktop test lives in ONE place instead of two that can disagree, and the one
  configuration the user was most explicit about is not second-guessed. Recorded in ADR 0019's
  rung-3 bullet (a clarifying sentence, the only edit to a done item's artifact), in
  `presenter.go`'s type comment, and pinned by `TestPresenterLadderPicksRung`. (b) Consequently
  `uiPresenter` holds LOCALITY only, not "locality/desktop facts" as the bullet says — a
  `HasDesktop` field here would be the second gate (a) just removed. (c) The item names only the
  type; the `Bridge` gains `SetPresentation`/`Presenter()` beside `Asker()`, because a
  `uiPresenter` no composition root can reach is dead code and item 8's "hand them to the TUI
  bridge" needs the conduit to exist (the same argument item 2's NOTES made for the facade
  re-export). `Presenter()` returns an untyped nil until `SetPresentation` runs — that IS the
  nil-delegate "not registered" contract, and a typed-nil pointer would defeat it. `Presentation`
  (the rungs the root resolves from config) carries `Opener`/`Docs`/`Local`; a nil `Opener` is
  how item 8 expresses `auto-open: false`, documented on the field. (d) The status suffix for a
  successful open is "opened on your machine", not the bullet's "opened in your browser": only
  rung 2 can promise a browser — rung 1 hands markdown to the OS-associated app — and the entry
  must not claim more than the rung delivered. A degraded rung reads "<reason> — path shown"
  (ADR 0019 §4's wording); every other rung closes with "cmd+click to open", which is true of
  both the path line and the URL line. A rung the host never wired is skipped, not failed, so it
  carries no reason. (e) The `toolpresent.go` entry also gets `detail: firstLineDetail` (the
  bullet named only label/verb/target): a nil detail extractor dumps the raw tool result as a
  BODY, which reshapes the card away from the one-line grammar and un-groups it — `ask_user`, the
  tool this one mirrors, uses the same extractor. (f) Rendering the entry needed two new
  look-and-feel values in `theme.go`: `glyphPresented` (▤ — deliberately not ✦, so a deliverable
  does not read as a tool call) and the `presentTitle` style. The path and URL lines are emitted
  RAW — unstyled, unwrapped, unclipped, one token per line — because terminal linkification is
  the whole mechanism; only the title and the status line wrap. (g) A presentation cannot know
  its sub-agent depth (`domain.PresentRequest` carries none), so the entry is always depth 0 and
  renders unrailed even when a sub-agent presented it. (h) All of this item's tests live in
  `presenter_test.go` — delegate, ladder, fold and rendering together (the `confine_test.go`
  precedent) rather than split across `transcript_test.go`/`render_test.go`.

- [x] **8. Config + wiring.** Read `cmd/apogee/config.go` (+ `defaults/config.yaml`) and
  `cmd/apogee/wire.go` (the `HostTools` build at ~line 312 and `bridge.Asker()` at ~159) first.
  New config-file-only block `present:` — `auto-open` (bool, default true; false disables rung 1
  but never rung 0), `command` (string, default empty), `port` (int, default 0), `host` (string,
  default empty). Parse with the existing precedence machinery (no flags, no env — match the
  newer keys' posture), validate (port 0–65535), and document every key in
  `defaults/config.yaml` in the template's comment style, including the macOS Local Network
  permission gotcha as the troubleshooting note for an unreachable URL. Wire: build the
  `present.Opener`/`present.DocServer`/detection facts from config in `wire.go`, hand them to
  the TUI bridge → `uiPresenter`, thread `Presenter` into `HostTools` and `domain.Config`, and
  hook `DocServer.Close` into the existing shutdown path. Acceptance: config parse/precedence
  tests for the four keys; a wire test asserting present_document is registered in the default
  interactive setup and absent when the bridge supplies no presenter (headless).
  NOTES (2026-07-21): three points the bullet did not spell out. (a) The four keys travel as ONE
  `presentSettings` value through `layer`/`settings`/`options`, mapped from one on-disk
  `presentConfig` (the `modelProfileConfig`/`toModelProfile` pattern), rather than as four flat
  fields each with its own default: they describe a single subsystem and are handed to the wire
  as a unit. `auto-open` is a `*bool` on the on-disk struct so a block that sets only `port:`
  keeps the other three defaults instead of reading as `auto-open: false`. (b)
  `presentationRungs` wires each mechanism only where its rung could be walked — the `Opener` on
  a Local session with auto-open on, the `DocServer` on a Remote one — rather than building both
  unconditionally as the bullet's literal text reads. Item 7's `tui.Presentation` fixed a nil
  field as "a rung this host did not wire" (skipped, never failed), so an opener a remote session
  must never fire, or an advertise address resolved (it may probe the routing table) for a local
  session that has no use for one, would be dead configuration the ladder then has to re-gate.
  Consequently `auto-open: false` wires no `Opener` at all, which is also how it disables rung 3:
  the key says whether a document is opened, `present.command` only says by what. (c) The
  acceptance's registration check runs through `tools.NewDefaultRegistryWithHost` driven by the
  Bridge `runRoot` handed the launcher — `*apogee.Agent` exposes no registry accessor, and that
  builder is the one `resolveTools` uses. `registryWithMCP` gets its own test beside it, because
  a hand-assembled `HostTools` is the one path on which a missing `Presenter:` line would
  silently drop the tool.

- [x] **9. Docs sweep.** CHANGELOG `[Unreleased]`: the new tool, the `present:` config block, the
  Presenter seam (additive API surface → minor bump note, per the CHANGELOG header convention).
  README: add `present_document` wherever the tool suite is tabulated/counted (the 2026-07-21
  refocus counted 20 tools — recount). `TODO.md`: under the skills/feature-parity notes, add one
  line: report-producing skills should end with `present_document` (skills stay user-authored,
  ADR 0002 — no builtin skill changes). Verify ADR 0019's number is still free and its index/link
  conventions match the other ADRs. Acceptance: `grep -ri present_document` over `docs/ README.md
  CHANGELOG.md` shows a consistent story; no doc claims auto-open works remotely.
  NOTES (2026-07-21): the recount is **21** (the 19 unconditional tools + `ask_user` +
  `present_document`, both host-delegate-gated), so README's `~20-tool suite` reads `~21` — the
  tilde stays because the count depends on which delegates the host supplies. Four additions
  beyond the bullet's literal doc list. (a) README gains a `### Showing a finished document`
  subsection under **Configuration**: the `present:` block is user-facing file-only config and
  README documents every other such key (`auto-compact`, `context-window`, `unconfined-hosts`),
  so tabulating the tool while leaving its four keys undocumented would be the inconsistency the
  acceptance grep is meant to catch. (b) `docs/design/technical-design.md` §4 gains a `Present`
  row beside `Ask` — `Presenter` is new *public API surface* and that table is where the surface
  is enumerated; ADR 0018's docs commit set the precedent of touching this file. (c) Follow-up
  (a) from the verifiers is fixed by naming `Asker`/`AskRequest`/`AskAnswer` and the four
  `Present*` types plus the three method constants in `example_test.go` — and, in the same pass,
  `ModeAllowEdits`, `ErrConfinementUnavailable` and `ErrIncompatibleMechanisms`, three further
  pre-existing omissions found by diffing `go doc -all .` against the guard: a guard whose stated
  job is naming the FULL surface is defective while any name is missing, and the fix is
  compile-time only. (d) Follow-ups (b) and (c) are fixed in place — ADR 0019's `internal/present`
  consequence now says stdlib **plus shlex** (matching `internal/present/doc.go`, and noting it
  adds no new dependency), and `resolveTools`'s doc comment names the Presenter beside the Asker.
  NOTES (2026-07-21, correction pass): the CHANGELOG's doc-server sentence claimed a bare 404 for
  "non-GET methods", which the shipped handler does not do — `internal/present/server.go:222`
  answers **GET and HEAD** and 404s every *other* method precisely so a 405 cannot confirm a real
  token (item 5's decision). Corrected, and with it "re-read from disk per GET" → "per request"
  (HEAD re-reads too, and the config template already said "per request"). Also "the `present.host`
  override" → "fallback", matching `AdvertiseHost`'s own NOTE, the config template and this
  entry's own `present:` bullet. ADR 0019 §3 is left untouched: it claims nothing about methods.
  Everything else in the sweep re-checked against the code and left as written.

- [x] **10. Full verification.** `make` targets the README documents (build, vet, full test
  suite) — all green, zero new skips. Then a manual smoke on this devbox: run apogee, have the
  model `write_file` a small HTML report and call `present_document`; confirm the transcript
  entry shows path + `http://192.168.64.2:<port>/d/…` URL, the URL serves from another devbox
  shell (`curl`), and the path line is cmd+clickable in Zed. Record the result in this file.
  Leave the **owner-run** checks as an explicit checklist at the bottom of this plan (do not
  attempt them from the devbox): (a) URL cmd+click from Zed → host browser renders (Safari
  works; Chrome needs Local Network permission — expected), (b) a local macOS run auto-opens an
  HTML deliverable in the default browser, (c) a local run with `present.command: "zed {path}"`
  opens the file in Zed.
  NOTES (2026-07-21): both halves ran on the devbox; both green.
  **Verification.** `make check` — the README's gate and a superset of this bullet's list (gofmt,
  `go vet ./...`, `go build ./...`, `go test -race -count=1 ./...`, the ADR-0010 import invariant,
  all six cross-builds, `apogee --help`) — passed in one run: 19 packages `ok`, no failures, "all
  Phase-2 gates passed"; `make build` green separately. The suite's skip set is unchanged at six,
  every one pre-existing and environment-gated (`TestE2ELiveModel`, `TestSmokeLiveProfileSeam`,
  `TestLandlockProbe{,Network}`, `TestSeatbeltProbe{,Network}`) — zero new skips.
  **Smoke** (this Zed-remoted devbox, `SSH_CONNECTION=192.168.64.1 … 192.168.64.2 22`): the real
  binary driven in a pty, `--mode allow-edits`, a workspace and `--config` home outside the repo.
  Run A used the live model on `192.168.64.1:1111` (`gemma-4-12b-it-qat-q4_0`): it wrote
  `report.html`, called `present_document{path,title}` on it, and the transcript showed the ▤
  block exactly as designed — `▤ Smoke Report` / `report.html` /
  `http://192.168.64.2:37963/d/cb5770855b6f83632ca997126f74567a/report.html` / `cmd+click to
  open` — above it the one-line `✦ Present ┕ report.html` card. `curl` from a SECOND devbox shell,
  while that session was still alive, returned `200`, `Content-Type: text/html; charset=utf-8` and
  the file's bytes; a wrong token, `/`, and the token's own directory each returned `404`; after
  the session exited the port refused connections (the doc server's lifetime is the app's). Run B
  repeated it against a scripted OpenAI-compatible server in place of the model — same ladder,
  same block, same 404s, plus a `..` traversal that 404s — so the result does not rest on one
  model's tool-calling luck. Checked in the RAW capture, not just the de-ANSI'd text: the path and
  URL lines carry no SGR and no wrap, which is the linkification invariant rung 0 depends on. The
  cmd+click itself stays on the owner-run checklist below (a click is not a devbox act).
  **The two cosmetic observations from item 7's verifier, judged.** (i) An untitled presentation
  putting the path beside the ▤ marker is RIGHT and stays: the alternative is a bare marker line
  above the path, and the shipped shape matches the `· note` grammar; it is pinned by
  `TestPresentedEntryRendering`. What was wrong was `presentedView.Path`'s comment ("always its own
  line"), which contradicted the code it documents — corrected here, comment only, no behaviour.
  (ii) A depth-0 presentation re-opening the ⤷ label mid-sub-agent-run is a true consequence of
  item 7's NOTES (g), but it is NOT presentation-specific: renderView re-announces a run after ANY
  depth-0 entry between two nested blocks (a `· cancelled` note does the same). Left as is — the
  real fix is carrying sub-agent depth on `domain.PresentRequest`, a domain change well outside a
  verification item; recorded in TODO.md instead.

## Non-goals / deferred (record, don't build)

- **No host back-channel, ever** (reverse SSH, host helper, shared-mount `file://` URLs as a
  required rung) — owner security decision 2026-07-21. A `file://`-over-shared-mount *optional*
  config rung was discussed and deliberately deferred.
- **No markdown→HTML rendering rung** (serving a rendered view of `.md` deliverables) — natural
  follow-on once the ladder exists; keep `DocServer` extension-agnostic so this is additive.
- **No port-forward integration** (Zed `port_forwards`, VS Code auto-forwarding) — the direct-IP
  URL covers the primary setup; revisit only if a user's topology defeats it.
- **No OSC 8 emission** — plain text is the portable form; revisit only with evidence.

## Owner-run live checklist (after implementation)

- [ ] Zed remote terminal: presentation URL cmd+click opens host browser (Safari default caveat;
      Chrome requires System Settings → Privacy & Security → Local Network).
- [ ] Zed remote terminal: presentation path line cmd+click opens the file in Zed.
- [ ] Local macOS run: HTML deliverable auto-opens in default browser (rung 1).
- [ ] Local macOS run with `present.command: "zed {path}"`: opens in Zed (rung 3).
- [ ] Cleanup from the design session: `rm -rf /workspace/present-test` (throwaway reachability
      probe; also delete the scratchpad copy if the session dir still exists).
