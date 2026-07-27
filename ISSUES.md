A: Activated / Active
P: Planned
X: Executed


- [A] I cannot start writing the next prompt when the model is working. I'd like to be able to type the next prompt already. I am also wondering if there is a way to send off messaged to the model while it is working: scheduled messages - this would be usefull to steer the model and add additional info / remarks. The scheduled messaged would need to be sent when possible even when the model is still working. 

- [A] The cursor in the prompt box is blinking. I don't really want anything blinking. I just want a full static symbol to show where the cursor is. Preferrably we use the terminals defined cursor symbol (line vs block)

- [ ] I cannot select text in apogee when the model is working. I'd like to be able to select text at any point in time.

- [ ] Functionality that exists in apogee-code has not been fully ported to apogee (`@file`, `/clear`, `/continue`, `/skill`, session-management UI now done; `/server`, inspector still pending). Verified + list collected → see **TODO.md → "apogee-code feature parity — user-facing affordances not yet ported"**. Porting still in progress.

- [X] model-, context and server information need to be updated on a timer of 10 sec. Currently there are situations where the model was changed in the background and apogee does not pick up the new model and context size. There is also a bug where the context size is not displayed correclty - this also affects the context usage gauge - these might be related. I also want to be able to start apogee without a running server - through the automatic refresh is shouls pick up when a server comes online. I want to implement this so that possible features that load/select available models on the connected server are still possible end easy to integrato - server switches included (this is a future feature though).
  → **Shipped 2026-07-27** as the upstream **Heartbeat**
  ([ADR 0024](docs/adr/0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md)):
  a ten-second monitor refreshes model / context window / reachability and **rebinds** the engine
  on an observed change (footer, gauge denominator, system prompt, validated set and Budget all
  follow); apogee now **starts without a server**, paints instantly, says so in the footer, blocks
  a send while offline with the typed message preserved, and binds late the moment a server comes
  online. The wrong-context-size half was two bugs, both fixed: a pinned model was probed with an
  empty hint (so a multi-model server's `models[0]` window was adopted) and the gauge clamped its
  bar but not its percentage text (`41k 137%`). Still open **by design**: the gauge counts the
  server's reported total tokens while the engine's Budget counts a prompt-side estimate, so the
  two readings differ — documented in ADR 0024, not a staleness bug. Model/server **selection UI**
  stays future work on the seams this shipped (`TODO.md → [P1] Server / model switching`).

- [ ] File references with filenames including spaces are not working properly: @"docs/plans/2026-07-23 - 04 - version-build-number-plan.md" returns this error: loop: @"docs/plans/2026-07-23 could not be resolved and was ignored: statat "docs/plans/2026-07-23: no such file or directory
