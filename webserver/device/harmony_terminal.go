package device

import (
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/pkg/errors"
	"github.com/uiautodev/uiautodev/webserver/utils"
)

// harmonyTerminal adapts the interactive shell of the hdc CLI to the
// read/write stream the websocket bridge consumes. The CLI allocates the
// pseudo-terminal on the device and forwards it over the hdc channel, so a
// local pty is all this side needs.
type harmonyTerminal struct {
	cmd       *exec.Cmd
	pty       *os.File
	closeOnce sync.Once
	waitOnce  sync.Once
	exitCode  int
	waitErr   error
}

// OpenTerminal starts `hdc shell` in a pseudo-terminal so the remote shell
// echoes input and supports job control like a local terminal.
func (d *HarmonyOSDevice) OpenTerminal(rows, cols uint16) (TerminalSession, error) {
	cmd := exec.Command(utils.FindBinary("hdc"), "-t", d.connectKey, "shell")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	var (
		file *os.File
		err  error
	)
	if rows > 0 && cols > 0 {
		file, err = pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	} else {
		file, err = pty.Start(cmd)
	}
	if err != nil {
		return nil, errors.Wrap(err, "start hdc shell")
	}
	return &harmonyTerminal{cmd: cmd, pty: file}, nil
}

func (t *harmonyTerminal) Read(p []byte) (int, error) {
	return t.pty.Read(p)
}

func (t *harmonyTerminal) Write(p []byte) (int, error) {
	return t.pty.Write(p)
}

// Resize updates the local pty window size, which also updates the size the
// remote shell sees.
func (t *harmonyTerminal) Resize(rows, cols uint16) error {
	if rows == 0 || cols == 0 {
		return nil
	}
	return pty.Setsize(t.pty, &pty.Winsize{Rows: rows, Cols: cols})
}

// Wait blocks until the hdc process exits and reports its exit code.
func (t *harmonyTerminal) Wait() (int, error) {
	t.waitOnce.Do(func() {
		err := t.cmd.Wait()
		if err == nil {
			return
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.exitCode = exitErr.ExitCode()
			return
		}
		t.waitErr = err
	})
	return t.exitCode, t.waitErr
}

// Close hangs up the pty and kills the hdc process so no shell is left
// running when the browser goes away.
func (t *harmonyTerminal) Close() error {
	var err error
	t.closeOnce.Do(func() {
		err = t.pty.Close()
		if t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
	})
	return err
}
