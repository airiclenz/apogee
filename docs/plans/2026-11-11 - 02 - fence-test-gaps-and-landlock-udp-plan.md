# Fence test gaps and landlock ABI 10 UDP egress

- **Goal:** Close the three test gaps the fence-escape plan deferred: confinetest's raw probe windows, bwrap's untested container-socket mask, and the RESOLVE_UNIX planner's unmeasured rule count. Then let a landlock ABI ≥ 10 network-deny box close UDP egress, and correct every claim that no kernel can.
- **Date:** 2026-11-11
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **base:** 60694063
- **Sources:** `.capcom/issues/confinetest-unix-wait-raw.md`; `.capcom/issues/bwrap-container-mask-untested.md`; `.capcom/issues/landlock-unix-rule-count.md`; `.capcom/issues/landlock-udp-abi10.md`; ADR 0112 (`capcom know show "ADR 0112" --with-children`); `docs/design/test-drivers.md` §Waiting; `docs/design/confinement-execution-contract.md`; kernel landlock docs (https://docs.kernel.org/userspace-api/landlock.html): UDP net rights are ABI 10; `golang.org/x/sys/unix` (go.mod version) defines `LANDLOCK_ACCESS_NET_BIND_UDP` and `LANDLOCK_ACCESS_NET_CONNECT_SEND_UDP`.
- **Ratified design calls** (owner, 2026-11-11):
  - **Rule count:** prove harmless, with no behaviour change. A pure `fs.FS` test pins the rule count as linear in the chain directories' entries, a pure benchmark measures the planner, and the doc comment states the bound and why it is harmless. There is no cap and no fallback.
  - **UDP at ABI 10:** implement and reword. At ABI ≥ 10 a network-deny box handles the UDP bind and connect/send rights and stops disclosing `connect(2) UDP`. Below ABI 10 it keeps disclosing it, and every claim is reworded to "below ABI 10". Coverage is the injected-ABI unit tests plus the existing live self-checks on any ABI ≥ 10 host. Neither this box (ABI 7) nor CI can run it live, the same as RESOLVE_UNIX.
- **Standing requirements:**
  - skills: coding-standards
  - This box is a Raspberry Pi: tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run '<TestX>' ./<one package>/`, one run at a time, never a package pattern; pass this rule to every sub-agent.
  - Use the `x/sys/unix` constants for landlock rights; never hand-code bit values.
- **Regression check (2026-11-11, 60694063):**
  - 1: guard folded (row #15's own Acceptance run; `cmd.WaitDelay` drain backstops become `testtime.Budget`)
  - 2: guard folded (decision: landlock_linux.go off Files; `TestUnixSocketPlan` named; fake host carries the production candidates)
  - 3: guard folded (rule-count bound and reachability wording corrected)
  - 4: guard folded (decision: `-run` patterns; pure handled-net mask helper; UDP bind refusal named)
  - 5: guard folded (decision: `-run` pattern; TCP-only sites, `namespace_linux.go` and an ADR 0081 amendment added)
- **Out of scope:** extending `TestNoFixedTimingInTests` to kit files or to `SetDeadline` calls; landlock `NET_BIND_TCP` handling (the `Capabilities` comment names it, but the ruleset handles only `CONNECT_TCP`); a rule-count cap; the other ready issues (`served-pdf-csp-sandbox`, `windows-token-spawner-posture`, `context-fill-bench-arm`, `lsp-symbol-navigation`, `serve-http-driver`, `user-view-reaction-cell`).

## 1. confinetest probe windows go through testtime — ✅ DONE (2026-11-11)

**What:**
**Goal:** Every probe window in `internal/platform/confinetest/confinetest.go` is a `testtime` duration. The UDP and unix "nothing arrives within" windows use `testtime.Fixed`, and no raw `time.Duration` literal bounds a deadline in that file.
**Approach (assumed at the header base):** `const unixProbeWait = 2 * time.Second` (row #15 `unix_connect_outside_box`, the `ln.SetDeadline` before `Accept`) and `const udpProbeWait = 2 * time.Second` (row #13's `pc.SetReadDeadline`) become `var … = testtime.Fixed(2 * time.Second)`. This mirrors `unixAcceptWait` in `internal/platform/namespace_linux_test.go`. Keep the comments that say the silent arm always waits the window in full. Rule for the sweep: every duration literal in confinetest.go that bounds a wait is converted (`grep -n 'time\.Second\|time\.Millisecond' internal/platform/confinetest/confinetest.go`). Importing `internal/testtime` from this non-test kit file is allowed; `internal/tuitest` already does it.
**Regression guard.** The sweep's grep also returns `cmd.WaitDelay = time.Second` in `runChainedClobber` and `runMergedClobber`: each is a drain backstop that ends on a condition, so it becomes `testtime.Budget(time.Second)` — never `Fixed`. Row #15 lives in `Probe`, run by `TestNamespaceProbe`, so the item's Acceptance runs that subtest as well as row #13.
**Files:** internal/platform/confinetest/confinetest.go
**Read first:** internal/platform/confinetest/confinetest.go — unixProbeWait, udpProbeWait, Probe (row unix_connect_outside_box), ProbeNetwork, runChainedClobber; internal/platform/namespace_linux_test.go — unixAcceptWait, TestNamespaceProbe, TestNamespaceProbeNetwork; internal/testtime/testtime.go — Fixed, Budget; internal/testtime/guard_test.go — scanDir (kitDir covers only internal/tuitest)
**Closes:** confinetest-unix-wait-raw
**Tests:** No new test. Rows #13 and #15 run through the existing live probes.
**Acceptance:**
- `go build ./internal/platform/... && go vet ./internal/platform/confinetest/`
- `go test -race -count=1 -run 'TestNamespaceProbeNetwork' ./internal/platform/` passes (or skips where bwrap cannot fence)
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestNamespaceProbe$/unix_connect_outside_box' ./internal/platform/` passes (or skips where bwrap cannot fence)
- `grep -n 'Wait = ' internal/platform/confinetest/confinetest.go` shows only `testtime.` initialisers
**Commit:** `test(confinetest): route the unix and UDP probe windows through testtime.Fixed`

## 2. Live bwrap test for the container-socket mask

**What:**
**Goal:** A live test runs a bwrap box whose container-socket candidate is a temp listener and shows that a confined connect to it reaches no listener. A control arm without the mask shows the same probe is accepted. The test skips where bwrap cannot fence. Production masks the same `/run/docker.sock` and `/run/podman/podman.sock` set as before.
**Approach (assumed at the header base):** The candidates move from the package variable `containerSocketCandidates` (`internal/platform/unixsockets_linux.go`) onto `unixSocketHost` as a field. `currentUnixSocketHost()` fills it with the two production paths, and `planUnixSockets` reads it from the host. Every reader of the package variable moves to the host field, including the landlock `unixSocketClosure` path. `namespaceConfiner` gains an unexported host override, and `Confine` uses it when it is set, else `currentUnixSocketHost()`. No new exported API. Mutating the package variable from a test is forbidden, because it races with parallel readers. The new test `TestNamespaceMasksContainerSockets` in `namespace_linux_test.go` builds on `TestNamespaceMasksRuntimeDirSockets`: `fencingNamespaceConfiner`, `listenUnix`, `confinetest.UnixConnectFromBox`, `acceptedWithin`.
**Regression guard.** drop internal/platform/landlock_linux.go from item 2's Files line unless the item's approach genuinely needs an edit there (currentUnixSocketHost() keeps filling the candidates for the landlock path), so items 2 and 3 stay file-disjoint. The production candidate pair lives in one unexported default that both `currentUnixSocketHost()` and `fakeUnixSocketHost` read, so the `TestUnixSocketPlan` container-socket cases and `TestNamespaceArgv`'s `container_sockets_covered…` / `missing_socket_paths_are_skipped` keep their `--ro-bind /dev/null` flags; `noUnixSockets` keeps it too. The plan table is `TestUnixSocketPlan` (no `TestPlanUnixSockets` exists).
**Files:** internal/platform/unixsockets_linux.go, internal/platform/unixsockets_linux_test.go, internal/platform/namespace_linux.go, internal/platform/namespace_linux_test.go
**Read first:** internal/platform/unixsockets_linux.go — containerSocketCandidates, unixSocketHost, currentUnixSocketHost, planUnixSockets; internal/platform/namespace_linux.go — namespaceConfiner, Confine, namespaceArgv; internal/platform/landlock_linux.go — Confine (unixSocketClosure(currentUnixSocketHost(), box)), unixSocketClosure;
internal/platform/unixsockets_linux_test.go — fakeUnixSocketHost, noUnixSockets, TestUnixSocketPlan; internal/platform/namespace_linux_test.go — TestNamespaceArgv, TestNamespaceMasksRuntimeDirSockets, fencingNamespaceConfiner, listenUnix, acceptedWithin; internal/platform/confinetest/confinetest.go — UnixConnectFromBox
**Closes:** bwrap-container-mask-untested
**Tests:** New `TestNamespaceMasksContainerSockets` (masked arm and control arm). The existing `TestNamespaceArgv` and the `unixsockets_linux_test.go` plan table (`TestUnixSocketPlan`) still pass with candidates injected through the host.
**Acceptance:**
- `go build ./... && go vet ./internal/platform/`
- `go test -race -count=1 -run 'TestNamespaceMasksContainerSockets|TestNamespaceArgv|TestNamespaceMasksRuntimeDirSockets' ./internal/platform/` passes, and the new test runs rather than skips on this box (`-v` shows PASS)
- `go test -race -count=1 -run 'TestUnixSocketPlan|TestLandlockResolveUnixPlan|TestLandlockConfineCarriesUnixSocketClosure' ./internal/platform/`
**Commit:** `test(platform): drive bwrap's container-socket mask with a live connect`

## 3. RESOLVE_UNIX planner rule count pinned on a busy /tmp

**What:**
**Goal:** A pure test shows that `planResolveUnixRules` on a busy /tmp (10 000 entries, the closed runtime dir under /tmp) returns exactly one rule per listed entry of each directory on the closed path's chain, plus the reopened paths. It never returns a rule beneath a granted directory. A benchmark measures the planner on the same tree. The function's doc comment states the bound and why it is harmless.
**Approach (assumed at the header base):** In `internal/platform/landlock_linux_test.go`, add a busy-/tmp case next to `TestLandlockResolveUnixPlan` (`fstest.MapFS`, built in a loop and mixing dirs, sockets and regular files, since files and symlinks get no rule). Assert the exact count and that no rule path has a granted dir as its proper ancestor. Add `BenchmarkPlanResolveUnixRules` on the same tree. Extend the doc comment on `planResolveUnixRules` with the bound:
- rules = entries listed along the chain + reopened paths;
- the case is reachable only when `XDG_RUNTIME_DIR` points under /tmp;
- landlock has no practical rule cap, since rules are charged to memcg;
- rules attach to inodes, so later access cost is unchanged;
- the apply-time cost is one `open(O_PATH)` + `landlock_add_rule` per rule.

No production logic changes.
**Regression guard.** `grantSiblings` gives no rule to regular files, symlinks, a closed path or a chain ancestor, so the count the test pins and the doc states is one rule per directory or socket entry listed along the chain, excluding closed paths and their ancestors, plus each existing reopened path; the doc bound reads "≤ entries listed + reopened". `maskableRuntimeDir` refuses only a dir that is or contains a protected path, so the reachability bullet reads "only when `XDG_RUNTIME_DIR` resolves outside /run/user — under /tmp, $HOME or any other directory, whose entries the chain then lists".
**Files:** internal/platform/landlock_linux.go, internal/platform/landlock_linux_test.go
**Read first:** internal/platform/landlock_linux.go — planResolveUnixRules, ancestorOfAny, fsysPath, allowResolveUnix, unixSocketClosure; internal/platform/landlock_linux_test.go — TestLandlockResolveUnixPlan (hostFS, resolve helper, closed-path assertion loop), TestMain; internal/platform/unixsockets_linux.go — maskableRuntimeDir, isBeneath, planUnixSockets
**Closes:** landlock-unix-rule-count
**Tests:** The new busy-/tmp case and `BenchmarkPlanResolveUnixRules`.
**Acceptance:**
- `go test -race -count=1 -run 'TestLandlockResolveUnixPlan' ./internal/platform/`
- `go test -count=1 -run '^$' -bench 'BenchmarkPlanResolveUnixRules' -benchtime 3x ./internal/platform/` reports a result
**Commit:** `test(platform): pin the RESOLVE_UNIX rule count on a busy /tmp`

## 4. landlock ABI ≥ 10 closes UDP egress in a network-deny box

**What:**
**Goal:** At landlock ABI ≥ 10, a box whose network is denied handles `LANDLOCK_ACCESS_NET_BIND_UDP` and `LANDLOCK_ACCESS_NET_CONNECT_SEND_UDP`, and `Capabilities` no longer lists `domain.ResidualUDPEgress`. Below ABI 10 both behave exactly as before. Every test that pins the residual tokens or the capability line covers both sides of ABI 10.
**Approach (assumed at the header base):** In `internal/platform/landlock_linux.go`:
- add `landlockABIUDP = 10` beside `landlockABIResolveUnix`;
- in `applyLandlock`, OR the two UDP rights into `Handled_access_net` when the box denies the network (`networkDenyDecision`) and `abi >= landlockABIUDP`, with no allow rule for them;
- in `landlockConfiner.Capabilities`, emit `ResidualUDPEgress` only when `NetworkEgress` holds and `abi < landlockABIUDP`;
- reword the `Capabilities` comment to say why below ABI 10.

Consumers to keep correct: `internal/probe/confinement.go` (`writeClassResiduals`, `CapabilityLine`) and confinetest row #13, which keys off the token's presence and so self-checks on an ABI-10 host. Add an `abi10_udp` row to `TestLandlockCapabilitiesHonest`. Cover both sides of ABI 10 in `TestNetworkDenyDecision`, or in a new pure test of the handled-net mask. Make `TestLandlockResidualsMatchHostABI` expect the token only below ABI 10. Correct the comments in this item's test files that say every kernel leaves UDP out.
**Regression guard.** every Acceptance and Tests command names a -run pattern (no bare `go test ./internal/probe/` or `./internal/domain/`) per the header's Pi standing requirement — pick the pattern(s) covering the residual-token and capability-line tests the reviewer named. `Handled_access_net` is set inline in `applyLandlock` and `networkDenyDecision` returns only a bool, so extract a pure helper (e.g. `netAccessMaskForABI(box, abi) (uint64, error)` over `networkDenyDecision`) that `applyLandlock` assigns to `attr.Handled_access_net`; its test pins 0 (open, ABI 9/10), `CONNECT_TCP` (deny, ABI 9), `CONNECT_TCP|BIND_UDP|CONNECT_SEND_UDP` (deny, ABI 10), error (deny, ABI 3). Handling `BIND_UDP` with no rule also refuses a confined UDP bind (explicit or autobind) while TCP bind stays unhandled: the `Capabilities` and `applyLandlock` comments (including its "a TCP-connect handler" clause) say a deny box at ABI ≥ 10 also refuses UDP bind, and so do item 5's docs.
**Files:** internal/platform/landlock_linux.go, internal/platform/landlock_linux_test.go, internal/platform/confiner_linux_test.go, internal/probe/confinement_test.go
**Read first:** internal/platform/landlock_linux.go — applyLandlock, networkDenyDecision, landlockConfiner.Capabilities, accessMaskForABI; internal/platform/landlock_linux_test.go — TestLandlockCapabilitiesHonest, TestLandlockResidualsMatchHostABI, TestNetworkDenyDecision; internal/platform/confinetest/confinetest.go — ProbeNetwork (udp_egress_under_network_deny), runDatagramProbe;
internal/platform/confiner_linux_test.go — TestSelectLinuxConfiner; internal/probe/confinement.go — writeClassResiduals, CapabilityLine
**Closes:** landlock-udp-abi10
**Tests:** As above. Pin the handled-net mask at ABI 9 and ABI 10 for both deny and allow boxes through the new pure helper's test (`TestNetAccessMaskForABI`), and ABI 3 deny as an error.
**Acceptance:**
- `go build ./... && go vet ./internal/platform/ ./internal/probe/`
- `go test -race -count=1 -run 'TestLandlock|TestNetworkDenyDecision|TestAccessMaskForABI|TestNetAccessMaskForABI' ./internal/platform/`
- `go test -race -count=1 -run 'TestCapabilityLine|TestResidualNotice' ./internal/probe/` and `go test -race -count=1 -run 'TestNetworkOnlyResidualsLeaveCapsAutoEligible|TestConfinementNetworkResidualTokensAreNamedBySyscall' ./internal/domain/`
**Commit:** `feat(platform): close UDP egress under landlock ABI 10 in a network-deny box`

## 5. Docs and comments stop claiming no kernel closes UDP

Depends on item 4.

**What:**
**Goal:** No doc, comment or Knowledge Note says that no kernel or landlock closes UDP egress, or that landlock has no UDP right. Each such site says that landlock below ABI 10 leaves `connect(2) UDP` open, and that ABI ≥ 10 closes it in a network-deny box.
**Approach (assumed at the header base):** Rule: every site making the claim, found by `grep -rn -i 'no kernel closes\|UDP right\|nothing else\|datagram egress' internal/ docs/ .capcom/knowledge/ README.md SECURITY.md`. Known sites at base:
- `internal/domain/confinement.go` (the `ResidualUDPEgress` comment and the type comment);
- `docs/manual/probe.md`;
- `docs/manual/configuration.md` (the "not waiting on a newer box" paragraph);
- `docs/design/confinement-execution-contract.md` (the network-deny section and the "standing fact" paragraph).

A Knowledge Note that states the claim is corrected by a dated amendment per AGENTS.md, never rewritten. Historical text (CHANGELOG, archived plans, reviews) stays. Sites in item 4's files are item 4's.
**Regression guard.** same -run rule for item 5's `./internal/domain/` command. The rule is "every non-historical line that says landlock's network rights are TCP-only or that UDP egress cannot be closed", found by `grep -rn -i 'udp\|datagram\|tcp.only\|TCP .connect./.bind. alone'` over the same roots; this adds `internal/platform/namespace_linux.go` (package header, "landlock's TCP-only rights"), the ADR 0081 Decision's "its own TCP-only rights" (corrected by a dated amendment `adr-0081-amend-1111b`, named on the record's Status line and reading-order list) and the contract's "network rights cover TCP `connect`/`bind` alone". Per item 4, the reworded sites also say a deny box at ABI ≥ 10 refuses UDP bind.
**Files:** internal/domain/confinement.go, docs/manual/probe.md, docs/manual/configuration.md, docs/design/confinement-execution-contract.md, internal/platform/namespace_linux.go, .capcom/knowledge/decisions/adr-0081.md, .capcom/knowledge/decisions/adr-0081/adr-0081-amend-1111b.md
**Read first:** internal/domain/confinement.go — ResidualUDPEgress, ConfinementCaps; docs/manual/configuration.md — Auto mode's blast radius (network-deny paragraph); docs/manual/probe.md — unfenced field paragraph; docs/design/confinement-execution-contract.md — §5 capability honesty (landlock ABI ladder, residual-class paragraph), §6.2 row #13;
internal/platform/namespace_linux.go — package header comment; .capcom/knowledge/decisions/adr-0081/adr-0081-decision.md — 2026-09-22 amendment
**Tests:** No new test. The drift tests that pin the public counts still pass.
**Acceptance:**
- `grep -rn -i 'no kernel closes' internal/ docs/manual docs/design/*.md .capcom/knowledge/` prints nothing
- `go test -race -count=1 -run 'TestNetworkOnlyResidualsLeaveCapsAutoEligible|TestConfinementNetworkResidualTokensAreNamedBySyscall' ./internal/domain/` and `go test -race -count=1 -run 'Drift|Doc' ./cmd/apogee/`
**Commit:** `docs(confinement): landlock closes UDP egress from ABI 10; reword the no-kernel claims`
