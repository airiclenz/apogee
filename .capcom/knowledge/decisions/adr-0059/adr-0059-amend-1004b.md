---
title: "ADR 0059 amendment of 2026-10-04: a Windows Console runs confined"
---

The Bounds bullet's premise — ConPTY creates the process itself and cannot take the restricted
token — is superseded, and with it the 2026-10-03 fail-closed Windows Console. The pseudoconsole
launcher now starts its child through `CreateProcessAsUser` under the token `Prepare` set
(`platform.PseudoConsoleSpec.Token`), so a Windows Console opened under a confinement box runs
under the same restricted, low-integrity token as a confined `terminal` call, inside the same
kill-on-close Job Object. The contract's "confine if you can" row now applies there as it does on
POSIX: in Auto `console_open` confines instead of being demoted to Approval, and a send to a
Console opened confined runs inside that fence. A confined spec whose `Prepare` set no token still
fails closed with `ErrConfinementUnavailable`, and a host whose Confiner cannot fence writes still
gates the open ("gate if you can't").

`Terminal.ConsoleConfines` is now true on Windows, so the 2026-10-04 Amendment and Note above hold
on every host: a send under a confinement box to a Console opened unconfined gets the
`domain.ConfineDemoteError` — the reopen-fenced advice to the model, and the Console-naming Reason
and Fix on the Approval prompt — rather than the generic host-incapacity wording.

One bound stays POSIX-only: kill-on-denial. The denial watch that stops a confined command at its
first denied operation and tells the model `[blocked by workspace confinement: …]` matches POSIX
denial spellings only (`internal/platform/denialkill.go`); the Windows token backend's
`Access is denied.` is deliberately not matched, as for `terminal` on Windows. A confined Windows
Console's denied write therefore fails inside the program, which keeps running, and the model
reads the program's own error in the next `console_read`.
