package agent

import (
	"os/exec"
	"syscall"
)

func newRuntimeCmd(cmd *exec.Cmd) *exec.Cmd {
	configureProcessGroup(cmd)
	cmd.Cancel = func() error {
		signalProcessGroup(cmd.Process, syscall.SIGKILL)
		return nil
	}
	return cmd
}
