---
title: ADR 0101 decision
---

1. **What counts as a regression:**
   - behaviour that worked at the previous commit or release and does not now; or
   - a path, name or value apogee itself announces to the user or the model (an orientation
     line, a skill header, a `{{…}}` expansion) that one of apogee's tools, modes or guards
     refuses or prompts on.
2. **It is never deferred.** It is fixed in the run that finds it, or it blocks that run's
   closeout. It is never filed as a deferred issue, and it never ships.
