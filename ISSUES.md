A: Activated / Active
P: Planned
X: Executed


- [A] I cannot start writing the next prompt when the model is working. I'd like to be able to type the next prompt already. I am also wondering if there is a way to send off messaged to the model while it is working: scheduled messages - this would be usefull to steer the model and add additional info / remarks. The scheduled messaged would need to be sent when possible even when the model is still working. 

- [A] The cursor in the prompt box is blinking. I don't really want anything blinking. I just want a full static symbol to show where the cursor is. Preferrably we use the terminals defined cursor symbol (line vs block)

- [A] I cannot select text in apogee when the model is working. I'd like to be able to select text at any point in time.

- [ ] Functionality that exists in apogee-code has not been fully ported to apogee (`@file`, `/clear`, `/continue`, `/skill`, session-management UI now done; `/server`, inspector still pending). Verified + list collected → see **TODO.md → "apogee-code feature parity — user-facing affordances not yet ported"**. Porting still in progress.

- [ ] File references with filenames including spaces are not working properly: @"docs/plans/2026-07-23 - 04 - version-build-number-plan.md" returns this error: loop: @"docs/plans/2026-07-23 could not be resolved and was ignored: statat "docs/plans/2026-07-23: no such file or directory
