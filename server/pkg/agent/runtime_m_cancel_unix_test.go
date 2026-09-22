//go:build unix

package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func runtimeMCancelFakeScript(ignoreTerm bool) string {
	trap := "trap 'exit 0' TERM\n"
	if ignoreTerm {
		trap = "trap '' TERM\n"
	}
	return "#!/bin/sh\n" + trap +
		`# Background grandchild so the test can assert the *whole* group is
# terminated on cancellation, not just the direct child.
( sleep 300 ) &
child=$!
if [ -n "$OPENCODE_PID_FILE" ]; then
  printf '%s %s\n' "$$" "$child" > "$OPENCODE_PID_FILE"
fi
printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'
while true; do
  printf '{"type":"text","timestamp":2,"sessionID":"ses_fake","part":{"type":"text","text":"tick"}}\n'
  sleep 0.1
done
`
}

func TestRuntimeMCancellationTerminatesProcessGroupGraceful(t *testing.T) {
	runRuntimeMCancellationTest(t, runtimeMCancelFakeScript(false))
}

func TestRuntimeMCancellationEscalatesToSIGKILL(t *testing.T) {
	runtimeMTerminateGraceNanos.Store(int64(300 * time.Millisecond))
	t.Cleanup(func() { runtimeMTerminateGraceNanos.Store(0) })
	runRuntimeMCancellationTest(t, runtimeMCancelFakeScript(true))
}

func runRuntimeMCancellationTest(t *testing.T, script string) {
	t.Helper()

	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "pids")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("runtime-m", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"OPENCODE_PID_FILE": pidFile},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Cwd: tempDir})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	go func() {
		for range session.Messages {
		}
	}()

	pids := waitForPids(t, pidFile)

	cancel()

	select {
	case res := <-session.Result:
		if res.Status != "aborted" {
			t.Errorf("status = %q, want aborted", res.Status)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after cancellation (possible scanner deadlock or unkilled process)")
	}

	for _, pid := range pids {
		waitProcessGone(t, pid)
	}
}

func waitForPids(t *testing.T, pidFile string) []int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(pidFile)
		if err == nil {
			fields := strings.Fields(string(raw))
			if len(fields) >= 2 {
				pids := make([]int, 0, len(fields))
				ok := true
				for _, f := range fields {
					n, perr := strconv.Atoi(f)
					if perr != nil || n <= 0 {
						ok = false
						break
					}
					pids = append(pids, n)
				}
				if ok {
					return pids
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("fake opencode never recorded its pids in %s", pidFile)
	return nil
}

func waitProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("process %d still alive after cancellation — orphaned/leaked", pid)
}
