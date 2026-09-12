//go:build windows

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func processIsRunning(process *os.Process) error {
	if process == nil || process.Pid <= 0 {
		return errors.New("execution child process is unavailable")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return err
	}
	if status == uint32(windows.WAIT_TIMEOUT) {
		return nil
	}
	return errors.New("execution child process has exited")
}
