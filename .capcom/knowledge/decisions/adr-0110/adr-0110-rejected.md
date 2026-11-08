---
title: ADR 0110 considered options
---

- **A column that hides itself when nothing is live.** Each time it came and went, the whole
  transcript would re-wrap and repaint, a cost a slow box feels and a re-wrap nobody asked for.
- **A left-hand panel.** apogee's drag-select assumes the transcript starts at screen column 0, and
  every mouse hit test would need an offset.
- **Full height, beside the prompt box, status line and footer too.** About 50 width readers would
  each need a "main width", and the prompt box would narrow while you type.
- **Reading TUI-side state where no Event exists** (parsing `task_list`'s text, folding writes in
  the TUI). Quicker to build, but watch mode, resume and other Drivers could not show the panel,
  and the parse stays fragile.
- **Proportional or user-set width.** A width that tracks the terminal re-wraps on every resize step
  inside the gate, and a user-set width complicates the gate arithmetic for no use asked for.
- **Auto sections reordering by importance.** The panel's layout would jump under the reader's eye.
  The list order stays fixed, and only presence is dynamic.
- **Scrolling inside the panel, or dropping the lowest sections.** A scrolled-away section is a
  hidden one. Folding to a summary line keeps every live section visible.
- **A key binding in v1.** It would take a key the ←/→ navigation design may want.
