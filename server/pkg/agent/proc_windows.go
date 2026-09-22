//go:build windows

package agent

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

const createNewConsole = 0x00000010

func hideAgentWindow(cmd *exec.Cmd) {
	attr := cmd.SysProcAttr
	if attr == nil {
		attr = &syscall.SysProcAttr{}
		cmd.SysProcAttr = attr
	}
	attr.HideWindow = true
	attr.CreationFlags |= createNewConsole
}

func configureProcessGroup(*exec.Cmd) {}

func runtimeEInitializeRetrySupported() bool { return false }

func signalProcessGroup(p *os.Process, _ syscall.Signal) {
	if p == nil {
		return
	}
	_ = p.Kill()
}

func waitProcessGroupGone(*os.Process, time.Duration) bool { return false }
