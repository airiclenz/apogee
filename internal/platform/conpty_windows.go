//go:build windows

package platform

import (
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrPseudoConsoleUnavailable is wrapped by StartPseudoConsole when the host will not create a
// pseudoconsole at all — a Windows below 10 1809, where the API does not exist, or one that
// refuses it. It is the failure a caller reports as "no console backend here" rather than as a
// broken launch.
var ErrPseudoConsoleUnavailable = errors.New("no pseudoconsole on this host")

// pseudoConsoleNoFlags is the flags word CreatePseudoConsole is given: no inherited cursor, so
// the pseudoconsole never stops to ask the caller where the cursor is.
const pseudoConsoleNoFlags = 0

// procCreatePseudoConsole is resolved before the x/sys wrapper is called, because the wrapper
// takes the procedure's address unconditionally and panics on a Windows that lacks it.
var procCreatePseudoConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreatePseudoConsole")

// procUpdateProcThreadAttribute is called directly rather than through the x/sys wrapper for one
// reason: PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE takes the pseudoconsole HANDLE in the value slot, not
// a pointer to it, and spelling that as an unsafe.Pointer is exactly the misuse `go vet`'s unsafeptr
// check exists to catch. Allocation still goes through the wrapper's container.
var procUpdateProcThreadAttribute = windows.NewLazySystemDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute")

// PseudoConsoleSpec describes one process to start inside a pseudoconsole.
type PseudoConsoleSpec struct {
	// Path is the executable CreateProcess runs. Empty leaves it to CreateProcess to take the
	// first token of CommandLine and search for it, so a caller that resolved a path passes it.
	Path string
	// CommandLine is handed to CreateProcess verbatim — never re-joined or re-quoted — because
	// Windows has no argv at the syscall boundary and cmd.exe does not read the escapes an argv
	// join would add (platform.Shell.CommandLine builds it).
	CommandLine string
	// Dir is the child's working directory; empty inherits this process's.
	Dir string
	// Env is the child's whole environment as KEY=value entries; nil inherits this process's.
	// An entry that cannot be spelled in UTF-16 (an embedded NUL) is dropped.
	Env []string
	// Cols and Rows are the pseudoconsole's size in character cells; both must be positive.
	Cols, Rows int
	// Token, when non-zero, is the primary token the child runs under — the restricted Low token a
	// Confiner put on an exec.Cmd's SysProcAttr — and the launch goes through CreateProcessAsUser.
	// It is borrowed, never owned: the launch duplicates it for the call's lifetime and closes only
	// the duplicate, so the caller keeps it and a confiner Close racing the launch cannot free the
	// handle mid-call. Zero runs the child under this process's own token.
	Token windows.Token
}

// PseudoConsole is a process running inside a Windows pseudoconsole (ConPTY), held in a
// kill-on-close Job Object together with every descendant it spawns.
//
// Read returns what the pseudoconsole renders — the child's output re-encoded as VT sequences —
// and Write types into the child's console input. A caller must keep reading: a pseudoconsole
// whose output pipe fills stops the child mid-write. Wait reports the exit code, Kill ends the
// whole tree, and Close releases everything; all of them are safe to call from any goroutine.
//
// A caller that reads to the end splits Close in two: Release ends the tree and the pseudoconsole
// but leaves the output open, so the reader drains conhost's final flush and sees io.EOF, and
// CloseOutput then gives the read end back. Closing the output at once — what Close does — cancels
// a read still in flight, and with it the last frame a short command painted.
type PseudoConsole struct {
	console windows.Handle
	// in is the write end of the child's console input; out is the read end of what the
	// pseudoconsole renders.
	in, out *os.File
	job     *jobTeardown
	pid     int

	// mu guards process: the reaper closes the leader's handle once it has exited and sets it to
	// windows.InvalidHandle, and Kill must never terminate through a handle closed under it.
	mu      sync.Mutex
	process windows.Handle

	done     chan struct{} // closed by the reaper once exitCode and waitErr are final
	exitCode int
	waitErr  error

	releaseOnce sync.Once
	releaseErr  error
	outputOnce  sync.Once
	outputErr   error
	closeOnce   sync.Once
	closeErr    error
}

// StartPseudoConsole creates a pseudoconsole of spec's size and starts spec's command inside it.
//
// The process is created SUSPENDED, assigned to a fresh Job Object carrying
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, and only then resumed, so no descendant can be spawned
// before the job holds the tree — the pre-join window that NewProcessTeardown documents for an
// os/exec launch does not exist here. A job that cannot be created or joined degrades Kill to the
// leader alone, as it does for every other spawner (planTreeKill).
//
// A host without a pseudoconsole yields an error wrapping ErrPseudoConsoleUnavailable; on every
// failure path nothing is left running and no handle is held.
func StartPseudoConsole(spec PseudoConsoleSpec) (*PseudoConsole, error) {
	if spec.Cols <= 0 || spec.Rows <= 0 || spec.Cols > math.MaxInt16 || spec.Rows > math.MaxInt16 {
		return nil, fmt.Errorf("pseudoconsole size %dx%d is out of range", spec.Cols, spec.Rows)
	}
	if spec.CommandLine == "" {
		return nil, errors.New("pseudoconsole: empty command line")
	}
	c, err := newPseudoConsole(spec.Cols, spec.Rows)
	if err != nil {
		return nil, err
	}
	if err := c.launch(spec); err != nil {
		c.discard()
		return nil, err
	}
	return c, nil
}

// newPseudoConsole creates the pseudoconsole and the two pipe ends this process keeps.
func newPseudoConsole(cols, rows int) (*PseudoConsole, error) {
	if err := procCreatePseudoConsole.Find(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPseudoConsoleUnavailable, err)
	}
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("pseudoconsole input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		_ = windows.CloseHandle(inRead)
		_ = windows.CloseHandle(inWrite)
		return nil, fmt.Errorf("pseudoconsole output pipe: %w", err)
	}
	var console windows.Handle
	err := windows.CreatePseudoConsole(
		windows.Coord{X: int16(cols), Y: int16(rows)}, inRead, outWrite, pseudoConsoleNoFlags, &console)
	// The pseudoconsole duplicated the ends it needs; this process keeps only its own two.
	_ = windows.CloseHandle(inRead)
	_ = windows.CloseHandle(outWrite)
	if err != nil {
		_ = windows.CloseHandle(inWrite)
		_ = windows.CloseHandle(outRead)
		return nil, fmt.Errorf("%w: CreatePseudoConsole: %v", ErrPseudoConsoleUnavailable, err)
	}
	return &PseudoConsole{
		console: console,
		in:      os.NewFile(uintptr(inWrite), "conpty-in"),
		out:     os.NewFile(uintptr(outRead), "conpty-out"),
		done:    make(chan struct{}),
	}, nil
}

