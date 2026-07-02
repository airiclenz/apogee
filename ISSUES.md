
- [ ] the /clear comand does not update the used context gauge display
- [ ] When writing in the prompt window and extending the text beyond one line, the box increases its size correctly but the scroll position is wong. The cursor stays in the top line and an empty line is displayed below. The cursor should be in the last line of the edit box without the empty line displayed.
- [ ] I cannot select text in the apogee session chat (transcript) — prompt selection now works (see below); transcript drag-select is the remaining scope (wrapped + ANSI-styled lines)
- [ ] Auto sizing prompt box is not working
- [ ] Functionality that exists in apogee-code has not been fully ported to apogee (`@file`, `/clear`, `/continue`, `/skill` now done; `/server`, session-management UI, inspector still pending). Verified + list collected → see **TODO.md → "apogee-code feature parity — user-facing affordances not yet ported"**. Porting still in progress.
- [x] Mouse clicks in the prompt: a click positions the cursor at the clicked position, and click-drag selects text and copies it to the clipboard (OSC52). Scope is the prompt box; transcript drag-select is still pending (tracked above).
