---
title: landlock/bwrap net-deny box leaves UDP and pathname-UNIX-socket egress open with no Residual disclosed (audit 2026-09-20 High)
status: done
type: bug
priority: 1
aliases: [apogee-qi3]
created: 2026-09-21T20:21:41Z
closed: 2026-09-23T06:06:57Z
reason: "Plan 2026-09-22 - 00 items 6-9: landlock and bwrap net-deny boxes disclose the egress they cannot fence as Residuals; residual notice words each; confinetest UDP row (positive landlock arm tracked by apogee-ifrv)."
---

Source: docs/reviews/code-audit-2026-09-20.md, High 'a network-deny box does not fence UDP or pathname-UNIX-socket egress, and reports no Residual'.
Where: internal/platform/landlock_linux.go:298-311,199-222; internal/platform/namespace_linux.go:237-238.
What: landlock handles only LANDLOCK_ACCESS_NET_CONNECT_TCP; unhandled rights are unrestricted, so a confined child keeps UDP egress (DNS exfil on 53) and pathname-UNIX-socket egress (LANDLOCK_ACCESS_FS_RESOLVE_UNIX never handled). bwrap --unshare-net cuts UDP and abstract sockets but a pathname UNIX socket is filesystem-scoped: /var/run/docker.sock, /run/user/<uid>/bus stay reachable. Capabilities() reports NetworkEgress: true with empty Residuals — a capability-honesty violation (contract: every gap is disclosed, as the truncate gap is).
Audit fix: handle LANDLOCK_ACCESS_FS_RESOLVE_UNIX at ABI >= 9 and the UDP rights at ABI >= 10 when the box opts into deny; disclose whatever an ABI leaves open in Capabilities().Residuals; for bwrap disclose the residual and refuse the tightening or document it; add a confinetest row driving a UDP send under a net-deny box. The disclosure half is small and could ship first; the landlock rights half is the design.