// launch starts spec's command suspended inside the pseudoconsole, joins it to the job, resumes
// it, and starts the reaper.
func (c *PseudoConsole) launch(spec PseudoConsoleSpec) error {
	attrs, err := pseudoConsoleAttributes(c.console)
	if err != nil {
		return err
	}
	defer attrs.Delete()
	call, err := newCreateProcessArgs(spec)
	if err != nil {
		return err
	}

	// The std handles are spelled out as invalid: without STARTF_USESTDHANDLES a child can inherit
	// this process's REDIRECTED stdio (a pipe under headless apogee or `go test`) in place of the
	// pseudoconsole's, and then nothing it prints reaches the console.
	si := &windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  windows.InvalidHandle,
			StdOutput: windows.InvalidHandle,
			StdErr:    windows.InvalidHandle,
		},
		ProcThreadAttributeList: attrs.List(),
	}
	pi, err := createSuspended(spec.Token, call, si)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(pi.Thread) }()

	c.process = pi.Process
	c.pid = int(pi.ProcessId)
	c.job = newJobTeardown()
	c.job.containHandle(pi.Process)
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		_ = windows.TerminateProcess(pi.Process, killedExitCode)
		_ = windows.CloseHandle(pi.Process)
		c.job.Release()
		return fmt.Errorf("ResumeThread: %w", err)
	}
	go c.reap()
	return nil
}

