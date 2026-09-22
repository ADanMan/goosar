//go:build !windows

package agent

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const processGroupGrace = 3 * time.Second

var killSignal = syscall.SIGKILL

func contextWithCancel(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(context.Background())
}

func waitForDescendant(t *testing.T, leaderPid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("pgrep", "-P", strconv.Itoa(leaderPid)).Output()
		if strings.TrimSpace(string(out)) != "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("leader never spawned its descendant")
}
