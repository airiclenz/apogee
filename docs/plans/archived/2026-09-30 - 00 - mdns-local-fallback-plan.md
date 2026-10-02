# mDNS `.local` fallback for release builds — plan

**Goal:** Release binaries (`make dist`, `CGO_ENABLED=0`) reach an LLM endpoint named by an mDNS
`.local` host, and a host name that does not resolve is reported as such instead of a raw
`lookup … no such host` behind "server offline".
**Date:** 2026-09-30
**Status:** done (status corrected 2026-10-02)
**sized for:** ~200k-context host
**base:** 627d8c27
**Closes:** bd apogee-mdns-local-unresolved

**Regression check (2026-09-30, 627d8c27):**
- 2: guard folded (wrap the cloned DefaultTransport's own DialContext; mandatory per-address dial seam, seam-swapping tests serial)
- 3: guard folded (ctx-cancelled/deadline DNS errors keep err.Error(); `internal/notice/doc.go` names UnresolvedHost)

**Sources:**
- `.beads/issues.jsonl` — `apogee-mdns-local-unresolved` (symptom, cause, fix directions)
- RFC 6762 §5.1 (one-shot multicast DNS queries) and §6.7 (legacy unicast responses)
- `docs/adr/0042-*` (external programs are optional, never prerequisites)

**Ratified design calls** (user, 2026-09-30):
- **Resolution:** apogee resolves `.local` itself with a one-shot mDNS query; no cgo release builds, no shelling out to avahi/dns-sd.
- **Scope:** the fallback fires only for hosts ending in `.local` (case-insensitive, trailing dot allowed), and only after the system resolver returned an error. Every other host, and every `.local` host the system resolves, behaves exactly as today.
- **Wording:** the refusal keeps its headline and gains a DNS detail: `cannot send — server offline (http://Apollo-II.local:1111): host name Apollo-II.local did not resolve — use the server's IP address or add it to /etc/hosts`.
- **Transport** (plan author, 2026-09-30): one package-level transport shared by every `provider.NewClient` client, never one per client (title/judge build short-lived clients; a private pool each would strand idle sockets).
- **Family** (plan author, 2026-09-30): the query goes to IPv4 multicast `224.0.0.251:5353` only; A and AAAA answers in the reply are both used.

**Standing requirements:**
- skills: coding-standards
- Tests on this machine: never `-race` over a package pattern; narrow to one package and `-run` one test (`GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./internal/<pkg>/`); never two test runs at once. Pass this to every sub-agent.
- Deviations from item text land as a dated NOTES line under the item.

**Out of scope:**
- llama-launcher module's own HTTP clients (`authedGet`, `authedPostJSON`) — a separate module.
- MCP transports, web tools, webhooks, the SSRF guard's lookup.
- mDNS service discovery (browsing `_http._tcp`), caching resolved addresses, IPv6 multicast.
- Changing `make dist` / cross-build flags.

## 1. `internal/mdns`: one-shot `.local` address lookup — ✅ DONE (2026-09-30)

NOTES (2026-09-30): Lookup reads A/AAAA records from the answer and additional sections of a reply (class compared with the RFC 6762 cache-flush bit masked off); the reply's DNS ID is not checked, a matching name is required instead. The no-answer sentinel stays unexported (errNoAnswer) so callers see only Lookup.

**What:**
**Goal:** a new package `internal/mdns` exports `Lookup(ctx context.Context, host string) ([]netip.Addr, error)` that returns the A/AAAA addresses a responder on the LAN announces for `host`, and a non-nil error when none answer before the deadline.
**Approach (assumed at the header base):**
- Build the query with `golang.org/x/net/dns/dnsmessage` (already in the required `golang.org/x/net` module; adds no new module): one question each for `TypeA` and `TypeAAAA`, class IN, the host fully qualified.
- Send from an ephemeral UDP port (legacy unicast query, RFC 6762 §6.7) to `224.0.0.251:5353`; read replies until one carries a matching A/AAAA answer (name compared case-insensitively) or the deadline passes. Deadline = the earlier of ctx and 1 s.
- Ignore malformed packets and answers for other names; never panic on hostile input.
- The destination address is a field on an unexported resolver struct so tests aim at a loopback responder; `Lookup` uses the package default.
- One deep module: callers see only `Lookup`; packet building/parsing stays unexported.
**Files:** `internal/mdns/mdns.go`, `internal/mdns/mdns_test.go`
**Read first:** go.mod — require golang.org/x/net v0.55.0 (dnsmessage already in the module cache, go.sum carries the h1 hash); internal/security/urlsafety.go — existing golang.org/x/net/idna import (precedent for x/net in internal/);
internal/provider/client.go — NewClient (the one consumer item 2 adds)
**Tests:** loopback UDP fake responder: answers A → address returned; answers AAAA only → returned; answers another name → times out with error; malformed packet then valid answer → valid answer wins; ctx cancelled → returns promptly with error; name match is case-insensitive.
**Acceptance:**
- `go build ./internal/mdns/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/mdns/`
- `go vet ./internal/mdns/`
**Commit:** `feat(mdns): add one-shot .local address lookup`

## 2. Provider transport falls back to mDNS for `.local` hosts — ✅ DONE (2026-09-30)

NOTES (2026-09-30): when mDNS answers but no returned address connects, the dial returns the last address's dial error (a real "server offline"), not the original *net.DNSError; the DNSError is kept only when the host is not `.local`, the failure is not DNS, or mDNS finds nothing — as the Goal requires.
NOTES (2026-09-30): the dial seam `dialAddress` takes the captured original DialContext as an argument, so a stub resolves host names and still dials IP literals through that captured dialer; `withLocalFallback(t)` is the testable wrapper that `providerTransport` applies to the DefaultTransport clone.

**What:** fixes the defect in `apogee-mdns-local-unresolved`: a `CGO_ENABLED=0` binary cannot dial an endpoint named `*.local`.
**Regression guard.** Clone `DefaultTransport`, capture its `DialContext` (`orig := t.DialContext`, the 30 s-timeout `net.Dialer`) and wrap THAT for both the first dial and every per-address fallback dial; never construct a fresh zero `net.Dialer` (drops the 30 s connect cap).
The dial seam is mandatory, stubbed per address (host name → stub result, IP literal → real dial). Tests that swap the dial seam or the mDNS lookup var stay serial (no `t.Parallel`; the package's ~200 parallel tests share the transport) and restore via `t.Cleanup`.
**Goal:** a client built by `provider.NewClient` without `WithHTTPClient` dials a `.local` endpoint that the system resolver fails on at the address `mdns.Lookup` returns; when mDNS also fails, the error the caller sees still unwraps to the system resolver's `*net.DNSError`.
**Approach (assumed at the header base):**
- A package-level transport in `internal/provider` built once (`http.DefaultTransport.(*http.Transport).Clone()`) with `DialContext` wrapped: dial through a `net.Dialer` as today; on an error that unwraps to `*net.DNSError` for a `.local` host (ratified Scope), call `mdns.Lookup` and dial each returned address with the original port until one connects. mDNS failure → return the original dial error unchanged.
- The mDNS lookup is a package-level func variable (unexported) so tests substitute it; a dial seam likewise (mandatory — see Regression guard).
- `NewClient`'s `http.Client` uses that transport. `WithHTTPClient` still overrides it wholesale.
- Update `Client.Close`'s doc-comment caveat: it names the shared provider transport instead of `DefaultTransport`. Rule: every comment in `internal/provider` naming `DefaultTransport` as the client's pool (`grep -rn DefaultTransport internal/provider`).
**Files:** `internal/provider/client.go`, `internal/provider/localdial.go`, `internal/provider/localdial_test.go`
**Read first:** internal/provider/client.go — NewClient, WithHTTPClient, Client.Close (DefaultTransport caveat doc), send/do (httpClient.Do + transport-fault retry); internal/provider/discovery.go — Discover, discoverModels, TransportError;
internal/provider/discovery_test.go — httptest-backed Discover tests; docs/plans/archived/2026-08-20 - 00 - engine-architecture-deepening-plan.md — item NOTES choosing one shared pool over per-client transports
**Tests:** stubbed resolver failure + stubbed `mdns.Lookup` returning `127.0.0.1` against an `httptest` server reached as `http://box.local:<port>` → `Discover` succeeds; non-`.local` failing host → mDNS stub never called; mDNS stub fails → error `errors.As` a `*net.DNSError` and is a `*TransportError`; `.local` host the system resolves → mDNS never called. Tests swapping the seams run serially (no `t.Parallel`) and restore via `t.Cleanup`; one test asserts the fallback dials through the captured original `DialContext`, not a zero `net.Dialer`.
**Acceptance:**
- `go build ./internal/provider/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Local|Discover' ./internal/provider/`
**Commit:** `fix(provider): resolve .local endpoints over mDNS when the system lookup fails`
Depends on item 1.

## 3. Unresolved host names get their own offline detail — ✅ DONE (2026-09-30)

**What:** fixes the diagnosis half of `apogee-mdns-local-unresolved`: a DNS failure reads as a bare `lookup … no such host`.
**Regression guard.** Map only when `errors.As(err, &dnsErr) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)` — a cancelled/timed-out lookup is a `*net.DNSError` whose `Unwrap` yields the ctx error, and `cmd/apogee/headless_test.go` pins the ctx-reason suffix.
Extend `internal/notice/doc.go`'s ServerOffline bullet to name `UnresolvedHost` as the detail that sentence carries for an unresolved host.
**Goal:** a beat whose discovery failed with a `*net.DNSError` carries `Failure` = `host name <name> did not resolve — use the server's IP address or add it to /etc/hosts`, stays `Answered == false`, and `notice.ServerOffline(endpoint, beat.Failure)` renders the ratified sentence.
**Approach (assumed at the header base):**
- `notice.UnresolvedHost(name string) string` holds the one spelling of the detail.
- `heartbeat.Monitor.Beat`: when `errors.As(err, &dnsErr)` and the error is not a ctx cancel/deadline (see Regression guard), `Failure = notice.UnresolvedHost(dnsErr.Name)`; every other failure keeps `err.Error()`. Offline classification (`Answered`, `Throttled`) unchanged.
**Files:** `internal/notice/serveroffline.go`, `internal/notice/serveroffline_test.go`, `internal/notice/doc.go`, `internal/heartbeat/heartbeat.go`, `internal/heartbeat/heartbeat_test.go`
**Read first:** internal/heartbeat/heartbeat.go — Monitor.Beat, Beat.Failure, Beat.Answered; internal/notice/serveroffline.go — ServerOffline; internal/provider/discovery.go — TransportError, TransportError.Unwrap;
internal/heartbeat/heartbeat_test.go — TestBeatUnreachableIsObservation, TestBeatAnsweredSeparatesADeadBoxFromAnUnusableReply; internal/tui/heartbeat.go — foldBeatFailure;
cmd/apogee/wire_firing.go — routing.Beat.Failure refusal
**Tests:** notice: exact string for `Apollo-II.local`. heartbeat: monitor built with `WithHTTPClient` whose transport's `DialContext` returns `&net.DNSError{Name: "Apollo-II.local", IsNotFound: true}` → `Answered` false, `Failure` exact, and `notice.ServerOffline("http://Apollo-II.local:1111", beat.Failure)` equals the ratified sentence verbatim; connection-refused beat keeps its raw failure text; `DialContext` returning `&net.DNSError{UnwrapErr: context.Canceled, ...}` → `Failure` keeps `err.Error()`.
**Acceptance:**
- `go build ./internal/notice/ ./internal/heartbeat/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/notice/`
- `GOMEMLIMIT=2GiB go test -count=1 -run Beat ./internal/heartbeat/`
**Commit:** `fix(heartbeat): report an unresolved endpoint host by name`

## 4. Document `.local` endpoints and record the decision — ✅ DONE (2026-09-30)

**What:**
**Goal:** the manual says `.local` endpoints work in release builds via apogee's own mDNS fallback and what the unresolved-host refusal means; an ADR records the in-process resolution decision.
**Approach (assumed at the header base):**
- `docs/manual/configuration.md`: at the server `endpoint` key, a short paragraph — `.local` names resolve through the system first, then a one-shot mDNS query; if neither answers, use the IP or `/etc/hosts`.
- New ADR `docs/adr/0091-endpoint-local-names-resolve-over-mdns-in-process.md` in the house ADR format: context (CGO-off resolver, NSS unreachable), decision (the ratified calls), consequences (launcher/MCP/web clients unaffected; IPv4 multicast only).
**Files:** `docs/manual/configuration.md`, `docs/adr/0091-endpoint-local-names-resolve-over-mdns-in-process.md`
**Read first:** docs/manual/configuration.md — "The servers you run models on" (servers: endpoint key); docs/adr/0042-external-programs-are-optional-enhancements-never-prerequisites.md — decision 1 (CGO_ENABLED=0 single binary);
docs/adr/0090-workflow-stages-are-enterable-views.md — front-matter/section house format; internal/notice/serveroffline.go — ServerOffline
**Tests:** none (docs).
**Acceptance:**
- `grep -n '\.local' docs/manual/configuration.md`
- `test -f docs/adr/0091-endpoint-local-names-resolve-over-mdns-in-process.md`
**Commit:** `docs: document .local endpoint resolution over mDNS`
Depends on items 2 and 3.
