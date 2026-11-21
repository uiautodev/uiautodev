//go:build unix

package utils

import (
	"os/exec"
	"syscall"
)

// StartBackgroundProcess starts a command that runs independently of the parent process.
// On Unix systems, it creates a new session using setsid.
func StartBackgroundProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session, detach from terminal
	}
	return cmd.Start()
}
