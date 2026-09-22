//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
)

func daemonSysProcAttr(_ bool) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

func isAccessDeniedSpawnErr(_ error) bool { return false }

func notifyShutdownContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
}

func repointStdioToErrLog(_ string) {}

func tailLogFile(logPath string, lines int, follow bool) error {
	args := []string{"-n", strconv.Itoa(lines)}
	if follow {

		args = append(args, "-F")
	}
	args = append(args, logPath)

	tail := exec.Command("tail", args...)
	tail.Stdout = os.Stdout
	tail.Stderr = os.Stderr
	return tail.Run()
}
