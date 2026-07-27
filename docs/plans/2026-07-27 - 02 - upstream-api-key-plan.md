# Plan — Upstream API key: the `api-key` config key, wired to every provider client

**Date:** 2026-07-27
**Status:** READY (not grilled — mechanical decisions recorded below with rationale; ground verified against the working tree 2026-07-27)
**Source:** Owner question 2026-07-27: "How can I define an API key for the LLM server in the apogee config?" — today you cannot. `provider.WithAPIKey` exists, sets `Authorization: Bearer <key>`, and redacts the key from server-echoed errors (`internal/provider/client.go:68,305-322`, tested in `reliability_test.go:161-193` and `discovery_test.go:238,278`), but **nothing calls it**: none of the four `provider.NewClient` construction sites passes a key, and there is no config key, flag, or env var. A server that requires a bearer token (llama.cpp `--api-key`, LM Studio, remote vLLM, any keyed OpenAI-compatible proxy) cannot be used.
**Track:** rides `[Unreleased]` (current `VERSION` v0.8.7; additive).
**Public API:** additive (ADR 0010): exported field `domain.Config.APIKey` (public automatically via the `apogee.Config` alias, `apogee.go:74`). Internal-only signature changes: `heartbeat.NewMonitor` and `probe.Discover` gain an apiKey parameter; `probe.Inputs`/`probe.Host` gain a field.
**Standing requirement:** `/coding-standards` is forwarded to the implementer and verifier sub-agents.

Per-item green gate:

```
gofmt -l .                                              # empty
make check                                              # vet + lint + go test -race -count=1 ./...
GOOS=windows go build ./... && GOOS=darwin go build ./...
```

**Dependencies.** Items 1 → 2 → 3 → 4 run in order; the tree is coherent and green after every item and you may stop after any completed one. (Item 3 needs only item 1, but it shares `cmd/apogee/wire.go` with item 2 — keep the order.)

**Deviations leave a trail.** Any authorized deviation gets a dated `NOTES (YYYY-MM-DD):` paragraph directly under the item heading.

**Authoritative sources**, in precedence order:
1. This plan.
2. ADR 0012 (the file-only precedent for keys the invocation environment must not set — NOT the posture here, see decisions), ADR 0024 (heartbeat owns discovery; one client per Monitor; `SetModel` rebinds, the endpoint never changes mid-session), ADR 0021 (`probe` is read-only diagnosis; `probe model` is the explicit spender).
3. The code as it stands.

---

## Decisions taken (mechanical — grounded, with rationale)

1. **Spelling: `api-key:` in config.yaml, `APOGEE_API_KEY` in the environment, NO `--api-key` flag.** Endpoint and model get flags because they are not secrets; a key on the command line lands in shell history and `ps` output on every OS, so the flag is deliberately absent. Precedence is therefore env > file, riding the existing generic resolution loop — the exact shape `host-alias` already has (a settings field the flag and env layers simply never set, `config.go:402-418` + `flagLayer` at `:867-886`; here the FILE and ENV layers set it and only the flag layer abstains).
2. **One key for the one upstream.** The key belongs to the endpoint, and apogee has exactly one upstream per session (ADR 0024: "switching servers means a new Client, not a mutated one"). No per-model or per-endpoint key map — a different server is a different invocation (env/flag/config already vary per machine). A heartbeat model REBIND keeps the same client and therefore the same key; correct, since the endpoint did not change.
3. **Empty means what it means today.** `setAuth` already no-ops on an empty key (`client.go:306-310`), so `WithAPIKey(cfg.APIKey)` is passed UNCONDITIONALLY at every site — no branch, and a keyless local server behaves byte-identically to today.
4. **Every wire that reaches the LLM server carries the key.** Four construction sites, all of them: the engine's client (`internal/agent/agent.go:103,111` — sub-agents share the parent's `a.upstream`, `subagent.go:125`, so they are covered for free), the heartbeat Monitor (`internal/heartbeat/heartbeat.go:74`), the host report's discovery probe (`internal/probe/discovery.go:55`), and `apogee probe model`'s two clients (`cmd/apogee/probemodel.go:101,115`). A partial wiring would be the worst outcome: the session works but the footer shows a heartbeat 401 forever (or vice versa).
5. **The value is never printed; its PRESENCE is.** The provider client already redacts the key from server-echoed error text (`sanitize`, `client.go:312-322`), and the discovery error paths echo only HTTP status codes (`discovery.go:72-93`) — no new leak path. The `apogee probe` host report gains one line in its upstream block stating whether a key is configured (`configured` / `none`), because "is my key even loaded?" is the first question behind every 401 — the value itself never appears anywhere.
6. **The secret-in-a-file posture is documented, not engineered around.** The config template's `api-key` block says plainly: the file is plain text; prefer `APOGEE_API_KEY` on shared machines, or tighten the file's permissions yourself. No keychain/credential-store integration (out of scope, listed below). This matches the file's existing posture — `mcp-servers:` env entries already carry tokens (`GITHUB_TOKEN=…` in the template).

