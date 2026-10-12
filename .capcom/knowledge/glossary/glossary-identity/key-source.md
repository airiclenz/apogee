---
title: "Key source: the one place a server entry's API key comes from"
---

**Key source**:
The one place a server entry's API key comes from: a literal `api-key`, a command (`api-key-cmd`)
whose output is the key, or a named environment variable (`api-key-env`). An entry has at most one;
having none is the keyless state. **Key migration** is the startup offer that moves a plaintext key
into the OS secret store and turns the entry into a command source. See
ADR 0047.
_Avoid_: "credential provider", "keychain support" (apogee keeps no API key of its own and links no
keychain library — a source is a line in the entry; the one credential apogee writes itself is an
MCP OAuth token, which a login mints and nothing else could hold — ADR 0095), "key fallback" (an entry names one source, not
a chain).
