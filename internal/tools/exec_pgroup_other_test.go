//go:build windows

package tools

import (
	"errors"

	"golang.org/x/sys/windows"
)

// syscallKill0 is the Windows liveness probe behind the process-tree teardown tests, the
// counterpart of POSIX's signal 0: a nil error means the process is still running, any error
// means it is gone. Windows has no signals, so it opens the process and asks for its exit
// code — STILL_ACTIVE is the "no exit code yet" sentinel. Both halves matter: OpenProcess
// fails once the process object is destroyed, and a terminated process whose handle is still
// held by someone opens fine but reports a real exit code.
func syscallKill0(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return err
	}
	if code == stillActive {
		return nil
	}
	return errors.New("process has exited")
}

// stillActive is STILL_ACTIVE (STATUS_PENDING), the exit code GetExitCodeProcess reports for
// a process that has not exited yet.
const stillActive = uint32(windows.STATUS_PENDING)

// killPID terminates pid, best-effort. It exists so a test that deliberately leaves a process
// running (the clean-run parity case) cannot leak it into the machine.
func killPID(pid int) {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(h) }()
	_ = windows.TerminateProcess(h, 1)
}
