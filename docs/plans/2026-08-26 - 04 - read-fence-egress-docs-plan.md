# Read fence, network egress, and documentation truth

**Goal:** a repo cannot relocate the read fence, no apogee HTTP client follows a redirect or
skips the operator's proxy, the `url-safety:` lists bind every network path they claim to,
the doc-server capability token never leaves the transcript — and the manual then documents
every one of those controls with the semantics the code actually has.

**Date:** 2026-08-26 · **Status:** unexecuted · **sized for:** ~200k-context host

**Evidence (security audit 2026-08-25 §F-13/F-18/F-21/F-40/F-41, chain-10; refocus 2026-08-25
R-1/R-2/R-4/R-5/R-6/R-7/R-undoc; merged in `docs/handoffs/2026-08-26 - 00 - merged-audit-findings.md`
§3.4 and §3.7):** on a stock install a cloned repo that ships `.apogee/skills` as a symlink to
`/home` or `/etc` makes `grep`/`list_dir`/`find_files`/`read_file` read those trees, because
`internal/tools/path_read.go:241` (`rootUsable`) mounts the extra read root by its UNRESOLVED
path through `os.OpenRoot`, which follows symlinks in the root path itself — the very hazard
`internal/skills/load.go:207-226` (`openAnchor`) already hardens skill *discovery* against.
The provider client (`internal/provider/client.go:182`) is built as a bare `&http.Client{}`,
so a 307/308 from the LLM endpoint re-aims the per-Turn POST — the whole conversation — at
any address; the other two client builders (`internal/tools/network.go:317`,
`internal/mcp/transport.go:233`) already refuse redirects. `cmd/apogee/wire_live.go:41,51`
connect MCP servers under a ZERO `security.URLGuard{}`, so a configured `deny-hosts` entry
never applies to an `sse`/`streamable-http` endpoint. Neither the network tools nor the MCP
transports set `Transport.Proxy`, so an operator's `HTTPS_PROXY` is bypassed for exactly the
destinations the model chooses. `present_document` relays the doc-server URL —
`/d/<32-hex token>/<basename>` — inside its tool result (`present_document.go:143`), from
where it is POSTed upstream on the next Turn and persisted in the session record; the
transcript codec (`transcriptcodec.go:487`) persists it a second time. Seven doc claims are
stale (three placeholders, `model:` "hint", probe-command flags, `v0.16.x`, "the other 20",
`docs/plans/` as what is next), and five live controls are documented nowhere user-facing.

**Authoritative sources:**
- `internal/tools/path_read.go:154-163` (`readScope.resolve`), `:172-192` (`open`),
  `:210-222` (`readRoot`), `:225-238` (`matchRoot`), `:241-248` (`rootUsable`);
  `internal/tools/path_safety.go:30` (`resolveInRoot`), `:167` (`safeOpen`).
- `internal/security/pathsafety.go:37` (`ResolveInRoot` — symlink-resolved containment),
  `:73` (`EvalRealPath`); `internal/security/safeio.go:635` (`rootRelative` — lexical).
- `internal/skills/load.go:47-51` (`Sources`), `:79-83` (`skillAnchor`), `:102-115`
  (`sourceAnchors`), `:120-127` (`sourceDirs`), `:227-236` (`openAnchor`);
  `internal/skills/provider.go:92` (`Provider.SourceDirs`).
- `internal/domain/config.go:128-149` (`Config.ExtraReadRoots` contract);
  `cmd/apogee/wire_boot.go:226`, `cmd/apogee/wire_firing.go:226` (the two mount sites),
  `cmd/apogee/wire_tools.go:241` (pass-through); `internal/agent/orientation.go:74-80`.
- `internal/provider/client.go:129` (`WithHTTPClient`), `:177-190` (`NewClient`), `:232-236`
  (shared-transport caveat), `:262` (non-200 → `StatusError`), `:348-356` (`do`).
- `internal/security/urlsafety.go:33` (`URLGuard`), `:73` (`NewURLGuard`), `:110`
  (`DisableIPFloor`), `:117` (`WithResolver`); `internal/security/ssrf.go:252`
  (`SafeDialControl`), `:290-320` (`PinnedDialControl`).
- `internal/tools/network.go:64` (`networkTool`), `:180-196` (pre-flight check, per-call
  client), `:298-321` (`newHTTPClient`); `internal/mcp/transport.go:127-150` (the two HTTP
  transport builders), `:157-171` (`vetEndpoint`), `:188-204` (`checkEndpoint`),
  `:220-237` (`newGuardedHTTPClient`).
- `cmd/apogee/wire_live.go:41-51` (MCP connect + reconnect closure), `cmd/apogee/wire_tools.go:220`
  (`security.NewURLGuard(cfg.URLAllowHosts, cfg.URLDenyHosts)` — the one constructor),
  `:163` (`liveTools.built`), `cmd/apogee/wire_mcp.go:83` (`liveMCP.reconnect`).
- `internal/domain/present.go:78-86` (`PresentOutcome`); `internal/tools/present_document.go:110-147`
  (`Execute`, `renderPresented`); `internal/tui/presenter.go:98-129` (`Present`), `:161-168`
  (`climb`, rung 2); `internal/tui/transcript.go:232-238` (`presentedView`), `:589-600`
  (`addPresented`); `internal/tui/transcriptcodec.go:327-331` (`wirePresented`), `:483-489`
  (`toWirePresented`), `:687-693` (`fromWirePresented`); `internal/tui/startupbox.go:43-44`
  (the Location line); `internal/present/server.go:196-237` (`DocServer.Serve`).
- `internal/config/registry.go:272` (`web-search-endpoint`), `:290` (`tools.disabled`), `:305`
  (`tools.enabled`), `:317`/`:325` (`url-safety.allow-hosts`/`deny-hosts`), `:331`
  (`use-project-skills`), `:494-498` (`bypass`, `EnvVar: EnvBypass`), `:557`
  (`validateSearchEndpoint`); `internal/config/config.go:2343-2352` (the `APOGEE_*` constants),
  `:2464-2468` (`--workspace` > `APOGEE_WORKSPACE` > cwd); `internal/tools/web_search.go:117-130`
  (the `off`/`none`/`disabled` sentinel); `internal/tools/registry.go:190-235` (`builtinTools`,
  menu order), `:386-395` (`KnownToolNames`); `internal/config/defaults/config.yaml:364`,
  `:421-423`, `:443-445`, `:453` (the template lines for the four keys).
