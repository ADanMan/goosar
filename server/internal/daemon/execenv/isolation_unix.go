//go:build !windows

package execenv

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

type preparationProcessController struct{}

func newPreparationProcessController(cmd *exec.Cmd) (*preparationProcessController, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return &preparationProcessController{}, nil
}

func (*preparationProcessController) attach(_ *exec.Cmd) error {
	return nil
}

func (*preparationProcessController) stop(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func (*preparationProcessController) finish() error {
	return nil
}

func (*preparationProcessController) close() {}
