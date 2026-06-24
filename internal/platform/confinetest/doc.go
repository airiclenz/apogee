// Package confinetest is the shared escape-probe harness both Confiner backends'
// acceptance tests call, so "confined" means the same thing on Linux landlock and
// macOS seatbelt (confinement-execution-contract §6). It builds confined *exec.Cmd
// values via the backend under test and asserts OS denial for a confined subprocess.
//
// It is test-support, not production code: it lives in its own package so a backend's
// _test.go (in package platform) can import it without a test-only build tag on the
// platform package itself. Probe drives the filesystem escape battery; ProbeNetwork
// drives the network arm separately (it needs a listener and is skipped where the
// backend cannot enforce network egress). The harness owns its temp dirs via
// t.TempDir, so cleanup is automatic.
//
// The harness is parameterised over domain.Confiner directly (Capabilities + the
// prepare-in-place Confine over a *exec.Cmd), so a backend's _test.go hands its
// platform.NewConfiner()-style value straight to Probe/ProbeNetwork.
package confinetest