- `docs/skill-runs/refocus/2026-08-25/corrections.md` (the exact CURRENT/PROPOSED text for
  R-1..R-7); `docs/manual/configuration.md:1-18` (precedence intro), `:66-99` (the `tools:`
  block), `:540-545` (placeholders), `:670` (`## The Console family`, the last section);
  `CONTEXT.md:1264-1267` (§ Heartbeat model-hint clause), `:511-520` (**Bypass mode**), `:748`
  and `:805` (**url-safety** in the safety guardrails and MCP entries), `:1028` (skills
  sources); `docs/manual/probe.md:67-75`; `README.md:165-167`, `:209`, `:214-215`; `VERSION`
  (`v0.17.3`).
- ADR 0012 (Amendment 2026-07-25: url-safety vouched-for by construction; Amendment
  2026-07-26: the SSRF floor is the anti-*model* control, a configured MCP endpoint is pinned
  instead), ADR 0019 §3 (doc server = capability-token allowlist; tokens never logged),
  ADR 0032 (skill sources and collision order), ADR 0006 (Bypass = the Mechanisms-off floor),
  ADR 0031 (Driver parity), ADR 0037 (live settings).

**Ratified design calls (owner, 2026-08-26, via AskUserQuestion):**
1. F-21 is FIXED, not accepted as-is: the doc-server capability token stays at the relay rung
   (the transcript entry) and reaches neither the model's context, nor the upstream POST, nor
   the persisted session record.
2. `url-safety:` is documented only AFTER F-40 wires it for MCP — a dependency between items
   9 and 4, not an option; the manual must not describe a control that is partly inert.
3. Accepted-risk candidates: none denied — every finding in scope is fixed here.
4. (owner, 2026-08-26) the tool-name list in the manual is guarded by a drift test — a test
   in `internal/tools` reads `docs/manual/configuration.md` and fails when a `KnownToolNames()`
   entry is missing from it (item 10).
5. (Author, no user-visible alternative) the read-fence fix lands at the MOUNT, in two layers
   that must agree: `internal/skills` decides which anchors are trustworthy and hands the
   tools their symlink-RESOLVED paths; `internal/tools` refuses to mount any extra root whose
   real path differs from the path it was handed. The trust decision never moves into the
   tools layer, which has no anchor to judge it against.
6. (Author, no user-visible alternative) the provider client gains the same
   `CheckRedirect → http.ErrUseLastResponse` policy the other two builders carry; it does
   NOT dial under `URLGuard.PinnedDialControl` (see OPEN CALLS in the coordinator note:
   `NewClient` and `Rebind` carry a "construction never fails" contract at
   `internal/agent/rebind.go:361`, a once-resolved pin breaks a session whose LAN endpoint
   moves IP, and with redirects refused the re-aim vector the finding names is closed).
7. (Author, no user-visible alternative) when an egress proxy applies, the transport dials the
   PROXY, so the dial-time control pins the proxy's own resolved addresses (the `MCP`
   precedent) while the guard's pre-flight still judges the DESTINATION host by string and
   resolved IP. A proxy on loopback or the LAN therefore works; a private destination is still
   refused before any request leaves; `NO_PROXY` hosts dial direct under the blanket floor.

**Standing requirements:**
- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- No version identifier changes (see closing note).
- Every item's Acceptance is targeted; `make check` runs once at closeout. Items 7, 8 and 9
  are docs-only commits — no `make check` for them; item 10 ships a test, so it is not.
- Every item that ships a user-visible change adds a `CHANGELOG.md` `[Unreleased]` line
  (named in its Files). No item adds a config key, so the registry-bijection contract is not
  exercised; no item closes an `ISSUES.md` entry.
- The `internal/security` package is the lowest layer and imports no sibling; `internal/skills`
  and `internal/tools` may import it, never the reverse. `cmd/apogee` is the only composition
  root — the engine (`internal/agent`) stays wire-silent (ADR 0031).
- Tests that need a live LLM are gated by `APOGEE_LIVE_ENDPOINT`; nothing here needs one.
  Symlink tests skip on Windows exactly as `internal/skills/load_test.go:337` does.

**Out of scope:**
- R-3 (the config re-read exclusion paragraph) — owned by plan `2026-08-26 - 03` together
  with F-23, as one "settings truthfulness" rewrite.