// createSuspended creates call's process suspended inside the pseudoconsole si carries: under
// token through CreateProcessAsUser when one is given, under this process's own token otherwise.
// The flags and the attribute list are the same either way, so a confined child gets the same
// console, the same environment encoding and the same suspended start the job containment needs.
func createSuspended(token windows.Token, call createProcessArgs, si *windows.StartupInfoEx) (windows.ProcessInformation, error) {
	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if token == 0 {
		if err := windows.CreateProcess(call.path, call.commandLine, nil, nil, false,
			flags, call.env, call.dir, &si.StartupInfo, &pi); err != nil {
			return pi, fmt.Errorf("CreateProcess: %w", err)
		}
		return pi, nil
	}
	held, err := duplicateToken(token)
	if err != nil {
		return pi, err
	}
	defer func() { _ = held.Close() }()
	if err := windows.CreateProcessAsUser(held, call.path, call.commandLine, nil, nil, false,
		flags, call.env, call.dir, &si.StartupInfo, &pi); err != nil {
		return pi, fmt.Errorf("CreateProcessAsUser: %w", err)
	}
	return pi, nil
}

// duplicateToken takes a handle of this process's own on a borrowed token, with the borrowed
// handle's access, so the token stays valid for the launch even if its owner closes it meanwhile.
func duplicateToken(token windows.Token) (windows.Token, error) {
	self := windows.CurrentProcess()
	var held windows.Handle
	if err := windows.DuplicateHandle(self, windows.Handle(token), self, &held, 0, false,
		windows.DUPLICATE_SAME_ACCESS); err != nil {
		return 0, fmt.Errorf("pseudoconsole token: %w", err)
	}
	return windows.Token(held), nil
}

// pseudoConsoleAttributes builds the attribute list that hands console to the child as its
// console. The caller deletes it once CreateProcess has returned.
func pseudoConsoleAttributes(console windows.Handle) (*windows.ProcThreadAttributeListContainer, error) {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("attribute list: %w", err)
	}
	rc, _, callErr := procUpdateProcThreadAttribute.Call(
		uintptr(unsafe.Pointer(attrs.List())),
		0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		uintptr(console),
		unsafe.Sizeof(console),
		0,
		0,
	)
	if rc == 0 {
		attrs.Delete()
		return nil, fmt.Errorf("UpdateProcThreadAttribute: %w", callErr)
	}
	return attrs, nil
}

// createProcessArgs is spec spelled the way CreateProcess takes it: nil for every field the child
// inherits.
type createProcessArgs struct {
	path, commandLine, dir *uint16
	env                    *uint16
}

// newCreateProcessArgs encodes spec for CreateProcess. The command line gets a buffer of its own,
// which CreateProcessW requires because it may write into it.
func newCreateProcessArgs(spec PseudoConsoleSpec) (createProcessArgs, error) {
	var call createProcessArgs
	var err error
	if call.commandLine, err = windows.UTF16PtrFromString(spec.CommandLine); err != nil {
		return call, fmt.Errorf("pseudoconsole command line: %w", err)
	}
	if spec.Path != "" {
		if call.path, err = windows.UTF16PtrFromString(spec.Path); err != nil {
			return call, fmt.Errorf("pseudoconsole executable: %w", err)
		}
	}
	if spec.Dir != "" {
		if call.dir, err = windows.UTF16PtrFromString(spec.Dir); err != nil {
			return call, fmt.Errorf("pseudoconsole directory: %w", err)
		}
	}
	if spec.Env != nil {
		call.env = environmentBlock(spec.Env)
	}
	return call, nil
}

// environmentBlock packs env into the doubly-NUL-terminated UTF-16 block CreateProcess wants. An
// entry with an embedded NUL cannot be spelled and is dropped rather than failing the launch.
func environmentBlock(env []string) *uint16 {
	var block []uint16
	for _, entry := range env {
		encoded, err := windows.UTF16FromString(entry)
		if err != nil {
			continue
		}
		block = append(block, encoded...)
	}
	if len(block) == 0 {
		// An empty block is still two NULs: the empty list's terminator after an empty entry's.
		block = append(block, 0)
	}
	block = append(block, 0)
	return &block[0]
}

