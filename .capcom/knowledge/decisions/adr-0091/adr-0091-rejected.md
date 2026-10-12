---
title: ADR 0091 considered options
---

- **Build releases with cgo.** The system resolver would then answer `.local` as it does for every
  other program. It ends the single static cross-built binary that ADR 0042 rests on.
- **Shell out to `avahi-resolve` / `dns-sd`.** It reuses the system's responder, but it makes a
  working endpoint depend on a program being installed, which ADR 0042 rules out.
- **Only improve the message.** The user could then fix it with an IP or an `/etc/hosts` line, but a
  name that the rest of the machine resolves would still fail in apogee alone.
