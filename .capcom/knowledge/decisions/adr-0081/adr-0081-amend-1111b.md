---
title: "ADR 0081 amendment of 2026-11-11 (b): landlock's network rights are TCP-only only below ABI 10"
---

**Why now.** Plan `2026-11-11 - 02` (items 4 and 5, issue `landlock-udp-abi10`) lets a landlock
network-deny box close UDP egress where the kernel offers landlock ABI 10.

**What changes.** The Decision's 2026-09-22 note calls landlock's network rights "its own TCP-only
rights". That holds only below ABI 10: from ABI 4 to ABI 9 the ruleset can handle TCP alone, so a
landlock deny box lets UDP datagrams out and discloses `connect(2) UDP`. From ABI 10 a deny box also
handles `LANDLOCK_ACCESS_NET_BIND_UDP` and `LANDLOCK_ACCESS_NET_CONNECT_SEND_UDP` with no rule
granting either, so a confined datagram is refused, a UDP bind (explicit, or the autobind a connect
makes) is refused too, and the token is not disclosed. The rest of the note stands: this backend's
`--unshare-net` cuts UDP on every kernel, and abstract AF_UNIX sockets stay outside landlock's
network rights at every ABI.
