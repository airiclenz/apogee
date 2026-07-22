// Package confinetest is the shared escape-probe harness every Confiner backend's
// acceptance tests call, so "confined" means the same thing on Linux landlock, macOS
// seatbelt and the Windows restricted-token backend (confinement-execution-contract
// §6, §9.3). It builds confined *exec.Cmd values via the backend under test and asserts
// OS denial for a confined subprocess.
//
// It is test-support, not production code: it lives in its own package so a backend's
// _test.go (in package platform) can import it without a test-only build tag on the
// platform package itself. Probe drives the filesystem escape battery; ProbeNetwork
// drives the network arm separately (it needs a listener and is skipped where the
// backend cannot enforce network egress). The harness owns its temp dirs via
// t.TempDir, so cleanup is automatic — including on Windows, where the harness labels
// the box Low: the test process stays Medium and writing DOWN is permitted.
//
// The harness is parameterised over domain.Confiner (Capabilities + the prepare-in-place
// Confine over a *exec.Cmd) and over a Shell, so a backend's _test.go hands its
// platform.NewConfiner()-style value and platform.Current() straight to
// Probe/ProbeNetwork. The Shell parameter is what un-hard-codes `sh -c`, which does not
// exist on a stock Windows host; the shell-dialect fragments the battery still needs —
// how you write a byte, how you nest a shell, which profile path stands for "outside" —
// are build-tagged here (lines_windows.go / lines_other.go) rather than modelled on
// platform.Host, which describes a shell's invocation and deliberately not its built-ins.
package confinetest
