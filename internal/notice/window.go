package notice

// WindowUnknown is the honesty line for a binding whose context window nobody could name: the
// Budget and automatic Compaction both bind against the window, so with none known they silently
// do nothing. It is the ONE spelling of that sentence — a session says it at its rebind seam, and
// an unpinned headless or daemon run says it as it composes, because an unattended run derives its
// Budget from configuration alone and the oversize warning that would otherwise catch the same
// trouble can never fire there (internal/domain/contextfile.go gates it on a system share the
// unattended path leaves at zero).
//
// Only the wording lives here. Each caller keeps its own guard and its own delivery — the TUI
// rides it on a rebind rather than the start-up sequence, because the window is known, or not,
// only once a beat has landed; headless prints it on stderr once per run; the daemon logs it at
// most once per process, or a nightly schedule repeats it in the supervisor's journal forever.
const WindowUnknown = "context window unknown — automatic compaction and the Budget are inactive; " +
	"set context-window: in config.yaml"
