//go:build !windows

package agent

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const pollInterval = 10 * time.Millisecond

func hideAgentWindow(*exec.Cmd) {}

func configureProcessGroup(cmd *exec.Cmd) {
	attr := cmd.SysProcAttr
	if attr == nil {
		attr = &syscall.SysProcAttr{}
		cmd.SysProcAttr = attr
	}
	attr.Setpgid = true
}

func runtimeEInitializeRetrySupported() bool { return true }

func signalProcessGroup(p *os.Process, sig syscall.Signal) {
	if p == nil {
		return
	}
	if err := syscall.Kill(-p.Pid, sig); err != nil {
		_ = p.Signal(sig)
	}
}

func waitProcessGroupGone(p *os.Process, timeout time.Duration) bool {
	if p == nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	for {
		if errors.Is(syscall.Kill(-p.Pid, 0), syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pollInterval)
	}
}
