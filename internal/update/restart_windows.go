//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func relaunchProcess(exe string) error {
	// Pass the paths through the environment instead of splicing them into the
	// command line, so cmd.exe does not reinterpret metacharacters such as
	// &, ^, or % in the install path. The variables are expanded inside quotes.
	script := `ping -n 3 127.0.0.1 >nul & cd /d "%UIAUTODEV_UPDATE_DIR%" & start "" "%UIAUTODEV_UPDATE_EXE%"`
	cmd := exec.Command("cmd")
	cmd.Env = append(os.Environ(),
		"UIAUTODEV_UPDATE_DIR="+filepath.Dir(exe),
		"UIAUTODEV_UPDATE_EXE="+exe,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       "cmd /c " + script,
		HideWindow:    true,
		CreationFlags: 0x00000008,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("relaunch app: %w", err)
	}
	return nil
}
