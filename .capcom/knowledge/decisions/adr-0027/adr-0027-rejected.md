---
title: ADR 0027 considered options
---

- **Strip the skill token out of the text at submit** (the apogee-code oracle's posture, where the
  webview posts `{text, skillIds}` with the token removed). Rejected by the owner: the token is the
  invocation, and a message whose visible text no longer says which skill it invoked cannot be
  accented, cannot be re-read honestly in the scrollback, and makes the box and the model disagree.
- **Keep the chips and add direct `/id` invocation beside them.** Rejected: two ways to attach the
  same thing, each with its own lifecycle (pop, drop, carry), and the parse layer would have to
  reconcile them at submit. The chips were the *only* place skill state lived outside the text, and
  deleting them deletes five special cases with it.
- **A separate skill sigil (`!id`, `#id`, `:id`).** Rejected: the issue asks for `/` specifically,
  it is what every comparable tool uses, and the collision it creates with command verbs is
  resolvable menu-side in one rule (decision 3) rather than by teaching users a second sigil.
- **Resolve command/skill collisions in the parser** (e.g. by prefix or by asking). Rejected: it
  would force the pure, Model-free parse layer to know the catalog exists for something other than
  a membership test, and there is already a route to a shadowed skill — the picker.
- **Let the parser recognise `/skill`.** Rejected, as before: keeping it `menuOnly` is exactly what
  keeps an unknown `/skill foo` line an ordinary message rather than a swallowed one.
- **Execute the command on Enter only, never on dropdown accept.** Rejected: it leaves the
  "I already typed something" symptom unfixed for every verb, since the whole-input rule cannot see
  a draft.
- **Submit the message when Enter is pressed on a fully-typed command mid-draft.** Rejected: the
  same keystroke would send a message in one context and run a command in another, decided by
  whether the token happened to be alone. Enter accepts what the menu is showing; the whole-input
  form is the one documented exception, and it exists because arguments need it.
- **Keep the trailing-token completion rule** (`TODO.md`'s deferral). Rejected — see decision 5.
  The "robustness" it bought was the defect the issue reported.
- **Accent every `/word` and `@path`, resolved or not.** Rejected: the accent's whole value is that
  it is a *verdict*. A uniformly lit typo teaches nothing.
- **Walk the workspace from the render path so an `@path` always resolves.** Rejected outright — a
  filesystem walk per frame, per keystroke. The accent answers from the listing the cache already
  has; a miss renders plain and the next walk heals it.
- **Send an unknown sole `/word` to the model as prose** (today's behaviour). Rejected: it is the
  exact confusion that spawned the issue. Refusing costs the human one keystroke; sending costs a
  wasted turn and a bewildering answer.
- **Refuse any unknown `/word` anywhere in a message.** Rejected: paths, dates and regexes contain
  slashes. The guard is deliberately scoped to the sole-token case, where the human can only have
  meant an invocation.
- **A second dropdown for skills beside the command menu.** Rejected: two overlays competing for
  the same slot above the box, and the human would have to know which one their token belongs to
  before typing it.
