package contract

import (
	"os"
	"testing"
)

// TestMain lets us print a coverage summary once, after all selected
// subtests (whatever -run matched) have finished, rather than only when the
// full suite runs.
func TestMain(m *testing.M) {
	code := m.Run()
	printCoverage()
	os.Exit(code)
}
