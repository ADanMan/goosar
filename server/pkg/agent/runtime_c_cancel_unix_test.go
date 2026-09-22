//go:build unix

package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func runtimeCCancelFakeScript(ignoreTerm bool) string {
	trap := "trap 'exit 0' TERM\n"
	if ignoreTerm {
		trap = "trap '' TERM\n"
	}
	return "#!/bin/sh\n" + trap +
		`# Background grandchild so the test can assert the *whole* group is
# terminated on cancellation, not just the direct child.
( sleep 300 ) &
child=$!
if [ -n "$CLAUDE_PID_FILE" ]; then
  printf '%s %s\n' "$$" "$child" > "$CLAUDE_PID_FILE"
fi
printf '{"type":"system","session_id":"ses_fake"}\n'
while true; do
  printf '{"type":"system","session_id":"ses_fake"}\n'
  sleep 0.1
done
`
}

func runtimeCMixedSignalFakeScript() string {
	return "#!/bin/sh\n" + "trap 'exit 0' TERM\n" +
		`# Grandchild ignores TERM and redirects its stdio away from the pipe so
# it does not keep claude's stdout open after the leader exits.
( trap '' TERM; sleep 300 ) </dev/null >/dev/null 2>&1 &
child=$!
if [ -n "$CLAUDE_PID_FILE" ]; then
  printf '%s %s\n' "$$" "$child" > "$CLAUDE_PID_FILE"
fi
printf '{"type":"system","session_id":"ses_fake"}\n'
while true; do
  printf '{"type":"system","session_id":"ses_fake"}\n'
  sleep 0.1
done
`
}

func TestRuntimeCCancellationTerminatesProcessGroupGraceful(t *testing.T) {
	runRuntimeCCancellationTest(t, runtimeCCancelFakeScript(false))
}

func TestRuntimeCCancellationEscalatesToSIGKILL(t *testing.T) {
	runtimeCTerminateGraceNanos.Store(int64(300 * time.Millisecond))
	t.Cleanup(func() { runtimeCTerminateGraceNanos.Store(0) })
	runRuntimeCCancellationTest(t, runtimeCCancelFakeScript(true))
}

func TestRuntimeCCancellationEscalatesWhenDescendantIgnoresTERM(t *testing.T) {
	runtimeCTerminateGraceNanos.Store(int64(300 * time.Millisecond))
	t.Cleanup(func() { runtimeCTerminateGraceNanos.Store(0) })
	runRuntimeCCancellationTest(t, runtimeCMixedSignalFakeScript())
}

func runRuntimeCCancellationTest(t *testing.T, script string) {
	t.Helper()

	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "pids")
	fakePath := filepath.Join(tempDir, "claude")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("runtime-c", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"CLAUDE_PID_FILE": pidFile},
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
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
