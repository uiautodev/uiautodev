//go:build windows

package utils

import (
	"os/exec"
	"syscall"
)

// StartBackgroundProcess starts a command that runs independently of the parent process.
// On Windows, it creates a new process group.
func StartBackgroundProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
	return cmd.Start()
}
