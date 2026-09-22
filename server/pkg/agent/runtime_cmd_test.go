package agent

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestEveryRuntimeCommandIsOwned(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	call := regexp.MustCompile(`\bexec\.Command(Context)?\(`)
	var offenders []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "proc_windows.go" {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || !call.MatchString(line) {
				continue
			}
			if !strings.Contains(line, "newRuntimeCmd(exec.Command") {
				offenders = append(offenders, name+":"+strconv.Itoa(i+1))
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("exec.Command outside newRuntimeCmd (process tree would not be owned):\n  %s", strings.Join(offenders, "\n  "))
	}
}

func TestNewRuntimeCmdGroupWideCancelKillsDescendants(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	ctx, cancel := contextWithCancel(t)

	cmd := newRuntimeCmd(exec.CommandContext(ctx, "sh", "-c", "sleep 300 & wait"))
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForDescendant(t, cmd.Process.Pid)

	cancel()
	_ = cmd.Wait()
	if !waitProcessGroupGone(cmd.Process, processGroupGrace) {
		signalProcessGroup(cmd.Process, killSignal)
		t.Fatal("descendant survived cancellation: the process group was not signalled as a whole")
	}
}
