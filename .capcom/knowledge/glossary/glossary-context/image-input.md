---
title: Image input (`vision:`)
---

**Image input** (`vision:`):
An image a user message carries to the model as an image part (`domain.Image` — name, media type,
the bytes as read), not as text. It arrives two ways: a **File reference** whose BYTES are a PNG,
JPEG, GIF or WebP image (judged by content, never by name), or a TUI attachment — `⌃v` with an image
on the clipboard, or a paste that is exactly an image file's path — which waits as a **pending
image** on the `attached: <name> (<size>)` line above the prompt and goes out with the next idle
send. Only a server whose `servers:` entry sets **`vision: true`** is ever sent one: the key is a
per-server opt-in, never detected (a server's `/props` does not say whether its model reads
images). On a server without it a new image is refused before any request goes out
(`server "<name>" does not accept images: set vision: true on its servers: entry`; an
`--endpoint` run has no entry to set the key on and no flag for it, so there the refusal reads
`server "<name>" does not accept images: an --endpoint server cannot turn vision on — add a servers: entry for it with vision: true and start with --server <name>`),
and an image
already in the history — a resumed session, a switch to another server — goes out as an
`[image omitted: <name>]` line instead while the history keeps it. One image, and a message's
images together, are capped at 5 MiB (`MaxImageBytes`, `MaxMessageImageBytes`). The session
snapshot keeps the bytes on the message; the sent user block shows an `attached:` row, which the
transcript record keeps as names and sizes only.
_Avoid_: "upload" (nothing is uploaded anywhere but in the request), "multimodal" (only images are
input), "vision model" for the key (it states what the server accepts, not what the model is).
