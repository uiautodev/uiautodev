//go:build !windows

package device

import (
	"context"
	"log/slog"
	"os/exec"

	"github.com/kballard/go-shellquote"
)

func NewCommand(name string, arg ...string) *exec.Cmd {
	slog.Debug("Creating command", "cmd", shellquote.Join(append([]string{name}, arg...)...))
	c := exec.Command(name, arg...)
	return c
}

func NewCommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	slog.Debug("Creating command with context", "cmd", shellquote.Join(append([]string{name}, arg...)...))
	c := exec.CommandContext(ctx, name, arg...)
	return c
}
