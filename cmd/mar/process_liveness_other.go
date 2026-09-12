//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

func processIsRunning(process *os.Process) error {
	if process == nil || process.Pid <= 0 {
		return errors.New("execution child process is unavailable")
	}
	return process.Signal(syscall.Signal(0))
}
