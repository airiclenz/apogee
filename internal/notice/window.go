package notice

// WindowUnknown is the honesty line for a binding whose context window nobody could name: the
// Budget binds against the window, so with none known it is inactive, and the engine's structural
// bounds — the predictive fold, boundary compaction and the tool-result clamp — fall back to ADR
// 0018's conservative ceiling (a 3072-token transcript budget that fits the smallest window a local
// server realistically runs), so a server with a larger window is managed as if it were small. It
// is the ONE spelling of that sentence — a session says it at its rebind seam, and
// a headless or daemon run says it as it composes when neither a `context-window:` pin nor its
// beat named a window, because with no window bound the oversize warning that would otherwise
// catch the same trouble can never fire there (internal/domain/contextfile.go gates it on an
// advisory ceiling that only a bound window gives).
//
// Only the wording lives here. Each caller keeps its own guard and its own delivery — the TUI
// rides it on a rebind rather than the start-up sequence, because the window is known, or not,
// only once a beat has landed; headless prints it on stderr once per run; the daemon logs it at
// most once per process, or a nightly schedule repeats it in the supervisor's journal forever.
const WindowUnknown = "context window unknown — apogee bounds each request by a conservative assumption " +
	"and the Budget is inactive; set context-window: in config.yaml"
