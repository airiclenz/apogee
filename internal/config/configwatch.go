package config

// The config file watcher: ADR 0041 decision 3.
//
// ADR 0037 made "the child process exited" the end-of-edit signal. A desktop opener — `open`,
// `xdg-open`, `cmd /c start` — returns the moment it has handed the file to the application that
// owns `.yaml`, so that signal fires before the human has typed a character, and the diff behind it
// reads unchanged bytes and concludes the user edited nothing. The signal therefore moved to the
// file itself: apogee polls `config.yaml` for the whole session and reports that it changed,
// whoever changed it — the pane's own editor jump, a GUI editor left open in another window, or a
// `vim ~/.apogee/config.yaml` in a second terminal.
//
// This type is that mechanism and nothing else. It reads no YAML, holds no projection and applies
// nothing: all it knows about the file is two numbers from os.Stat, and all it says is "it
// changed". Parsing, the baseline diff, the last-good rule and the per-key apply stay where they
// already are — ADR 0041's "cmd/apogee grows a watcher, and nothing else grows" — which is also why
// it is one goroutine and no dependency: no fsnotify, no inotify, no daemon, nothing out of process.

import (
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// configWatchInterval is the poll cadence. One os.Stat a second against a file that changes a
// handful of times a session is the entire running cost of the feature, and a faster poll would buy
// latency nobody can perceive on an act that ends with a human saving a document.
const configWatchInterval = time.Second

// configWatchSettle is how long the file must hold still before a change is reported. An editor
// saving is not one write — it writes, truncates, and renames a temp file over the target — and a
// poll that reported every observation would apply a half-written document on the way to applying
// the finished one. The delay costs one tick of latency and collapses the burst into one report.
const configWatchSettle = 250 * time.Millisecond

// configFileState is what a poll can learn about the file cheaply: when it was last written, and how
// long it is. Either witness moving counts as a change, which is ADR 0041's rejection of "mtime
// alone": a one-key edit is exactly the edit that can rewrite the same number of bytes in the same
// second on a coarse-timestamp filesystem, and the size is a second witness that costs nothing.
type configFileState struct {
	modTime time.Time
	size    int64
}

// equal reports whether two samples describe the same file as far as a poll can tell.
func (s configFileState) equal(other configFileState) bool {
	return s.size == other.size && s.modTime.Equal(other.modTime)
}

// Watcher reports that one path changed. Start it once, read Changes for the life of the
// session, Stop it at teardown — Start and Stop belong to the goroutine that owns the watcher (the
// composition root), and Stop does not return until the poll goroutine is gone.
type Watcher struct {
	// path is the single file under watch: config.yaml, and deliberately nothing else. Skills,
	// MCP manifests and context files each have their own reload semantics (ADR 0041 decision 5
	// — one file, one contract).
	path string
	// Interval is the poll cadence and Settle the quiet period a change must survive before it is
	// reported. NewWatcher sets both to the production constants below; they are exported and
	// settable BEFORE Start so a test — in this package or in a Driver's — can drive the watcher in
	// milliseconds instead of waiting out seconds of real time. Changing either after Start has no
	// effect: the poll loop reads them once.
	Interval time.Duration
	Settle   time.Duration

	// changes carries one value per settled change. It is buffered by one and sent to without
	// blocking, because the signal is "the file changed": a report the consumer has not taken yet
	// already says everything a second one would, and a poll blocked on a busy consumer would
	// have stopped watching the file in order to wait for it.
	changes chan struct{}
	// stop is closed by Stop to end the poll goroutine.
	stop chan struct{}
	// done is closed by the poll goroutine as it returns. Waiting on it is what lets Stop promise
	// that no report arrives after it returns, rather than merely making it unlikely.
	done chan struct{}
	// started records that there is a goroutine to wait for, so that a Stop on a watcher which was
	// never started cannot hang the teardown path it sits on.
	started atomic.Bool
	// stopOnce keeps a second Stop — a teardown that runs its closers twice — from closing an
	// already-closed channel.
	stopOnce sync.Once
}

// NewWatcher builds a watcher over path at the production cadence. It touches nothing, and
// starts nothing, until Start.
func NewWatcher(path string) *Watcher {
	return &Watcher{
		path:     path,
		Interval: configWatchInterval,
		Settle:   configWatchSettle,
		changes:  make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Changes delivers one value per settled change to the watched file. It is closed once Stop has
// returned, so a consumer ranging over it terminates with the watcher; a consumer selecting on it
// must treat a closed receive as the end of the watch and stop selecting on it.
func (w *Watcher) Changes() <-chan struct{} { return w.changes }

// Start samples the file and launches the poll. The baseline is taken here rather than on the first
// tick so that a change made immediately after Start is measured against the file as it stood when
// the caller began watching. A file that cannot be stat'ed at Start leaves the zero sample as the
// baseline, which no real file matches — so the file appearing later is reported as the change it
// is. A second Start is a no-op.
func (w *Watcher) Start() {
	if !w.started.CompareAndSwap(false, true) {
		return
	}
	baseline, _ := w.sample()
	go w.poll(baseline)
}

// Stop ends the poll and closes Changes. It waits for the poll goroutine to return before closing,
// so the goroutine cannot outlive the watcher and cannot send into a channel Stop has closed. Safe
// to call twice, and safe to call on a watcher that was never started.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stop)
		if w.started.Load() {
			<-w.done
		}
		close(w.changes)
	})
}

// poll is the watcher's one goroutine: sample, compare, and report once the file has held still for
// the Settle delay. last is the sample every comparison is made against; it advances on each
// observed change, so the write/truncate/rename burst of a single save moves it several times and
// reports once.
func (w *Watcher) poll(last configFileState) {
	defer close(w.done)

	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	// reportAt is when the pending change becomes reportable, and the zero time means nothing is
	// pending. Every further change pushes it out, which is the coalescing.
	var reportAt time.Time
	for {
		select {
		case <-w.stop:
			return
		case now := <-ticker.C:
			current, ok := w.sample()
			if !ok {
				continue
			}
			if !current.equal(last) {
				last = current
				reportAt = now.Add(w.Settle)
				continue
			}
			if reportAt.IsZero() || now.Before(reportAt) {
				continue
			}
			reportAt = time.Time{}
			w.report()
		}
	}
}

// report announces a settled change, dropping it when one is already queued: two undelivered
// reports say exactly what one says, and the poll must not block on the consumer.
func (w *Watcher) report() {
	select {
	case w.changes <- struct{}{}:
	default:
	}
}

// sample reads the file's two change witnesses. A failed os.Stat is not an answer and not an error:
// the file is briefly absent while an editor renames its temp file over it, so calling that a change
// would report one save twice, and calling it an error would end a watch over a file that is about to
// exist again. The tick is skipped, the last known sample stands, and the next tick retries.
func (w *Watcher) sample() (configFileState, bool) {
	info, err := os.Stat(w.path)
	if err != nil {
		return configFileState{}, false
	}
	return configFileState{modTime: info.ModTime(), size: info.Size()}, true
}
