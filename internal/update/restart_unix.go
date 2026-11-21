//go:build !windows

package update

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

func relaunchProcess(exe string) error {
	command := "exec " + shellQuote(exe)
	if runtime.GOOS == "darwin" {
		if bundle := appBundle(exe); bundle != "" {
			command = "open -n " + shellQuote(bundle)
		}
	}
	// Wait for the current process to release its listening port before the
	// replacement starts. The helper is detached with its own session.
	cmd := exec.Command("/bin/sh", "-c", "sleep 2; "+command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("relaunch app: %w", err)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
