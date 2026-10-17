---
title: "Watched home: another user's apogee home, read only, whose sessions can be watched"
---

**Watched home**:
Another user's `~/.apogee` home, named under the global-only key `sessions.watch-homes` with a
label, whose sessions apogee lists and opens in **Watch mode**. It is a read-only source and never
a second home (ADR 0100): nothing in it is swept, written, held or configured from, and none of
its sessions is resumable. Its rows join the `/sessions` list by time, tagged `· <label>`. Reading
it needs the owner to grant OS-group read access with `sessions.share-group`. See ADR 0103.
_Avoid_: "remote home" (it is on the same machine), "shared home" (nothing in it is shared back),
"second home".
