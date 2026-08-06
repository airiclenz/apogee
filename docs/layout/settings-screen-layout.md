- Items marked with **XXX** are just printed bold.
- When a row is in edit mode (after pressing enter), it should be printed in a highlighted color
- Section headers like UPSTREAM or AUTONOMY should be printed in white
- before a section header needs to be a free spacer line
- all multi select fields like `server` or `mode` should get question popo up to select one of the available options (serve does current just enter write mode)
- settings must take effect immediatelly when leaving the settings screen. I do not want "→ new value (next launch)". Also the new value should be displayed in place marked with a ` *` e.g. `false *`
- text edit mode must allow using the cursors to move in the text (same functionality like in the prompt edit window incl. usage of mouse)

╭─────────────────────────────────────── **SETTINGS** ────────────────────────────────────────╮
│                                                                                             │
│ **Description:** The description of the currently selected settings lands here. Longer      │
│ are continued in the 2nd line. There are max two lines for the description.                 │
│                                                                                             │
│ UPSTREAM                                                                                    │
│ · servers                2 servers            edit in config.yaml                           │
│ · server                 macStudio                                                          │
│ · llama-launcher                                                                            │
│                                                                                             │
│ AUTONOMY                                                                                    │
│ · mode                   ask-before                                                         │
│                                                                                             │
│ SYSTEM PROMPT                                                                               │
│ · system-prompt-text     7 lines              edit in config.yaml                           │
│ · system-prompt-file     none                 edit in config.yaml                           │
│ · system-prompt-models   none                 edit in config.yaml                           │
│ · context-files-enable   true                                                               │
│ · context-files-names    [AGENTS.md]          edit in config.yaml                           │
│                                                                                             │
│ CONFINEMENT                                                                                 │
│ · confine-to-workspace   true                 use /confine                                  │
│ · unconfined-hosts       none                 use /confine                                  │
│                                                                                             │
                                           ...
│                                                                                             │
│ MODEL-PROFILE                                                                               │
│ · model-profile           native              edit in config.yaml                           │
│                                                                                             │
│ ↑/↓ select · ⏎ edit · esc close                                                             │
╰─────────────────────────────────────────────────────────────────────────────────────────────╯