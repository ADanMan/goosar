//go:build windows

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	detachedProcess = 0x00000008

	createBreakawayFromJob = 0x01000000
	sigBreak               = syscall.Signal(0x15)
)

func daemonSysProcAttr(withBreakaway bool) *syscall.SysProcAttr {
	flags := uint32(detachedProcess)
	if withBreakaway {
		flags |= createBreakawayFromJob
	}
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: flags,
	}
}

func isAccessDeniedSpawnErr(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
}

func notifyShutdownContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, sigBreak)
}

func repointStdioToErrLog(errLogPath string) {
	f, err := openBoundedErrLog(errLogPath)
	if err != nil {
		return
	}
	_ = os.Stdout.Close()
	_ = os.Stderr.Close()
	h := windows.Handle(f.Fd())
	_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, h)
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, h)
	os.Stdout = f
	os.Stderr = f
}

func openLogForTail(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(
		p,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

func tailLogFile(logPath string, lines int, follow bool) error {
	f, err := openLogForTail(logPath)
	if err != nil {
		return err
	}
	defer func() { f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return err
	}
	size := fi.Size()

	var tailStart int64
	if size > 0 {
		scanBuf := make([]byte, 8192)
		nlCount := 0
		pos := size
	scan:
		for pos > 0 {
			chunk := int64(len(scanBuf))
			if chunk > pos {
				chunk = pos
			}
			pos -= chunk
			f.ReadAt(scanBuf[:chunk], pos)
			for i := chunk - 1; i >= 0; i-- {
				if scanBuf[i] == '\n' {
					nlCount++
					if nlCount > lines {
						tailStart = pos + i + 1
						break scan
					}
				}
			}
		}
	}

	if _, err := f.Seek(tailStart, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.Copy(os.Stdout, f); err != nil {
		return err
	}

	if !follow {
		return nil
	}

	offset, _ := f.Seek(0, io.SeekCurrent)
	buf := make([]byte, 4096)
	for {
		time.Sleep(500 * time.Millisecond)

		if fi, statErr := os.Stat(logPath); statErr == nil && fi.Size() < offset {
			if nf, reopenErr := openLogForTail(logPath); reopenErr == nil {
				f.Close()
				f = nf
				offset = 0
			}
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			os.Stdout.Write(buf[:n])
			offset += int64(n)
		}
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
	}
}
