---
title: "Presentation ladder: the host-side ladder of mechanisms the Presenter walks per call"
---

**Presentation ladder**:
The **host-side mechanism ladder** the Presenter walks per call; the highest applicable rung
runs *in addition to* rung 0, never instead of it. **Rung 0 (baseline, always)** — a prominent
transcript entry carrying the workspace-relative path as **plain text on its own line**
(cmd+clickable in Zed/VS Code/iTerm2/WezTerm/kitty, copyable everywhere else); it is the rung
that is never wrong. **Rung 1 (local desktop)** — the OS opener auto-opens the file when the
session is local (no `SSH_CONNECTION`/`SSH_TTY`/`SSH_CLIENT`), a desktop is detected, *and* the
extension is one an OS handler **renders rather than runs** (documents, images, text — its own
allow-list, which **crosses** rung 2's rather than containing it, since `.html`/`.htm`/`.svg` are
rung 2's alone and `.xhtml` is on neither: a browser runs a page as much as it shows one, and only
a served response can carry a policy that bounds it; a `report.bat` degrades to rung 0 instead of
executing). **Rung 2 (remote + browser-renderable)** — the
doc server serves the file and the URL joins the entry, also as
plain text; every served document is answered under a restrictive `Content-Security-Policy`
(`default-src 'none'`, bare `sandbox`) plus `nosniff`, which is what makes rung 2 the rung that
may still show active content. **Rung 3** — the
`present.command` config template replaces rung 1's opener, and is **execution-capable** by
construction ({path} is a path the *model* chose, so `sh {path}` runs the document rather than
showing it): on a document the model named it runs only when the file-only
`present.command-on-model-documents` key is set, and with the key off (the default) the ladder
**skips** rung 3 for rung 0 and the tool result names the key. Its argv[0] is resolved absolutely
and refused inside the workspace exactly as rung 1's is. It **fails visible**: any rung above
0 that fails degrades to rung 0 and the entry says what happened.
_Avoid_: "fallback chain" (rung 0 is not a fallback — it always runs), "auto-open" for the whole
thing (that is rung 1 only).
