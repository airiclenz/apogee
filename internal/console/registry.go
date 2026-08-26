package console

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MaxOpen is how many Consoles one engine holds at once. It is a constant rather than a knob on
// purpose (ADR 0059 §6): the number exists to stop a model from filling the host with forgotten
// shells, and a model that needs a fifth one is a model that has lost track of the four it has —
// what it needs is to close one, which the refusal tells it by name.
const MaxOpen = 4

// The two ways the registry declines. Both are sentinels the layer above matches on before it
// turns them into text for the model, and both carry the ids in their message so that text can
// say which Consoles it is talking about.
var (
	// ErrUnknown reports an id this registry does not hold — never issued, or already closed.
	// A Console that a restart or a /new took away is an unknown id too: the process that held
	// it is gone, so "unknown" is the whole truth about it.
	ErrUnknown = errors.New("no such console")
	// ErrTooMany reports that MaxOpen Consoles are already open.
	ErrTooMany = errors.New("too many open consoles")
)

// Console is one open console: a live process behind a pseudo-terminal, plus the three facts the
// engine and the model need about it — the id they address it by, the delegation that owns it,
// and the command line it was opened with.
//
// Everything it can do is forwarded to the process, and everything it exposes is safe from any
// goroutine. Only the registry closes one: a Console removed from the registry is finished, so
// closing has to be the same act as forgetting.
type Console struct {
	// ID is the small positive integer the model addresses this Console by. Ids are issued in
	// order and never reused within an engine.
	ID int
	// Owner is the engine-minted owner key of the delegation that opened this Console
	// ([Registry.MintOwner]), empty for the top-level agent. It is what
	// [Registry.CloseOwnedBy] matches on when a delegation ends (ADR 0059 §6).
	Owner string
	// Command is the command line as the model gave it, kept for display: the open result
	// names it, and a transcript reading "console 3" is only useful next to what console 3 is.
	Command string

	proc *Process
}

// Read returns the output produced since the previous read, with terminal control sequences
// stripped, and how many bytes the buffer dropped over the same span. See [Process.Read] for the
// wait semantics.
func (c *Console) Read(wait time.Duration) (string, int) { return c.proc.Read(wait) }

// Write sends input to the Console's terminal, where its process reads it as keyboard input.
func (c *Console) Write(input []byte) (int, error) { return c.proc.Write(input) }

// Kill stops the Console's process and everything it spawned, without waiting for the exit to be
// recorded. The Console stays in the registry — killing is not closing.
func (c *Console) Kill() { c.proc.Kill() }

// Alive reports whether the Console's process is still running.
func (c *Console) Alive() bool { return c.proc.Alive() }

// ExitCode returns the code the process exited with, or -1 while it runs and for a process a
// signal killed — Alive tells those two apart.
func (c *Console) ExitCode() int { return c.proc.ExitCode() }

// DenialStopped reports that the kill-on-denial watch stopped this Console: it was confined and
// its output carried an OS denial signature (ADR 0056 §2).
func (c *Console) DenialStopped() bool { return c.proc.DenialStopped() }

// close tears the process down and waits for it to be reaped, so Alive and ExitCode are final
// afterwards and the tail it printed on the way out is readable. Only the registry calls it.
func (c *Console) close() error { return c.proc.Close() }

// OpenSpec is one request to open a Console: who is asking, what to run, and how the caller has
// already prepared the command. Everything below Owner and Command is handed straight to the
// process layer's [Spec] — the registry adds identity and ownership to it and nothing else.
type OpenSpec struct {
	// Owner is the engine-minted owner key of the delegation opening this Console
	// ([Registry.MintOwner]), empty at the top level.
	Owner string
	// Command is the command line as given, for display in results and transcripts.
	Command string
	// Argv is the program and arguments actually executed — the shell wrapping of Command.
	Argv []string
	// Dir is the working directory, already resolved and fenced by the caller.
	Dir string
	// Env is the child's complete environment; nil inherits this process's.
	Env []string
	// Confined reports that the caller fenced the command, which puts the kill-on-denial watch
	// on the Console's output path.
	Confined bool
	// Prepare is the caller's hook on the assembled command — confinement and refusals. nil
	// means no preparation.
	Prepare func(*exec.Cmd) error
}

// Registry is the set of Consoles one engine holds open. It is LIVE HOST STATE: the processes in
// it belong to this run of the program, so the registry is built at engine construction, shared
// by pointer with every delegation, and never serialized into a session (ADR 0059 §1).
//
// A tool call reaches it through the context seam in context.go rather than through a tool
// instance, because tool instances are rebuilt mid-session while the processes must not be.
//
// All of its methods are safe from any goroutine. Two of them — [Registry.CloseOwnedBy] and
// [Registry.CloseAll] — also tolerate a nil receiver, which reads as "this engine holds no
// consoles"; the rest need a real registry, which the seam guarantees every dispatch carries.
type Registry struct {
	mu sync.Mutex
	// consoles holds every Console this engine has open, including ones whose process has
	// already exited: an exited Console is still readable, so it stays until someone closes it.
	consoles map[int]*Console
	// nextID is the id the next successfully opened Console gets. It only ever climbs, so a
	// closed id is never handed to a second process and a stale id in a model's context can
	// never silently address a different Console.
	nextID int
	// nextOwner is the number the next minted owner key renders. It climbs like nextID and for
	// the same reason: a key handed to a second delegation would let that delegation reap — and
	// drive — the Consoles of the one that retired the key.
	nextOwner int
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{consoles: make(map[int]*Console), nextID: 1, nextOwner: 1}
}

