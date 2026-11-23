package device

import (
	"io"
	"sync"

	"github.com/codeskyblue/go-adbkit/adb"
)

// androidTerminal adapts a shell protocol v2 session to the single
// read/write stream the websocket bridge consumes. Shell v2 exposes stdout and
// stderr as separate readers, so both are pumped into one pipe; in PTY mode
// the device already merges them, but raw sessions keep working too.
type androidTerminal struct {
	session   *adb.ShellV2Session
	output    *io.PipeReader
	input     *io.PipeWriter
	closeOnce sync.Once
}

// OpenTerminal opens an interactive shell with a pseudo-terminal so readline,
// completion and full-screen programs behave like a local terminal.
func (d *AndroidDevice) OpenTerminal(rows, cols uint16) (TerminalSession, error) {
	session, err := d.getAdbkitDevice().ShellV2WithOptions("sh", adb.ShellV2Options{
		Term: "xterm-256color",
		PTY:  true,
	})
	if err != nil {
		return nil, err
	}

	terminal := newAndroidTerminal(session)
	if rows > 0 && cols > 0 {
		if err := session.Resize(rows, cols); err != nil {
			terminal.Close()
			return nil, err
		}
	}
	return terminal, nil
}

func newAndroidTerminal(session *adb.ShellV2Session) *androidTerminal {
	reader, writer := io.Pipe()
	terminal := &androidTerminal{session: session, output: reader, input: writer}

	var pumps sync.WaitGroup
	for _, stream := range []io.Reader{session.Stdout(), session.Stderr()} {
		pumps.Add(1)
		go func(stream io.Reader) {
			defer pumps.Done()
			_, _ = io.Copy(writer, stream)
		}(stream)
	}
	go func() {
		pumps.Wait()
		_ = writer.Close()
	}()
	return terminal
}

// Read returns the merged stdout/stderr stream; it unblocks with an error when
// the session is closed before the remote shell exits.
func (t *androidTerminal) Read(p []byte) (int, error) {
	return t.output.Read(p)
}

// Write forwards keyboard input to the remote stdin.
func (t *androidTerminal) Write(p []byte) (int, error) {
	return t.session.Write(p)
}

// Resize reports a new terminal window size to the remote PTY.
func (t *androidTerminal) Resize(rows, cols uint16) error {
	return t.session.Resize(rows, cols)
}

// Wait blocks until the remote shell exits and returns its exit code.
func (t *androidTerminal) Wait() (int, error) {
	return t.session.Wait()
}

// Close releases the pipe first so writers blocked on a browser that went
// away can exit, then closes the remote shell.
func (t *androidTerminal) Close() error {
	var err error
	t.closeOnce.Do(func() {
		_ = t.input.CloseWithError(io.ErrClosedPipe)
		err = t.session.Close()
	})
	return err
}