// reap waits for the leader to exit, records how it ended, and gives its handle back.
func (c *PseudoConsole) reap() {
	defer close(c.done)

	// Only the reaper ever replaces process, so reading it here without the lock is race-free.
	c.exitCode, c.waitErr = waitExitCode(c.process)

	c.mu.Lock()
	defer c.mu.Unlock()
	_ = windows.CloseHandle(c.process)
	c.process = windows.InvalidHandle
}

// waitExitCode blocks until process exits and returns its exit code, or -1 and the reason the
// exit could not be observed.
func waitExitCode(process windows.Handle) (int, error) {
	if _, err := windows.WaitForSingleObject(process, windows.INFINITE); err != nil {
		return -1, fmt.Errorf("waiting for the pseudoconsole process: %w", err)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(process, &code); err != nil {
		return -1, fmt.Errorf("pseudoconsole process exit code: %w", err)
	}
	return int(code), nil
}

// Pid returns the leader's process ID.
func (c *PseudoConsole) Pid() int { return c.pid }

// Read reads what the pseudoconsole has rendered. It returns io.EOF once the pseudoconsole is
// released and its output drained, and an error once Close or CloseOutput has closed the pipe.
func (c *PseudoConsole) Read(p []byte) (int, error) { return c.out.Read(p) }

// Write types p into the child's console input.
func (c *PseudoConsole) Write(p []byte) (int, error) { return c.in.Write(p) }

// Wait blocks until the leader exits and returns its exit code. The error is non-nil only when
// the exit could not be observed, and the code is then -1. Descendants are not waited for: Kill or
// Close reaps them.
func (c *PseudoConsole) Wait() (int, error) {
	<-c.done
	return c.exitCode, c.waitErr
}

// Kill terminates the leader and every descendant the job holds. It is idempotent, and a no-op
// once the tree has gone.
func (c *PseudoConsole) Kill() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.job.terminateHandle(c.process)
}

// Close kills the tree, closes the pseudoconsole and both pipe ends, and releases the job: Release
// and CloseOutput back to back. A read in flight on another goroutine returns io.EOF or an error,
// so a caller that wants the final frame calls Release, drains to io.EOF and only then
// CloseOutput. Close does not wait for the leader to be reaped — Wait does — and it is idempotent;
// the first call's error is the one every call returns.
func (c *PseudoConsole) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = errors.Join(c.Release(), c.CloseOutput())
	})
	return c.closeErr
}

// Release kills the tree, closes the pseudoconsole and the input pipe, and releases the job, but
// leaves the output pipe open: conhost flushes its final frame as it exits and closes its write
// end, so a reader drains that frame and then sees io.EOF. CloseOutput gives the read end back
// afterwards. Release is idempotent; the first call's error is the one every call returns.
func (c *PseudoConsole) Release() error {
	c.releaseOnce.Do(func() {
		c.Kill()
		c.releaseErr = c.releaseConsole()
		c.job.Release()
	})
	return c.releaseErr
}

// CloseOutput closes the read end of the pseudoconsole's output, ending a read still in flight.
// It is idempotent; the first call's error is the one every call returns.
func (c *PseudoConsole) CloseOutput() error {
	c.outputOnce.Do(func() {
		c.outputErr = c.out.Close()
	})
	return c.outputErr
}

// releaseConsole closes the pseudoconsole and then the input pipe. The pseudoconsole goes first:
// closing it is what detaches the child's console and lets a pending read see the end of output.
// The output pipe is left to CloseOutput, so the reader can drain the final frame first.
func (c *PseudoConsole) releaseConsole() error {
	windows.ClosePseudoConsole(c.console)
	return c.in.Close()
}

// discard gives back everything a pseudoconsole whose launch failed still holds. Nothing is
// running and no reader exists yet, so there is no final frame to wait for: both pipe ends go now.
func (c *PseudoConsole) discard() {
	_ = c.releaseConsole() // the launch error is the one worth reporting
	_ = c.CloseOutput()
}
