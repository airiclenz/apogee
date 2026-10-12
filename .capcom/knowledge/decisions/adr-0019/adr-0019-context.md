---
title: ADR 0019 context
---

A Skill that produces a report — an architecture review, a research summary, a migration plan —
ends with a file on disk the user never sees. `write_file` renders as a one-line
`Write File <path> +N bytes` card in the transcript and nothing more, so the deliverable an
Exchange just spent its whole Budget producing is, from the user's seat, invisible. The model's
remaining moves are to paste the document back into chat (which defeats writing it to a file) or
to guess at a shell command that might display it — and platform guessing is exactly what a
~4B–35B model is worst at. Apogee's premise is that small models need single, dumb, explicitly
named affordances; "show this document to the user" is one such affordance and does not exist.

The obvious implementation — shell out to an editor or `xdg-open` — is wrong the moment Apogee is
not running on the user's desktop, which for this project is a *normal* case, not an edge. These
facts were established live in the owner's devbox (2026-07-21) and are the ground this decision
stands on:

- Apogee's primary remote context is a **Zed SSH-remoted terminal** (`TERM_PROGRAM=zed`,
  `SSH_CONNECTION` set). Zed ships **no `zed` CLI on remotes** (zed-industries/zed discussions
  #32214, #33601 — confirmed still true June 2026), so the VS Code-style `code <file>` shell-out
  has no analogue there.
- Zed's terminal makes **file paths in output cmd+clickable** (opening in the editor through the
  remote server) and **URLs cmd+clickable** (opening the *host's* default browser). Both work on
  **plain text** — no OSC 8 needed, and plain text is the more portable form (iTerm2, WezTerm,
  kitty and VS Code's terminal all detect it too).
- An HTTP server bound in the devbox on `0.0.0.0` **is reachable from the host** at the
  server-side IP carried in `$SSH_CONNECTION`, proven with `python3 -m http.server` and a host
  browser.
- **Host-side policy can still block that URL invisibly**: macOS Local Network permission
  (Sequoia+) made Chrome fail with a generic "site can't be reached" while Safari worked on the
  same URL. Every mechanism above the baseline must therefore **fail visible**, and the baseline
  must always be printed.
- The owner **rejected any host back-channel** (reverse SSH, a host-side helper daemon, path
  mapping over a shared mount as a required rung) on security and dependency grounds.
