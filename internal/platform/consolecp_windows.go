//go:build windows

package platform

import "golang.org/x/sys/windows"

// procGetOEMCP is loaded lazily because golang.org/x/sys/windows has no wrapper for GetOEMCP.
var procGetOEMCP = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetOEMCP")

// ConsoleOutputCodepage returns the codepage console programs write their output in: the console's
// output codepage when this process has a console, else the system OEM codepage a console would
// start in — the one a child spawned without a console of ours writes. It returns 65001 (UTF-8,
// the pass-through value for DecodeConsoleOutput) when neither can be read.
func ConsoleOutputCodepage() uint32 {
	codepage, err := windows.GetConsoleOutputCP()
	if err == nil && codepage != 0 {
		return codepage
	}
	return oemCodepage()
}

// oemCodepage returns the system OEM codepage, or 65001 when kernel32 lacks GetOEMCP or it
// answers 0. GetOEMCP sets no last error, so the call's error result carries nothing to report.
func oemCodepage() uint32 {
	if err := procGetOEMCP.Find(); err != nil {
		return codepageUTF8
	}
	codepage, _, _ := procGetOEMCP.Call()
	if codepage == 0 {
		return codepageUTF8
	}
	return uint32(codepage)
}