---

## The ground (verified 2026-07-27 against the working tree)

**The provider layer is finished.** `Client.apiKey` (`internal/provider/client.go:57`), `WithAPIKey` (`:68-69`), `setAuth` on the chat request (`:220`) and on BOTH discovery probes (`discovery.go:74,108`), `sanitize` redaction in `statusError` (`:240-247`). Tested: `TestRespond_SanitizesAPIKey` (`reliability_test.go:161`), bearer-header assertions (`reliability_test.go:193`, `discovery_test.go:238,278`). Nothing outside tests calls `WithAPIKey`.

**Config resolution.** `settings`/`layer`/`fileConfig` in `cmd/apogee/config.go`: the generic precedence loop (`resolveSettings`, `:402-418`) overlays `[]layer{file, env, flag}` for the scalar fields; `envLayer` (`:843-862`) reads `APOGEE_*`; `flagLayer` (`:867-886`) projects only endpoint/model/mode/bypass; `fileConfig.layer()` (`:731-801`) projects non-empty fields. `applyConfig` (`:903-967`) writes resolved settings back into `options` (`root.go:17-111`). `hostAlias` is the precedent for a field the flag layer never sets. The root command's Long text enumerates the APOGEE_* env vars (`root.go:141-143`).

**Construction sites.** Engine: `New`/`Resume` at `internal/agent/agent.go:102-112` build `provider.NewClient(cfg.Endpoint, cfg.Model)` from `domain.Config` (`internal/domain/config.go:16-22`; aliased as `apogee.Config` at `apogee.go:74`); a sub-agent reuses `a.upstream` (`subagent.go:125`). Heartbeat: `NewMonitor(endpoint, modelHint)` at `internal/heartbeat/heartbeat.go:73-75`, constructed at `cmd/apogee/wire.go:303`; rebind reuses the same client via `SetModel` (ADR 0024), endpoint fixed for the session. Host report: `probe.Discover(ctx, endpoint)` at `internal/probe/discovery.go:49-55`, called from `GatherHost` (`host.go:70`) with `Inputs.Endpoint` (`host.go:24`), wired from `cmd/apogee/probe.go:83-100`; the upstream block renders in `upstreamLines()` (`host.go:138-171`). Battery: `cmd/apogee/probemodel.go:101` (label discovery) and `:115` (the battery client); both commands resolve settings through the same `applyConfig` (`probe.go:66`, `probemodel.go:77`), so `opts.apiKey` will be populated there with zero extra work.

**The template.** `cmd/apogee/defaults/config.yaml` opens with the `endpoint:` block; every key documents its flag/env line in the same comment style.

---

## 1. Config — the `api-key` key, `APOGEE_API_KEY`, and precedence

**What.** `cmd/apogee/config.go`: `settings` gains `apiKey string` (doc comment: the upstream bearer token; env > file, NO flag — a secret must not land in shell history or `ps` output; empty ⇒ no Authorization header). `layer` gains `apiKey *string`. `fileConfig` gains `APIKey string \`yaml:"api-key"\`` with the same doc; `fileConfig.layer()` projects it when non-empty. `envLayer` reads `envAPIKey = "APOGEE_API_KEY"` (constant beside the others, `:830-837`). `flagLayer` is untouched. `resolveSettings`'s generic loop (`:402-418`) gains the `apiKey` case — file then env overlay, flag never set, so env wins over file by the loop's own order. `applyConfig` writes `opts.apiKey = s.apiKey`. `cmd/apogee/root.go`: `options` gains `apiKey string` (documented as resolved-not-flag-bound, like `hostAlias`); the Long text's env enumeration (`:141-143`) gains `APOGEE_API_KEY`.

**Tests** (`cmd/apogee/config_test.go`, existing table style):
- Extend `TestResolveSettingsPrecedence` — file sets the key; env beats file; flag layer cannot set it (no field to set — assert the resolved value with all three layers present).
- `TestApplyConfigAPIKey` — end-to-end through `applyConfig` with an injected file + getenv: file-only, env-only, env-beats-file, absent ⇒ empty.
- `TestApplyConfigDefaults` (`:441`) — extend the zero-config assertion: `apiKey` resolves empty.

**Acceptance.** Green gate; `grep -n "APOGEE_API_KEY" cmd/apogee/config.go cmd/apogee/root.go` hits both; `opts.apiKey` exists but nothing consumes it yet (the wiring lands in items 2–3).

**commit.** `feat(config): the api-key key — upstream bearer token, env > file, deliberately no flag`

