package platform

import (
	"testing"

	"github.com/airiclenz/apogee/internal/platform/confinetest"
)

// TestConfinetestHelper is the in-box client of the escape battery's row #15
// (confinement-execution-contract §6.2): confinetest.Probe re-runs this test binary inside the
// box, selecting this test alone, to connect to a unix socket the parent listens on outside it.
// In an ordinary run it does nothing. It carries no build tag because every OS's Probe driver
// lives in this package's test binary; the name is confinetest.HelperTestName.
func TestConfinetestHelper(t *testing.T) {
	confinetest.RunHelper(t)
}
