---
title: Linux release (CGO-off) builds cannot resolve .local mDNS endpoints; reported as 'server offline'
status: done
type: bug
priority: 1
aliases: [apogee-mdns-local-unresolved]
created: 2026-09-24T19:29:23Z
closed: 2026-09-30T09:34:01Z
reason: "Resolved by plan mdns-local-fallback (archived 2490841b): .local hosts fall back to a one-shot mDNS lookup in CGO-off builds, and an unresolved host name is reported by name instead of a raw lookup error"
---

Released binaries cannot resolve mDNS `.local` hosts, and the failure is reported as "server offline".

## Symptom
With `endpoint: http://Apollo-II.local:1111`, the Homebrew v0.23.0 binary refuses to send:

    cannot send — server offline (http://Apollo-II.local:1111): apogee: model discovery:
    Get "http://Apollo-II.local:1111/v1/models": dial tcp: lookup Apollo-II.local on
    100.100.100.100:53: no such host

The server is up: `getent hosts Apollo-II.local` → `192.168.0.55`.

## Cause
`make dist` builds every release archive with `CGO_ENABLED=0` (Makefile, dist target), so the Homebrew binary uses Go's pure-Go resolver. It cannot load NSS modules, so `hosts: files mdns4_minimal [NOTFOUND=return] dns` never reaches avahi. The query goes to the unicast nameserver in /etc/resolv.conf (Tailscale MagicDNS, 100.100.100.100), which returns NXDOMAIN.
A local `make build` binary (native, CGO_ENABLED=1) uses the cgo resolver and works. That is why it "worked earlier today": `~/.apogee/server-stats.jsonl` shows successful Apollo-II requests at 10:12Z from `./apogee`, and the failure appeared after switching to the brew v0.23.0 binary.

## Regression status
This is not a code regression. The dist target has used CGO_ENABLED=0 since it was added, so brew v0.22.x and earlier would fail the same way on this host. It regressed from the user's point of view: the same config works with a source build and fails with the shipped build.

## Two defects
1. **Resolution:** shipped binaries cannot reach `.local` (mDNS) endpoints on Linux (and likely macOS, depending on the Go version's darwin resolver path), which is a common way to address a LAN LLM box.
2. **Diagnosis:** a DNS lookup failure is presented as "server offline". It should say that the host name did not resolve, and hint at `/etc/hosts` or the IP for `.local` names.

## Workarounds
Use the IP in the endpoint, or add `192.168.0.55 Apollo-II.local` to /etc/hosts (the pure-Go resolver reads it).

## Fix directions (to design)
- Resolve `.local` names in apogee itself via a small mDNS query (multicast 224.0.0.251:5353) when the system lookup fails, so CGO_ENABLED=0 builds keep working.
- Or ship cgo-enabled Linux/macOS builds (hard for cross-compiled `make dist`).
- Either way, classify `*net.DNSError` separately from connection-refused in the offline/probe message.

## Acceptance

A release-built (CGO_ENABLED=0) apogee reaches an endpoint named <host>.local that the OS resolves via mDNS; a DNS lookup failure is reported as an unresolved host name, not 'server offline'; both pinned by tests.

## Log

- 2026-09-24 beads: confirmed Linux-only. The same brew binary works on macOS: since Go 1.20, darwin builds call libSystem's resolver even with CGO_ENABLED=0, and that resolver handles mDNS. On Linux the pure-Go resolver cannot load NSS (mdns4_minimal), so it queries unicast DNS only. It hits any Linux host running a release build whose LLM server is addressed by an mDNS-only .local name. Source builds (make build, CGO on) are unaffected.