- The `url-safety` choke-point plan parked in `ISSUES.md:296` ("Configurable tool × mode
  security matrix") — untouched; this plan wires the guard that exists, it adds no policy.
- Consolidating the three HTTP client builders into one (`internal/mcp/transport.go:216-218`
  names it an architecture-deepening candidate) — item 5 keeps three builders and changes each.
- A per-server `proxy:` config key; only the environment (`HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`)
  is honoured, which is what the LLM client already does through Go's default transport.
- `use-project-skills:` semantics themselves, the skill cap order (F-06, wave 9), and the
  `/skills` report — item 1 changes what the read tools MOUNT, not what discovery LOADS.
- Extending the doc-vs-registry drift test beyond the tool-name list (mechanism IDs, config
  keys): item 10 guards the one list it writes; a wider idiom is its own decision.

---

## 1. Mount skill dirs by their resolved path; refuse a relocated workspace anchor (F-13, engine half) — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the plan folded every `load_test.go` case under one name; the fixtures need
different shapes (a relocated anchor is table-driven over three symlink placements, the in-base,
home-library and missing-dir cases are single fixtures), so they landed as four functions —
`TestReadRootsRefuseARelocatedWorkspaceAnchor` plus `TestReadRootsResolveAnInBaseSymlinkedAnchor`,
`TestReadRootsFollowTheHomeLibrarySymlink`, `TestReadRootsListADirThatDoesNotExist`. Same coverage,
one assertion per behaviour. Two helpers came with them (`realDir`, `underDir`).
NOTES (2026-08-26): both guards were checked to be load-bearing by temporarily reverting each half
(`readRoots` → `sourceDirs`, and `matchRoot`'s resolved-path skip) and confirming the new tests fail.

**What:** the read tools mount only what `internal/skills` vouches for, and never through a
symlink.
- `internal/skills/load.go`: beside `sourceDirs` (`:120`), add `readRoots(src Sources) []string`
  — the MOUNT view of `sourceAnchors(src)`. Per anchor: an untrusted one (`base` = the
  workspace) is resolved through `security.ResolveInRoot(filepath.FromSlash(a.rel), a.base)`
  and the returned real path is the root — a `security.ErrPathEscape` (the anchor, or a
  component of it, is a symlink leaving the workspace) DROPS that anchor from the list; the
  trusted home anchor is `security.EvalRealPath(a.dir())` — the operator's dotfiles symlink is
  followed and the mount pinned at what it resolves to, exactly `openAnchor`'s (`:227-236`)
  two-way rule restated for the mount. A missing dir is still listed (as today — the mount
  side skips an unusable root). Doc comment names F-13 and the two-layer rule (design call 5).
- `internal/skills/provider.go`: add `func (p *Provider) ReadRoots() []string { return
  readRoots(p.sources()) }` beside `SourceDirs` (`:92`), with `SourceDirs`'s live-view
  semantics (reads the current sources, follows `SetSources`). `SourceDirs` keeps its DISPLAY
  meaning — the `/skills` report goes on naming the configured dir — and its doc comment's
  "It exists for the host that mounts …" paragraph moves to `ReadRoots`, replaced by one
  sentence pointing there.
- `internal/tools/path_read.go` `matchRoot` (`:225`): before `rootUsable`, a candidate whose
  `security.EvalRealPath(candidate)` is not byte-equal to `filepath.Clean(candidate)` is
  SKIPPED like an unusable root — the mount and the fence must agree (`ResolveInRoot`
  compares real paths, `rootRelative` relativises lexically; on a symlinked root they
  disagree), and a root reached through a symlink is one the host did not resolve, which the
  host contract below forbids. Doc comment on `matchRoot` states the rule and why the trust
  decision is NOT taken here (design call 5).
- `internal/domain/config.go` `ExtraReadRoots` (`:128-149`): add to the contract that every
  root must be the host's symlink-RESOLVED real path, that the tools refuse to mount one that
  is not, and that a workspace-contributed dir must be vouched for by the host (the skills
  provider does this through `ReadRoots`).
- Binding standards: `internal/skills` imports `internal/security` for the two helpers (a
  downward import; `internal/tools` already does the same); no new exported symbol in
  `internal/tools`; `readRoots` is pure (no I/O beyond `EvalSymlinks`).

**Files:** `internal/skills/load.go`, `internal/skills/provider.go`,
`internal/skills/load_test.go`, `internal/skills/provider_test.go`,
`internal/tools/path_read.go`, `internal/tools/path_read_test.go`, `internal/domain/config.go`

**Tests:** `load_test.go` — `TestReadRootsRefuseARelocatedWorkspaceAnchor`, mirroring
`TestLoadAnchorSymlinkRefused` (`:337`): with `.apogee/skills`, `.apogee`, and (under
`UseProjectSkills`) `skills` each in turn a symlink to a dir OUTSIDE the workspace,
`readRoots` omits that anchor and keeps the others; a symlink whose target stays inside the
workspace is kept and reported as the RESOLVED path; the home `skills` symlink to a dotfiles
dir is kept, resolved (the `TestLoadHomeLibraryAnchorSymlinkFollowed` case, `:419`); a missing
dir is still listed. `provider_test.go` — `ReadRoots` follows `SetSources` like
`TestProviderSourceDirsFollowSetSources` (`:140`). `path_read_test.go` — beside
`TestReadScopeRefusesSymlinkEscapingExtraRoot` (`:278`): an extra root handed in AS a symlink
(to a dir holding `secret.txt`) is never matched — `resolve`, `open` and `readBounded` all
answer the workspace's `ErrPathEscape`; the same dir handed in by its real path still reads.
Fixtures keep using `tempRoot` (`path_safety_test.go:50`, already `realPath`-resolved).

**Acceptance:** `go build ./... && go test ./internal/skills/ ./internal/tools/ ./internal/domain/`

**Commit:** `fix(skills): read tools mount skill dirs by their resolved path and refuse a relocated workspace anchor`

---

## 2. Wire `ReadRoots` at both mount sites; replicate the audit's exploit end to end (F-13, Driver half) — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the plan's own end-to-end case passes with EITHER mount wired, because item 1's
`matchRoot` guard already refuses a symlinked root at the consuming end — it pins the exploit, not
this item's wiring. A third case was added to make the Driver half load-bearing:
`TestSymlinkedHomeLibraryStaysReadableThroughTheMount` — a `~/.apogee/skills` symlink into a
dotfiles dir stays readable, which only `ReadRoots` delivers (with `SourceDirs` the operator's whole
library goes unreadable). Verified by reverting `wire_boot.go` to `SourceDirs` and confirming it
fails.
NOTES (2026-08-26): the plan's sibling "real `.apogee/skills` still reads" case landed as its own
test function (`TestRealSkillsDirStillMountsItsBundledFiles`) rather than a sub-case, matching item
1's one-assertion-per-behaviour split.

Depends on item 1.

**What:** the composition root mounts the vouched-for roots, and one test proves a
repo-shipped symlink no longer reads the host.
- `cmd/apogee/wire_boot.go:226` and `cmd/apogee/wire_firing.go:226`: `ExtraReadRoots:
  w.skillProvider.ReadRoots` / `skillProvider.ReadRoots` (the method value, so the mount stays
  live across a `use-project-skills` flip exactly as the surrounding comment promises —
  amend that comment to name `ReadRoots` and the F-13 reason).
- `cmd/apogee/wire_firing_test.go:164` and `cmd/apogee/schedule_test.go:703`: the two
  assertions compare against `provider.ReadRoots()` instead of `SourceDirs()`.
- The orientation block (`internal/agent/orientation.go:74-80`) now lists resolved paths;
  no code change there — it prints whatever the func returns, which is now the path the model
  can actually read.
- `CHANGELOG.md` `[Unreleased]` → `### Fixed`: one entry naming the hole (a repo-shipped
  symlink at `.apogee/skills` relocated the read fence) and the fix (skill dirs are mounted by
  their resolved path; a workspace anchor that resolves outside the workspace is not mounted
  at all, matching what discovery already refused).

**Files:** `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_firing.go`,
`cmd/apogee/wire_firing_test.go`, `cmd/apogee/schedule_test.go`,
`cmd/apogee/readfence_test.go` (new), `CHANGELOG.md`

**Tests:** `cmd/apogee/readfence_test.go` — `TestRepoSymlinkedSkillsDirCannotRelocateTheReadFence`:
build the wiring through the `newRootWiring`/`resolveConfig`/`wireSession` fixture
(`wire_test.go:165-188`) over a workspace whose `.apogee/skills` is a symlink to a temp dir
outside it holding `secret.txt`; look up `grep`, `read_file`, `list_dir` and `find_files` on
`w.cfg.Tools` and call each with the ABSOLUTE path of `secret.txt` (and, for `grep`, its
containing dir): every result is the "outside the workspace" refusal and none contains the
file's bytes. A sibling case with `.apogee/skills` a REAL dir holding `demo/SKILL.md` and
`demo/notes.txt` still reads `notes.txt` through `read_file`. Skips on Windows.

**Acceptance:** `go build ./... && go test ./cmd/apogee/ -run 'ReadFence|Firing|Schedule'`

**Commit:** `fix(cmd): mount the skill read roots the provider vouches for; a repo symlink cannot widen the fence`

---

## 3. The provider client never follows a redirect (F-18) — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the plan's discovery case says a `301` on `/v1/models` makes `Discover` "fail
the same way". It cannot be the same error TYPE: `discoverModels` has never produced a
`*StatusError` — its non-200 branch is `fmt.Errorf("apogee: model discovery: upstream HTTP %d")` —
and giving it one is a wire-shape change no item asked for. The subtest therefore asserts the same
BEHAVIOUR (the error names the 301, the redirect target is never hit) without the type assertion.
NOTES (2026-08-26): the three cases landed as four subtests of one `TestClientNeverFollowsARedirect`
(Respond and Stream split, because Stream's 3xx is a `DeltaError`, not a returned error). The
refusal was checked to be load-bearing by temporarily restoring the bare `&http.Client{}` and
confirming the Respond, Stream and Discover subtests all fail.

**What:** the LLM endpoint's HTTP client takes the same redirect policy the network tools and
the MCP transports already have.
- `internal/provider/client.go` `NewClient` (`:177-190`): the default `httpClient` becomes
  `&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return
  http.ErrUseLastResponse }}` — still no client-level `Timeout` (the comment at `:182` stays).
  A 3xx therefore comes back as the response itself, which `Respond`'s existing non-200 check
  (`:262`) surfaces as a `*StatusError{Code: 3xx}`; `send` (`:316`) does not retry it
  (`isRetryableStatus` is 429/5xx). Add a doc paragraph on `NewClient`: the per-Turn POST
  carries the whole conversation, so a redirect from the endpoint must never re-aim it (F-18),
  and a server that redirects has to be configured at the URL it redirects to — the same
  sentence `internal/mcp/transport.go:212-213` uses.
- `WithHTTPClient` (`:129`): amend its comment — an injected client is the embedder's, and
  carries the embedder's redirect policy; the default one refuses redirects.
- No `PinnedDialControl` on this client (design call 6). Record the reason in the same
  `NewClient` doc paragraph in two sentences, so the next audit does not re-open it without
  the trade-off in view.
- `CHANGELOG.md` `[Unreleased]` → `### Fixed`: the provider client no longer follows
  redirects; a redirecting endpoint is reported as an HTTP 3xx error naming the status.

**Files:** `internal/provider/client.go`, `internal/provider/client_test.go`, `CHANGELOG.md`

**Tests:** `client_test.go` — `TestClientNeverFollowsARedirect`: an `httptest` server whose
`/v1/chat/completions` answers `307` with `Location: /elsewhere` and whose `/elsewhere`
handler counts hits; `Respond` returns an error that `errors.As` a `*StatusError` with
`Code == 307`, the counter stays 0, and the same holds for `Stream` (both go through `send`).
A second case: `301` on `/v1/models` makes `Discover` fail the same way (the models path uses
the same client). A third case: a client built `WithHTTPClient(&http.Client{})` DOES follow —
pinning that the policy lives on the default, not on the option.

**Acceptance:** `go build ./... && go test ./internal/provider/`

**Commit:** `fix(provider): the LLM client never follows a redirect; a 3xx surfaces as the status error`

---

## 4. MCP connects under the configured `url-safety:` guard, at boot and on reconnect (F-40) — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the plan asks for "one paragraph" on the comment at `:41-51`; it landed as two,
one per call site — the startup connect's says what the guard now is and what it does not disable
(the floor exemption and the pin), the reconnect closure's says why its guard is read from
`toolSet.built()` rather than from the snapshot the closure closed over. The two sites now sit apart
in the file, so one shared paragraph would have had to be read at the wrong one.
NOTES (2026-08-26): `mcpGuard` carries the receiver the plan's signature names but does not use it —
it is a seam against drift between the two call sites, not a read of the wiring. Kept as specified
rather than demoted to a package function, so the plan's text and the code agree.
NOTES (2026-08-26): both halves were checked to be load-bearing by temporarily neutralising each
(`mcpGuard` returning `security.URLGuard{}`, and the reconnect closure reading `w.cfg` instead of
the live spec) and confirming the matching test fails with the connection-refused error the zero
guard produces.

**What:** the zero guard at `cmd/apogee/wire_live.go:41` and `:51` becomes the operator's.
- `cmd/apogee/wire_live.go:41`: `mcp.Connect(ctx, w.opts.MCPServers,
  security.NewURLGuard(w.cfg.URLAllowHosts, w.cfg.URLDenyHosts))` — the same constructor and
  the same two fields `registryWithMCP` reads at `wire_tools.go:220`, so the MCP endpoint check
  and the network tools can never disagree about which hosts are closed.
- The reconnect closure (`:51`) runs later, when a `mcp-servers:` edit lands (`wire_mcp.go:83`),
  by which time the lists may have moved through `/settings` — so it builds its guard from the
  LIVE spec: `spec := w.toolSet.built(); security.NewURLGuard(spec.allowHosts, spec.denyHosts)`
  (`liveTools.built`, `wire_tools.go:163`, is lock-guarded). Hoist both constructions into one
  unexported `func (w *rootWiring) mcpGuard(allow, deny []string) security.URLGuard` so the
  two call sites cannot drift; the comment at `:41-51` gains one paragraph: the endpoint is
  checked against the operator's host lists (scheme/host allow-deny; the floor is disabled for
  it and the connection pinned — `internal/mcp/transport.go:188-204`, ADR 0012 amendment
  2026-07-26), so a denied host is refused at startup with the url-safety message.
- `CHANGELOG.md` `[Unreleased]` → `### Fixed`: `url-safety:` `allow-hosts`/`deny-hosts` now
  apply to `sse`/`streamable-http` MCP endpoints, at startup and on a live `mcp-servers:`
  edit; before, the MCP check ran against an empty guard.

**Files:** `cmd/apogee/wire_live.go`, `cmd/apogee/wire_live_test.go` (new), `CHANGELOG.md`

**Tests:** `wire_live_test.go` — `TestWireSessionConnectsMCPUnderTheConfiguredURLGuard`: the
`newRootWiring` fixture with `Options{URLDenyHosts: []string{"localhost"}, MCPServers:
[{Name: "denied", Transport: sse, Endpoint: "http://localhost:1/mcp"}]}`; `wireSession`
returns an error containing `blocked by url-safety` (the deny is judged BEFORE any dial, so a
connection-refused error would mean the guard is still zero — that is the failing assertion
today). `TestMCPReconnectUsesTheLiveURLSafetyLists`: a wiring with no MCP servers; after
`w.toolSet.setDenyHosts([]string{"localhost"}, engine)` (the fake engine `wire_test.go` already
uses for `TestApplySettingURLSafetyHostsSwapTheSet`, `:1361`), `w.mcpSet.reconnect([]mcp.ServerConfig{
that same entry}, w.toolSet, engine)` fails with `blocked by url-safety`; with an empty deny
list the same reconnect fails with a connection error instead (proving the guard, not the
network, made the difference).

**Acceptance:** `go build ./... && go test ./cmd/apogee/ -run 'MCP|URLSafety|URLGuard'`

**Commit:** `fix(cmd): MCP endpoints are checked under the configured url-safety guard, live lists included`

---

## 5. Network tools and MCP transports honour `HTTP(S)_PROXY`/`NO_PROXY`; the dial pins the proxy (F-41) — ✅ DONE (2026-08-26)

NOTES (2026-08-26): `docs/design/mcp-client.md` is not in the item's Files list, but its
"pinned to that endpoint's own resolved addresses" bullet became untrue the moment the pin grew
the proxy host, so one clause was added there — the design doc the change affects (procedure
step 6), not new scope.
NOTES (2026-08-26): `newHTTPClient` takes `ctx` as its first parameter (five in all) — the plan
names the target URL and the proxy func but the pin resolves through the guard's resolver, which
needs a ctx to bound the lookup. It also defaults a nil proxy to `http.ProxyFromEnvironment`
itself rather than through a helper on `networkTool`, so the "nil means the environment" contract
lives in one place.
NOTES (2026-08-26): a proxy resolver's own error is deliberately NOT interpolated into the
model-facing message in either package — the resolver quotes the proxy value back and a proxy URL
may carry credentials, the same reasoning `mcp`'s `checkEndpoint` already rests on. The message is
the bare "the configured egress proxy is not a usable URL", rendered through the pre-flight's
`blockedMessage` shape.
NOTES (2026-08-26): `TestPinnedDialControl_PinsEveryNamedHost` also carries a "no hosts at all
fails closed" subtest — the zero-host branch is new with the variadic signature and the existing
failure table (which the plan asked to leave compiling unchanged) passes an empty host string, not
an empty list.
NOTES (2026-08-26): the MCP test's second case exercises the NO_PROXY shape (the seam returns the
proxy for the endpoint host only), because with `http.ProxyURL` every destination — including the
"different private address" the plan names — would be carried by the proxy and so reach it. As
written, the unproxied private address dials direct and still meets the floor, which is the bound
the case is there to pin.
NOTES (2026-08-26): all four halves were checked to be load-bearing by temporarily neutralising
each (`Transport.Proxy` dropped, and the proxy host removed from the pin, in both packages) and
confirming `TestWebFetch_ProxiedDialPinsTheProxy` / `TestGuardedClient_ProxiedEndpointPinsBothHosts`
fail with the connect error the missing half produces.

**What:** the two model-facing client builders gain the proxy the LLM client already honours,
without loosening the dial-time floor (design call 7).
- `internal/security/ssrf.go` `PinnedDialControl` (`:290`): the signature becomes
  `PinnedDialControl(ctx context.Context, hosts ...string)`; every host is resolved (or
  parsed as an IP literal) and the union is the permitted set; zero hosts, or a host that
  resolves to nothing, stays the fail-closed `ErrURLBlocked`. The one production caller
  (`internal/mcp/transport.go:166`) and the tests at `ssrf_test.go:363-470` compile unchanged
  (one host is the variadic's first element). Doc: "hosts are the addresses a caller has made
  a HOST trust decision about — the endpoint, and the egress proxy that carries it".
- `internal/tools/network.go`: `networkTool` (`:64`) gains `proxy func(*http.Request)
  (*url.URL, error)`; nil means `http.ProxyFromEnvironment` (read at use, so the constructors
  `NewWebFetch`/`NewHTTPRequest`/`NewWebSearch` change no signature). `newHTTPClient`
  (`:298`) takes the target `*url.URL` (already parsed by the pre-flight at `:180-190`) and
  the proxy func: `proxyURL := proxy(&http.Request{URL: target})`; when non-nil the dialer's
  `Control` is `guard.PinnedDialControl(rctx, proxyURL.Hostname())` — a pin failure is the
  same `blockedMessage` the pre-flight uses — else `guard.SafeDialControl()` as today; the
  `http.Transport` gets `Proxy: proxy`. The doc comment states the order of judgement: the
  pre-flight judges the DESTINATION (string lists + resolved-IP floor), the dial-time control
  judges what is actually DIALLED (the proxy's pinned addresses when one applies, the
  destination under the blanket floor otherwise); a redirect is still never followed.
- `internal/mcp/transport.go` `vetEndpoint` (`:157`): compute the proxy for the endpoint the
  same way through an unexported package var `proxyForRequest = http.ProxyFromEnvironment`
  (the test seam — tests that swap it do not run in parallel); pin
  `guard.PinnedDialControl(ctx, u.Hostname(), proxyHost…)` (the proxy host only when one
  applies); `newGuardedHTTPClient` (`:220`) sets `Transport.Proxy: proxyForRequest`. Amend the
  builder's doc (`:206-218`) with the same order-of-judgement sentence.
- `CONTEXT.md` **url-safety** clause in the safety-guardrails entry (`:748-752`): one
  sentence — the network tools and MCP transports honour the process's `HTTP(S)_PROXY`/
  `NO_PROXY`; the guard judges the destination before the request leaves and pins the proxy's
  own addresses for the dial. The MCP entry (`:805-808`) gains "and through the configured
  egress proxy" beside "redirects are not followed".
- `CHANGELOG.md` `[Unreleased]` → `### Fixed`: the network tools and MCP HTTP transports now
  honour `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` like the LLM client; `url-safety` still judges
  the destination, and the proxy's own address is what the dial is pinned to.

**Files:** `internal/security/ssrf.go`, `internal/security/ssrf_test.go`,
`internal/tools/network.go`, `internal/tools/network_test.go`, `internal/mcp/transport.go`,
`internal/mcp/transport_test.go`, `CONTEXT.md`, `CHANGELOG.md`

**Tests:** `ssrf_test.go` — `TestPinnedDialControl_PinsEveryNamedHost`: two hosts through a
fixed resolver; both address sets pass, a third private address is refused; the one-host
cases at `:363-470` unchanged. `network_test.go` — `TestWebFetch_ProxiedDialPinsTheProxy`: an
`httptest` server acting as a forward proxy (it sees an ABSOLUTE request URI and `r.Host ==
"example.test"`, answers `200 via proxy`); the tool's `proxy` is `http.ProxyURL(proxySrv)`,
its guard resolver maps `example.test` to a public IP; fetching `http://example.test/` returns
`via proxy` — the proxy is on loopback, so the assertion also proves the pin (the blanket floor
would refuse the dial). `TestWebFetch_ProxyDoesNotLaunderAPrivateDestination`: same proxy,
target `http://10.0.0.1/` → the pre-flight `blocked` message, the proxy handler never runs.
`TestWebFetch_NoProxyDialsDirectUnderTheFloor`: proxy func returns nil → existing behaviour
(reuse the fixtures of `TestWebFetch_DoesNotFollowRedirectToPrivate`, `:115`).
`transport_test.go` — `TestGuardedClient_ProxiedEndpointPinsBothHosts` beside
`TestGuardedClient_PinsTheEndpointAndRefusesEverythingElsePrivate` (`:156`): with
`proxyForRequest` swapped to a loopback forward proxy, the endpoint connects through it and
a request to a DIFFERENT private address through the same client is refused.

**Acceptance:** `go build ./... && go test ./internal/security/ ./internal/tools/ ./internal/mcp/`

**Commit:** `fix(tools,mcp): honour HTTP(S)_PROXY/NO_PROXY on model-driven egress; the dial pins the proxy, the guard still judges the destination`

---

## 6. The doc-server capability token stays in the transcript entry (F-21) — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the item names `presenter_test.go`'s `TestPresenterLadderPicksRung`;
`TestBridgeSetPresentationSwapsTheLadderInPlace` (same file, `:426`) also asserted
`out.Location` held the served URL and had to move that assertion onto the last `presentedMsg` —
same proof, the only surface that still carries the URL.
NOTES (2026-08-26): `transcriptcodec_test.go`'s `mixedEntries` fixture (`:74`) was
served-with-a-URL, which the round-trip's DeepEqual can no longer satisfy; per the item's
"adjust the fixture" clause it now carries the OPENED rung (the shape every non-rung-2
presentation has), and the served case is pinned by the new
`TestTranscriptCodecDropsTheServedURLOnEncode`.

**What:** the served URL — `/d/<token>/<basename>` — is relayed by rung 0 alone; the tool
result, the upstream POST and the session record carry only the rung and the display path.
- `internal/domain/present.go` `PresentOutcome.Location` (`:83-85`): the doc becomes "the
  DisplayPath, on every rung — never a served URL: the tool result is model context, sent
  upstream on the next Turn and persisted with the session, and the doc-server URL carries a
  capability token (ADR 0019 §3). Where the user finds a served document is the transcript
  entry's to say." No field is removed (the struct is a freeze-safe shape).
- `internal/tui/presenter.go` `Present` (`:124-129`): the outcome's `Location` is
  `req.DisplayPath` unconditionally; `location` (the served URL) reaches `presentedMsg` only.
  Amend the comment at `:124-126`.
- `internal/tools/present_document.go` `renderPresented` (`:137-147`): the served wording
  becomes exactly `Presented <display>: shown in the transcript with a link.` — `Location` is
  never interpolated on any rung (defence in depth: a Presenter that still hands a URL cannot
  leak it through this tool). Update the function's doc comment; drop the "served rung with
  no URL" degrade — a served rung now always reads the served sentence.
- `internal/tui/transcriptcodec.go` `toWirePresented` (`:483-489`): `Location` is written
  EMPTY when `pv.Method == domain.PresentServed` — the doc server and its grants die with the
  process (ADR 0019 §3: lazily started, closed on shutdown), so a persisted URL is dead on
  every resume and the token in it is the only thing the record would keep. Decode
  (`:687-693`) is unchanged; a restored served entry renders its path line and status as
  today (`startupbox.go:43-44` prints no Location line when it is empty).
- `docs/adr/0019-documents-are-presented-not-opened.md`: a dated addendum (≤ 6 lines) under
  §3 — the token's whole reach is the live transcript entry; the tool result, the wire and the
  session record never carry it (2026-08-25 audit F-21).
- `CHANGELOG.md` `[Unreleased]` → `### Fixed`: the doc-server link (and its capability
  token) no longer appears in `present_document`'s tool result or in the saved session; the
  transcript entry still shows it.

**Files:** `internal/domain/present.go`, `internal/tools/present_document.go`,
`internal/tools/present_document_test.go`, `internal/tui/presenter.go`,
`internal/tui/presenter_test.go`, `internal/tui/transcriptcodec.go`,
`internal/tui/transcriptcodec_test.go`, `docs/adr/0019-documents-are-presented-not-opened.md`,
`CHANGELOG.md`

**Tests:** `present_document_test.go` `TestPresentDocument_OutcomeWordingPerRung` (`:58`): the
`served` row's outcome carries a `/d/0123…/report.html` URL and `want` is the new sentence;
add an assertion that `res.Content` contains neither `/d/` nor the token; the "served
without a location" row now expects the served sentence too. `presenter_test.go`
`TestPresenterLadderPicksRung` (`:286-298`): the served case asserts `out.Location ==
req.DisplayPath` while `msg.Location` still carries the URL. `transcriptcodec_test.go` — a
new `TestTranscriptCodecDropsTheServedURLOnEncode`: encode a served `presentedView` whose
Location is `http://192.168.64.2:8080/d/<32 hex>/report.html`; the JSON bytes contain no
`/d/` and no token; decode yields `Method == PresentServed`, `Location == ""`, `Path` intact;
an `opened`/`shown` entry round-trips unchanged; adjust the fixture at `:76` if its Method is
served.

**Acceptance:** `go build ./... && go test ./internal/domain/ ./internal/tools/ ./internal/tui/`

**Commit:** `fix(present): the doc-server token stays in the transcript — never in the tool result, the wire or the session record`

---

## 7. Manual and CONTEXT truth: four placeholders, a trusted `model:`, root-command trace flags (R-1, R-2, R-4) — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the three PROPOSED texts are reproduced verbatim in wording; only the
line wrapping differs, chosen so each phrase the item's Acceptance greps for
(`is a **trusted** id, never substituted`, ``they sit on the root command, not on `apogee probe` ``)
sits on ONE line — a natural re-wrap would have split both across a newline and failed the check.
Line widths stay inside each file's existing maximum (configuration.md ~80, CONTEXT.md ~108,
probe.md ~97).

**What:** three stale claims, each replaced by the PROPOSED text from
`docs/skill-runs/refocus/2026-08-25/corrections.md` verbatim. Docs-only commit, no `make check`.
- `docs/manual/configuration.md:540-545` — replace the paragraph beginning "Three placeholders
  are substituted fresh on every request" with:
  "Four placeholders are substituted fresh on every request: `{{workspace}}` (the
  workspace path), `{{datetime}}` (today's **date** — not a timestamp, which would
  change the prompt every turn and throw away your server's prefix cache), `{{mode}}`
  (the autonomy mode, so a Shift+Tab shows up from the next request on), and
  `{{scratch}}` (this session's scratch directory). The spelling is strict and the set
  is closed — anything else in double braces, `{{ workspace }}` included, is a startup
  error listing the four."
- `CONTEXT.md:1264-1267` (§ Heartbeat, the **Rebind** paragraph) — replace from "a `servers:`
  entry's `model` is a **hint**" through "config named at launch." with:
  "a `servers:` entry's `model` is a **trusted** id, never substituted: whenever it is set
  it is the active model verbatim, and an advertised entry supplies only its context window
  (an id the server does not list runs as configured, with no window known). Only an empty
  `model` falls back to the first model the server advertises — and the resolved id
  **follows the binding**, restated on every commit, so discovery keeps resolving the model
  the session actually runs rather than the one config named at launch."
- `docs/manual/probe.md:67-68` — replace "**When a frame comes out wrong**, two hidden flags
  record the evidence a rendering bug is argued from —" with:
  "**When a frame comes out wrong**, two hidden flags on `apogee` itself — they sit on the
  root command, not on `apogee probe` — record the evidence a rendering bug is argued from —"
  (rest of the paragraph unchanged).

**Files:** `docs/manual/configuration.md`, `CONTEXT.md`, `docs/manual/probe.md`

**Tests:** none (docs-only).

**Acceptance:** `grep -c 'Four placeholders' docs/manual/configuration.md | grep -q 1 && ! grep -q 'Three placeholders' docs/manual/configuration.md && grep -q 'is a \*\*trusted\*\* id, never substituted' CONTEXT.md && ! grep -q 'is a \*\*hint\*\*$' CONTEXT.md && grep -q 'they sit on the root command, not on `apogee probe`' docs/manual/probe.md`

**Commit:** `docs(manual,context): four placeholders, a trusted model id, root-command trace flags`

---

## 8. README status and mechanisms lines (R-5, R-6, R-7) — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the status line took the item's RECOMMENDED wording
("**Pre-production `0.x` on `main`.**", which names no series) rather than
corrections.md §5's literal `v0.17.x` — the item's own text and its Acceptance both
name the recommended form, so this is the instruction, not a deviation from it.
NOTES (2026-08-26): the mechanisms bullet and the status sentence are the item's verbatim
text, re-wrapped to README's ~76-column prose width; no word changed.

**What:** three README sentences corrected; docs-only commit, no `make check`.
- `README.md:209` — replace "**`v0.16.x` on `main` — pre-production.**" with
  "**Pre-production `0.x` on `main`.**" — the sentence names no series, so it cannot go stale
  at the next minor bump; `VERSION` and the CHANGELOG are where the number lives (this is the
  recommended wording; corrections.md §5's literal `v0.17.x` is the alternative — see OPEN
  CALLS). The rest of the paragraph ("Under SemVer a `0.x` version …") is unchanged.
- `README.md:165-167` — replace the mechanisms bullet with, verbatim:
  "- **Small-model mechanisms** — context compaction is built in and structural, not one of
    them; all 21 catalogued mechanisms ship off until bench evidence turns them on, and
    Validated sets apply the measured winners per model automatically."
- `README.md:214-215` — replace "What changed lately and what is next live in the
  [CHANGELOG](CHANGELOG.md) and [`docs/plans/`](docs/plans/)." with, verbatim:
  "What changed lately lives in the [CHANGELOG](CHANGELOG.md); what is next lives in
  [`ISSUES.md`](ISSUES.md)."

**Files:** `README.md`

**Tests:** none (docs-only).

**Acceptance:** `grep -q 'Pre-production `0.x` on `main`' README.md && ! grep -q 'v0.16.x' README.md && grep -q 'all 21 catalogued mechanisms' README.md && ! grep -q 'the other 20' README.md && grep -q 'what is next lives in' README.md && ! grep -q 'docs/plans/' README.md`

**Commit:** `docs(readme): status names no series; compaction is structural, catalogue is 21; what is next is ISSUES.md`

---

## 9. Manual sections for `url-safety:`, `web-search-endpoint:` and `use-project-skills:` (R-undoc, config keys)

Depends on items 4, 5.

**What:** three new `##` sections in `docs/manual/configuration.md`, inserted after the
`tools:` roster paragraphs (`:66-99`) and before the Compaction paragraph (`:101`), each
written from the code — the implementer re-reads the cited anchors and states what they do,
in the manual's voice (prose first, one YAML fence, no bullet lists of flags). Docs-only
commit, no `make check`.
- `## What the network tools may reach — url-safety:` — the `url-safety:` block with its two
  lists (`allow-hosts`, `deny-hosts`; registry `:317-329`, template `:426-445`): they bind
  `web_fetch`, `http_request`, `web_search` AND (item 4) an `sse`/`streamable-http`
  `mcp-servers:` endpoint; an entry matches the host and its subdomains; entries are
  normalised the way the guard normalises a dialled host (trimmed, lower-cased, IDNA-mapped,
  trailing dot dropped, IPv6 brackets stripped — `urlsafety.go:224-245`); deny wins over
  allow; an empty allow list means every host. Then the part no list can change: the
  **always-on SSRF floor** — loopback, private, link-local, metadata, CGNAT and the other
  ranges CONTEXT.md `:748-752` enumerates — judged by RESOLVED address before the request and
  again at dial time; that a configured MCP endpoint is exempt from the floor and pinned to
  its own addresses instead (ADR 0012 amendment 2026-07-26) while your lists still apply to
  it; that redirects are never followed (the model sees the `Location` and may ask again);
  that (item 5) the process's `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` are honoured by these
  tools, the MCP transports and the LLM client alike, the destination still being judged
  before anything leaves; that the LLM endpoint itself is NOT subject to the lists; and that
  both lists are live in `/settings` (rows `url-safety.allow-hosts`/`deny-hosts`, a commit
  rebuilds the tool set — `wire_tools.go:139-155`). One YAML fence copied from the template
  (`:443-445`).
- `## Where web_search looks — web-search-endpoint:` — a single URL string (registry `:272-276`,
  template `:362-364`): unset means the built-in DuckDuckGo provider; the value `off` (also
  `none`/`disabled`, case-insensitive — `web_search.go:117-130`; state what the tool then
  answers, read off `:54-60`) turns the tool into a graceful refusal without taking it off
  the menu (use `tools.disabled` for that); a URL must parse (`validateSearchEndpoint`,
  registry `:557`); the endpoint is subject to `url-safety:` like any other; it is a file key
  with no flag or env, live from `/settings` (`wire_tools.go:96`). Fix the intro paragraph
  (`:11`) to link this section where it says "the web-search endpoint".
- `## Skills a repository ships — use-project-skills:` — the boolean (default `true`;
  registry `:331-335`, template `:448-453`; `skills/load.go:102-115`): the global library
  `~/.apogee/skills` and the project's `.apogee/skills` are ALWAYS scanned; this key adds the
  project's bare `skills/` folder. Write the trust decision plainly: a skill is prompt text
  the repository's author wrote, offered in your `/` menu and prepended to your message when
  you invoke it — cloning a repo means accepting that its skills are on your menu; a repo can
  add a new id but never replace one in your global library (home wins collisions, ADR 0032,
  and the displaced one is listed by `/skills`); the skill folders are mounted read-only for
  the model's read tools, and (items 1–2) a folder that is a symlink leaving the workspace is
  neither loaded nor mounted. The flip is live: `/settings` or a file save changes where the
  next scan looks. Link `commands.md`'s `/skills` row and `{{SKILL_DIR}}` section.
- `docs/manual/README.md` index row for Configuration: append "url-safety, web search, project
  skills" to its Covers cell.

**Files:** `docs/manual/configuration.md`, `docs/manual/README.md`

**Tests:** none (docs-only). The implementer verifies each stated behaviour against the cited
code before writing it; a sentence the code does not support is left out, not softened.

**Acceptance:** `grep -q '^## .*url-safety:' docs/manual/configuration.md && grep -q '^## .*web-search-endpoint:' docs/manual/configuration.md && grep -q '^## .*use-project-skills:' docs/manual/configuration.md && grep -q 'HTTPS_PROXY' docs/manual/configuration.md && grep -q 'url-safety' docs/manual/README.md`

**Commit:** `docs(manual): document url-safety, web-search-endpoint and use-project-skills with their live semantics`

---

## 10. Manual: the environment overrides and the built-in tool names (R-undoc, env + roster)

**What:** two additions to `docs/manual/configuration.md`, and the drift test that keeps the
second one true (design call 4). Not a docs-only commit: the test rides with the text.
- A new `## Environment overrides` section placed directly after the precedence intro
  (`:1-18`), which today names only some of the variables. It lists, in prose, every
  `APOGEE_*` the binary reads (`config.go:2343-2352`) with its flag and precedence: the three
  registry-backed keys `APOGEE_SERVER` (`--server`), `APOGEE_MODE` (`--mode`), `APOGEE_BYPASS`
  (`--bypass`) — flag > env > file > default; the startup-server overrides `APOGEE_ENDPOINT`
  (`--endpoint`), `APOGEE_MODEL` (`--model`), `APOGEE_API_KEY` (no flag, on purpose — link
  the API-key section); and the two the file cannot set because they say WHERE the file and
  the workspace are: `APOGEE_CONFIG` (`--config` > env > `~/.apogee`) and `APOGEE_WORKSPACE`
  (`--workspace` > env > the current directory — `config.go:2464-2468`), the latter being the
  root every file tool is fenced to. `APOGEE_BYPASS` gets its own paragraph that says plainly
  what it does: it turns apogee's **Mechanisms off for the whole session** — every catalogued
  mechanism and every Validated set is silent, so a small model runs with none of the help
  apogee exists to give it (the honest "Mechanisms-off" floor the bench measures against, ADR
  0006) — while the structural parts (context compaction, the Budget, the empty-response
  off-ramp) stay on; a set-but-unparseable value is a startup error, not a silent `false`;
  the same switch is the `bypass` row in `/settings`, live. Cross-link CONTEXT.md **Bypass
  mode**. Do not mention `APOGEE_LIVE_ENDPOINT` (a developer test gate, documented in
  `building.md` if anywhere).
- The `tools:` roster paragraph (`:66-99`): after "The names are the ones the model calls a
  tool by", add the full list of names this build knows, generated from `KnownToolNames()`
  (`internal/tools/registry.go:386`) in MENU order — the implementer runs
  `go test ./internal/tools/ -run TestKnownToolNamesCoversTheComposedSet -v` or a one-line
  throwaway test to print it and copies the output; the expected 31 are `read_file`,
  `write_file`, `list_dir`, `grep`, `find_files`, `single_find_and_replace`,
  `multi_find_and_replace`, `edit_existing_file`, `view_diff`, `copy_file`, `move_file`,
  `delete_file`, `terminal`, `python_exec`, `git_branch`, `git_commit`, `git_diff_range`,
  `git_status`, `git_log`, `diagnostics`, `run_tests`, `web_fetch`, `http_request`,
  `web_search`, `sub_agent`, `console_open`, `console_send`, `console_read`, `console_close`,
  `ask_user`, `present_document` — mark the Console four as default-off and `ask_user`/
  `present_document` as host-supplied (absent in `headless`/`daemon`, so naming them there
  is a notice). State that the list is the one `tools.disabled`, `tools.enabled` and a
  profile's `tools:` axis are checked against, and that a name outside it is the startup
  notice the paragraph already describes. Keep the marker sentence "(as of this build;
  `KnownToolNames` in `internal/tools` is the source, and a test fails when this list falls
  behind it)" beside the list.
- `internal/tools/manual_drift_test.go` (new): `TestManualListsEveryKnownToolName` — reads
  `../../docs/manual/configuration.md` relative to the package dir (`os.ReadFile`; a missing
  file is a `t.Fatal`, never a skip — the repo layout is fixed), and asserts every
  `KnownToolNames()` entry appears in it wrapped in back-ticks (`` "`" + name + "`" ``),
  naming each missing tool in one `t.Errorf`. About fifteen lines; no other doc is parsed. It
  is the first test in the repo that reads a manual page — say so in its doc comment, so the
  next hand-maintained list has a shape to copy.

**Files:** `docs/manual/configuration.md`, `internal/tools/manual_drift_test.go`

**Tests:** `TestManualListsEveryKnownToolName` passes against the list the item writes; removing
any one name from the manual (e.g. `git_diff_range`) makes it fail naming that tool.

**Acceptance:** `go test -run TestManualListsEveryKnownToolName ./internal/tools/ && grep -q '^## Environment overrides' docs/manual/configuration.md && grep -q 'APOGEE_WORKSPACE' docs/manual/configuration.md && grep -q 'APOGEE_BYPASS' docs/manual/configuration.md && grep -q 'Mechanisms off for the whole session' docs/manual/configuration.md && grep -q '`present_document`' docs/manual/configuration.md && grep -q '`git_diff_range`' docs/manual/configuration.md`

**Commit:** `docs(manual): every APOGEE_* override, what APOGEE_BYPASS gives up, and the built-in tool names (+ drift test)`

---

**Suggested version bump (not performed):** patch — `0.17.4`. Items 1–6 are security fixes
that change no config key and no wire shape (one tool-result sentence and one persisted field
now omitted); items 7–10 are documentation, item 10 with a drift test. The bump is the owner's call, after the run.
