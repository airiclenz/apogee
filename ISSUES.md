

- [ ] Auto sizing prompt box is not working
- [ ] Functionality that exists in apogee-code has not been fully ported to apogee (`@file`, `/clear`, `/continue`, `/skill` now done; `/server`, session-management UI, inspector still pending). Verified + list collected → see **TODO.md → "apogee-code feature parity — user-facing affordances not yet ported"**. Porting still in progress.
- [ ] Mouse clicks not working yet (to strat with placing the cursor at a certina position in the prompt)



## Resolved

- [X] Context size is not read properly from the server — `provider/discovery.go` now probes llama.cpp `GET /props` (the deferred `llamacpp-props` discovery strategy) and uses `default_generation_settings.n_ctx` (the *runtime* window the server was launched with) as the authoritative context window, overriding the model's *training* window (`context_length`/`meta.n_ctx_train`) from `/v1/models` — which was often far larger than the loaded window, so the live context-fill gauge measured against the wrong denominator. Best-effort: a non-llama.cpp server (no `/props`) keeps the `/v1/models` value.

- [X] the list of used skills is not visible after a prompt has been sent — the sent user block now renders the attached skills as chips (`transcript.entry.skills` → `renderUserChipRow`), so a `/skill` attachment stays visible after send.

- [X] The complete area of the prompt box including the border pluss the info line above needs to have black backgroud
- [X] the last user prompt is not sticking to the top when scrolling
- [X] Loaded model name is dispayed with full path - i jst want to display the model name (even the file ending e.g. `,gguf` should be removed)
- [X] [Esc] must not end the application. [Ctrl]+[C] twice within one second shoukd do that.
- [X] [Shift]+[Tab] must change mode
- [X] bottom information shows full model path and ignored
- [X] formatting the chat text:
  - markdown: replace **bold text** with bold text
  - markdown: ## print all headings as bold text in white
  - markdown: `colorize this with orange`
  - code - can we format code (e.g. when a tool call reads and displays a file)
- [X] scrolling in the session chat does not show a vertical scroll bar
- [X] scrolling in the session chat does only work intermittently
