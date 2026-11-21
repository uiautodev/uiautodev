//go:build windows

package device

import (
	"context"
	"os/exec"
	"syscall"
)

func NewCommand(name string, arg ...string) *exec.Cmd {
	c := exec.Command(name, arg...)
	c.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
	return c
}

func NewCommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, name, arg...)
	c.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
	return c
}