// MintOwner returns a fresh owner key for one delegation: never empty, and never handed out twice
// by this registry.
//
// The registry mints because the registry is what COMPARES the key ([Registry.CloseOwnedBy]), so
// the namespace has exactly one author. The alternative — the tool-call id of the sub_agent call
// that spawned the delegation — is the model's to choose, and two siblings of one Turn can carry
// the same id: a collision there reaps another delegation's shells.
//
// A nil registry mints "" — the top-level key — because an engine with no registry holds no
// Console for any key to reach, so the caller needs no nil check of its own.
func (r *Registry) MintOwner() string {
	if r == nil {
		return ""
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := "run-" + strconv.Itoa(r.nextOwner)
	r.nextOwner++
	return key
}

// Open starts spec's process and registers it as a new Console.
//
// It refuses with [ErrTooMany] once MaxOpen Consoles are open — counting the ones whose process
// has exited, since those still hold an id and unread output — and the refusal names the open
// ids so the caller can tell the model which ones it could close. A process that fails to start
// (including [ErrUnsupported] on a platform with no pseudo-terminal backend) consumes no id.
//
// The registry is locked across the start, which is what keeps the cap exact and the ids in
// order under concurrent delegations; the wait for the process to say something is the caller's
// business afterwards, not the registry's.
func (r *Registry) Open(spec OpenSpec) (*Console, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.consoles) >= MaxOpen {
		return nil, fmt.Errorf("%w (limit %d): close one of %s first",
			ErrTooMany, MaxOpen, formatIDs(r.openIDs()))
	}

	process, err := Start(Spec{
		Argv:     spec.Argv,
		Dir:      spec.Dir,
		Env:      spec.Env,
		Confined: spec.Confined,
		Prepare:  spec.Prepare,
	})
	if err != nil {
		return nil, err
	}

	console := &Console{ID: r.nextID, Owner: spec.Owner, Command: spec.Command, proc: process}
	r.consoles[console.ID] = console
	r.nextID++
	return console, nil
}

// Get returns the Console with this id, or false when the registry holds none — an id that was
// never issued, one already closed, or one from before a restart.
func (r *Registry) Get(id int) (*Console, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	console, ok := r.consoles[id]
	return console, ok
}

// OpenIDs returns the ids of every open Console, in ascending order — what a caller shows the
// model when the id it asked for is not one of them.
func (r *Registry) OpenIDs() []int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.openIDs()
}

// OpenIDsOwnedBy returns the ids of the open Consoles this owner opened, in ascending order like
// [Registry.OpenIDs] — what a caller shows the model when the id it asked for is not one it may
// address. A run learns only about its own Consoles, so the refusal it reads cannot tell it that
// another delegation's id exists (ADR 0059 §6).
//
// It is a query, not a policy: [Registry.Get] stays owner-blind, and the sweeps that match on the
// key ([Registry.CloseOwnedBy]) are unaffected.
func (r *Registry) OpenIDsOwnedBy(owner string) []int {
	r.mu.Lock()
	defer r.mu.Unlock()

	ids := make([]int, 0, len(r.consoles))
	for id, console := range r.consoles {
		if console.Owner == owner {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// Close stops the Console with this id and drops it from the registry, returning once the
// process has been reaped so its exit code is final. It reports [ErrUnknown] for an id the
// registry does not hold, which is what makes a second close of the same id an error rather than
// a silent no-op.
//
// The Console is dropped whatever the teardown says: an error here is the pseudo-terminal
// refusing to be released, not a Console that is still running, and retrying the close would
// only report an id that is already gone.
func (r *Registry) Close(id int) error {
	r.mu.Lock()
	console, ok := r.consoles[id]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("%w %d", ErrUnknown, id)
	}
	delete(r.consoles, id)
	r.mu.Unlock()

	return console.close()
}

// CloseOwnedBy closes every Console this owner opened and leaves the rest running. It is the
// delegation-end sweep: a sub-agent's result is text, and a live orphan is not a result
// (ADR 0059 §6), while the Consoles the top-level agent opened — owner "" — outlive it.
//
// It never reports anything. Teardown errors are the terminal's, not the process's, and there is
// no caller here to act on one: the delegation has already ended.
func (r *Registry) CloseOwnedBy(owner string) {
	if r == nil {
		return
	}

	r.mu.Lock()
	closing := make([]*Console, 0, len(r.consoles))
	for id, console := range r.consoles {
		if console.Owner == owner {
			closing = append(closing, console)
			delete(r.consoles, id)
		}
	}
	r.mu.Unlock()

	closeEach(closing)
}

// CloseAll closes every Console the engine holds. It runs where the whole set stops being
// addressable: the engine's exit, and /new — a cleared context cannot name a console it can no
// longer see (ADR 0059 §1). Like CloseOwnedBy it never reports anything, and it is a no-op on an
// empty registry and on a nil one.
func (r *Registry) CloseAll() {
	if r == nil {
		return
	}

	r.mu.Lock()
	closing := make([]*Console, 0, len(r.consoles))
	for id, console := range r.consoles {
		closing = append(closing, console)
		delete(r.consoles, id)
	}
	r.mu.Unlock()

	closeEach(closing)
}

// openIDs returns the open ids in ascending order. The caller holds the lock.
func (r *Registry) openIDs() []int {
	ids := make([]int, 0, len(r.consoles))
	for id := range r.consoles {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// closeEach tears down a batch of Consoles already removed from the registry, one after the
// other. Each teardown is bounded by the process layer, so a wedged terminal delays the sweep
// instead of wedging it.
func closeEach(consoles []*Console) {
	for _, console := range consoles {
		_ = console.close()
	}
}

// formatIDs renders console ids the way a message to the model names them: "1, 2, 3".
func formatIDs(ids []int) string {
	rendered := make([]string, 0, len(ids))
	for _, id := range ids {
		rendered = append(rendered, strconv.Itoa(id))
	}
	return strings.Join(rendered, ", ")
}
