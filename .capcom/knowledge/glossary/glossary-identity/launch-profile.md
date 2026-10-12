---
title: "Launch profile: llama-launcher's recipe for getting a model running"
---

**Launch profile**:
The recipe for *getting a model running* — which model file, which Upstream
hosts it (llama.cpp, Ollama, LM Studio) and under what launch flags — defined and stored by
**llama-launcher**, the owner's separate server-lifecycle tool, never by apogee. Apogee imports the
launcher as a library, and activating a Launch profile is what "switch model" means on a host that
has one: the launcher **actuates** (starts and
stops servers, loads and unloads models), the Heartbeat **observes**,
and every actuation is completed by the next Beat binding what it finds — the session follows the
loaded profile to its server. Which sessions get that answer is a property of the **server entry**:
a `servers:` entry may carry `llama-launcher: auto` (the launcher's own default config) or a path to
one, and absent — the default, and what every remote entry wants — means the launcher is off for
that server. The integration therefore **follows the session**: `/model` offers Launch profiles only
while the session is on such an entry, every other server keeps the models it advertises, and a
Launch profile load that moves the session to an endpoint no entry names keeps the launcher it just
used. There is no global launcher key; a `config.yaml` still carrying the retired top-level one is
refused at startup with the per-entry line to paste. The deliberate contrast is the Model profile:
a Launch profile is **launch-side** (how a model comes to exist at an endpoint), a Model profile is
**request-side** (how apogee speaks to whatever exists). The two never touch — loading a Launch
profile changes what runs; the Model profile only says how apogee speaks to whatever now runs, and
is re-resolved for that model when the load lands. See
ADR 0029.
**remember-model** is what makes such a choice outlive the session. With that top-level toggle on
(on by default), an *explicit* choice — a `/model` pick that bound, or a Launch profile load that
**committed** — is recorded into apogee's own `servers:` entry: a wire model id into a plain server's
existing `model:` key, a profile name into a launcher-fronted entry's `launch-profile:` pointer. The
pointer's home is the **actuating entry**, the one whose `llama-launcher:` key the session's launcher
path follows, even when the load moved the session to an endpoint no entry names; the launcher's own
config is never written. A heartbeat-observed rebind, a `--model` override, an unload and a stop all
record nothing. At the next *interactive* start-up the recorded profile is loaded back through the
same actuation latch a pick takes — but only while nothing is already running under that launcher
(any instance, any profile, any port, yields with a note). See
ADR 0048.
_Avoid_: "profile" unqualified in docs (two profile namespaces exist; inside the launcher's own
picker the short word is fine — context disambiguates), "launcher profile" (owner-named where the pair is
axis-named), "server profile" (collides with the launcher's own `servers:` config notion).
