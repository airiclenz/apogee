# The naming calls' prompt assets — editing one re-words every session or delegation name

Every `.txt` file in this directory is prompt text a naming call sends to a live model, and the
wording is what the reply looks like: `system-instruction.txt` asks for the dominant thread of the
work in 3 to 8 words and nothing else, `user-instruction.txt` repeats that constraint next to the
material the model is naming, and `window-header.txt` is what tells the model the numbered requests
run oldest first. Those three serve the SESSION title. `delegation-instruction.txt` serves the
other naming call — the short name an unnamed delegation is given out of band (ADR 0068) — and asks
for 2 to 4 lowercase words naming the job, because that name shares a status line rather than
owning a session-browser row. Re-word one and every session named from then on is named by a different
instruction — and small models, the ones this agent exists to run, answer the last thing they read,
so a softened line does not degrade gracefully: the reply comes back wrapped in a label or naming
the project, the folder or the date, and only the wrapping half of that is something `Sanitize` can
strip.

There is no version constant to bump here — unlike the probe battery, a title is a maintenance
nicety rather than a comparable record — so the pin tests ARE the gate. Editing a file in this
directory obliges you to update the pin that guards it in the same commit:
`TestSystemInstructionAsksForTheDominantThreadBiasedRecent` holds the phrases the system prompt must
keep saying, `TestUserInstructionAndWindowHeaderPinTheirExactWording` holds the two one-line
assets byte-for-byte, and `TestDelegationPrompt_CarriesTheDelegationInstruction` holds the phrases
`delegation-instruction.txt` must keep saying — all in `internal/title/title_test.go`. Their
literals are deliberately duplicated rather than derived from the assets: a pin that fails is the
question "did you mean to re-word this?", not a bug.

The files carry no comments and no header of their own — this README is not embedded, while a
comment line inside a `.txt` would be sent to the model as part of the prompt. Each asset ends with
exactly one trailing newline, which `mustPrompt` (`internal/title/title.go`) strips, so the string
in memory is the file minus that one byte.
