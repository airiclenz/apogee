---
title: "Doc server: the lazily started HTTP server that serves presented files to a remote user"
---

**Doc server**:
The embedded, **lazily started** HTTP server that makes a presented file reachable from the
user's machine when Apogee runs remotely (rung 2). A **capability-token allowlist, not a file
server**: only explicitly presented files, each under a random token at `/d/<32-hex>/<basename>`,
no directory listing, 404 for everything else (including prefix walks and `..`), the file
**re-read from disk per GET**, and closed on app shutdown. Its advertised address is the server
IP from `$SSH_CONNECTION` → the `present.host` override → an outbound-dial probe → `127.0.0.1`;
its port is `present.port`, default **0** (ephemeral), because the URL is printed fresh per
presentation. There is deliberately **no host back-channel** anywhere in this path (ADR 0019).
_Avoid_: "web server" / "file server" (both suggest a served tree; this serves an allowlist of
individually granted files), "preview server".