---

## 2. Engine — `Config.APIKey`, passed to the session's provider client

**What.** `internal/domain/config.go`: `Config` gains `APIKey string` directly under `Endpoint`/`Model` (`:16-22`), doc comment: *the bearer token sent as `Authorization` on every upstream request; empty — the local-server default — sends no auth header. The provider client redacts it from server-echoed errors; it must never be logged, persisted, or shown by any consumer.* The `apogee.Config` alias (`apogee.go:74`) publishes it for free. `internal/agent/agent.go`: `New` (`:103`) and `Resume` (`:111`) become `provider.NewClient(cfg.Endpoint, cfg.Model, provider.WithAPIKey(cfg.APIKey))` — unconditional (decision 3). Sub-agents inherit via `a.upstream` (`subagent.go:125`), untouched. `cmd/apogee/wire.go`: the `apogee.Config` literal (`:147-184`) gains `APIKey: opts.apiKey` beside `Endpoint`/`Model`.

**Tests** (`internal/agent`, new `apikey_test.go`, httptest-server style):
- `TestNewWiresAPIKeyToUpstream` — `New` against an `httptest.Server` that records the `Authorization` header and answers one minimal chat completion; `cfg.APIKey = "tok"`; Submit + Step; recorded header is `Bearer tok`.
- `TestNewWithoutAPIKeySendsNoAuthHeader` — same harness, empty key ⇒ the request carries NO `Authorization` header (pins decision 3 at the engine seam, not just in `internal/provider`).
- `TestResumeWiresAPIKeyToUpstream` — same assertion through `Resume` with a minimal snapshot.

**Acceptance.** Green gate; a live session against a keyed server now authenticates (manually verified in the whole-plan verification below).

**commit.** `feat(agent): Config.APIKey — the upstream bearer token, wired into the session client`

---

## 3. Heartbeat and probes — the remaining three wires, and the report's presence line

**What.** Every non-engine client (decision 4). `internal/heartbeat/heartbeat.go`: `NewMonitor(endpoint, modelHint string)` becomes `NewMonitor(endpoint, modelHint, apiKey string)` building `provider.NewClient(endpoint, modelHint, provider.WithAPIKey(apiKey))`; doc comment notes the key rides every beat so a keyed server's heartbeat does not 401 while the session works. `internal/probe/discovery.go`: `Discover(ctx, endpoint)` becomes `Discover(ctx, endpoint, apiKey string)`, passing `provider.WithAPIKey(apiKey)`. `internal/probe/host.go`: `Inputs` gains `APIKey string`; `GatherHost` passes it to `Discover` (`:70`) and sets a new `Host.APIKeyConfigured bool` (`in.APIKey != ""` — the report never holds the value); `upstreamLines()` (`:138-171`) gains one field line in the upstream block, both in the reached and unreached shapes: `api key` → `configured (sent as a bearer token)` / `none`. Callers: `cmd/apogee/wire.go:303` → `heartbeat.NewMonitor(opts.endpoint, opts.model, opts.apiKey)`; `cmd/apogee/probe.go:83-100` → `Inputs{… APIKey: opts.apiKey}`; `cmd/apogee/probemodel.go:101,115` → both `NewClient` calls gain `provider.WithAPIKey(opts.apiKey)`. Both probe commands already resolve `opts.apiKey` through `applyConfig` (probe.go:66, probemodel.go:77) — no flag is added there either (decision 1; the commands' flag sets stay "only what CHANGES the report", and the key changes reachability, which env/file already deliver).

**Tests:**
- `internal/heartbeat/heartbeat_test.go`: `TestMonitorSendsAPIKey` — httptest `/v1/models` recording the header; `NewMonitor(url, "", "tok").Beat(ctx)` ⇒ `Bearer tok` recorded, beat Reachable; existing tests updated with the third argument `""` mechanically.
- `internal/probe/discovery_test.go` (or sibling): `TestDiscoverSendsAPIKey` — same header assertion through `probe.Discover`.
- `internal/probe/host_test.go` style: `TestHostReportNamesAPIKeyPresence` — `Inputs.APIKey` set ⇒ report contains the `configured` line and NOT the key value (assert the literal token is absent from the whole report); empty ⇒ `none`.
- `cmd/apogee/probemodel_test.go`: extend the existing harness so the recorded battery/discovery requests carry the header when the config layer supplies a key (follow whatever fake-server pattern the file already uses; if it stubs above the HTTP layer, pin instead that both `NewClient` sites receive the option — a compile-level read suffices, note it in the test comment).

**Acceptance.** Green gate; `grep -rn "WithAPIKey" --include='*.go' internal cmd | grep -v _test` hits exactly: `provider/client.go` (definition), `agent/agent.go` (×2), `heartbeat/heartbeat.go`, `probe/discovery.go`, `probemodel.go` (×2) — no client site left keyless.

