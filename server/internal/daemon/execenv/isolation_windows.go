//go:build windows

package execenv

import (
	"fmt"
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type preparationProcessController struct {
	job windows.Handle
}

type jobObjectBasicAccountingInformation struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func newPreparationProcessController(_ *exec.Cmd) (*preparationProcessController, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}

	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("set KILL_ON_JOB_CLOSE: %w", err)
	}

	return &preparationProcessController{job: job}, nil
}

func (c *preparationProcessController) attach(cmd *exec.Cmd) error {
	handle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return fmt.Errorf("open helper process: %w", err)
	}
	defer windows.CloseHandle(handle)

	if err := windows.AssignProcessToJobObject(c.job, handle); err != nil {
		return fmt.Errorf("assign helper to job object: %w", err)
	}
	return nil
}

func (c *preparationProcessController) stop(_ *exec.Cmd) error {
	err := windows.TerminateJobObject(c.job, 1)
	if err == nil {
		return nil
	}
	if active, queryErr := c.activeProcesses(); queryErr == nil && active == 0 {
		return nil
	}
	return err
}

func (c *preparationProcessController) finish() error {
	active, err := c.activeProcesses()
	if err != nil {
		return err
	}
	if active > 0 {
		if err := windows.TerminateJobObject(c.job, 1); err != nil {
			return err
		}
	}
	for {
		if active, err = c.activeProcesses(); err != nil {
			return err
		}
		if active == 0 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (c *preparationProcessController) activeProcesses() (uint32, error) {
	var accounting jobObjectBasicAccountingInformation
	err := windows.QueryInformationJobObject(
		c.job,
		windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&accounting)),
		uint32(unsafe.Sizeof(accounting)),
		nil,
	)
	if err != nil {
		return 0, fmt.Errorf("query job object members: %w", err)
	}
	return accounting.ActiveProcesses, nil
}

func (c *preparationProcessController) close() {
	if c.job == 0 {
		return
	}
	_ = windows.CloseHandle(c.job)
	c.job = 0
}
