//go:build !windows

package execenv

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const envRootHolderTestMode = "execenv-env-root-holder"

func TestEnvRootHolderProcess(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != envRootHolderTestMode {
		return
	}
	in := bufio.NewReader(os.Stdin)
	line, err := in.ReadString('\n')
	if err != nil {
		os.Exit(3)
	}
	parts := strings.Split(strings.TrimSpace(line), "\t")
	if len(parts) != 3 {
		os.Exit(3)
	}
	claim, err := ClaimEnvRoot(parts[0], parts[1], parts[2])
	if err != nil {
		os.Exit(4)
	}
	os.Stdout.WriteString("held\n")
	_, _ = io.Copy(io.Discard, in)
	claim.Release()
	os.Exit(0)
}

func TestClaimEnvRootIsExclusiveAcrossProcesses(t *testing.T) {
	workspacesRoot := t.TempDir()
	const (
		wsID   = "ws-cross-process"
		taskID = "01a01ec0-e69d-7000-8000-0123456789ab"
	)

	child := exec.Command(os.Args[0], "-test.run=^TestEnvRootHolderProcess$", "--", envRootHolderTestMode)
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := child.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()

	if _, err := io.WriteString(stdin, workspacesRoot+"\t"+wsID+"\t"+taskID+"\n"); err != nil {
		t.Fatalf("send claim request: %v", err)
	}
	ready := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		ready <- strings.TrimSpace(line)
	}()
	select {
	case got := <-ready:
		if got != "held" {
			t.Fatalf("holder reported %q, want held", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("holder process never claimed the env root")
	}

	envRoot := PredictRootDir(workspacesRoot, wsID, taskID)
	live := envRoot + "/workdir/in-flight.txt"
	if err := os.MkdirAll(envRoot+"/workdir", 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(live, []byte("running in another process"), 0o644); err != nil {
		t.Fatalf("seed live work: %v", err)
	}

	if _, err := ClaimEnvRoot(workspacesRoot, wsID, taskID); !errors.Is(err, ErrEnvRootBusy) {
		t.Fatalf("claim against a live foreign process: err = %v, want ErrEnvRootBusy", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("refused claim still touched the live work: %v", err)
	}

	stdin.Close()
	if err := child.Wait(); err != nil {
		t.Fatalf("holder exit: %v", err)
	}
	claim, err := ClaimEnvRoot(workspacesRoot, wsID, taskID)
	if err != nil {
		t.Fatalf("claim after the holder exited: %v", err)
	}
	defer claim.Release()
	if _, err := os.Stat(live); !os.IsNotExist(err) {
		t.Fatalf("recovered claim did not reset the stale root (stat err = %v)", err)
	}
}