**commit.** `feat(heartbeat,probe): the api key rides every upstream wire; the host report states its presence`

---

## 4. Template, docs, and release bookkeeping

**What.** `cmd/apogee/defaults/config.yaml`: a commented `api-key:` block directly under the `endpoint:` block, in the file's house comment style: what it is (bearer token for a keyed server — llama.cpp `--api-key`, LM Studio, remote vLLM/proxies), `env: APOGEE_API_KEY` and DELIBERATELY no flag (one line saying why: shell history / process lists), empty ⇒ no auth header (the local default), and the plain-text caveat: *this file is plain text — on a shared machine prefer the environment variable, or restrict this file's permissions*. `README.md`: the config-key/env documentation gains `api-key` / `APOGEE_API_KEY`; soften the line 14 claim ("no API key is required for local models") to note keyed servers are supported via `api-key:`. `docs/design/technical-design.md`: if it enumerates config keys or the provider client's construction inputs, amend in place (grep for `endpoint`/`APOGEE_`); otherwise untouched. `CHANGELOG.md` `[Unreleased]` Added: one block — the `api-key` config key + `APOGEE_API_KEY`, wired to the session, heartbeat, and both probe halves; presence (never value) shown in `apogee probe`; no flag, and why. No ADR: this exercises existing decisions (ADR 0024's client topology) rather than making a new one — the rationale lives in the template comment and this plan.

**Tests.** `cmd/apogee/defaults_test.go`: extend the template assertions — the embedded template mentions `api-key` and `APOGEE_API_KEY` (keeps the seeded file honest against the code, matching whatever key-presence assertions the file already makes).

**Acceptance.** `grep -n "api-key" cmd/apogee/defaults/config.yaml README.md CHANGELOG.md` all hit; green gate.

**commit.** `docs(config): document api-key — template block, README, changelog`

---

## Explicitly NOT in this plan

- **An `--api-key` flag** — deliberately absent (decision 1): secrets on the command line leak via shell history and process lists; env and file cover every invocation shape.
- **Per-model or per-endpoint key maps** — one session, one upstream, one key (decision 2); a different server is a different invocation.
- **A custom auth header name** (`x-api-key`-style servers) — the OpenAI-compatible contract is `Authorization: Bearer`; a nonconforming proxy is future, additive (`WithAuthHeader` would slot into the same option seam).
- **Keychain / OS credential-store integration** — the env var is the secure-enough channel today; documented posture instead (decision 6).
- **MCP server auth** — already covered per-server by `mcp-servers:` env entries; unrelated wire.
- **`web-search-endpoint` keys** — a different feature with its own redaction (the network tools' scrubbing); untouched.
- **Redaction work in the provider client** — already built and tested (`sanitize`, `client.go:312-322`); this plan only wires the key in.

## Critical files

- `cmd/apogee/config.go`, `cmd/apogee/root.go` — the key's resolution (settings/layer/fileConfig/envLayer/applyConfig; options field; Long text).
- `internal/domain/config.go`, `apogee.go` (alias, no edit) — the public `APIKey` field.
- `internal/agent/agent.go` — the session client (sub-agents ride `subagent.go:125`, no edit).
- `internal/heartbeat/heartbeat.go` — the Monitor's client.
- `internal/probe/discovery.go`, `internal/probe/host.go` — the report probe + the presence line.
- `cmd/apogee/wire.go`, `cmd/apogee/probe.go`, `cmd/apogee/probemodel.go` — the four wire-ups.
- `cmd/apogee/defaults/config.yaml`, `README.md`, `CHANGELOG.md` — the documentation surface.

## Verification (whole plan)

Manual live run against the llama-launcher host (`http://192.168.64.1:1111`; server control via the llama-launcher MCP), with the server restarted keyed (llama.cpp `--api-key <secret>`):

1. No key configured → the TUI paints, the footer shows the heartbeat's unreachable/failure state naming HTTP 401 — and the secret appears nowhere in the transcript.
2. `APOGEE_API_KEY=<secret> apogee` → the first beat binds the model; a full exchange with tool calls runs; `tail_log` on the server shows authenticated requests.
3. Key in `config.yaml` alone → same; then a WRONG key in the file with the good key in the env → still works (env wins).
4. `apogee probe` with the key set → the upstream block reaches the server and reads `api key  configured (sent as a bearer token)`; with no key → `none` and the 401 failure line.
5. `apogee probe model --no-save` with the key set → the battery runs authenticated end-to-end.
6. Restart the server unkeyed, unset everything → behaviour byte-identical to today (empty key sends no header).

Automated: the per-item green gate after every item; the header-recording httptest suites (items 2–3) are the CI-proof that every wire carries the key.
