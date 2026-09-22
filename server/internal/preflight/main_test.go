package preflight

import (
	"os"
	"testing"

	"github.com/adanman/goosar/server/internal/testtiming"
)

func TestMain(m *testing.M) {
	versionTimeout = testtiming.Scale(versionTimeout)
	os.Exit(m.Run())
}
