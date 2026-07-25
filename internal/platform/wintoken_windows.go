//go:build windows

package platform

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows token Confiner — the FENCE (ADR 0020 §1; confinement-execution-contract §9).
//
// This file is about the TOKEN, not the box. The box is a label on the disk and belongs to
// internal/platform/winlabel; what is minted here is the restricted, Low-integrity primary
// token the backend hands to SysProcAttr.Token, plus the one guardrail input the token's
// own identity supplies (the user-profile root).
//
// The design's reachability from an ORDINARY user account is the point of the shape below.
// Handing a foreign token to CreateProcessAsUser requires SeAssignPrimaryTokenPrivilege,
// which an unelevated interactive user does not hold; a RESTRICTED version of the caller's
// own token is the documented exception, so CreateRestrictedToken + DuplicateTokenEx is not
// one of several ways to mint the fence — it is the only one that needs no privilege the
// user must be granted first. Everything else here follows from that: the integrity level is
// lowered on the duplicate, and DISABLE_MAX_PRIVILEGE strips what a child could otherwise
// use to walk around a mandatory label.

// createRestrictedToken is CreateRestrictedToken, which golang.org/x/sys/windows v0.45.0
// does not bind — one advapi32 LazyProc, the same shape landlock takes with raw
// unix.Syscall for the landlock_* numbers x/sys has no wrapper for.
var createRestrictedToken = windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")

// disableMaxPrivilege is CreateRestrictedToken's DISABLE_MAX_PRIVILEGE flag: the new token
// drops every privilege but SeChangeNotifyPrivilege. It is DEFENCE IN DEPTH, not the fence
// — it strips SeBackup/SeRestore/SeTakeOwnership/SeDebug, with which a child could otherwise
// walk around a mandatory label. No restricting SIDs and no deny-only SIDs are used: they
// force a double access check that breaks ordinary programs (which must still read system
// DLLs) and buy nothing the integrity level does not already give under ADR 0012's threat
// model.
const disableMaxPrivilege = 0x1

// mintRestrictedLowToken mints the fence: a copy of this process's token with every
// privilege stripped (CreateRestrictedToken + DISABLE_MAX_PRIVILEGE) and its integrity level
// set to Low. CreateProcessAsUser accepts it without SeAssignPrimaryToken because it is a
// restricted version of the caller's own token — which is what makes the whole design
// reachable from an ordinary user account.
func mintRestrictedLowToken() (windows.Token, error) {
	var self windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_ADJUST_DEFAULT,
		&self); err != nil {
		return 0, fmt.Errorf("apogee: confine: OpenProcessToken: %w", err)
	}
	defer func() { _ = self.Close() }()

	var restricted windows.Token
	if ret, _, err := createRestrictedToken.Call(
		uintptr(self), disableMaxPrivilege,
		0, 0, // no deny-only SIDs
		0, 0, // no privileges to delete beyond DISABLE_MAX_PRIVILEGE
		0, 0, // no restricting SIDs (ADR 0020 §1)
		uintptr(unsafe.Pointer(&restricted)),
	); ret == 0 {
		return 0, fmt.Errorf("apogee: confine: CreateRestrictedToken: %w", err)
	}
	defer func() { _ = restricted.Close() }()

	// CreateRestrictedToken's result is usable as-is only with the access rights it was
	// granted; duplicating it produces the primary token with the access CreateProcessAsUser
	// needs, and leaves the intermediate handle to be closed here.
	var primary windows.Token
	if err := windows.DuplicateTokenEx(restricted, windows.TOKEN_ALL_ACCESS, nil,
		windows.SecurityImpersonation, windows.TokenPrimary, &primary); err != nil {
		return 0, fmt.Errorf("apogee: confine: DuplicateTokenEx: %w", err)
	}

	sid, err := windows.CreateWellKnownSid(windows.WinLowLabelSid)
	if err != nil {
		_ = primary.Close()
		return 0, fmt.Errorf("apogee: confine: CreateWellKnownSid(Low): %w", err)
	}
	label := windows.Tokenmandatorylabel{
		Label: windows.SIDAndAttributes{Sid: sid, Attributes: windows.SE_GROUP_INTEGRITY},
	}
	if err := windows.SetTokenInformation(primary, windows.TokenIntegrityLevel,
		(*byte)(unsafe.Pointer(&label)), label.Size()); err != nil {
		_ = primary.Close()
		return 0, fmt.Errorf("apogee: confine: SetTokenInformation(TokenIntegrityLevel=Low): %w", err)
	}
	return primary, nil
}

// userProfileRoot returns the user-profile directory the labelling guardrails protect.
func userProfileRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
