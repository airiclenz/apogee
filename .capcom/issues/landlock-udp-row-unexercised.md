---
title: "confinetest row #13's landlock UDP arm has never been exercised: it needs a landlock ABI 4+ runner"
status: done
type: task
priority: 3
labels: [owner-run]
aliases: [apogee-ifrv]
created: 2026-09-22T20:56:55Z
closed: 2026-10-03T19:45:08Z
reason: Positive arm passed on a landlock ABI 8 host (kernel 7.0.0-38-generic, aarch64), 2026-10-03; see note.
---

Item 9 of the 2026-09-22 audit-design-findings plan (commit 2c5f7e03) added the UDP-egress row to the confinement escape battery: a confined child sends a real datagram out of a network-deny box and the parent's loopback listener decides the verdict (internal/platform/confinetest/confinetest.go:282-324).

The negative arm (no landlock network support here) runs on this box; the POSITIVE arm — the backend discloses connect(2) UDP as unfenced and the datagram must therefore arrive — is compile-checked only: TestLandlockProbeNetwork (internal/platform/landlock_linux_test.go:69) skips on this machine, so no runner has executed the branch that proves item 6's disclosure matches the kernel's actual behaviour.

Needs one run on a Linux host at landlock ABI >= 4.

## Log

- 2026-09-30 beads: cannot run on the owner's Pi 5 — both installed kernels (6.18.50+rpt stock, 6.18.53-v8-16k-race) have CONFIG_SECURITY_LANDLOCK unset, active LSMs = capability. Run once on any Linux host with landlock ABI >= 4 (kernel >= 6.7 with landlock in the LSM list, e.g. stock Ubuntu 24.04+/Fedora 40+): APOGEE_REQUIRE_LANDLOCK_NET=1 go test -count=1 -run TestLandlockProbeNetwork ./internal/platform/ — the test logs the ABI it met; pass there closes this bead.
- 2026-10-03 beads: run on an aarch64 Linux host, kernel 7.0.0-38-generic, LSMs lockdown,capability,landlock,yama,apparmor,ima,evm — landlock ABI 8. APOGEE_REQUIRE_LANDLOCK_NET=1 go test -count=1 -run TestLandlockProbeNetwork ./internal/platform/ PASS, including udp_egress_under_network_deny (the positive arm). Full ./internal/platform/... (incl. confinetest) also PASS; go1.26.6, no -race (no C compiler on this host).
